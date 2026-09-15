package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type llmMonitorTestMailer struct {
	sent    []llmMonitorSentMail
	sendErr error
}

type llmMonitorSentMail struct {
	to      []string
	subject string
	body    string
}

func (m *llmMonitorTestMailer) Send(_ context.Context, to []string, subject string, body string) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sent = append(m.sent, llmMonitorSentMail{to: to, subject: subject, body: body})
	return nil
}

func (m *llmMonitorTestMailer) SendHubRegistrationConfirmation(_ context.Context, _ string, _ string, _ string) error {
	return nil
}

func TestLoadLLMProviderMonitorConfigDefaults(t *testing.T) {
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	cfg := loadLLMProviderMonitorConfig(t.Context(), svc)
	if cfg.Enabled || cfg.IntervalHours != defaultLLMProviderMonitorIntervalHours {
		t.Fatalf("default config = %+v", cfg)
	}
}

func TestLoadLLMProviderMonitorConfigNormalizesInterval(t *testing.T) {
	settings := &llmDeleteTestSettings{data: map[string]string{
		llmProviderMonitorConfigKey: `{"enabled":true,"interval_hours":0}`,
	}}
	cfg := loadLLMProviderMonitorConfig(t.Context(), llmservice.NewService(settings))
	if !cfg.Enabled || cfg.IntervalHours != defaultLLMProviderMonitorIntervalHours {
		t.Fatalf("normalized config = %+v", cfg)
	}
	settings.data[llmProviderMonitorConfigKey] = `{"enabled":true,"interval_hours":6}`
	cfg = loadLLMProviderMonitorConfig(t.Context(), llmservice.NewService(settings))
	if !cfg.Enabled || cfg.IntervalHours != 6 {
		t.Fatalf("stored config = %+v", cfg)
	}
}

func TestAdminSaveLLMProviderMonitorConfigSendsTestEmailOnEnable(t *testing.T) {
	settings := &llmDeleteTestSettings{data: map[string]string{}}
	svc := llmservice.NewService(settings)
	mailer := &llmMonitorTestMailer{}
	SetLLMProviderMonitorMailer(mailer)
	defer SetLLMProviderMonitorMailer(nil)

	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/provider-monitor/config", strings.NewReader(`{"enabled":true,"interval_hours":4}`))
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{Email: "admin@example.com"}))
	rec := httptest.NewRecorder()
	adminSaveLLMProviderMonitorConfig(svc)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("test emails sent = %d, want 1", len(mailer.sent))
	}
	if got := mailer.sent[0].to; len(got) != 1 || got[0] != "admin@example.com" {
		t.Fatalf("test email recipient = %v", got)
	}
	var stored llmProviderMonitorConfig
	if err := json.Unmarshal([]byte(settings.data[llmProviderMonitorConfigKey]), &stored); err != nil {
		t.Fatalf("stored config: %v", err)
	}
	if !stored.Enabled || stored.IntervalHours != 4 {
		t.Fatalf("stored config = %+v", stored)
	}
	if stored.NotifyEmail != "admin@example.com" {
		t.Fatalf("stored notify_email = %q, want admin@example.com", stored.NotifyEmail)
	}
}

func TestAdminSaveLLMProviderMonitorConfigKeepsDisabledWhenTestEmailFails(t *testing.T) {
	settings := &llmDeleteTestSettings{data: map[string]string{}}
	svc := llmservice.NewService(settings)
	mailer := &llmMonitorTestMailer{sendErr: errors.New("mail delivery is not configured")}
	SetLLMProviderMonitorMailer(mailer)
	defer SetLLMProviderMonitorMailer(nil)

	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/provider-monitor/config", strings.NewReader(`{"enabled":true,"interval_hours":3}`))
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{Email: "admin@example.com"}))
	rec := httptest.NewRecorder()
	adminSaveLLMProviderMonitorConfig(svc)(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(got.Error, "mail delivery is not configured") {
		t.Fatalf("error = %q", got.Error)
	}
	if raw := settings.data[llmProviderMonitorConfigKey]; raw != "" {
		t.Fatalf("config must not be persisted when the test email fails, got %s", raw)
	}
}

