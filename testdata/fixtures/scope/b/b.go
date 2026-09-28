// Package b has a workflow named Run; so does its sibling, which makes
// the short name "Run" ambiguous in a scope config.
package b

import "go.temporal.io/sdk/workflow"

func Run(ctx workflow.Context) error { return nil }
