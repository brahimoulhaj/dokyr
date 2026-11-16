package api

import (
	"net/http"
	"time"

	"panel/internal/brand"
)

// metricsCheck is how often the stream looks for a fresh sample. The collectors keep the
// latest one in memory, so this costs a comparison, not a measurement.
const metricsCheck = time.Second

// handleMetricsStream pushes host and Docker metrics as the collectors take them (every
// 5 s and 15 s), instead of the browser asking for them. ?host=1 and ?docker=1 pick which;
// each is sent once at the start and again whenever a new sample is taken.
func (s *Server) handleMetricsStream(w http.ResponseWriter, r *http.Request) {
	wantHost := r.URL.Query().Get("host") == "1" && s.metrics != nil
	wantDocker := r.URL.Query().Get("docker") == "1" && s.dockerMetrics != nil
	stream, err := startSSE(w)
	if err != nil {
		s.log.Error("metrics stream", "error", err)
		return
	}
	ctx, cancel := s.streamContext(r)
	defer cancel()
	check := time.NewTicker(metricsCheck)
	defer check.Stop()
	ping := time.NewTicker(ssePing)
	defer ping.Stop()
	var hostAt, dockerAt time.Time
	for {
		if wantHost {
			if snap := s.metrics.Snapshot(); !snap.CollectedAt.IsZero() && !snap.CollectedAt.Equal(hostAt) {
				hostAt = snap.CollectedAt
				if stream.event("", "host", snap) != nil {
					return
				}
			}
		}
		if wantDocker {
			if snap := s.dockerMetrics.Snapshot(); !snap.CheckedAt.IsZero() && !snap.CheckedAt.Equal(dockerAt) {
				dockerAt = snap.CheckedAt
				if stream.event("", "docker", snap) != nil {
					return
				}
			}
		}
		select {
		case <-check.C:
		case <-ping.C:
			// End the stream once its session is signed out or expires.
			if c, err := r.Cookie(brand.SessionCookie()); err == nil {
				if _, err := s.accounts.SessionUser(ctx, c.Value); err != nil {
					_ = stream.event("", "end", map[string]string{"reason": "signed out"})
					return
				}
			}
			if stream.ping() != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
