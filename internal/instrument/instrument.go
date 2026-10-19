// Package instrument makes the marked-up copies of workflow files that
// "pathkit test" swaps in with "go test -overlay". The user's files are
// never changed.
//
// Two rules keep the copies honest:
//   - No line break is ever inserted, so every line keeps its number and
//     test failures and panics point at the user's real lines.
//   - Every recorded ID comes from model (Graph.ExitFor). This package has
//     no ID logic of its own.
package instrument

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/template"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// RecorderFile is the name of the generated recorder file added to each
// package through the overlay. It never exists on disk in the project.
const RecorderFile = "zz_pathkit_recorder.go"

// Names the generated code adds. If the user's code already uses one,
// instrumenting stops with a clear error instead of producing broken code.
var packageNames = []string{"pathkitStart", "pathkitRecorder", "pathkitTrace", "pathkitTraceDir", "pathkitLoop",
	"pathkitSite", "pathkitEnter", "pathkitFrame", "pathkitTables", "pathkitCtxKey", "pathkitPending", "pathkitSource", "pathkitTestName"}

// maxResults is how many results a workflow function may have; the
// recorder declares one pathkitRetN helper for each N up to it.
const maxResults = 8

const recVar = "pathkitRec"

// frameVar is the name of the handle a marked-up helper records through.
const frameVar = "pathkitH"

// ctxVar is the name given to a workflow's context parameter that the
// user wrote as "_", so the recorder can be started with it.
const ctxVar = "pathkitCtx"

// Target is one workflow to instrument, with its graph.
type Target struct {
	Workflow discover.Workflow
	Graph    *model.Graph
}

// Edit is one text insertion into an original file.
type Edit struct {
	Offset int    // byte offset in the original file
	Order  int    // for edits at the same offset: lower Order ends up first
	Text   string // never contains a line break
	// Delete is how many original bytes at Offset the text replaces. It is
	// used for one thing only: naming a context parameter written as "_".
	Delete int
}

// Result holds the generated sources, keyed by the real file path they
// stand in for (an existing workflow file, or a new recorder file).
type Result struct {
	Files map[string][]byte
	Edits map[string][]Edit // per workflow file, for tests
}

// Instrument marks up every target's file and adds one recorder file per
// package. traceDir must be absolute; it is baked into the recorder.
//
// The code of helper functions that a target's graph follows into is marked
// up too (docs/DESIGN.md D15), in whatever file it is. A helper used by several
// workflows is marked up once: every edit to it is the same for all of them
// (it depends only on the code, never on the workflow), so the edits of all
// targets are merged and an edit that is already there (same file, offset and
// text) is dropped. A workflow that does not follow a call another one wraps
// is not harmed: the recorder ignores keys its own table does not have.
func Instrument(targets []Target, traceDir string) (*Result, error) {
	res := &Result{Files: map[string][]byte{}, Edits: map[string][]Edit{}}
	perFile := map[string][]Edit{}
	type fileEdit struct {
		file string
		Edit
	}
	seen := map[fileEdit]bool{}
	packages := map[string]string{}                     // package dir -> package name
	tables := map[string]map[string]map[string]string{} // package dir -> workflow -> table
	for _, t := range targets {
		byFile, err := workflowEdits(t)
		if err != nil {
			return nil, err
		}
		for file, es := range byFile {
			for _, e := range es {
				if k := (fileEdit{file, e}); !seen[k] {
					seen[k] = true
					perFile[file] = append(perFile[file], e)
				}
			}
		}
		dir := filepath.Dir(t.Workflow.Filename)
		packages[dir] = t.Workflow.Pkg.Name
		if tables[dir] == nil {
			tables[dir] = map[string]map[string]string{}
		}
		tables[dir][t.Workflow.Name] = t.Graph.RecorderTable()
	}
	for filename, edits := range perFile {
		src, err := os.ReadFile(filename)
		if err != nil {
			return nil, err
		}
		res.Files[filename] = Apply(src, edits)
		res.Edits[filename] = edits
	}

	for dir, name := range packages {
		path := filepath.Join(dir, RecorderFile)
		if _, err := os.Stat(path); err == nil {
			return nil, fmt.Errorf("cannot record %s: a file named %s already exists there", dir, RecorderFile)
		}
		var buf bytes.Buffer
		if err := recorderTemplate.Execute(&buf, map[string]any{"Package": name, "TraceDir": traceDir, "Rets": retHelpers(), "Tables": tableTexts(tables[dir])}); err != nil {
			return nil, err
		}
		res.Files[path] = buf.Bytes()
	}
	return res, nil
}

// tableText is one workflow's lookup table, written as Go source.
type tableText struct{ Workflow, Entries string }

