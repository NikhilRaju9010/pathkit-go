package load

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// customConverterWords are the names that show a Go module sets its own
// Temporal data converter, payload codec or number decoding. The text of any
// Go file in the module is searched (comments count: it is safer to switch
// a rule off than to trust it wrongly).
var customConverterWords = []string{
	"DataConverter", "PayloadConverter", "PayloadCodec", "NewCodecDataConverter",
	"NewCompositeDataConverter", "converter.New", "UseNumber", "json.Number",
}

// ModuleRoot returns the folder with the go.mod that applies to path ("" when
// there is none).
func ModuleRoot(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	if info, err := os.Stat(abs); err == nil && !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	for d := abs; ; d = filepath.Dir(d) {
		if info, err := os.Stat(filepath.Join(d, "go.mod")); err == nil && !info.IsDir() {
			return d
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}

// CustomConverterUse lists (at most five) places in the module under root
// that mention a custom data converter, payload codec or JSON number
// decoding, as "file.go:12: the line". Empty means none found. Folders named
// vendor, testdata, node_modules, .git and .pathkit, symbolic links to
// folders, and files that cannot be read are skipped.
func CustomConverterUse(root string) []string {
	var hits []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if len(hits) >= 5 {
			return filepath.SkipAll
		}
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", ".git", ".pathkit", "node_modules", "testdata":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for n, line := range strings.Split(string(data), "\n") {
			for _, w := range customConverterWords {
				if strings.Contains(line, w) {
					rel, _ := filepath.Rel(root, path)
					hits = append(hits, fmt.Sprintf("%s:%d: %s", filepath.ToSlash(rel), n+1, strings.TrimSpace(line)))
					return nil
				}
			}
		}
		return nil
	})
	return hits
}
