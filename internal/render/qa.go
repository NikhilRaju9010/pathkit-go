package render

import (
	"fmt"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/advice"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
)

// The report for people who test (milestone M11, R4, R5): branch coverage
// first, in plain words, then the workflows worst first, then what to test next.
// Everything older is under "More detail". The technical report (--technical)
// is ReportText, unchanged.

// TopNext is how many missed outcomes the full report lists under "What to test
// next"; the summary lists TopNextSummary.
const (
	TopNext        = 10
	TopNextSummary = 5
)

// riskPhrase is a risk with an article, for the summary sentence.
func riskPhrase(tag string) string {
	switch tag {
	case advice.TagCancel:
		return "a cancel"
	case advice.TagCompensation:
		return "an undo (compensation)"
	case advice.TagFailure:
		return "an activity failure"
	case advice.TagContinue:
		return "a continue-as-new"
	case advice.TagTimer:
		return "a timer or timeout"
	case advice.TagSignal:
		return "a signal or update"
	case advice.TagOldVersion:
		return "an old-version branch"
	}
	return "a plain decision"
}

// BranchHeadlineLines are the lines 1 to 5 of the headline.
func BranchHeadlineLines(res coverage.Result, items []advice.OutcomeItem) []string {
	h := res.BranchHeadline()
	var lines []string
	if h.Possible == 0 {
		lines = append(lines, "Branch coverage: no decisions to test")
	} else {
		l := fmt.Sprintf("Branch coverage: %d / %d outcomes that can happen — %s — %s", h.Covered, h.Possible, Pct(h.Percent, nil), h.Grade)
		if h.Impossible > 0 {
			l += fmt.Sprintf(" (%d can't happen, removed)", h.Impossible)
		}
		lines = append(lines, l)
	}
	second := []string{"Two-step combinations: " + Ratio(h.PairsCovered, h.PairsTotal, nil, "none to measure")}
	cond := "Each part of a condition: " + Ratio(h.Conditions.Covered, h.Conditions.Total, nil, "no conditions with && or ||")
	if h.Conditions.NotRecorded {
		cond = "Each part of a condition: not recorded by these test runs (run the tests again)"
	}
	second = append(second, cond, fmt.Sprintf("Blocked by known bugs: %d", h.Blocked))
	lines = append(lines, strings.Join(second, "  ·  "))
	if dc := res.DecisionCheck; dc != nil {
		lines = append(lines, fmt.Sprintf("Tests notice wrong decisions: %s (last check %s)", Pct(dc.Score(), nil), dc.Date))
	}
	if bugs := coverage.PossibleBugs(res); len(bugs) > 0 {
		lines = append(lines, fmt.Sprintf("Possible bugs found: %d", len(bugs)))
	}
	lines = append(lines, SummarySentence(res, items))
	return lines
}

// SummarySentence is the one plain sentence under the headline.
func SummarySentence(res coverage.Result, items []advice.OutcomeItem) string {
	h := res.BranchHeadline()
	if h.Possible == 0 {
		return "There are no decisions to test in these workflows."
	}
	s := fmt.Sprintf("Tests cover %d of %d decision outcomes that can happen (%s, %s).", h.Covered, h.Possible, Pct(h.Percent, nil), h.Grade)
	missed := h.Possible - h.Covered - h.Blocked
	switch {
	case missed <= 0 && h.Blocked == 0:
		s += " Every one of them is tested."
	case missed <= 0:
		s += fmt.Sprintf(" %d more cannot be reached until a known bug is fixed.", h.Blocked)
	default:
		never := fmt.Sprintf(" %d are never tested", missed)
		if missed == 1 {
			never = " 1 is never tested"
		}
		s += never
		if len(items) > 0 {
			s += fmt.Sprintf("; the riskiest is %s at %s:%d", riskPhrase(items[0].Risk), items[0].File, items[0].Line)
		}
		s += "."
	}
	return s
}

