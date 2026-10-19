package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A .pathkit folder copied from another machine can hold an overlay.json
// that names absolute paths in another folder. PathKit refuses to use it,
// leaves that other folder alone, and says what to delete.
func TestPrepareRefusesACopiedOverlayThatPointsOutside(t *testing.T) {
	mod := copyActivityArg(t)
	other := t.TempDir()
	victim := filepath.Join(other, "keep.go")
	if err := os.WriteFile(victim, []byte("package keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(mod, ".pathkit", "overlay")
	if err := os.MkdirAll(overlay, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := `{"Replace": {` + jsonString(victim) + `: ` + jsonString(filepath.Join(other, "000_keep.go")) + `}}`
	if err := os.WriteFile(filepath.Join(overlay, "overlay.json"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(mod)
	_, stderr, code := run(t, "prepare", "./activityarg")
	if code != ExitError || !strings.Contains(stderr, "outside the module folder") || !strings.Contains(stderr, "Delete ") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if got, _ := os.ReadFile(filepath.Join(overlay, "overlay.json")); string(got) != stale {
		t.Errorf("the copied overlay.json was changed")
	}
	if data, _ := os.ReadFile(victim); string(data) != "package keep\n" {
		t.Errorf("a file outside the module was changed")
	}
}

// Near miss: an overlay.json from an earlier run in this module (paths inside
// it) is simply replaced.
func TestPrepareReplacesAnOverlayFromThisModule(t *testing.T) {
	mod := copyActivityArg(t)
	overlay := filepath.Join(mod, ".pathkit", "overlay")
	if err := os.MkdirAll(overlay, 0o755); err != nil {
		t.Fatal(err)
	}
	old := `{"Replace": {` + jsonString(filepath.Join(mod, "activityarg", "activityarg.go")) + `: ` + jsonString(filepath.Join(overlay, "000_activityarg.go")) + `}}`
	if err := os.WriteFile(filepath.Join(overlay, "overlay.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(mod)
	if _, stderr, code := run(t, "prepare", "./activityarg"); code != ExitOK {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

// A .pathkit/overlay that is a link to a folder elsewhere (a copy keeps links)
// is refused too: PathKit deletes and writes files in that folder.
func TestPrepareRefusesAnOverlayFolderThatIsALinkOutside(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links need extra rights on Windows")
	}
	mod := copyActivityArg(t)
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "overlay.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(mod, ".pathkit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(mod, ".pathkit", "overlay")); err != nil {
		t.Skip("cannot make a link here:", err)
	}
	t.Chdir(mod)
	_, stderr, code := run(t, "prepare", "./activityarg")
	if code != ExitError || !strings.Contains(stderr, "not inside the module folder") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(other, "overlay.json")); err != nil {
		t.Errorf("a file outside the module was deleted")
	}
	entries, _ := os.ReadDir(other)
	if len(entries) != 1 {
		t.Errorf("files were written outside the module: %v", entries)
	}
}
