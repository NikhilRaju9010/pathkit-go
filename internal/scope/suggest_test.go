package scope

import "testing"

func TestSuggest(t *testing.T) {
	known := []string{"shipment.ShipmentWorkflow", "orders.OrderWorkflow", "fulfillment.PaymentWorkflow", "scope.Svc.Handle"}
	tests := []struct{ name, want string }{
		{"ShipmentWorkflw", "ShipmentWorkflow"},         // one letter missing
		{"shipmentworkflow", "ShipmentWorkflow"},        // case only: close, and still a hint
		{"orders.OrderWorkfow", "orders.OrderWorkflow"}, // written in full: hint in full
		{"Svc.Handel", "Svc.Handle"},                    // two letters swapped
		{"BillingWorkflow", ""},                         // not close to anything
		{"Pay", ""},                                     // too short to guess
	}
	for _, tt := range tests {
		if got := Suggest(tt.name, known); got != tt.want {
			t.Errorf("Suggest(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
