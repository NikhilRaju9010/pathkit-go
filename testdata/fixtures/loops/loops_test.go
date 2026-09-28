package loops

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

// run executes one workflow and returns its error; result receives the
// result when there is no error.
func run(t *testing.T, wf any, arg any, result any) error {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.RegisterActivity(Step)
	env.RegisterActivity(Check)
	env.ExecuteWorkflow(wf, arg)
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		return err
	}
	if err := env.GetWorkflowResult(result); err != nil {
		t.Fatal(err)
	}
	return nil
}

func wantInt(t *testing.T, wf any, arg any, want int) {
	t.Helper()
	var got int
	if err := run(t, wf, arg, &got); err != nil || got != want {
		t.Fatalf("got %d, %v; want %d", got, err, want)
	}
}

func TestNestedNoTrips(t *testing.T) {
	wantInt(t, NestedWorkflow, Grid{Outer: 0, Inner: 5, FailAt: -1}, 0)
}

func TestNestedManyTrips(t *testing.T) {
	wantInt(t, NestedWorkflow, Grid{Outer: 3, Inner: 2, FailAt: -1}, 6)
}

func TestNestedFailsLate(t *testing.T) {
	var n int
	if err := run(t, NestedWorkflow, Grid{Outer: 3, Inner: 3, FailAt: 21}, &n); err == nil {
		t.Fatal("want the step for (2, 1) to fail")
	}
}

func TestNestedInnerEmpty(t *testing.T) {
	wantInt(t, NestedWorkflow, Grid{Outer: 2, Inner: 0, FailAt: -1}, 0)
}

func TestScanFindsAfterSkips(t *testing.T) { wantInt(t, ScanWorkflow, []int{0, 5, 7, 14}, 2) }

func TestScanNothing(t *testing.T) { wantInt(t, ScanWorkflow, []int{3, 5}, -1) }

func TestScanEmpty(t *testing.T) { wantInt(t, ScanWorkflow, []int{}, -1) }

func TestScanOnlyZeros(t *testing.T) { wantInt(t, ScanWorkflow, []int{0, 0}, -1) }

func TestScanCheckFails(t *testing.T) {
	var n int
	if err := run(t, ScanWorkflow, []int{4, -1}, &n); err == nil {
		t.Fatal("want Check(-1) to fail")
	}
}

func TestWaitThreeHours(t *testing.T) { wantInt(t, WaitWorkflow, 3, 3) }

func TestGridSkipsRowThenFinishes(t *testing.T) {
	wantInt(t, GridWorkflow, [][]int{{1, -1, 5}, {2}}, 2)
}

func TestGridStopsAtZero(t *testing.T) { wantInt(t, GridWorkflow, [][]int{{3}, {0, 4}}, 1) }

func TestSumManyItems(t *testing.T) {
	var s string
	if err := run(t, SumWorkflow, []int{50, 60, 70}, &s); err != nil || s != "big" {
		t.Fatalf("got %q, %v; want big", s, err)
	}
}

func TestSumNoItems(t *testing.T) {
	var s string
	if err := run(t, SumWorkflow, []int{}, &s); err != nil || s != "small" {
		t.Fatalf("got %q, %v; want small", s, err)
	}
}

func TestCountBigMixed(t *testing.T) { wantInt(t, CountBigWorkflow, []int{5, 20, 3}, 1) }
