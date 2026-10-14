package htmlreport

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
)

func cls(bucket classify.Bucket, label string, src classify.Source, conf classify.Confidence, reason string) classify.Classification {
	c := classify.DefaultTestable()
	c.Bucket, c.Label, c.Source, c.Confidence, c.Explanation = bucket, label, src, conf, reason
	c.PossibleInProduction = bucket != classify.CannotBeTested
	return c
}

// classified is a report with one path of every kind: a detector's
// cannot-be-tested, a rule's, a detector's needs-special-test, a custom
// label, a detector's needs-review and a plain testable path.
func classified() ReportInput {
	in := sample("p.W", "p/w.go", "why")
	detector := cls(classify.CannotBeTested, "closed-signal-channel", classify.SourceAuto, classify.Certain, "J1 (if !more) takes the closed side. "+danger)
	detector.Detector = "closed-signal-channel"
	rule := cls(classify.CannotBeTested, "loop-minimum", classify.SourceManual, classify.Certain, "Rule r1: loop runs once")
	rule.Origin, rule.RuleID = classify.OriginRule, "r1"
	special := cls(classify.NeedsSpecialTest, "history-limit", classify.SourceAuto, classify.Certain, "SDK suggests continue-as-new")
	special.Detector = "history-limit"
	custom := cls(classify.CannotBeTested, "my-label", classify.SourceManual, classify.Certain, "custom")
	custom.Origin = classify.OriginOverride
	review := cls(classify.NeedsReview, "needs-review", classify.SourceAuto, classify.Likely, "origin unknown")
	review.Detector = "closed-signal-channel"
	in.Result.Workflows[0].Paths = []coverage.PathResult{
		{Number: 1, Covered: true, Class: classify.DefaultTestable()},
		{Number: 2, Class: detector}, {Number: 3, Class: rule}, {Number: 4, Class: special},
		{Number: 5, Class: custom}, {Number: 6, Class: review},
	}
	in.Classification = &render.ClassificationView{Basis: coverage.BasisOverall}
	in.CustomLabels = []classify.Definition{{Kind: classify.KindLabel, Name: "my-label", Bucket: classify.CannotBeTested,
		Meaning: "custom meaning " + danger, Why: "because", Example: "e.g.", PossibleInProduction: "as chosen",
		Confidence: classify.Certain, DecidedBy: "person", WhatToDo: "see config"}}
	return in
}

func ids(page string) map[string]int {
	out := map[string]int{}
	for _, m := range regexp.MustCompile(`\bid="([^"]*)"`).FindAllStringSubmatch(page, -1) {
		out[html.UnescapeString(m[1])]++
	}
	return out
}

// The classified page is still one self-contained file: no outside
// addresses; the only links are inside the page.
func TestGlossaryPageIsSelfContained(t *testing.T) {
	page := mustReport(t, classified())
	lower := strings.ToLower(page)
	for _, bad := range []string{"<link", "@import", "http:", "https:", "<iframe", "<img", "<object", "<embed", " src="} {
		if strings.Contains(lower, bad) {
			t.Errorf("page contains %q", bad)
		}
	}
	for _, m := range regexp.MustCompile(`(?i)\bhref\s*=\s*"([^"]*)"`).FindAllStringSubmatch(page, -1) {
		if !strings.HasPrefix(m[1], "#") {
			t.Errorf("a link leaves the page: %q", m[1])
		}
	}
	if strings.Count(page, "Content-Security-Policy") != 1 {
		t.Error("want exactly one CSP meta")
	}
}

// Every link in the page lands on something, and no id is used twice. A
// link to a path ("#use-<workflow>-<path>") lands on the path's row, which
// the script draws: it must name a workflow and a path number in the data.
func TestEveryLinkHasATarget(t *testing.T) {
	page := mustReport(t, classified())
	have := ids(page)
	for id, n := range have {
		if n > 1 {
			t.Errorf("id %q is used %d times", id, n)
		}
	}
	d := decodeData(t, page)
	links := regexp.MustCompile(`href="#([^"]*)"`).FindAllStringSubmatch(page, -1)
	if len(links) < 10 {
		t.Fatalf("only %d links; the page is missing its glossary", len(links))
	}
	for _, l := range links {
		id := html.UnescapeString(l[1])
		if have[id] > 0 {
			continue
		}
		var wi, num int
		if n, _ := fmt.Sscanf(id, "use-%d-%d", &wi, &num); n == 2 && wi >= 1 && wi <= len(d.W) && num >= 1 && num <= len(d.W[wi-1].R) {
			continue
		}
		t.Errorf("link #%s has no target", l[1])
	}
}

