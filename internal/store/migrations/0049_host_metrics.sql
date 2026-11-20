-- The host's own usage is kept beside the containers', so its history survives a panel
-- restart and reaches back 30 days like theirs. One resource, kind 'host', id 'host':
-- cpu in cores used, memory and memory_limit the host's used and total RAM, net_rx and
-- net_tx the physical interfaces' rates, and (this kind only) disk_read and disk_write
-- the data disk's used and total bytes, since the host card reports usage, not I/O.
CREATE TABLE metrics_samples_new (
 kind TEXT NOT NULL CHECK(kind IN ('app','database','host')),
 resource_id TEXT NOT NULL,
 resolution INTEGER NOT NULL CHECK(resolution IN (10,60,3600)),
 ts INTEGER NOT NULL,
 cpu REAL NOT NULL,
 cpu_max REAL NOT NULL,
 memory INTEGER NOT NULL,
 memory_max INTEGER NOT NULL,
 memory_limit INTEGER NOT NULL,
 net_rx REAL NOT NULL,
 net_tx REAL NOT NULL,
 disk_read REAL NOT NULL,
 disk_write REAL NOT NULL,
 PRIMARY KEY(kind,resource_id,resolution,ts)
) WITHOUT ROWID;
INSERT INTO metrics_samples_new SELECT * FROM metrics_samples;
DROP TABLE metrics_samples;
ALTER TABLE metrics_samples_new RENAME TO metrics_samples;
CREATE INDEX metrics_samples_age ON metrics_samples(resolution,ts);
