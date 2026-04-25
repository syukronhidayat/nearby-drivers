# Nearby Drivers Service (Go + Redis)

A real-time backend service for ingesting driver GPS updates and streaming nearby driver changes to admin clients over WebSocket.

This project is designed as a portfolio-grade implementation of:
- geospatial search with Redis GEO
- freshness and stale handling
- real-time pub/sub fanout
- subscription matching and lifecycle management
- production-oriented concerns (throttling, cleanup, graceful shutdown)

## Features

- `POST /v1/drivers/location` to ingest driver updates
- Redis-backed location storage:
  - GEO index (`driver:geo`)
  - freshness clock (`driver:updated`)
  - metadata blob (`driver:{id}`)
- Atomic stale-write rejection + publish via Lua script
- Redis Pub/Sub ingestion (`driver:updates`) with batching (last-write-wins per driver)
- `GET /v1/ws` plain WebSocket endpoint (no Socket.IO)
- Subscription model:
  - initial nearby snapshot
  - real-time `driver_added`, `driver_updated`, `driver_removed`
- Removal reasons:
  - `offline`
  - `outside_radius`
  - `stale_location`
- In-memory per-driver ingest throttling (returns 200 + throttled)
- Periodic stale sweeper in WS hub
- Optional periodic Redis cleanup job for stale index members
- Graceful shutdown (`SIGINT`/`SIGTERM`)

## Tech Stack

- Go 1.25+
- `net/http` + `chi`
- `go-redis/v9`
- `gorilla/websocket`
- Redis (single node)

## Project Structure

```text
cmd/server/main.go                  # app wiring, goroutines, graceful shutdown
internal/config/                    # env parsing
internal/contracts/                 # HTTP + WS contracts/enums
internal/httpapi/                   # router + handlers
internal/store/redisstore/          # Redis data access + Lua write path
internal/pubsub/                    # Redis pub/sub consumer + batching
internal/ws/                        # hub, subscription lifecycle, matching, sweeps
internal/throttle/                  # in-memory + Redis NX/PX throttlers
internal/cleanup/                   # periodic Redis stale-index cleanup job
```

## Architecture (High-Level)

1. Driver sends update to `POST /v1/drivers/location`.
2. Handler validates + normalizes payload and applies per-driver throttling.
3. Store executes Lua script:
   - reject stale write (`timestamp <= last`)
   - `GEOADD` + `ZADD` + `SET PX`
   - `PUBLISH` update to `driver:updates`
4. Pub/Sub consumer batches updates (last-write-wins per driver ID) and forwards to WS hub.
5. Hub matches each update against active subscriptions and emits:
   - `driver_added`
   - `driver_updated`
   - `driver_removed` (offline/outside/stale)
6. Periodic stale sweep evicts stale visible drivers even without incoming updates.

## API

### Health

- `GET /healthz`
- Response: `200 {"status":"ok"}`

### Ingest Driver Location

- `POST /v1/drivers/location`
- Content-Type: `application/json`

Example request:

```json
{
  "driverId": "d-1001",
  "lat": -6.2000,
  "lon": 106.8166,
  "timestamp": 1714058045000,
  "status": "available",
  "bearing": 120.5,
  "speed": 8.7,
  "accuracy": 5.0
}
```

Example response:

```json
{
  "accepted": true
}
```

Throttled response:

```json
{
  "accepted": true,
  "throttled": true
}
```

### Nearby (Debug Endpoint)

- `GET /v1/drivers/nearby`
- Query params:
  - `lat`
  - `lon`
  - `radiusMiles`
  - `maxAgeSec`
  - `includeOffline`
  - `limit`

Example:

`/v1/drivers/nearby?lat=-6.2&lon=106.81&radiusMiles=3&maxAgeSec=120&includeOffline=false&limit=50`

## WebSocket

- `GET /v1/ws`

Envelope:

```json
{
  "type": "string",
  "requestId": "optional",
  "payload": {}
}
```

### Client -> Server

- `subscribe_location_drivers`
- `unsubscribe`

Subscribe payload:

```json
{
  "subscriptionId": "sub-1",
  "pickup": { "lat": -6.2, "lon": 106.81 },
  "radiusMiles": 3,
  "maxAgeSec": 120,
  "includeOffline": false
}
```

### Server -> Client

- `drivers_initial`
- `driver_added`
- `driver_updated`
- `driver_removed`
- `error`

`driver_removed` reasons:
- `offline`
- `outside_radius`
- `stale_location`

## Redis Data Model

- `driver:geo` (GEO index)
  - `GEOADD`, `GEOSEARCH`
- `driver:updated` (ZSET score = timestamp ms)
  - `ZADD`, `ZMSCORE`
- `driver:{id}` (JSON metadata, TTL)
  - `SET PX`, `MGET`
- `driver:updates` (Pub/Sub channel)
  - `PUBLISH`, `SUBSCRIBE`

### Atomic Ingest Lua Flow

The Lua script in `internal/store/redisstore/lua.go` ensures:
- stale write rejection
- state write + publish happen atomically

This avoids race conditions from out-of-order updates.

## Staleness and Cleanup

### WS Stale Sweep

- Implemented in `internal/ws/hub.go`
- Runs every 1 second
- For each subscription, removes visible drivers older than `maxAgeSec`
- Emits `driver_removed` with reason `stale_location`

### Redis Cleanup Job

- Implemented in `internal/cleanup/redis_cleanup.go`
- Runs periodically (configured in `main.go`)
- Removes old IDs from:
  - `driver:updated`
  - `driver:geo`
  - `driver:{id}`

Purpose: prevents stale index growth over time.

## Configuration

Environment variables:

- `ENV` (default: `local`)
- `HTTP_ADDR` (default: `:8080`)
- `SHUTDOWN_TIMEOUT` (default: `10s`)
- `READ_HEADER_TIMEOUT` (default: `5s`)
- `HEALTHZ_PATH` (default: `/healthz`)
- `REDIS_ADDR` (default: `127.0.0.1:6379`)
- `REDIS_PASSWORD` (default: empty)
- `REDIS_DB` (default: `0`)

## Run Locally

### 1) Start Redis

```bash
docker run --rm -p 6379:6379 redis:7
```

### 2) Run API

```bash
go run ./cmd/server
```

### 3) Quick checks

Health:

```bash
curl -sS http://localhost:8080/healthz
```

Ingest:

```bash
curl -sS -X POST http://localhost:8080/v1/drivers/location \
  -H "Content-Type: application/json" \
  -d '{
    "driverId":"d1",
    "lat":-6.2,
    "lon":106.81,
    "timestamp":1714058045000,
    "status":"available"
  }'
```

Nearby:

```bash
curl -sS "http://localhost:8080/v1/drivers/nearby?lat=-6.2&lon=106.81&radiusMiles=3&maxAgeSec=120&includeOffline=false&limit=50"
```

## Design Notes and Trade-offs

- Current throttling in `main.go` uses in-memory implementation for simplicity.
  - Good for single instance.
  - Redis NX/PX throttler exists and can be wired for multi-instance behavior.
- Cleanup/stale intervals are currently hard-coded in wiring.
  - Easy to move to env config later.
- Nearby endpoint is marked debug-oriented in this phase.
- No auth layer yet; can be added via middleware/JWT/session depending on deployment needs.

## What’s Next

- Add comprehensive unit + integration tests
- Add WS auth for admin clients
- Add observability (structured logs, metrics, traces)
- Add Docker Compose for one-command local startup
- Add CI (lint, test, race detector)