package trace

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestListReadClear(t *testing.T) {
	dir := t.TempDir()
	good := `{"schemaVersion":1,"tool":"pathkit-go","workflow":"orders.OrderWorkflow","functionHash":"abc","status":"complete","steps":["J1.true"]}`
	files := map[string]string{
		"a.trace.json": good,
		"b.trace.json": `{"schemaVersion":2}`,
		"c.trace.json": `not json`,
		"notes.txt":    "keep me",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	list, err := List(dir)
	if err != nil || len(list) != 3 {
		t.Fatalf("List = %v, %v; want the 3 .trace.json files", list, err)
	}
	f, err := Read(list[0])
	if err != nil || f.Workflow != "orders.OrderWorkflow" || !slices.Equal(f.Steps, []string{"J1.true"}) {
		t.Errorf("Read(a) = %+v, %v", f, err)
	}
	if _, err := Read(list[1]); err == nil || !strings.Contains(err.Error(), "unsupported trace schemaVersion 2") {
		t.Errorf("Read(b) error = %v", err)
	}
	if _, err := Read(list[2]); err == nil || !strings.Contains(err.Error(), "not a valid trace file") {
		t.Errorf("Read(c) error = %v", err)
	}

	n, err := Clear(dir)
	if err != nil || n != 3 {
		t.Errorf("Clear = %d, %v; want 3", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Errorf("Clear deleted a file that is not a trace: %v", err)
	}
}

func TestMissingDirHasNoTraces(t *testing.T) {
	list, err := List(filepath.Join(t.TempDir(), "nope"))
	if err != nil || len(list) != 0 {
		t.Errorf("List(missing) = %v, %v", list, err)
	}
}

func TestNoTracesMessage(t *testing.T) {
	want := `no trace files found in .pathkit/traces; coverage is recorded only by "pathkit test" (plain "go test" records nothing). Run "pathkit test ./..." first, from the folder that holds go.mod, then run this command from the same folder (or point to the traces with --traces)`
	if got := NoTracesMessage(".pathkit/traces"); got != want {
		t.Errorf("got %q", got)
	}
}

// The package field is optional: an older file without it still loads, and
// a new one keeps it. A trace for another package is not matched to a
// workflow of the same name (CheckIn).
func TestPackageFieldIsOptional(t *testing.T) {
	dir := t.TempDir()
	old := `{"schemaVersion":1,"tool":"pathkit-go","workflow":"orders.OrderWorkflow","functionHash":"abc","status":"complete","steps":["J1.true"]}`
	withPkg := `{"schemaVersion":1,"tool":"pathkit-go","workflow":"orders.OrderWorkflow","package":"example.com/p/orders","functionHash":"abc","status":"complete","steps":["J1.true"]}`
	for name, body := range map[string]string{"old.trace.json": old, "new.trace.json": withPkg} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	o, err := Read(filepath.Join(dir, "old.trace.json"))
	if err != nil || o.Package != "" {
		t.Fatalf("old: %+v, %v", o, err)
	}
	n, err := Read(filepath.Join(dir, "new.trace.json"))
	if err != nil || n.Package != "example.com/p/orders" {
		t.Fatalf("new: %+v, %v", n, err)
	}
}
