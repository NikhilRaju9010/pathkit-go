package htmlreport

import (
	"fmt"
	"sort"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
)

// The glossary, the summary table and the detector audit. They
// are shown only when the classification block is shown, so a report made
// with classification off is byte-for-byte what it was before.
//
// Everything here is laid out from data computed elsewhere: the paths'
// classifications come from the shared measure, the wording of every label
// from classify.Definitions (the one list), and the sentences from render.
// A label is a link to its definition, a definition lists links back to
// where it is used (a link to a path is drawn by the page's script), and
// the browser's own Back button returns to where the reader was.

const maxUseLinks = 10

type (
	glossaryView struct {
		Version    string
		Summary    []summaryRow
		Used       []defView
		Unused     []defView
		Audit      []auditRow
		AuditOther int // paths a detector sent to needs-review or needs-special-test
		// Assumptions are the assumptions that are off or relied on.
		Assumptions []assumptionView
		// Groups are the unknown-input-type paths, by the type switch they take.
		Groups []groupRow
		// Terms are the coverage words of the headline, in plain English.
		Terms []termView
	}
	termView       struct{ Name, Meaning string }
	assumptionView struct {
		Name, Status, Meaning, Why, WhatToDo string
		Claims                               int
	}
	groupRow struct {
		Switch string
		Paths  int
	}
	summaryRow struct {
		Label, Bucket, DecidedBy string
		Count, Auto, Manual      int
		NotDetectedYet           bool
	}
	defView struct {
		Name, Bucket, Meaning, Why, Example, PossibleInProduction string
		DecidedBy, Confidence, WhatToDo                           string
		Count                                                     int
		Custom, NotDetectedYet                                    bool
		// PersonCanSet: a mark or a rule may use this label (only
		// cannot-be-tested labels can be set by a person).
		PersonCanSet              bool
		ExampleWhere, ExampleText string
		FirstUse                  string
		Uses                      []useLink
		MoreUses                  int
	}
	useLink struct{ Anchor, Text string }
	// auditRow is one path a detector (not a rule or a person) marked
	// cannot-be-tested.
	auditRow struct {
		Workflow, Anchor, Label, Detector, Confidence, Reason, Path string
		Number, WfIndex                                             int
	}
)

func useAnchor(wi, number int) string { return fmt.Sprintf("use-%d-%d", wi+1, number) }

// definitionsByName is the built-in labels plus the config's custom ones.
func definitionsByName(custom []classify.Definition) (order []classify.Definition, byName map[string]classify.Definition) {
	byName = map[string]classify.Definition{}
	for _, d := range classify.Definitions() {
		if d.Kind == classify.KindLabel {
			order = append(order, d)
			byName[d.Name] = d
		}
	}
	for _, d := range custom {
		order = append(order, d)
		byName[d.Name] = d
	}
	return order, byName
}

// headlineTerms explain the words of the project headline.
var headlineTerms = []termView{
	{"Path coverage", "The share of a workflow's paths that a test ran, counting only the paths a test can run (not the impossible ones). In the headline it covers only the workflows whose paths are all listed (at most 2000 each)."},
	{"Too big for path coverage", "A workflow with more paths than the list holds. Its listed paths are only the first ones, so a coverage figure over them says little (0 of 2000 listed can hide thousands of paths a test ran). It is measured by stage coverage and branch coverage instead."},
	{"Stage coverage", "A stage is a stretch of a workflow between two decisions that every path goes through. The paths of the stages multiply into the paths of the workflow, so each stage is small enough to list and cover on its own. Stage coverage is the stage paths a test ran, over the stage paths not shown impossible."},
	{"Branch coverage", "The share of the exits of decisions that a test took at least once, over the whole map of every workflow."},
}

