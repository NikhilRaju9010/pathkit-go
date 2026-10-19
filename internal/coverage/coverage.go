// Package coverage works out which listed paths the recorded runs took.
// It only calculates; printing is done by the caller (render, cli).
//
// Rules (docs/DESIGN.md D2, D5, D9):
//   - A path is covered when at least one complete, matched, not-stale
//     trace lands on it (with --allow-stale, a stale trace that still fits
//     counts too, and the path is marked). Repeated runs count once.
//   - Every other trace is classified and reported, never dropped:
//     unmatched (with the step where it left the map), stale, incomplete,
//     excluded, unknown workflow, unreadable.
//   - Branch coverage: of all exits on the listed paths (loop retry edges
//     included), how many a counted trace took. It uses the trace's raw
//     steps, before loop folding, so an exit taken on an early loop trip
//     counts even when no covered path uses it. It is shown, never used
//     for pass/fail.
package coverage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"sort"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
	"github.com/NikhilRaju9010/pathkit-go/internal/trace"
)

// Workflow is one in-scope workflow to measure.
type Workflow struct {
	Name    string
	File    string // where it is declared, as it should be printed (report)
	Graph   *model.Graph
	Hash    string // current model.FunctionHash
	Package string // import path of the workflow's package ("" when unknown)
	// Module is the Go module the workflow is in; set only when the run covers
	// several modules (a folder that holds more than one), "" otherwise.
	Module        string
	AddedByConfig bool
}

// Input fields that decide how a workflow is cut up are in Input (Listing,
// Plan); a nil one means a plain listing and plain stages.

// TraceFile is one trace file: its content, or why it couldn't be read.
type TraceFile struct {
	Path    string
	File    trace.File
	ReadErr error
}

// Input is everything one coverage run looks at.
type Input struct {
	Workflows  []Workflow
	Excluded   []scope.Excluded
	Others     []string // in-scope workflows not picked (--function): their traces are counted as "other"
	Traces     []TraceFile
	AllowStale bool
	// Classifier decides each path's bucket; nil means classify.Classify.
	Classifier classify.Classifier
	// Policy is how needs-review paths count in testable coverage; ""
	// means classify.CountAsTestable.
	Policy classify.Policy
	// Explained are the traces the config accepts as understood (by
	// workflow and steps id). Only used when Classifier is set.
	Explained []scope.ExplainedTrace
	// Listing gives a workflow's path list (with the impossible parts of a
	// workflow over the cap left out and counted); nil means a plain listing.
	Listing func(workflow string, g *model.Graph) *classify.Listing
	// Plan gives a workflow's stages and the class of every stage path; nil
	// means plain stages, every stage path testable.
	Plan func(workflow string, g *model.Graph) *classify.StagePlan
	// Handlers are what the workflows register (signals, queries, updates and
	// their validators); the handler table is built from them.
	Handlers []model.Handler
	// Display shortens a file name for the report (the caller's
	// load.DisplayPath); nil leaves names as they are.
	Display func(string) string
}

// Unexplained is a group of trace files of one workflow that fit no path
// and have the same steps: the workflow's map is incomplete or wrong
// somewhere. Until it is explained, detector and rule claims of cannot-be-
// tested in that workflow are not trusted (classify.DowngradeUnexplained).
type Unexplained struct {
	StepsID  string   // "t1:" + 12 hex digits of the steps; stable for the same trace
	Steps    []string // the recorded steps
	Count    int      // trace files with exactly these steps
	Mismatch string   // where the trace leaves the map
	// Detail is the matcher's account of the first trace in the group:
	// the steps that fitted and where the walk stood (nil if unknown).
	Detail *model.Mismatch
	// Acknowledged is true when the config lists this trace in
	// classification.explained_traces; Reason is what it says.
	Acknowledged bool
	Reason       string
}

// StepsID is the id the report prints for a trace that fits no path.
func StepsID(steps []string) string {
	sum := sha256.Sum256([]byte(strings.Join(steps, "\n")))
	return "t1:" + hex.EncodeToString(sum[:])[:12]
}

// PathResult is one listed path and whether it was run.
type PathResult struct {
	Number     int // as analyze numbers it
	Path       model.Path
	Covered    bool
	Traces     int  // how many counted traces landed on it
	StaleTrace bool // covered, and a stale trace counted for it (--allow-stale)
	// StableID is model.Path.StableID: the same on every run of the same
	// code, and unaffected by unrelated edits (unlike Path.ID()).
	StableID string
	// Class is the path's bucket and reason. A covered path is always
	// testable: a real run proves it possible.
	Class classify.Classification
}

// WorkflowResult is one workflow's coverage.
type WorkflowResult struct {
	Name          string
	File          string
	AddedByConfig bool
	Paths         []PathResult
	Truncated     bool // more paths exist than the listing shows
	Covered       int
	Branches      int // exits on the listed paths
	BranchesTaken int
	Breakdown     Breakdown
	// Unexplained lists the traces of this workflow that fit no path (not
	// the ones merely beyond the path cap), most-seen first.
	Unexplained []Unexplained
	// Unmatched is the same list whether classification is on or not (the
	// unmatched-trace report, --unmatched); Unexplained stays empty with
	// classification off, so that output does not change.
	Unmatched []Unexplained

	// Exact is the exact number of paths the workflow has; Capped says the list
	// above holds fewer (the cap, and the parts left out as pruned groups).
	Exact  *big.Int
	Capped bool
	// OldVersionPaths is the exact number of paths through the old side of a
	// workflow.GetVersion check; nil when the workflow has no such check.
	OldVersionPaths *big.Int
	// Module is the workflow's Go module when the run covers several ("" when
	// it covers one).
	Module string
	// TracesCounted is how many recorded runs were counted for the workflow.
	TracesCounted int
	// OldRecorderTraces is how many of them an older recorder wrote: they have
	// no steps for handlers, so a handler no trace shows is "not recorded".
	OldRecorderTraces int
	// HelpersSummarized says the helpers are mapped once each (Units) instead of
	// copied into the paths; HelpersWhy is why PathKit chose that on its own
	// ("" when the config asked for it).
	HelpersSummarized bool
	HelpersWhy        string
	Units             []UnitResult
	// BeyondCap is how many counted traces ran a path that is not in the list.
	BeyondCap int
	// Pruned are the parts of the map left out of the list because the engine
	// shows them impossible, with their exact counts.
	Pruned []PrunedResult
	// Stages is the workflow cut into stages, with the stage paths a test ran.
	Stages []StageResult
	// Ran holds every prefix of every path a counted trace ran, as the exit
	// ids joined by newlines (a path beyond the list too); empty when no test
	// ran the workflow. MissedGroups reads it.
	Ran map[string]bool
	// AllBranches and AllBranchesTaken count the exits of the whole map (the
	// same as Branches when the list is whole).
	AllBranches, AllBranchesTaken int
}

