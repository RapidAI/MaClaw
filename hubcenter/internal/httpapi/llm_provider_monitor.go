package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
)

// llmProviderMonitorConfigKey stores the provider monitor settings as raw JSON
// in system settings, following the payment-config persistence pattern.
const llmProviderMonitorConfigKey = "llm_provider_monitor_config"

const defaultLLMProviderMonitorIntervalHours = 3

// maxLLMProviderMonitorIntervalHours caps the interval so
// time.Duration(hours)*time.Hour cannot overflow into a negative duration
// (which would make the monitor tick every minute).
const maxLLMProviderMonitorIntervalHours = 24 * 30

type llmProviderMonitorConfig struct {
	Enabled       bool `json:"enabled"`
	IntervalHours int  `json:"interval_hours"`
	// NotifyEmail is the alert recipient captured when monitoring was enabled
	// (the admin_email system setting at that time, or the enabling admin's
	// session email as a fallback). The cycle falls back to the current
	// admin_email setting when this is empty.
	NotifyEmail string `json:"notify_email,omitempty"`
}

func normalizeLLMProviderMonitorConfig(cfg llmProviderMonitorConfig) llmProviderMonitorConfig {
	if cfg.IntervalHours < 1 || cfg.IntervalHours > maxLLMProviderMonitorIntervalHours {
		cfg.IntervalHours = defaultLLMProviderMonitorIntervalHours
	}
	cfg.NotifyEmail = strings.TrimSpace(cfg.NotifyEmail)
	return cfg
}

func loadLLMProviderMonitorConfig(ctx context.Context, svc *llmservice.Service) llmProviderMonitorConfig {
	cfg := normalizeLLMProviderMonitorConfig(llmProviderMonitorConfig{})
	if svc == nil {
		return cfg
	}
	raw, err := svc.GetSystemSetting(ctx, llmProviderMonitorConfigKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return cfg
	}
	_ = json.Unmarshal([]byte(raw), &cfg)
	return normalizeLLMProviderMonitorConfig(cfg)
}

// The monitor's mailer is a process-wide dependency: LLM routes are registered
// through a hook that does not carry the mailer, so bootstrap installs it here
// before the router is built (same registry style as the LLM route hook).
var (
	llmProviderMonitorMailerMu sync.RWMutex
	llmProviderMonitorMailer   mail.Mailer
)

// SetLLMProviderMonitorMailer installs the mailer used by the provider monitor
// config handlers. Call this during bootstrap, before NewRouter.
func SetLLMProviderMonitorMailer(mailer mail.Mailer) {
	llmProviderMonitorMailerMu.Lock()
	defer llmProviderMonitorMailerMu.Unlock()
	llmProviderMonitorMailer = mailer
}

func currentLLMProviderMonitorMailer() mail.Mailer {
	llmProviderMonitorMailerMu.RLock()
	defer llmProviderMonitorMailerMu.RUnlock()
	return llmProviderMonitorMailer
}

func adminGetLLMProviderMonitorConfig(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSONResp(w, http.StatusOK, loadLLMProviderMonitorConfig(r.Context(), svc))
	}
}

