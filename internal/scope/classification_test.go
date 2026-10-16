package scope

import (
	"path/filepath"
	"strings"
	"testing"
)

// The "classification" section is new. A config without it, and an old
// config with every older key, must keep working and mean "off".
func TestClassificationKey(t *testing.T) {
	tests := []struct {
		name, content string
		want          bool
	}{
		{"no section", `{}`, false},
		{"an old config with every older key", `{"packages": ["./..."], "traces": "t", "workflows": {"include": ["A"], "exclude": [{"name": "B", "reason": "r"}]}, "failUnder": 80, "html": true, "out": "o.txt", "json": false, "noColor": true, "allowStale": true}`, false},
		{"empty section", `{"classification": {}}`, false},
		{"enabled false", `{"classification": {"enabled": false}}`, false},
		{"enabled true", `{"classification": {"enabled": true}}`, true},
		{"enabled next to the old keys", `{"failUnder": 80, "classification": {"enabled": true}}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), FileName)
			write(t, path, tt.content)
			c, err := Read(path)
			if err != nil {
				t.Fatal(err)
			}
			if c.Classification.Enabled != tt.want {
				t.Errorf("ClassificationEnabled = %v, want %v", c.Classification.Enabled, tt.want)
			}
		})
	}
}

const okID = "s1:59aa014811bd"

// A full, valid section is read completely.
func TestClassificationSectionValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	write(t, path, `{
  "workflows": {"exclude": [{"name": "Legacy", "reason": "r"}]},
  "classification": {
    "enabled": true,
    "paths": {
      "mark_untestable": [
        {"workflow": "billing.SubscriptionWorkflow", "stable_id": "`+okID+`", "label": "manual", "reason": "input count is never 0", "added_by": "qa", "added_on": "2026-10-09"},
        {"workflow": "OrderWorkflow", "stable_id": "s1:000000000001", "reason": "no label given"}
      ],
      "mark_testable": [{"workflow": "OrderWorkflow", "stable_id": "s1:000000000002", "reason": "PathKit was wrong"}]
    },
    "rules": [{"id": "flag-checked-later", "workflow": "OrderWorkflow", "label": "set-then-test",
               "when": ["if a --true", "if b --false"], "impossible": true, "reason": "a flag set earlier is always checked later"}],
    "custom_labels": [{"name": "my-label", "definition": "what it means", "bucket": "needs-review"}],
    "unclassified_policy": "exclude",
    "ci": {"baseline": "ci/pathkit-baseline.json", "fail_under_testable": 87.5}
  }
}`)
	c, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	cl := c.Classification
	if !cl.Enabled || cl.Policy != "exclude" || len(cl.Overrides) != 3 || len(cl.Rules) != 1 || len(cl.Custom) != 1 {
		t.Fatalf("%+v", cl)
	}
	o := cl.Overrides[0]
	if !o.Untestable || o.Workflow != "billing.SubscriptionWorkflow" || o.StableID != okID || o.Label != "manual" || o.Reason != "input count is never 0" || o.AddedBy != "qa" || o.AddedOn != "2026-10-09" {
		t.Errorf("first override: %+v", o)
	}
	if cl.Overrides[1].Label != "" || !cl.Overrides[1].Untestable || cl.Overrides[2].Untestable {
		t.Errorf("label default and the testable list: %+v %+v", cl.Overrides[1], cl.Overrides[2])
	}
	if r := cl.Rules[0]; r.ID != "flag-checked-later" || len(r.When) != 2 || r.Label != "set-then-test" {
		t.Errorf("rule: %+v", r)
	}
	if cl.CI.Baseline != "ci/pathkit-baseline.json" || cl.CI.FailUnderTestable == nil || *cl.CI.FailUnderTestable != 87.5 {
		t.Errorf("ci: %+v", cl.CI)
	}
	if len(c.Exclude) != 1 {
		t.Error("the older keys must still be read next to the new section")
	}
}

// The default policy is the strict one.
func TestClassificationDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	write(t, path, `{"classification": {"enabled": true}}`)
	c, _ := Read(path)
	if c.Classification.Policy != "count_as_testable" || len(c.Classification.Overrides) != 0 || c.Classification.CI.Baseline != "" || c.Classification.CI.FailUnderTestable != nil {
		t.Errorf("%+v", c.Classification)
	}
}

// Every mistake in the section is a load error that says what is wrong;
// above all, every override and rule needs a reason.
func TestClassificationErrors(t *testing.T) {
	untestable := func(entry string) string {
		return `{"classification": {"paths": {"mark_untestable": [` + entry + `]}}}`
	}
	rule := func(fields string) string { return `{"classification": {"rules": [{` + fields + `}]}}` }
	okRule := `"id": "r1", "workflow": "W", "label": "set-then-test", "when": ["if a --true"], "impossible": true, "reason": "why"`
	tests := []struct{ name, content, want string }{
		{"an unknown key in the section", `{"classification": {"colour": 1}}`, `unknown key "colour"`},
		{"ci: an unknown key", `{"classification": {"ci": {"fail_on_needs_review": true}}}`, `classification.ci must look like`},
		{"ci: an empty baseline", `{"classification": {"ci": {"baseline": " "}}}`, `classification.ci.baseline must be a file path`},
		{"ci: a floor above 100", `{"classification": {"ci": {"fail_under_testable": 101}}}`, `fail_under_testable must be a number from 0 to 100, got 101`},
		{"ci: a floor as text", `{"classification": {"ci": {"fail_under_testable": "80"}}}`, `classification.ci must look like`},
		{"enabled as text", `{"classification": {"enabled": "yes"}}`, "has the wrong type"},
		{"section as a list", `{"classification": []}`, `"classification" has the wrong type`},
		{"bad policy", `{"classification": {"unclassified_policy": "ignore"}}`, `unclassified_policy must be "count_as_testable" or "exclude", got "ignore"`},

		{"detector disabled without a reason", `{"classification": {"detectors": {"disable": [{"name": "closed-signal-channel"}]}}}`, `classification.detectors.disable[0] (closed-signal-channel) needs a "reason"`},
		{"detector disabled with a blank reason", `{"classification": {"detectors": {"disable": [{"name": "closed-signal-channel", "reason": " "}]}}}`, `needs a "reason"`},
		{"detector disabled without a name", `{"classification": {"detectors": {"disable": [{"reason": "r"}]}}}`, `needs a "name"`},
		{"detector disabled twice", `{"classification": {"detectors": {"disable": [{"name": "a", "reason": "r"}, {"name": "a", "reason": "r"}]}}}`, `detector "a" is disabled twice`},
		{"an unknown key under detectors", `{"classification": {"detectors": {"enable": []}}}`, `unknown key "enable"`},
		{"override without a reason", untestable(`{"workflow": "W", "stable_id": "` + okID + `"}`), `classification.paths.mark_untestable[0] (W ` + okID + `) needs a "reason"`},
		{"override with a blank reason", untestable(`{"workflow": "W", "stable_id": "` + okID + `", "reason": "  "}`), `needs a "reason"`},
		{"override without a workflow", untestable(`{"stable_id": "` + okID + `", "reason": "r"}`), `needs a "workflow"`},
		{"override without a stable_id", untestable(`{"workflow": "W", "reason": "r"}`), `needs a "stable_id"`},
		{"an old-style id", untestable(`{"workflow": "W", "stable_id": "40dbc77a36", "reason": "r"}`), `is not a stable id (expected "s1:" and 12 hex digits`},
		{"a short id", untestable(`{"workflow": "W", "stable_id": "s1:abc", "reason": "r"}`), `is not a stable id`},
		{"an id from a newer version", untestable(`{"workflow": "W", "stable_id": "s2:59aa014811bd", "reason": "r"}`), `uses version "s2", but this PathKit writes "s1"`},
		{"an unknown key in an override", untestable(`{"workflow": "W", "stable_id": "` + okID + `", "reason": "r", "why": "x"}`), `unknown key "why"`},
		{"a bad label", untestable(`{"workflow": "W", "stable_id": "` + okID + `", "reason": "r", "label": "Not A Label"}`), `"label" must be lowercase words`},
		{"a bad date", untestable(`{"workflow": "W", "stable_id": "` + okID + `", "reason": "r", "added_on": "yesterday"}`), `"added_on" must be a date like 2026-10-09`},
		{"the same path twice", untestable(`{"workflow": "W", "stable_id": "` + okID + `", "reason": "r"}, {"workflow": "W", "stable_id": "` + okID + `", "reason": "r"}`), "is listed twice in classification.paths.mark_untestable"},
		{"in both lists", `{"classification": {"paths": {"mark_untestable": [{"workflow": "W", "stable_id": "` + okID + `", "reason": "r"}], "mark_testable": [{"workflow": "W", "stable_id": "` + okID + `", "reason": "r"}]}}}`, "is in both classification.paths.mark_untestable and classification.paths.mark_testable"},
		{"testable without a reason", `{"classification": {"paths": {"mark_testable": [{"workflow": "W", "stable_id": "` + okID + `"}]}}}`, `mark_testable[0] (W ` + okID + `) needs a "reason"`},
		{"a string instead of an object", untestable(`"x"`), "classification.paths.mark_untestable[0] must look like"},

		{"an empty without list", rule(okRule + `, "without": []`), `has an empty "without" list`},
		{"a blank without entry", rule(okRule + `, "without": [" "]`), `has an empty entry in "without"`},
		{"the same decision in when and without", rule(okRule + `, "without": ["if  a --true"]`), `lists "if  a --true" in both "when" and "without"`},
		{"without as text", rule(okRule + `, "without": "if b --true"`), "must look like"},
		{"explained trace without a reason", `{"classification": {"explained_traces": [{"workflow": "W", "steps_id": "t1:0123456789ab"}]}}`, `needs a "reason"`},
		{"explained trace with a blank reason", `{"classification": {"explained_traces": [{"workflow": "W", "steps_id": "t1:0123456789ab", "reason": " "}]}}`, `needs a "reason"`},
		{"explained trace without an id", `{"classification": {"explained_traces": [{"workflow": "W", "reason": "r"}]}}`, `needs a "steps_id"`},
		{"explained trace with a stable id", `{"classification": {"explained_traces": [{"workflow": "W", "steps_id": "s1:0123456789ab", "reason": "r"}]}}`, `needs a "steps_id" like "t1:`},
		{"explained trace left at TODO", `{"classification": {"explained_traces": [{"workflow": "W", "steps_id": "t1:0123456789ab", "reason": "TODO"}]}}`, `still has the reason "TODO"`},
		{"explained trace without a workflow", `{"classification": {"explained_traces": [{"steps_id": "t1:0123456789ab", "reason": "r"}]}}`, `needs a "workflow"`},
		{"explained trace twice", `{"classification": {"explained_traces": [{"workflow": "W", "steps_id": "t1:0123456789ab", "reason": "r"}, {"workflow": "W", "steps_id": "t1:0123456789ab", "reason": "r"}]}}`, "is listed twice in classification.explained_traces"},
		{"rule without a reason", rule(`"id": "r1", "workflow": "W", "label": "set-then-test", "when": ["a"], "impossible": true`), `rule "r1" needs a "reason"`},
		{"rule without an id", rule(`"workflow": "W", "label": "x", "when": ["a"], "impossible": true, "reason": "r"`), `needs an "id"`},
		{"rule with a bad id", rule(`"id": "no spaces", "workflow": "W", "label": "x", "when": ["a"], "impossible": true, "reason": "r"`), `needs an "id"`},
		{"rule without a workflow", rule(`"id": "r1", "label": "x", "when": ["a"], "impossible": true, "reason": "r"`), `rule "r1" needs a "workflow"`},
		{"rule without a label", rule(`"id": "r1", "workflow": "W", "when": ["a"], "impossible": true, "reason": "r"`), `rule "r1" needs a "label"`},
		{"rule without when", rule(`"id": "r1", "workflow": "W", "label": "set-then-test", "impossible": true, "reason": "r"`), `needs a non-empty "when" list`},
		{"rule with an empty when", rule(`"id": "r1", "workflow": "W", "label": "set-then-test", "when": [], "impossible": true, "reason": "r"`), `needs a non-empty "when" list`},
		{"rule with a blank decision", rule(`"id": "r1", "workflow": "W", "label": "set-then-test", "when": [" "], "impossible": true, "reason": "r"`), `has an empty entry in "when"`},
		{"rule not impossible", rule(`"id": "r1", "workflow": "W", "label": "set-then-test", "when": ["a"], "impossible": false, "reason": "r"`), `"impossible" must be true`},
		{"rule without impossible", rule(`"id": "r1", "workflow": "W", "label": "set-then-test", "when": ["a"], "reason": "r"`), `"impossible" must be true`},
		{"two rules with one id", rule(okRule + `}, {` + okRule), `rule id "r1" is used twice`},

		{"custom label without a definition", `{"classification": {"custom_labels": [{"name": "my-label", "bucket": "cannot-be-tested"}]}}`, `custom label "my-label" needs a "definition"`},
		{"custom label with a bad name", `{"classification": {"custom_labels": [{"name": "My Label", "definition": "d", "bucket": "cannot-be-tested"}]}}`, `needs a "name" of lowercase words`},
		{"custom label in the testable bucket", `{"classification": {"custom_labels": [{"name": "my-label", "definition": "d", "bucket": "testable"}]}}`, `"bucket" must be one of cannot-be-tested, needs-special-test, needs-review`},
		{"custom label without a bucket", `{"classification": {"custom_labels": [{"name": "my-label", "definition": "d"}]}}`, `"bucket" must be one of`},
		{"a custom label twice", `{"classification": {"custom_labels": [{"name": "a-b", "definition": "d", "bucket": "needs-review"}, {"name": "a-b", "definition": "d", "bucket": "needs-review"}]}}`, `custom label "a-b" is defined twice`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), FileName)
			write(t, path, tt.content)
			_, err := Read(path)
			if err == nil || !strings.HasPrefix(err.Error(), path+": ") || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v\nwant it to start with %q and contain %q", err, path+": ", tt.want)
			}
		})
	}
}

func TestDisabledDetectorsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	write(t, path, `{"classification": {"enabled": true, "detectors": {"disable": [{"name": "closed-signal-channel", "reason": "we close them in tests"}]}}}`)
	c, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	got := c.Classification.DisabledDetectors
	if len(got) != 1 || got[0].Name != "closed-signal-channel" || got[0].Reason != "we close them in tests" {
		t.Errorf("DisabledDetectors = %+v", got)
	}
}

// "without" and "explained_traces" are read completely.
func TestWithoutAndExplainedTracesAreRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	write(t, path, `{"classification": {"enabled": true,
  "rules": [{"id": "r", "workflow": "W", "label": "same-list-loops", "when": ["for range a --exit"], "without": ["for range a --iterate", "any: if x --true"], "impossible": true, "reason": "why"}],
  "explained_traces": [{"workflow": "W", "steps_id": "t1:0123456789ab", "reason": "helper returns early"}]}}`)
	c, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	r := c.Classification.Rules[0]
	if len(r.When) != 1 || len(r.Without) != 2 || r.Without[1] != "any: if x --true" {
		t.Errorf("rule: %+v", r)
	}
	if x := c.Classification.Explained; len(x) != 1 || x[0].Workflow != "W" || x[0].StepsID != "t1:0123456789ab" || x[0].Reason != "helper returns early" {
		t.Errorf("explained: %+v", x)
	}
}

