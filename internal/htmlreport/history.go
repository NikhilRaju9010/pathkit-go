package htmlreport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// HistoryFile is the trend's file name. It lives next to the HTML file,
// so each HTML file keeps its own trend. Only report --html writes it;
// pathkit test, pathkit clean and --clean never delete it (they delete
// *.trace.json files and pathkit's own overlay copies only).
const HistoryFile = "report-history.json"

// MaxRuns is how many report runs the trend keeps.
const MaxRuns = 5

// Run is one report --html run in the trend.
type Run struct {
	Time     time.Time `json:"time"`
	Paths    int       `json:"paths"`
	Covered  int       `json:"covered"`
	InScope  int       `json:"inScope"`
	Excluded int       `json:"excluded"`
	// Scope fingerprints which workflows were in scope and which were
	// excluded, so a jump in % caused by a scope change is marked.
	Scope string `json:"scope"`
}

type historyFile struct {
	SchemaVersion int    `json:"schemaVersion"`
	Tool          string `json:"tool"`
	Runs          []Run  `json:"runs"`
}

// HistoryPath is the history file for an HTML file.
func HistoryPath(htmlPath string) string {
	return filepath.Join(filepath.Dir(htmlPath), HistoryFile)
}

// ReadHistory reads the trend. A missing or damaged file never fails the
// report: it returns no runs and a warning, and the trend starts fresh.
func ReadHistory(path string) (runs []Run, warning string) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Sprintf("no report history at %s yet; starting a new trend", path)
	}
	if err != nil {
		return nil, fmt.Sprintf("could not read report history %s: %v; starting a new trend", path, err)
	}
	var h historyFile
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&h); err != nil {
		return nil, fmt.Sprintf("report history %s is damaged (%v); starting a new trend", path, err)
	}
	if h.SchemaVersion != 1 || h.Tool != "pathkit-go" {
		return nil, fmt.Sprintf("report history %s is not a pathkit-go history file (schemaVersion %d, tool %q); starting a new trend", path, h.SchemaVersion, h.Tool)
	}
	for i, r := range h.Runs {
		if r.Paths < 0 || r.Covered < 0 || r.Covered > r.Paths {
			return nil, fmt.Sprintf("report history %s is damaged (run %d has %d of %d paths covered); starting a new trend", path, i+1, r.Covered, r.Paths)
		}
	}
	return h.Runs, ""
}

// AddRun appends r and keeps the last MaxRuns runs.
func AddRun(runs []Run, r Run) []Run {
	runs = append(slices.Clone(runs), r)
	if len(runs) > MaxRuns {
		runs = runs[len(runs)-MaxRuns:]
	}
	return runs
}

// WriteHistory writes the trend.
func WriteHistory(path string, runs []Run) error {
	data, err := json.MarshalIndent(historyFile{SchemaVersion: 1, Tool: "pathkit-go", Runs: runs}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Fingerprint identifies a scope: which workflows were in scope and
// which were excluded (order doesn't matter).
func Fingerprint(inScope, excluded []string) string {
	a, b := slices.Clone(inScope), slices.Clone(excluded)
	slices.Sort(a)
	slices.Sort(b)
	sum := sha256.Sum256([]byte(strings.Join(a, "\n") + "\n--\n" + strings.Join(b, "\n")))
	return hex.EncodeToString(sum[:])[:12]
}

func scopeCounts(r Run) string {
	return fmt.Sprintf("%d in scope, %d excluded", r.InScope, r.Excluded)
}
