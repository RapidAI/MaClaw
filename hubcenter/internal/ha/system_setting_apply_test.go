package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type fakeSystemSettings struct {
	mu      sync.Mutex
	data    map[string]string
	failSet bool
}

func (s *fakeSystemSettings) Set(_ context.Context, key, valueJSON string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failSet {
		return fmt.Errorf("disk full")
	}
	if s.data == nil {
		s.data = map[string]string{}
	}
	s.data[key] = valueJSON
	return nil
}

func (s *fakeSystemSettings) Get(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[key], nil
}

func (s *fakeSystemSettings) List(_ context.Context) ([]*store.SystemSettingEntry, error) {
	return nil, nil
}

func TestApplySystemSettingOpInvalidatesLLMRegistryCache(t *testing.T) {
	settings := &fakeSystemSettings{data: map[string]string{}}
	invalidated := 0
	svc := &Service{
		settings: settings,
		llmRegistryCacheInvalidator: func() {
			invalidated++
		},
	}
	payload, err := json.Marshal(systemSettingPayload{
		Key:       llmservice.RegistrySettingKey,
		ValueJSON: `{"providers":[]}`,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := svc.applySystemSettingOp(context.Background(), &store.HASyncOp{
		OpType:      OpUpsert,
		PayloadJSON: string(payload),
	}); err != nil {
		t.Fatalf("applySystemSettingOp: %v", err)
	}
	if invalidated != 1 {
		t.Fatalf("invalidations = %d, want 1", invalidated)
	}
	if settings.data[llmservice.RegistrySettingKey] != `{"providers":[]}` {
		t.Fatalf("stored value = %q", settings.data[llmservice.RegistrySettingKey])
	}
}

func TestApplySystemSettingOpDoesNotInvalidateOtherKeys(t *testing.T) {
	settings := &fakeSystemSettings{data: map[string]string{}}
	invalidated := 0
	svc := &Service{
		settings: settings,
		llmRegistryCacheInvalidator: func() {
			invalidated++
		},
	}
	payload, err := json.Marshal(systemSettingPayload{
		Key:       "llm_cardstore_payment_config",
		ValueJSON: `{}`,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := svc.applySystemSettingOp(context.Background(), &store.HASyncOp{
		OpType:      OpUpsert,
		PayloadJSON: string(payload),
	}); err != nil {
		t.Fatalf("applySystemSettingOp: %v", err)
	}
	if invalidated != 0 {
		t.Fatalf("invalidations = %d, want 0", invalidated)
	}
}

func TestApplySystemSettingOpDoesNotInvalidateOnSaveFailure(t *testing.T) {
	settings := &fakeSystemSettings{failSet: true}
	invalidated := 0
	svc := &Service{
		settings: settings,
		llmRegistryCacheInvalidator: func() {
			invalidated++
		},
	}
	payload, err := json.Marshal(systemSettingPayload{
		Key:       llmservice.RegistrySettingKey,
		ValueJSON: `{"providers":[]}`,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := svc.applySystemSettingOp(context.Background(), &store.HASyncOp{
		OpType:      OpUpsert,
		PayloadJSON: string(payload),
	}); err == nil {
		t.Fatal("expected save error")
	}
	if invalidated != 0 {
		t.Fatalf("invalidations = %d, want 0", invalidated)
	}
}

func TestApplySystemSettingOpNilOpIsNoop(t *testing.T) {
	svc := &Service{settings: &fakeSystemSettings{data: map[string]string{}}}
	if err := svc.applySystemSettingOp(context.Background(), nil); err != nil {
		t.Fatalf("nil op: %v", err)
	}
}

func TestApplySystemSettingOpAcksOfficialClassHead(t *testing.T) {
	settings := &fakeSystemSettings{data: map[string]string{}}
	acked := 0
	svc := &Service{
		settings: settings,
		officialClassHeadAck: func(string) {
			acked++
		},
	}
	payload, err := json.Marshal(systemSettingPayload{
		Key:       llmservice.OfficialClassHeadKey,
		ValueJSON: `{"pipeline":"on"}`,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := svc.applySystemSettingOp(context.Background(), &store.HASyncOp{
		OpType:      OpUpsert,
		PayloadJSON: string(payload),
	}); err != nil {
		t.Fatalf("applySystemSettingOp: %v", err)
	}
	if acked != 1 {
		t.Fatalf("acks = %d, want 1", acked)
	}
}

func TestApplySystemSettingOpDoesNotAckOtherKeys(t *testing.T) {
	settings := &fakeSystemSettings{data: map[string]string{}}
	acked := 0
	svc := &Service{
		settings: settings,
		officialClassHeadAck: func(string) {
			acked++
		},
	}
	payload, err := json.Marshal(systemSettingPayload{
		Key:       llmservice.RegistrySettingKey,
		ValueJSON: `{"providers":[]}`,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := svc.applySystemSettingOp(context.Background(), &store.HASyncOp{
		OpType:      OpUpsert,
		PayloadJSON: string(payload),
	}); err != nil {
		t.Fatalf("applySystemSettingOp: %v", err)
	}
	if acked != 0 {
		t.Fatalf("acks = %d, want 0", acked)
	}
}

func TestApplySystemSettingOpDoesNotAckPerGroupClassHead(t *testing.T) {
	settings := &fakeSystemSettings{data: map[string]string{}}
	acked := 0
	svc := &Service{
		settings: settings,
		officialClassHeadAck: func(string) {
			acked++
		},
	}
	payload, err := json.Marshal(systemSettingPayload{
		Key:       "llm_class_head_v1:coding-auto",
		ValueJSON: `{"pipeline":"on"}`,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := svc.applySystemSettingOp(context.Background(), &store.HASyncOp{
		OpType:      OpUpsert,
		PayloadJSON: string(payload),
	}); err != nil {
		t.Fatalf("applySystemSettingOp: %v", err)
	}
	if acked != 0 {
		t.Fatalf("per-group class head must not ack official head, acks=%d", acked)
	}
}

func applyMonitorLeaseOp(t *testing.T, svc *Service, valueJSON string) {
	t.Helper()
	payload, err := json.Marshal(systemSettingPayload{Key: LLMProviderMonitorLeaseKey, ValueJSON: valueJSON})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := svc.applySystemSettingOp(context.Background(), &store.HASyncOp{OpType: OpUpsert, PayloadJSON: string(payload)}); err != nil {
		t.Fatalf("applySystemSettingOp: %v", err)
	}
}

func TestApplySystemSettingOpFencesStaleMonitorLease(t *testing.T) {
	fresh := `{"owner":"node-a","until":2000}`
	settings := &fakeSystemSettings{data: map[string]string{LLMProviderMonitorLeaseKey: fresh}}
	svc := &Service{settings: settings}

	applyMonitorLeaseOp(t, svc, `{"owner":"node-b","until":1000}`)

	if got := settings.data[LLMProviderMonitorLeaseKey]; got != fresh {
		t.Fatalf("stale lease op demoted the fresher local lease: %q", got)
	}
}

func TestApplySystemSettingOpAppliesNewerMonitorLease(t *testing.T) {
	settings := &fakeSystemSettings{data: map[string]string{LLMProviderMonitorLeaseKey: `{"owner":"node-a","until":2000}`}}
	svc := &Service{settings: settings}

	newer := `{"owner":"node-b","until":3000}`
	applyMonitorLeaseOp(t, svc, newer)

	if got := settings.data[LLMProviderMonitorLeaseKey]; got != newer {
		t.Fatalf("newer lease op not applied: %q", got)
	}
}

func TestApplySystemSettingOpAppliesMalformedMonitorLease(t *testing.T) {
	settings := &fakeSystemSettings{data: map[string]string{LLMProviderMonitorLeaseKey: `{"owner":"node-a","until":2000}`}}
	svc := &Service{settings: settings}

	// Malformed incoming payloads apply as usual so a corrupted lease never
	// blocks recovery.
	applyMonitorLeaseOp(t, svc, `{not json}`)

	if got := settings.data[LLMProviderMonitorLeaseKey]; got != `{not json}` {
		t.Fatalf("malformed lease op not applied: %q", got)
	}
}
