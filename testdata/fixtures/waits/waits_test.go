package waits

import (
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"
)

// signalAt sends one signal at the given point in the test's fake time.
type signalAt struct {
	after time.Duration
	name  string
	value any
}

// run executes a workflow with the given signals and returns its result.
func run[T any](t *testing.T, wf any, arg any, signals ...signalAt) T {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.RegisterActivity(Lookup)
	for _, sig := range signals {
		env.RegisterDelayedCallback(func() { env.SignalWorkflow(sig.name, sig.value) }, sig.after)
	}
	var args []any
	if arg != nil {
		args = append(args, arg)
	}
	env.ExecuteWorkflow(wf, args...)
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var result T
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func want[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Selector: the signal wins, then the timer wins.
func TestRaceSignalWins(t *testing.T) {
	want(t, run[string](t, RaceWorkflow, time.Hour, signalAt{time.Minute, "answer", "yes"}), "yes")
}

func TestRaceTimerWins(t *testing.T) {
	want(t, run[string](t, RaceWorkflow, time.Hour), "no answer")
}

// Selector with AddDefault: a signal is waiting, or nothing is.
func TestPendingSignalWaiting(t *testing.T) {
	want(t, run[string](t, PendingWorkflow, nil, signalAt{time.Minute, "note", "hi"}), "hi")
}

func TestPendingNothing(t *testing.T) {
	want(t, run[string](t, PendingWorkflow, nil), "nothing")
}

// Selector with an activity future: the activity wins.
func TestLookupActivityWins(t *testing.T) {
	want(t, run[string](t, LookupWorkflow, "k"), "value of k")
}

// Select inside a loop: signal, nothing, signal, over three rounds.
func TestCollectThreeRounds(t *testing.T) {
	got := run[int](t, CollectWorkflow, 3,
		signalAt{30 * time.Minute, "item", "a"}, signalAt{150 * time.Minute, "item", "b"})
	want(t, got, 2)
}

// AwaitWithTimeout: the signal arrives, then the wait times out.
func TestApproveArrives(t *testing.T) {
	want(t, run[string](t, ApproveWorkflow, 48*time.Hour, signalAt{time.Hour, "approve", "ok"}), "approved")
}

func TestApproveTimesOut(t *testing.T) {
	want(t, run[string](t, ApproveWorkflow, 48*time.Hour), "expired")
}

// ReceiveWithTimeout: the signal arrives, then the wait times out.
func TestReadArrives(t *testing.T) {
	want(t, run[string](t, ReadWorkflow, time.Hour, signalAt{time.Minute, "data", "d1"}), "d1")
}

func TestReadTimesOut(t *testing.T) {
	want(t, run[string](t, ReadWorkflow, time.Hour), "none")
}

// ReceiveAsync: a signal is waiting, or nothing is.
func TestPeekFindsSignal(t *testing.T) {
	want(t, run[string](t, PeekWorkflow, nil, signalAt{time.Minute, "data", "d2"}), "d2")
}

func TestPeekFindsNothing(t *testing.T) {
	want(t, run[string](t, PeekWorkflow, nil), "none")
}
