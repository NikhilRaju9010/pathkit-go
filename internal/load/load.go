// Package load reads Go packages with full type information, the same way
// "go build" sees them, for the command-line argument a user passes.
package load

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Result is what a load produced.
type Result struct {
	Packages []*packages.Package
	// File is the absolute path of the one file the user named, or "" when
	// they named a folder or a "..." pattern.
	File string
}

const mode = packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
	packages.NeedImports | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax

// Load accepts a .go file, a package folder, or "<folder>/..." for every
// package under a folder. Packages are loaded from the named folder, so the
// go.mod that applies there is used (for example testdata/pilot's own).
func Load(arg string) (*Result, error) {
	dir, pattern, file, err := Resolve(arg)
	if err != nil {
		return nil, err
	}
	cfg := &packages.Config{Mode: mode, Dir: dir}
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		return nil, fmt.Errorf("could not load %s: %v", arg, err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no Go packages found in %s", arg)
	}
	for _, p := range pkgs {
		if len(p.Errors) > 0 {
			return nil, fmt.Errorf("package does not compile: %s", p.Errors[0])
		}
	}
	return &Result{Packages: pkgs, File: file}, nil
}

// Resolve turns a command-line argument into the folder to run Go tools
// from, the package pattern to use there ("." or "./..."), and the absolute
// file path when a single .go file was named.
func Resolve(arg string) (dir, pattern, file string, err error) {
	if arg == "..." || strings.HasSuffix(arg, "/...") {
		dir = strings.TrimSuffix(strings.TrimSuffix(arg, "..."), "/")
		if dir == "" {
			dir = "."
		}
		if !isDir(dir) {
			//lint:ignore ST1005 wording matches the setup guide ("Workflow file not found", "Directory not found")
			return "", "", "", fmt.Errorf("Directory not found: %s", dir)
		}
		return dir, "./...", "", nil
	}

	info, statErr := os.Stat(arg)
	switch {
	case errors.Is(statErr, os.ErrNotExist) && strings.HasSuffix(arg, ".go"):
		//lint:ignore ST1005 wording matches the setup guide ("Workflow file not found", "Directory not found")
		return "", "", "", fmt.Errorf("Workflow file not found: %s", arg)
	case errors.Is(statErr, os.ErrNotExist):
		//lint:ignore ST1005 wording matches the setup guide ("Workflow file not found", "Directory not found")
		return "", "", "", fmt.Errorf("Directory not found: %s", arg)
	case statErr != nil:
		return "", "", "", statErr
	case info.IsDir():
		return arg, ".", "", nil
	case !strings.HasSuffix(arg, ".go"):
		return "", "", "", fmt.Errorf("not a Go file: %s", arg)
	}
	abs, err := filepath.Abs(arg)
	if err != nil {
		return "", "", "", err
	}
	return filepath.Dir(abs), ".", abs, nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// SameFile reports whether two paths name the same file on disk.
func SameFile(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(ia, ib)
}
