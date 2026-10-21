// Package trace reads, lists and clears trace files (format: docs/DESIGN.md,
// "Trace file format (schemaVersion 2)") and checks each one against the
// model's graph.
package trace

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// SchemaVersion is the only trace format this version understands. Version 2
// adds the markers a run records when it goes into and out of the copy of a
// helper's code (docs/DESIGN.md D15).
const SchemaVersion = 2

// OlderFormatError means a trace was recorded by an older PathKit: its steps
// cannot be matched against this version's map, so it is not read.
type OlderFormatError struct{ Version int }

func (e *OlderFormatError) Error() string {
	return fmt.Sprintf("traces are from an older PathKit (trace format %d, this PathKit reads %d): re-run \"pathkit test\" to record them again", e.Version, SchemaVersion)
}

// Suffix ends every trace file name. Clearing deletes only such files.
const Suffix = ".trace.json"

// File is one trace file.
type File struct {
	SchemaVersion int    `json:"schemaVersion"`
	Tool          string `json:"tool"`
	Workflow      string `json:"workflow"`
	// Package is the workflow's import path. Optional: traces recorded by
	// older versions do not have it and still load. It tells apart two
	// workflows with the same package and function name in different
	// folders.
	Package      string   `json:"package,omitempty"`
	FunctionHash string   `json:"functionHash"`
	Status       string   `json:"status"`
	Steps        []string `json:"steps"`
	WorkflowID   string   `json:"workflowId"`
	RunID        string   `json:"runId"`
	// Recorder says which recorder wrote the trace: 0 for the ones before
	// handlers, summarized helpers and direct returns were recorded (the field
	// is absent there), 3 for the ones before condition parts were recorded,
	// 4 for this one. A step kind the recorder did not know
	// is "not recorded by this trace", never a missing or an unmatched step.
	Recorder   int    `json:"recorder,omitempty"`
	RecordedAt string `json:"recordedAt"`
	// TestName is the Go test that ran the workflow ("" in traces recorded
	// before it was recorded, and when the recorder could not tell which of
	// several parallel tests it was).
	TestName string `json:"testName,omitempty"`
	// Conditions are the parts of && / || conditions the run evaluated, each as
	// "c:<id>:t" (seen true) or "c:<id>:f" (seen false). Recorder 4 writes it;
	// an older trace has none, and its parts are "not recorded", never missed.
	Conditions []string `json:"conditions,omitempty"`

	Path string `json:"-"` // where it was read from
}

// List returns the trace files in dir, sorted. A missing dir is not an
// error: it just has no traces.
func List(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not read traces directory %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), Suffix) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

// Read reads and checks one trace file.
func Read(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return File{}, fmt.Errorf("not a valid trace file: %v", err)
	}
	if f.SchemaVersion >= 1 && f.SchemaVersion < SchemaVersion {
		return File{}, &OlderFormatError{Version: f.SchemaVersion}
	}
	if f.SchemaVersion != SchemaVersion {
		return File{}, fmt.Errorf("unsupported trace schemaVersion %d (this pathkit reads %d); re-record it", f.SchemaVersion, SchemaVersion)
	}
	f.Path = path
	return f, nil
}

// Clear deletes the trace files in dir and nothing else, and returns how
// many it deleted.
func Clear(dir string) (int, error) {
	files, err := List(dir)
	if err != nil {
		return 0, err
	}
	for _, f := range files {
		if err := os.Remove(f); err != nil {
			return 0, err
		}
	}
	return len(files), nil
}

// NoTracesMessage is the message shown when a trace folder has no traces.
func NoTracesMessage(dir string) string {
	return fmt.Sprintf("no trace files found in %s; coverage is recorded only by \"pathkit test\" (plain \"go test\" records nothing). Run \"pathkit test ./...\" first, from the folder that holds go.mod, then run this command from the same folder (or point to the traces with --traces)", dir)
}

// Kind is how a trace compares with the current code.
type Kind string

// The ways a trace can compare with the current code.
const (
	Matched         Kind = "matched"
	Unmatched       Kind = "unmatched"
	Stale           Kind = "stale"
	Incomplete      Kind = "incomplete"
	UnknownWorkflow Kind = "unknown workflow"
	// Excluded: the trace's workflow is out of scope (.pathkitrc.json,
	// --include, --exclude). Such traces are listed, never counted.
	Excluded Kind = "excluded"
)

// Outcome is the result of checking one trace.
type Outcome struct {
	Kind Kind
	Path model.Path // when Matched
	// Steps are the steps as read against the current map (a trace recorded
	// before helpers were summarized is converted first); when Matched.
	Steps  []string
	Reason string // when not Matched
	// Mismatch is the matcher's own account when Unmatched: the steps
	// that fitted and where the walk stood (the unmatched-trace report).
	Mismatch *model.Mismatch
}

// Check compares a trace with the workflow's current graph. g is nil when
// the workflow isn't among the analyzed ones; hash is its current
// model.FunctionHash.
func Check(f File, g *model.Graph, hash string) Outcome { return CheckIn(f, g, hash, "") }

// CheckIn is Check that also compares the import path of the package the
// workflow is in (pkg; "" when unknown). A trace that names a package
// different from pkg was recorded for another workflow with the same name,
// and is reported as such, never matched. A trace with no package field
// (an older file) is checked as before.
func CheckIn(f File, g *model.Graph, hash, pkg string) Outcome {
	switch {
	case g != nil && f.Package != "" && pkg != "" && f.Package != pkg:
		return Outcome{Kind: UnknownWorkflow, Reason: fmt.Sprintf("recorded for the workflow of that name in package %s, but the workflow here is in package %s", f.Package, pkg)}
	case g == nil:
		return Outcome{Kind: UnknownWorkflow, Reason: "workflow not found in the analyzed packages (or not analyzable)"}
	case f.Status != "complete":
		return Outcome{Kind: Incomplete, Reason: "the run never reached a return (panic, timeout, or stopped)"}
	case f.FunctionHash != hash:
		// A trace recorded before helpers were summarized fits the map that
		// copies them in; convert it, when the code has not changed since.
		if old := g.InlineHash(); old != "" && f.FunctionHash == old {
			if steps, ok := g.FromInline(f.Steps); ok {
				f.Steps = steps
				break
			}
		}
		return Outcome{Kind: Stale, Reason: "recorded for a different version of the workflow function; re-record it"}
	}
	p, mm := g.Match(f.Steps)
	if mm != nil {
		return Outcome{Kind: Unmatched, Reason: mm.Reason, Mismatch: mm}
	}
	return Outcome{Kind: Matched, Path: p, Steps: f.Steps}
}

// ClearOlderThan deletes the trace files in dir last written before
// cutoff (only *.trace.json files), and returns how many it deleted.
func ClearOlderThan(dir string, cutoff time.Time) (int, error) {
	files, err := List(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(f); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
