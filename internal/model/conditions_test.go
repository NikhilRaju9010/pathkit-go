package model_test

import (
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// The parts of an && / || chain get IDs; a decision with one part gets none.
func TestConditionsOfAChain(t *testing.T) {
	res, err := load.Load("../../testdata/fixtures/conditions/...")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string][]string{}
	ids := map[string]bool{}
	for _, wf := range discover.Find(res.Packages) {
		g, err := model.BuildWith(wf, model.Options{Helpers: model.HelpersInline})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range g.Conditions() {
			found[wf.Name] = append(found[wf.Name], c.Text)
			if ids[c.ID] || !strings.HasPrefix(c.ID, "c:") || len(c.ID) != 14 {
				t.Errorf("bad or repeated condition id %q", c.ID)
			}
			ids[c.ID] = true
		}
	}
	want := map[string]string{
		"conditions.Chain":     "req.Amount > 0|req.Kind == \"A\"|req.Express|req.Region != \"\"",
		"conditions.Repeated":  "isOK(ok)|isOK(ok)",
		"conditions.HelperAnd": "n > 1|n < 10",
	}
	for wf, w := range want {
		if got := strings.Join(found[wf], "|"); got != w {
			t.Errorf("%s: parts %q, want %q", wf, got, w)
		}
	}
	if len(found["conditions.Single"]) != 0 {
		t.Errorf("a one-part condition has no parts: %v", found["conditions.Single"])
	}
}
