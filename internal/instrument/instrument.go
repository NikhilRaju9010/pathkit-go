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
var packageNames = []string{"pathkitStart", "pathkitRecorder", "pathkitTrace", "pathkitTraceDir", "pathkitLoop"}

// maxResults is how many results a workflow function may have; the
// recorder declares one pathkitRetN helper for each N up to it.
const maxResults = 8

const recVar = "pathkitRec"

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
}

// Result holds the generated sources, keyed by the real file path they
// stand in for (an existing workflow file, or a new recorder file).
type Result struct {
	Files map[string][]byte
	Edits map[string][]Edit // per workflow file, for tests
}

// Instrument marks up every target's file and adds one recorder file per
// package. traceDir must be absolute; it is baked into the recorder.
func Instrument(targets []Target, traceDir string) (*Result, error) {
	res := &Result{Files: map[string][]byte{}, Edits: map[string][]Edit{}}
	byFile := map[string][]Target{}
	for _, t := range targets {
		byFile[t.Workflow.Filename] = append(byFile[t.Workflow.Filename], t)
	}

	packages := map[string]string{} // package dir -> package name
	for filename, ts := range byFile {
		var edits []Edit
		for _, t := range ts {
			e, err := workflowEdits(t)
			if err != nil {
				return nil, err
			}
			edits = append(edits, e...)
		}
		src, err := os.ReadFile(filename)
		if err != nil {
			return nil, err
		}
		res.Files[filename] = Apply(src, edits)
		res.Edits[filename] = edits
		packages[filepath.Dir(filename)] = ts[0].Workflow.Pkg.Name
	}

	for dir, name := range packages {
		path := filepath.Join(dir, RecorderFile)
		if _, err := os.Stat(path); err == nil {
			return nil, fmt.Errorf("cannot record %s: a file named %s already exists there", dir, RecorderFile)
		}
		var buf bytes.Buffer
		if err := recorderTemplate.Execute(&buf, map[string]any{"Package": name, "TraceDir": traceDir, "Rets": retHelpers()}); err != nil {
			return nil, err
		}
		res.Files[path] = buf.Bytes()
	}
	return res, nil
}

