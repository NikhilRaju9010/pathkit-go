package billing

import "context"

// ChargeMonthly is a fake call to the billing provider.
func ChargeMonthly(ctx context.Context, s Subscription) error {
	return nil
}
