package classify

import (
	"fmt"

	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

// Registry knows every label a config may use: the built-in ones and the
// config's own custom_labels.
type Registry struct {
	custom map[string]Definition
}

// NewRegistry adds the config's custom labels to the built-in ones. A
// custom label may not reuse a built-in name: the built-in text stays.
func NewRegistry(custom []scope.CustomLabel) (*Registry, error) {
	r := &Registry{custom: map[string]Definition{}}
	for _, c := range custom {
		if _, ok := Lookup(KindLabel, c.Name); ok {
			return nil, fmt.Errorf("custom label %q has the name of a built-in label; pick another name", c.Name)
		}
		if _, dup := r.custom[c.Name]; dup {
			return nil, fmt.Errorf("custom label %q is defined twice", c.Name)
		}
		b := Bucket(c.Bucket)
		if b != CannotBeTested && b != NeedsSpecialTest && b != NeedsReview {
			return nil, fmt.Errorf("custom label %q: bucket %q is not one of cannot-be-tested, needs-special-test, needs-review", c.Name, c.Bucket)
		}
		r.custom[c.Name] = Definition{
			Kind: KindLabel, Name: c.Name, Bucket: b, Meaning: c.Definition,
			Why: "A label defined in the config file (custom_labels).", Example: "see the config file",
			PossibleInProduction: "as chosen in the config", Confidence: Certain, DecidedBy: "person",
			WhatToDo: "See the definition in the config file.",
		}
	}
	return r, nil
}

// Label finds a label's definition, built-in or custom.
func (r *Registry) Label(name string) (Definition, bool) {
	if d, ok := Lookup(KindLabel, name); ok {
		return d, true
	}
	d, ok := r.custom[name]
	return d, ok
}

// Custom returns the config's custom labels' definitions, for the glossary.
func (r *Registry) Custom() []Definition {
	var out []Definition
	for _, d := range r.custom {
		out = append(out, d)
	}
	sortDefinitions(out)
	return out
}
