package redisstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/syukronhidayat/nearby-drivers/internal/contracts"
)

type Store struct {
	RDB redis.UniversalClient
}

type NearbyQuery struct {
	PickupLat, PickupLon float64
	RadiusMiles          float64
	Limit                int
}

type NearbyResult struct {
	DriverID      string
	DistanceMiles float64
	Location      *contracts.DriverLocation
	UpdatedAtMs   *int64
}

// WriteDriverLocation writes geo+metadata+updated+pubsub, rejecting stale writes.
// Returns accepted=false when rejected due to stale timestamp.
func (s *Store) WriteDriverLocation(ctx context.Context, update contracts.PostDriverLocationRequest, ttl time.Duration) (accepted bool, err error) {
	if s == nil || s.RDB == nil {
		return false, errors.New("redis store not configured")
	}

	loc := contracts.DriverLocation{
		DriverID:    update.DriverID,
		Lat:         update.Lat,
		Lon:         update.Lon,
		TimestampMs: update.TimestampMs,
		Status:      update.Status,
		Bearing:     update.Bearing,
		Speed:       update.Speed,
		Accuracy:    update.Accuracy,
	}

	metaJSON, err := json.Marshal(loc)
	if err != nil {
		return false, fmt.Errorf("marshal meta: %w", err)
	}

	pub := DriverUpdateMessage{
		Type:   DriverUpdateUpsert,
		Driver: loc,
	}
	pubJSON, err := json.Marshal(pub)
	if err != nil {
		return false, fmt.Errorf("marshal pubsub: %w", err)
	}

	ttlMs := ttl.Milliseconds()
	if ttlMs <= 0 {
		return false, fmt.Errorf("ttl must be >0")
	}

	keys := []string{
		KeyDriverUpdated,
		KeyDriverGeo,
		KeyDriverMeta(update.DriverID),
	}

	args := []any{
		update.DriverID,
		update.TimestampMs,
		update.Lon,
		update.Lat,
		string(metaJSON),
		ttlMs,
		ChannelDriverUpdates,
		string(pubJSON),
	}

	n, err := s.RDB.Eval(ctx, luaWriteDriverLocation, keys, args...).Int64()
	if err != nil {
		return false, fmt.Errorf("eval lua write: %w", err)
	}

	return n == 1, nil
}

// GetNearbyDriverIDs queries Redis GEO and returns nearest-first IDs+distance.
func (s *Store) GetNearbyDriverIDs(ctx context.Context, q NearbyQuery) ([]NearbyResult, error) {
	if s == nil || s.RDB == nil {
		return nil, errors.New("redis store not configured")
	}
	if q.Limit <= 0 {
		q.Limit = 50
	}

	geoQ := &redis.GeoSearchLocationQuery{
		GeoSearchQuery: redis.GeoSearchQuery{
			Latitude:   q.PickupLat,
			Longitude:  q.PickupLon,
			Radius:     q.RadiusMiles,
			RadiusUnit: "mi",
			Sort:       "ASC",
			Count:      q.Limit,
			CountAny:   true,
		},
		WithCoord: false,
		WithDist:  true,
		WithHash:  false,
	}

	locs, err := s.RDB.GeoSearchLocation(ctx, KeyDriverGeo, geoQ).Result()
	if err != nil {
		return nil, fmt.Errorf("geosearch: %w", err)
	}

	out := make([]NearbyResult, 0, len(locs))
	for _, l := range locs {
		out = append(out, NearbyResult{
			DriverID:      l.Name,
			DistanceMiles: l.Dist,
		})
	}

	return out, nil
}

// LoadDriverLocations loads `driver:{id}` for ids (JSON or hash depending on your choice).
func (s *Store) LoadDriverLocations(ctx context.Context, driverIDs []string) (map[string]contracts.DriverLocation, error) {
	if s == nil || s.RDB == nil {
		return nil, errors.New("redis store not configured")
	}
	res := make(map[string]contracts.DriverLocation, len(driverIDs))
	if len(driverIDs) == 0 {
		return res, nil
	}

	keys := make([]string, 0, len(driverIDs))
	for _, id := range driverIDs {
		keys = append(keys, KeyDriverMeta(id))
	}

	vals, err := s.RDB.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("mget meta: %w", err)
	}

	for i, v := range vals {
		if v == nil {
			continue
		}
		str, ok := v.(string)
		if !ok {
			continue
		}
		var loc contracts.DriverLocation
		if err := json.Unmarshal([]byte(str), &loc); err != nil {
			continue
		}
		res[driverIDs[i]] = loc
	}

	return res, nil
}

// LoadDriverUpdatedMs loads ZSET scores for ids.
func (s *Store) LoadDriverUpdatedMs(ctx context.Context, driverIDs []string) (map[string]int64, error) {
	if s == nil || s.RDB == nil {
		return nil, errors.New("redis store not configured")
	}
	res := make(map[string]int64, len(driverIDs))
	if len(driverIDs) == 0 {
		return res, nil
	}

	scores, err := s.RDB.ZMScore(ctx, KeyDriverUpdated, driverIDs...).Result()
	if err != nil {
		return nil, fmt.Errorf("zmsscore: %w", err)
	}

	for i, s := range scores {
		// go-redis returns float64
		res[driverIDs[i]] = int64(s)
	}

	return res, nil
}

// RemoveDriver removes driver from geo, updated, metadata.
func (s *Store) RemoveDriver(ctx context.Context, driverID string) error {
	if s == nil || s.RDB == nil {
		return errors.New("redis store not configured")
	}

	pipe := s.RDB.Pipeline()
	pipe.ZRem(ctx, KeyDriverUpdated, driverID)
	pipe.ZRem(ctx, KeyDriverGeo, driverID)
	pipe.Del(ctx, KeyDriverMeta(driverID))
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("remove driver: %w", err)
	}
	return nil
}

func parseInt(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil
}
