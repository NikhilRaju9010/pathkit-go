// Package advice turns a coverage result into "tests to write first": the
// missed scenarios (or, where scenarios are not measured, the missed groups of
// paths), tagged by the riskiest thing each does and put in order. Ranking only
// orders and explains; it never changes a number, a bucket or a claim
// (docs/DESIGN.md D28).
package advice

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// The tags, riskiest first. A scenario gets the first one that applies.
const (
	TagCancel       = "cancel"
	TagCompensation = "compensation"
	TagFailure      = "activity failure"
	TagContinue     = "continue-as-new"
	TagTimer        = "timers and timeouts"
	TagSignal       = "signals and updates"
	TagLogic        = "plain logic"
	TagOldVersion   = "old version branches"
)

var tagOrder = []string{TagCancel, TagCompensation, TagFailure, TagContinue, TagTimer, TagSignal, TagLogic, TagOldVersion}

func tagRank(tag string) int {
	for i, t := range tagOrder {
		if t == tag {
			return i
		}
	}
	return len(tagOrder)
}

// Item is one test worth writing.
type Item struct {
	// ID names the item: a scenario ID ("sc:…"), or "grp:…" for a group of
	// paths, or "hd:…" for a handler. "pathkit scaffold" takes it.
	ID       string
	Workflow string
	Tag      string
	// File and Line are the place of the first step the tests never took.
	File string
	Line int
	// Hint says in one line what the test must make happen.
	Hint string
	// TestName is a suggested name: Test<Workflow>_<ShortScenario>.
	TestName string
	// Paths is how many listed paths the test would cover, Branches how many
	// decisions on them no test has taken.
	Paths, Branches int
	// Steps are the external steps of the scenario ("" for a group or handler).
	Steps []string
	// Variants is how many scenarios with this hint were folded into the item.
	Variants int
	// PathNumber is the main listed path (0 for a handler).
	PathNumber int
}

// Covers is the "what it covers" sentence.
func (it Item) Covers() string {
	if it.Variants > 1 {
		return fmt.Sprintf("%d variants of this scenario, %s", it.Variants, plural(it.Paths, "path"))
	}
	switch {
	case it.Paths > 0 && it.Branches > 0:
		return fmt.Sprintf("%s and %s no test has taken", plural(it.Paths, "path"), plural(it.Branches, "branch"))
	case it.Paths > 0:
		return plural(it.Paths, "path")
	}
	return "a handler no test used"
}

func plural(n int, w string) string {
	if n == 1 {
		return "1 " + w
	}
	if strings.HasSuffix(w, "ch") {
		return strconv.Itoa(n) + " " + w + "es"
	}
	return strconv.Itoa(n) + " " + w + "s"
}

