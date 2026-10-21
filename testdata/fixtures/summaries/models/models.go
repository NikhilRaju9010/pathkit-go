// Package models has plain functions (they take no workflow context) that the
// workflows of the summaries fixture call. All names are made up.
package models

// Item is a made-up order item.
type Item struct {
	Name     string
	Note     *string
	Count    int
	Children []Item
}

// ToMap turns an item into a map. It stores a string, a *string, an int and a
// list of maps under four constant keys.
func (i Item) ToMap() map[string]interface{} {
	m := map[string]interface{}{}
	m["Name"] = i.Name
	m["Note"] = i.Note
	m["Count"] = i.Count
	var kids []map[string]interface{}
	for _, c := range i.Children {
		kids = append(kids, c.ToMap())
	}
	m["Children"] = kids
	return m
}

// Kind is always the same text.
func (i Item) Kind() string { return "item" }

// Payload gives a string or an int.
func Payload(flag bool) interface{} {
	if flag {
		return "yes"
	}
	return 1
}

// Pair gives a string and no error.
func Pair() (interface{}, error) { return "ok", nil }

// NewItem never gives nil.
func NewItem() *Item { return &Item{} }

// Leaky hands the map to other code before it returns it.
func Leaky() map[string]interface{} {
	m := map[string]interface{}{"k": "x"}
	mutate(m)
	return m
}

func mutate(m map[string]interface{}) { m["k"] = 5 }

// NonConstKey writes under a key that is not a constant.
func NonConstKey(k string) map[string]interface{} {
	m := map[string]interface{}{}
	m[k] = "x"
	return m
}

// Deleter removes a key.
func Deleter() map[string]interface{} {
	m := map[string]interface{}{"k": "x"}
	delete(m, "k")
	return m
}

// Closure writes the map from a function literal.
func Closure() map[string]interface{} {
	m := map[string]interface{}{"k": "x"}
	set := func() { m["k"] = 5 }
	set()
	return m
}

// Describer is an interface: a call through it is not followed.
type Describer interface{ Describe() interface{} }

// Name is a Describer.
type Name string

// Describe gives a string.
func (n Name) Describe() interface{} { return string(n) }

// Loop calls itself.
func Loop(n int) interface{} {
	if n > 0 {
		return Loop(n - 1)
	}
	return "done"
}

func d1() interface{} { return d2() }
func d2() interface{} { return d3() }
func d3() interface{} { return d4() }
func d4() interface{} { return "deep" }

// Deep needs four nested calls: more than a summary looks through.
func Deep() interface{} { return d1() }

// Shallow needs two.
func Shallow() interface{} { return d3() }

// Switchy returns a different type on each side, through a variable.
func Switchy(flag bool) interface{} {
	var v interface{}
	if flag {
		v = 1.5
	} else {
		v = true
	}
	return v
}

// Param returns its argument: nothing is known about its type.
func Param(x interface{}) interface{} { return x }

// MaybeNil gives nil or a string.
func MaybeNil(flag bool) interface{} {
	if flag {
		return "x"
	}
	return nil
}

// ModeName gives a named constant, which may differ between builds.
func ModeName() string { return Mode }

// Envelope builds a map with a closure that stores strings under the constant
// keys it is called with, and one list of maps under another key.
func (i Item) Envelope(extra *string) map[string]interface{} {
	result := map[string]interface{}{"Count": i.Count}
	add := func(key string, val *string) {
		if val != nil {
			result[key] = *val
		}
	}
	add("Alpha", i.Note)
	add("Beta", extra)
	if len(i.Children) > 0 {
		kids := make([]map[string]interface{}, len(i.Children))
		result["Kids"] = kids
	}
	return result
}

// Sneaky has a closure that stores under a key it is given, called with a
// computed key: unknown.
func (i Item) Sneaky(key string) map[string]interface{} {
	result := map[string]interface{}{}
	add := func(k string, val *string) { result[k] = *val }
	add(key, i.Note)
	return result
}

// Escaping hands its writer closure on: unknown.
func (i Item) Escaping() map[string]interface{} {
	result := map[string]interface{}{"A": "x"}
	add := func(k string, v int) { result[k] = v }
	run(add)
	return result
}

func run(f func(string, int)) { f("A", 1) }
