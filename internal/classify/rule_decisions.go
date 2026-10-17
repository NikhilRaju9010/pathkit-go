package classify

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/facts"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// A rule's "when" and "without" entries name decisions the way
// `pathkit analyze` prints them: the decision's text, then --, then the exit
// taken ("if more --false").
//
// When one workflow has several decisions with the same text (three
// "if more" checks, one per signal), PathKit tells them apart internally
// as "if more", "if more #2", "if more #3". A rule must say which it means:
//
//	"if more #2 --false"      the second one ("#1" names the first)
//	"any: if more --false"    any of them
//
// A plain "if more --false" is an error then. It used to mean "the first
// one", silently, which is almost never what the writer meant.

var (
	numberSuffix = regexp.MustCompile(` #\d+$`)
	firstNumber  = regexp.MustCompile(` #1 --`)
	// "if more #2/3 --false": the second of the 3 decisions with this
	// text. The "/3" is optional; when it is written and the workflow has
	// another number of such decisions now, the rule is not applied (a
	// decision may have been added or removed above, so #2 may no longer
	// be the one the writer meant).
	numberOfCount = regexp.MustCompile(` #(\d+)/(\d+) --`)
)

// countChanged says a "#n/m" entry was written for m decisions with the
// text, and the workflow has another number now.
type countChanged struct {
	entry    string
	text     string
	was, now int
}

func (e *countChanged) Error() string {
	return fmt.Sprintf("%q was written when this workflow had %d decisions with the text %q; it has %d now, so the number may point at a different one", e.entry, e.was, e.text, e.now)
}

const anyPrefix = "any:"

// decisionTable is every decision of one workflow's graph.
type decisionTable struct {
	full  map[string]bool     // "if more #2 --false"
	plain map[string][]string // "if more --false" -> the full forms (one per junction)
}

func newDecisionTable(g *model.Graph) decisionTable {
	t := decisionTable{full: map[string]bool{}, plain: map[string][]string{}}
	stables := map[string]bool{}
	for _, j := range g.Junctions {
		stables[j.StableText()] = true
	}
	for _, j := range g.Junctions {
		stable := j.StableText()
		base := stable
		if b := numberSuffix.ReplaceAllString(stable, ""); b != stable && stables[b] {
			base = b
		}
		exits := j.Exits
		if j.Retry != nil {
			exits = append(exits[:len(exits):len(exits)], j.Retry)
		}
		for _, e := range exits {
			label := collapse(e.Label)
			full := stable + " --" + label
			t.full[full] = true
			t.plain[base+" --"+label] = append(t.plain[base+" --"+label], full)
		}
	}
	return t
}

// resolve turns one entry into the set of decisions it stands for (a path
// satisfies the entry when it takes any one of them). found is false when
// the entry names no decision of the workflow.
func (t decisionTable) resolve(entry string) (set []string, found bool, err error) {
	s := collapse(entry)
	any := false
	if rest, ok := strings.CutPrefix(s, anyPrefix); ok {
		any, s = true, collapse(rest)
	}
	if m := numberOfCount.FindStringSubmatch(s); m != nil {
		var want int
		fmt.Sscanf(m[2], "%d", &want)
		bare := numberOfCount.ReplaceAllString(s, " #"+m[1]+" --")
		plainText := numberSuffix.ReplaceAllString(strings.TrimSuffix(bare[:strings.Index(bare, " --")], ""), "")
		_, exit, _ := strings.Cut(bare, " --")
		now := len(t.plain[plainText+" --"+exit])
		if now != want {
			return nil, true, &countChanged{entry: collapse(entry), text: plainText, was: want, now: now}
		}
		set, found, err = t.resolve(bare)
		return set, found, err
	}
	if firstNumber.MatchString(s) { // "#1" names the first one, explicitly
		s = firstNumber.ReplaceAllString(s, " --")
		if t.full[s] {
			return []string{s}, true, nil
		}
		return nil, false, nil
	}
	switch fulls := t.plain[s]; {
	case len(fulls) == 1:
		return fulls, true, nil
	case len(fulls) > 1:
		if any {
			return fulls, true, nil
		}
		return nil, true, &ambiguousError{entry: collapse(entry), options: fulls}
	}
	if t.full[s] {
		return []string{s}, true, nil
	}
	return nil, false, nil
}

