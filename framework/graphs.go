package framework

import (
	"fmt"
	"os"
	"time"

	chart "github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"
)

type Track = struct {
	NowPlaying string "xml:\"nowplaying,attr,omitempty\""
	Artist     struct {
		Name string "xml:\",chardata\""
		Mbid string "xml:\"mbid,attr\""
	} "xml:\"artist\""
	Name       string "xml:\"name\""
	Streamable string "xml:\"streamable\""
	Mbid       string "xml:\"mbid\""
	Album      struct {
		Name string "xml:\",chardata\""
		Mbid string "xml:\"mbid,attr\""
	} "xml:\"album\""
	Url    string "xml:\"url\""
	Images []struct {
		Size string "xml:\"size,attr\""
		Url  string "xml:\",chardata\""
	} "xml:\"image\""
	Date struct {
		Uts  string "xml:\"uts,attr\""
		Date string "xml:\",chardata\""
	} "xml:\"date\""
}

type UserGraphInformation struct {
	LastFMName string
	DiscordId  int
	Tracks     []Track
	Color      int
}

func GenerateDailyActivityGraph(users []UserGraphInformation) string {
	// func GenerateDailyActivityGraph(users []LastFMEntry) string {
	graph := chart.Chart{
		Title: "# Of Listens Per Day",
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
			Style: chart.Style{
				ClassName:   user.LastFMName,
				StrokeColor: drawing.ColorFromHex(string(user.Color)),
			},
		})
	}

	graph.Elements = []chart.Renderable{
		chart.Legend(&graph),
	}

	f, _ := os.Create(fmt.Sprintf("lastfm-stats-%s.png", time.Now().Format("2006-01-02")))
	defer f.Close()
	graph.Render(chart.PNG, f)
	return f.Name()
}
