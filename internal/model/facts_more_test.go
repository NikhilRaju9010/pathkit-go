package model_test

import (
	"fmt"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

const closedDir = "../../testdata/fixtures/closedchannel"

// moreFacts lists the MoreCheck facts of a workflow as "exit/origin/name".
func moreFacts(t *testing.T, name string) []string {
	t.Helper()
	wf, ok := workflowsIn(t, closedDir)["closedchannel."+name]
	if !ok {
		t.Fatalf("fixture closedchannel.%s not found", name)
	}
	g, err := model.Build(wf)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var out []string
	for _, j := range g.Junctions {
		for _, f := range j.Facts {
			if f.Kind == model.MoreCheck {
				out = append(out, fmt.Sprintf("%s:%s/%s/%s", j.ID, f.Exit, f.Origin, f.Name))
			}
		}
	}
	return out
}

// TestMoreFacts: every shape of the closed-channel check, the exit that
// means "closed", and where the channel came from. An empty "want" means
// no fact at all.
func TestMoreFacts(t *testing.T) {
	cases := []struct {
		name string
		want string // "" = no fact
	}{
		// catch
		{"CatchAddReceive", "J2:true/signal/cb"},
		{"CatchReceive", "J1:true/signal/orders"},
		{"CatchDirectCall", "J1:true/signal/direct"},
		{"CatchReceiveWithTimeout", "J1:true/signal/rwt"},
		{"CatchReceiveAsyncMore", "J1:true/signal/async"},
		{"CatchWithOptions", "J1:true/signal/opts"},
		{"CatchEqFalse", "J1:true/signal/eq"},
		{"CatchFalseEq", "J1:true/signal/feq"},
		{"CatchNeTrue", "J1:true/signal/ne"},
		{"CatchElseSide", "J1:false/signal/else"},
		{"CatchInitStatement", "J1:true/signal/init"},
		// the workflow's own channel: a fact, but origin own
		{"OwnClosed", "J1:true/own/"},
		{"OwnBuffered", "J1:true/own/"},
		{"OwnNamed", "J1:true/own/"},
		{"OwnReceiveWithTimeout", "J1:true/own/"},
		{"OwnAddReceive", "J2:true/own/"},
		// origin cannot be followed
		{"ParamChannel", "J1:true/unknown/"},
		{"HelperChannel", "J1:true/unknown/"},
		{"TwiceAssigned", "J1:true/unknown/"},
		{"SetInABranch", "J2:true/unknown/"},
		{"FieldChannel", "J1:true/unknown/"},
		{"ClosureChannel", "J2:true/unknown/"},
		{"ElementChannel", "J1:true/unknown/"},
		{"NamedResult", "J1:true/unknown/"},
		{"AssertedClose", "J1:true/unknown/"},
		{"PassedOn", "J1:true/unknown/"},
		// not a closed-channel check at all
		{"PlainBoolMore", ""},
		{"ShadowedMore", ""},
		{"CompoundCondition", ""},
		{"OkResult", ""},
		{"NotTheSDK", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := moreFacts(t, c.name)
			if c.want == "" {
				if len(got) != 0 {
					t.Fatalf("expected no fact, got %v", got)
				}
				return
			}
			if len(got) != 1 || got[0] != c.want {
				t.Fatalf("got %v, want [%s]", got, c.want)
			}
		})
	}
}

// Facts must change nothing: with the extractor off, every workflow of the
// pilot and of every fixture has the same junctions, exits and paths (id,
// stableId, key) as with it on.
func TestFactsChangeNothing(t *testing.T) {
	on := snapshotText(t, nil)
	restore := model.SetFactsEnabled(false)
	off := snapshotText(t, nil)
	restore()
	if on != off {
		t.Fatal("the extractor changed the map")
	}
}
