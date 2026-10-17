package helperrun

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

func run(t *testing.T, wf any, failing []string, args ...any) error {
	t.Helper()
	SetFail(failing...)
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.RegisterActivity(Charge)
	env.RegisterActivity(Ship)
	env.RegisterActivity(Notify)
	env.RegisterActivity(Item)
	env.ExecuteWorkflow(wf, args...)
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	return env.GetWorkflowError()
}

func TestOneHelperOK(t *testing.T)   { _ = run(t, OneHelper, nil, 5) }
func TestOneHelperFail(t *testing.T) { _ = run(t, OneHelper, []string{"Charge"}, 5) }

func TestTwoCallsOK(t *testing.T) { _ = run(t, TwoCalls, nil) }
func TestTwoCallsFirstFails(t *testing.T) {
	_ = run(t, TwoCalls, []string{"Charge"})
}

func TestBranchExpress(t *testing.T)      { _ = run(t, BranchHelper, nil, true) }
func TestBranchPlain(t *testing.T)        { _ = run(t, BranchHelper, nil, false) }
func TestBranchPlainFails(t *testing.T)   { _ = run(t, BranchHelper, []string{"Ship"}, false) }
func TestBranchExpressFails(t *testing.T) { _ = run(t, BranchHelper, []string{"Ship"}, true) }

func TestThreeDeepSkips(t *testing.T) { _ = run(t, ThreeDeep, nil, 0) }
func TestThreeDeepItem(t *testing.T)  { _ = run(t, ThreeDeep, nil, 1) }
func TestThreeDeepItemFails(t *testing.T) {
	_ = run(t, ThreeDeep, []string{"Item"}, 1)
}

func TestMethodOpen(t *testing.T)   { _ = run(t, MethodHelper, nil, true) }
func TestMethodClosed(t *testing.T) { _ = run(t, MethodHelper, nil, false) }

func TestClosureLoud(t *testing.T)  { _ = run(t, ClosureHelper, nil, true) }
func TestClosureQuiet(t *testing.T) { _ = run(t, ClosureHelper, nil, false) }

func TestLoopHelperTwice(t *testing.T) { _ = run(t, LoopHelper, nil, []string{"a", "b"}) }
func TestLoopHelperNone(t *testing.T)  { _ = run(t, LoopHelper, nil, []string{}) }
func TestLoopHelperFails(t *testing.T) { _ = run(t, LoopHelper, []string{"Item"}, []string{"a"}) }

func TestInLoopTwice(t *testing.T) { _ = run(t, InLoop, nil, []string{"a", "b"}) }
func TestInLoopFails(t *testing.T) { _ = run(t, InLoop, []string{"Charge"}, []string{"a"}) }

func TestPureOK(t *testing.T) { _ = run(t, PureHelper, nil, 1) }

func TestManyAllSucceed(t *testing.T) { _ = run(t, Many, nil) }
func TestManyAllFail(t *testing.T)    { _ = run(t, Many, []string{"Charge"}) }
