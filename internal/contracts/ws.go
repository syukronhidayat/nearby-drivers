package contracts

import "encoding/json"

type WSEnvelope struct {
	Type      WSMsgType       `json:"type"`
	RequestID *string         `json:"requestId,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

type WSSubscribeLocationDriverPayload struct {
	SubscriptionID string `json:"subscriptionId"`
	Pickup         struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
	} `json:"pickup"`
	RadiusMiles    float64 `json:"radiusMiles"`
	MaxAgeSec      int64   `json:"maxAgeSec"`
	IncludeOffline bool    `json:"includeOffline"`
}

type WSUnsubscribePayload struct {
	SubscriptionID string `json:"subscriptionId"`
}
type DriverLocation struct {
	DriverID    string        `json:"driverId"`
	Lat         float64       `json:"lat"`
	Lon         float64       `json:"lon"`
	TimestampMs int64         `json:"timestamp"`
	Status      *DriverStatus `json:"status,omitempty"`
	Bearing     *float64      `json:"bearing,omitempty"`
	Speed       *float64      `json:"speed,omitempty"`
	Accuracy    *float64      `json:"accuracy,omitempty"`
}

type DriverView struct {
	DriverLocation
	DistanceMiles float64 `json:"distanceMiles"`
}

type WSDriversInitialPayload struct {
	SubscriptionID string       `json:"subscriptionId"`
	Drivers        []DriverView `json:"drivers"`
}

type WSDriverAddedPayload struct {
	SubscriptionID string     `json:"subscriptionId"`
	Driver         DriverView `json:"driver"`
}

type WSDriverUpdatedPayload struct {
	SubscriptionID string     `json:"subscriptionId"`
	Driver         DriverView `json:"driver"`
}
type WSDriverRemovedPayload struct {
	SubscriptionID string          `json:"subscriptionId"`
	DriverID       string          `json:"driverId"`
	Reason         RemovalReason   `json:"reason"`
	LastKnown      *DriverLocation `json:"lastKnown,omitempty"`
}
type WSErrorPayload struct {
	Code    HTTPErrorCode `json:"code"`
	Message string        `json:"message"`
	Details any           `json:"details,omitempty"`
}
