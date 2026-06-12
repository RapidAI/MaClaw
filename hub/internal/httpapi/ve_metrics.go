package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"golang.org/x/sync/singleflight"
)

type veDiscoverableMetrics struct {
	RequestsTotal              atomic.Uint64
	SuccessTotal               atomic.Uint64
	NotModifiedTotal           atomic.Uint64
	AuthFailedTotal            atomic.Uint64
	AuthorizationInactiveTotal atomic.Uint64
	EmployeesReturnedTotal     atomic.Uint64
	DurationMillisecondsTotal  atomic.Uint64
	LastEmployeesReturned      atomic.Uint64
	LastDurationMilliseconds   atomic.Uint64
	LastUpdatedUnix            atomic.Int64
	CacheHitTotal              atomic.Uint64
	CacheMissTotal             atomic.Uint64
	CoalescedTotal             atomic.Uint64
	CacheInvalidationTotal     atomic.Uint64
	OverloadedTotal            atomic.Uint64
	StaleServedTotal           atomic.Uint64
	BuildInFlightMax           atomic.Uint64
}

type veMetricsSnapshot struct {
	Discoverable       map[string]any `json:"discoverable"`
	Initiate           map[string]any `json:"initiate"`
	AuthResponse       map[string]any `json:"auth_response"`
	DiscussionDelivery map[string]any `json:"discussion_delivery"`
	RuntimeDelivery    map[string]any `json:"runtime_delivery"`
	ControlDelivery    map[string]any `json:"control_delivery"`
}

var globalVEMetrics veDiscoverableMetrics
var globalVEDiscussionDeliveryMetrics veDiscussionDeliveryMetrics
var globalVEInitiateMetrics veOutcomeMetrics
var globalVEAuthResponseMetrics veOutcomeMetrics
var globalVERuntimeDeliveryMetrics veRuntimeDeliveryMetrics
var globalVEControlDeliveryMetrics veControlDeliveryMetrics

type veOutcomeMetrics struct {
	RequestsTotal             atomic.Uint64
	SuccessTotal              atomic.Uint64
	FailedTotal               atomic.Uint64
	DurationMillisecondsTotal atomic.Uint64
	LastDurationMilliseconds  atomic.Uint64
	LastUpdatedUnix           atomic.Int64
	MethodNotAllowedTotal     atomic.Uint64
	AuthFailedTotal           atomic.Uint64
	AuthorizationFailedTotal  atomic.Uint64
	InvalidInputTotal         atomic.Uint64
	UnavailableTotal          atomic.Uint64
	NotFoundTotal             atomic.Uint64
	NotActiveTotal            atomic.Uint64
	RuntimeOfflineTotal       atomic.Uint64
	AccessDeniedTotal         atomic.Uint64
	PendingConfirmationTotal  atomic.Uint64
	ReusedSessionTotal        atomic.Uint64
	CreatedSessionTotal       atomic.Uint64
	AlreadyHandledTotal       atomic.Uint64
	AllowedTotal              atomic.Uint64
	DeniedTotal               atomic.Uint64
	BlockedTotal              atomic.Uint64
	SaveFailedTotal           atomic.Uint64
}

type veDiscussionDeliveryMetrics struct {
	MessagesTotal             atomic.Uint64
	TargetsTotal              atomic.Uint64
	AsyncQueuedTotal          atomic.Uint64
	AsyncQueueFailedTotal     atomic.Uint64
	SyncHandledTotal          atomic.Uint64
	WebsocketSentTotal        atomic.Uint64
	WebsocketFailedTotal      atomic.Uint64
	WebsocketOfflineTotal     atomic.Uint64
	WebsocketBufferFullTotal  atomic.Uint64
	WebsocketOtherFailedTotal atomic.Uint64
	RepliesPersistedTotal     atomic.Uint64
	ReplyPersistFailedTotal   atomic.Uint64
	ReplyNotifyFailedTotal    atomic.Uint64
	DurationMillisecondsTotal atomic.Uint64
	LastTargets               atomic.Uint64
	LastDurationMilliseconds  atomic.Uint64
	LastUpdatedUnix           atomic.Int64
	InviteSentTotal           atomic.Uint64
	InviteFailedTotal         atomic.Uint64
	CancelSentTotal           atomic.Uint64
	CancelFailedTotal         atomic.Uint64
	RenameSentTotal           atomic.Uint64
	RenameFailedTotal         atomic.Uint64
}

type veRuntimeDeliveryMetrics struct {
	AcceptedTotal             atomic.Uint64
	RejectedTotal             atomic.Uint64
	CompletedTotal            atomic.Uint64
	FailedTotal               atomic.Uint64
	TimeoutTotal              atomic.Uint64
	HTTPStatusFailedTotal     atomic.Uint64
	EmptyReplyTotal           atomic.Uint64
	TransportFailedTotal      atomic.Uint64
	OtherFailedTotal          atomic.Uint64
	CircuitOpenTotal          atomic.Uint64
	CircuitRejectedTotal      atomic.Uint64
	InFlight                  atomic.Int64
	InFlightMax               atomic.Uint64
	DurationMillisecondsTotal atomic.Uint64
	LastDurationMilliseconds  atomic.Uint64
	LastUpdatedUnix           atomic.Int64
}

