package tenant

import (
	"context"
	"log"
	"sync"
	"time"
)

// CloudCredentialResolver resolves the Center registration used for Cloud-side service management.
type CloudCredentialResolver func(context.Context) (centerID, centerSecret string, err error)

// CloudReadinessResolver returns the current iWorker operating readiness for Cloud visibility.
type CloudReadinessResolver func(context.Context) *CloudIWorkerReadiness

// CloudRuntimeResolver returns platform runtime state for Cloud coordination.
type CloudRuntimeResolver func(context.Context) *CloudCenterRuntime

type CloudHeartbeatSnapshot struct {
	Configured          bool      `json:"configured"`
	Status              string    `json:"status"`
	CenterID            string    `json:"center_id,omitempty"`
	LastAttemptAt       time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt       time.Time `json:"last_success_at,omitempty"`
	LastError           string    `json:"last_error,omitempty"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	RuntimeType         string    `json:"runtime_type"`
	ProductKind         string    `json:"product_kind"`
	AdminConsole        string    `json:"admin_console"`
	NonBlocking         bool      `json:"non_blocking"`
	BusinessImpact      string    `json:"business_impact"`
}

// CloudHeartbeatMonitor keeps iWorkerCloud aware that this process is an iWorkerCenter service.
type CloudHeartbeatMonitor struct {
	client              *CloudClient
	resolve             CloudCredentialResolver
	readiness           CloudReadinessResolver
	runtime             CloudRuntimeResolver
	interval            time.Duration
	timeout             time.Duration
	stop                chan struct{}
	done                chan struct{}
	sendMu              sync.Mutex
	mu                  sync.Mutex
	started             bool
	lastCenterID        string
	lastAttemptAt       time.Time
	lastSuccessAt       time.Time
	lastError           string
	consecutiveFailures int
	triggerInFlight     bool
	startOnce           sync.Once
	stopOnce            sync.Once
}

func NewCloudHeartbeatMonitor(client *CloudClient, resolve CloudCredentialResolver, interval time.Duration) *CloudHeartbeatMonitor {
	if interval <= 0 {
		interval = time.Minute
	}
	return &CloudHeartbeatMonitor{
		client:   client,
		resolve:  resolve,
		interval: interval,
		timeout:  10 * time.Second,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

func (m *CloudHeartbeatMonitor) SetRuntimeResolver(resolve CloudRuntimeResolver) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.runtime = resolve
	m.mu.Unlock()
}

func (m *CloudHeartbeatMonitor) SetReadinessResolver(resolve CloudReadinessResolver) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.readiness = resolve
	m.mu.Unlock()
}

func (m *CloudHeartbeatMonitor) Start() {
	if m == nil || m.client == nil || m.resolve == nil {
		return
	}
	m.startOnce.Do(func() {
		m.mu.Lock()
		m.started = true
		m.mu.Unlock()
		go m.loop()
	})
}

func (m *CloudHeartbeatMonitor) TriggerNow() {
	if m == nil || m.client == nil || m.resolve == nil {
		return
	}
	m.mu.Lock()
	if m.triggerInFlight {
		m.mu.Unlock()
		return
	}
	m.triggerInFlight = true
	m.mu.Unlock()
	go func() {
		defer func() {
			m.mu.Lock()
			m.triggerInFlight = false
			m.mu.Unlock()
		}()
		m.sendOnce()
	}()
}

func (m *CloudHeartbeatMonitor) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	started := m.started
	m.mu.Unlock()
	if !started {
		return
	}
	m.stopOnce.Do(func() {
		close(m.stop)
		<-m.done
	})
}

func (m *CloudHeartbeatMonitor) Snapshot() CloudHeartbeatSnapshot {
	if m == nil {
		return CloudHeartbeatSnapshot{Configured: false, Status: "disabled", RuntimeType: "service", ProductKind: "iworkercenter", AdminConsole: "web_console", NonBlocking: true, BusinessImpact: "none_local_center_and_iworker_continue"}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	status := "waiting_for_credentials"
	if m.lastSuccessAt.IsZero() && m.lastError != "" {
		status = "error"
	} else if !m.lastSuccessAt.IsZero() && m.consecutiveFailures == 0 {
		status = "online"
	} else if !m.lastSuccessAt.IsZero() && m.consecutiveFailures > 0 {
		status = "degraded"
	}
	return CloudHeartbeatSnapshot{
		Configured:          m.client != nil && m.resolve != nil,
		Status:              status,
		CenterID:            m.lastCenterID,
		LastAttemptAt:       m.lastAttemptAt,
		LastSuccessAt:       m.lastSuccessAt,
		LastError:           m.lastError,
		ConsecutiveFailures: m.consecutiveFailures,
		RuntimeType:         "service",
		ProductKind:         "iworkercenter",
		AdminConsole:        "web_console",
		NonBlocking:         true,
		BusinessImpact:      "none_local_center_and_iworker_continue",
	}
}

func (m *CloudHeartbeatMonitor) loop() {
	defer close(m.done)
	m.sendOnce()
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.sendOnce()
		case <-m.stop:
			return
		}
	}
}

func (m *CloudHeartbeatMonitor) sendOnce() {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()
	centerID, centerSecret, err := m.resolve(ctx)
	now := time.Now().UTC()
	m.recordAttempt(centerID, now)
	if err != nil {
		m.recordFailure(err.Error())
		log.Printf("[tenant/cloud-heartbeat] resolve credentials failed: %v", err)
		return
	}
	if centerID == "" || centerSecret == "" {
		return
	}
	readiness := m.resolveReadiness(ctx)
	runtime := m.resolveRuntime(ctx)
	if err := m.client.SendCenterHeartbeat(ctx, centerID, centerSecret, readiness, runtime); err != nil {
		m.recordFailure(err.Error())
		log.Printf("[tenant/cloud-heartbeat] send heartbeat failed: %v", err)
		return
	}
	m.recordSuccess(time.Now().UTC())
}

func (m *CloudHeartbeatMonitor) resolveRuntime(ctx context.Context) *CloudCenterRuntime {
	m.mu.Lock()
	resolver := m.runtime
	m.mu.Unlock()
	if resolver == nil {
		return nil
	}
	return resolver(ctx)
}

func (m *CloudHeartbeatMonitor) resolveReadiness(ctx context.Context) *CloudIWorkerReadiness {
	m.mu.Lock()
	resolver := m.readiness
	m.mu.Unlock()
	if resolver == nil {
		return nil
	}
	return resolver(ctx)
}

func (m *CloudHeartbeatMonitor) recordAttempt(centerID string, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastCenterID = centerID
	m.lastAttemptAt = at
}

func (m *CloudHeartbeatMonitor) recordSuccess(at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastSuccessAt = at
	m.lastError = ""
	m.consecutiveFailures = 0
}

func (m *CloudHeartbeatMonitor) recordFailure(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastError = message
	m.consecutiveFailures++
}
