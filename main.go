package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/jasonlvhit/gocron"
	"github.com/joho/godotenv"
	"github.com/shkh/lastfm-go/lastfm"
	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/bwmarrin/discordgo"
)

var dSession *discordgo.Session
var lastFMApi *lastfm.Api
var dyn *dynamodb.Client
var spotifyClient *spotify.Client
var spotifyConfig clientcredentials.Config

func main() {
	err := godotenv.Load("/.aws/config/.env")
	check(err)

	fmt.Println("INIT...")

	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion("us-west-2"))
	check(err)
	dyn = dynamodb.NewFromConfig(cfg)

	dSession, err = discordgo.New("Bot " + os.Getenv("DISCORD_TOKEN"))
	check(err)

	lastFMApi = lastfm.New(os.Getenv("LASTFM_API_KEY"), os.Getenv("LASTFM_API_SECRET"))

	dSession.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		fmt.Printf("Logged in as %s\n", s.State.User.Username)
	})
	dSession.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if h, ok := commandHandlers[i.ApplicationCommandData().Name]; ok {
			h(s, i)
		}
	})

	ctx := context.Background()
	spotifyConfig = clientcredentials.Config{
		ClientID:     os.Getenv("SPOTIFY_ID"),
		ClientSecret: os.Getenv("SPOTIFY_SECRET"),
		TokenURL:     spotifyauth.TokenURL,
	}

	spotifyClient = refreshSpotifyClient(ctx, spotifyConfig)

	fmt.Println("Starting")
	err = dSession.Open()
	check(err)

	registeredCommands := make([]*discordgo.ApplicationCommand, len(commands))
	for i, v := range commands {
		cmd, err := dSession.ApplicationCommandCreate(dSession.State.User.ID, v.GuildID, v)
		check(err)
		registeredCommands[i] = cmd
	}

	gocron.Every(1).Saturday().At("12:30").Do(func() {
		embeds := _triggerWeeklyDigest()
		dSession.ChannelMessageSendEmbeds(os.Getenv("CHANNEL_ID"), embeds)
	})

	dSession.UpdateListeningStatus("your music :eyes:")

	<-gocron.Start()
	defer dSession.Close()

	stop := make(chan os.Signal, 1)
	fmt.Printf("Press Ctrl+C to exit\n")

	<-stop
	fmt.Printf("Gracefully shutting down")
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
		"ping": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "Hey there! Congratulations, you just executed your first slash command",
				},
			})
		},
		"link-lastfm": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			options := i.ApplicationCommandData().Options
			userId, err := strconv.Atoi(i.Member.User.ID)
			check(err)
			_, err = putDocdbEntry(LastFMEntry{userId, options[0].StringValue()})
			if err == nil {
				content := fmt.Sprintf("(<@%d>) :link: (%s)", userId, options[0].StringValue())
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: content,
					},
				})
			} else {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: "There was an unexpected error...",
					},
				})
				print(err)
			}
		},
		"manual-trigger": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "manually triggered... (please wait a few seconds)",
				},
			})
			embeds := _triggerWeeklyDigest()

			s.ChannelMessageSendEmbeds(i.ChannelID, embeds)
		},
	}
)

type LastFMEntry struct {
	DiscordID  int    `dynamodbav:"discordID"`
	LastFMName string `dynamodbav:"lastFMName"`
}

func _triggerWeeklyDigest() []*discordgo.MessageEmbed {
	dSession.UpdateListeningStatus("CRUNCHING THE NUMBERS")
	res, err := dyn.Scan(context.Background(), &dynamodb.ScanInput{
		TableName: aws.String(os.Getenv("TABLE_NAME")),
	})
	check(err)

	items := res.Items
	embeds := []*discordgo.MessageEmbed{}

	for _, item := range items {
		user := LastFMEntry{}
		attributevalue.UnmarshalMap(item, &user)

		topTracks, err := lastFMApi.User.GetTopTracks(lastfm.P{
			"user":   user.LastFMName,
			"period": "7day",
			"limit":  5,
		})
		check(err)

		// If there are no tracks, skip everything else
		if len(topTracks.Tracks) <= 0 {
			embeds = append(embeds, &discordgo.MessageEmbed{
				Type:        discordgo.EmbedTypeArticle,
				Title:       fmt.Sprintf("%s's plays: %d", user.LastFMName, topTracks.Total),
				Description: fmt.Sprintf("**<@%d> listened to nothing this week...**", user.DiscordID),
				Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: "https://media.discordapp.net/attachments/442416724357283841/1063684079431721032/image.png"},
			})
			continue
		}

		topArtists, err := lastFMApi.User.GetTopArtists(lastfm.P{
			"user":   user.LastFMName,
			"period": "7day",
			"limit":  5,
		})
		check(err)

		// dailyListenCount := "**Daily Breakdown (i fixed it :) ):** "
		// for i := -7; i >= 0; i++ {
		// 	res, err := lastFMApi.User.GetRecentTracks(lastfm.P{
		// 		"user": user.LastFMName,
		// 		"to":   time.Now().AddDate(0, 0, i+1),
		// 		"from": time.Now().AddDate(0, 0, i),
		// 	})
		// 	check(err)

		// 	fmt.Printf("res: %+v\n\n%e", res, err)

		// 	dailyListenCount += fmt.Sprintf("%d:%d ", i, res.Total)
		// }

		trackInfo := fmt.Sprintf("**<@%d>'s top listens.**", user.DiscordID)

		artistInfo := fmt.Sprintf("**Top Artists.**")

		for _, track := range topTracks.Tracks {
			trackInfo = trackInfo + fmt.Sprintf("\n%s. **%s** by %s (%s)", track.Rank, track.Name, track.Artist.Name, track.PlayCount)
		}

		for _, artist := range topArtists.Artists {
			artistInfo = artistInfo + fmt.Sprintf("\n%s. %s (%s)", artist.Rank, artist.Name, artist.PlayCount)
		}

		spotifyClient = refreshSpotifyClient(context.Background(), spotifyConfig)

		res, err := spotifyClient.Search(context.Background(), topArtists.Artists[0].Name, spotify.SearchTypeArtist)
		var artistUrl string
		if err != nil {
			fmt.Printf("ERROR SEARFCHING %e", err)
			artistInfo = "https://media.discordapp.net/attachments/442416724357283841/1063684079431721032/image.png"
		} else {
			artistUrl = res.Artists.Artists[0].Images[0].URL
		}

		embeds = append(embeds, &discordgo.MessageEmbed{
			Type:        discordgo.EmbedTypeArticle,
			Title:       fmt.Sprintf("%s's plays: %d", user.LastFMName, topTracks.Total),
			Description: fmt.Sprintf("%s\\n\n%s,", trackInfo, artistInfo),
			Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: artistUrl},
		})
	}

	dSession.UpdateListeningStatus("your music :eyes:")
	return embeds
}

func putDocdbEntry(entry LastFMEntry) (*dynamodb.PutItemOutput, error) {
	r, err := attributevalue.MarshalMap(entry)
	check(err)

	res, err := dyn.PutItem(context.Background(), &dynamodb.PutItemInput{
		TableName: aws.String(os.Getenv("TABLE_NAME")),
		Item:      r,
	})

	return res, err
}

func check(err error) {

	if err != nil {
		log.Fatalf("ERROR: %s", err)
	}
}

func refreshSpotifyClient(ctx context.Context, cfg clientcredentials.Config) *spotify.Client {
	newToken, err := cfg.TokenSource(ctx).Token()
	check(err)

	newClient := spotifyauth.New().Client(ctx, newToken)
	client := spotify.New(newClient)

	return client
}