func TestAdminSaveLLMProviderMonitorConfigSkipsTestEmailWhenAlreadyEnabled(t *testing.T) {
	settings := &llmDeleteTestSettings{data: map[string]string{
		llmProviderMonitorConfigKey: `{"enabled":true,"interval_hours":3}`,
	}}
	svc := llmservice.NewService(settings)
	mailer := &llmMonitorTestMailer{}
	SetLLMProviderMonitorMailer(mailer)
	defer SetLLMProviderMonitorMailer(nil)

	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/provider-monitor/config", strings.NewReader(`{"enabled":true,"interval_hours":8}`))
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{Email: "admin@example.com"}))
	rec := httptest.NewRecorder()
	adminSaveLLMProviderMonitorConfig(svc)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(mailer.sent) != 0 {
		t.Fatalf("test emails sent = %d, want 0", len(mailer.sent))
	}
}

func TestRunLLMProviderMonitorCycleReportsOnlyFailedProviders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "broken") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"error":{"message":"model overloaded"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	}))
	defer upstream.Close()

	settings := &llmDeleteTestSettings{data: map[string]string{
		"admin_email": `{"value":"admin@example.com"}`,
	}}
	svc := llmservice.NewService(settings)
	ctx := t.Context()
	for _, provider := range []llmpool.ProviderConfig{
		{ID: "healthy", Name: "Healthy", APIURL: upstream.URL + "/v1", Models: []string{"model-a"}},
		{ID: "broken", Name: "Broken", APIURL: upstream.URL + "/broken/v1", Models: []string{"model-b"}},
		{ID: "paused-broken", Name: "Paused", APIURL: upstream.URL + "/broken/v1", Models: []string{"model-c"}, Paused: true},
		{ID: "no-models", Name: "NoModels", APIURL: upstream.URL + "/broken/v1"},
	} {
		if err := svc.AddProvider(ctx, provider); err != nil {
			t.Fatalf("add provider %s: %v", provider.ID, err)
		}
	}

	mailer := &llmMonitorTestMailer{}
	runLLMProviderMonitorCycle(ctx, svc, mailer, nil)

	if len(mailer.sent) != 1 {
		t.Fatalf("notification emails sent = %d, want 1", len(mailer.sent))
	}
	mail := mailer.sent[0]
	if len(mail.to) != 1 || mail.to[0] != "admin@example.com" {
		t.Fatalf("notification recipient = %v", mail.to)
	}
	if !strings.Contains(mail.body, "broken") || !strings.Contains(mail.body, "model overloaded") || !strings.Contains(mail.body, "model-b") {
		t.Fatalf("failure report missing the failed provider details:\n%s", mail.body)
	}
	for _, absent := range []string{"healthy", "paused-broken", "no-models"} {
		if strings.Contains(mail.body, absent) {
			t.Fatalf("failure report must not mention %q:\n%s", absent, mail.body)
		}
	}
}

func TestLoadLLMProviderMonitorConfigClampsExcessiveInterval(t *testing.T) {
	settings := &llmDeleteTestSettings{data: map[string]string{
		llmProviderMonitorConfigKey: `{"enabled":true,"interval_hours":100000}`,
	}}
	cfg := loadLLMProviderMonitorConfig(t.Context(), llmservice.NewService(settings))
	if cfg.IntervalHours != defaultLLMProviderMonitorIntervalHours {
		t.Fatalf("clamped config = %+v", cfg)
	}
}

func TestAdminSaveLLMProviderMonitorConfigRejectsEnableWithoutAnyAdminEmail(t *testing.T) {
	settings := &llmDeleteTestSettings{data: map[string]string{}}
	svc := llmservice.NewService(settings)
	mailer := &llmMonitorTestMailer{}
	SetLLMProviderMonitorMailer(mailer)
	defer SetLLMProviderMonitorMailer(nil)

	// No admin_email system setting and no admin in the request context.
	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/provider-monitor/config", strings.NewReader(`{"enabled":true,"interval_hours":3}`))
	rec := httptest.NewRecorder()
	adminSaveLLMProviderMonitorConfig(svc)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(mailer.sent) != 0 {
		t.Fatalf("test emails sent = %d, want 0", len(mailer.sent))
	}
	if raw := settings.data[llmProviderMonitorConfigKey]; raw != "" {
		t.Fatalf("config must not be persisted without an admin email, got %s", raw)
	}
}

