package model

import (
	"go/ast"
	"go/types"
)

// Facts: notes the extractor sticks on junctions that already
// exist, such as "this if tests whether a signal channel was closed".
//
// A fact is read-only information for the detectors in internal/classify.
// The extractor runs after the graph is fully built and numbered (the same
// place //pathkit:untestable comments are attached), and it only appends to
// Junction.Facts. It cannot create, remove, renumber or relabel a junction
// or an exit, so no path, id or stableId can change. testdata/golden/model
// /snapshot.txt and TestFactsChangeNothing hold it to that.

// FactKind says what a fact is about.
type FactKind int

const (
	// MoreCheck: the if tests the "more" result of a channel receive.
	MoreCheck FactKind = iota + 1
	// ConstCondition: the compiler knows the condition's value.
	ConstCondition
	// VersionCheck: the if tests the result of workflow.GetVersion.
	VersionCheck
	// HistorySuggested: the if tests whether the SDK suggests
	// Continue-As-New.
	HistorySuggested
	// HistorySize: the if compares the history length or size with a
	// number (a weaker hint than HistorySuggested).
	HistorySize
	// SameValue: the if tests a value that other ifs of the workflow test
	// too, and the value cannot change in between (proved with SSA).
	SameValue
	// SameList: the range loop walks a list whose length is the same as
	// that of another range loop in the workflow.
	SameList
	// ListBuiltFrom: the range loop walks a list that another range loop
	// builds with one append per trip (or is that building loop). Two
	// junctions with equal Keys are such a pair.
	ListBuiltFrom
	// LoopFirstTripAlways: the for loop's condition is true the first time
	// it is tested, so the loop always runs at least once (Exit is "exit":
	// leaving without a trip is impossible).
	LoopFirstTripAlways
	// ValueKnown: when the if takes exit Exit, the value named by Key (a
	// bool field of a Get result) is known: false when Negated, else true.
	ValueKnown
)

// ChannelOrigin says where the channel behind a MoreCheck came from.
type ChannelOrigin string

const (
	// OriginSignal: workflow.GetSignalChannel(...WithOptions). Temporal
	// feeds it, and the workflow cannot close it.
	OriginSignal ChannelOrigin = "signal"
	// OriginOwn: workflow.NewChannel / NewBufferedChannel (named or not).
	// The workflow may close it itself.
	OriginOwn ChannelOrigin = "own"
	// OriginUnknown: PathKit could not follow the channel back to one
	// creating call (a parameter, a field, a helper, a variable assigned
	// more than once, ...). Never treated as impossible.
	OriginUnknown ChannelOrigin = "unknown"
)

// Fact is one note on a junction.
type Fact struct {
	Kind FactKind
	// Exit is the label of the exit where the fact holds ("true", "false"):
	// for MoreCheck, the exit taken when the channel is closed; for
	// ConstCondition, the exit that can never be taken; for VersionCheck,
	// the exit that is the old-version side; for HistorySuggested, the
	// exit taken when Continue-As-New is suggested.
	Exit string
	// Origin and Name describe the channel of a MoreCheck (Name is the
	// signal name when it is a constant).
	Origin ChannelOrigin
	Name   string
	// Detail is a short plain-English description for explanations.
	Detail string
	// Key names a value (SameValue) or a list length (SameList): two
	// junctions with equal Keys read the same thing. Negated is true when
	// the if tests the opposite of the value (if !valid).
	Key     string
	Negated bool
}

// factsEnabled lets a test run the builder without the extractor, to prove
// that facts change nothing.
var factsEnabled = true

// extractors are the fact families, each in its own file.
var extractors = []func(b *builder, g *Graph){
	extractMore,
	extractConst,
	extractVersion,
	extractHistory,
	extractSameValue,
	extractSameList,
	extractListBuiltFrom,
	extractLoopFirstTrip,
	extractGetValue,
}

func (b *builder) attachFacts(g *Graph) {
	if !factsEnabled {
		return
	}
	for _, x := range extractors {
		x(b, g)
	}
}

// singleDefinition follows a use of a local variable back to its value, but
// only when that is certain: the variable is set exactly once, by := or
// var x = ..., its address is never taken, and it is not declared outside
// the function literal the use is in (a closure may run at another time
// than the code around it). Anything else returns false.
func (b *builder) singleDefinition(use *ast.Ident) (ast.Expr, bool) {
	obj, _ := b.info.Uses[use].(*types.Var)
	if obj == nil || obj.IsField() || b.declaredOutsideItsClosure(obj, use) {
		return nil, false
	}
	ws, escaped := b.writesTo(obj)
	if escaped || len(ws) != 1 || !ws[0].define || ws[0].rhs == nil || ws[0].index != 0 {
		return nil, false
	}
	return ws[0].rhs, true
}
