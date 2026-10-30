-- This composite index supports GetUnobservedIntervals, which finds, for each
-- sync event, the newest live event with a smaller id. Without it, the lookup
-- scans back over every sync event of a run, so the query cost grows with the
-- square of the run length.
CREATE INDEX IF NOT EXISTS channel_events_sync_id_idx ON channel_events (is_sync, id);
