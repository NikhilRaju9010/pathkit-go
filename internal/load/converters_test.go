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

// A module that sets its own data converter or payload codec is found; a
// clean one, a vendored copy, testdata, and text that only mentions the names
// are not. UseNumber and json.Number do not count here: they concern the
// decoder that produced one value.
func TestCustomConverterUse(t *testing.T) {
	found := map[string]string{
		"data converter option": "package w\nvar _ = client.Options{DataConverter: x}\n",
		"data converter set":    "package w\nfunc f(o *client.Options) { o.DataConverter = x }\n",
		"payload codec":         "package w\nvar _ = converter.NewCodecDataConverter(a, b)\n",
		"composite converter":   "package w\nvar _ = converter.NewCompositeDataConverter(a)\n",
		"payload converter":     "package w\ntype C struct{}\nfunc (C) ToPayload(v any) {}\nfunc (C) FromPayload(p any) {}\n",
		"data converter type":   "package w\ntype C struct{}\nfunc (*C) ToPayloads(v any) {}\nfunc (*C) FromPayloads(p any) {}\n",
		"codec type":            "package w\ntype C struct{}\nfunc (C) Encode(p []*common.Payload) {}\nfunc (C) Decode(p []*common.Payload) {}\n",
	}
	for name, src := range found {
		root := t.TempDir()
		write(t, root, "go.mod", "module example.com/m\n")
		write(t, root, "w/w.go", src)
		hits := CustomConverterUse(root)
		if len(hits) != 1 || !strings.HasPrefix(hits[0], "w/w.go:") {
			t.Errorf("%s: hits = %v", name, hits)
		}
	}
	notFound := map[string]string{
		"use number":           "package w\nfunc f(d *json.Decoder) { d.UseNumber() }\n",
		"json number":          "package w\nvar _ json.Number\n",
		"in a comment":         "package w\n// DataConverter: x  NewCodecDataConverter\nfunc F() {}\n",
		"in a string":          "package w\nvar s = \"DataConverter\"\n",
		"half an interface":    "package w\ntype C struct{}\nfunc (C) ToPayload(v any) {}\n",
		"Encode and Decode":    "package w\ntype C struct{}\nfunc (C) Encode(s string) {}\nfunc (C) Decode(s string) {}\n",
		"methods of two types": "package w\ntype A struct{}\ntype B struct{}\nfunc (A) ToPayload(v any) {}\nfunc (B) FromPayload(p any) {}\n",
	}
	for name, src := range notFound {
		root := t.TempDir()
		write(t, root, "go.mod", "module example.com/m\n")
		write(t, root, "w/w.go", src)
		if hits := CustomConverterUse(root); len(hits) != 0 {
			t.Errorf("%s: hits = %v, want none", name, hits)
		}
	}
	clean := t.TempDir()
	write(t, clean, "go.mod", "module example.com/m\n")
	write(t, clean, "w/w.go", "package w\nfunc F() {}\n")
	write(t, clean, "vendor/x/x.go", "package x\nvar _ = Options{DataConverter: x}\n")
	write(t, clean, "testdata/y.go", "package y\nvar _ = Options{DataConverter: x}\n")
	write(t, clean, "w/notes.txt", "DataConverter\n")
	if hits := CustomConverterUse(clean); len(hits) != 0 {
		t.Errorf("a clean module: hits = %v", hits)
	}
}
