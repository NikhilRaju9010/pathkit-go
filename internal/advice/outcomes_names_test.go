package advice

import (
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/coverage"
	"github.com/NikhilRaju9010/pathkit-go/internal/model"
)

func TestOutcomeNamesFollowTheDecision(t *testing.T) {
	cases := []struct {
		o    coverage.OutcomeResult
		want string
	}{
		{coverage.OutcomeResult{Kind: model.PlainIf, Decision: "if !ok", Outcome: "true"}, "TestOrder_NotOkTrue"},
		{coverage.OutcomeResult{Kind: model.PlainIf, Decision: "if !ok", Outcome: "false"}, "TestOrder_NotOkFalse"},
		{coverage.OutcomeResult{Kind: model.PlainIf, Decision: "if workflow.GetVersion(ctx, versionX, workflow.DefaultVersion, 1) != workflow.DefaultVersion", Outcome: "false"}, "TestOrder_OldVersion"},
	}
	for _, c := range cases {
		if got := outcomeTestName("pkg.OrderWorkflow", []string{"x"}, c.o, 1); got != c.want {
			t.Errorf("%+v: %q, want %q", c.o, got, c.want)
		}
	}
	// near miss: the new side of a version check is not the old version
	o := coverage.OutcomeResult{Kind: model.PlainIf, Decision: "if workflow.GetVersion(ctx, v, workflow.DefaultVersion, 1) != workflow.DefaultVersion", Outcome: "true"}
	if oldVersionOutcome(o) {
		t.Error("the new side is not the old version")
	}
	sig := coverage.OutcomeResult{Kind: model.WaitResult, Decision: "if workflow.GetSignalChannel(ctx, SignalCancel).ReceiveAsync(&dummy) (ReceiveAsync)", Outcome: "received"}
	if got := outcomeTestName("pkg.OrderWorkflow", nil, sig, 1); got != "TestOrder_SignalCancelReceived" {
		t.Errorf("signal name %q", got)
	}
}
