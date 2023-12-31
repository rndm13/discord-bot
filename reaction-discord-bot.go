package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	disc "github.com/bwmarrin/discordgo"

	"database/sql"

	_ "github.com/lib/pq"
)

var (
    token = flag.String("token", "", "A bot token to connect to")
    emoji = flag.String("emoji", "💖", "Emoji to keep track of")
    db_conn = flag.String("db", "", "Database address")
    ownerid = flag.String("owner", "", "Bot owner id")
)

func init() {
    flag.Parse();
    
    if *token == "" {
        log.Fatalln("token flag must be set to your discord token");
    }

    if *db_conn == "" {
        log.Fatalln("db flag must be set to connect to your postgreSQL db");
    }
}

var db *sql.DB

func init() {
    db_, err := sql.Open("postgres", *db_conn);
    db = db_

    if err != nil {
        log.Fatalln("Failed to connect to database: ", err)
    }
    
    init_db(db);
}

var dg *disc.Session

var commands = []*disc.ApplicationCommand{
    {
        Name: "leaderboard",
        Description: "get leaderboard (counts how many reactions users got under their posts)",
    },
}

var command_handlers = map[string]func(s *disc.Session, i *disc.InteractionCreate) {
    "leaderboard": func(s *disc.Session, i *disc.InteractionCreate) {
        if i.Member == nil || i.GuildID == "" { // In a dm or smth
            return
        }

        respondWithContent := func(cont string) {
            s.InteractionRespond(i.Interaction, &disc.InteractionResponse {
                Type: disc.InteractionResponseChannelMessageWithSource,
                Data: &disc.InteractionResponseData{
                    Content: cont,
                },  
            })
        }

        query, err := db.Query(`
        SELECT row_number() OVER (ORDER BY reactions DESC) AS position, *
        FROM (
            SELECT author_id, sum(actual_reaction_count) as reactions
            FROM reacted_messages
            WHERE server_id = $1
            GROUP BY author_id) AS total_reactions
        LIMIT 10;
        `, i.GuildID)

        if err != nil {
            log.Println("Failed to fetch leaderboard: ", err)
            respondWithContent("Failed to fetch leaderboard")
            return
        }

        leaderboard_em := &disc.MessageEmbed{
            Author: &disc.MessageEmbedAuthor{},
            Fields: []*disc.MessageEmbedField{
                {
                    Name: "Position",
                    Value: "",
                    Inline: true,
                },
                {
                    Name: "Username",
                    Value: "",
                    Inline: true,
                },
                {
                    Name: "Reaction count",
                    Value: "",
                    Inline: true,
                },
            },
        }

        personal_em := &disc.MessageEmbed{
            Author: &disc.MessageEmbedAuthor{},
            Fields: []*disc.MessageEmbedField{
                {
                    Name: "Position",
                    Value: "",
                    Inline: true,
                },
                {
                    Name: "Username",
                    Value: "",
                    Inline: true,
                },
                {
                    Name: "Reaction count",
                    Value: "",
                    Inline: true,
                },
            },
        }

        
        for query.Next() {
            position := 0
            author := ""
            reactions := 0

            err := query.Scan(&position, &author, &reactions)

            if err != nil {
                log.Println("Failed to scan query results: ", err);
                respondWithContent("Failed to scan query results")
                return
            }

            user, err := s.GuildMember(i.GuildID, author)

            if err != nil {
                log.Println("Failed to get users info: ", err);
                respondWithContent("Failed to get users info")
                return
            }

            leaderboard_em.Fields[0].Value += fmt.Sprintln(position)
            leaderboard_em.Fields[1].Value += fmt.Sprintln(user.User.Username)
            leaderboard_em.Fields[2].Value += fmt.Sprintln(reactions)

            // cont += fmt.Sprintf("[%v] %v - %v %v\n", position, user.User.Username, reactions, *emoji)
        }

        // cont += "------------------\n"

        personalData := db.QueryRow(`
            SELECT * 
            FROM (
                SELECT row_number() OVER (ORDER BY reactions DESC) AS position, *
                FROM (
                    SELECT author_id, sum(actual_reaction_count) as reactions
                    FROM reacted_messages
                    WHERE server_id = $1
                    GROUP BY author_id
                    ) AS total_reactions
                ) AS total_positions
            WHERE author_id = $2;
        `, i.GuildID, i.Member.User.ID)
        
        if personalData.Err() != nil {
            log.Println("Error: failed to get personal data for user: ", i.Member.User.ID);
        } else {
            personal_em.Fields[1].Value = i.Member.User.Username

            personalPosition := 0
            personalReactions := 0
            id := 0
            
            err = personalData.Scan(&personalPosition, &id, &personalReactions)
            if err != nil { // probably no records found
                personal_em.Fields[0].Value = "-"
                personal_em.Fields[2].Value = "-"
            } else {
                personal_em.Fields[0].Value = fmt.Sprintln(personalPosition)
                personal_em.Fields[2].Value = fmt.Sprintln(personalReactions)
            }
        }
        
        s.InteractionRespond(i.Interaction, &disc.InteractionResponse {
            Type: disc.InteractionResponseChannelMessageWithSource,
            Data: &disc.InteractionResponseData{
                Embeds: []*disc.MessageEmbed {
                    leaderboard_em,
                    personal_em,
                },
            },  
        })
    },
}

