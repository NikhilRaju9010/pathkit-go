package scope

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Classification is the checked "classification" section of the config.
// Only its shape is checked here. Whether a workflow name exists, whether a
// label is defined, and whether a stableId matches a path are checked later,
// when the workflows are loaded (internal/classify and the CLI).
type Classification struct {
	Enabled   bool
	Overrides []PathOverride
	Rules     []Rule
	Custom    []CustomLabel
	Policy    string // "count_as_testable" (the default) or "exclude"
	// DisabledDetectors are the automatic detectors a person turned off,
	// each with the reason. Whether a name is a real detector is checked
	// when the engine is built.
	DisabledDetectors []DisabledDetector
	// Explained are traces a person has looked at and accepted as
	// understood: a trace that fits no path, identified by its steps id
	// (printed by the report), with the reason it is fine.
	Explained []ExplainedTrace
	// CI is the "ci" part: what makes a CI run fail.
	CI CISettings
	// EngineOff is true when the config turns the fact engine off
	// (classification.engine.enabled: false). The default is on.
	EngineOff bool
	// JSONConverter is classification.assumptions.default_json_converter:
	// "auto" (the default: on unless the module sets a custom data
	// converter) or "off".
	JSONConverter string
}

// CISettings is classification.ci.
type CISettings struct {
	// Baseline is the baseline file as written in the config ("" none);
	// relative to the config file's folder (Config.Abs).
	Baseline string
	// FailUnderTestable is the floor for testable coverage, in percent;
	// nil means no floor.
	FailUnderTestable *float64
}

// ExplainedTrace is one explained_traces entry.
type ExplainedTrace struct {
	Workflow string
	StepsID  string
	Reason   string
}

// DisabledDetector is one detectors.disable entry.
type DisabledDetector struct {
	Name   string
	Reason string
}

// PathOverride is one paths.mark_untestable or paths.mark_testable entry:
// a person's decision about one path, found by its stableId.
type PathOverride struct {
	Workflow   string
	StableID   string
	Label      string // untestable only; "" means "manual"
	Reason     string
	AddedBy    string
	AddedOn    string
	Untestable bool // false: mark_testable
}

// Rule says a combination of decisions is impossible: every path that takes
// all the listed decisions is cannot-be-tested.
type Rule struct {
	ID       string
	Workflow string
	Label    string
	// When lists decisions as they read in `pathkit analyze`: the
	// decision's text, then --, then the exit taken, e.g.
	// "if o.AmountCents > 100_000 --true".
	When []string
	// Without lists decisions the path must NOT take. A path matches the
	// rule only when it takes every decision of When and none of Without.
	Without []string
	Reason  string
}

// CustomLabel is a label the config defines, usable in overrides and rules.
type CustomLabel struct {
	Name       string
	Definition string
	Bucket     string // cannot-be-tested, needs-special-test or needs-review
}

// The classification section's JSON shape; unknown keys are refused.
type classificationPart struct {
	Enabled            *bool             `json:"enabled"`
	Paths              *pathsPart        `json:"paths"`
	Rules              []json.RawMessage `json:"rules"`
	CustomLabels       []json.RawMessage `json:"custom_labels"`
	UnclassifiedPolicy *string           `json:"unclassified_policy"`
	Detectors          *detectorsPart    `json:"detectors"`
	ExplainedTraces    []json.RawMessage `json:"explained_traces"`
	CI                 json.RawMessage   `json:"ci"`
	Engine             *enginePart       `json:"engine"`
	Assumptions        *assumptionsPart  `json:"assumptions"`
}

type assumptionsPart struct {
	DefaultJSONConverter *string `json:"default_json_converter"`
}

type enginePart struct {
	Enabled *bool `json:"enabled"`
}

type ciEntry struct {
	Baseline          *string  `json:"baseline"`
	FailUnderTestable *float64 `json:"fail_under_testable"`
}

type explainedEntry struct {
	Workflow *string `json:"workflow"`
	StepsID  *string `json:"steps_id"`
	Reason   *string `json:"reason"`
}

var stepsIDPattern = regexp.MustCompile(`^t1:[0-9a-f]{12}$`)

type detectorsPart struct {
	Disable []json.RawMessage `json:"disable"`
}

type disableEntry struct {
	Name   *string `json:"name"`
	Reason *string `json:"reason"`
}

type pathsPart struct {
	MarkUntestable []json.RawMessage `json:"mark_untestable"`
	MarkTestable   []json.RawMessage `json:"mark_testable"`
}

