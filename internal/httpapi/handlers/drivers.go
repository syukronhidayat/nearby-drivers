package handlers

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/syukronhidayat/nearby-drivers/internal/contracts"
	"github.com/syukronhidayat/nearby-drivers/internal/store/redisstore"
	"github.com/syukronhidayat/nearby-drivers/internal/throttle"
)

type DriversHandler struct {
	Store *redisstore.Store

	Throttler throttle.Throttler
	MetaTTL   time.Duration

	FutureSkew time.Duration
	MaxPast    time.Duration
}

func (h *DriversHandler) PostDriverLocation(w http.ResponseWriter, r *http.Request) {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		writeHTTPError(w, http.StatusUnsupportedMediaType, contracts.HTTPErrorUnsupported, "expected application/json", nil)
		return
	}

	var req contracts.PostDriverLocationRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeHTTPError(w, http.StatusBadRequest, contracts.HTTPErrorValidation, "invalid JSON", map[string]any{"error": err.Error()})
		return
	}

	issues := validatePostDriverLocation(req)
	if len(issues) > 0 {
		writeHTTPError(w, http.StatusBadRequest, contracts.HTTPErrorValidation, "invalid request", map[string]any{"issues": issues})
		return
	}

	now := time.Now()

	normalized, nIssues := h.normalizePostDriverLocation(req, now)
	if len(nIssues) > 0 {
		writeHTTPError(w, http.StatusBadRequest, contracts.HTTPErrorValidation, "invalid request", map[string]any{"issues": nIssues})
		return
	}

	if h.Throttler != nil {
		throttled, err := h.Throttler.ShouldThrottle(r.Context(), normalized.DriverID, now)
		if err != nil && throttled {
			writeJSON(w, http.StatusOK, contracts.PostDriverLocationResponse{Accepted: true, Throttled: true})
			return
		}
	}

	ttl := h.MetaTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}

	accepted, err := h.Store.WriteDriverLocation(r.Context(), normalized, ttl)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, contracts.HTTPErrorInternal, "internal server error", nil)
		return
	}

	_ = accepted
	writeJSON(w, http.StatusOK, contracts.PostDriverLocationResponse{Accepted: true})
}

func (h *DriversHandler) GetDriversNearby(w http.ResponseWriter, r *http.Request) {
	q, issues := parseNearbyQuery(r)
	if len(issues) > 0 {
		writeHTTPError(w, http.StatusBadRequest, contracts.HTTPErrorValidation, "invalid query", map[string]any{"issues": issues})
		return
	}

	results, err := h.Store.GetNearbyDriverIDs(r.Context(), redisstore.NearbyQuery{
		PickupLat:   q.Lat,
		PickupLon:   q.Lon,
		RadiusMiles: q.RadiusMiles,
		Limit:       q.Limit,
	})
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, contracts.HTTPErrorInternal, "internal server error", nil)
		return
	}

	ids := make([]string, 0, len(results))
	distByID := make(map[string]float64, len(results))
	for _, it := range results {
		ids = append(ids, it.DriverID)
		distByID[it.DriverID] = it.DistanceMiles
	}

	locs, err := h.Store.LoadDriverLocations(r.Context(), ids)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, contracts.HTTPErrorInternal, "internal server error", nil)
		return
	}

	updated, err := h.Store.LoadDriverUpdatedMs(r.Context(), ids)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, contracts.HTTPErrorInternal, "internal server error", nil)
		return
	}

	nowMs := time.Now().UnixMilli()
	maxAgeMs := q.MaxAgeSec * 1000

	out := make([]contracts.DriverView, 0, len(ids))
	for _, id := range ids {
		loc, ok := locs[id]
		if !ok {
			continue
		}
		updatedMs, ok := updated[id]
		if !ok {
			updatedMs = loc.TimestampMs
		}

		if maxAgeMs > 0 && (nowMs-updatedMs) > maxAgeMs {
			continue
		}

		if !q.IncludeOffline && loc.Status != nil && *loc.Status == contracts.DriverStatusOffline {
			continue
		}

		out = append(out, contracts.DriverView{
			DriverLocation: loc,
			DistanceMiles:  distByID[id],
		})
	}

	writeJSON(w, http.StatusOK, contracts.GetDriversNearbyResponse{Drivers: out})
}

