package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ManagerConfig holds configuration for manager initialization
type ManagerConfig struct {
	// Core configuration
	EnableMCP          bool
	EnableToolRouter   bool
	EnableUsageTracker bool

	// Session configuration
	EnableRemoteSessions  bool
	EnableBrowserSessions bool

	// Security configuration
	EnableRiskAssessment bool
	EnablePolicyEngine   bool
	EnableAuditLog       bool

	// Network configuration
	EnableMDNSScanner    bool
	EnableProjectScanner bool
	EnableGossipClient   bool

	// UI configuration
	EnableFloatingAssistant bool
	EnablePowerManagement   bool

	// Timeouts
	InitializationTimeout time.Duration
	HealthCheckInterval   time.Duration
}

// DefaultManagerConfig returns default configuration
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		EnableMCP:               true,
		EnableToolRouter:        true,
		EnableUsageTracker:      true,
		EnableRemoteSessions:    true,
		EnableBrowserSessions:   true,
		EnableRiskAssessment:    true,
		EnablePolicyEngine:      true,
		EnableAuditLog:          true,
		EnableMDNSScanner:       true,
		EnableProjectScanner:    true,
		EnableGossipClient:      true,
		EnableFloatingAssistant: true,
		EnablePowerManagement:   true,
		InitializationTimeout:   30 * time.Second,
		HealthCheckInterval:     60 * time.Second,
	}
}

// InitPhase represents the different phases of initialization
type InitPhase int

const (
	InitPhaseCore InitPhase = iota
	InitPhaseSessions
	InitPhaseSecurity
	InitPhaseNetworking
	InitPhaseUI
	InitPhaseComplete
)

func (p InitPhase) String() string {
	switch p {
	case InitPhaseCore:
		return "Core"
	case InitPhaseSessions:
		return "Sessions"
	case InitPhaseSecurity:
		return "Security"
	case InitPhaseNetworking:
		return "Networking"
	case InitPhaseUI:
		return "UI"
	case InitPhaseComplete:
		return "Complete"
	default:
		return "Unknown"
	}
}

// InitResult represents the result of an initialization phase
type InitResult struct {
	Phase    InitPhase
	Success  bool
	Error    error
	Duration time.Duration
}

// InitProgress tracks initialization progress
type InitProgress struct {
	CurrentPhase InitPhase
	TotalPhases  int
	Completed    int
	Results      []InitResult
	StartTime    time.Time
	mu           sync.RWMutex
}

// NewInitProgress creates a new initialization progress tracker
func NewInitProgress() *InitProgress {
	return &InitProgress{
		CurrentPhase: InitPhaseComplete,
		TotalPhases:  5,
		Results:      make([]InitResult, 0, 6),
		StartTime:    time.Now(),
	}
}

func normalizeDuration(duration time.Duration) time.Duration {
	if duration <= 0 {
		return time.Nanosecond
	}
	return duration
}

// UpdatePhase updates the current phase and records a result
func (p *InitProgress) UpdatePhase(phase InitPhase, success bool, err error, duration time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.CurrentPhase = phase
	if success {
		p.Completed++
	}

	result := InitResult{
		Phase:    phase,
		Success:  success,
		Error:    err,
		Duration: normalizeDuration(duration),
	}

	newResults := make([]InitResult, len(p.Results)+1)
	copy(newResults, p.Results)
	newResults[len(p.Results)] = result
	p.Results = newResults
}

// GetProgress returns current progress information
func (p *InitProgress) GetProgress() (current InitPhase, completed, total int, results []InitResult) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	resultsCopy := make([]InitResult, len(p.Results))
	copy(resultsCopy, p.Results)

	return p.CurrentPhase, p.Completed, p.TotalPhases, resultsCopy
}

// GetTotalDuration returns the total initialization duration
func (p *InitProgress) GetTotalDuration() time.Duration {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return normalizeDuration(time.Since(p.StartTime))
}

// OptimizedAppInitializer provides optimized initialization for App managers
type OptimizedAppInitializer struct {
	config       ManagerConfig
	progress     *InitProgress
	parallel     bool
	timeout      time.Duration
	retryCount   int
	lastDuration time.Duration
	metricsMu    sync.RWMutex
}

// NewOptimizedAppInitializer creates a new optimized initializer
func NewOptimizedAppInitializer(config ManagerConfig) *OptimizedAppInitializer {
	return &OptimizedAppInitializer{
		config:     config,
		progress:   NewInitProgress(),
		parallel:   true,
		timeout:    config.InitializationTimeout,
		retryCount: 3,
	}
}

// SetParallel sets whether to use parallel initialization
func (o *OptimizedAppInitializer) SetParallel(parallel bool) {
	o.parallel = parallel
}

// SetTimeout sets the initialization timeout
func (o *OptimizedAppInitializer) SetTimeout(timeout time.Duration) {
	o.timeout = timeout
}

