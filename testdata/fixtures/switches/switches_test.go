package switches

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

func route(t *testing.T, weight int) (string, error) {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.RegisterActivity(Measure)
	env.ExecuteWorkflow(RouteWorkflow, weight)
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		return "", err
	}
	var lane string
	if err := env.GetWorkflowResult(&lane); err != nil {
		t.Fatal(err)
	}
	return lane, nil
}

func TestRouteHugeFallsThrough(t *testing.T) {
	if lane, err := route(t, 5000); err != nil || lane != "freight truck" {
		t.Fatalf("lane = %q, %v; want freight truck", lane, err)
	}
}

func TestRouteLarge(t *testing.T) {
	if lane, err := route(t, 500); err != nil || lane != "truck" {
		t.Fatalf("lane = %q, %v; want truck", lane, err)
	}
}

func TestRouteTiny(t *testing.T) {
	if lane, err := route(t, 1); err != nil || lane != "bike" {
		t.Fatalf("lane = %q, %v; want bike", lane, err)
	}
}

func TestRouteMediumHasNoLane(t *testing.T) {
	if _, err := route(t, 50); err == nil {
		t.Fatal("want an error for a medium parcel")
	}
}

func TestRouteMeasureFails(t *testing.T) {
	if _, err := route(t, -1); err == nil {
		t.Fatal("want an error for a negative weight")
	}
}

func ledger(t *testing.T, e Event) (int, error) {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(LedgerWorkflow, e)
	if err := env.GetWorkflowError(); err != nil {
		return 0, err
	}
	var n int
	if err := env.GetWorkflowResult(&n); err != nil {
		t.Fatal(err)
	}
	return n, nil
}

func TestLedgerRefund(t *testing.T) {
	if n, err := ledger(t, Event{"refund", 30}); err != nil || n != -30 {
		t.Fatalf("got %d, %v; want -30", n, err)
	}
}

func TestLedgerCharge(t *testing.T) {
	if n, err := ledger(t, Event{"charge", 30}); err != nil || n != 30 {
		t.Fatalf("got %d, %v; want 30", n, err)
	}
}

func TestLedgerUnknown(t *testing.T) {
	if _, err := ledger(t, Event{"gift", 30}); err == nil {
		t.Fatal("want an error for an unknown event")
	}
}
