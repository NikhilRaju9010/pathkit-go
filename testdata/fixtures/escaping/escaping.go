// Package escaping has a workflow whose code is full of characters that
// mean something in HTML (<, >, &, both kinds of quotes), to prove the
// HTML report escapes everything that comes from user code (M8).
package escaping

import (
	"errors"

	"go.temporal.io/sdk/workflow"
)

type Input struct {
	Note, Kind string
	A, B       int
}

func EscapeWorkflow(ctx workflow.Context, in Input) error {
	if in.Note == "<b>\"x\" & 'y'</b>" && in.A < in.B {
		return errors.New("<script>alert(1)</script>")
	}
	switch in.Kind {
	case "a<b", "c>d":
		return nil
	case "&amp;":
	}
	var s string
	sel := workflow.NewSelector(ctx)
	sel.AddReceive(workflow.GetSignalChannel(ctx, "sig<&>\"'"), func(c workflow.ReceiveChannel, more bool) {
		c.Receive(ctx, &s)
	})
	sel.AddDefault(func() {})
	sel.Select(ctx)
	return nil
}

// OtherWorkflow is excluded by the test's config, whose reason is HTML.
func OtherWorkflow(ctx workflow.Context) error { return nil }
