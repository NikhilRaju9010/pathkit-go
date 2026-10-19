package model_test

import (
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

func unreadOf(t *testing.T, name string) []model.Unread {
	t.Helper()
	wf, ok := workflowsIn(t, "../../testdata/fixtures/unread")["unread."+name]
	if !ok {
		t.Fatalf("fixture workflow unread.%s not found", name)
	}
	g, err := model.Build(wf)
	if err != nil {
		t.Fatal(err)
	}
	return g.Unread()
}

func TestUnreadKinds(t *testing.T) {
	for name, want := range map[string]string{
		"ViaInterface": model.UnreadInterfaceCall,
		"ViaFuncValue": model.UnreadFuncValue,
		"OtherPackage": model.UnreadOtherPackage,
		"OtherModule":  model.UnreadOtherModule,
		"ByName":       model.UnreadByName,
	} {
		got := unreadOf(t, name)
		if len(got) != 1 || got[0].Kind != want {
			t.Errorf("%s: got %+v, want one %q", name, got, want)
			continue
		}
		if got[0].Line == 0 || got[0].File == "" || got[0].Effect == "" {
			t.Errorf("%s: incomplete entry %+v", name, got[0])
		}
	}
}

// Near misses: a call PathKit reads fine, and a helper with no workflow work,
// are not listed.
func TestUnreadLeavesReadCallsOut(t *testing.T) {
	for _, name := range []string{"ByConstName", "NoWorkflowWork"} {
		if got := unreadOf(t, name); len(got) != 0 {
			t.Errorf("%s: listed %+v", name, got)
		}
	}
}

// A function that takes the context, returns an error and is not followed
// comes with the entry that would tell PathKit about it.
func TestUnreadSuggestsAWrapperEntry(t *testing.T) {
	got := unreadOf(t, "OtherPackage")
	if len(got) != 1 || got[0].Suggestion != "example.com/fixtures/unread/side.Audit" {
		t.Fatalf("got %+v", got)
	}
}

func unreadIn(t *testing.T, dir, name string) []model.Unread {
	t.Helper()
	wf, ok := workflowsIn(t, "../../testdata/constructs/"+dir)[name]
	if !ok {
		t.Fatalf("workflow %s not found in %s", name, dir)
	}
	g, err := model.Build(wf)
	if err != nil {
		t.Fatal(err)
	}
	return g.Unread()
}

// A future that is started and never read is listed; one that is read later,
// in a loop, or by a selector is not.
func TestUnreadFutureNeverRead(t *testing.T) {
	got := unreadIn(t, "future_unread", "future_unread.Run")
	if len(got) != 1 || got[0].Kind != model.UnreadFutureNeverRead || got[0].Line == 0 {
		t.Fatalf("got %+v", got)
	}
	for _, c := range [][2]string{{"future_later", "future_later.Run"}, {"futures_loop", "futures_loop.Run"}, {"future_selector", "future_selector.Run"}, {"timer_cancel", "timer_cancel.Run"}, {"timer_selector", "timer_selector.Run"}} {
		if got := unreadIn(t, c[0], c[1]); len(got) != 0 {
			t.Errorf("%s: listed %+v", c[1], got)
		}
	}
}

// A function told to be an activity call (classification.activity_wrappers)
// becomes a step with a failure and a success; without the entry it is listed
// as unread together with the entry that would fix it.
func TestActivityWrapperBecomesAStep(t *testing.T) {
	wf, ok := workflowsIn(t, "../../testdata/constructs/wrapper_activity")["wrapper_activity.Run"]
	if !ok {
		t.Fatal("workflow not found")
	}
	plain, err := model.Build(wf)
	if err != nil {
		t.Fatal(err)
	}
	if n := plain.PathCount().String(); n != "1" {
		t.Errorf("without the entry: %s paths, want 1", n)
	}
	u := plain.Unread()
	if len(u) != 1 || u[0].Suggestion != "example.com/constructs/wrapper_activity/wrap.Run" {
		t.Fatalf("unread: %+v", u)
	}
	g, err := model.BuildWith(wf, model.Options{Wrappers: []model.Wrapper{{Function: u[0].Suggestion, Kind: "activity"}}})
	if err != nil {
		t.Fatal(err)
	}
	if n := g.PathCount().String(); n != "2" {
		t.Errorf("with the entry: %s paths, want 2", n)
	}
	if len(g.Unread()) != 0 {
		t.Errorf("still listed as unread: %+v", g.Unread())
	}
	if lab := g.Junctions[0].Label; lab != "Run (activity)" {
		t.Errorf("label %q", lab)
	}
}
