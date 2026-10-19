package model_test

import (
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

func callSitesOf(t *testing.T, dir string) []model.CallSite {
	t.Helper()
	wf, ok := workflowsIn(t, "../../testdata/constructs/"+dir)[dir+".Run"]
	if !ok {
		t.Fatalf("%s.Run not found", dir)
	}
	g, err := model.Build(wf)
	if err != nil {
		t.Fatal(err)
	}
	return g.CallSites()
}

// The kinds of failure the code goes on to tell apart are listed with the call;
// a call whose failure is not told apart has none.
func TestCallSitesFailureKinds(t *testing.T) {
	for dir, want := range map[string]string{
		"errkind_application":    model.FailureApplication,
		"errkind_timeout":        model.FailureTimeout,
		"errkind_child_canceled": model.FailureCancelled,
		"errkind_nonretryable":   model.FailureNonRetryable,
	} {
		sites := callSitesOf(t, dir)
		if len(sites) != 1 {
			t.Errorf("%s: %d call sites, want 1", dir, len(sites))
			continue
		}
		found := false
		for _, f := range sites[0].Failures {
			found = found || f.Kind == want
		}
		if !found {
			t.Errorf("%s: failure kinds %+v, want %q", dir, sites[0].Failures, want)
		}
		if sites[0].Line == 0 || sites[0].Name == "" {
			t.Errorf("%s: incomplete %+v", dir, sites[0])
		}
	}
	// Near miss: nothing is told apart.
	sites := callSitesOf(t, "loop_rounds")
	if len(sites) != 1 || len(sites[0].Failures) != 0 || sites[0].Kind != "activity" || sites[0].Name != "Process" {
		t.Errorf("loop_rounds: %+v", sites)
	}
	if sites := callSitesOf(t, "nexus_op"); len(sites) != 1 || sites[0].Kind != "nexus operation" || sites[0].Name != "echo" {
		t.Errorf("nexus_op: %+v", sites)
	}
}
