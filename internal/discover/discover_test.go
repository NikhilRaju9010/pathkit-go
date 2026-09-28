package discover_test

import (
	"slices"
	"testing"

	"github.com/NikhilRaju9010/pathkit-go/internal/discover"
	"github.com/NikhilRaju9010/pathkit-go/internal/load"
)

func names(t *testing.T, arg string) []string {
	t.Helper()
	res, err := load.Load(arg)
	if err != nil {
		t.Fatalf("load %s: %v", arg, err)
	}
	var out []string
	for _, wf := range discover.Find(res.Packages) {
		out = append(out, wf.Name)
	}
	return out
}

func TestPilotWorkflowsFound(t *testing.T) {
	got := names(t, "../../testdata/pilot/...")
	want := []string{
		"approval.ApprovalWorkflow",
		"billing.SubscriptionWorkflow",
		"fulfillment.OrderFulfillmentWorkflow",
		"fulfillment.PaymentWorkflow",
		"orders.OrderWorkflow",
		"polling.ReportPollingWorkflow",
		"reports.DailyReportWorkflow",
		"shipment.ShipmentWorkflow",
	}
	if !slices.Equal(got, want) {
		t.Errorf("found %v\nwant  %v", got, want)
	}
	// Take a workflow.Context but are not workflows (EXPECTED.md).
	for _, notWorkflow := range []string{"fulfillment.newChildCtx", "fulfillment.AuditLog"} {
		if slices.Contains(got, notWorkflow) {
			t.Errorf("%s must not be listed as a workflow", notWorkflow)
		}
	}
}

func TestFixtureRules(t *testing.T) {
	got := names(t, "../../testdata/fixtures/rules")
	if !slices.Contains(got, "rules.Service.MethodWorkflow") {
		t.Errorf("method workflow not found in %v", got)
	}
	for _, notWorkflow := range []string{"rules.unexportedWorkflow", "rules.NoErrorResult", "rules.NoContext"} {
		if slices.Contains(got, notWorkflow) {
			t.Errorf("%s must not be listed as a workflow", notWorkflow)
		}
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct{ arg, want string }{
		{"nope.go", "Workflow file not found: nope.go"},
		{"nope-dir", "Directory not found: nope-dir"},
		{"nope-dir/...", "Directory not found: nope-dir"},
		{"../../testdata/fixtures/broken", "package does not compile: "},
	}
	for _, tt := range tests {
		_, err := load.Load(tt.arg)
		if err == nil || len(err.Error()) < len(tt.want) || err.Error()[:len(tt.want)] != tt.want {
			t.Errorf("Load(%q) error = %v, want prefix %q", tt.arg, err, tt.want)
		}
	}
}
