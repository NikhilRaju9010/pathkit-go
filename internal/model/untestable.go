package model

import (
	"fmt"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
)

// Mark is one //pathkit:untestable comment, attached to the decision it
// sits on: every path that takes this exit at this junction is one a
// person says can never run.
//
// Syntax (on the decision's own line, or the line above it):
//
//	//pathkit:untestable exit="false" reason="the carrier never sends this"
//
// exit is the exit's label as `pathkit analyze` prints it ("true",
// "failure", `case "x"`, "default", ...). Both exit and reason are
// required. This is NOT //pathkit:ignore, which takes a decision off the
// map; a marked path stays on the map and counts in overall coverage.
type Mark struct {
	Junction *Junction
	Exit     *Exit
	Reason   string
	Pos      token.Position // of the comment
}

// PragmaError is a //pathkit:untestable comment PathKit can't use. The
// workflow is not analyzable until the comment is fixed or removed: a
// comment that silently did nothing would hide a decision somebody made.
type PragmaError struct {
	Pos token.Position
	Msg string
}

func (e *PragmaError) Error() string {
	return fmt.Sprintf("//pathkit:untestable at %s:%d: %s", filepath.Base(e.Pos.Filename), e.Pos.Line, e.Msg)
}

const untestablePrefix = "//pathkit:untestable"

// A setting's value is in double quotes; \" and \\ inside it stand for " and
// \ (an exit label such as case "legacy" has quotes in it).
var untestableAttr = regexp.MustCompile(`(\w+)\s*=\s*"((?:[^"\\]|\\.)*)"`)

var unescape = strings.NewReplacer(`\"`, `"`, `\\`, `\`)

// attachMarks reads the //pathkit:untestable comments inside the workflow
// function and attaches each to its decision. g's junctions are numbered
// and have their exits by now.
func (b *builder) attachMarks(g *Graph) error {
	for _, cg := range b.wf.File.Comments {
		for _, c := range cg.List {
			if c.Pos() < b.wf.Func.Pos() || c.End() > b.wf.Func.End() {
				continue
			}
			pos := b.fset.Position(c.Slash)
			fail := func(format string, args ...any) error {
				return &PragmaError{Pos: pos, Msg: fmt.Sprintf(format, args...)}
			}
			exact := strings.HasPrefix(c.Text, untestablePrefix)
			if exact {
				if r := c.Text[len(untestablePrefix):]; r != "" && r[0] != ' ' && r[0] != '\t' {
					exact = false // //pathkit:untestables
				}
			}
			if !exact {
				// A comment that looks like ours but is not written exactly
				// right would silently do nothing: refuse it instead.
				if t := strings.TrimSpace(strings.TrimPrefix(c.Text, "//")); strings.HasPrefix(t, "pathkit:untest") {
					return fail("this looks like //pathkit:untestable but is not written exactly that way (no space after //, and the word untestable); write //pathkit:untestable exit=\"...\" reason=\"...\"")
				}
				continue
			}
			rest := c.Text[len(untestablePrefix):]
			attrs := map[string]string{}
			for _, m := range untestableAttr.FindAllStringSubmatch(rest, -1) {
				switch m[1] {
				case "exit", "reason":
				default:
					return fail("unknown setting %q; write exit=\"...\" reason=\"...\"", m[1])
				}
				if _, dup := attrs[m[1]]; dup {
					return fail("%s is given twice", m[1])
				}
				attrs[m[1]] = unescape.Replace(m[2])
			}
			if left := strings.TrimSpace(untestableAttr.ReplaceAllString(rest, "")); left != "" {
				return fail("could not read %q; write exit=\"...\" reason=\"...\" (both in double quotes)", left)
			}
			if strings.TrimSpace(attrs["reason"]) == "" {
				return fail(`it needs a reason, like reason="the carrier never sends this"`)
			}
			if strings.TrimSpace(attrs["exit"]) == "" {
				return fail(`it needs the exit it marks, like exit="false" (as "pathkit analyze" prints it)`)
			}

			j, err := b.markedJunction(g, pos.Line)
			if err != nil {
				return fail("%v", err)
			}
			var exit *Exit
			var labels []string
			for _, e := range j.Exits {
				labels = append(labels, e.Label)
				if e.Label == attrs["exit"] {
					exit = e
				}
			}
			if exit == nil {
				return fail("%s has no exit %q; its exits are: %s", j.Label, attrs["exit"], strings.Join(labels, ", "))
			}
			g.Marks = append(g.Marks, Mark{Junction: j, Exit: exit, Reason: attrs["reason"], Pos: pos})
		}
	}
	return nil
}

// markedJunction finds the decision a comment on line belongs to: one on
// the same line, else one on the next line.
func (b *builder) markedJunction(g *Graph, line int) (*Junction, error) {
	for _, l := range []int{line, line + 1} {
		var found []*Junction
		for _, j := range g.Junctions {
			if j.Pos.Line == l {
				found = append(found, j)
			}
		}
		switch len(found) {
		case 0:
		case 1:
			return found[0], nil
		default:
			return nil, fmt.Errorf("more than one decision starts on line %d; put the comment where only one does", l)
		}
	}
	return nil, fmt.Errorf("there is no decision PathKit maps on this line or the next (an if that is not a junction, such as a plain error check, has nothing to mark)")
}
