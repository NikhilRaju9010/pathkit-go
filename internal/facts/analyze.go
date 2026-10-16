package facts

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// Analyze walks one path of the analyzer's workflow and returns the claim
// (the path is impossible) or suspect (PathKit lost track of a value it was
// asked about twice) it finds, if any. A claim is final; a path with both
// returns both, and the caller prefers the claim.
func (a *Analyzer) Analyze(p model.Path) Result {
	if a.off != "" || len(p.Steps) == 0 {
		return Result{}
	}
	s := newState(a)
	s.run(a.g.Start.Nodes)
	for i, st := range p.Steps {
		step := i + 1
		switch st.Junction.Kind {
		case model.PlainIf:
			if c := s.ifStep(step, st.Junction, st.Exit); c != nil {
				return Result{Claim: c, Suspect: s.suspect}
			}
		case model.Loop:
			s.loopStep(st.Junction)
		}
		s.stepsDone = step
		s.run(st.Exit.To.Nodes)
	}
	return Result{Suspect: s.suspect}
}

func (s *state) loopStep(j *model.Junction) {
	if st, ok := j.Stmt.(ast.Stmt); ok {
		_, line := s.a.baseFile(st.Pos())
		reason := fmt.Sprintf("the loop at line %d may change it", line)
		for v := range s.a.assignedVars(st) {
			s.forgetVar(v, reason)
		}
	}
}

func (s *state) ifStep(step int, j *model.Junction, exit *model.Exit) *Claim {
	ifs, ok := j.Stmt.(*ast.IfStmt)
	if !ok {
		return nil
	}
	taken := exit.Label == "true"
	side := exit.Label
	si := stepInfo{step: step, where: fmt.Sprintf("%s (%s)", j.ID, j.Label), side: side, pos: ifs.Pos()}
	if taken {
		si.early = ifs.Else != nil && terminates(ifs.Else)
	} else {
		si.early = terminates(ifs.Body)
	}

	if t, used := s.eval(ifs.Cond, false); t != unknown && (t == isTrue) != taken {
		return s.claim(step, si, ifs, t, used)
	}
	if s.suspect == nil {
		if t, used := s.eval(ifs.Cond, true); t != unknown && (t == isTrue) != taken {
			s.noteSuspect(step, si, ifs, used)
		}
	}
	rec := takenStep{info: si}
	if taken && ifs.Else != nil {
		rec.skipped = ifs.Else
	} else if !taken {
		rec.skipped = ifs.Body
	}
	s.taken = append(s.taken, rec)
	if !s.inSkip(ifs.Pos()) {
		s.learn(ifs.Cond, taken, si)
	}
	return nil
}

// claim builds the claim and its proof: the facts the answer rests on, then
// the decision that cannot happen.
func (s *state) claim(step int, si stepInfo, ifs *ast.IfStmt, t tri, used []*datum) *Claim {
	whys := collect(used)
	kind := s.labelFor(whys, used, step)
	type item struct {
		time, seq int
		ps        ProofStep
	}
	var items []item
	earliest := step
	roots := map[*types.Var]bool{}
	for _, w := range whys {
		when := 2*w.atStep + 1
		if w.origin == oBranch || (w.origin == oCommaOk && w.step > 0) {
			when = 2 * w.step
		}
		if w.atStep+1 < earliest {
			earliest = w.atStep + 1
		}
		if w.key.v != nil {
			roots[w.key.v] = true
		}
		file, line := s.a.where(w)
		items = append(items, item{when, w.seq, ProofStep{Step: w.step, File: file, Line: line, Text: s.sentence(w)}})
	}
	// A decision between the facts and the claim that skipped code which
	// would have changed one of the values: that is why the fact still holds.
	for _, tk := range s.taken {
		if tk.info.step < earliest || tk.info.step >= step || tk.skipped == nil {
			continue
		}
		if text, ok := s.firstWrite(tk.skipped, roots); ok {
			f, l := s.a.baseFile(tk.info.pos)
			items = append(items, item{2 * tk.info.step, 1 << 30, ProofStep{Step: tk.info.step, File: f, Line: l,
				Text: fmt.Sprintf("%s takes the %s side, so `%s` is skipped.", tk.info.where, tk.info.side, text)}})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].time != items[j].time {
			return items[i].time < items[j].time
		}
		return items[i].seq < items[j].seq
	})
	var proof []ProofStep
	for _, it := range items {
		proof = append(proof, it.ps)
	}
	proof = append(proof, ProofStep{Step: step, File: baseName(s, ifs), Line: s.a.fset.Position(ifs.Pos()).Line,
		Text: fmt.Sprintf("%s takes the %s side.", si.where, si.side)})
	answer := "true"
	if t == isFalse {
		answer = "false"
	}
	proof = append(proof, ProofStep{Text: fmt.Sprintf("Nothing on the path changes this in between, so `%s` is always %s at that point and the %s side cannot be taken.", exprText(ifs.Cond), answer, si.side)})
	var known []string
	for _, w := range whys {
		known = append(known, s.sentence(w))
	}
	expl := fmt.Sprintf("%s takes the %s side, but there `%s` is always %s: %s", si.where, si.side, exprText(ifs.Cond), answer, strings.Join(known, " "))
	return &Claim{Step: step, Kind: kind, Explanation: expl, Proof: proof}
}

