package pubsub

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/syukronhidayat/nearby-drivers/internal/contracts"
	"github.com/syukronhidayat/nearby-drivers/internal/store/redisstore"
	"github.com/syukronhidayat/nearby-drivers/internal/ws"
)

type DriverUpdatesConsumer struct {
	RDB redis.UniversalClient
	Hub *ws.Hub

	// Batching controls
	FlushEvery time.Duration // default 50ms
	MaxPending int           // default 500
}

func (c *DriverUpdatesConsumer) Run(ctx context.Context) error {
	sub := c.RDB.Subscribe(ctx, redisstore.ChannelDriverUpdates)
	defer func() { _ = sub.Close() }()

	flushEvery := c.FlushEvery
	if flushEvery <= 0 {
		flushEvery = 50 * time.Millisecond
	}
	maxPending := c.MaxPending
	if maxPending <= 0 {
		maxPending = 500
	}

	pending := make(map[string]contracts.DriverLocation, maxPending)
	flush := func() {
		if len(pending) == 0 {
			return
		}
		for _, loc := range pending {
			c.Hub.OnDriverUpsert(loc)
		}
		// clear map without realloc
		for k := range pending {
			delete(pending, k)
		}
	}

	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			flush()
			return ctx.Err()

		case <-ticker.C:
			flush()

		case msg, ok := <-ch:
			if !ok {
				flush()
				return nil
			}

			var up redisstore.DriverUpdateMessage
			if err := json.Unmarshal([]byte(msg.Payload), &up); err != nil {
				log.Printf("pubsub decode error: %v", err)
				continue
			}

			pending[up.Driver.DriverID] = up.Driver

			if len(pending) >= maxPending {
				flush()
			}
		}
	}
}
