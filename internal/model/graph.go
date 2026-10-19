// Package model is PathKit's single source of truth for paths (docs/DESIGN.md
// D5). It decides which statements are junctions, gives every junction and
// exit its ID, and lists the paths. Every other part of PathKit (the
// printer, the recorder and the trace matcher) gets IDs from
// here and never makes its own.
package model

import (
	"go/ast"
	"go/token"
	"go/types"
)

// EdgeID names one exit of one junction, like "J2.failure". Its field is
// hidden, so only this package can create one; other packages can only
// read and print them.
type EdgeID struct{ s string }

func (e EdgeID) String() string { return e.s }

// JunctionKind says what kind of decision a junction is.
type JunctionKind int

const (
	// PlainIf is an ordinary if statement: exits "true" and "false".
	PlainIf JunctionKind = iota
	// ErrCheck is an if err != nil (or == nil) right after a Temporal call:
	// exits "failure" and "success".
	ErrCheck
	// Switch is a switch or type switch: one exit per case ("case x"),
	// plus "default" (written, or added when none is written).
	Switch
	// Loop is a for or range loop with a Temporal call or a junction in
	// it: exits "iterate" (go into the body) and "exit" (the condition is
	// false, or the range ran out; a "for {}" has none), plus its Retry
	// edge (the body finished and the loop goes round again). See the
	// loop rule in docs/DESIGN.md D3.
	Loop
	// Selector is a workflow.Selector's Select call: one exit per Add…
	// call ("signal \"name\"", "timeout", "activity Name", "default", ...).
	// Each exit's road is that callback's body.
	Selector
	// WaitResult is an if on the "did it arrive?" result of
	// AwaitWithTimeout (exits "signaled", "timeout") or of
	// ReceiveWithTimeout / ReceiveAsync ("received", "not received").
	WaitResult
)

// Junction is one decision point in a workflow.
type Junction struct {
	ID    string // "J1", "J2", ... in source order
	Kind  JunctionKind
	Label string   // "if x > 0", "ChargeCard (activity)", "switch status", "for i < n"
	Stmt  ast.Node // the *ast.IfStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.ForStmt, *ast.RangeStmt, or the Select call's *ast.ExprStmt
	Pos   token.Position
	Exits []*Exit // display order: true before false, failure before success, cases in source order, iterate before exit
	// Retry is a loop's back-edge, "J1.retry". It is not in Exits because
	// it is not chosen at the loop's head: the end of the body leads to it.
	Retry *Exit

	// Site is the copy of a helper's code this junction is in (see Site);
	// nil for a decision in the workflow function itself.
	Site *Site

	// order is where the decision starts in the source, for numbering:
	// Stmt's position, except for a Selector, which starts at its first
	// Add… call (so it comes before any junction inside its callbacks).
	// A decision inside a helper starts with the positions of the calls
	// that lead to it, so it is numbered right after its call site.
	order []token.Pos

	// Facts are notes the extractor stuck on this junction (see facts.go).
	// They never change the junction, its exits, or any ID.
	Facts []Fact

	// viaHelper: the junction exists only because of what newer versions read
	// (the decision of a "return <call>" is one too): it exists only because PathKit follows a helper
	// function (the error check on a helper's result, or a loop with a call
	// of a helper in it). Before helpers were followed it was not one, so
	// path IDs from then do not have it (see Path.LegacyStableIDs).
	viaHelper bool

	// call is where the Temporal call this decision reads is (the activity,
	// child workflow or Nexus operation that is started); nil if not known.
	call *token.Position

	// stable is the junction's description for stable path IDs (see
	// assignStable): its label, made unique within the workflow.
	stable string
}

// Exit is one way out of a junction.
type Exit struct {
	ID       EdgeID
	Label    string // "true", "false", "failure", "success", "case x", "default", "iterate", "exit", "retry"
	Junction *Junction
	// Road is the statement this exit leads into, where the recorder
	// inserts its call: the if's body or else branch (nil when there is
	// no else), or the switch's case clause (nil for a default PathKit
	// added because none is written). Loop exits have no Road: the
	// recorder handles loops as a whole.
	Road ast.Stmt
	To   Target
}

// EndKind is how a path finishes.
type EndKind string

// The ways a path can end.
const (
	EndCompleted      EndKind = "completed"
	EndFailed         EndKind = "failed"
	EndContinuedAsNew EndKind = "continued-as-new"
	EndUnknown        EndKind = "" // PathKit can't tell; printed as plain "End"
	endDead           EndKind = "dead"
)

// String is the end station's printed name, e.g. "End (failed)".
func (k EndKind) String() string {
	if k == EndUnknown {
		return "End"
	}
	return "End (" + string(k) + ")"
}

