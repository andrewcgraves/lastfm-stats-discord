package framework

import (
	"fmt"
	"strconv"
	"time"

	"github.com/shkh/lastfm-go/lastfm"
)

var lastFMApi *lastfm.Api

func InitLastFM(key string, secret string) {
	lastFMApi = lastfm.New(key, secret)
	fmt.Println("Connected to LastFM...")
}

func GetDailyListeningCountsForWeek(user string) []int {
	results := make([]int, 7)
	rootDate := time.Now().AddDate(0, 0, -7)
	fmt.Println(rootDate, time.Now())

	topTracks, _ := lastFMApi.User.GetRecentTracks(lastfm.P{
		"user":  user,
		"from":  strconv.Itoa(int(rootDate.Unix())),
		"to":    strconv.Itoa(int(time.Now().Unix())),
		"limit": 1000,
	})

	format := "02 Jan 2006, 15:04"
	for _, track := range topTracks.Tracks {
		if track.Date.Date != "" {
			t, _ := time.Parse(format, track.Date.Date)

			daysSinceRoot := int(t.Sub(rootDate).Hours() / 24)
			results[daysSinceRoot]++
		}
	}
	return results
}
