package metrics

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"

	"panel/internal/executor"
	"panel/internal/hostmetrics"
	"panel/internal/store"
)

type fakeHost struct {
	list  []executor.Container
	stats map[string]container.StatsResponse
}

func (f *fakeHost) ListContainers(context.Context) ([]executor.Container, error) { return f.list, nil }
func (f *fakeHost) ContainerStats(_ context.Context, id string) (container.StatsResponse, error) {
	return f.stats[id], nil
}

func stats(at time.Time, cpuSeconds, rx, written, memory, inactive uint64) container.StatsResponse {
	return container.StatsResponse{
		Read:        at,
		CPUStats:    container.CPUStats{CPUUsage: container.CPUUsage{TotalUsage: cpuSeconds * 1e9}},
		MemoryStats: container.MemoryStats{Usage: memory, Limit: 1 << 30, Stats: map[string]uint64{"inactive_file": inactive}},
		Networks:    map[string]container.NetworkStats{"eth0": {RxBytes: rx, TxBytes: rx / 2}},
		BlkioStats:  container.BlkioStats{IoServiceBytesRecursive: []container.BlkioStatEntry{{Op: "Write", Value: written}, {Op: "Read", Value: 7}}},
	}
}

func TestSampler(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	host := &fakeHost{list: []executor.Container{
		{ID: "old", Kind: executor.KindApp, ResourceID: "app1", Running: true},
		{ID: "new", Kind: executor.KindApp, ResourceID: "app1", Running: true},
		{ID: "db", Kind: executor.KindDatabase, ResourceID: "db1", Running: true, Name: "db"},
		{ID: "gate", Kind: executor.KindDatabase, ResourceID: "db1", Running: true, Name: "gate"},
		{ID: "proxy", Kind: executor.KindProxy, ResourceID: "caddy", Running: true},
		{ID: "stopped", Kind: executor.KindApp, ResourceID: "app2"},
	}}
	t0 := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	s := New(st, host, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Include = func(c executor.Container) bool { return c.Name != "gate" }
	clock := t0
	s.now = func() time.Time { return clock }
	set := func(at time.Time, cpu, rx, written uint64) {
		host.stats = map[string]container.StatsResponse{
			"old": stats(at, cpu, rx, written, 300, 100), "new": stats(at, cpu, rx, written, 300, 100),
			"db": stats(at, 2*cpu, 0, 0, 1000, 0), "gate": stats(at, 100, 1e9, 0, 1e6, 0),
		}
	}
	set(t0, 10, 1000, 0)
	if err := s.Sample(ctx); err != nil {
		t.Fatal(err)
	}
	clock = t0.Add(10 * time.Second)
	set(clock, 15, 11000, 2000) // +5 s CPU, +10000 B received, +2000 B written in 10 s
	if err := s.Sample(ctx); err != nil {
		t.Fatal(err)
	}
	series, _ := st.MetricSeries(ctx, "app", "app1", store.MetricRaw, 10, 0, clock.Unix()+1)
	// The first reading only primes the counters: no sample with zero rates.
	if len(series) != 1 {
		t.Fatalf("app samples %+v", series)
	}
	m := series[0]
	// Two containers of the same app are added together.
	if m.CPU != 1 || m.Memory != 400 || m.MemoryLimit != 1<<30 || m.NetRx != 2000 || m.NetTx != 1000 || m.DiskWrite != 400 || m.DiskRead != 0 {
		t.Fatalf("app sample %+v", m)
	}
	db, _ := st.MetricSeries(ctx, "database", "db1", store.MetricRaw, 10, 0, clock.Unix()+1)
	if len(db) != 1 || db[0].CPU != 1 || db[0].Memory != 1000 || db[0].NetRx != 0 {
		t.Fatalf("database sample (gate excluded) %+v", db)
	}
	for _, kind := range []string{"proxy", "app"} {
		id := map[string]string{"proxy": "caddy", "app": "app2"}[kind]
		if got, _ := st.MetricSeries(ctx, kind, id, store.MetricRaw, 10, 0, clock.Unix()+1); len(got) != 0 {
			t.Fatalf("%s %s sampled", kind, id)
		}
	}
	// A container that disappears loses its counters; a new one starts without rates.
	host.list = host.list[1:]
	clock = clock.Add(10 * time.Second)
	set(clock, 20, 21000, 4000)
	if err := s.Sample(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.prev["old"]; ok {
		t.Fatal("counters of a removed container kept")
	}
}

// The host's own usage is stored with the containers', also when no container runs, and
// stays out of the windows the usage alerts read.
func TestSamplerHost(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	s := New(st, &fakeHost{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	clock := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	percent, rx := 25.0, 1000.0
	snapshot := hostmetrics.Snapshot{}
	s.HostSample = FromHost(func() hostmetrics.Snapshot { return snapshot })
	if err := s.Sample(ctx); err != nil { // the collector has nothing yet
		t.Fatal(err)
	}
	snapshot = hostmetrics.Snapshot{CollectedAt: time.Now(), CPU: hostmetrics.CPU{Cores: 4}}
	if err := s.Sample(ctx); err != nil { // no CPU reading yet: its first one primes the counters
		t.Fatal(err)
	}
	read := func() []store.MetricSample {
		t.Helper()
		got, err := st.MetricSeries(ctx, store.MetricHost, store.MetricHost, store.MetricRaw, 10, clock.Unix()-3600, clock.Unix()+1)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := read(); len(got) != 0 {
		t.Fatalf("stored before there was a reading: %+v", got)
	}
	snapshot = hostmetrics.Snapshot{
		CollectedAt: time.Now(), CPU: hostmetrics.CPU{Cores: 4, Percent: &percent},
		Memory:  &hostmetrics.Memory{Total: 8 << 30, Used: 2 << 30},
		Network: &hostmetrics.Network{ReceiveRate: &rx},
		Disk:    &hostmetrics.Disk{Memory: hostmetrics.Memory{Total: 100 << 30, Used: 40 << 30}},
	}
	if err := s.Sample(ctx); err != nil {
		t.Fatal(err)
	}
	got := read()
	if len(got) != 1 || got[0].CPU != 1 || got[0].Memory != 2<<30 || got[0].MemoryLimit != 8<<30 || got[0].NetRx != 1000 || got[0].NetTx != 0 ||
		got[0].DiskRead != 40<<30 || got[0].DiskWrite != 100<<30 {
		t.Fatalf("host sample = %+v", got)
	}
	// An old snapshot (the collector stopped) is not stored again and again.
	snapshot.CollectedAt = time.Now().Add(-time.Minute)
	clock = clock.Add(Interval)
	if err := s.Sample(ctx); err != nil {
		t.Fatal(err)
	}
	if got := read(); len(got) != 1 {
		t.Fatalf("a stale snapshot was stored: %+v", got)
	}
	// Rolled up like any resource, and not a target of the usage alerts on "everything".
	if err := st.RollupMetrics(ctx, clock.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	minutes, err := st.MetricSeries(ctx, store.MetricHost, store.MetricHost, store.MetricMinute, 60, clock.Unix()-3600, clock.Unix()+1)
	if err != nil || len(minutes) != 1 || minutes[0].DiskWrite != 100<<30 {
		t.Fatalf("minute rollup = %+v %v", minutes, err)
	}
	if w, err := st.MetricWindows(ctx, "", "", clock.Unix()-3600); err != nil || len(w) != 0 {
		t.Fatalf("the host is in the alert windows: %+v %v", w, err)
	}
	if w, err := st.MetricWindows(ctx, store.MetricHost, "", clock.Unix()-3600); err != nil || len(w) != 1 {
		t.Fatalf("asked for by kind: %+v %v", w, err)
	}
}

func TestHostHistory(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	ts := now - now%10 - 60
	for i, m := range []store.MetricSample{
		{CPU: 1, Memory: 250, MemoryLimit: 1000, NetRx: 5, NetTx: 3},
		{CPU: 9}, // more than the cores, and no memory reading
	} {
		m.Kind, m.ResourceID = store.MetricHost, store.MetricHost
		if err := st.AddMetricSamples(ctx, ts+int64(i)*10, []store.MetricSample{m}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.AddMetricSamples(ctx, ts-3600, []store.MetricSample{{Kind: store.MetricHost, ResourceID: store.MetricHost, CPU: 2}}); err != nil {
		t.Fatal(err)
	}
	points := HostHistory(st)(ctx, 4, time.Unix(ts-600, 0))
	if len(points) != 2 {
		t.Fatalf("points = %+v", points)
	}
	a, b := points[0], points[1]
	if a.At.Unix() != ts || *a.CPU != 25 || *a.Memory != 25 || *a.ReceiveRate != 5 || *a.SendRate != 3 {
		t.Fatalf("first point = %v cpu %v memory %v", a.At, *a.CPU, *a.Memory)
	}
	if b.At.Unix() != ts+10 || *b.CPU != 100 || b.Memory != nil {
		t.Fatalf("second point = %+v", b)
	}
}