// tableTexts writes the tables in a fixed order, so the same code always
// gives the same recorder file.
func tableTexts(tables map[string]map[string]string) []tableText {
	var out []tableText
	for _, wf := range sortedKeys(tables) {
		var b strings.Builder
		for _, k := range sortedKeys(tables[wf]) {
			fmt.Fprintf(&b, "%q: %q, ", k, tables[wf][k])
		}
		out = append(out, tableText{Workflow: wf, Entries: b.String()})
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// scope is the code a junction's edits go into: the workflow function (it
// records through the recorder, with the IDs of the model) or a helper (it
// records through its handle, with keys of its own that the recorder's table
// turns into IDs).
type scope struct {
	recv    string
	exitKey func(j *model.Junction, label string) (string, bool)
	loopKey func(j *model.Junction) string
}

func workflowScope(g *model.Graph) scope {
	return scope{
		recv: recVar,
		exitKey: func(j *model.Junction, label string) (string, bool) {
			id, ok := g.ExitFor(j.Stmt, label)
			return id.String(), ok
		},
		loopKey: func(j *model.Junction) string { return j.ID },
	}
}

func helperScope(g *model.Graph) scope {
	return scope{
		recv: frameVar,
		exitKey: func(j *model.Junction, label string) (string, bool) {
			return g.LocalKey(j.Stmt, label), true
		},
		loopKey: func(j *model.Junction) string { return g.LocalKey(j.Stmt, "loop") },
	}
}

// workflowEdits lists the insertions for one workflow function and for the
// helper code its graph follows into, by file.
func workflowEdits(t Target) (map[string][]Edit, error) {
	wf, g := t.Workflow, t.Graph
	fset := wf.Pkg.Fset
	off := func(n ast.Node) int { return fset.Position(n.Pos()).Offset }
	end := func(n ast.Node) int { return fset.Position(n.End()).Offset }

	if err := checkNames(wf, g); err != nil {
		return nil, err
	}
	params := wf.Func.Type.Params.List
	if len(params[0].Names) == 0 {
		return nil, fmt.Errorf("cannot record %s: its parameters have no names (func %s(workflow.Context, ...)); give them names", wf.Name, wf.Func.Name.Name)
	}
	ctxName := params[0].Names[0].Name
	var edits []Edit
	if ctxName == "_" {
		// "func W(_ workflow.Context, ...)": name the parameter in the
		// marked-up copy (one character replaced by a name, same line).
		ctxName = ctxVar
		edits = append(edits, Edit{Offset: off(params[0].Names[0]), Delete: 1, Text: ctxVar})
	}

	hash := g.CodeHash()
	edits = append(edits, Edit{
		Offset: off(wf.Func.Body) + 1, // just after the function's "{"
		Text:   fmt.Sprintf(" %s := pathkitStart(%s, %q, %q, %q); defer %s.flush();", recVar, ctxName, wf.Name, hash, wf.Pkg.PkgPath, recVar),
	})

	labels := labelsOf(wf.Func.Body)
	wscope := workflowScope(g)
	for _, j := range g.Junctions {
		if j.Site != nil {
			continue
		}
		e, err := junctionEdits(wscope, g, j, labels, fset)
		if err != nil {
			return nil, fmt.Errorf("%v in %s", err, wf.Name)
		}
		edits = append(edits, e...)
	}

	resultTypes, err := resultTypeTexts(wf)
	if err != nil {
		return nil, err
	}
	var retErr error
	ast.Inspect(wf.Func.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false // a closure's return isn't the workflow's return
		case *ast.ReturnStmt:
			e, err := returnEdits(wf, n, resultTypes, off, end)
			if err != nil && retErr == nil {
				retErr = err
			}
			edits = append(edits, e...)
		}
		return true
	})
	if retErr != nil {
		return nil, retErr
	}
	out := map[string][]Edit{wf.Filename: edits}

	// The code of helpers: hand the site over at each call, take it at the
	// top of the helper, and record the helper's own decisions.
	hscope := helperScope(g)
	for _, s := range g.Sites() {
		recv := recVar
		if s.Parent != nil {
			recv = frameVar
		}
		file := fset.Position(s.Call.Pos()).Filename
		out[file] = append(out[file],
			Edit{Offset: off(s.Ctx), Text: fmt.Sprintf("pathkitSite(%s, ", recv)},
			Edit{Offset: end(s.Ctx), Order: 3, Text: fmt.Sprintf(", %q)", g.CallKey(s.Call))})
	}
	for _, uc := range g.UnitCalls() {
		recv := recVar
		if uc.From != nil {
			recv = frameVar
		}
		file := fset.Position(uc.Call.Pos()).Filename
		out[file] = append(out[file],
			Edit{Offset: off(uc.Ctx), Text: fmt.Sprintf("pathkitSite(%s, ", recv)},
			Edit{Offset: end(uc.Ctx), Order: 3, Text: fmt.Sprintf(", %q)", g.CallKey(uc.Call))})
	}
	done := map[ast.Node]bool{}
	for _, s := range g.Helpers() {
		file := fset.Position(s.Body.Pos()).Filename
		ctxIdent := s.CtxName.Name
		var es []Edit
		if ctxIdent == "_" {
			ctxIdent = ctxVar
			es = append(es, Edit{Offset: off(s.CtxName), Delete: 1, Text: ctxVar})
		}
		es = append(es, Edit{
			Offset: off(s.Body) + 1,
			Text:   fmt.Sprintf(" %s, %s := pathkitEnter(%s, %q); defer %s.exit();", frameVar, ctxIdent, ctxIdent, s.Key, frameVar),
		})
		hlabels := labelsOf(s.Body)
		for _, j := range g.AllJunctions() {
			if j.Site == nil || j.Site.Body != s.Body {
				continue
			}
			st := j.Stmt
			if done[st] {
				continue // one edit per decision, however many copies of the helper there are
			}
			done[st] = true
			e, err := junctionEdits(hscope, g, j, hlabels, fset)
			if err != nil {
				return nil, fmt.Errorf("%v in %s", err, wf.Name)
			}
			es = append(es, e...)
		}
		out[file] = append(out[file], es...)
	}
	return out, nil
}

