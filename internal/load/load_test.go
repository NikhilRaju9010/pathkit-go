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

// "<folder>/..." written with the system's own separator (what joining a
// config's "./..." onto its folder gives: "<folder>\..." on Windows) means
// every package under the folder, not the folder alone.
func TestRecursivePatternWithOSSeparator(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"a/a.go":   "package a\n",
		"a/b/b.go": "package b\n",
	})
	arg := filepath.Join(dir, "...")
	gotDir, pattern, file, err := Resolve(arg)
	if err != nil || gotDir != dir || pattern != "./..." || file != "" {
		t.Errorf("Resolve(%q) = %q, %q, %q, %v; want %q, \"./...\"", arg, gotDir, pattern, file, err, dir)
	}
	res, err := Load(arg)
	if err != nil {
		t.Fatalf("Load(%q): %v", arg, err)
	}
	var names []string
	for _, p := range res.Packages {
		names = append(names, p.PkgPath)
	}
	if strings.Join(names, " ") != "example.com/bp/a example.com/bp/a/b" {
		t.Errorf("Load(%q) found %v; want both packages", arg, names)
	}
	// The slash form keeps working everywhere, and a bare "..." too.
	if d, p, _, err := Resolve(dir + "/..."); err != nil || d != dir || p != "./..." {
		t.Errorf("Resolve(%q/...) = %q, %q, %v", dir, d, p, err)
	}
}

// When the current folder is reached through a symlink (on macOS the temp
// folder /var/... is really /private/var/...), file names are still shown
// relative to it, not as long absolute paths.
func TestSymlinkedFolderShowsShortPaths(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"a/a.go": "package a\n\nvar X int = \"s\"\n",
	})
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatalf("could not create a symlink: %v", err)
	}
	t.Chdir(link)
	_, err := Load("./...")
	want := "package does not compile: example.com/bp/a: " + filepath.Join("a", "a.go") + ":3:13: cannot use"
	if err == nil || !strings.HasPrefix(err.Error(), want) {
		t.Errorf("error =\n  %v\nwant it to start with\n  %s", err, want)
	}
}
