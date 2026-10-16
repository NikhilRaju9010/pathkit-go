package facts

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
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
	s.initParams()
	s.run(a.g.Start.Nodes)
	for i, st := range p.Steps {
		step := i + 1
		var c *Claim
		switch st.Junction.Kind {
		case model.PlainIf:
			c = s.ifStep(step, st.Junction, st.Exit)
		case model.Loop:
			c = s.loopStep(step, st.Junction, st.Exit)
		case model.Switch:
			c = s.switchStep(step, st.Junction, st.Exit)
		}
		if c != nil {
			return Result{Claim: c, Suspect: s.suspect, Unknown: s.unknown}
		}
		s.stepsDone = step
		s.run(st.Exit.To.Nodes)
	}
	return Result{Suspect: s.suspect, Unknown: s.unknown}
}

// loopStep handles a step on a loop. Leaving a loop at its very first test is
// impossible when the test is true then (or a range has something to walk);
// whatever the loop assigns is forgotten.
func (s *state) loopStep(step int, j *model.Junction, exit *model.Exit) *Claim {
	st, ok := j.Stmt.(ast.Stmt)
	if !ok {
		return nil
	}
	first := !s.seenLoop[j]
	s.seenLoop[j] = true
	where := fmt.Sprintf("%s (%s)", j.ID, j.Label)
	if first && exit.Label == "exit" {
		var used []*datum
		var known bool
		var closing string
		switch l := st.(type) {
		case *ast.ForStmt:
			if l.Cond != nil {
				if t, u := s.eval(l.Cond, false); t == isTrue {
					used, known = u, true
					closing = fmt.Sprintf("`%s` is true the first time it is tested, so the loop always runs its body at least once.", exprText(l.Cond))
				}
			}
		case *ast.RangeStmt:
			if u, ok := s.nonEmpty(l.X); ok {
				used, known = u, true
				closing = fmt.Sprintf("`%s` is not empty, so the loop always runs its body at least once.", exprText(l.X))
			}
		}
		if known {
			return s.buildClaim(claimSpec{step: step, pos: st.Pos(), used: used, kind: LoopExit,
				decision: fmt.Sprintf("%s is left without running its body.", where), closing: closing,
				explanation: fmt.Sprintf("%s is left without running its body, but %%s", where)})
		}
	}
	// a range value takes the elements of what it walks
	var elemFacts []spec
	if rs, ok := st.(*ast.RangeStmt); ok && exit.Label == "iterate" && rs.Value != nil {
		elemFacts = s.rangeValueFacts(rs)
	}
	_, line := s.a.baseFile(st.Pos())
	reason := fmt.Sprintf("the loop at line %d may change it", line)
	for v := range s.a.assignedVars(st) {
		s.forgetVar(v, reason)
	}
	if rs, ok := st.(*ast.RangeStmt); ok && len(elemFacts) > 0 {
		if vk, ok := s.operand(rs.Value); ok && s.a.trackable(vk.v) {
			s.killKey(vk)
			s.apply(vk, elemFacts)
		}
	}
	return nil
}

// nonEmpty: is the string, slice or map in e known not to be empty?
func (s *state) nonEmpty(e ast.Expr) ([]*datum, bool) {
	k, ok := s.operand(e)
	if !ok {
		return nil, false
	}
	for _, d := range s.datums(k, false) {
		switch {
		case d.op == dLenNonZero:
			return []*datum{d}, true
		case d.op == dNe && d.val != nil && d.val.Kind() == constant.String && constant.StringVal(d.val) == "":
			return []*datum{d}, true
		case d.op == dEq && d.val != nil && d.val.Kind() == constant.String && constant.StringVal(d.val) != "":
			return []*datum{d}, true
		}
	}
	return nil, false
}

// rangeValueFacts: the value variable of "for _, v := range coll" holds one
// of the elements of coll.
func (s *state) rangeValueFacts(rs *ast.RangeStmt) []spec {
	ck, ok := s.operand(rs.X)
	if !ok || s.a.escapes[ck.v] != "" || !isInterface(s.a.info.TypeOf(rs.Value)) {
		return nil
	}
	for v := range s.a.assignedVars(rs) { // the loop changes the collection itself
		if v == ck.v {
			return nil
		}
	}
	for _, d := range s.datums(ck, false) {
		if d.op == dElem {
			cp := d.ts.clone()
			dd := s.typesDatum(cp, rs.Pos(), key{})
			dd.why.parents = append(dd.why.parents, datumWhys(d)...)
			return []spec{{d: *dd}}
		}
	}
	return nil
}

