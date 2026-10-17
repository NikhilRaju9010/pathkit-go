package coverage

import (
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

func groupsFor(t *testing.T, steps ...[]string) []MissedGroup {
	t.Helper()
	orders := pilotWorkflow(t, "orders.OrderWorkflow")
	var traces []TraceFile
	for i, s := range steps {
		traces = append(traces, tf(string(rune('a'+i)), orders, "complete", s...))
	}
	return MissedGroups(Compute(Input{Workflows: []Workflow{orders}, Traces: traces}).Workflows[0])
}

func summary(gs []MissedGroup) []string {
	var out []string
	for _, g := range gs {
		out = append(out, g.Step.Exit.ID.String()+"x"+string(rune('0'+g.Missed))+"/"+string(rune('0'+len(g.Prefix))))
	}
	return out
}

// Missed paths are grouped by the first step no test took, found by walking
// the tree of the paths the tests ran.
func TestMissedGroups(t *testing.T) {
	cases := []struct {
		name  string
		steps [][]string
		want  []string // exit x missed paths / steps the tests walked before it
	}{
		// no test: groups by the first decision's exits, the bigger first
		{"nothing ran", nil, []string{"J1.falsex2/0", "J1.truex1/0"}},
		// only "true" ran: the two paths behind "false" leave at the same step
		{"one side ran", [][]string{{"J1.true"}}, []string{"J1.falsex2/0"}},
		// the tests reached the second decision but never took "failure"
		{"second decision", [][]string{{"J1.true"}, {"J1.false", "J2.success"}}, []string{"J2.failurex1/1"}},
		{"all ran", [][]string{{"J1.true"}, {"J1.false", "J2.success"}, {"J1.false", "J2.failure"}}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := summary(groupsFor(t, c.steps...))
			if len(got) != len(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("got %v, want %v", got, c.want)
				}
			}
		})
	}
}

// A path that cannot be tested is not a missed path to write a test for: it
// is left out of the groups.
func TestMissedGroupsSkipPathsThatCannotBeTested(t *testing.T) {
	orders := pilotWorkflow(t, "orders.OrderWorkflow")
	impossible := func(_ string, p model.Path) classify.Classification {
		c := classify.DefaultTestable()
		if p.Key() == "J1.false J2.failure|failed" {
			c.Bucket, c.Label = classify.CannotBeTested, "manual"
		}
		return c
	}
	res := Compute(Input{Workflows: []Workflow{orders}, Classifier: impossible, Traces: []TraceFile{tf("a", orders, "complete", "J1.true")}})
	gs := MissedGroups(res.Workflows[0])
	if len(gs) != 1 || gs[0].Missed != 1 || gs[0].Step.Exit.ID.String() != "J1.false" {
		t.Errorf("got %v, want one group of 1 path", summary(gs))
	}
}