// junctionEdits records the exits of one decision, in the workflow function
// or in a helper (sc).
func junctionEdits(sc scope, g *model.Graph, j *model.Junction, labels map[ast.Stmt]*ast.LabeledStmt, fset *token.FileSet) ([]Edit, error) {
	off := func(n ast.Node) int { return fset.Position(n.Pos()).Offset }
	end := func(n ast.Node) int { return fset.Position(n.End()).Offset }
	if j.Kind == model.Loop {
		return loopEdits(sc, g, j, labels, off, end)
	}
	if rs, ok := j.Stmt.(*ast.ReturnStmt); ok {
		return returnCallEdits(sc, j, rs, off, end)
	}
	var edits []Edit
	var fellInto map[*ast.CaseClause]bool
	if body := switchBody(j.Stmt); body != nil {
		var e []Edit
		e, fellInto = fallthroughEdits(sc.recv, body, off)
		edits = append(edits, e...)
	}
	for _, e := range j.Exits {
		key, ok := sc.exitKey(j, e.Label)
		if !ok {
			return nil, fmt.Errorf("internal error: no ID for %s exit %q", j.ID, e.Label)
		}
		hit := fmt.Sprintf("%s.hit(%q)", sc.recv, key)
		switch road := e.Road.(type) {
		case *ast.BlockStmt:
			edits = append(edits, Edit{Offset: off(road) + 1, Text: " " + hit + ";"})
		case *ast.IfStmt: // an "else if": wrap it in a block that records first
			edits = append(edits,
				Edit{Offset: off(road), Text: "{ " + hit + "; "},
				Edit{Offset: end(road), Order: 2, Text: " }"})
		case *ast.CaseClause: // record first thing after "case ...:"
			if fellInto[road] {
				// Reached by the previous case's fallthrough, the
				// run is still on that case's exit: record only when
				// this case was chosen by the switch itself.
				hit = fmt.Sprintf("%s.hitUnlessFell(%q)", sc.recv, key)
			}
			edits = append(edits, Edit{Offset: fset.Position(road.Colon).Offset + 1, Text: " " + hit + ";"})
		case nil:
			switch s := j.Stmt.(type) {
			case *ast.IfStmt: // no else: add one
				edits = append(edits, Edit{Offset: end(s.Body), Order: 1, Text: " else { " + hit + " }"})
			default: // a switch with no default: add one before its "}"
				body := switchBody(s)
				edits = append(edits, Edit{Offset: fset.Position(body.Rbrace).Offset, Text: "; default: " + hit + " "})
			}
		default:
			return nil, fmt.Errorf("internal error: unexpected road %T", road)
		}
	}
	return edits, nil
}

// returnCallEdits records the decision of a "return <call>" (an error check
// that is a return): the last result is wrapped, so the run records the
// failure or the success exit as the error comes back. The wrap sits inside
// the one returnEdits puts around the results (a lower Order is written first
// at the start and, here, also at the end).
func returnCallEdits(sc scope, j *model.Junction, rs *ast.ReturnStmt, off, end func(ast.Node) int) ([]Edit, error) {
	failure, ok1 := sc.exitKey(j, "failure")
	success, ok2 := sc.exitKey(j, "success")
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("internal error: no ID for the exits of %s", j.ID)
	}
	last := rs.Results[len(rs.Results)-1]
	return []Edit{
		{Offset: off(last), Order: 1, Text: fmt.Sprintf("%s.errExit(%q, %q, ", sc.recv, failure, success)},
		{Offset: end(last), Order: 2, Text: ")"},
	}, nil
}

// switchBody returns the { case ... } block of a switch or type switch,
// or nil for any other statement.
func switchBody(n ast.Node) *ast.BlockStmt {
	switch s := n.(type) {
	case *ast.SwitchStmt:
		return s.Body
	case *ast.TypeSwitchStmt:
		return s.Body
	}
	return nil
}

// fallthroughEdits marks every "fallthrough" in a switch, so the case it
// falls into doesn't record a second exit, and returns those cases.
func fallthroughEdits(recv string, body *ast.BlockStmt, off func(ast.Node) int) ([]Edit, map[*ast.CaseClause]bool) {
	var edits []Edit
	fellInto := map[*ast.CaseClause]bool{}
	for i, c := range body.List {
		stmts := c.(*ast.CaseClause).Body
		if len(stmts) == 0 || i+1 == len(body.List) {
			continue
		}
		if br, ok := stmts[len(stmts)-1].(*ast.BranchStmt); ok && br.Tok == token.FALLTHROUGH {
			edits = append(edits, Edit{Offset: off(br), Text: recv + ".fell(); "})
			fellInto[body.List[i+1].(*ast.CaseClause)] = true
		}
	}
	return edits, fellInto
}

