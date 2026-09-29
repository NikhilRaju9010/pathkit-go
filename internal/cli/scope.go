package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

// scopeFlags are the flags every scoped command has (CLAUDE.md D8).
type scopeFlags struct {
	config  string
	include []string
	exclude []string
	all     bool // analyze only: ignore the scope
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
// printed, never dropped from view (owner's decision, 2026-09-28; from
// M6/M7, coverage and report refuse to run while one is in scope).
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
}

// loadScoped loads target and applies the scope from .pathkitrc.json (or
// --config) and --include/--exclude. With --all the scope is ignored, and
// a line on stderr says so.
func loadScoped(cmdName, target string, sf scopeFlags, stderr io.Writer) (*scoped, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	cfg, err := scope.Load(sf.config, cwd)
	if err != nil {
		return nil, userError("%s", err)
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
	fns := discover.All(res.Packages)

	// Names are checked against the config's packages, so a misspelled
	// name is caught even when this command loaded only one file.
	nameSet, where := fns, target
	if cfg != nil && (cfg.HasInclude || len(cfg.Exclude) > 0 || flags.Include != nil || len(flags.Exclude) > 0) {
		nameSet, where = nil, "the configured packages ("+strings.Join(cfg.Packages, ", ")+")"
		for _, p := range cfg.Packages {
			r, err := load.Load(cfg.Abs(p))
			if err != nil {
				return nil, userError("%s: packages: %v", cfg.Path, err)
			}
			nameSet = append(nameSet, discover.All(r.Packages)...)
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
	out := &scoped{target: target, res: res, cfg: cfg, excluded: applied.Excluded}
	for _, e := range applied.InScope {
		g, err := model.Build(e.Workflow)
		var u *model.UnsupportedError
		if errors.As(err, &u) {
			out.notAnalyzable = append(out.notAnalyzable, notAnalyzable{e.Workflow.Name, err})
			continue
		}
		if err != nil {
			return nil, err
		}
		out.mapped = append(out.mapped, recordable{
			wf: e.Workflow, graph: g, hash: model.FunctionHash(e.Workflow.Pkg.Fset, e.Workflow.Func), addedByConfig: e.AddedByConfig,
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

// excludedBlock lists the excluded workflows with their reasons, so none
// disappears silently.
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
