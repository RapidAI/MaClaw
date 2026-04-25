package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type haSyncReader interface {
	NodeID() string
	ListOpsAfterSeq(ctx context.Context, afterSeq int64, limit int) ([]*store.HASyncOp, error)
	MaxOpSeq(ctx context.Context) (int64, error)
	AuthenticatePeerRequest(r *http.Request) error
}

func HAOpsPullHandler(svc haSyncReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusNotImplemented, "HA_NOT_ENABLED", "HA sync is not enabled")
			return
		}
		if err := svc.AuthenticatePeerRequest(r); err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
			return
		}
		afterSeq := int64(0)
		if raw := r.URL.Query().Get("after_seq"); raw != "" {
			v, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || v < 0 {
				writeError(w, http.StatusBadRequest, "INVALID_AFTER_SEQ", "after_seq must be a non-negative integer")
				return
			}
			afterSeq = v
		}
		limit := 200
		if raw := r.URL.Query().Get("limit"); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil || v <= 0 {
				writeError(w, http.StatusBadRequest, "INVALID_LIMIT", "limit must be a positive integer")
				return
			}
			if v > 500 {
				v = 500
			}
			limit = v
		}
		ops, err := svc.ListOpsAfterSeq(r.Context(), afterSeq, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "HA_PULL_FAILED", err.Error())
			return
		}
		maxSeq, err := svc.MaxOpSeq(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "HA_PULL_FAILED", err.Error())
			return
		}
		nextAfterSeq := afterSeq
		if len(ops) > 0 {
			nextAfterSeq = ops[len(ops)-1].Seq
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"node_id":        svc.NodeID(),
			"ops":            ops,
			"next_after_seq": nextAfterSeq,
			"has_more":       len(ops) >= limit,
			"max_seq":        maxSeq,
		})
	}
}