type veControlDeliveryMetrics struct {
	SentTotal        atomic.Uint64
	FailedTotal      atomic.Uint64
	OfflineTotal     atomic.Uint64
	BufferFullTotal  atomic.Uint64
	OtherFailedTotal atomic.Uint64
	LastUpdatedUnix  atomic.Int64
}

type veDiscoverableCacheEntry struct {
	Data      []byte
	ETag      string
	Employees int
	ExpiresAt time.Time
}

type veDiscoverableBuildResult struct {
	Entry      veDiscoverableCacheEntry
	Overloaded bool
	Stale      bool
}

var veDiscoverableCache = struct {
	sync.Mutex
	Entries map[string]veDiscoverableCacheEntry
}{Entries: map[string]veDiscoverableCacheEntry{}}

const (
	defaultVEDiscoverableCacheTTLSeconds  = 2
	defaultVEDiscoverableStaleTTLSeconds  = 30
	defaultVEDiscoverableCacheMaxKeys     = 2048
	defaultVEDiscoverableBuildConcurrency = 128
	defaultVERuntimeDeliveryConcurrency   = 64
	defaultVERuntimeCircuitFailureLimit   = 3
	defaultVERuntimeCircuitFailureWindow  = 30
	defaultVERuntimeCircuitOpenSeconds    = 5
	defaultVERuntimeCircuitMaxKeys        = 4096
	maxVEDiscoverableCacheTTLSeconds      = 300
	maxVEDiscoverableStaleTTLSeconds      = 600
	maxVEDiscoverableCacheMaxKeys         = 100000
	maxVEDiscoverableBuildConcurrency     = 4096
	maxVERuntimeDeliveryConcurrency       = 4096
	maxVERuntimeCircuitFailureLimit       = 100
	maxVERuntimeCircuitFailureWindow      = 3600
	maxVERuntimeCircuitOpenSeconds        = 300
	maxVERuntimeCircuitMaxKeys            = 100000
)

var (
	veDiscoverableCacheTTL         = time.Duration(veIntEnv("HUB_VE_DISCOVERABLE_CACHE_TTL_SECONDS", defaultVEDiscoverableCacheTTLSeconds, 1, maxVEDiscoverableCacheTTLSeconds)) * time.Second
	veDiscoverableStaleTTL         = time.Duration(veIntEnv("HUB_VE_DISCOVERABLE_STALE_TTL_SECONDS", defaultVEDiscoverableStaleTTLSeconds, 1, maxVEDiscoverableStaleTTLSeconds)) * time.Second
	veDiscoverableCacheMaxKeys     = veIntEnv("HUB_VE_DISCOVERABLE_CACHE_MAX_KEYS", defaultVEDiscoverableCacheMaxKeys, 16, maxVEDiscoverableCacheMaxKeys)
	veDiscoverableBuildConcurrency = veIntEnv("HUB_VE_DISCOVERABLE_BUILD_CONCURRENCY", defaultVEDiscoverableBuildConcurrency, 1, maxVEDiscoverableBuildConcurrency)
	veRuntimeDeliveryConcurrency   = veIntEnv("HUB_VE_RUNTIME_DELIVERY_CONCURRENCY", defaultVERuntimeDeliveryConcurrency, 1, maxVERuntimeDeliveryConcurrency)
	veRuntimeCircuitFailureLimit   = veIntEnv("HUB_VE_RUNTIME_CIRCUIT_FAILURE_LIMIT", defaultVERuntimeCircuitFailureLimit, 1, maxVERuntimeCircuitFailureLimit)
	veRuntimeCircuitFailureWindow  = time.Duration(veIntEnv("HUB_VE_RUNTIME_CIRCUIT_FAILURE_WINDOW_SECONDS", defaultVERuntimeCircuitFailureWindow, 1, maxVERuntimeCircuitFailureWindow)) * time.Second
	veRuntimeCircuitOpenDuration   = time.Duration(veIntEnv("HUB_VE_RUNTIME_CIRCUIT_OPEN_SECONDS", defaultVERuntimeCircuitOpenSeconds, 1, maxVERuntimeCircuitOpenSeconds)) * time.Second
	veRuntimeCircuitMaxKeys        = veIntEnv("HUB_VE_RUNTIME_CIRCUIT_MAX_KEYS", defaultVERuntimeCircuitMaxKeys, 16, maxVERuntimeCircuitMaxKeys)
	veDiscoverableSingleflight     singleflight.Group
	veDiscoverableBuildSemaphore   = make(chan struct{}, veDiscoverableBuildConcurrency)
	veRuntimeDeliverySemaphore     = make(chan struct{}, veRuntimeDeliveryConcurrency)
	veRuntimeDeliveryCircuit       = struct {
		sync.Mutex
		Entries map[string]veRuntimeDeliveryCircuitEntry
	}{Entries: map[string]veRuntimeDeliveryCircuitEntry{}}
)

