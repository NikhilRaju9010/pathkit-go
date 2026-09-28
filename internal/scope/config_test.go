package scope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The file is searched from the current folder upward, stopping after
// the folder with go.mod; with no go.mod above, only the current folder.
func TestFind(t *testing.T) {
	root := t.TempDir()
	mod := filepath.Join(root, "outer", "mod")
	deep := filepath.Join(mod, "sub", "deep")
	write(t, filepath.Join(mod, "go.mod"), "module x\n")
	write(t, filepath.Join(root, "outer", FileName), "{}") // above the module: never used
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	found := func(start string) string {
		t.Helper()
		p, err := Find(start)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	if p := found(deep); p != "" {
		t.Errorf("found %s above the go.mod folder; want none", p)
	}
	write(t, filepath.Join(mod, FileName), "{}")
	if p := found(deep); p != filepath.Join(mod, FileName) {
		t.Errorf("found %q, want the one in the go.mod folder", p)
	}
	write(t, filepath.Join(mod, "sub", FileName), "{}")
	if p := found(deep); p != filepath.Join(mod, "sub", FileName) {
		t.Errorf("found %q, want the nearest one (sub)", p)
	}

	// No go.mod anywhere above: only the starting folder counts.
	loose := t.TempDir()
	write(t, filepath.Join(loose, FileName), "{}")
	inner := filepath.Join(loose, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if p := found(inner); p != "" {
		t.Errorf("found %s in a parent with no go.mod anywhere; want none", p)
	}
	if p := found(loose); p != filepath.Join(loose, FileName) {
		t.Errorf("found %q in the starting folder, want it", p)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	if c, err := Load("", dir); c != nil || err != nil {
		t.Errorf("Load with no file = %v, %v; want nil, nil (no config: everything in scope)", c, err)
	}
	_, err := Load(filepath.Join(dir, "nope.json"), dir)
	if err == nil || !strings.Contains(err.Error(), "--config file not found: ") {
		t.Errorf("missing --config: err = %v", err)
	}
	cfgPath := filepath.Join(dir, "other", "custom.json")
	write(t, cfgPath, `{"traces": "out/traces"}`)
	c, err := Load(cfgPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	// Relative paths are relative to the config file's folder.
	if got, want := c.Abs(c.Traces), filepath.Join(dir, "other", "out", "traces"); got != want {
		t.Errorf("traces = %q, want %q", got, want)
	}
	if got, want := c.Packages, []string{"./..."}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("default packages = %v, want %v", got, want)
	}
}

func TestReadValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	write(t, path, `{
  "packages": ["./internal/workflows/..."],
  "traces": ".pathkit/traces",
  "workflows": {
    "include": ["OrderWorkflow", "pkg.(*Svc).Run"],
    "exclude": [{ "name": "LegacyBillingWorkflow", "reason": "needs real bank sandbox" }]
  },
  "failUnder": 80,
  "html": true,
  "out": "report.txt",
  "json": false,
  "noColor": true,
  "allowStale": false
}`)
	c, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !c.HasInclude || len(c.Include) != 2 || len(c.Exclude) != 1 || c.Exclude[0].Reason != "needs real bank sandbox" {
		t.Errorf("workflows read wrong: %+v", c)
	}
	if c.FailUnder == nil || *c.FailUnder != 80 || c.HTML != true || c.Out != "report.txt" || !c.NoColor {
		t.Errorf("later keys read wrong: %+v", c)
	}
}

// Every problem is an error that starts with the file's absolute path.
func TestReadErrors(t *testing.T) {
	tests := []struct{ name, content, want string }{
		{"invalid JSON", "{\n  \"traces\": \"x\",\n  \"workflows\": {,\n}", "invalid JSON at line 3, column 17: "},
		{"file ends early", `{"traces": "x"`, "invalid JSON: the file ends too early"},
		{"empty file", "", "the file is empty; write {} for no settings"},
		{"unknown key", `{"workflow": {}}`, `unknown key "workflow"`},
		{"packages not a list", `{"packages": "./..."}`, `"packages" has the wrong type (line 1): expected a list of text, like ["a", "b"], found text`},
		{"unknown nested key", `{"workflows": {"exclude": [], "includes": []}}`, `unknown key "includes"`},
		{"wrong type", `{"failUnder": "high"}`, `"failUnder" has the wrong type (line 1): expected a number, found text`},
		{"failUnder range", `{"failUnder": 120}`, `"failUnder" must be a number from 0 to 100, got 120`},
		{"html type", `{"html": 5}`, `"html" must be true, false, or a file path`},
		{"empty include", `{"workflows": {"include": []}}`, "workflows.include is empty: remove it to include every workflow, or list the ones you want"},
		{"exclude as a string", `{"workflows": {"exclude": ["X"]}}`, `workflows.exclude[0] must look like {"name": "OrderWorkflow", "reason": "why it doesn't count"}`},
		{"exclude without reason", `{"workflows": {"exclude": [{"name": "X"}]}}`, `exclude "X" needs a "reason"`},
		{"exclude with empty reason", `{"workflows": {"exclude": [{"name": "X", "reason": " "}]}}`, `exclude "X" needs a "reason"`},
		{"text after the object", `{} {}`, "unexpected text after the closing }"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), FileName)
			write(t, path, tt.content)
			_, err := Read(path)
			if err == nil || !strings.HasPrefix(err.Error(), path+": ") || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v\nwant it to start with %q and contain %q", err, path+": ", tt.want)
			}
		})
	}
}
