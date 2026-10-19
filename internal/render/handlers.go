package render

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
)

// HandlerName writes a handler's name for a table: "add", or "(computed)".
func HandlerName(h coverage.HandlerResult) string {
	if h.Name == "" {
		return "(computed)"
	}
	return h.Name
}

// HandlerLine is one row of the handler table on one line.
func HandlerLine(h coverage.HandlerResult) string {
	s := fmt.Sprintf("%s %s (%s, registered at %s): exercised by a test: %s", h.Kind, HandlerName(h), h.Owner, filepath.ToSlash(h.Where), h.Exercised)
	if h.Branches > 0 || h.Workflow != "" {
		if h.Branches == 0 {
			s += "; no branches of its own"
		} else {
			s += fmt.Sprintf("; branches %d/%d", h.BranchesTaken, h.Branches)
		}
	}
	return s
}

// HandlerLines is the handler table as text: a heading and a line per
// handler, or with gapsOnly just the ones that need attention (a handler no
// test exercised, or with branches no test took). Nothing when no workflow
// registers a handler.
func HandlerLines(res coverage.Result, gapsOnly bool) []string {
	n := len(res.Handlers)
	if n == 0 {
		return nil
	}
	gaps := 0
	for _, h := range res.Handlers {
		if h.IsGap() {
			gaps++
		}
	}
	unknown := 0
	for _, h := range res.Handlers {
		if h.Exercised == "not recorded" {
			unknown++
		}
	}
	head := fmt.Sprintf("Handlers: %d registered (signals, queries, updates and update validators), %d with a gap", n, gaps)
	if unknown > 0 {
		head += fmt.Sprintf("; for %d of them a recorded run cannot show whether a test used them (\"not recorded\")", unknown)
	}
	lines := []string{head + ":"}
	if gaps == 0 && gapsOnly {
		lines[0] = head
		return lines
	}
	for _, h := range res.Handlers {
		if gapsOnly && !h.IsGap() {
			continue
		}
		lines = append(lines, "  "+HandlerLine(h))
	}
	return lines
}

// HandlerBlock is HandlerLines as text ending in a newline ("" when none).
func HandlerBlock(res coverage.Result, gapsOnly bool) string {
	lines := HandlerLines(res, gapsOnly)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n\n"
}

// CallOutcomeLine is one call site of the outcome table on one line, with
// every outcome and whether a test saw it.
func CallOutcomeLine(r coverage.CallOutcomeRow) string {
	var parts []string
	for _, o := range r.Outcomes {
		state := "not seen"
		if o.Seen {
			state = "seen"
		}
		parts = append(parts, o.Outcome+" "+state)
	}
	return fmt.Sprintf("%s:%d: %s (%s) in %s: %s", filepath.ToSlash(r.File), r.Line, r.Name, r.Kind, r.Workflow, strings.Join(parts, ", "))
}

// CallOutcomeLines is the outcome table as text: a heading and a line per call
// site, or with gapsOnly just the call sites with an outcome no test saw, and
// only those outcomes. Nothing when no workflow checks the result of such a
// call.
func CallOutcomeLines(res coverage.Result, gapsOnly bool) []string {
	if len(res.CallOutcomes) == 0 {
		return nil
	}
	gapSites, gaps := 0, 0
	for _, r := range res.CallOutcomes {
		if g := r.Gaps(); len(g) > 0 {
			gapSites++
			gaps += len(g)
		}
	}
	head := fmt.Sprintf("Call outcomes: %s checked, %s no test saw", count(len(res.CallOutcomes), "activity, child workflow or Nexus call"), count(gaps, "outcome"))
	if !gapsOnly {
		lines := []string{head + ":"}
		for _, r := range res.CallOutcomes {
			lines = append(lines, "  "+CallOutcomeLine(r))
		}
		return lines
	}
	if gaps == 0 {
		return []string{head}
	}
	lines := []string{head + ":"}
	for _, r := range res.CallOutcomes {
		if g := r.Gaps(); len(g) > 0 {
			lines = append(lines, fmt.Sprintf("  %s:%d: %s (%s) in %s: not seen: %s", filepath.ToSlash(r.File), r.Line, r.Name, r.Kind, r.Workflow, strings.Join(g, ", ")))
		}
	}
	return lines
}

// CallOutcomeBlock is CallOutcomeLines as text ending in a blank line ("" when none).
func CallOutcomeBlock(res coverage.Result, gapsOnly bool) string {
	lines := CallOutcomeLines(res, gapsOnly)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n\n"
}

// LoopRoundLine is one loop of the loop rounds table on one line.
func LoopRoundLine(r coverage.LoopRoundsRow) string {
	var parts []string
	for _, c := range r.Rounds {
		switch {
		case !c.Possible:
			parts = append(parts, c.Bucket+" cannot happen")
		case c.Seen:
			parts = append(parts, c.Bucket+" seen")
		default:
			parts = append(parts, c.Bucket+" not seen")
		}
	}
	return fmt.Sprintf("%s:%d: %s in %s: %s", filepath.ToSlash(r.File), r.Line, r.Loop, r.Workflow, strings.Join(parts, ", "))
}

// LoopRoundLines is the loop rounds table as text, or with gapsOnly only the
// loops with a round count that can happen and no test ran (a loop never
// tested with 2+ rounds is the one that matters). Nothing when no workflow
// has a loop on its map.
func LoopRoundLines(res coverage.Result, gapsOnly bool) []string {
	if len(res.LoopRounds) == 0 {
		return nil
	}
	gaps := 0
	for _, r := range res.LoopRounds {
		if len(r.Gaps()) > 0 {
			gaps++
		}
	}
	head := fmt.Sprintf("Loop rounds: %s on the map, %s with a round count no test ran", count(len(res.LoopRounds), "loop"), count(gaps, "loop"))
	if gapsOnly && gaps == 0 {
		return []string{head}
	}
	lines := []string{head + ":"}
	for _, r := range res.LoopRounds {
		g := r.Gaps()
		if gapsOnly && len(g) == 0 {
			continue
		}
		if gapsOnly {
			lines = append(lines, fmt.Sprintf("  %s:%d: %s in %s: no test ran it with: %s", filepath.ToSlash(r.File), r.Line, r.Loop, r.Workflow, strings.Join(g, ", ")))
		} else {
			lines = append(lines, "  "+LoopRoundLine(r))
		}
	}
	return lines
}

// LoopRoundBlock is LoopRoundLines as text ending in a blank line ("" when none).
func LoopRoundBlock(res coverage.Result, gapsOnly bool) string {
	lines := LoopRoundLines(res, gapsOnly)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n\n"
}
