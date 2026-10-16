package load

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, name, text string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestModuleRoot(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/m\n")
	write(t, root, "a/b/c.go", "package b\n")
	for _, p := range []string{root, filepath.Join(root, "a/b"), filepath.Join(root, "a/b/c.go")} {
		got, want := ModuleRoot(p), root
		if gr, _ := filepath.EvalSymlinks(got); gr != "" {
			got = gr
		}
		if wr, _ := filepath.EvalSymlinks(want); wr != "" {
			want = wr
		}
		if got != want {
			t.Errorf("ModuleRoot(%s) = %q, want %q", p, got, want)
		}
	}
}

// A module that sets its own data converter, payload codec or number decoding
// is found; a clean one, a vendored copy and testdata are not.
func TestCustomConverterUse(t *testing.T) {
	cases := map[string]string{
		"data converter": "package w\nvar _ = client.Options{DataConverter: x}\n",
		"payload codec":  "package w\nvar _ = converter.NewCodecDataConverter(a, b)\n",
		"use number":     "package w\nfunc f(d *json.Decoder) { d.UseNumber() }\n",
		"json number":    "package w\nvar _ json.Number\n",
	}
	for name, src := range cases {
		root := t.TempDir()
		write(t, root, "go.mod", "module example.com/m\n")
		write(t, root, "w/w.go", src)
		hits := CustomConverterUse(root)
		if len(hits) != 1 || !strings.HasPrefix(hits[0], "w/w.go:") {
			t.Errorf("%s: hits = %v", name, hits)
		}
	}
	clean := t.TempDir()
	write(t, clean, "go.mod", "module example.com/m\n")
	write(t, clean, "w/w.go", "package w\nfunc F() {}\n")
	write(t, clean, "vendor/x/x.go", "package x\nvar _ = DataConverter\n")
	write(t, clean, "testdata/y.go", "package y\nvar _ = DataConverter\n")
	write(t, clean, "w/notes.txt", "DataConverter\n")
	if hits := CustomConverterUse(clean); len(hits) != 0 {
		t.Errorf("a clean module: hits = %v", hits)
	}
}
