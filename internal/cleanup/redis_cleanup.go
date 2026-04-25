package cleanup

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/syukronhidayat/nearby-drivers/internal/store/redisstore"
)

type RedisCleanup struct {
	RDB       redis.UniversalClient
	Every     time.Duration
	MaxAge    time.Duration
	BatchSize int
}

func (c *RedisCleanup) Run(ctx context.Context) error {
	if c.RDB == nil {
		return fmt.Errorf("redis cleanup: RDB not configured")
	}

	every := c.Every
	if every <= 0 {
		every = 60 * time.Second
	}
	maxAge := c.MaxAge
	if maxAge <= 0 {
		maxAge = 10 * time.Minute
	}
	batch := c.BatchSize
	if batch <= 0 {
		batch = 1000
	}

	t := time.NewTicker(every)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			_ = c.sweepOnce(ctx, time.Now(), maxAge, batch)
		}
	}
}

func (c *RedisCleanup) sweepOnce(ctx context.Context, now time.Time, maxAge time.Duration, batch int) error {
	cutOffMs := now.Add(-maxAge).UnixMilli()

	sweepCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	for {
		ids, err := c.RDB.ZRangeByScore(sweepCtx, redisstore.KeyDriverUpdated, &redis.ZRangeBy{
			Min:    "-inf",
			Max:    fmt.Sprintf("(%d", cutOffMs),
			Offset: 0,
			Count:  int64(batch),
		}).Result()
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}

		metaKeys := make([]string, 0, len(ids))
		for _, id := range ids {
			metaKeys = append(metaKeys, redisstore.KeyDriverMeta(id))
		}

		pipe := c.RDB.Pipeline()
		pipe.ZRem(sweepCtx, redisstore.KeyDriverUpdated, anySlice(ids)...)
		pipe.ZRem(sweepCtx, redisstore.KeyDriverGeo, anySlice(ids)...)
		pipe.Del(sweepCtx, metaKeys...)
		if _, err := pipe.Exec(sweepCtx); err != nil {
			return err
		}
	}
}

func anySlice(ss []string) []any {
	out := make([]any, 0, len(ss))
	for _, s := range ss {
		out = append(out, s)
	}
	return out
}