type veRuntimeDeliveryCircuitEntry struct {
	Failures    int
	LastFailure time.Time
	OpenUntil   time.Time
}

type veTemporaryDeliveryError struct {
	Code       string
	Message    string
	RetryAfter int
}

func (e veTemporaryDeliveryError) Error() string {
	return e.Message
}

func newVETemporaryDeliveryError(code, message string, retryAfter int) error {
	if retryAfter <= 0 {
		retryAfter = 1
	}
	return veTemporaryDeliveryError{Code: code, Message: message, RetryAfter: retryAfter}
}

func veIntEnv(name string, fallback, minValue, maxValue int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minValue || value > maxValue {
		return fallback
	}
	return value
}

func VEMetricsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, globalVEMetrics.snapshot())
	}
}

func observeVEDiscoverable(start time.Time, status string, employeesReturned int) {
	durationMS := uint64(time.Since(start).Milliseconds())
	if durationMS == 0 {
		durationMS = 1
	}
	globalVEMetrics.RequestsTotal.Add(1)
	switch status {
	case "success":
		globalVEMetrics.SuccessTotal.Add(1)
		globalVEMetrics.EmployeesReturnedTotal.Add(uint64(employeesReturned))
		globalVEMetrics.LastEmployeesReturned.Store(uint64(employeesReturned))
	case "not_modified":
		globalVEMetrics.NotModifiedTotal.Add(1)
	case "auth_failed":
		globalVEMetrics.AuthFailedTotal.Add(1)
	case "authorization_inactive":
		globalVEMetrics.AuthorizationInactiveTotal.Add(1)
		globalVEMetrics.SuccessTotal.Add(1)
		globalVEMetrics.LastEmployeesReturned.Store(0)
	case "overloaded":
		globalVEMetrics.OverloadedTotal.Add(1)
	}
	globalVEMetrics.DurationMillisecondsTotal.Add(durationMS)
	globalVEMetrics.LastDurationMilliseconds.Store(durationMS)
	globalVEMetrics.LastUpdatedUnix.Store(time.Now().UTC().Unix())
}

func (m *veDiscoverableMetrics) snapshot() veMetricsSnapshot {
	requests := m.RequestsTotal.Load()
	durationTotal := m.DurationMillisecondsTotal.Load()
	avgDuration := float64(0)
	if requests > 0 {
		avgDuration = float64(durationTotal) / float64(requests)
	}
	return veMetricsSnapshot{
		Discoverable: map[string]any{
			"requests_total":               requests,
			"success_total":                m.SuccessTotal.Load(),
			"not_modified_total":           m.NotModifiedTotal.Load(),
			"auth_failed_total":            m.AuthFailedTotal.Load(),
			"authorization_inactive_total": m.AuthorizationInactiveTotal.Load(),
			"employees_returned_total":     m.EmployeesReturnedTotal.Load(),
			"duration_ms_total":            durationTotal,
			"duration_ms_avg":              avgDuration,
			"last_employees_returned":      m.LastEmployeesReturned.Load(),
			"last_duration_ms":             m.LastDurationMilliseconds.Load(),
			"last_updated_unix":            m.LastUpdatedUnix.Load(),
			"cache_hit_total":              m.CacheHitTotal.Load(),
			"cache_miss_total":             m.CacheMissTotal.Load(),
			"coalesced_total":              m.CoalescedTotal.Load(),
			"cache_invalidation_total":     m.CacheInvalidationTotal.Load(),
			"overloaded_total":             m.OverloadedTotal.Load(),
			"stale_served_total":           m.StaleServedTotal.Load(),
			"cache_entries":                veDiscoverableCacheSize(),
			"cache_ttl_seconds":            int(veDiscoverableCacheTTL.Seconds()),
			"stale_ttl_seconds":            int(veDiscoverableStaleTTL.Seconds()),
			"cache_max_keys":               veDiscoverableCacheMaxKeys,
			"build_concurrency_limit":      veDiscoverableBuildConcurrency,
			"build_in_flight":              len(veDiscoverableBuildSemaphore),
			"build_in_flight_max":          m.BuildInFlightMax.Load(),
		},
		Initiate:           globalVEInitiateMetrics.snapshot(),
		AuthResponse:       globalVEAuthResponseMetrics.snapshot(),
		DiscussionDelivery: globalVEDiscussionDeliveryMetrics.snapshot(),
		RuntimeDelivery:    globalVERuntimeDeliveryMetrics.snapshot(),
		ControlDelivery:    globalVEControlDeliveryMetrics.snapshot(),
	}
}

func observeVEInitiate(start time.Time, outcome string) {
	globalVEInitiateMetrics.observe(start, outcome)
}

func observeVEAuthResponse(start time.Time, outcome string) {
	globalVEAuthResponseMetrics.observe(start, outcome)
}

