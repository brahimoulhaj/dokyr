package store

import (
	"context"
	"testing"
	"time"
)

func TestMetricRollupsAndRetention(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	// Two minutes of raw samples: CPU 1..12 cores, memory 100..1200.
	for i := 0; i < 12; i++ {
		ts := base.Add(time.Duration(i*10) * time.Second).Unix()
		if err := s.AddMetricSamples(ctx, ts, []MetricSample{{Kind: "app", ResourceID: "a1", CPU: float64(i + 1), Memory: int64(100 * (i + 1)), MemoryLimit: 2048, NetRx: 10}}); err != nil {
			t.Fatal(err)
		}
	}
	// Mid-way through the second minute only the first one is complete.
	if err := s.RollupMetrics(ctx, base.Add(90*time.Second)); err != nil {
		t.Fatal(err)
	}
	minutes, _ := s.MetricSeries(ctx, "app", "a1", MetricMinute, 60, base.Unix(), base.Add(time.Hour).Unix())
	if len(minutes) != 1 || minutes[0].CPU != 3.5 || minutes[0].CPUMax != 6 || minutes[0].Memory != 350 || minutes[0].MemoryMax != 600 || minutes[0].MemoryLimit != 2048 {
		t.Fatalf("first minute %+v", minutes)
	}
	if err := s.RollupMetrics(ctx, base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if minutes, _ = s.MetricSeries(ctx, "app", "a1", MetricMinute, 60, base.Unix(), base.Add(time.Hour).Unix()); len(minutes) != 2 || minutes[1].CPU != 9.5 {
		t.Fatalf("minutes %+v", minutes)
	}
	// Raw samples read in 30 s buckets.
	raw, _ := s.MetricSeries(ctx, "app", "a1", MetricRaw, 30, base.Unix(), base.Add(time.Hour).Unix())
	if len(raw) != 4 || raw[0].CPU != 2 || raw[0].CPUMax != 3 {
		t.Fatalf("raw buckets %+v", raw)
	}
	// After the hour closes, the hour bucket averages its minutes.
	if err := s.RollupMetrics(ctx, base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	hours, _ := s.MetricSeries(ctx, "app", "a1", MetricHour, 3600, base.Unix(), base.Add(2*time.Hour).Unix())
	if len(hours) != 1 || hours[0].CPU != 6.5 || hours[0].CPUMax != 12 || hours[0].NetRx != 10 {
		t.Fatalf("hours %+v", hours)
	}
	// Two days later raw samples are gone; minutes and hours remain.
	if err := s.RollupMetrics(ctx, base.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if raw, _ = s.MetricSeries(ctx, "app", "a1", MetricRaw, 10, 0, base.Add(time.Hour).Unix()); len(raw) != 0 {
		t.Fatalf("raw samples kept past 24 hours: %d", len(raw))
	}
	if minutes, _ = s.MetricSeries(ctx, "app", "a1", MetricMinute, 60, 0, base.Add(time.Hour).Unix()); len(minutes) != 2 {
		t.Fatalf("minutes pruned early: %d", len(minutes))
	}
	if err := s.RollupMetrics(ctx, base.Add(31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if hours, _ = s.MetricSeries(ctx, "app", "a1", MetricHour, 3600, 0, base.Add(2*time.Hour).Unix()); len(hours) != 0 {
		t.Fatal("hours kept past 30 days")
	}
}
