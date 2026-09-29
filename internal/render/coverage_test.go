package render

import "testing"

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
