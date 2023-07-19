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
	// results := make(map[time.Time]int)
	results := make([]int, 7)
	// dateArray := make(map[int]time.Time)
	rootDate := time.Now().AddDate(0, 0, -7)
	fmt.Println(rootDate, time.Now())

	topTracks, _ := lastFMApi.User.GetRecentTracks(lastfm.P{
		"user": user,
		"from": strconv.Itoa(int(rootDate.Unix())),
		"to":   strconv.Itoa(int(time.Now().Unix())),
	})

	fmt.Println(topTracks)

	// for i := 0; i < 7; i++ {
	// 	results[time.Now().AddDate(0, 0, -1)] = 0
	// }

	for _, track := range topTracks.Tracks {
		t, _ := strconv.Atoi(track.Date.Date)
		trackTime := time.Unix(int64(t), 0)
		// daysSinceRoot := time.Now().Sub(time.Unix(int64(t), 0)).

		fmt.Println(trackTime)
		fmt.Println(rootDate)

		daysSinceRoot := int(trackTime.Sub(rootDate).Hours() / 24)
		// int(time.Since(rootDate).Hours()/24)
		results[daysSinceRoot]++
	}

	fmt.Print(results)

	return results
}
