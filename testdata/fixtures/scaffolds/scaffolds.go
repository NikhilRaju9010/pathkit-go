// Package scaffolds has workflows whose decisions read the input, what an
// activity gives back and what a signal carries, to test that a scaffold sets
// those values so that the decisions go the way a scenario needs. All names
// are made up.
package scaffolds

import (
	"context"
	"time"

	"go.temporal.io/sdk/workflow"
)

// Request is a made-up request.
type Request struct {
	Type     string
	Note     *string
	Items    []string
	Priority int
	Payload  interface{}
}

// Verdict is what the Assess activity gives back.
type Verdict struct {
	Status string
	Score  int
}

// Decision is what the "decide" signal carries.
type Decision struct {
	Approved bool
	By       string
}

// Handle, Audit and Notify are activities that do nothing.
func Handle(_ context.Context, _ Request) error { return nil }
func Audit(_ context.Context, _ Request) error  { return nil }
func Notify(_ context.Context, _ Request) error { return nil }

// Assess is an activity that gives a verdict.
func Assess(_ context.Context, _ Request) (Verdict, error) { return Verdict{}, nil }

func opts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
}

// Route reads the input: a text, a pointer, a list, a number.
func Route(ctx workflow.Context, req Request) error {
	ctx = opts(ctx)
	if req.Type == "RESET" {
		return workflow.ExecuteActivity(ctx, Handle, req).Get(ctx, nil)
	}
	if req.Note != nil && len(req.Items) == 0 {
		return workflow.ExecuteActivity(ctx, Audit, req).Get(ctx, nil)
	}
	if req.Priority >= 5 && req.Priority < 10 && req.Type != "A" {
		return workflow.ExecuteActivity(ctx, Notify, req).Get(ctx, nil)
	}
	return nil
}

// Review reads fields of what an activity gives back.
func Review(ctx workflow.Context, req Request) error {
	ctx = opts(ctx)
	var v Verdict
	if err := workflow.ExecuteActivity(ctx, Assess, req).Get(ctx, &v); err != nil {
		return err
	}
	if v.Status == "APPROVED" {
		return workflow.ExecuteActivity(ctx, Handle, req).Get(ctx, nil)
	} else if v.Score > 50 {
		return workflow.ExecuteActivity(ctx, Audit, req).Get(ctx, nil)
	}
	return nil
}

// Kinds switches on the type of an interface value in the input.
func Kinds(ctx workflow.Context, req Request) error {
	ctx = opts(ctx)
	switch req.Payload.(type) {
	case string:
		return workflow.ExecuteActivity(ctx, Handle, req).Get(ctx, nil)
	case map[string]interface{}:
		return workflow.ExecuteActivity(ctx, Audit, req).Get(ctx, nil)
	case float64:
		return workflow.ExecuteActivity(ctx, Notify, req).Get(ctx, nil)
	}
	return nil
}

// Wait reads a field of what a signal carries.
func Wait(ctx workflow.Context, req Request) error {
	ctx = opts(ctx)
	ch := workflow.GetSignalChannel(ctx, "decide")
	sel := workflow.NewSelector(ctx)
	var d Decision
	sel.AddReceive(ch, func(c workflow.ReceiveChannel, more bool) { c.Receive(ctx, &d) })
	sel.AddFuture(workflow.NewTimer(ctx, time.Hour), func(f workflow.Future) {})
	sel.Select(ctx)
	if d.Approved {
		return workflow.ExecuteActivity(ctx, Handle, req).Get(ctx, nil)
	}
	return nil
}

// Describe is a method: a decision on its result is not a field of the input.
func (r Request) Describe() string { return r.Type + "!" }

// Opaque decides on the result of a method, which PathKit cannot turn into a
// value for the input: the scaffold must say so in a TODO.
func Opaque(ctx workflow.Context, req Request) error {
	ctx = opts(ctx)
	if req.Describe() == "RESET!" {
		return workflow.ExecuteActivity(ctx, Handle, req).Get(ctx, nil)
	}
	return nil
}
