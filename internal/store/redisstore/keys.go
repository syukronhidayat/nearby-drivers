package redisstore

const (
	KeyDriverGeo         = "driver:geo"
	KeyDriverUpdated     = "driver:updated"
	ChannelDriverUpdates = "driver:updates"
)

func KeyDriverMeta(driverID string) string {
	return "driver:" + driverID
}
