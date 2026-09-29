package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/NikhilRaju9010/pathkit-go/internal/htmlreport"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

// The HTML files (CLAUDE.md D12): report and analyze write separate files,
// each complete from its own run, so neither overwrites the other.
const (
	defaultReportHTML   = ".pathkit/report.html"
	defaultAnalysisHTML = ".pathkit/analysis.html"
)

// now is the clock for the page's "Generated" time and the trend; tests
// set it, so the saved example page never changes.
var now = time.Now

// addHTMLFlag adds --html: alone it means the default file, --html=path
// names one, --html=false turns it off (over the config's "html").
func addHTMLFlag(cmd *cobra.Command, target *string, def, what string) {
	cmd.Flags().StringVar(target, "html", "", "also write "+what+" as one self-contained HTML `file` (--html alone: "+def+"; write --html=path, with =, to name one)")
	cmd.Flags().Lookup("html").NoOptDefVal = def
}

// htmlPath decides where the HTML goes: the --html flag, else (report
// only) the config's "html" (true: the default file; a path: relative to
// the config file). "" means no HTML.
func htmlPath(cmd *cobra.Command, flagValue, def string, cfg *scope.Config) string {
	def = filepath.FromSlash(def) // shown with the system's separator
	if cmd.Flags().Changed("html") {
		switch flagValue {
		case "false":
			return ""
		case "true", "":
			return def
		}
		return flagValue
	}
	if cfg == nil {
		return ""
	}
	switch h := cfg.HTML.(type) {
	case bool:
		if h {
			return def
		}
	case string:
		return cfg.Abs(h)
	}
	return ""
}

// htmlSpaceHint catches "--html out.html": with a space, the file name is
// read as the command's folder argument.
func htmlSpaceHint(cmd *cobra.Command, flagValue, def, arg string) error {
	if cmd.Flags().Changed("html") && flagValue == def && strings.HasSuffix(strings.ToLower(arg), ".html") {
		return userError("%s was read as the folder argument; to name the HTML file, write --html=%s (with =)", arg, arg)
	}
	return nil
}

// writeHTMLFile writes page to path, creating its folder.
func writeHTMLFile(path string, page []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return userError("could not write --html file: %v", err)
	}
	if err := os.WriteFile(path, page, 0o644); err != nil {
		return userError("could not write --html file: %v", err)
	}
	return nil
}

// reportHTML is report's --html step (run by finish, before --clean): it
// reads the trend next to the file (a missing or damaged one only warns),
// adds this run, writes the page, then the trend.
func reportHTML(cmd *cobra.Command, m *measured, path string) func() error {
	return func() error {
		stderr := cmd.ErrOrStderr()
		histPath := htmlreport.HistoryPath(path)
		runs, warning := htmlreport.ReadHistory(histPath)
		if warning != "" {
			fmt.Fprintf(stderr, "pathkit report: warning: %s\n", warning)
		}
		var inScope, excluded []string
		for _, w := range m.res.Workflows {
			inScope = append(inScope, w.Name)
		}
		for _, e := range m.res.Excluded {
			excluded = append(excluded, e.Name)
		}
		t := now()
		runs = htmlreport.AddRun(runs, htmlreport.Run{Time: t.UTC(), Paths: m.res.Paths, Covered: m.res.Covered,
			InScope: len(inScope), Excluded: len(excluded), Scope: htmlreport.Fingerprint(inScope, excluded)})
		page, err := htmlreport.Report(htmlreport.ReportInput{
			Header: header(t, m.sc), Result: m.res, Threshold: m.threshold, ExcludedBy: m.excludedBy, Trend: runs,
		})
		if err != nil {
			return err
		}
		if err := writeHTMLFile(path, page); err != nil {
			return err
		}
		if err := htmlreport.WriteHistory(histPath, runs); err != nil {
			fmt.Fprintf(stderr, "pathkit report: warning: could not save the trend to %s: %v\n", histPath, err)
		}
		fmt.Fprintf(stderr, "pathkit report: wrote %s\n", path)
		return nil
	}
}

func header(t time.Time, sc *scoped) htmlreport.Header {
	h := htmlreport.Header{Generated: t, Folder: load.DisplayPath(sc.target)}
	if sc.cfg != nil {
		h.Config = load.DisplayPath(sc.cfg.Path)
	}
	return h
}
