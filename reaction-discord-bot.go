package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	disc "github.com/bwmarrin/discordgo"
)

var (
    // token_env, token_env_set = os.LookupEnv("DISCORD_TOKEN");
    token = flag.String("token", "", "A bot token to connect to")
    emoji = flag.String("emoji", "💖", "Emoji to keep track of")
)

func main() {
    flag.Parse();
    dg, err := disc.New("Bot " + *token)

    if err != nil {
        log.Fatalln("failed to create a bot: ", err)
    }

    dg.AddHandler(reactionAdd)
    dg.AddHandler(reactionRemove)
	
    dg.Identify.Intents = disc.IntentsGuildMessages | disc.IntentsDirectMessageReactions | disc.IntentsGuildMessageReactions
    err = dg.Open()

    if err != nil {
        log.Fatalln("failed to create connection: ", err)
    }

	// Wait here until CTRL-C or other term signal is received.
    log.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<- sc
    
    dg.Close()
}

// This function will be called (due to AddHandler above) every time a new
// message is created on any channel that the authenticated bot has access to.
func reactionAdd(s *disc.Session, m *disc.MessageReactionAdd) {
    if (m.Emoji.Name == *emoji) {
        log.Printf("User %s reacted with matching emote\n", m.UserID);
        // s.ChannelMessageSend(m.ChannelID, "You reacted with correct emote")
    }
}

func reactionRemove(s *disc.Session, m *disc.MessageReactionRemove) {
    if (m.Emoji.Name == *emoji) {
        log.Printf("User %s removed matching emote\n", m.UserID);
        // s.ChannelMessageSend(m.ChannelID, "You reacted with correct emote")
    }
}
