package api

import (
	"net/http"
	"time"

	"panel/internal/store"
)

// metricRange maps a requested window to the stored resolution it reads and the
// bucket it averages into, keeping every answer at about 360 points or fewer.
type metricRange struct {
	window     time.Duration
	resolution int
	step       int64
}

var metricRanges = map[string]metricRange{
	"1h":  {time.Hour, store.MetricRaw, 10},
	"6h":  {6 * time.Hour, store.MetricRaw, 60},
	"24h": {24 * time.Hour, store.MetricMinute, 240},
	"7d":  {7 * 24 * time.Hour, store.MetricMinute, 1800},
	"30d": {30 * 24 * time.Hour, store.MetricHour, 7200},
}

type metricSeriesJSON struct {
	Range  string               `json:"range"`
	Step   int64                `json:"step"`
	From   int64                `json:"from"`
	To     int64                `json:"to"`
	Points []store.MetricSample `json:"points"`
}

// handleResourceMetrics serves the usage history of an app or a database instance.
func (s *Server) handleResourceMetrics(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var err error
		if kind == "app" {
			_, err = s.store.GetApp(r.Context(), id)
		} else {
			_, err = s.store.GetDBInstance(r.Context(), id)
		}
		if s.storeError(w, "get "+kind, err, "") {
			return
		}
		name := r.URL.Query().Get("range")
		if name == "" {
			name = "1h"
		}
		rg, ok := metricRanges[name]
		if !ok {
			invalid(w, "range must be one of 1h, 6h, 24h, 7d, 30d")
			return
		}
		to := time.Now().Unix()
		from := to - int64(rg.window/time.Second)
		from -= from % rg.step
		points, err := s.store.MetricSeries(r.Context(), kind, id, rg.resolution, rg.step, from, to+1)
		if s.storeError(w, "read metrics", err, "") {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, metricSeriesJSON{Range: name, Step: rg.step, From: from, To: to, Points: points})
	}
}
