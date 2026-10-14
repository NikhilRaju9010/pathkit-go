package classify

import (
	"strings"
	"testing"
)

const dataflowDir = "../../testdata/fixtures/dataflow"

// Same plain value tested twice (detector "contradictory").
// Catch: the pair that gives opposite answers. Near-miss: every shape the
// spike showed gives "no claim" must stay testable.
func TestContradictory(t *testing.T) {
	// A path is marked when it takes BOTH edges of a listed pair.
	type pair struct {
		workflow string
		edges    [][2]string // pairs of edges that must be marked together
	}
	catch := []pair{
		{"TwiceLocal", [][2]string{{"J1.true", "J2.true"}, {"J1.false", "J2.false"}}},
		{"TwiceField", [][2]string{{"J1.true", "J2.true"}, {"J1.false", "J2.false"}}},
		{"SameConditionTwice", [][2]string{{"J1.true", "J2.false"}, {"J1.false", "J2.true"}}},
		{"ElseIfOpposite", [][2]string{{"J1.false", "J2.false"}}},
		// Get(&res): claimed by the detector
		// "contradictory-get-result" (see detect_getvalue_test.go).
		{"TwiceViaGet", [][2]string{{"J1.true", "J2.true"}, {"J1.false", "J2.false"}}},
	}
	for _, c := range catch {
		t.Run("catch "+c.workflow, func(t *testing.T) {
			marked := 0
			for _, d := range detected(t, dataflowDir, "dataflow."+c.workflow) {
				want := false
				for _, e := range c.edges {
					want = want || takes(d.Path, e[0]) && takes(d.Path, e[1])
				}
				got := d.C.Label == "contradictory"
				if got != want {
					t.Errorf("path %s: marked=%v, want %v (%s)", d.Path.Key(), got, want, d.C.Explanation)
				}
				if got {
					marked++
					if d.C.Bucket != CannotBeTested || d.C.Confidence != Certain || d.C.Source != SourceAuto || d.C.PossibleInProduction {
						t.Errorf("wrong classification: %+v", d.C)
					}
				}
			}
			if marked != len(c.edges) {
				t.Errorf("%d paths marked, want %d", marked, len(c.edges))
			}
		})
	}
	// Near-misses: nothing may be marked.
	for _, name := range []string{
		"Reassigned",            // assigned again in between
		"TwoCalls",              // two calls are two facts
		"FieldWritten",          // the field is written in between
		"ClosureWrites",         // a closure writes it
		"TestsInsideLoop",       // inside a loop: other trips can differ
		"ChannelReceiveTwice",   // a received value is new each time
		"PointerParamField",     // through a pointer: can change
		"CompoundTwice",         // compound conditions are their own value
		"WholeStructReassigned", // the whole struct is assigned again
		"IntSizePair",           // strconv.IntSize is 64 here, 32 elsewhere
		"LimitPair",             // a constant that differs by operating system
		"LongStrings",           // two different long constants
		"FloatConstants",        // two different float constants
		"MapLength",             // a map's length changes when written to
		"ChannelLength",         // a channel's length changes
		"ZeroThenStore",         // zero first, assigned once later
		"StoreInBranch",         // the single assignment is in a branch
		"AddressStored",         // the address is stored and written through
	} {
		t.Run("near-miss "+name, func(t *testing.T) {
			for _, d := range detected(t, dataflowDir, "dataflow."+name) {
				if d.C.Bucket != Testable {
					t.Errorf("path %s is %s/%s: %s", d.Path.Key(), d.C.Bucket, d.C.Label, d.C.Explanation)
				}
			}
		})
	}
}

func TestContradictoryExplanation(t *testing.T) {
	for _, d := range detected(t, dataflowDir, "dataflow.TwiceLocal") {
		if d.C.Label == "contradictory" {
			for _, want := range []string{"J1", "J2", "same value", "cannot change"} {
				if !strings.Contains(d.C.Explanation, want) {
					t.Errorf("explanation %q lacks %q", d.C.Explanation, want)
				}
			}
			return
		}
	}
	t.Fatal("nothing marked")
}

// Two loops over the same list (detector "same-list-loops").
func TestSameListLoops(t *testing.T) {
	// A loop is "skipped" on a path that takes its exit without an iterate.
	type loops struct {
		workflow string
		marked   int // paths marked: first skipped + second runs, or the reverse
	}
	for _, c := range []loops{
		{"TwoLoops", 2}, {"TwoLoopsLocal", 2}, {"RangeParamTwice", 2},
	} {
		t.Run("catch "+c.workflow, func(t *testing.T) {
			n := 0
			for _, d := range detected(t, dataflowDir, "dataflow."+c.workflow) {
				iter1, iter2 := takes(d.Path, "J1.iterate"), takes(d.Path, "J2.iterate")
				want := iter1 != iter2
				got := d.C.Label == "same-list-loops"
				if got != want {
					t.Errorf("path %s: marked=%v, want %v", d.Path.Key(), got, want)
				}
				if got {
					n++
					if d.C.Bucket != CannotBeTested || d.C.Confidence != Certain {
						t.Errorf("wrong classification %+v", d.C)
					}
				}
			}
			if n != c.marked {
				t.Errorf("%d paths marked, want %d", n, c.marked)
			}
		})
	}
	for _, name := range []string{
		"ListAppended",   // the list is re-assigned in between
		"DifferentLists", // two different lists
		"GrowsInLoop",    // the first loop changes the list it walks
		"MapRangeTwice",  // a map's length can change
		"ResliceTwice",   // two slice expressions are two values
	} {
		t.Run("near-miss "+name, func(t *testing.T) {
			ds := detected(t, dataflowDir, "dataflow."+name)
			if len(ds) < 4 {
				t.Fatalf("only %d paths: the loops are not junctions, so this near-miss tests nothing", len(ds))
			}
			for _, d := range ds {
				if d.C.Bucket != Testable {
					t.Errorf("path %s is %s/%s", d.Path.Key(), d.C.Bucket, d.C.Label)
				}
			}
		})
	}
}
