// Package constructs checks PathKit against testdata/constructs: one small made-up
// workflow for every Temporal code shape PathKit is meant to read, each with an
// expected.json that says what PathKit must make of it (its paths and their
// buckets, and what it must list as not read). docs/CONSTRUCTS.md has the same
// list in words; a test here keeps the two in step.
//
// expected.json is written by hand and checked by eye. PATHKIT_UPDATE_CONSTRUCTS=1
// writes what PathKit makes of a construct that has no expected.json yet, to
// start from; it never overwrites a file, so it cannot make a failing test pass.
package constructs

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/classify"
	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
	"github.com/NikhilRaju9010/pathkit-go/internal/render"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

const root = "../../testdata/constructs"

type expectedPath struct {
	Text   string `json:"text"`
	Bucket string `json:"bucket"`
}

type expectedWorkflow struct {
	Name  string         `json:"name"`
	Paths []expectedPath `json:"paths"`
}

// expected is the shape of expected.json.
type expected struct {
	// Construct is the name of the shape, Status says whether PathKit reads it
	// ("handled") or lists it as not read ("reported"), Note says in plain words
	// what PathKit does with it.
	Construct string `json:"construct"`
	Status    string `json:"status"`
	Note      string `json:"note"`
	// Config is the part of .pathkitrc.json the construct needs.
	Config struct {
		ActivityWrappers []string `json:"activityWrappers"`
		DirectReturns    string   `json:"directReturns"`
	} `json:"config"`
	Workflows []expectedWorkflow `json:"workflows"`
	// Unread lists the kinds of "could not fully read" entry the construct gives.
	Unread []string `json:"unread"`
}

func constructDirs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	return out
}

// analyse reads one construct the way the commands do.
func analyse(t *testing.T, dir string, exp expected) (workflows []expectedWorkflow, unread []string) {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join(root, dir))
	if err != nil {
		t.Fatal(err)
	}
	res, err := load.Load(abs)
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	opts := model.Options{DirectReturns: exp.Config.DirectReturns == "step"}
	for _, w := range exp.Config.ActivityWrappers {
		opts.Wrappers = append(opts.Wrappers, model.Wrapper{Function: w, Kind: "activity"})
	}
	eng, err := classify.NewEngine(scope.Classification{Enabled: true, StageMaxPaths: 256, MaxContexts: 16})
	if err != nil {
		t.Fatal(err)
	}
	found := discover.Find(res.Packages)
	known := map[*ast.FuncDecl]string{}
	for _, wf := range found {
		known[wf.Func] = wf.Name
	}
	add := func(wf discover.Workflow) {
		g, err := model.BuildWith(wf, opts)
		if err != nil {
			t.Fatalf("%s: %s: %v", dir, wf.Name, err)
		}
		if err := eng.AddGraph(wf.Name, g); err != nil {
			t.Fatal(err)
		}
		ew := expectedWorkflow{Name: wf.Name}
		for _, p := range g.Paths(model.DefaultMaxPaths).List {
			ew.Paths = append(ew.Paths, expectedPath{Text: render.PathText(p), Bucket: string(eng.Classify(wf.Name, p).Bucket)})
		}
		workflows = append(workflows, ew)
		for _, u := range g.Unread() {
			unread = append(unread, u.Kind)
		}
	}
	for _, wf := range found {
		add(wf)
		for _, h := range model.FindHandlers(wf, known) {
			if h.Workflow != nil {
				add(*h.Workflow)
			}
		}
	}
	sort.Strings(unread)
	return workflows, unread
}

func TestConstructs(t *testing.T) {
	dirs := constructDirs(t)
	if len(dirs) < 20 {
		t.Fatalf("only %d construct folders", len(dirs))
	}
	for _, dir := range dirs {
		t.Run(dir, func(t *testing.T) {
			path := filepath.Join(root, dir, "expected.json")
			data, err := os.ReadFile(path)
			var exp expected
			if err == nil {
				d := json.NewDecoder(bytes.NewReader(data))
				d.DisallowUnknownFields()
				if err := d.Decode(&exp); err != nil {
					t.Fatalf("%s: %v", path, err)
				}
			} else if os.Getenv("PATHKIT_UPDATE_CONSTRUCTS") == "" {
				t.Fatalf("%s: %v (PATHKIT_UPDATE_CONSTRUCTS=1 prints a first draft to check by eye)", path, err)
			}
			got, unread := analyse(t, dir, exp)
			if os.Getenv("PATHKIT_UPDATE_CONSTRUCTS") != "" && err != nil {
				draft := exp
				draft.Construct, draft.Workflows, draft.Unread = dir, got, unread
				var out bytes.Buffer
				enc := json.NewEncoder(&out)
				enc.SetEscapeHTML(false)
				enc.SetIndent("", "  ")
				_ = enc.Encode(draft)
				if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("wrote a first draft of %s: set status and note, and check the paths by eye", path)
				return
			}
			if exp.Construct != dir {
				t.Errorf("construct is %q, want the folder name %q", exp.Construct, dir)
			}
			if exp.Status != "handled" && exp.Status != "reported" {
				t.Errorf("status %q: want handled or reported", exp.Status)
			}
			if exp.Status == "reported" && len(unread) == 0 {
				t.Errorf("status is reported, but PathKit lists nothing as not read")
			}
			if exp.Note == "" {
				t.Errorf("no note")
			}
			gj, _ := json.MarshalIndent(got, "", "  ")
			ej, _ := json.MarshalIndent(exp.Workflows, "", "  ")
			if string(gj) != string(ej) {
				t.Errorf("paths differ\ngot\n%s\nwant\n%s", gj, ej)
			}
			if strings.Join(unread, ",") != strings.Join(exp.Unread, ",") {
				t.Errorf("unread kinds: got %v, want %v", unread, exp.Unread)
			}
		})
	}
}

// docRow reads a row of the table in docs/CONSTRUCTS.md: its status and folder.
var docRow = regexp.MustCompile("^\\| .+ \\| (handled|reported) \\| `([a-z_0-9]+)` \\|")

// docs/CONSTRUCTS.md lists every construct folder, with the status its
// expected.json has.
func TestDocListsEveryConstruct(t *testing.T) {
	data, err := os.ReadFile("../../docs/CONSTRUCTS.md")
	if err != nil {
		t.Fatal(err)
	}
	inDoc := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if m := docRow.FindStringSubmatch(line); m != nil {
			inDoc[m[2]] = m[1]
		}
	}
	for _, dir := range constructDirs(t) {
		raw, err := os.ReadFile(filepath.Join(root, dir, "expected.json"))
		if err != nil {
			t.Fatal(err)
		}
		var exp expected
		if err := json.Unmarshal(raw, &exp); err != nil {
			t.Fatal(err)
		}
		if got, ok := inDoc[dir]; !ok {
			t.Errorf("docs/CONSTRUCTS.md has no row for %s", dir)
		} else if got != exp.Status {
			t.Errorf("docs/CONSTRUCTS.md says %s is %s, expected.json says %s", dir, got, exp.Status)
		}
	}
	if len(inDoc) != len(constructDirs(t)) {
		t.Errorf("the table has %d rows for %d folders", len(inDoc), len(constructDirs(t)))
	}
}
