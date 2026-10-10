package chat

import (
	"context"
	"time"
)

// waitRetryAfter honors a server-provided retry delay without preventing the
// user from canceling the pending request.
func waitRetryAfter(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
