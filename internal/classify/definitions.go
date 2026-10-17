package classify

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// DefinitionKind says what a Definition describes.
type DefinitionKind string

// The kinds of definition: a bucket, a reason label, or a comment.
const (
	KindBucket  DefinitionKind = "bucket"
	KindLabel   DefinitionKind = "label"
	KindComment DefinitionKind = "comment" // a //pathkit:... comment in the user's code
	// KindAssumption: something a claim rests on that PathKit does not check
	// by itself (and turns off when it sees it does not hold).
	KindAssumption DefinitionKind = "assumption"
)

// Definition explains one bucket, reason label or comment. This list is
// the only place the wording lives: the console report, the HTML glossary
// and the tests all read it, and a test fails if a label PathKit can print
// has no entry here.
type Definition struct {
	Kind                 DefinitionKind
	Name                 string
	Meaning              string // one line
	Why                  string
	Example              string
	PossibleInProduction string // "yes", "no", "no (assumed)", "not applicable"
	Bucket               Bucket // the bucket the label puts a path in ("" for a bucket or a comment)
	// Confidence is how sure the detector is: Certain or Likely for a
	// label; empty for a bucket or a comment, which no detector decides.
	Confidence Confidence
	// DecidedBy says who puts a path under the label: "detector", "config
	// rule" or "person" (empty for a bucket or a comment).
	DecidedBy string
	WhatToDo  string
	// NotDetectedYet is true for a label whose detector does not exist yet
	// (listed in LIMITATIONS.md). Nothing prints it today; it is here so the
	// definitions list and the detectors cannot drift apart: a test requires
	// every detector-decided label to have a detector, or to say so.
	NotDetectedYet bool
}

// notYet marks a label as "not detected yet".
func (d Definition) notYet() Definition {
	d.NotDetectedYet = true
	return d
}

// Definitions returns every definition, buckets first, then labels in the
// order of docs/DESIGN.md D13, then comments.
func Definitions() []Definition { return append([]Definition(nil), definitions...) }

// Lookup finds one definition by kind and name.
func Lookup(kind DefinitionKind, name string) (Definition, bool) {
	for _, d := range definitions {
		if d.Kind == kind && d.Name == name {
			return d, true
		}
	}
	return Definition{}, false
}

// MustLabel returns a label's definition and panics when there is none, so
// a detector can never print a label nobody defined.
func MustLabel(name string) Definition {
	d, ok := Lookup(KindLabel, name)
	if !ok {
		panic(fmt.Sprintf("classify: label %q has no definition", name))
	}
	return d
}

func bucket(b Bucket, meaning, why, example, inCoverage string) Definition {
	return Definition{Kind: KindBucket, Name: string(b), Meaning: meaning, Why: why, Example: example,
		PossibleInProduction: "not applicable", WhatToDo: inCoverage}
}

// label builds a label's definition. kind is "certain" or "likely" (a
// detector, with that confidence), "rule" (a config rule decides; certain
// once it matches) or "person" (a person decides, in the config or with a
// comment).
func label(name string, b Bucket, kind, meaning, why, example, possible, todo string) Definition {
	d := Definition{Kind: KindLabel, Name: name, Bucket: b, Meaning: meaning, Why: why,
		Example: example, PossibleInProduction: possible, WhatToDo: todo}
	switch kind {
	case "rule":
		d.Confidence, d.DecidedBy = Certain, "config rule"
	case "person":
		d.Confidence, d.DecidedBy = Certain, "person"
	default:
		d.Confidence, d.DecidedBy = Confidence(kind), "detector"
	}
	return d
}

