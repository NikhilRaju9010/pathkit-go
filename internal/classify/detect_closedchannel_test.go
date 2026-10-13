package classify

import (
	"strings"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/scope"
)

const closedFixtures = "../../testdata/fixtures/closedchannel"

func TestClosedSignalChannel(t *testing.T) {
	const cant, review = CannotBeTested, NeedsReview
	checkExpectations(t, closedFixtures, "closedchannel.", []expectation{
		// catch: the closed side of a signal channel's more
		{"CatchAddReceive", "J2.true", cant, "closed-signal-channel"},
		{"CatchReceive", "J1.true", cant, "closed-signal-channel"},
		{"CatchDirectCall", "J1.true", cant, "closed-signal-channel"},
		{"CatchReceiveWithTimeout", "J1.true", cant, "closed-signal-channel"},
		{"CatchReceiveAsyncMore", "J1.true", cant, "closed-signal-channel"},
		{"CatchWithOptions", "J1.true", cant, "closed-signal-channel"},
		{"CatchEqFalse", "J1.true", cant, "closed-signal-channel"},
		{"CatchFalseEq", "J1.true", cant, "closed-signal-channel"},
		{"CatchNeTrue", "J1.true", cant, "closed-signal-channel"},
		{"CatchElseSide", "J1.false", cant, "closed-signal-channel"},
		{"CatchInitStatement", "J1.true", cant, "closed-signal-channel"},
		// catch: declared with no value, assigned once before any use
		{"CatchLateAssigned", "J1.true", cant, "closed-signal-channel"},
		{"CatchLateAssignedAddReceive", "J2.true", cant, "closed-signal-channel"},
		// near-miss: the workflow's own channel can be closed by the workflow
		{"OwnClosed", "", "", ""},
		{"OwnBuffered", "", "", ""},
		{"OwnNamed", "", "", ""},
		{"OwnReceiveWithTimeout", "", "", ""},
		{"OwnAddReceive", "", "", ""},
		// near-miss: origin unknown is never "impossible"
		{"ParamChannel", "J1.true", review, "needs-review"},
		{"HelperChannel", "J1.true", review, "needs-review"},
		{"TwiceAssigned", "J1.true", review, "needs-review"},
		{"SetInABranch", "J2.true", review, "needs-review"},
		{"FieldChannel", "J1.true", review, "needs-review"},
		{"ClosureChannel", "J2.true", review, "needs-review"},
		{"ElementChannel", "J1.true", review, "needs-review"},
		{"NamedResult", "J1.true", review, "needs-review"},
		{"AssertedClose", "J1.true", review, "needs-review"},
		{"PassedOn", "J1.true", review, "needs-review"},
		// near-miss: the late-assignment shape, one thing off
		{"LateOwnClosed", "J1.true", review, "needs-review"}, // aliased own channel: unknown, never impossible
		{"LateAssignedTwice", "J1.true", review, "needs-review"},
		{"LateUsedBefore", "J1.true", review, "needs-review"},
		{"LateInIf", "J2.true", review, "needs-review"},
		{"LateAddress", "J1.true", review, "needs-review"},
		{"LateParenAddress", "J1.true", review, "needs-review"},
		{"LateClosure", "J2.true", review, "needs-review"},
		{"LateNestedBlock", "J1.true", review, "needs-review"},
		// near-miss: not a closed-channel check
		{"PlainBoolMore", "", "", ""},
		{"ShadowedMore", "", "", ""},
		{"CompoundCondition", "", "", ""},
		{"OkResult", "", "", ""},
		{"NotTheSDK", "", "", ""},
	})
}

// The explanation says which decision and which signal.
func TestClosedSignalChannelExplanation(t *testing.T) {
	for _, d := range detected(t, closedFixtures, "closedchannel.CatchReceive") {
		if d.C.Label != "closed-signal-channel" {
			continue
		}
		for _, want := range []string{"J1", "if !more", `"orders"`, "more == false"} {
			if !strings.Contains(d.C.Explanation, want) {
				t.Errorf("explanation %q lacks %q", d.C.Explanation, want)
			}
		}
		if d.C.Confidence != Certain || d.C.PossibleInProduction {
			t.Errorf("want certain and not possible in production: %+v", d.C)
		}
		return
	}
	t.Fatal("no path was marked")
}

