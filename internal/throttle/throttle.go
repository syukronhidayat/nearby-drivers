package throttle

import (
	"context"
	"time"
)

type Throttler interface {
	// Returns throttled=true if caller should skip ingest but still return 200 OK.
	ShouldThrottle(ctx context.Context, driverID string, now time.Time) (throttled bool, err error)
}
