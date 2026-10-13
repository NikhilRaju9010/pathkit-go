// Package closedchannelleak holds near-misses for the closed-signal-channel
// detector found by the independent review: ANOTHER handle of the same
// named signal channel is turned into a Channel and closed, so a receive
// returns more == false. Every closed side here is reachable and must not
// be called impossible.
package closedchannelleak

import "go.temporal.io/sdk/workflow"

func closeIt(c workflow.ReceiveChannel) { c.(workflow.Channel).Close() }

// The close goes through one handle, the receive through another.
func SameNameClosed(ctx workflow.Context) error {
	a := workflow.GetSignalChannel(ctx, "s")
	a.(workflow.Channel).Close()
	ch := workflow.GetSignalChannel(ctx, "s")
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

// The same, with the assertion applied to the call directly.
func InlineClose(ctx workflow.Context) error {
	workflow.GetSignalChannel(ctx, "s2").(workflow.Channel).Close()
	var ch workflow.ReceiveChannel
	ch = workflow.GetSignalChannel(ctx, "s2")
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

// A helper does the closing.
func HelperClosed(ctx workflow.Context) error {
	closeIt(workflow.GetSignalChannel(ctx, "s3"))
	ch := workflow.GetSignalChannel(ctx, "s3")
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

// A different signal name than the one closed above: still not claimed
// (the helper takes any channel).
func OtherName(ctx workflow.Context) error {
	ch := workflow.GetSignalChannel(ctx, "unrelated")
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}
