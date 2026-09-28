package expected

import (
	"errors"
	"fmt"
	"sort"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

// Build builds the graph of one answer-key workflow. Since M4 every
// workflow in EXPECTED.md must be mapped, so anything that stops it is an
// error for the caller to fail on: not found by discovery, skipped as
// unsupported (the error names the construct), or any other build error.
// A skipped workflow must never make an answer-key test pass quietly.
func Build(workflows map[string]discover.Workflow, name string) (*model.Graph, error) {
	wf, ok := workflows[name]
	if !ok {
		return nil, fmt.Errorf("%s is in EXPECTED.md, but discovery didn't find it", name)
	}
	g, err := model.Build(wf)
	var u *model.UnsupportedError
	if errors.As(err, &u) {
		return nil, fmt.Errorf("%s is skipped (%v); every EXPECTED.md workflow must be mapped", name, err)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %v", name, err)
	}
	return g, nil
}

// Names lists the key's workflows in a fixed (sorted) order.
func Names(key map[string]*Workflow) []string {
	var out []string
	for name := range key {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TestCount is the number of tests the key lists, over all workflows.
func TestCount(key map[string]*Workflow) int {
	n := 0
	for _, w := range key {
		n += len(w.Tests)
	}
	return n
}
