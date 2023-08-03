package framework

import (
	"fmt"
	"os"
	"time"

	chart "github.com/wcharczuk/go-chart/v2"
)

func GenerateDailyActivityGraph(users []LastFMEntry) string {
	graph := chart.Chart{
		XAxis: chart.XAxis{
			Name: "Date",
		},
		YAxis: chart.YAxis{
			Name: "Number of Listens",
		},
	}

	for _, user := range users {
		timeScale, stats := GetDailyListeningCountsForWeek(user.LastFMName)
		fmt.Printf("%s First Time Scale: %s | %d\n\n", user.LastFMName, timeScale[0], timeScale[0].Unix())
		for i, t := range timeScale {
			if t.Unix() <= 0 {
				timeScale[i] = time.Now().AddDate(0, 0, -7+i)
			}
		}
		graph.Series = append(graph.Series, chart.TimeSeries{
			Name:    user.LastFMName,
			YAxis:   chart.YAxisPrimary,
			XValues: timeScale,
			YValues: stats,
		})
	}

	graph.Elements = []chart.Renderable{
		chart.Legend(&graph),
	}

	f, _ := os.Create(fmt.Sprintf("lastfm-stats-%s.png", time.Now().Format(time.DateOnly)))
	defer f.Close()
	graph.Render(chart.PNG, f)
	return f.Name()
}
