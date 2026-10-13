package render

import (
	"errors"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// Every decision of every path of the pilot and of the construct fixtures
// gets a specific hint, never the catch-all wording; the last line says
// how the run ends when the map knows it.
func TestHowToCoverEveryKind(t *testing.T) {
	fallbacks := []string{": take ", "be ready first", "return an error", "return no error"}
	kinds := map[model.JunctionKind]bool{}
	for _, dir := range []string{"../../testdata/pilot/...", "../../testdata/fixtures/switches", "../../testdata/fixtures/loops", "../../testdata/fixtures/waits", "../../testdata/fixtures/rules"} {
		res, err := load.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, wf := range discover.Find(res.Packages) {
			g, err := model.Build(wf)
			var u *model.UnsupportedError
			if errors.As(err, &u) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range g.Paths(model.DefaultMaxPaths).List {
				hints := HowToCover(p)
				for _, s := range p.Steps {
					kinds[s.Junction.Kind] = true
				}
				for _, h := range hints {
					for _, f := range fallbacks {
						if strings.Contains(h, f) {
							t.Errorf("%s %s: catch-all hint %q", wf.Name, PathText(p), h)
						}
					}
				}
				want := len(p.Steps)
				if p.End != model.EndUnknown {
					want++
				}
				if len(hints) != want {
					t.Errorf("%s %s: %d hints for %d steps:\n%s", wf.Name, PathText(p), len(hints), len(p.Steps), strings.Join(hints, "\n"))
				}
			}
		}
	}
	for _, k := range []model.JunctionKind{model.PlainIf, model.ErrCheck, model.Switch, model.Loop, model.Selector, model.WaitResult} {
		if !kinds[k] {
			t.Errorf("junction kind %d not covered by the fixtures", k)
		}
	}
}

func TestHowToCoverWording(t *testing.T) {
	res, err := load.Load("../../testdata/pilot/polling")
	if err != nil {
		t.Fatal(err)
	}
	g, err := model.Build(discover.Find(res.Packages)[0])
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, p := range g.Paths(model.DefaultMaxPaths).List {
		got[PathText(p)] = strings.Join(HowToCover(p), "\n")
	}
	for path, want := range map[string]string{
		"Start -> for attempt <= in.MaxPolls --exit--> End (completed)": "skip `for attempt <= in.MaxPolls` entirely (its condition is false, or the list is empty, the first time)\n" +
			"expect the workflow to complete",
		"Start -> for attempt <= in.MaxPolls --iterate--> CheckStatus (activity) --success--> switch status --default--> retry --> for attempt <= in.MaxPolls --exit--> End (completed)": "go into `for attempt <= in.MaxPolls` (one trip is enough; what follows inside the loop must happen on its last trip)\n" +
			"let activity CheckStatus succeed (mock it with env.OnActivity, passing the same activity the workflow starts (CheckStatus), with .Return(...); or let it run)\n" +
			"make `status` match none of the cases (its default)\n" +
			"let that trip end normally (no return or break), so `for attempt <= in.MaxPolls` checks again whether to go round\n" +
			"then leave `for attempt <= in.MaxPolls` (its condition becomes false, or the list runs out)\n" +
			"expect the workflow to complete",
	} {
		if got[path] != want {
			t.Errorf("%s:\n%s\nwant\n%s", path, got[path], want)
		}
	}
}
