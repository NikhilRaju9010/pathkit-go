// Package elemtype is shape (e): a type assertion on the elements of a
// slice that only ever received maps.
package elemtype

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

// CATCH: every element is a map, so the assertion cannot fail.
func AssertOnLocalSlice(ctx workflow.Context, a, b string) error {
	items := []interface{}{}
	items = append(items, map[string]interface{}{"name": a})
	items = append(items, map[string]interface{}{"name": b})
	elem := items[0]
	_, ok := elem.(map[string]interface{})
	if !ok {
		_ = act(ctx)
	}
	return nil
}

// CATCH (in a loop, the real-world shape).
func AssertInLoop(ctx workflow.Context, a, b string) error {
	items := []interface{}{}
	items = append(items, map[string]interface{}{"name": a})
	items = append(items, map[string]interface{}{"name": b})
	for _, elem := range items {
		_, ok := elem.(map[string]interface{})
		if !ok {
			_ = act(ctx)
			continue
		}
		_ = act(ctx)
	}
	return nil
}

// NEAR-MISS: one element comes from an input of interface type.
func AssertWithUnknownElement(ctx workflow.Context, a string, extra interface{}) error {
	items := []interface{}{}
	items = append(items, map[string]interface{}{"name": a})
	items = append(items, extra)
	elem := items[1]
	_, ok := elem.(map[string]interface{})
	if !ok {
		_ = act(ctx)
	}
	return nil
}
