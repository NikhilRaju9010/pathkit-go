package scope

import (
	"fmt"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
)

// The reasons PathKit gives for exclusions it makes itself.
const (
	ReasonNotIncluded = "not in include list"
	ReasonExcludeFlag = "excluded by --exclude flag"
)

// Flags are the one-off command-line options (D8: they "stay for one-off
// runs").
type Flags struct {
	Include []string // --include: replaces the config's include list; nil when not given
	Exclude []string // --exclude: each excluded with ReasonExcludeFlag
}

// Rules is a resolved scope: every name in the config and the flags has
// been checked and turned into a full workflow name ("orders.OrderWorkflow").
// A nil *Rules means no scope: every automatically found workflow counts.
type Rules struct {
	restrict bool              // an include list was given: only its workflows count
	included map[string]bool   // full names listed in include
	added    map[string]bool   // included functions the automatic rule doesn't find
	excluded map[string]string // full name -> reason
}

// Resolve checks every include and exclude name against fns (every
// function in the packages the names are checked against, see
// discover.All) and resolves them. where names those packages in errors.
// The rules (CLAUDE.md D8):
//
//   - include narrows the scope to the workflows it lists: only they
//     count. Every automatically found workflow it leaves out is excluded
//     with the reason "not in include list". A listed function the
//     automatic rule misses (unexported, or no error result) is added if
//     its first parameter is workflow.Context, and labelled "added by
//     config"; if not, that is an error. An empty list is an error.
//   - exclude takes workflows out, each with its reason; --exclude adds
//     exclusions with the reason "excluded by --exclude flag".
//   - Any name that matches nothing, or matches more than one workflow,
//     is an error; so is a workflow named in both lists.
func Resolve(cfg *Config, flags Flags, fns []discover.Function, where string) (*Rules, error) {
	if cfg == nil && flags.Include == nil && len(flags.Exclude) == 0 {
		return nil, nil
	}
	r := &Rules{included: map[string]bool{}, added: map[string]bool{}, excluded: map[string]string{}}
	cfgErr := func(format string, args ...any) error {
		return fmt.Errorf("%s: %s", cfg.Path, fmt.Sprintf(format, args...))
	}
	flagErr := func(format string, args ...any) error { return fmt.Errorf(format, args...) }

	include, includeErr, includeWord := []string(nil), cfgErr, "include"
	if cfg != nil && cfg.HasInclude {
		include = cfg.Include
	}
	if flags.Include != nil {
		include, includeErr, includeWord = flags.Include, flagErr, "--include"
		if len(include) == 0 {
			return nil, flagErr("--include is empty: leave it out to include every workflow, or list the ones you want")
		}
	}
	if include != nil {
		r.restrict = true
		for _, name := range include {
			f, err := pick(fns, name, func(discover.Function) bool { return true })
			if err != nil {
				return nil, includeErr("%s %q %s", includeWord, name, err.of("function", where))
			}
			if !f.TakesContext {
				return nil, includeErr("%s %q is not a workflow: its first parameter is not workflow.Context", includeWord, name)
			}
			r.included[f.Workflow.Name] = true
			if !f.Auto {
				r.added[f.Workflow.Name] = true
			}
		}
	}

	isWorkflow := func(f discover.Function) bool { return f.Auto || r.added[f.Workflow.Name] }
	exclude := func(name, reason string, fail func(string, ...any) error, word string) error {
		f, err := pick(fns, name, isWorkflow)
		if err != nil {
			return fail("%s %q %s", word, name, err.of("workflow", where))
		}
		full := f.Workflow.Name
		if r.included[full] {
			return fail("%q is both included and excluded", name)
		}
		r.excluded[full] = reason
		return nil
	}
	if cfg != nil {
		for _, e := range cfg.Exclude {
			if err := exclude(e.Name, e.Reason, cfgErr, "exclude"); err != nil {
				return nil, err
			}
		}
	}
	for _, name := range flags.Exclude {
		if err := exclude(name, ReasonExcludeFlag, flagErr, "--exclude"); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// matchError says why a name didn't pick exactly one function.
type matchError struct{ matches []string }

func (e *matchError) of(kind, where string) string {
	if len(e.matches) == 0 {
		return fmt.Sprintf("matches no %s in %s", kind, where)
	}
	return fmt.Sprintf("matches %d %ss (%s); write it with its package, e.g. %q",
		len(e.matches), kind, strings.Join(e.matches, ", "), e.matches[0])
}

// pick finds the one function among fns (those keep accepts) that name
// means. Accepted forms: Func, pkg.Func, Type.Method, pkg.Type.Method and
// pkg.(*Type).Method.
func pick(fns []discover.Function, name string, keep func(discover.Function) bool) (discover.Function, *matchError) {
	want := strings.NewReplacer("(*", "", ")", "").Replace(strings.TrimSpace(name))
	var found []discover.Function
	for _, f := range fns {
		full := f.Workflow.Name
		if keep(f) && (full == want || strings.HasSuffix(full, "."+want)) {
			found = append(found, f)
		}
	}
	if len(found) != 1 {
		var names []string
		for _, f := range found {
			names = append(names, f.Workflow.Name)
		}
		return discover.Function{}, &matchError{names}
	}
	return found[0], nil
}

// Entry is one workflow in scope.
type Entry struct {
	Workflow      discover.Workflow
	AddedByConfig bool // in scope only because include named it
}

// Excluded is one workflow out of scope, with the reason.
type Excluded struct {
	Name   string
	Reason string
}

// Result is the scope applied to the functions a command loaded.
type Result struct {
	InScope  []Entry
	Excluded []Excluded
}

// Apply sorts fns (the functions of the packages a command loaded) into
// in scope and excluded. With nil rules every automatically found
// workflow is in scope, exactly as without a config.
func (r *Rules) Apply(fns []discover.Function) Result {
	var res Result
	for _, f := range fns {
		name := f.Workflow.Name
		switch {
		case r == nil:
			if f.Auto {
				res.InScope = append(res.InScope, Entry{Workflow: f.Workflow})
			}
		case !f.Auto && !r.added[name]:
			// not a workflow, and not added by include
		case r.excluded[name] != "":
			res.Excluded = append(res.Excluded, Excluded{Name: name, Reason: r.excluded[name]})
		case r.restrict && !r.included[name]:
			res.Excluded = append(res.Excluded, Excluded{Name: name, Reason: ReasonNotIncluded})
		default:
			res.InScope = append(res.InScope, Entry{Workflow: f.Workflow, AddedByConfig: r.added[name]})
		}
	}
	return res
}
