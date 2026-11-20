package store

import (
	"context"
	"time"
)

// Metric resolutions in seconds and how long each is kept.
const (
	MetricRaw    = 10
	MetricMinute = 60
	MetricHour   = 3600
)

var metricRetention = map[int]time.Duration{
	MetricRaw:    24 * time.Hour,
	MetricMinute: 7 * 24 * time.Hour,
	MetricHour:   30 * 24 * time.Hour,
}

// MetricHost is the kind, and the resource id, of the host's own samples. For them
// DiskRead and DiskWrite hold the data disk's used and total bytes.
const MetricHost = "host"

// MetricSample is one bucket of container usage for an app or database instance.
type MetricSample struct {
	Kind        string  `json:"-"`
	ResourceID  string  `json:"-"`
	TS          int64   `json:"ts"`
	CPU         float64 `json:"cpu"`
	CPUMax      float64 `json:"cpu_max"`
	Memory      int64   `json:"memory"`
	MemoryMax   int64   `json:"memory_max"`
	MemoryLimit int64   `json:"memory_limit"`
	NetRx       float64 `json:"net_rx"`
	NetTx       float64 `json:"net_tx"`
	DiskRead    float64 `json:"disk_read"`
	DiskWrite   float64 `json:"disk_write"`
}

// AddMetricSamples stores raw samples taken at ts (Unix seconds) in one transaction.
func (s *Store) AddMetricSamples(ctx context.Context, ts int64, samples []MetricSample) error {
	if len(samples) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, m := range samples {
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO metrics_samples(kind,resource_id,resolution,ts,cpu,cpu_max,memory,memory_max,memory_limit,net_rx,net_tx,disk_read,disk_write) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			m.Kind, m.ResourceID, MetricRaw, ts, m.CPU, m.CPU, m.Memory, m.Memory, m.MemoryLimit, m.NetRx, m.NetTx, m.DiskRead, m.DiskWrite); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RollupMetrics folds raw samples into minutes and minutes into hours for the
// last few complete buckets, then prunes rows past their retention. Rollups
// replace their bucket, so running it again (or after a restart) is harmless.
func (s *Store) RollupMetrics(ctx context.Context, at time.Time) error {
	now := at.Unix()
	for _, r := range []struct{ from, to int }{{MetricRaw, MetricMinute}, {MetricMinute, MetricHour}} {
		end := now - now%int64(r.to) // only complete buckets
		start := end - 3*int64(r.to)
		if _, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO metrics_samples(kind,resource_id,resolution,ts,cpu,cpu_max,memory,memory_max,memory_limit,net_rx,net_tx,disk_read,disk_write)
SELECT kind,resource_id,?,ts-ts%?,AVG(cpu),MAX(cpu_max),CAST(AVG(memory) AS INTEGER),MAX(memory_max),MAX(memory_limit),AVG(net_rx),AVG(net_tx),AVG(disk_read),AVG(disk_write)
FROM metrics_samples WHERE resolution=? AND ts>=? AND ts<? GROUP BY kind,resource_id,ts-ts%?`, r.to, r.to, r.from, start, end, r.to); err != nil {
			return err
		}
	}
	for res, keep := range metricRetention {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM metrics_samples WHERE resolution=? AND ts<?`, res, at.Add(-keep).Unix()); err != nil {
			return err
		}
	}
	return nil
}

// MetricSeries returns samples of one resource between from and to (Unix
// seconds), read at resolution and averaged into buckets of step seconds.
func (s *Store) MetricSeries(ctx context.Context, kind, resourceID string, resolution int, step, from, to int64) ([]MetricSample, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ts-ts%?,AVG(cpu),MAX(cpu_max),CAST(AVG(memory) AS INTEGER),MAX(memory_max),MAX(memory_limit),AVG(net_rx),AVG(net_tx),AVG(disk_read),AVG(disk_write)
FROM metrics_samples WHERE kind=? AND resource_id=? AND resolution=? AND ts>=? AND ts<? GROUP BY ts-ts%? ORDER BY 1`, step, kind, resourceID, resolution, from, to, step)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MetricSample{}
	for rows.Next() {
		m := MetricSample{Kind: kind, ResourceID: resourceID}
		if err := rows.Scan(&m.TS, &m.CPU, &m.CPUMax, &m.Memory, &m.MemoryMax, &m.MemoryLimit, &m.NetRx, &m.NetTx, &m.DiskRead, &m.DiskWrite); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MetricWindow sums up one resource's raw samples since a time: how many there are,
// and the lowest CPU and memory share of the limit among them (usage alerts fire
// when even the lowest is above the threshold), plus the latest sample's values.
type MetricWindow struct {
	Kind         string
	ResourceID   string
	Samples      int
	MinCPU       float64
	MinMemoryPct float64
	LastCPU      float64
	LastMemPct   float64
}

// MetricWindows returns a window per resource of kind ("" = apps and databases),
// or of one resource when resourceID is set, over raw samples at or after from.
func (s *Store) MetricWindows(ctx context.Context, kind, resourceID string, from int64) ([]MetricWindow, error) {
	rows, err := s.db.QueryContext(ctx, `WITH w AS (
 SELECT kind,resource_id,ts,cpu,CASE WHEN memory_limit>0 THEN 100.0*memory/memory_limit ELSE 0 END AS pct
 FROM metrics_samples WHERE resolution=? AND ts>=? AND ((?='' AND kind IN ('app','database')) OR kind=?) AND (?='' OR resource_id=?))
SELECT kind,resource_id,COUNT(*),MIN(cpu),MIN(pct),
 (SELECT cpu FROM w l WHERE l.kind=w.kind AND l.resource_id=w.resource_id ORDER BY ts DESC LIMIT 1),
 (SELECT pct FROM w l WHERE l.kind=w.kind AND l.resource_id=w.resource_id ORDER BY ts DESC LIMIT 1)
FROM w GROUP BY kind,resource_id`, MetricRaw, from, kind, kind, resourceID, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MetricWindow{}
	for rows.Next() {
		var m MetricWindow
		if err := rows.Scan(&m.Kind, &m.ResourceID, &m.Samples, &m.MinCPU, &m.MinMemoryPct, &m.LastCPU, &m.LastMemPct); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