// classification.engine.enabled turns the fact engine off; the default is on.
func TestEngineEnabledRead(t *testing.T) {
	read := func(body string) (Classification, error) {
		path := filepath.Join(t.TempDir(), FileName)
		write(t, path, body)
		c, err := Read(path)
		if err != nil {
			return Classification{}, err
		}
		return c.Classification, nil
	}
	if c, err := read(`{"classification": {"enabled": true}}`); err != nil || c.EngineOff {
		t.Errorf("default: off=%v err=%v", c.EngineOff, err)
	}
	if c, err := read(`{"classification": {"enabled": true, "engine": {"enabled": false}}}`); err != nil || !c.EngineOff {
		t.Errorf("disabled: off=%v err=%v", c.EngineOff, err)
	}
	if c, err := read(`{"classification": {"enabled": true, "engine": {"enabled": true}}}`); err != nil || c.EngineOff {
		t.Errorf("enabled: off=%v err=%v", c.EngineOff, err)
	}
	if _, err := read(`{"classification": {"enabled": true, "engine": {"enable": false}}}`); err == nil {
		t.Error("a misspelled key under engine must be refused")
	}
}

// classification.assumptions.default_json_converter is "auto" (the default)
// or "off"; anything else, and any other key under assumptions, is refused.
func TestAssumptionsRead(t *testing.T) {
	read := func(body string) (string, error) {
		path := filepath.Join(t.TempDir(), FileName)
		write(t, path, body)
		c, err := Read(path)
		if err != nil {
			return "", err
		}
		return c.Classification.JSONConverter, nil
	}
	if v, err := read(`{"classification": {"enabled": true}}`); err != nil || v != "auto" {
		t.Errorf("default: %q %v", v, err)
	}
	if v, err := read(`{"classification": {"enabled": true, "assumptions": {"default_json_converter": "off"}}}`); err != nil || v != "off" {
		t.Errorf("off: %q %v", v, err)
	}
	for _, bad := range []string{
		`{"classification": {"enabled": true, "assumptions": {"default_json_converter": "on"}}}`,
		`{"classification": {"enabled": true, "assumptions": {"default_json": "off"}}}`,
	} {
		if _, err := read(bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}
