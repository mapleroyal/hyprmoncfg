package apply

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrQueryTimeout distinguishes a retryable compositor read from an apply or
// rollback failure. Callers retain the full error if rollback also fails.
var ErrQueryTimeout = errors.New("compositor query timed out")

func query[T any](ctx context.Context, engine Engine, operation string, read func(context.Context) (T, error)) (T, error) {
	if engine.QueryTimeout <= 0 {
		return read(ctx)
	}
	queryCtx, cancel := context.WithTimeout(ctx, engine.QueryTimeout)
	defer cancel()
	started := time.Now()
	result, err := read(queryCtx)
	if errors.Is(queryCtx.Err(), context.DeadlineExceeded) {
		var zero T
		result = zero
		// An inherited apply/validation deadline is an exhausted attempt,
		// not a stalled individual read eligible for the daemon's busy retry.
		if ctx.Err() != nil {
			err = ctx.Err()
		} else {
			err = fmt.Errorf("%w (%s): %w", ErrQueryTimeout, operation, queryCtx.Err())
		}
	}
	if engine.Logf != nil && (err != nil || time.Since(started) >= 100*time.Millisecond) {
		engine.Logf("apply query operation=%s elapsed=%s error=%v", operation, time.Since(started).Round(time.Millisecond), err)
	}
	return result, err
}