func adminSaveLLMProviderMonitorConfig(svc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req llmProviderMonitorConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		req = normalizeLLMProviderMonitorConfig(req)
		previous := loadLLMProviderMonitorConfig(r.Context(), svc)
		// The frontend only sends enabled/interval_hours; keep the recipient
		// captured at enable time unless the request carries a new one.
		// notify_email otherwise changes only through the enable path or an
		// explicit admin API call.
		if req.NotifyEmail == "" {
			req.NotifyEmail = previous.NotifyEmail
		}
		if req.Enabled && !previous.Enabled {
			// Enabling the monitor is a commitment to email the admin on
			// failures. Prove the notification path with a test email first;
			// if it cannot be delivered, keep monitoring disabled so the admin
			// is not silently unprotected. Resolve the recipient the same way
			// the background monitor does (the admin_email system setting,
			// falling back to the session email) and persist it as
			// notify_email: the fallback recipient the cycle uses if the
			// admin_email setting is ever cleared afterwards.
			adminEmail := adminEmailFromSystemSettings(r.Context(), svc)
			if adminEmail == "" {
				adminEmail = strings.TrimSpace(adminEmailFromRequest(r))
			}
			if adminEmail == "" {
				writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": "no admin email is configured; set an admin email before enabling provider monitoring"})
				return
			}
			mailer := currentLLMProviderMonitorMailer()
			if mailer == nil {
				writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": "mail service is unavailable; cannot send the monitor test email"})
				return
			}
			subject := "[HubCenter] 服务商监控已开启 / Provider monitor enabled"
			body := fmt.Sprintf(
				"您好，\r\n\r\nHubCenter 服务商监控已开启。系统将每 %d 小时检测一次所有未暂停的服务商状态，发现故障时会发送邮件通知到本邮箱。\r\n\r\nHello,\r\n\r\nHubCenter provider monitoring is now enabled. All unpaused providers are tested every %d hour(s); failure reports are sent to this address.\r\n",
				req.IntervalHours,
				req.IntervalHours,
			)
			if err := mailer.Send(r.Context(), []string{adminEmail}, subject, body); err != nil {
				writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": "send monitor test email failed: " + err.Error()})
				return
			}
			req.NotifyEmail = adminEmail
		}
		raw, err := json.Marshal(req)
		if err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if err := svc.SetSystemSetting(r.Context(), llmProviderMonitorConfigKey, string(raw)); err != nil {
			writeJSONResp(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ---------------------------------------------------------------------------
// Background monitor
// ---------------------------------------------------------------------------

// llmProviderMonitorFailure describes one provider that failed its
// availability test during a monitor cycle.
type llmProviderMonitorFailure struct {
	ProviderID string
	Name       string
	Model      string
	Error      string
	LatencyMs  int64
}

// RunLLMProviderMonitor periodically tests every non-paused provider and
// emails the admin when one or more providers fail. The config is re-read on
// every tick, so interval/enabled changes apply without a restart.
//
// nodeID identifies this process in the monitor lease. Every node in an HA
// deployment runs this loop, but only the lease holder executes due check
// cycles and sends alerts (see claimLLMProviderMonitorLease). Non-holder nodes
// keep ticking so they take over within one lease TTL after the holder dies.
// leaseTTL must comfortably exceed the HA replication lag of system settings;
// bootstrap derives it from the sync intervals.
func RunLLMProviderMonitor(ctx context.Context, svc *llmservice.Service, mailer mail.Mailer, nodeID string, leaseTTL time.Duration) {
	if svc == nil || mailer == nil {
		log.Printf("[llm-monitor] provider monitor not started: svc=%t mailer=%t", svc != nil, mailer != nil)
		return
	}
	// lastRun starts at process start: when monitoring is already enabled at
	// startup, the first check runs after one full interval rather than
	// immediately, avoiding a provider-test burst on every boot.
	state := &llmProviderMonitorState{lastRun: time.Now()}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		llmProviderMonitorTick(ctx, svc, mailer, nodeID, leaseTTL, state, time.Now())
	}
}

// llmProviderMonitorState carries the monitor loop's in-memory schedule.
type llmProviderMonitorState struct {
	lastRun time.Time
}

// llmProviderMonitorTick executes one monitor tick: reload config, elect the
// single runner via the lease, and run a check cycle when one is due.
func llmProviderMonitorTick(ctx context.Context, svc *llmservice.Service, mailer mail.Mailer, nodeID string, leaseTTL time.Duration, state *llmProviderMonitorState, now time.Time) {
	cfg := loadLLMProviderMonitorConfig(ctx, svc)
	if !cfg.Enabled {
		// Restart the interval when monitoring is (re)enabled, so an
		// enable does not trigger an immediate catch-up run.
		state.lastRun = now
		return
	}
	prior, ok := claimLLMProviderMonitorLease(ctx, svc, nodeID, leaseTTL, now)
	if !ok {
		// Another node holds the lease; it runs the cycles. lastRun is
		// intentionally not touched: on takeover the schedule resumes from
		// the lease's last_run instead of this node's idle time.
		return
	}
	if prior.Owner != "" && prior.Owner != nodeID {
		// Took over from another (presumably dead) holder. Resume its
		// schedule so a holder that died right after a cycle does not cause
		// an immediate duplicate run; without a recorded last_run, run now.
		if prior.LastRun > 0 {
			state.lastRun = time.Unix(prior.LastRun, 0)
		} else {
			state.lastRun = time.Time{}
		}
	}
	if now.Sub(state.lastRun) < time.Duration(cfg.IntervalHours)*time.Hour {
		return
	}
	state.lastRun = now
	runLLMProviderMonitorCycle(ctx, svc, mailer)
	recordLLMProviderMonitorCycle(ctx, svc, nodeID, leaseTTL, now)
}

// llmProviderMonitorLeaseKey stores the single-runner lease in system
// settings. System setting writes are replicated to HA peers
// (haSystemSettings.Set), so every node observes the same lease. The constant
// lives in the ha package because the HA apply path fences stale replicated
// writes to this key and ha cannot import httpapi.
const llmProviderMonitorLeaseKey = ha.LLMProviderMonitorLeaseKey

// llmProviderMonitorLeaseTTL is the non-HA lease TTL: ticks are 1 minute, so
// local failover bookkeeping never waits long. HA deployments pass a larger
// TTL derived from the replication intervals (see bootstrap).
const llmProviderMonitorLeaseTTL = 3 * time.Minute

type llmProviderMonitorLease struct {
	Owner string `json:"owner"`
	Until int64  `json:"until"`
	// LastRun is when the holder last executed a check cycle; a takeover node
	// resumes the schedule from it instead of bursting a duplicate cycle.
	LastRun int64 `json:"last_run,omitempty"`
}

// claimLLMProviderMonitorLease reports whether nodeID holds the monitor lease
// after this attempt: it claims a missing or expired lease and renews its own
// (every tick, so the lease stays fresh while the holder is alive). It returns
// the lease as it was before any overwrite, so the caller can resume a dead
// holder's schedule from last_run. The settings store has no compare-and-swap,
// so this is a best-effort read-then-write — acceptable at a 1-minute tick
// cadence, where the worst case of a lost race is one duplicate cycle and
// alert email.
func claimLLMProviderMonitorLease(ctx context.Context, svc *llmservice.Service, nodeID string, ttl time.Duration, now time.Time) (llmProviderMonitorLease, bool) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return llmProviderMonitorLease{}, false
	}
	raw, err := svc.GetSystemSetting(ctx, llmProviderMonitorLeaseKey)
	if err != nil {
		llmMonitorLeaseReadLog.logf("[llm-monitor] read monitor lease failed: %v", err)
		return llmProviderMonitorLease{}, false
	}
	llmMonitorLeaseReadLog.reset()
	var prior llmProviderMonitorLease
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &prior)
	}
	if prior.Owner != nodeID && prior.Until > now.Unix() {
		// A live peer holds the lease.
		return prior, false
	}
	next := llmProviderMonitorLease{Owner: nodeID, Until: now.Add(ttl).Unix(), LastRun: prior.LastRun}
	data, err := json.Marshal(next)
	if err != nil {
		return prior, false
	}
	if err := svc.SetSystemSetting(ctx, llmProviderMonitorLeaseKey, string(data)); err != nil {
		llmMonitorLeaseWriteLog.logf("[llm-monitor] write monitor lease failed: %v", err)
		return prior, false
	}
	llmMonitorLeaseWriteLog.reset()
	return prior, true
}

