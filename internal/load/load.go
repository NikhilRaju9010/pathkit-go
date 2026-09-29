// Package load reads Go packages with full type information, the same way
// "go build" sees them, for the command-line argument a user passes.
package load

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	if err := compileErrors(pkgs); err != nil {
		return nil, err
	}
	return &Result{Packages: pkgs, File: file}, nil
}

// compileErrors names every package that doesn't compile, each with its
// first real error, all on one line (owner's decision, M7: stop, but list
// them all). A package that only imports a broken one has no errors of
// its own, so it is not listed.
func compileErrors(pkgs []*packages.Package) error {
	var broken []string
	for _, p := range pkgs {
		if len(p.Errors) > 0 {
			broken = append(broken, p.PkgPath+": "+firstError(p.Errors))
		}
	}
	sort.Strings(broken)
	switch len(broken) {
	case 0:
		return nil
	case 1:
		return fmt.Errorf("package does not compile: %s", broken[0])
	}
	return fmt.Errorf("%d packages do not compile: %s", len(broken), strings.Join(broken, "; "))
}

// firstError picks the error worth showing. go list's own error for a
// package that fails to build is a two-line "-: # <package>" summary,
// whose useful second line would be cut off (errors are one line), so a
// type or syntax error with a position is preferred. The position is shown
// with DisplayPath.
func firstError(errs []packages.Error) string {
	e := errs[0]
	for _, c := range errs {
		if c.Kind == packages.TypeError || c.Kind == packages.ParseError {
			e = c
			break
		}
	}
	pos := DisplayPath(e.Pos)
	msg := strings.Join(strings.Fields(e.Msg), " ")
	if pos == "" || pos == "-" {
		return msg
	}
	return pos + ": " + msg
}

// Resolve turns a command-line argument into the folder to run Go tools
// from, the package pattern to use there ("." or "./..."), and the absolute
// file path when a single .go file was named.
//
// "<folder>/..." may also be written with the system's own separator
// ("<folder>\..." on Windows), which is also what joining a config's
// "./..." onto its folder produces there.
func Resolve(arg string) (dir, pattern, file string, err error) {
	if folder, ok := recursive(arg); ok {
		dir = folder
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

// recursive reports whether arg is "...", "<folder>/..." or, on Windows,
// "<folder>\...", and returns the folder ("." for a bare "...").
func recursive(arg string) (folder string, ok bool) {
	if arg == "..." {
		return ".", true
	}
	for _, sep := range []string{"/", string(filepath.Separator)} {
		if rest, found := strings.CutSuffix(arg, sep+"..."); found {
			if rest == "" {
				rest = sep // "/..." is everything under the root
			}
			return rest, true
		}
	}
	return "", false
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// DisplayPath shows an absolute path relative to the current folder when
// it is inside it, else unchanged, with the system's own separators.
// Anything after the file name (such as ":8:14") is kept.
//
// The current folder may be reached through a symlink (on macOS, /var and
// /tmp are shortcuts to /private/var and /private/tmp), while Go's tools
// report the real path; so the real current folder is tried too.
func DisplayPath(p string) string {
	cwd, err := os.Getwd()
	if err != nil || !filepath.IsAbs(p) {
		return p
	}
	if rel, ok := inside(cwd, p); ok {
		return rel
	}
	if real, err := filepath.EvalSymlinks(cwd); err == nil && real != cwd {
		if rel, ok := inside(real, p); ok {
			return rel
		}
	}
	return p
}

// inside returns p relative to dir when p is inside dir.
func inside(dir, p string) (string, bool) {
	rel, err := filepath.Rel(dir, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
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
