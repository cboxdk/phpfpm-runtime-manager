-- Create events table for storing operational events
CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    timestamp DATETIME NOT NULL,
    pool TEXT,
    summary TEXT NOT NULL,
    details TEXT NOT NULL, -- JSON blob
    correlation_id TEXT,
    severity TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
CREATE INDEX IF NOT EXISTS idx_events_pool ON events(pool);
CREATE INDEX IF NOT EXISTS idx_events_type ON events(type);
CREATE INDEX IF NOT EXISTS idx_events_severity ON events(severity);
CREATE INDEX IF NOT EXISTS idx_events_correlation_id ON events(correlation_id);

-- Create composite index for common query patterns
CREATE INDEX IF NOT EXISTS idx_events_time_pool_type ON events(timestamp, pool, type);