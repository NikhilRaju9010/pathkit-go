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

// writeModule writes a throwaway module: files maps a slash path to its
// content.
func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["go.mod"] = "module example.com/bp\n\ngo 1.26.0\n"
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Owner's decision (M7): a package that doesn't compile stops the load,
// and every broken package is named with its real error (not go list's
// "-: # <package>" summary line). A package that only imports a broken
// one is not listed.
func TestEveryBrokenPackageIsNamed(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"a/a.go":  "package a\n\nvar X int = \"s\"\n",
		"b/b.go":  "package b\n\nvar Y string = 1\n",
		"c/c.go":  "package c\n\nimport \"example.com/bp/a\"\n\nvar Z = a.X\n",
		"ok/k.go": "package ok\n",
	})
	t.Chdir(dir)
	_, err := Load("./...")
	want := "2 packages do not compile: " +
		"example.com/bp/a: " + filepath.Join("a", "a.go") + ":3:13: cannot use \"s\" (untyped string constant) as int value in variable declaration; " +
		"example.com/bp/b: " + filepath.Join("b", "b.go") + ":3:16: cannot use 1 (untyped int constant) as string value in variable declaration"
	if err == nil || err.Error() != want {
		t.Errorf("error =\n  %v\nwant\n  %s", err, want)
	}

	// One broken package keeps the M2 wording, now with the real error.
	_, err = Load("./b")
	want = "package does not compile: example.com/bp/b: " + filepath.Join("b", "b.go") + ":3:16: cannot use 1 (untyped int constant) as string value in variable declaration"
	if err == nil || err.Error() != want {
		t.Errorf("error =\n  %v\nwant\n  %s", err, want)
	}
}
