// Package coverage works out which listed paths the recorded runs took.
// It only calculates; printing is done by the caller (render, cli).
//
// Rules (CLAUDE.md D2, D5, D9 and the M6 decisions):
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
	"fmt"
	"path/filepath"
	"sort"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
	"github.com/NikhilRaju9010/pathkit-go/internal/trace"
)

// Workflow is one in-scope workflow to measure.
type Workflow struct {
	Name          string
	Graph         *model.Graph
	Hash          string // current model.FunctionHash
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
}

// PathResult is one listed path and whether it was run.
type PathResult struct {
	Number     int // as analyze numbers it
	Path       model.Path
	Covered    bool
	Traces     int  // how many counted traces landed on it
	StaleTrace bool // covered, and a stale trace counted for it (--allow-stale)
}

// WorkflowResult is one workflow's coverage.
type WorkflowResult struct {
	Name          string
	AddedByConfig bool
	Paths         []PathResult
	Truncated     bool // more paths exist than the listing shows
	Covered       int
	Branches      int // exits on the listed paths
	BranchesTaken int
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
}

// Percent is covered / total as a percentage (0 when there is nothing).
func Percent(covered, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(covered) / float64(total) * 100
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
	}
	states := map[string]*state{}
	var order []*state
	for _, w := range in.Workflows {
		st := &state{wf: w, paths: w.Graph.Paths(model.DefaultMaxPaths), byKey: map[string]int{}, branches: map[string]bool{}}
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

	var res Result
	res.Excluded = in.Excluded
	warn := func(tf TraceFile, kind trace.Kind, format string, args ...any) {
		res.Warnings = append(res.Warnings, Warning{File: filepath.Base(tf.Path), Workflow: tf.File.Workflow, Kind: kind, Message: fmt.Sprintf(format, args...)})
	}
	for _, tf := range in.Traces {
		res.Counts.Read++
		name := filepath.Base(tf.Path)
		if tf.ReadErr != nil {
			res.Counts.Unreadable++
			warn(tf, "unreadable", "could not read trace %s: %v", name, tf.ReadErr)
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
			warn(tf, trace.UnknownWorkflow, "trace %s: workflow %s is not among the analyzed workflows", name, f.Workflow)
			continue
		}
		out := trace.Check(f, st.wf.Graph, st.wf.Hash)
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
				out = trace.Outcome{Kind: trace.Unmatched, Reason: mm.Reason}
			} else {
				out = trace.Outcome{Kind: trace.Matched, Path: p}
			}
		}
		switch out.Kind {
		case trace.Incomplete:
			res.Counts.Incomplete++
			warn(tf, trace.Incomplete, "trace %s (%s): the run never finished (panic, timeout, or stopped)", name, f.Workflow)
			continue
		case trace.Unmatched:
			res.Counts.Unmatched++
			how := ""
			if stale {
				how = " (a stale trace, allowed by --allow-stale)"
			}
			warn(tf, trace.Unmatched, "trace %s (%s)%s fits no path: %s", name, f.Workflow, how, out.Reason)
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
			warn(tf, trace.Unmatched, "trace %s (%s) took a path beyond the first %d listed paths", name, f.Workflow, model.DefaultMaxPaths)
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

	for _, st := range order {
		wr := WorkflowResult{Name: st.wf.Name, AddedByConfig: st.wf.AddedByConfig, Truncated: st.paths.Truncated, Branches: len(st.branches)}
		for i, p := range st.paths.List {
			pr := PathResult{Number: i + 1, Path: p, Covered: st.traces[i] > 0, Traces: st.traces[i], StaleTrace: st.stale[i]}
			if pr.Covered {
				wr.Covered++
			}
			wr.Paths = append(wr.Paths, pr)
		}
		for _, taken := range st.branches {
			if taken {
				wr.BranchesTaken++
			}
		}
		res.Workflows = append(res.Workflows, wr)
		res.Paths += len(wr.Paths)
		res.Covered += wr.Covered
		res.Branches += wr.Branches
		res.BranchesTaken += wr.BranchesTaken
	}
	sort.SliceStable(res.Warnings, func(i, j int) bool { return res.Warnings[i].File < res.Warnings[j].File })
	return res
}
