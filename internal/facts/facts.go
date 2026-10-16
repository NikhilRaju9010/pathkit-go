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
)

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
}

// Suspect says an uncovered path asks two questions about the same value
// and PathKit had to forget what it knew in between, so it cannot tell
// whether the path is possible.
type Suspect struct {
	First, Second int    // the two steps (1-based)
	Detail        string // which two steps, and why PathKit lost track
	Explanation   string // Detail plus what it means; also the last proof sentence
	Proof         []ProofStep
}

// Result is what the engine found on one path. Both are nil for most paths.
type Result struct {
	Claim   *Claim
	Suspect *Suspect
}
