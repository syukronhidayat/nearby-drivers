package redisstore

// luaWriteDriverLocation atomically rejects stale updates and writes geo+meta+updated+pubsub.
// KEYS[1] = driver:updated
// KEYS[2] = driver:geo
// KEYS[3] = driver:{id}
// ARGV[1] = driverId
// ARGV[2] = timestampMs (number)
// ARGV[3] = lon
// ARGV[4] = lat
// ARGV[5] = metaJSON
// ARGV[6] = ttlMs
// ARGV[7] = pubsubChannel (driver:updates)
// ARGV[8] = pubsubJSON
//
// Returns:
// 1 if accepted (written + published)
// 0 if rejected (stale write)
const luaWriteDriverLocation = `
local updatedKey = KEYS[1]
local geoKey = KEYS[2]
local metaKey = KEYS[3]

local driverId = ARGV[1]
local ts = tonumber(ARGV[2])
local lon = tonumber(ARGV[3])
local lat = tonumber(ARGV[4])
local metaJSON = ARGV[5]
local ttlMs = tonumber(ARGV[6])
local channel = ARGV[7]
local pubJSON = ARGV[8]

local last = redis.call("ZSCORE", updatedKey, driverId)
if last ~= false and tonumber(last) ~= nil then
  if ts <= tonumber(last) then
    return 0
  end
end

redis.call("GEOADD", geoKey, lon, lat, driverId)
redis.call("ZADD", updatedKey, ts, driverId)
redis.call("SET", metaKey, metaJSON, "PX", ttlMs)
redis.call("PUBLISH", channel, pubJSON)

return 1
`
