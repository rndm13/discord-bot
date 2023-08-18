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

func reactionAdd(s *disc.Session, m *disc.MessageReactionAdd) {
    if m.Emoji.Name != *emoji {
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
    `,m.GuildID, m.ChannelID, m.MessageID);
    announced_message_id := "";

    if announ != nil {
        announ.Scan(&announced_message_id, &author_react)
    }

    actual_count := count
    if author_react {
        actual_count--
    }
    
    if announced_message_id == "" {
        config := db.QueryRow(`
    SELECT announcement_channel_id, announcement_min_reactions FROM server_settings WHERE server_id = $1
        `, m.GuildID);
        
        if config == nil {
            log.Printf("Error: failed to find config for server: %v:%v\n", m.GuildID, err);
        } else {
            channel_id := "";
            min_react := 0;

            config.Scan(&channel_id, &min_react)

            if actual_count >= min_react {
                sentmsg, err := s.ChannelMessageSendComplex(
                    channel_id, 
                    &disc.MessageSend{
                        Content: fmt.Sprintf("by %v, %v %v (original message: %v)\n%v", msg.Author.Username, actual_count, *emoji, makeLink(msg), msg.Content),
                        // Embeds: msg.Embeds,
                    },
                );

                if err != nil {
                    log.Println("Failed to sent announcement message: ", err);
                } else {
                    announced_message_id = sentmsg.ID
                }
            }
        }
    }

    _, err = db.Query(`
UPDATE reacted_messages
SET reaction_count = $5,
    author_reacted = author_reacted OR $4,
    announced_message_id = $6
WHERE 
    server_id = $1 AND 
    channel_id = $2 AND 
    message_id = $3;
    `, msg.GuildID, msg.ChannelID, msg.ID, author_react, count, announced_message_id);

    if err != nil {
        log.Println("Error: db update query: ", err);
        return;
    }

    _, err = db.Query(`
INSERT INTO reacted_messages (server_id, channel_id, message_id, reaction_count, author_reacted, announced_message_id)
SELECT $1, $2, $3, $5, $4, $6
WHERE NOT EXISTS (SELECT 1 FROM reacted_messages WHERE server_id = $1 AND channel_id = $2 AND message_id = $3);
    `, msg.GuildID, msg.ChannelID, msg.ID, author_react, count, announced_message_id);

    if err != nil {
        log.Println("Error: db insert query: ", err);
        return;
    }

    log.Println("Added or updated record to reacted_messages");
}

func reactionRemove(s *disc.Session, m *disc.MessageReactionRemove) {
    if (m.Emoji.Name != *emoji) {
        return
    }
    log.Printf("User %s removed matching emote\n", m.UserID);
    
    msg, err := s.ChannelMessage(m.ChannelID, m.MessageID)

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
        
    _, err = db.Query(`
UPDATE reacted_messages
SET reaction_count = $5,
    author_reacted = author_reacted AND $4
WHERE 
    server_id = $1 AND 
    channel_id = $2 AND 
    message_id = $3;
    `, msg.GuildID, msg.ChannelID, msg.ID, author_react, count);

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
