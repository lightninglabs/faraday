-- This standalone timestamp index supports the global age-based prune in
-- PruneChannelEvents, which deletes across all channels by timestamp with no
-- channel_id predicate. The composite (channel_id, timestamp) index cannot
-- serve that query because its leading column is channel_id, so without this
-- index every retention prune would scan the full channel_events table.
CREATE INDEX IF NOT EXISTS channel_events_ts_idx ON channel_events (timestamp);