type overrideEntry struct {
	Workflow *string `json:"workflow"`
	StableID *string `json:"stable_id"`
	Label    *string `json:"label"`
	Reason   *string `json:"reason"`
	AddedBy  *string `json:"added_by"`
	AddedOn  *string `json:"added_on"`
}

type testableEntry struct {
	Workflow *string `json:"workflow"`
	StableID *string `json:"stable_id"`
	Reason   *string `json:"reason"`
}

type ruleEntry struct {
	ID         *string   `json:"id"`
	Workflow   *string   `json:"workflow"`
	Label      *string   `json:"label"`
	When       *[]string `json:"when"`
	Without    *[]string `json:"without"`
	Impossible *bool     `json:"impossible"`
	Reason     *string   `json:"reason"`
}

type customEntry struct {
	Name       *string `json:"name"`
	Definition *string `json:"definition"`
	Bucket     *string `json:"bucket"`
}

var (
	stableIDPattern = regexp.MustCompile(`^s1:[0-9a-f]{12}$`)
	ruleIDPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	labelPattern    = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

// customBuckets are the buckets a custom label may put paths in (not
// "testable": a label is a reason a path is NOT an ordinary testable one).
var customBuckets = []string{"cannot-be-tested", "needs-special-test", "needs-review"}

func strict(raw json.RawMessage, into any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(into)
}

func blank(s *string) bool { return s == nil || strings.TrimSpace(*s) == "" }

// readClassification checks the section. fail builds an error that starts
// with the file's path.
func readClassification(p *classificationPart, fail func(string, ...any) error) (Classification, error) {
	var c Classification
	c.Enabled = p.Enabled != nil && *p.Enabled
	c.Policy = "count_as_testable"
	if p.UnclassifiedPolicy != nil {
		switch *p.UnclassifiedPolicy {
		case "count_as_testable", "exclude":
			c.Policy = *p.UnclassifiedPolicy
		default:
			return c, fail(`classification.unclassified_policy must be "count_as_testable" or "exclude", got %q`, *p.UnclassifiedPolicy)
		}
	}

	seen := map[string]string{} // workflow|stable_id -> the list it was in
	if p.Paths != nil {
		for i, raw := range p.Paths.MarkUntestable {
			where := fmt.Sprintf("classification.paths.mark_untestable[%d]", i)
			var e overrideEntry
			if err := strict(raw, &e); err != nil {
				return c, fail(`%s must look like {"workflow": "OrderWorkflow", "stable_id": "s1:0123456789ab", "label": "manual", "reason": "why it can never run"} (%s)`, where, jsonProblem(raw, err))
			}
			o, err := overrideFrom(where, e.Workflow, e.StableID, e.Reason, fail)
			if err != nil {
				return c, err
			}
			o.Untestable = true
			if e.Label != nil {
				if !labelPattern.MatchString(*e.Label) {
					return c, fail(`%s: "label" must be lowercase words joined by - (like "contradictory"), got %q`, where, *e.Label)
				}
				o.Label = *e.Label
			}
			if e.AddedBy != nil {
				o.AddedBy = *e.AddedBy
			}
			if e.AddedOn != nil {
				if _, err := time.Parse("2006-01-02", *e.AddedOn); err != nil {
					return c, fail(`%s: "added_on" must be a date like 2026-10-09, got %q`, where, *e.AddedOn)
				}
				o.AddedOn = *e.AddedOn
			}
			if err := noDuplicate(seen, o, "mark_untestable", fail); err != nil {
				return c, err
			}
			c.Overrides = append(c.Overrides, o)
		}
		for i, raw := range p.Paths.MarkTestable {
			where := fmt.Sprintf("classification.paths.mark_testable[%d]", i)
			var e testableEntry
			if err := strict(raw, &e); err != nil {
				return c, fail(`%s must look like {"workflow": "OrderWorkflow", "stable_id": "s1:0123456789ab", "reason": "why it can run"} (%s)`, where, jsonProblem(raw, err))
			}
			o, err := overrideFrom(where, e.Workflow, e.StableID, e.Reason, fail)
			if err != nil {
				return c, err
			}
			if err := noDuplicate(seen, o, "mark_testable", fail); err != nil {
				return c, err
			}
			c.Overrides = append(c.Overrides, o)
		}
	}

	ids := map[string]bool{}
	for i, raw := range p.Rules {
		where := fmt.Sprintf("classification.rules[%d]", i)
		var e ruleEntry
		if err := strict(raw, &e); err != nil {
			return c, fail(`%s must look like {"id": "flag-checked-later", "workflow": "OrderWorkflow", "label": "set-then-test", "when": ["if a --true", "if b --false"], "without": ["for x --iterate"], "impossible": true, "reason": "..."} (%s)`, where, jsonProblem(raw, err))
		}
		if blank(e.ID) || !ruleIDPattern.MatchString(*e.ID) {
			return c, fail(`%s needs an "id" of letters, digits, - and _`, where)
		}
		id := *e.ID
		if ids[id] {
			return c, fail(`rule id %q is used twice`, id)
		}
		ids[id] = true
		if blank(e.Workflow) {
			return c, fail(`rule %q needs a "workflow"`, id)
		}
		if blank(e.Label) {
			return c, fail(`rule %q needs a "label"`, id)
		}
		if !labelPattern.MatchString(*e.Label) {
			return c, fail(`rule %q: "label" must be lowercase words joined by - (like "set-then-test"), got %q`, id, *e.Label)
		}
		if e.When == nil || len(*e.When) == 0 {
			return c, fail(`rule %q needs a non-empty "when" list of decisions`, id)
		}
		for _, w := range *e.When {
			if strings.TrimSpace(w) == "" {
				return c, fail(`rule %q has an empty entry in "when"`, id)
			}
		}
		var without []string
		if e.Without != nil {
			if len(*e.Without) == 0 {
				return c, fail(`rule %q has an empty "without" list: leave "without" out, or list the decisions the path must not take`, id)
			}
			for _, w := range *e.Without {
				if strings.TrimSpace(w) == "" {
					return c, fail(`rule %q has an empty entry in "without"`, id)
				}
				for _, x := range *e.When {
					if strings.Join(strings.Fields(w), " ") == strings.Join(strings.Fields(x), " ") {
						return c, fail(`rule %q lists %q in both "when" and "without": no path can take it and not take it`, id, w)
					}
				}
			}
			without = *e.Without
		}
		if e.Impossible == nil || !*e.Impossible {
			return c, fail(`rule %q: "impossible" must be true (a rule can only say a combination of decisions is impossible)`, id)
		}
		if blank(e.Reason) {
			return c, fail(`rule %q needs a "reason"`, id)
		}
		c.Rules = append(c.Rules, Rule{ID: id, Workflow: strings.TrimSpace(*e.Workflow), Label: *e.Label, When: *e.When, Without: without, Reason: *e.Reason})
	}

	if p.Engine != nil && p.Engine.Enabled != nil && !*p.Engine.Enabled {
		c.EngineOff = true
	}
	c.JSONConverter = "auto"
	if p.Assumptions != nil && p.Assumptions.DefaultJSONConverter != nil {
		switch v := *p.Assumptions.DefaultJSONConverter; v {
		case "auto", "off":
			c.JSONConverter = v
		default:
			return c, fail(`classification.assumptions.default_json_converter must be "auto" or "off", got %q`, v)
		}
	}

	if p.Detectors != nil {
		seenDet := map[string]bool{}
		for i, raw := range p.Detectors.Disable {
			where := fmt.Sprintf("classification.detectors.disable[%d]", i)
			var e disableEntry
			if err := strict(raw, &e); err != nil {
				return c, fail(`%s must look like {"name": "closed-signal-channel", "reason": "why it is turned off"} (%s)`, where, jsonProblem(raw, err))
			}
			if blank(e.Name) {
				return c, fail(`%s needs a "name" (the detector's name, like "closed-signal-channel")`, where)
			}
			name := strings.TrimSpace(*e.Name)
			if blank(e.Reason) {
				return c, fail(`%s (%s) needs a "reason": a detector is turned off on purpose, and the reason is shown in every report`, where, name)
			}
			if seenDet[name] {
				return c, fail(`detector %q is disabled twice`, name)
			}
			seenDet[name] = true
			c.DisabledDetectors = append(c.DisabledDetectors, DisabledDetector{Name: name, Reason: *e.Reason})
		}
	}

	seenTrace := map[string]bool{}
	for i, raw := range p.ExplainedTraces {
		where := fmt.Sprintf("classification.explained_traces[%d]", i)
		var e explainedEntry
		if err := strict(raw, &e); err != nil {
			return c, fail(`%s must look like {"workflow": "OrderWorkflow", "steps_id": "t1:0123456789ab", "reason": "why this run takes a road PathKit does not draw"} (%s)`, where, jsonProblem(raw, err))
		}
		if blank(e.Workflow) {
			return c, fail(`%s needs a "workflow"`, where)
		}
		if blank(e.StepsID) || !stepsIDPattern.MatchString(*e.StepsID) {
			return c, fail(`%s needs a "steps_id" like "t1:0123456789ab" (the report prints it next to the trace that fits no path)`, where)
		}
		if blank(e.Reason) {
			return c, fail(`%s (%s %s) needs a "reason": accepting a trace that fits no path is a decision, and the reason is shown in every report`, where, strings.TrimSpace(*e.Workflow), *e.StepsID)
		}
		key := strings.TrimSpace(*e.Workflow) + "|" + *e.StepsID
		if seenTrace[key] {
			return c, fail(`%s %s is listed twice in classification.explained_traces`, strings.TrimSpace(*e.Workflow), *e.StepsID)
		}
		seenTrace[key] = true
		c.Explained = append(c.Explained, ExplainedTrace{Workflow: strings.TrimSpace(*e.Workflow), StepsID: *e.StepsID, Reason: *e.Reason})
	}

	if p.CI != nil {
		var e ciEntry
		if err := strict(p.CI, &e); err != nil {
			return c, fail(`classification.ci must look like {"baseline": "pathkit-baseline.json", "fail_under_testable": 80} (%s)`, jsonProblem(p.CI, err))
		}
		if e.Baseline != nil {
			if blank(e.Baseline) {
				return c, fail(`classification.ci.baseline must be a file path (like "pathkit-baseline.json"), not empty`)
			}
			c.CI.Baseline = *e.Baseline
		}
		if v := e.FailUnderTestable; v != nil {
			if *v < 0 || *v > 100 {
				return c, fail(`classification.ci.fail_under_testable must be a number from 0 to 100, got %v`, *v)
			}
			c.CI.FailUnderTestable = v
		}
	}

	names := map[string]bool{}
	for i, raw := range p.CustomLabels {
		where := fmt.Sprintf("classification.custom_labels[%d]", i)
		var e customEntry
		if err := strict(raw, &e); err != nil {
			return c, fail(`%s must look like {"name": "my-label", "definition": "what it means", "bucket": "cannot-be-tested"} (%s)`, where, jsonProblem(raw, err))
		}
		if blank(e.Name) || !labelPattern.MatchString(*e.Name) {
			return c, fail(`%s needs a "name" of lowercase words joined by - (like "my-label")`, where)
		}
		if names[*e.Name] {
			return c, fail(`custom label %q is defined twice`, *e.Name)
		}
		names[*e.Name] = true
		if blank(e.Definition) {
			return c, fail(`custom label %q needs a "definition"`, *e.Name)
		}
		ok := false
		if e.Bucket != nil {
			for _, b := range customBuckets {
				ok = ok || *e.Bucket == b
			}
		}
		if !ok {
			return c, fail(`custom label %q: "bucket" must be one of %s`, *e.Name, strings.Join(customBuckets, ", "))
		}
		c.Custom = append(c.Custom, CustomLabel{Name: *e.Name, Definition: *e.Definition, Bucket: *e.Bucket})
	}
	return c, nil
}

func overrideFrom(where string, workflow, stableID, reason *string, fail func(string, ...any) error) (PathOverride, error) {
	var o PathOverride
	if blank(workflow) {
		return o, fail(`%s needs a "workflow"`, where)
	}
	if blank(stableID) {
		return o, fail(`%s needs a "stable_id"`, where)
	}
	if !stableIDPattern.MatchString(*stableID) {
		if v, _, ok := strings.Cut(*stableID, ":"); ok && v != "s1" {
			return o, fail(`%s: stable_id %q uses version %q, but this PathKit writes "s1"; ids are listed by "pathkit coverage --json" (stableId)`, where, *stableID, v)
		}
		return o, fail(`%s: stable_id %q is not a stable id (expected "s1:" and 12 hex digits, like "s1:0123456789ab"); ids are listed by "pathkit coverage --json" (stableId)`, where, *stableID)
	}
	if blank(reason) {
		return o, fail(`%s (%s %s) needs a "reason"`, where, strings.TrimSpace(*workflow), *stableID)
	}
	o.Workflow, o.StableID, o.Reason = strings.TrimSpace(*workflow), *stableID, *reason
	return o, nil
}

func noDuplicate(seen map[string]string, o PathOverride, list string, fail func(string, ...any) error) error {
	key := o.Workflow + "|" + o.StableID
	if prev, ok := seen[key]; ok {
		if prev == list {
			return fail(`%s %s is listed twice in classification.paths.%s`, o.Workflow, o.StableID, list)
		}
		return fail(`%s %s is in both classification.paths.%s and classification.paths.%s; it can only be one`, o.Workflow, o.StableID, prev, list)
	}
	seen[key] = list
	return nil
}
