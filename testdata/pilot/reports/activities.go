package reports

import (
	"context"
	"time"
)

// BuildReport is a fake database query.
func BuildReport(ctx context.Context, since time.Time) (Summary, error) {
	return Summary{GeneratedAt: time.Now(), Rows: 0}, nil
}

// EmailReport is a fake email call.
func EmailReport(ctx context.Context, s Summary) error {
	return nil
}
