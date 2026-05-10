package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type haSyncReader interface {
	NodeID() string
	ListOpsAfterSeq(ctx context.Context, afterSeq int64, limit int) ([]*store.HASyncOp, error)
	MaxOpSeq(ctx context.Context) (int64, error)
	AuthenticatePeerRequest(r *http.Request) error
}

type haSyncApplier interface {
	AuthenticatePeerRequest(r *http.Request) error
	ApplyRemoteOps(ctx context.Context, ops []*store.HASyncOp) error
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
			if v > 2000 {
				v = 2000
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

func HAOpsApplyHandler(svc haSyncApplier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusNotImplemented, "HA_NOT_ENABLED", "HA sync is not enabled")
			return
		}
		if err := svc.AuthenticatePeerRequest(r); err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
			return
		}
		var req struct {
			Ops []*store.HASyncOp `json:"ops"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 128<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid HA ops payload")
			return
		}
		if len(req.Ops) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"applied": 0})
			return
		}
		if len(req.Ops) > 2000 {
			writeError(w, http.StatusBadRequest, "TOO_MANY_OPS", "ops batch is too large")
			return
		}
		if err := svc.ApplyRemoteOps(r.Context(), req.Ops); err != nil {
			var invalidOp ha.InvalidRemoteOpError
			if errors.As(err, &invalidOp) {
				writeError(w, http.StatusBadRequest, "INVALID_HA_OP", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "HA_APPLY_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"applied": len(req.Ops)})
	}
}