// loopEdits records a loop junction (the loop rule, docs/DESIGN.md D3). Just
// before the loop statement (before its label, if it has one), enter
// hands the recorder the loop's three exit IDs from the model and resets
// the loop. After that, every decision at the loop's head goes through
// pathkitRec.loop, which records "retry" when the body already ran, then
// "iterate" or "exit":
//   - "for init; cond; post" and "for cond": the condition is wrapped,
//     so each time it is checked is one decision (continue included);
//   - "for {}" and range: loop(key, true) at the top of the body, since
//     every time the body starts, the loop chose to go in;
//   - range also gets rangeDone after its "}", which records the run-out
//     (retry, exit), and broke before every break that leaves it, because
//     a break leaves without an "exit" step.
func loopEdits(sc scope, g *model.Graph, j *model.Junction, labels map[ast.Stmt]*ast.LabeledStmt, off, end func(ast.Node) int) ([]Edit, error) {
	id := func(label string) (string, error) {
		k, ok := sc.exitKey(j, label)
		if !ok {
			return "", fmt.Errorf("internal error: no ID for %s exit %q", j.ID, label)
		}
		return k, nil
	}
	iterate, err := id("iterate")
	if err != nil {
		return nil, err
	}
	retry, err := id("retry")
	if err != nil {
		return nil, err
	}
	exit := "" // a "for {}" has no exit
	if k, ok := sc.exitKey(j, "exit"); ok && hasExit(j) {
		exit = k
	}

	stmt := j.Stmt.(ast.Stmt)
	start := ast.Node(stmt)
	if l := labels[stmt]; l != nil {
		start = l
	}
	key := sc.loopKey(j)
	recv := sc.recv
	edits := []Edit{{Offset: off(start), Text: fmt.Sprintf("%s.enter(%q, %q, %q, %q); ", recv, key, iterate, exit, retry)}}
	atBodyTop := func(body *ast.BlockStmt) Edit {
		return Edit{Offset: off(body) + 1, Text: fmt.Sprintf(" %s.loop(%q, true);", recv, key)}
	}

	switch s := stmt.(type) {
	case *ast.ForStmt:
		if s.Cond == nil {
			return append(edits, atBodyTop(s.Body)), nil
		}
		// pathkitRec.loop takes a plain bool. A condition of a named bool type
		// ("type Flag bool; for !done") or of a type parameter constrained by
		// ~bool needs a conversion, which means the same.
		open, shut := fmt.Sprintf("%s.loop(%q, ", recv, key), ")"
		if !plainBool(g.TypesInfo(), s.Cond) {
			open, shut = open+"bool(", "))"
		}
		return append(edits,
			Edit{Offset: off(s.Cond), Text: open},
			Edit{Offset: end(s.Cond), Order: 3, Text: shut}), nil
	case *ast.RangeStmt:
		edits = append(edits, atBodyTop(s.Body), Edit{Offset: end(s), Text: fmt.Sprintf("; %s.rangeDone(%q)", recv, key)})
		name := ""
		if l := labels[stmt]; l != nil {
			name = l.Label.Name
		}
		for _, br := range breaksLeaving(s.Body, name) {
			edits = append(edits, Edit{Offset: off(br), Text: fmt.Sprintf("%s.broke(%q); ", recv, key)})
		}
		return edits, nil
	}
	return nil, fmt.Errorf("internal error: %s is a loop junction but not a loop", j.ID)
}

// hasExit: the loop junction has an "exit" exit (a "for {}" has none).
func hasExit(j *model.Junction) bool {
	for _, e := range j.Exits {
		if e.Label == "exit" {
			return true
		}
	}
	return false
}

// plainBool reports whether e has the type bool or an untyped bool, so it
// can be passed where a bool is wanted without a conversion.
func plainBool(info *types.Info, e ast.Expr) bool {
	tv, ok := info.Types[e]
	if !ok {
		return true
	}
	b, ok := types.Unalias(tv.Type).(*types.Basic)
	return ok && b.Info()&types.IsBoolean != 0 && (b.Kind() == types.Bool || b.Kind() == types.UntypedBool)
}

// labelsOf maps each labeled statement in body to its label.
func labelsOf(body *ast.BlockStmt) map[ast.Stmt]*ast.LabeledStmt {
	out := map[ast.Stmt]*ast.LabeledStmt{}
	ast.Inspect(body, func(n ast.Node) bool {
		if l, ok := n.(*ast.LabeledStmt); ok {
			out[l.Stmt] = l
		}
		return true
	})
	return out
}

