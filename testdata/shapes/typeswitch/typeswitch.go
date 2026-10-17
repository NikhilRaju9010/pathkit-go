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

// NEAR-MISS: the map also gets a value from a call PathKit cannot see into,
// so any type is possible.
func SwitchWithUnknownValue(ctx workflow.Context, name string) error {
	vals := map[string]interface{}{}
	vals["name"] = name
	vals["extra"] = lookup(name)
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

func lookup(s string) interface{} { return s }

// CATCH under the assumption default-json-converter: a workflow input of
// type interface{} is filled by Temporal's decoder, which gives a string, a
// float64, a bool, nil, a []interface{} or a map[string]interface{}; never an
// int. (If the module sets its own data converter, the rule turns itself off.)
func SwitchOnDecodedInput(ctx workflow.Context, extra interface{}) error {
	switch extra.(type) {
	case string:
		_ = act(ctx)
	case int:
		_ = act(ctx)
	default:
		_ = act(ctx)
	}
	return nil
}
