package advice

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// OutcomeItem is one decision outcome that can happen and that no test took:
// the unit of the to-do list (milestone M11, B5). Ranking only orders and
// explains; it never changes a number, a bucket or a claim (D28).
type OutcomeItem struct {
	Rank     int
	ID       string // the branch ID
	Workflow string
	// Risk is the riskiest thing a test of it must do (the tags of D28).
	Risk string
	File string
	Line int
	// Sentence says in plain words what never happened.
	Sentence string
	TestName string
	// Paths and Scenarios are how many listed paths and scenarios a test of it
	// would cover (the ones through it that no test ran).
	Paths, Scenarios int
	// PathNumber is the path a test of it would run (0 when there is none
	// listed); Decision and Outcome are the words of the decision and its exit.
	PathNumber        int
	Decision, Outcome string
}

// RiskWord is the plain name of a risk tag.
func RiskWord(tag string) string {
	switch tag {
	case TagCancel:
		return "Cancel"
	case TagCompensation:
		return "Undo (compensation)"
	case TagFailure:
		return "Activity failure"
	case TagContinue:
		return "Continue as new"
	case TagTimer:
		return "Timer or timeout"
	case TagSignal:
		return "Signal or update"
	case TagLogic:
		return "Plain logic"
	case TagOldVersion:
		return "Old version"
	}
	return tag
}