func TestAdminSaveLLMProviderMonitorConfigPrefersSystemAdminEmail(t *testing.T) {
	settings := &llmDeleteTestSettings{data: map[string]string{
		"admin_email": `{"value":"system-admin@example.com"}`,
	}}
	svc := llmservice.NewService(settings)
	mailer := &llmMonitorTestMailer{}
	SetLLMProviderMonitorMailer(mailer)
	defer SetLLMProviderMonitorMailer(nil)

	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/provider-monitor/config", strings.NewReader(`{"enabled":true,"interval_hours":3}`))
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{Email: "session@example.com"}))
	rec := httptest.NewRecorder()
	adminSaveLLMProviderMonitorConfig(svc)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(mailer.sent) != 1 || len(mailer.sent[0].to) != 1 || mailer.sent[0].to[0] != "system-admin@example.com" {
		t.Fatalf("test email recipient = %+v, want system-admin@example.com", mailer.sent)
	}
	var stored llmProviderMonitorConfig
	if err := json.Unmarshal([]byte(settings.data[llmProviderMonitorConfigKey]), &stored); err != nil {
		t.Fatalf("stored config: %v", err)
	}
	if stored.NotifyEmail != "system-admin@example.com" {
		t.Fatalf("stored notify_email = %q, want system-admin@example.com", stored.NotifyEmail)
	}
}

// llmMonitorRegistryFailSettings fails only the registry read, so the monitor
// cycle hits its registry-error path while other settings (admin_email, the
// monitor config itself) remain readable.
type llmMonitorRegistryFailSettings struct {
	data map[string]string
}

func (s *llmMonitorRegistryFailSettings) Get(_ context.Context, key string) (string, error) {
	if key == llmservice.RegistrySettingKey {
		return "", errors.New("registry store unavailable")
	}
	return s.data[key], nil
}

func (s *llmMonitorRegistryFailSettings) Set(_ context.Context, key, value string) error {
	if s.data == nil {
		s.data = map[string]string{}
	}
	s.data[key] = value
	return nil
}

func (s *llmMonitorRegistryFailSettings) List(_ context.Context) ([]*store.SystemSettingEntry, error) {
	return nil, nil
}

func TestRunLLMProviderMonitorCycleAlertsOnRegistryError(t *testing.T) {
	settings := &llmMonitorRegistryFailSettings{data: map[string]string{
		"admin_email": `{"value":"admin@example.com"}`,
	}}
	svc := llmservice.NewService(settings)
	mailer := &llmMonitorTestMailer{}

	runLLMProviderMonitorCycle(t.Context(), svc, mailer, nil)

	if len(mailer.sent) != 1 {
		t.Fatalf("alert emails sent = %d, want 1", len(mailer.sent))
	}
	mail := mailer.sent[0]
	if len(mail.to) != 1 || mail.to[0] != "admin@example.com" {
		t.Fatalf("alert recipient = %v", mail.to)
	}
	if !strings.Contains(mail.subject, "Provider monitor error") || !strings.Contains(mail.body, "registry store unavailable") {
		t.Fatalf("alert = %q / %q", mail.subject, mail.body)
	}
}

func TestRunLLMProviderMonitorCycleRegistryErrorWithoutRecipientSkips(t *testing.T) {
	settings := &llmMonitorRegistryFailSettings{data: map[string]string{}}
	svc := llmservice.NewService(settings)
	mailer := &llmMonitorTestMailer{}

	// Must log-and-skip without panicking.
	runLLMProviderMonitorCycle(t.Context(), svc, mailer, nil)

	if len(mailer.sent) != 0 {
		t.Fatalf("alert emails sent = %d, want 0", len(mailer.sent))
	}
}

func TestRunLLMProviderMonitorCycleRecipientPriority(t *testing.T) {
	newSvc := func(data map[string]string) *llmservice.Service {
		return llmservice.NewService(&llmMonitorRegistryFailSettings{data: data})
	}
	t.Run("live admin email wins over captured notify email", func(t *testing.T) {
		svc := newSvc(map[string]string{
			"admin_email":               `{"value":"admin@example.com"}`,
			llmProviderMonitorConfigKey: `{"enabled":true,"interval_hours":3,"notify_email":"captured@example.com"}`,
		})
		mailer := &llmMonitorTestMailer{}
		runLLMProviderMonitorCycle(t.Context(), svc, mailer, nil)
		if len(mailer.sent) != 1 || len(mailer.sent[0].to) != 1 || mailer.sent[0].to[0] != "admin@example.com" {
			t.Fatalf("alert recipient = %+v, want admin@example.com", mailer.sent)
		}
	})
	t.Run("captured notify email covers a cleared admin email", func(t *testing.T) {
		svc := newSvc(map[string]string{
			llmProviderMonitorConfigKey: `{"enabled":true,"interval_hours":3,"notify_email":"captured@example.com"}`,
		})
		mailer := &llmMonitorTestMailer{}
		runLLMProviderMonitorCycle(t.Context(), svc, mailer, nil)
		if len(mailer.sent) != 1 || len(mailer.sent[0].to) != 1 || mailer.sent[0].to[0] != "captured@example.com" {
			t.Fatalf("alert recipient = %+v, want captured@example.com", mailer.sent)
		}
	})
}

