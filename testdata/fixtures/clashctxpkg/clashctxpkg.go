// Package clashctxpkg declares, at package level, the name pathkit gives a
// context parameter written "_". Recording must say so, not produce a copy
// that does not compile.
package clashctxpkg

import "go.temporal.io/sdk/workflow"

var pathkitCtx = 5

func Clash(_ workflow.Context, n int) error {
	if n > pathkitCtx {
		return nil
	}
	return nil
}
