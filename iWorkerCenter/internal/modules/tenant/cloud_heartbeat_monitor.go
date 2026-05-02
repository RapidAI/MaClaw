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
}

// CloudHeartbeatMonitor keeps iWorkerCloud aware that this process is an iWorkerCenter service.
type CloudHeartbeatMonitor struct {
	client              *CloudClient
	resolve             CloudCredentialResolver
	readiness           CloudReadinessResolver
	interval            time.Duration
	timeout             time.Duration
	stop                chan struct{}
	done                chan struct{}
	mu                  sync.Mutex
	started             bool
	lastCenterID        string
	lastAttemptAt       time.Time
	lastSuccessAt       time.Time
	lastError           string
	consecutiveFailures int
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
	m.sendOnce()
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
		return CloudHeartbeatSnapshot{Configured: false, Status: "disabled", RuntimeType: "service", ProductKind: "iworkercenter", AdminConsole: "web_console"}
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
	if err := m.client.SendCenterHeartbeat(ctx, centerID, centerSecret, readiness); err != nil {
		m.recordFailure(err.Error())
		log.Printf("[tenant/cloud-heartbeat] send heartbeat failed: %v", err)
		return
	}
	m.recordSuccess(time.Now().UTC())
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
