package cli

import (
	"strings"
	"testing"
)

// "explain" prints a claim's proof as numbered plain sentences, and says
// which step of the path and which line each sentence is about.
func TestExplainPrintsTheProof(t *testing.T) {
	folder := absPath(t, "../../testdata/fixtures/dataflow")
	cfg := writeConfig(t, `{"packages": [`+jsonString(folder)+`], "classification": {"enabled": true}}`)
	var claimed string
	for _, id := range stableIDsOf(t, folder, "dataflow.TwiceLocal") {
		out, stderr, code := run(t, "explain", id, "--config", cfg, "--function", "TwiceLocal", "--traces", t.TempDir())
		if code != ExitOK {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		if strings.Contains(out, "Label:       contradictory") {
			claimed = out
		} else if strings.Contains(out, "Proof:") {
			t.Errorf("a path nobody claimed has a proof:\n%s", out)
		}
	}
	if claimed == "" {
		t.Fatal("no path of TwiceLocal was claimed")
	}
	i := strings.Index(claimed, "Proof:\n")
	if i < 0 {
		t.Fatalf("no proof printed:\n%s", claimed)
	}
	proof := claimed[i:]
	for _, want := range []string{
		"  1. J1 (if valid) takes the ",
		" [step 1, dataflow.go:",
		"  2. J2 (if !valid) takes the ",
		" [step 2, dataflow.go:",
		"  3. The value cannot change in between, so the two steps cannot give these two answers.\n",
	} {
		if !strings.Contains(proof, want) {
			t.Errorf("proof lacks %q:\n%s", want, proof)
		}
	}
	if strings.Contains(proof, "[step 0") {
		t.Errorf("the closing sentence should name no step:\n%s", proof)
	}
}

// --json gets a new "proof" key on a claimed path and nowhere else: a plain
// testable path has none, and no other key changed.
func TestJSONProofKeyOnClaimsOnly(t *testing.T) {
	folder := absPath(t, "../../testdata/fixtures/dataflow")
	cfg := writeConfig(t, `{"packages": [`+jsonString(folder)+`], "classification": {"enabled": true}}`)
	traces := writeTracesFor(t, "../../testdata/fixtures/dataflow", "dataflow.TwiceLocal", []string{"J1.true", "J2.false"})
	for _, cmd := range []string{"coverage", "report"} {
		out, stderr, code := run(t, cmd, folder, "--config", cfg, "--include", "TwiceLocal", "--traces", traces, "--json")
		if code != ExitOK {
			t.Fatalf("%s: code=%d stderr=%q", cmd, code, stderr)
		}
		wf := decode(t, out)["workflows"].([]any)[0].(map[string]any)
		withProof := 0
		for _, p := range wf["pathList"].([]any) {
			path := p.(map[string]any)
			proof, has := path["proof"]
			if path["bucket"] == "testable" {
				if has {
					t.Errorf("%s: testable path %v has a proof", cmd, path["id"])
				}
				continue
			}
			if !has {
				t.Errorf("%s: claimed path %v has no proof", cmd, path["id"])
				continue
			}
			withProof++
			steps := proof.([]any)
			last := steps[len(steps)-1].(map[string]any)
			first := steps[0].(map[string]any)
			if first["step"].(float64) < 1 || first["line"].(float64) < 1 || first["file"] == "" || first["text"] == "" {
				t.Errorf("%s: first proof step incomplete: %v", cmd, first)
			}
			if last["step"].(float64) != 0 || last["text"] == "" {
				t.Errorf("%s: last proof step should be a closing sentence: %v", cmd, last)
			}
		}
		if withProof == 0 {
			t.Errorf("%s: no path had a proof", cmd)
		}
	}
}
