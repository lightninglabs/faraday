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
WHERE channel_id = $1 AND timestamp >= $2 AND timestamp < $3
ORDER BY timestamp ASC, id ASC
LIMIT $4;