func (m *veOutcomeMetrics) observe(start time.Time, outcome string) {
	durationMS := uint64(time.Since(start).Milliseconds())
	if durationMS == 0 {
		durationMS = 1
	}
	m.RequestsTotal.Add(1)
	m.DurationMillisecondsTotal.Add(durationMS)
	m.LastDurationMilliseconds.Store(durationMS)
	m.LastUpdatedUnix.Store(time.Now().UTC().Unix())
	switch outcome {
	case "created":
		m.SuccessTotal.Add(1)
		m.CreatedSessionTotal.Add(1)
	case "reused":
		m.SuccessTotal.Add(1)
		m.ReusedSessionTotal.Add(1)
	case "pending_confirmation":
		m.SuccessTotal.Add(1)
		m.PendingConfirmationTotal.Add(1)
	case "allowed":
		m.SuccessTotal.Add(1)
		m.AllowedTotal.Add(1)
	case "denied":
		m.SuccessTotal.Add(1)
		m.DeniedTotal.Add(1)
	case "blocked":
		m.SuccessTotal.Add(1)
		m.BlockedTotal.Add(1)
	case "method_not_allowed":
		m.FailedTotal.Add(1)
		m.MethodNotAllowedTotal.Add(1)
	case "auth_failed":
		m.FailedTotal.Add(1)
		m.AuthFailedTotal.Add(1)
	case "authorization_failed":
		m.FailedTotal.Add(1)
		m.AuthorizationFailedTotal.Add(1)
	case "invalid_input":
		m.FailedTotal.Add(1)
		m.InvalidInputTotal.Add(1)
	case "unavailable":
		m.FailedTotal.Add(1)
		m.UnavailableTotal.Add(1)
	case "not_found":
		m.FailedTotal.Add(1)
		m.NotFoundTotal.Add(1)
	case "not_active":
		m.FailedTotal.Add(1)
		m.NotActiveTotal.Add(1)
	case "runtime_offline":
		m.FailedTotal.Add(1)
		m.RuntimeOfflineTotal.Add(1)
	case "access_denied":
		m.FailedTotal.Add(1)
		m.AccessDeniedTotal.Add(1)
	case "already_handled":
		m.FailedTotal.Add(1)
		m.AlreadyHandledTotal.Add(1)
	case "save_failed":
		m.FailedTotal.Add(1)
		m.SaveFailedTotal.Add(1)
	default:
		m.FailedTotal.Add(1)
	}
}

func (m *veOutcomeMetrics) snapshot() map[string]any {
	requests := m.RequestsTotal.Load()
	durationTotal := m.DurationMillisecondsTotal.Load()
	avgDuration := float64(0)
	if requests > 0 {
		avgDuration = float64(durationTotal) / float64(requests)
	}
	return map[string]any{
		"requests_total":             requests,
		"success_total":              m.SuccessTotal.Load(),
		"failed_total":               m.FailedTotal.Load(),
		"duration_ms_total":          durationTotal,
		"duration_ms_avg":            avgDuration,
		"last_duration_ms":           m.LastDurationMilliseconds.Load(),
		"last_updated_unix":          m.LastUpdatedUnix.Load(),
		"method_not_allowed_total":   m.MethodNotAllowedTotal.Load(),
		"auth_failed_total":          m.AuthFailedTotal.Load(),
		"authorization_failed_total": m.AuthorizationFailedTotal.Load(),
		"invalid_input_total":        m.InvalidInputTotal.Load(),
		"unavailable_total":          m.UnavailableTotal.Load(),
		"not_found_total":            m.NotFoundTotal.Load(),
		"not_active_total":           m.NotActiveTotal.Load(),
		"runtime_offline_total":      m.RuntimeOfflineTotal.Load(),
		"access_denied_total":        m.AccessDeniedTotal.Load(),
		"pending_confirmation_total": m.PendingConfirmationTotal.Load(),
		"reused_session_total":       m.ReusedSessionTotal.Load(),
		"created_session_total":      m.CreatedSessionTotal.Load(),
		"already_handled_total":      m.AlreadyHandledTotal.Load(),
		"allowed_total":              m.AllowedTotal.Load(),
		"denied_total":               m.DeniedTotal.Load(),
		"blocked_total":              m.BlockedTotal.Load(),
		"save_failed_total":          m.SaveFailedTotal.Load(),
	}
}

func observeVEDiscussionDelivery(start time.Time, targets int) {
	durationMS := uint64(time.Since(start).Milliseconds())
	if durationMS == 0 {
		durationMS = 1
	}
	globalVEDiscussionDeliveryMetrics.MessagesTotal.Add(1)
	globalVEDiscussionDeliveryMetrics.TargetsTotal.Add(uint64(targets))
	globalVEDiscussionDeliveryMetrics.DurationMillisecondsTotal.Add(durationMS)
	globalVEDiscussionDeliveryMetrics.LastTargets.Store(uint64(targets))
	globalVEDiscussionDeliveryMetrics.LastDurationMilliseconds.Store(durationMS)
	globalVEDiscussionDeliveryMetrics.LastUpdatedUnix.Store(time.Now().UTC().Unix())
}

