package main

import (
	"context"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// TestManagerCreationPerformance tests the performance of manager creation
func TestManagerCreationPerformance(t *testing.T) {
	ctx := context.Background()

	// Test original App creation (simulated)
	t.Run("OriginalAppCreation", func(t *testing.T) {
		start := time.Now()
		for i := 0; i < 1000; i++ {
			// Simulate the old way - creating all components directly
			app := &App{
				// This would have been the old monolithic initialization
				remoteSessions:        &RemoteSessionManager{},
				browserSessions:       &BrowserAgentManager{},
				mcpRegistry:           &MCPRegistry{},
				localMCPManager:       &LocalMCPManager{},
				skillExecutor:         &SkillExecutor{},
				skillRunner:           &SkillRunner{},
				sessionStarter:        &CodingSessionStarter{},
				skillMarketClient:     &SkillMarketClient{},
				gossipClient:          &GossipClient{},
				gossipAutoPublish:     &AutoPublishTrigger{},
				riskAssessor:          &RiskAssessor{},
				policyEngine:          &PolicyEngine{},
				auditLog:              &AuditLog{},
				llmSecurityReview:     &LLMSecurityReview{},
				mdnsScanner:           &MDNSScanner{},
				projectScanner:        &ProjectScanner{},
				toolDefGenerator:      &ToolDefinitionGenerator{},
				toolRouter:            &ToolRouter{},
				usageTracker:          &coretool.UsageTracker{},
				experienceExtractor:   &ExperienceExtractor{},
				orchestrator:          &Orchestrator{},
				sharedContext:         &SharedContextStore{},
				toolSelector:          &ToolSelector{},
				skillHubClient:        &SkillHubClient{},
				capabilityGapDetector: &CapabilityGapDetector{},
			}
			_ = app
		}
		duration := time.Since(start)
		t.Logf("Original App creation (1000 iterations): %v", duration)
	})

	// Test new manager-based creation
	t.Run("ManagerBasedCreation", func(t *testing.T) {
		start := time.Now()
		for i := 0; i < 1000; i++ {
			managers := NewAppManagers(ctx)
			_ = managers
		}
		duration := time.Since(start)
		t.Logf("Manager-based creation (1000 iterations): %v", duration)
	})
}

// TestManagerAccessPerformance tests the performance of accessing components
func TestManagerAccessPerformance(t *testing.T) {
	ctx := context.Background()
	managers := NewAppManagers(ctx)

	// Test direct field access (old way)
	t.Run("DirectFieldAccess", func(t *testing.T) {
		app := &App{
			remoteSessions:  &RemoteSessionManager{},
			browserSessions: &BrowserAgentManager{},
			mcpRegistry:     &MCPRegistry{},
		}

		start := time.Now()
		for i := 0; i < 10000; i++ {
			_ = app.remoteSessions
			_ = app.browserSessions
			_ = app.mcpRegistry
		}
		duration := time.Since(start)
		t.Logf("Direct field access (10000 iterations): %v", duration)
	})

	// Test manager-based access (new way)
	t.Run("ManagerBasedAccess", func(t *testing.T) {
		start := time.Now()
		for i := 0; i < 10000; i++ {
			// Use the actual manager access methods
			_ = managers.core.GetMCPRegistry()
			_ = managers.sessions.GetRemoteSessions()
			_ = managers.sessions.GetBrowserSessions()
		}
		duration := time.Since(start)
		t.Logf("Manager-based access (10000 iterations): %v", duration)
	})
}

// TestInitializationPerformance compares sequential vs parallel initialization
func TestInitializationPerformance(t *testing.T) {
	config := DefaultManagerConfig()

	// Test sequential initialization
	t.Run("SequentialInitialization", func(t *testing.T) {
		start := time.Now()
		for i := 0; i < 100; i++ {
			ctx := context.Background()
			managers := NewAppManagers(ctx)
			initializer := NewOptimizedAppInitializer(config)
			initializer.SetParallel(false)
			_ = initializer.InitializeManagers(ctx, managers)
		}
		duration := time.Since(start)
		t.Logf("Sequential initialization (100 iterations): %v", duration)
	})

	// Test parallel initialization
	t.Run("ParallelInitialization", func(t *testing.T) {
		start := time.Now()
		for i := 0; i < 100; i++ {
			ctx := context.Background()
			managers := NewAppManagers(ctx)
			initializer := NewOptimizedAppInitializer(config)
			initializer.SetParallel(true)
			_ = initializer.InitializeManagers(ctx, managers)
		}
		duration := time.Since(start)
		t.Logf("Parallel initialization (100 iterations): %v", duration)
	})
}

// TestMemoryUsage compares memory usage between old and new approaches
func TestMemoryUsage(t *testing.T) {
	ctx := context.Background()

	// Test memory usage of old approach
	t.Run("OldApproachMemory", func(t *testing.T) {
		var apps []*App
		for i := 0; i < 1000; i++ {
			app := &App{
				remoteSessions:        &RemoteSessionManager{},
				browserSessions:       &BrowserAgentManager{},
				mcpRegistry:           &MCPRegistry{},
				localMCPManager:       &LocalMCPManager{},
				skillExecutor:         &SkillExecutor{},
				skillRunner:           &SkillRunner{},
				sessionStarter:        &CodingSessionStarter{},
				skillMarketClient:     &SkillMarketClient{},
				gossipClient:          &GossipClient{},
				gossipAutoPublish:     &AutoPublishTrigger{},
				riskAssessor:          &RiskAssessor{},
				policyEngine:          &PolicyEngine{},
				auditLog:              &AuditLog{},
				llmSecurityReview:     &LLMSecurityReview{},
				mdnsScanner:           &MDNSScanner{},
				projectScanner:        &ProjectScanner{},
				toolDefGenerator:      &ToolDefinitionGenerator{},
				toolRouter:            &ToolRouter{},
				usageTracker:          &coretool.UsageTracker{},
				experienceExtractor:   &ExperienceExtractor{},
				orchestrator:          &Orchestrator{},
				sharedContext:         &SharedContextStore{},
				toolSelector:          &ToolSelector{},
				skillHubClient:        &SkillHubClient{},
				capabilityGapDetector: &CapabilityGapDetector{},
			}
			apps = append(apps, app)
		}
		t.Logf("Old approach: Created %d App instances", len(apps))
	})

	// Test memory usage of new approach
	t.Run("NewApproachMemory", func(t *testing.T) {
		var managers []*AppManagers
		for i := 0; i < 1000; i++ {
			manager := NewAppManagers(ctx)
			managers = append(managers, manager)
		}
		t.Logf("New approach: Created %d AppManagers instances", len(managers))
	})
}

// TestConcurrentManagerAccess tests concurrent access to managers
func TestConcurrentManagerAccess(t *testing.T) {
	ctx := context.Background()
	managers := NewAppManagers(ctx)

	t.Run("ConcurrentManagerAccess", func(t *testing.T) {
		start := time.Now()

		done := make(chan bool, 10)
		for i := 0; i < 10; i++ {
			go func() {
				for j := 0; j < 1000; j++ {
					_ = managers.sessions.GetRemoteSessions()
					_ = managers.sessions.GetBrowserSessions()
					_ = managers.core.GetMCPRegistry()
					_ = managers.core.GetLocalMCPManager()
					_ = managers.core.GetToolDefGenerator()
					_ = managers.core.GetToolRouter()
					_ = managers.core.GetUsageTracker()
					_ = managers.security.GetRiskAssessor()
					_ = managers.security.GetPolicyEngine()
					_ = managers.security.GetAuditLog()
				}
				done <- true
			}()
		}

		// Wait for all goroutines to complete
		for i := 0; i < 10; i++ {
			<-done
		}

		duration := time.Since(start)
		t.Logf("Concurrent manager access (10 goroutines x 1000 operations): %v", duration)
	})
}

// TestHealthCheckPerformance tests the performance of health checking
func TestHealthCheckPerformance(t *testing.T) {
	ctx := context.Background()
	managers := NewMockAppManagers()
	checker := NewHealthChecker(managers, 100*time.Millisecond, 5*time.Second)

	t.Run("HealthCheckPerformance", func(t *testing.T) {
		start := time.Now()
		for i := 0; i < 1000; i++ {
			_ = checker.CheckHealth(ctx)
		}
		duration := time.Since(start)
		t.Logf("Health check (1000 iterations): %v", duration)
	})
}

// TestMetricsCollectionPerformance tests the performance of metrics collection
func TestMetricsCollectionPerformance(t *testing.T) {
	config := DefaultManagerConfig()
	ctx := context.Background()
	managers := NewAppManagers(ctx)
	initializer := NewOptimizedAppInitializer(config)
	_ = initializer.InitializeManagers(ctx, managers)

	t.Run("MetricsCollectionPerformance", func(t *testing.T) {
		start := time.Now()
		for i := 0; i < 1000; i++ {
			_ = initializer.GetMetrics()
		}
		duration := time.Since(start)
		t.Logf("Metrics collection (1000 iterations): %v", duration)
	})
}