func add_commands(s *disc.Session, g *disc.Guild) {
    log.Println("Adding commands for guild: ", g.ID, g.Name);

   	registeredCommands := make([]*disc.ApplicationCommand, len(commands))
	for i, v := range commands {
		cmd, err := s.ApplicationCommandCreate(s.State.User.ID, g.ID, v)
		if err != nil {
			log.Panicf("Cannot create '%v' command: %v", v.Name, err)
		}
		registeredCommands[i] = cmd
	} 
}

func main() {
    dg_, err := disc.New("Bot " + *token)
    dg = dg_

    if err != nil {
        log.Fatalln("Failed to create a bot: ", err)
    }

    dg.AddHandler(reactionAdd)
    dg.AddHandler(reactionRemove)
	
    dg.Identify.Intents = disc.IntentsGuildMessages | disc.IntentsDirectMessageReactions | disc.IntentsGuildMessageReactions
	
    dg.AddHandler(func(s *disc.Session, i *disc.InteractionCreate) {
		if handler, ok := command_handlers[i.ApplicationCommandData().Name]; ok {
            if i.Member != nil { // inside a guild
                log.Printf("User %v used command %v on server %v", i.Member.User.Username, i.ApplicationCommandData().Name, i.GuildID);
            }
            handler(s, i)
		}
	})

	dg.AddHandler(func(s *disc.Session, r *disc.Ready) {
		log.Printf("Logged in as: %v#%v", s.State.User.Username, s.State.User.Discriminator)
        for _, g := range s.State.Guilds {
            add_commands(s, g);
        }
		log.Printf("Finished adding commands");
		log.Printf("Adding default settings");
        insert_settings(s.State.Guilds);
		log.Printf("Finished adding default settings");
	})

    dg.AddHandler(handleConfigCommands)

    err = dg.Open()

    if err != nil {
        log.Fatalln("Failed to create connection: ", err)
    }
    defer dg.Close()

	// Wait here until CTRL-C or other term signal is received.
    log.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<- sc
}

func makeLink(msg *disc.Message) string {
    return fmt.Sprintf("<https://discord.com/channels/%v/%v/%v>", msg.GuildID, msg.ChannelID, msg.ID);
}

func handleConfigCommands(s *disc.Session, m *disc.MessageCreate) {
    if len(m.Content) <= 3 || m.Content[0:3] != "rc!" {
        return
    }

    // TODO: figure out how to allow admins to use config commands
    allowed_admin := /*m.Member.Permissions & disc.PermissionAdministrator > 0 ||*/ m.Author.ID == *ownerid

    if !allowed_admin {
        s.ChannelMessageSend(m.ChannelID, "You aren't allowed to use config commands, only bot owner can use it (admins should too but bot dev didn't figure out how to do that)");
        return;
    }

    args := strings.Split(m.Content[3:], " ")

    switch args[0] {
    case "announcements":
        if (len(args) < 2) {
            s.ChannelMessageSend(m.ChannelID, "This function takes 2 params:\nannouncement channel id\nminimum message reactions")
            return 
        }

        chan_id := args[1]
        min_react, err := strconv.Atoi(args[2])

        if err != nil {
            s.ChannelMessageSend(m.ChannelID, "minimum reactions should be a number")
            return
        }

        _, err = db.Query(`
            UPDATE server_settings
            SET announcement_channel_id = $2,
                announcement_min_reactions = $3
            WHERE server_id = $1;
        `, m.GuildID, chan_id, min_react);

        if err != nil {
            log.Println("Error: failed to update server settings: ", err);
            return
        }

    default: 
        s.ChannelMessageSend(m.ChannelID, "Unknown config command");
    }
}

