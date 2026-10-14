package render

import (
	"fmt"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// WantsHint says whether a path gets a "how to cover it" hint: it is
// testable and no recorded run took it.
func WantsHint(p coverage.PathResult) bool {
	return !p.Covered && p.Class.Bucket == classify.Testable
}

// hintLines are the indented lines printed under a path with --hints.
func hintLines(p coverage.PathResult) string {
	var b strings.Builder
	b.WriteString("       how to cover it:\n")
	for _, h := range HowToCover(p.Path) {
		b.WriteString("         - " + h + "\n")
	}
	return b.String()
}

// HowToCover says, in plain words, what a test must make happen for a run
// to take path p: one line per decision on the path, in order, then how
// the run ends. It only reads the path's own steps (the same ones analyze
// prints), so it never claims more than the map knows: what the code must
// see is named, not how to get your data there.
func HowToCover(p model.Path) []string {
	var out []string
	entered := map[*model.Junction]bool{}
	for _, s := range p.Steps {
		j, label := s.Junction, s.Exit.Label
		switch j.Kind {
		case model.PlainIf:
			out = append(out, fmt.Sprintf("make `%s` %s", strings.TrimPrefix(j.Label, "if "), label))
		case model.ErrCheck:
			out = append(out, errCheckHint(j.Label, label == "failure"))
		case model.Switch:
			out = append(out, switchHint(j.Label, label))
		case model.Loop:
			// The loop rule keeps only the LAST trip of a run (D3), so what
			// the path shows inside the loop must happen on the last trip,
			// and one trip is enough.
			switch label {
			case "iterate":
				if entered[j] {
					out = append(out, fmt.Sprintf("go into `%s` again", j.Label))
				} else {
					out = append(out, fmt.Sprintf("go into `%s` (one trip is enough; what follows inside the loop must happen on its last trip)", j.Label))
				}
				entered[j] = true
			case "retry":
				out = append(out, fmt.Sprintf("let that trip end normally (no return or break), so `%s` checks again whether to go round", j.Label))
			case "exit":
				if entered[j] {
					out = append(out, fmt.Sprintf("then leave `%s` (its condition becomes false, or the list runs out)", j.Label))
				} else {
					out = append(out, fmt.Sprintf("skip `%s` entirely (its condition is false, or the list is empty, the first time)", j.Label))
				}
			default:
				out = append(out, fmt.Sprintf("`%s`: %s", j.Label, label))
			}
		case model.Selector:
			out = append(out, selectorHint(label))
		case model.WaitResult:
			out = append(out, waitHint(j.Label, label))
		default:
			out = append(out, fmt.Sprintf("`%s`: take %s", j.Label, label))
		}
	}
	switch p.End {
	case model.EndCompleted:
		out = append(out, "expect the workflow to complete")
	case model.EndFailed:
		out = append(out, "expect the workflow to fail (env.GetWorkflowError() is not nil)")
	case model.EndContinuedAsNew:
		out = append(out, "expect the workflow to continue as new (env.GetWorkflowError() is a *workflow.ContinueAsNewError)")
	}
	return out
}

// errCheckHint is the hint for "if err != nil" after a Temporal call; the
// junction label is "Name (activity)", "Sleep (timer)" and so on.
func errCheckHint(label string, fail bool) string {
	name, kind, _ := strings.Cut(label, " (")
	kind = strings.TrimSuffix(kind, ")")
	switch kind {
	case "activity", "local activity":
		if fail {
			return fmt.Sprintf("make %s %s fail: mock it with env.OnActivity, passing the same activity the workflow starts (%s), with .Return(..., temporal.NewNonRetryableApplicationError(...)) (a retryable error is retried first)", kind, name, name)
		}
		return fmt.Sprintf("let %s %s succeed (mock it with env.OnActivity, passing the same activity the workflow starts (%s), with .Return(...); or let it run)", kind, name, name)
	case "child workflow":
		if fail {
			return fmt.Sprintf("make child workflow %s fail: mock it with env.OnWorkflow, passing the same workflow the code starts (%s), with .Return(..., an error)", name, name)
		}
		return fmt.Sprintf("let child workflow %s succeed (mock it with env.OnWorkflow, passing the same workflow the code starts (%s), with .Return(...); or let it run)", name, name)
	case "timer":
		if fail {
			return fmt.Sprintf("make %s end with an error (the workflow is cancelled while it waits, env.CancelWorkflow())", name)
		}
		return fmt.Sprintf("let %s fire (the test clock skips ahead by itself)", name)
	case "wait":
		if fail {
			return fmt.Sprintf("make %s end with an error (the workflow is cancelled while it waits, env.CancelWorkflow())", name)
		}
		return fmt.Sprintf("let %s return without an error", name)
	case "signal":
		if fail {
			return "make SignalExternalWorkflow fail: env.OnSignalExternalWorkflow(...).Return(an error)"
		}
		return "let SignalExternalWorkflow succeed: env.OnSignalExternalWorkflow(...).Return(nil)"
	case "call":
		if fail {
			return fmt.Sprintf("make the call %s fail (it is ordinary code, not Temporal: give it input it rejects)", name)
		}
		return fmt.Sprintf("let the call %s succeed", name)
	case "future":
		if fail {
			return "make the future checked here end with an error (the activity, child workflow or timer behind it fails)"
		}
		return "let the future checked here end without an error"
	case "cancel":
		if fail {
			return "make RequestCancelExternalWorkflow fail: env.OnRequestCancelExternalWorkflow(...).Return(an error)"
		}
		return "let RequestCancelExternalWorkflow succeed: env.OnRequestCancelExternalWorkflow(...).Return(nil)"
	}
	if fail {
		return fmt.Sprintf("make `%s` return an error", label)
	}
	return fmt.Sprintf("make `%s` return no error", label)
}

// switchHint: one case of a switch. A " #2" suffix (two cases with the
// same text) is PathKit's, not source code, so it is said in words.
func switchHint(junction, label string) string {
	nth := ""
	if m := strings.LastIndex(label, " #"); m >= 0 {
		nth = fmt.Sprintf(" (the %s case with this text)", ordinal(label[m+2:]))
		label = label[:m]
	}
	subject := strings.TrimSpace(strings.TrimPrefix(junction, "switch"))
	if label == "default" {
		if subject == "" {
			return "make none of the cases of the `switch` true (its default)"
		}
		if v, ok := strings.CutSuffix(subject, ".(type)"); ok {
			return fmt.Sprintf("make `%s` hold a value of none of the listed types (its default)", v)
		}
		return fmt.Sprintf("make `%s` match none of the cases (its default)", subject)
	}
	cond := strings.TrimPrefix(label, "case ")
	switch v, typeSwitch := strings.CutSuffix(subject, ".(type)"); {
	case typeSwitch && cond == "nil":
		return fmt.Sprintf("make `%s` nil%s", v, nth)
	case typeSwitch:
		return fmt.Sprintf("make `%s` hold a value of type `%s`%s", v, cond, nth)
	case subject == "":
		return fmt.Sprintf("make `%s` true, and no case above it%s", cond, nth)
	}
	return fmt.Sprintf("make `%s` equal to `%s`%s", subject, cond, nth)
}

func ordinal(n string) string {
	switch n {
	case "2":
		return "2nd"
	case "3":
		return "3rd"
	}
	return n + "th"
}

func selectorHint(label string) string {
	switch {
	case strings.HasPrefix(label, "signal "):
		name := strings.TrimPrefix(label, "signal ")
		if i := strings.Index(name, " #"); i >= 0 {
			name = name[:i]
		}
		return fmt.Sprintf("at the select, let signal %s win: send it before anything else is ready (env.RegisterDelayedCallback(func() { env.SignalWorkflow(%s, ...) }, a short delay)); a racing timer must be longer than that delay, and a racing activity must be mocked slower (env.OnActivity(...).After(longer than the delay)), or it finishes first", name, name)
	case label == "timeout" || strings.HasPrefix(label, "timeout #"):
		return "at the select, let the timer win: send nothing before it fires (a racing activity must be slower: env.OnActivity(...).After(longer than the timer))"
	case label == "default" || strings.HasPrefix(label, "default #"):
		return "at the select, have nothing ready yet (no signal sent, no future done), so the default runs"
	}
	for _, kind := range []string{"activity ", "local activity ", "child "} {
		if strings.HasPrefix(label, kind) {
			name := strings.TrimPrefix(label, kind)
			if i := strings.Index(name, " #"); i >= 0 {
				name = name[:i]
			}
			what := strings.TrimSuffix(kind, " ")
			if what == "child" {
				what = "child workflow"
			}
			return fmt.Sprintf("at the select, let %s %s finish first (against a timer it must be faster: mock it with a short .After(...), or none)", what, name)
		}
	}
	switch kind, what, _ := strings.Cut(label, " "); kind {
	case "receive":
		return fmt.Sprintf("at the select, have a value waiting on `%s` before anything else is ready", what)
	case "send":
		return fmt.Sprintf("at the select, have something ready to take a value from `%s` before anything else is ready", what)
	case "future":
		return fmt.Sprintf("at the select, let the future `%s` finish before anything else is ready", what)
	}
	return fmt.Sprintf("at the select, let `%s` be ready first", label)
}

func waitHint(junction, label string) string {
	switch label {
	case "signaled":
		return fmt.Sprintf("at `%s`, make the condition come true before the timeout (for example send the signal with env.RegisterDelayedCallback)", junction)
	case "timeout":
		return fmt.Sprintf("at `%s`, let the timeout pass with the condition still false (the test clock skips ahead by itself)", junction)
	case "received":
		return fmt.Sprintf("at `%s`, have a value waiting on the channel (send the signal before this point)", junction)
	case "not received":
		return fmt.Sprintf("at `%s`, have nothing waiting on the channel (do not send the signal, or send it later)", junction)
	}
	return fmt.Sprintf("at `%s`, take %s", junction, label)
}
