package shipment

import "context"

// CreateLabel is a fake call to the carrier.
func CreateLabel(ctx context.Context, in Shipment) (string, error) {
	return "label-" + in.OrderID, nil
}

// NotifyCustomer is a fake email/SMS call.
func NotifyCustomer(ctx context.Context, orderID, outcome string) error {
	return nil
}
