// Package typeswitch is shape (b): a type switch on an interface{} whose
// values were only ever a string or a *string, kept in a local map.
package typeswitch

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

func act(ctx workflow.Context) error { return workflow.Sleep(ctx, time.Second) }

// CATCH: "case int" can never be taken: the map only holds a string and a
// *string. The key is an input, so any of the two (or a missing key, which
// gives nil and goes to default) can come out.
func SwitchOnLocalMap(ctx workflow.Context, key string, name string, alias *string) error {
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

// NEAR-MISS: the map also gets a value from unknown code (an input of
// interface type), so any type is possible.
func SwitchWithUnknownValue(ctx workflow.Context, name string, extra interface{}) error {
	vals := map[string]interface{}{}
	vals["name"] = name
	vals["extra"] = extra
	switch vals["extra"].(type) {
	case string:
		_ = act(ctx)
	case int:
		_ = act(ctx)
	default:
		_ = act(ctx)
	}
	return nil
}
