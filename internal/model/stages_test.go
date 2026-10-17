package model_test

import (
	"math/big"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// For every workflow that can be listed in full, the stages add up: the stage
// paths multiply out to the number of paths, every path cuts into stage pieces
// that are stage paths, and no stage is bigger than the limit unless it is
// marked big. Run with several limits.
func TestStagesAddUp(t *testing.T) {
	dirs := append([]string{"../../testdata/pilot/..."}, fixtureDirs(t)...)
	checked, multi := 0, 0
	for _, limit := range []int{4, 16, 256} {
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
				ps := g.Paths(20000)
				if ps.Truncated {
					continue
				}
				stages := g.Stages(limit)
				// the stage paths of every stage
				keys := make([]map[string]bool, len(stages))
				ends := make([]*big.Int, len(stages))
				goes := make([]*big.Int, len(stages))
				for k, st := range stages {
					if st.Big {
						if st.Count.Cmp(big.NewInt(int64(limit))) <= 0 {
							t.Errorf("%s: stage %d is marked big with %s paths (limit %d)", wf.Name, k, st.Count, limit)
						}
						keys[k] = nil
						continue
					}
					if st.Count.Cmp(big.NewInt(int64(limit))) > 0 {
						t.Errorf("%s: stage %d has %s paths, over the limit %d, and is not marked big", wf.Name, k, st.Count, limit)
					}
					sp, trunc := g.StagePaths(st, limit+1)
					if trunc {
						t.Errorf("%s: stage %d lists more than it counts", wf.Name, k)
					}
					if int64(len(sp)) != st.Count.Int64() {
						t.Errorf("%s: stage %d counts %s but lists %d", wf.Name, k, st.Count, len(sp))
					}
					keys[k] = map[string]bool{}
					ends[k], goes[k] = new(big.Int), new(big.Int)
					for _, p := range sp {
						keys[k][p.Key()] = true
						if p.Continue {
							goes[k].Add(goes[k], big.NewInt(1))
						} else {
							ends[k].Add(ends[k], big.NewInt(1))
						}
					}
				}
				allSmall := true
				for _, st := range stages {
					allSmall = allSmall && !st.Big
				}
				if allSmall {
					total, through := new(big.Int), big.NewInt(1)
					for k := range stages {
						total.Add(total, new(big.Int).Mul(through, ends[k]))
						through = new(big.Int).Mul(through, goes[k])
					}
					if total.Cmp(g.PathCount()) != 0 {
						t.Errorf("%s (limit %d): stages multiply out to %s paths, PathCount says %s", wf.Name, limit, total, g.PathCount())
					}
				}
				for _, p := range ps.List {
					pieces := g.StagePieces(p, stages)
					for _, pc := range pieces {
						if keys[pc.Stage] != nil && !keys[pc.Stage][pc.Key()] {
							t.Errorf("%s: path %s has the piece %s in stage %d, which is not one of its stage paths", wf.Name, p.Key(), pc.Key(), pc.Stage)
						}
					}
				}
				if len(stages) > 1 {
					multi++
				}
				checked++
			}
		}
	}
	if checked < 150 || multi < 10 {
		t.Errorf("checked %d workflows, %d with several stages", checked, multi)
	}
}

// A workflow far over the path cap: the count is exact, and it is cut into
// stages of at most 256 paths.
func TestBigWorkflowStages(t *testing.T) {
	wfs := workflowsIn(t, "../../testdata/fixtures/bigflow")
	g, err := model.Build(wfs["bigflow.TwentyFourIfs"])
	if err != nil {
		t.Fatal(err)
	}
	if got, want := g.PathCount().String(), "16777216"; got != want {
		t.Errorf("PathCount = %s, want %s", got, want)
	}
	stages := g.Stages(256)
	if len(stages) != 3 {
		t.Fatalf("%d stages, want 3 (8 ifs each, 256 paths)", len(stages))
	}
	for _, st := range stages {
		if st.Count.Int64() != 256 || st.Big {
			t.Errorf("stage %d: %s paths, big %v", st.Index, st.Count, st.Big)
		}
	}
	e, err := model.Build(wfs["bigflow.EarlyExits"])
	if err != nil {
		t.Fatal(err)
	}
	// 3 early ends, then 2^20 paths; the early ends are paths of the first stage
	want := new(big.Int).Add(big.NewInt(3), new(big.Int).Lsh(big.NewInt(1), 20))
	if e.PathCount().Cmp(want) != 0 {
		t.Errorf("EarlyExits: PathCount = %s, want %s", e.PathCount(), want)
	}
	if st := e.Stages(256); len(st) < 2 {
		t.Errorf("EarlyExits: %d stages", len(st))
	}
}