// breaksLeaving lists the break statements in a loop's body that leave
// that loop: an unlabeled break not inside a nested for, range, switch
// or select (those take unlabeled breaks for themselves), or a break
// naming the loop's label.
func breaksLeaving(body *ast.BlockStmt, label string) []*ast.BranchStmt {
	var out []*ast.BranchStmt
	var visit func(n ast.Node, nested bool)
	visit = func(root ast.Node, nested bool) {
		ast.Inspect(root, func(n ast.Node) bool {
			if n == root {
				return true
			}
			switch n := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
				visit(n, true)
				return false
			case *ast.BranchStmt:
				if n.Tok == token.BREAK && ((n.Label == nil && !nested) || (n.Label != nil && n.Label.Name == label)) {
					out = append(out, n)
				}
			}
			return true
		})
	}
	visit(body, false)
	return out
}

// returnEdits marks a return so the run counts as complete only after all
// return values were evaluated. (Marking before the return would count a
// run whose return value panics as complete: a known trap.)
//   - bare "return": nothing left to evaluate, so mark just before it.
//   - "return a, b": becomes "return pathkitRet2[T0, T1](pathkitRec, a, b)";
//     the helper sets the flag after a and b are evaluated. Explicit type
//     arguments let untyped values like nil convert as before.
//   - "return f()" where f returns several values: becomes
//     "return func() (T0, T1) { r0, r1 := f(); mark; return r0, r1 }()".
func returnEdits(wf discover.Workflow, ret *ast.ReturnStmt, types []string, off, end func(ast.Node) int) ([]Edit, error) {
	switch {
	case len(ret.Results) == 0:
		return []Edit{{Offset: off(ret), Text: recVar + ".returned(); "}}, nil
	case len(ret.Results) == len(types):
		first, last := ret.Results[0], ret.Results[len(ret.Results)-1]
		return []Edit{
			{Offset: off(first), Text: fmt.Sprintf("pathkitRet%d[%s](%s, ", len(types), strings.Join(types, ", "), recVar)},
			{Offset: end(last), Order: 3, Text: ")"},
		}, nil
	case len(ret.Results) == 1:
		names := make([]string, len(types))
		for i := range names {
			names[i] = fmt.Sprintf("pathkitR%d", i)
		}
		list := strings.Join(names, ", ")
		return []Edit{
			{Offset: off(ret.Results[0]), Text: fmt.Sprintf("func() (%s) { %s := ", strings.Join(types, ", "), list)},
			{Offset: end(ret.Results[0]), Order: 3, Text: fmt.Sprintf("; %s.returned(); return %s }()", recVar, list)},
		}, nil
	}
	return nil, fmt.Errorf("cannot record %s: unexpected return at %s", wf.Name, wf.Pkg.Fset.Position(ret.Pos()))
}

// resultTypeTexts returns the source text of each result type, one entry
// per result ("(a, b int, err error)" gives int, int, error).
func resultTypeTexts(wf discover.Workflow) ([]string, error) {
	src, err := os.ReadFile(wf.Filename)
	if err != nil {
		return nil, err
	}
	fset := wf.Pkg.Fset
	var out []string
	for _, field := range wf.Func.Type.Results.List {
		text := string(src[fset.Position(field.Type.Pos()).Offset:fset.Position(field.Type.End()).Offset])
		text = strings.Join(strings.Fields(text), " ") // keep it on one line
		n := len(field.Names)
		if n == 0 {
			n = 1
		}
		for range n {
			out = append(out, text)
		}
	}
	if len(out) > maxResults {
		return nil, fmt.Errorf("cannot record %s: it has %d results; pathkit handles at most %d", wf.Name, len(out), maxResults)
	}
	return out, nil
}

func checkNames(wf discover.Workflow, g *model.Graph) error {
	scope := wf.Pkg.Types.Scope()
	for _, name := range scope.Names() {
		if slices.Contains(packageNames, name) || strings.HasPrefix(name, "pathkitRet") {
			return fmt.Errorf("cannot record %s: the package already declares %q, a name pathkit needs", wf.Name, name)
		}
	}
	// pathkitCtx is only added when the context parameter is written "_";
	// then any package-level name or local declaration of it would clash.
	if params := wf.Func.Type.Params.List; len(params) > 0 && len(params[0].Names) > 0 && params[0].Names[0].Name == "_" {
		if scope.Lookup(ctxVar) != nil {
			return fmt.Errorf("cannot record %s: the package already declares %q, a name pathkit needs for a context parameter written \"_\"; name that parameter in your code, or rename %s", wf.Name, ctxVar, ctxVar)
		}
	}
	clash := false
	ast.Inspect(wf.Func, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return !clash
		}
		obj := wf.Pkg.TypesInfo.Defs[id]
		if _, isVar := obj.(*types.Var); isVar && id.Name == recVar {
			clash = true
		}
		if obj != nil && id.Name == ctxVar {
			clash = true // any local variable, constant, type or label
		}
		return !clash
	})
	if clash {
		return fmt.Errorf("cannot record %s: it already uses the name %q or %q, which pathkit needs", wf.Name, recVar, ctxVar)
	}
	// The helper functions the graph follows into get the same two names.
	for _, s := range g.Helpers() {
		ast.Inspect(s.Body, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return !clash
			}
			if obj := wf.Pkg.TypesInfo.Defs[id]; obj != nil && (id.Name == frameVar || id.Name == ctxVar) {
				clash = true
			}
			return !clash
		})
		if clash {
			return fmt.Errorf("cannot record %s: the helper %s already uses the name %q or %q, which pathkit needs", wf.Name, s.Helper, frameVar, ctxVar)
		}
		if s.CtxName != nil && s.CtxName.Name == "_" && wf.Pkg.Types.Scope().Lookup(ctxVar) != nil {
			return fmt.Errorf("cannot record %s: the package already declares %q, a name pathkit needs for the context parameter of the helper %s, which is written \"_\"", wf.Name, ctxVar, s.Helper)
		}
	}
	return nil
}

