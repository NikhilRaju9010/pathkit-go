package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
	"github.com/NikhilRaju9010/pathkit-go/internal/trace"
)

// A doctor check has a result: fine, a problem (with what to do), or only
// something worth knowing.
type checkResult struct {
	level string // "ok", "problem" or "info"
	what  string
	fix   string
}

func newDoctorCommand() *cobra.Command {
	var traces string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that PathKit is set up and its results can be trusted",
		Long: `Checks the setup in plain words and says what to do about each problem: that
you are in a Go module, that PathKit finds modules and workflows, that the
config is spelled right, that traces exist and are fresh, that no workflow was
left without a test, that the traces fit the maps, that .pathkit/ is not
committed to git, and how many places PathKit could not fully read.

Exit codes: 0 healthy, 1 problems found.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			results := runDoctor(cmd, traces)
			problems := 0
			out := cmd.OutOrStdout()
			for _, r := range results {
				switch r.level {
				case "ok":
					fmt.Fprintf(out, "ok       %s\n", r.what)
				case "info":
					fmt.Fprintf(out, "note     %s\n", r.what)
				default:
					problems++
					fmt.Fprintf(out, "PROBLEM  %s\n", r.what)
				}
				if r.fix != "" {
					fmt.Fprintf(out, "         what to do: %s\n", r.fix)
				}
			}
			if problems > 0 {
				fmt.Fprintln(out)
				return userError("%s found; fix them and run \"pathkit doctor\" again", count(problems, "problem"))
			}
			fmt.Fprintln(out, "\nThe setup is fine. Next: run \"pathkit report --summary\" and start with \"Tests to write first\" (the setup being fine says nothing about how much is covered).")
			return nil
		},
	}
	cmd.Flags().StringVar(&traces, "traces", defaultTraceDir, "folder the trace files are in (the config's \"traces\" sets another default)")
	return cmd
}

var knownKeys = []string{"packages", "traces", "workflows", "failUnder", "html", "out", "json", "noColor", "allowStale", "classification",
	"include", "exclude", "enabled", "paths", "rules", "custom_labels", "unclassified_policy", "detectors", "explained_traces", "ci",
	"engine", "stages", "assumptions", "helpers", "activity_wrappers", "direct_returns", "loops", "scenarios", "targets", "main_number",
	"baseline", "fail_under_testable", "stage_floor", "capped_basis", "tolerance", "main_floor", "max_listed", "max_states", "max_paths", "max_contexts",
	"mode", "model", "force", "default", "mark_untestable", "mark_testable", "disable"}

var unknownKey = regexp.MustCompile(`unknown key "([^"]+)"`)

// didYouMean suggests the known key closest to a misspelled one.
func didYouMean(key string) string {
	best, bestD := "", 3
	for _, k := range knownKeys {
		if d := editDistance(strings.ToLower(key), strings.ToLower(k)); d < bestD {
			best, bestD = k, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			c := 1
			if a[i-1] == b[j-1] {
				c = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+c)
		}
		prev = cur
	}
	return prev[len(b)]
}

