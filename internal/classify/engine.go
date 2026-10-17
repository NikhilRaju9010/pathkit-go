package classify

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/facts"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

func sortDefinitions(d []Definition) {
	sort.Slice(d, func(i, j int) bool { return d[i].Name < d[j].Name })
}

// Engine classifies paths from what people wrote down: config overrides
// (by stableId), //pathkit:untestable comments and config rules. It is the
// only classifier that exists until detectors arrive; with no overrides,
// comments or rules it says "testable" for every path.
//
// Order, strongest first (docs/DESIGN.md D13):
//
//	config mark_testable / mark_untestable  >  //pathkit:untestable
//	comment  >  config rule  >  detectors  >  testable
//
// The first that applies is the main reason; the others that also apply
// are kept as ExtraReasons. Whether a path a test ran may be called
// untestable is not decided here: coverage.Compute never lets it.
//
// An Engine counts what it matched while it classifies, so Findings and
// Notes are only meaningful after coverage.Compute has run with it.
type Engine struct {
	registry  *Registry
	policy    Policy
	overrides map[string]*override // workflow + "\x00" + stableId
	order     []*override
	rules     map[string][]*rule // by workflow
	allRules  []*rule
	marks     map[string][]model.Mark
	detectors []Detector
	disabled  []scope.DisabledDetector
	// facts is the fact engine's analyzer for each workflow (see AddGraph);
	// engineOff is true when the config turns the engine off.
	facts     map[string]*facts.Analyzer
	graphs    map[string]*model.Graph
	engineOff bool
	// jsonMode is classification.assumptions.default_json_converter; jsonHits
	// are the places a custom data converter was found in the module (the
	// assumption is then off).
	jsonMode string
	jsonHits []string
	// jsonRelevant: some workflow has a value the assumption is about.
	jsonRelevant bool
	// icptMode is classification.assumptions.no_custom_interceptor; icptHits are
	// the places a custom interceptor is registered in the module (the
	// assumption is then off); icptRelevant: some workflow hands a slice or map
	// to a call that encodes its arguments.
	icptMode     string
	icptHits     []string
	icptRelevant bool
	// stageMax and maxContexts are classification.stages.max_paths and
	// classification.engine.max_contexts; plans and listings are kept per workflow.
	stageMax, maxContexts int
	pathCap               int // how many paths a listing holds (model.DefaultMaxPaths)
	plans                 map[string]*StagePlan
	listings              map[string]*Listing
	explained             []scope.ExplainedTrace
	// extraWarnings are config warnings found while resolving rules.
	extraWarnings []string
	// anchors are where each numbered ("#n") rule entry points, for the
	// CI baseline (see Anchor).
	anchors []Anchor
}

// SetExplained gives the engine the traces the config accepts (workflow
// names already resolved to full names).
func (e *Engine) SetExplained(x []scope.ExplainedTrace) { e.explained = x }

// Explained returns the accepted traces, for coverage.Input.
func (e *Engine) Explained() []scope.ExplainedTrace { return e.explained }

type override struct {
	scope.PathOverride
	matched int
}

