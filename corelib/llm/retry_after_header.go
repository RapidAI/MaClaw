package llm

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// retryAfterHeaderBody turns an HTTP Retry-After header on an empty-bodied
// 429 into the structured retry_after_seconds field that HubRetryWait and the
// provider cooldown already read. No usable header returns nil.
func retryAfterHeaderBody(resp *http.Response, now time.Time) []byte {
	if resp == nil {
		return nil
	}
	secs := parseRetryAfter(resp.Header.Get("Retry-After"), now)
	if secs <= 0 {
		return nil
	}
	return []byte(fmt.Sprintf(`{"error":{"message":"rate limited","type":"rate_limit"},"retry_after_seconds":%d}`, secs))
}

func parseRetryAfter(v string, now time.Time) int64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return int64((d + time.Second - 1) / time.Second)
		}
	}
	return 0
}
