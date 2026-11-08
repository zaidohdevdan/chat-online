package metrics

import (
	"net/http"
	"sync/atomic"
	"time"

	"chat-online/internal/httpx"
)

type Collector struct {
	TotalRequests     atomic.Int64
	TotalErrors       atomic.Int64
	TotalLatencyMs    atomic.Int64
	ActiveWSConns     atomic.Int64
	OutboxPendingSize atomic.Int64
}

var Default = &Collector{}

func (c *Collector) RecordHTTP(status int, duration time.Duration) {
	c.TotalRequests.Add(1)
	if status >= 400 {
		c.TotalErrors.Add(1)
	}
	c.TotalLatencyMs.Add(duration.Milliseconds())
}

func (c *Collector) IncWS() {
	c.ActiveWSConns.Add(1)
}

func (c *Collector) DecWS() {
	c.ActiveWSConns.Add(-1)
}

func (c *Collector) SetOutboxPending(n int64) {
	c.OutboxPendingSize.Store(n)
}

func (c *Collector) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	reqs := c.TotalRequests.Load()
	totalLat := c.TotalLatencyMs.Load()
	var avgLat float64
	if reqs > 0 {
		avgLat = float64(totalLat) / float64(reqs)
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"httpRequestsTotal":       reqs,
		"httpErrorsTotal":         c.TotalErrors.Load(),
		"httpAvgLatencyMs":        avgLat,
		"websocketActiveConns":    c.ActiveWSConns.Load(),
		"outboxPendingEventsSize": c.OutboxPendingSize.Load(),
	})
}
