-- name: InsertPeer :one
INSERT INTO peers (pubkey) VALUES ($1) RETURNING id;

-- name: GetPeerByPubKey :one
SELECT * FROM peers WHERE pubkey = $1;

-- name: InsertChannel :one
INSERT INTO channels (channel_point, short_channel_id, peer_id) VALUES ($1, $2, $3) RETURNING id;

-- name: GetChannelByChanPoint :one
SELECT * FROM channels WHERE channel_point = $1;

-- name: GetChannelByShortChanID :one
SELECT * FROM channels WHERE short_channel_id = $1;

-- name: InsertChannelEvent :exec
INSERT INTO channel_events (
    channel_id, event_type, timestamp, local_balance_sat, remote_balance_sat,
    is_sync
) VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetChannelEvents :many
SELECT * FROM channel_events
WHERE channel_id = $1
  AND id > $2
  AND timestamp >= $3
  AND timestamp < $4
ORDER BY id ASC
LIMIT $5;

-- name: GetLatestChannelEventBefore :one
SELECT * FROM channel_events
WHERE channel_id = $1 AND event_type = $2 AND timestamp < $3
ORDER BY timestamp DESC, id DESC
LIMIT 1;

-- name: GetChannels :many
SELECT c.id, c.short_channel_id, p.pubkey
FROM channels c
JOIN peers p ON c.peer_id = p.id;

-- name: PruneChannelEventsBySize :execrows
-- PruneChannelEventsBySize enforces the size ceiling on the channel_events
-- table, returning the number of rows deleted. It keeps the newest rows by
-- deleting everything with a smaller (earlier-inserted) id than the id found at
-- the given offset from the newest row, so an offset of (max-events - 1) keeps
-- exactly max-events rows.
DELETE FROM channel_events
WHERE channel_events.id < COALESCE((
    SELECT id FROM channel_events
    ORDER BY id DESC
    LIMIT 1 OFFSET $1
), 0);

-- name: PruneChannelEventsByAge :execrows
-- PruneChannelEventsByAge enforces the retention window on the channel_events
-- table, returning the number of rows deleted. It deletes any row whose
-- timestamp predates the given cutoff.
DELETE FROM channel_events
WHERE channel_events.timestamp < $1;

-- name: GetUnobservedIntervals :many
-- GetUnobservedIntervals returns one row per sync batch whose last sync event
-- lies at or after start_time. A batch is the set of sync events that share
-- the same preceding live event. The row carries the timestamp of that live
-- event (NULL if none exists) and the timestamp of the last sync event of the
-- batch. Rows whose live event lies at or after end_time are left out. The
-- query aggregates over ids, not timestamps, because SQLite cannot scan an
-- aggregated timestamp back into a time value.
WITH sync_events AS (
    SELECT s.id, (
        SELECT l.id FROM channel_events l
        WHERE l.is_sync = FALSE AND l.id < s.id
        ORDER BY l.id DESC
        LIMIT 1
    ) AS live_id
    FROM channel_events s
    WHERE s.is_sync = TRUE AND s.timestamp >= sqlc.arg(start_time)
), batches AS (
    SELECT live_id, MAX(id) AS last_sync_id
    FROM sync_events
    GROUP BY live_id
)
SELECT live.timestamp AS live_timestamp, sync.timestamp AS sync_timestamp
FROM batches b
JOIN channel_events sync ON sync.id = b.last_sync_id
LEFT JOIN channel_events live ON live.id = b.live_id
WHERE live.id IS NULL OR live.timestamp < sqlc.arg(end_time)
ORDER BY sync.id ASC;
