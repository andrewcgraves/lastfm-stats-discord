package main

import (
	"fmt"
	"os"

	"github.com/andrewcgraves/lastfm-stats-discord/cmd"
	"github.com/andrewcgraves/lastfm-stats-discord/framework"
	"github.com/bwmarrin/discordgo"
	"github.com/jasonlvhit/gocron"
	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load("/.aws/config/.env")
	if err != nil {
		fmt.Printf("Failed to load .env: %s", err.Error())
	}

	framework.InitDBConnection()
	dClient := framework.InitDiscordConnection(os.Getenv("DISCORD_TOKEN"), commands, commandHandlers)
	framework.InitLastFM(os.Getenv("LASTFM_API_KEY"), os.Getenv("LASTFM_API_SECRET"))
	framework.InitSpotifyService(os.Getenv("SPOTIFY_ID"), os.Getenv("SPOTIFY_SECRET"))
	framework.InitBackblaze(os.Getenv("BACKBLAZE_ACCOUNT"), os.Getenv("BACKBLAZE_APPLICATION_KEY"), os.Getenv("BACKBLAZE_BUCKET_NAME"))
	fmt.Println("Services Started...")

	fmt.Println("Scheduling Gochron")
	gocron.Every(1).Saturday().At("12:30").Do(func() {
		embeds := framework.TriggerWeeklyDigest()
		dClient.SendComplexMessageToChannel(os.Getenv("CHANNEL_ID"), embeds)
	})

	// fmt.Println("Making channels...")
	// stop := make(chan os.Signal, 1)
	// signal.Notify(stop, os.Interrupt)
	// fmt.Println("Press Ctrl+C to exit")
	// fmt.Println("Starting Gochron")
	<-gocron.Start()

	// <-stop
	// fmt.Printf("Gracefully shutting down")
	// framework.TerminateDiscordConnection(dClient)
}

var (
	commands = []*discordgo.ApplicationCommand{
		{
			Name:        "ping",
			Description: "Ping test command",
		},
		{
			Name:        "link-lastfm",
			Description: "Register your lastfm username with your discord account",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "lastfm-username",
					Description: "Last.fm Username",
					Required:    true,
				},
			},
		},
		{
			Name:        "manual-trigger",
			Description: "get the weekly lastfm stats for registered users",
		},
	}

	commandHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"ping":           cmd.Ping,
		"link-lastfm":    cmd.LinkLastFM,
		"manual-trigger": cmd.ManualTrigger,
	}
)
