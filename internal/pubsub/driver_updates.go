package pubsub

import (
	"context"
	"encoding/json"
	"log"

	"github.com/redis/go-redis/v9"
	"github.com/syukronhidayat/nearby-drivers/internal/store/redisstore"
	"github.com/syukronhidayat/nearby-drivers/internal/ws"
)

type DriverUpdatesConsumer struct {
	RDB redis.UniversalClient
	Hub *ws.Hub
}

func (c *DriverUpdatesConsumer) Run(ctx context.Context) error {
	sub := c.RDB.Subscribe(ctx, redisstore.ChannelDriverUpdates)
	defer func() { _ = sub.Close() }()

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			var up redisstore.DriverUpdateMessage
			if err := json.Unmarshal([]byte(msg.Payload), &up); err != nil {
				log.Printf("pubsub decode error: %v", err)
				continue
			}
			c.Hub.OnDriverUpsert(up.Driver)
		}
	}
}
