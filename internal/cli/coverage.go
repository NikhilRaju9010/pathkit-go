package cli

import (
	"encoding/json"
	"math"
	"path/filepath"
	"sort"
	"strconv"
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
	paths   bool
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
	f.BoolVar(&opt.paths, "paths", false, "list the paths of a workflow with more paths than the list holds (by default only its counts, stages and groups are printed)")
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
		output = render.CoverageTextFull(m.res, m.overallThreshold(), m.showClass, opt.hints, !opt.paths)
		if v := m.classView(); v != nil {
			output += "\n" + render.ClassificationBlock(m.res, *v)
		}
		for _, l := range m.ci.floorLines(m.res) {
			output += l + "\n"
		}
		output += "\n" + render.HandlerBlock(m.res, false) + render.CallOutcomeBlock(m.res, false) + render.LoopRoundBlock(m.res, false) + render.UnreadBlock(m.res, render.UnreadTop)
		output += "\n" + render.ExcludedBlock(m.res.Excluded, m.excludedBy) + render.TracesLine(m.res.Counts)
		if opt.unmatched {
			output += "\n" + render.UnmatchedSection(m.res)
		}
	}
	return finish(cmd, "coverage", m, output, output, opt.out, opt.clean, nil)
}

// maxPrunedJSON is how many pruned groups the JSON lists (the biggest ones);
// the text and the page give the total.
const maxPrunedJSON = 100

