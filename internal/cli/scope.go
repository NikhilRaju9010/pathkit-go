package cli

import (
	"errors"
	"fmt"
	"go/ast"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/tools/go/packages"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
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
	all     bool   // analyze only: ignore the scope
	module  string // limit to one module of a folder that holds several (a module path or a folder name)
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
	f.StringVar(&sf.module, "module", "", "when the folder holds several Go modules, only this `module` (its module path, or its folder name)")
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
	// handler is set when the workflow is the code of an update handler or
	// validator that another workflow registers (see model.FindHandlers).
	handler *model.Handler
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
	handlers      []model.Handler // what the mapped workflows register (the handler table)
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
	if sf.module != "" {
		if res, err = onlyModule(res, sf.module); err != nil {
			return nil, userError("%s", err)
		}
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
	opts := model.Options{Helpers: helperMode(cfg), Wrappers: wrapperSpecs(cfg), DirectReturns: cfg != nil && cfg.Classification.DirectReturns == "step"}
	known := map[*ast.FuncDecl]string{}
	for _, f := range fns {
		if f.Auto {
			known[f.Workflow.Func] = f.Workflow.Name
		}
	}
	for _, e := range applied.InScope {
		g, err := model.BuildWith(e.Workflow, opts)
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
		// The update handlers and validators the workflow registers are small
		// workflows of their own (their code has paths and branches).
		for _, h := range model.FindHandlers(e.Workflow, known) {
			if h.Workflow != nil {
				hg, err := model.BuildWith(*h.Workflow, opts)
				var hu *model.UnsupportedError
				var hp *model.PragmaError
				switch {
				case errors.As(err, &hu) || errors.As(err, &hp):
					h.Why, h.Workflow = "not analysed: "+err.Error(), nil
				case err != nil:
					return nil, err
				default:
					hc := h
					out.mapped = append(out.mapped, recordable{wf: *h.Workflow, graph: hg, hash: hg.CodeHash(), handler: &hc})
				}
			}
			out.handlers = append(out.handlers, h)
		}
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
		merged.Modules = append(merged.Modules, r.Modules...)
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

// notMappedUnread lists the workflows in scope that PathKit could not map at
// all (an unsupported construct) for the "could not fully read" panel.
func notMappedUnread(sc *scoped) []coverage.UnreadEntry {
	var out []coverage.UnreadEntry
	for _, n := range sc.notAnalyzable {
		var u *model.UnsupportedError
		if !errors.As(n.err, &u) {
			continue
		}
		out = append(out, coverage.UnreadEntry{Workflow: n.name, File: load.DisplayPath(u.Pos.Filename), Line: u.Pos.Line, Kind: coverage.UnreadNotMapped,
			Call: u.Construct, Effect: "the whole workflow is left out of the report: " + u.Reason + selectorAdvice(u.Reason)})
	}
	return out
}

// helperMode is the config's classification.helpers.mode (auto when unset).
func helperMode(cfg *scope.Config) model.HelperMode {
	if cfg == nil {
		return model.HelpersAuto
	}
	switch cfg.Classification.HelperMode {
	case "inline":
		return model.HelpersInline
	case "summarize":
		return model.HelpersSummarize
	}
	return model.HelpersAuto
}

// wrapperSpecs are the config's classification.activity_wrappers for the model.
func wrapperSpecs(cfg *scope.Config) []model.Wrapper {
	if cfg == nil {
		return nil
	}
	var out []model.Wrapper
	for _, w := range cfg.Classification.ActivityWrappers {
		out = append(out, model.Wrapper{Function: w.Function, Kind: w.Kind, Label: w.Label})
	}
	return out
}

// onlyModule keeps the packages of one module of a folder that holds several:
// named by its module path or by its folder (its name, or its path below the
// folder given).
func onlyModule(res *load.Result, want string) (*load.Result, error) {
	if len(res.Modules) == 0 {
		return nil, fmt.Errorf("--module %s: the folder holds one Go module, not several; leave --module out", want)
	}
	matches := func(dir, path string) bool {
		return want == path || want == filepath.Base(dir) || want == filepath.ToSlash(dir) || strings.HasSuffix(filepath.ToSlash(dir), "/"+strings.TrimPrefix(filepath.ToSlash(want), "./"))
	}
	out := &load.Result{File: res.File}
	for _, p := range res.Packages {
		if p.Module != nil && matches(p.Module.Dir, p.Module.Path) {
			out.Packages = append(out.Packages, p)
		}
	}
	var names []string
	for _, m := range res.Modules {
		names = append(names, filepath.Base(m))
		for _, p := range out.Packages {
			if p.Module != nil && p.Module.Dir == m {
				out.Modules = append(out.Modules, m)
				break
			}
		}
	}
	if len(out.Packages) == 0 {
		return nil, fmt.Errorf("--module %s matches none of the modules found (%s)", want, strings.Join(names, ", "))
	}
	return out, nil
}

// moduleOf is the module path a workflow's package belongs to.
func moduleOf(wf discover.Workflow) string {
	if wf.Pkg != nil && wf.Pkg.Module != nil {
		return wf.Pkg.Module.Path
	}
	return ""
}

// moduleIfSeveral is the workflow's module path when the folder holds several
// modules, so reports name it; "" otherwise (nothing changes for one module).
func moduleIfSeveral(sc *scoped, wf discover.Workflow) string {
	if len(sc.res.Modules) > 1 {
		return moduleOf(wf)
	}
	return ""
}

// selectorAdvice adds, to the reason a selector cannot be mapped, why that
// shape is out of reach and what to write instead (the model's own message is
// frozen; this is the explanation next to it in the "could not fully read"
// panel).
func selectorAdvice(reason string) string {
	switch {
	case strings.Contains(reason, "Select calls; PathKit maps exactly one"):
		return " — a second Select on the same selector only sees the exits the first one did not take, which PathKit cannot draw; use one selector for each wait"
	case strings.Contains(reason, "must be kept in a variable created with workflow.NewSelector"):
		return " — if it is built and used in one chained expression, NewSelector(…).AddFuture(…).Select(…), write sel := workflow.NewSelector(ctx), then the Add… calls, then sel.Select(ctx), and PathKit can map it"
	case strings.Contains(reason, "is inside a loop; PathKit needs every Add"):
		return " — an Add… call in a loop adds a number of exits that is only known when the workflow runs, so no fixed map of them can be drawn"
	}
	return ""
}
