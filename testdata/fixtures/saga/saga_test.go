package saga

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

// book runs the saga and returns how often it compensated, and its error.
func book(t *testing.T, trip string) (int32, error) {
	t.Helper()
	Cancellations.Store(0)
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.RegisterActivity(Reserve)
	env.RegisterActivity(Pay)
	env.RegisterActivity(CancelReservation)
	env.ExecuteWorkflow(BookTripWorkflow, trip)
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	return Cancellations.Load(), env.GetWorkflowError()
}

// Fails before the defer is registered: nothing to compensate.
func TestBookTripFull(t *testing.T) {
	if n, err := book(t, "full"); err == nil || n != 0 {
		t.Fatalf("err = %v, cancellations = %d; want an error and 0", err, n)
	}
}

// Fails after the defer: the compensation runs.
func TestBookTripPaymentFails(t *testing.T) {
	if n, err := book(t, "broke"); err == nil || n != 1 {
		t.Fatalf("err = %v, cancellations = %d; want an error and 1", err, n)
	}
}

// Succeeds: the defer is registered, but its inner if skips.
func TestBookTripSucceeds(t *testing.T) {
	if n, err := book(t, "paris"); err != nil || n != 0 {
		t.Fatalf("err = %v, cancellations = %d; want no error and 0", err, n)
	}
}
