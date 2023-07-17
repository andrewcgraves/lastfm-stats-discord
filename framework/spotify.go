package framework

import (
	"context"
	"fmt"
	"os"

	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2/clientcredentials"
)

var spotifyClient *spotify.Client
var spotifyConfig clientcredentials.Config
var refreshContext context.Context

func InitSpotifyService(id string, secret string) {
	spotifyConfig = clientcredentials.Config{
		ClientID:     os.Getenv("SPOTIFY_ID"),
		ClientSecret: os.Getenv("SPOTIFY_SECRET"),
		TokenURL:     spotifyauth.TokenURL,
	}
	refreshContext = context.Background()

	spotifyClient = refreshSpotifyClient()
	fmt.Println("Connected to Spotify...")
}

func refreshSpotifyClient() *spotify.Client {
	newToken, _ := spotifyConfig.TokenSource(refreshContext).Token()

	newClient := spotifyauth.New().Client(refreshContext, newToken)
	client := spotify.New(newClient)

	return client
}
