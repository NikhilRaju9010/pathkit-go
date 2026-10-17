package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/tools/go/packages"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

// scopeFlags are the flags every scoped command has (docs/DESIGN.md D8).
type scopeFlags struct {
	config  string
	include []string
	exclude []string
	all     bool // analyze only: ignore the scope
	// wantNames (not a flag): the command resolves workflow names from the
	// classification section (coverage and report), so the configured
	// packages must be loaded to check them.
	wantNames bool
	// everyPackage (report only, not a flag): with no target, load every
	// entry of the config's "packages", not just a single one.
	everyPackage bool
}

func addScopeFlags(cmd *cobra.Command, sf *scopeFlags, withAll bool) {
	f := cmd.Flags()
	f.StringVar(&sf.config, "config", "", "use this config `file` instead of searching for .pathkitrc.json")
	f.StringSliceVar(&sf.include, "include", nil, "only these `workflows` count (replaces the config's include list)")
	f.StringSliceVar(&sf.exclude, "exclude", nil, "leave these `workflows` out (reason: \"excluded by --exclude flag\")")
	if withAll {
		f.BoolVar(&sf.all, "all", false, "show every workflow, ignoring .pathkitrc.json, --include and --exclude")
	}
}

// recordable is a workflow PathKit can map, with its graph and hash.
type recordable struct {
	wf            discover.Workflow
	graph         *model.Graph
	hash          string
	addedByConfig bool
}

// notAnalyzable is an in-scope workflow PathKit can't map. It is always
// printed, never dropped from view (coverage and report refuse to run
// while one is in scope).
type notAnalyzable struct {
	name string
	err  error
}

// scoped is what a command works on: its target loaded, and the scope
// applied, the one way every command does it.
type scoped struct {
	target        string // what was loaded (the config's package when none was given)
	res           *load.Result
	cfg           *scope.Config // nil: no config file in use
	mapped        []recordable
	excluded      []scope.Excluded
	notAnalyzable []notAnalyzable
	// names are every workflow-like function the names in the config were
	// checked against (full names, "orders.OrderWorkflow").
	names []string
	// defaulted: no folder was given, so the config's own packages (all of
	// them for report) are what was loaded.
	defaulted bool
}