func runDoctor(cmd *cobra.Command, traces string) []checkResult {
	var rs []checkResult
	add := func(level, what, fix string) { rs = append(rs, checkResult{level, what, fix}) }
	cwd, _ := os.Getwd()

	// 1. a module or workspace
	if root := findUp(cwd, "go.work", "go.mod"); root == "" {
		add("problem", "this folder is not inside a Go module or workspace (no go.mod or go.work above it)", "run PathKit from the folder that holds go.mod (or go.work)")
		return rs
	} else {
		add("ok", "inside a Go module or workspace ("+displayDir(cwd, root)+")", "")
	}

	// 2. the config
	cfg, cfgErr := scope.Load("", cwd)
	switch {
	case cfgErr != nil:
		fix := "fix the line the message names"
		if m := unknownKey.FindStringSubmatch(cfgErr.Error()); m != nil {
			if s := didYouMean(m[1]); s != "" {
				fix = fmt.Sprintf("%q is not a PathKit setting; did you mean %q?", m[1], s)
			} else {
				fix = fmt.Sprintf("%q is not a PathKit setting; check the spelling against SETUP-GUIDE.md", m[1])
			}
		}
		add("problem", "the config file cannot be read: "+firstLine(cfgErr.Error()), fix)
		return rs
	case cfg == nil:
		add("info", "no "+scope.FileName+" here, so PathKit uses its defaults (every package, classification off)", "\"pathkit init\" writes a starting one")
	default:
		add("ok", "config "+displayDir(cwd, cfg.Path)+" is valid (no unknown or misspelled keys)", "")
	}

	// 3. modules and workflows
	var sink bytes.Buffer
	sub := &cobra.Command{}
	sub.SetErr(&sink)
	sub.SetOut(&sink)
	opt := measureOptions{traces: traces, tracesOptional: true, noCI: true}
	opt.scope.everyPackage = true
	m, err := measure(sub, "doctor", "", opt)
	if err != nil {
		add("problem", "PathKit could not measure the workflows: "+firstLine(err.Error()), "run \"pathkit report\" to see the whole message")
		return rs
	}
	if mods := m.sc.res.Modules; len(mods) > 1 {
		add("ok", fmt.Sprintf("%d Go modules found", len(mods)), "")
	} else {
		add("ok", "one Go module found", "")
	}
	if len(m.res.Workflows) == 0 {
		add("problem", "no workflows found in scope", "a workflow is an exported function with a workflow.Context first parameter and an error last result; check the config's packages and workflows.exclude")
		return rs
	}
	add("ok", fmt.Sprintf("%s found in scope", count(len(m.res.Workflows), "workflow")), "")
	for _, n := range m.sc.notAnalyzable {
		add("problem", fmt.Sprintf("%s is in scope but PathKit cannot read it: %s", n.name, firstLine(n.err.Error())), "change the code the message names, or exclude it in the config with a reason")
	}

	// 4. traces
	counts := m.res.Counts
	switch {
	case counts.Read == 0:
		add("problem", "no traces: no test has been recorded yet", "run \"pathkit test ./...\" (plain \"go test\" records nothing)")
	default:
		add("ok", fmt.Sprintf("%s read", count(counts.Read, "trace")), "")
		if counts.Stale > 0 {
			add("problem", fmt.Sprintf("%s recorded for an older version of the code (stale)", count(counts.Stale, "trace")), "run \"pathkit test ./...\" again")
		}
		if counts.Unreadable > 0 {
			add("problem", fmt.Sprintf("%s could not be read (a trace format this PathKit does not know, or a damaged file)", count(counts.Unreadable, "trace")), "run \"pathkit clean\" and then \"pathkit test ./...\"")
		}
		if counts.Unmatched > 0 {
			add("problem", fmt.Sprintf("%s fit no path of the map", count(counts.Unmatched, "trace")), "run \"pathkit report --unmatched\" to see where each leaves the map")
		}
		if counts.Incomplete > 0 {
			add("problem", fmt.Sprintf("%s never finished (a panic, a timeout or a stopped test)", count(counts.Incomplete, "trace")), "fix the test that stops it, then run \"pathkit test ./...\" again")
		}
		if stale := newerSource(m, cwd); stale != "" {
			add("problem", "the source of "+stale+" is newer than the traces", "run \"pathkit test ./...\" again so the report reads the code as it is now")
		}
		var idle []string
		for _, w := range m.res.Workflows {
			if w.TracesCounted == 0 {
				idle = append(idle, w.Name)
			}
		}
		if len(idle) > 0 {
			sort.Strings(idle)
			add("problem", fmt.Sprintf("%s recorded nothing: %s", count(len(idle), "workflow"), shortList(idle)), "no test ran it (or its tests were skipped); write a test, or exclude the workflow in the config with a reason")
		}
		if v := trace.SchemaVersion; v > 0 && counts.Unreadable == 0 {
			add("ok", fmt.Sprintf("the traces use the trace format this PathKit reads (%d)", v), "")
		}
	}

	// 5. wrappers
	var wrappers []string
	for _, u := range m.res.Unread {
		if u.Suggestion != "" {
			wrappers = append(wrappers, u.Suggestion)
		}
	}
	if len(wrappers) > 0 {
		sort.Strings(wrappers)
		add("info", fmt.Sprintf("%s could be told about with classification.activity_wrappers: %s", count(len(wrappers), "function"), shortList(dedupe(wrappers))), "see the \"could not fully read\" panel of \"pathkit report\" for the entries to add")
	}
	if n := len(m.res.Unread); n > 0 {
		add("info", fmt.Sprintf("PathKit could not fully read %s (the \"could not fully read\" panel of \"pathkit report\"): the numbers around those places are approximate", count(n, "place")), "nothing is required; if they are calls of your own wrapper functions, tell PathKit with classification.activity_wrappers")
	} else {
		add("ok", "PathKit read everything in scope", "")
	}

	// 6. git hygiene
	if ign := gitIgnoreProblem(cwd); ign != "" {
		add("problem", ign, "add a line \".pathkit/\" to .gitignore")
	}

	// 7. the safe runner
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("unshare"); err == nil {
			add("info", "tests can run without a network: \"pathkit test --no-network ./...\"", "use it for tests that might reach a real service")
		} else {
			add("info", "\"pathkit test --no-network\" is not available here (the unshare program was not found)", "")
		}
	}
	return rs
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func shortList(xs []string) string {
	if len(xs) <= 5 {
		return strings.Join(xs, ", ")
	}
	return strings.Join(xs[:5], ", ") + fmt.Sprintf(" and %d more", len(xs)-5)
}

func dedupe(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// findUp returns the folder that holds one of the files, looking in dir and
// above it.
func findUp(dir string, names ...string) string {
	for {
		for _, n := range names {
			if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// newerSource names a workflow whose source file changed after the newest trace
// of the run was written ("" when none did).
func newerSource(m *measured, cwd string) string {
	var newest int64
	for _, p := range m.tracePaths {
		if st, err := os.Stat(p); err == nil && st.ModTime().Unix() > newest {
			newest = st.ModTime().Unix()
		}
	}
	if newest == 0 {
		return ""
	}
	var names []string
	for _, w := range m.res.Workflows {
		file := w.File
		if !filepath.IsAbs(file) {
			file = filepath.Join(cwd, file)
		}
		if st, err := os.Stat(file); err == nil && st.ModTime().Unix() > newest {
			names = append(names, w.Name)
		}
	}
	sort.Strings(names)
	return shortList(names)
}

// gitIgnoreProblem says so when the folder is in a git repository and
// .pathkit/ (traces, overlay, reports) is not ignored.
func gitIgnoreProblem(cwd string) string {
	if _, err := exec.LookPath("git"); err != nil {
		return ""
	}
	inside := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	inside.Dir = cwd
	if out, err := inside.Output(); err != nil || strings.TrimSpace(string(out)) != "true" {
		return ""
	}
	check := exec.Command("git", "check-ignore", "-q", filepath.Join(".pathkit", "traces", "x.trace.json"))
	check.Dir = cwd
	if err := check.Run(); err != nil {
		return ".pathkit/ (traces, the overlay and the reports) is not ignored by git"
	}
	return ""
}
