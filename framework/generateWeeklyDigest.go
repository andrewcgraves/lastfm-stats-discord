package framework

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/bwmarrin/discordgo"
	"github.com/shkh/lastfm-go/lastfm"
	"github.com/zmb3/spotify/v2"
)

func TriggerWeeklyDigest() ([]*discordgo.MessageEmbed, string) {
	res, _ := dyn.Scan(context.Background(), &dynamodb.ScanInput{
		TableName: aws.String(os.Getenv("TABLE_NAME")),
	})

	items := res.Items
	embeds := []*discordgo.MessageEmbed{}
	users := []LastFMEntry{}

	for _, item := range items {
		user := LastFMEntry{}
		attributevalue.UnmarshalMap(item, &user)
		users = append(users, user)

		topTracks, _ := lastFMApi.User.GetTopTracks(lastfm.P{
			"user":   user.LastFMName,
			"period": "7day",
			"limit":  5,
		})

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

		topArtists, _ := lastFMApi.User.GetTopArtists(lastfm.P{
			"user":   user.LastFMName,
			"period": "7day",
			"limit":  5,
		})

		trackInfo := fmt.Sprintf("**<@%d>'s top listens.**", user.DiscordID)
		artistInfo := fmt.Sprintf("**Top Artists.**")

		for _, track := range topTracks.Tracks {
			trackInfo = trackInfo + fmt.Sprintf("\n%s. **%s** by %s (%s)", track.Rank, track.Name, track.Artist.Name, track.PlayCount)
		}

		for _, artist := range topArtists.Artists {
			artistInfo = artistInfo + fmt.Sprintf("\n%s. %s (%s)", artist.Rank, artist.Name, artist.PlayCount)
		}

		spotifyClient = refreshSpotifyClient()

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

	path := GenerateDailyActivityGraph(users)
	url, _ := UploadFile(path, path)
	os.Remove(path)
	fmt.Println(url)

	return embeds, url
}