// workflowEdits lists the insertions for one workflow function.
func workflowEdits(t Target) ([]Edit, error) {
	wf, g := t.Workflow, t.Graph
	fset := wf.Pkg.Fset
	off := func(n ast.Node) int { return fset.Position(n.Pos()).Offset }
	end := func(n ast.Node) int { return fset.Position(n.End()).Offset }

	if err := checkNames(wf); err != nil {
		return nil, err
	}
	params := wf.Func.Type.Params.List
	if len(params[0].Names) == 0 || params[0].Names[0].Name == "_" {
		return nil, fmt.Errorf("cannot record %s: its workflow.Context parameter has no name", wf.Name)
	}
	ctxName := params[0].Names[0].Name

	hash := model.FunctionHash(fset, wf.Func)
	edits := []Edit{{
		Offset: off(wf.Func.Body) + 1, // just after the function's "{"
		Text:   fmt.Sprintf(" %s := pathkitStart(%s, %q, %q); defer %s.flush();", recVar, ctxName, wf.Name, hash, recVar),
	}}

	labels := labelsOf(wf.Func.Body)
	for _, j := range g.Junctions {
		if j.Kind == model.Loop {
			e, err := loopEdits(g, j, labels, off, end)
			if err != nil {
				return nil, fmt.Errorf("%v in %s", err, wf.Name)
			}
			edits = append(edits, e...)
			continue
		}
		var fellInto map[*ast.CaseClause]bool
		if body := switchBody(j.Stmt); body != nil {
			var e []Edit
			e, fellInto = fallthroughEdits(body, off)
			edits = append(edits, e...)
		}
		for _, e := range j.Exits {
			id, ok := g.ExitFor(j.Stmt, e.Label)
			if !ok {
				return nil, fmt.Errorf("internal error: no ID for %s exit %q in %s", j.ID, e.Label, wf.Name)
			}
			hit := fmt.Sprintf("%s.hit(%q)", recVar, id.String())
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
					hit = fmt.Sprintf("%s.hitUnlessFell(%q)", recVar, id.String())
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
				return nil, fmt.Errorf("internal error: unexpected road %T in %s", road, wf.Name)
			}
		}
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
	return edits, retErr
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
func fallthroughEdits(body *ast.BlockStmt, off func(ast.Node) int) ([]Edit, map[*ast.CaseClause]bool) {
	var edits []Edit
	fellInto := map[*ast.CaseClause]bool{}
	for i, c := range body.List {
		stmts := c.(*ast.CaseClause).Body
		if len(stmts) == 0 || i+1 == len(body.List) {
			continue
		}
		if br, ok := stmts[len(stmts)-1].(*ast.BranchStmt); ok && br.Tok == token.FALLTHROUGH {
			edits = append(edits, Edit{Offset: off(br), Text: recVar + ".fell(); "})
			fellInto[body.List[i+1].(*ast.CaseClause)] = true
		}
	}
	return edits, fellInto
}

// loopEdits records a loop junction (the loop rule, CLAUDE.md D3). Just
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
func loopEdits(g *model.Graph, j *model.Junction, labels map[ast.Stmt]*ast.LabeledStmt, off, end func(ast.Node) int) ([]Edit, error) {
	id := func(label string) (string, error) {
		e, ok := g.ExitFor(j.Stmt, label)
		if !ok {
			return "", fmt.Errorf("internal error: no ID for %s exit %q", j.ID, label)
		}
		return e.String(), nil
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
	if e, ok := g.ExitFor(j.Stmt, "exit"); ok {
		exit = e.String()
	}

	stmt := j.Stmt.(ast.Stmt)
	start := ast.Node(stmt)
	if l := labels[stmt]; l != nil {
		start = l
	}
	key := j.ID
	edits := []Edit{{Offset: off(start), Text: fmt.Sprintf("%s.enter(%q, %q, %q, %q); ", recVar, key, iterate, exit, retry)}}
	atBodyTop := func(body *ast.BlockStmt) Edit {
		return Edit{Offset: off(body) + 1, Text: fmt.Sprintf(" %s.loop(%q, true);", recVar, key)}
	}

	switch s := stmt.(type) {
	case *ast.ForStmt:
		if s.Cond == nil {
			return append(edits, atBodyTop(s.Body)), nil
		}
		return append(edits,
			Edit{Offset: off(s.Cond), Text: fmt.Sprintf("%s.loop(%q, ", recVar, key)},
			Edit{Offset: end(s.Cond), Order: 3, Text: ")"}), nil
	case *ast.RangeStmt:
		edits = append(edits, atBodyTop(s.Body), Edit{Offset: end(s), Text: fmt.Sprintf("; %s.rangeDone(%q)", recVar, key)})
		name := ""
		if l := labels[stmt]; l != nil {
			name = l.Label.Name
		}
		for _, br := range breaksLeaving(s.Body, name) {
			edits = append(edits, Edit{Offset: off(br), Text: fmt.Sprintf("%s.broke(%q); ", recVar, key)})
		}
		return edits, nil
	}
	return nil, fmt.Errorf("internal error: %s is a loop junction but not a loop", j.ID)
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
// run whose return value panics as complete: the TS d0aa17a trap.)
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

func checkNames(wf discover.Workflow) error {
	scope := wf.Pkg.Types.Scope()
	for _, name := range scope.Names() {
		if slices.Contains(packageNames, name) || strings.HasPrefix(name, "pathkitRet") {
			return fmt.Errorf("cannot record %s: the package already declares %q, a name pathkit needs", wf.Name, name)
		}
	}
	clash := false
	ast.Inspect(wf.Func, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == recVar {
			if _, isVar := wf.Pkg.TypesInfo.Defs[id].(*types.Var); isVar {
				clash = true
			}
		}
		return !clash
	})
	if clash {
		return fmt.Errorf("cannot record %s: it already uses the name %q, which pathkit needs", wf.Name, recVar)
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
		out = append(out[:e.Offset], append([]byte(e.Text), out[e.Offset:]...)...)
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
	pathkittime "time"

	pathkitworkflow "go.temporal.io/sdk/workflow"
)

const pathkitTraceDir = {{printf "%q" .TraceDir}}

// pathkitTrace is trace file format schemaVersion 1 (see CLAUDE.md).
type pathkitTrace struct {
	SchemaVersion int      ` + "`json:\"schemaVersion\"`" + `
	Tool          string   ` + "`json:\"tool\"`" + `
	Workflow      string   ` + "`json:\"workflow\"`" + `
	FunctionHash  string   ` + "`json:\"functionHash\"`" + `
	Status        string   ` + "`json:\"status\"`" + `
	Steps         []string ` + "`json:\"steps\"`" + `
	WorkflowID    string   ` + "`json:\"workflowId\"`" + `
	RunID         string   ` + "`json:\"runId\"`" + `
	RecordedAt    string   ` + "`json:\"recordedAt\"`" + `
}

// pathkitRecorder belongs to one call of one workflow function.
type pathkitRecorder struct {
	trace       pathkitTrace
	done        bool
	fellThrough bool                    // a switch case just ended with fallthrough
	loops       map[string]*pathkitLoop // by junction ID, e.g. "J1"
}

// pathkitLoop is one loop's state in this run (the loop rule, CLAUDE.md D3).
type pathkitLoop struct {
	iterate, exit, retry string // the loop's exit IDs, from pathkit's model
	entered              bool   // the body started since the loop statement began
	broke                bool   // a break just left this range loop
}

func pathkitStart(ctx pathkitworkflow.Context, workflow, hash string) *pathkitRecorder {
	r := &pathkitRecorder{trace: pathkitTrace{SchemaVersion: 1, Tool: "pathkit-go", Workflow: workflow, FunctionHash: hash, Steps: []string{}}}
	if info := pathkitworkflow.GetInfo(ctx); info != nil {
		r.trace.WorkflowID, r.trace.RunID = info.WorkflowExecution.ID, info.WorkflowExecution.RunID
	}
	return r
}

// hit records one junction exit, at the moment the decision is made.
func (r *pathkitRecorder) hit(id string) { r.trace.Steps = append(r.trace.Steps, id) }

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
	if l.broke {
		l.broke = false
		return
	}
	r.loop(key, false)
}

// broke runs just before a break that leaves a range loop.
func (r *pathkitRecorder) broke(key string) { r.loops[key].broke = true }

// returned marks that the run finished a return of the workflow function
// (all return values evaluated). pathkitRetN do the same for "return a, b".
func (r *pathkitRecorder) returned() { r.done = true }
{{range .Rets}}
func pathkitRet{{.N}}[{{.TypeParams}} any](r *pathkitRecorder, {{.Params}}) ({{.Types}}) {
	r.done = true
	return {{.Args}}
}
{{end}}
// flush writes the trace file. It runs deferred, so it also runs after a
// panic or runtime.Goexit; those runs never called returned() and are
// saved as incomplete. There is no recover(): panics are left untouched.
func (r *pathkitRecorder) flush() {
	r.trace.Status = "incomplete"
	if r.done {
		r.trace.Status = "complete"
	}
	r.trace.RecordedAt = pathkittime.Now().UTC().Format(pathkittime.RFC3339)
	data, err := pathkitjson.MarshalIndent(r.trace, "", "  ")
	if err != nil {
		return
	}
	var b [6]byte
	_, _ = pathkitrand.Read(b[:])
	_ = pathkitos.MkdirAll(pathkitTraceDir, 0o755)
	name := r.trace.Workflow + "." + pathkithex.EncodeToString(b[:]) + ".trace.json"
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
