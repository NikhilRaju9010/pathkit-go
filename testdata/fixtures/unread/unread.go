// Package unread has calls that take a workflow.Context and that PathKit
// does not follow into: each one is listed in the "could not fully read" panel.
package unread

import (
	"go.temporal.io/sdk/workflow"

	"example.com/ext"
	"example.com/fixtures/unread/side"
)

// Notifier can be anything.
type Notifier interface {
	Send(ctx workflow.Context, id string) error
}

// ViaInterface calls a method through an interface.
func ViaInterface(ctx workflow.Context, n Notifier) error {
	if err := n.Send(ctx, "a"); err != nil {
		return err
	}
	return nil
}

// ViaFuncValue calls a function that is a parameter.
func ViaFuncValue(ctx workflow.Context, do func(workflow.Context) error) error {
	return do(ctx)
}

// OtherPackage calls a function of another package of this module.
func OtherPackage(ctx workflow.Context) error {
	return side.Audit(ctx, "x")
}

// OtherModule calls a function from a library.
func OtherModule(ctx workflow.Context) error {
	return ext.Notify(ctx, "x")
}

// ByName starts an activity whose name is computed.
func ByName(ctx workflow.Context, name string) error {
	if err := workflow.ExecuteActivity(ctx, name).Get(ctx, nil); err != nil {
		return err
	}
	return nil
}

// ByConstName starts an activity by a fixed name: this is read fine.
func ByConstName(ctx workflow.Context) error {
	if err := workflow.ExecuteActivity(ctx, "Charge").Get(ctx, nil); err != nil {
		return err
	}
	return nil
}

func quiet(ctx workflow.Context) {
	_ = ctx
}

// NoWorkflowWork calls a helper that never touches the workflow API.
func NoWorkflowWork(ctx workflow.Context) error {
	quiet(ctx)
	return nil
}