// TestsToWriteFirst lists up to limit items over the whole project, riskiest
// first; inside one tag, the ones that cover more paths or branches first.
func TestsToWriteFirst(res coverage.Result, display func(string) string, limit int) []Item {
	var items []Item
	for _, w := range res.Workflows {
		items = append(items, workflowItems(w, display)...)
	}
	items = append(items, handlerItems(res, display)...)
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if ra, rb := tagRank(a.Tag), tagRank(b.Tag); ra != rb {
			return ra < rb
		}
		if a.Paths+a.Branches != b.Paths+b.Branches {
			return a.Paths+a.Branches > b.Paths+b.Branches
		}
		return false
	})
	// items with the same workflow and hint are one test with variants
	merged := items[:0]
	at := map[string]int{}
	for _, it := range items {
		key := it.Workflow + "\x00" + it.Hint
		if k, ok := at[key]; ok {
			merged[k].Paths += it.Paths
			merged[k].Variants++
			continue
		}
		it.Variants = 1
		at[key] = len(merged)
		merged = append(merged, it)
	}
	items = merged
	nameSeen := map[string]int{}
	for i := range items {
		n := items[i].TestName
		nameSeen[n]++
		if nameSeen[n] > 1 {
			if items[i].PathNumber > 0 {
				n += "_Path" + strconv.Itoa(items[i].PathNumber)
			}
			items[i].TestName = n
		}
	}
	// a name still shared gets a number
	final := map[string]int{}
	for i := range items {
		final[items[i].TestName]++
		if k := final[items[i].TestName]; k > 1 {
			items[i].TestName += strconv.Itoa(k)
		}
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func workflowItems(w coverage.WorkflowResult, display func(string) string) []Item {
	var items []Item
	took := w.TookExits()
	byNumber := map[int]coverage.PathResult{}
	for _, p := range w.Paths {
		byNumber[p.Number] = p
	}
	if w.Scenarios.Measured {
		for _, sc := range w.Scenarios.Items {
			if sc.Covered {
				continue
			}
			bucket := sc.Class.Bucket
			if bucket != classify.Testable && bucket != classify.NeedsReview && bucket != classify.NeedsSpecialTest {
				continue
			}
			main, ok := byNumber[sc.Paths[0]]
			if !ok {
				continue
			}
			it := Item{ID: sc.ID, Workflow: w.Name, Steps: sc.Steps, Paths: sc.PathCount, PathNumber: main.Number}
			it.Tag = tagOf(sc.Steps, main.Path.Compensation)
			if sc.Class.Label == "version-gate-old-branch" {
				it.Tag = TagOldVersion // needs a replay of an old history: the last thing to write
			}
			fillFromPath(&it, main.Path, took, display)
			it.Hint = hintOf(sc.Steps, main.Path, took)
			it.TestName = testName(w.Name, sc.Steps, main.Number)
			items = append(items, it)
		}
		return items
	}
	for _, g := range coverage.MissedGroups(w) {
		it := Item{Workflow: w.Name, Paths: g.Missed, PathNumber: 0}
		var main coverage.PathResult
		for _, p := range w.Paths {
			if p.Path.Key() == g.Example.Key() {
				main = p
				it.PathNumber = p.Number
			}
		}
		it.ID = "grp:" + shortHash(w.Name+"|"+g.Example.Key()[:min(len(g.Example.Key()), 200)])
		fp := stepsOfPath(main)
		it.Steps = fp
		it.Tag = tagOf(fp, g.Example.Compensation)
		fillFromPath(&it, g.Example, took, display)
		it.Hint = hintOf(fp, g.Example, took)
		it.TestName = testName(w.Name, fp, it.PathNumber)
		items = append(items, it)
	}
	return items
}

// stepsOfPath is the labels of the exits of a path, for tagging a group (whose
// scenario is not known): the junction labels and exit labels in order.
func stepsOfPath(p coverage.PathResult) []string {
	var out []string
	for _, s := range p.Path.Steps {
		switch s.Junction.Kind {
		case model.ErrCheck, model.WaitResult:
			out = append(out, s.Junction.Label+": "+s.Exit.Label)
		case model.Selector:
			out = append(out, "wait for "+s.Exit.Label)
		}
	}
	return out
}

// fillFromPath sets the place (the first decision no test took, else the
// first decision) and the number of untaken branches.
func fillFromPath(it *Item, p model.Path, took map[string]bool, display func(string) string) {
	first := -1
	for i, s := range p.Steps {
		if s.Junction.Retry == s.Exit {
			continue
		}
		if !took[s.Exit.ID.String()] {
			it.Branches++
			if first < 0 {
				first = i
			}
		}
	}
	if first < 0 {
		first = 0
	}
	if len(p.Steps) > 0 {
		pos := p.Steps[first].Junction.Pos
		file := pos.Filename
		if display != nil {
			file = display(file)
		}
		it.File, it.Line = filepath.ToSlash(file), pos.Line
	}
}

// isCallStep reports a step that is the outcome of a call: "X (activity): …",
// "X (child workflow), via h: …", "X (helper): …".
func isCallStep(s string) bool {
	for _, k := range []string{"(activity)", "(local activity)", "(child workflow)", "(nexus operation)", "(helper)"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// failedCall reports the step of a call that failed.
func failedCall(s string) bool { return isCallStep(s) && strings.HasSuffix(s, ": failure") }

func anyStep(steps []string, f func(string) bool) bool {
	for _, s := range steps {
		if f(s) {
			return true
		}
	}
	return false
}

// callName is the name in front of the first " (" of a step.
func callName(s string) string {
	if i := strings.Index(s, " ("); i > 0 {
		return s[:i]
	}
	return s
}

// tagOf tags a list of external steps with the riskiest thing in it.
func tagOf(steps []string, compensationDefer bool) string {
	has := func(sub string) bool {
		for _, s := range steps {
			if strings.Contains(s, sub) {
				return true
			}
		}
		return false
	}
	switch {
	case has("cancel context") || has("cancel sent") || has("failure kind: cancelled") || has("canceled") || has("cancelled") || has("(timer): failure") || has("(wait): failure"):
		return TagCancel
	}
	// compensation: an activity or child step after a failure, with a success
	// before it
	success, failed := false, false
	for _, s := range steps {
		isCall := isCallStep(s)
		switch {
		case isCall && failed && success:
			return TagCompensation
		case isCall && strings.HasSuffix(s, ": success"):
			success = true
		case isCall && strings.HasSuffix(s, ": failure"):
			if success {
				failed = true
			}
		}
	}
	if compensationDefer {
		return TagCompensation
	}
	switch {
	case anyStep(steps, failedCall) || has("failure kind"):
		return TagFailure
	case has("continue-as-new"):
		return TagContinue
	case has("timer") || has("timeout") || has("wait with timeout") || has("(timer)"):
		return TagTimer
	case has("signal") || has("update"):
		return TagSignal
	case has("version") && has(": old"):
		return TagOldVersion
	}
	return TagLogic
}

var kindWords = map[string]string{
	"timeout":                         "a timeout",
	"cancelled":                       "a cancellation",
	"application error":               "an application error",
	"non-retryable application error": "a non-retryable error",
}

// hintOf says in one line what the test must make happen.
func hintOf(steps []string, p model.Path, took map[string]bool) string {
	var parts []string
	for i, s := range steps {
		switch {
		case strings.Contains(s, "(timer): failure") || strings.Contains(s, "(wait): failure"):
			parts = append(parts, "cancel the workflow while it waits")
		case failedCall(s):
			name := callName(s)
			part := "make " + name + " fail"
			if i+1 < len(steps) && strings.HasPrefix(steps[i+1], "failure kind: ") {
				kind := strings.TrimPrefix(steps[i+1], "failure kind: ")
				if w, ok := kindWords[kind]; ok {
					part += " with " + w
				}
			}
			parts = append(parts, part)
		case strings.HasPrefix(s, "wait for signal"):
			parts = append(parts, "send the signal "+strings.TrimPrefix(s, "wait for signal "))
		case s == "wait for timeout":
			parts = append(parts, "let the wait time out (send nothing)")
		case s == "cancel context" || strings.Contains(s, "cancel sent"):
			parts = append(parts, "cancel the workflow")
		case strings.HasPrefix(s, "version ") && strings.HasSuffix(s, ": old"):
			parts = append(parts, "replay a history recorded by the old code")
		case strings.HasPrefix(s, "loop ") && strings.HasSuffix(s, ": 0 rounds"):
			parts = append(parts, "make the loop run no rounds")
		case s == "continue-as-new":
			parts = append(parts, "make the workflow continue as new")
		case strings.Contains(s, "): success"):
			// the default; not worth a word unless nothing else is said
		}
	}
	// the first decision on the code nobody has taken
	cond := ""
	for _, s := range p.Steps {
		if s.Junction.Kind == model.ErrCheck && s.Exit.Label == "success" && !took[s.Exit.ID.String()] && len(parts) == 0 {
			name := s.Junction.Label
			if i := strings.Index(name, " ("); i > 0 {
				name = name[:i]
			}
			parts = append(parts, "let "+name+" succeed")
			break
		}
		if (s.Junction.Kind == model.PlainIf && !isVersionGate(s.Junction)) || s.Junction.Kind == model.Switch {
			if !took[s.Exit.ID.String()] {
				if s.Junction.Kind == model.Switch {
					cond = fmt.Sprintf("`%s` taking %s", s.Junction.Label, s.Exit.Label)
				} else {
					cond = fmt.Sprintf("`%s` %s", strings.TrimPrefix(s.Junction.Label, "if "), s.Exit.Label)
				}
				break
			}
		}
	}
	if len(parts) == 0 {
		if cond != "" {
			return "Make " + cond
		}
		for _, st := range p.Steps {
			if st.Junction.Retry == st.Exit || took[st.Exit.ID.String()] {
				continue
			}
			if d := describeExit(st); d != "" {
				return strings.ToUpper(d[:1]) + d[1:]
			}
		}
		return "Run the workflow so that it takes the first decision no test has taken"
	}
	text := strings.ToUpper(parts[0][:1]) + parts[0][1:] + joinThen(parts[1:])
	if cond != "" {
		return "With " + cond + ": " + strings.ToLower(text[:1]) + text[1:]
	}
	return text
}

func isVersionGate(j *model.Junction) bool {
	for _, f := range j.Facts {
		if f.Kind == model.VersionCheck {
			return true
		}
	}
	return false
}

// describeExit says in words what makes a run take one exit of a decision.
func describeExit(st model.Step) string {
	j, e := st.Junction, st.Exit
	switch j.Kind {
	case model.PlainIf:
		for _, f := range j.Facts {
			if f.Kind == model.VersionCheck {
				if f.Exit == e.Label {
					return "replay a history recorded by the old code (the old side of " + strings.TrimPrefix(j.Label, "if ") + ")"
				}
				return "start a new run of the current code (the new side of " + strings.TrimPrefix(j.Label, "if ") + ")"
			}
		}
		return fmt.Sprintf("make `%s` %s", strings.TrimPrefix(j.Label, "if "), e.Label)
	case model.ErrCheck:
		if e.Label == "failure" {
			return "make " + callName(j.Label) + " fail"
		}
		return "let " + callName(j.Label) + " succeed"
	case model.Switch:
		return fmt.Sprintf("make `%s` take %s", j.Label, e.Label)
	case model.Loop:
		if e.Label == "iterate" {
			return "make `" + j.Label + "` run at least one round"
		}
		return "make `" + j.Label + "` run no round"
	case model.Selector:
		return "make the selector take " + e.Label
	case model.WaitResult:
		return "make " + j.Label + " end as " + e.Label
	}
	return ""
}

func joinThen(parts []string) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(", then " + p)
	}
	return b.String()
}

// testName is Test<Workflow>_<ShortScenario>.
func testName(workflow string, steps []string, number int) string {
	short := workflow
	if i := strings.LastIndex(short, "."); i >= 0 {
		short = short[i+1:]
	}
	short = strings.TrimSuffix(short, "Workflow")
	var bits []string
	for i, s := range steps {
		switch {
		case strings.Contains(s, "(timer): failure") || strings.Contains(s, "(wait): failure"):
			bits = append(bits, "Cancelled")
		case failedCall(s):
			n := camel(callName(s)) + "Fails"
			if i+1 < len(steps) && strings.HasPrefix(steps[i+1], "failure kind: ") {
				n += camel(strings.TrimPrefix(steps[i+1], "failure kind: "))
			}
			bits = append(bits, n)
		case strings.HasPrefix(s, "wait for signal"):
			bits = append(bits, "Signal"+camel(strings.Trim(strings.TrimPrefix(s, "wait for signal"), " \"")))
		case s == "wait for timeout":
			bits = append(bits, "TimesOut")
		case s == "cancel context" || strings.Contains(s, "cancel sent"):
			bits = append(bits, "Cancelled")
		case strings.HasPrefix(s, "version ") && strings.HasSuffix(s, ": old"):
			bits = append(bits, "OldVersion")
		case s == "continue-as-new":
			bits = append(bits, "ContinuesAsNew")
		}
		if n := len(bits); n >= 2 && bits[n-1] == bits[n-2] {
			bits = bits[:n-1] // the same word twice in a row
		}
		if len(bits) == 3 {
			break
		}
	}
	if len(bits) == 0 {
		for i := len(steps) - 1; i >= 0; i-- {
			if isCallStep(steps[i]) && strings.HasSuffix(steps[i], ": success") {
				bits = []string{camel(callName(steps[i])) + "Succeeds"}
				break
			}
		}
	}
	if len(bits) == 0 {
		bits = []string{"Path" + strconv.Itoa(number)}
	}
	return "Test" + camel(short) + "_" + strings.Join(bits, "")
}

// camel keeps letters and digits, each word starting in upper case.
func camel(s string) string {
	var b strings.Builder
	up := true
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if up {
				r = unicode.ToUpper(r)
			}
			b.WriteRune(r)
			up = false
		default:
			up = true
		}
	}
	return b.String()
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:10]
}

