package expected

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The real answer key is read completely, including the parts the M2/M3
// reader silently skipped: the loop's "retry" step and the tests listed
// as bullets under polling's **Tests** line.
func TestReadRealKey(t *testing.T) {
	key, err := Read("../../testdata/pilot/EXPECTED.md")
	if err != nil {
		t.Fatal(err)
	}
	paths, covered, tests := 0, 0, 0
	for _, w := range key {
		paths += w.Count
		covered += w.Covered
		tests += len(w.Tests)
	}
	if len(key) != 8 || paths != 38 || covered != 20 || tests != 21 {
		t.Errorf("read %d workflows, %d paths, %d covered, %d tests; want 8, 38, 20, 21", len(key), paths, covered, tests)
	}

	polling := key["polling.ReportPollingWorkflow"]
	if got, want := polling.Paths[4].Key, "J1.iterate J2.success J3.default J1.retry J1.exit|completed"; got != want {
		t.Errorf("polling path 5 = %q, want %q", got, want)
	}
	var names []string
	for _, tt := range polling.Tests {
		names = append(names, tt.Name)
	}
	if want := []string{"TestPollingCompleteFirstTime", "TestPollingJobFailed", "TestPollingPendingThenComplete", "TestPollingGivesUp"}; !slices.Equal(names, want) {
		t.Errorf("polling tests = %v, want %v", names, want)
	}
	if !slices.Equal(polling.NotCovered, []int{1, 2}) {
		t.Errorf("polling not covered = %v, want [1 2]", polling.NotCovered)
	}

	var notes []bool
	for _, p := range key["fulfillment.OrderFulfillmentWorkflow"].Paths {
		notes = append(notes, p.Compensation)
	}
	if want := []bool{false, true, true, true}; !slices.Equal(notes, want) {
		t.Errorf("fulfillment compensation notes = %v, want %v", notes, want)
	}
	if got := key["shipment.ShipmentWorkflow"].Paths[1].Key; got != `J1.success J2.signal "delivery-update" J3.case "delivered" J4.failure|failed` {
		t.Errorf("shipment path 2 = %q", got)
	}
}

func TestParsePathLine(t *testing.T) {
	tests := []struct {
		line string
		key  string
		comp bool
	}{
		{"J1 --true--> End (completed)", "J1.true|completed", false},
		{"J1 --false--> J2 ChargeCard --failure--> End (failed)", "J1.false J2.failure|failed", false},
		{"J1 --exit--> End (completed), zero polls", "J1.exit|completed", false},
		{"J1 --iterate--> J2 --success--> retry --> J1 --exit--> End (continued-as-new)", "J1.iterate J2.success J1.retry J1.exit|continued-as-new", false},
		{`J1 --success--> J2 --signal "delivery-update"--> J3 --case "delivered"--> End (failed)`, `J1.success J2.signal "delivery-update" J3.case "delivered"|failed`, false},
		{"J1 --success--> J2 `if !ok` --timeout--> End (completed)", "J1.success J2.timeout|completed", false},
		{"J1 --success--> J2 child PaymentWorkflow --failure--> End (failed) [compensation (defer)]", "J1.success J2.failure|failed", true},
		{"J1 --success--> End (completed) [compensation (defer)]. A remark.", "J1.success|completed", true},
		{"J1 --x--> End", "J1.x|", false},
	}
	for _, tt := range tests {
		key, comp, err := ParsePathLine(tt.line)
		if err != nil || key != tt.key || comp != tt.comp {
			t.Errorf("ParsePathLine(%q) = %q, %v, %v; want %q, %v", tt.line, key, comp, err, tt.key, tt.comp)
		}
	}
}

