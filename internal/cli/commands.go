package cli

import "github.com/spf13/cobra"

// The three commands exist from M0 so their argument checks and error
// style are tested from the start. Their real work arrives in the
// milestone named in each "not implemented yet" message.

func newAnalyzeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analyze <file>",
		Short: "List every possible path through the workflows in a file",
		Args:  exactlyOne("<file>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented("M2")
		},
	}
	// Placeholder: registered now so tests can prove that a flag written
	// after the file name is still read. Implemented in M2.
	cmd.Flags().Bool("summary", false, "only print the workflow name and total path count (coming in M2)")
	return cmd
}

func newCoverageCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "coverage <file> --traces <dir>",
		Short: "Show which paths of one workflow your tests ran",
		Args:  exactlyOne("<file>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireTraces(cmd); err != nil {
				return err
			}
			return notImplemented("M6")
		},
	}
	cmd.Flags().String("traces", "", "folder of recorded trace files")
	return cmd
}

func newReportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report <dir> --traces <dir>",
		Short: "Show path coverage for every workflow in a project",
		Args:  exactlyOne("<dir>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireTraces(cmd); err != nil {
				return err
			}
			return notImplemented("M7")
		},
	}
	cmd.Flags().String("traces", "", "folder of recorded trace files")
	return cmd
}

// exactlyOne checks a command got exactly one positional argument, with
// PathKit's own wording ("missing <file> argument").
func exactlyOne(name string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		switch {
		case len(args) == 0:
			return userError("missing %s argument", name)
		case len(args) > 1:
			return userError("expected one %s argument, got %d", name, len(args))
		}
		return nil
	}
}

func requireTraces(cmd *cobra.Command) error {
	if v, _ := cmd.Flags().GetString("traces"); v == "" {
		return userError("missing required --traces <dir> argument")
	}
	return nil
}

func notImplemented(milestone string) error {
	return userError("not implemented yet (planned for %s)", milestone)
}
