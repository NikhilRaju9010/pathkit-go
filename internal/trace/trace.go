// Package trace reads, lists and clears trace files (format: CLAUDE.md,
// "Trace file format (schemaVersion 1)") and checks each one against the
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

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// SchemaVersion is the only trace format this version understands.
const SchemaVersion = 1

// Suffix ends every trace file name. Clearing deletes only such files.
const Suffix = ".trace.json"

// File is one trace file.
type File struct {
	SchemaVersion int      `json:"schemaVersion"`
	Tool          string   `json:"tool"`
	Workflow      string   `json:"workflow"`
	FunctionHash  string   `json:"functionHash"`
	Status        string   `json:"status"`
	Steps         []string `json:"steps"`
	WorkflowID    string   `json:"workflowId"`
	RunID         string   `json:"runId"`
	RecordedAt    string   `json:"recordedAt"`

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
	return fmt.Sprintf("no trace files found in %s; coverage is recorded only by \"pathkit test\" (plain \"go test\" records nothing)", dir)
}

// Kind is how a trace compares with the current code.
type Kind string

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
	Kind   Kind
	Path   model.Path // when Matched
	Reason string     // when not Matched
}

// Check compares a trace with the workflow's current graph. g is nil when
// the workflow isn't among the analyzed ones; hash is its current
// model.FunctionHash.
func Check(f File, g *model.Graph, hash string) Outcome {
	switch {
	case g == nil:
		return Outcome{Kind: UnknownWorkflow, Reason: "workflow not found in the analyzed packages (or not analyzable)"}
	case f.Status != "complete":
		return Outcome{Kind: Incomplete, Reason: "the run never reached a return (panic, timeout, or stopped)"}
	case f.FunctionHash != hash:
		return Outcome{Kind: Stale, Reason: "recorded for a different version of the workflow function; re-record it"}
	}
	p, mm := g.Match(f.Steps)
	if mm != nil {
		return Outcome{Kind: Unmatched, Reason: mm.Reason}
	}
	return Outcome{Kind: Matched, Path: p}
}
