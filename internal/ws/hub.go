package ws

import (
	"context"
	"math"
	"sync/atomic"
	"time"

	"github.com/syukronhidayat/nearby-drivers/internal/contracts"
	"github.com/syukronhidayat/nearby-drivers/internal/store/redisstore"
)

type Hub struct {
	store          *redisstore.Store
	maxSubsPerConn int
	snapshotSem    chan struct{}

	register     chan *Client
	unregister   chan string
	subscribe    chan subscribeReq
	unsubscribe  chan unsubscribeReq
	driverUpsert chan contracts.DriverLocation
	snapshotDone chan snapshotResult

	conns      map[string]*Client
	subsByConn map[string]map[string]*subscription

	closed atomic.Bool
}

func NewHub(store *redisstore.Store) *Hub {
	return &Hub{
		store:          store,
		maxSubsPerConn: 25,
		snapshotSem:    make(chan struct{}, 8),
		register:       make(chan *Client, 64),
		unregister:     make(chan string, 64),
		subscribe:      make(chan subscribeReq, 64),
		unsubscribe:    make(chan unsubscribeReq, 64),
		driverUpsert:   make(chan contracts.DriverLocation, 1024),
		snapshotDone:   make(chan snapshotResult, 64),
		conns:          make(map[string]*Client),
		subsByConn:     make(map[string]map[string]*subscription),
	}
}

func (h *Hub) Run(ctx context.Context) {
	staleTicker := time.NewTicker(1 * time.Second)
	defer staleTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.closed.Store(true)
			for id := range h.conns {
				h.dropConn(id)
			}
			return

		case <-staleTicker.C:
			h.sweepStale(time.Now().UnixMilli())

		case c := <-h.register:
			h.conns[c.ID] = c
			if _, ok := h.subsByConn[c.ID]; !ok {
				h.subsByConn[c.ID] = make(map[string]*subscription)
			}

		case connID := <-h.unregister:
			h.dropConn(connID)

		case req := <-h.subscribe:
			h.handleSubscribe(req)

		case req := <-h.unsubscribe:
			h.handleUnsubscribe(req)

		case snap := <-h.snapshotDone:
			h.handleSnapshotDone(snap)

		case up := <-h.driverUpsert:
			h.handleDriverUpsert(up)
		}
	}
}

func (h *Hub) Register(c *Client) {
	if h.closed.Load() {
		return
	}
	h.register <- c
}

func (h *Hub) Unregister(connID string) {
	if h.closed.Load() {
		return
	}
	h.unregister <- connID
}

func (h *Hub) Subscribe(connID string, reqID *string, p contracts.WSSubscribeLocationDriverPayload) {
	if h.closed.Load() {
		return
	}
	h.subscribe <- subscribeReq{connID: connID, reqID: reqID, payload: p}
}

func (h *Hub) UnsubscribeByID(connID string, reqID *string, subID string) {
	if h.closed.Load() {
		return
	}
	h.unsubscribe <- unsubscribeReq{connID: connID, reqID: reqID, subID: subID}
}

func (h *Hub) OnDriverUpsert(loc contracts.DriverLocation) {
	if h.closed.Load() {
		return
	}
	h.driverUpsert <- loc
}

func (h *Hub) dropConn(connID string) {
	c, ok := h.conns[connID]
	if !ok {
		return
	}
	delete(h.conns, connID)
	delete(h.subsByConn, connID)
	c.Close()
}

type subscribeReq struct {
	connID  string
	reqID   *string
	payload contracts.WSSubscribeLocationDriverPayload
}

type unsubscribeReq struct {
	connID string
	reqID  *string
	subID  string
}

type subscription struct {
	id             string
	pickupLat      float64
	pickupLon      float64
	radiusMiles    float64
	maxAgeSec      int64
	includeOffline bool
	visible        map[string]contracts.DriverView
}

type snapshotResult struct {
	connID string
	subID  string
	reqID  *string
	err    *contracts.WSErrorPayload
	views  []contracts.DriverView
}

func (h *Hub) handleUnsubscribe(req unsubscribeReq) {
	subs := h.subsByConn[req.connID]
	if subs == nil {
		return
	}
	delete(subs, req.subID)
}

