package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/baseline"
	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
)

// The CI check (path classification). It is only ever active
// when classification is enabled in the config, so a project that has not
// opted in sees exactly what it saw before, --fail-under included.
//
//   - classification.ci.fail_under_testable: a floor for testable
//     coverage, checked on its own, next to --fail-under (exit 2).
//   - a baseline file (--baseline, or classification.ci.baseline): exit 2
//     when a testable path with no test, or a needs-review path, is not in
//     the baseline as such (internal/baseline.Check).
//   - --write-baseline writes the file instead of checking it.

// ciOptions are the CI flags of coverage and report.
type ciOptions struct {
	baseline       string
	writeBaseline  bool
	allowLostTests bool
}

// addCIFlags adds --baseline, and for report also --write-baseline: only
// report, run with no folder, measures every workflow of the config's
// scope, which is what a baseline must hold.
func addCIFlags(cmd *cobra.Command, opt *ciOptions, withWrite bool) {
	f := cmd.Flags()
	f.StringVar(&opt.baseline, "baseline", "", "check the run against this baseline `file` (default: the config's classification.ci.baseline); needs classification enabled")
	f.BoolVar(&opt.allowLostTests, "allow-lost-tests", false, "do not fail the CI check when a path a test ran in the baseline has no test now (it is still listed, as a warning)")
	if withWrite {
		f.BoolVar(&opt.writeBaseline, "write-baseline", false, "write the baseline file (--baseline, else classification.ci.baseline) from this run instead of checking it; only with no folder argument")
	}
}

// ciState is what the CI check found; nil on measured when it is off.
type ciState struct {
	baselinePath string // as given, for messages
	baselineAbs  string
	write        bool
	floor        *float64
	floorFrom    string
	stageFloor   *float64
	untested     []baseline.Finding
	review       []baseline.Finding
	lost         []baseline.Finding
	allowLost    bool
	checked      bool // a baseline was read and compared
	// anchors are this run's numbered rule entries (written with the
	// baseline); anchorWarnings are the ones that moved since it was.
	anchors        []baseline.RuleAnchor
	anchorWarnings []string
}

// prepareCI checks the CI flags and settings before anything is measured,
// and reads the baseline file (so a missing one stops the run early).
func prepareCI(cmd *cobra.Command, opt ciOptions, sc *scoped, sf scopeFlags, function string) (*ciState, error) {
	fl := cmd.Flags()
	asked := fl.Changed("baseline") || opt.writeBaseline || opt.allowLostTests
	if asked && sf.all {
		return nil, userError("--baseline, --write-baseline and --allow-lost-tests cannot be used with --all: --all ignores the config, and with it the classification the CI check needs")
	}
	cfg := sc.cfg
	if cfg == nil || !cfg.Classification.Enabled {
		if asked {
			where := "a .pathkitrc.json"
			if cfg != nil {
				where = cfg.Path
			}
			return nil, userError("--baseline, --write-baseline and --allow-lost-tests need path classification: set \"enabled\": true under \"classification\" in %s", where)
		}
		return nil, nil
	}
	ci := cfg.Classification.CI
	st := &ciState{write: opt.writeBaseline, allowLost: opt.allowLostTests}
	if ci.FailUnderTestable != nil {
		st.floor, st.floorFrom = ci.FailUnderTestable, cfg.Path
	}
	if ci.StageFloor != nil {
		st.stageFloor, st.floorFrom = ci.StageFloor, cfg.Path
	}
	switch {
	case fl.Changed("baseline"):
		if strings.TrimSpace(opt.baseline) == "" {
			return nil, userError("--baseline needs a file")
		}
		st.baselinePath, st.baselineAbs = opt.baseline, opt.baseline
	case ci.Baseline != "":
		st.baselinePath, st.baselineAbs = ci.Baseline, cfg.Abs(ci.Baseline)
	}
	if st.write {
		if st.baselinePath == "" {
			return nil, userError("--write-baseline needs a file: pass --baseline <file>, or set \"baseline\" under \"classification\": {\"ci\": ...} in %s", cfg.Path)
		}
		// The file must hold every workflow the config's scope counts, or a
		// later full run would find the left-out ones "new".
		var narrowed []string
		if function != "" {
			narrowed = append(narrowed, "--function")
		}
		if sf.include != nil {
			narrowed = append(narrowed, "--include")
		}
		if len(sf.exclude) > 0 {
			narrowed = append(narrowed, "--exclude")
		}
		if !sc.defaulted {
			narrowed = append(narrowed, "a folder argument")
		}
		if len(narrowed) > 0 {
			return nil, userError("--write-baseline cannot be used with %s: the baseline must hold every workflow in the config's scope, so run \"pathkit report --write-baseline\" with no folder (it uses every entry of the config's \"packages\")", strings.Join(narrowed, ", "))
		}
	}
	if opt.allowLostTests && st.baselinePath == "" {
		return nil, userError("--allow-lost-tests needs a baseline to compare with: pass --baseline <file>, or set \"baseline\" under \"classification\": {\"ci\": ...}")
	}
	if opt.allowLostTests && st.write {
		return nil, userError("--allow-lost-tests has no effect with --write-baseline (nothing is checked while the baseline is written)")
	}
	if st.baselinePath == "" && st.floor == nil && st.stageFloor == nil {
		return nil, nil
	}
	return st, nil
}