// The --json shape (schemaVersion 1), documented in SETUP-GUIDE.md.
type (
	jsonCount struct {
		Total   int     `json:"total"`
		Covered int     `json:"covered"`
		Percent float64 `json:"percent"` // one decimal; use covered/total for exact math
		// Added with the honest numbers: set to false, with the reason, only
		// when total is 0 (the percent is then meaningless); absent otherwise.
		Measured *bool  `json:"measured,omitempty"`
		Reason   string `json:"reason,omitempty"`
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
		Name          string    `json:"name"`
		AddedByConfig bool      `json:"addedByConfig"`
		Paths         jsonCount `json:"paths"`
		Branches      jsonCount `json:"branches"`
		Truncated     bool      `json:"truncated"`
		// Added with several modules: the Go module the workflow is in; absent
		// when the run covers one module.
		Module         string             `json:"module,omitempty"`
		PathList       []jsonPath         `json:"pathList"`
		Classification jsonClassification `json:"classification"`
		// Added with the unexplained-trace check; absent when every trace
		// of the workflow fits a path.
		UnexplainedTraces []jsonUnexplained `json:"unexplainedTraces,omitempty"`
		// Added with exact counts and stages. pathCountExact is the exact
		// number of paths as a decimal string (it can have hundreds of
		// digits). These keys are there when the workflow has several stages or
		// more paths than the list holds; prunedGroups when parts of the map were
		// left out of the list as impossible.
		PathCountExact string `json:"pathCountExact,omitempty"`
		// Added with the old-version-branch count; absent when the workflow has no GetVersion check. An exact count, as a string.
		OldVersionPaths string `json:"oldVersionPaths,omitempty"`
		// Added with summarized helpers; absent when the workflow's helpers are
		// copied into its paths.
		HelpersSummarized bool         `json:"helpersSummarized,omitempty"`
		HelpersReason     string       `json:"helpersReason,omitempty"`
		Helpers           []jsonHelper `json:"helpers,omitempty"`
		PathsListed       int          `json:"pathsListed,omitempty"`
		Stages            []jsonStage  `json:"stages,omitempty"`
		PrunedGroups      []jsonPruned `json:"prunedGroups,omitempty"`
		// PrunedGroupsTotal is how many groups there are when prunedGroups holds only
		// the biggest ones.
		PrunedGroupsTotal int `json:"prunedGroupsTotal,omitempty"`
		// PrunedScope says what the groups cover: only the part of the map the
		// list reached, not every impossible part of the map.
		PrunedScope string     `json:"prunedScope,omitempty"`
		BranchesAll *jsonCount `json:"branchesAll,omitempty"`
	}
	jsonStage struct {
		Index          int       `json:"index"`
		Paths          string    `json:"paths"` // exact, a decimal string
		TooBigToList   bool      `json:"tooBigToList"`
		Covered        int       `json:"covered"`
		Testable       int       `json:"testable"`
		CannotBeTested int       `json:"cannotBeTested"`
		NeedsReview    int       `json:"needsReview"`
		Branches       jsonCount `json:"branches"`
		Contexts       int       `json:"contexts"`
		CutOff         bool      `json:"contextsCutOff"`
		// Added with the cut of too-big stages: Part says the stage is a piece of
		// a bigger stretch (splitFrom paths, a decimal string); splitWhy, on a stage
		// too big to list, says why it could not be cut.
		Part      bool       `json:"part,omitempty"`
		SplitFrom string     `json:"splitFrom,omitempty"`
		SplitWhy  string     `json:"splitWhy,omitempty"`
		PathList  []jsonSPth `json:"pathList,omitempty"`
	}
	jsonSPth struct {
		ID      string   `json:"id"`
		Covered bool     `json:"covered"`
		Traces  int      `json:"traces"`
		Bucket  string   `json:"bucket"`
		Label   *string  `json:"label"`
		End     string   `json:"end"` // how the workflow ends, or "continues" into the next stage
		Steps   []string `json:"steps"`
		// Why PathKit put the stage path outside testable, with its proof; absent
		// for a testable one.
		Explanation string          `json:"explanation,omitempty"`
		Proof       []jsonProofStep `json:"proof,omitempty"`
	}
	jsonPruned struct {
		Paths       string          `json:"paths"`
		Prefix      []string        `json:"prefix"`
		Bucket      string          `json:"bucket"`
		Label       *string         `json:"label"`
		Explanation string          `json:"explanation"`
		UnderReview bool            `json:"underReview"`
		Proof       []jsonProofStep `json:"proof,omitempty"`
		Assumptions []string        `json:"assumptions,omitempty"`
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
		// Added with exact counts: traces that fit the whole map but run a path
		// that is not among the listed ones. They are counted, not unmatched.
		BeyondCap int `json:"beyondCap,omitempty"`
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
		// Added with the known-defect label; absent when no path has it.
		BlockedByKnownBugs int `json:"blockedByKnownBugs,omitempty"`
		// Added with the three headline numbers; absent unless a workflow has
		// more paths than the list holds. The older total, branches and
		// classification keys keep their meaning: listed paths only.
		Headline *jsonHeadline `json:"headline,omitempty"`
		// Added with the honest numbers: the paths left out of the lists as
		// impossible; absent unless some were. The classification counts
		// above are the listed paths only.
		LeftOutImpossible *jsonLeftOut `json:"leftOutImpossible,omitempty"`
		// Added with the "could not fully read" panel: every place PathKit
		// did not follow or cut off; an empty list means it read everything
		// in scope.
		Unread []jsonUnread `json:"unread"`
		// Added with the handler table: every signal, query, update and update
		// validator a workflow registers; absent when none does.
		Handlers []jsonHandlerRow `json:"handlers,omitempty"`
		// Added with the outcome table: every checked activity, child workflow
		// and Nexus call, its outcomes and whether a test saw them; absent when
		// there is none.
		CallOutcomes []jsonCallOutcome `json:"callOutcomes,omitempty"`
		// Added with the loop rounds table: every loop on a map, which of 0, 1
		// and 2+ rounds can happen and which a test ran; absent when no
		// workflow has a loop.
		LoopRounds []jsonLoopRounds `json:"loopRounds,omitempty"`
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
		// Added with the assumptions panel: every assumption, on or off, with
		// how many claims rest on it; absent when classification is off.
		AssumptionPanel []jsonAssumption `json:"assumptionPanel,omitempty"`
	}
	jsonHeadline struct {
		// PathCoverage: the workflows whose paths are all listed (testable
		// figure when classification is on).
		PathCoverage  jsonCount `json:"pathCoverage"`
		PathWorkflows int       `json:"pathWorkflows"`
		// WorkflowsTooBigForPathCoverage is how many workflows are measured by
		// stage coverage instead.
		WorkflowsTooBigForPathCoverage int       `json:"workflowsTooBigForPathCoverage"`
		StageCoverage                  jsonCount `json:"stageCoverage"`
		StagesTooBigToList             int       `json:"stagesTooBigToList"`
		BranchCoverage                 jsonCount `json:"branchCoverage"`
	}
	jsonHelper struct {
		Helper   string    `json:"helper"`
		File     string    `json:"file"`
		Line     int       `json:"line"`
		Branches jsonCount `json:"branches"`
		Sites    int       `json:"sites"` // places that call it
	}
	jsonHandlerRow struct {
		Workflow  string  `json:"workflow"` // the workflow that registers it
		Kind      string  `json:"kind"`     // "signal", "query", "update" or "update validator"
		Name      *string `json:"name"`     // null when the name is computed
		File      string  `json:"file"`
		Line      int     `json:"line"`
		Exercised string  `json:"exercised"` // "yes", "no" or "not recorded"
		// HandlerWorkflow is the small workflow made of the handler's code, when PathKit maps it.
		HandlerWorkflow *string    `json:"handlerWorkflow"`
		Branches        *jsonCount `json:"branches"` // null when the handler has no map
		Why             *string    `json:"why"`
	}
	jsonCallOutcome struct {
		Workflow string `json:"workflow"`
		File     string `json:"file"`
		Line     int    `json:"line"`
		Kind     string `json:"kind"`
		Name     string `json:"name"`
		Outcomes []struct {
			Outcome  string `json:"outcome"`
			Possible bool   `json:"possible"`
			Seen     bool   `json:"seen"`
		} `json:"outcomes"`
	}
	jsonLoopRounds struct {
		Workflow string `json:"workflow"`
		File     string `json:"file"`
		Line     int    `json:"line"`
		Loop     string `json:"loop"`
		Rounds   []struct {
			Rounds   string `json:"rounds"` // "0 rounds", "1 round" or "2+ rounds"
			Possible bool   `json:"possible"`
			Seen     bool   `json:"seen"`
		} `json:"rounds"`
	}
	jsonUnread struct {
		Workflow   string  `json:"workflow"`
		File       string  `json:"file"`
		Line       int     `json:"line"`
		Kind       string  `json:"kind"`
		Call       string  `json:"call"`
		Effect     string  `json:"effect"`
		Suggestion *string `json:"suggestion"` // the activity_wrappers entry that would tell PathKit about the function; null if none
	}
	jsonLeftOut struct {
		// Paths is an exact count (a string; it can pass 64 bits).
		Paths   string            `json:"paths"`
		Groups  int               `json:"groups"`
		ByLabel map[string]string `json:"byLabel"`
		// ListedPathsOnly: the classification numbers do not include them.
		ListedPathsOnly bool `json:"listedPathsOnly"`
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
	panel       []classify.AssumptionStatus
}

func (m *measured) basisInfo() basisInfo {
	return basisInfo{m.basis, m.showClass, m.assumptions, m.panel}
}

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

// count1r is count1 for the headline ratios: with nothing to divide by it adds
// measured:false and the reason.
func count1r(covered, total int, reason string) jsonCount {
	c := count1(covered, total)
	if total == 0 {
		f := false
		c.Measured, c.Reason = &f, reason
	}
	return c
}

func coverageJSON(res coverage.Result, threshold *float64, bi basisInfo) jsonCoverage {
	out := jsonCoverage{
		SchemaVersion: 1, Tool: "pathkit-go",
		Workflows: []jsonWorkflow{}, Excluded: []jsonExcluded{},
		Total:    count1(res.Covered, res.Paths),
		Branches: count1(res.BranchesTaken, res.Branches),
		Traces: jsonTraces{Read: res.Counts.Read, Counted: res.Counts.Counted, Unmatched: res.Counts.Unmatched, Stale: res.Counts.Stale,
			Incomplete: res.Counts.Incomplete, Excluded: res.Counts.Excluded, Unknown: res.Counts.Unknown, Unreadable: res.Counts.Unreadable, Other: res.Counts.Other, BeyondCap: res.Counts.BeyondCap},
		Classification: classificationJSON(res.Breakdown), CoverageBasis: string(bi.basis), ClassificationShown: bi.shown,
	}
	out.BlockedByKnownBugs = render.BlockedByKnownBugs(res)
	if h, ok := res.Headline(); ok {
		out.Headline = &jsonHeadline{PathCoverage: count1r(h.PathCovered, h.PathTotal, strconv.Itoa(h.PathWorkflows)+" workflows under the cap"), PathWorkflows: h.PathWorkflows, WorkflowsTooBigForPathCoverage: h.TooBig,
			StageCoverage: count1r(h.StageCovered, h.StageTotal, "no stage paths to measure"), StagesTooBigToList: h.StagesTooBig, BranchCoverage: count1r(h.BranchCovered, h.BranchTotal, "no branches")}
	}
	out.Unread = unreadJSON(res)
	out.Handlers = handlersJSON(res)
	out.CallOutcomes = callOutcomesJSON(res)
	out.LoopRounds = loopRoundsJSON(res)
	if lo := render.LeftOutImpossible(res); lo.Groups > 0 {
		by := map[string]string{}
		for l, n := range lo.ByLabel {
			by[l] = n.String()
		}
		out.LeftOutImpossible = &jsonLeftOut{Paths: lo.Paths.String(), Groups: lo.Groups, ByLabel: by, ListedPathsOnly: true}
	}
	if names := render.CapNames(res); len(names) > 0 {
		out.PathCap = &jsonPathCap{Limit: model.DefaultMaxPaths, Workflows: names,
			Message: render.CapHeadline(names) + " " + strings.Join(render.CapAdvice(), " ")}
	}
	for _, w := range res.Workflows {
		jw := jsonWorkflow{Name: w.Name, Module: w.Module, AddedByConfig: w.AddedByConfig, Paths: count1(w.Covered, len(w.Paths)),
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
		if render.ShowStages(w) {
			jw.PathCountExact, jw.PathsListed = w.Exact.String(), len(w.Paths)
		}
		if w.OldVersionPaths != nil {
			jw.OldVersionPaths = w.OldVersionPaths.String()
		}
		if w.HelpersSummarized {
			jw.HelpersSummarized, jw.HelpersReason = true, w.HelpersWhy
			for _, u := range w.Units {
				jw.Helpers = append(jw.Helpers, jsonHelper{Helper: u.Name, File: filepath.ToSlash(u.File), Line: u.Line, Branches: count1(u.BranchesTaken, u.Branches), Sites: u.Sites})
			}
		}
		if w.Capped {
			jw.BranchesAll = &jsonCount{Total: w.AllBranches, Covered: w.AllBranchesTaken, Percent: coverage.Percent(w.AllBranchesTaken, w.AllBranches)}
		}
		if render.ShowStages(w) {
			for _, s := range w.Stages {
				js := jsonStage{Part: s.Part, SplitWhy: s.SplitWhy, Index: s.Index + 1, Paths: s.Count.String(), TooBigToList: s.Big, Covered: s.Covered, Testable: s.Testable, CannotBeTested: s.CannotBeTested,
					NeedsReview: s.NeedsReview, Branches: count1(s.BranchesTaken, s.Branches), Contexts: s.Contexts, CutOff: s.CutOff}
				if s.SplitFrom != nil {
					js.SplitFrom = s.SplitFrom.String()
				}
				for _, sp := range s.Paths {
					end := string(sp.Path.End)
					if sp.Path.Continue {
						end = "continues"
					}
					ids := []string{}
					for _, st := range sp.Path.Steps {
						ids = append(ids, st.Exit.ID.String())
					}
					jp := jsonSPth{ID: sp.ID, Covered: sp.Covered, Traces: sp.Traces, Bucket: string(sp.Class.Bucket), Label: optional(sp.Class.Label), End: end, Steps: ids}
					if sp.Class.Bucket != classify.Testable {
						jp.Explanation, jp.Proof = sp.Class.Explanation, proofJSON(sp.Class.Proof)
					}
					js.PathList = append(js.PathList, jp)
				}
				jw.Stages = append(jw.Stages, js)
			}
		}
		pruned := append([]coverage.PrunedResult(nil), w.Pruned...)
		sort.SliceStable(pruned, func(i, k int) bool { return pruned[i].Count.Cmp(pruned[k].Count) > 0 })
		if len(pruned) > maxPrunedJSON {
			jw.PrunedGroupsTotal = len(pruned)
			pruned = pruned[:maxPrunedJSON]
		}
		if len(pruned) > 0 {
			jw.PrunedScope = "the part of the map the list reached"
		}
		for _, pg := range pruned {
			jw.PrunedGroups = append(jw.PrunedGroups, jsonPruned{Paths: pg.Count.String(), Prefix: pg.Prefix, Bucket: string(pg.Class.Bucket), Label: optional(pg.Class.Label),
				Explanation: pg.Class.Explanation, UnderReview: pg.Unpruned, Proof: proofJSON(pg.Class.Proof), Assumptions: pg.Class.Assumptions})
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
	for _, a := range bi.panel {
		ja := jsonAssumption{Name: a.Name, On: a.On, Claims: a.Claims}
		if !a.On {
			why := a.Why
			ja.Why = &why
		}
		out.AssumptionPanel = append(out.AssumptionPanel, ja)
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

func unreadJSON(res coverage.Result) []jsonUnread {
	out := []jsonUnread{}
	for _, u := range res.Unread {
		out = append(out, jsonUnread{Workflow: u.Workflow, File: filepath.ToSlash(u.File), Line: u.Line, Kind: u.Kind, Call: u.Call, Effect: u.Effect, Suggestion: optional(u.Suggestion)})
	}
	return out
}

func handlersJSON(res coverage.Result) []jsonHandlerRow {
	var out []jsonHandlerRow
	for _, h := range res.Handlers {
		file, line := h.Where, 0
		if i := strings.LastIndex(h.Where, ":"); i >= 0 {
			file = h.Where[:i]
			line, _ = strconv.Atoi(h.Where[i+1:])
		}
		row := jsonHandlerRow{Workflow: h.Owner, Kind: h.Kind, Name: optional(h.Name), File: filepath.ToSlash(file), Line: line,
			Exercised: h.Exercised, HandlerWorkflow: optional(h.Workflow), Why: optional(h.Why)}
		if h.Workflow != "" {
			c := count1(h.BranchesTaken, h.Branches)
			row.Branches = &c
		}
		out = append(out, row)
	}
	return out
}

func callOutcomesJSON(res coverage.Result) []jsonCallOutcome {
	var out []jsonCallOutcome
	for _, r := range res.CallOutcomes {
		row := jsonCallOutcome{Workflow: r.Workflow, File: filepath.ToSlash(r.File), Line: r.Line, Kind: r.Kind, Name: r.Name}
		for _, o := range r.Outcomes {
			row.Outcomes = append(row.Outcomes, struct {
				Outcome  string `json:"outcome"`
				Possible bool   `json:"possible"`
				Seen     bool   `json:"seen"`
			}{o.Outcome, true, o.Seen})
		}
		out = append(out, row)
	}
	return out
}

func loopRoundsJSON(res coverage.Result) []jsonLoopRounds {
	var out []jsonLoopRounds
	for _, r := range res.LoopRounds {
		row := jsonLoopRounds{Workflow: r.Workflow, File: filepath.ToSlash(r.File), Line: r.Line, Loop: r.Loop}
		for _, c := range r.Rounds {
			row.Rounds = append(row.Rounds, struct {
				Rounds   string `json:"rounds"`
				Possible bool   `json:"possible"`
				Seen     bool   `json:"seen"`
			}{c.Bucket, c.Possible, c.Seen})
		}
		out = append(out, row)
	}
	return out
}
