// Package helperwrites has helpers that change the caller's values through a
// pointer, a pointer receiver, a map, a slice or a captured variable. The
// decision after each call reads the changed value, so PathKit must not
// assume the value from before the call.
package helperwrites

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

type state struct{ name string }

func (s *state) mark(ctx workflow.Context) error {
	s.name = "z"
	return workflow.Sleep(ctx, time.Millisecond)
}

func setPointer(ctx workflow.Context, p *string) error {
	*p = "z"
	return workflow.Sleep(ctx, time.Millisecond)
}

func setMap(ctx workflow.Context, m map[string]string) error {
	m["k"] = "z"
	return workflow.Sleep(ctx, time.Millisecond)
}

func setSlice(ctx workflow.Context, s []string) error {
	s[0] = "z"
	return workflow.Sleep(ctx, time.Millisecond)
}

// ViaPointer reads a string after a helper wrote it through a pointer.
func ViaPointer(ctx workflow.Context) error {
	v := ""
	_ = setPointer(ctx, &v)
	if v == "" {
		return workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// ViaReceiver reads a field after a method with a pointer receiver wrote it.
func ViaReceiver(ctx workflow.Context) error {
	s := &state{}
	_ = s.mark(ctx)
	if s.name == "" {
		return workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// ViaMap reads a map entry after a helper wrote it.
func ViaMap(ctx workflow.Context) error {
	m := map[string]string{"k": ""}
	_ = setMap(ctx, m)
	if m["k"] == "" {
		return workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// ViaSlice reads a slice element after a helper wrote it.
func ViaSlice(ctx workflow.Context) error {
	s := []string{""}
	_ = setSlice(ctx, s)
	if s[0] == "" {
		return workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// ViaCapture reads a variable a closure wrote.
func ViaCapture(ctx workflow.Context) error {
	v := ""
	set := func(ctx workflow.Context) error {
		v = "z"
		return workflow.Sleep(ctx, time.Millisecond)
	}
	_ = set(ctx)
	if v == "" {
		return workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// NearMiss never passes v to the helper, so the decision really is fixed:
// PathKit may (and does) call its second branch untestable.
func NearMiss(ctx workflow.Context) error {
	v := ""
	var other string
	_ = setPointer(ctx, &other)
	if v == "" {
		return workflow.Sleep(ctx, time.Second)
	}
	return nil
}