// Target is where a road leads: a junction, an end station, or a loop's
// retry edge (the body finished; the loop goes round again). A dead
// target (after a panic) is not a path at all.
type Target struct {
	Junction *Junction
	End      EndKind
	// Retry, when set, means "take this loop's retry edge (a step on the
	// path), then continue at Retry.To, the loop's head".
	Retry *Exit
	// Compensation: the road to this target registers a saga
	// compensation defer. It is a note on the path, never a branch.
	Compensation bool
	// Nodes are the statements and expressions a run executes, in order,
	// on the way to this target (from the start of the road to the
	// junction, retry edge or end station). They are extra information for
	// the fact engine (internal/facts); nothing in the graph, in an ID or
	// in a path depends on them. A SkipMark among them says the walk
	// assumed one side of an if or a loop that it did not decide.
	Nodes []ast.Node
	// Only, when set on a target at a junction, says the road can take just
	// this one exit there: the code before it already decided (a helper
	// that returned nil, so the caller's "if err != nil" cannot be true).
	// The step is still recorded and still a step of the path. It belongs
	// to the target, not to the junction: the same junction (one decision in
	// the code) can be reached by a road that decided it and by one that did not.
	Only *Exit
}

// SkipMark is a pseudo node in Target.Nodes: the walk passes an if that is
// not a junction (an error check it assumes succeeds, an if marked
// //pathkit:ignore) or a loop that is not a junction. A real run may go the
// other way or round the loop any number of times, so whatever Stmt assigns
// is unknown afterwards, and the nodes inside Stmt that follow the mark are
// not facts.
type SkipMark struct{ Stmt ast.Stmt }

// Pos and End make SkipMark an ast.Node; it covers its statement.
func (m SkipMark) Pos() token.Pos { return m.Stmt.Pos() }
func (m SkipMark) End() token.Pos { return m.Stmt.End() }

// EnterMark is a pseudo node in Target.Nodes: the walk goes into the copy of
// a helper's code at Site. Every variable declared in that code (its
// parameters and locals) starts new there, whatever an earlier copy left
// in them.
type EnterMark struct{ Site *Site }

// Pos and End make EnterMark an ast.Node; it covers the helper.
func (m EnterMark) Pos() token.Pos { return m.Site.Body.Pos() }
func (m EnterMark) End() token.Pos { return m.Site.Body.End() }

// Dead reports a road that ends in a panic or os.Exit: not a path at all.
func (t Target) Dead() bool { return t.Junction == nil && t.Retry == nil && t.End == endDead }

// IsEnd reports a target that is an end station.
func (t Target) IsEnd() bool { return t.Junction == nil && t.Retry == nil && t.End != endDead }

// Graph is the junction map of one workflow function.
type Graph struct {
	Workflow  string
	Junctions []*Junction // in ID order (J1, J2, ...)
	Start     Target
	// Marks are the //pathkit:untestable comments in the function.
	Marks []Mark

	byStmt    map[ast.Node]*Junction // the workflow function's own junctions
	byID      map[string]*Exit
	sites     []*Site
	stages    []Stage
	stagesFor int
	stageOf   map[*Junction]int

	// info, fn and fset are what the fact engine needs to read the code.
	info *types.Info
	fn   *ast.FuncDecl
	fset *token.FileSet
	pkg  *types.Package

	trusted func(ast.Expr) bool

	// units are the helpers summarized instead of copied into the map (see
	// HelperUnit); summarized says the graph was built that way on purpose,
	// and summarizedWhy gives the reason when PathKit chose it.
	units         []*HelperUnit
	unitByID      map[string]*Exit
	summarized    bool
	summarizedWhy string
	// inline gives the map of the same workflow with the helpers copied in
	// (only on a summarized graph); traces recorded against it still count.
	inline func() *Graph
	// directReturnSteps: some "return <call>" is a decision on this map.
	directReturnSteps bool

	// unread are the places PathKit did not follow (see Unread).
	unread []Unread
}

// TrustedConstant reports whether expr is a constant whose value is the
// same on every machine and in every build (see extractConst): only then
// may a claim rest on it.
func (g *Graph) TrustedConstant(expr ast.Expr) bool {
	return g.trusted != nil && g.trusted(expr)
}

// TypesInfo is the type information of the workflow's package.
func (g *Graph) TypesInfo() *types.Info { return g.info }

// Func is the workflow function.
func (g *Graph) Func() *ast.FuncDecl { return g.fn }

// Types is the workflow's package, for writing type names.
func (g *Graph) Types() *types.Package { return g.pkg }

// FileSet is the file set the workflow's positions belong to.
func (g *Graph) FileSet() *token.FileSet { return g.fset }

// ExitFor returns the ID of the exit with this label ("true", "failure",
// "case x", ...) of the junction built from stmt. The recorder uses it to
// find the ID to record at each exit.
func (g *Graph) ExitFor(stmt ast.Node, label string) (EdgeID, bool) {
	j, ok := g.byStmt[stmt]
	if !ok {
		return EdgeID{}, false
	}
	for _, e := range j.Exits {
		if e.Label == label {
			return e.ID, true
		}
	}
	if j.Retry != nil && j.Retry.Label == label {
		return j.Retry.ID, true
	}
	return EdgeID{}, false
}

// LookupEdge turns a recorded string like "J2.failure" back into its exit.
func (g *Graph) LookupEdge(s string) (*Exit, bool) {
	e, ok := g.byID[s]
	return e, ok
}
