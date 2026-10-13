// Package clashctx uses the name pathkit gives an unnamed context
// parameter, so recording it must stop with a clear error.
package clashctx

import "go.temporal.io/sdk/workflow"

func Clash(_ workflow.Context) error {
	pathkitCtx := 1
	if pathkitCtx > 0 {
		return nil
	}
	return nil
}
