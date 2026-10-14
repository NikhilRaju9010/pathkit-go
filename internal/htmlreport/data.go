package htmlreport

import (
	"strconv"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
)

// The page's data block: every path of every workflow, once, in a compact
// form that the page's script draws one page at a time.
//
//	s   string table: every label, bucket, reason, step text is stored once
//	m   true when coverage was measured (report), false for analyze
//	c   true when classification is on (buckets, labels, reasons)
//	w   the workflows: n name, f file, c covered, t total, tr truncated,
//	    a added by config, p priority, r rows
//	a   the detector audit rows
//
// A row is [number, stableId, status, bucket, label, by, reason, steps]:
// status 0 missed, 1 covered, 2 covered by a stale trace; bucket, label, by
// and reason are indexes into s (-1 for none); steps are indexes into s of
// the path text cut after every "--> " (joined again with "--> ").
type pageData struct {
	V int      `json:"v"`
	M bool     `json:"m"`
	C bool     `json:"c"`
	S []string `json:"s"`
	W []wfData `json:"w"`
	A [][]any  `json:"a,omitempty"`
	t map[string]int
}

type wfData struct {
	N  string  `json:"n"`
	F  string  `json:"f"`
	C  int     `json:"c"`
	T  int     `json:"t"`
	Tr bool    `json:"tr,omitempty"`
	A  bool    `json:"a,omitempty"`
	P  string  `json:"p,omitempty"`
	R  [][]any `json:"r"`
}

func newPageData(measured, classified bool) *pageData {
	return &pageData{V: 1, M: measured, C: classified, t: map[string]int{}}
}

func (d *pageData) str(s string) int {
	if s == "" {
		return -1
	}
	if i, ok := d.t[s]; ok {
		return i
	}
	d.t[s] = len(d.S)
	d.S = append(d.S, s)
	return d.t[s]
}

// steps cuts a path's text after every "--> " and stores each piece once.
func (d *pageData) steps(text string) []int {
	parts := strings.Split(text, "--> ")
	out := make([]int, len(parts))
	for i, p := range parts {
		out[i] = d.str(p)
		if out[i] < 0 { // an empty piece must still be there to join again
			out[i] = d.str("\x00")
		}
	}
	return out
}

func (d *pageData) addWorkflow(w coverage.WorkflowResult, classified bool) {
	wf := wfData{N: w.Name, F: strings.ReplaceAll(w.File, "\\", "/"), C: w.Covered, T: len(w.Paths), Tr: w.Truncated, A: w.AddedByConfig}
	wf.P, _ = coverage.Priority(w.Covered, len(w.Paths))
	wf.R = make([][]any, 0, len(w.Paths))
	for _, p := range w.Paths {
		status := 0
		if p.Covered {
			status = 1
			if p.StaleTrace {
				status = 2
			}
		}
		b, l, by, ex := -1, -1, -1, -1
		if classified {
			b = d.str(string(p.Class.Bucket))
			l = d.str(p.Class.Label)
			by = d.str(decidedBy(p.Class))
			ex = d.str(p.Class.Explanation)
		}
		wf.R = append(wf.R, []any{p.Number, p.StableID, status, b, l, by, ex, d.steps(render.PathText(p.Path))})
	}
	d.W = append(d.W, wf)
}

func (d *pageData) addAnalysisWorkflow(w AnalysisWorkflow) {
	wf := wfData{N: w.Name, F: strings.ReplaceAll(w.File, "\\", "/"), T: len(w.Paths.List), Tr: w.Paths.Truncated, A: w.AddedByConfig}
	wf.R = make([][]any, 0, len(w.Paths.List))
	for i, p := range w.Paths.List {
		wf.R = append(wf.R, []any{i + 1, "", 0, -1, -1, -1, -1, d.steps(render.PathText(p))})
	}
	d.W = append(d.W, wf)
}

// addAudit stores the detector audit rows: [workflow index, path number,
// label, detector, confidence, reason].
func (d *pageData) addAudit(rows []auditRow) {
	for _, a := range rows {
		d.A = append(d.A, []any{a.WfIndex, a.Number, d.str(a.Label), d.str(a.Detector), d.str(a.Confidence), d.str(a.Reason)})
	}
}

func (d *pageData) finish() *pageData {
	if d.S == nil {
		d.S = []string{}
	}
	if d.W == nil {
		d.W = []wfData{}
	}
	return d
}

// decidedBy says in a few words who decided a path's class: "auto ·
// detector closed-signal-channel", "manual · rule "x"", or "" when nobody
// did (an ordinary testable path).
func decidedBy(c classify.Classification) string {
	var by string
	switch {
	case c.Origin == classify.OriginRule:
		by = "rule " + strconv.Quote(c.RuleID)
	case c.Origin == classify.OriginOverride:
		by = "config override"
	case c.Origin == classify.OriginComment:
		by = "//pathkit:untestable"
	case c.Detector != "":
		by = "detector " + c.Detector
	}
	if by == "" {
		return ""
	}
	if c.Source != "" {
		by = string(c.Source) + " · " + by
	}
	return by
}

func itoa(n int) string { return strconv.Itoa(n) }

// plural: "689 of 761 testable paths".
func plural(a int, mid string, b int, noun string) string {
	s := itoa(a) + " " + mid + " " + itoa(b) + " " + noun
	if b != 1 {
		s += "s"
	}
	return s
}