func (h *Hub) handleSubscribe(req subscribeReq) {
	c := h.conns[req.connID]
	if c == nil {
		return
	}

	// Enforce per-connection subscription limit.
	subs := h.subsByConn[req.connID]
	if subs == nil {
		subs = make(map[string]*subscription)
		h.subsByConn[req.connID] = subs
	}
	if h.maxSubsPerConn > 0 && len(subs) >= h.maxSubsPerConn {
		_ = c.SendEnvelope(contracts.WSMsgError, req.reqID, contracts.WSErrorPayload{
			Code:    contracts.HTTPErrorValidation,
			Message: "too many subscriptions for this connection",
			Details: map[string]any{"max": h.maxSubsPerConn},
		})
		return
	}

	p := req.payload
	sub := &subscription{
		id:             p.SubscriptionID,
		pickupLat:      p.Pickup.Lat,
		pickupLon:      p.Pickup.Lon,
		radiusMiles:    p.RadiusMiles,
		maxAgeSec:      p.MaxAgeSec,
		includeOffline: p.IncludeOffline,
		visible:        make(map[string]contracts.DriverView),
	}

	subs[sub.id] = sub

	// Async initial snapshot with bounded concurrency.
	go func() {
		if h.snapshotSem != nil {
			h.snapshotSem <- struct{}{}
			defer func() { <-h.snapshotSem }()
		}

		views, errPayload := h.buildInitialSnapshot(req.connID, sub)
		h.snapshotDone <- snapshotResult{
			connID: req.connID,
			subID:  sub.id,
			reqID:  req.reqID,
			err:    errPayload,
			views:  views,
		}
	}()
}

func (h *Hub) handleSnapshotDone(s snapshotResult) {
	c := h.conns[s.connID]
	if c == nil {
		return
	}
	sub := h.subsByConn[s.connID][s.subID]
	if sub == nil {
		return // unsubscribed while snapshot running
	}

	if s.err != nil {
		_ = c.SendEnvelope(contracts.WSMsgError, s.reqID, *s.err)
		return
	}

	sub.visible = make(map[string]contracts.DriverView, len(s.views))
	for _, v := range s.views {
		sub.visible[v.DriverID] = v
	}

	payload := contracts.WSDriversInitialPayload{
		SubscriptionID: sub.id,
		Drivers:        s.views,
	}
	_ = c.SendEnvelope(contracts.WSMsgDriversInitial, s.reqID, payload)
}

func (h *Hub) handleDriverUpsert(loc contracts.DriverLocation) {
	nowMs := time.Now().UnixMilli()

	for connID, subs := range h.subsByConn {
		c := h.conns[connID]
		if c == nil {
			continue
		}

		for _, sub := range subs {
			// Stale filter
			if sub.maxAgeSec > 0 {
				maxAgeMs := sub.maxAgeSec * 1000
				if (nowMs - loc.TimestampMs) > maxAgeMs {
					if _, ok := sub.visible[loc.DriverID]; ok {
						delete(sub.visible, loc.DriverID)
						_ = c.SendEnvelope(contracts.WSMsgDriverRemoved, nil, contracts.WSDriverRemovedPayload{
							SubscriptionID: sub.id,
							DriverID:       loc.DriverID,
							Reason:         contracts.RemovalReasonStaleLocation,
							LastKnown:      &loc,
						})
					}
					continue
				}
			}

			// Oflline filter
			if !sub.includeOffline && loc.Status != nil && *loc.Status == contracts.DriverStatusOffline {
				if _, ok := sub.visible[loc.DriverID]; ok {
					delete(sub.visible, loc.DriverID)
					_ = c.SendEnvelope(contracts.WSMsgDriverRemoved, nil, contracts.WSDriverRemovedPayload{
						SubscriptionID: sub.id,
						DriverID:       loc.DriverID,
						Reason:         contracts.RemovalReasonOffline,
						LastKnown:      &loc,
					})
				}
				continue
			}

			// Radius check (Haversine miles)
			distMiles := haversineMiles(sub.pickupLat, sub.pickupLon, loc.Lat, loc.Lon)
			if distMiles > sub.radiusMiles {
				if _, ok := sub.visible[loc.DriverID]; ok {
					delete(sub.visible, loc.DriverID)
					_ = c.SendEnvelope(contracts.WSMsgDriverRemoved, nil, contracts.WSDriverRemovedPayload{
						SubscriptionID: sub.id,
						DriverID:       loc.DriverID,
						Reason:         contracts.RemovalReasonOutsideRadius,
						LastKnown:      &loc,
					})
				}
				continue
			}

			view := contracts.DriverView{
				DriverLocation: loc,
				DistanceMiles:  distMiles,
				ETASeconds:     estimateETASeconds(distMiles, loc.Speed),
			}

			if _, ok := sub.visible[loc.DriverID]; ok {
				sub.visible[loc.DriverID] = view
				_ = c.SendEnvelope(contracts.WSMsgDriverUpdated, nil, contracts.WSDriverUpdatedPayload{
					SubscriptionID: sub.id,
					Driver:         view,
				})
			} else {
				sub.visible[loc.DriverID] = view
				_ = c.SendEnvelope(contracts.WSMsgDriverAdded, nil, contracts.WSDriverAddedPayload{
					SubscriptionID: sub.id,
					Driver:         view,
				})
			}
		}
	}
}

