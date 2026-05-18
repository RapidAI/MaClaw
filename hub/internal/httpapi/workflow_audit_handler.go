package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
)

// WorkflowAuditQueryHandler handles GET /api/v1/audit.
// Supports filters: instance_id, approver_id, requester_id, time_range (start/end), decision.
// Results are paginated at 100 records per page.
//
// Query parameters:
//   - instance_id: filter by workflow instance ID
//   - approver_id: filter by approver VE ID
//   - requester_id: filter by requester ID (actor_id)
//   - start: start of time range (RFC3339 format)
//   - end: end of time range (RFC3339 format)
//   - decision: filter by decision outcome (approve/reject/escalate)
//   - page: page number (1-based, default 1)
//
// When multiple filters are provided, the handler uses the most specific filter
// in the following priority order: instance_id > approver_id > requester_id > decision > time_range.
// If no filter is provided, returns an error indicating at least one filter is required.
func WorkflowAuditQueryHandler(auditStore workflow.AuditStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}

		q := r.URL.Query()

		// Parse page number (1-based).
		page := 0 // AuditStore uses 0-based pages internally
		if p := strings.TrimSpace(q.Get("page")); p != "" {
			if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
				page = parsed - 1 // Convert 1-based to 0-based
			}
		}

		pageSize := workflow.DefaultAuditPageSize

		instanceID := strings.TrimSpace(q.Get("instance_id"))
		approverID := strings.TrimSpace(q.Get("approver_id"))
		requesterID := strings.TrimSpace(q.Get("requester_id"))
		decision := strings.TrimSpace(q.Get("decision"))
		startStr := strings.TrimSpace(q.Get("start"))
		endStr := strings.TrimSpace(q.Get("end"))

		var (
			entries []workflow.AuditEntry
			total   int
			err     error
		)

		ctx := r.Context()

		// Use the most specific filter in priority order.
		switch {
		case instanceID != "":
			entries, total, err = auditStore.QueryByInstance(ctx, instanceID, page, pageSize)

		case approverID != "":
			entries, total, err = auditStore.QueryByApprover(ctx, approverID, page, pageSize)

		case requesterID != "":
			// requester_id maps to actor_id in the audit store — use QueryByApprover
			// which queries by actor_id field.
			entries, total, err = auditStore.QueryByApprover(ctx, requesterID, page, pageSize)

		case decision != "":
			entries, total, err = auditStore.QueryByDecision(ctx, decision, page, pageSize)

		case startStr != "" || endStr != "":
			start, end, parseErr := parseTimeRange(startStr, endStr)
			if parseErr != nil {
				writeError(w, http.StatusBadRequest, "INVALID_TIME_RANGE", parseErr.Error())
				return
			}
			entries, total, err = auditStore.QueryByTimeRange(ctx, start, end, page, pageSize)

		default:
			writeError(w, http.StatusBadRequest, "MISSING_FILTER",
				"at least one filter is required: instance_id, approver_id, requester_id, decision, or time range (start/end)")
			return
		}

		if err != nil {
			writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "failed to query audit trail: "+err.Error())
			return
		}

		if entries == nil {
			entries = []workflow.AuditEntry{}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"entries":   entries,
			"total":     total,
			"page":      page + 1, // Return 1-based page to client
			"page_size": pageSize,
		})
	}
}

// parseTimeRange parses start and end time strings in RFC3339 format.
// If start is empty, it defaults to 3 years ago (matching audit retention period).
// If end is empty, it defaults to now.
func parseTimeRange(startStr, endStr string) (time.Time, time.Time, error) {
	var start, end time.Time
	var err error

	if startStr != "" {
		start, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start time format, use RFC3339 (e.g. 2024-01-01T00:00:00Z): %v", err)
		}
	} else {
		// Default: 3 years ago (matches audit retention requirement 10.6)
		start = time.Now().UTC().AddDate(-3, 0, 0)
	}

	if endStr != "" {
		end, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end time format, use RFC3339 (e.g. 2024-12-31T23:59:59Z): %v", err)
		}
	} else {
		end = time.Now().UTC()
	}

	if start.After(end) {
		return time.Time{}, time.Time{}, fmt.Errorf("start time must be before end time")
	}

	return start, end, nil
}