type rule struct {
	scope.Rule
	when    []string
	matched int // paths that take every decision of "when" and none of "without"
	// whenSets and withoutSets are the entries resolved against the
	// workflow's graph (see AddGraph); resolved is false until then.
	whenSets, withoutSets [][]string
	resolved              bool
	off                   bool // not applied: a "#n/m" entry's count changed (see AddGraph)
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// NewEngine checks a config's classification section against the label
// registry. Workflow names in c must already be full names
// ("orders.OrderWorkflow"): the caller resolves them against the loaded
// code.
func NewEngine(c scope.Classification) (*Engine, error) {
	reg, err := NewRegistry(c.Custom)
	if err != nil {
		return nil, err
	}
	e := &Engine{registry: reg, policy: CountAsTestable, overrides: map[string]*override{}, rules: map[string][]*rule{}, marks: map[string][]model.Mark{},
		facts: map[string]*facts.Analyzer{}, graphs: map[string]*model.Graph{}, engineOff: c.EngineOff, jsonMode: c.JSONConverter, icptMode: c.NoCustomInterceptor,
		stageMax: orDefault(c.StageMaxPaths, model.DefaultStageMaxPaths), maxContexts: orDefault(c.MaxContexts, facts.DefaultMaxContexts),
		plans: map[string]*StagePlan{}, listings: map[string]*Listing{}, pathCap: model.DefaultMaxPaths}
	if c.Policy == string(Exclude) {
		e.policy = Exclude
	}
	if err := e.setDetectors(c.DisabledDetectors); err != nil {
		return nil, err
	}
	for _, o := range c.Overrides {
		key := o.Workflow + "\x00" + o.StableID
		if _, dup := e.overrides[key]; dup {
			return nil, fmt.Errorf("%s %s is listed more than once in classification.paths (the same workflow written two ways?)", o.Workflow, o.StableID)
		}
		if o.Untestable {
			if o.Label == "" {
				o.Label = "manual"
			}
			d, ok := reg.Label(o.Label)
			if !ok {
				return nil, fmt.Errorf("classification.paths.mark_untestable (%s %s): label %q is not defined (built-in labels are in the definitions list; add your own with custom_labels)", o.Workflow, o.StableID, o.Label)
			}
			if d.Bucket != "" && d.Bucket != CannotBeTested {
				return nil, fmt.Errorf("classification.paths.mark_untestable (%s %s): label %q puts paths in %s, not cannot-be-tested", o.Workflow, o.StableID, o.Label, d.Bucket)
			}
		}
		ov := &override{PathOverride: o}
		e.overrides[key] = ov
		e.order = append(e.order, ov)
	}
	for _, r := range c.Rules {
		d, ok := reg.Label(r.Label)
		if !ok {
			return nil, fmt.Errorf("classification.rules: rule %q: label %q is not defined (add it with custom_labels)", r.ID, r.Label)
		}
		if d.Bucket != CannotBeTested {
			if d.Bucket == "" {
				d.Bucket = CannotBeTested // "manual": as chosen, and a rule says impossible
			} else {
				return nil, fmt.Errorf("classification.rules: rule %q: label %q puts paths in %s, but a rule says a combination is impossible (cannot-be-tested)", r.ID, r.Label, d.Bucket)
			}
		}
		rl := &rule{Rule: r}
		for _, w := range r.When {
			rl.when = append(rl.when, collapse(w))
		}
		e.rules[r.Workflow] = append(e.rules[r.Workflow], rl)
		e.allRules = append(e.allRules, rl)
	}
	return e, nil
}

// CustomLabels returns the definitions of the config's custom labels, for
// the report's glossary.
func (e *Engine) CustomLabels() []Definition { return e.registry.Custom() }

// Label returns a label's definition, built-in or from the config.
func (e *Engine) Label(name string) (Definition, bool) { return e.registry.Label(name) }

// Policy is how needs-review paths count in testable coverage.
func (e *Engine) Policy() Policy { return e.policy }

// AddMarks gives the engine a workflow's //pathkit:untestable comments.
func (e *Engine) AddMarks(workflow string, marks []model.Mark) {
	if len(marks) > 0 {
		e.marks[workflow] = marks
	}
}

// Classify implements Classifier.
func (e *Engine) Classify(workflow string, p model.Path) Classification {
	return e.classify(workflow, p, true)
}

// Claim is what Classify would say about p, without counting the match
// (the stale-entry check counts matches; explain asks a second time).
func (e *Engine) Claim(workflow string, p model.Path) Classification {
	return e.classify(workflow, p, false)
}

func (e *Engine) classify(workflow string, p model.Path, count bool) Classification {
	type candidate struct {
		c    Classification
		note string
	}
	var found []candidate

	if o, ok := e.overrides[workflow+"\x00"+p.StableID()]; ok {
		if count {
			o.matched++
		}
		c := DefaultTestable()
		c.Source, c.Origin = SourceManual, OriginOverride
		if o.Untestable {
			c.Bucket, c.Label, c.PossibleInProduction = CannotBeTested, o.Label, false
			c.Explanation = fmt.Sprintf("Marked untestable in the config (%s): %s", o.Label, o.Reason)
			found = append(found, candidate{c, "config mark_untestable"})
		} else {
			c.Explanation = "Marked testable in the config: " + o.Reason
			found = append(found, candidate{c, "config mark_testable"})
		}
	}

	for _, m := range e.marks[workflow] {
		for _, s := range p.Steps {
			if s.Exit == m.Exit {
				c := DefaultTestable()
				c.Source, c.Origin, c.Bucket, c.Label, c.PossibleInProduction = SourceManual, OriginComment, CannotBeTested, "manual", false
				c.Explanation = fmt.Sprintf("Marked untestable by //pathkit:untestable at %s:%d: %s", filepath.Base(m.Pos.Filename), m.Pos.Line, m.Reason)
				found = append(found, candidate{c, fmt.Sprintf("//pathkit:untestable at %s:%d", filepath.Base(m.Pos.Filename), m.Pos.Line)})
				break
			}
		}
	}

	decisions := map[string]bool{}
	for _, d := range p.Decisions() {
		decisions[d] = true
	}
	for _, r := range e.rules[workflow] {
		if !r.matches(decisions) {
			continue
		}
		if count {
			r.matched++
		}
		c := DefaultTestable()
		c.Source, c.Origin, c.Bucket, c.Label, c.PossibleInProduction, c.RuleID = SourceManual, OriginRule, CannotBeTested, r.Label, false, r.ID
		c.Explanation = fmt.Sprintf("Rule %q: %s", r.ID, r.Reason)
		found = append(found, candidate{c, fmt.Sprintf("rule %q", r.ID)})
	}

	for _, d := range e.detectors {
		if c, ok := d.Detect(p); ok {
			c = e.settle(c)
			c.Detector = d.Name()
			found = append(found, candidate{c, "detector " + d.Name()})
		}
	}

	if len(found) == 0 {
		if c, ok := e.engineClaim(workflow, p); ok {
			return c
		}
		return DefaultTestable()
	}
	main := found[0].c
	for _, f := range found[1:] {
		main.ExtraReasons = append(main.ExtraReasons, f.note)
	}
	return main
}

// Findings are the warnings a run produces about the config itself, for
// the workflows that were measured: an entry that matches no path is stale
// (the code changed, or the id was mistyped), and so is a rule none of
// whose paths takes all its decisions.
//
// truncated names the workflows with more paths than PathKit lists
// (model.DefaultMaxPaths, per workflow). For those, "matches no listed
// path" does not mean stale: the path may be beyond the path cap, and the
// message says so instead of calling the entry stale.
func (e *Engine) Findings(measured []string, truncated map[string]bool) []string {
	in := map[string]bool{}
	for _, m := range measured {
		in[m] = true
	}
	cap := func(workflow string) string {
		return fmt.Sprintf("%s has more paths than the first %d that PathKit lists (beyond the path cap), so the path may simply not be listed", workflow, model.DefaultMaxPaths)
	}
	var out []string
	out = append(out, e.extraWarnings...)
	for _, d := range e.disabled {
		out = append(out, fmt.Sprintf("classification: detector %s is turned off in the config (%s); paths it would have marked stay testable", d.Name, d.Reason))
	}
	for _, o := range e.order {
		if !in[o.Workflow] || o.matched > 0 {
			continue
		}
		if truncated[o.Workflow] {
			out = append(out, fmt.Sprintf("classification: %s %s (%s) matches none of the listed paths; %s",
				o.Workflow, o.StableID, o.Reason, cap(o.Workflow)))
			continue
		}
		out = append(out, fmt.Sprintf("classification: stale entry: %s %s (%s) matches no path of the current code; check the id with \"pathkit coverage --json\" (stableId), or remove the entry%s",
			o.Workflow, o.StableID, o.Reason, e.replacements(o.Workflow, o.StableID)))
	}
	for _, r := range e.allRules {
		if !in[r.Workflow] || r.matched > 0 || r.off {
			continue
		}
		if truncated[r.Workflow] {
			out = append(out, fmt.Sprintf("classification: rule %q matches none of the listed paths of %s; %s", r.ID, r.Workflow, cap(r.Workflow)))
			continue
		}
		hint := ""
		if g := e.graphs[r.Workflow]; g != nil && g.UsesHelpers() {
			hint = fmt.Sprintf(". %s follows helper functions now: a decision inside one has its own text, with the helper and the call named (\"pathkit analyze\" prints it)", r.Workflow)
		}
		out = append(out, fmt.Sprintf("classification: stale rule %q: no path of %s takes all of its \"when\" decisions; write them as \"pathkit analyze\" prints them, like \"if x > 0 --true\"%s", r.ID, r.Workflow, hint))
	}
	return out
}

// Notes are lines for the classification block saying how many paths each
// source marked. final is every path's classification after coverage has
// run, so a path a test ran (and so stayed testable) is not counted as
// marked. Only entries for the measured workflows are listed. Empty when
// nothing was configured for them.
func (e *Engine) Notes(final []Classification, measured []string) []string {
	in := map[string]bool{}
	for _, m := range measured {
		in[m] = true
	}
	var untestable, testable, comments int
	byRule := map[string]int{}
	byDetector := map[string]int{} // "<detector>\x00<bucket>"
	for _, c := range final {
		if c.Detector != "" && c.Bucket != Testable && c.Origin == "" {
			byDetector[c.Detector+"\x00"+string(c.Bucket)]++
		}
		switch c.Origin {
		case OriginOverride:
			if c.Bucket == Testable {
				testable++
			} else {
				untestable++
			}
		case OriginComment:
			comments++
		case OriginRule:
			byRule[c.RuleID]++
		}
	}
	var out []string
	anyOverride, anyMark := false, false
	for _, o := range e.order {
		anyOverride = anyOverride || in[o.Workflow]
	}
	for w := range e.marks {
		anyMark = anyMark || in[w]
	}
	if anyOverride {
		out = append(out, fmt.Sprintf("Config overrides: %s marked untestable, %s marked testable", pathsN(untestable), pathsN(testable)))
	}
	if anyMark {
		out = append(out, fmt.Sprintf("//pathkit:untestable comments: %s marked untestable", pathsN(comments)))
	}
	for _, r := range e.allRules {
		if !in[r.Workflow] {
			continue
		}
		out = append(out, fmt.Sprintf("Rule %q (%s): %s marked untestable", r.ID, r.Workflow, pathsN(byRule[r.ID])))
	}
	var keys []string
	for k := range byDetector {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		name, bucket, _ := strings.Cut(k, "\x00")
		out = append(out, fmt.Sprintf("Detector %s: %s marked %s", name, pathsN(byDetector[k]), bucket))
	}
	for _, d := range e.disabled {
		out = append(out, fmt.Sprintf("Detector %s is turned off in the config: %s", d.Name, d.Reason))
	}
	out = append(out, groupNotes(final)...)
	out = append(out, e.assumptionNotes(final)...)
	return out
}

func pathsN(n int) string {
	if n == 1 {
		return "1 path"
	}
	return fmt.Sprintf("%d paths", n)
}

// SetConverterScan tells the engine where the module sets its own Temporal
// data converter (see load.CustomConverterUse). Any hit turns the assumption
// default-json-converter off. Call it before AddGraph.
func (e *Engine) SetConverterScan(hits []string) { e.jsonHits = hits }

// JSONConverterOn reports whether claims may rest on Temporal's default JSON
// converter, and when not, why.
func (e *Engine) JSONConverterOn() (on bool, why string) {
	switch {
	case e.engineOff:
		return false, "the fact engine is off"
	case e.jsonMode == "off":
		return false, "turned off in the config (classification.assumptions.default_json_converter)"
	case len(e.jsonHits) > 0:
		return false, "the module sets its own data converter or decodes numbers specially (" + e.jsonHits[0] + ")"
	}
	return true, ""
}

// replacements says, for a saved stableId that no path has any more, which
// paths took its place when PathKit started following helper functions: the
// ones whose older ID it was. "" when there are none.
func (e *Engine) replacements(workflow, stableID string) string {
	g := e.graphs[workflow]
	if g == nil || !g.UsesHelpers() {
		return ""
	}
	var ids []string
	for _, p := range g.Paths(model.DefaultMaxPaths).List {
		if slices.Contains(p.LegacyStableIDs(), stableID) {
			ids = append(ids, p.StableID())
		}
	}
	if len(ids) == 0 {
		return ""
	}
	return fmt.Sprintf(". %s follows helper functions now, and this path was split by what happens inside them: the paths that replace it are %s; mark the ones you mean under their new ids", workflow, strings.Join(ids, ", "))
}

// SetInterceptorScan tells the engine where the module registers a custom
// interceptor (see load.CustomInterceptorUse). Any hit turns the assumption
// no-custom-interceptor off. Call it before AddGraph.
func (e *Engine) SetInterceptorScan(hits []string) { e.icptHits = hits }

// InterceptorOn reports whether facts about a slice or map may be kept across
// a call that encodes its arguments, and when not, why.
func (e *Engine) InterceptorOn() (on bool, why string) {
	switch {
	case e.engineOff:
		return false, "the fact engine is off"
	case e.icptMode == "off":
		return false, "turned off in the config (classification.assumptions.no_custom_interceptor)"
	case len(e.icptHits) > 0:
		return false, "the module registers a custom interceptor (" + e.icptHits[0] + ")"
	}
	return true, ""
}

func orDefault(v, d int) int {
	if v <= 0 {
		return d
	}
	return v
}

// SetPathCap changes how many paths a listing holds. PathKit itself always
// uses model.DefaultMaxPaths; the soundness test uses a small number so that
// pruning happens on small programs.
func (e *Engine) SetPathCap(n int) { e.pathCap = n }
