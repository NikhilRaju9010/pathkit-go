package factsengine

import (
	"go.temporal.io/sdk/workflow"
)

// Fixtures for the type, nil and length facts (milestone M3). A workflow
// whose name starts with Impossible has a path the engine proves impossible;
// Possible has none. internal/facts/expected_test.go lists the exact claims.

type box struct{ n int }

func unknownIface(s string) interface{}  { return s }
func unknownMap() map[string]interface{} { return map[string]interface{}{"k": 1} }
func fill(m map[string]interface{})      { m["k"] = 5 }

// ---- type sets of interface values ----

// A value of a concrete type stored in an interface: only that type.
func ImpossibleTypeSwitchOnStored(ctx workflow.Context, name string) error {
	var v interface{} = name
	switch v.(type) {
	case string:
		_ = act(ctx)
	case int:
		_ = act(ctx)
	}
	return nil
}

// The default is impossible when every possible type takes an earlier case.
func ImpossibleDefaultCase(ctx workflow.Context, name string) error {
	var v interface{} = name
	switch v.(type) {
	case string:
		_ = act(ctx)
	default:
		_ = act(ctx)
	}
	return nil
}

// A case for an interface type the stored type does not satisfy (and the
// default, which every possible type already takes an earlier case for).
func ImpossibleInterfaceCase(ctx workflow.Context, name string) error {
	var v interface{} = name
	switch v.(type) {
	case interface{ Len() int }:
		_ = act(ctx)
	case string:
		_ = act(ctx)
	default:
		_ = act(ctx)
	}
	return nil
}

type label string

func (l label) String() string { return string(l) }

// A case for an interface type the stored type does satisfy.
func MixedInterfaceCase(ctx workflow.Context, name string) error {
	var v interface{} = label(name)
	switch v.(type) {
	case interface{ String() string }:
		_ = act(ctx)
	}
	return nil
}

// A map built here: its values hold the union of what was stored. The key
// is an input, so a missing key (nil) is possible and goes to default.
func ImpossibleMapUnion(ctx workflow.Context, key, name string, alias *string) error {
	vals := map[string]interface{}{}
	vals["name"] = name
	vals["alias"] = alias
	switch vals[key].(type) {
	case string:
		_ = act(ctx)
	case *string:
		_ = act(ctx)
	case int:
		_ = act(ctx)
	default:
		_ = act(ctx)
	}
	return nil
}

// A key that was stored is proven to exist: nil is not possible, so the
// "case nil" and the default cannot be taken.
func ImpossibleProvenKey(ctx workflow.Context, name string) error {
	vals := map[string]interface{}{"a": name}
	switch vals["a"].(type) {
	case nil:
		_ = act(ctx)
	case string:
		_ = act(ctx)
	default:
		_ = act(ctx)
	}
	return nil
}

// A slice built with append.
func ImpossibleAppendedSlice(ctx workflow.Context, a, b string) error {
	items := []interface{}{}
	items = append(items, a)
	items = append(items, b)
	switch items[0].(type) {
	case string:
		_ = act(ctx)
	case float64:
		_ = act(ctx)
	}
	return nil
}

// ---- comma-ok assertions decided by the possible types ----

func ImpossibleAssertionFails(ctx workflow.Context, name string) error {
	var v interface{} = name
	_, ok := v.(int)
	if ok {
		_ = act(ctx)
	}
	return nil
}

func ImpossibleAssertionSucceeds(ctx workflow.Context, name string) error {
	var v interface{} = name
	_, ok := v.(string)
	if !ok {
		_ = act(ctx)
	}
	return nil
}

// Elements of two types: the assertion can go either way.
func PossibleAssertionMixed(ctx workflow.Context, name string, n int) error {
	items := []interface{}{name, n}
	_, ok := items[0].(string)
	if !ok {
		_ = act(ctx)
	}
	return nil
}

// ---- typed nil ----

func ImpossibleTypedNilInMap(ctx workflow.Context, p *box) error {
	m := map[string]interface{}{"k": p}
	if m["k"] == nil {
		_ = act(ctx)
	}
	return nil
}

// A key that is not proven may be missing, which gives nil.
func PossibleMissingKeyNil(ctx workflow.Context, p *box, key string) error {
	m := map[string]interface{}{"k": p}
	if m[key] == nil {
		_ = act(ctx)
	}
	return nil
}

// ---- lengths ----

func ImpossibleLengthOfLiteral(ctx workflow.Context) error {
	items := []string{"a"}
	if len(items) == 0 {
		_ = act(ctx)
	}
	m := map[string]int{}
	if len(m) > 0 {
		_ = act(ctx)
	}
	return nil
}

func ImpossibleLengthAfterAppend(ctx workflow.Context, name string) error {
	var items []string
	items = append(items, name)
	if len(items) == 0 {
		_ = act(ctx)
	}
	return nil
}

