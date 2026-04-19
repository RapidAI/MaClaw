package main

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestOptimizedAppInitializerCreation tests the creation of optimized initializer
func TestOptimizedAppInitializerCreation(t *testing.T) {
	config := DefaultManagerConfig()
	initializer := NewOptimizedAppInitializer(config)

	if initializer == nil {
		t.Fatal("Expected initializer to be created, got nil")
	}

	// Test default settings
	if !initializer.parallel {
		t.Error("Parallel initialization should be enabled by default")
	}

	if initializer.timeout != config.InitializationTimeout {
		t.Errorf("Expected timeout to be %v, got %v", config.InitializationTimeout, initializer.timeout)
	}

	if initializer.retryCount != 3 {
		t.Errorf("Expected retry count to be 3, got %d", initializer.retryCount)
	}
}

// TestSequentialInitialization tests sequential initialization
func TestSequentialInitialization(t *testing.T) {
	config := DefaultManagerConfig()
	initializer := NewOptimizedAppInitializer(config)
	initializer.SetParallel(false)

	ctx := context.Background()
	managers := NewAppManagers(ctx)

	err := initializer.InitializeManagers(ctx, managers)
	if err != nil {
		t.Errorf("Sequential initialization failed: %v", err)
	}

	// Check progress
	progress := initializer.GetProgress()
	current, completed, total, results := progress.GetProgress()

	if current != InitPhaseComplete {
		t.Errorf("Expected current phase to be Complete, got %s", current)
	}

	// completed includes the Complete phase, so it will be total + 1
	if completed != total+1 {
		t.Errorf("Expected completed %d, got %d", total+1, completed)
	}

	if len(results) != 6 { // 5 phases + Complete phase
		t.Errorf("Expected 6 results, got %d", len(results))
	}

	// Check that all phases succeeded
	for _, result := range results {
		if !result.Success {
			t.Errorf("Phase %s failed: %v", result.Phase, result.Error)
		}
	}
}

// TestParallelInitialization tests parallel initialization
func TestParallelInitialization(t *testing.T) {
	config := DefaultManagerConfig()
	initializer := NewOptimizedAppInitializer(config)
	initializer.SetParallel(true)

	ctx := context.Background()
	managers := NewAppManagers(ctx)

	err := initializer.InitializeManagers(ctx, managers)
	if err != nil {
		t.Errorf("Parallel initialization failed: %v", err)
	}

	// Check progress
	progress := initializer.GetProgress()
	current, completed, total, results := progress.GetProgress()

	if current != InitPhaseComplete {
		t.Errorf("Expected current phase to be Complete, got %s", current)
	}

	// completed includes the Complete phase, so it will be total + 1
	if completed != total+1 {
		t.Errorf("Expected completed %d, got %d", total+1, completed)
	}

	if len(results) != 6 { // 5 phases + complete phase
		t.Errorf("Expected 6 results, got %d", len(results))
	}
}

// TestInitializationWithDisabledFeatures tests initialization when features are disabled
func TestInitializationWithDisabledFeatures(t *testing.T) {
	config := ManagerConfig{
		EnableMCP:               false,
		EnableToolRouter:        false,
		EnableUsageTracker:      false,
		EnableRemoteSessions:    false,
		EnableBrowserSessions:   false,
		EnableRiskAssessment:    false,
		EnablePolicyEngine:      false,
		EnableAuditLog:          false,
		EnableMDNSScanner:       false,
		EnableProjectScanner:    false,
		EnableGossipClient:      false,
		EnableFloatingAssistant: false,
		EnablePowerManagement:   false,
		InitializationTimeout:   30 * time.Second,
		HealthCheckInterval:     60 * time.Second,
	}

	initializer := NewOptimizedAppInitializer(config)
	ctx := context.Background()
	managers := NewAppManagers(ctx)

	err := initializer.InitializeManagers(ctx, managers)
	if err != nil {
		t.Errorf("Initialization with disabled features failed: %v", err)
	}
}

// TestInitializationTimeout tests initialization timeout handling
func TestInitializationTimeout(t *testing.T) {
	config := DefaultManagerConfig()
	initializer := NewOptimizedAppInitializer(config)
	initializer.SetTimeout(1 * time.Millisecond) // Very short timeout

	ctx := context.Background()
	managers := NewAppManagers(ctx)

	err := initializer.InitializeManagers(ctx, managers)
	// This might fail due to timeout, which is expected
	if err != nil {
		t.Logf("Initialization timed out as expected: %v", err)
	}
}

