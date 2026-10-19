package htmlreport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// HistoryFile is the trend's file name. It lives next to the HTML file,
// so each HTML file keeps its own trend. Only report --html writes it;
// pathkit test, pathkit clean and --clean never delete it (they delete
// *.trace.json files and pathkit's own overlay copies only).
const HistoryFile = "report-history.json"

// MaxRuns is how many report runs the trend keeps.
const MaxRuns = 5

// Run is one report --html run in the trend.
type Run struct {
	Time     time.Time `json:"time"`
	Paths    int       `json:"paths"`
	Covered  int       `json:"covered"`
	InScope  int       `json:"inScope"`
	Excluded int       `json:"excluded"`
	// Scope fingerprints which workflows were in scope and which were
	// excluded, so a jump in % caused by a scope change is marked.
	Scope string `json:"scope"`
	// The headline of the run (added with main numbers; absent in older
	// histories, which the chart leaves out): how many workflows met their
	// target of how many, and the branches taken of all.
	MainMet       int `json:"mainMet,omitempty"`
	MainTotal     int `json:"mainTotal,omitempty"`
	BranchCovered int `json:"branchCovered,omitempty"`
	BranchTotal   int `json:"branchTotal,omitempty"`
}

type historyFile struct {
	SchemaVersion int    `json:"schemaVersion"`
	Tool          string `json:"tool"`
	Runs          []Run  `json:"runs"`
}

// HistoryPath is the history file for an HTML file.
func HistoryPath(htmlPath string) string {
	return filepath.Join(filepath.Dir(htmlPath), HistoryFile)
}

// ReadHistory reads the trend. A missing or damaged file never fails the
// report: it returns no runs and a warning, and the trend starts fresh.
func ReadHistory(path string) (runs []Run, warning string) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Sprintf("no report history at %s yet; starting a new trend", path)
	}
	if err != nil {
		return nil, fmt.Sprintf("could not read report history %s: %v; starting a new trend", path, err)
	}
	var h historyFile
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&h); err != nil {
		return nil, fmt.Sprintf("report history %s is damaged (%v); starting a new trend", path, err)
	}
	if h.SchemaVersion != 1 || h.Tool != "pathkit-go" {
		return nil, fmt.Sprintf("report history %s is not a pathkit-go history file (schemaVersion %d, tool %q); starting a new trend", path, h.SchemaVersion, h.Tool)
	}
	for i, r := range h.Runs {
		if r.Paths < 0 || r.Covered < 0 || r.Covered > r.Paths {
			return nil, fmt.Sprintf("report history %s is damaged (run %d has %d of %d paths covered); starting a new trend", path, i+1, r.Covered, r.Paths)
		}
	}
	return h.Runs, ""
}

// AddRun appends r and keeps the last MaxRuns runs.
func AddRun(runs []Run, r Run) []Run {
	runs = append(slices.Clone(runs), r)
	if len(runs) > MaxRuns {
		runs = runs[len(runs)-MaxRuns:]
	}
	return runs
}

// WriteHistory writes the trend.
func WriteHistory(path string, runs []Run) error {
	data, err := json.MarshalIndent(historyFile{SchemaVersion: 1, Tool: "pathkit-go", Runs: runs}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Fingerprint identifies a scope: which workflows were in scope and
// which were excluded (order doesn't matter).
func Fingerprint(inScope, excluded []string) string {
	a, b := slices.Clone(inScope), slices.Clone(excluded)
	slices.Sort(a)
	slices.Sort(b)
	sum := sha256.Sum256([]byte(strings.Join(a, "\n") + "\n--\n" + strings.Join(b, "\n")))
	return hex.EncodeToString(sum[:])[:12]
}

func scopeCounts(r Run) string {
	return fmt.Sprintf("%d in scope, %d excluded", r.InScope, r.Excluded)
}

// chartView is the trend chart as data: the page's template draws it as an
// inline SVG, so every number goes through the template's escaping like any
// other text (no outside file, no script, no library).
type chartView struct {
	W, H  int
	Desc  string
	Grid  []chartGrid
	Lines []chartLine
}

type chartGrid struct {
	X1, X2, Y, TextX, TextY, Label string
}

type chartLine struct {
	Class, Name, Points string
	Dots                []chartDot
}

type chartDot struct{ X, Y, Tip string }

type trendPoint struct{ main, branch float64 }

// TrendChart draws the headline of the last runs as a small line chart: the
// share of workflows that met their target, and branch coverage, in percent,
// oldest run first. nil when fewer than two runs carry the headline.
func TrendChart(runs []Run) *chartView {
	var pts []trendPoint
	var labels []string
	for _, r := range runs {
		if r.MainTotal == 0 && r.BranchTotal == 0 {
			continue
		}
		p := trendPoint{main: -1, branch: -1}
		if r.MainTotal > 0 {
			p.main = 100 * float64(r.MainMet) / float64(r.MainTotal)
		}
		if r.BranchTotal > 0 {
			p.branch = 100 * float64(r.BranchCovered) / float64(r.BranchTotal)
		}
		pts = append(pts, p)
		labels = append(labels, r.Time.UTC().Format("2006-01-02 15:04"))
	}
	if len(pts) < 2 {
		return nil
	}
	const w, h, left, right, top, bottom = 480, 150, 40, 12, 12, 24
	x := func(i int) float64 { return left + float64(i)*float64(w-left-right)/float64(len(pts)-1) }
	y := func(v float64) float64 { return top + (100-v)/100*float64(h-top-bottom) }
	f := func(v float64) string { return fmt.Sprintf("%.1f", v) }
	c := &chartView{W: w, H: h}
	c.Desc = fmt.Sprintf("%d report runs, oldest first. Workflows meeting their target, in percent: %s. Branch coverage, in percent: %s.",
		len(pts), seriesText(pts, func(p trendPoint) float64 { return p.main }), seriesText(pts, func(p trendPoint) float64 { return p.branch }))
	for _, g := range []int{0, 50, 100} {
		c.Grid = append(c.Grid, chartGrid{X1: f(left), X2: f(w - right), Y: f(y(float64(g))), TextX: f(left - 4), TextY: f(y(float64(g)) + 3), Label: fmt.Sprintf("%d%%", g)})
	}
	line := func(class, name string, get func(trendPoint) float64) {
		l := chartLine{Class: class, Name: name}
		var path []string
		for i, p := range pts {
			if v := get(p); v >= 0 {
				path = append(path, f(x(i))+","+f(y(v)))
				l.Dots = append(l.Dots, chartDot{X: f(x(i)), Y: f(y(v)), Tip: fmt.Sprintf("%s: %s%% (%s)", labels[i], f(v), name)})
			}
		}
		if len(path) > 0 {
			l.Points = strings.Join(path, " ")
			c.Lines = append(c.Lines, l)
		}
	}
	line("main", "workflows meeting their target", func(p trendPoint) float64 { return p.main })
	line("branch", "branch coverage", func(p trendPoint) float64 { return p.branch })
	return c
}

// seriesText writes the values of one line for the chart's description ("n/a"
// for a run without the value).
func seriesText(pts []trendPoint, get func(trendPoint) float64) string {
	parts := make([]string, len(pts))
	for i, p := range pts {
		if v := get(p); v >= 0 {
			parts[i] = fmt.Sprintf("%.1f", v)
		} else {
			parts[i] = "n/a"
		}
	}
	return strings.Join(parts, ", ")
}