// recordLLMProviderMonitorCycle folds the cycle time into the lease (and
// renews it) so a takeover node resumes the schedule instead of immediately
// re-running. Best-effort: if the lease was lost mid-cycle, skip the write.
func recordLLMProviderMonitorCycle(ctx context.Context, svc *llmservice.Service, nodeID string, ttl time.Duration, now time.Time) {
	raw, err := svc.GetSystemSetting(ctx, llmProviderMonitorLeaseKey)
	if err != nil {
		return
	}
	var lease llmProviderMonitorLease
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &lease)
	}
	if lease.Owner != nodeID {
		return
	}
	lease.LastRun = now.Unix()
	lease.Until = now.Add(ttl).Unix()
	data, err := json.Marshal(lease)
	if err != nil {
		return
	}
	if err := svc.SetSystemSetting(ctx, llmProviderMonitorLeaseKey, string(data)); err != nil {
		llmMonitorLeaseWriteLog.logf("[llm-monitor] write monitor lease failed: %v", err)
		return
	}
	llmMonitorLeaseWriteLog.reset()
}

// llmProviderMonitorFailureLog rate-limits repeated failure logs: the first
// failure is logged, then only every 10th consecutive one, and the counter
// resets on success. Lease read/write failures would otherwise log every
// minute forever.
type llmProviderMonitorFailureLog struct {
	mu    sync.Mutex
	fails int
}

func (l *llmProviderMonitorFailureLog) logf(format string, args ...any) {
	l.mu.Lock()
	l.fails++
	n := l.fails
	l.mu.Unlock()
	if n == 1 || n%10 == 0 {
		log.Printf("%s (consecutive failures: %d)", fmt.Sprintf(format, args...), n)
	}
}

func (l *llmProviderMonitorFailureLog) reset() {
	l.mu.Lock()
	l.fails = 0
	l.mu.Unlock()
}

var (
	llmMonitorLeaseReadLog  llmProviderMonitorFailureLog
	llmMonitorLeaseWriteLog llmProviderMonitorFailureLog
)

