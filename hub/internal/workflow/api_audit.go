package workflow

import (
	"net/http"
	"strconv"
	"time"
)

// AuditAPI provides HTTP handlers for querying the immutable audit trail.
type AuditAPI struct {
	auditStore AuditStore
}

// NewAuditAPI creates a new AuditAPI with the given audit store.
func NewAuditAPI(auditStore AuditStore) *AuditAPI {
	return &AuditAPI{
		auditStore: auditStore,
	}
}

// RegisterRoutes registers audit trail API routes on the given mux.
// The authMiddleware extracts the authenticated user from the request.
func (api *AuditAPI) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/audit", authMiddleware(api.handleQueryAudit))
}

// handleQueryAudit handles GET /api/v1/audit with query string filters.
//
// Supported filters (query parameters):
//   - instance_id: filter by workflow instance ID
//   - approver_id: filter by approver VE ID
//   - requester_id: filter by requester ID (actor_id field)
//   - decision: filter by decision outcome (approve/reject/escalate)
//   - start_time: filter by time range start (RFC3339 format)
//   - end_time: filter by time range end (RFC3339 format)
//   - page: page number (default 1)
//
// Pagination is fixed at 100 records per page (DefaultAuditPageSize).
//
// Filter priority: instance_id > approver_id > requester_id > time_range > decision.
// Only one primary filter is applied per request. If multiple are provided,
// the highest-priority filter is used.
//
// Response:
//
//	{
//	  "entries": [...],
//	  "total": 250,
//	  "page": 1,
//	  "page_size": 100
//	}
func (api *AuditAPI) handleQueryAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	page := 1
	if p := q.Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	pageSize := DefaultAuditPageSize

	instanceID := q.Get("instance_id")
	approverID := q.Get("approver_id")
	requesterID := q.Get("requester_id")
	decision := q.Get("decision")
	startTimeStr := q.Get("start_time")
	endTimeStr := q.Get("end_time")

	var (
		entries []AuditEntry
		total   int
		err     error
	)

	ctx := r.Context()

	// Apply filters in priority order: instance_id > approver_id > requester_id > time_range > decision
	switch {
	case instanceID != "":
		entries, total, err = api.auditStore.QueryByInstance(ctx, instanceID, page, pageSize)

	case approverID != "":
		entries, total, err = api.auditStore.QueryByApprover(ctx, approverID, page, pageSize)

	case requesterID != "":
		// requester_id maps to actor_id in the audit store; reuse QueryByApprover
		// since it queries by actor_id field.
		entries, total, err = api.auditStore.QueryByApprover(ctx, requesterID, page, pageSize)

	case startTimeStr != "" || endTimeStr != "":
		start, end, parseErr := parseTimeRange(startTimeStr, endTimeStr)
		if parseErr != nil {
			apiWriteError(w, http.StatusBadRequest, "INVALID_TIME_RANGE", parseErr.Error())
			return
		}
		entries, total, err = api.auditStore.QueryByTimeRange(ctx, start, end, page, pageSize)

	case decision != "":
		entries, total, err = api.auditStore.QueryByDecision(ctx, decision, page, pageSize)

	default:
		// No filter provided — return empty result with guidance
		apiWriteError(w, http.StatusBadRequest, "MISSING_FILTER", "at least one filter is required: instance_id, approver_id, requester_id, start_time/end_time, or decision")
		return
	}

	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "QUERY_FAILED", "failed to query audit trail: "+err.Error())
		return
	}

	if entries == nil {
		entries = []AuditEntry{}
	}

	apiWriteJSON(w, http.StatusOK, map[string]any{
		"entries":   entries,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// parseTimeRange parses start and end time strings in RFC3339 format.
// If start is empty, it defaults to 30 days ago.
// If end is empty, it defaults to now.
func parseTimeRange(startStr, endStr string) (time.Time, time.Time, error) {
	var start, end time.Time
	var err error

	if startStr != "" {
		start, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			return time.Time{}, time.Time{}, &timeParseError{field: "start_time", value: startStr, err: err}
		}
	} else {
		start = time.Now().UTC().AddDate(0, 0, -30)
	}

	if endStr != "" {
		end, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			return time.Time{}, time.Time{}, &timeParseError{field: "end_time", value: endStr, err: err}
		}
	} else {
		end = time.Now().UTC()
	}

	return start.UTC(), end.UTC(), nil
}

// timeParseError provides a user-friendly error message for time parsing failures.
type timeParseError struct {
	field string
	value string
	err   error
}

func (e *timeParseError) Error() string {
	return e.field + " must be in RFC3339 format (e.g. 2024-01-15T09:00:00Z), got: " + e.value
}
