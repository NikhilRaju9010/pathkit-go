package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// A config entry written for a path of a workflow that did not follow helpers
// points at an id that no path has now. The warning names the paths that took
// its place, so the entry can be moved.
func TestVanishedStableIDListsReplacements(t *testing.T) {
	fixture := absPath(t, "../../testdata/fixtures/helperrun")
	// OneHelper had one path before helpers were followed: nothing between
	// Start and an unknown end. Its stable id was the hash of that key.
	sum := sha256.Sum256([]byte(model.StableIDVersion + "\n|"))
	old := model.StableIDVersion + ":" + hex.EncodeToString(sum[:])[:12]
	cfg := writeConfig(t, `{"packages": [`+jsonString(fixture)+`], "classification": {"enabled": true,
	  "paths": {"mark_untestable": [{"workflow": "OneHelper", "stable_id": "`+old+`", "reason": "never runs"}]}}}`)
	traces := writeTracesFor(t, "../../testdata/fixtures/helperrun", "helperrun.OneHelper", []string{"J1.success", "J2.success"})
	_, stderr, code := run(t, "coverage", fixture, "--function", "helperrun.OneHelper", "--traces", traces, "--config", cfg)
	if code != ExitOK {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "stale entry") || !strings.Contains(stderr, "the paths that replace it are s1:") {
		t.Errorf("the warning does not list the paths that replace the id:\n%s", stderr)
	}
	// Both paths of OneHelper are named.
	if n := strings.Count(stderr[strings.Index(stderr, "replace it are"):], "s1:"); n != 2 {
		t.Errorf("%d replacement ids named, want 2:\n%s", n, stderr)
	}
}

// An entry that never pointed at a helper-following workflow is warned about
// as before, without a list.
func TestVanishedStableIDWithoutHelpersHasNoList(t *testing.T) {
	fixture := absPath(t, "../../testdata/fixtures/untestable")
	cfg := writeConfig(t, `{"packages": [`+jsonString(fixture)+`], "classification": {"enabled": true,
	  "paths": {"mark_untestable": [{"workflow": "SameLine", "stable_id": "s1:ffffffffffff", "reason": "r"}]}}}`)
	_, stderr, _ := run(t, "coverage", fixture, "--function", "untestable.SameLine", "--traces", writeUntestableTraces(t, []string{"J1.false"}), "--config", cfg)
	if !strings.Contains(stderr, "stale entry") || strings.Contains(stderr, "replace it are") {
		t.Errorf("stderr %q", stderr)
	}
}

// The unmatched-trace warning prints the ready-made entry under it.
func TestUnmatchedWarningPrintsEntry(t *testing.T) {
	traces := writeTracesFor(t, "../../testdata/fixtures/helperrun", "helperrun.OneHelper", []string{"J9.nowhere"})
	_, stderr, code := run(t, "coverage", absPath(t, "../../testdata/fixtures/helperrun"), "--function", "helperrun.OneHelper", "--traces", traces)
	if code != ExitOK {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, `"reason": "TODO"`) || !strings.Contains(stderr, `"workflow": "helperrun.OneHelper"`) || !strings.Contains(stderr, "classification.explained_traces") {
		t.Errorf("no ready-made entry:\n%s", stderr)
	}
}
