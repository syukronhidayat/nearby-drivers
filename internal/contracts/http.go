package contracts

type PostDriverLocationRequest struct {
	DriverID    string        `json:"driverId"`
	Lat         float64       `json:"lat"`
	Lon         float64       `json:"lon"`
	TimestampMs int64         `json:"timestamp"`
	Status      *DriverStatus `json:"status,omitempty"`
	Bearing     *float64      `json:"bearing,omitempty"`
	Speed       *float64      `json:"speed,omitempty"`
	Accuracy    *float64      `json:"accuracy,omitempty"`
}

type PostDriverLocationResponse struct {
	Accepted  bool `json:"accepted"`
	Throttled bool `json:"throttled,omitempty"`
}

type GetDriversNearbyQuery struct {
	Lat            float64 `json:"lat"`
	Lon            float64 `json:"lon"`
	RadiusMiles    float64 `json:"radiusMiles"`
	MaxAgeSec      int64   `json:"maxAgeSec"`
	IncludeOffline bool    `json:"includeOffline"`
	Limit          int     `json:"limit"`
}

type GetDriversNearbyResponse struct {
	Drivers []DriverView `json:"drivers"`
}

type HTTPErrorResponse struct {
	Error struct {
		Code    HTTPErrorCode `json:"code"`
		Message string        `json:"message"`
		Details any           `json:"details,omitempty"`
	} `json:"error"`
}