// runLLMProviderMonitorCycle tests all providers once and, when any fail,
// sends a single failure report listing only the failed providers. A registry
// load failure also notifies the admin — otherwise a broken registry would
// silently disable monitoring.
func runLLMProviderMonitorCycle(ctx context.Context, svc *llmservice.Service, mailer mail.Mailer) {
	failures, err := collectLLMProviderMonitorFailures(ctx, svc)
	if err != nil {
		adminEmail := llmProviderMonitorNotifyEmail(ctx, svc)
		if adminEmail == "" {
			log.Printf("[llm-monitor] load provider registry failed and no admin email is configured: %v", err)
			return
		}
		subject := "[HubCenter] 服务商监控异常 / Provider monitor error"
		body := fmt.Sprintf(
			"HubCenter 服务商监控无法读取服务商配置，本轮检测未执行。\r\nHubCenter provider monitoring cannot load the provider registry; this cycle was skipped.\r\n\r\n错误 Error: %s\r\n",
			err.Error(),
		)
		if sendErr := mailer.Send(ctx, []string{adminEmail}, subject, body); sendErr != nil {
			log.Printf("[llm-monitor] send registry-error notification to %s failed: %v", adminEmail, sendErr)
		}
		return
	}
	if len(failures) == 0 {
		return
	}
	adminEmail := llmProviderMonitorNotifyEmail(ctx, svc)
	if adminEmail == "" {
		log.Printf("[llm-monitor] %d provider(s) failed but no admin email is configured; skipping notification", len(failures))
		return
	}
	subject, body := buildLLMProviderMonitorEmail(failures)
	if err := mailer.Send(ctx, []string{adminEmail}, subject, body); err != nil {
		log.Printf("[llm-monitor] send failure notification to %s failed: %v", adminEmail, err)
	}
}

// llmProviderMonitorNotifyEmail resolves the alert recipient: the live
// admin_email system setting first (when the admin changes their email, the
// new address should win), then the notify_email captured in the monitor
// config at enable time (covers the setting being cleared), then empty
// (caller logs and skips).
func llmProviderMonitorNotifyEmail(ctx context.Context, svc *llmservice.Service) string {
	if email := adminEmailFromSystemSettings(ctx, svc); email != "" {
		return email
	}
	return loadLLMProviderMonitorConfig(ctx, svc).NotifyEmail
}

// collectLLMProviderMonitorFailures tests every non-paused provider that has
// at least one configured model and returns only the ones that failed.
func collectLLMProviderMonitorFailures(ctx context.Context, svc *llmservice.Service) ([]llmProviderMonitorFailure, error) {
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		return nil, err
	}
	if reg == nil {
		return nil, nil
	}
	var failures []llmProviderMonitorFailure
	for _, provider := range reg.Providers {
		if provider.Paused {
			continue
		}
		if len(provider.Models) == 0 {
			// Mirrors the admin UI test button: without a model there is
			// nothing to test.
			continue
		}
		success, errMsg, latencyMs := testLLMProviderChatStatus(ctx, svc, provider.ID)
		if success {
			continue
		}
		failures = append(failures, llmProviderMonitorFailure{
			ProviderID: provider.ID,
			Name:       provider.Name,
			Model:      provider.Models[0],
			Error:      errMsg,
			LatencyMs:  latencyMs,
		})
	}
	return failures, nil
}

// adminEmailFromSystemSettings reads the admin email written by the auth
// module as {"value": "..."} under the "admin_email" system setting.
func adminEmailFromSystemSettings(ctx context.Context, svc *llmservice.Service) string {
	raw, err := svc.GetSystemSetting(ctx, "admin_email")
	if err != nil || strings.TrimSpace(raw) == "" {
		return ""
	}
	var payload struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Value)
}

func buildLLMProviderMonitorEmail(failures []llmProviderMonitorFailure) (subject string, body string) {
	subject = fmt.Sprintf("[HubCenter] 服务商故障通知 / Provider failure alert (%d)", len(failures))
	var b strings.Builder
	b.WriteString("HubCenter 服务商监控检测到以下服务商不可用：\r\n")
	b.WriteString("HubCenter provider monitoring detected the following unavailable provider(s):\r\n\r\n")
	for i, failure := range failures {
		name := strings.TrimSpace(failure.Name)
		if name == "" {
			name = failure.ProviderID
		}
		fmt.Fprintf(&b, "%d. %s (%s)\r\n", i+1, name, failure.ProviderID)
		fmt.Fprintf(&b, "   模型 Model: %s\r\n", failure.Model)
		fmt.Fprintf(&b, "   失败原因 Error: %s\r\n", failure.Error)
		fmt.Fprintf(&b, "   耗时 Latency: %dms\r\n\r\n", failure.LatencyMs)
	}
	return subject, b.String()
}
