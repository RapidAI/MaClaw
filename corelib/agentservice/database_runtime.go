package agentservice

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/database"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/security"
)

func sessionDatabaseApprovalKey(tenantID, userID, sessionID string) string {
	return strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(userID) + "\x00" + strings.TrimSpace(sessionID)
}

// IssueDatabaseApproval mints a one-time, fingerprint-bound approval through
// the user's Manager.IssueApproval (PolicyEngine gated) and stores the opaque
// token in a session envelope. HTTP handlers return only the approval ID.
func (e *CoreAgentExecutor) IssueDatabaseApproval(req ExecuteRequest, approvalReq database.ApprovalRequest) (database.ApprovalContext, error) {
	if e == nil {
		return database.ApprovalContext{}, fmt.Errorf("permission: executor is nil")
	}
	manager := e.databaseManagerForRequest(req)
	if manager == nil {
		return database.ApprovalContext{}, fmt.Errorf("permission: database manager unavailable")
	}
	if strings.TrimSpace(approvalReq.OwnerID) == "" {
		approvalReq.OwnerID = memoryOwnerIDForPrincipal(req.Principal)
	}
	if strings.TrimSpace(approvalReq.SessionID) == "" {
		approvalReq.SessionID = strings.TrimSpace(req.Session.ID)
	}
	if strings.TrimSpace(approvalReq.OperationID) == "" {
		// Bind approvals to the same stable request/session scope used by the
		// managed semantic database surface. Legacy managers keep this optional
		// metadata inert because their strict lineage gate is disabled.
		approvalReq.OperationID = firstNonEmptyDynamicOperationScope(req.Message.ID, req.Session.ID)
	}
	issued, err := manager.IssueApproval(approvalReq)
	if err != nil {
		return database.ApprovalContext{}, err
	}
	key := sessionDatabaseApprovalKey(req.Principal.TenantID, req.Principal.UserID, req.Session.ID)
	e.mu.Lock()
	if e.sessionDatabaseApprovals == nil {
		e.sessionDatabaseApprovals = make(map[string]database.ApprovalContext)
	}
	e.sessionDatabaseApprovals[key] = issued
	e.mu.Unlock()
	return issued, nil
}

// SetDatabaseEnabled applies the kill switch to every cached manager of a
// tenant user and immediately closes live connections when disabled.
func (e *CoreAgentExecutor) SetDatabaseEnabled(tenantID, userID string, enabled bool) {
	if e == nil {
		return
	}
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	if tenantID == "" || userID == "" {
		return
	}
	prefix := tenantID + "\x00" + userID + "\x00"
	e.mu.Lock()
	managers := make([]*database.Manager, 0)
	for key, manager := range e.userDatabases {
		if manager != nil && strings.HasPrefix(key, prefix) {
			managers = append(managers, manager)
		}
	}
	e.mu.Unlock()
	for _, manager := range managers {
		manager.SetEnabled(enabled)
	}
}

// SnapshotDatabaseMetrics sums process-local counters for a tenant user.
func (e *CoreAgentExecutor) SnapshotDatabaseMetrics(tenantID, userID string) database.MetricsSnapshot {
	var out database.MetricsSnapshot
	if e == nil {
		return out
	}
	prefix := strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(userID) + "\x00"
	e.mu.Lock()
	defer e.mu.Unlock()
	for key, manager := range e.userDatabases {
		if manager == nil || !strings.HasPrefix(key, prefix) {
			continue
		}
		snap := manager.SnapshotMetrics()
		out.ConnectAttempts += snap.ConnectAttempts
		out.ConnectSuccess += snap.ConnectSuccess
		out.QueryCount += snap.QueryCount
		out.Truncations += snap.Truncations
		out.Denials += snap.Denials
		out.Timeouts += snap.Timeouts
		out.ApprovalsIssued += snap.ApprovalsIssued
		out.ApprovalsDenied += snap.ApprovalsDenied
	}
	return out
}

func (e *CoreAgentExecutor) takeSessionDatabaseApproval(tenantID, userID, sessionID string) *database.ApprovalContext {
	if e == nil {
		return nil
	}
	key := sessionDatabaseApprovalKey(tenantID, userID, sessionID)
	e.mu.Lock()
	defer e.mu.Unlock()
	approval, ok := e.sessionDatabaseApprovals[key]
	if !ok {
		return nil
	}
	delete(e.sessionDatabaseApprovals, key)
	if !approval.ExpiresAt.IsZero() && time.Now().After(approval.ExpiresAt) {
		return nil
	}
	copy := approval
	return &copy
}

func (e *CoreAgentExecutor) defaultDatabaseIssueGate() database.IssueGate {
	engine := security.NewPolicyEngine()
	return func(req database.ApprovalRequest) error {
		action := engine.Evaluate("database", map[string]interface{}{
			"action":          "execute",
			"profile_id":      req.ProfileID,
			"sql_fingerprint": req.SQLFingerprint,
		}, security.RiskHigh)
		if action == security.PolicyDeny {
			return fmt.Errorf("permission: denied by policy engine")
		}
		return nil
	}
}

func (e *CoreAgentExecutor) attachSessionDatabaseApproval(ctx context.Context, req ExecuteRequest) context.Context {
	if req.DatabaseApproval != nil {
		return ctx
	}
	approval := e.takeSessionDatabaseApproval(req.Principal.TenantID, req.Principal.UserID, req.Session.ID)
	if approval == nil {
		return ctx
	}
	return database.WithApprovalContext(ctx, *approval)
}

func configureDatabaseManager(e *CoreAgentExecutor, manager *database.Manager, req ExecuteRequest) {
	manager.SetWorkspaceRoot(req.Instance.Workspace)
	manager.SetEnabled(req.Config.DatabaseToolIsEnabled())
	manager.SetIssueGate(e.defaultDatabaseIssueGate())
	dataDir := strings.TrimSpace(req.DataDir)
	if dataDir == "" {
		return
	}
	pendingPath := filepath.Join(dataDir, "database_pending.json")
	if store, err := database.NewPendingStore(pendingPath); err == nil {
		manager.SetPendingStore(store)
	} else {
		log.Printf("[agentservice] WARNING: database pending store init failed path=%s: %v", pendingPath, err)
	}
	manager.SetResultStoreDir(filepath.Join(dataDir, "database_results"))
	manager.SetTunnelDialer(e.DatabaseTunnelDialer(req.Principal.TenantID, req.Principal.UserID))
	if store, err := database.NewFavoriteStore(filepath.Join(dataDir, "database_favorites.json")); err == nil {
		manager.SetFavoriteStore(store)
	} else {
		log.Printf("[agentservice] WARNING: database favorites store init failed path=%s: %v", filepath.Join(dataDir, "database_favorites.json"), err)
	}
}

// DatabaseTunnelDialer reuses the tenant user's already-approved SSH sessions
// for database profiles that bind ssh_session_id. It never opens SSH itself.
func (e *CoreAgentExecutor) DatabaseTunnelDialer(tenantID, userID string) database.TunnelDialer {
	if e == nil {
		return nil
	}
	return remote.SSHTunnelDialer(func() *remote.SSHSessionManager {
		resources := e.sshResourcesForUser(tenantID, userID)
		if resources == nil {
			return nil
		}
		return resources.mgr
	})
}