func ImpossibleLengthAfterStore(ctx workflow.Context, name string) error {
	m := map[string]string{}
	m["k"] = name
	if len(m) == 0 {
		_ = act(ctx)
	}
	return nil
}

func ImpossibleLengthOfMake(ctx workflow.Context) error {
	s := make([]int, 3)
	if len(s) == 0 {
		_ = act(ctx)
	}
	return nil
}

// A range loop over something not empty cannot be left before its first round.
func ImpossibleRangeNonEmpty(ctx workflow.Context, a, b string) error {
	items := []string{a, b}
	for _, it := range items {
		if it == "stop" {
			_ = act(ctx)
		}
	}
	return nil
}

// An empty literal can be filled in a loop: nothing is known afterwards.
func PossibleRangeAfterFill(ctx workflow.Context, in []string) error {
	var out []string
	for _, it := range in {
		out = append(out, it)
		_ = act(ctx)
	}
	for _, it := range out {
		if it == "x" {
			_ = act(ctx)
		}
	}
	return nil
}

// A for loop whose condition is true when it starts cannot be left at once.
func ImpossibleForCondition(ctx workflow.Context) error {
	i := 0
	for i < 3 {
		_ = workflow.Sleep(ctx, 1)
		i++
	}
	return nil
}

// ---- values decoded by Temporal (assumption default-json-converter) ----

func ImpossibleJSONParam(ctx workflow.Context, in interface{}) error {
	switch in.(type) {
	case string:
		_ = act(ctx)
	case int:
		_ = act(ctx)
	case float64:
		_ = act(ctx)
	}
	return nil
}

func ImpossibleJSONMapParam(ctx workflow.Context, in map[string]interface{}) error {
	switch in["id"].(type) {
	case float64:
		_ = act(ctx)
	case int64:
		_ = act(ctx)
	}
	return nil
}

// A container found inside a decoded value holds decoded values again.
func ImpossibleJSONNested(ctx workflow.Context, in map[string]interface{}) error {
	list, _ := in["list"].([]interface{})
	for _, e := range list {
		switch e.(type) {
		case string:
			_ = act(ctx)
		case int:
			_ = act(ctx)
		}
	}
	return nil
}

// An activity result decoded into an interface{}.
func ImpossibleJSONGet(ctx workflow.Context) error {
	var x interface{}
	_ = workflow.ExecuteActivity(ctx, "Fetch").Get(ctx, &x)
	switch x.(type) {
	case bool:
		_ = act(ctx)
	case uint8:
		_ = act(ctx)
	}
	return nil
}

// If the interface already holds a pointer, the decoder fills the pointee
// and leaves the variable alone.
func PossibleJSONGetIntoPointer(ctx workflow.Context) error {
	var target string
	var x interface{} = &target
	_ = workflow.ExecuteActivity(ctx, "Fetch").Get(ctx, &x)
	switch x.(type) {
	case *string:
		_ = act(ctx)
	case int:
		_ = act(ctx)
	}
	return nil
}

