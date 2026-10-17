package model

import (
	"fmt"
	"go/ast"
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
	// The workflow function, and the declared helpers whose code the graph
	// follows into: a comment on a decision in a helper marks that decision in
	// every copy of the helper.
	type region struct {
		file     *ast.File
		from, to token.Pos
	}
	regions := []region{{b.wf.File, b.wf.Func.Pos(), b.wf.Func.End()}}
	seen := map[*ast.FuncDecl]bool{}
	for _, s := range g.sites {
		if s.Decl != nil && !seen[s.Decl] {
			seen[s.Decl] = true
			regions = append(regions, region{s.File, s.Decl.Pos(), s.Decl.End()})
		}
	}
	for _, rg := range regions {
		if err := b.attachMarksIn(g, rg.file, rg.from, rg.to); err != nil {
			return err
		}
	}
	return nil
}

// attachMarksIn reads the comments of one function.
func (b *builder) attachMarksIn(g *Graph, file *ast.File, from, to token.Pos) error {
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			if c.Pos() < from || c.End() > to {
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

			copies, err := b.markedJunctions(g, pos)
			if err != nil {
				return fail("%v", err)
			}
			for _, j := range copies {
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
	}
	return nil
}

// markedJunctions finds the decision a comment at pos belongs to: one on
// the same line, else one on the next line. Inside a helper there is one copy
// of the decision for every call that was followed; all of them are returned.
func (b *builder) markedJunctions(g *Graph, pos token.Position) ([]*Junction, error) {
	for _, l := range []int{pos.Line, pos.Line + 1} {
		byStmt := map[ast.Node][]*Junction{}
		var order []ast.Node
		for _, j := range g.Junctions {
			if j.Pos.Line == l && j.Pos.Filename == pos.Filename {
				if byStmt[j.Stmt] == nil {
					order = append(order, j.Stmt)
				}
				byStmt[j.Stmt] = append(byStmt[j.Stmt], j)
			}
		}
		switch len(order) {
		case 0:
		case 1:
			return byStmt[order[0]], nil
		default:
			return nil, fmt.Errorf("more than one decision starts on line %d; put the comment where only one does", l)
		}
	}
	return nil, fmt.Errorf("there is no decision PathKit maps on this line or the next (an if that is not a junction, such as a plain error check, has nothing to mark)")
}
