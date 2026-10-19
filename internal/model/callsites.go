package model

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"sort"
	"strings"
)

// Kinds of failure a call can end in, when the code tells them apart
// (docs/CONSTRUCTS.md): PathKit does not make a path outcome of each; it lists
// the ones the code branches on next to the call, and whether a test took them.
const (
	FailureApplication       = "application error"
	FailureNonRetryable      = "non-retryable application error"
	FailureTimeout           = "timeout"
	FailureCancelled         = "cancelled"
	callKindActivity         = "activity"
	callKindLocalActivity    = "local activity"
	callKindChildWorkflow    = "child workflow"
	callKindNexusOperation   = "nexus operation"
	maxFailureKindSearchStep = 4
)

// A CallSite is one activity, child workflow or Nexus operation that a
// workflow starts and whose result it checks: where it is, what it is called,
// the decision on its result, and the kinds of failure the code goes on to
// tell apart.
type CallSite struct {
	Workflow string
	File     string
	Line     int
	// Kind is "activity", "local activity", "child workflow" or "nexus operation".
	Kind string
	Name string
	// Junction is the decision on the call's result (success / failure).
	Junction *Junction
	// Failures are the kinds of failure the code branches on after the call
	// failed, each with the exit of the decision that takes it.
	Failures []FailureKind
}

// FailureKind is one kind of failure the code tells apart, and the exit that
// is taken when the failure is of that kind.
type FailureKind struct {
	Kind string
	Exit *Exit
}

var callLabel = regexp.MustCompile(`^(.+) \((activity|local activity|child workflow|nexus operation)\)$`)

// CallSites lists the checked calls of the workflow: its own, and those in
// the helpers it follows (a helper's call is listed once however many copies of
// the helper there are). In order of position.
func (g *Graph) CallSites() []CallSite {
	var out []CallSite
	byPos := map[string]int{}
	for _, j := range g.AllJunctions() {
		if j.Kind != ErrCheck || j.call == nil {
			continue
		}
		m := callLabel.FindStringSubmatch(j.Label)
		if m == nil {
			continue
		}
		key := j.call.Filename + ":" + itoa(j.call.Line) + ":" + m[1]
		if i, ok := byPos[key]; ok {
			// another copy of the same helper code: the sites are one
			out[i].Failures = mergeFailures(out[i].Failures, g.failureKinds(j))
			continue
		}
		byPos[key] = len(out)
		out = append(out, CallSite{Workflow: g.Workflow, File: j.call.Filename, Line: j.call.Line, Kind: m[2], Name: m[1], Junction: j, Failures: g.failureKinds(j)})
	}
	sort.SliceStable(out, func(i, k int) bool {
		if out[i].File != out[k].File {
			return out[i].File < out[k].File
		}
		return out[i].Line < out[k].Line
	})
	return out
}

func mergeFailures(a, b []FailureKind) []FailureKind {
	for _, f := range b {
		dup := false
		for _, x := range a {
			dup = dup || x.Kind == f.Kind
		}
		if !dup {
			a = append(a, f)
		}
	}
	return a
}

// failureKinds walks the decisions right after the failure exit of j and
// reads the ones that tell failures apart: errors.As to a Temporal error type,
// temporal.IsTimeoutError, temporal.IsCanceledError and a NonRetryable check.
func (g *Graph) failureKinds(j *Junction) []FailureKind {
	var out []FailureKind
	seen := map[*Junction]bool{}
	var walk func(t Target, depth int)
	walk = func(t Target, depth int) {
		if t.Junction == nil || depth > maxFailureKindSearchStep || seen[t.Junction] {
			return
		}
		x := t.Junction
		seen[x] = true
		if x.Kind == PlainIf {
			if ifs, ok := x.Stmt.(*ast.IfStmt); ok {
				for _, k := range g.kindsOfCondition(ifs.Cond) {
					for _, e := range x.Exits {
						if e.Label == "true" {
							out = mergeFailures(out, []FailureKind{{Kind: k, Exit: e}})
						}
					}
				}
			}
		}
		for _, e := range x.Exits {
			walk(e.To, depth+1)
		}
	}
	for _, e := range j.Exits {
		if e.Label == "failure" {
			walk(e.To, 0)
		}
	}
	return out
}

// kindsOfCondition reads a condition for the failure kinds it takes: every one
// of them when the condition is true.
func (g *Graph) kindsOfCondition(cond ast.Expr) []string {
	var kinds []string
	add := func(k string) {
		for _, x := range kinds {
			if x == k {
				return
			}
		}
		kinds = append(kinds, k)
	}
	ast.Inspect(cond, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := ""
		switch f := ast.Unparen(call.Fun).(type) {
		case *ast.SelectorExpr:
			name = f.Sel.Name
			if id, ok := f.X.(*ast.Ident); ok {
				name = id.Name + "." + name
			}
		case *ast.Ident:
			name = f.Name
		}
		switch name {
		case "temporal.IsTimeoutError":
			add(FailureTimeout)
		case "temporal.IsCanceledError":
			add(FailureCancelled)
		case "temporal.IsApplicationError":
			add(FailureApplication)
		case "errors.As":
			if len(call.Args) == 2 {
				if t := g.info.TypeOf(call.Args[1]); t != nil {
					if p, ok := types.Unalias(t).(*types.Pointer); ok {
						switch typeName(p.Elem()) {
						case "ApplicationError":
							add(FailureApplication)
						case "TimeoutError":
							add(FailureTimeout)
						case "CanceledError":
							add(FailureCancelled)
						}
					}
				}
			}
		}
		if strings.HasSuffix(name, ".NonRetryable") {
			add(FailureNonRetryable)
		}
		return true
	})
	return kinds
}

// typeName is the name of a (pointer to a) named type, without its package.
func typeName(t types.Type) string {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := types.Unalias(t).(*types.Named); ok {
		return n.Obj().Name()
	}
	return ""
}

// callPos is where the call that a junction decides on is: the activity, child
// workflow or Nexus operation that is started, found from the error variable
// the check reads (its nearest assignment, then back to the Execute call).
func (b *builder) callPos(obj types.Object, ifs *ast.IfStmt) *token.Position {
	rhs, _, ok := b.nearestAssign(obj, ifs.Cond.Pos())
	if !ok {
		return nil
	}
	call, ok := ast.Unparen(rhs).(*ast.CallExpr)
	if !ok {
		return nil
	}
	pos := call
	if f := b.calledFunc(call); isSDKMethod(f, []string{"Future"}, "Get") {
		if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
			if o := b.originCall(sel.X, call, 0); o != nil {
				pos = o
			}
		}
	}
	p := b.fset.Position(pos.Pos())
	return &p
}
