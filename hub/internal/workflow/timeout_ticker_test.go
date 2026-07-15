package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestTimeoutTickerStopIsIdempotent(t *testing.T) {
	ticker := NewTimeoutTicker(nil, nil)
	ticker.Stop()
	ticker.Stop()
}

func TestTimeoutTickerStartIsIdempotent(t *testing.T) {
	ticker := NewTimeoutTicker(nil, nil)
	ticker.Start()
	ticker.Start()
	ticker.Stop()
}

func TestApprovalExecutionTimeout(t *testing.T) {
	tests := []struct {
		name string
		data string
		want time.Duration
	}{
		{name: "configured timeout", data: `{"timeout_hours": 48}`, want: 48 * time.Hour},
		{name: "missing timeout", data: `{}`, want: defaultApprovalTimeout},
		{name: "invalid timeout", data: `{"timeout_hours": "48"}`, want: defaultApprovalTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := approvalExecutionTimeout(NodeExecution{Result: json.RawMessage(tt.data)}); got != tt.want {
				t.Fatalf("approvalExecutionTimeout() = %s, want %s", got, tt.want)
			}
		})
	}
}

type tenantCapturingInstanceStore struct {
	InstanceStore
	tenantID string
}

func (s *tenantCapturingInstanceStore) GetPendingApprovals(context.Context, string) ([]NodeExecution, error) {
	return []NodeExecution{{ID: "exec", InstanceID: "instance", NodeID: "approval", TenantID: "tenant_b", StartedAt: time.Now().Add(-25 * time.Hour)}}, nil
}

func (s *tenantCapturingInstanceStore) Get(ctx context.Context, id string) (*WorkflowInstance, error) {
	s.tenantID = store.TenantIDFromContext(ctx)
	return nil, errors.New("stop after recording tenant")
}

func TestTimeoutTickerUsesExecutionTenant(t *testing.T) {
	instanceStore := &tenantCapturingInstanceStore{}
	executor := NewWorkflowExecutor(nil, instanceStore, nil, nil)
	ticker := NewTimeoutTicker(executor, instanceStore)
	ticker.checkTimeouts()
	if instanceStore.tenantID != "tenant_b" {
		t.Fatalf("timeout tenant = %q, want tenant_b", instanceStore.tenantID)
	}
}
