package api

import (
	"net/http"

	"panel/internal/brand"
	"slices"
	"time"
)

// changesSettle is how long the stream waits after a write before sending, so a burst of
// writes (a deploy moving through its steps) goes out as one event, and the commit that
// triggered it has finished before the browser refetches.
const changesSettle = 150 * time.Millisecond

// handleChanges streams which tables changed, as {"tables": [...]}. The browser refetches
// the data it shows from those tables; nothing else travels on this stream. A reconnecting
// browser refetches everything, so missed events need no replay.
func (s *Server) handleChanges(w http.ResponseWriter, r *http.Request) {
	sub, stop := s.store.Changes().Subscribe()
	defer stop()
	stream, err := startSSE(w)
	if err != nil {
		s.log.Error("change stream", "error", err)
		return
	}
	ctx, cancel := s.streamContext(r)
	defer cancel()
	ping := time.NewTicker(ssePing)
	defer ping.Stop()
	for {
		select {
		case <-sub.Ready:
			select {
			case <-time.After(changesSettle):
			case <-ctx.Done():
				return
			}
			tables := sub.Drain()
			if len(tables) == 0 {
				continue
			}
			slices.Sort(tables)
			if stream.event("", "change", map[string][]string{"tables": tables}) != nil {
				return
			}
		case <-ping.C:
			// The session was checked when the stream opened; end it once that session is
			// signed out or expires.
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
