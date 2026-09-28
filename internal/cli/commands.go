package cli

import "github.com/spf13/cobra"

// coverage and report exist from M0 so their argument checks and error
// style are tested from the start. Their real work arrives in the
// milestone named in each "not implemented yet" message. (analyze is real
// since M2: analyze.go.)

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
