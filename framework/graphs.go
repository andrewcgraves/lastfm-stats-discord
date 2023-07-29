package framework

import (
	"fmt"
	"os"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"
)

func GenerateDailyActivityGraph(users []LastFMEntry) string {
	line := charts.NewLine()

	line.SetXAxis([]string{"-6", "-5", "-4", "-3", "-2", "-1", "0"})

	fmt.Println("PRINTING STATS")
	for _, user := range users {
		stats := GetDailyListeningCountsForWeek(user.LastFMName)
		line.AddSeries(user.LastFMName, []opts.LineData{{Value: stats[0]}, {Value: stats[1]}, {Value: stats[2]}, {Value: stats[3]}, {Value: stats[4]}, {Value: stats[5]}, {Value: stats[6]}})
	}

	f, _ := os.Create("line.html")
	line.Render(f)
	return f.Name()
}
