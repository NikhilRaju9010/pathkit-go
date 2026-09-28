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
var packageNames = []string{"pathkitStart", "pathkitRecorder", "pathkitTrace", "pathkitTraceDir"}

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

	for _, j := range g.Junctions {
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
			case nil: // no else: add one
				edits = append(edits, Edit{Offset: end(j.Stmt.Body), Order: 1, Text: " else { " + hit + " }"})
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
	trace pathkitTrace
	done  bool
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
