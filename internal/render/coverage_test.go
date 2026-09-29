package render

import (
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
)

// Owner's condition (M6): when one decimal would make the value look equal
// to the --fail-under threshold, more decimals are shown.
func TestPct(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	tests := []struct {
		value     float64
		threshold *float64
		want      string
	}{
		{52.631578, nil, "52.6%"},
		{52.59, f(52.6), "52.59%"},     // looks like 52.6 at one decimal: show the difference
		{52.631578, f(52.6), "52.63%"}, // passes, and shows why
		{66.666666, f(66.7), "66.67%"}, // fails, and shows why
		{52.631578, f(80), "52.6%"},    // clearly different: one decimal is enough
		{80, f(80), "80.0%"},           // exactly equal: nothing to show
		{99.99, f(100), "99.99%"},      // 100.0 vs 100.0 at one decimal
	}
	for _, tt := range tests {
		if got := Pct(tt.value, tt.threshold); got != tt.want {
			t.Errorf("Pct(%v, %v) = %q, want %q", tt.value, tt.threshold, got, tt.want)
		}
	}
	if got := Threshold(52.6); got != "52.6%" {
		t.Errorf("Threshold(52.6) = %q", got)
	}
}

// D11: a workflow's % never looks like it sits on a boundary it isn't on.
func TestWorkflowPct(t *testing.T) {
	tests := []struct {
		covered, total int
		want           string
	}{
		{1, 2, "50.0%"},
		{4, 5, "80.0%"},
		{4999, 10000, "49.99%"},
		{5001, 10000, "50.01%"},
		{7999, 10000, "79.99%"},
		{8001, 10000, "80.01%"},
		{2, 3, "66.7%"},
		{0, 0, "0.0%"},
	}
	for _, tt := range tests {
		if got := WorkflowPct(tt.covered, tt.total); got != tt.want {
			t.Errorf("WorkflowPct(%d, %d) = %s, want %s", tt.covered, tt.total, got, tt.want)
		}
	}
}

func TestColorEnabled(t *testing.T) {
	tests := []struct {
		noColor  bool
		env      string
		terminal bool
		want     bool
	}{
		{false, "", true, true},
		{false, "", false, false}, // a pipe or a file: never
		{true, "", true, false},   // --no-color or config noColor
		{false, "1", true, false}, // NO_COLOR, any non-empty value
		{false, "0", true, false},
	}
	for _, tt := range tests {
		if got := ColorEnabled(tt.noColor, tt.env, tt.terminal); got != tt.want {
			t.Errorf("ColorEnabled(%v, %q, %v) = %v", tt.noColor, tt.env, tt.terminal, got)
		}
	}
}

// Colour only decorates: without the codes, the text is identical.
func TestReportColor(t *testing.T) {
	res := coverage.Result{Paths: 3, Covered: 2, Workflows: []coverage.WorkflowResult{{
		Name: "p.W", File: "p/w.go", Covered: 2,
		Paths: []coverage.PathResult{{Number: 1, Covered: true}, {Number: 2, Covered: true}, {Number: 3}},
	}}}
	plain := ReportText(res, ReportOptions{})
	colored := ReportText(res, ReportOptions{Color: true})
	for _, want := range []string{"\x1b[32mcovered\x1b[0m", "\x1b[31mmissed\x1b[0m", "priority \x1b[33mMedium\x1b[0m"} {
		if !strings.Contains(colored, want) {
			t.Errorf("coloured text lacks %q:\n%s", want, colored)
		}
	}
	if strings.Contains(plain, "\x1b[") {
		t.Errorf("plain text has colour codes:\n%s", plain)
	}
	strip := strings.NewReplacer("\x1b[32m", "", "\x1b[31m", "", "\x1b[33m", "", "\x1b[0m", "")
	if strip.Replace(colored) != plain {
		t.Errorf("colour changed more than decoration:\n%s\nvs\n%s", colored, plain)
	}
}

func TestExcludedLine(t *testing.T) {
	tests := []struct {
		n    int
		by   []string
		want string
	}{
		{0, nil, "0 workflows excluded (no scope in use)"},
		{0, []string{".pathkitrc.json"}, "0 workflows excluded by .pathkitrc.json"},
		{1, []string{".pathkitrc.json"}, `1 workflow excluded by .pathkitrc.json (see "reason"):`},
		{2, []string{".pathkitrc.json", "--exclude"}, `2 workflows excluded by .pathkitrc.json and --exclude (see "reason"):`},
	}
	for _, tt := range tests {
		if got := ExcludedLine(tt.n, tt.by); got != tt.want {
			t.Errorf("ExcludedLine(%d, %v) = %q, want %q", tt.n, tt.by, got, tt.want)
		}
	}
}
