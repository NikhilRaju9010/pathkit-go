package classify

import (
	"fmt"
	"path/filepath"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// Entry kinds.
const (
	EntryRule     = "rule"     // classification.rules
	EntryOverride = "override" // classification.paths.mark_untestable or mark_testable
	EntryComment  = "comment"  // a //pathkit:untestable comment in the code
)

// Entry is one thing a person wrote that marks paths: a config rule, a
// config override or a source comment. "pathkit rules check" counts what
// each one hits.
type Entry struct {
	Kind     string
	ID       string // rule id, stable id, or "file:line" for a comment
	Workflow string
	Label    string // "" for a mark_testable override
	Reason   string
	// Claims is what the entry says about a path it hits: the bucket, or
	// testable for a mark_testable override.
	Claims Bucket
}

// Key identifies the entry inside one report.
func (en Entry) Key() string { return en.Kind + "\x00" + en.Workflow + "\x00" + en.ID }

// Entries lists every entry written for the workflows in `measured`
// (rules, then overrides, then comments), in the order they were written.
func (e *Engine) Entries(measured []string) []Entry {
	in := map[string]bool{}
	for _, m := range measured {
		in[m] = true
	}
	var out []Entry
	for _, r := range e.allRules {
		if in[r.Workflow] {
			out = append(out, e.ruleEntry(r))
		}
	}
	for _, o := range e.order {
		if in[o.Workflow] {
			out = append(out, e.overrideEntry(o))
		}
	}
	for _, w := range measured {
		for _, m := range e.marks[w] {
			out = append(out, commentEntry(w, m))
		}
	}
	return out
}

func (e *Engine) ruleEntry(r *rule) Entry {
	b := CannotBeTested
	if r.Label == KnownDefect {
		b = NeedsReview
	}
	return Entry{Kind: EntryRule, ID: r.ID, Workflow: r.Workflow, Label: r.Label, Reason: r.Reason, Claims: b}
}

func (e *Engine) overrideEntry(o *override) Entry {
	en := Entry{Kind: EntryOverride, ID: o.StableID, Workflow: o.Workflow, Reason: o.Reason, Claims: Testable}
	if o.Untestable {
		en.Label, en.Claims = o.Label, CannotBeTested
		if o.Label == KnownDefect {
			en.Claims = NeedsReview
		}
	}
	return en
}

func commentEntry(workflow string, m model.Mark) Entry {
	return Entry{Kind: EntryComment, ID: fmt.Sprintf("%s:%d", filepath.Base(m.Pos.Filename), m.Pos.Line), Workflow: workflow,
		Label: "manual", Reason: m.Reason, Claims: CannotBeTested}
}

// EntriesHitting lists every entry that marks path p of a workflow, whatever
// else decides its classification (the first reason wins in Classify; this
// shows them all, to find entries that overlap).
func (e *Engine) EntriesHitting(workflow string, p model.Path) []Entry {
	var out []Entry
	decisions := map[string]bool{}
	for _, d := range p.Decisions() {
		decisions[d] = true
	}
	for _, r := range e.rules[workflow] {
		if r.matches(decisions) {
			out = append(out, e.ruleEntry(r))
		}
	}
	if o, ok := e.overrides[workflow+"\x00"+p.StableID()]; ok {
		out = append(out, e.overrideEntry(o))
	}
	for _, m := range e.marks[workflow] {
		for _, s := range p.Steps {
			if s.Exit == m.Exit {
				out = append(out, commentEntry(workflow, m))
				break
			}
		}
	}
	return out
}
