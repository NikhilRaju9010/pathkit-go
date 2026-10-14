// Package closedchannel holds analysis fixtures for the closed-signal-channel
// detector: shapes that must be read as "the channel is closed"
// and near-misses that must not.
package closedchannel

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

const orders = "orders"

// ---- catch: a signal channel's "more" result, tested for "closed" ----

func CatchAddReceive(ctx workflow.Context) error {
	sel := workflow.NewSelector(ctx)
	ch := workflow.GetSignalChannel(ctx, "cb")
	sel.AddReceive(ch, func(c workflow.ReceiveChannel, more bool) {
		if !more {
			return
		}
		var s string
		c.Receive(ctx, &s)
	})
	sel.Select(ctx)
	return nil
}

func CatchReceive(ctx workflow.Context) error {
	ch := workflow.GetSignalChannel(ctx, orders) // a constant name
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

func CatchDirectCall(ctx workflow.Context) error {
	var v string
	more := workflow.GetSignalChannel(ctx, "direct").Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

func CatchReceiveWithTimeout(ctx workflow.Context) error {
	ch := workflow.GetSignalChannel(ctx, "rwt")
	var v string
	ok, more := ch.ReceiveWithTimeout(ctx, time.Second, &v)
	_ = ok
	if !more {
		return nil
	}
	return nil
}

func CatchReceiveAsyncMore(ctx workflow.Context) error {
	ch := workflow.GetSignalChannel(ctx, "async")
	var v string
	_, more := ch.ReceiveAsyncWithMoreFlag(&v)
	if !more {
		return nil
	}
	return nil
}

func CatchWithOptions(ctx workflow.Context) error {
	ch := workflow.GetSignalChannelWithOptions(ctx, "opts", workflow.SignalChannelOptions{})
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

func CatchEqFalse(ctx workflow.Context) error {
	ch := workflow.GetSignalChannel(ctx, "eq")
	var v string
	more := ch.Receive(ctx, &v)
	if more == false {
		return nil
	}
	return nil
}

func CatchFalseEq(ctx workflow.Context) error {
	ch := workflow.GetSignalChannel(ctx, "feq")
	var v string
	more := ch.Receive(ctx, &v)
	if false == more {
		return nil
	}
	return nil
}

func CatchNeTrue(ctx workflow.Context) error {
	ch := workflow.GetSignalChannel(ctx, "ne")
	var v string
	more := ch.Receive(ctx, &v)
	if more != true {
		return nil
	}
	return nil
}

// "if more { ... } else { closed }": the closed side is the false exit.
func CatchElseSide(ctx workflow.Context) error {
	ch := workflow.GetSignalChannel(ctx, "else")
	var v string
	more := ch.Receive(ctx, &v)
	if more {
		return nil
	} else {
		return workflow.Sleep(ctx, time.Second)
	}
}

func CatchInitStatement(ctx workflow.Context) error {
	ch := workflow.GetSignalChannel(ctx, "init")
	var v string
	if _, more := ch.ReceiveAsyncWithMoreFlag(&v); !more {
		return nil
	}
	return nil
}

// ---- near-misses: stay testable (own channel) or needs-review (unknown) ----

// The workflow makes the channel and closes it itself: reachable.
func OwnClosed(ctx workflow.Context) error {
	ch := workflow.NewChannel(ctx)
	workflow.Go(ctx, func(ctx workflow.Context) { ch.Close() })
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

func OwnBuffered(ctx workflow.Context) error {
	ch := workflow.NewBufferedChannel(ctx, 1)
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

func OwnNamed(ctx workflow.Context) error {
	ch := workflow.NewNamedChannel(ctx, "mine")
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

func OwnReceiveWithTimeout(ctx workflow.Context) error {
	ch := workflow.NewChannel(ctx)
	var v string
	_, more := ch.ReceiveWithTimeout(ctx, time.Second, &v)
	if !more {
		return nil
	}
	return nil
}

func OwnAddReceive(ctx workflow.Context) error {
	sel := workflow.NewSelector(ctx)
	ch := workflow.NewChannel(ctx)
	sel.AddReceive(ch, func(c workflow.ReceiveChannel, more bool) {
		if !more {
			return
		}
	})
	sel.Select(ctx)
	return nil
}

// A channel given to the workflow as a parameter: PathKit cannot see where
// it came from.
func ParamChannel(ctx workflow.Context, ch workflow.ReceiveChannel) error {
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

func makeChannel(ctx workflow.Context) workflow.ReceiveChannel {
	return workflow.GetSignalChannel(ctx, "helper")
}

// A channel returned by a helper function.
func HelperChannel(ctx workflow.Context) error {
	ch := makeChannel(ctx)
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

// Assigned twice (a signal channel, then the workflow's own).
func TwiceAssigned(ctx workflow.Context) error {
	var ch workflow.ReceiveChannel = workflow.GetSignalChannel(ctx, "twice")
	ch = workflow.NewChannel(ctx)
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

// Declared first, set later in a branch.
func SetInABranch(ctx workflow.Context, flag bool) error {
	var ch workflow.ReceiveChannel
	if flag {
		ch = workflow.GetSignalChannel(ctx, "branch")
	} else {
		ch = workflow.NewChannel(ctx)
	}
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

type holder struct{ ch workflow.ReceiveChannel }

// A channel kept in a struct field.
func FieldChannel(ctx workflow.Context, h holder) error {
	var v string
	more := h.ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

// The channel is made outside the callback that uses it.
func ClosureChannel(ctx workflow.Context) error {
	ch := workflow.GetSignalChannel(ctx, "closure")
	sel := workflow.NewSelector(ctx)
	sel.AddFuture(workflow.NewTimer(ctx, time.Second), func(f workflow.Future) {
		var v string
		more := ch.Receive(ctx, &v)
		if !more {
			return
		}
	})
	sel.Select(ctx)
	return nil
}

// A channel taken from a slice.
func ElementChannel(ctx workflow.Context) error {
	chs := []workflow.ReceiveChannel{workflow.GetSignalChannel(ctx, "e")}
	var v string
	more := chs[0].Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

// ---- near-misses: not a closed-channel check at all (no fact) ----

// An ordinary bool that happens to be called more.
func PlainBoolMore(ctx workflow.Context, n int) error {
	more := n > 0
	if !more {
		return nil
	}
	return nil
}

// The real more is shadowed by an ordinary bool in an inner scope.
func ShadowedMore(ctx workflow.Context) error {
	ch := workflow.GetSignalChannel(ctx, "shadow")
	var v string
	more := ch.Receive(ctx, &v)
	_ = more
	{
		more := len(v) > 0
		if !more {
			return nil
		}
	}
	return nil
}

// A compound condition is never read.
func CompoundCondition(ctx workflow.Context, n int) error {
	ch := workflow.GetSignalChannel(ctx, "compound")
	var v string
	more := ch.Receive(ctx, &v)
	if !more && n > 0 {
		return nil
	}
	return nil
}

// The ok result, not more.
func OkResult(ctx workflow.Context) error {
	ch := workflow.GetSignalChannel(ctx, "ok")
	var v string
	ok, _ := ch.ReceiveWithTimeout(ctx, time.Second, &v)
	if !ok {
		return nil
	}
	return nil
}

// A user method that is not the SDK's.
type fake struct{}

func (fake) Receive(ctx workflow.Context, v any) bool { return true }

func NotTheSDK(ctx workflow.Context) error {
	var f fake
	var v string
	more := f.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

// A named result is read (as false) before it is assigned.
func NamedResult(ctx workflow.Context) (more bool, err error) {
	ch := workflow.GetSignalChannel(ctx, "named")
	var v string
	if !more {
		return false, nil
	}
	more = ch.Receive(ctx, &v)
	return more, nil
}

// The signal channel is turned into a Channel, which has Close.
func AssertedClose(ctx workflow.Context) error {
	ch := workflow.GetSignalChannel(ctx, "assert")
	ch.(workflow.Channel).Close()
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

func drain(ch workflow.ReceiveChannel) {}

// The signal channel is handed to another function.
func PassedOn(ctx workflow.Context) error {
	ch := workflow.GetSignalChannel(ctx, "passed")
	drain(ch)
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

// ---- declared with no value, assigned once ----

// CATCH: declared first, assigned once before any use.
func CatchLateAssigned(ctx workflow.Context) error {
	var ch workflow.ReceiveChannel
	ch = workflow.GetSignalChannel(ctx, "late")
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

// CATCH: the same, used as the channel of AddReceive.
func CatchLateAssignedAddReceive(ctx workflow.Context) error {
	var ch workflow.ReceiveChannel
	ch = workflow.GetSignalChannel(ctx, "late-sel")
	sel := workflow.NewSelector(ctx)
	sel.AddReceive(ch, func(c workflow.ReceiveChannel, more bool) {
		if !more {
			return
		}
	})
	sel.Select(ctx)
	return nil
}

// NEAR-MISS: assigned once, but to the workflow's own channel, which it
// closes: the closed side is reachable (run for real).
func LateOwnClosed(ctx workflow.Context) error {
	var ch workflow.ReceiveChannel
	own := workflow.NewChannel(ctx)
	ch = own
	own.Close()
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

// NEAR-MISS: assigned twice (a signal channel, then the workflow's own,
// which it closes): reachable (run for real).
func LateAssignedTwice(ctx workflow.Context) error {
	var ch workflow.ReceiveChannel
	own := workflow.NewChannel(ctx)
	ch = workflow.GetSignalChannel(ctx, "twice")
	ch = own
	own.Close()
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

// NEAR-MISS: used before the assignment (nil channel at that point).
func LateUsedBefore(ctx workflow.Context) error {
	var ch workflow.ReceiveChannel
	var v string
	more := ch.Receive(ctx, &v)
	ch = workflow.GetSignalChannel(ctx, "before")
	if !more {
		return nil
	}
	return nil
}

// NEAR-MISS: the assignment is inside an if.
func LateInIf(ctx workflow.Context, flag bool) error {
	var ch workflow.ReceiveChannel
	if flag {
		ch = workflow.GetSignalChannel(ctx, "inif")
	}
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

// NEAR-MISS: the address is taken.
func LateAddress(ctx workflow.Context) error {
	var ch workflow.ReceiveChannel
	p := &ch
	ch = workflow.GetSignalChannel(ctx, "addr")
	*p = nil
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

// NEAR-MISS: the address in parentheses.
func LateParenAddress(ctx workflow.Context) error {
	var ch workflow.ReceiveChannel
	ch = workflow.GetSignalChannel(ctx, "paren")
	p := &(ch)
	*p = workflow.NewChannel(ctx)
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}

// NEAR-MISS: a closure uses it (it could run at another time).
func LateClosure(ctx workflow.Context) error {
	var ch workflow.ReceiveChannel
	ch = workflow.GetSignalChannel(ctx, "closure2")
	sel := workflow.NewSelector(ctx)
	sel.AddFuture(workflow.NewTimer(ctx, time.Second), func(f workflow.Future) {
		var v string
		more := ch.Receive(ctx, &v)
		if !more {
			return
		}
	})
	sel.Select(ctx)
	return nil
}

// NEAR-MISS: assigned in a nested block, not the declaration's block.
func LateNestedBlock(ctx workflow.Context) error {
	var ch workflow.ReceiveChannel
	{
		ch = workflow.GetSignalChannel(ctx, "nested")
	}
	var v string
	more := ch.Receive(ctx, &v)
	if !more {
		return nil
	}
	return nil
}
