package redisstore

import "github.com/syukronhidayat/nearby-drivers/internal/contracts"

type DriverUpdateType string

const (
	DriverUpdateUpsert DriverUpdateType = "upsert"
)

type DriverUpdateMessage struct {
	Type   DriverUpdateType         `json:"type"`
	Driver contracts.DriverLocation `json:"driver"`
}
