// Package render prints model data as text or Mermaid. It works only from
// the model, never from source code, so what is printed is exactly what
// the model decided.
package render

import (
	"fmt"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// PathText prints one path, e.g.
// "Start -> if x > 0 --false--> ChargeCard (activity) --failure--> End (failed)".
func PathText(p model.Path) string {
	var b strings.Builder
	b.WriteString("Start ->")
	for _, s := range p.Steps {
		if s.Exit == s.Junction.Retry {
			b.WriteString(" retry -->") // the loop goes round again; its head comes next
			continue
		}
		fmt.Fprintf(&b, " %s --%s-->", s.Junction.Label, s.Exit.Label)
	}
	b.WriteString(" " + p.End.String())
	return b.String()
}

// TotalLine is the "Total paths: N" line, marking a capped count.
func TotalLine(ps model.PathList, max int) string {
	if ps.Truncated {
		return fmt.Sprintf("Total paths: %d+ (truncated at maxPaths=%d)", len(ps.List), max)
	}
	return fmt.Sprintf("Total paths: %d", len(ps.List))
}

// TextOptions controls Analysis text output.
type TextOptions struct {
	Summary bool
	Limit   int // 0 means no limit
}

// Text prints one workflow's paths in the setup guide's format.
func Text(g *model.Graph, ps model.PathList, max int, opt TextOptions) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Workflow: %s\n%s\n", g.Workflow, TotalLine(ps, max))
	if opt.Summary {
		return b.String()
	}
	shown := ps.List
	if opt.Limit > 0 && len(shown) > opt.Limit {
		shown = shown[:opt.Limit]
	}
	for i, p := range shown {
		fmt.Fprintf(&b, "\n  %d. %s\n", i+1, PathText(p))
	}
	if more := len(ps.List) - len(shown); more > 0 {
		fmt.Fprintf(&b, "\n  ... and %d more paths (use --summary or increase --limit to see them)\n", more)
	}
	return b.String()
}

// Mermaid prints one workflow as a Mermaid flowchart.
func Mermaid(g *model.Graph, ps model.PathList, max int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Workflow: %s\n%s\n\n```mermaid\nflowchart TD\n", g.Workflow, TotalLine(ps, max))

	ids := map[*model.Junction]string{}
	fmt.Fprintf(&b, "  n0([\"Start\"])\n")
	for i, j := range g.Junctions {
		ids[j] = fmt.Sprintf("n%d", i+1)
		fmt.Fprintf(&b, "  %s{\"%s\"}\n", ids[j], escape(j.Label))
	}
	var ends []model.EndKind
	endID := map[model.EndKind]string{}
	target := func(t model.Target) string {
		if t.Retry != nil {
			return ids[t.Retry.Junction]
		}
		if t.Junction != nil {
			return ids[t.Junction]
		}
		if _, ok := endID[t.End]; !ok {
			endID[t.End] = fmt.Sprintf("e%d", len(ends))
			ends = append(ends, t.End)
		}
		return endID[t.End]
	}

	var edges []string
	if !g.Start.Dead() {
		edges = append(edges, fmt.Sprintf("  n0 --> %s", target(g.Start)))
	}
	for _, j := range g.Junctions {
		for _, e := range j.Exits {
			if e.To.Dead() {
				continue
			}
			label := e.Label
			if e.To.Retry != nil {
				label += ", then retry" // back to the loop's head
			}
			edges = append(edges, fmt.Sprintf("  %s -->|%s| %s", ids[j], escape(label), target(e.To)))
		}
	}
	for _, k := range ends {
		fmt.Fprintf(&b, "  %s([\"%s\"])\n", endID[k], escape(k.String()))
	}
	b.WriteString(strings.Join(edges, "\n"))
	b.WriteString("\n```\n")
	return b.String()
}

var escaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")

func escape(s string) string { return escaper.Replace(s) }
