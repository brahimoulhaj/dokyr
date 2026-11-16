package notify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"panel/internal/store"
)

// Usage rules compare stored container metrics against their own threshold, so
// they are evaluated here and sent straight to their rule's channel (not through
// Handle, which would send to every rule of the event whatever its threshold).
const (
	UsageCPU    = "usage.cpu_high"    // threshold in cores
	UsageMemory = "usage.memory_high" // threshold in percent of the memory limit
)

// coverage is the share of a window's 10-second samples that must exist before a
// usage rule may fire: a container that just started does not count as "for 5 minutes".
const coverage = 0.8

// WatchUsage evaluates usage rules every interval until ctx ends.
func (s *Service) WatchUsage(ctx context.Context, interval time.Duration) {
	fired := map[string]bool{}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s.checkUsage(ctx, time.Now(), fired)
	}
}

// checkUsage fires a rule for a resource once every sample of its window is at or
// above the threshold, and sends "back to normal" (re-arming it) once the latest
// sample is below. fired holds the alerts in progress, by rule and resource.
func (s *Service) checkUsage(ctx context.Context, now time.Time, fired map[string]bool) {
	seen := map[string]bool{}
	for _, event := range []string{UsageCPU, UsageMemory} {
		rules, err := s.st.EnabledAlertRules(ctx, event)
		if err != nil {
			s.log.Error("usage rules", "error", err)
			return
		}
		for _, r := range rules {
			window := time.Duration(max(60, r.Duration)) * time.Second
			list, err := s.st.MetricWindows(ctx, "", r.TargetID, now.Add(-window).Unix())
			if err != nil {
				s.log.Error("usage metrics", "error", err)
				return
			}
			need := int(coverage * window.Seconds() / 10)
			for _, w := range list {
				key := r.ID + "/" + w.Kind + "/" + w.ResourceID
				seen[key] = true
				low, last := w.MinCPU, w.LastCPU
				if event == UsageMemory {
					low, last = w.MinMemoryPct, w.LastMemPct
				}
				switch {
				case !fired[key] && w.Samples >= need && low >= r.Threshold:
					fired[key] = true
					s.sendUsage(ctx, r, w, event, last, window, true)
				case fired[key] && last < r.Threshold:
					delete(fired, key)
					s.sendUsage(ctx, r, w, event, last, window, false)
				}
			}
		}
	}
	for key := range fired {
		if !seen[key] {
			delete(fired, key) // the rule went, or the resource stopped reporting
		}
	}
}

func (s *Service) sendUsage(ctx context.Context, r store.AlertRule, w store.MetricWindow, event string, now float64, window time.Duration, high bool) {
	name, path, targetType := w.ResourceID, "/", "app"
	if w.Kind == "app" {
		if a, err := s.st.GetApp(ctx, w.ResourceID); err == nil {
			name, path = a.Name, "/projects/"+a.ProjectID+"/apps/"+a.ID+"?tab=metrics"
		}
	} else {
		targetType = "database"
		if d, err := s.st.GetDBInstance(ctx, w.ResourceID); err == nil {
			name, path = d.Name, "/databases/"+d.ID+"?tab=metrics"
		}
	}
	what, value, limit := "CPU", cores(now), cores(r.Threshold)
	if event == UsageMemory {
		what, value, limit = "Memory", percent(now), percent(r.Threshold)+" of its limit"
	}
	minutes := int(window / time.Minute)
	m := Message{At: time.Now(), Level: "error"}
	action := event
	if high {
		m.Title = "High CPU: " + name
		if event == UsageMemory {
			m.Title = "High memory: " + name
		}
		m.Text = fmt.Sprintf("%s has stayed at or above %s for %d minute%s (now %s).", what, limit, minutes, plural(minutes), value)
	} else {
		action = strings.TrimSuffix(event, "_high") + "_normal"
		m.Level, m.Title = "success", what+" back to normal: "+name
		m.Text = fmt.Sprintf("%s is now %s, below %s.", what, value, limit)
	}
	m.URL, _ = s.panelURL(ctx, path)
	// The Activity log keeps a trace; Handle ignores usage events (see Handle).
	if err := s.st.AddEvent(ctx, store.Event{Actor: "system", Action: action, TargetType: targetType, TargetID: w.ResourceID, TargetName: name, Detail: m.Text}); err != nil {
		s.log.Error("record event", "error", err)
	}
	_ = s.deliver(ctx, r.ChannelID, m)
}

func cores(n float64) string   { return fmt.Sprintf("%.2f cores", n) }
func percent(n float64) string { return fmt.Sprintf("%.0f%%", n) }
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