// TestInitProgressTracking tests the progress tracking functionality
func TestInitProgressTracking(t *testing.T) {
	progress := NewInitProgress()

	// Test initial state
	current, completed, total, results := progress.GetProgress()
	if current != InitPhaseComplete { // Default phase
		t.Errorf("Expected initial phase to be Complete, got %s", current)
	}

	if completed != 0 {
		t.Errorf("Expected initial completed to be 0, got %d", completed)
	}

	if total != 5 {
		t.Errorf("Expected total phases to be 5, got %d", total)
	}

	if len(results) != 0 {
		t.Errorf("Expected initial results to be empty, got %d", len(results))
	}

	// Test phase updates
	progress.UpdatePhase(InitPhaseCore, true, nil, 10*time.Millisecond)
	progress.UpdatePhase(InitPhaseSessions, false, fmt.Errorf("test error"), 5*time.Millisecond)

	current, completed, total, results = progress.GetProgress()
	if completed != 1 {
		t.Errorf("Expected completed to be 1, got %d", completed)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	// Check results
	if results[0].Phase != InitPhaseCore {
		t.Errorf("Expected first result phase to be Core, got %s", results[0].Phase)
	}

	if !results[0].Success {
		t.Error("Expected first result to be successful")
	}

	if results[1].Phase != InitPhaseSessions {
		t.Errorf("Expected second result phase to be Sessions, got %s", results[1].Phase)
	}

	if results[1].Success {
		t.Error("Expected second result to be unsuccessful")
	}
}

// TestInitMetrics tests the initialization metrics collection
func TestInitMetrics(t *testing.T) {
	config := DefaultManagerConfig()
	initializer := NewOptimizedAppInitializer(config)
	ctx := context.Background()
	managers := NewAppManagers(ctx)

	// Initialize to generate metrics
	err := initializer.InitializeManagers(ctx, managers)
	if err != nil {
		t.Errorf("Initialization failed: %v", err)
	}

	// Get metrics
	metrics := initializer.GetMetrics()

	// Check metrics
	if metrics.TotalDuration <= 0 {
		t.Error("Expected total duration to be positive")
	}

	if metrics.SuccessRate <= 0 || metrics.SuccessRate > 1 {
		t.Errorf("Expected success rate to be between 0 and 1, got %f", metrics.SuccessRate)
	}

	if !metrics.ParallelUsed {
		t.Error("Expected parallel to be used")
	}

	if metrics.RetryCount != 3 {
		t.Errorf("Expected retry count to be 3, got %d", metrics.RetryCount)
	}

	// Check configured features
	features := metrics.ConfiguredFeatures
	expectedFeatures := []string{
		"mcp", "toolRouter", "usageTracker",
		"remoteSessions", "browserSessions",
		"riskAssessment", "policyEngine", "auditLog",
		"mdnsScanner", "projectScanner", "gossipClient",
		"floatingAssistant", "powerManagement",
	}

	for _, feature := range expectedFeatures {
		if enabled, exists := features[feature]; !exists {
			t.Errorf("Expected feature %s to be configured", feature)
		} else if !enabled {
			t.Errorf("Expected feature %s to be enabled", feature)
		}
	}

	// Check phase metrics
	expectedPhases := []InitPhase{
		InitPhaseCore, InitPhaseSessions, InitPhaseSecurity,
		InitPhaseNetworking, InitPhaseUI,
	}

	for _, phase := range expectedPhases {
		if duration, exists := metrics.PhaseMetrics[phase]; !exists {
			t.Errorf("Expected phase %s to have metrics", phase)
		} else if duration <= 0 {
			t.Errorf("Expected phase %s duration to be positive, got %v", phase, duration)
		}
	}
}

// TestHealthChecker tests the health checker functionality
func TestHealthChecker(t *testing.T) {
	ctx := context.Background()
	managers := NewMockAppManagers()

	checker := NewHealthChecker(managers, 100*time.Millisecond, 5*time.Second)
	if checker == nil {
		t.Fatal("Expected health checker to be created, got nil")
	}

	// Test single health check
	health := checker.CheckHealth(ctx)
	if len(health) != 5 {
		t.Errorf("Expected 5 health checks, got %d", len(health))
	}

	// Check that all managers are healthy
	for manager, healthy := range health {
		if !healthy {
			t.Errorf("Manager %s should be healthy", manager)
		}
	}
}

// TestHealthCheckerPeriodic tests periodic health checking
func TestHealthCheckerPeriodic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	managers := NewMockAppManagers()
	checker := NewHealthChecker(managers, 100*time.Millisecond, 5*time.Second)

	// Start periodic health checking
	healthChan := checker.StartHealthChecking(ctx)

	// Collect health results
	healthCount := 0
	for health := range healthChan {
		if len(health) == 5 {
			healthCount++
		}
	}

	// Should get at least 3 health checks (500ms / 100ms interval)
	if healthCount < 3 {
		t.Errorf("Expected at least 3 health checks, got %d", healthCount)
	}
}

// BenchmarkSequentialInitialization benchmarks sequential initialization
func BenchmarkSequentialInitialization(b *testing.B) {
	config := DefaultManagerConfig()
	initializer := NewOptimizedAppInitializer(config)
	initializer.SetParallel(false)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		managers := NewAppManagers(ctx)
		_ = initializer.InitializeManagers(ctx, managers)
	}
}

// BenchmarkParallelInitialization benchmarks parallel initialization
func BenchmarkParallelInitialization(b *testing.B) {
	config := DefaultManagerConfig()
	initializer := NewOptimizedAppInitializer(config)
	initializer.SetParallel(true)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		managers := NewAppManagers(ctx)
		_ = initializer.InitializeManagers(ctx, managers)
	}
}

// BenchmarkHealthCheck benchmarks health checking
func BenchmarkHealthCheck(b *testing.B) {
	ctx := context.Background()
	managers := NewMockAppManagers()
	checker := NewHealthChecker(managers, 0, 5*time.Second)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = checker.CheckHealth(ctx)
	}
}
