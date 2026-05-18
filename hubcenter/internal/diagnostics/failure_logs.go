package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type FailureEventRecorder struct {
	repo store.FailureEventLogRepository
}

type FailureEventInput struct {
	TenantID  string
	Category  string
	EventCode string
	Message   string
	EntityID  string
	Email     string
	ClientIP  string
	Details   map[string]any
}

func NewFailureEventRecorder(repo store.FailureEventLogRepository) *FailureEventRecorder {
	return &FailureEventRecorder{repo: repo}
}

func (r *FailureEventRecorder) Record(ctx context.Context, input FailureEventInput) {
	if r == nil || r.repo == nil {
		return
	}
	detailsJSON := "{}"
	if len(input.Details) > 0 {
		if data, err := json.Marshal(input.Details); err == nil {
			detailsJSON = string(data)
		}
	}
	_ = r.repo.Create(ctx, &store.FailureEventLog{
		ID:          fmt.Sprintf("fl_%d", time.Now().UnixNano()),
		TenantID:    strings.TrimSpace(input.TenantID),
		Category:    strings.TrimSpace(input.Category),
		EventCode:   strings.TrimSpace(input.EventCode),
		Message:     strings.TrimSpace(input.Message),
		EntityID:    strings.TrimSpace(input.EntityID),
		Email:       strings.TrimSpace(strings.ToLower(input.Email)),
		ClientIP:    strings.TrimSpace(input.ClientIP),
		DetailsJSON: detailsJSON,
		CreatedAt:   time.Now().UTC(),
	})
}
