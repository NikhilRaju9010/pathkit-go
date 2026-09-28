package scope

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
)

// fixtureFunctions is every function in testdata/fixtures/scope/...:
// VisibleFlow, a.Run and b.Run (found automatically), scope.Svc.Handle
// (a method workflow), lowerFlow and NoErrorFlow (only include can add
// them), and sendEmail (never a workflow).
func fixtureFunctions(t *testing.T) []discover.Function {
	t.Helper()
	res, err := load.Load("../../testdata/fixtures/scope/...")
	if err != nil {
		t.Fatal(err)
	}
	return discover.All(res.Packages)
}

func cfgWith(include []string, exclude ...Exclude) *Config {
	return &Config{Path: "/x/.pathkitrc.json", Include: include, HasInclude: include != nil, Exclude: exclude}
}

type view struct {
	in       []string // "name" or "name (added by config)"
	excluded []string // "name: reason"
}

func apply(t *testing.T, cfg *Config, flags Flags) (view, error) {
	t.Helper()
	fns := fixtureFunctions(t)
	r, err := Resolve(cfg, flags, fns, "the configured packages (./...)")
	if err != nil {
		return view{}, err
	}
	res := r.Apply(fns)
	var v view
	for _, e := range res.InScope {
		s := e.Workflow.Name
		if e.AddedByConfig {
			s += " (added by config)"
		}
		v.in = append(v.in, s)
	}
	for _, e := range res.Excluded {
		v.excluded = append(v.excluded, e.Name+": "+e.Reason)
	}
	return v, nil
}

func TestScopeRules(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *Config
		flags    Flags
		in       []string
		excluded []string
	}{
		{"no config, no flags: every automatic workflow, as before M5", nil, Flags{},
			[]string{"a.Run", "b.Run", "scope.Svc.Handle", "scope.VisibleFlow"}, nil},
		{"config without workflows: the same", cfgWith(nil), Flags{},
			[]string{"a.Run", "b.Run", "scope.Svc.Handle", "scope.VisibleFlow"}, nil},
		{"include restricts; every workflow left out is listed", cfgWith([]string{"VisibleFlow"}), Flags{},
			[]string{"scope.VisibleFlow"},
			[]string{"a.Run: not in include list", "b.Run: not in include list", "scope.Svc.Handle: not in include list"}},
		{"include adds what the automatic rule misses, labelled", cfgWith([]string{"lowerFlow", "NoErrorFlow", "a.Run"}), Flags{},
			[]string{"a.Run", "scope.NoErrorFlow (added by config)", "scope.lowerFlow (added by config)"},
			[]string{"b.Run: not in include list", "scope.Svc.Handle: not in include list", "scope.VisibleFlow: not in include list"}},
		{"exclude with a reason", cfgWith(nil, Exclude{"VisibleFlow", "flaky sandbox"}), Flags{},
			[]string{"a.Run", "b.Run", "scope.Svc.Handle"},
			[]string{"scope.VisibleFlow: flaky sandbox"}},
		{"--exclude adds its own reason", cfgWith(nil, Exclude{"VisibleFlow", "flaky sandbox"}), Flags{Exclude: []string{"a.Run"}},
			[]string{"b.Run", "scope.Svc.Handle"},
			[]string{"a.Run: excluded by --exclude flag", "scope.VisibleFlow: flaky sandbox"}},
		{"--exclude with no config", nil, Flags{Exclude: []string{"b.Run"}},
			[]string{"a.Run", "scope.Svc.Handle", "scope.VisibleFlow"},
			[]string{"b.Run: excluded by --exclude flag"}},
		{"--include replaces the config's include", cfgWith([]string{"VisibleFlow"}), Flags{Include: []string{"b.Run"}},
			[]string{"b.Run"},
			[]string{"a.Run: not in include list", "scope.Svc.Handle: not in include list", "scope.VisibleFlow: not in include list"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := apply(t, tt.cfg, tt.flags)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(v.in, tt.in) || !slices.Equal(v.excluded, tt.excluded) {
				t.Errorf("in scope %q\nexcluded %q\nwant in scope %q\nexcluded %q", v.in, v.excluded, tt.in, tt.excluded)
			}
		})
	}

	// Every accepted name form picks the same method workflow.
	for _, name := range []string{"Handle", "Svc.Handle", "scope.Svc.Handle", "scope.(*Svc).Handle"} {
		v, err := apply(t, cfgWith([]string{name}), Flags{})
		if err != nil || !slices.Equal(v.in, []string{"scope.Svc.Handle"}) {
			t.Errorf("include %q: in scope %q, err %v; want scope.Svc.Handle", name, v.in, err)
		}
	}
}

func TestScopeErrors(t *testing.T) {
	const where = "the configured packages (./...)"
	tests := []struct {
		name  string
		cfg   *Config
		flags Flags
		want  string
	}{
		{"misspelled include", cfgWith([]string{"VisibleFlw"}), Flags{},
			`/x/.pathkitrc.json: include "VisibleFlw" matches no function in ` + where},
		{"include of a non-workflow (D8's example)", cfgWith([]string{"sendEmail"}), Flags{},
			`/x/.pathkitrc.json: include "sendEmail" is not a workflow: its first parameter is not workflow.Context`},
		{"ambiguous short name", cfgWith([]string{"Run"}), Flags{},
			`/x/.pathkitrc.json: include "Run" matches 2 functions (a.Run, b.Run); write it with its package, e.g. "a.Run"`},
		{"misspelled exclude", cfgWith(nil, Exclude{"VisibleFlw", "x"}), Flags{},
			`/x/.pathkitrc.json: exclude "VisibleFlw" matches no workflow in ` + where},
		{"exclude of a function that isn't a workflow", cfgWith(nil, Exclude{"NoErrorFlow", "x"}), Flags{},
			`/x/.pathkitrc.json: exclude "NoErrorFlow" matches no workflow in ` + where},
		{"both lists", cfgWith([]string{"Svc.Handle"}, Exclude{"scope.(*Svc).Handle", "x"}), Flags{},
			`/x/.pathkitrc.json: "scope.(*Svc).Handle" is both included and excluded`},
		{"misspelled --exclude", nil, Flags{Exclude: []string{"VisibleFlw"}},
			`--exclude "VisibleFlw" matches no workflow in ` + where},
		{"misspelled --include", nil, Flags{Include: []string{"lowerFlw"}},
			`--include "lowerFlw" matches no function in ` + where},
		{"empty --include", nil, Flags{Include: []string{}},
			"--include is empty: leave it out to include every workflow, or list the ones you want"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := apply(t, tt.cfg, tt.flags)
			if err == nil || err.Error() != tt.want {
				t.Errorf("err = %v\nwant    %s", err, tt.want)
			}
		})
	}
}

// Relative config paths are relative to the config file, not the folder
// the command runs in.
func TestAbs(t *testing.T) {
	c := &Config{Dir: "/proj/cfg"}
	if got := c.Abs("../pilot/..."); got != filepath.Clean("/proj/pilot/...") {
		t.Errorf("Abs = %q", got)
	}
	if got := c.Abs("/abs/x"); got != "/abs/x" {
		t.Errorf("Abs(absolute) = %q", got)
	}
}
