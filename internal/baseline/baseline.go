// Package baseline is the CI baseline file (path classification):
// a record, committed to the project, of every listed path's stable id and
// bucket at one moment. A later run compares itself with it to find what
// is NEW: a testable path nobody tests yet, or a path PathKit is unsure
// about (needs-review). Paths that were already there are known, and do
// not fail the build again.
//
// It only reads, writes and compares; the CLI decides what to print and
// which exit code to use.
package baseline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
)

// SchemaVersion is the only format this PathKit reads and writes.
const SchemaVersion = 1

// Note is written at the top of every baseline file, for whoever opens it.
const Note = `Written by "pathkit report --write-baseline" (or coverage). Commit this file. A later run with classification enabled fails (exit 2) when a testable path that is not listed here as testable has no test, or a needs-review path appears that is not listed here as needs-review. A path a test ran when the file was written that no test runs now also fails it (unless --allow-lost-tests). Write it again after you have looked at such paths.`

// File is one baseline file.
type File struct {
	SchemaVersion int        `json:"schemaVersion"`
	Tool          string     `json:"tool"`
	Note          string     `json:"note"`
	Workflows     []Workflow `json:"workflows"`
	// RuleAnchors (optional; older files have none): where each numbered
	// rule entry ("if more #2 --false") pointed when the file was written,
	// so a later run can warn when "#2" may point at another decision.
	RuleAnchors []RuleAnchor `json:"ruleAnchors,omitempty"`
}

// RuleAnchor is one numbered rule entry and the decisions the map said
// come right before the decision it named.
type RuleAnchor struct {
	Rule     string   `json:"rule"`
	Workflow string   `json:"workflow"`
	Entry    string   `json:"entry"`
	Before   []string `json:"before"`
	After    []string `json:"after"`
	// Count is how many decisions had this text (0 in files written before
	// it was recorded: then it is not compared).
	Count int `json:"count,omitempty"`
}

// AnchorChanges compares the anchors of a run with the file's. Only
// entries in both are compared; an entry that is new, gone or no longer
// resolves has its own warnings elsewhere.
func AnchorChanges(was, now []RuleAnchor) []string {
	key := func(a RuleAnchor) string { return a.Rule + "\x00" + a.Workflow + "\x00" + a.Entry }
	old := map[string]RuleAnchor{}
	for _, a := range was {
		old[key(a)] = a
	}
	var out []string
	for _, a := range now {
		b, ok := old[key(a)]
		if !ok {
			continue
		}
		var moved []string
		if b.Count > 0 && a.Count != b.Count {
			moved = append(moved, fmt.Sprintf("the workflow had %d decisions with this text, now %d", b.Count, a.Count))
		}
		if strings.Join(a.Before, "\x00") != strings.Join(b.Before, "\x00") {
			moved = append(moved, fmt.Sprintf("it came right after %s, now after %s", list(b.Before), list(a.Before)))
		}
		if strings.Join(a.After, "\x00") != strings.Join(b.After, "\x00") {
			moved = append(moved, fmt.Sprintf("its exits led to %s, now to %s", list(b.After), list(a.After)))
		}
		if len(moved) == 0 {
			continue
		}
		out = append(out, fmt.Sprintf("rule %q (%s) entry %q: since the baseline was written, the decision it names changed place (%s). An identical decision may have been added, removed or moved, so the number may point at a different decision now; check the rule, then write the baseline again",
			a.Rule, a.Workflow, a.Entry, strings.Join(moved, "; ")))
	}
	return out
}

func list(xs []string) string {
	if len(xs) == 0 {
		return "nothing"
	}
	return "\"" + strings.Join(xs, "\", \"") + "\""
}

// Workflow is one workflow's paths, sorted by stable id.
type Workflow struct {
	Name string `json:"name"`
	// Truncated: the workflow had more paths than PathKit lists, so paths
	// beyond the cap are not in the file.
	Truncated bool   `json:"truncated,omitempty"`
	Paths     []Path `json:"paths"`
	// Main is the workflow's main number when the file was written (older
	// files have none; then it is not compared).
	Main *Main `json:"mainNumber,omitempty"`
}

// Main is a workflow's main number: its kind ("path", "scenario", "branch" or
// "handler") and the counts behind it.
type Main struct {
	Kind    string  `json:"kind"`
	Covered int     `json:"covered"`
	Total   int     `json:"total"`
	Percent float64 `json:"percent"`
}

// Path is one listed path.
type Path struct {
	StableID string          `json:"stableId"`
	Bucket   classify.Bucket `json:"bucket"`
	Covered  bool            `json:"covered"`
}

// From records a coverage result: every listed path of every measured
// workflow, sorted, so the same code and tests always give the same file.
func From(res coverage.Result) File {
	f := File{SchemaVersion: SchemaVersion, Tool: "pathkit-go", Note: Note, Workflows: []Workflow{}}
	for _, w := range res.Workflows {
		bw := Workflow{Name: w.Name, Truncated: w.Truncated, Paths: []Path{}}
		if w.Main.Measured {
			bw.Main = &Main{Kind: w.Main.Kind, Covered: w.Main.Covered, Total: w.Main.Total, Percent: w.Main.Percent}
		}
		for _, p := range w.Paths {
			bw.Paths = append(bw.Paths, Path{StableID: p.StableID, Bucket: p.Class.Bucket, Covered: p.Covered})
		}
		sort.Slice(bw.Paths, func(i, j int) bool { return bw.Paths[i].StableID < bw.Paths[j].StableID })
		f.Workflows = append(f.Workflows, bw)
	}
	sort.Slice(f.Workflows, func(i, j int) bool { return f.Workflows[i].Name < f.Workflows[j].Name })
	return f
}

