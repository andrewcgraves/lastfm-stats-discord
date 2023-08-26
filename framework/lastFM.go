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

func GetDailyListeningCountsForWeek(user string) ([]time.Time, []float64) {
	results := make([]float64, 7)
	timeScale := make([]time.Time, 7)
	rootDate := time.Now().AddDate(0, 0, -7)
	fmt.Printf("ROOT AND NOW: %d -> %d | %s\n", rootDate.Unix(), time.Now().Unix(), time.Now())

	// GOLANG is there a do while loop where the loop can run before the condition. We need to get every page.
	recentTracks, _ := lastFMApi.User.GetRecentTracks(lastfm.P{
		"user":  user,
		"from":  strconv.Itoa(int(rootDate.Unix())),
		"to":    strconv.Itoa(int(time.Now().Unix())),
		"limit": 1000,
	})

	allTracks := recentTracks.Tracks

	format := "02 Jan 2006, 15:04"
	for _, track := range allTracks {
		if track.Date.Date != "" {
			t, _ := time.Parse(format, track.Date.Date)

			if t.Unix() > 0 {
				daysSinceRoot := int(t.Sub(rootDate).Hours() / 24)
				timeScale[daysSinceRoot] = t
				results[daysSinceRoot]++
			}
		}
	}
	return timeScale, results
}
