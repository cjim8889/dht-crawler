-- Reverse the operations

-- Update files' reference
ALTER TABLE dht_crawler.files DROP CONSTRAINT files_torrent_id_fkey;
ALTER TABLE dht_crawler.files ADD CONSTRAINT files_torrent_id_fkey FOREIGN KEY (torrent_id) REFERENCES torrents(id) ON DELETE CASCADE ON UPDATE RESTRICT;

-- Update sequences' tables
ALTER TABLE dht_crawler.torrents ALTER COLUMN id SET DEFAULT nextval('seq_torrents_id');
ALTER TABLE dht_crawler.files ALTER COLUMN id SET DEFAULT nextval('seq_files_id');

-- Move tables to public schema
ALTER TABLE dht_crawler.files SET SCHEMA public;
ALTER TABLE dht_crawler.torrents SET SCHEMA public;

-- Move sequences to public schema
ALTER SEQUENCE dht_crawler.seq_files_id SET SCHEMA public;
ALTER SEQUENCE dht_crawler.seq_torrents_id SET SCHEMA public;

-- Remove the schema
DROP SCHEMA IF EXISTS dht_crawler;
