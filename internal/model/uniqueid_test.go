package model_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// No two listed paths of one workflow may share a stable id: an override
// written for one would silently apply to the other. This runs on the pilot,
// every fixture and every shape fixture, and on any folders named in
// PATHKIT_UNIQUE_STABLE_DIRS (a path list, for projects kept outside the repo).
func TestStableIDsAreUniqueInEveryWorkflow(t *testing.T) {
	dirs := []string{"../../testdata/pilot/..."}
	for _, root := range []string{"../../testdata/fixtures", "../../testdata/shapes"} {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() && e.Name() != "broken" && !strings.HasPrefix(e.Name(), ".") {
				dirs = append(dirs, filepath.Join(root, e.Name())+"/...")
			}
		}
	}
	if extra := os.Getenv("PATHKIT_UNIQUE_STABLE_DIRS"); extra != "" {
		dirs = append(dirs, filepath.SplitList(extra)...)
	}
	checked := 0
	for _, dir := range dirs {
		res, err := load.Load(dir)
		if err != nil {
			t.Logf("%s: not loaded (%v)", dir, err)
			continue
		}
		for _, wf := range discover.Find(res.Packages) {
			g, err := model.Build(wf)
			if err != nil {
				continue // a workflow PathKit cannot map has no paths
			}
			seen := map[string]string{}
			for _, p := range g.Paths(model.DefaultMaxPaths).List {
				id := p.StableID()
				if other, dup := seen[id]; dup {
					t.Errorf("%s: stable id %s is used by two paths:\n  %s\n  %s", wf.Name, id, other, p.StableKey())
				}
				seen[id] = strings.ReplaceAll(p.StableKey(), "\x1f", " ")
			}
			checked++
		}
	}
	if checked < 50 {
		t.Errorf("only %d workflows were checked", checked)
	}
}
