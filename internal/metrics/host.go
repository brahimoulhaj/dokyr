package metrics

import (
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