// Apply inserts edits into src. Edits are applied from the end of the file
// backwards, so earlier offsets stay valid.
func Apply(src []byte, edits []Edit) []byte {
	sorted := append([]Edit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Offset != sorted[j].Offset {
			return sorted[i].Offset > sorted[j].Offset
		}
		return sorted[i].Order > sorted[j].Order
	})
	out := append([]byte(nil), src...)
	for _, e := range sorted {
		if strings.ContainsAny(e.Text, "\r\n") {
			panic("instrument: an edit must never add a line break")
		}
		out = append(out[:e.Offset], append([]byte(e.Text), out[e.Offset+e.Delete:]...)...)
	}
	return out
}

var recorderTemplate = template.Must(template.New("recorder").Parse(`// Code generated by pathkit test. DO NOT EDIT.
// This file exists only in pathkit's overlay (.pathkit/overlay); it is never
// written into your project, and your go.mod is not changed.

package {{.Package}}

import (
	pathkitrand "crypto/rand"
	pathkithex "encoding/hex"
	pathkitjson "encoding/json"
	pathkitos "os"
	pathkitfilepath "path/filepath"
	pathkitruntime "runtime"
	pathkitstrings "strings"
	pathkittime "time"

	pathkitworkflow "go.temporal.io/sdk/workflow"
)

const pathkitTraceDir = {{printf "%q" .TraceDir}}

// pathkitTrace is trace file format schemaVersion 2 (see docs/DESIGN.md).
type pathkitTrace struct {
	SchemaVersion int      ` + "`json:\"schemaVersion\"`" + `
	Tool          string   ` + "`json:\"tool\"`" + `
	Workflow      string   ` + "`json:\"workflow\"`" + `
	Package       string   ` + "`json:\"package,omitempty\"`" + `
	FunctionHash  string   ` + "`json:\"functionHash\"`" + `
	Status        string   ` + "`json:\"status\"`" + `
	Steps         []string ` + "`json:\"steps\"`" + `
	WorkflowID    string   ` + "`json:\"workflowId\"`" + `
	RunID         string   ` + "`json:\"runId\"`" + `
	Recorder      int      ` + "`json:\"recorder,omitempty\"`" + `
	RecordedAt    string   ` + "`json:\"recordedAt\"`" + `
	TestName      string   ` + "`json:\"testName,omitempty\"`" + `
}

// pathkitRecorder belongs to one call of one workflow function.
type pathkitRecorder struct {
	trace       pathkitTrace
	table       map[string]string // this workflow's lookup table, see pathkitTables
	done        bool
	fellThrough bool                    // a switch case just ended with fallthrough
	loops       map[string]*pathkitLoop // by junction ID, e.g. "J1"
}

// pathkitLoop is one loop's state in this run (the loop rule, docs/DESIGN.md D3).
type pathkitLoop struct {
	iterate, exit, retry string // the loop's exit IDs, from pathkit's model
	entered              bool   // the body started since the loop statement began
	broke                bool   // a break just left this range loop
}

// pathkitTables turn what marked-up helper code records into the exit IDs of
// each workflow's graph, and say which call leads to which copy of a helper
// (see model.Graph.RecorderTable).
var pathkitTables = map[string]map[string]string{
{{range .Tables}}	{{printf "%q" .Workflow}}: { {{.Entries}} },
{{end}}}

func pathkitStart(ctx pathkitworkflow.Context, workflow, hash, pkg string) *pathkitRecorder {
	r := &pathkitRecorder{table: pathkitTables[workflow], trace: pathkitTrace{SchemaVersion: 2, Recorder: 3, Tool: "pathkit-go", Workflow: workflow, Package: pkg, FunctionHash: hash, Steps: []string{}}}
	if info := pathkitworkflow.GetInfo(ctx); info != nil {
		r.trace.WorkflowID, r.trace.RunID = info.WorkflowExecution.ID, info.WorkflowExecution.RunID
	}
	return r
}

// hit records one junction exit, at the moment the decision is made.
func (r *pathkitRecorder) hit(id string) { r.trace.Steps = append(r.trace.Steps, id) }

// errExit records the decision of a "return <call>": the call's error is the
// failure exit when it is not nil, the success exit when it is. It hands the
// error back unchanged.
func (r *pathkitRecorder) errExit(failure, success string, err error) error {
	if err != nil {
		r.hit(failure)
	} else {
		r.hit(success)
	}
	return err
}

// fell and hitUnlessFell handle fallthrough: the case a switch falls into
// is not a new decision, so its exit is recorded only when the switch
// chose that case itself.
func (r *pathkitRecorder) fell() { r.fellThrough = true }

func (r *pathkitRecorder) hitUnlessFell(id string) {
	if r.fellThrough {
		r.fellThrough = false
		return
	}
	r.hit(id)
}

// enter runs just before a loop statement starts (again, when an outer
// loop goes round), with the loop's exit IDs.
func (r *pathkitRecorder) enter(key, iterate, exit, retry string) {
	if r.loops == nil {
		r.loops = map[string]*pathkitLoop{}
	}
	r.loops[key] = &pathkitLoop{iterate: iterate, exit: exit, retry: retry}
}

// loop records one decision at a loop's head: "retry" first if the body
// already ran (the loop came round again), then "iterate" or "exit". It
// returns more, so it can wrap the loop's condition.
func (r *pathkitRecorder) loop(key string, more bool) bool {
	l := r.loops[key]
	if l == nil {
		return more
	}
	if l.entered {
		r.hit(l.retry)
	}
	l.entered = more
	if more {
		r.hit(l.iterate)
	} else {
		r.hit(l.exit)
	}
	return more
}

// rangeDone runs after a range loop's "}". Reached by a break, it records
// nothing; reached because the range ran out, that is the loop's exit.
func (r *pathkitRecorder) rangeDone(key string) {
	l := r.loops[key]
	if l == nil {
		return
	}
	if l.broke {
		l.broke = false
		return
	}
	r.loop(key, false)
}

// broke runs just before a break that leaves a range loop.
func (r *pathkitRecorder) broke(key string) {
	if l := r.loops[key]; l != nil {
		l.broke = true
	}
}

// pathkitCtxKey is the context key a call site hands its identity to a
// helper under, and pathkitPending what it hands over.
type pathkitCtxKey struct{}

type pathkitPending struct {
	r    *pathkitRecorder
	site string
}

// pathkitFrame is one marked-up helper's handle: the recorder and the site
// the helper was entered from. A nil frame (the helper was entered from
// somewhere the graph does not follow) records nothing.
type pathkitFrame struct {
	r    *pathkitRecorder
	site string
}

// pathkitSource is where a call site is: the workflow function (the
// recorder) or a helper (its frame).
type pathkitSource interface{ pathkitSource() (*pathkitRecorder, string) }

func (r *pathkitRecorder) pathkitSource() (*pathkitRecorder, string) { return r, "" }

func (f *pathkitFrame) pathkitSource() (*pathkitRecorder, string) {
	if f == nil {
		return nil, ""
	}
	return f.r, f.site
}

// pathkitSite wraps the context argument of a call of a helper: when the
// graph follows this call, the helper finds the site in the context.
func pathkitSite(src pathkitSource, ctx pathkitworkflow.Context, call string) pathkitworkflow.Context {
	r, parent := src.pathkitSource()
	if r == nil {
		return ctx
	}
	site, ok := r.table[parent+"|"+call]
	if !ok {
		return ctx
	}
	return pathkitworkflow.WithValue(ctx, pathkitCtxKey{}, pathkitPending{r: r, site: site})
}

// pathkitEnter runs first thing in a marked-up helper. It takes the site out
// of the context (so a call further down that is not followed cannot take
// it again) and records that the helper was entered.
func pathkitEnter(ctx pathkitworkflow.Context, callee string) (*pathkitFrame, pathkitworkflow.Context) {
	p, _ := ctx.Value(pathkitCtxKey{}).(pathkitPending)
	if p.r == nil {
		return nil, ctx
	}
	ctx = pathkitworkflow.WithValue(ctx, pathkitCtxKey{}, pathkitPending{})
	if p.r.table[p.site+"|"] != callee {
		return nil, ctx
	}
	p.r.hit(">" + p.site)
	return &pathkitFrame{r: p.r, site: p.site}, ctx
}

func (f *pathkitFrame) exit() {
	if f != nil {
		f.r.hit("<" + f.site)
	}
}

// id turns a key the helper records into the exit ID of this copy of it.
func (f *pathkitFrame) id(local string) string { return f.r.table[f.site+"|"+local] }

func (f *pathkitFrame) hit(local string) {
	if f == nil {
		return
	}
	if id := f.id(local); id != "" {
		f.r.hit(id)
	}
}

func (f *pathkitFrame) hitUnlessFell(local string) {
	if f == nil {
		return
	}
	if id := f.id(local); id != "" {
		f.r.hitUnlessFell(id)
	}
}

func (f *pathkitFrame) fell() {
	if f != nil {
		f.r.fell()
	}
}

func (f *pathkitFrame) enter(key, iterate, exit, retry string) {
	if f == nil || f.id(iterate) == "" {
		return
	}
	f.r.enter(f.site+"|"+key, f.id(iterate), f.id(exit), f.id(retry))
}

func (f *pathkitFrame) loop(key string, more bool) bool {
	if f == nil {
		return more
	}
	return f.r.loop(f.site+"|"+key, more)
}

func (f *pathkitFrame) rangeDone(key string) {
	if f != nil {
		f.r.rangeDone(f.site + "|" + key)
	}
}

func (f *pathkitFrame) broke(key string) {
	if f != nil {
		f.r.broke(f.site + "|" + key)
	}
}

// returned marks that the run finished a return of the workflow function
// (all return values evaluated). pathkitRetN do the same for "return a, b".
func (r *pathkitRecorder) returned() { r.done = true }
{{range .Rets}}
func pathkitRet{{.N}}[{{.TypeParams}} any](r *pathkitRecorder, {{.Params}}) ({{.Types}}) {
	r.done = true
	return {{.Args}}
}
{{end}}
// pathkitTestName finds the Go test that runs this workflow: the Test function
// on the goroutine that waits in the Temporal test environment's
// ExecuteWorkflow (the workflow itself runs on another goroutine, so its own
// stack does not show the test). "" when there is none, or when several tests
// wait there at once (parallel tests): the stack cannot tell which one is ours.
func pathkitTestName() string {
	// only a "go test" binary has a test to name (and the dump of every goroutine
	// is not worth its cost anywhere else)
	if pathkitos.Getenv("PATHKIT_TESTNAMES") == "off" || len(pathkitos.Args) == 0 || !(pathkitstrings.HasSuffix(pathkitos.Args[0], ".test") || pathkitstrings.HasSuffix(pathkitos.Args[0], ".test.exe")) {
		return ""
	}
	buf := make([]byte, 1<<16)
	for {
		n := pathkitruntime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		if len(buf) >= 1<<19 { // a very long dump: not worth the time inside a workflow
			return ""
		}
		buf = make([]byte, 2*len(buf))
	}
	found := ""
	for _, g := range pathkitstrings.Split(string(buf), "\n\n") {
		if !pathkitstrings.Contains(g, "TestWorkflowEnvironment).ExecuteWorkflow(") {
			continue
		}
		name := ""
		for _, line := range pathkitstrings.Split(g, "\n") {
			if line == "" || line[0] == '\t' || pathkitstrings.HasPrefix(line, "goroutine ") || pathkitstrings.HasPrefix(line, "created by ") {
				continue
			}
			if i := pathkitstrings.LastIndex(line, "("); i > 0 {
				line = line[:i]
			}
			parts := pathkitstrings.Split(line, ".")
			for len(parts) > 1 && (pathkitstrings.HasPrefix(parts[len(parts)-1], "func") || pathkitstrings.HasPrefix(parts[len(parts)-1], "gowrap")) {
				parts = parts[:len(parts)-1]
			}
			base := parts[len(parts)-1]
			if pathkitstrings.HasPrefix(base, "Test") && !pathkitstrings.HasPrefix(line, "testing.") && !pathkitstrings.Contains(line, "go.temporal.io/sdk") {
				name = base
			}
		}
		if name == "" {
			continue
		}
		if found != "" && found != name {
			return ""
		}
		found = name
	}
	return found
}

// flush writes the trace file. It runs deferred, so it also runs after a
// panic or runtime.Goexit; those runs never called returned() and are
// saved as incomplete. There is no recover(): panics are left untouched.
func (r *pathkitRecorder) flush() {
	r.trace.Status = "incomplete"
	if r.done {
		r.trace.Status = "complete"
	}
	r.trace.RecordedAt = pathkittime.Now().UTC().Format(pathkittime.RFC3339)
	r.trace.TestName = pathkitTestName()
	data, err := pathkitjson.MarshalIndent(r.trace, "", "  ")
	if err != nil {
		return
	}
	var b [6]byte
	_, _ = pathkitrand.Read(b[:])
	_ = pathkitos.MkdirAll(pathkitTraceDir, 0o755)
	name := pathkitstrings.ReplaceAll(r.trace.Workflow, "/", "_") + "." + pathkithex.EncodeToString(b[:]) + ".trace.json"
	_ = pathkitos.WriteFile(pathkitfilepath.Join(pathkitTraceDir, name), append(data, '\n'), 0o644)
}
`))

type retHelper struct{ N, TypeParams, Params, Types, Args string }

// retHelpers describes pathkitRet1 ... pathkitRet8 for the recorder template.
func retHelpers() []retHelper {
	var out []retHelper
	for n := 1; n <= maxResults; n++ {
		var tps, params, args []string
		for i := range n {
			tps = append(tps, fmt.Sprintf("T%d", i))
			params = append(params, fmt.Sprintf("v%d T%d", i, i))
			args = append(args, fmt.Sprintf("v%d", i))
		}
		out = append(out, retHelper{
			N:          fmt.Sprint(n),
			TypeParams: strings.Join(tps, ", "),
			Params:     strings.Join(params, ", "),
			Types:      strings.Join(tps, ", "),
			Args:       strings.Join(args, ", "),
		})
	}
	return out
}
