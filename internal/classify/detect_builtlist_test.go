package classify

import "testing"

const builtDir = "../../testdata/fixtures/builtlist"

// Detector "list-built-from-list": the second loop walks a list the first
// loop builds, one append per trip.
func TestListBuiltFromList(t *testing.T) {
	for _, name := range []string{"BuiltTwoLoops", "BuiltWithMake"} {
		t.Run("catch "+name, func(t *testing.T) {
			n := 0
			ds := detected(t, builtDir, "builtlist."+name)
			if len(ds) != 4 {
				t.Fatalf("%d paths, want 4", len(ds))
			}
			for _, d := range ds {
				// J1 is the building loop, J2 the loop over the list.
				want := takes(d.Path, "J1.iterate") != takes(d.Path, "J2.iterate")
				got := d.C.Label == "same-list-loops" && d.C.Detector == "list-built-from-list"
				if got != want {
					t.Errorf("path %s: marked=%v, want %v (%s %s)", d.Path.Key(), got, want, d.C.Bucket, d.C.Detector)
				}
				if got {
					n++
					if d.C.Bucket != CannotBeTested || d.C.Confidence != Certain || d.C.Source != SourceAuto || d.C.PossibleInProduction {
						t.Errorf("wrong classification %+v", d.C)
					}
				}
			}
			if n != 2 {
				t.Errorf("%d paths marked, want 2", n)
			}
		})
	}
	// Near-misses: nothing may be marked. The first ten are run for real
	// by TestLiveFixtures (builtlist): the "impossible-looking" path is
	// reachable.
	for _, name := range []string{
		"ConditionalAppend", "ContinueBeforeAppend", "StartsNotEmpty", "AppendedBetween",
		"ReplacedBetween", "ClosureAppends", "ReturnBeforeAppend",
		"MethodReset", "MethodValueReset", "ParenAddress",
		"AddressPassed", "InsideOuterLoop", "WalkedBeforeBuilt", "BuiltFromMap",
	} {
		t.Run("near-miss "+name, func(t *testing.T) {
			ds := detected(t, builtDir, "builtlist."+name)
			if len(ds) < 4 {
				t.Fatalf("only %d paths: the loops are not junctions, so this near-miss tests nothing", len(ds))
			}
			for _, d := range ds {
				if d.C.Bucket != Testable {
					t.Errorf("path %s is %s/%s: %s", d.Path.Key(), d.C.Bucket, d.C.Label, d.C.Explanation)
				}
			}
		})
	}
}
