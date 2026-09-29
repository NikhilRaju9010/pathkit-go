package cli

import "github.com/spf13/cobra"

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

// atMostOne checks a command got at most one positional argument.
func atMostOne(name string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			return userError("expected at most one %s argument, got %d", name, len(args))
		}
		return nil
	}
}
