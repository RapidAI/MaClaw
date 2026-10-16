package desktopd

import (
	"context"
	"errors"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Proxy error accounting. CONNECT failures used to be one log line each, which
// on a busy desktop is hundreds of identical "i/o timeout" / "HTTP 502" lines
// a day. Each class+target is logged at most once per proxyErrorLogInterval.
// Totals are always counted and are exposed on GET /v1/health as "proxy", and
// a one-line summary is logged on proxySummaryInterval while the totals move.

// ProxyMetrics is the process-wide egress-proxy error counters since start.
type ProxyMetrics struct {
	Timeout        uint64 `json:"timeout"`
	HTTP502        uint64 `json:"http_502"`
	Other          uint64 `json:"other"`
	Retries        uint64 `json:"retries"`
	Direct         uint64 `json:"direct"`
	SuppressedLogs uint64 `json:"suppressed_logs"`
}

type proxyStats struct {
	timeout    atomic.Uint64
	http502    atomic.Uint64
	other      atomic.Uint64
	retries    atomic.Uint64
	direct     atomic.Uint64
	suppressed atomic.Uint64

	mu      sync.Mutex
	lastLog map[string]time.Time
}

var (
	egressStats           proxyStats
	proxyErrorLogInterval = time.Minute
	proxySummaryInterval  = time.Minute
)

// SnapshotProxyMetrics returns a copy of the counters.
func SnapshotProxyMetrics() ProxyMetrics {
	return ProxyMetrics{
		Timeout:        egressStats.timeout.Load(),
		HTTP502:        egressStats.http502.Load(),
		Other:          egressStats.other.Load(),
		Retries:        egressStats.retries.Load(),
		Direct:         egressStats.direct.Load(),
		SuppressedLogs: egressStats.suppressed.Load(),
	}
}

func resetProxyMetrics() {
	egressStats.timeout.Store(0)
	egressStats.http502.Store(0)
	egressStats.other.Store(0)
	egressStats.retries.Store(0)
	egressStats.direct.Store(0)
	egressStats.suppressed.Store(0)
	egressStats.mu.Lock()
	egressStats.lastLog = nil
	egressStats.mu.Unlock()
}

// StartProxyErrorSummary logs a one-line delta of the counters on
// proxySummaryInterval until ctx is cancelled. Call it once from main.
func StartProxyErrorSummary(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(proxySummaryInterval)
		defer ticker.Stop()
		last := SnapshotProxyMetrics()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cur := SnapshotProxyMetrics()
				if cur.Timeout == last.Timeout && cur.HTTP502 == last.HTTP502 && cur.Other == last.Other {
					last = cur
					continue
				}
				log.Printf("[desktopd-proxy] upstream errors +%s timeout=%d http_502=%d other=%d retries=%d direct=%d (total timeout=%d http_502=%d other=%d suppressed=%d)",
					proxySummaryInterval,
					cur.Timeout-last.Timeout, cur.HTTP502-last.HTTP502, cur.Other-last.Other,
					cur.Retries-last.Retries, cur.Direct-last.Direct,
					cur.Timeout, cur.HTTP502, cur.Other, cur.SuppressedLogs)
				last = cur
			}
		}
	}()
}

func noteProxyRetry() { egressStats.retries.Add(1) }

func noteProxyDirect() { egressStats.direct.Add(1) }

// noteProxyFailure counts err and logs it unless this class+target was already
// logged inside proxyErrorLogInterval. The line carries the running totals so
// a single sample still shows how often the failure is happening.
func noteProxyFailure(target, from string, err error) {
	if err == nil {
		return
	}
	class := classifyProxyError(err)
	switch class {
	case "timeout":
		egressStats.timeout.Add(1)
	case "http_502":
		egressStats.http502.Add(1)
	default:
		egressStats.other.Add(1)
	}
	if !egressStats.allowLog(class+" "+target, time.Now()) {
		return
	}
	s := SnapshotProxyMetrics()
	log.Printf("[desktopd-proxy] CONNECT %s from %s: %v (timeout=%d http_502=%d other=%d retries=%d direct=%d suppressed=%d)",
		target, from, err, s.Timeout, s.HTTP502, s.Other, s.Retries, s.Direct, s.SuppressedLogs)
}

func (m *proxyStats) allowLog(key string, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastLog == nil {
		m.lastLog = map[string]time.Time{}
	}
	if prev, ok := m.lastLog[key]; ok && now.Sub(prev) < proxyErrorLogInterval {
		m.suppressed.Add(1)
		return false
	}
	m.lastLog[key] = now
	return true
}

// upstreamStatusError is a non-200 CONNECT response from the chained proxy.
// The text matches the historical log line ("answered HTTP N").
type upstreamStatusError struct {
	host string
	code int
}

func (e *upstreamStatusError) Error() string {
	if e == nil {
		return "upstream proxy answered HTTP 0"
	}
	return "upstream proxy " + e.host + " answered HTTP " + strconv.Itoa(e.code)
}

func classifyProxyError(err error) string {
	var status *upstreamStatusError
	if errors.As(err, &status) {
		if status.code == 502 {
			return "http_502"
		}
		return "other"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded") {
		return "timeout"
	}
	return "other"
}

// retryableUpstream reports whether another CONNECT attempt could succeed.
// A bad proxy key (407) and a forbidden target are not retried.
func retryableUpstream(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var status *upstreamStatusError
	if errors.As(err, &status) {
		return status.code == 502 || status.code == 503 || status.code == 504
	}
	if classifyProxyError(err) == "timeout" {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") || strings.Contains(msg, "broken pipe") || strings.Contains(msg, "eof")
}
