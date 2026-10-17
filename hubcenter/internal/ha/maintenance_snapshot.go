package ha

import (
	"context"
	"strings"
)

// MaintenanceSnapshot is the replication view used by automatic SQLite
// conversion. PendingOps is the ha_sync_ops row count. InboundPending is
// true until every configured peer has a successful pull with an empty
// last error, so a restart does not VACUUM before the first sync.
type MaintenanceSnapshot struct {
	PeerCount      int
	AckKnown       bool
	MaxSeq         int64
	PinAck         int64
	InboundPending bool
	PendingOps     int64
}

type maintenanceOpStats interface {
	Count(ctx context.Context) (int64, error)
	GetMaxSeq(ctx context.Context) (int64, error)
}

// MaintenanceSnapshot reads local replication state. It does not contact peers.
func (s *Service) MaintenanceSnapshot(ctx context.Context) (MaintenanceSnapshot, error) {
	if s == nil {
		return MaintenanceSnapshot{}, nil
	}
	peers := s.PeerNodeIDs()
	snap := MaintenanceSnapshot{PeerCount: len(peers)}
	if stats, ok := s.ops.(maintenanceOpStats); ok {
		n, err := stats.Count(ctx)
		if err != nil {
			return snap, err
		}
		snap.PendingOps = n
		maxSeq, err := stats.GetMaxSeq(ctx)
		if err != nil {
			return snap, err
		}
		snap.MaxSeq = maxSeq
	}
	if len(peers) == 0 {
		snap.AckKnown = true
		return snap, nil
	}
	if reader, ok := s.ops.(peerAckReader); ok {
		floor, err := reader.MinPeerAck(ctx, peers, s.peerLagGrace)
		if err != nil {
			return snap, err
		}
		snap.AckKnown = floor.Known
		snap.PinAck = floor.PinAck
	}
	for _, id := range peers {
		if s.cursors == nil {
			snap.InboundPending = true
			break
		}
		cursor, err := s.cursors.Get(ctx, id)
		if err != nil {
			return snap, err
		}
		if cursor == nil || cursor.LastSuccessAt == nil || strings.TrimSpace(cursor.LastError) != "" {
			snap.InboundPending = true
			break
		}
	}
	return snap, nil
}