// SetRetryCount sets the retry count for failed initializations
func (o *OptimizedAppInitializer) SetRetryCount(count int) {
	o.retryCount = count
}

// InitializeManagers initializes all managers with optimized flow
func (o *OptimizedAppInitializer) InitializeManagers(ctx context.Context, managers *AppManagers) error {
	start := time.Now()
	if o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}

	var err error
	if o.parallel {
		err = o.initializeParallel(ctx, managers)
	} else {
		err = o.initializeSequential(ctx, managers)
	}

	o.metricsMu.Lock()
	o.lastDuration = normalizeDuration(time.Since(start))
	o.metricsMu.Unlock()

	return err
}

// initializeSequential initializes managers sequentially
func (o *OptimizedAppInitializer) initializeSequential(ctx context.Context, managers *AppManagers) error {
	phases := []struct {
		name  string
		init  func(context.Context, *AppManagers) error
		phase InitPhase
	}{
		{"Core", o.initCore, InitPhaseCore},
		{"Sessions", o.initSessions, InitPhaseSessions},
		{"Security", o.initSecurity, InitPhaseSecurity},
		{"Networking", o.initNetworking, InitPhaseNetworking},
		{"UI", o.initUI, InitPhaseUI},
	}

	for _, phaseInfo := range phases {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		start := time.Now()
		err := o.retryInit(ctx, func() error {
			return phaseInfo.init(ctx, managers)
		})
		duration := time.Since(start)

		success := err == nil
		o.progress.UpdatePhase(phaseInfo.phase, success, err, duration)

		if !success {
			return fmt.Errorf("failed to initialize %s manager: %w", phaseInfo.name, err)
		}
	}

	o.progress.UpdatePhase(InitPhaseComplete, true, nil, time.Since(o.progress.StartTime))
	return nil
}

// initializeParallel initializes managers in parallel where possible
func (o *OptimizedAppInitializer) initializeParallel(ctx context.Context, managers *AppManagers) error {
	start := time.Now()
	err := o.retryInit(ctx, func() error {
		return o.initCore(ctx, managers)
	})
	o.progress.UpdatePhase(InitPhaseCore, err == nil, err, time.Since(start))
	if err != nil {
		return fmt.Errorf("failed to initialize core manager: %w", err)
	}

	var wg sync.WaitGroup
	results := make(chan struct {
		phase    InitPhase
		err      error
		duration time.Duration
	}, 4)

	managersToInit := []struct {
		name  string
		init  func(context.Context, *AppManagers) error
		phase InitPhase
	}{
		{"Sessions", o.initSessions, InitPhaseSessions},
		{"Security", o.initSecurity, InitPhaseSecurity},
		{"Networking", o.initNetworking, InitPhaseNetworking},
		{"UI", o.initUI, InitPhaseUI},
	}

	for _, managerInfo := range managersToInit {
		wg.Add(1)
		go func(info struct {
			name  string
			init  func(context.Context, *AppManagers) error
			phase InitPhase
		}) {
			defer wg.Done()

			select {
			case <-ctx.Done():
				results <- struct {
					phase    InitPhase
					err      error
					duration time.Duration
				}{info.phase, ctx.Err(), 0}
				return
			default:
			}

			start := time.Now()
			err := o.retryInit(ctx, func() error {
				return info.init(ctx, managers)
			})
			results <- struct {
				phase    InitPhase
				err      error
				duration time.Duration
			}{info.phase, err, time.Since(start)}
		}(managerInfo)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var initErrors []error
	for result := range results {
		success := result.err == nil
		o.progress.UpdatePhase(result.phase, success, result.err, result.duration)
		if !success {
			initErrors = append(initErrors, result.err)
		}
	}

	if len(initErrors) > 0 {
		return fmt.Errorf("parallel initialization failed with %d errors: %v", len(initErrors), initErrors[0])
	}

	o.progress.UpdatePhase(InitPhaseComplete, true, nil, time.Since(o.progress.StartTime))
	return nil
}

// retryInit retries initialization with exponential backoff
func (o *OptimizedAppInitializer) retryInit(ctx context.Context, initFunc func() error) error {
	var lastErr error
	backoff := 100 * time.Millisecond

	for i := 0; i < o.retryCount; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
				if backoff > 5*time.Second {
					backoff = 5 * time.Second
				}
			}
		}

		err := initFunc()
		if err == nil {
			return nil
		}
		lastErr = err
	}

	return lastErr
}

// initCore initializes the core manager
func (o *OptimizedAppInitializer) initCore(ctx context.Context, managers *AppManagers) error {
	if !o.config.EnableMCP && !o.config.EnableToolRouter && !o.config.EnableUsageTracker {
		return nil
	}

	return managers.core.Initialize()
}

