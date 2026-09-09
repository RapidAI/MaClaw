package guiapp

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/database"
)

func appConfigDatabaseProfiles(app *App) []database.Profile {
	if app == nil {
		return nil
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		return nil
	}
	return cfg.DatabaseProfiles
}

func refreshDatabaseManager(app *App, manager *database.Manager) {
	if app == nil || manager == nil {
		return
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		// Never erase live profiles because a transient config read failed.
		return
	}
	manager.UpdateProfiles(cfg.DatabaseProfiles)
	manager.SetWorkspaceRoot(app.GetCurrentProjectPath())
	publishDatabaseProfileSearchText(app.toolRouter, manager)
}

func publishDatabaseProfileSearchText(router *ToolRouter, manager *database.Manager) {
	if router == nil || manager == nil {
		return
	}
	tokens := manager.SearchTokens()
	router.SetSearchTextExtra("database", tokens)
	router.SetSearchTextExtra("database_query", tokens)
}

func databaseKeyringSecret(ctx context.Context, ref string) (string, error) {
	u, err := url.Parse(ref)
	if err != nil || u.Scheme != "keyring" || u.Host == "" || strings.Trim(u.Path, "/") == "" {
		return "", fmt.Errorf("invalid secret_ref")
	}
	return databaseKeyringGet(u.Host, strings.Trim(u.Path, "/"))
}

// toolDatabase is the GUI host adapter for the shared database contract. It
// deliberately keeps credentials and DSNs out of model arguments; profiles
// are loaded from AppConfig and keyring-backed secret_ref values are resolved
// by the GUI host. Excel actions continue to use the hardened office
// implementation.
func (h *IMMessageHandler) toolDatabase(args map[string]interface{}) string {
	return h.toolDatabaseWithContext(context.Background(), args)
}

// toolDatabaseWithContext is the context-aware GUI adapter used by the
// registered-tool dispatcher. Approval context is injected by the trusted
// task-panel/host path and consumed only by corelib/database; it is never
// copied into model arguments.
func (h *IMMessageHandler) toolDatabaseWithContext(ctx context.Context, args map[string]interface{}) string {
	if h == nil {
		return "数据库连接工具未初始化。请先配置数据源 profile。"
	}
	refreshDatabaseManager(h.app, h.databaseManager)
	if args == nil {
		args = map[string]interface{}{}
	}
	for key := range args {
		if strings.EqualFold(strings.TrimSpace(key), "approval_token") {
			return "Error: approval_token must be supplied by the trusted host approval context, not tool arguments"
		}
	}
	if msg, ok := h.emitDatabaseMutationApprovalIfNeeded(ctx, args); ok {
		return msg
	}
	action := stringVal(args, "action")
	var result string
	switch {
	case action == "write_table" || action == "export_excel":
		// Spreadsheet mutations use the same context-bound approval and audit
		// contract as SQL mutations. Do not route them through the legacy office
		// handler, which cannot see the host-injected database approval context.
		result = database.HandleTool(ctx, h.databaseManager, args)
	case action == "read_table":
		// A configured database manager is the authoritative owner/session and
		// profile-policy boundary. Route profile-bound reads through the shared
		// core implementation so GUI cannot bypass AllowedOperations or path
		// isolation by falling back to the legacy Office handler. Keep the
		// compatibility fallback only for callers that construct a handler
		// without a database manager (older tests/embedded integrations).
		if h.databaseManager != nil {
			result = database.HandleTool(ctx, h.databaseManager, args)
		} else {
			result = h.toolDatabaseExcel(action, args)
		}
	default:
		result = database.HandleTool(ctx, h.databaseManager, args)
	}
	// The model often list_connections / connect instead of propose_profile.
	// Open the password form whenever the host knows a target and no profile
	// exists yet; waiting for the model to call propose_profile leaves the
	// task panel empty.
	h.emitDatabaseProposeProfileAgentViewIfNeeded(args, result)
	// Inspect/connect remember catalog names during HandleTool. Publish after
	// that write so the next user turn's router snapshot includes them.
	h.publishDatabaseSearchText()
	return result
}

