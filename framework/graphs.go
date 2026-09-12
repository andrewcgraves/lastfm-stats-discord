package framework

import (
	"fmt"
	"os"
	"time"

	chart "github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"
)

// defaultSeriesColor is used when a user has no stored role color to draw their line with.
var defaultSeriesColor = drawing.ColorFromHex("5865F2")

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

func GenerateDailyActivityGraph(users []UserGraphInformation) (string, error) {
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
		for i, t := range timeScale {
			if t.Unix() <= 0 {
				timeScale[i] = time.Now().AddDate(0, 0, -7+i)
			}
		}

		seriesColor := defaultSeriesColor
		if user.Color != 0 {
			seriesColor = drawing.ColorFromHex(fmt.Sprintf("%06x", user.Color))
		}

		graph.Series = append(graph.Series, chart.TimeSeries{
			Name:    user.LastFMName,
			YAxis:   chart.YAxisPrimary,
			XValues: timeScale,
			YValues: stats,
			Style: chart.Style{
				ClassName:   user.LastFMName,
				StrokeColor: seriesColor,
			},
		})
	}

	graph.Elements = []chart.Renderable{
		chart.Legend(&graph),
	}

	f, err := os.Create(fmt.Sprintf("lastfm-stats-%s.png", time.Now().Format("2006-01-02")))
	if err != nil {
		return "", err
	}
	defer f.Close()

	if err := graph.Render(chart.PNG, f); err != nil {
		return "", err
	}
	return f.Name(), nil
}