// initSessions initializes the session manager
func (o *OptimizedAppInitializer) initSessions(ctx context.Context, managers *AppManagers) error {
	if !o.config.EnableRemoteSessions && !o.config.EnableBrowserSessions {
		return nil
	}

	return managers.sessions.Initialize()
}

// initSecurity initializes the security manager
func (o *OptimizedAppInitializer) initSecurity(ctx context.Context, managers *AppManagers) error {
	if !o.config.EnableRiskAssessment && !o.config.EnablePolicyEngine && !o.config.EnableAuditLog {
		return nil
	}

	return managers.security.Initialize()
}

// initNetworking initializes the networking manager
func (o *OptimizedAppInitializer) initNetworking(ctx context.Context, managers *AppManagers) error {
	if !o.config.EnableMDNSScanner && !o.config.EnableProjectScanner && !o.config.EnableGossipClient {
		return nil
	}

	return managers.networking.Initialize()
}

// initUI initializes the UI manager
func (o *OptimizedAppInitializer) initUI(ctx context.Context, managers *AppManagers) error {
	if !o.config.EnableFloatingAssistant && !o.config.EnablePowerManagement {
		return nil
	}

	return managers.ui.Initialize()
}

// GetProgress returns the current initialization progress
func (o *OptimizedAppInitializer) GetProgress() *InitProgress {
	return o.progress
}

// InitMetrics provides metrics about the initialization process
type InitMetrics struct {
	TotalDuration      time.Duration
	PhaseMetrics       map[InitPhase]time.Duration
	SuccessRate        float64
	ParallelUsed       bool
	RetryCount         int
	ConfiguredFeatures map[string]bool
}

// GetMetrics returns initialization metrics
func (o *OptimizedAppInitializer) GetMetrics() InitMetrics {
	_, _, total, results := o.progress.GetProgress()

	phaseMetrics := make(map[InitPhase]time.Duration)
	successCount := 0

	for _, result := range results {
		if result.Phase == InitPhaseComplete {
			continue
		}
		phaseMetrics[result.Phase] = normalizeDuration(result.Duration)
		if result.Success {
			successCount++
		}
	}

	for _, phase := range []InitPhase{InitPhaseCore, InitPhaseSessions, InitPhaseSecurity, InitPhaseNetworking, InitPhaseUI} {
		if _, exists := phaseMetrics[phase]; !exists {
			phaseMetrics[phase] = time.Nanosecond
		}
	}

	var successRate float64
	if total > 0 {
		successRate = float64(successCount) / float64(total)
	}

	o.metricsMu.RLock()
	totalDuration := o.lastDuration
	o.metricsMu.RUnlock()
	if totalDuration <= 0 {
		totalDuration = o.progress.GetTotalDuration()
	}

	return InitMetrics{
		TotalDuration: normalizeDuration(totalDuration),
		PhaseMetrics:  phaseMetrics,
		SuccessRate:   successRate,
		ParallelUsed:  o.parallel,
		RetryCount:    o.retryCount,
		ConfiguredFeatures: map[string]bool{
			"mcp":               o.config.EnableMCP,
			"toolRouter":        o.config.EnableToolRouter,
			"usageTracker":      o.config.EnableUsageTracker,
			"remoteSessions":    o.config.EnableRemoteSessions,
			"browserSessions":   o.config.EnableBrowserSessions,
			"riskAssessment":    o.config.EnableRiskAssessment,
			"policyEngine":      o.config.EnablePolicyEngine,
			"auditLog":          o.config.EnableAuditLog,
			"mdnsScanner":       o.config.EnableMDNSScanner,
			"projectScanner":    o.config.EnableProjectScanner,
			"gossipClient":      o.config.EnableGossipClient,
			"floatingAssistant": o.config.EnableFloatingAssistant,
			"powerManagement":   o.config.EnablePowerManagement,
		},
	}
}

// HealthChecker provides health checking for managers
type HealthChecker struct {
	managers      AppManagersInterface
	checkInterval time.Duration
	timeout       time.Duration
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(managers AppManagersInterface, interval, timeout time.Duration) *HealthChecker {
	return &HealthChecker{
		managers:      managers,
		checkInterval: interval,
		timeout:       timeout,
	}
}

// StartHealthChecking starts periodic health checking
func (h *HealthChecker) StartHealthChecking(ctx context.Context) <-chan map[string]bool {
	resultChan := make(chan map[string]bool)

	go func() {
		defer close(resultChan)
		interval := h.checkInterval
		if interval <= 0 {
			interval = 100 * time.Millisecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, cancel := context.WithTimeout(ctx, h.timeout)
				health := h.managers.HealthCheck()
				resultChan <- health
				cancel()
			}
		}
	}()

	return resultChan
}

// CheckHealth performs a single health check
func (h *HealthChecker) CheckHealth(ctx context.Context) map[string]bool {
	_, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	return h.managers.HealthCheck()
}
