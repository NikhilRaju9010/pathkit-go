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
	"fmt"
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
	Name          string
	File          string // where it is declared, as it should be printed (report)
	Graph         *model.Graph
	Hash          string // current model.FunctionHash
	Package       string // import path of the workflow's package ("" when unknown)
	AddedByConfig bool
}

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
}

// Warning is one trace that didn't count, and why.
type Warning struct {
	File     string // base name
	Workflow string
	Kind     trace.Kind
	Message  string
}

// Counts sorts every trace file read.
type Counts struct {
	Read, Counted, Unmatched, Stale, Incomplete, Excluded, Unknown, Unreadable int
	Other                                                                      int // for in-scope workflows not picked with --function
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
	Counts        Counts
	Breakdown     Breakdown
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
	}
	states := map[string]*state{}
	var order []*state
	for _, w := range in.Workflows {
		st := &state{wf: w, paths: w.Graph.Paths(model.DefaultMaxPaths), byKey: map[string]int{}, branches: map[string]bool{}, unexp: map[string]*Unexplained{}}
		for i, p := range st.paths.List {
			st.byKey[p.Key()] = i
			for _, s := range p.Steps {
				st.branches[s.Exit.ID.String()] = false
			}
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
	for _, tf := range in.Traces {
		res.Counts.Read++
		name := filepath.Base(tf.Path)
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
				out = trace.Outcome{Kind: trace.Matched, Path: p}
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
			warn(tf, trace.Unmatched, "trace %s (%s)%s fits no path: %s (\"pathkit report --unmatched\" shows the likely cause)", name, f.Workflow, how, out.Reason)
			id := StepsID(f.Steps)
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
		i, ok := st.byKey[out.Path.Key()]
		if !ok { // a real path beyond the listing cap
			res.Counts.Unmatched++
			warn(tf, trace.Unmatched, "trace %s (%s) took a path beyond the first %d listed paths; the workflow is over the path cap, so split it into smaller workflows", name, f.Workflow, model.DefaultMaxPaths)
			continue
		}
		res.Counts.Counted++
		st.traces[i]++
		if stale {
			st.stale[i] = true
		}
		for _, id := range f.Steps { // raw steps, before loop folding
			if _, onPath := st.branches[id]; onPath {
				st.branches[id] = true
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
		for i, p := range st.paths.List {
			pr := PathResult{Number: i + 1, Path: p, Covered: st.traces[i] > 0, Traces: st.traces[i], StaleTrace: st.stale[i], StableID: p.StableID(), Class: classifier(st.wf.Name, p)}
			if pr.Class.Bucket == "" {
				pr.Class.Bucket = classify.Testable // never leave a path without a bucket
			}
			if why != "" && !pr.Covered {
				if c, changed := classify.DowngradeUnexplained(pr.Class, why); changed {
					pr.Class = c
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
			msg := fmt.Sprintf("%s: %s. Its map may be incomplete, so cannot-be-tested claims by detectors and rules in this workflow are shown as needs-review until the trace is explained (fix the map, re-record, or accept it under classification.explained_traces with a reason)", st.wf.Name, why)
			if manual > 0 {
				msg += fmt.Sprintf("; %s marked by a person %s kept", plural(manual, "path"), map[bool]string{true: "is", false: "are"}[manual == 1])
			}
			res.Warnings = append(res.Warnings, Warning{Workflow: st.wf.Name, Kind: "classification", Message: msg})
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