type server_config struct {
    announcement_channel_id string
    announcement_min_reactions int
};

func getServerConfig(server_id string) (server_config, error) {
    conf := server_config{"",0}

    config := db.QueryRow(`
        SELECT announcement_channel_id, announcement_min_reactions FROM server_settings WHERE server_id = $1
    `, server_id);
    
    if config.Err() != nil {
        return conf, config.Err()
    }

    err := config.Scan(&conf.announcement_channel_id, &conf.announcement_min_reactions)
    if err != nil {
        return conf, err
    }

    return conf, nil
}

func getAnnouncedMessage(s *disc.Session, original *disc.Message) *disc.Message {
    query := db.QueryRow(`
        SELECT announcement_channel_id, announced_message_id
        FROM reacted_messages
        INNER JOIN server_settings ON server_settings.server_id = reacted_messages.server_id
        WHERE reacted_messages.server_id = $1 AND reacted_messages.channel_id = $2 AND reacted_messages.message_id = $3
    `, original.GuildID, original.ChannelID, original.ID)
    
    if query.Err() != nil {
        log.Println("Failed to query announced message: ", query.Err());
        return nil
    }

    announced_message_id := ""
    announcement_channel_id := ""

    err := query.Scan(&announcement_channel_id, &announced_message_id)

    if err != nil {
        log.Printf("Failed to scan query announced message: %v", err);
        return nil
    }

    msg, err := s.ChannelMessage(announcement_channel_id, announced_message_id)

    if err != nil {
        log.Printf("Failed to get announced message %v %v: %v", announcement_channel_id, announced_message_id, err);
        return nil
    }

    return msg
}

func reactionAdd(s *disc.Session, m *disc.MessageReactionAdd) {
    if m.Emoji.Name != *emoji {
        return
    }

    if m.GuildID == "" {
        log.Printf("User %s reacted with matching emote ... in a dm\n", m.UserID);
        return 
    }

    log.Printf("User %s reacted with matching emote\n", m.UserID);
    
    msg, err := s.ChannelMessage(m.ChannelID, m.MessageID)

    if err != nil {
        log.Println("Error: failed to get channel message reacted to: ", err);
        return;
    }
    
    msg.GuildID = m.GuildID // needed for makeLink to work because for some reason it's not set?????????/
   	
    if msg.Author.Bot { 
		return
	} 

    author_react := m.Member.User.ID == msg.Author.ID

    to_increment := 1
    if author_react {
        to_increment = 0
    }

    _, err = db.Query(`
        UPDATE reacted_messages
        SET reaction_count = reaction_count + $4,
            author_reacted = author_reacted OR $5
        WHERE 
            server_id = $1 AND 
            channel_id = $2 AND 
            message_id = $3;
    `, msg.GuildID, msg.ChannelID, msg.ID, to_increment, author_react);

    if err != nil {
        log.Println("Error: db update query: ", err);
        return;
    }
    
    ins := db.QueryRow(`
        INSERT INTO reacted_messages (server_id, channel_id, message_id, author_id, reaction_count, author_reacted)
        SELECT $1, $2, $3, $4, $5, $6
        WHERE NOT EXISTS (SELECT 1 FROM reacted_messages WHERE server_id = $1 AND channel_id = $2 AND message_id = $3);
    `, msg.GuildID, msg.ChannelID, msg.ID, msg.Author.ID, 1, author_react);

    if ins.Err() != nil {
        log.Println("Error: db insert query: ", err);
        return;
    }

    sendOrEditAnnouncement(s, msg)

    log.Println("Added or updated record to reacted_messages");
}