func observeVEDiscussionEvent(kind string, err error) {
	switch kind {
	case "async_queue":
		if err != nil {
			globalVEDiscussionDeliveryMetrics.AsyncQueueFailedTotal.Add(1)
		} else {
			globalVEDiscussionDeliveryMetrics.AsyncQueuedTotal.Add(1)
		}
	case "sync_handled":
		if err == nil {
			globalVEDiscussionDeliveryMetrics.SyncHandledTotal.Add(1)
		}
	case "websocket":
		if err != nil {
			globalVEDiscussionDeliveryMetrics.WebsocketFailedTotal.Add(1)
			switch {
			case errors.Is(err, device.ErrMachineSendBufferFull):
				globalVEDiscussionDeliveryMetrics.WebsocketBufferFullTotal.Add(1)
			case errors.Is(err, device.ErrMachineOffline):
				globalVEDiscussionDeliveryMetrics.WebsocketOfflineTotal.Add(1)
			default:
				globalVEDiscussionDeliveryMetrics.WebsocketOtherFailedTotal.Add(1)
			}
		} else {
			globalVEDiscussionDeliveryMetrics.WebsocketSentTotal.Add(1)
		}
	case "reply_persist":
		if err != nil {
			globalVEDiscussionDeliveryMetrics.ReplyPersistFailedTotal.Add(1)
		} else {
			globalVEDiscussionDeliveryMetrics.RepliesPersistedTotal.Add(1)
		}
	case "reply_notify":
		if err != nil {
			globalVEDiscussionDeliveryMetrics.ReplyNotifyFailedTotal.Add(1)
		}
	case "invite":
		if err != nil {
			globalVEDiscussionDeliveryMetrics.InviteFailedTotal.Add(1)
		} else {
			globalVEDiscussionDeliveryMetrics.InviteSentTotal.Add(1)
		}
	case "cancel":
		if err != nil {
			globalVEDiscussionDeliveryMetrics.CancelFailedTotal.Add(1)
		} else {
			globalVEDiscussionDeliveryMetrics.CancelSentTotal.Add(1)
		}
	case "rename":
		if err != nil {
			globalVEDiscussionDeliveryMetrics.RenameFailedTotal.Add(1)
		} else {
			globalVEDiscussionDeliveryMetrics.RenameSentTotal.Add(1)
		}
	}
}

func (m *veDiscussionDeliveryMetrics) snapshot() map[string]any {
	messages := m.MessagesTotal.Load()
	durationTotal := m.DurationMillisecondsTotal.Load()
	avgDuration := float64(0)
	if messages > 0 {
		avgDuration = float64(durationTotal) / float64(messages)
	}
	return map[string]any{
		"messages_total":               messages,
		"targets_total":                m.TargetsTotal.Load(),
		"async_queued_total":           m.AsyncQueuedTotal.Load(),
		"async_queue_failed_total":     m.AsyncQueueFailedTotal.Load(),
		"sync_handled_total":           m.SyncHandledTotal.Load(),
		"websocket_sent_total":         m.WebsocketSentTotal.Load(),
		"websocket_failed_total":       m.WebsocketFailedTotal.Load(),
		"websocket_offline_total":      m.WebsocketOfflineTotal.Load(),
		"websocket_buffer_full_total":  m.WebsocketBufferFullTotal.Load(),
		"websocket_other_failed_total": m.WebsocketOtherFailedTotal.Load(),
		"replies_persisted_total":      m.RepliesPersistedTotal.Load(),
		"reply_persist_failed_total":   m.ReplyPersistFailedTotal.Load(),
		"reply_notify_failed_total":    m.ReplyNotifyFailedTotal.Load(),
		"duration_ms_total":            durationTotal,
		"duration_ms_avg":              avgDuration,
		"last_targets":                 m.LastTargets.Load(),
		"last_duration_ms":             m.LastDurationMilliseconds.Load(),
		"last_updated_unix":            m.LastUpdatedUnix.Load(),
		"invite_sent_total":            m.InviteSentTotal.Load(),
		"invite_failed_total":          m.InviteFailedTotal.Load(),
		"cancel_sent_total":            m.CancelSentTotal.Load(),
		"cancel_failed_total":          m.CancelFailedTotal.Load(),
		"rename_sent_total":            m.RenameSentTotal.Load(),
		"rename_failed_total":          m.RenameFailedTotal.Load(),
	}
}

