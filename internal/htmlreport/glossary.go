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

func buildGlossary(res coverage.Result, custom []classify.Definition) *glossaryView {
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
	g := &glossaryView{Version: classify.DefinitionsVersion()}
	for wi, w := range res.Workflows {
		for _, p := range w.Paths {
			c := p.Class
			if c.Label == "" {
				continue
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
