package polling

import "context"

// CheckStatus is a fake call to the reporting service.
func CheckStatus(ctx context.Context, reportID string) (string, error) {
	return "complete", nil
}
