package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/bwmarrin/discordgo"
	chart "github.com/wcharczuk/go-chart/v2"
)

func Test(s *discordgo.Session, i *discordgo.InteractionCreate) {
	fmt.Println("Executed Test Command...")
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "working on test",
		},
	})

	graph := chart.Chart{
		XAxis: chart.XAxis{
			ValueFormatter: chart.TimeDateValueFormatter,
		},
		Series: []chart.Series{
			chart.TimeSeries{
				XValues: []time.Time{time.Now(), time.Now().AddDate(0, 0, -1), time.Now().AddDate(0, 0, -2), time.Now().AddDate(0, 0, -3)},
				YValues: []float64{5, 7, 2, 8},
			},
		},
		Title: "TEST",
	}

	// line := charts.NewLine()
	// line.SetXAxis([]string{"-6", "-5", "-4", "-3", "-2", "-1", "0"})
	// fmt.Println("PRINTING STATS")
	// for _, user := range users {
	// 	// discordUserStats, err := GetUserInformation(strconv.Itoa(user.DiscordID))
	// 	// Check(err)

	// 	timeScale, stats := GetDailyListeningCountsForWeek(user.LastFMName)
	// 	fmt.Printf("TIMESCALE -> [%+v]\nSTATS -> [%+v]\n\n", timeScale, stats)
	// 	graph.Series = append(graph.Series, chart.TimeSeries{
	// 		Name:    user.LastFMName,
	// 		YAxis:   chart.YAxisPrimary,
	// 		XValues: timeScale,
	// 		YValues: stats,
	// 	})
	// 	// line.AddSeries(user.LastFMName, []opts.LineData{{Value: stats[0]}, {Value: stats[1]}, {Value: stats[2]}, {Value: stats[3]}, {Value: stats[4]}, {Value: stats[5]}, {Value: stats[6]}})
	// }
	f, _ := os.Create(fmt.Sprintf("lastfm-stats-%s", time.Now()))
	defer f.Close()
	graph.Render(chart.PNG, f)
	// line.Render(f)
	// return f.Name()
}