// PrunedResult is a part of the map left out of the path list.
type PrunedResult struct {
	Count *big.Int
	// Prefix is the exits that lead to the impossible one (the last).
	Prefix []string
	Class  classify.Classification
	// Unpruned: a trace ran through it, so the proof is not trusted and the
	// paths count as needs-review.
	Unpruned bool
}

// StageResult is one stage of a workflow and which of its paths a test ran.
type StageResult struct {
	Index int
	Count *big.Int
	Big   bool // too many paths to list: branch coverage only
	// Part says the stage is a piece of a stretch that was too big for one stage
	// (SplitFrom paths), cut at the decisions of a nested region; SplitWhy, on a
	// Big stage, says why it could not be cut.
	Part      bool
	SplitFrom *big.Int
	SplitWhy  string
	Paths     []StagePathResult
	Contexts  int
	CutOff    bool
	// Branches counts the exits of the stage's decisions, and how many a test took.
	Branches, BranchesTaken int
	// Covered, and the buckets of the stage paths (Testable counts needs-review
	// ones too, like testable coverage under the default policy).
	Covered, Testable, CannotBeTested, NeedsReview int
}

// StagePathResult is one stage path and whether a test ran it.
type StagePathResult struct {
	ID      string
	Path    model.StagePath
	Covered bool
	Traces  int
	Class   classify.Classification
}

// Warning is one trace that didn't count, and why.
type Warning struct {
	File     string // base name
	Workflow string
	Kind     trace.Kind
	Message  string
	// Entry, for a trace that fits no path, is the ready-made
	// classification.explained_traces entry that would accept it, with the
	// reason "TODO" that the config refuses until a person replaces it.
	Entry string
}

// Counts sorts every trace file read.
type Counts struct {
	Read, Counted, Unmatched, Stale, Incomplete, Excluded, Unknown, Unreadable int
	Other                                                                      int // for in-scope workflows not picked with --function
	// BeyondCap counts traces that fit the whole map but run a path that is not
	// among the listed ones (the workflow has more paths than the cap lists).
	// They are counted like any other trace; none is unmatched.
	BeyondCap int
}

// Result is a whole coverage run.
type Result struct {
	Workflows     []WorkflowResult
	Excluded      []scope.Excluded
	Paths         int
	Covered       int
	Branches      int
	BranchesTaken int
	Warnings      []Warning
	// Unread are the places PathKit did not (fully) read, in workflow order
	// (the "could not fully read" panel; see UnreadEntry).
	Unread []UnreadEntry
	// Handlers is the handler table: every signal, query, update and update
	// validator the workflows register, whether a test exercised it, and how
	// many of its branches a test took.
	Handlers []HandlerResult
	// CallOutcomes is the outcome table: for every activity, child workflow and
	// Nexus operation call whose result is checked, the outcomes it can have
	// and which of them a test saw.
	CallOutcomes []CallOutcomeRow
	// LoopRounds is the loop rounds table: for every loop, which of 0, 1 and 2+
	// rounds can happen and which a test ran.
	LoopRounds []LoopRoundsRow
	Counts     Counts
	Breakdown  Breakdown
	// PrunedPaths is how many paths were left out of the lists because the
	// fact engine shows them impossible (counted exactly, kept as groups), over
	// all workflows.
	PrunedPaths *big.Int
	// InternalError is set when PathKit's own consistency check failed (a
	// path a test ran is not testable). It never happens in a correct
	// build; the command that called Compute stops with this message
	// instead of printing numbers it cannot stand behind.
	InternalError error
}

// Basis says which paths --fail-under and "coverage basis" use as the
// whole: every listed path (overall), or only the testable ones.
type Basis string

// The two coverage bases.
const (
	BasisOverall  Basis = "overall"
	BasisTestable Basis = "testable"
)

// BucketCount is how many paths are in a bucket, and how many of them a
// real run took.
type BucketCount struct{ Paths, Covered int }

// Breakdown counts paths by bucket. The two coverage numbers come from
// it: overall (every path) and testable (testable paths, plus needs-review
// ones under the strict default policy).
type Breakdown struct {
	Testable, CannotBeTested, NeedsSpecialTest, NeedsReview BucketCount
	// Unexplained is the part of NeedsReview that is there because the
	// workflow has a trace fitting no path (a claim that is not trusted).
	// It counts in testable coverage under every policy: leaving it out
	// under "exclude" would let an untrusted claim raise the number.
	Unexplained BucketCount
	Policy      classify.Policy
}

// slot returns the count of one bucket. An unknown bucket is counted as
// testable, the safe direction.
func (b *Breakdown) slot(k classify.Bucket) *BucketCount {
	switch k {
	case classify.CannotBeTested:
		return &b.CannotBeTested
	case classify.NeedsSpecialTest:
		return &b.NeedsSpecialTest
	case classify.NeedsReview:
		return &b.NeedsReview
	}
	return &b.Testable
}

// Of returns the count of one bucket.
func (b Breakdown) Of(k classify.Bucket) BucketCount { return *b.slot(k) }

func (b *Breakdown) add(k classify.Bucket, covered bool) {
	c := b.slot(k)
	c.Paths++
	if covered {
		c.Covered++
	}
}

func (b *Breakdown) merge(o Breakdown) {
	for _, k := range classify.Buckets {
		c := b.slot(k)
		c.Paths += o.Of(k).Paths
		c.Covered += o.Of(k).Covered
	}
	b.Unexplained.Paths += o.Unexplained.Paths
	b.Unexplained.Covered += o.Unexplained.Covered
}

// Overall is every listed path.
func (b Breakdown) Overall() BucketCount {
	return BucketCount{
		Paths:   b.Testable.Paths + b.CannotBeTested.Paths + b.NeedsSpecialTest.Paths + b.NeedsReview.Paths,
		Covered: b.Testable.Covered + b.CannotBeTested.Covered + b.NeedsSpecialTest.Covered + b.NeedsReview.Covered,
	}
}

