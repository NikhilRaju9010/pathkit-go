package factsengine

import (
	"encoding/json"
	"unsafe"

	"go.temporal.io/sdk/workflow"
)

// ---- the safety list: every one of these must stay possible ----

// &x anywhere: Get fills it.
func PossibleAddressTaken(ctx workflow.Context) error {
	var x string
	_ = workflow.ExecuteActivity(ctx, "Fetch").Get(ctx, &x)
	if x == "" {
		_ = act(ctx)
	}
	return nil
}

// &x after the check still means x is never tracked.
func PossibleAddressTakenLater(ctx workflow.Context) error {
	var x string
	if x == "" {
		_ = act(ctx)
	}
	_ = json.Unmarshal([]byte("{}"), &x)
	return nil
}

func PossibleSideEffectGet(ctx workflow.Context) error {
	var v string
	_ = workflow.SideEffect(ctx, func(ctx workflow.Context) interface{} { return "id" }).Get(&v)
	if v == "" {
		_ = act(ctx)
	}
	return nil
}

func PossibleSignalReceive(ctx workflow.Context) error {
	var s string
	workflow.GetSignalChannel(ctx, "sig").Receive(ctx, &s)
	if s == "" {
		_ = act(ctx)
	}
	return nil
}

type counter struct{ name string }

func (c *counter) fill() { c.name = "filled" }

// A method with a pointer receiver takes &c without writing it.
func PossiblePointerReceiver(ctx workflow.Context) error {
	var c counter
	c.fill()
	if c.name == "" {
		_ = act(ctx)
	}
	return nil
}

// Written inside a closure of any kind.
func PossibleClosureWrite(ctx workflow.Context) error {
	x := ""
	set := func() { x = "set" }
	set()
	if x == "" {
		_ = act(ctx)
	}
	return nil
}

func PossibleGoWrite(ctx workflow.Context) error {
	var x string
	workflow.Go(ctx, func(ctx workflow.Context) { x = "set" })
	_ = act(ctx)
	if x == "" {
		_ = act(ctx)
	}
	return nil
}

func PossibleSelectorWrite(ctx workflow.Context) error {
	var x string
	sel := workflow.NewSelector(ctx)
	sel.AddFuture(workflow.NewTimer(ctx, 1), func(f workflow.Future) { x = "timer" })
	sel.Select(ctx)
	if x == "" {
		_ = act(ctx)
	}
	return nil
}

func PossibleDeferWrite(ctx workflow.Context) error {
	var x string
	defer func() { x = "done" }()
	if x == "" {
		_ = act(ctx)
	}
	return nil
}

// A loop may change what the body assigns.
func PossibleLoopAssigns(ctx workflow.Context, items []string) error {
	done := false
	for _, it := range items {
		if it == "stop" {
			done = true
		}
		_ = act(ctx)
	}
	if done {
		_ = act(ctx)
	}
	return nil
}

// Declared before, assigned in the loop, checked at the loop head.
func PossibleLoopHead(ctx workflow.Context, n int) error {
	i := 0
	for i < n {
		_ = act(ctx)
		i++
	}
	return nil
}

// What a loop does NOT assign is still known after it.
func ImpossibleLoopKeepsOthers(ctx workflow.Context, items []string) error {
	var fixed string
	count := 0
	for range items {
		_ = act(ctx)
		count++
	}
	if fixed != "" {
		_ = act(ctx)
	}
	return nil
}

// An unknown call result, op-assign and ++ all make the value unknown.
func PossibleNotUnderstood(ctx workflow.Context, name string) error {
	a := ""
	a = unknownString(name)
	b := 0
	b += len(name)
	c := 0
	c++
	if a == "" {
		_ = act(ctx)
	}
	if b == 0 {
		_ = act(ctx)
	}
	if c == 0 {
		_ = act(ctx)
	}
	return nil
}

// A transparent err check (assumed to succeed) may in fact run its body.
func PossibleTransparentBody(ctx workflow.Context, name string) error {
	x := ""
	_, err := json.Marshal(name)
	if err != nil {
		x = "failed"
	}
	if x == "" {
		_ = act(ctx)
	}
	return nil
}

// An if marked //pathkit:ignore: the walk assumes its body does not run.
func PossibleIgnoredIf(ctx workflow.Context, name string) error {
	x := ""
	//pathkit:ignore
	if name == "boom" {
		x = "boom"
	}
	if x == "" {
		_ = act(ctx)
	}
	return nil
}

