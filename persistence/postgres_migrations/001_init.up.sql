-- Initial Setup for schema version 0:

-- Torrents ID sequence generator
CREATE SEQUENCE IF NOT EXISTS seq_torrents_id;
-- Files ID sequence generator
CREATE SEQUENCE IF NOT EXISTS seq_files_id;

CREATE TABLE IF NOT EXISTS torrents (
    id             INTEGER PRIMARY KEY DEFAULT nextval('seq_torrents_id'),
    info_hash      bytea NOT NULL UNIQUE,
    name           TEXT NOT NULL,
    total_size     BIGINT NOT NULL CHECK(total_size > 0),
    discovered_on  INTEGER NOT NULL CHECK(discovered_on > 0)
);

-- Indexes for search sorting options
CREATE INDEX IF NOT EXISTS idx_torrents_total_size ON torrents (total_size);
CREATE INDEX IF NOT EXISTS idx_torrents_discovered_on ON torrents (discovered_on);

-- Using pg_trgm GIN index for fast ILIKE queries
-- You need to execute "CREATE EXTENSION pg_trgm" on your database for this index to work
-- Be aware that using this type of index implies that making ILIKE queries with less that
-- 3 character values will cause full table scan instead of using index.
-- You can try to avoid that by doing 'SET enable_seqscan=off'.
CREATE INDEX IF NOT EXISTS idx_torrents_name_gin_trgm ON torrents USING GIN (name gin_trgm_ops);

CREATE TABLE IF NOT EXISTS files (
    id          INTEGER PRIMARY KEY DEFAULT nextval('seq_files_id'),
    torrent_id  INTEGER REFERENCES torrents ON DELETE CASCADE ON UPDATE RESTRICT,
    size        BIGINT NOT NULL,
    path        TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_files_torrent_id ON files (torrent_id);