// compareCI compares the run with the baseline (not when writing it).
func (st *ciState) compare(res coverage.Result) error {
	if st == nil || st.write || st.baselinePath == "" {
		return nil
	}
	b, err := baseline.Read(st.baselineAbs)
	if err != nil {
		return userError("%s", err)
	}
	st.untested, st.review, st.lost = baseline.Check(b, res)
	st.checked = true
	st.anchorWarnings = baseline.AnchorChanges(b.RuleAnchors, st.anchors)
	return nil
}

// ruleAnchors converts the engine's anchors for the baseline file.
func ruleAnchors(as []classify.Anchor) []baseline.RuleAnchor {
	var out []baseline.RuleAnchor
	for _, a := range as {
		out = append(out, baseline.RuleAnchor{Rule: a.Rule, Workflow: a.Workflow, Entry: a.Entry, Before: a.Before, After: a.After, Count: a.Count})
	}
	return out
}

// failures are the CI check's exit-2 messages, each a list of lines: the
// first line gets the "pathkit <command>: " prefix, the rest are indented.
func (st *ciState) failures(res coverage.Result) [][]string {
	if st == nil {
		return nil
	}
	var out [][]string
	if st.floor != nil {
		c := res.Breakdown.TestableCoverage()
		if v := coverage.Percent(c.Covered, c.Paths); v < *st.floor {
			out = append(out, []string{fmt.Sprintf("testable coverage %s is below classification.ci.fail_under_testable %s (in %s)", render.Pct(v, st.floor), render.Threshold(*st.floor), st.floorFrom)})
		}
	}
	if st.stageFloor != nil {
		covered, total := coverage.StageTotals(res)
		bigStages := 0
		for _, w := range res.Workflows {
			if render.ShowStages(w) {
				_, _, b := render.StageCoverage(w)
				bigStages += b
			}
		}
		left := ""
		if bigStages > 0 {
			left = fmt.Sprintf("; %s too big to list are not in this number", count(bigStages, "stage"))
		}
		switch v := coverage.Percent(covered, total); {
		case total == 0:
			out = append(out, []string{fmt.Sprintf("classification.ci.stage_floor %s (in %s) cannot be checked: no stage path is listed%s", render.Threshold(*st.stageFloor), st.floorFrom, left)})
		case v < *st.stageFloor:
			out = append(out, []string{fmt.Sprintf("stage coverage %s (%d of %d stage paths%s) is below classification.ci.stage_floor %s (in %s)", render.Pct(v, st.stageFloor), covered, total, left, render.Threshold(*st.stageFloor), st.floorFrom)})
		}
	}
	text := pathTexts(res)
	list := func(fs []baseline.Finding, what func(int) string) {
		if len(fs) == 0 {
			return
		}
		lines := []string{fmt.Sprintf("%s not in the baseline %s:", what(len(fs)), st.baselinePath)}
		for _, f := range fs {
			was := "new"
			if f.WasBucket != "" {
				was = "was " + string(f.WasBucket)
			}
			lines = append(lines, fmt.Sprintf("  %s path %d (%s, %s): %s", f.Workflow, f.Number, f.StableID, was, text[f.Workflow][f.Number]))
		}
		out = append(out, lines)
	}
	list(st.untested, func(n int) string { return count(n, "testable path") + " with no test" })
	list(st.review, func(n int) string { return count(n, "needs-review path") })
	if !st.allowLost {
		st.listLost(&out, text)
	}
	return out
}