var definitions = []Definition{
	// Buckets.
	bucket(Testable, "A normal test can make the code take this path.",
		"This is the default. A path stays here unless a detector, a rule or a person has a reason to move it. A path a real test ran is always here, because a real run proves it is possible.",
		"if order.Amount <= 0 is true.",
		"Counts in testable coverage. Write a test that makes the code take it."),
	bucket(CannotBeTested, "The path is impossible: no run, test or production, can take it.",
		"Some combinations of decisions contradict each other, or Temporal guarantees they cannot happen.",
		"A check that is false, then true later, when nothing changed in between.",
		"Left out of testable coverage, but still counted in overall coverage. Nothing to do; check the reason is right."),
	bucket(NeedsSpecialTest, "The path is possible in production, but a normal test can't reach it.",
		"It needs a replay of an old history, a huge history, or a deliberate panic.",
		"The old side of a workflow.GetVersion check.",
		"Left out of testable coverage and shown on its own line. Cover it with the special test named in its label."),
	bucket(NeedsReview, "PathKit is not sure, or its model can't judge.",
		"A person should look. Any unknown or unrecognized label also lands here. The goal is zero.",
		"Event order between two workflow.Go coroutines.",
		"Counted in testable coverage by default (strict). Review it, then mark it testable or untestable in the config."),

	// Labels: A. condition logic.
	label("contradictory", CannotBeTested, "certain", "The same variable gives opposite answers in one run, or two values cannot both hold.",
		"A run can't test the same unchanged value twice and get two answers.", "if ok { ... } later if !ok { ... } with ok unchanged.", "no",
		"Nothing, once you have checked the two conditions really read the same value."),
	label("constant-condition", CannotBeTested, "certain", "The condition is always true or always false.",
		"The compiler can see its value.", "if false { ... }", "no", "Remove the dead branch, or ignore the path."),
	label("value-flow", CannotBeTested, "certain", "An earlier assignment, copy or decision settles this check, and the path takes the other answer.",
		"PathKit followed the path step by step and kept a note of what each value holds. At this check the notes give one answer, and the path takes the other. It forgets a value whenever it cannot be sure (an address is taken, a closure writes it, a loop may change it), so it never claims this on a guess.", "id starts as \"\", an if ok { id = v } is skipped, and then if id == \"\" takes its false side.", "no",
		"Nothing, once you have read the proof (pathkit explain) and agree. If the code is wrong, fix it."),
	label("dead-code", CannotBeTested, "certain", "Code after a return, panic, Continue-As-New or similar.",
		"Nothing can reach it.", "A statement written after return.", "no", "Delete the code.").notYet(),
	label("either-or-wait", CannotBeTested, "certain", "A wait ends for exactly one of two reasons (timer or signal, or an AwaitWithTimeout condition or its timeout), so \"neither\" or \"both\" is impossible.",
		"The wait always ends one way.", "A timer-fired and a signal-arrived exit both taken on one wait.", "no", "Nothing.").notYet(),
	label("set-then-test", CannotBeTested, "rule", "An earlier step sets a value and a later step checks it.",
		"The later check always sees what the earlier step set.", "A flag is set by an earlier step, then a later step checks it.", "no",
		"Nothing; the config rule that says so also carries the reason."),
	label("depends-on-missing", CannotBeTested, "rule", "A condition only makes sense if another one holds.",
		"The second condition is meaningless when the first is false.", "\"Check the discount only if a coupon was given\", on a path with no coupon.", "no",
		"Nothing; the config rule that says so also carries the reason."),

	label("possible-correlation", NeedsReview, "likely", "The path asks about the same value twice, and PathKit had to forget the value in between.",
		"If nothing changed the value, the two answers could not both happen. PathKit cannot tell whether something did, so it does not call the path impossible.", "A value is checked, then a loop that may change it runs, then it is checked again.", "yes",
		"Check whether the value can change in between. If it cannot, mark the path untestable in the config with that reason; if it can, write a test."),

	label("impossible-type", CannotBeTested, "certain", "A type switch case, or a type assertion, that no type the value can hold takes.",
		"PathKit followed what is stored in the interface, slice or map (or what Temporal's decoder puts in it), so it knows the list of types it can hold. A case for a type not in the list can never be taken.", "A map that only ever received a string and a *string, then a type switch with case int.", "no",
		"Nothing, once you have read the proof (pathkit explain) and agree. If a type can reach the value another way that PathKit cannot see, mark the path testable in the config (classification.paths.mark_testable, with a reason)."),
	label("impossible-nil", CannotBeTested, "certain", "v == nil taken true, for an interface that holds a typed value: even a nil pointer inside an interface makes it non-nil.",
		"An interface value is nil only when it holds no type at all. A *string that is nil, stored in an interface{}, makes a non-nil interface.", "var v interface{} = note, where note is a *string, then if v == nil.", "no",
		"Nothing, once you have read the proof (pathkit explain). If the code means \"the pointer is nil\", compare the pointer, not the interface."),
	label("unknown-input-type", NeedsReview, "likely", "An untested path takes a type switch case on a value PathKit cannot follow, and no test has ever taken that case.",
		"PathKit does not know where the value comes from, so it does not know which types it can hold. The case may be one the input never has, or one nobody has tried.", "A type switch on a value passed in by a helper, with case int, when no test ever sent an int.", "yes",
		"Check what the input can hold. If the case can happen, write a test that sends it; if it cannot, mark the path untestable in the config with that reason."),

	// B. loops.
	label("same-list-loops", CannotBeTested, "certain", "Two loops over the same collection must run the same number of times.",
		"One loop running zero times while the other runs once can't happen.", "Two range loops over the same slice.", "no", "Nothing."),
	label("loop-minimum", CannotBeTested, "rule", "A loop that always runs at least once, for example a retry loop.",
		"Its \"not entered\" path can't happen.", "A retry loop that always makes the first attempt.", "no", "Nothing."),
	label("impossible-loop-exit", CannotBeTested, "certain", "The loop exits in a way the code does not allow.",
		"It only exits on a flag, or the flag starts false so it can't exit before the first round.", "A loop that only leaves when `done` is set.", "no", "Nothing."),

	// C. Temporal guarantees.
	label("closed-signal-channel", CannotBeTested, "certain", "more == false on a channel from workflow.GetSignalChannel.",
		"The Temporal SDK and server do not close a signal channel (checked in SDK v1.49.0). Only code can: a type assertion that turns it into a channel that has Close (PathKit looks for this), or a custom worker interceptor that hands the workflow a closed channel (PathKit does not look for this). This is an assessment, not a proof.", "A signal handler's \"channel closed\" branch.", "no (assumed)",
		"Nothing. A channel the workflow makes and closes itself is NOT this label: it is reachable and testable."),
	label("rejected-update-invisible", CannotBeTested, "certain", "The workflow checks for a rejected update, but it never sees updates a validator rejected.",
		"Rejected updates never reach workflow code.", "A branch for \"update was rejected\".", "no", "Nothing.").notYet(),
	label("side-effect-error", CannotBeTested, "certain", "An error branch after workflow.SideEffect.",
		"A failing side effect makes the workflow task retry, so the code never gets an error back.", "if err != nil after SideEffect.", "no", "Nothing.").notYet(),
	label("after-continue-as-new", CannotBeTested, "certain", "Steps after Continue-As-New in the same run.",
		"Continue-As-New ends the run.", "Code after return workflow.NewContinueAsNewError(...).", "no", "Nothing.").notYet(),
	label("update-handler-continue-as-new", CannotBeTested, "certain", "Continue-As-New inside an update handler.",
		"Temporal does not support it.", "NewContinueAsNewError returned from SetUpdateHandler's function.", "no", "Move it to the workflow function.").notYet(),

	// D. possible in production, hard to test.
	label("version-gate-old-branch", NeedsSpecialTest, "certain", "The old side of a workflow.GetVersion check.",
		"Replays of old histories reach it; new runs do not.", "if v == workflow.DefaultVersion { ... }", "yes", "Mock the version in a test: env.OnGetVersion(changeID, min, max).Return(workflow.DefaultVersion), or replay a saved history."),
	label("history-limit", NeedsSpecialTest, "certain", "The branch taken when the SDK suggests Continue-As-New because the history is large.",
		"Limits are 51,200 events or 50 MB.", "if info.GetContinueAsNewSuggested() { ... }", "yes", "Fake it in a test: env.SetContinueAsNewSuggested(true), or SetCurrentHistoryLength / SetCurrentHistorySize."),
	label("workflow-task-failure", NeedsSpecialTest, "certain", "Code that panics, or non-determinism errors.",
		"A panic fails the workflow task and it retries; it does not fail the workflow.", "panic(\"unexpected\")", "yes", "A replay test, or a deliberate panic test.").notYet(),
	label("unfinished-handlers", NeedsSpecialTest, "certain", "The workflow ends while signal or update handlers are still running.",
		"It depends on timing.", "A return while a handler is mid-way.", "yes", "A special timing test.").notYet(),

	// E. PathKit's own limits.
	label("model-limit", NeedsReview, "likely", "PathKit's model can't represent the behavior. NOT assumed impossible.",
		"An example is an earlier signal answering a later step.", "Two selectors reading one signal. (PathKit itself also puts this label on a detector or rule claim it does not trust because the workflow has a trace that fits none of its paths; no detector does that.)", "unknown", "Review it; mark it testable or untestable in the config, or explain the trace under classification.explained_traces.").notYet(),
	label("concurrency-order", NeedsReview, "likely", "Event order between workflow.Go coroutines or selector branches that PathKit can't judge.",
		"PathKit does not model concurrency.", "Two coroutines racing to set one variable.", "unknown", "Review it; mark it testable or untestable in the config.").notYet(),

	// F. manual and other.
	label("manual", "", "person", "Marked by a person, in the config file or with //pathkit:untestable, with a reason.",
		"A person knows something PathKit can't see.", "mark_untestable with a reason.", "as chosen", "Keep the reason up to date."),
	label("needs-review", NeedsReview, "likely", "PathKit is not sure, or the label is unknown.",
		"The safe place for anything unrecognized.", "A custom label with no definition.", "unknown", "Review it."),

	// Assumptions.
	{Kind: KindAssumption, Name: AssumptionJSON, Meaning: "a value Temporal decodes into an interface{} holds only a string, float64, bool, nil, []interface{} or map[string]interface{}",
		Why:                  "Temporal's default data converter decodes JSON that way. The claims named impossible-type rest on it for workflow inputs, activity results, signal payloads and SideEffect results that land in an interface{}.",
		Example:              "A workflow input of type map[string]interface{}: a type switch on one of its values has no int case that can run.",
		PossibleInProduction: "not applicable", DecidedBy: "PathKit", WhatToDo: "PathKit turns this off by itself when the module sets its own DataConverter (a client or worker option), a payload codec, or a type that is a data converter, payload converter or payload codec, and says so in every report. A decoder that uses json.Number or UseNumber elsewhere does not switch it off: it only changes the values that decoder produces. Values that encoding/json decodes in the workflow itself (json.Unmarshal, or Decode on a decoder that never calls UseNumber) follow from the standard library and need no assumption. To turn it off yourself: classification.assumptions.default_json_converter = \"off\"."},

	// Comments in the user's code.
	{Kind: KindComment, Name: "//pathkit:ignore", Meaning: "On an if (same line, or the line above): this if is never a junction. It is NOT the same as //pathkit:untestable.",
		Why: "A defensive check you can't test. PathKit then assumes the condition is false (for an error check, \"no error\").", Example: "//pathkit:ignore\nif x == nil { return err }",
		PossibleInProduction: "not applicable", DecidedBy: "person", WhatToDo: "Use it to take a decision off the map. The paths behind it are not listed at all, so they are not counted anywhere."},
	{Kind: KindComment, Name: "//pathkit:branch", Meaning: "On an if: always a junction, even an error check PathKit would treat as transparent.",
		Why: "A non-Temporal error you do want tracked.", Example: "//pathkit:branch\nif err != nil { return err }",
		PossibleInProduction: "not applicable", DecidedBy: "person", WhatToDo: "Use it to add a decision to the map."},
	{Kind: KindComment, Name: "//pathkit:untestable", Meaning: "Marks a path untestable, with a reason. It is NOT the same as //pathkit:ignore.",
		Why: "The path is listed and counted in overall coverage, but left out of testable coverage. //pathkit:ignore removes the decision from the map instead.", Example: "//pathkit:untestable exit=\"true\" reason=\"the carrier never returns this\"",
		PossibleInProduction: "not applicable", DecidedBy: "person", WhatToDo: "A reason is required. The config file wins if it disagrees."},
}

// DefinitionsVersion identifies the wording of the definitions list: a
// short hash of every field of every definition. A report prints it in its
// footer, so a saved report says which wording it was written with. It
// changes whenever any text in this list changes.
func DefinitionsVersion() string {
	h := sha256.New()
	for _, d := range definitions {
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%v\n",
			d.Kind, d.Name, d.Meaning, d.Why, d.Example, d.PossibleInProduction, d.Bucket, d.Confidence, d.DecidedBy, d.WhatToDo, d.NotDetectedYet)
	}
	return hex.EncodeToString(h.Sum(nil))[:8]
}
