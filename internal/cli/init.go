package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

// initDefaults is what "pathkit init" writes: every package, classification on
// (so the buckets and the CI check work), the default scenario list size and
// the default target for every workflow's main number.
const initDefaults = `{
  "packages": ["./..."],
  "classification": {
    "enabled": true,
    "scenarios": {"max_listed": 500},
    "targets": {"default": 80}
  }
}
`

func newInitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a starting " + scope.FileName + " and list the workflows PathKit finds",
		Long: `Writes a ` + scope.FileName + ` with sensible defaults (every package in scope,
classification on, the default scenario list size and a target of 80% for each
workflow's main number) in the current folder, but only if there is none. If a
config already exists it is not changed: PathKit prints the settings it would
add. Either way it lists the Go modules and workflows it finds, so you can see
what is in scope before you run "pathkit test".`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return runInit(cmd) },
	}
	return cmd
}

func runInit(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	path := filepath.Join(cwd, scope.FileName)
	existing, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		fmt.Fprintf(out, "%s already exists, so PathKit did not change it.\n", scope.FileName)
		if adds := missingDefaults(existing); len(adds) > 0 {
			fmt.Fprintln(out, "Settings PathKit suggests adding:")
			for _, a := range adds {
				fmt.Fprintln(out, "+ "+a)
			}
		} else {
			fmt.Fprintln(out, "It already has every setting PathKit would add.")
		}
	case os.IsNotExist(readErr):
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return userError("could not write %s: %v", path, err)
		}
		if _, err := f.WriteString(initDefaults); err != nil {
			_ = f.Close()
			return userError("could not write %s: %v", path, err)
		}
		if err := f.Close(); err != nil {
			return userError("could not write %s: %v", path, err)
		}
		fmt.Fprintf(out, "Wrote %s:\n%s\n", scope.FileName, initDefaults)
	default:
		return userError("could not read %s: %v", path, readErr)
	}

	// what is in scope: a config of this folder is read like any other command does
	sc, err := loadScoped("init", "", scopeFlags{everyPackage: true}, cmd.ErrOrStderr())
	if err != nil {
		fmt.Fprintf(out, "PathKit could not list the workflows yet: %v\n", err)
		return nil
	}
	mods := sc.res.Modules
	if len(mods) == 0 {
		fmt.Fprintln(out, "Modules: this folder (one Go module)")
	} else {
		fmt.Fprintf(out, "Modules: %d\n", len(mods))
		for _, m := range mods {
			fmt.Fprintf(out, "  %s\n", displayDir(cwd, m))
		}
	}
	var names []string
	for _, r := range sc.mapped {
		names = append(names, r.wf.Name+" ("+displayDir(cwd, r.wf.Filename)+")")
	}
	sort.Strings(names)
	fmt.Fprintf(out, "Workflows PathKit can read: %d\n", len(names))
	for _, n := range names {
		fmt.Fprintln(out, "  "+n)
	}
	for _, n := range sc.notAnalyzable {
		fmt.Fprintf(out, "  %s: in scope but PathKit cannot read it yet (%v)\n", n.name, n.err)
	}
	for _, e := range sc.excluded {
		fmt.Fprintf(out, "  %s: left out by the scope (%s)\n", e.Name, e.Reason)
	}
	if len(names) == 0 {
		fmt.Fprintln(out, "No workflows found here. A workflow is an exported function whose first parameter is a workflow.Context and whose last result is an error; run this from the folder that holds go.mod.")
		return nil
	}
	fmt.Fprintln(out, "Next: pathkit test ./...   then   pathkit report --summary   (pathkit doctor checks the setup)")
	return nil
}

// displayDir writes a path relative to base when it is below it.
func displayDir(base, p string) string {
	if rel, err := filepath.Rel(base, p); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(p)
}

// missingDefaults lists, as dotted paths with their values, the settings of
// initDefaults the existing config does not have.
func missingDefaults(existing []byte) []string {
	var have, want map[string]any
	if err := json.Unmarshal(existing, &have); err != nil {
		return []string{"(the file is not valid JSON, so PathKit cannot compare it; \"pathkit doctor\" says what is wrong)"}
	}
	_ = json.Unmarshal([]byte(initDefaults), &want)
	var out []string
	var walk func(prefix string, w, h map[string]any)
	walk = func(prefix string, w, h map[string]any) {
		keys := make([]string, 0, len(w))
		for k := range w {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			wv := w[k]
			hv, ok := h[k]
			if !ok {
				raw, _ := json.Marshal(wv)
				out = append(out, prefix+k+": "+string(raw))
				continue
			}
			if wm, isMap := wv.(map[string]any); isMap {
				if hm, ok := hv.(map[string]any); ok {
					walk(prefix+k+".", wm, hm)
				}
			}
		}
	}
	walk("", want, have)
	return out
}
