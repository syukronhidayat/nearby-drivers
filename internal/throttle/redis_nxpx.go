package throttle

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisNXPX struct {
	RDB         redis.UniversalClient
	MinInterval time.Duration
	KeyPrefix   string
}

func (t *RedisNXPX) ShouldThrottle(ctx context.Context, driverID string, now time.Time) (bool, error) {
	_ = now
	if t.RDB == nil {
		return false, fmt.Errorf("redis throttle not configured")
	}
	if t.KeyPrefix == "" {
		t.KeyPrefix = "throttle:driver:"
	}
	ok, err := t.RDB.SetNX(ctx, t.KeyPrefix+driverID, "1", t.MinInterval).Result()
	if err != nil {
		return false, err
	}
	return !ok, nil
}