func (h *Hub) buildInitialSnapshot(connID string, sub *subscription) ([]contracts.DriverView, *contracts.WSErrorPayload) {
	if h.store == nil {
		return nil, &contracts.WSErrorPayload{
			Code:    contracts.HTTPErrorInternal,
			Message: "store not configured",
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	results, err := h.store.GetNearbyDriverIDs(ctx, redisstore.NearbyQuery{
		PickupLat:   sub.pickupLat,
		PickupLon:   sub.pickupLon,
		RadiusMiles: sub.radiusMiles,
		Limit:       200,
	})
	if err != nil {
		return nil, &contracts.WSErrorPayload{
			Code:    contracts.HTTPErrorInternal,
			Message: "failed to query nearby drivers",
			Details: map[string]any{"error": err.Error()},
		}
	}

	ids := make([]string, 0, len(results))
	distByID := make(map[string]float64, len(results))
	for _, it := range results {
		ids = append(ids, it.DriverID)
		distByID[it.DriverID] = it.DistanceMiles
	}

	locs, err := h.store.LoadDriverLocations(ctx, ids)
	if err != nil {
		return nil, &contracts.WSErrorPayload{
			Code:    contracts.HTTPErrorInternal,
			Message: "failed to load driver locations",
			Details: map[string]any{"error": err.Error()},
		}
	}
	updated, err := h.store.LoadDriverUpdatedMs(ctx, ids)
	if err != nil {
		return nil, &contracts.WSErrorPayload{
			Code:    contracts.HTTPErrorInternal,
			Message: "failed to load driver updated times",
			Details: map[string]any{"error": err.Error()},
		}
	}

	nowMs := time.Now().UnixMilli()
	maxAgeMs := sub.maxAgeSec * 1000

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

		if sub.maxAgeSec > 0 && (nowMs-updatedMs) > maxAgeMs {
			continue
		}
		if !sub.includeOffline && loc.Status != nil && *loc.Status == contracts.DriverStatusOffline {
			continue
		}
		dist := distByID[id]
		out = append(out, contracts.DriverView{
			DriverLocation: loc,
			DistanceMiles:  dist,
			ETASeconds:     estimateETASeconds(dist, loc.Speed),
		})
	}

	return out, nil
}

func (h *Hub) sweepStale(nowMs int64) {
	for connID, subs := range h.subsByConn {
		c := h.conns[connID]
		if c == nil {
			continue
		}

		for _, sub := range subs {
			if sub.maxAgeSec <= 0 {
				continue
			}
			cutoffMs := nowMs - (sub.maxAgeSec * 1000)
			if cutoffMs <= 0 {
				continue
			}

			for driverID, view := range sub.visible {
				if view.TimestampMs > cutoffMs {
					continue
				}

				// Remove + emit stale removal
				delete(sub.visible, driverID)

				last := view.DriverLocation
				_ = c.SendEnvelope(contracts.WSMsgDriverRemoved, nil, contracts.WSDriverRemovedPayload{
					SubscriptionID: sub.id,
					DriverID:       driverID,
					Reason:         contracts.RemovalReasonStaleLocation,
					LastKnown:      &last,
				})
			}
		}
	}
}

const (
	milesToMeters    = 1609.344
	fallbackSpeedMps = 8.33
	minSpeedMps      = 1.0
)

func estimateETASeconds(distanceMiles float64, speedMps *float64) *int64 {
	if distanceMiles <= 0 {
		z := int64(0)
		return &z
	}
	v := fallbackSpeedMps
	if speedMps != nil && *speedMps > 0 && !math.IsNaN(*speedMps) && !math.IsInf(*speedMps, 0) {
		v = *speedMps
	}
	if v < minSpeedMps {
		v = minSpeedMps
	}
	meters := distanceMiles * milesToMeters
	sec := int64(math.Ceil(meters / v))
	return &sec
}

func haversineMiles(lat1, lon1, lat2, lon2 float64) float64 {
	const rMiles = 3958.7613
	dLat := (lat2 - lat1) * (math.Pi / 180)
	dLon := (lon2 - lon1) * (math.Pi / 180)
	la1 := lat1 * (math.Pi / 180)
	la2 := lat2 * (math.Pi / 180)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Sin(dLon/2)*math.Sin(dLon/2)*math.Cos(la1)*math.Cos(la2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return rMiles * c
}