func TestClaimLLMProviderMonitorLease(t *testing.T) {
	readLease := func(t *testing.T, settings *llmDeleteTestSettings) llmProviderMonitorLease {
		t.Helper()
		var lease llmProviderMonitorLease
		if err := json.Unmarshal([]byte(settings.data[llmProviderMonitorLeaseKey]), &lease); err != nil {
			t.Fatalf("stored lease: %v", err)
		}
		return lease
	}
	now := time.Now()
	ttl := llmProviderMonitorLeaseTTL

	t.Run("fresh claim", func(t *testing.T) {
		settings := &llmDeleteTestSettings{data: map[string]string{}}
		svc := llmservice.NewService(settings)
		if _, ok := claimLLMProviderMonitorLease(t.Context(), svc, "node-a", ttl, now); !ok {
			t.Fatal("fresh claim failed")
		}
		lease := readLease(t, settings)
		if lease.Owner != "node-a" || lease.Until != now.Add(ttl).Unix() {
			t.Fatalf("lease = %+v", lease)
		}
	})

	t.Run("owner renews and keeps last_run", func(t *testing.T) {
		lastRun := now.Add(-time.Hour).Unix()
		settings := &llmDeleteTestSettings{data: map[string]string{
			llmProviderMonitorLeaseKey: fmt.Sprintf(`{"owner":"node-a","until":%d,"last_run":%d}`, now.Add(time.Minute).Unix(), lastRun),
		}}
		svc := llmservice.NewService(settings)
		later := now.Add(2 * time.Minute)
		if _, ok := claimLLMProviderMonitorLease(t.Context(), svc, "node-a", ttl, later); !ok {
			t.Fatal("owner renew failed")
		}
		lease := readLease(t, settings)
		if lease.Until != later.Add(ttl).Unix() || lease.LastRun != lastRun {
			t.Fatalf("lease not renewed correctly: %+v", lease)
		}
	})

	t.Run("non-owner skipped while lease is live", func(t *testing.T) {
		held := fmt.Sprintf(`{"owner":"node-a","until":%d}`, now.Add(time.Minute).Unix())
		settings := &llmDeleteTestSettings{data: map[string]string{llmProviderMonitorLeaseKey: held}}
		svc := llmservice.NewService(settings)
		if _, ok := claimLLMProviderMonitorLease(t.Context(), svc, "node-b", ttl, now); ok {
			t.Fatal("non-owner claimed a live lease")
		}
		if raw := settings.data[llmProviderMonitorLeaseKey]; raw != held {
			t.Fatalf("lease was overwritten: %s", raw)
		}
	})

	t.Run("expired lease takeover returns prior lease", func(t *testing.T) {
		lastRun := now.Add(-30 * time.Minute).Unix()
		settings := &llmDeleteTestSettings{data: map[string]string{
			llmProviderMonitorLeaseKey: fmt.Sprintf(`{"owner":"node-a","until":%d,"last_run":%d}`, now.Add(-time.Minute).Unix(), lastRun),
		}}
		svc := llmservice.NewService(settings)
		prior, ok := claimLLMProviderMonitorLease(t.Context(), svc, "node-b", ttl, now)
		if !ok {
			t.Fatal("takeover of expired lease failed")
		}
		if prior.Owner != "node-a" || prior.LastRun != lastRun {
			t.Fatalf("prior lease = %+v", prior)
		}
		if lease := readLease(t, settings); lease.Owner != "node-b" || lease.LastRun != lastRun {
			t.Fatalf("lease = %+v", lease)
		}
	})

	t.Run("empty node id never claims", func(t *testing.T) {
		settings := &llmDeleteTestSettings{data: map[string]string{}}
		svc := llmservice.NewService(settings)
		if _, ok := claimLLMProviderMonitorLease(t.Context(), svc, "  ", ttl, now); ok {
			t.Fatal("empty node id claimed the lease")
		}
		if raw := settings.data[llmProviderMonitorLeaseKey]; raw != "" {
			t.Fatalf("lease written for empty node id: %s", raw)
		}
	})
}