// loadScoped loads target and applies the scope from .pathkitrc.json (or
// --config) and --include/--exclude. With --all the scope is ignored, and
// a line on stderr says so.
func loadScoped(cmdName, target string, sf scopeFlags, stderr io.Writer) (*scoped, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	cfg, err := loadConfig(sf.config, cwd)
	if err != nil {
		return nil, err
	}
	flags := scope.Flags{Include: sf.include, Exclude: sf.exclude}
	if sf.all {
		if cfg != nil {
			fmt.Fprintf(stderr, "pathkit %s: --all: %s is ignored; showing every workflow\n", cmdName, cfg.Path)
		}
		if flags.Include != nil || len(flags.Exclude) > 0 {
			fmt.Fprintf(stderr, "pathkit %s: --all: --include and --exclude are ignored\n", cmdName)
		}
		cfg, flags = nil, scope.Flags{}
	}

	targets := []string{target}
	defaulted := target == "" // the config's own packages are what was loaded
	if target == "" {
		targets = []string{"./..."}
		if cfg != nil {
			if len(cfg.Packages) != 1 && !sf.everyPackage {
				return nil, userError("%s lists %d packages; pass the folder to use as an argument", cfg.Path, len(cfg.Packages))
			}
			targets = nil
			for _, p := range cfg.Packages {
				targets = append(targets, cfg.Abs(p))
			}
		}
		target = strings.Join(targets, ", ")
	}
	res, err := loadAll(targets)
	if err != nil {
		return nil, userError("%s", err)
	}
	// Names must not depend on which folder this command was given: with a
	// config they are decided over the config's packages (loaded once more
	// when the command was given another folder), else over what was loaded.
	namer := discover.NewNamer(res.Packages)
	if cfg != nil && !sf.all && !defaulted {
		// Best effort: if the config's packages cannot be loaded here,
		// the names come from what this command loaded (a config problem
		// is reported where it matters, when its names are needed).
		all := append([]*packages.Package(nil), res.Packages...)
		for _, p := range cfg.Packages {
			if r, err := load.Load(cfg.Abs(p)); err == nil {
				all = append(all, r.Packages...)
			}
		}
		namer = discover.NewNamer(all)
	}
	fns := discover.AllWith(namer, res.Packages)

	// Names are checked against the config's packages, so a misspelled
	// name is caught even when this command loaded only one file.
	nameSet, where := fns, target
	if cfg != nil && (cfg.HasInclude || len(cfg.Exclude) > 0 || flags.Include != nil || len(flags.Exclude) > 0 || sf.wantNames && cfg.Classification.Enabled && classificationNames(cfg) > 0) {
		nameSet, where = nil, "the configured packages ("+strings.Join(cfg.Packages, ", ")+")"
		for _, p := range cfg.Packages {
			r, err := load.Load(cfg.Abs(p))
			if err != nil {
				return nil, userError("%s: packages: %v", cfg.Path, err)
			}
			nameSet = append(nameSet, discover.AllWith(namer, r.Packages)...)
		}
	}
	rules, err := scope.Resolve(cfg, flags, nameSet, where)
	if err != nil {
		return nil, userError("%s", err)
	}

	if res.File != "" { // only the workflows declared in the named file
		var inFile []discover.Function
		for _, f := range fns {
			if load.SameFile(f.Workflow.Filename, res.File) {
				inFile = append(inFile, f)
			}
		}
		fns = inFile
	}
	applied := rules.Apply(fns)
	out := &scoped{target: target, res: res, cfg: cfg, excluded: applied.Excluded, defaulted: defaulted}
	for _, f := range nameSet {
		out.names = append(out.names, f.Workflow.Name)
	}
	for _, e := range applied.InScope {
		g, err := model.Build(e.Workflow)
		var u *model.UnsupportedError
		var pe *model.PragmaError
		if errors.As(err, &u) || errors.As(err, &pe) {
			out.notAnalyzable = append(out.notAnalyzable, notAnalyzable{e.Workflow.Name, err})
			continue
		}
		if err != nil {
			return nil, err
		}
		out.mapped = append(out.mapped, recordable{
			wf: e.Workflow, graph: g, hash: g.CodeHash(), addedByConfig: e.AddedByConfig,
		})
	}
	return out, nil
}

// loadAll loads each target and merges the packages, each package once
// (config entries such as "./..." and "./orders" can overlap).
func loadAll(targets []string) (*load.Result, error) {
	if len(targets) == 1 {
		return load.Load(targets[0])
	}
	merged := &load.Result{}
	seen := map[string]bool{}
	for _, t := range targets {
		r, err := load.Load(t)
		if err != nil {
			return nil, err
		}
		for _, p := range r.Packages {
			if !seen[p.ID] {
				seen[p.ID] = true
				merged.Packages = append(merged.Packages, p)
			}
		}
	}
	return merged, nil
}

// notAnalyzableHint ends every "in scope but not analyzable" line.
const notAnalyzableHint = "(fix it, or exclude it in .pathkitrc.json with a reason)"

// excludedBlock is analyze's list of excluded workflows with their
// reasons, so none disappears silently. analyze has --all, so it says so.
// (coverage and report print render.ExcludedBlock instead.)
func excludedBlock(excluded []scope.Excluded) string {
	if len(excluded) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nExcluded from scope (%d), pass --all to show them:\n", len(excluded))
	for _, e := range excluded {
		fmt.Fprintf(&b, "  %s: %s\n", e.Name, e.Reason)
	}
	return b.String()
}

// classificationNames counts the workflow names the classification section
// refers to (overrides and rules).
func classificationNames(cfg *scope.Config) int {
	return len(cfg.Classification.Overrides) + len(cfg.Classification.Rules)
}

// loadConfig reads the config file and checks what only the classify
// package knows (detector names), so every command that loads a config
// reports a misspelled detector, whether or not classification is enabled.
func loadConfig(explicit, cwd string) (*scope.Config, error) {
	cfg, err := scope.Load(explicit, cwd)
	if err != nil {
		return nil, userError("%s", err)
	}
	if cfg != nil {
		if err := classify.CheckDetectorNames(cfg.Classification.DisabledDetectors); err != nil {
			return nil, userError("%s: %s", cfg.Path, err)
		}
	}
	return cfg, nil
}
