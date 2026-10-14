package classify

import "testing"

const getValueDir = "../../testdata/fixtures/getvalue"

// Detector "contradictory-get-result": a value filled in once by
// Get(ctx, &res), tested twice.
func TestContradictoryGetResult(t *testing.T) {
	// A path is marked when it takes BOTH edges of a listed pair.
	type c struct {
		workflow string
		edges    [][2]string
	}
	for _, c := range []c{
		{"ValueTwice", [][2]string{{"J1.true", "J2.true"}, {"J1.false", "J2.false"}}},
		{"PointerAndConjunction", [][2]string{{"J1.false", "J2.true"}}},
		{"NestedField", [][2]string{{"J1.false", "J2.false"}}},
	} {
		t.Run("catch "+c.workflow, func(t *testing.T) {
			n := 0
			for _, d := range detected(t, getValueDir, "getvalue."+c.workflow) {
				want := false
				for _, e := range c.edges {
					want = want || takes(d.Path, e[0]) && takes(d.Path, e[1])
				}
				got := d.C.Label == "contradictory" && d.C.Detector == "contradictory-get-result"
				if got != want {
					t.Errorf("path %s: marked=%v, want %v (%s)", d.Path.Key(), got, want, d.C.Explanation)
				}
				if got {
					n++
					if d.C.Bucket != CannotBeTested || d.C.Confidence != Certain || d.C.Source != SourceAuto || d.C.PossibleInProduction {
						t.Errorf("wrong classification %+v", d.C)
					}
				}
			}
			if n != len(c.edges) {
				t.Errorf("%d paths marked, want %d", n, len(c.edges))
			}
		})
	}
	// Near-misses: nothing may be marked by this detector. The first nine
	// are run for real by TestLiveFixtures (getvalue): the
	// "impossible-looking" path is reachable.
	for _, name := range []string{
		"SecondGet", "WriteBetween", "AddressPassedOn", "MethodOnResult",
		"MethodValueOnResult", "PointerAlias", "PointerPassed", "ClosureWrites", "PointerToPointerPassed",
		"TestBeforeGet", "GetInLoop", "DeferredGet", "ThroughSlice",
		"ParenAlias", "IncrementBetween", "ThroughPointerField", "NotTemporalGet", "TwoGetsBeforeTests",
	} {
		t.Run("near-miss "+name, func(t *testing.T) {
			ds := detected(t, getValueDir, "getvalue."+name)
			if len(ds) < 4 {
				t.Fatalf("only %d paths: the decisions are not junctions", len(ds))
			}
			for _, d := range ds {
				if d.C.Bucket != Testable {
					t.Errorf("path %s is %s/%s: %s", d.Path.Key(), d.C.Bucket, d.C.Label, d.C.Explanation)
				}
			}
		})
	}
}
