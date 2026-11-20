package metrics

import (
	"context"
	"time"

	"panel/internal/hostmetrics"
	"panel/internal/store"
)

// FromHost turns the host collector's latest snapshot into a sample: CPU in cores used,
// RAM used and total, the network rates, and the data disk's used and total bytes in the
// disk fields. Nothing while the collector has no CPU reading yet (its first one only
// primes the counters), or when its snapshot is old.
func FromHost(snapshot func() hostmetrics.Snapshot) func() (store.MetricSample, bool) {
	return func() (store.MetricSample, bool) {
		s := snapshot()
		if s.CollectedAt.IsZero() || time.Since(s.CollectedAt) > 3*hostmetrics.Interval || s.CPU.Percent == nil {
			return store.MetricSample{}, false
		}
		m := store.MetricSample{CPU: *s.CPU.Percent / 100 * float64(max(s.CPU.Cores, 1))}
		if s.Memory != nil {
			m.Memory, m.MemoryLimit = int64(s.Memory.Used), int64(s.Memory.Total)
		}
		if s.Network != nil {
			if s.Network.ReceiveRate != nil {
				m.NetRx = *s.Network.ReceiveRate
			}
			if s.Network.SendRate != nil {
				m.NetTx = *s.Network.SendRate
			}
		}
		if s.Disk != nil {
			m.DiskRead, m.DiskWrite = float64(s.Disk.Used), float64(s.Disk.Total)
		}
		return m, true
	}
}

// HostHistory reads the host samples stored since a time back as points of the host
// collector's live history (CPU and RAM in percent), so a restarted panel continues its
// ten-minute charts instead of starting them empty.
func HostHistory(st *store.Store) func(ctx context.Context, cores int, since time.Time) []hostmetrics.Point {
	return func(ctx context.Context, cores int, since time.Time) []hostmetrics.Point {
		step := int64(Interval / time.Second)
		samples, err := st.MetricSeries(ctx, store.MetricHost, store.MetricHost, store.MetricRaw, step, since.Unix(), time.Now().Unix()+1)
		if err != nil {
			return nil
		}
		points := make([]hostmetrics.Point, 0, len(samples))
		for _, m := range samples {
			cpu, rx, tx := min(100, m.CPU/float64(max(cores, 1))*100), m.NetRx, m.NetTx
			p := hostmetrics.Point{At: time.Unix(m.TS, 0).UTC(), CPU: &cpu, ReceiveRate: &rx, SendRate: &tx}
			if m.MemoryLimit > 0 {
				mem := float64(m.Memory) / float64(m.MemoryLimit) * 100
				p.Memory = &mem
			}
			points = append(points, p)
		}
		return points
	}
}
