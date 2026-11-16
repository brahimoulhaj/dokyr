-- Container usage per app or database instance. "resolution" is the bucket size
-- in seconds: 10 (raw samples, kept 24 hours), 60 (kept 7 days) and 3600 (kept
-- 30 days). Rates are bytes per second; cpu is in cores (1.0 = one full core).
CREATE TABLE metrics_samples (
 kind TEXT NOT NULL CHECK(kind IN ('app','database')),
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
CREATE INDEX metrics_samples_age ON metrics_samples(resolution,ts);
