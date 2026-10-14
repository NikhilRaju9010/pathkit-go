package render

import (
	"fmt"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// Unmatched is one group of traces that fit no path (same workflow, same
// steps), told in plain words for the unmatched-trace report (--unmatched).
// Everything comes from the matcher's own walk (model.Mismatch); nothing
// here walks the map again.
type Unmatched struct {
	Workflow     string
	StepsID      string
	Count        int
	Acknowledged bool
	Reason       string   // the acknowledgement's reason
	Steps        []string // as recorded
	// Followed is the part of the path the trace did follow, as analyze
	// prints paths ("Start -> if x --true--> ..."), "Start" when nothing.
	Followed string
	// Then is the matcher's message: the exact step where it left the map.
	Then string
	// Expected lists what the map allows at that point.
	Expected []string
	// Cause is the most likely reason, in plain words. It is a guess and
	// says so.
	Cause string
}

// UnmatchedTraces lists every group of traces that fit no path, workflow
// by workflow, most-seen first (coverage's order).
func UnmatchedTraces(res coverage.Result) []Unmatched {
	var out []Unmatched
	for _, w := range res.Workflows {
		for _, u := range w.Unmatched {
			item := Unmatched{Workflow: w.Name, StepsID: u.StepsID, Count: u.Count, Acknowledged: u.Acknowledged, Reason: u.Reason,
				Steps: u.Steps, Followed: "Start", Then: u.Mismatch}
			if d := u.Detail; d != nil {
				if len(d.Followed) > 0 {
					item.Followed = strings.TrimSuffix(PathText(model.Path{Steps: d.Followed, End: model.EndUnknown}), " End")
				}
				fitted := len(u.Steps) // steps read before the walk stopped
				if d.Step > 0 {
					fitted = d.Step - 1
				}
				if len(d.Followed) < fitted {
					item.Followed += " (repeated loop trips folded: only the last one is kept)"
				}
				item.Expected = expected(d.At)
				item.Cause = likelyCause(d)
			}
			out = append(out, item)
		}
	}
	return out
}

// expected is what the map allows where the walk stood.
func expected(at model.Target) []string {
	switch {
	case at.Retry != nil:
		return []string{fmt.Sprintf("go round %s again (%s)", at.Retry.Junction.Label, at.Retry.ID)}
	case at.Junction != nil:
		var out []string
		for _, e := range at.Junction.Exits {
			out = append(out, fmt.Sprintf("%s --%s (%s)", at.Junction.Label, e.Label, e.ID))
		}
		return out
	case at.Dead():
		return []string{"nothing: the map says this road ends in a panic"}
	}
	return []string{"nothing more: the map says the run ends here, at " + at.End.String()}
}

// likelyCause is a plain-words guess from where the walk stood and what the
// trace did instead. The most common real cause (see LIMITATIONS.md) is an
// error check PathKit assumes never fails.
func likelyCause(d *model.Mismatch) string {
	const transparent = "PathKit assumes an `if err != nil` on an error from a helper or another non-Temporal call never fails, and an `if` marked //pathkit:ignore always takes its usual side. If such a check took its other side in this run, put //pathkit:branch on it (for a //pathkit:ignore, remove the comment) so the map draws both sides, then run pathkit test again"
	capital := strings.ToUpper(transparent[:1]) + transparent[1:]
	switch {
	case d.At.Dead():
		return "likely: the map says this road ends in a panic (or os.Exit, log.Fatal), but the run went on; check the call PathKit takes for one that always stops the run."
	case d.Step == 0 && (d.At.Junction != nil || d.At.Retry != nil):
		return "likely: the run ended (returned or failed) earlier than the map allows; " + transparent + "."
	case d.Step == 0:
		return "likely: the trace ended somewhere the map has no end; run pathkit test again, and if it stays, the map is wrong here."
	case d.Bad == nil:
		return "likely: the step is not on this workflow's map at all, so the trace comes from another build of the code; run pathkit test again."
	case d.At.IsEnd():
		return "likely: the map says the run had ended, but it went on deciding: a return the map draws did not happen. " + capital + "."
	case d.At.Retry != nil:
		return "likely: the map says the loop goes round here, but the run did something else first: a road inside the loop body that the map does not draw. " + capital + "."
	case d.At.Junction == nil:
		return "likely: the run took a road the map does not draw. " + capital + "."
	case d.Bad == d.Bad.Junction.Retry && d.Bad.Junction == d.At.Junction:
		return fmt.Sprintf("likely: the run went round `%s` without going into it first, which the map does not allow; run pathkit test again, and if it stays, the map is wrong here.", d.At.Junction.Label)
	case d.Bad == d.Bad.Junction.Retry:
		return fmt.Sprintf("likely: the run went round `%s` again without the decision `%s` inside the loop body: the body has a road the map does not draw (a continue, or a check that took its other side). %s.", d.Bad.Junction.Label, d.At.Junction.Label, capital)
	case d.Bad.Junction == d.At.Junction:
		return fmt.Sprintf("likely: the run took a way out of `%s` the map does not allow here. %s.", d.At.Junction.Label, capital)
	}
	return fmt.Sprintf("likely: the run reached `%s` without the map's next decision `%s`: between them it took a road the map does not draw. %s.", d.Bad.Junction.Label, d.At.Junction.Label, capital)
}

// UnmatchedSection is the text of the unmatched-trace report. It always
// says how many groups there are, so "none" is visible too.
func UnmatchedSection(res coverage.Result) string {
	items := UnmatchedTraces(res)
	files := 0
	for _, u := range items {
		files += u.Count
	}
	var b strings.Builder
	if len(items) == 0 {
		b.WriteString("Traces that fit no path: none\n")
	} else {
		fmt.Fprintf(&b, "Traces that fit no path: %s, %s\n", count(len(items), "group"), count(files, "trace file"))
	}
	// The Traces line counts traces beyond the path cap as unmatched too;
	// they fit a real path that is only not listed, so they are not here.
	if beyond := res.Counts.Unmatched - files; beyond > 0 {
		fmt.Fprintf(&b, "  (%s on the Traces line fit paths beyond the %d-path cap; they are not listed here)\n", count(beyond, "more unmatched trace"), model.DefaultMaxPaths)
	}
	for _, u := range items {
		state := "not acknowledged"
		if u.Acknowledged {
			state = "acknowledged: " + u.Reason
		}
		fmt.Fprintf(&b, "\n  %s, steps id %s, %s, %s\n", u.Workflow, u.StepsID, count(u.Count, "trace file"), state)
		steps := strings.Join(u.Steps, " ")
		if steps == "" {
			steps = "(none: the run made no decision)"
		}
		fmt.Fprintf(&b, "    recorded steps:    %s\n", steps)
		fmt.Fprintf(&b, "    followed the map:  %s\n", u.Followed)
		fmt.Fprintf(&b, "    then:              %s\n", u.Then)
		for i, e := range u.Expected {
			label := "                       "
			if i == 0 {
				label = "    the map expected:  "
			}
			b.WriteString(label + e + "\n")
		}
		if u.Cause != "" {
			fmt.Fprintf(&b, "    %s\n", u.Cause)
		}
	}
	return b.String()
}