func acquireVERuntimeDeliverySlot() (func(error), bool) {
	select {
	case veRuntimeDeliverySemaphore <- struct{}{}:
		start := time.Now()
		globalVERuntimeDeliveryMetrics.AcceptedTotal.Add(1)
		inFlight := globalVERuntimeDeliveryMetrics.InFlight.Add(1)
		observeVEUintMax(&globalVERuntimeDeliveryMetrics.InFlightMax, uint64(inFlight))
		var once sync.Once
		return func(err error) {
			once.Do(func() {
				<-veRuntimeDeliverySemaphore
				durationMS := uint64(time.Since(start).Milliseconds())
				if durationMS == 0 {
					durationMS = 1
				}
				globalVERuntimeDeliveryMetrics.InFlight.Add(-1)
				globalVERuntimeDeliveryMetrics.CompletedTotal.Add(1)
				if err != nil {
					globalVERuntimeDeliveryMetrics.FailedTotal.Add(1)
					observeVERuntimeDeliveryFailureKind(err)
				}
				globalVERuntimeDeliveryMetrics.DurationMillisecondsTotal.Add(durationMS)
				globalVERuntimeDeliveryMetrics.LastDurationMilliseconds.Store(durationMS)
				globalVERuntimeDeliveryMetrics.LastUpdatedUnix.Store(time.Now().UTC().Unix())
			})
		}, true
	default:
		globalVERuntimeDeliveryMetrics.RejectedTotal.Add(1)
		globalVERuntimeDeliveryMetrics.LastUpdatedUnix.Store(time.Now().UTC().Unix())
		return nil, false
	}
}

func observeVERuntimeDeliveryFailureKind(err error) {
	switch classifyVERuntimeDeliveryFailure(err) {
	case "timeout":
		globalVERuntimeDeliveryMetrics.TimeoutTotal.Add(1)
	case "http_status":
		globalVERuntimeDeliveryMetrics.HTTPStatusFailedTotal.Add(1)
	case "empty_reply":
		globalVERuntimeDeliveryMetrics.EmptyReplyTotal.Add(1)
	case "transport":
		globalVERuntimeDeliveryMetrics.TransportFailedTotal.Add(1)
	default:
		globalVERuntimeDeliveryMetrics.OtherFailedTotal.Add(1)
	}
}

func classifyVERuntimeDeliveryFailure(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(text, "timeout") || strings.Contains(text, "deadline exceeded"):
		return "timeout"
	case strings.Contains(text, "returned status"):
		return "http_status"
	case strings.Contains(text, "did not include assistant content") || strings.Contains(text, "sse response did not include content"):
		return "empty_reply"
	case strings.Contains(text, "runtime delivery failed"):
		return "transport"
	default:
		return "other"
	}
}

func veRuntimeDeliveryCircuitKey(tenantID string, entry digitalEmployeeEntry, runtime macLawSrvRuntimeEntry) string {
	return strings.ToLower(strings.TrimSpace(tenantID) + "|" + strings.TrimSpace(entry.ID) + "|" + strings.TrimSpace(entry.PlatformEmployeeID) + "|" + strings.TrimSpace(runtime.RuntimeID) + "|" + strings.TrimSpace(runtime.BaseURL))
}

func veRuntimeDeliveryCircuitAllows(key string, now time.Time) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return true
	}
	veRuntimeDeliveryCircuit.Lock()
	defer veRuntimeDeliveryCircuit.Unlock()
	pruneVERuntimeDeliveryCircuitLocked(now)
	entry, ok := veRuntimeDeliveryCircuit.Entries[key]
	if !ok || entry.OpenUntil.IsZero() {
		return true
	}
	if now.Before(entry.OpenUntil) {
		globalVERuntimeDeliveryMetrics.CircuitRejectedTotal.Add(1)
		globalVERuntimeDeliveryMetrics.LastUpdatedUnix.Store(time.Now().UTC().Unix())
		return false
	}
	delete(veRuntimeDeliveryCircuit.Entries, key)
	return true
}

func recordVERuntimeDeliveryResult(key string, err error, now time.Time) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	veRuntimeDeliveryCircuit.Lock()
	defer veRuntimeDeliveryCircuit.Unlock()
	pruneVERuntimeDeliveryCircuitLocked(now)
	if err == nil {
		if entry, ok := veRuntimeDeliveryCircuit.Entries[key]; ok && !entry.OpenUntil.IsZero() && now.Before(entry.OpenUntil) {
			return
		}
		delete(veRuntimeDeliveryCircuit.Entries, key)
		return
	}
	entry := veRuntimeDeliveryCircuit.Entries[key]
	if !entry.LastFailure.IsZero() && now.Sub(entry.LastFailure) > veRuntimeCircuitFailureWindow {
		entry = veRuntimeDeliveryCircuitEntry{}
	}
	entry.Failures++
	entry.LastFailure = now
	if entry.Failures >= veRuntimeCircuitFailureLimit && (entry.OpenUntil.IsZero() || !now.Before(entry.OpenUntil)) {
		entry.OpenUntil = now.Add(veRuntimeCircuitOpenDuration)
		globalVERuntimeDeliveryMetrics.CircuitOpenTotal.Add(1)
		globalVERuntimeDeliveryMetrics.LastUpdatedUnix.Store(time.Now().UTC().Unix())
	}
	veRuntimeDeliveryCircuit.Entries[key] = entry
	if len(veRuntimeDeliveryCircuit.Entries) > veRuntimeCircuitMaxKeys {
		veRuntimeDeliveryCircuit.Entries = map[string]veRuntimeDeliveryCircuitEntry{key: entry}
	}
}

