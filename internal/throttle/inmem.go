package throttle

import (
	"context"
	"sync"
	"time"
)

type InMem struct {
	MinInterval time.Duration

	mu   sync.Mutex
	next map[string]time.Time
}

func NewInMem(minInterval time.Duration) *InMem {
	return &InMem{
		MinInterval: minInterval,
		next:        make(map[string]time.Time),
	}
}

func (t *InMem) ShouldThrottle(ctx context.Context, driverID string, now time.Time) (bool, error) {
	_ = ctx
	t.mu.Lock()
	defer t.mu.Unlock()

	if until, ok := t.next[driverID]; ok && now.Before(until) {
		return true, nil
	}

	t.next[driverID] = now.Add(t.MinInterval)
	return false, nil
}