// Rule id, source and detector name stand with each path: in the data the
// page draws from.
func TestTagsShowSourceRuleAndDetector(t *testing.T) {
	d := decodeData(t, mustReport(t, classified()))
	row := func(n int) (bucket, label, by string) {
		r := d.W[0].R[n-1]
		return d.str(r[3]), d.str(r[4]), d.str(r[5])
	}
	for n, want := range map[int][3]string{
		1: {"testable", "", ""},
		2: {"cannot-be-tested", "closed-signal-channel", "auto · detector closed-signal-channel"},
		3: {"cannot-be-tested", "loop-minimum", `manual · rule "r1"`},
		4: {"needs-special-test", "history-limit", "auto · detector history-limit"},
		5: {"cannot-be-tested", "my-label", "manual · config override"},
		6: {"needs-review", "needs-review", "auto · detector closed-signal-channel"},
	} {
		b, l, by := row(n)
		if b != want[0] || l != want[1] || by != want[2] {
			t.Errorf("path %d: bucket %q label %q by %q, want %q", n, b, l, by, want)
		}
	}
}

// The audit lists only paths a detector marked cannot-be-tested.
func TestDetectorAudit(t *testing.T) {
	page := mustReport(t, classified())
	d := decodeData(t, page)
	if len(d.A) != 1 || int(d.A[0][1].(float64)) != 2 {
		t.Fatalf("audit rows = %v, want only path 2 (not the rule's, not needs-review)", d.A)
	}
	if got := d.str(d.A[0][2]); got != "closed-signal-channel" {
		t.Errorf("audit label = %q", got)
	}
	if !strings.Contains(page, `data-audit="1"`) {
		t.Error("audit count missing")
	}
	section := page[strings.Index(page, `id="audit"`):]
	section = section[:strings.Index(section, "</section>")]
	if !strings.Contains(section, "2 more paths were sent by a detector to needs-review or needs-special-test") {
		t.Errorf("the other detector decisions are not counted: %s", section)
	}
	if got := d.str(d.A[0][4]); got != "certain" {
		t.Errorf("audit confidence = %q", got)
	}
	if got := d.str(d.A[0][5]); !strings.Contains(got, "takes the closed side") {
		t.Errorf("audit reason = %q", got)
	}
}

// Every built-in label has a definition in the page, whether used or not,
// and the labels no detector sets say so.
func TestEveryBuiltInLabelIsDefinedInThePage(t *testing.T) {
	in := classified()
	page := mustReport(t, in)
	have := ids(page)
	notYet := 0
	for _, d := range classify.Definitions() {
		if d.Kind != classify.KindLabel {
			continue
		}
		if have["def-"+d.Name] != 1 {
			t.Errorf("label %q has no definition in the page", d.Name)
		}
		if d.NotDetectedYet {
			notYet++
		}
	}
	if got := strings.Count(page, "<strong>Not detected yet.</strong>"); got != notYet || notYet == 0 {
		t.Errorf("page says 'not detected yet' %d times, definitions say %d", got, notYet)
	}
	// A label PathKit does set (used here) is not called "not detected yet".
	i := strings.Index(page, `id="def-closed-signal-channel"`)
	j := strings.Index(page[i:], "</article>")
	if strings.Contains(page[i:i+j], "Not detected yet") {
		t.Error("closed-signal-channel is detected, but the page says it is not")
	}
	// Unused labels are in the collapsed group.
	k := strings.Index(page, `<details class="unused">`)
	if k < 0 || strings.Index(page, `id="def-dead-code"`) < k {
		t.Error("an unused label is not in the collapsed group")
	}
}

// A path whose label has no definition is never printed silently.
func TestMissingDefinitionIsSaidOutLoud(t *testing.T) {
	in := classified()
	in.Result.Workflows[0].Paths[1].Class.Label = "no-such-label"
	page := mustReport(t, in)
	if !strings.Contains(page, "No definition was found for this label") {
		t.Error("a label without a definition was hidden")
	}
	if strings.Contains(mustReport(t, classified()), "No definition was found") {
		t.Error("a defined label was reported as missing")
	}
}

// Custom label text and every explanation are escaped; the built-in text
// is the built-in text.
func TestGlossaryEscapes(t *testing.T) {
	page := mustReport(t, classified())
	for _, raw := range []string{"<script>", "<b>", `alert("x")`} {
		if strings.Contains(page, raw) {
			t.Errorf("raw %q in the page", raw)
		}
	}
	if !strings.Contains(html.UnescapeString(page), "custom meaning "+danger) {
		t.Error("custom label meaning not shown")
	}
}

func TestFooterVersionAndExample(t *testing.T) {
	page := mustReport(t, classified())
	if !strings.Contains(page, "Definitions version "+classify.DefinitionsVersion()) {
		t.Error("definitions version missing from the footer")
	}
	if !strings.Contains(html.UnescapeString(page), "From this run:") {
		t.Error("a used label has no example from this run")
	}
}

// With classification off the page has no glossary, audit or extra styles.
func TestNoGlossaryWithoutClassification(t *testing.T) {
	page := mustReport(t, sample("p.W", "p/w.go", "why"))
	for _, s := range []string{`id="definitions"`, `id="audit"`, "Definitions version", `class="jump"`, `id="def-`} {
		if strings.Contains(page, s) {
			t.Errorf("page has %q with classification off", s)
		}
	}
}
