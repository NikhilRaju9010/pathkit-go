package cli

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
)

type coverageOptions struct {
	measure measureOptions
	out     string
	json    bool
	clean   bool
	hints   bool
	// unmatched adds the unmatched-trace report (--unmatched).
	unmatched bool
}

func newCoverageCommand() *cobra.Command {
	var opt coverageOptions
	cmd := &cobra.Command{
		Use:     "coverage <file|folder|folder/...>",
		Short:   "Show which paths your tests ran, workflow by workflow",
		Example: "  pathkit coverage ./... --fail-under 80",
		Long: `Compares the traces "pathkit test" recorded with the paths "analyze"
lists, and shows which paths your tests ran.

Exit codes: 0 fine, 1 a real error, 2 coverage below --fail-under, or
(with classification enabled) the CI check failed (see "pathkit report
--help").
Traces that don't count (stale, unmatched, incomplete, ...) are warnings
on stderr; they never change the exit code.`,
		Args: exactlyOne("<file>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCoverage(cmd, args[0], opt)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opt.measure.traces, "traces", defaultTraceDir, "folder the trace files are in (the config's \"traces\" sets another default)")
	f.StringVar(&opt.measure.function, "function", "", "measure only this `workflow`")
	f.StringVar(&opt.out, "out", "", "also write exactly what was printed to this `file`")
	f.StringVar(&opt.measure.coverageBasis, "coverage-basis", "", "which coverage --fail-under checks: `basis`: testable or overall (default: overall; testable when the config enables classification)")
	f.StringVar(&opt.measure.failUnder, "fail-under", "", "exit 2 when path coverage is below this `percent` (0-100)")
	f.BoolVar(&opt.json, "json", false, "print JSON instead of text")
	f.BoolVar(&opt.hints, "hints", false, "under every testable path no test ran, say what a test must make happen to run it")
	f.BoolVar(&opt.unmatched, "unmatched", false, "add a report of the traces that fit no path: where each left the map, what the map expected, and the likely cause")
	f.BoolVar(&opt.measure.allowStale, "allow-stale", false, "also count traces recorded for an older version of a workflow, when they still fit a path")
	f.BoolVar(&opt.clean, "clean", false, "delete the trace files after the report (only when the report was produced)")
	addCIFlags(cmd, &opt.measure.ci, false)
	addScopeFlags(cmd, &opt.measure.scope, false)
	return cmd
}

func runCoverage(cmd *cobra.Command, target string, opt coverageOptions) error {
	m, err := measure(cmd, "coverage", target, opt.measure)
	if err != nil {
		return err
	}
	var output string
	if opt.json {
		var buf strings.Builder
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false) // keep "->" readable in the path text
		enc.SetIndent("", "  ")
		out := coverageJSON(m.res, m.threshold, m.basisInfo())
		out.CI = m.ci.json(m.res)
		if opt.hints {
			addHints(out.Workflows, m.res)
		}
		if opt.unmatched {
			out.Unmatched = unmatchedJSON(m.res)
		}
		if err := enc.Encode(out); err != nil {
			return err
		}
		output = buf.String()
	} else {
		output = render.CoverageTextHints(m.res, m.overallThreshold(), m.showClass, opt.hints)
		if v := m.classView(); v != nil {
			output += "\n" + render.ClassificationBlock(m.res, *v)
		}
		output += "\n" + render.ExcludedBlock(m.res.Excluded, m.excludedBy) + render.TracesLine(m.res.Counts)
		if opt.unmatched {
			output += "\n" + render.UnmatchedSection(m.res)
		}
	}
	return finish(cmd, "coverage", m, output, output, opt.out, opt.clean, nil)
}

// The --json shape (schemaVersion 1), documented in SETUP-GUIDE.md.
type (
	jsonCount struct {
		Total   int     `json:"total"`
		Covered int     `json:"covered"`
		Percent float64 `json:"percent"` // one decimal; use covered/total for exact math
	}
	jsonPath struct {
		Number       int      `json:"number"`
		ID           string   `json:"id"`
		Text         string   `json:"text"`
		Steps        []string `json:"steps"`
		End          string   `json:"end"`
		Compensation bool     `json:"compensation"`
		Covered      bool     `json:"covered"`
		Traces       int      `json:"traces"`
		StaleTrace   bool     `json:"staleTrace"`
		// Added with path classification; every field above is unchanged.
		StableID             string  `json:"stableId"`
		Bucket               string  `json:"bucket"`
		Label                *string `json:"label"` // null for a plain testable path
		Source               string  `json:"source"`
		Confidence           string  `json:"confidence"`
		PossibleInProduction bool    `json:"possibleInProduction"`
		Explanation          string  `json:"explanation"`
		RuleID               *string `json:"ruleId"`
		// Other things that also applied; left out when there are none.
		ExtraReasons []string `json:"extraReasons,omitempty"`
		// Added with claim proofs: the steps behind a detector's claim;
		// left out when the path has none.
		Proof []jsonProofStep `json:"proof,omitempty"`
		// Added with the type facts: the named assumptions the claim rests on.
		Assumptions []string `json:"assumptions,omitempty"`
		// With --hints only, on testable paths no test ran.
		HowToCover []string `json:"howToCover,omitempty"`
	}
	jsonProofStep struct {
		Step int    `json:"step"` // 1-based step on the path; 0 for the closing sentence
		File string `json:"file,omitempty"`
		Line int    `json:"line,omitempty"`
		Text string `json:"text"`
	}
	jsonBucket struct {
		Paths   int `json:"paths"`
		Covered int `json:"covered"`
	}
	jsonClassification struct {
		Buckets struct {
			Testable         jsonBucket `json:"testable"`
			CannotBeTested   jsonBucket `json:"cannotBeTested"`
			NeedsSpecialTest jsonBucket `json:"needsSpecialTest"`
			NeedsReview      jsonBucket `json:"needsReview"`
		} `json:"buckets"`
		TestableCoverage    jsonCount `json:"testableCoverage"`
		SpecialTestCoverage jsonCount `json:"specialTestCoverage"`
		OverallCoverage     jsonCount `json:"overallCoverage"`
	}
	jsonWorkflow struct {
		Name           string             `json:"name"`
		AddedByConfig  bool               `json:"addedByConfig"`
		Paths          jsonCount          `json:"paths"`
		Branches       jsonCount          `json:"branches"`
		Truncated      bool               `json:"truncated"`
		PathList       []jsonPath         `json:"pathList"`
		Classification jsonClassification `json:"classification"`
		// Added with the unexplained-trace check; absent when every trace
		// of the workflow fits a path.
		UnexplainedTraces []jsonUnexplained `json:"unexplainedTraces,omitempty"`
	}
	jsonUnexplained struct {
		StepsID      string   `json:"stepsId"`
		Steps        []string `json:"steps"`
		Count        int      `json:"count"`
		Mismatch     string   `json:"mismatch"`
		Acknowledged bool     `json:"acknowledged"`
		Reason       *string  `json:"reason"`
	}
	jsonExcluded struct {
		Name   string `json:"name"`
		Reason string `json:"reason"`
	}
	jsonTraces struct {
		Read       int `json:"read"`
		Counted    int `json:"counted"`
		Unmatched  int `json:"unmatched"`
		Stale      int `json:"stale"`
		Incomplete int `json:"incomplete"`
		Excluded   int `json:"excluded"`
		Unknown    int `json:"unknownWorkflow"`
		Unreadable int `json:"unreadable"`
		Other      int `json:"otherWorkflows"`
	}
	jsonFailUnder struct {
		Threshold float64 `json:"threshold"`
		Passed    bool    `json:"passed"`
		Basis     string  `json:"basis"` // added with path classification
	}
	jsonCoverage struct {
		SchemaVersion int            `json:"schemaVersion"`
		Tool          string         `json:"tool"`
		Workflows     []jsonWorkflow `json:"workflows"`
		Excluded      []jsonExcluded `json:"excluded"`
		Total         jsonCount      `json:"total"`
		Branches      jsonCount      `json:"branches"`
		Traces        jsonTraces     `json:"traces"`
		FailUnder     *jsonFailUnder `json:"failUnder"`
		// Added with path classification.
		Classification      jsonClassification `json:"classification"`
		CoverageBasis       string             `json:"coverageBasis"`
		ClassificationShown bool               `json:"classificationShown"`
		// Added with the CI check; absent when it is off.
		CI *jsonCI `json:"ci,omitempty"`
		// With --unmatched only.
		Unmatched *[]jsonUnmatched `json:"unmatchedTraces,omitempty"`
		// Added with the path-cap warning; absent when no workflow reached
		// the cap.
		PathCap *jsonPathCap `json:"pathCapWarning,omitempty"`
		// Added with the type facts: assumptions that are off, or that
		// claims rest on; absent when there is nothing to say.
		Assumptions []jsonAssumption `json:"assumptions,omitempty"`
	}
	jsonAssumption struct {
		Name   string  `json:"name"`
		On     bool    `json:"on"`
		Why    *string `json:"why"` // why it is off; null when on
		Claims int     `json:"claims"`
	}
	jsonPathCap struct {
		Limit     int      `json:"limit"`
		Workflows []string `json:"workflows"`
		Message   string   `json:"message"`
	}
	jsonUnmatched struct {
		Workflow     string   `json:"workflow"`
		StepsID      string   `json:"stepsId"`
		Count        int      `json:"count"`
		Acknowledged bool     `json:"acknowledged"`
		Reason       *string  `json:"reason"`
		Steps        []string `json:"steps"`
		Followed     string   `json:"followed"`
		Then         string   `json:"then"`
		Expected     []string `json:"expected"`
		LikelyCause  string   `json:"likelyCause"`
	}
)

// basisInfo is the coverage basis, for the JSON and the HTML page.
type basisInfo struct {
	basis       coverage.Basis
	shown       bool // the classification block is printed (see measured.showClass)
	assumptions []classify.AssumptionStatus
}

func (m *measured) basisInfo() basisInfo { return basisInfo{m.basis, m.showClass, m.assumptions} }

func classificationJSON(b coverage.Breakdown) jsonClassification {
	var c jsonClassification
	set := func(dst *jsonBucket, src coverage.BucketCount) { *dst = jsonBucket{src.Paths, src.Covered} }
	set(&c.Buckets.Testable, b.Testable)
	set(&c.Buckets.CannotBeTested, b.CannotBeTested)
	set(&c.Buckets.NeedsSpecialTest, b.NeedsSpecialTest)
	set(&c.Buckets.NeedsReview, b.NeedsReview)
	t, sp, o := b.TestableCoverage(), b.Special(), b.Overall()
	c.TestableCoverage, c.SpecialTestCoverage, c.OverallCoverage = count1(t.Covered, t.Paths), count1(sp.Covered, sp.Paths), count1(o.Covered, o.Paths)
	return c
}

func proofJSON(ps []classify.ProofStep) []jsonProofStep {
	var out []jsonProofStep
	for _, s := range ps {
		out = append(out, jsonProofStep{Step: s.Step, File: s.File, Line: s.Line, Text: s.Text})
	}
	return out
}

func count1(covered, total int) jsonCount {
	return jsonCount{Total: total, Covered: covered, Percent: math.Round(coverage.Percent(covered, total)*10) / 10}
}

func coverageJSON(res coverage.Result, threshold *float64, bi basisInfo) jsonCoverage {
	out := jsonCoverage{
		SchemaVersion: 1, Tool: "pathkit-go",
		Workflows: []jsonWorkflow{}, Excluded: []jsonExcluded{},
		Total:    count1(res.Covered, res.Paths),
		Branches: count1(res.BranchesTaken, res.Branches),
		Traces: jsonTraces{Read: res.Counts.Read, Counted: res.Counts.Counted, Unmatched: res.Counts.Unmatched, Stale: res.Counts.Stale,
			Incomplete: res.Counts.Incomplete, Excluded: res.Counts.Excluded, Unknown: res.Counts.Unknown, Unreadable: res.Counts.Unreadable, Other: res.Counts.Other},
		Classification: classificationJSON(res.Breakdown), CoverageBasis: string(bi.basis), ClassificationShown: bi.shown,
	}
	if names := render.CapNames(res); len(names) > 0 {
		out.PathCap = &jsonPathCap{Limit: model.DefaultMaxPaths, Workflows: names,
			Message: render.CapHeadline(names) + " " + strings.Join(render.CapAdvice(), " ")}
	}
	for _, w := range res.Workflows {
		jw := jsonWorkflow{Name: w.Name, AddedByConfig: w.AddedByConfig, Paths: count1(w.Covered, len(w.Paths)),
			Branches: count1(w.BranchesTaken, w.Branches), Truncated: w.Truncated, PathList: []jsonPath{}, Classification: classificationJSON(w.Breakdown)}
		for _, p := range w.Paths {
			steps := []string{}
			for _, s := range p.Path.Steps {
				steps = append(steps, s.Exit.ID.String())
			}
			jw.PathList = append(jw.PathList, jsonPath{Number: p.Number, ID: p.Path.ID(), Text: render.PathText(p.Path), Steps: steps,
				End: string(p.Path.End), Compensation: p.Path.Compensation, Covered: p.Covered, Traces: p.Traces, StaleTrace: p.StaleTrace,
				StableID: p.StableID, Bucket: string(p.Class.Bucket), Label: optional(p.Class.Label), Source: string(p.Class.Source),
				Confidence: string(p.Class.Confidence), PossibleInProduction: p.Class.PossibleInProduction, Explanation: p.Class.Explanation, RuleID: optional(p.Class.RuleID), ExtraReasons: p.Class.ExtraReasons, Proof: proofJSON(p.Class.Proof), Assumptions: p.Class.Assumptions})
		}
		for _, u := range w.Unexplained {
			jw.UnexplainedTraces = append(jw.UnexplainedTraces, jsonUnexplained{StepsID: u.StepsID, Steps: u.Steps, Count: u.Count, Mismatch: u.Mismatch,
				Acknowledged: u.Acknowledged, Reason: optional(u.Reason)})
		}
		out.Workflows = append(out.Workflows, jw)
	}
	for _, e := range res.Excluded {
		out.Excluded = append(out.Excluded, jsonExcluded{e.Name, e.Reason})
	}
	for _, a := range bi.assumptions {
		ja := jsonAssumption{Name: a.Name, On: a.On, Claims: a.Claims}
		if !a.On {
			why := a.Why
			ja.Why = &why
		}
		out.Assumptions = append(out.Assumptions, ja)
	}
	if threshold != nil {
		c := res.Breakdown.For(bi.basis)
		out.FailUnder = &jsonFailUnder{Threshold: *threshold, Passed: coverage.Percent(c.Covered, c.Paths) >= *threshold, Basis: string(bi.basis)}
	}
	return out
}

// addHints fills howToCover (--hints) on every testable path no test ran.
func addHints(ws []jsonWorkflow, res coverage.Result) {
	for i, w := range res.Workflows {
		for k, p := range w.Paths {
			if render.WantsHint(p) {
				ws[i].PathList[k].HowToCover = render.HowToCover(p.Path)
			}
		}
	}
}

// unmatchedJSON is the unmatched-trace report for --json --unmatched (an
// empty list when every trace fits, so "none" is visible).
func unmatchedJSON(res coverage.Result) *[]jsonUnmatched {
	out := []jsonUnmatched{}
	for _, u := range render.UnmatchedTraces(res) {
		exp := u.Expected
		if exp == nil {
			exp = []string{}
		}
		out = append(out, jsonUnmatched{Workflow: u.Workflow, StepsID: u.StepsID, Count: u.Count, Acknowledged: u.Acknowledged, Reason: optional(u.Reason),
			Steps: u.Steps, Followed: u.Followed, Then: u.Then, Expected: exp, LikelyCause: u.Cause})
	}
	return &out
}

// optional is a JSON string that is null when empty.
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