func pruneVERuntimeDeliveryCircuitLocked(now time.Time) {
	if len(veRuntimeDeliveryCircuit.Entries) == 0 {
		return
	}
	for key, entry := range veRuntimeDeliveryCircuit.Entries {
		if entry.OpenUntil.IsZero() {
			if !entry.LastFailure.IsZero() && now.Sub(entry.LastFailure) > veRuntimeCircuitFailureWindow {
				delete(veRuntimeDeliveryCircuit.Entries, key)
			}
			continue
		}
		if !now.Before(entry.OpenUntil) {
			delete(veRuntimeDeliveryCircuit.Entries, key)
		}
	}
}

func veRuntimeDeliveryCircuitSize() int {
	veRuntimeDeliveryCircuit.Lock()
	defer veRuntimeDeliveryCircuit.Unlock()
	pruneVERuntimeDeliveryCircuitLocked(time.Now())
	return len(veRuntimeDeliveryCircuit.Entries)
}

func (m *veRuntimeDeliveryMetrics) snapshot() map[string]any {
	completed := m.CompletedTotal.Load()
	durationTotal := m.DurationMillisecondsTotal.Load()
	avgDuration := float64(0)
	if completed > 0 {
		avgDuration = float64(durationTotal) / float64(completed)
	}
	return map[string]any{
		"accepted_total":                 m.AcceptedTotal.Load(),
		"rejected_total":                 m.RejectedTotal.Load(),
		"completed_total":                completed,
		"failed_total":                   m.FailedTotal.Load(),
		"timeout_total":                  m.TimeoutTotal.Load(),
		"http_status_failed_total":       m.HTTPStatusFailedTotal.Load(),
		"empty_reply_total":              m.EmptyReplyTotal.Load(),
		"transport_failed_total":         m.TransportFailedTotal.Load(),
		"other_failed_total":             m.OtherFailedTotal.Load(),
		"circuit_open_total":             m.CircuitOpenTotal.Load(),
		"circuit_rejected_total":         m.CircuitRejectedTotal.Load(),
		"circuit_entries":                veRuntimeDeliveryCircuitSize(),
		"circuit_max_keys":               veRuntimeCircuitMaxKeys,
		"circuit_failure_limit":          veRuntimeCircuitFailureLimit,
		"circuit_failure_window_seconds": int(veRuntimeCircuitFailureWindow.Seconds()),
		"circuit_open_seconds":           int(veRuntimeCircuitOpenDuration.Seconds()),
		"in_flight":                      m.InFlight.Load(),
		"in_flight_max":                  m.InFlightMax.Load(),
		"concurrency_limit":              veRuntimeDeliveryConcurrency,
		"delivery_timeout_sec":           int(platformA2ADeliveryTimeout.Seconds()),
		"duration_ms_total":              durationTotal,
		"duration_ms_avg":                avgDuration,
		"last_duration_ms":               m.LastDurationMilliseconds.Load(),
		"last_updated_unix":              m.LastUpdatedUnix.Load(),
	}
}

func observeVEControlDelivery(err error) {
	if err == nil {
		globalVEControlDeliveryMetrics.SentTotal.Add(1)
		globalVEControlDeliveryMetrics.LastUpdatedUnix.Store(time.Now().UTC().Unix())
		return
	}
	globalVEControlDeliveryMetrics.FailedTotal.Add(1)
	switch {
	case errors.Is(err, device.ErrMachineSendBufferFull):
		globalVEControlDeliveryMetrics.BufferFullTotal.Add(1)
	case errors.Is(err, device.ErrMachineOffline):
		globalVEControlDeliveryMetrics.OfflineTotal.Add(1)
	default:
		globalVEControlDeliveryMetrics.OtherFailedTotal.Add(1)
	}
	globalVEControlDeliveryMetrics.LastUpdatedUnix.Store(time.Now().UTC().Unix())
}

func (m *veControlDeliveryMetrics) snapshot() map[string]any {
	return map[string]any{
		"sent_total":         m.SentTotal.Load(),
		"failed_total":       m.FailedTotal.Load(),
		"offline_total":      m.OfflineTotal.Load(),
		"buffer_full_total":  m.BufferFullTotal.Load(),
		"other_failed_total": m.OtherFailedTotal.Load(),
		"last_updated_unix":  m.LastUpdatedUnix.Load(),
	}
}

func observeVEDiscoverableCache(hit bool) {
	if hit {
		globalVEMetrics.CacheHitTotal.Add(1)
	} else {
		globalVEMetrics.CacheMissTotal.Add(1)
	}
}

func observeVEDiscoverableCoalesced() {
	globalVEMetrics.CoalescedTotal.Add(1)
}

func observeVEDiscoverableStaleServed() {
	globalVEMetrics.StaleServedTotal.Add(1)
}

