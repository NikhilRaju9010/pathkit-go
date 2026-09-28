package orders

import "context"

// ChargeCard is a fake payment call.
func ChargeCard(ctx context.Context, in Order) (Receipt, error) {
	return Receipt{ID: "rcpt-" + in.ID}, nil
}