// WorkflowTable is one row per workflow, worst first, and the line saying how
// many are at the team's target grade.
func WorkflowTable(res coverage.Result) string {
	rows := res.WorkflowRows()
	if len(rows) == 0 {
		return ""
	}
	nameW := len("Workflow")
	for _, r := range rows {
		nameW = max(nameW, len(r.Name))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-*s  %-16s  %-22s  %6s  %s\n", nameW, "Workflow", "Outcomes tested", "Grade", "Missed", "Two-step combinations")
	for _, r := range rows {
		tested := "no decisions"
		if r.Possible > 0 {
			tested = fmt.Sprintf("%d / %d  %s", r.Covered, r.Possible, Pct(r.Percent, nil))
		}
		pairs := "—"
		if r.PairsTotal > 0 {
			pairs = fmt.Sprintf("%d / %d", r.PairsCovered, r.PairsTotal)
		}
		fmt.Fprintf(&b, "%-*s  %-16s  %-22s  %6d  %s\n", nameW, r.Name, tested, r.Grade, r.Missed, pairs)
	}
	h := res.BranchHeadline()
	fmt.Fprintf(&b, "\n%d of %d workflows at %s or better", h.AtTarget, h.Workflows-h.NoDecision, h.TargetGrade)
	if h.NoDecision > 0 {
		fmt.Fprintf(&b, " (%d with no decisions to test not counted)", h.NoDecision)
	}
	b.WriteString("\n")
	return b.String()
}

// NextBlock is "What to test next": the top missed outcomes.
func NextBlock(items []advice.OutcomeItem, top int) string {
	if len(items) == 0 {
		return "What to test next: nothing — every decision outcome that can happen has a test.\n"
	}
	if top > 0 && len(items) > top {
		items = items[:top]
	}
	var b strings.Builder
	b.WriteString("What to test next\n")
	for _, it := range items {
		fmt.Fprintf(&b, "  %d. [%s] %s:%d — %s\n", it.Rank, advice.RiskWord(it.Risk), it.File, it.Line, it.Sentence)
		covers := ""
		switch {
		case it.Paths > 0 && it.Scenarios > 0:
			covers = fmt.Sprintf(" · covers %d path(s), %d scenario(s)", it.Paths, it.Scenarios)
		case it.Paths > 0:
			covers = fmt.Sprintf(" · covers %d path(s)", it.Paths)
		}
		fmt.Fprintf(&b, "     Test name: %s%s\n", it.TestName, covers)
	}
	return b.String()
}

// MissedOutcomesBlock is every missed outcome, one line each (branches --missed).
func MissedOutcomesBlock(items []advice.OutcomeItem) string {
	if len(items) == 0 {
		return "No missed outcomes.\n"
	}
	var b strings.Builder
	for _, it := range items {
		fmt.Fprintf(&b, "%s:%d  [%s]  %s  (%s, workflow %s)\n", it.File, it.Line, advice.RiskWord(it.Risk), it.Sentence, it.ID, it.Workflow)
	}
	return b.String()
}

// PossibleBugsBlock lists each place the code tests a pointed-to value on a pointer.
func PossibleBugsBlock(res coverage.Result) string {
	bugs := coverage.PossibleBugs(res)
	if len(bugs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Possible bugs found\n")
	for _, x := range bugs {
		fmt.Fprintf(&b, "  %s:%d — %s\n", x.File, x.Line, x.Hint)
	}
	return b.String()
}

// DecisionSurvivorsBlock lists the decisions that went the other way without a
// test failing.
func DecisionSurvivorsBlock(res coverage.Result) string {
	dc := res.DecisionCheck
	if dc == nil || len(dc.Survivors) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Decisions no test noticed (the code was changed to go the other way and every test still passed)\n")
	for _, s := range dc.Survivors {
		fmt.Fprintf(&b, "  %s:%d — %s\n", s.File, s.Line, s.Sentence)
	}
	return b.String()
}

// QAOptions control the report for people.
type QAOptions struct {
	// Detail is the older, technical report printed under "More detail" (nil
	// for none: --summary and the branches command).
	Detail string
	// Summary lists the top 5 only and no detail.
	Summary bool
	Items   []advice.OutcomeItem
}

// QAReportText is the report for people who test.
func QAReportText(res coverage.Result, opt QAOptions) string {
	var b strings.Builder
	b.WriteString(CapWarning(res))
	b.WriteString(ZeroPathWarning(res))
	b.WriteString(strings.Join(BranchHeadlineLines(res, opt.Items), "\n") + "\n\n")
	b.WriteString(WorkflowTable(res))
	b.WriteString("\n")
	top := TopNext
	if opt.Summary {
		top = TopNextSummary
	}
	b.WriteString(NextBlock(opt.Items, top))
	if s := SplitCandidatesBlock(res, nil); s != "" {
		b.WriteString("\n" + s)
	}
	if s := PossibleBugsBlock(res); s != "" {
		b.WriteString("\n" + s)
	}
	if s := DecisionSurvivorsBlock(res); s != "" {
		b.WriteString("\n" + s)
	}
	if !opt.Summary && opt.Detail != "" {
		b.WriteString("\nMore detail\n===========\n")
		b.WriteString(Plain(opt.Detail))
	}
	return b.String()
}

// SplitCandidates are the places to cut a big workflow (one whose main number is
// not path coverage): the biggest section, and the helpers whose decisions the
// paths repeat. Advice only; no number changes (milestone M11, O3).
func SplitCandidates(w coverage.WorkflowResult, display func(string) string) []string {
	if w.Main.Kind == coverage.MainPath {
		return nil
	}
	var out []string
	if s := SplitAdvice(w, display); s != "" {
		out = append(out, s)
	}
	n := 0
	for _, h := range w.HelperRegions {
		if h.Decisions < 2 && h.Sites < 2 || h.Score() < 3 {
			continue
		}
		out = append(out, fmt.Sprintf("Helper candidate: %s (%s:%d) holds %s and is called from %s, so its decisions are repeated in the paths; testing it on its own would cut that", h.Name, strings.ReplaceAll(h.File, "\\", "/"), h.Line, count(h.Decisions, "decision"), count(h.Sites, "place")))
		if n++; n == 3 {
			break
		}
	}
	return out
}

// SplitCandidatesBlock is the section of the report for people that lists them.
func SplitCandidatesBlock(res coverage.Result, display func(string) string) string {
	var b strings.Builder
	for _, w := range res.Workflows {
		for _, l := range SplitCandidates(w, display) {
			fmt.Fprintf(&b, "  %s: %s\n", w.Name, l)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "Ways to make a big workflow easier to test (advice only; no number depends on it)\n" + b.String()
}
