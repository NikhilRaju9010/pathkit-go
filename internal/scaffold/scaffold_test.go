package scaffold

import "testing"

func TestFileName(t *testing.T) {
	for in, want := range map[string]string{
		"orders.OrderWorkflow":      "pathkit_scaffold_OrderWorkflow_test.go",
		"billing.(*Service).Run":    "pathkit_scaffold_Run_test.go",
		"a/b.Weird-Name Workflow!?": "pathkit_scaffold_WeirdNameWorkflow_test.go",
	} {
		if got := File(in); got != want {
			t.Errorf("File(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSourceNeedsATest(t *testing.T) {
	if _, err := Source(nil); err == nil {
		t.Error("no tests should be an error")
	}
	src, err := Source([]Test{{Name: "TestX", Package: "p", Func: "func TestX(t *testing.T) {}\n", Imports: map[string]string{"errors": "errors"}}, {Name: "TestX", Package: "p", Func: "func TestX(t *testing.T) {}\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if n := countOf(string(src), "func TestX("); n != 1 {
		t.Errorf("the same test name twice should be written once, got %d:\n%s", n, src)
	}
}

func countOf(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
		}
	}
	return n
}
