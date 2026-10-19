package model_test

import (
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

func TestOldVersionPathCount(t *testing.T) {
	wfs := workflowsIn(t, "../../testdata/fixtures/versiongate")
	for name, want := range map[string]string{
		"CatchEqDefault":       "1",
		"CatchBoolVar":         "1",
		"CatchHelperGate":      "1",
		"NotAGateFunction":     "", // no GetVersion check
		"GateFunctionWithMore": "",
	} {
		g, err := model.Build(wfs["versiongate."+name])
		if err != nil {
			t.Fatal(err)
		}
		n, has := g.OldVersionPathCount()
		switch {
		case want == "" && has:
			t.Errorf("%s: reported %s old paths for a workflow with no recognised gate", name, n)
		case want != "" && (!has || n.String() != want):
			t.Errorf("%s: got %v (has %v), want %s", name, n, has, want)
		}
	}
}
