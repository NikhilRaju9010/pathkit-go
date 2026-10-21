package htmlreport

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/advice"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
)

// The page for people who test (milestone M11, R2): branch coverage first,
// what to test next, the workflows worst first, a decision checklist per
// workflow; everything the page showed before is under "More detail".

type (
	heroView struct {
		Percent, Grade, GradeKey, GradeIcon string
		Covered, Possible                   int
		Impossible, Blocked                 int
		Bar                                 float64
		Sentence                            string
		NoDecisions                         bool
	}
	smallCardView struct {
		Tip, Title, Value, Detail string
	}
	nextRowView struct {
		Rank                                       int
		Risk, RiskIcon, Where, Sentence, Name, Why string
	}
	wfRowView struct {
		Name, Anchor, Tested, Percent, Grade, GradeKey, GradeIcon, Pairs string
		Bar                                                              float64
		Missed                                                           int
		NoDecisions                                                      bool
	}
	outcomeView struct {
		Label, Status, Icon, Word, Why string
	}
	decisionView struct {
		Text, Where string
		Missed      bool
		// Cleanup: the decision runs under a disconnected context; FirstCleanup is
		// the first of them, which carries the heading of the block.
		Cleanup, FirstCleanup bool
		Outcomes              []outcomeView
	}
	wfSection struct {
		Name, Anchor, Summary string
		Decisions             []decisionView
		Missed                int
		Open                  bool
	}
	bugView struct {
		Workflow, Where, Hint string
	}
)

var gradeIcons = map[string][2]string{ // grade -> key, icon
	"exemplary":   {"exemplary", "★"},
	"commendable": {"commendable", "✔"},
	"acceptable":  {"acceptable", "●"},
	"needs work":  {"needswork", "✘"},
}

func gradeLook(grade string) (key, icon string) {
	if g, ok := gradeIcons[grade]; ok {
		return g[0], g[1]
	}
	return "nodecisions", "–"
}

var riskIcons = map[string]string{
	advice.TagCancel: "⏹", advice.TagCompensation: "↩", advice.TagFailure: "✘", advice.TagContinue: "↻",
	advice.TagTimer: "⏱", advice.TagSignal: "✉", advice.TagLogic: "◇", advice.TagOldVersion: "⌛",
}

var statusLook = map[string][2]string{ // status -> icon, word
	coverage.OutcomeCovered:    {"✔", "Tested"},
	coverage.OutcomeMissed:     {"✘", "Missed"},
	coverage.OutcomeImpossible: {"∅", "Can't happen"},
	coverage.OutcomeBlocked:    {"⚠", "Blocked by a known bug"},
}

// maxChecklistDecisions is how many decisions of one workflow the page writes
// out; a workflow with more says how many it left out (the JSON has them all).
const maxChecklistDecisions = 400

