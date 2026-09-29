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
type matchError struct {
	name    string
	matches []string // what it matched (0, or more than 1)
	known   []string // every name it could have meant, for the hint
}

func (e *matchError) of(kind, where string) string {
	if len(e.matches) == 0 {
		return withHint(fmt.Sprintf("matches no %s in %s", kind, where), e.name, e.known)
	}
	return fmt.Sprintf("matches %d %ss (%s); write it with its package, e.g. %q",
		len(e.matches), kind, strings.Join(e.matches, ", "), e.matches[0])
}

// pick finds the one function among fns (those keep accepts) that name
// means. Accepted forms: Func, pkg.Func, Type.Method, pkg.Type.Method and
// pkg.(*Type).Method.
func pick(fns []discover.Function, name string, keep func(discover.Function) bool) (discover.Function, *matchError) {
	want := normalize(name)
	var found []discover.Function
	var known []string
	for _, f := range fns {
		if !keep(f) {
			continue
		}
		known = append(known, f.Workflow.Name)
		if nameMatches(f.Workflow.Name, want) {
			found = append(found, f)
		}
	}
	if len(found) != 1 {
		var names []string
		for _, f := range found {
			names = append(names, f.Workflow.Name)
		}
		return discover.Function{}, &matchError{name: name, matches: names, known: known}
	}
	return found[0], nil
}

func normalize(name string) string {
	return strings.NewReplacer("(*", "", ")", "").Replace(strings.TrimSpace(name))
}

// nameMatches: full is "pkg.Func" or "pkg.Type.Method"; want is any
// accepted form of a name (already normalized).
func nameMatches(full, want string) bool {
	return full == want || strings.HasSuffix(full, "."+want)
}

// MatchName picks the one name in known (full workflow names) that name
// means, with the same forms and errors as the scope config, including
// the "did you mean" hint. flag names the option in the error.
func MatchName(known []string, name, flag, where string) (string, error) {
	want := normalize(name)
	var found []string
	for _, k := range known {
		if nameMatches(k, want) {
			found = append(found, k)
		}
	}
	if len(found) == 1 {
		return found[0], nil
	}
	e := &matchError{name: name, matches: found, known: known}
	return "", fmt.Errorf("%s %q %s", flag, name, e.of("workflow", where))
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
