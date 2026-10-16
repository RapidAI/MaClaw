package transporthttp

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PrefersAsyncResponse implements RFC 7240 Prefer token matching.
func PrefersAsyncResponse(values []string) bool {
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			token := strings.TrimSpace(strings.SplitN(item, ";", 2)[0])
			if strings.EqualFold(token, "respond-async") {
				return true
			}
		}
	}
	return false
}

type PageQuery struct {
	Limit  int
	Before time.Time
}

const (
	DefaultPageLimit = 100
	MaxPageLimit     = 500
)

// ParsePageLimit parses only the shared `limit` query parameter. Handlers
// whose `before` cursor is not an RFC3339 timestamp (for example skill name
// cursors) must use this instead of ParsePageQuery.
func ParsePageLimit(r *http.Request) (int, error) {
	if r == nil {
		return DefaultPageLimit, nil
	}
	limit := DefaultPageLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return 0, fmt.Errorf("limit must be a positive integer")
		}
		if parsed > MaxPageLimit {
			parsed = MaxPageLimit
		}
		limit = parsed
	}
	return limit, nil
}

// ParsePageQuery parses the common timestamp-cursor pagination parameters
// used by the HTTP API. Domain handlers remain responsible for applying the
// cursor. Skill-name (and similar non-time) cursors must not call this.
func ParsePageQuery(r *http.Request) (PageQuery, error) {
	limit, err := ParsePageLimit(r)
	if err != nil {
		return PageQuery{}, err
	}
	page := PageQuery{Limit: limit}
	if r == nil {
		return page, nil
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("before")); raw != "" {
		before, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return PageQuery{}, fmt.Errorf("before must be an RFC3339 timestamp")
		}
		page.Before = before
	}
	return page, nil
}

// WantsAsyncResponse resolves the transport-level async preference from
// Prefer and the legacy async query parameter.
func WantsAsyncResponse(r *http.Request) (bool, error) {
	if r == nil {
		return false, nil
	}
	if PrefersAsyncResponse(r.Header.Values("Prefer")) {
		return true, nil
	}
	raw := strings.TrimSpace(r.URL.Query().Get("async"))
	if raw == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("invalid async query parameter")
	}
	return parsed, nil
}
