package framework

import (
	"fmt"
	"os"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"
)

func GenerateDailyActivityGraph(users []LastFMEntry) string {
	// listeningGraphStats := GetDailyListeningCountsForWeek(user)
	// listeningDataPerUser := map[string][]int{}
	line := charts.NewLine()

	line.SetGlobalOptions(charts.WithTitleOpts(opts.Title{
		Title: "Server Weekly Listening Stats",
	}))

	line.SetXAxis([]string{"Mon", "Tue", "Wed", "Thur", "Fri", "Sat", "Sun"}).AddSeries("Sample Series", []opts.LineData{{Value: 10}, {Value: 12}})

	for _, user := range users {
		stats := GetDailyListeningCountsForWeek(user.LastFMName)
		fmt.Println(stats)
		line.AddSeries(user.LastFMName, []opts.LineData{{Value: stats[0]}, {Value: stats[1]}, {Value: stats[2]}, {Value: stats[3]}, {Value: stats[4]}, {Value: stats[5]}, {Value: stats[6]}})
	}

	f, _ := os.Create("line.png")
	line.Render(f)
	return f.Name()
}