// Turning the detector off leaves every path testable and the engine says so.
func TestDetectorDisable(t *testing.T) {
	off := scope.DisabledDetector{Name: "closed-signal-channel", Reason: "we close signal channels in tests"}
	for _, d := range detected(t, closedFixtures, "closedchannel.CatchReceive", off) {
		if d.C.Bucket != Testable {
			t.Errorf("path %s is %s with the detector off", d.Path.Key(), d.C.Bucket)
		}
	}
	e, err := NewEngine(scope.Classification{Enabled: true, DisabledDetectors: []scope.DisabledDetector{off}})
	if err != nil {
		t.Fatal(err)
	}
	warn := strings.Join(e.Findings(nil, nil), "\n")
	if !strings.Contains(warn, "closed-signal-channel") || !strings.Contains(warn, "we close signal channels in tests") {
		t.Errorf("no warning for the disabled detector: %q", warn)
	}
	notes := strings.Join(e.Notes(nil, nil), "\n")
	if !strings.Contains(notes, "turned off") {
		t.Errorf("the block does not say the detector is off: %q", notes)
	}
	if _, err := NewEngine(scope.Classification{Enabled: true, DisabledDetectors: []scope.DisabledDetector{{Name: "closed-signal-chanel", Reason: "x"}}}); err == nil || !strings.Contains(err.Error(), "closed-signal-channel") {
		t.Errorf("a misspelled detector name must be an error that lists the real names, got %v", err)
	}
}

// Overrides win over a detector; the detector's finding is kept as an extra reason.
func TestDetectorBelowOverride(t *testing.T) {
	paths := detected(t, closedFixtures, "closedchannel.CatchReceive")
	var sid string
	for _, d := range paths {
		if d.C.Label == "closed-signal-channel" {
			sid = d.Path.StableID()
		}
	}
	e, err := NewEngine(scope.Classification{Enabled: true, Overrides: []scope.PathOverride{
		{Workflow: "closedchannel.CatchReceive", StableID: sid, Reason: "we do close it", AddedBy: "x"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range paths {
		if d.Path.StableID() != sid {
			continue
		}
		c := e.Classify("closedchannel.CatchReceive", d.Path)
		if c.Bucket != Testable || c.Origin != OriginOverride {
			t.Errorf("an override must win: %+v", c)
		}
		if len(c.ExtraReasons) != 1 || !strings.Contains(c.ExtraReasons[0], "closed-signal-channel") {
			t.Errorf("extra reasons = %v", c.ExtraReasons)
		}
	}
}

// A closed-channel finding and an unknown-origin one are counted under the
// detector's name and bucket, never under the "needs-review" label.
func TestDetectorNotesNameTheDetector(t *testing.T) {
	e, err := NewEngine(scope.Classification{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	var final []Classification
	for _, name := range []string{"closedchannel.CatchReceive", "closedchannel.ParamChannel"} {
		for _, d := range detected(t, closedFixtures, name) {
			final = append(final, e.Classify(name, d.Path))
		}
	}
	notes := strings.Join(e.Notes(final, nil), "\n")
	for _, want := range []string{
		"Detector closed-signal-channel: 1 path marked cannot-be-tested",
		"Detector closed-signal-channel: 1 path marked needs-review",
	} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q:\n%s", want, notes)
		}
	}
	if strings.Contains(notes, "Detector needs-review") {
		t.Errorf("a label was printed as a detector:\n%s", notes)
	}
}

// Found by the independent review: another handle of the same named signal
// channel is turned into a Channel and closed (also through a helper), so
// the closed side is reachable. Nothing in such a package may be called
// impossible, for the channels that could be closed.
func TestSignalChannelClosedThroughAnotherHandle(t *testing.T) {
	const review = NeedsReview
	checkExpectations(t, "../../testdata/fixtures/closedchannelleak", "closedchannelleak.", []expectation{
		{"SameNameClosed", "J1.true", review, "needs-review"},
		{"InlineClose", "J1.true", review, "needs-review"},
		{"HelperClosed", "J1.true", review, "needs-review"},
		// a helper that closes "any" channel: every signal in the package
		{"OtherName", "J1.true", review, "needs-review"},
	})
}
