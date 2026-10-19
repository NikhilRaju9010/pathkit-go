package coverage

import (
	"math/big"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

// The rules, in order: handlers for an event loop (not tested here: it needs a
// graph), paths under the cap, scenarios when counted and few enough to list,
// else branches; a forced choice wins; a number with nothing to measure is
// not "met".
func TestMainNumberChoice(t *testing.T) {
	under := WorkflowResult{Name: "w.Under", Breakdown: Breakdown{Testable: BucketCount{Paths: 4, Covered: 3}}, AllBranches: 10, AllBranchesTaken: 5}
	capped := WorkflowResult{Name: "w.Capped", Capped: true, AllBranches: 10, AllBranchesTaken: 9,
		Scenarios: ScenarioResult{Measured: true, Count: big.NewInt(5), Covered: 4, Testable: 5, Unchecked: new(big.Int)}}
	many := capped
	many.Name, many.Scenarios.Count = "w.Many", big.NewInt(900)
	unmeasured := WorkflowResult{Name: "w.Un", Capped: true, AllBranches: 8, AllBranchesTaken: 2, Scenarios: ScenarioResult{Why: "has places PathKit could not read", Count: new(big.Int), Unchecked: new(big.Int)}}
	empty := WorkflowResult{Name: "w.Empty"}
	opt := MainOptions{Targets: scope.NewTargets(80, nil)}
	for _, c := range []struct {
		w       WorkflowResult
		kind    string
		cov, to int
		met     bool
		measure bool
	}{
		{under, MainPath, 3, 4, false, true}, // 75% < 80%
		{capped, MainScenario, 4, 5, true, true},
		{many, MainBranch, 9, 10, true, true},
		{unmeasured, MainBranch, 2, 8, false, true},
		{empty, MainPath, 0, 0, false, false},
	} {
		m := mainNumber(c.w, nil, HandlerStats{}, classify.CountAsTestable, opt, 500)
		if m.Kind != c.kind || m.Covered != c.cov || m.Total != c.to || m.Met != c.met || m.Measured != c.measure || m.Why == "" {
			t.Errorf("%s: %+v, want %s %d/%d met=%v measured=%v", c.w.Name, m, c.kind, c.cov, c.to, c.met, c.measure)
		}
	}
	forced := mainNumber(capped, nil, HandlerStats{}, classify.CountAsTestable, MainOptions{Targets: opt.Targets, Force: []scope.MainForce{{Workflow: "w.Capped", Kind: MainBranch, Reason: "decisions matter"}}}, 500)
	if forced.Kind != MainBranch || !forced.Forced || forced.ForcedReason != "decisions matter" {
		t.Errorf("forced: %+v", forced)
	}
	// a per-kind target
	per := mainNumber(under, nil, HandlerStats{}, classify.CountAsTestable, MainOptions{Targets: scope.NewTargets(80, map[string]float64{"path": 70})}, 500)
	if !per.Met || per.Target != 70 {
		t.Errorf("per-kind target: %+v", per)
	}
}