// TestableCoverage is covered testable paths out of testable paths. With
// the default policy (count_as_testable), needs-review paths count too, so
// an unsure path can only lower the number. With "exclude" they are left out.
func (b Breakdown) TestableCoverage() BucketCount {
	c := b.Testable
	if b.Policy != classify.Exclude {
		c.Paths += b.NeedsReview.Paths
		c.Covered += b.NeedsReview.Covered
	} else {
		c.Paths += b.Unexplained.Paths
		c.Covered += b.Unexplained.Covered
	}
	return c
}

// Special is the needs-special-test paths on their own.
func (b Breakdown) Special() BucketCount { return b.NeedsSpecialTest }

// For returns the count for a basis.
func (b Breakdown) For(basis Basis) BucketCount {
	if basis == BasisTestable {
		return b.TestableCoverage()
	}
	return b.Overall()
}

// Percent is covered / total as a percentage (0 when there is nothing).
func Percent(covered, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(covered) / float64(total) * 100
}

// Priority labels, docs/DESIGN.md D11.
const (
	PriorityHigh   = "High"
	PriorityMedium = "Medium"
	PriorityLow    = "Low"
)

// Priority is a workflow's priority label (docs/DESIGN.md D11): how urgently
// it needs more tests. High below 50%, Medium from 50% up to and
// including 80%, Low above 80%. The boundaries are compared with whole
// numbers (covered*100 against boundary*total), never with a rounded
// percentage, so 4/5 is exactly 80% (Medium) and 8001/10000 is Low. A
// workflow with no listed paths has no label (ok is false).
func Priority(covered, total int) (label string, ok bool) {
	switch {
	case total <= 0:
		return "", false
	case covered*100 < 50*total:
		return PriorityHigh, true
	case covered*100 <= 80*total:
		return PriorityMedium, true
	}
	return PriorityLow, true
}

