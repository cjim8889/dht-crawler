-- Create new schema
CREATE SCHEMA IF NOT EXISTS dht_crawler;

-- Move sequences to new schema
ALTER SEQUENCE seq_torrents_id SET SCHEMA dht_crawler;
ALTER SEQUENCE seq_files_id SET SCHEMA dht_crawler;

-- Move tables to new schema
ALTER TABLE torrents SET SCHEMA dht_crawler;
ALTER TABLE files SET SCHEMA dht_crawler;

-- Update sequences' tables
ALTER TABLE dht_crawler.torrents ALTER COLUMN id SET DEFAULT nextval('dht_crawler.seq_torrents_id');
ALTER TABLE dht_crawler.files ALTER COLUMN id SET DEFAULT nextval('dht_crawler.seq_files_id');

-- Update files' reference
ALTER TABLE dht_crawler.files DROP CONSTRAINT files_torrent_id_fkey;
ALTER TABLE dht_crawler.files ADD CONSTRAINT files_torrent_id_fkey FOREIGN KEY (torrent_id) REFERENCES dht_crawler.torrents(id) ON DELETE CASCADE ON UPDATE RESTRICT;
