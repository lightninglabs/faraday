-- The peers table stores all the peers that we have channels with.
CREATE TABLE IF NOT EXISTS peers (
    -- The auto incrementing primary key.
    id INTEGER PRIMARY KEY,
    -- The public key of the peer.
    pubkey TEXT NOT NULL UNIQUE
);

-- The channels table stores all the channels that we have with our peers.
CREATE TABLE IF NOT EXISTS channels (
    -- The auto incrementing primary key.
    id INTEGER PRIMARY KEY,
    -- The channel point, as a 'txid:output_index' string.
    channel_point TEXT NOT NULL UNIQUE,
    -- The short channel ID.
    short_channel_id BIGINT NOT NULL UNIQUE,
    -- The peer that this channel is with.
    peer_id BIGINT NOT NULL REFERENCES peers(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS channel_peer_idx ON channels (peer_id);

-- The channel_events table stores all the events that are associated with a
-- particular channel.
CREATE TABLE IF NOT EXISTS channel_events (
    -- The auto incrementing primary key.
    id INTEGER PRIMARY KEY,
    -- The channel that this event is associated with.
    channel_id BIGINT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    -- The type of event.
    event_type SMALLINT NOT NULL,
    -- The time the event occurred.
    timestamp TIMESTAMP NOT NULL,
    -- The local balance of the channel at the time of the event.
    -- This is only populated for balance update events.
    local_balance_sat BIGINT CHECK (local_balance_sat >= 0),
    -- The remote balance of the channel at the time of the event.
    -- This is only populated for balance update events.
    remote_balance_sat BIGINT CHECK (remote_balance_sat >= 0),
    -- Whether this event was recorded during an initial sync rather than
    -- from a live subscription.
    is_sync BOOLEAN NOT NULL DEFAULT FALSE
);

-- This composite index is crucial for efficiently querying the event history
-- of a specific channel. It allows the database to quickly locate relevant rows
-- for a given channel, sorted by time. This is useful for fetching events
-- within a time range, and for finding the latest event before a certain time.
CREATE INDEX IF NOT EXISTS channel_events_chan_id_ts_idx ON channel_events (channel_id, timestamp);