// buildPeople fills the parts of the page made for people who test.
func (v *view) buildPeople(res coverage.Result, items []advice.OutcomeItem) {
	h := res.BranchHeadline()
	key, icon := gradeLook(h.Grade)
	hero := &heroView{Percent: render.Pct(h.Percent, nil), Grade: capitalize(h.Grade), GradeKey: key, GradeIcon: icon, Covered: h.Covered, Possible: h.Possible,
		Impossible: h.Impossible, Blocked: h.Blocked, Bar: math.Round(h.Percent*10) / 10, Sentence: render.SummarySentence(res, items), NoDecisions: h.Possible == 0}
	if hero.NoDecisions {
		hero.Grade = "No decisions to test"
	}
	v.Hero = hero
	v.SmallCards = []smallCardView{
		{"two-step", "Two-step combinations", ratioOrNone(h.PairsCovered, h.PairsTotal, "none"), ""},
		{"condition-part", "Each part of a condition", ratioOrNone(h.Conditions.Covered, h.Conditions.Total, "none"), ""},
		{"known-bug", "Blocked by known bugs", itoa(h.Blocked), "still counted in the percentage"},
	}
	if h.Conditions.NotRecorded {
		v.SmallCards[1].Value, v.SmallCards[1].Detail = "not recorded", "these test runs were recorded by an older PathKit; run the tests again"
	}
	if dc := res.DecisionCheck; dc != nil {
		v.SmallCards = append(v.SmallCards, smallCardView{"notice", "Tests notice wrong decisions", render.Pct(dc.Score(), nil), "last check " + dc.Date})
	}
	if bugs := coverage.PossibleBugs(res); len(bugs) > 0 {
		v.SmallCards = append(v.SmallCards, smallCardView{"possible-bugs-card", "Possible bugs found", itoa(len(bugs)), "listed below"})
		for _, b := range bugs {
			v.Bugs = append(v.Bugs, bugView{Workflow: b.Workflow, Where: fmt.Sprintf("%s:%d", filepath.ToSlash(b.File), b.Line), Hint: b.Hint})
		}
	}
	if dc := res.DecisionCheck; dc != nil {
		for _, s := range dc.Survivors {
			v.Survivors = append(v.Survivors, bugView{Workflow: s.Workflow, Where: fmt.Sprintf("%s:%d", s.File, s.Line), Hint: s.Sentence})
		}
	}
	top := items
	if len(top) > render.TopNext {
		top = top[:render.TopNext]
	}
	for _, it := range top {
		why := "" // nothing listed goes through it (a helper): no count to show
		switch {
		case it.Paths > 0 && it.Scenarios > 0:
			why = fmt.Sprintf("%d path(s), %d scenario(s)", it.Paths, it.Scenarios)
		case it.Paths > 0:
			why = fmt.Sprintf("%d path(s)", it.Paths)
		}
		v.Next = append(v.Next, nextRowView{Rank: it.Rank, Risk: advice.RiskWord(it.Risk), RiskIcon: riskIcons[it.Risk], Where: fmt.Sprintf("%s:%d", it.File, it.Line), Sentence: it.Sentence, Name: it.TestName, Why: why})
	}
	v.NextMore = len(items) - len(top)
	byName := map[string]coverage.WorkflowResult{}
	for _, w := range res.Workflows {
		byName[w.Name] = w
	}
	for i, r := range res.WorkflowRows() {
		k, ic := gradeLook(r.Grade)
		row := wfRowView{Name: r.Name, Anchor: fmt.Sprintf("wfs-%d", i), Grade: capitalize(r.Grade), GradeKey: k, GradeIcon: ic, Missed: r.Missed, Bar: math.Round(r.Percent*10) / 10, NoDecisions: r.Possible == 0}
		row.Tested = fmt.Sprintf("%d / %d", r.Covered, r.Possible)
		row.Percent = render.Pct(r.Percent, nil)
		if r.Possible == 0 {
			row.Tested, row.Percent, row.Grade = "no decisions to test", "", "No decisions to test"
		}
		row.Pairs = "—"
		if r.PairsTotal > 0 {
			row.Pairs = fmt.Sprintf("%d / %d", r.PairsCovered, r.PairsTotal)
		}
		v.WfRows = append(v.WfRows, row)
		v.WfSections = append(v.WfSections, checklist(byName[r.Name], row.Anchor, i == 0))
	}
	v.AtTarget = fmt.Sprintf("%d of %d workflows at %s or better", h.AtTarget, h.Workflows-h.NoDecision, h.TargetGrade)
	v.RunCount = res.Counts.Counted
	v.Terms = append(v.Terms, terms...)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// checklist writes a workflow's decisions as plain sentences with their
// outcomes ticked, crossed or marked.
func checklist(w coverage.WorkflowResult, anchor string, open bool) wfSection {
	sec := wfSection{Name: w.Name, Anchor: anchor, Missed: w.BranchCov.Missed(), Open: open}
	sec.Summary = fmt.Sprintf("%d of %d outcomes tested", w.BranchCov.Covered, w.BranchCov.Possible)
	if w.BranchCov.Possible == 0 {
		sec.Summary = "no decisions to test"
	}
	type key struct {
		text, file string
		line       int
	}
	at := map[key]int{}
	for _, o := range w.Outcomes {
		k := key{o.Decision + "\x00" + o.Func, o.File, o.Line}
		i, ok := at[k]
		if !ok {
			if len(sec.Decisions) >= maxChecklistDecisions {
				continue
			}
			i = len(sec.Decisions)
			at[k] = i
			sec.Decisions = append(sec.Decisions, decisionView{Text: advice.DecisionText(o), Where: fmt.Sprintf("%s:%d", filepath.ToSlash(o.File), o.Line), Cleanup: o.Cleanup})
		}
		look := statusLook[o.Status]
		ov := outcomeView{Label: advice.OutcomeLabel(o), Status: o.Status, Icon: look[0], Word: look[1]}
		if o.Why != "" {
			ov.Why = o.Why
		}
		if o.Status == coverage.OutcomeMissed {
			sec.Decisions[i].Missed = true
		}
		if o.Helper && o.Sites > 1 {
			ov.Label += fmt.Sprintf(" (a helper called from %d places; tested if any place took it)", o.Sites)
		}
		sec.Decisions[i].Outcomes = append(sec.Decisions[i].Outcomes, ov)
	}
	// the cleanup after a cancel is a block of its own, after the rest
	sort.SliceStable(sec.Decisions, func(i, j int) bool { return !sec.Decisions[i].Cleanup && sec.Decisions[j].Cleanup })
	for i := range sec.Decisions {
		if sec.Decisions[i].Cleanup {
			sec.Decisions[i].FirstCleanup = true
			break
		}
	}
	return sec
}

// ratioOrNone is "28/42 (66.7%)", or the word given when there is nothing to count.
func ratioOrNone(covered, total int, none string) string {
	if total == 0 {
		return none
	}
	return render.Ratio(covered, total, nil, none)
}
