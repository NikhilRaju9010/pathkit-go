package coverage

import (
	"math/big"
	"testing"
)

func floorResult() Result {
	small := WorkflowResult{Name: "w.Small"}
	small.Breakdown.Testable = BucketCount{Paths: 4, Covered: 3}
	big1 := WorkflowResult{Name: "w.Big", Capped: true, Exact: big.NewInt(9999), AllBranches: 10, AllBranchesTaken: 4,
		Stages: []StageResult{
			{Paths: make([]StagePathResult, 5), CannotBeTested: 1, Covered: 2},
			{Big: true, Paths: make([]StagePathResult, 9), Covered: 9},
		}}
	// Listed paths of the capped workflow must not count.
	big1.Breakdown.Testable = BucketCount{Paths: 2000, Covered: 0}
	return Result{Workflows: []WorkflowResult{small, big1}}
}

func TestTestableFloorUsesStagesForCappedWorkflows(t *testing.T) {
	c, n, parts := TestableFloor(floorResult(), "stage")
	if c != 5 || n != 8 { // 3/4 paths + 2/4 stage paths; the too-big stage is left out
		t.Fatalf("got %d/%d", c, n)
	}
	if len(parts) != 2 || parts[1].Basis != "stage" || parts[1].Workflow != "w.Big" {
		t.Fatalf("parts: %+v", parts)
	}
}

func TestTestableFloorBranchBasis(t *testing.T) {
	if c, n, _ := TestableFloor(floorResult(), "branch"); c != 7 || n != 14 {
		t.Fatalf("got %d/%d", c, n)
	}
}

// Near miss: no workflow over the cap, the number is the plain testable one.
func TestTestableFloorNoCappedWorkflow(t *testing.T) {
	res := floorResult()
	res.Workflows = res.Workflows[:1]
	c, n, parts := TestableFloor(res, "stage")
	if c != 3 || n != 4 || len(parts) != 1 {
		t.Fatalf("got %d/%d %+v", c, n, parts)
	}
}
