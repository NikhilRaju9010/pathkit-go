// Package cli wires up the pathkit command line: the commands, their
// flags, and the shared "pathkit <command>: <message>" error style.
package cli

import (
	"io"

	"github.com/NikhilRaju9010/pathkit-go/internal/version"
	"github.com/spf13/cobra"
)

// Run runs pathkit with the given arguments (without the program name)
// and returns the exit code. It never calls os.Exit, so tests can call it
// directly.
func Run(args []string, stdout, stderr io.Writer) int {
	root := newRootCommand()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	cmd, err := root.ExecuteC()
	if err == nil {
		return ExitOK
	}
	io.WriteString(stderr, formatError(subcommandName(root, cmd), err))
	return exitCodeFor(err)
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:     "pathkit",
		Short:   "Find the execution paths in Temporal Go workflows, and which ones your tests run",
		Version: version.String(),
		// PathKit prints its own one-line errors (see Run); cobra's
		// automatic error print and usage dump would bury them.
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetVersionTemplate("{{.Version}}\n")

	root.AddCommand(newAnalyzeCommand(), newTestCommand(), newTracesCommand(), newCoverageCommand(), newReportCommand(),
		newCleanCommand(), newPrepareCommand())
	return root
}

// subcommandName returns "analyze", "coverage", ... for an error raised by
// a subcommand, or "" when the error belongs to pathkit itself (for
// example an unknown command).
func subcommandName(root, cmd *cobra.Command) string {
	if cmd == nil || cmd == root {
		return ""
	}
	return cmd.Name()
}
