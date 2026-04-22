package ha

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

func TestShouldApplyRemoteVersion(t *testing.T) {
	now := time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		current *store.HAEntityVersion
		op      *store.HASyncOp
		want    bool
	}{
		{
			name:    "apply when current is missing",
			current: nil,
			op:      &store.HASyncOp{EntityVersion: 1, OccurredAt: now, SourceNodeID: "node-b"},
			want:    true,
		},
		{
			name:    "apply higher version",
			current: &store.HAEntityVersion{Version: 2, UpdatedAt: now, UpdatedByNodeID: "node-a"},
			op:      &store.HASyncOp{EntityVersion: 3, OccurredAt: now.Add(-time.Second), SourceNodeID: "node-b"},
			want:    true,
		},
		{
			name:    "ignore lower version",
			current: &store.HAEntityVersion{Version: 3, UpdatedAt: now, UpdatedByNodeID: "node-a"},
			op:      &store.HASyncOp{EntityVersion: 2, OccurredAt: now.Add(time.Second), SourceNodeID: "node-b"},
			want:    false,
		},
		{
			name:    "apply newer timestamp on same version",
			current: &store.HAEntityVersion{Version: 3, UpdatedAt: now, UpdatedByNodeID: "node-a"},
			op:      &store.HASyncOp{EntityVersion: 3, OccurredAt: now.Add(time.Second), SourceNodeID: "node-b"},
			want:    true,
		},
		{
			name:    "ignore older timestamp on same version",
			current: &store.HAEntityVersion{Version: 3, UpdatedAt: now, UpdatedByNodeID: "node-a"},
			op:      &store.HASyncOp{EntityVersion: 3, OccurredAt: now.Add(-time.Second), SourceNodeID: "node-b"},
			want:    false,
		},
		{
			name:    "use node id tie breaker when timestamp matches",
			current: &store.HAEntityVersion{Version: 3, UpdatedAt: now, UpdatedByNodeID: "node-a"},
			op:      &store.HASyncOp{EntityVersion: 3, OccurredAt: now, SourceNodeID: "node-z"},
			want:    true,
		},
		{
			name:    "keep current when tie breaker loses",
			current: &store.HAEntityVersion{Version: 3, UpdatedAt: now, UpdatedByNodeID: "node-z"},
			op:      &store.HASyncOp{EntityVersion: 3, OccurredAt: now, SourceNodeID: "node-a"},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldApplyRemoteVersion(tt.current, tt.op); got != tt.want {
				t.Fatalf("shouldApplyRemoteVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}