// Compute measures every workflow in in against every trace in in.
func Compute(in Input) Result {
	type state struct {
		wf       Workflow
		paths    model.PathList
		byKey    map[string]int // path key -> index in paths
		traces   []int          // counted traces per path
		stale    []bool
		branches map[string]bool // exit ID -> taken
		unexp    map[string]*Unexplained
		unexpIDs []string

		listing  *classify.Listing
		plan     *classify.StagePlan
		capped   bool                                // the workflow has more paths than the list holds
		allExits map[string]*model.Exit              // every exit some path takes
		seen     map[string]bool                     // every exit ID some counted trace recorded
		hits     []map[string]int                    // per stage: traces per stage path key
		unpruned map[int]bool                        // pruned groups some trace ran through
		unitHit  map[string]bool                     // exit IDs of summarized helpers some counted trace took
		counted  int                                 // traces counted for this workflow
		oldRec   int                                 // of those, recorded by a recorder before handlers were recorded
		rounds   map[*model.Junction]map[string]bool // loop -> round buckets some counted trace ran
		beyond   int                                 // traces that fit the map but not the list
		ran      map[string]bool                     // every prefix of every path a counted trace ran (a path beyond the list too)
	}
	states := map[string]*state{}
	var order []*state
	for _, w := range in.Workflows {
		lst := classify.PlainListing(w.Graph)
		if in.Listing != nil {
			lst = in.Listing(w.Name, w.Graph)
		}
		plan := classify.PlainPlan(w.Graph, model.DefaultStageMaxPaths)
		if in.Plan != nil {
			plan = in.Plan(w.Name, w.Graph)
		}
		st := &state{wf: w, paths: lst.PathList, listing: lst, plan: plan, byKey: map[string]int{}, branches: map[string]bool{}, unexp: map[string]*Unexplained{},
			seen: map[string]bool{}, unitHit: map[string]bool{}, rounds: map[*model.Junction]map[string]bool{}, ran: map[string]bool{}, unpruned: map[int]bool{}, hits: make([]map[string]int, len(plan.Stages))}
		for k := range st.hits {
			st.hits[k] = map[string]int{}
		}
		pruned := new(big.Int)
		for _, pg := range lst.Pruned {
			pruned.Add(pruned, pg.Group.Count)
		}
		st.capped = lst.Exact.Cmp(new(big.Int).Add(big.NewInt(int64(len(st.paths.List))), pruned)) > 0
		for i, p := range st.paths.List {
			st.byKey[p.Key()] = i
			for _, s := range p.Steps {
				st.branches[s.Exit.ID.String()] = false
			}
		}
		st.allExits = map[string]*model.Exit{}
		for _, e := range w.Graph.ReachableExits() {
			st.allExits[e.ID.String()] = e
		}
		st.traces = make([]int, len(st.paths.List))
		st.stale = make([]bool, len(st.paths.List))
		states[w.Name] = st
		order = append(order, st)
	}
	excluded := map[string]bool{}
	for _, e := range in.Excluded {
		excluded[e.Name] = true
	}
	others := map[string]bool{}
	for _, n := range in.Others {
		others[n] = true
	}

	classifier := in.Classifier
	if classifier == nil {
		classifier = classify.Classify
	}
	policy := in.Policy
	if policy == "" {
		policy = classify.CountAsTestable
	}

	var res Result
	res.Excluded = in.Excluded
	res.Breakdown.Policy = policy
	warn := func(tf TraceFile, kind trace.Kind, format string, args ...any) {
		res.Warnings = append(res.Warnings, Warning{File: filepath.Base(tf.Path), Workflow: tf.File.Workflow, Kind: kind, Message: fmt.Sprintf(format, args...)})
	}
	olderSeen := 0
	for _, tf := range in.Traces {
		var older *trace.OlderFormatError
		if errors.As(tf.ReadErr, &older) {
			olderSeen++
		}
	}
	olderTold := false
	for _, tf := range in.Traces {
		res.Counts.Read++
		name := filepath.Base(tf.Path)
		var older *trace.OlderFormatError
		if errors.As(tf.ReadErr, &older) {
			// One message for all of them: they were all recorded the same way.
			res.Counts.Unreadable++
			if !olderTold {
				olderTold = true
				warn(tf, "unreadable", "%v (%s, one of %d trace files like it; none of them is counted)", older, name, olderSeen)
			}
			continue
		}
		if tf.ReadErr != nil {
			res.Counts.Unreadable++
			warn(tf, "unreadable", "could not read trace %s: %v; delete the file, or run \"pathkit clean\" and then \"pathkit test\" again", name, tf.ReadErr)
			continue
		}
		f := tf.File
		if excluded[f.Workflow] {
			res.Counts.Excluded++ // listed in the summary, not a warning
			continue
		}
		if others[f.Workflow] {
			res.Counts.Other++ // --function picked another workflow
			continue
		}
		st := states[f.Workflow]
		if st == nil {
			res.Counts.Unknown++
			warn(tf, trace.UnknownWorkflow, "trace %s: workflow %s is not among the analyzed workflows (it is outside the folder you gave, or was renamed or removed); give the folder that holds it, or run \"pathkit clean\" and \"pathkit test\" again", name, f.Workflow)
			continue
		}
		out := trace.CheckIn(f, st.wf.Graph, st.wf.Hash, st.wf.Package)
		stale := false
		if out.Kind == trace.Stale {
			if !in.AllowStale {
				res.Counts.Stale++
				warn(tf, trace.Stale, "trace %s was recorded for an older version of %s; re-run pathkit test, or pass --allow-stale", name, f.Workflow)
				continue
			}
			// --allow-stale: match it against the current map anyway.
			stale = true
			if p, mm := st.wf.Graph.Match(f.Steps); mm != nil {
				out = trace.Outcome{Kind: trace.Unmatched, Reason: mm.Reason, Mismatch: mm}
			} else {
				out = trace.Outcome{Kind: trace.Matched, Path: p, Steps: f.Steps}
			}
		}
		switch out.Kind {
		case trace.Incomplete:
			res.Counts.Incomplete++
			warn(tf, trace.Incomplete, "trace %s (%s): the run never finished (panic, timeout, or stopped); fix the test that stops it, then run \"pathkit test\" again", name, f.Workflow)
			continue
		case trace.Unmatched:
			res.Counts.Unmatched++
			how := ""
			if stale {
				how = " (a stale trace, allowed by --allow-stale)"
			}
			id := StepsID(f.Steps)
			warn(tf, trace.Unmatched, "trace %s (%s)%s fits no path: %s (\"pathkit report --unmatched\" shows the likely cause)", name, f.Workflow, how, out.Reason)
			res.Warnings[len(res.Warnings)-1].Entry = fmt.Sprintf("{\"workflow\": %q, \"steps_id\": %q, \"reason\": \"TODO\"}", f.Workflow, id)
			if u := st.unexp[id]; u != nil {
				u.Count++
			} else {
				st.unexp[id] = &Unexplained{StepsID: id, Steps: f.Steps, Count: 1, Mismatch: out.Reason, Detail: out.Mismatch}
				st.unexpIDs = append(st.unexpIDs, id)
			}
			continue
		case trace.Matched:
		default:
			res.Counts.Unknown++
			warn(tf, out.Kind, "trace %s (%s): %s", name, f.Workflow, out.Reason)
			continue
		}
		for _, h := range st.wf.Graph.UnitHits(out.Steps) {
			st.unitHit[h] = true
		}
		i, ok := st.byKey[out.Path.Key()]
		if !ok {
			// It fits the whole map. If it runs through a part the engine proved
			// impossible, the proof was wrong: list the path, and warn.
			if gi := prunedBy(st.listing, out.Path); gi >= 0 {
				st.unpruned[gi] = true
				res.Warnings = append(res.Warnings, Warning{File: filepath.Base(tf.Path), Workflow: f.Workflow, Kind: "classification",
					Message: fmt.Sprintf("trace %s (%s) ran a path inside a part of the map that PathKit had proved impossible (%s); that proof is not trusted: the %s paths in that part of the map are needs-review, and the path the trace ran is listed", name, f.Workflow, st.listing.Pruned[gi].Class.Explanation, st.listing.Pruned[gi].Group.Count)})
				st.paths.List = append(st.paths.List, out.Path)
				st.traces = append(st.traces, 0)
				st.stale = append(st.stale, false)
				i = len(st.paths.List) - 1
				st.byKey[out.Path.Key()] = i
				ok = true
			}
		}
		res.Counts.Counted++
		prefix := ""
		for _, s := range out.Path.Steps {
			prefix += s.Exit.ID.String() + "\n"
			st.ran[prefix] = true
		}
		st.ran[""] = true
		if !ok { // a path of a workflow with more paths than the list holds
			res.Counts.BeyondCap++
			st.beyond++
		} else {
			st.traces[i]++
			if stale {
				st.stale[i] = true
			}
		}
		st.counted++
		if f.Recorder == 0 {
			st.oldRec++
		}
		for j, ns := range st.wf.Graph.Rounds(out.Steps) {
			if st.rounds[j] == nil {
				st.rounds[j] = map[string]bool{}
			}
			for _, n := range ns {
				st.rounds[j][model.RoundBucket(n)] = true
			}
		}
		for _, id := range out.Steps { // raw steps (as read against this map), before loop folding
			st.seen[id] = true
			if _, onPath := st.branches[id]; onPath {
				st.branches[id] = true
			}
		}
		for _, pc := range st.wf.Graph.StagePieces(out.Path, st.plan.Stages) {
			if pc.Stage < len(st.hits) {
				st.hits[pc.Stage][pc.Key()]++
			}
		}
	}

	explained := map[string]string{} // workflow + "\x00" + steps id -> reason
	usedAck := map[string]bool{}
	for _, e := range in.Explained {
		explained[e.Workflow+"\x00"+e.StepsID] = e.Reason
	}

	for _, st := range order {
		// Traces that fit no path, and whether the config accepts them.
		var unacknowledged []*Unexplained
		for _, id := range st.unexpIDs {
			u := st.unexp[id]
			if r, ok := explained[st.wf.Name+"\x00"+id]; ok && in.Classifier != nil {
				u.Acknowledged, u.Reason = true, r
				usedAck[st.wf.Name+"\x00"+id] = true
			} else {
				unacknowledged = append(unacknowledged, u)
			}
		}
		why := ""
		if len(unacknowledged) > 0 {
			n := 0
			var ids []string
			for _, u := range unacknowledged {
				n += u.Count
				ids = append(ids, u.StepsID)
			}
			why = fmt.Sprintf("this workflow has %s that fit%s no path (%s)", plural(n, "trace"), map[bool]string{true: "s", false: ""}[n == 1], strings.Join(ids, ", "))
		}
		wr := WorkflowResult{Name: st.wf.Name, File: st.wf.File, AddedByConfig: st.wf.AddedByConfig, Truncated: st.paths.Truncated, Branches: len(st.branches)}
		wr.Breakdown.Policy = policy
		wr.Exact, wr.Capped, wr.BeyondCap = st.listing.Exact, st.capped, st.beyond
		wr.TracesCounted, wr.OldRecorderTraces = st.counted, st.oldRec
		wr.Module = st.wf.Module
		wr.Ran = st.ran
		if n, has := st.wf.Graph.OldVersionPathCount(); has {
			wr.OldVersionPaths = n
		}
		wr.HelpersSummarized, wr.HelpersWhy = st.wf.Graph.Summarized(), st.wf.Graph.SummarizedBecause()
		for _, u := range st.wf.Graph.Units() {
			ur := UnitResult{ID: u.ID, Name: u.Name(), Sites: len(u.Calls)}
			pos := u.Position()
			ur.File, ur.Line = pos.Filename, pos.Line
			if in.Display != nil {
				ur.File = in.Display(ur.File)
			}
			for _, id := range u.Branches() {
				ur.Branches++
				if st.unitHit[id.String()] {
					ur.BranchesTaken++
				}
			}
			wr.Units = append(wr.Units, ur)
		}
		for i, p := range st.paths.List {
			pr := PathResult{Number: i + 1, Path: p, Covered: st.traces[i] > 0, Traces: st.traces[i], StaleTrace: st.stale[i], StableID: p.StableID(), Class: classifier(st.wf.Name, p)}
			if pr.Class.Bucket == "" {
				pr.Class.Bucket = classify.Testable // never leave a path without a bucket
			}
			if len(unacknowledged) > 0 && !pr.Covered && pr.Class.Bucket == classify.CannotBeTested {
				if w := distrustWhy(pr.Class, unacknowledged); w != "" {
					if c, changed := classify.DowngradeUnexplained(pr.Class, w); changed {
						pr.Class = c
					}
				}
			}
			if pr.Class.Label == "unknown-input-type" && (pr.Covered || st.branches[pr.Class.SuspectExit]) {
				// Only for a case no test has taken: this one was run (by this
				// path or another), so it is an ordinary path.
				pr.Class = classify.DefaultTestable()
			}
			if pr.Covered && pr.Class.Label == "possible-correlation" {
				// A suspect is only for paths no test ran; a run settled this
				// one, so it is simply testable, with nothing to warn about.
				pr.Class = classify.DefaultTestable()
			}
			if pr.Covered && pr.Class.Bucket != classify.Testable {
				// A real run proves the path is possible: never hide it.
				res.Warnings = append(res.Warnings, Warning{Workflow: st.wf.Name, Kind: "classification",
					Message: fmt.Sprintf("%s path %d (%s) was classified %s (%s), but a test ran it; counting it as testable", st.wf.Name, pr.Number, pr.StableID, pr.Class.Bucket, strings.TrimSuffix(pr.Class.Explanation, "."))})
				pr.Class = classify.DefaultTestable()
			}
			if pr.Covered {
				wr.Covered++
			}
			wr.Breakdown.add(pr.Class.Bucket, pr.Covered)
			if pr.Class.Origin == classify.OriginUnexplained {
				wr.Breakdown.Unexplained.Paths++
				if pr.Covered {
					wr.Breakdown.Unexplained.Covered++
				}
			}
			wr.Paths = append(wr.Paths, pr)
		}
		for _, taken := range st.branches {
			if taken {
				wr.BranchesTaken++
			}
		}
		for _, id := range st.unexpIDs {
			wr.Unmatched = append(wr.Unmatched, *st.unexp[id])
			if in.Classifier != nil { // classification off: output unchanged
				wr.Unexplained = append(wr.Unexplained, *st.unexp[id])
			}
		}
		sort.SliceStable(wr.Unmatched, func(i, j int) bool { return wr.Unmatched[i].Count > wr.Unmatched[j].Count })
		sort.SliceStable(wr.Unexplained, func(i, j int) bool { return wr.Unexplained[i].Count > wr.Unexplained[j].Count })
		if why != "" && in.Classifier != nil {
			manual := 0
			for _, pr := range wr.Paths {
				if pr.Class.Bucket == classify.CannotBeTested && (pr.Class.Origin == classify.OriginOverride || pr.Class.Origin == classify.OriginComment) {
					manual++
				}
			}
			msg := fmt.Sprintf("%s: %s. Its map may be incomplete from the step where the trace leaves it, so cannot-be-tested claims by detectors and rules that rest on that step or a later one are shown as needs-review until the trace is explained (fix the map, re-record, or accept it under classification.explained_traces with a reason)", st.wf.Name, why)
			if manual > 0 {
				msg += fmt.Sprintf("; %s marked by a person %s kept", plural(manual, "path"), map[bool]string{true: "is", false: "are"}[manual == 1])
			}
			res.Warnings = append(res.Warnings, Warning{Workflow: st.wf.Name, Kind: "classification", Message: msg})
		}
		finishStages(&wr, st.plan, st.hits, st.seen, st.allExits)
		for gi, pg := range st.listing.Pruned {
			pr := PrunedResult{Count: pg.Group.Count, Class: pg.Class, Unpruned: st.unpruned[gi]}
			for _, s := range pg.Group.Prefix {
				pr.Prefix = append(pr.Prefix, s.Exit.ID.String())
			}
			if pr.Unpruned {
				pr.Class = classify.DefaultTestable()
				pr.Class.Bucket, pr.Class.Label, pr.Class.Confidence, pr.Class.PossibleInProduction = classify.NeedsReview, "model-limit", classify.Likely, true
				pr.Class.Explanation = "A test ran a path inside this part of the map, so the proof that it is impossible is not trusted: " + pg.Class.Explanation
			} else {
				res.PrunedPaths = new(big.Int).Add(orZero(res.PrunedPaths), pg.Group.Count)
			}
			wr.Pruned = append(wr.Pruned, pr)
		}
		res.Workflows = append(res.Workflows, wr)
		res.Breakdown.merge(wr.Breakdown)
		res.Paths += len(wr.Paths)
		res.Covered += wr.Covered
		res.Branches += wr.Branches
		res.BranchesTaken += wr.BranchesTaken
	}
	if in.Classifier != nil {
		for _, e := range in.Explained {
			if _, measured := states[e.Workflow]; !measured || usedAck[e.Workflow+"\x00"+e.StepsID] {
				continue
			}
			res.Warnings = append(res.Warnings, Warning{Workflow: e.Workflow, Kind: "classification",
				Message: fmt.Sprintf("classification: stale entry: explained_traces %s %s (%s) matches no trace that fits no path in the current run; remove the entry, or check the id in the report", e.Workflow, e.StepsID, e.Reason)})
		}
	}
	sort.SliceStable(res.Warnings, func(i, j int) bool { return res.Warnings[i].File < res.Warnings[j].File })
	res.Unread = collectUnread(in, res.Workflows)
	graphs, seen := map[string]*model.Graph{}, map[string]map[string]bool{}
	for name, st := range states {
		graphs[name], seen[name] = st.wf.Graph, st.seen
	}
	res.Handlers = handlerTable(in, graphs, seen, res.Workflows)
	res.CallOutcomes = callOutcomeTable(in, seen)
	var loopIn []loopInput
	for _, st := range order {
		loopIn = append(loopIn, loopInput{st.wf.Name, st.wf.Graph, st.rounds})
	}
	res.LoopRounds = loopRoundsTable(in, loopIn)
	// Internal assertion: the demotion above makes this impossible. If a
	// later change breaks it, say so loudly (the caller stops with an error)
	// instead of hiding a path a test ran.
	if err := CheckCoveredAreTestable(res); err != nil {
		res.InternalError = fmt.Errorf("internal error, please report it: %v", err)
	}
	return res
}

