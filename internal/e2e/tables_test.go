package e2e

import (
	"path/filepath"
	"testing"
)

// The loop rounds table says which of 0, 1 and 2+ rounds the recorded tests ran.
func TestLoopRoundsTableFromRecordedRuns(t *testing.T) {
	fixture := abs(t, "../../testdata/fixtures/summarize")
	t.Chdir(t.TempDir())
	traces := filepath.Join(t.TempDir(), "traces")
	if _, stderr, code := pathkit(t, "test", fixture+"/...", "--traces", traces); code != 0 {
		t.Fatalf("pathkit test: exit %d\n%s", code, stderr)
	}
	doc := summarizeReport(t, fixture, traces, "")
	loops, _ := doc["loopRounds"].([]any)
	if len(loops) == 0 {
		t.Fatalf("no loop rounds table: %v", doc["loopRounds"])
	}
	// Looping.spin has one loop: the test ran it with 2 and with 1 round; 0 rounds
	// can happen and no test ran it.
	var spin map[string]any
	for _, l := range loops {
		if l.(map[string]any)["workflow"] == "summarize.Looping" {
			spin = l.(map[string]any)
		}
	}
	if spin == nil {
		t.Fatalf("no loop of summarize.Looping in %v", loops)
	}
	got := map[string]string{}
	for _, r := range spin["rounds"].([]any) {
		m := r.(map[string]any)
		state := "not seen"
		if m["seen"] == true {
			state = "seen"
		}
		if m["possible"] == false {
			state = "cannot happen"
		}
		got[m["rounds"].(string)] = state
	}
	want := map[string]string{"0 rounds": "not seen", "1 round": "seen", "2+ rounds": "seen"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("loop rounds %s: %s, want %s (all: %v)", k, got[k], v, got)
		}
	}
}
