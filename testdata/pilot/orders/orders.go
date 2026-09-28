// Package orders holds the simplest pilot workflow: one plain if and one
// activity call whose error is checked.
package orders

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/workflow"
)

type Order struct {
	ID          string
	SKU         string
	AmountCents int64
}

type Receipt struct {
	ID string
}

// OrderWorkflow charges the customer for a valid order.
func OrderWorkflow(ctx workflow.Context, in Order) (string, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})

	if in.AmountCents <= 0 {
		return "rejected", nil
	}

	// A plain Go error check, not a Temporal call: PathKit treats it as
	// transparent (D2).
	sku, err := parseSKU(in.SKU)
	if err != nil {
		return "", err
	}
	in.SKU = sku

	var receipt Receipt
	err = workflow.ExecuteActivity(ctx, ChargeCard, in).Get(ctx, &receipt)
	if err != nil {
		return "", fmt.Errorf("charge: %w", err)
	}
	return receipt.ID, nil
}

func parseSKU(s string) (string, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return "", errors.New("empty SKU")
	}
	return s, nil
}