// Write saves f as indented JSON.
func Write(path string, f File) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keep "-->" readable for people reviewing the file
	enc.SetIndent("", "  ")
	if err := enc.Encode(f); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// Read loads a baseline file. Unknown keys, another schema version and a
// file that is not a baseline are errors: a CI gate must never guess.
func Read(path string) (File, error) {
	var f File
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return f, fmt.Errorf("baseline file %s not found; write it with --write-baseline", path)
		}
		return f, fmt.Errorf("could not read baseline file %s: %v", path, err)
	}
	// Some Windows editors save UTF-8 with a three-byte "byte order mark" in
	// front: skip it. A UTF-16 file (what PowerShell 5's ">" writes) cannot
	// be read, and the message says how to fix it.
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	if bytes.HasPrefix(raw, []byte{0xff, 0xfe}) || bytes.HasPrefix(raw, []byte{0xfe, 0xff}) {
		return f, fmt.Errorf("baseline file %s is saved as UTF-16; save it again as UTF-8 (in an editor, choose the \"UTF-8\" encoding), or write it again with --write-baseline", path)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&f); err != nil {
		return f, fmt.Errorf("baseline file %s is not a PathKit baseline (%v); write it again with --write-baseline", path, err)
	}
	if f.Tool != "pathkit-go" {
		return f, fmt.Errorf("baseline file %s is not a PathKit baseline (\"tool\" is %q, expected \"pathkit-go\")", path, f.Tool)
	}
	if f.SchemaVersion != SchemaVersion {
		return f, fmt.Errorf("baseline file %s has schemaVersion %d, but this PathKit reads only %d; write it again with --write-baseline", path, f.SchemaVersion, SchemaVersion)
	}
	return f, nil
}

// Finding is one path the CI check stops on.
type Finding struct {
	Workflow string
	Number   int // as analyze numbers it
	StableID string
	Text     string // the path as analyze prints it, filled in by the caller
	// WasBucket is the path's bucket in the baseline; "" when the path (or
	// its whole workflow) is not in the baseline.
	WasBucket classify.Bucket
	// NowBucket is the path's bucket in this run.
	NowBucket classify.Bucket
}

// Check compares a run with the baseline. It returns:
//   - untested: testable paths with no test that were not testable in the
//     baseline (new paths, or paths that became testable);
//   - review: needs-review paths that were not needs-review in the
//     baseline;
//   - lost: paths a test ran when the baseline was written that no test
//     runs now (whatever their bucket now). A path that no longer exists
//     is not "lost": its stable id is gone, and it is not listed.
//
// Only the measured workflows are compared; a workflow missing from the
// baseline counts as new, so all its paths are new.
func Check(b File, res coverage.Result) (untested, review, lost []Finding) {
	type was struct {
		bucket  classify.Bucket
		covered bool
	}
	before := map[string]was{}
	for _, w := range b.Workflows {
		for _, p := range w.Paths {
			before[w.Name+"|"+p.StableID] = was{p.Bucket, p.Covered}
		}
	}
	for _, w := range res.Workflows {
		for _, p := range w.Paths {
			old, known := before[w.Name+"|"+p.StableID]
			f := Finding{Workflow: w.Name, Number: p.Number, StableID: p.StableID, WasBucket: old.bucket, NowBucket: p.Class.Bucket}
			if known && old.covered && !p.Covered {
				lost = append(lost, f)
			}
			switch {
			case p.Class.Bucket == classify.Testable && !p.Covered && old.bucket != classify.Testable:
				untested = append(untested, f)
			case p.Class.Bucket == classify.NeedsReview && old.bucket != classify.NeedsReview:
				review = append(review, f)
			}
		}
	}
	return untested, review, lost
}

// MainDrop is a workflow whose main number is lower than the baseline's by more
// than the tolerance, or that has a main number in the baseline and none now.
type MainDrop struct {
	Workflow string
	Kind     string
	Was, Now Main
	// NotMeasured: the workflow has nothing to measure now.
	NotMeasured bool
}

// CheckMain compares each workflow's main number with the baseline's. Only
// workflows that have one in the baseline are compared, and only when the
// kind is the same (a changed kind is returned in changed, for a warning: the
// two numbers measure different things). tolerance is in percent points.
func CheckMain(b File, res coverage.Result, tolerance float64) (drops []MainDrop, changed []string) {
	was := map[string]Main{}
	for _, w := range b.Workflows {
		if w.Main != nil {
			was[w.Name] = *w.Main
		}
	}
	for _, w := range res.Workflows {
		old, ok := was[w.Name]
		if !ok {
			continue
		}
		if !w.Main.Measured {
			drops = append(drops, MainDrop{Workflow: w.Name, Kind: old.Kind, Was: old, NotMeasured: true})
			continue
		}
		if w.Main.Kind != old.Kind {
			changed = append(changed, fmt.Sprintf("%s: the main number was %s coverage in the baseline and is %s coverage now, so the two are not compared", w.Name, old.Kind, w.Main.Kind))
			continue
		}
		if w.Main.Percent < old.Percent-tolerance {
			drops = append(drops, MainDrop{Workflow: w.Name, Kind: old.Kind, Was: old, Now: Main{Kind: w.Main.Kind, Covered: w.Main.Covered, Total: w.Main.Total, Percent: w.Main.Percent}})
		}
	}
	return drops, changed
}
