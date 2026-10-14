package htmlreport

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
)

// decoded is the page's data block as a test reads it.
type decoded struct {
	V int      `json:"v"`
	M bool     `json:"m"`
	C bool     `json:"c"`
	S []string `json:"s"`
	W []decWf  `json:"w"`
	A [][]any  `json:"a"`
}

type decWf struct {
	N  string  `json:"n"`
	F  string  `json:"f"`
	C  int     `json:"c"`
	T  int     `json:"t"`
	Tr bool    `json:"tr"`
	P  string  `json:"p"`
	R  [][]any `json:"r"`
}

// str is the string behind an index of the data block ("" for none).
func (d decoded) str(v any) string {
	i := int(v.(float64))
	if i < 0 {
		return ""
	}
	return d.S[i]
}

var dataBlock = regexp.MustCompile(`(?s)<script id="pk-data" type="application/json">(.*?)</script>`)

func decodeData(t *testing.T, page string) decoded {
	t.Helper()
	m := dataBlock.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("the page has no data block")
	}
	var d decoded
	if err := json.Unmarshal([]byte(m[1]), &d); err != nil {
		t.Fatalf("the data block is not JSON: %v\n%.300s", err, m[1])
	}
	return d
}

// pathText puts a row's path text back together.
func (d decoded) pathText(row []any) string {
	var parts []string
	for _, v := range row[7].([]any) {
		s := d.str(v)
		if s == "\x00" {
			s = ""
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "--> ")
}

// The data block is what the script draws from, so it must be the report:
// every path is there once, with its number, id, status and class, and the
// path text is put back exactly as the text report writes it.
func TestDataBlockIsTheReport(t *testing.T) {
	in := manyPaths(30)
	page := mustReport(t, in)
	d := decodeData(t, page)
	if d.V != 1 || !d.M || !d.C || len(d.W) != 1 {
		t.Fatalf("header of the data: %+v", d)
	}
	w := in.Result.Workflows[0]
	if d.W[0].T != len(w.Paths) || len(d.W[0].R) != len(w.Paths) || d.W[0].C != w.Covered {
		t.Fatalf("workflow: total %d rows %d covered %d, want %d / %d", d.W[0].T, len(d.W[0].R), d.W[0].C, len(w.Paths), w.Covered)
	}
	covered := 0
	for i, p := range w.Paths {
		r := d.W[0].R[i]
		if int(r[0].(float64)) != p.Number || r[1] != p.StableID {
			t.Errorf("row %d: %v", i, r[:2])
		}
		st := int(r[2].(float64))
		if (st != 0) != p.Covered {
			t.Errorf("row %d: status %d, covered %v", i, st, p.Covered)
		}
		if st != 0 {
			covered++
		}
		if got, want := d.str(r[3]), string(p.Class.Bucket); got != want {
			t.Errorf("row %d: bucket %q, want %q", i, got, want)
		}
		if got, want := d.str(r[4]), p.Class.Label; got != want {
			t.Errorf("row %d: label %q, want %q", i, got, want)
		}
		if got, want := d.pathText(r), render.PathText(p.Path); got != want {
			t.Errorf("row %d: path text %q, want %q", i, got, want)
		}
	}
	if covered != w.Covered {
		t.Errorf("rows say %d covered, the result says %d", covered, w.Covered)
	}
}

// The summary cards and the bucket bar print the numbers coverage.Compute
// made, nothing recomputed.
func TestSummaryNumbers(t *testing.T) {
	in := manyPaths(10)
	in.Result.Breakdown = coverage.Breakdown{
		Testable:       coverage.BucketCount{Paths: 7, Covered: 2},
		CannotBeTested: coverage.BucketCount{Paths: 1},
		NeedsReview:    coverage.BucketCount{Paths: 2, Covered: 0},
	}
	in.Result.Paths, in.Result.Covered = 10, 2
	page := mustReport(t, in)
	for _, want := range []string{
		`data-card="testable-coverage" data-covered="2" data-total="9"`, // testable + needs-review, the default policy
		`data-card="overall-coverage" data-covered="2" data-total="10"`,
		`data-card="paths" data-covered="0" data-total="10"`,
		`data-card="missed" data-covered="0" data-total="10"`,
		`data-bucket="testable" data-paths="7" data-covered="2"`,
		`data-bucket="cannot-be-tested" data-paths="1" data-covered="0"`,
		`data-bucket="needs-review" data-paths="2" data-covered="0"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if !strings.Contains(page, `<div class="v">22.2%</div>`) || !strings.Contains(page, `<div class="v">20.0%</div>`) {
		t.Error("the coverage cards do not show 22.2% (testable) and 20.0% (overall)")
	}
}

// Hostile text in every field that reaches the page: names, files, labels,
// reasons, rules, path text. None of it may close the data element, appear
// raw, or change the number of scripts; the data comes back unchanged.
func TestHostileStringsAreHarmless(t *testing.T) {
	evil := `</script><script>alert(1)</script><!-- "q" 'a' \ &amp; ` + "  " + ` ]]>`
	in := manyPaths(3)
	in.Result.Workflows[0].Name = "w" + evil
	in.Result.Workflows[0].File = "dir" + evil + "/w.go"
	bad := cls(classify.CannotBeTested, "lab"+evil, classify.SourceManual, classify.Certain, "why "+evil)
	bad.Origin, bad.RuleID = classify.OriginRule, "rule"+evil
	in.Result.Workflows[0].Paths[0].Class = bad
	in.Result.Workflows[0].Paths[0].StableID = "s1:" + evil
	in.CustomLabels = []classify.Definition{{Kind: classify.KindLabel, Name: "lab" + evil, Bucket: classify.CannotBeTested,
		Meaning: evil, Why: evil, Example: evil, PossibleInProduction: evil, Confidence: classify.Certain, DecidedBy: evil, WhatToDo: evil}}
	page := mustReport(t, in)
	if n := strings.Count(strings.ToLower(page), "<script"); n != 2 {
		t.Errorf("%d script elements, want 2", n)
	}
	if n := strings.Count(page, "</script>"); n != 2 {
		t.Errorf("%d closing script tags, want 2", n)
	}
	for _, raw := range []string{"<script>alert", "<!--"} {
		if strings.Contains(page, raw) {
			t.Errorf("raw %q in the page", raw)
		}
	}
	// older scripts treat these two as line ends: not raw in the data block
	block := dataBlock.FindString(page)
	for _, raw := range []string{" ", " "} {
		if strings.Contains(block, raw) {
			t.Errorf("raw %q in the data block", raw)
		}
	}
	d := decodeData(t, page)
	if d.W[0].N != "w"+evil {
		t.Errorf("name came back as %q", d.W[0].N)
	}
	r := d.W[0].R[0]
	if r[1] != "s1:"+evil || d.str(r[4]) != "lab"+evil || d.str(r[6]) != "why "+evil {
		t.Errorf("row came back as %v", r[:7])
	}
}

// A workflow with no path at all, a single path and a very long id or label
// all produce a valid data block.
func TestOddShapes(t *testing.T) {
	in := manyPaths(1)
	in.Result.Workflows = append(in.Result.Workflows, coverage.WorkflowResult{Name: "empty.W", File: "e.go"})
	in.Result.Workflows[0].Paths[0].Class.Label = strings.Repeat("l", 400)
	in.Result.Workflows[0].Paths[0].StableID = "s1:" + strings.Repeat("9", 400)
	d := decodeData(t, mustReport(t, in))
	if len(d.W) != 2 || len(d.W[1].R) != 0 || len(d.W[0].R) != 1 {
		t.Fatalf("shapes: %+v", d.W)
	}
	if d.str(d.W[0].R[0][4]) != strings.Repeat("l", 400) {
		t.Error("a long label was cut")
	}
}