// CheckCoveredAreTestable returns an error naming the first path that a
// test ran but that is not in the testable bucket. That must never happen
// (a real run proves a path possible); Compute asserts it on every run and
// the tests check it on every project they measure.
func CheckCoveredAreTestable(res Result) error {
	for _, w := range res.Workflows {
		for _, p := range w.Paths {
			if p.Covered && p.Class.Bucket != classify.Testable {
				return fmt.Errorf("%s path %d (%s) was run by a test but is classified %s (%s)", w.Name, p.Number, p.StableID, p.Class.Bucket, p.Class.Label)
			}
		}
	}
	return nil
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// divergence is the 1-based step of a path where a trace that fits no path
// leaves the map: the first step the map could not follow. Everything the
// trace did before it (steps 1 to divergence-1) fitted.
func (u *Unexplained) divergence() int {
	if u.Detail == nil {
		return 1
	}
	return len(u.Detail.Followed) + 1
}

// distrustWhy says why a cannot-be-tested claim of a detector or rule is not
// trusted because of these traces ("" when it is). A claim whose reasoning
// stays before the point where every one of the traces leaves the map keeps
// its label; one that rests on that step or a later one, or whose steps are
// not known, does not.
func distrustWhy(c classify.Classification, traces []*Unexplained) string {
	uses := c.UsesStep
	if uses == 0 {
		for _, p := range c.Proof {
			uses = max(uses, p.Step)
		}
	}
	var hit []*Unexplained
	count, first := 0, 0
	for _, u := range traces {
		if uses == 0 || uses >= u.divergence() {
			hit = append(hit, u)
			count += u.Count
			if first == 0 || u.divergence() < first {
				first = u.divergence()
			}
		}
	}
	if len(hit) == 0 {
		return ""
	}
	ids := make([]string, len(hit))
	for i, u := range hit {
		ids[i] = u.StepsID
	}
	where := ""
	if uses > 0 {
		where = fmt.Sprintf("; the claim rests on step %d, and the map stops fitting at step %d", uses, first)
	}
	return fmt.Sprintf("this workflow has %s that fit%s no path (%s)%s", plural(count, "trace"), map[bool]string{true: "s", false: ""}[count == 1], strings.Join(ids, ", "), where)
}

// prunedBy finds the pruned group a path runs through, or -1.
func prunedBy(l *classify.Listing, p model.Path) int {
	for gi, pg := range l.Pruned {
		if len(p.Steps) < len(pg.Group.Prefix) {
			continue
		}
		match := true
		for i, s := range pg.Group.Prefix {
			if p.Steps[i].Exit != s.Exit {
				match = false
				break
			}
		}
		if match {
			return gi
		}
	}
	return -1
}

func orZero(n *big.Int) *big.Int {
	if n == nil {
		return new(big.Int)
	}
	return n
}

// finishStages fills in the stage results of a workflow: which stage paths a
// test ran (hits are the traces per stage path key), and how many exits of
// each stage a trace took (seen are the exit IDs recorded; allExits the exits
// some path takes).
func finishStages(wr *WorkflowResult, plan *classify.StagePlan, hits []map[string]int, seen map[string]bool, allExits map[string]*model.Exit) {
	taken := 0
	for id := range allExits {
		if seen[id] {
			taken++
		}
	}
	wr.AllBranches, wr.AllBranchesTaken = len(allExits), taken
	for k, stg := range plan.Stages {
		sr := StageResult{Index: k, Count: stg.Count, Big: stg.Big, Contexts: plan.Contexts[k], CutOff: plan.CutOff[k], Part: stg.Part, SplitFrom: stg.SplitFrom, SplitWhy: stg.SplitWhy}
		inStage := map[*model.Junction]bool{}
		for _, j := range stg.Junctions {
			inStage[j] = true
		}
		for id, e := range allExits {
			if inStage[e.Junction] {
				sr.Branches++
				if seen[id] {
					sr.BranchesTaken++
				}
			}
		}
		for _, sc := range plan.Classes[k] {
			pr := StagePathResult{ID: sc.Path.StageID(), Path: sc.Path, Class: sc.Class, Traces: hits[k][sc.Path.Key()]}
			pr.Covered = pr.Traces > 0
			if pr.Covered && sc.Class.Bucket != classify.Testable {
				pr.Class = classify.DefaultTestable() // a real run proves it possible
			}
			switch pr.Class.Bucket {
			case classify.CannotBeTested:
				sr.CannotBeTested++
			case classify.NeedsReview:
				sr.NeedsReview++
				sr.Testable++
			default:
				sr.Testable++
			}
			if pr.Covered {
				sr.Covered++
			}
			sr.Paths = append(sr.Paths, pr)
		}
		wr.Stages = append(wr.Stages, sr)
	}
}

// StageTotals counts, over every workflow, the stage paths a test ran and the
// ones a test can run (not shown impossible), leaving out the stages that are
// too big to list.
func StageTotals(res Result) (covered, total int) {
	for _, w := range res.Workflows {
		if !w.Capped && len(w.Pruned) == 0 {
			continue // all its paths are listed: the path numbers say it
		}
		for _, s := range w.Stages {
			if s.Big {
				continue
			}
			covered += s.Covered
			total += len(s.Paths) - s.CannotBeTested
		}
	}
	return covered, total
}

// FloorPart is one slice of the testable floor number: the path coverage of
// the workflows under the cap (Workflow ""), or one workflow over the cap
// measured by its stages ("stage") or its branches ("branch").
type FloorPart struct {
	Workflow       string
	Basis          string // "path", "stage" or "branch"
	Covered, Total int
}

// TestableFloor is the number classification.ci.fail_under_testable is checked
// against. A workflow under the cap counts by its testable paths. One over the
// cap lists only part of its paths, so listed-path coverage would mislead: it
// counts by cappedBasis instead, "stage" (its testable stage paths) or
// "branch" (the exits taken over the whole map). With no workflow over the cap
// this is exactly the testable coverage of the project.
func TestableFloor(res Result, cappedBasis string) (covered, total int, parts []FloorPart) {
	var under FloorPart
	under.Basis = "path"
	for _, w := range res.Workflows {
		if !w.Capped {
			t := w.Breakdown.TestableCoverage()
			under.Covered += t.Covered
			under.Total += t.Paths
			continue
		}
		p := FloorPart{Workflow: w.Name, Basis: cappedBasis}
		if cappedBasis == "branch" {
			p.Covered, p.Total = w.AllBranchesTaken, w.AllBranches
		} else {
			p.Basis = "stage"
			for _, s := range w.Stages {
				if s.Big {
					continue
				}
				p.Covered += s.Covered
				p.Total += len(s.Paths) - s.CannotBeTested
			}
		}
		parts = append(parts, p)
		covered += p.Covered
		total += p.Total
	}
	covered += under.Covered
	total += under.Total
	return covered, total, append([]FloorPart{under}, parts...)
}

// UnreadEntry is one row of the "PathKit could not fully read" panel: a place
// PathKit treated as unknown or cut off, and what it did instead.
type UnreadEntry struct {
	Workflow   string
	File       string // as it should be printed
	Line       int
	Kind       string
	Call       string
	Effect     string
	Suggestion string
}

// Kinds of UnreadEntry that come from the measurement and not from the model
// (model.Unread* are the ones the model finds in the code).
const (
	UnreadPathCap    = "path cap"
	UnreadContextCut = "context cut-off"
	UnreadNotMapped  = "workflow not mapped"
)

// collectUnread gathers the model's unread places of every workflow, plus the
// cut-offs the measurement itself made: the path cap and stages whose fact
// engine passed the limit of incoming states.
func collectUnread(in Input, results []WorkflowResult) []UnreadEntry {
	show := in.Display
	if show == nil {
		show = func(s string) string { return s }
	}
	graphs := map[string]*Workflow{}
	for i := range in.Workflows {
		graphs[in.Workflows[i].Name] = &in.Workflows[i]
	}
	var out []UnreadEntry
	for _, w := range results {
		src := graphs[w.Name]
		if src == nil || src.Graph == nil {
			continue
		}
		pos := src.Graph.FileSet().Position(src.Graph.Func().Pos())
		for _, u := range src.Graph.Unread() {
			out = append(out, UnreadEntry{Workflow: w.Name, File: show(u.File), Line: u.Line, Kind: u.Kind, Call: u.Call, Effect: u.Effect, Suggestion: u.Suggestion})
		}
		if w.Capped {
			out = append(out, UnreadEntry{Workflow: w.Name, File: show(pos.Filename), Line: pos.Line, Kind: UnreadPathCap,
				Effect: fmt.Sprintf("only %d of %s paths are listed; the rest are counted and covered through stages", len(w.Paths), w.Exact)})
		}
		for _, s := range w.Stages {
			if s.CutOff {
				out = append(out, UnreadEntry{Workflow: w.Name, File: show(pos.Filename), Line: pos.Line, Kind: UnreadContextCut,
					Effect: fmt.Sprintf("stage %d had more incoming states than the limit; the extra ones are not followed, so claims there are made with less information", s.Index+1)})
			}
		}
	}
	return out
}

// UnitResult is one summarized helper: its branches, and how many some test
// took. A branch counts when any call of the helper takes it.
type UnitResult struct {
	ID            string
	Name          string
	File          string
	Line          int
	Branches      int
	BranchesTaken int
	Sites         int // places that call it
}

// HandlerResult is one row of the handler table.
type HandlerResult struct {
	Owner string // the workflow that registers it
	Kind  string // "signal", "query", "update" or "update validator"
	Name  string // "" when the name is computed
	Where string // file:line of the registration, as it should be printed
	// Workflow is the small workflow made of its code (an update handler or
	// validator PathKit maps), "" when there is none.
	Workflow string
	// Exercised is "yes", "no", or "not recorded" (a trace cannot tell: a query
	// handler, or a signal read in a way no decision shows).
	Exercised string
	// Branches and BranchesTaken count the exits of the handler's own decisions
	// (zero when it has no map).
	Branches, BranchesTaken int
	// Why says why the handler has no map of its code ("" when it has one).
	Why string
}

// IsGap says the row is worth attention: a handler no test exercised, or one
// with branches no test took.
func (h HandlerResult) IsGap() bool {
	return h.Exercised == "no" || h.BranchesTaken < h.Branches
}

// handlerTable builds the handler table from what the workflows register and
// the traces counted for them.
func handlerTable(in Input, graphs map[string]*model.Graph, seen map[string]map[string]bool, results []WorkflowResult) []HandlerResult {
	byName := map[string]WorkflowResult{}
	for _, w := range results {
		byName[w.Name] = w
	}
	show := in.Display
	if show == nil {
		show = func(s string) string { return s }
	}
	var out []HandlerResult
	done := map[string]bool{}
	for _, h := range in.Handlers {
		key := fmt.Sprintf("%s|%s|%s|%s:%d", h.Owner, h.Kind, h.Name, h.Pos.Filename, h.Pos.Line)
		if done[key] {
			continue
		}
		done[key] = true
		if graphs[h.Owner] == nil {
			continue // its workflow was not measured (--function, scope)
		}
		r := HandlerResult{Owner: h.Owner, Kind: h.Kind, Name: h.Name, Where: fmt.Sprintf("%s:%d", show(h.Pos.Filename), h.Pos.Line), Why: h.Why}
		own, mapped := byName[h.WorkflowName]
		switch {
		case h.Kind == model.HandlerSignal:
			r.Exercised = signalExercised(h, graphs[h.Owner], seen[h.Owner])
			r.Why = ""
		case mapped && h.WorkflowName != "":
			r.Workflow = h.WorkflowName
			if h.Workflow == nil {
				r.Workflow = h.WorkflowName // a workflow PathKit finds on its own
			}
			r.Exercised = "no"
			if o := byName[h.Owner]; own.TracesCounted == 0 && o.TracesCounted > 0 && o.OldRecorderTraces == o.TracesCounted {
				r.Exercised = "not recorded" // every trace of the owner is from a recorder that did not record handlers
			}
			if own.TracesCounted > 0 {
				r.Exercised = "yes"
			}
			r.Branches, r.BranchesTaken = own.Branches, own.BranchesTaken
			r.Why = ""
		default:
			r.Exercised = "not recorded"
		}
		out = append(out, r)
	}
	return out
}

// signalExercised looks for a decision on the signal in the owner's map:
// "yes" when a counted trace took the exit for it, "no" when the map has
// such an exit and none did, and "not recorded" when no decision shows the
// signal (it is read with a plain Receive, say).
func signalExercised(h model.Handler, g *model.Graph, seen map[string]bool) string {
	if g == nil || h.Name == "" {
		return "not recorded"
	}
	want := fmt.Sprintf("signal %q", h.Name)
	found := false
	for _, j := range g.AllJunctions() {
		if j.Kind != model.Selector {
			continue
		}
		for _, e := range j.Exits {
			if e.Label == want {
				found = true
				if seen[e.ID.String()] {
					return "yes"
				}
			}
		}
	}
	if found {
		return "no"
	}
	return "not recorded"
}

// CallOutcome is one outcome of a call: "success", "failure", or a kind of
// failure the code tells apart ("timeout"); Seen says whether some counted trace
// took it.
type CallOutcome struct {
	Outcome string
	Seen    bool
}

// CallOutcomeRow is one call site of the outcome table.
type CallOutcomeRow struct {
	Workflow string
	File     string // as it should be printed
	Line     int
	Kind     string // "activity", "local activity", "child workflow" or "nexus operation"
	Name     string
	Outcomes []CallOutcome
}

// Gaps are the outcomes no test saw.
func (r CallOutcomeRow) Gaps() []string {
	var out []string
	for _, o := range r.Outcomes {
		if !o.Seen {
			out = append(out, o.Outcome)
		}
	}
	return out
}

// callOutcomeTable lists the checked calls of every measured workflow and the
// outcomes counted traces took (seen: every exit ID a counted trace recorded).
func callOutcomeTable(in Input, seen map[string]map[string]bool) []CallOutcomeRow {
	show := in.Display
	if show == nil {
		show = func(s string) string { return s }
	}
	var out []CallOutcomeRow
	for _, w := range in.Workflows {
		for _, site := range w.Graph.CallSites() {
			row := CallOutcomeRow{Workflow: w.Name, File: show(site.File), Line: site.Line, Kind: site.Kind, Name: site.Name}
			for _, e := range site.Junction.Exits {
				row.Outcomes = append(row.Outcomes, CallOutcome{Outcome: map[string]string{"success": "success", "failure": "failure"}[e.Label], Seen: seen[w.Name][e.ID.String()]})
			}
			// success first, then failure, then the kinds of failure
			sort.SliceStable(row.Outcomes, func(i, k int) bool {
				return row.Outcomes[i].Outcome == "success" && row.Outcomes[k].Outcome != "success"
			})
			for _, f := range site.Failures {
				row.Outcomes = append(row.Outcomes, CallOutcome{Outcome: "failure: " + f.Kind, Seen: seen[w.Name][f.Exit.ID.String()]})
			}
			out = append(out, row)
		}
	}
	return out
}

// LoopRoundsRow is one loop of the loop rounds table.
type LoopRoundsRow struct {
	Workflow string
	File     string
	Line     int
	Loop     string // "for range items"
	Rounds   []RoundCount
}

// RoundCount is one of the three round counts of a loop: whether it can
// happen, and whether some test ran it.
type RoundCount struct {
	Bucket   string // "0 rounds", "1 round" or "2+ rounds"
	Possible bool
	Seen     bool
}

// Gaps are the round counts that can happen and no test ran.
func (r LoopRoundsRow) Gaps() []string {
	var out []string
	for _, c := range r.Rounds {
		if c.Possible && !c.Seen {
			out = append(out, c.Bucket)
		}
	}
	return out
}

// loopRoundsTable builds the table from the rounds counted traces ran. Copies
// of one helper's loop (one per call) are one loop.
type loopInput struct {
	name   string
	g      *model.Graph
	rounds map[*model.Junction]map[string]bool
}

func loopRoundsTable(in Input, states []loopInput) []LoopRoundsRow {
	show := in.Display
	if show == nil {
		show = func(s string) string { return s }
	}
	var out []LoopRoundsRow
	for _, st := range states {
		idx := map[string]int{}
		for _, j := range st.g.Loops() {
			key := fmt.Sprintf("%s:%d:%d", j.Pos.Filename, j.Pos.Line, j.Pos.Column)
			i, ok := idx[key]
			if !ok {
				idx[key] = len(out)
				i = len(out)
				row := LoopRoundsRow{Workflow: st.name, File: show(j.Pos.Filename), Line: j.Pos.Line, Loop: j.Label}
				for _, b := range model.RoundBuckets {
					row.Rounds = append(row.Rounds, RoundCount{Bucket: b})
				}
				out = append(out, row)
			}
			for k, b := range model.RoundBuckets {
				possible := b != model.RoundBuckets[0] || j.CanRun0()
				out[i].Rounds[k].Possible = out[i].Rounds[k].Possible || possible
				out[i].Rounds[k].Seen = out[i].Rounds[k].Seen || st.rounds[j][b]
			}
		}
	}
	return out
}
