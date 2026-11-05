package guiapp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/botcatalog"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// BotCatalogRuntime is the Wails/JSON view of per-bot skill/MCP allow-lists.
type BotCatalogRuntime = botcatalog.RuntimeView

// GetBotCatalogRuntime returns the assignments the named bot should load.
func (a *App) GetBotCatalogRuntime(botID string) (*BotCatalogRuntime, error) {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return nil, fmt.Errorf("bot id is required")
	}
	store, err := a.botCatalogAssignmentStore()
	if err != nil {
		return nil, err
	}
	view := store.RuntimeFor(botID)
	return &view, nil
}

// ActivateBotCatalogAssignments ensures assigned MCP servers that already exist
// in local config are marked AutoStart and re-synced. Skills remain global;
// the allow-list gate applies them at list/run time.
func (a *App) ActivateBotCatalogAssignments(botID string) error {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return fmt.Errorf("bot id is required")
	}
	store, err := a.botCatalogAssignmentStore()
	if err != nil {
		return err
	}
	for _, mcpID := range store.RuntimeFor(botID).MCPs {
		_ = a.ensureCatalogMCPActivated(mcpID)
	}
	return nil
}

func (a *App) ensureCatalogMCPActivated(mcpID string) error {
	mcpID = strings.TrimSpace(mcpID)
	if a == nil || mcpID == "" {
		return nil
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil
	}
	changed := false
	for i := range cfg.LocalMCPServers {
		entry := &cfg.LocalMCPServers[i]
		if strings.EqualFold(strings.TrimSpace(entry.ID), mcpID) || strings.EqualFold(strings.TrimSpace(entry.Name), mcpID) {
			if entry.Disabled {
				entry.Disabled = false
				changed = true
			}
			if !entry.AutoStart {
				entry.AutoStart = true
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	if err := a.SaveConfig(cfg); err != nil {
		return err
	}
	if a.imHandler != nil {
		if mgr := a.imHandler.getLocalMCPManager(); mgr != nil {
			mgr.SyncFromConfig()
		}
	}
	return nil
}

// ResolveCatalogBotID extracts the Hub/local bot id that owns the current turn.
// Order: explicit arg → OwnerID desktop-user:bot:<id> → session key suffix → single active desktop run.
func ResolveCatalogBotID(ownerID, sessionKey, explicitBotID string, activeBotIDs []string) string {
	if id := strings.TrimSpace(explicitBotID); id != "" {
		return id
	}
	if botID, ok := memory.ParseBotMemoryOwner(desktopUserID, strings.TrimSpace(ownerID)); ok {
		return botID
	}
	// session keys look like "alice:bot_1" / "local:bot_1"
	if sk := strings.TrimSpace(sessionKey); sk != "" {
		if i := strings.LastIndex(sk, ":"); i >= 0 && i+1 < len(sk) {
			cand := strings.TrimSpace(sk[i+1:])
			if cand != "" && !strings.Contains(cand, ":") {
				return cand
			}
		}
	}
	if len(activeBotIDs) == 1 {
		return strings.TrimSpace(activeBotIDs[0])
	}
	return ""
}

func (a *App) activeCatalogBotIDs() []string {
	if a == nil {
		return nil
	}
	activeDesktopRunsMu.Lock()
	defer activeDesktopRunsMu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, run := range activeDesktopRuns {
		if run == nil {
			continue
		}
		id := strings.TrimSpace(run.BotID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (a *App) resolveCatalogBotIDForTool(ownerID string, args map[string]interface{}) string {
	explicit := ""
	sessionKey := ""
	if args != nil {
		explicit = strings.TrimSpace(nonEmptyStringFromAny(args["bot_id"]))
		if explicit == "" {
			explicit = strings.TrimSpace(nonEmptyStringFromAny(args["catalog_bot_id"]))
		}
		sessionKey = strings.TrimSpace(nonEmptyStringFromAny(args["session_key"]))
	}
	return ResolveCatalogBotID(ownerID, sessionKey, explicit, a.activeCatalogBotIDs())
}

func (a *App) botCatalogSkillAllowed(botID, skillName string) bool {
	botID = strings.TrimSpace(botID)
	skillName = strings.TrimSpace(skillName)
	if botID == "" || skillName == "" {
		return true
	}
	store, err := a.botCatalogAssignmentStore()
	if err != nil || store == nil {
		return true
	}
	return store.Allowed(botID, botcatalog.KindSkill, skillName)
}

func (a *App) botCatalogMCPAllowed(botID, serverRef string) bool {
	botID = strings.TrimSpace(botID)
	serverRef = strings.TrimSpace(serverRef)
	if botID == "" || serverRef == "" {
		return true
	}
	store, err := a.botCatalogAssignmentStore()
	if err != nil || store == nil {
		return true
	}
	if store.Allowed(botID, botcatalog.KindMCP, serverRef) {
		return true
	}
	for _, id := range store.AssignedIDs(botID, botcatalog.KindMCP) {
		if strings.EqualFold(id, serverRef) {
			return true
		}
	}
	return false
}

// botCatalogToolRejection returns a non-empty rejection when the call is outside
// the bot's catalog allow-list. Empty means allowed / not applicable.
func (a *App) botCatalogToolRejection(ownerID, toolName, argsJSON string) string {
	if a == nil {
		return ""
	}
	toolName = strings.TrimSpace(toolName)
	var args map[string]interface{}
	if strings.TrimSpace(argsJSON) != "" {
		_ = json.Unmarshal([]byte(argsJSON), &args)
	}
	botID := a.resolveCatalogBotIDForTool(ownerID, args)
	if botID == "" {
		return ""
	}
	switch toolName {
	case "manage_skill", "run_skill":
		action := strings.TrimSpace(nonEmptyStringFromAny(args["action"]))
		name := strings.TrimSpace(nonEmptyStringFromAny(args["name"]))
		if toolName == "run_skill" || strings.EqualFold(action, "run") || strings.EqualFold(action, "info") {
			if name != "" && !a.botCatalogSkillAllowed(botID, name) {
				return fmt.Sprintf("[system rejected] bot %s is not assigned skill %s; install it in Bot Catalog first.", botID, name)
			}
		}
	case "call_mcp_tool":
		serverRef := strings.TrimSpace(nonEmptyStringFromAny(args["server_id"]))
		if serverRef != "" && !a.botCatalogMCPAllowed(botID, serverRef) {
			return fmt.Sprintf("[system rejected] bot %s is not assigned MCP %s; install it in Bot Catalog first.", botID, serverRef)
		}
	}
	return ""
}

func filterSkillsForBotCatalog(a *App, botID string, skills []NLSkillDefinition) []NLSkillDefinition {
	botID = strings.TrimSpace(botID)
	if a == nil || botID == "" || len(skills) == 0 {
		return skills
	}
	store, err := a.botCatalogAssignmentStore()
	if err != nil || store == nil || !store.HasAssignments(botID, botcatalog.KindSkill) {
		return skills
	}
	out := make([]NLSkillDefinition, 0, len(skills))
	for _, s := range skills {
		if store.Allowed(botID, botcatalog.KindSkill, s.Name) {
			out = append(out, s)
		}
	}
	return out
}
