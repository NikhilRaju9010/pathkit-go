package model_test

import (
	"math/big"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// The exact count equals the number of paths Paths lists, on the pilot, every
// fixture and every shape fixture (loops, selectors, switches, helpers).
func TestPathCountMatchesTheListing(t *testing.T) {
	dirs := []string{"../../testdata/pilot/..."}
	dirs = append(dirs, fixtureDirs(t)...)
	checked := 0
	for _, dir := range dirs {
		res, err := load.Load(dir)
		if err != nil {
			continue
		}
		for _, wf := range discover.Find(res.Packages) {
			g, err := model.Build(wf)
			if err != nil {
				continue
			}
			ps := g.Paths(1 << 20)
			if ps.Truncated {
				continue
			}
			if got, want := g.PathCount(), big.NewInt(int64(len(ps.List))); got.Cmp(want) != 0 {
				t.Errorf("%s: PathCount = %s, Paths lists %s", wf.Name, got, want)
			}
			checked++
		}
	}
	if checked < 50 {
		t.Errorf("only %d workflows were checked", checked)
	}
}
