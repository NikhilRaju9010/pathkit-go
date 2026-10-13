package model_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// The frozen map. testdata/golden/model/snapshot.txt
// holds, for every workflow of the pilot and of every fixture package that
// compiles, each junction (id, kind, label, exits) and each path (id,
// stableId, key, end). It was generated once, from the code as it was
// before the extractor "facts" existed. Facts are notes stuck on junctions;
// they must never create, remove, renumber or relabel anything, so this
// file must never change.
//
// There is deliberately NO switch to rewrite it. If this test fails, the map
// changed: that is a bug, or a design question, never a
// refresh.

const snapshotFile = "../../testdata/golden/model/snapshot.txt"

// snapshotPackages are loaded one at a time. "broken" does not compile, so
// it has no workflows to map.
func snapshotPackages(t *testing.T) []string {
	t.Helper()
	pkgs := []string{"../../testdata/pilot/..."}
	entries, err := os.ReadDir("../../testdata/fixtures")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() != "broken" {
			dir := filepath.Join("../../testdata/fixtures", e.Name())
			if goFiles, _ := filepath.Glob(filepath.Join(dir, "*.go")); len(goFiles) == 0 {
				// a folder of packages (samename/a/dup, samename/b/dup)
				dir += "/..."
			}
			pkgs = append(pkgs, filepath.ToSlash(dir))
		}
	}
	return pkgs
}

// snapshotText describes the workflows in only (all of them when only is
// nil). Fixture packages added after the freeze are not in the frozen file,
// so the comparison looks at exactly the workflows the file lists.
func snapshotText(t *testing.T, only map[string]bool) string {
	t.Helper()
	var b strings.Builder
	var names []string
	all := map[string]string{}
	for _, pkg := range snapshotPackages(t) {
		for name := range workflowsIn(t, pkg) {
			all[name] = pkg
		}
	}
	for name := range all {
		if only == nil || only[name] {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		wf := workflowsIn(t, all[name])[name]
		fmt.Fprintf(&b, "## %s\n", name)
		g, err := model.Build(wf)
		if err != nil {
			fmt.Fprintf(&b, "build: %v\n\n", strings.ReplaceAll(err.Error(), filepath.ToSlash(wd(t)), "<wd>"))
			continue
		}
		for _, j := range g.Junctions {
			var exits []string
			for _, e := range j.Exits {
				exits = append(exits, e.ID.String())
			}
			retry := ""
			if j.Retry != nil {
				retry = " retry=" + j.Retry.ID.String()
			}
			fmt.Fprintf(&b, "junction %s kind=%d %q exits=[%s]%s\n", j.ID, j.Kind, j.Label, strings.Join(exits, " | "), retry)
		}
		list := g.Paths(model.DefaultMaxPaths)
		for i, p := range list.List {
			fmt.Fprintf(&b, "path %d id=%s stable=%s %s\n", i+1, p.ID(), p.StableID(), withNote(p.Key(), p.Compensation))
		}
		fmt.Fprintf(&b, "paths=%d truncated=%v\n\n", len(list.List), list.Truncated)
	}
	return b.String()
}

func wd(t *testing.T) string {
	t.Helper()
	d, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestModelSnapshot(t *testing.T) {
	want, err := os.ReadFile(snapshotFile)
	if err != nil {
		t.Fatalf("the frozen map is missing: %v", err)
	}
	only := map[string]bool{}
	for _, line := range strings.Split(string(want), "\n") {
		if name, ok := strings.CutPrefix(line, "## "); ok {
			only[name] = true
		}
	}
	got := snapshotText(t, only)
	if got != string(want) {
		gl, wl := strings.Split(got, "\n"), strings.Split(string(want), "\n")
		for i := 0; i < len(gl) || i < len(wl); i++ {
			var g, w string
			if i < len(gl) {
				g = gl[i]
			}
			if i < len(wl) {
				w = wl[i]
			}
			if g != w {
				t.Fatalf("the map changed at snapshot line %d (this file must never be edited to make the test pass):\n want: %s\n  got: %s", i+1, w, g)
			}
		}
	}
}