// minimal is a small, valid answer key. Each case below breaks one line
// and must fail at that line.
const minimal = "# key\n" + // 1
	"## Workflows PathKit must find (and must not find)\n" + // 2
	"Found (1): `a.W`.\n" + // 3
	"## 1. `a.W` (a/a.go)\n" + // 4
	"**2 paths:**\n" + // 5
	"1. J1 --true--> End (completed)\n" + // 6
	"2. J1 --false--> End (failed)\n" + // 7
	"**Tests (a_test.go), 1/2 covered:** `TestA` → 1. Not covered: 2.\n" + // 8
	"## Totals\n" + // 9
	"| Workflow | Paths | Covered by tests |\n" + // 10
	"| --- | --- | --- |\n" + // 11
	"| a.W | 2 | 1 |\n" + // 12
	"| **Total** | **2** | **1 (50.0%)** |\n" + // 13
	"## Disagreements\n" + // 14
	"*(None yet. A quoted J1 --x--> End here is fine.)*\n" // 15

func readString(t *testing.T, doc string) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "EXPECTED.md")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Read(path)
	return err
}

func TestMinimalKeyReads(t *testing.T) {
	if err := readString(t, minimal); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsWhatItCannotRead(t *testing.T) {
	tests := []struct {
		name, old, new string
		line           int
		want           string
	}{
		{"unreadable step", "2. J1 --false--> End (failed)", "2. J1 --false--> ??? End (failed)", 7, "can't read the step"},
		{"step hidden in a remark", "2. J1 --false--> End (failed)", "2. J1 --false--> End (failed), then J2 --x--> End", 7, "looks like a step"},
		{"no end station", "2. J1 --false--> End (failed)", "2. J1 --false-->", 7, "can't read the step"},
		{"retry before the end", "2. J1 --false--> End (failed)", "2. J1 --false--> retry --> End (failed)", 7, "retry"},
		{"junction inside a description", "2. J1 --false--> End (failed)", "2. J1 then J2 --false--> End (failed)", 7, "names another junction"},
		{"unknown note", "2. J1 --false--> End (failed)", "2. J1 --false--> End (failed) [retried]", 7, "unexpected text"},
		{"path numbered wrong", "2. J1 --false-->", "3. J1 --false-->", 7, "path number 3, want 2"},
		{"prose inside the path list", "2. J1 --false--> End (failed)", "and more paths", 7, "expected the **Tests (...)** line"},
		{"count differs from the list", "**2 paths:**", "**3 paths:**", 5, "says 3 paths but lists 2"},
		{"stray step outside a list", "# key", "# key J1 --true--> End", 1, "outside a path list"},
		{"stray test mention", "# key", "# key `TestB` → 2", 1, "test mention outside"},
		{"test beyond the paths", "`TestA` → 1.", "`TestA` → 3.", 8, "has paths 1..2"},
		{"malformed test entry", "`TestA` → 1.", "`TestA` → 1, `TestB` -> 2.", 8, "form `TestName` → N"},
		{"covered count wrong", "1/2 covered", "2/2 covered", 4, "says 2 paths are covered"},
		{"not covered overlaps", "Not covered: 2.", "Not covered: 1.", 4, "covered by a test"},
		{"unknown section", "## Totals", "## Totalz", 9, "unknown section header"},
		{"totals row wrong", "| a.W | 2 | 1 |", "| a.W | 3 | 1 |", 0, "Totals says a.W has 3 paths"},
		{"total row wrong", "**1 (50.0%)**", "**1 (40.0%)**", 0, "the Total row says"},
		{"found list wrong", "Found (1): `a.W`.", "Found (1): `a.X`.", 0, "\"Found\" list"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(minimal, tt.old) {
				t.Fatalf("test setup: %q not in the minimal key", tt.old)
			}
			err := readString(t, strings.Replace(minimal, tt.old, tt.new, 1))
			if err == nil {
				t.Fatalf("read without error; want a failure mentioning %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
			if tt.line > 0 && !strings.Contains(err.Error(), "EXPECTED.md:"+strconv.Itoa(tt.line)+": ") {
				t.Errorf("error %q does not name line %d", err, tt.line)
			}
		})
	}
}