func acquireVEDiscoverableBuildSlot() (func(), bool) {
	select {
	case veDiscoverableBuildSemaphore <- struct{}{}:
		observeVEUintMax(&globalVEMetrics.BuildInFlightMax, uint64(len(veDiscoverableBuildSemaphore)))
		var once sync.Once
		return func() { once.Do(func() { <-veDiscoverableBuildSemaphore }) }, true
	default:
		return nil, false
	}
}

func observeVEUintMax(metric *atomic.Uint64, value uint64) {
	for {
		current := metric.Load()
		if value <= current || metric.CompareAndSwap(current, value) {
			return
		}
	}
}

func getVEDiscoverableCache(key string, now time.Time) (veDiscoverableCacheEntry, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return veDiscoverableCacheEntry{}, false
	}
	veDiscoverableCache.Lock()
	defer veDiscoverableCache.Unlock()
	entry, ok := veDiscoverableCache.Entries[key]
	if !ok || !now.Before(entry.ExpiresAt) {
		if ok && !now.Before(entry.ExpiresAt.Add(veDiscoverableStaleTTL)) {
			delete(veDiscoverableCache.Entries, key)
		}
		return veDiscoverableCacheEntry{}, false
	}
	return veDiscoverableCacheEntry{
		Data:      append([]byte(nil), entry.Data...),
		ETag:      entry.ETag,
		Employees: entry.Employees,
		ExpiresAt: entry.ExpiresAt,
	}, true
}

func getVEDiscoverableStaleCache(key string, now time.Time) (veDiscoverableCacheEntry, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return veDiscoverableCacheEntry{}, false
	}
	veDiscoverableCache.Lock()
	defer veDiscoverableCache.Unlock()
	entry, ok := veDiscoverableCache.Entries[key]
	if !ok || !now.Before(entry.ExpiresAt.Add(veDiscoverableStaleTTL)) {
		if ok {
			delete(veDiscoverableCache.Entries, key)
		}
		return veDiscoverableCacheEntry{}, false
	}
	return veDiscoverableCacheEntry{
		Data:      append([]byte(nil), entry.Data...),
		ETag:      entry.ETag,
		Employees: entry.Employees,
		ExpiresAt: entry.ExpiresAt,
	}, true
}

func veDiscoverableCacheSize() int {
	veDiscoverableCache.Lock()
	defer veDiscoverableCache.Unlock()
	return len(veDiscoverableCache.Entries)
}

func setVEDiscoverableCache(key string, data []byte, etag string, employees int, now time.Time) {
	key = strings.TrimSpace(key)
	if key == "" || len(data) == 0 || etag == "" {
		return
	}
	veDiscoverableCache.Lock()
	defer veDiscoverableCache.Unlock()
	if len(veDiscoverableCache.Entries) >= veDiscoverableCacheMaxKeys {
		for k, entry := range veDiscoverableCache.Entries {
			if !now.Before(entry.ExpiresAt) {
				delete(veDiscoverableCache.Entries, k)
			}
		}
	}
	if len(veDiscoverableCache.Entries) >= veDiscoverableCacheMaxKeys {
		oldestKey := ""
		var oldestExpiresAt time.Time
		for k, entry := range veDiscoverableCache.Entries {
			if oldestKey == "" || entry.ExpiresAt.Before(oldestExpiresAt) {
				oldestKey = k
				oldestExpiresAt = entry.ExpiresAt
			}
		}
		if oldestKey != "" {
			delete(veDiscoverableCache.Entries, oldestKey)
		}
	}
	veDiscoverableCache.Entries[key] = veDiscoverableCacheEntry{
		Data:      append([]byte(nil), data...),
		ETag:      etag,
		Employees: employees,
		ExpiresAt: now.Add(veDiscoverableCacheTTL),
	}
}

func clearVEDiscoverableCache() {
	veDiscoverableCache.Lock()
	veDiscoverableCache.Entries = map[string]veDiscoverableCacheEntry{}
	veDiscoverableCache.Unlock()
	globalVEMetrics.CacheInvalidationTotal.Add(1)
}

func writeVEConditionalJSON(w http.ResponseWriter, r *http.Request, payload any) (bool, []byte, string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return false, nil, "", err
	}
	etag := veResponseETag(data)
	notModified, err := writeVEConditionalJSONBytes(w, r, data, etag)
	return notModified, data, etag, err
}

func writeVEConditionalJSONBytes(w http.ResponseWriter, r *http.Request, data []byte, etag string) (bool, error) {
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=5, must-revalidate")
	if veIfNoneMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return true, nil
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(append(data, '\n'))
	return false, err
}

func veResponseETag(data []byte) string {
	sum := sha256.Sum256(data)
	return `"ve-discoverable-` + base64.RawURLEncoding.EncodeToString(sum[:12]) + `"`
}

func veIfNoneMatch(header, etag string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "*" || part == etag || strings.TrimPrefix(part, "W/") == etag {
			return true
		}
	}
	return false
}
