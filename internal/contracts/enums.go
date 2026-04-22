package contracts

type DriverStatus string

const (
	DriverStatusAvailable DriverStatus = "available"
	DriverStatusBusy      DriverStatus = "busy"
	DriverStatusOffline   DriverStatus = "offline"
)

type RemovalReason string

const (
	RemovalReasonOffline       RemovalReason = "offline"
	RemovalReasonOutsideRadius RemovalReason = "outside_radius"
	RemovalReasonStaleLocation RemovalReason = "stale_location"
)

type WSMsgType string

const (
	// client -> server
	WSMsgSubscribeLocationDrivers WSMsgType = "subscribe_location_drivers"
	WSMsgUnsubscribe              WSMsgType = "unsubscribe"

	// server -> client
	WSMsgDriversInitial WSMsgType = "drivers_initial"
	WSMsgDriverAdded    WSMsgType = "driver_added"
	WSMsgDriverUpdated  WSMsgType = "driver_updated"
	WSMsgDriverRemoved  WSMsgType = "driver_removed"
	WSMsgError          WSMsgType = "error"
)

type HTTPErrorCode string

const (
	HTTPErrorValidation  HTTPErrorCode = "validation_error"
	HTTPErrorUnsupported HTTPErrorCode = "unsupported_media_type"
	HTTPErrorInternal    HTTPErrorCode = "internal_error"
)
