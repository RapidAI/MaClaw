package ha

import "github.com/RapidAI/CodeClaw/hubcenter/internal/store"

const (
	// ProtocolVersion is advertised on HA pull and push requests.
	// Peers that omit it are protocol 1: they only understand the original
	// ops list and must not have fully-acked history deleted out from under
	// a cursor they have not reported.
	ProtocolVersion = 2
	// ProtocolHeader is the request header that carries ProtocolVersion.
	// Old peers ignore it. New peers record it with the puller's ack.
	ProtocolHeader = "X-HA-Protocol"
)

// IsAppendOnlyEntity reports entity types whose every op is an independent
// fact. Coalescing or replacing them with a snapshot head would drop data
// (usage and ledger batches). Last-write-wins entities are everything else.
func IsAppendOnlyEntity(entityType string) bool {
	switch entityType {
	case EntityLLMUsageBatch, EntityTokenBankLedgerBatch:
		return true
	default:
		return false
	}
}

// CoalesceOps keeps the last op in the batch for each last-write-wins entity
// and every append-only op. Order follows the surviving ops' sequence, so the
// last surviving op is still the highest seq in the input (nil ops aside).
// A peer that advances its cursor to that seq therefore does not skip a newer
// state. dropped is how many ops were removed.
func CoalesceOps(ops []*store.HASyncOp) (out []*store.HASyncOp, dropped int) {
	if len(ops) < 2 {
		return ops, 0
	}
	latest := make(map[string]int, len(ops))
	for i, op := range ops {
		if op == nil || IsAppendOnlyEntity(op.EntityType) {
			continue
		}
		latest[op.EntityType+"\x00"+op.EntityID] = i
	}
	out = make([]*store.HASyncOp, 0, len(ops))
	for i, op := range ops {
		if op == nil {
			dropped++
			continue
		}
		if IsAppendOnlyEntity(op.EntityType) {
			out = append(out, op)
			continue
		}
		if latest[op.EntityType+"\x00"+op.EntityID] != i {
			dropped++
			continue
		}
		out = append(out, op)
	}
	return out, dropped
}