type ambiguousError struct {
	entry   string
	options []string
}

func (e *ambiguousError) Error() string {
	var names []string
	for _, o := range e.options {
		d, exit, _ := strings.Cut(o, " --")
		names = append(names, fmt.Sprintf("%q", d+" --"+exit))
	}
	second := e.options[1]
	return fmt.Sprintf("%q matches %d different decisions (%s); write the one you mean with its number, like %q (#1 is the first), or write %q for any of them",
		e.entry, len(e.options), strings.Join(names, ", "), second, anyPrefix+" "+e.entry)
}

// AddGraph gives the engine a workflow's decisions and resolves its rules'
// entries against them. It returns an error (the config must be fixed) when:
//   - an entry is a plain text that fits more than one decision, or
//   - a "without" entry names no decision of the workflow. A misspelled
//     "when" entry only makes a rule match nothing (reported as stale), but
//     a misspelled "without" entry would make the rule match MORE paths.
func (e *Engine) AddGraph(workflow string, g *model.Graph) error {
	on, _ := e.JSONConverterOn()
	copied, _ := e.InterceptorOn()
	e.graphs[workflow] = g
	e.facts[workflow] = facts.New(g, facts.Options{JSONDecode: on, ArgsCopied: copied})
	e.jsonRelevant = e.jsonRelevant || e.facts[workflow].JSONRelevant()
	e.icptRelevant = e.icptRelevant || e.facts[workflow].InterceptorRelevant()
	t := newDecisionTable(g)
	kept := e.anchors[:0]
	for _, a := range e.anchors {
		if a.Workflow != workflow {
			kept = append(kept, a)
		}
	}
	e.anchors = kept
	for _, r := range e.rules[workflow] {
		for _, w := range append(append([]string(nil), r.Rule.When...), r.Rule.Without...) {
			if a, ok := anchorFor(g, t, r.ID, workflow, w); ok && !e.hasAnchor(a) {
				e.anchors = append(e.anchors, a)
			}
		}
	}
	for _, r := range e.rules[workflow] {
		r.whenSets, r.withoutSets, r.off = nil, nil, false
		for _, w := range r.Rule.When {
			set, found, err := t.resolve(w)
			if cc, ok := err.(*countChanged); ok {
				r.off = true
				e.extraWarnings = append(e.extraWarnings, fmt.Sprintf("classification: rule %q (%s) is not applied: %v; check it and update the number in %q", r.ID, workflow, cc, w))
				continue
			}
			if err != nil {
				return fmt.Errorf("classification.rules: rule %q (%s) \"when\": %v", r.ID, workflow, err)
			}
			if !found { // matches nothing: the rule is reported as stale
				set = []string{collapse(w)}
			}
			r.whenSets = append(r.whenSets, set)
		}
		for _, w := range r.Rule.Without {
			set, found, err := t.resolve(w)
			if cc, ok := err.(*countChanged); ok {
				r.off = true
				e.extraWarnings = append(e.extraWarnings, fmt.Sprintf("classification: rule %q (%s) is not applied: %v; check it and update the number in %q", r.ID, workflow, cc, w))
				continue
			}
			if err != nil {
				return fmt.Errorf("classification.rules: rule %q (%s) \"without\": %v", r.ID, workflow, err)
			}
			if !found {
				return fmt.Errorf("classification.rules: rule %q (%s) \"without\": %q names no decision of this workflow (write it as \"pathkit analyze\" prints it, like \"for range items --iterate\"); a misspelled \"without\" would make the rule claim more paths, so it is an error", r.ID, workflow, w)
			}
			r.withoutSets = append(r.withoutSets, set)
		}
		r.resolved = true
	}
	return nil
}