// handlerItems are handlers (signals, updates and their validators) no test
// used, or whose branches (an update validator's accept and reject) no test took.
func handlerItems(res coverage.Result, display func(string) string) []Item {
	var items []Item
	for _, h := range res.Handlers {
		if !h.IsGap() || h.Exercised == "not recorded" {
			continue
		}
		it := Item{ID: "hd:" + shortHash(h.Owner+"|"+h.Kind+"|"+h.Name+"|"+h.Where), Workflow: h.Owner, Tag: TagSignal, Branches: h.Branches - h.BranchesTaken}
		it.File, it.Line = splitWhere(h.Where)
		name := h.Name
		if name == "" {
			name = "(name computed)"
		}
		switch h.Kind {
		case model.HandlerSignal:
			it.Hint = "Send the signal " + strconv.Quote(name) + " to the workflow"
		case model.HandlerUpdate:
			it.Hint = "Send the update " + strconv.Quote(name) + " and let its handler run"
		default:
			it.Hint = "Send the update " + strconv.Quote(name) + " so that its validator rejects it, and once so that it accepts it"
		}
		it.TestName = testName(h.Owner, nil, 0)
		it.TestName = strings.TrimSuffix(it.TestName, "_Path0") + "_" + camel(h.Kind) + camel(name)
		items = append(items, it)
	}
	return items
}

func splitWhere(where string) (string, int) {
	i := strings.LastIndex(where, ":")
	if i < 0 {
		return where, 0
	}
	n, _ := strconv.Atoi(where[i+1:])
	return filepath.ToSlash(where[:i]), n
}
