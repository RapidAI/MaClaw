package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/database"
	"github.com/RapidAI/CodeClaw/corelib/security"
)

type tuiDatabaseRuntime struct {
	manager      *database.Manager
	config       func() corelib.AppConfig
	workspace    func() string
	ownerID      string
	sessionID    string
	confirmWrite func(summary string) bool
	policy       *security.PolicyEngine
}

func newTUIDatabaseRuntime(manager *database.Manager, cfg func() corelib.AppConfig, workspace func() string, ownerID, sessionID string, interactive bool) *tuiDatabaseRuntime {
	rt := &tuiDatabaseRuntime{
		manager:   manager,
		config:    cfg,
		workspace: workspace,
		ownerID:   ownerID,
		sessionID: sessionID,
		policy:    security.NewPolicyEngine(),
	}
	if interactive {
		rt.confirmWrite = tuiConfirmDatabaseWrite
	}
	if manager != nil {
		manager.SetIssueGate(rt.issueGate)
	}
	return rt
}

func (rt *tuiDatabaseRuntime) applyConfig() {
	if rt == nil || rt.manager == nil || rt.config == nil {
		return
	}
	cfg := rt.config()
	rt.manager.UpdateProfiles(cfg.DatabaseProfiles)
	rt.manager.SetEnabled(cfg.DatabaseToolIsEnabled())
	if rt.workspace != nil {
		rt.manager.SetWorkspaceRoot(rt.workspace())
	}
}

func (rt *tuiDatabaseRuntime) issueGate(req database.ApprovalRequest) error {
	if rt == nil || rt.policy == nil {
		return nil
	}
	action := rt.policy.Evaluate("database", map[string]interface{}{
		"action":          "execute",
		"profile_id":      req.ProfileID,
		"sql_fingerprint": req.SQLFingerprint,
	}, security.RiskHigh)
	if action == security.PolicyDeny {
		return fmt.Errorf("permission: denied by policy engine")
	}
	return nil
}

func (rt *tuiDatabaseRuntime) handlerCtx() agent.ToolHandlerCtx {
	return func(ctx context.Context, args map[string]interface{}) string {
		if rt == nil || rt.manager == nil {
			return "数据库连接工具未初始化。请先配置数据源 profile。"
		}
		rt.applyConfig()
		if ctx == nil {
			ctx = context.Background()
		}
		ctx = database.WithRequestScope(ctx, database.RequestScope{OwnerID: rt.ownerID, SessionID: rt.sessionID})
		ctx = rt.maybeApprove(ctx, args)
		return database.HandleTool(ctx, rt.manager, args)
	}
}

func (rt *tuiDatabaseRuntime) maybeApprove(ctx context.Context, args map[string]interface{}) context.Context {
	if rt == nil || rt.manager == nil || !database.MutationNeedsHostApproval(ctx, args) {
		return ctx
	}
	sqlFP, paramsFP, err := database.MutationFingerprints(args)
	if err != nil {
		return ctx
	}
	if rt.confirmWrite == nil || !rt.confirmWrite(sqlFP) {
		return ctx
	}
	profileID := strings.TrimSpace(fmt.Sprint(args["profile_id"]))
	if connectionID := strings.TrimSpace(fmt.Sprint(args["connection_id"])); connectionID != "" {
		if id := rt.manager.ProfileIDForConnection(connectionID); id != "" {
			profileID = id
		}
	}
	issued, err := rt.manager.IssueApproval(database.ApprovalRequest{
		ProfileID:         profileID,
		SQLFingerprint:    sqlFP,
		ParamsFingerprint: paramsFP,
		OwnerID:           rt.ownerID,
		SessionID:         rt.sessionID,
	})
	if err != nil {
		return ctx
	}
	return database.WithApprovalContext(ctx, issued)
}

func tuiConfirmDatabaseWrite(summary string) bool {
	fmt.Fprintf(os.Stderr, "\n[database] approve write fingerprint %s? [y/N]: ", summary)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false
	}
	line := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return line == "y" || line == "yes"
}
