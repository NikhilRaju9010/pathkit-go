package instrument

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ModuleRoot is the folder above dir (or dir itself) that holds go.work or
// go.mod, the highest such folder when there are several; "" when there is
// none. PathKit writes and reads its overlay and trace folders only inside it.
func ModuleRoot(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	root := ""
	for d := abs; ; d = filepath.Dir(d) {
		for _, f := range []string{"go.work", "go.mod"} {
			if info, err := os.Stat(filepath.Join(d, f)); err == nil && !info.IsDir() {
				root = d
			}
		}
		if filepath.Dir(d) == d {
			return root
		}
	}
}

// inside reports whether path is root or below it, comparing real locations
// (links followed) for the part that exists. A path that does not exist yet
// is judged by its nearest existing parent.
func inside(root, path string) bool {
	r := realPath(root)
	p := realPath(path)
	rel, err := filepath.Rel(r, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func realPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	var rest []string
	for d := abs; ; d = filepath.Dir(d) {
		if real, err := filepath.EvalSymlinks(d); err == nil {
			for i := len(rest) - 1; i >= 0; i-- {
				real = filepath.Join(real, rest[i])
			}
			return real
		}
		rest = append(rest, filepath.Base(d))
		if filepath.Dir(d) == d {
			return abs
		}
	}
}

// CheckOverlayDir refuses an overlay folder that would make PathKit read or
// write outside the module root: the folder (or a link inside its path) that
// leads out, or an overlay.json left there, say by copying a project's
// .pathkit folder from another machine, that names files outside the root.
// PathKit deletes and rewrites the files in that folder, so it must be sure
// they are its own.
func CheckOverlayDir(overlayDir, root string) error {
	err, _ := checkOverlayDir(overlayDir, root)
	return err
}

// StaleOverlay says whether the overlay folder holds an overlay.json that names
// files outside the module root (copied from another folder or machine), with
// a plain sentence; "" when it is fine or not there. Only this case is safe to
// delete: the folder is inside the root, and PathKit rewrites its files anyway.
func StaleOverlay(overlayDir, root string) string {
	if err, stale := checkOverlayDir(overlayDir, root); err != nil && stale {
		return err.Error()
	}
	return ""
}

// checkOverlayDir is CheckOverlayDir; stale is true for the overlay.json that
// names files outside the root (not for a folder that itself leads out).
func checkOverlayDir(overlayDir, root string) (err error, stale bool) {
	if root == "" {
		return nil, false
	}
	abs, _ := filepath.Abs(overlayDir)
	if !inside(root, abs) {
		return fmt.Errorf("the overlay folder %s is not inside the module folder %s (a link may lead out of it), so PathKit will not write there; delete %s and run the command again", abs, root, abs), false
	}
	data, err := os.ReadFile(filepath.Join(abs, "overlay.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		return fmt.Errorf("could not read %s: %v", filepath.Join(abs, "overlay.json"), err), false
	}
	var o struct{ Replace map[string]string }
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, false // not ours: WriteOverlay replaces it
	}
	for from, to := range o.Replace {
		for _, p := range []string{from, to} {
			if p != "" && filepath.IsAbs(p) && !inside(root, p) {
				return fmt.Errorf("the overlay in %s names %s, which is outside the module folder %s (was the .pathkit folder copied from another project or machine?); PathKit will not use it. Delete %s and run the command again", abs, p, root, abs), true
			}
		}
	}
	return nil, false
}
