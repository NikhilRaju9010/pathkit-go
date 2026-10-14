package classify

import "testing"

const loopFirstDir = "../../testdata/fixtures/loopfirst"

// Detector "loop-exit-before-first-trip": a `for cond` loop whose condition
// is true the first time it is tested must run at least once.
func TestLoopExitBeforeFirstTrip(t *testing.T) {
	for _, c := range []struct{ workflow, loop string }{
		{"FlagLoop", "J1"}, {"KeepGoing", "J1"}, {"DeclaredInOuterLoop", "J2"},
	} {
		t.Run("catch "+c.workflow, func(t *testing.T) {
			n := 0
			for _, d := range detected(t, loopFirstDir, "loopfirst."+c.workflow) {
				want := takes(d.Path, c.loop+".exit") && !takes(d.Path, c.loop+".iterate")
				got := d.C.Label == "impossible-loop-exit" && d.C.Detector == "loop-exit-before-first-trip"
				if got != want {
					t.Errorf("path %s: marked=%v, want %v", d.Path.Key(), got, want)
				}
				if got {
					n++
					if d.C.Bucket != CannotBeTested || d.C.Confidence != Certain || d.C.Source != SourceAuto || d.C.PossibleInProduction {
						t.Errorf("wrong classification %+v", d.C)
					}
				}
			}
			if n != 1 {
				t.Errorf("%d paths marked, want 1", n)
			}
		})
	}
	// Near-misses: nothing may be marked. The first seven and ParenAssign are run for real
	// by TestLiveFixtures (loopfirst).
	for _, name := range []string{
		"SetBefore", "ClosureSetsBefore", "AddressBefore", "FromParameter",
		"DeclaredOutsideOuterLoop", "AssignedBefore", "PackageLevel",
		"NeverEntered", "Compound", "ParenAssign",
	} {
		t.Run("near-miss "+name, func(t *testing.T) {
			ds := detected(t, loopFirstDir, "loopfirst."+name)
			if len(ds) < 2 {
				t.Fatalf("only %d paths: the loop is not a junction, so this near-miss tests nothing", len(ds))
			}
			for _, d := range ds {
				if d.C.Bucket != Testable {
					t.Errorf("path %s is %s/%s: %s", d.Path.Key(), d.C.Bucket, d.C.Label, d.C.Explanation)
				}
			}
		})
	}
}

// A named bool type can have pointer-receiver methods that change the value
// without writing &v. Analysis-only fixtures (such loops cannot be recorded).
func TestLoopExitNamedBoolIsNotClaimed(t *testing.T) {
	for _, name := range []string{"MethodSets", "MethodValueSets"} {
		ds := detected(t, "../../testdata/fixtures/loopfirstnamed", "loopfirstnamed."+name)
		if len(ds) < 2 {
			t.Fatalf("%s: only %d paths", name, len(ds))
		}
		for _, d := range ds {
			if d.C.Bucket != Testable {
				t.Errorf("%s: path %s is %s/%s", name, d.Path.Key(), d.C.Bucket, d.C.Label)
			}
		}
	}
}