// Anchor is where a numbered rule entry ("if more #2 --false") points: the
// decisions the map says come right before the one it names. The text of
// identical decisions is the same by definition, so a shift of "#2" (an
// identical decision added or removed above it) cannot show in the text;
// it shows in the neighbourhood. The CI baseline stores anchors, and a
// later run warns when one changed.
type Anchor struct {
	Rule     string
	Workflow string
	Entry    string   // as written, spaces collapsed
	Before   []string // "Start", or "<decision> --<exit>", sorted
	// After is where each exit of the decision leads ("--true--> <next
	// decision>", "--false--> End (completed)"), in exit order. Before
	// alone misses a decision added at the very top: the new #1 also comes
	// right after Start.
	After []string
	// Count is how many decisions of the workflow have this text. With
	// three or more in a row, adding or removing one shifts every number
	// together, so the new #2's neighbours look just like the old #2's;
	// the count is what changes then.
	Count int
}

var numbered = regexp.MustCompile(` #\d+(/\d+)? --`)

func anchorFor(g *model.Graph, t decisionTable, ruleID, workflow, entry string) (Anchor, bool) {
	s := collapse(entry)
	if strings.HasPrefix(s, anyPrefix) || !numbered.MatchString(s) {
		return Anchor{}, false
	}
	set, found, err := t.resolve(s)
	if err != nil || !found || len(set) != 1 {
		return Anchor{}, false
	}
	text := set[0][:strings.LastIndex(set[0], " --")]
	var target *model.Junction
	base := numberSuffix.ReplaceAllString(text, "")
	same := 0
	for _, j := range g.Junctions {
		if j.StableText() == text {
			target = j
		}
		if numberSuffix.ReplaceAllString(j.StableText(), "") == base {
			same++
		}
	}
	if target == nil {
		return Anchor{}, false
	}
	return Anchor{Rule: ruleID, Workflow: workflow, Entry: s, Before: before(g, target), After: after(target), Count: same}, true
}

// after lists where each exit of j leads.
func after(j *model.Junction) []string {
	var out []string
	for _, e := range j.Exits {
		to, via := e.To, ""
		if to.Retry != nil {
			via, to = "retry --> ", to.Retry.To
		}
		next := to.End.String()
		switch {
		case to.Junction != nil:
			next = to.Junction.StableText()
		case to.Dead():
			next = "panic"
		}
		out = append(out, "--"+collapse(e.Label)+"--> "+via+next)
	}
	return out
}

// before lists the exits that lead straight to j ("Start" for the start).
func before(g *model.Graph, j *model.Junction) []string {
	seen := map[string]bool{}
	if g.Start.Junction == j {
		seen["Start"] = true
	}
	for _, from := range g.Junctions {
		exits := from.Exits
		if from.Retry != nil {
			exits = append(exits[:len(exits):len(exits)], from.Retry)
		}
		for _, e := range exits {
			to := e.To
			if to.Retry != nil {
				to = to.Retry.To
			}
			if to.Junction == j {
				seen[from.StableText()+" --"+collapse(e.Label)] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (e *Engine) hasAnchor(a Anchor) bool {
	for _, b := range e.anchors {
		if b.Rule == a.Rule && b.Workflow == a.Workflow && b.Entry == a.Entry {
			return true
		}
	}
	return false
}

// Anchors are the anchors of every numbered rule entry of the workflows
// given so far, sorted by rule and entry.
func (e *Engine) Anchors() []Anchor {
	out := append([]Anchor(nil), e.anchors...)
	sort.Slice(out, func(i, k int) bool {
		if out[i].Rule != out[k].Rule {
			return out[i].Rule < out[k].Rule
		}
		return out[i].Entry < out[k].Entry
	})
	return out
}

// matches reports whether a path's decisions satisfy the rule.
func (r *rule) matches(decisions map[string]bool) bool {
	if r.off {
		return false
	}
	whens, withouts := r.whenSets, r.withoutSets
	if !r.resolved { // no graph was given: exact text, as before
		whens, withouts = nil, nil
		for _, w := range r.when {
			whens = append(whens, []string{w})
		}
		for _, w := range r.Rule.Without {
			withouts = append(withouts, []string{collapse(w)})
		}
	}
	takes := func(set []string) bool {
		for _, d := range set {
			if decisions[d] {
				return true
			}
		}
		return false
	}
	for _, set := range whens {
		if !takes(set) {
			return false
		}
	}
	for _, set := range withouts {
		if takes(set) {
			return false
		}
	}
	return true
}
