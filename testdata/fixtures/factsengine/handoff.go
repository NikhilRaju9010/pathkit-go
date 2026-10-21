package factsengine

import (
	"go.temporal.io/sdk/workflow"
)

// Values handed to other code (the mistake found in seed 5 / program 1371):
// what the workflow learned about a value BEFORE it was handed on must not be
// used from the hand-off onwards. Each kind has a Possible... workflow (the
// check comes after the hand-off: no claim) and, where the fact is a real one,
// an Impossible... near miss (the check comes before it: the claim stays) when
// the engine follows that fact at all; where it does not follow it (a slice's
// length through a pointer, the fields of a struct, a map inside a struct) the
// "before" workflow is a Possible... one too: no claim either way.

// ---- slices ----

func setFirst(s []interface{}) {
	if len(s) > 0 {
		s[0] = 1
	}
}

func PossibleSliceElementAfterWriter(ctx workflow.Context) error {
	s := []interface{}{"x"}
	setFirst(s)
	switch s[0].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func ImpossibleSliceElementBeforeWriter(ctx workflow.Context) error {
	s := []interface{}{"x"}
	switch s[0].(type) {
	case int:
		_ = act(ctx)
	}
	setFirst(s)
	return nil
}

func grow(p *[]string) { *p = append(*p, "more") }

func PossibleSliceLengthAfterGrow(ctx workflow.Context) error {
	s := []string{}
	grow(&s)
	if len(s) == 0 {
		_ = act(ctx)
	}
	return nil
}

func PossibleSliceLengthBeforeGrow(ctx workflow.Context) error {
	s := []string{}
	if len(s) != 0 {
		_ = act(ctx)
	}
	grow(&s)
	return nil
}

// ---- pointers ----

type holder struct {
	m map[string]interface{}
	n string
}

func setN(h *holder) { h.n = "set" }

func PossiblePointerFieldAfterWriter(ctx workflow.Context) error {
	h := &holder{n: ""}
	setN(h)
	if h.n == "" {
		_ = act(ctx)
	}
	return nil
}

func PossiblePointerFieldBeforeWriter(ctx workflow.Context) error {
	h := &holder{n: ""}
	if h.n != "" {
		_ = act(ctx)
	}
	setN(h)
	return nil
}

func PossiblePointerFieldAfterAssign(ctx workflow.Context) error {
	h := &holder{n: "a"}
	other := h
	setN(other)
	if h.n == "a" {
		_ = act(ctx)
	}
	return nil
}

// ---- struct fields reached through a pointer ----

func setNOn(h *holder) { h.n = "set" }

func PossibleFieldViaPointerToLocal(ctx workflow.Context) error {
	var h holder
	p := &h
	setNOn(p)
	if h.n == "" {
		_ = act(ctx)
	}
	return nil
}

// ---- values stored in a struct that is handed on ----

func fillHolder(h holder) { h.m["a"] = 1 }

func PossibleMapInStructAfterWriter(ctx workflow.Context) error {
	h := holder{m: map[string]interface{}{}}
	fillHolder(h)
	if len(h.m) == 0 {
		_ = act(ctx)
	}
	return nil
}

func PossibleMapInStructBeforeWriter(ctx workflow.Context) error {
	h := holder{m: map[string]interface{}{}}
	if len(h.m) != 0 {
		_ = act(ctx)
	}
	fillHolder(h)
	return nil
}

func fillPtrHolder(h *holder) { h.m["a"] = 1 }

func PossibleMapInPointerStructAfterWriter(ctx workflow.Context) error {
	h := &holder{m: map[string]interface{}{"k": "x"}}
	fillPtrHolder(h)
	switch h.m["a"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleMapStoredInStructThenPassedOn(ctx workflow.Context) error {
	m := map[string]interface{}{}
	h := holder{m: m}
	fillHolder(h)
	if len(m) == 0 {
		_ = act(ctx)
	}
	return nil
}

// ---- channels ----

// Nothing is known about what a channel carries: a value received into a
// variable is not known, whatever the workflow put in the variable before.
func PossibleChannelReceive(ctx workflow.Context) error {
	ch := workflow.NewChannel(ctx)
	workflow.Go(ctx, func(gctx workflow.Context) { ch.Send(gctx, "x") })
	v := ""
	ok := ch.ReceiveAsync(&v)
	if ok && v == "" {
		_ = act(ctx)
	}
	return nil
}

func PossibleChannelHandedToGo(ctx workflow.Context) error {
	ch := workflow.NewChannel(ctx)
	var v interface{} = "x"
	workflow.Go(ctx, func(gctx workflow.Context) { ch.Send(gctx, 1) })
	ch.Receive(ctx, &v)
	switch v.(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

// The shape the generator found (seed 1 / program 49): a check of the length of
// a map held in a struct field teaches the engine "empty" on one side; the
// struct is then handed to a function that fills the map; a second check must
// not use what the first one taught.
func PossibleMapInStructLearnedFromBranch(ctx workflow.Context) error {
	h := holder{m: map[string]interface{}{}}
	if len(h.m) == 0 {
		fillHolder(h)
		if len(h.m) != 0 {
			_ = act(ctx)
		}
	}
	return nil
}