// lostLines lists the paths that lost their test, in the same shape as the
// other lists ("was covered" instead of the old bucket).
func (st *ciState) lostLines(text map[string]map[int]string) []string {
	lines := []string{fmt.Sprintf("%s had a test in the baseline %s and %s none now:", count(len(st.lost), "path"), st.baselinePath, map[bool]string{true: "has", false: "have"}[len(st.lost) == 1])}
	for _, f := range st.lost {
		lines = append(lines, fmt.Sprintf("  %s path %d (%s, now %s): %s", f.Workflow, f.Number, f.StableID, f.NowBucket, text[f.Workflow][f.Number]))
	}
	return lines
}

func (st *ciState) listLost(out *[][]string, text map[string]map[int]string) {
	if len(st.lost) > 0 {
		*out = append(*out, st.lostLines(text))
	}
}

// lostWarnings are the lost-test lines printed as a warning when
// --allow-lost-tests lets them pass.
func (st *ciState) lostWarnings(res coverage.Result) []string {
	if st == nil || !st.allowLost || len(st.lost) == 0 {
		return nil
	}
	lines := st.lostLines(pathTexts(res))
	lines[0] = strings.TrimSuffix(lines[0], ":") + " (allowed by --allow-lost-tests):"
	return lines
}

func pathTexts(res coverage.Result) map[string]map[int]string {
	out := map[string]map[int]string{}
	for _, w := range res.Workflows {
		out[w.Name] = map[int]string{}
		for _, p := range w.Paths {
			out[w.Name][p.Number] = render.PathText(p.Path)
		}
	}
	return out
}

// The "ci" part of --json; absent when the CI check is off.
type (
	jsonCI struct {
		Baseline       *string         `json:"baseline"`
		BaselineMode   string          `json:"baselineMode"` // "check", "write" or "none"
		Floor          *jsonCIFloor    `json:"floor"`
		StageFloor     *jsonCIFloor    `json:"stage_floor,omitempty"`
		NewUntested    []jsonCIFinding `json:"newUntested"`
		NewNeedsReview []jsonCIFinding `json:"newNeedsReview"`
		LostTests      []jsonCIFinding `json:"lostTests"`
		LostAllowed    bool            `json:"lostTestsAllowed"`
		Passed         bool            `json:"passed"`
	}
	jsonCIFloor struct {
		Threshold float64 `json:"threshold"`
		Passed    bool    `json:"passed"`
	}
	jsonCIFinding struct {
		Workflow  string  `json:"workflow"`
		Number    int     `json:"number"`
		StableID  string  `json:"stableId"`
		Text      string  `json:"text"`
		WasBucket *string `json:"wasBucket"` // null: not in the baseline
		NowBucket string  `json:"nowBucket"`
	}
)

func (st *ciState) json(res coverage.Result) *jsonCI {
	if st == nil {
		return nil
	}
	out := &jsonCI{BaselineMode: "none", NewUntested: []jsonCIFinding{}, NewNeedsReview: []jsonCIFinding{}, LostTests: []jsonCIFinding{}, Passed: true}
	if st.baselinePath != "" {
		out.Baseline = &st.baselinePath
		out.BaselineMode = map[bool]string{true: "write", false: "check"}[st.write]
	}
	if st.floor != nil {
		c := res.Breakdown.TestableCoverage()
		out.Floor = &jsonCIFloor{Threshold: *st.floor, Passed: coverage.Percent(c.Covered, c.Paths) >= *st.floor}
		out.Passed = out.Floor.Passed
	}
	if st.stageFloor != nil {
		covered, total := coverage.StageTotals(res)
		out.StageFloor = &jsonCIFloor{Threshold: *st.stageFloor, Passed: coverage.Percent(covered, total) >= *st.stageFloor}
		out.Passed = out.Passed && out.StageFloor.Passed
	}
	text := pathTexts(res)
	conv := func(fs []baseline.Finding) []jsonCIFinding {
		out := []jsonCIFinding{}
		for _, f := range fs {
			out = append(out, jsonCIFinding{Workflow: f.Workflow, Number: f.Number, StableID: f.StableID, Text: text[f.Workflow][f.Number], WasBucket: optional(string(f.WasBucket)), NowBucket: string(f.NowBucket)})
		}
		return out
	}
	out.NewUntested, out.NewNeedsReview, out.LostTests, out.LostAllowed = conv(st.untested), conv(st.review), conv(st.lost), st.allowLost
	if len(st.untested)+len(st.review) > 0 || len(st.lost) > 0 && !st.allowLost {
		out.Passed = false
	}
	return out
}
