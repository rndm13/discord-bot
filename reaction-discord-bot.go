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
        Description: "get leaderboard (counts how many reactions user got under their posts)",
    },
}

var command_handlers = map[string]func(s *disc.Session, i *disc.InteractionCreate) {
    "leaderboard": func(s *disc.Session, i *disc.InteractionCreate) {
        s.InteractionRespond(i.Interaction, &disc.InteractionResponse{
            Type: disc.InteractionResponseChannelMessageWithSource,
            Data: &disc.InteractionResponseData{
                Content: "todo",
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

func editAnnouncement(s *disc.Session, actual_count int, original *disc.Message) {
    // Kinda stupid but should work....
    edit_num := false
    num_start := 0
    num_end := 0
    
    msg := getAnnouncedMessage(s, original)

    if msg == nil {
        log.Printf("Failed to get announcement message to edit");
        return
    }

    for i, v := range msg.Content {
        if v >= '0' && v <= '9' {
            if num_start == 0 {
                edit_num = true
                num_start = i
            }
            if edit_num {
                num_end = i
            }
        } else {
            edit_num = false
        }
    }

    edited_content := fmt.Sprintf("%v%v%v", msg.Content[0:num_start], actual_count, msg.Content[num_end + 1:])

    log.Println(msg.ChannelID, msg.ID, edited_content)
    _, err := s.ChannelMessageEditComplex(&disc.MessageEdit{
        Content: &edited_content,

        ID: msg.ID,
        Channel: msg.ChannelID,
    })

    if err != nil {
        log.Printf("Failed to edit message: %v", err);
    }
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
    msg.GuildID = m.GuildID // needed for makeLink to work because for some reason it's not set?????????/

    if err != nil {
        log.Println("Error: failed to get channel message reacted to: ", err);
        return;
    }
   	
    if msg.Author.ID == s.State.User.ID {
		return
	} 

    count := 1

    for _, mr := range msg.Reactions {
        if mr.Emoji.Name != *emoji {
            continue
        }

        count = mr.Count
        break
    }
    
    author_react := m.Member.User.ID == msg.Author.ID
    
    announ := db.QueryRow(`
        SELECT announced_message_id, author_reacted FROM reacted_messages WHERE server_id = $1 AND channel_id = $2 AND message_id = $3
    `,m.GuildID, m.ChannelID, m.MessageID)
    announced_message_id := ""
    var announced_message *disc.Message = nil

    if announ != nil {
        announ.Scan(&announced_message_id, &author_react)
    }

    actual_count := count
    if author_react {
        actual_count--
    }
    
    if announced_message_id == "" {
        conf, err := getServerConfig(m.GuildID)

        if err != nil {
            log.Println("Error: failed to get server config: ", err)
        } else if actual_count >= conf.announcement_min_reactions {
            attach_urls := ""
            for _, ma := range msg.Attachments {
                attach_urls += ma.URL;
            }
            sentmsg, err := s.ChannelMessageSendComplex(
                conf.announcement_channel_id, 
                &disc.MessageSend{
                    Content: fmt.Sprintf("by %v, %v %v (original message: %v)\n\n%v%v", msg.Author.Username, actual_count, *emoji, makeLink(msg), msg.Content, attach_urls),

                    // Embeds: msg.Embeds,
                },
            );

            if err != nil {
                log.Println("Failed to sent announcement message: ", err);
            } else {
                announced_message = sentmsg
                announced_message_id = announced_message.ID
            }
        }
    }

    upd := db.QueryRow(`
        UPDATE reacted_messages
        SET reaction_count = $5,
            author_reacted = author_reacted OR $4,
            announced_message_id = $6
        WHERE 
            server_id = $1 AND 
            channel_id = $2 AND 
            message_id = $3;
    `, msg.GuildID, msg.ChannelID, msg.ID, author_react, count, announced_message_id);

    if upd.Err() != nil {
        log.Println("Error: db update query: ", err);
        return;
    }
    
    editAnnouncement(s, actual_count, msg);

    ins := db.QueryRow(`
        INSERT INTO reacted_messages (server_id, channel_id, message_id, reaction_count, author_reacted, announced_message_id)
        SELECT $1, $2, $3, $5, $4, $6
        WHERE NOT EXISTS (SELECT 1 FROM reacted_messages WHERE server_id = $1 AND channel_id = $2 AND message_id = $3);
    `, msg.GuildID, msg.ChannelID, msg.ID, author_react, count, announced_message_id);

    if ins.Err() != nil {
        log.Println("Error: db insert query: ", err);
        return;
    }

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
    
    count := 0

    for _, mr := range msg.Reactions {
        if mr.Emoji.Name != *emoji {
            continue
        }

        count = mr.Count
        break
    }
    
    author_react := count > 0 && m.UserID == msg.Author.ID
        
    actual_count := count
    if author_react {
        actual_count--
    }

    _, err = db.Query(`
        UPDATE reacted_messages
        SET reaction_count = $5,
            author_reacted = author_reacted AND $4
        WHERE 
            server_id = $1 AND 
            channel_id = $2 AND 
            message_id = $3;
    `, msg.GuildID, msg.ChannelID, msg.ID, author_react, count);
    
    editAnnouncement(s, actual_count, msg);

    if err != nil {
        log.Println("Error: db update query: ", err);
        return;
    }

    log.Println("Updated record to reacted_messages");
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
            reaction_count INT NOT NULL,
            author_reacted BOOL NOT NULL,
            actual_reaction_conut INT GENERATED ALWAYS AS (reaction_count - author_reacted::int) STORED,

            announced_message_id TEXT
        );
    `);

    if err != nil {
        log.Fatalln("Failed to initialize database", err);
    }

    log.Println("Successfully initialized database");
}
