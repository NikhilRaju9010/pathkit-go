// Package unnamedparams has a workflow with no parameter names at all.
// PathKit cannot record it (a name cannot be added to one parameter of a
// list without naming all), and says so.
package unnamedparams

import "go.temporal.io/sdk/workflow"

func NoNames(workflow.Context, int) error { return nil }
