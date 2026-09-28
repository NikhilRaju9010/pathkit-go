package load

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotAGoFile(t *testing.T) {
	arg := "../../testdata/pilot/EXPECTED.md"
	_, err := Load(arg)
	if err == nil || err.Error() != "not a Go file: "+arg {
		t.Errorf("Load(%q) error = %v, want %q", arg, err, "not a Go file: "+arg)
	}
}

func TestNoGoPackagesFound(t *testing.T) {
	// A module folder with no .go files in it at all.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/empty\n\ngo 1.26.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir + "/...")
	if err == nil || !strings.HasPrefix(err.Error(), "no Go packages found in ") {
		t.Errorf("Load(%s/...) error = %v, want prefix %q", dir, err, "no Go packages found in ")
	}
}

func TestBareDotDotDot(t *testing.T) {
	// "..." on its own means "every package under the current folder".
	t.Chdir("../../testdata/pilot/fulfillment")
	res, err := Load("...")
	if err != nil {
		t.Fatalf("Load(\"...\"): %v", err)
	}
	if len(res.Packages) != 1 || res.Packages[0].Name != "fulfillment" || res.File != "" {
		t.Errorf("got %d packages (first %q), File=%q; want just fulfillment", len(res.Packages), res.Packages[0].Name, res.File)
	}
}
