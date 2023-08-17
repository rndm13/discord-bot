package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
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
    db, err := sql.Open("postgres", *db_conn);

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
    dg, err := disc.New("Bot " + *token)

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
	})

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

func reactionAdd(s *disc.Session, m *disc.MessageReactionAdd) {
    if (m.Emoji.Name != *emoji) {
        return
    }
    log.Printf("User %s reacted with matching emote\n", m.UserID);
}

func reactionRemove(s *disc.Session, m *disc.MessageReactionRemove) {
    if (m.Emoji.Name != *emoji) {
        return
    }
    log.Printf("User %s removed matching emote\n", m.UserID);
}

func init_db(db *sql.DB) {
    time.Sleep(1 * time.Second);

    _, err := db.Query(`
CREATE TABLE IF NOT EXISTS server_settings(
    server_id VARCHAR PRIMARY KEY,

    announcement_channel_id VARCHAR,
    announcement_min_reactions INT
);

CREATE TABLE IF NOT EXISTS reacted_messages(
    id SERIAL PRIMARY KEY,
    
    server_id VARCHAR NOT NULL,
    channel_id VARCHAR NOT NULL,
    message_id VARCHAR NOT NULL,
    author_id VARCHAR NOT NULL,
    reaction_count INT NOT NULL,
    author_reacted BOOL NOT NULL,
    actual_reaction_conut INT GENERATED ALWAYS AS (reaction_count - author_reacted::int) STORED,

    announced_message_id VARCHAR NOT NULL
);
`);

    if err != nil {
        log.Fatalln("Failed to initialize database", err);
    }
    log.Println("Successfully initialized database");
}
