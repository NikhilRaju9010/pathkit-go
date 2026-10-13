package classify

import (
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// Every label PathKit can print has a definition, and every definition is
// complete. A label with no definition would print something nobody can look up.
func TestDefinitionsAreComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range Definitions() {
		key := string(d.Kind) + ":" + d.Name
		if seen[key] {
			t.Errorf("%s is defined twice", key)
		}
		seen[key] = true
		for field, v := range map[string]string{"Meaning": d.Meaning, "Why": d.Why, "Example": d.Example,
			"PossibleInProduction": d.PossibleInProduction, "WhatToDo": d.WhatToDo} {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s has an empty %s", key, field)
			}
		}
		if d.Kind == KindLabel && d.Bucket != "" {
			if _, ok := Lookup(KindBucket, string(d.Bucket)); !ok {
				t.Errorf("label %s puts paths in bucket %q, which has no definition", d.Name, d.Bucket)
			}
		}
	}
}

func TestEveryBucketIsDefined(t *testing.T) {
	for _, b := range Buckets {
		if _, ok := Lookup(KindBucket, string(b)); !ok {
			t.Errorf("bucket %q has no definition", b)
		}
	}
	if len(Buckets) != 4 {
		t.Errorf("%d buckets, want 4", len(Buckets))
	}
}

// Every label in definitions.go, and the three comments.
func TestPlanLabelsAndCommentsAreDefined(t *testing.T) {
	labels := []string{"contradictory", "constant-condition", "dead-code", "either-or-wait", "set-then-test", "depends-on-missing",
		"same-list-loops", "loop-minimum", "impossible-loop-exit",
		"closed-signal-channel", "rejected-update-invisible", "side-effect-error", "after-continue-as-new", "update-handler-continue-as-new",
		"version-gate-old-branch", "history-limit", "workflow-task-failure", "unfinished-handlers",
		"model-limit", "concurrency-order", "manual", "needs-review"}
	for _, l := range labels {
		if _, ok := Lookup(KindLabel, l); !ok {
			t.Errorf("label %q has no definition", l)
		}
	}
	for _, c := range []string{"//pathkit:ignore", "//pathkit:branch", "//pathkit:untestable"} {
		if _, ok := Lookup(KindComment, c); !ok {
			t.Errorf("comment %q has no definition", c)
		}
	}
}

// The two comments must not be confused: each definition names the other.
func TestIgnoreAndUntestableAreToldApart(t *testing.T) {
	ig, _ := Lookup(KindComment, "//pathkit:ignore")
	un, _ := Lookup(KindComment, "//pathkit:untestable")
	if !strings.Contains(ig.Meaning, "NOT the same as //pathkit:untestable") {
		t.Errorf("//pathkit:ignore does not say it differs from //pathkit:untestable: %q", ig.Meaning)
	}
	if !strings.Contains(un.Meaning, "NOT the same as //pathkit:ignore") {
		t.Errorf("//pathkit:untestable does not say it differs from //pathkit:ignore: %q", un.Meaning)
	}
}

func TestMustLabelPanicsOnUnknown(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustLabel of an undefined label did not panic")
		}
	}()
	MustLabel("not-a-label")
}

// Without a config every path is testable, from the
// automatic source, with a plain explanation and no label.
func TestDefaultClassifierIsTestable(t *testing.T) {
	c := Classify("w", model.Path{})
	if c.Bucket != Testable || c.Label != "" || c.Source != SourceAuto || c.Confidence != Certain || !c.PossibleInProduction || c.RuleID != "" || c.Explanation == "" {
		t.Errorf("Classify = %+v", c)
	}
}

// A label's Confidence is certain or likely, nothing else; a bucket or a
// comment has none. Who decides is a separate field.
func TestDefinitionConfidenceIsCertainOrLikely(t *testing.T) {
	for _, d := range Definitions() {
		switch d.Kind {
		case KindLabel:
			if d.Confidence != Certain && d.Confidence != Likely {
				t.Errorf("label %s: Confidence = %q, want %q or %q", d.Name, d.Confidence, Certain, Likely)
			}
			if d.DecidedBy != "detector" && d.DecidedBy != "config rule" && d.DecidedBy != "person" {
				t.Errorf("label %s: DecidedBy = %q", d.Name, d.DecidedBy)
			}
		default:
			if d.Confidence != "" {
				t.Errorf("%s %s: Confidence = %q, want none", d.Kind, d.Name, d.Confidence)
			}
		}
	}
}