func (h *IMMessageHandler) publishDatabaseSearchText() {
	if h == nil {
		return
	}
	router := h.toolRouter
	if router == nil && h.app != nil {
		router = h.app.toolRouter
	}
	publishDatabaseProfileSearchText(router, h.databaseManager)
}

func (h *IMMessageHandler) emitDatabaseProposeProfileAgentViewIfNeeded(args map[string]interface{}, result string) {
	if h == nil || h.app == nil {
		return
	}
	var draft map[string]interface{}
	if parsed, ok := parseDatabaseNeedsSecretDraft(result); ok {
		draft = databaseProposeDraftFromArgs(parsed)
	} else if databaseResultNeedsHostSecretForm(args, result) {
		draft = databaseProposeDraftFromArgs(args)
	} else {
		return
	}
	if agentViewNonEmpty(draft["host"]) == "" && agentViewNonEmpty(draft["file_path"]) == "" {
		return
	}
	opened := h.app.emitAgentView(buildDatabaseProposeProfileAgentView(draft))
	log.Printf("[agent-view] database password form emit ok=%v host=%s action=%s", opened, agentViewNonEmpty(draft["host"]), stringVal(args, "action"))
}

func (h *IMMessageHandler) toolDatabaseExcel(action string, args map[string]interface{}) string {
	// The GUI's existing tool-policy/approval pipeline governs office writes.
	// Keep this adapter a thin projection so it does not invent a second token
	// transport (database.HandleTool uses the trusted context transport).
	switch action {
	case "read_table":
		args["action"] = "read_excel"
		return h.toolOffice(args)
	case "write_table":
		args["action"] = "write_excel"
		return h.toolOffice(args)
	case "export_excel":
		args["action"] = "write_excel"
		if args["data"] == nil && args["rows"] != nil {
			args["data"] = map[string]interface{}{"sheets": []interface{}{map[string]interface{}{"name": stringVal(args, "sheet"), "rows": args["rows"]}}}
		}
		return h.toolOffice(args)
	default:
		return "unsupported database Excel action"
	}
}

// issueDatabaseApprovalContext mints a strict, fingerprint-bound one-time
// approval for an approved database mutation. Issuance binds the exact SQL/
// parameter fingerprints (the same canonical form the handler validates) and
// the live profile schema version, so a stale preview can never be committed.
// When the manager has no pending store configured (or the action is not a
// mutation), it falls back to the generic in-memory execution token so hosts
// without the store keep working.
func issueDatabaseApprovalContext(ctx context.Context, manager *database.Manager, approval registeredToolPendingApproval, scope database.RequestScope) context.Context {
	legacy := func() context.Context {
		if strings.TrimSpace(approval.ExecutionToken) == "" {
			return ctx
		}
		return database.WithApprovalContext(ctx, database.ApprovalContext{
			Token: approval.ExecutionToken,
			ID:    approval.ID,
		})
	}
	sqlFingerprint, paramsFingerprint, err := database.MutationFingerprints(approval.Args)
	if err != nil {
		// Not a mutation action (or malformed args): nothing to bind. The
		// handler fail-closes any real commit without a valid context.
		return legacy()
	}
	profileID := ""
	if connectionID := stringVal(approval.Args, "connection_id"); connectionID != "" {
		profileID = manager.ProfileIDForConnection(connectionID)
	}
	issued, err := manager.IssueApproval(database.ApprovalRequest{
		ProfileID:         profileID,
		SQLFingerprint:    sqlFingerprint,
		ParamsFingerprint: paramsFingerprint,
		OwnerID:           scope.OwnerID,
		SessionID:         scope.SessionID,
	})
	if err != nil {
		log.Printf("[agent_view] WARNING: database approval issuance failed, falling back to in-memory token: %v", err)
		return legacy()
	}
	return database.WithApprovalContext(ctx, issued)
}
