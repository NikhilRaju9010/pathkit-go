package advice

import (
	"strings"
	"testing"
)

// The tag is the riskiest thing a scenario does: cancel, then compensation,
// activity failure, continue-as-new, timers, signals, plain logic, old versions.
func TestTagIsTheRiskiestThing(t *testing.T) {
	for _, c := range []struct {
		name   string
		steps  []string
		defer_ bool
		want   string
	}{
		{"cancel beats failure", []string{"Charge (activity): failure", "failure kind: cancelled", "end: failed"}, false, TagCancel},
		{"a cancelled timer is a cancel", []string{"Sleep (timer): failure", "end: failed"}, false, TagCancel},
		{"cleanup after a failure that followed a success", []string{"Reserve (activity): success", "Charge (activity): failure", "Release (activity): success", "end: failed"}, false, TagCompensation},
		{"a failure before any success is not compensation", []string{"Charge (activity): failure", "Notify (activity): success", "end: failed"}, false, TagFailure},
		{"the compensation defer", []string{"Charge (activity): success", "end: completed"}, true, TagCompensation},
		{"activity failure", []string{"Charge (activity): failure", "end: failed"}, false, TagFailure},
		{"continue as new", []string{"Charge (activity): success", "continue-as-new", "end: continued-as-new"}, false, TagContinue},
		{"timeout", []string{"wait for timeout", "end: completed"}, false, TagTimer},
		{"signal", []string{`wait for signal "go"`, "end: completed"}, false, TagSignal},
		{"plain", []string{"Charge (activity): success", "end: completed"}, false, TagLogic},
		{"old version", []string{"version v1: old", "end: completed"}, false, TagOldVersion},
	} {
		if got := tagOf(c.steps, c.defer_); got != c.want {
			t.Errorf("%s: tag %q, want %q", c.name, got, c.want)
		}
	}
	for i := 1; i < len(tagOrder); i++ {
		if tagRank(tagOrder[i-1]) >= tagRank(tagOrder[i]) {
			t.Errorf("tag order is not strict at %s", tagOrder[i])
		}
	}
}

func TestTestNamesAreGoNames(t *testing.T) {
	got := testName("orders.OrderWorkflow", []string{"Save-Record (activity): failure", "failure kind: timeout", "cancel context"}, 3)
	if got != "TestOrder_SaveRecordFailsTimeoutCancelled" {
		t.Errorf("name %q", got)
	}
	if got := testName("a.Plain", []string{"end: completed"}, 7); got != "TestPlain_Path7" {
		t.Errorf("name %q", got)
	}
	for _, name := range []string{testName("x.Y", []string{`wait for signal "pause-now"`}, 1), testName("x.Y", nil, 1)} {
		if !strings.HasPrefix(name, "Test") || strings.ContainsAny(name, " -\"") {
			t.Errorf("not a Go test name: %q", name)
		}
	}
}

func TestHintReadsLikeAnInstruction(t *testing.T) {
	h := hintOf([]string{"Save (activity): failure", "failure kind: timeout", "cancel context", "end: failed"}, emptyPath(), nil)
	if h != "Make Save fail with a timeout, then cancel the workflow" {
		t.Errorf("hint %q", h)
	}
}