func validatePostDriverLocation(req contracts.PostDriverLocationRequest) []map[string]any {
	var issues []map[string]any

	if strings.TrimSpace(req.DriverID) == "" {
		issues = append(issues, issue("driverId", "must be non-empty"))
	}
	if !isFinite(req.Lat) || req.Lat < -90 || req.Lat > 90 {
		issues = append(issues, issue("lat", "must be between -90 and 90"))
	}
	if !isFinite(req.Lon) || req.Lon < -180 || req.Lon > 180 {
		issues = append(issues, issue("lon", "must be between -180 and 180"))
	}
	if req.TimestampMs <= 0 {
		issues = append(issues, issue("timestamp", "must be > 0 (ms since epoch)"))
	}
	if req.Status != nil {
		switch *req.Status {
		case contracts.DriverStatusAvailable, contracts.DriverStatusBusy, contracts.DriverStatusOffline:
		default:
			issues = append(issues, issue("status", "must be one of available|busy|offline"))
		}
	}
	if req.Bearing != nil {
		if !isFinite(*req.Bearing) || *req.Bearing < 0 || *req.Bearing >= 360 {
			issues = append(issues, issue("bearing", "must be in [0, 360)"))
		}
	}
	if req.Speed != nil {
		if !isFinite(*req.Speed) || *req.Speed < 0 {
			issues = append(issues, issue("speed", "must be >= 0"))
		}
	}
	if req.Accuracy != nil {
		if !isFinite(*req.Accuracy) || *req.Accuracy < 0 {
			issues = append(issues, issue("accuracy", "must be >= 0"))
		}
	}
	return issues
}

func (h *DriversHandler) normalizePostDriverLocation(req contracts.PostDriverLocationRequest, now time.Time) (contracts.PostDriverLocationRequest, []map[string]any) {
	var issues []map[string]any

	req.DriverID = strings.TrimSpace(req.DriverID)

	futureSkew := h.FutureSkew
	if futureSkew <= 0 {
		futureSkew = 60 * time.Second
	}
	maxPast := h.MaxPast
	if maxPast <= 0 {
		maxPast = 24 * time.Hour
	}

	nowMs := now.UnixMilli()
	ts := req.TimestampMs

	if ts > nowMs+futureSkew.Milliseconds() {
		ts = nowMs
	}

	if ts < nowMs-maxPast.Milliseconds() {
		issues = append(issues, issue("timestamp", "too old"))
	}

	req.TimestampMs = ts

	return req, issues
}

type nearbyQuery struct {
	Lat            float64
	Lon            float64
	RadiusMiles    float64
	MaxAgeSec      int64
	IncludeOffline bool
	Limit          int
}

func parseNearbyQuery(r *http.Request) (nearbyQuery, []map[string]any) {
	var issues []map[string]any
	qp := r.URL.Query()

	lat, ok := parseFloat(qp.Get("lat"))
	if !ok || lat < -90 || lat > 90 {
		issues = append(issues, issue("lat", "must be between -90 and 90"))
	}
	lon, ok := parseFloat(qp.Get("lon"))
	if !ok || lon < -180 || lon > 180 {
		issues = append(issues, issue("lon", "must be between -180 and 180"))
	}

	radius, ok := parseFloat(qp.Get("radiusMiles"))
	if !ok || radius <= 0 || radius > 50 {
		issues = append(issues, issue("radiusMiles", "must be > 0 and <= 50"))
	}

	maxAgeSec, ok := parseInt64(qp.Get("maxAgeSec"))
	if !ok || maxAgeSec <= 0 || maxAgeSec > 3600 {
		issues = append(issues, issue("maxAgeSec", "must be > 0 and <= 3600"))
	}

	includeOffline := false
	if v := qp.Get("includeOffline"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			issues = append(issues, issue("includeOffline", "must be a boolean"))
		} else {
			includeOffline = b
		}
	}

	limit := 50
	if v := qp.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 500 {
			issues = append(issues, issue("limit", "must be between 1 and 500"))
		} else {
			limit = n
		}
	}

	return nearbyQuery{
		Lat:            lat,
		Lon:            lon,
		RadiusMiles:    radius,
		MaxAgeSec:      maxAgeSec,
		IncludeOffline: includeOffline,
		Limit:          limit,
	}, issues
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeHTTPError(w http.ResponseWriter, status int, code contracts.HTTPErrorCode, message string, details any) {
	var resp contracts.HTTPErrorResponse
	resp.Error.Code = code
	resp.Error.Message = message
	resp.Error.Details = details
	writeJSON(w, status, resp)
}

func issue(field, msg string) map[string]any {
	return map[string]any{"field": field, "issue": msg}
}

func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

func parseFloat(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil && isFinite(f)
}

func parseInt64(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}
