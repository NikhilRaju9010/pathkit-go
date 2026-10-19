package htmlreport

import (
	"strings"
	"testing"
	"time"
)

func TestTrendChart(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 10, 1, h, 0, 0, 0, time.UTC) }
	runs := []Run{
		{Time: at(1), Paths: 10, Covered: 2}, // an older run, without the headline
		{Time: at(2), Paths: 10, Covered: 3, MainMet: 1, MainTotal: 4, BranchCovered: 10, BranchTotal: 40},
		{Time: at(3), Paths: 10, Covered: 5, MainMet: 2, MainTotal: 4, BranchCovered: 20, BranchTotal: 40},
		{Time: at(4), Paths: 10, Covered: 6, MainMet: 3, MainTotal: 4, BranchCovered: 30, BranchTotal: 40},
	}
	c := TrendChart(runs)
	if c == nil || len(c.Lines) != 2 {
		t.Fatalf("chart: %+v", c)
	}
	// the older run without a headline is left out: three points on each line
	for _, l := range c.Lines {
		if len(l.Dots) != 3 {
			t.Errorf("line %s has %d points, want 3", l.Class, len(l.Dots))
		}
	}
	for _, want := range []string{"25.0, 50.0, 75.0", "3 report runs"} {
		if !strings.Contains(c.Desc, want) {
			t.Errorf("description lacks %q: %s", want, c.Desc)
		}
	}
	// near misses: one run with a headline is not a trend
	if got := TrendChart(runs[:2]); got != nil {
		t.Errorf("one point drew a chart: %+v", got)
	}
	if got := TrendChart(nil); got != nil {
		t.Errorf("no runs drew a chart")
	}
	// and the page draws it as an SVG with no outside file and no script
	in := sample("p.W", "p/w.go", "why")
	in.Trend = runs
	page := mustReport(t, in)
	for _, want := range []string{`<svg class="trend-chart"`, `role="img"`, "<polyline"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	svg := page[strings.Index(page, `<svg class="trend-chart"`):]
	svg = svg[:strings.Index(svg, "</svg>")]
	for _, bad := range []string{"<script", "<image", "http://", "https://", "href=", "src="} {
		if strings.Contains(svg, bad) {
			t.Errorf("chart contains %q", bad)
		}
	}
}
