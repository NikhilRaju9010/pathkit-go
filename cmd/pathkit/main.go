// Command pathkit finds the execution paths in Temporal Go workflows and
// reports which ones your tests actually run.
package main

import (
	"os"

	"github.com/NikhilRaju9010/pathkit-go/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