// switchStep handles a type switch: a case none of the possible types can
// take is impossible.
func (s *state) switchStep(step int, j *model.Junction, exit *model.Exit) *Claim {
	ts, ok := j.Stmt.(*ast.TypeSwitchStmt)
	if !ok {
		return nil
	}
	var x ast.Expr
	switch a := ts.Assign.(type) {
	case *ast.ExprStmt:
		if ta, ok := a.X.(*ast.TypeAssertExpr); ok {
			x = ta.X
		}
	case *ast.AssignStmt:
		if len(a.Rhs) == 1 {
			if ta, ok := a.Rhs[0].(*ast.TypeAssertExpr); ok {
				x = ta.X
			}
		}
	}
	if x == nil {
		return nil
	}
	set, used, known := s.valueTypes(x)
	where := fmt.Sprintf("%s (%s)", j.ID, j.Label)
	if !known {
		if s.unknown == nil && isInterface(s.a.info.TypeOf(x)) && !s.inSkip(ts.Pos()) {
			line := s.a.fset.Position(ts.Pos()).Line
			file, _ := s.a.baseFile(ts.Pos())
			detail := fmt.Sprintf("%s takes `%s`, but PathKit cannot follow where `%s` comes from, so it does not know which types it can hold.", where, exit.Label, exprText(x))
			s.unknown = &Suspect{First: step, Second: step, Detail: detail, Explanation: detail + " If no test has ever sent a value of that type, this path may never have been tried; please check what the input can hold.",
				Exit: exit.ID.String(), Group: fmt.Sprintf("%s:%d", file, line), GroupLine: line,
				Proof: []ProofStep{{Step: step, File: file, Line: line, Text: where + " takes `" + exit.Label + "`."}, {Text: detail}}}
		}
		return nil
	}
	cc, _ := exit.Road.(*ast.CaseClause)
	isDefault := cc == nil || cc.List == nil
	var caseTypes []types.Type
	nilCase := false
	for _, c := range ts.Body.List {
		clause := c.(*ast.CaseClause)
		for _, te := range clause.List {
			if tv, ok := s.a.info.Types[te]; ok && tv.IsNil() {
				nilCase = true
				continue
			}
			if t := s.a.info.TypeOf(te); t != nil {
				caseTypes = append(caseTypes, t)
			}
		}
	}
	possible := false
	if !isDefault {
		for _, te := range cc.List {
			if tv, ok := s.a.info.Types[te]; ok && tv.IsNil() {
				possible = possible || set.mayNil
				continue
			}
			T := s.a.info.TypeOf(te)
			for _, e := range set.ents {
				if T != nil && matchesCase(e.t, T) {
					possible = true
				}
			}
		}
	} else {
		possible = set.mayNil && !nilCase
		for _, e := range set.ents {
			matched := false
			for _, T := range caseTypes {
				if matchesCase(e.t, T) {
					matched = true
				}
			}
			if !matched {
				possible = true
			}
		}
	}
	if possible {
		return nil
	}
	holds := s.a.holdsText(set)
	closing := fmt.Sprintf("`%s` can hold %s, none of which takes `%s`, so that case cannot be taken.", exprText(x), strings.TrimPrefix(holds, "only "), exit.Label)
	if isDefault {
		closing = fmt.Sprintf("`%s` can hold %s, and every one of them takes an earlier case, so default cannot be taken.", exprText(x), strings.TrimPrefix(holds, "only "))
	}
	return s.buildClaim(claimSpec{step: step, pos: ts.Pos(), used: used, extraWhys: set.whys(), kind: ImpossibleType,
		decision: fmt.Sprintf("%s takes `%s`.", where, exit.Label), closing: closing,
		explanation: fmt.Sprintf("%s takes `%s`, but %%s", where, exit.Label)})
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
		answer := "true"
		if t == isFalse {
			answer = "false"
		}
		return s.buildClaim(claimSpec{step: step, pos: ifs.Pos(), used: used, nilCompare: isNilCompare(ifs.Cond),
			decision:    fmt.Sprintf("%s takes the %s side.", si.where, si.side),
			closing:     fmt.Sprintf("Nothing on the path changes this in between, so `%s` is always %s at that point and the %s side cannot be taken.", exprText(ifs.Cond), answer, si.side),
			explanation: fmt.Sprintf("%s takes the %s side, but there `%s` is always %s: %%s", si.where, si.side, exprText(ifs.Cond), answer)})
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

// isNilCompare: is the condition a comparison with nil?
func isNilCompare(e ast.Expr) bool {
	b, ok := ast.Unparen(e).(*ast.BinaryExpr)
	if !ok || (b.Op != token.EQL && b.Op != token.NEQ) {
		return false
	}
	for _, side := range []ast.Expr{b.X, b.Y} {
		if id, ok := ast.Unparen(side).(*ast.Ident); ok && id.Name == "nil" {
			return true
		}
	}
	return false
}

// claimSpec describes a claim to build: the step, the facts it rests on and
// the sentences around them.
type claimSpec struct {
	step       int
	pos        token.Pos
	used       []*datum
	extraWhys  []*why // more reasons to list (the types in a set)
	kind       Kind   // "" means decide from the facts
	nilCompare bool
	decision   string // the step's own sentence
	closing    string
	// explanation has one %s for the facts, written as sentences.
	explanation string
}

// buildClaim writes the claim and its proof: the facts the answer rests on,
// in order, then the decision that cannot happen, then what it adds up to.
func (s *state) buildClaim(c claimSpec) *Claim {
	whys := collect(c.used, c.extraWhys...)
	kind := c.kind
	if kind == "" {
		kind = s.labelFor(whys, c.used, c.step, c.nilCompare)
	}
	type item struct {
		time, seq int
		ps        ProofStep
	}
	var items []item
	earliest := c.step
	roots := map[*types.Var]bool{}
	assumes := false
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
		if w.origin == oJSON {
			assumes = true
		}
		file, line := s.a.where(w)
		items = append(items, item{when, w.seq, ProofStep{Step: w.step, File: file, Line: line, Text: s.sentence(w)}})
	}
	// A decision between the facts and the claim that skipped code which
	// would have changed one of the values: that is why the fact still holds.
	for _, tk := range s.taken {
		if tk.info.step < earliest || tk.info.step >= c.step || tk.skipped == nil {
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
	file, line := s.a.baseFile(c.pos)
	proof = append(proof, ProofStep{Step: c.step, File: file, Line: line, Text: c.decision})
	proof = append(proof, ProofStep{Text: c.closing})
	var known []string
	for _, w := range whys {
		known = append(known, s.sentence(w))
	}
	claim := &Claim{Step: c.step, Kind: kind, Explanation: fmt.Sprintf(c.explanation, strings.Join(known, " ")), Proof: proof}
	if assumes {
		claim.Assumptions = []string{AssumptionJSON}
	}
	return claim
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
func (s *state) labelFor(whys []*why, used []*datum, claimStep int, nilCompare bool) Kind {
	if len(used) == 0 {
		return ConstantCondition
	}
	for _, w := range whys {
		if w.origin == oTypeAssert {
			return ImpossibleType
		}
	}
	if nilCompare {
		// A value that holds a type (a typed pointer too) is not a nil
		// interface. The set is the one the comparison used: for coll[i] it is
		// the element set (dElem), which is what the check asked about.
		for _, d := range used {
			if (d.op == dTypes || d.op == dElem) && d.ts != nil && len(d.ts.ents) > 0 && !d.ts.mayNil {
				return ImpossibleNil
			}
		}
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
func collect(used []*datum, extra ...*why) []*why {
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
		for _, w := range datumWhys(d) {
			add(w)
		}
	}
	for _, w := range extra {
		add(w)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].seq < out[j].seq })
	return out
}

// sentence is the plain-words line for one reason.
func (s *state) sentence(w *why) string {
	switch w.origin {
	case oDecl, oConst, oNil, oNonNil, oStore, oJSON, oLen:
		if w.key.v == nil {
			return ensurePeriod(capitalize(w.text()))
		}
		return fmt.Sprintf("`%s` %s.", w.key, w.text())
	case oCopy:
		return fmt.Sprintf("`%s` %s, so what is known about it carries over.", w.key, w.text())
	}
	return ensurePeriod(w.text())
}

func ensurePeriod(s string) string {
	if s == "" || strings.HasSuffix(s, ".") {
		return s
	}
	return s + "."
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
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