func TestLLMProviderMonitorTickElectsSingleRunner(t *testing.T) {
	// Two monitor instances tick against one shared settings store; only the
	// lease holder runs the (registry-error) cycle and sends the alert.
	settings := &llmMonitorRegistryFailSettings{data: map[string]string{
		"admin_email":               `{"value":"admin@example.com"}`,
		llmProviderMonitorConfigKey: `{"enabled":true,"interval_hours":3}`,
	}}
	svcA := llmservice.NewService(settings)
	svcB := llmservice.NewService(settings)
	mailerA := &llmMonitorTestMailer{}
	mailerB := &llmMonitorTestMailer{}
	now := time.Now()
	ttl := llmProviderMonitorLeaseTTL
	// Both nodes have a cycle due.
	stateA := &llmProviderMonitorState{lastRun: now.Add(-4 * time.Hour)}
	stateB := &llmProviderMonitorState{lastRun: now.Add(-4 * time.Hour)}

	llmProviderMonitorTick(t.Context(), svcA, mailerA, "node-a", ttl, stateA, now, nil)
	llmProviderMonitorTick(t.Context(), svcB, mailerB, "node-b", ttl, stateB, now, nil)

	if len(mailerA.sent) != 1 {
		t.Fatalf("holder alerts = %d, want 1", len(mailerA.sent))
	}
	if len(mailerB.sent) != 0 {
		t.Fatalf("non-holder alerts = %d, want 0", len(mailerB.sent))
	}
	if !stateA.lastRun.Equal(now) {
		t.Fatalf("holder lastRun = %s, want %s", stateA.lastRun, now)
	}
	if !stateB.lastRun.Equal(now.Add(-4 * time.Hour)) {
		t.Fatalf("non-holder lastRun was touched: %s", stateB.lastRun)
	}
	// The holder recorded the cycle time in the lease for takeover continuity.
	var lease llmProviderMonitorLease
	if err := json.Unmarshal([]byte(settings.data[llmProviderMonitorLeaseKey]), &lease); err != nil {
		t.Fatalf("stored lease: %v", err)
	}
	if lease.Owner != "node-a" || lease.LastRun != now.Unix() {
		t.Fatalf("lease = %+v", lease)
	}
}

func TestLLMProviderMonitorTickTakeoverResumesHolderSchedule(t *testing.T) {
	newSettings := func(lease string) *llmMonitorRegistryFailSettings {
		return &llmMonitorRegistryFailSettings{data: map[string]string{
			"admin_email":               `{"value":"admin@example.com"}`,
			llmProviderMonitorConfigKey: `{"enabled":true,"interval_hours":3}`,
			llmProviderMonitorLeaseKey:  lease,
		}}
	}
	now := time.Now()
	ttl := llmProviderMonitorLeaseTTL

	t.Run("recent last_run suppresses the takeover burst", func(t *testing.T) {
		dead := now.Add(-30 * time.Minute)
		settings := newSettings(fmt.Sprintf(`{"owner":"node-a","until":%d,"last_run":%d}`, now.Add(-time.Minute).Unix(), dead.Unix()))
		svc := llmservice.NewService(settings)
		mailer := &llmMonitorTestMailer{}
		state := &llmProviderMonitorState{lastRun: now}

		llmProviderMonitorTick(t.Context(), svc, mailer, "node-b", ttl, state, now, nil)

		if len(mailer.sent) != 0 {
			t.Fatalf("takeover burst: %d alerts, want 0", len(mailer.sent))
		}
		if !state.lastRun.Equal(dead.Truncate(time.Second)) {
			t.Fatalf("lastRun = %s, want resumed %s", state.lastRun, dead)
		}
	})

	t.Run("missing last_run runs immediately", func(t *testing.T) {
		settings := newSettings(fmt.Sprintf(`{"owner":"node-a","until":%d}`, now.Add(-time.Minute).Unix()))
		svc := llmservice.NewService(settings)
		mailer := &llmMonitorTestMailer{}
		state := &llmProviderMonitorState{lastRun: now}

		llmProviderMonitorTick(t.Context(), svc, mailer, "node-b", ttl, state, now, nil)

		if len(mailer.sent) != 1 {
			t.Fatalf("alerts = %d, want 1", len(mailer.sent))
		}
		if !state.lastRun.Equal(now) {
			t.Fatalf("lastRun = %s, want %s", state.lastRun, now)
		}
	})
}

