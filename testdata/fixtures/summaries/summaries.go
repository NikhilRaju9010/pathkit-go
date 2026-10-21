// Package summaries has workflows that call plain functions of another
// package. Names starting with Impossible must have an impossible path proved
// by reading the called function; names starting with Possible must not.
package summaries

import (
	"go.temporal.io/sdk/workflow"

	"example.com/fixtures/summaries/models"
)

func act(ctx workflow.Context) error {
	return workflow.ExecuteActivity(ctx, "Act").Get(ctx, nil)
}

// ToMap stores a []map[string]interface{} under "Children": the case for
// []interface{} cannot be taken.
func ImpossibleListOfMaps(ctx workflow.Context, item models.Item) error {
	m := item.ToMap()
	switch m["Children"].(type) {
	case []interface{}:
		_ = act(ctx)
	case []map[string]interface{}:
		_ = act(ctx)
	}
	return nil
}

// "Name" holds only a string.
func ImpossibleNameIsString(ctx workflow.Context, item models.Item) error {
	m := item.ToMap()
	switch m["Name"].(type) {
	case int:
		_ = act(ctx)
	case string:
		_ = act(ctx)
	}
	return nil
}

// "Note" holds a *string: `case string` is a likely defect, not impossible.
func LikelyDefectNoteIsPointer(ctx workflow.Context, item models.Item) error {
	m := item.ToMap()
	switch m["Note"].(type) {
	case string:
		_ = act(ctx)
	}
	return nil
}

// A key the function never writes is not in the map.
func ImpossibleMissingKey(ctx workflow.Context, item models.Item) error {
	m := item.ToMap()
	switch m["Other"].(type) {
	case string:
		_ = act(ctx)
	}
	return nil
}

func ImpossiblePayloadType(ctx workflow.Context, flag bool) error {
	v := models.Payload(flag)
	switch v.(type) {
	case float64:
		_ = act(ctx)
	case string:
		_ = act(ctx)
	case int:
		_ = act(ctx)
	}
	return nil
}

func ImpossibleTupleResult(ctx workflow.Context) error {
	v, err := models.Pair()
	if err != nil {
		return err
	}
	switch v.(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func ImpossibleConstantResult(ctx workflow.Context, item models.Item) error {
	k := item.Kind()
	if k == "other" {
		_ = act(ctx)
	}
	return nil
}

func ImpossibleNeverNil(ctx workflow.Context) error {
	p := models.NewItem()
	if p == nil {
		_ = act(ctx)
	}
	return nil
}

func ImpossibleTwoLevels(ctx workflow.Context) error {
	v := models.Shallow()
	switch v.(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func ImpossibleThroughVariable(ctx workflow.Context, flag bool) error {
	v := models.Switchy(flag)
	switch v.(type) {
	case string:
		_ = act(ctx)
	}
	return nil
}

// ---- near misses: nothing may be claimed ----

func PossibleLeakyMap(ctx workflow.Context) error {
	m := models.Leaky()
	switch m["k"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleNonConstantKey(ctx workflow.Context) error {
	m := models.NonConstKey("a")
	switch m["a"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleDeletedKey(ctx workflow.Context) error {
	m := models.Deleter()
	switch m["k"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleClosureWrites(ctx workflow.Context) error {
	m := models.Closure()
	switch m["k"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleInterfaceCall(ctx workflow.Context, d models.Describer) error {
	v := d.Describe()
	switch v.(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleRecursive(ctx workflow.Context) error {
	v := models.Loop(3)
	switch v.(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleTooDeep(ctx workflow.Context) error {
	v := models.Deep()
	switch v.(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleParamReturned(ctx workflow.Context, x interface{}) error {
	v := models.Param(x)
	switch v.(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

// The summary says string or nil: the nil case stays possible.
func PossibleNilFromSummary(ctx workflow.Context, flag bool) error {
	v := models.MaybeNil(flag)
	if v == nil {
		_ = act(ctx)
	}
	return nil
}

// The map is changed after the call with a value PathKit knows nothing about:
// the summary no longer describes it.
func PossibleWrittenAfterCall(ctx workflow.Context, item models.Item, d models.Describer) error {
	m := item.ToMap()
	m["Name"] = d.Describe()
	switch m["Name"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

// A store under a constant key after the call replaces what that key holds
// (the walk follows one path): "Name" is an int now, so nothing else can be taken.
func ImpossibleStoreAfterCall(ctx workflow.Context, item models.Item) error {
	m := item.ToMap()
	m["Name"] = 5
	switch m["Name"].(type) {
	case int:
		_ = act(ctx)
	case string:
		_ = act(ctx)
	}
	return nil
}

// Other keys keep their types after a store under one key.
func ImpossibleOtherKeyKept(ctx workflow.Context, item models.Item) error {
	m := item.ToMap()
	m["workflow_id"] = "w"
	switch m["Children"].(type) {
	case []interface{}:
		_ = act(ctx)
	}
	return nil
}

// Code that only one system builds is not read as the function that runs.
func PossibleSystemSpecific(ctx workflow.Context) error {
	v := models.SystemKind()
	switch v.(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

// A named constant may be declared in a file only some builds use: unknown.
func PossibleNamedConstant(ctx workflow.Context) error {
	if models.ModeName() == "b" {
		_ = act(ctx)
	}
	return nil
}

// A closure that stores under constant keys: "Alpha" holds only a string.
func ImpossibleClosureWriter(ctx workflow.Context, item models.Item) error {
	m := item.Envelope(nil)
	switch m["Alpha"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

// ... and "Kids" a list of maps, never a []interface{} (the real shape of a
// struct converted to a map).
func ImpossibleClosureWriterKids(ctx workflow.Context, item models.Item) error {
	m := item.Envelope(nil)
	switch m["Kids"].(type) {
	case []interface{}:
		_ = act(ctx)
	}
	return nil
}

func PossibleClosureComputedKey(ctx workflow.Context, item models.Item) error {
	m := item.Sneaky("Alpha")
	switch m["Alpha"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}

func PossibleClosureEscapes(ctx workflow.Context, item models.Item) error {
	m := item.Escaping()
	switch m["A"].(type) {
	case int:
		_ = act(ctx)
	}
	return nil
}
