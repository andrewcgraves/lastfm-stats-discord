package digest

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/andrewcgraves/lastfm-stats-discord/internal/store"
	chart "github.com/wcharczuk/go-chart/v2"
)

const graphDays = 7

// DailyCounts buckets scrobble timestamps into the graphDays calendar days
// ending on the day of now (in loc). It returns the start of each day and the
// number of scrobbles on it. Timestamps outside the window are ignored.
func DailyCounts(scrobbles []time.Time, now time.Time, loc *time.Location) ([]time.Time, []float64) {
	now = now.In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	days := make([]time.Time, graphDays)
	counts := make([]float64, graphDays)
	for i := range days {
		days[i] = today.AddDate(0, 0, i-(graphDays-1))
	}
	for _, ts := range scrobbles {
		ts = ts.In(loc)
		d := time.Date(ts.Year(), ts.Month(), ts.Day(), 0, 0, 0, 0, loc)
		for i := range days {
			if d.Equal(days[i]) {
				counts[i]++
				break
			}
		}
	}
	return days, counts
}

func (b *Builder) activityGraph(ctx context.Context, users []store.User, now time.Time) (f *File, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("rendering graph panicked: %v", r)
		}
	}()

	loc := b.Location
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	from := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(graphDays - 1))

	// The embed carries the title, so the chart itself doesn't need one.
	graph := chart.Chart{
		Background: chart.Style{Padding: chart.Box{Top: 40, Left: 20, Right: 20, Bottom: 10}},
		YAxis:      chart.YAxis{Name: "Scrobbles"},
	}

	var max float64
	var days []time.Time
	for _, u := range users {
		uctx, cancel := context.WithTimeout(ctx, perUserTimeout)
		scrobbles, err := b.LastFM.Scrobbles(uctx, u.LastFMName, from, now)
		cancel()
		if err != nil {
			b.Log.Warn("fetching scrobbles for graph", "lastfm_user", u.LastFMName, "err", err)
			continue
		}
		var counts []float64
		days, counts = DailyCounts(scrobbles, now, loc)
		for _, c := range counts {
			if c > max {
				max = c
			}
		}
		graph.Series = append(graph.Series, chart.TimeSeries{
			Name:    u.LastFMName,
			XValues: days,
			YValues: counts,
			Style:   chart.Style{StrokeWidth: 3, DotWidth: 4},
		})
	}
	if len(graph.Series) == 0 || max == 0 {
		return nil, nil // nothing worth drawing
	}

	// One labelled tick per day, and whole-number scrobble ticks.
	for _, d := range days {
		graph.XAxis.Ticks = append(graph.XAxis.Ticks, chart.Tick{Value: chart.TimeToFloat64(d), Label: d.Format("Mon 2")})
	}
	step := math.Max(1, math.Ceil(max/5))
	top := step * math.Ceil(max/step)
	graph.YAxis.Range = &chart.ContinuousRange{Min: 0, Max: top}
	for v := 0.0; v <= top; v += step {
		graph.YAxis.Ticks = append(graph.YAxis.Ticks, chart.Tick{Value: v, Label: strconv.Itoa(int(v))})
	}
	graph.Elements = []chart.Renderable{chart.LegendThin(&graph)}

	var buf bytes.Buffer
	if err := graph.Render(chart.PNG, &buf); err != nil {
		return nil, err
	}
	return &File{Name: "scrobbles.png", ContentType: "image/png", Data: buf.Bytes()}, nil
}
