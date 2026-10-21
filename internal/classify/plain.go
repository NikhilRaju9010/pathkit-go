package classify

// Plain names (milestone M11, R1). The reports for people use these words;
// the technical names stay in tooltips, in the JSON and with --technical.

// PlainBucket is a bucket's name for people.
func PlainBucket(b Bucket) string {
	switch b {
	case Testable:
		return "Can be tested"
	case CannotBeTested:
		return "Can't happen"
	case NeedsSpecialTest:
		return "Needs a special test"
	case NeedsReview:
		return "Needs a person to check"
	}
	return string(b)
}

// plainLabels gives every built-in label a plain name: a short sentence.
var plainLabels = map[string]string{
	"contradictory":                  "Two checks of the same value cannot both come out this way",
	"constant-condition":             "This check always has the same answer",
	"value-flow":                     "A value set earlier makes this impossible",
	"dead-code":                      "This code can never run",
	"either-or-wait":                 "Only one of two waits can end first",
	"set-then-test":                  "A value is set and then checked, so this side never happens",
	"depends-on-missing":             "This check only makes sense if another one holds",
	"possible-correlation":           "The same value is checked twice and may have changed in between",
	"impossible-type":                "This value can never be this type",
	"impossible-nil":                 "This value can never be nil",
	"unknown-input-type":             "PathKit cannot tell which types this value can hold",
	"likely-defect":                  "Possible bug",
	"same-list-loops":                "Two loops over the same list run the same number of times",
	"loop-minimum":                   "This loop always runs at least once",
	"impossible-loop-exit":           "The loop cannot end this way",
	"closed-signal-channel":          "Temporal never closes a signal channel",
	"rejected-update-invisible":      "Rejected updates never reach the workflow",
	"side-effect-error":              "A side effect that fails never gives an error back",
	"after-continue-as-new":          "Nothing runs after continue-as-new",
	"update-handler-continue-as-new": "Continue-as-new is not supported in an update handler",
	"version-gate-old-branch":        "The old side of a version check (needs a replay of old history)",
	"history-limit":                  "Only happens when the history is very large",
	"workflow-task-failure":          "A panic or a non-determinism error",
	"unfinished-handlers":            "The workflow ends while handlers are still running",
	"model-limit":                    "PathKit cannot draw this behaviour",
	"concurrency-order":              "Depends on the order of events between coroutines",
	"manual":                         "Marked by a person",
	"known-defect":                   "Blocked by a known bug",
	"needs-review":                   "Needs a person to check",
}

// PlainLabel is a label's name for people: a plain sentence. A label of the
// config's own (custom_labels) keeps its own name.
func PlainLabel(label string) string {
	if s, ok := plainLabels[label]; ok {
		return s
	}
	return label
}
