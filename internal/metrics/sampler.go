// Package metrics samples the CPU, memory, network and disk use of app and
// database containers every 10 seconds and stores it with rollups to minutes
// and hours (see store.RollupMetrics for retention).
package metrics

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/types/container"

	"panel/internal/executor"
	"panel/internal/store"
)

const Interval = 10 * time.Second

// Host lists managed containers and reads their usage.
type Host interface {
	ListContainers(context.Context) ([]executor.Container, error)
	ContainerStats(context.Context, string) (container.StatsResponse, error)
}

// counters are a container's cumulative readings, kept to compute rates.
type counters struct {
	at                time.Time
	cpu               uint64 // nanoseconds of CPU time
	rx, tx            uint64
	diskRead, diskWrt uint64
}

type Sampler struct {
	st   *store.Store
	host Host
	log  *slog.Logger
	// Include picks the containers measured; nil keeps every app and database container.
	Include func(executor.Container) bool
	now     func() time.Time
	mu      sync.Mutex
	prev    map[string]counters
}

func New(st *store.Store, host Host, log *slog.Logger) *Sampler {
	return &Sampler{st: st, host: host, log: log, now: time.Now, prev: map[string]counters{}}
}

func (s *Sampler) Run(ctx context.Context) {
	ticker := time.NewTicker(Interval)
	defer ticker.Stop()
	lastRollup := time.Time{}
	for {
		if err := s.Sample(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("container metrics", "error", err)
		}
		if now := s.now(); now.Sub(lastRollup) >= time.Minute {
			lastRollup = now
			if err := s.st.RollupMetrics(ctx, now); err != nil && ctx.Err() == nil {
				s.log.Warn("metrics rollup", "error", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Sample reads every running app and database container once and stores one
// sample per resource (containers of the same app, such as the old and new one
// during a rolling deploy, are added together).
func (s *Sampler) Sample(ctx context.Context) error {
	list, err := s.host.ListContainers(ctx)
	if err != nil {
		return err
	}
	type reading struct {
		c     executor.Container
		stats container.StatsResponse
		err   error
	}
	var picked []executor.Container
	for _, c := range list {
		if !c.Running || c.ResourceID == "" || (c.Kind != executor.KindApp && c.Kind != executor.KindDatabase) {
			continue
		}
		if s.Include != nil && !s.Include(c) {
			continue
		}
		picked = append(picked, c)
	}
	readings := make([]reading, len(picked))
	var wg sync.WaitGroup
	limit := make(chan struct{}, 8)
	for i, c := range picked {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			statsCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			st, err := s.host.ContainerStats(statsCtx, c.ID)
			readings[i] = reading{c, st, err}
		}()
	}
	wg.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	at := s.now()
	seen := map[string]bool{}
	byResource := map[string]*store.MetricSample{}
	rated := map[string]bool{} // resources with at least one container that has rates
	var order []string
	for _, r := range readings {
		if r.err != nil {
			continue
		}
		key := string(r.c.Kind) + "/" + r.c.ResourceID
		m := byResource[key]
		if m == nil {
			m = &store.MetricSample{Kind: string(r.c.Kind), ResourceID: r.c.ResourceID}
			byResource[key] = m
			order = append(order, key)
		}
		cur := read(r.stats, at)
		seen[r.c.ID] = true
		m.Memory += memoryUsed(r.stats.MemoryStats)
		if l := int64(r.stats.MemoryStats.Limit); l > m.MemoryLimit {
			m.MemoryLimit = l
		}
		prev, ok := s.prev[r.c.ID]
		s.prev[r.c.ID] = cur
		if !ok {
			continue // rates need a second reading
		}
		rated[key] = true
		secs := cur.at.Sub(prev.at).Seconds()
		if secs <= 0 {
			continue
		}
		m.CPU += rate(prev.cpu, cur.cpu, secs) / 1e9
		m.NetRx += rate(prev.rx, cur.rx, secs)
		m.NetTx += rate(prev.tx, cur.tx, secs)
		m.DiskRead += rate(prev.diskRead, cur.diskRead, secs)
		m.DiskWrite += rate(prev.diskWrt, cur.diskWrt, secs)
	}
	for id := range s.prev {
		if !seen[id] {
			delete(s.prev, id) // stopped or removed
		}
	}
	samples := make([]store.MetricSample, 0, len(order))
	for _, k := range order {
		// A resource seen for the first time (new container, panel restart) waits
		// for its second reading, so its rates never show a false drop to zero.
		if rated[k] {
			samples = append(samples, *byResource[k])
		}
	}
	ts := at.Unix()
	return s.st.AddMetricSamples(ctx, ts-ts%int64(Interval/time.Second), samples)
}

func read(s container.StatsResponse, fallback time.Time) counters {
	c := counters{at: s.Read, cpu: s.CPUStats.CPUUsage.TotalUsage}
	if c.at.IsZero() {
		c.at = fallback
	}
	for _, n := range s.Networks {
		c.rx += n.RxBytes
		c.tx += n.TxBytes
	}
	for _, e := range s.BlkioStats.IoServiceBytesRecursive {
		switch strings.ToLower(e.Op) {
		case "read":
			c.diskRead += e.Value
		case "write":
			c.diskWrt += e.Value
		}
	}
	return c
}

// rate is the per-second increase of a counter; a counter that went back
// (the container restarted in place) counts as no activity.
func rate(prev, cur uint64, secs float64) float64 {
	if cur < prev {
		return 0
	}
	return float64(cur-prev) / secs
}

// memoryUsed matches `docker stats`: usage minus the inactive page cache.
func memoryUsed(m container.MemoryStats) int64 {
	cache := m.Stats["inactive_file"] // cgroup v2
	if v, ok := m.Stats["total_inactive_file"]; ok {
		cache = v // cgroup v1
	}
	if cache > m.Usage {
		return 0
	}
	return int64(m.Usage - cache)
}