func buildGlossary(res coverage.Result, custom []classify.Definition, assumptions []classify.AssumptionStatus) *glossaryView {
	order, _ := definitionsByName(custom)
	isCustom := map[string]bool{}
	for _, d := range custom {
		isCustom[d.Name] = true
	}
	type usage struct {
		count, auto, manual int
		uses                []useLink
		exampleWhere, text  string
	}
	use := map[string]*usage{}
	groups := map[string]int{}
	g := &glossaryView{Version: classify.DefinitionsVersion(), Terms: headlineTerms}
	for wi, w := range res.Workflows {
		for _, p := range w.Paths {
			c := p.Class
			if c.Label == "" {
				continue
			}
			if c.Label == "unknown-input-type" && c.Group != "" {
				groups[c.Group]++
			}
			u := use[c.Label]
			if u == nil {
				u = &usage{}
				use[c.Label] = u
			}
			u.count++
			if c.Source == classify.SourceAuto {
				u.auto++
			} else {
				u.manual++
			}
			where := fmt.Sprintf("%s path %d", w.Name, p.Number)
			if len(u.uses) == 0 {
				u.exampleWhere, u.text = where, render.PathText(p.Path)
			}
			u.uses = append(u.uses, useLink{Anchor: useAnchor(wi, p.Number), Text: where})
			if c.Detector != "" && c.Source == classify.SourceAuto && c.Bucket == classify.CannotBeTested {
				g.Audit = append(g.Audit, auditRow{Workflow: w.Name, WfIndex: wi, Number: p.Number, Anchor: useAnchor(wi, p.Number),
					Label: c.Label, Detector: c.Detector, Confidence: string(c.Confidence), Reason: c.Explanation, Path: render.PathText(p.Path)})
			} else if c.Detector != "" && c.Source == classify.SourceAuto {
				g.AuditOther++
			}
		}
	}
	mk := func(d classify.Definition, u *usage) defView {
		v := defView{Name: d.Name, Bucket: string(d.Bucket), Meaning: d.Meaning, Why: d.Why, Example: d.Example,
			PossibleInProduction: d.PossibleInProduction, DecidedBy: d.DecidedBy, Confidence: string(d.Confidence),
			WhatToDo: d.WhatToDo, Custom: isCustom[d.Name], NotDetectedYet: d.NotDetectedYet,
			PersonCanSet: d.Bucket == classify.CannotBeTested || d.Bucket == ""}
		if u != nil {
			v.Count, v.ExampleWhere, v.ExampleText = u.count, u.exampleWhere, u.text
			uses := u.uses
			if len(uses) > maxUseLinks {
				v.MoreUses = len(uses) - maxUseLinks
				uses = uses[:maxUseLinks]
			}
			v.Uses = uses
		}
		return v
	}
	seen := map[string]bool{}
	for _, d := range order {
		seen[d.Name] = true
		if u := use[d.Name]; u != nil {
			v := mk(d, u)
			g.Used = append(g.Used, v)
			g.Summary = append(g.Summary, summaryRow{Label: d.Name, Bucket: string(d.Bucket), DecidedBy: d.DecidedBy,
				Count: u.count, Auto: u.auto, Manual: u.manual, NotDetectedYet: d.NotDetectedYet})
		} else {
			g.Unused = append(g.Unused, mk(d, nil))
		}
	}
	for name, n := range groups {
		g.Groups = append(g.Groups, groupRow{Switch: name, Paths: n})
	}
	sort.Slice(g.Groups, func(i, j int) bool {
		if g.Groups[i].Paths != g.Groups[j].Paths {
			return g.Groups[i].Paths > g.Groups[j].Paths
		}
		return g.Groups[i].Switch < g.Groups[j].Switch
	})
	for _, a := range assumptions {
		d, _ := classify.Lookup(classify.KindAssumption, a.Name)
		status := "on"
		if !a.On {
			status = "OFF: " + a.Why
		}
		g.Assumptions = append(g.Assumptions, assumptionView{Name: a.Name, Status: status, Meaning: d.Meaning, Why: d.Why, WhatToDo: d.WhatToDo, Claims: a.Claims})
	}
	// A label with no definition is a bug (a test forbids it); never hide
	// it: list it as used, with a plain statement.
	var missing []string
	for name := range use {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		u := use[name]
		d := classify.Definition{Name: name, Meaning: "No definition was found for this label. This is a PathKit bug; please report it.", PossibleInProduction: "unknown", DecidedBy: "unknown"}
		g.Used = append(g.Used, mk(d, u))
		g.Summary = append(g.Summary, summaryRow{Label: name, Count: u.count, Auto: u.auto, Manual: u.manual})
	}
	return g
}
