package fulfillment

import "context"

// Fake calls to the warehouse, payment provider and carrier.

func ReserveInventory(ctx context.Context, o Order) error { return nil }

func ReleaseInventory(ctx context.Context, o Order) error { return nil }

func ShipOrder(ctx context.Context, o Order, paymentID string) error { return nil }

func AuthorizeCard(ctx context.Context, o Order) (string, error) { return "auth-" + o.ID, nil }

func FraudReview(ctx context.Context, o Order) error { return nil }
