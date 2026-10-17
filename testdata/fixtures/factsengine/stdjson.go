package factsengine

import (
	"encoding/json"
	"strings"
	"time"

	"go.temporal.io/sdk/workflow"
)

// ---- values encoding/json decodes in the workflow (no assumption about
// Temporal's converter): json.Unmarshal, and Decode on a decoder made here
// that never calls UseNumber ----

// An interface{} that json.Unmarshal filled holds only string, float64, bool,
// nil, []interface{} or map[string]interface{}: an int is never there.
func ImpossibleStdUnmarshal(ctx workflow.Context, raw []byte) error {
	var v interface{}
	_ = json.Unmarshal(raw, &v)
	switch v.(type) {
	case int:
		return workflow.Sleep(ctx, time.Second)
	case string:
		return workflow.Sleep(ctx, time.Minute)
	}
	return nil
}

func ImpossibleStdDecoder(ctx workflow.Context, text string) error {
	var v interface{}
	dec := json.NewDecoder(strings.NewReader(text))
	_ = dec.Decode(&v)
	if _, ok := v.(int); ok {
		return workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// A decoder in some other function that uses UseNumber does not change what
// this decoder produces.
func ImpossibleStdUnmarshalDespiteOtherUseNumber(ctx workflow.Context, raw []byte) error {
	var v interface{}
	_ = json.Unmarshal(raw, &v)
	if _, ok := v.(int); ok {
		return workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// Unrelated to every workflow: a decoder that reads numbers as json.Number.
func readNumbers(text string) interface{} {
	var v interface{}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	_ = dec.Decode(&v)
	return v
}

var _ = readNumbers

// UseNumber on this decoder: the value can hold a json.Number, not followed.
func PossibleStdDecoderUseNumber(ctx workflow.Context, text string) error {
	var v interface{}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	_ = dec.Decode(&v)
	if _, ok := v.(json.Number); ok {
		return workflow.Sleep(ctx, time.Second)
	}
	return nil
}

func switchToNumbers(d *json.Decoder) { d.UseNumber() }

// The decoder is handed to another function, which may call UseNumber.
func PossibleStdDecoderHandedOn(ctx workflow.Context, text string) error {
	var v interface{}
	dec := json.NewDecoder(strings.NewReader(text))
	switchToNumbers(dec)
	_ = dec.Decode(&v)
	if _, ok := v.(json.Number); ok {
		return workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// A decoder that is not made here: nothing is known about it.
func PossibleStdDecoderFromParameter(ctx workflow.Context, dec *json.Decoder) error {
	var v interface{}
	_ = dec.Decode(&v)
	if _, ok := v.(json.Number); ok {
		return workflow.Sleep(ctx, time.Second)
	}
	return nil
}

// The value already held something before json.Unmarshal: not followed.
func PossibleStdUnmarshalIntoSetValue(ctx workflow.Context, raw []byte) error {
	var v interface{} = 7
	_ = json.Unmarshal(raw, &v)
	if _, ok := v.(int); ok {
		return workflow.Sleep(ctx, time.Second)
	}
	return nil
}
