// Package model is PathKit's single source of truth for paths (CLAUDE.md
// D5). It decides which statements are junctions, gives every junction and
// exit its ID, and lists the paths. Every other part of PathKit (the
// printer, and from M3 the recorder and the trace matcher) gets IDs from
// here and never makes its own.
package model

import (
	"go/ast"
	"go/token"
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
	// loop rule in CLAUDE.md D3.
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

	// order is where the decision starts in the source, for numbering:
	// Stmt's position, except for a Selector, which starts at its first
	// Add… call (so it comes before any junction inside its callbacks).
	order token.Pos
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
}

// Dead reports a road that ends in a panic or os.Exit: not a path at all.
func (t Target) Dead() bool { return t.Junction == nil && t.Retry == nil && t.End == endDead }

// IsEnd reports a target that is an end station.
func (t Target) IsEnd() bool { return t.Junction == nil && t.Retry == nil && t.End != endDead }

// Graph is the junction map of one workflow function.
type Graph struct {
	Workflow  string
	Junctions []*Junction // in ID order (J1, J2, ...)
	Start     Target

	byStmt map[ast.Node]*Junction
	byID   map[string]*Exit
}

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