// Another name for the same decoded map may store any type.
func PossibleJSONMapWritten(ctx workflow.Context, in map[string]interface{}) error {
	in["extra"] = 5
	switch in["id"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

// A decoded value is a typed target when its static type is not interface{}.
func MixedTypedParam(ctx workflow.Context, in *box) error {
	var v interface{} = in
	switch v.(type) {
	case *box:
		_ = act(ctx)
	}
	return nil
}

// ---- forget rules for slices and maps ----

func PossibleMapDeleted(ctx workflow.Context, p *box) error {
	m := map[string]interface{}{"k": p}
	delete(m, "k")
	if m["k"] == nil {
		_ = act(ctx)
	}
	return nil
}

func PossibleMapPassedToCall(ctx workflow.Context) error {
	m := map[string]interface{}{"k": "x"}
	fill(m)
	switch m["k"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleMapPassedToActivity(ctx workflow.Context) error {
	m := map[string]interface{}{"k": "x"}
	_ = workflow.ExecuteActivity(ctx, "Fetch", m).Get(ctx, nil)
	switch m["k"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleMapReassigned(ctx workflow.Context) error {
	m := map[string]interface{}{"k": "x"}
	m = unknownMap()
	switch m["k"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleMapAddressTaken(ctx workflow.Context) error {
	m := map[string]interface{}{"k": "x"}
	p := &m
	*p = unknownMap()
	switch m["k"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleMapWrittenInClosure(ctx workflow.Context) error {
	m := map[string]interface{}{"k": "x"}
	set := func() { m["k"] = 5 }
	set()
	switch m["k"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleMapAliasWrites(ctx workflow.Context) error {
	m := map[string]interface{}{"k": "x"}
	alias := m
	alias["k"] = 5
	switch m["k"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleSliceAliasWrites(ctx workflow.Context) error {
	s := []interface{}{"x"}
	alias := s
	alias[0] = 5
	switch s[0].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleStoreOfUnknownType(ctx workflow.Context, s string) error {
	m := map[string]interface{}{"k": "x"}
	m["k"] = unknownIface(s)
	switch m["k"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleAppendSpread(ctx workflow.Context, more []interface{}) error {
	items := []interface{}{"x"}
	items = append(items, more...)
	switch items[0].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleStoreInLoop(ctx workflow.Context, in []string) error {
	m := map[string]interface{}{"k": "x"}
	for range in {
		m["k"] = 5
		_ = workflow.Sleep(ctx, 1)
	}
	switch m["k"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleStoreInAssumedBranch(ctx workflow.Context, name string) error {
	m := map[string]interface{}{"k": "x"}
	_, err := workflowErr(name)
	if err != nil {
		m["k"] = 5
	}
	switch m["k"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func workflowErr(name string) (string, error) { return name, nil }

// A slice made with a length holds nil elements.
func ImpossibleMakeSliceHasNils(ctx workflow.Context) error {
	s := make([]interface{}, 2)
	if s[0] == nil {
		_ = act(ctx)
	}
	return nil
}

// ---- found by the independent review of milestone M3 ----

func pair() (interface{}, error) { return "s", nil }

// A slice literal with indexes: the slots before the first index hold nil.
func PossibleSparseSliceLiteral(ctx workflow.Context) error {
	s := []interface{}{2: "a"}
	if s[0] == nil {
		_ = act(ctx)
	}
	switch s[0].(type) {
	case string:
		_ = act(ctx)
	}
	return nil
}

// A tuple assignment into an element is not understood.
func PossibleTupleStoreIntoElement(ctx workflow.Context) error {
	m := map[string]interface{}{"a": 1}
	var err error
	m["a"], err = pair()
	_ = err
	switch m["a"].(type) {
	case string:
		_ = act(ctx)
	case int:
		_ = act(ctx)
	}
	return nil
}

// Range with an element on the left of the "=".
func PossibleRangeIntoElement(ctx workflow.Context, in []interface{}) error {
	s := []interface{}{"x"}
	for _, s[0] = range in {
		_ = workflow.Sleep(ctx, 1)
	}
	switch s[0].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

// A map with interface keys: 1, 1.0 and int64(1) are three different keys.
func PossibleInterfaceKeys(ctx workflow.Context, p *box) error {
	m := map[interface{}]interface{}{1: p}
	if m[1] == nil {
		_ = act(ctx)
	}
	if m[1.0] == nil {
		_ = act(ctx)
	}
	if m[int64(1)] == nil {
		_ = act(ctx)
	}
	return nil
}

// With plain keys the same key is proven: the typed-nil claim stays.
func ImpossiblePlainKeyIntConst(ctx workflow.Context, p *box) error {
	m := map[int]interface{}{1: p}
	if m[1] == nil {
		_ = act(ctx)
	}
	return nil
}

func mutateAny(v interface{}) { v.(map[string]interface{})["a"] = 5 }

// The container a decoded value holds is changed by code that got the
// interface.
func PossibleContainerChangedViaInterface(ctx workflow.Context, in interface{}) error {
	m := in.(map[string]interface{})
	mutateAny(in)
	switch m["a"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

// The same container under a second name that is handed on.
func PossibleContainerAliasPassedOn(ctx workflow.Context, in interface{}) error {
	m := in.(map[string]interface{})
	m2 := in.(map[string]interface{})
	fill(m2)
	switch m["k"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

// A channel the workflow makes itself hands over Go values untouched: an int
// stays an int (only values that came through the data converter are decoded).
func PossibleOwnChannelReceive(ctx workflow.Context) error {
	ch := workflow.NewChannel(ctx)
	workflow.Go(ctx, func(ctx workflow.Context) { ch.Send(ctx, 42) })
	var x interface{}
	ch.Receive(ctx, &x)
	switch x.(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleSettableFuture(ctx workflow.Context) error {
	f, set := workflow.NewFuture(ctx)
	set.SetValue(7)
	var x interface{}
	_ = f.Get(ctx, &x)
	switch x.(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

// Signals and side effects are decoded by the data converter too.
func ImpossibleJSONReceive(ctx workflow.Context) error {
	var v interface{}
	workflow.GetSignalChannel(ctx, "note").Receive(ctx, &v)
	switch v.(type) {
	case uint8:
		_ = act(ctx)
	case string:
		_ = act(ctx)
	}
	return nil
}

func ImpossibleJSONSideEffect(ctx workflow.Context) error {
	var v interface{}
	_ = workflow.SideEffect(ctx, func(ctx workflow.Context) interface{} { return 1 }).Get(&v)
	switch v.(type) {
	case uint8:
		_ = act(ctx)
	case float64:
		_ = act(ctx)
	}
	return nil
}

// A signal channel kept in a variable that is set once.
func ImpossibleJSONReceiveFromVariable(ctx workflow.Context) error {
	ch := workflow.GetSignalChannel(ctx, "note")
	var v interface{}
	ch.Receive(ctx, &v)
	switch v.(type) {
	case uint8:
		_ = act(ctx)
	}
	return nil
}