func reactionRemove(s *disc.Session, m *disc.MessageReactionRemove) {
    if (m.Emoji.Name != *emoji) {
        return
    }
    
    if m.GuildID == "" {
        log.Printf("User %s removed matching emote ... in a dm\n", m.UserID);
        return 
    }

    log.Printf("User %s removed matching emote\n", m.UserID);
    
    msg, err := s.ChannelMessage(m.ChannelID, m.MessageID)
    msg.GuildID = m.GuildID // needed to work because for some reason it's not set?????????/

    if msg.Author.ID == s.State.User.ID {
		return
	} 

    if err != nil {
        log.Println("Error: failed to get channel message reacted to: ", err);
        return;
    }
    
    author_unreact := m.UserID == msg.Author.ID
    to_decrement := 1
    if !author_unreact {
        to_decrement = 0
    }
        
    _, err = db.Query(`
        UPDATE reacted_messages
        SET reaction_count = reaction_count - $4,
            author_reacted = author_reacted AND NOT $5
        WHERE 
            server_id = $1 AND 
            channel_id = $2 AND 
            message_id = $3;
    `, msg.GuildID, msg.ChannelID, msg.ID, to_decrement, author_unreact);
    
    sendOrEditAnnouncement(s, msg)

    if err != nil {
        log.Println("Error: db update query: ", err);
        return;
    }

    log.Println("Updated record to reacted_messages");
}

func sendOrEditAnnouncement(s *disc.Session, orig *disc.Message) {
    conf, err := getServerConfig(orig.GuildID)
    
    if err != nil {
        log.Println("Failed to get server config: ", err);
        return
    }

    query := db.QueryRow(`
        SELECT announced_message_id, actual_reaction_count, author_reacted
        FROM reacted_messages
        WHERE server_id = $1 AND channel_id = $2 AND message_id = $3
    `, orig.GuildID, orig.ChannelID, orig.ID);

    if query.Err() != nil {
        log.Println("Failed to query original message data: ", err);
        return
    }

    announ_id := &sql.NullString{}
    count := 0
    author := false

    err = query.Scan(announ_id, &count, &author)
    
    if err != nil {
        log.Println("Failed to scan original message data: ", err);
        return
    }

    if conf.announcement_min_reactions > count {
        return
    }

    attach_urls := ""
    for _, attach := range orig.Attachments {
        attach_urls += attach.URL;
    }

    self_react := ""
    if author {
        self_react = "(self react)"
    }
    
    content := fmt.Sprintf("%v %v %v; by %v [original message](%v)\n\n%v%v", count, *emoji, self_react, orig.Author.Username, makeLink(orig), orig.Content, attach_urls)

    if announ_id.Valid { // needs to be edited
        _, err := s.ChannelMessageEditComplex(
            &disc.MessageEdit{
                ID: announ_id.String,
                Channel: conf.announcement_channel_id,
                Content: &content,
            },
        )
        
        if err != nil {
            log.Println("Error: failed to send announcement message: ", err)
            return
        }
    } else { // needs to be sent
        sentmsg, err := s.ChannelMessageSendComplex(
            conf.announcement_channel_id,
            &disc.MessageSend{
                Content: content,
            },
        )
        
        if err != nil {
            log.Println("Error: failed to send announcement message: ", err)
            return
        }

        _, err = db.Query(`
            UPDATE reacted_messages
            SET announced_message_id = $4
            WHERE 
                server_id = $1 AND 
                channel_id = $2 AND 
                message_id = $3;
        `, orig.GuildID, orig.ChannelID, orig.ID, sentmsg.ID)

        if err != nil {
            log.Println("Error: failed to update original message record to set announced message id: ", err)
        }
    }
}

func insert_settings(g []*disc.Guild) {
    for _, v := range g {
        _, err := db.Query(`
            INSERT INTO server_settings (server_id) VALUES ($1)
        `, v.ID);
        if err != nil {
            // Commented out so doesn't print out when server settings already exist
            // log.Println("failed to initialize settings for guild ", v.ID, ": ", err);
        }
    }
}

func init_db(db *sql.DB) {
    time.Sleep(1 * time.Second);

    _, err := db.Query(`
        CREATE TABLE IF NOT EXISTS server_settings(
            server_id TEXT PRIMARY KEY,

            announcement_channel_id TEXT,
            announcement_min_reactions INT
        );

        CREATE TABLE IF NOT EXISTS reacted_messages(
            id SERIAL PRIMARY KEY,
            
            server_id TEXT NOT NULL,
            channel_id TEXT NOT NULL,
            message_id TEXT NOT NULL,
            author_id TEXT NOT NULL,

            reaction_count INT NOT NULL,
            author_reacted BOOL NOT NULL,
            actual_reaction_count INT GENERATED ALWAYS AS (reaction_count - author_reacted::int) STORED,

            announced_message_id TEXT
        );
    `);

    if err != nil {
        log.Fatalln("Failed to initialize database", err);
    }

    log.Println("Successfully initialized database");
}
