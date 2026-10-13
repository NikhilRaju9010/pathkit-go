package cli

import (
	"slices"
	"testing"
)

func keysOf(m map[string]any) []string {
	var k []string
	for key := range m {
		k = append(k, key)
	}
	slices.Sort(k)
	return k
}

// The JSON's key sets, level by level: the keys that existed before path
// classification, and the ones it added. A key may only ever be added to
// this list of additions; the first list must never lose or rename one.
func TestJSONKeySets(t *testing.T) {
	traces := ordersTraces(t)
	inPilot(t)
	check := func(t *testing.T, level string, got map[string]any, original, added []string) {
		t.Helper()
		want := append(slices.Clone(original), added...)
		slices.Sort(want)
		if g := keysOf(got); !slices.Equal(g, want) {
			t.Errorf("%s keys\n got %v\nwant %v (original %v + added %v)", level, g, want, original, added)
		}
		for _, k := range original {
			if _, ok := got[k]; !ok {
				t.Errorf("%s: original key %q is missing", level, k)
			}
		}
	}
	for _, cmd := range []string{"coverage", "report"} {
		out, _, _ := run(t, cmd, "./orders", "--traces", traces, "--json", "--fail-under", "60")
		m := decode(t, out)
		topOriginal := []string{"schemaVersion", "tool", "workflows", "excluded", "total", "branches", "traces", "failUnder"}
		wfOriginal := []string{"name", "addedByConfig", "paths", "branches", "truncated", "pathList"}
		if cmd == "report" {
			topOriginal = append(topOriginal, "excludedBy")
			wfOriginal = append(wfOriginal, "file", "priority")
		}
		check(t, cmd+" top level", m, topOriginal, []string{"classification", "coverageBasis", "classificationShown"})
		wf := m["workflows"].([]any)[0].(map[string]any)
		check(t, cmd+" workflow", wf, wfOriginal, []string{"classification"})
		path := wf["pathList"].([]any)[0].(map[string]any)
		check(t, cmd+" path", path,
			[]string{"number", "id", "text", "steps", "end", "compensation", "covered", "traces", "staleTrace"},
			[]string{"stableId", "bucket", "label", "source", "confidence", "possibleInProduction", "explanation", "ruleId"})
		check(t, cmd+" failUnder", m["failUnder"].(map[string]any), []string{"threshold", "passed"}, []string{"basis"})
	}
}
