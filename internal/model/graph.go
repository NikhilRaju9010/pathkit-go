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
)

// Junction is one decision point in a workflow.
type Junction struct {
	ID    string // "J1", "J2", ... in source order
	Kind  JunctionKind
	Label string   // "if x > 0", "ChargeCard (activity)", "switch status"
	Stmt  ast.Node // the *ast.IfStmt, *ast.SwitchStmt or *ast.TypeSwitchStmt
	Pos   token.Position
	Exits []*Exit // display order: true before false, failure before success, cases in source order
}

// Exit is one way out of a junction.
type Exit struct {
	ID       EdgeID
	Label    string // "true", "false", "failure", "success", "case x", "default"
	Junction *Junction
	// Road is the statement this exit leads into, where the recorder
	// inserts its call: the if's body or else branch (nil when there is
	// no else), or the switch's case clause (nil for a default PathKit
	// added because none is written).
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

// Target is where a road leads: a junction, or an end station. A dead
// target (after a panic) is not a path at all.
type Target struct {
	Junction *Junction
	End      EndKind
}

// Dead reports a road that ends in a panic or os.Exit: not a path at all.
func (t Target) Dead() bool { return t.Junction == nil && t.End == endDead }

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
	return EdgeID{}, false
}

// LookupEdge turns a recorded string like "J2.failure" back into its exit.
func (g *Graph) LookupEdge(s string) (*Exit, bool) {
	e, ok := g.byID[s]
	return e, ok
}
