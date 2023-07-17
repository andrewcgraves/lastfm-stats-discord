package framework

import (
	"fmt"

	"github.com/shkh/lastfm-go/lastfm"
)

var lastFMApi *lastfm.Api

func InitLastFM(key string, secret string) {
	lastFMApi = lastfm.New(key, secret)
	fmt.Println("Connected to LastFM...")
}
