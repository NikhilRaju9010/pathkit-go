package cli

import (
	"os"
	"path/filepath"
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

// The fact engine through the program: explain prints the proof with
// numbered sentences (decision steps with step and line, code between
// decisions with a line), the JSON has the proof, and the config can turn
// the engine off.
func TestExplainAnEngineClaim(t *testing.T) {
	folder := absPath(t, "../../testdata/shapes/valueflow")
	cfg := writeConfig(t, `{"packages": [`+jsonString(folder)+`], "classification": {"enabled": true}}`)
	var claimed string
	for _, id := range stableIDsOf(t, folder, "valueflow.ContradictionThroughAssign") {
		out, stderr, code := run(t, "explain", id, "--config", cfg, "--function", "ContradictionThroughAssign", "--traces", t.TempDir())
		if code != ExitOK {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		if strings.Contains(out, "Label:       value-flow") {
			claimed = out
		}
	}
	if claimed == "" {
		t.Fatal("no path was explained as value-flow")
	}
	for _, want := range []string{
		"Bucket:      cannot-be-tested",
		"Decided by:  detector value-flow (automatic, certain)",
		"Proof:\n",
		"  1. `id` is set to \"\". [valueflow.go:17]\n",
		"  2. J1 (if ok) takes the false side, so `id = v` is skipped. [step 1, valueflow.go:19]\n",
		"  3. J2 (if id == \"\") takes the false side. [step 2, valueflow.go:22]\n",
		"  4. Nothing on the path changes this in between, so `id == \"\"` is always true at that point and the false side cannot be taken.\n",
	} {
		if !strings.Contains(claimed, want) {
			t.Errorf("explain lacks %q:\n%s", want, claimed)
		}
	}

	// The same workflow with the engine turned off in the config: every path testable.
	off := writeConfig(t, `{"packages": [`+jsonString(folder)+`], "classification": {"enabled": true, "engine": {"enabled": false}}}`)
	traces := writeTracesFor(t, "../../testdata/shapes/valueflow", "valueflow.ContradictionThroughAssign", []string{"J1.true", "J2.true"})
	out, _, code := run(t, "coverage", folder, "--config", off, "--include", "ContradictionThroughAssign", "--traces", traces, "--json")
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if b := decode(t, out)["classification"].(map[string]any)["buckets"].(map[string]any)["cannotBeTested"].(map[string]any)["paths"].(float64); b != 0 {
		t.Errorf("engine off: %v cannot-be-tested paths, want 0", b)
	}
	on := writeConfig(t, `{"packages": [`+jsonString(folder)+`], "classification": {"enabled": true}}`)
	out, _, _ = run(t, "coverage", folder, "--config", on, "--include", "ContradictionThroughAssign", "--traces", traces, "--json")
	if b := decode(t, out)["classification"].(map[string]any)["buckets"].(map[string]any)["cannotBeTested"].(map[string]any)["paths"].(float64); b != 1 {
		t.Errorf("engine on: %v cannot-be-tested paths, want 1", b)
	}
}

// copyShape copies the typeswitch shape fixture into a throw-away module.
func copyShape(t *testing.T) string {
	t.Helper()
	src := absPath(t, "../../testdata/shapes")
	dst := t.TempDir()
	for _, f := range []string{"go.mod", "go.sum", "typeswitch/typeswitch.go"} {
		data, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dst, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, f), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

// The assumption default-json-converter: claims rest on it and say so; when
// the module sets its own data converter the rule turns itself off, the claim
// goes, and the JSON, the text report and the HTML all say so.
func TestConverterScanTurnsTheRuleOff(t *testing.T) {
	mod := copyShape(t)
	cfg := writeConfig(t, `{"packages": [`+jsonString(mod+"/typeswitch")+`], "workflows": {"include": ["SwitchOnDecodedInput"]}, "classification": {"enabled": true}}`)
	traces := writeTracesFor(t, "../../testdata/shapes/typeswitch", "typeswitch.SwitchOnDecodedInput", []string{"J1.case string"})

	out, stderr, code := run(t, "coverage", mod+"/typeswitch", "--config", cfg, "--traces", traces, "--json")
	if code != ExitOK {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	m := decode(t, out)
	as := m["assumptions"].([]any)
	if len(as) != 1 {
		t.Fatalf("assumptions: %v", as)
	}
	a := as[0].(map[string]any)
	if a["name"] != "default-json-converter" || a["on"] != true || a["claims"].(float64) != 1 {
		t.Errorf("on: %v", a)
	}
	var claimed map[string]any
	for _, p := range m["workflows"].([]any)[0].(map[string]any)["pathList"].([]any) {
		if pp := p.(map[string]any); pp["bucket"] == "cannot-be-tested" {
			claimed = pp
		}
	}
	if claimed == nil || claimed["label"] != "impossible-type" || claimed["assumptions"].([]any)[0] != "default-json-converter" {
		t.Errorf("the claim: %v", claimed)
	}
	text, _, _ := run(t, "report", mod+"/typeswitch", "--config", cfg, "--traces", traces)
	if !strings.Contains(text, "Assumption default-json-converter: on; 1 claim rests on it") {
		t.Errorf("text report lacks the assumption line:\n%s", text)
	}

	// A mention in a comment, and a decoder elsewhere that uses UseNumber, do
	// not switch the rule off: neither touches a value Temporal decoded.
	if err := os.WriteFile(filepath.Join(mod, "typeswitch", "other.go"), []byte("package typeswitch\n\n// the worker may use a DataConverter one day\n\nimport \"encoding/json\"\n\nfunc numbers(d *json.Decoder) { d.UseNumber() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, _ = run(t, "coverage", mod+"/typeswitch", "--config", cfg, "--traces", traces, "--json")
	if a := decode(t, out)["assumptions"].([]any)[0].(map[string]any); a["on"] != true || a["claims"].(float64) != 1 {
		t.Errorf("a comment and an unrelated UseNumber switched the rule off: %v", a)
	}
	if err := os.Remove(filepath.Join(mod, "typeswitch", "other.go")); err != nil {
		t.Fatal(err)
	}

	// The module now sets its own data converter.
	if err := os.WriteFile(filepath.Join(mod, "typeswitch", "conv.go"), []byte("package typeswitch\n\ntype workerOptions struct{ DataConverter any }\n\nvar _ = workerOptions{DataConverter: nil}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, _ = run(t, "coverage", mod+"/typeswitch", "--config", cfg, "--traces", traces, "--json")
	m = decode(t, out)
	a = m["assumptions"].([]any)[0].(map[string]any)
	if a["on"] != false || !strings.Contains(a["why"].(string), "typeswitch/conv.go:5") {
		t.Errorf("off: %v", a)
	}
	for _, p := range m["workflows"].([]any)[0].(map[string]any)["pathList"].([]any) {
		if pp := p.(map[string]any); pp["bucket"] == "cannot-be-tested" {
			t.Errorf("a claim about a decoded value with the rule off: %v", pp)
		}
	}
	text, _, _ = run(t, "report", mod+"/typeswitch", "--config", cfg, "--traces", traces)
	if !strings.Contains(text, "Assumption default-json-converter: OFF") || !strings.Contains(text, "typeswitch/conv.go:5") {
		t.Errorf("text report lacks the OFF line:\n%s", text)
	}
	page := filepath.Join(t.TempDir(), "r.html")
	run(t, "report", mod+"/typeswitch", "--config", cfg, "--traces", traces, "--html="+page)
	html, err := os.ReadFile(page)
	if err != nil || !strings.Contains(string(html), "Assumption: default-json-converter") || !strings.Contains(string(html), "OFF: the module sets its own data converter") {
		t.Errorf("the HTML lacks the assumption (err %v)", err)
	}
}

// copyActivityArg copies the activityarg fixture into a throw-away module.
func copyActivityArg(t *testing.T) string {
	t.Helper()
	src := absPath(t, "../../testdata/fixtures")
	dst := t.TempDir()
	for _, f := range []string{"go.mod", "go.sum", "activityarg/activityarg.go"} {
		data, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dst, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, f), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

// The assumption no-custom-interceptor: a map handed to an activity keeps its
// facts (the SDK encodes it at the call) and the claim says what it rests on;
// a local activity never does; when the module registers an interceptor the
// rule turns itself off, and the JSON, the text report and the HTML say so.
func TestInterceptorScanTurnsTheRelaxationOff(t *testing.T) {
	mod := copyActivityArg(t)
	cfg := writeConfig(t, `{"packages": [`+jsonString(mod+"/activityarg")+`], "classification": {"enabled": true}}`)
	traces := writeTracesFor(t, "../../testdata/fixtures/activityarg", "activityarg.MapToActivity", []string{"J1.success", "J2.case string"})

	claims := func(m map[string]any) map[string][]any {
		out := map[string][]any{}
		for _, w := range m["workflows"].([]any) {
			ww := w.(map[string]any)
			for _, p := range ww["pathList"].([]any) {
				if pp := p.(map[string]any); pp["bucket"] == "cannot-be-tested" {
					a, _ := pp["assumptions"].([]any)
					out[ww["name"].(string)] = append(out[ww["name"].(string)], a)
				}
			}
		}
		return out
	}
	out, stderr, code := run(t, "coverage", mod+"/activityarg", "--config", cfg, "--traces", traces, "--json")
	if code != ExitOK {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	m := decode(t, out)
	got := claims(m)
	// the int case, and the default (the value is a string, which the first case takes)
	if len(got["activityarg.MapToActivity"]) != 2 || len(got["activityarg.MapToLocalActivity"]) != 0 {
		t.Fatalf("claims by workflow: %v (want two for MapToActivity, none for the local activity)", got)
	}
	for _, c := range got["activityarg.MapToActivity"] {
		if as := c.([]any); len(as) != 1 || as[0] != "no-custom-interceptor" {
			t.Errorf("the claim rests on %v", as)
		}
	}
	var a map[string]any
	for _, x := range m["assumptions"].([]any) {
		if x.(map[string]any)["name"] == "no-custom-interceptor" {
			a = x.(map[string]any)
		}
	}
	if a == nil || a["on"] != true || a["claims"].(float64) != 2 {
		t.Errorf("assumption: %v", a)
	}
	text, _, _ := run(t, "report", mod+"/activityarg", "--config", cfg, "--traces", traces)
	if !strings.Contains(text, "Assumption no-custom-interceptor: on; 2 claims rest on it") {
		t.Errorf("text report lacks the assumption line:\n%s", text)
	}

	// The module registers an interceptor.
	if err := os.WriteFile(filepath.Join(mod, "activityarg", "worker.go"), []byte("package activityarg\n\ntype workerOptions struct{ Interceptors []any }\n\nvar _ = workerOptions{Interceptors: nil}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, _ = run(t, "coverage", mod+"/activityarg", "--config", cfg, "--traces", traces, "--json")
	m = decode(t, out)
	if got := claims(m); len(got) != 0 {
		t.Errorf("claims with a custom interceptor registered: %v", got)
	}
	a = nil
	for _, x := range m["assumptions"].([]any) {
		if x.(map[string]any)["name"] == "no-custom-interceptor" {
			a = x.(map[string]any)
		}
	}
	if a == nil || a["on"] != false || !strings.Contains(a["why"].(string), "activityarg/worker.go:5") {
		t.Errorf("off: %v", a)
	}
	text, _, _ = run(t, "report", mod+"/activityarg", "--config", cfg, "--traces", traces)
	if !strings.Contains(text, "Assumption no-custom-interceptor: OFF") || !strings.Contains(text, "activityarg/worker.go:5") {
		t.Errorf("text report lacks the OFF line:\n%s", text)
	}
	page := filepath.Join(t.TempDir(), "r.html")
	run(t, "report", mod+"/activityarg", "--config", cfg, "--traces", traces, "--html="+page)
	html, err := os.ReadFile(page)
	if err != nil || !strings.Contains(string(html), "Assumption: no-custom-interceptor") {
		t.Errorf("the HTML lacks the assumption (err %v)", err)
	}

	// Turned off in the config: no relaxation, and the report says so.
	if err := os.Remove(filepath.Join(mod, "activityarg", "worker.go")); err != nil {
		t.Fatal(err)
	}
	cfgOff := writeConfig(t, `{"packages": [`+jsonString(mod+"/activityarg")+`], "classification": {"enabled": true, "assumptions": {"no_custom_interceptor": "off"}}}`)
	out, _, _ = run(t, "coverage", mod+"/activityarg", "--config", cfgOff, "--traces", traces, "--json")
	if got := claims(decode(t, out)); len(got) != 0 {
		t.Errorf("claims with the assumption turned off in the config: %v", got)
	}
}