// MissedOutcomes lists up to limit missed outcomes over the whole project (all
// of them when limit is 0), riskiest first; inside one risk, the ones that
// would cover more paths and scenarios first, then by place.
func MissedOutcomes(res coverage.Result, display func(string) string, limit int) []OutcomeItem {
	var items []OutcomeItem
	for _, w := range res.Workflows {
		items = append(items, workflowOutcomeItems(w, display)...)
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if ra, rb := tagRank(a.Risk), tagRank(b.Risk); ra != rb {
			return ra < rb
		}
		if a.Paths+a.Scenarios != b.Paths+b.Scenarios {
			return a.Paths+a.Scenarios > b.Paths+b.Scenarios
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	seen := map[string]int{}
	for i := range items {
		seen[items[i].TestName]++
		if n := seen[items[i].TestName]; n > 1 {
			items[i].TestName += "_" + strconv.Itoa(n)
		}
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	for i := range items {
		items[i].Rank = i + 1
	}
	return items
}

func workflowOutcomeItems(w coverage.WorkflowResult, display func(string) string) []OutcomeItem {
	// the listed paths through each exit
	through := map[string][]int{}
	for i, p := range w.Paths {
		for _, s := range p.Path.Steps {
			id := s.Exit.ID.String()
			through[id] = append(through[id], i)
		}
	}
	scenarioSteps := map[string][]string{}
	for _, sc := range w.Scenarios.Items {
		scenarioSteps[sc.ID] = sc.Steps
	}
	took := w.TookExits()
	var items []OutcomeItem
	for _, o := range w.Outcomes {
		if o.Status != coverage.OutcomeMissed {
			continue
		}
		it := OutcomeItem{ID: o.ID, Workflow: w.Name, Decision: o.Decision, Outcome: o.Outcome, Paths: o.MissedPaths, Scenarios: o.MissedScenarios, Sentence: Sentence(o)}
		file := o.File
		if display != nil {
			file = display(file)
		}
		it.File, it.Line = filepath.ToSlash(file), o.Line
		// the path a test of it would run: the least risky one that no test ran
		best, bestRank, bestNew := -1, -1, 0
		for _, e := range o.Exits {
			for _, pi := range through[e] {
				p := w.Paths[pi]
				if p.Covered {
					continue
				}
				steps := stepsOfPath(p)
				if sc, ok := scenarioSteps[p.ScenarioID]; ok && w.Scenarios.Measured {
					steps = sc
				}
				tag := tagOf(steps, p.Path.Compensation)
				if p.Class.Label == "version-gate-old-branch" || oldVersionOutcome(o) {
					tag = TagOldVersion
				}
				newExits := 0
				for _, s := range p.Path.Steps {
					if s.Junction.Retry != s.Exit && !took[s.Exit.ID.String()] {
						newExits++
					}
				}
				r := tagRank(tag)
				if best < 0 || r > bestRank || (r == bestRank && newExits < bestNew) {
					best, bestRank, bestNew = pi, r, newExits
					it.Risk = tag
				}
			}
		}
		if best >= 0 {
			it.PathNumber = w.Paths[best].Number
			steps := stepsOfPath(w.Paths[best])
			if sc, ok := scenarioSteps[w.Paths[best].ScenarioID]; ok && w.Scenarios.Measured {
				steps = sc
			}
			it.TestName = outcomeTestName(w.Name, steps, o, it.PathNumber)
		} else {
			// no listed path goes through it (a large workflow, or a helper kept
			// apart): tag it by what the outcome itself is
			it.Risk = outcomeTag(o)
			it.TestName = outcomeTestName(w.Name, nil, o, 0)
		}
		items = append(items, it)
	}
	return items
}

// outcomeTag is the risk of an outcome with no listed path: from the outcome
// alone.
// oldVersionOutcome: the outcome of a workflow.GetVersion check that is the old
// side ("== DefaultVersion" true, "!= DefaultVersion" false). A test reaches it
// only by replaying a history recorded before the change.
func oldVersionOutcome(o coverage.OutcomeResult) bool {
	if o.Kind != model.PlainIf || !strings.Contains(o.Decision, "GetVersion(") {
		return false
	}
	switch {
	case strings.Contains(o.Decision, "!= workflow.DefaultVersion"):
		return o.Outcome == "false"
	case strings.Contains(o.Decision, "== workflow.DefaultVersion"):
		return o.Outcome == "true"
	}
	return false
}

func outcomeTag(o coverage.OutcomeResult) string {
	if oldVersionOutcome(o) {
		return TagOldVersion
	}
	switch o.Kind {
	case model.ErrCheck:
		if o.Outcome == "failure" {
			return TagFailure
		}
	case model.Selector, model.WaitResult:
		if strings.Contains(o.Outcome, "timeout") {
			return TagTimer
		}
		return TagSignal
	}
	return TagLogic
}

// Sentence says in plain words what never happened at an outcome.
func Sentence(o coverage.OutcomeResult) string {
	cond := strings.TrimPrefix(o.Decision, "if ")
	switch o.Kind {
	case model.ErrCheck:
		name := o.Call
		if name == "" {
			name = callName(o.Decision)
		}
		if strings.Contains(o.Decision, "(timer)") || strings.Contains(o.Decision, "(wait)") {
			if o.Outcome == "failure" {
				return "The " + name + " wait was never cancelled"
			}
			return "The " + name + " wait never finished normally"
		}
		if o.FailureKind != "" {
			w := kindWords[o.FailureKind]
			if w == "" {
				w = "a " + o.FailureKind
			}
			return name + " never failed with " + w + " here"
		}
		if o.Outcome == "failure" {
			return name + " never failed here"
		}
		return name + " never succeeded here"
	case model.Switch:
		if o.Outcome == "default" {
			return "`" + o.Decision + "` never fell through to its default (no case matched)"
		}
		return "`" + o.Decision + "` never took `" + o.Outcome + "`"
	case model.Loop:
		if o.Outcome == "iterate" {
			return "The loop `" + o.Decision + "` never ran a round"
		}
		return "The loop `" + o.Decision + "` never ran to its end"
	case model.Selector:
		switch {
		case o.Outcome == "timeout":
			return "The wait never timed out"
		case strings.HasPrefix(o.Outcome, "signal "):
			return "The signal " + strings.TrimPrefix(o.Outcome, "signal ") + " never arrived while waiting"
		case o.Outcome == "default":
			return "The wait never took its default"
		}
		return "The wait never ended with " + o.Outcome
	case model.WaitResult:
		return "The wait never ended as \"" + o.Outcome + "\" (" + o.Decision + ")"
	}
	return "`" + cond + "` was never " + o.Outcome
}

// outcomeTestName is Test<Workflow>_<what>: from the path's steps when they
// say something, else from the outcome.
func outcomeTestName(workflow string, steps []string, o coverage.OutcomeResult, number int) string {
	// a plain decision is named after the decision and the side, not after the
	// path chosen to reach it (that path says nothing about this decision)
	if (o.Kind == model.ErrCheck || o.Kind == model.Selector) && len(steps) > 0 {
		n := testName(workflow, steps, number)
		if !strings.Contains(n, "_Path") {
			return n
		}
	}
	short := workflow
	if i := strings.LastIndex(short, "."); i >= 0 {
		short = short[i+1:]
	}
	short = strings.TrimSuffix(short, "Workflow")
	var bit string
	if o.Kind == model.ErrCheck {
		bit = camel(callNameOr(o)) + map[bool]string{true: "Fails", false: "Succeeds"}[o.Outcome == "failure"] + camel(o.FailureKind)
	} else {
		bit = decisionNameBit(o)
	}
	return "Test" + camel(short) + "_" + bit
}

var signalNameRe = regexp.MustCompile(`GetSignalChannel\([^,]+,\s*([A-Za-z0-9_.]+)\)`)

// decisionNameBit names a test after a decision and the side it takes: a few
// words of the condition (cut at a word, never in the middle of one).
func decisionNameBit(o coverage.OutcomeResult) string {
	if oldVersionOutcome(o) {
		return "OldVersion"
	}
	side := camel(o.Outcome)
	if m := signalNameRe.FindStringSubmatch(o.Decision); m != nil {
		sig := m[1]
		if i := strings.LastIndex(sig, "."); i >= 0 {
			sig = sig[i+1:]
		}
		return "Signal" + strings.TrimPrefix(camel(sig), "Signal") + camel(o.Outcome)
	}
	cond := strings.TrimPrefix(o.Decision, "if ")
	var words []string
	if strings.HasPrefix(cond, "!") && !strings.HasPrefix(cond, "!=") {
		words = append(words, "Not") // `!ok` is "not ok", not "ok"
	}
	for _, w := range splitCamel(camel(cond)) {
		switch w {
		case "Workflow", "Ctx", "Context", "If":
			continue
		}
		words = append(words, w)
	}
	bit := ""
	for _, w := range words {
		if len(bit)+len(w) > 36 {
			break
		}
		bit += w
	}
	return bit + side
}

// splitCamel splits "ReceiveAsyncDone2" into "Receive", "Async", "Done2".
func splitCamel(s string) []string {
	var out []string
	start := 0
	for i, r := range s {
		if i > start && unicode.IsUpper(r) {
			out = append(out, s[start:i])
			start = i
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func callNameOr(o coverage.OutcomeResult) string {
	if o.Call != "" {
		return o.Call
	}
	return callName(o.Decision)
}

// DecisionText says in plain words which decision an outcome belongs to.
func DecisionText(o coverage.OutcomeResult) string {
	switch o.Kind {
	case model.ErrCheck:
		name := callNameOr(o)
		if strings.Contains(o.Decision, "(timer)") || strings.Contains(o.Decision, "(wait)") {
			return "How the wait " + name + " ends"
		}
		return "When " + name + " finishes"
	case model.Switch:
		return "Which case of `" + o.Decision + "` runs"
	case model.Loop:
		return "The loop `" + o.Decision + "`"
	case model.Selector:
		return "What the workflow is waiting for"
	case model.WaitResult:
		return "How the wait `" + o.Decision + "` ends"
	}
	return "If `" + strings.TrimPrefix(o.Decision, "if ") + "`"
}

// OutcomeLabel says in plain words what the outcome is.
func OutcomeLabel(o coverage.OutcomeResult) string {
	switch o.Kind {
	case model.ErrCheck:
		wait := strings.Contains(o.Decision, "(timer)") || strings.Contains(o.Decision, "(wait)")
		switch {
		case o.Outcome == "failure" && wait:
			return "it is cancelled"
		case o.Outcome == "failure" && o.FailureKind != "":
			w := kindWords[o.FailureKind]
			if w == "" {
				w = "a " + o.FailureKind
			}
			return "it fails with " + w
		case o.Outcome == "failure":
			return "it fails"
		case wait:
			return "it ends normally"
		}
		return "it succeeds"
	case model.Switch:
		if o.Outcome == "default" {
			return "no case matches (default)"
		}
		return o.Outcome
	case model.Loop:
		if o.Outcome == "iterate" {
			return "it runs a round"
		}
		return "it runs to its end"
	case model.Selector:
		switch {
		case o.Outcome == "timeout":
			return "the wait times out"
		case strings.HasPrefix(o.Outcome, "signal "):
			return "the signal " + strings.TrimPrefix(o.Outcome, "signal ") + " arrives"
		}
		return o.Outcome
	case model.WaitResult:
		return o.Outcome
	}
	switch o.Outcome {
	case "true":
		return "yes (true)"
	case "false":
		return "no (false)"
	}
	return o.Outcome
}
