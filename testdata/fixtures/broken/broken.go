// Package broken does not compile on purpose (a type error, so the syntax
// is still valid and gofmt is happy). PathKit must report it cleanly.
package broken

import "go.temporal.io/sdk/workflow"

func BrokenWorkflow(ctx workflow.Context) (string, error) {
	var n int = "not a number"
	return "", nil
}
