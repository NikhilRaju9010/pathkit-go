package model_test

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// workflowsIn loads a folder or pattern once per test binary and returns
// its workflows by name.
var cache sync.Map

func workflowsIn(t *testing.T, arg string) map[string]discover.Workflow {
	t.Helper()
	if v, ok := cache.Load(arg); ok {
		return v.(map[string]discover.Workflow)
	}
	res, err := load.Load(arg)
	if err != nil {
		t.Fatalf("load %s: %v", arg, err)
	}
	m := map[string]discover.Workflow{}
	for _, wf := range discover.Find(res.Packages) {
		m[wf.Name] = wf
	}
	cache.Store(arg, m)
	return m
}

func buildRule(t *testing.T, name string) (*model.Graph, error) {
	t.Helper()
	wf, ok := workflowsIn(t, "../../testdata/fixtures/rules")["rules."+name]
	if !ok {
		t.Fatalf("fixture workflow rules.%s not found", name)
	}
	return model.Build(wf)
}

// pathKeys renders each path as "J1.true J2.failure|failed" in listing order.
func pathKeys(g *model.Graph) []string {
	var out []string
	for _, p := range g.Paths(model.DefaultMaxPaths).List {
		out = append(out, p.Key())
	}
	return out
}

func TestRules(t *testing.T) {
	tests := []struct {
		workflow string
		want     []string
	}{
		{"PlainIfElse", []string{"J1.true|completed", "J1.false|completed"}},
		{"IfNoElse", []string{"J1.true|completed", "J1.false|completed"}},
		{"ElseIfChain", []string{"J1.true|completed", "J1.false J2.true|completed", "J1.false J2.false|completed"}},
		{"IfWithInit", []string{"J1.true|completed", "J1.false|completed"}},
		{"Sequential", []string{"J1.true J2.true|completed", "J1.true J2.false|completed", "J1.false J2.true|completed", "J1.false J2.false|completed"}},
		{"CompoundCondition", []string{"J1.true|", "J1.false|completed"}},
		{"Panics", []string{"J1.false|completed"}},
		{"NoBranches", []string{"|completed"}},

		{"ActivityErr", []string{"J1.failure|failed", "J1.success|completed"}},
		{"NilOnLeft", []string{"J1.failure|failed", "J1.success|completed"}},
		{"EqNilTemporal", []string{"J1.failure|completed", "J1.success|completed"}},
		{"ChildErr", []string{"J1.failure|failed", "J1.success|completed"}},
		{"SleepAwaitErr", []string{"J1.failure|failed", "J1.success J2.failure|failed", "J1.success J2.success|completed"}},
		{"FutureVariable", []string{"J1.failure|failed", "J1.success|completed"}},
		{"TransparentPlainErr", []string{"|completed"}},
		{"TransparentEqNil", []string{"J1.true|completed", "J1.false|completed"}}, // M1 finding 2
		{"QueryHandlerErr", []string{"|completed"}},                               // M1 finding 1
		{"NearestAssignment", []string{"J1.failure|failed", "J1.success|completed"}},

		{"IgnoredErrCheck", []string{"|completed"}},
		{"IgnoredPlainIf", []string{"|completed"}},
		{"ForcedBranch", []string{"J1.failure|failed", "J1.success|completed"}},

		{"EndKinds", []string{
			"J1.true|completed",
			"J1.false J2.true|continued-as-new",
			"J1.false J2.false J3.true|failed",
			"J1.false J2.false J3.false J4.true|failed",
			"J1.false J2.false J3.false J4.false J5.true|failed",
			"J1.false J2.false J3.false J4.false J5.false|",
		}},
		{"NamedBare", []string{"|"}},
		{"UncheckedVar", []string{"|"}},
		{"ReturnsCall", []string{"J1.true|", "J1.false|completed"}},
		{"PlainDefer", []string{"|completed"}},

		{"UsesSwitch", []string{`J1.case "a"|completed`, "J1.default|completed"}},
		{"UsesTypeSwitch", []string{"J1.case int|completed", "J1.default|completed"}},
		{"SwitchWithDefault", []string{`J1.case "a", "b"|completed`, "J1.default|failed", `J1.case "c"|completed`}},
		{"TaglessSwitchWithInit", []string{
			"J1.case m > 10|completed",
			"J1.case m > 4 J2.true|completed",
			"J1.case m > 4 J2.false|completed",
			"J1.default|completed",
		}},
		{"TypeSwitchAssign", []string{"J1.case nil|completed", "J1.case string, int|completed", "J1.default|failed"}},
		{"Fallthrough", []string{"J1.case n > 10|completed", "J1.case n > 5|completed", "J1.default|completed"}},
		{"OnlyDefault", []string{"|completed"}},
		{"BreakInSwitch", []string{`J1.case "skip" J2.true|completed`, `J1.case "skip" J2.false|completed`, "J1.default|completed"}},
		{"SwitchOnActivityResult", []string{
			"J1.failure|failed",
			`J1.success J2.case "ok"|completed`,
			`J1.success J2.case "retry"|failed`,
			"J1.success J2.default|completed",
		}},
		{"DuplicateCases", []string{"J1.case x > 0|completed", "J1.case x > 0 #2|completed", "J1.default|completed"}},
		{"QualifiedTypeCase", []string{"J1.case *types.Basic|completed", "J1.default|completed"}},

		{"UsesFor", []string{"|completed"}},
		{"UsesRange", []string{"|completed"}},
		{"TransparentLoopBeforeIf", []string{"J1.true|completed", "J1.false|completed"}},
		{"IgnoredIfInLoop", []string{"|completed"}},
		{"LoopWithActivity", []string{
			"J1.iterate J2.failure|failed",
			"J1.iterate J2.success J1.retry J1.exit|completed",
			"J1.exit|completed",
		}},
		{"LoopWithOnlyIf", []string{
			"J1.iterate J2.true J1.retry J1.exit|completed",
			"J1.iterate J2.false J1.retry J1.exit|completed",
			"J1.exit|completed",
		}},
		{"ForeverWithBreak", []string{"J1.iterate J2.failure|failed", "J1.iterate J2.success J3.true|completed"}},
		{"ContinueInLoop", []string{
			"J1.iterate J2.true J1.retry J1.exit|completed",
			"J1.iterate J2.false J3.failure|failed",
			"J1.iterate J2.false J3.success J1.retry J1.exit|completed",
			"J1.exit|completed",
		}},
		{"NestedLoops", []string{
			"J1.iterate J2.iterate J3.failure|failed",
			"J1.iterate J2.iterate J3.success J2.retry J2.exit J1.retry J1.exit|completed",
			"J1.iterate J2.exit J1.retry J1.exit|completed",
			"J1.exit|completed",
		}},
		{"LabeledBreakContinue", []string{
			"J1.iterate J2.iterate J3.true J1.retry J1.exit|completed",
			"J1.iterate J2.iterate J3.false J4.true|completed",
			"J1.iterate J2.iterate J3.false J4.false J5.failure|failed",
			"J1.iterate J2.iterate J3.false J4.false J5.success J2.retry J2.exit J1.retry J1.exit|completed",
			"J1.iterate J2.exit J1.retry J1.exit|completed",
			"J1.exit|completed",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.workflow, func(t *testing.T) {
			g, err := buildRule(t, tt.workflow)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if got := pathKeys(g); !slices.Equal(got, tt.want) {
				t.Errorf("paths:\n got  %q\n want %q", got, tt.want)
			}
		})
	}
}

func TestJunctionLabels(t *testing.T) {
	tests := []struct{ workflow, want string }{
		{"PlainIfElse", "if x > 0"},
		{"IfWithInit", "if n > 3"},
		{"CompoundCondition", "if err != nil && x > 0"},
		{"ActivityErr", "Charge (activity)"},
		{"NilOnLeft", "Charge (activity)"},
		{"EqNilTemporal", "Notify (local activity)"},
		{"ChildErr", "ChildFlow (child workflow)"},
		{"SleepAwaitErr", "Sleep (timer)"},
		{"FutureVariable", "Charge (activity)"},
		{"ForcedBranch", "decode (call)"},
		{"UsesSwitch", "switch s"},
		{"UsesTypeSwitch", "switch v.(type)"},
		{"TaglessSwitchWithInit", "switch"},
		{"TypeSwitchAssign", "switch v.(type)"},
		{"LoopWithActivity", "for i < n"},
		{"LoopWithOnlyIf", "for range xs"},
		{"ForeverWithBreak", "for"},
	}
	for _, tt := range tests {
		g, err := buildRule(t, tt.workflow)
		if err != nil {
			t.Fatalf("%s: %v", tt.workflow, err)
		}
		if got := g.Junctions[0].Label; got != tt.want {
			t.Errorf("%s: J1 label = %q, want %q", tt.workflow, got, tt.want)
		}
	}
	g, _ := buildRule(t, "SleepAwaitErr")
	if got := g.Junctions[1].Label; got != "Await (wait)" {
		t.Errorf("SleepAwaitErr J2 label = %q, want %q", got, "Await (wait)")
	}
}

func TestUnsupported(t *testing.T) {
	const planned = "is supported from M4"
	tests := []struct{ workflow, construct, ending string }{
		{"UsesSelector", "workflow.Selector", planned},
		{"UsesAwaitResult", "result of AwaitWithTimeout used in an if", planned},
		{"UsesReceiveWithTimeout", "result of ReceiveWithTimeout used in an if", planned},
		{"UsesDeferCompensation", "defer with a Temporal call (saga compensation)", planned},
		// never supported: the message says why, and promises nothing
		{"UsesGoSelect", "select statement", "is not supported: Temporal workflows must use workflow.Selector instead of Go's select"},
		{"UsesLabel", "goto", "is not supported: PathKit maps break, continue and return, but not goto"},
	}
	for _, tt := range tests {
		_, err := buildRule(t, tt.workflow)
		var u *model.UnsupportedError
		if !errors.As(err, &u) {
			t.Errorf("%s: err = %v, want an UnsupportedError", tt.workflow, err)
			continue
		}
		if u.Construct != tt.construct || !strings.HasSuffix(err.Error(), tt.ending) {
			t.Errorf("%s: err = %q, want construct %q ending %q", tt.workflow, err, tt.construct, tt.ending)
		}
	}
}

func TestMethodWorkflow(t *testing.T) {
	g, err := buildRule(t, "Service.MethodWorkflow")
	if err != nil {
		t.Fatal(err)
	}
	if got := pathKeys(g); len(got) != 2 {
		t.Errorf("paths = %q, want 2", got)
	}
}

func TestPathCap(t *testing.T) {
	g, err := buildRule(t, "TwelveIfs")
	if err != nil {
		t.Fatal(err)
	}
	ps := g.Paths(model.DefaultMaxPaths)
	if len(ps.List) != 2000 || !ps.Truncated {
		t.Errorf("got %d paths, truncated=%v; want 2000, true", len(ps.List), ps.Truncated)
	}
	full := g.Paths(10000)
	if len(full.List) != 4096 || full.Truncated {
		t.Errorf("uncapped: got %d paths, truncated=%v; want 4096, false", len(full.List), full.Truncated)
	}
}