// A loop with nothing to decide is walked once but may run any number of times.
func PossibleTransparentLoop(ctx workflow.Context, items []string) error {
	x := ""
	for _, it := range items {
		x = it
	}
	if x == "" {
		_ = act(ctx)
	}
	return nil
}

var pkgLevel = ""

func PossiblePackageLevel(ctx workflow.Context) error {
	if pkgLevel == "" {
		_ = act(ctx)
	}
	return nil
}

// Map and slice elements are never tracked.
func PossibleElements(ctx workflow.Context) error {
	m := map[string]string{"a": ""}
	s := []string{""}
	if m["a"] != "" {
		_ = act(ctx)
	}
	if s[0] != "" {
		_ = act(ctx)
	}
	return nil
}

// Fields behind a pointer are shared with whoever else holds it.
func PossiblePointerField(ctx workflow.Context, in *Input) error {
	if in.Name == "" {
		in.Name = "x"
	}
	if in.Name == "" {
		_ = act(ctx)
	}
	return nil
}

// A value that came from a pointer parameter's own field is not tracked, but
// the pointer itself is: nil-ness is known.
func ImpossiblePointerNilness(ctx workflow.Context, in *Input) error {
	if in == nil {
		return nil
	}
	if in == nil {
		_ = act(ctx)
	}
	return nil
}

// A function that calls recover: the engine is off for it.
func PossibleRecover(ctx workflow.Context) (err error) {
	var s string
	defer func() {
		if r := recover(); r != nil {
			s = "recovered"
		}
	}()
	if s == "" {
		_ = act(ctx)
	}
	return nil
}

// Rewritten between the two asks: no contradiction.
func PossibleRewritten(ctx workflow.Context, x string) error {
	if x == "a" {
		_ = act(ctx)
	}
	x = unknownString(x)
	if x == "b" {
		_ = act(ctx)
	}
	return nil
}

// The ok flag was reassigned, so it says nothing about v.
func PossibleCommaOkBroken(ctx workflow.Context, raw interface{}) error {
	v, ok := raw.(string)
	v = unknownString(v)
	if !ok {
		if v != "" {
			_ = act(ctx)
		}
	}
	return nil
}

// A copy of an interface from a pointer is not nil even if the pointer is.
func PossibleTypedNil(ctx workflow.Context) error {
	var p *Input
	var v interface{} = p
	if v == nil {
		_ = act(ctx)
	}
	return nil
}

// Possible-correlation: a loop may change v between two asks that would
// otherwise contradict each other.
func SuspectLoopBetween(ctx workflow.Context, items []string, x string) error {
	if x == "a" {
		_ = act(ctx)
	}
	for _, it := range items {
		x = it
		_ = act(ctx)
	}
	if x == "b" {
		_ = act(ctx)
	}
	return nil
}

// A function that uses the unsafe package: the engine is off for it.
func PossibleUnsafe(ctx workflow.Context) error {
	var s string
	_ = unsafe.Sizeof(s)
	if s == "" {
		_ = act(ctx)
	}
	return nil
}

type Rec struct {
	A  string
	In struct{ ID string }
}

// The guarded struct is changed field by field after the comma-ok form, so
// "ok is false" no longer means it holds the zero value: the field is "x",
// the true side of the field check is impossible and the false side stays
// possible. (The engine once claimed the false side: the link between ok and
// the zero value did not end when only a field was written.)
func MixedCommaOkFieldWritten(ctx workflow.Context, m map[string]Rec) error {
	v, ok := m["k"]
	v.A = "x"
	if !ok {
		if v.A == "" {
			return nil
		}
		return act(ctx)
	}
	return nil
}

func PossibleCommaOkFieldAppended(ctx workflow.Context, m map[string]Rec) error {
	v, ok := m["k"]
	v.A += "x"
	if !ok {
		if v.A == "" {
			return nil
		}
		return act(ctx)
	}
	return nil
}

func MixedCommaOkNestedFieldWritten(ctx workflow.Context, raw interface{}) error {
	v, ok := raw.(Rec)
	v.In.ID = "x"
	if !ok {
		if v.In.ID == "" {
			return nil
		}
		return act(ctx)
	}
	return nil
}

// Nothing written after the comma-ok form: the zero struct is known.
func ImpossibleCommaOkStructZero(ctx workflow.Context, m map[string]Rec) error {
	v, ok := m["k"]
	if !ok {
		if v.A != "" {
			return act(ctx)
		}
	}
	return nil
}
