package instrument

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// generatedCopy matches the names WriteOverlay gives its copies.
var generatedCopy = regexp.MustCompile(`^[0-9]{3}_.+\.go$`)

// WriteOverlay writes the generated files into overlayDir and an
// overlay.json that tells "go test" to use them. It returns the absolute
// path of overlay.json.
//
// Before writing, it deletes the files an earlier run generated there
// (overlay.json and the "NNN_<name>.go" copies) and nothing else, so no
// other file in that folder (a report-history.json, say) is ever lost.
func WriteOverlay(res *Result, overlayDir string) (string, error) {
	abs, err := filepath.Abs(overlayDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() && (e.Name() == "overlay.json" || generatedCopy.MatchString(e.Name())) {
			if err := os.Remove(filepath.Join(abs, e.Name())); err != nil {
				return "", err
			}
		}
	}

	var real []string
	for path := range res.Files {
		real = append(real, path)
	}
	sort.Strings(real)
	replace := map[string]string{}
	for i, path := range real {
		copyPath := filepath.Join(abs, fmt.Sprintf("%03d_%s", i, filepath.Base(path)))
		if err := os.WriteFile(copyPath, res.Files[path], 0o644); err != nil {
			return "", err
		}
		replace[path] = copyPath
	}

	data, err := json.MarshalIndent(map[string]any{"Replace": replace}, "", "  ")
	if err != nil {
		return "", err
	}
	overlayJSON := filepath.Join(abs, "overlay.json")
	return overlayJSON, os.WriteFile(overlayJSON, append(data, '\n'), 0o644)
}
