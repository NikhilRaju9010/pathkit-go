// Package nexus_op calls a Nexus operation.
package nexus_op

import "go.temporal.io/sdk/workflow"

// Run starts an operation and waits for it.
func Run(ctx workflow.Context, input string) error {
	client := workflow.NewNexusClient("endpoint", "service")
	fut := client.ExecuteOperation(ctx, "echo", input, workflow.NexusOperationOptions{})
	var out string
	if err := fut.Get(ctx, &out); err != nil {
		return err
	}
	return nil
}
