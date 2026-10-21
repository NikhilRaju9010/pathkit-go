// Package facts is PathKit's fact engine (docs/DESIGN.md D14). It walks one
// listed path step by step, keeping a small notebook of things it is sure
// of about the workflow's local values ("id is the empty string", "ok is
// false", "p is not nil"). When a decision on the path asks a question the
// notebook already answers, and the path takes the other answer, the path is
// impossible, and the notebook entries that led there are the proof.
//
// The engine is careful in one direction only. Whenever it cannot be sure
// (an address is taken, a closure writes the value, a loop may change it, a
// call it cannot see into), it forgets; forgetting can only make a path
// look possible, never impossible. Everything it does not understand is
// "unknown", and unknown never produces a claim.
//
// It reads the graph internal/model builds and changes nothing in it: the
// paths, their ids and their stable ids are exactly the same with or
// without the engine.
package facts

// Kind says which label a claim gets.
type Kind string

const (
	// Contradictory: the same value was tested earlier on the path (and
	// not changed since), and this decision gives the opposite answer.
	Contradictory Kind = "contradictory"
	// ConstantCondition: the answer follows from declarations and
	// constants alone, with no decision in between.
	ConstantCondition Kind = "constant-condition"
	// ValueFlow: the answer follows from earlier decisions, assignments,
	// copies or early returns.
	ValueFlow Kind = "value-flow"
	// ImpossibleType: a type switch case, or a type assertion, that no type
	// the value can hold takes.
	ImpossibleType Kind = "impossible-type"
	// ImpossibleNil: "v == nil" for an interface that holds a typed value
	// (a typed pointer, even a nil one, is never a nil interface).
	ImpossibleNil Kind = "impossible-nil"
	// LoopExit: leaving a loop before its first round, when the loop always
	// has one.
	LoopExit Kind = "impossible-loop-exit"
)

// AssumptionJSON names the assumption a claim rests on when it relies on
// Temporal's default JSON converter.
const AssumptionJSON = "default-json-converter"

// AssumptionInterceptor names the assumption a claim rests on when it keeps
// facts about a slice or map across a call that encodes its arguments:
// no workflow interceptor in the module sees the original value.
const AssumptionInterceptor = "no-custom-interceptor"

// ProofStep is one plain sentence of a proof. Step is the 1-based step of
// the path the sentence is about (0 when it is about code between steps or
// is the closing sentence); Line is the source line it points at (0 for the
// closing sentence); File is the base name of that file.
type ProofStep struct {
	Step int
	File string
	Line int
	Text string
}

// Claim says a path is impossible.
type Claim struct {
	// Step is the 1-based step of the path whose outcome cannot happen.
	Step        int
	Kind        Kind
	Explanation string
	Proof       []ProofStep
	// Assumptions are the named assumptions the claim rests on.
	Assumptions []string
	// Uses is the last step of the path the claim's reasoning depends on:
	// its own decision, the decisions its facts were learned from, and the
	// road after the last step done when each fact was learned. Local
	// distrust of a trace that leaves the map after step d-1 applies to a
	// claim only when Uses >= d.
	Uses int
}

// Suspect says an uncovered path asks two questions about the same value
// and PathKit had to forget what it knew in between, so it cannot tell
// whether the path is possible.
type Suspect struct {
	First, Second int    // the two steps (1-based)
	Detail        string // which two steps, and why PathKit lost track
	Explanation   string // Detail plus what it means; also the last proof sentence
	Proof         []ProofStep
	// Exit and Group are set for an unknown-input-type suspect: the edge id
	// of the type switch case the path takes, and the switch ("file:line")
	// it belongs to.
	Exit      string
	Group     string
	GroupLine int
}

// Result is what the engine found on one path. Both are nil for most paths.
type Result struct {
	Claim   *Claim
	Suspect *Suspect
	// Unknown: the path takes a type switch case on a value whose origin
	// PathKit cannot follow (a candidate for unknown-input-type).
	Unknown *Suspect
	// Defect: the path takes a case, or a comparison, that no value can satisfy
	// only because the value is a pointer and the code tests the pointed-to type
	// or value. The path is NOT called impossible.
	Defect *Suspect
}