// firstWrite finds the first statement inside n that assigns one of the
// variables, as the plain text of that statement ("id = v").
func (s *state) firstWrite(n ast.Node, vars map[*types.Var]bool) (string, bool) {
	var text string
	ast.Inspect(n, func(c ast.Node) bool {
		if text != "" {
			return false
		}
		switch x := c.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			for _, l := range x.Lhs {
				if v := s.a.rootVar(l); v != nil && vars[v] {
					if len(x.Lhs) == 1 && len(x.Rhs) == 1 {
						text = exprText(x.Lhs[0]) + " " + x.Tok.String() + " " + exprText(x.Rhs[0])
					} else {
						text = "the assignment to " + exprText(l)
					}
					return false
				}
			}
		case *ast.IncDecStmt:
			if v := s.a.rootVar(x.X); v != nil && vars[v] {
				text = exprText(x.X) + x.Tok.String()
				return false
			}
		}
		return true
	})
	return text, text != ""
}

func baseName(s *state, n ast.Node) string {
	f, _ := s.a.baseFile(n.Pos())
	return f
}

// labelFor decides the label of a claim: see the Kind constants.
func (s *state) labelFor(whys []*why, used []*datum, claimStep int) Kind {
	if len(used) == 0 {
		return ConstantCondition
	}
	allBranch, sameKey, noEarly, allConst := true, true, true, true
	for _, w := range whys {
		leaf := w.origin == oDecl || w.origin == oConst || w.origin == oNil || w.origin == oNonNil
		if w.origin != oBranch {
			allBranch = false
		}
		if w.early {
			noEarly = false
		}
		if !leaf || w.atStep != claimStep-1 {
			allConst = false
		}
	}
	for _, d := range used {
		if d.why.key != used[0].why.key {
			sameKey = false
		}
	}
	switch {
	case allConst:
		return ConstantCondition
	case allBranch && sameKey && noEarly:
		return Contradictory
	}
	return ValueFlow
}

// collect gathers every reason in the chains behind the used facts, oldest
// first, each once.
func collect(used []*datum) []*why {
	seen := map[*why]bool{}
	var out []*why
	var add func(w *why)
	add = func(w *why) {
		if w == nil || seen[w] {
			return
		}
		seen[w] = true
		for _, p := range w.parents {
			add(p)
		}
		out = append(out, w)
	}
	for _, d := range used {
		add(d.why)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].seq < out[j].seq })
	return out
}

// sentence is the plain-words line for one reason.
func (s *state) sentence(w *why) string {
	switch w.origin {
	case oDecl, oConst, oNil, oNonNil:
		return fmt.Sprintf("`%s` %s.", w.key, w.text())
	case oCopy:
		return fmt.Sprintf("`%s` %s, so what is known about it carries over.", w.key, w.text())
	}
	return w.text()
}

// noteSuspect records the first possible-correlation on the path: the
// answer would be settled by an earlier decision on the same value, but
// PathKit had to forget that value in between.
func (s *state) noteSuspect(step int, si stepInfo, ifs *ast.IfStmt, used []*datum) {
	var first *datum
	for _, d := range used {
		if d.soft != "" && d.why.origin == oBranch && (first == nil || d.why.step < first.why.step) {
			first = d
		}
	}
	if first == nil {
		return
	}
	detail := fmt.Sprintf("Step %d and step %d ask about the same value (`%s`), but PathKit lost track of it in between: %s.", first.why.step, step, first.why.key, first.soft)
	explanation := detail + " If nothing changed the value, this path would be impossible; PathKit cannot tell, so please check."
	firstFile, firstLine := s.a.where(first.why)
	proof := []ProofStep{
		{Step: first.why.step, File: firstFile, Line: firstLine, Text: first.why.text()},
		{Step: step, File: baseName(s, ifs), Line: s.a.fset.Position(ifs.Pos()).Line, Text: fmt.Sprintf("%s takes the %s side.", si.where, si.side)},
		{Text: explanation},
	}
	s.suspect = &Suspect{First: first.why.step, Second: step, Detail: detail, Explanation: explanation, Proof: proof}
}
