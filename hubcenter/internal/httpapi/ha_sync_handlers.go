package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

const (
	haOpsApplyJSONBodyLimit = 128 << 20
	haOpsPullMaxBatchSize   = 50000
	// haOpsPullResponseByteBudget caps the JSON payload of a pull response.
	// Clients read the body with a 128MiB limit; a batch of large ops
	// (llm_official_class_head records run ~1MB each) could exceed it, the
	// truncated payload failed to decode on the peer, and the peer's cursor
	// stopped advancing while pruning kept deleting the ops behind it
	// (hc-1 was deadlocked ~25h this way, 2026-09-25). Trimming the batch
	// here keeps every response under the client limit; has_more makes the
	// peer come back for the remainder.
	haOpsPullResponseByteBudget = 64 << 20
)

type haSyncReader interface {
	NodeID() string
	ListOpsAfterSeq(ctx context.Context, afterSeq int64, limit int) ([]*store.HASyncOp, error)
	MaxOpSeq(ctx context.Context) (int64, error)
	MinOpSeq(ctx context.Context) (int64, error)
	AuthenticatePeerRequest(r *http.Request) error
}

type haSyncApplier interface {
	AuthenticatePeerRequest(r *http.Request) error
	ApplyRemoteOps(ctx context.Context, ops []*store.HASyncOp) error
}

type haPeerAckRecorder interface {
	RecordPeerPullAck(ctx context.Context, nodeID string, ackSeq int64, protocol int) error
}

type haSnapshotSource interface {
	AuthenticatePeerRequest(r *http.Request) error
	NodeID() string
	ListSnapshotHeads(ctx context.Context, afterSeq int64, limit int) ([]*store.HASyncOp, error)
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
			if v > haOpsPullMaxBatchSize {
				v = haOpsPullMaxBatchSize
			}
			limit = v
		}
		ops, err := svc.ListOpsAfterSeq(r.Context(), afterSeq, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "HA_PULL_FAILED", err.Error())
			return
		}
		recordPeerPullAck(r, svc, afterSeq)
		// Collapse last-write-wins duplicates inside this page before the
		// byte budget. Append-only ops are kept. The highest seq stays, so
		// an old peer that advances its cursor to the last returned op does
		// not skip a newer state.
		ops, _ = ha.CoalesceOps(ops)
		// NOTE: the budget bounds the *response*, not the *fetch*. A request
		// with limit=50000 still materializes every op in memory before this
		// trim runs. In practice the only caller is the peer syncer whose
		// pull_batch_size is ~100, so the exposure is theoretical; if that
		// ever changes, move the budget into the store query itself.
		ops = trimOpsToByteBudget(ops, haOpsPullResponseByteBudget)
		maxSeq, err := svc.MaxOpSeq(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "HA_PULL_FAILED", err.Error())
			return
		}
		minSeq, err := svc.MinOpSeq(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "HA_PULL_FAILED", err.Error())
			return
		}
		nextAfterSeq := afterSeq
		if len(ops) > 0 {
			nextAfterSeq = ops[len(ops)-1].Seq
		}
		writeJSONMaybeGzip(w, r, http.StatusOK, map[string]any{
			"node_id":        svc.NodeID(),
			"ops":            ops,
			"next_after_seq": nextAfterSeq,
			"has_more":       maxSeq > nextAfterSeq,
			"max_seq":        maxSeq,
			"min_seq":        minSeq,
			"protocol":       ha.ProtocolVersion,
		})
	}
}

func HASnapshotPullHandler(svc haSnapshotSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusNotFound, "HA_SNAPSHOT_UNAVAILABLE", "HA snapshot catch-up is not enabled")
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
		ops, err := svc.ListSnapshotHeads(r.Context(), afterSeq, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "HA_SNAPSHOT_FAILED", err.Error())
			return
		}
		if ops == nil {
			ops = []*store.HASyncOp{}
		}
		nextAfterSeq := afterSeq
		if len(ops) > 0 {
			nextAfterSeq = ops[len(ops)-1].Seq
		}
		writeJSONMaybeGzip(w, r, http.StatusOK, map[string]any{
			"node_id":        svc.NodeID(),
			"ops":            ops,
			"next_after_seq": nextAfterSeq,
			"has_more":       len(ops) >= limit && limit > 0,
			"protocol":       ha.ProtocolVersion,
		})
	}
}

func recordPeerPullAck(r *http.Request, svc haSyncReader, afterSeq int64) {
	rec, ok := svc.(haPeerAckRecorder)
	if !ok || r == nil {
		return
	}
	raw := r.URL.Query().Get("ack_seq")
	if raw == "" {
		return
	}
	ack, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || ack < 0 {
		return
	}
	nodeID := ha.RequestPeerNodeID(r)
	if nodeID == "" {
		nodeID = strings.TrimSpace(r.URL.Query().Get("node_id"))
	}
	if nodeID == "" {
		return
	}
	protocol := 1
	if p, err := strconv.Atoi(strings.TrimSpace(r.Header.Get(ha.ProtocolHeader))); err == nil && p > 0 {
		protocol = p
	}
	if err := rec.RecordPeerPullAck(r.Context(), nodeID, ack, protocol); err != nil {
		log.Printf("[hubcenter][ha] record peer ack %s seq=%d: %v", nodeID, ack, err)
	}
	_ = afterSeq
}

func acceptsGzip(r *http.Request) bool {
	if r == nil {
		return false
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if strings.EqualFold(strings.TrimSpace(part), "gzip") {
			return true
		}
	}
	return false
}

func writeJSONMaybeGzip(w http.ResponseWriter, r *http.Request, status int, data any) {
	if !acceptsGzip(r) {
		writeJSON(w, status, data)
		return
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(data); err != nil {
		writeError(w, http.StatusInternalServerError, "HA_ENCODE_FAILED", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Encoding", "gzip")
	w.WriteHeader(status)
	zw := gzip.NewWriter(w)
	_, _ = zw.Write(buf.Bytes())
	_ = zw.Close()
}

// trimOpsToByteBudget shortens an op batch so its JSON encoding fits within
// budget bytes. Because ops are ordered by seq, dropping the tail keeps the
// batch contiguous; the next_after_seq / has_more computation that follows
// then makes the peer re-pull the remainder on its next pass.
// At least one op is always kept so a batch of pathologically large ops still
// makes forward progress instead of deadlocking the cursor at seq boundaries.
func trimOpsToByteBudget(ops []*store.HASyncOp, budget int64) []*store.HASyncOp {
	if len(ops) == 0 || budget <= 0 {
		return ops
	}
	var total int64
	for i, op := range ops {
		encoded, err := json.Marshal(op)
		if err != nil {
			// Let the normal response write path surface the encoding error.
			return ops
		}
		total += int64(len(encoded)) + 1 // comma between array elements
		if total > budget {
			if i == 0 {
				return ops[:1]
			}
			return ops[:i]
		}
	}
	return ops
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
		if err := decodeLimitedJSON(w, r, &req, haOpsApplyJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_BODY", "invalid HA ops payload")
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
