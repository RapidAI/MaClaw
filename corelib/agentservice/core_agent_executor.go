package agentservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agent/sshtool"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/task"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

const (
	metaResponseSource     = "response_source"
	metaAskUserQuestion    = "ask_user_question"
	metaAskUserInputType   = "ask_user_input_type"
	metaAskUserOptionsJSON = "ask_user_options_json"
)

type CoreAgentExecutor struct {
	HTTPClient                 *http.Client
	AllowLocalBash             bool
	LocalBashTrustedSingleUser bool
	LocalBashTenantID          string
	LocalBashUserID            string
	AllowDirectSSH             bool
	AllowSSHFileTransfer       bool

	mu             sync.Mutex
	userMemory     map[string]*memory.Store
	tasks          map[string]*task.Store
	userSSH        map[string]*coreAgentSSHResources
	knowledgeStore KnowledgeStore
	mcpProvider    MCPToolProvider
	skillProvider  SkillToolProvider
}

type coreAgentSSHResources struct {
	mgr *remote.SSHSessionManager
	bg  *remote.SSHBackgroundTaskManager
}

type coreAgentCallbacks struct {
	ctx                        context.Context
	appCfg                     corelib.AppConfig
	llmCfg                     corelib.MaclawLLMConfig
	principal                  Principal
	userText                   string
	workspace                  string
	dataDir                    string
	allowLocalBash             bool
	localBashTrustedSingleUser bool
	localBashTenantID          string
	localBashUserID            string
	allowDirectSSH             bool
	allowSSHFileTransfer       bool
	memory                     *memory.Store
	tasks                      *task.Store
	sshDeps                    sshtool.SSHToolDeps
	httpClient                 *http.Client
	toolPolicy                 workflow.ToolFilterPolicy
	opsApprovedCommands        []workflow.OpsApprovedCommand
	knowledgeStore             KnowledgeStore
	mcpProvider                MCPToolProvider
	skillProvider              SkillToolProvider
}

func (e *CoreAgentExecutor) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	llmCfg, err := ResolveLLMConfig(req.Config)
	if err != nil {
		return nil, err
	}
	resources, err := e.resourcesForUser(req.Principal.TenantID, req.Principal.UserID, req.DataDir)
	if err != nil {
		return nil, err
	}
	sshResources := e.sshResourcesForUser(req.Principal.TenantID, req.Principal.UserID)
	taskStore := e.taskStoreForSession(req.Session.ID)
	cb := &coreAgentCallbacks{
		ctx:                  ctx,
		appCfg:               req.Config,
		llmCfg:               llmCfg,
		principal:            req.Principal,
		userText:             req.Message.Content,
		workspace:            req.Instance.Workspace,
		dataDir:              req.DataDir,
		allowLocalBash:       e.AllowLocalBash,
		localBashTenantID:    strings.TrimSpace(e.LocalBashTenantID),
		localBashUserID:      strings.TrimSpace(e.LocalBashUserID),
		allowDirectSSH:       e.AllowDirectSSH,
		allowSSHFileTransfer: e.AllowSSHFileTransfer,
		memory:               resources,
		tasks:                taskStore,
		knowledgeStore:       e.knowledgeStore,
		mcpProvider:          e.mcpProvider,
		skillProvider:        e.skillProvider,
		sshDeps: sshtool.SSHToolDeps{
			Manager:   sshResources.mgr,
			BGTaskMgr: sshResources.bg,
			HostLoader: func() []corelib.SSHHostEntry {
				return req.Config.SSHHosts
			},
		},
		httpClient: e.clientFor(llmCfg),
		toolPolicy: req.ToolPolicy,
		opsApprovedCommands: append([]workflow.OpsApprovedCommand(nil),
			req.OpsApprovedCommands...),
	}
	result := agent.RunLoop(cb, req.Message.Content, convertHistoryToEntries(req.History, req.Message.ID), cb.httpClient)
	if result.Error != "" {
		return nil, errors.New(result.Error)
	}
	metadata := map[string]string{
		"executor": "core_agent",
		"agent_id": req.Session.AgentID,
		"provider": llmCfg.ProviderName,
		"model":    llmCfg.Model,
		"protocol": llmCfg.Protocol,
		"wire_api": llmCfg.WireAPI,
	}
	if result.HardExit {
		metadata["hard_exit"] = "true"
	}
	if result.AskUser != nil {
		metadata[metaResponseSource] = string(responseSourceAskUser)
		metadata[metaAskUserQuestion] = result.AskUser.Question
		metadata[metaAskUserInputType] = result.AskUser.InputType
		if len(result.AskUser.Options) > 0 {
			if data, err := json.Marshal(result.AskUser.Options); err == nil {
				metadata[metaAskUserOptionsJSON] = string(data)
			}
		}
	}
	return &ExecuteResult{Content: result.Text, OutputType: "text/plain", Metadata: metadata}, nil
}

func (e *CoreAgentExecutor) clientFor(cfg corelib.MaclawLLMConfig) *http.Client {
	if e.HTTPClient != nil {
		return e.HTTPClient
	}
	return &http.Client{Timeout: time.Duration(cfg.EffectiveTimeoutSec()) * time.Second}
}

func (e *CoreAgentExecutor) resourcesForUser(tenantID, userID, dataDir string) (*memory.Store, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.userMemory == nil {
		e.userMemory = map[string]*memory.Store{}
	}
	key := tenantID + ":" + userID
	if store := e.userMemory[key]; store != nil {
		return store, nil
	}
	store, err := memory.NewStore(filepath.Join(dataDir, "agent_memory.json"))
	if err != nil {
		return nil, err
	}
	e.userMemory[key] = store
	return store, nil
}

func (e *CoreAgentExecutor) taskStoreForSession(sessionID string) *task.Store {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.tasks == nil {
		e.tasks = map[string]*task.Store{}
	}
	if store := e.tasks[sessionID]; store != nil {
		return store
	}
	store := task.NewStore()
	e.tasks[sessionID] = store
	return store
}

func (e *CoreAgentExecutor) sshResourcesForUser(tenantID, userID string) *coreAgentSSHResources {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.userSSH == nil {
		e.userSSH = map[string]*coreAgentSSHResources{}
	}
	key := tenantID + ":" + userID
	if resources := e.userSSH[key]; resources != nil {
		return resources
	}
	mgr := remote.NewSSHSessionManager(remote.NewSSHPool())
	resources := &coreAgentSSHResources{
		mgr: mgr,
		bg:  remote.NewSSHBackgroundTaskManager(mgr),
	}
	e.userSSH[key] = resources
	return resources
}

func convertHistoryToEntries(history []Message, currentID string) []agent.ConversationEntry {
	entries := make([]agent.ConversationEntry, 0, len(history))
	for _, msg := range history {
		if msg.ID == currentID {
			continue
		}
		role := strings.TrimSpace(string(msg.Role))
		if role == "" {
			continue
		}
		entries = append(entries, agent.ConversationEntry{Role: role, Content: msg.Content})
	}
	return entries
}

func (c *coreAgentCallbacks) GetLLMConfig() corelib.MaclawLLMConfig { return c.llmCfg }

func (c *coreAgentCallbacks) GetMaxIterations() int {
	return config.EffectiveMaxIterations(c.appCfg.MaclawAgentMaxIterations)
}

func (c *coreAgentCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	roleName := strings.TrimSpace(c.appCfg.MaclawRoleName)
	if roleName == "" {
		roleName = "MaClaw"
	}
	roleDescription := strings.TrimSpace(c.appCfg.MaclawRoleDescription)
	if roleDescription == "" {
		roleDescription = "A REST-served MaClaw agent runtime for end-user assistance."
	}
	basePrompt := agent.BuildSystemPrompt(agent.SystemPromptDeps{
		Config: agent.SystemPromptConfig{
			RoleName:        roleName,
			RoleDescription: roleDescription,
			IsProMode:       false,
		},
		MemoryStore: c.memory,
	}, userText, isFirstTurn)

	// Append knowledge auto-recall if knowledge store is available.
	// Only on first turn — subsequent iterations have the same user text,
	// and knowledge results are already in the prompt from turn 1.
	if c.knowledgeStore != nil && userText != "" && isFirstTurn {
		var b strings.Builder
		b.WriteString(basePrompt)
		c.appendKnowledgeAutoRecall(&b, userText)
		return b.String()
	}
	return basePrompt
}

type coreToolSpec struct {
	Name           string
	Description    string
	Parameters     map[string]interface{}
	Enabled        bool
	DisabledReason string
}

func (e *CoreAgentExecutor) DescribeCapabilities(ctx context.Context, req ExecuteRequest) (*AgentCapabilities, error) {
	_ = ctx
	cb := &coreAgentCallbacks{appCfg: req.Config, principal: req.Principal, workspace: req.Instance.Workspace, dataDir: req.DataDir, allowLocalBash: e.AllowLocalBash, localBashTrustedSingleUser: e.LocalBashTrustedSingleUser, localBashTenantID: strings.TrimSpace(e.LocalBashTenantID), localBashUserID: strings.TrimSpace(e.LocalBashUserID), allowDirectSSH: e.AllowDirectSSH, allowSSHFileTransfer: e.AllowSSHFileTransfer}
	return &AgentCapabilities{
		Executor:          "core_agent",
		SupportsSessions:  true,
		SupportsAskUser:   true,
		SupportsSSH:       cb.canUseSSH(),
		SupportsLocalBash: cb.canUseLocalBash(),
		Tools:             cb.toolCapabilities(),
		Metadata: map[string]string{
			"workspace_dir":              req.Instance.Workspace,
			"bash_enabled":               boolString(cb.canUseLocalBash()),
			"bash_scope_tenant_id":       strings.TrimSpace(e.LocalBashTenantID),
			"bash_scope_user_id":         strings.TrimSpace(e.LocalBashUserID),
			"bash_trusted_single_user":   boolString(e.LocalBashTrustedSingleUser),
			"ssh_direct_connect_enabled": boolString(e.AllowDirectSSH),
			"ssh_file_transfer_enabled":  boolString(e.AllowSSHFileTransfer),
		},
	}, nil
}

func (c *coreAgentCallbacks) coreToolSpecs() []coreToolSpec {
	return []coreToolSpec{
		{
			Name:        "bash",
			Description: bashToolDescription(c.localBashTenantID, c.localBashUserID),
			Enabled:     c.canUseLocalBash(),
			DisabledReason: func() string {
				if !c.allowLocalBash {
					return "local bash is disabled for this MaClawSrv deployment"
				}
				if !c.canUseLocalBash() {
					return c.localBashDeniedReason()
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command":     map[string]interface{}{"type": "string"},
					"working_dir": map[string]interface{}{"type": "string"},
					"timeout":     map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 120},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "ssh",
			Description: sshToolDescription(c.allowDirectSSH, c.allowSSHFileTransfer, len(c.appCfg.SSHHosts) > 0),
			Enabled:     c.canUseSSH(),
			DisabledReason: func() string {
				if !c.canUseSSH() {
					return c.sshDeniedReason()
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action":          map[string]interface{}{"type": "string", "enum": sshAllowedActions(c.allowSSHFileTransfer)},
					"host":            map[string]interface{}{"type": "string"},
					"user":            map[string]interface{}{"type": "string"},
					"port":            map[string]interface{}{"type": "integer"},
					"auth_method":     map[string]interface{}{"type": "string", "enum": []string{"password", "key", "agent"}},
					"key_path":        map[string]interface{}{"type": "string"},
					"password":        map[string]interface{}{"type": "string"},
					"label":           map[string]interface{}{"type": "string"},
					"initial_command": map[string]interface{}{"type": "string"},
					"force_new":       map[string]interface{}{"type": "boolean"},
					"session_id":      map[string]interface{}{"type": "string"},
					"command":         map[string]interface{}{"type": "string"},
					"wait_seconds":    map[string]interface{}{"type": "integer"},
					"task_id":         map[string]interface{}{"type": "string"},
					"tail_lines":      map[string]interface{}{"type": "integer"},
					"local_path":      map[string]interface{}{"type": "string"},
					"remote_path":     map[string]interface{}{"type": "string"},
				},
				"required": []string{"action"},
			},
		},
		{
			Name:        "ask_user",
			Description: "Ask the user a structured follow-up question when you cannot proceed safely without input.",
			Enabled:     true,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"question":   map[string]interface{}{"type": "string"},
					"input_type": map[string]interface{}{"type": "string", "enum": []string{"text", "choice", "confirm"}},
					"context":    map[string]interface{}{"type": "string"},
					"options": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
				},
				"required": []string{"question"},
			},
		},
		{
			Name:        "task",
			Description: "Manage the agent's internal task checklist for multi-step work.",
			Enabled:     true,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action":      map[string]interface{}{"type": "string", "enum": []string{"create", "update", "complete", "fail", "list", "delete"}},
					"title":       map[string]interface{}{"type": "string"},
					"description": map[string]interface{}{"type": "string"},
					"task_id":     map[string]interface{}{"type": "string"},
					"status":      map[string]interface{}{"type": "string", "enum": []string{"pending", "in_progress", "completed", "failed", "blocked"}},
					"status_note": map[string]interface{}{"type": "string"},
					"depends_on": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
				},
				"required": []string{"action"},
			},
		},
		{
			Name:        "knowledge_search",
			Description: "Search the local knowledge base (documents, URLs, saved text). Returns ranked knowledge cards, facts, and source citations without calling an LLM. Use when the user asks about saved knowledge, imported documents, or previously stored information.",
			Enabled:     c.knowledgeStore != nil,
			DisabledReason: func() string {
				if c.knowledgeStore == nil {
					return "knowledge base is not configured"
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query":        map[string]interface{}{"type": "string", "description": "Search query"},
					"search_scope": map[string]interface{}{"type": "string", "description": "all | project | personal. Default all."},
					"topic_hint":   map[string]interface{}{"type": "string", "description": "Optional topic hint for local re-ranking."},
					"source_kinds": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional: url, pdf, docx, xlsx, csv, markdown, text"},
					"labels":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional source labels to filter by."},
					"limit":        map[string]interface{}{"type": "integer", "description": "Max results, default 8, max 50"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "knowledge_context_pack",
			Description: "Build a compact, citation-backed knowledge context pack from the local knowledge base. Use before answering from stored knowledge when you need a prompt-ready bundle of ranked cards and facts under a character budget.",
			Enabled:     c.knowledgeStore != nil,
			DisabledReason: func() string {
				if c.knowledgeStore == nil {
					return "knowledge base is not configured"
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query":     map[string]interface{}{"type": "string", "description": "Search query for the context pack"},
					"max_items": map[string]interface{}{"type": "integer", "description": "Max items in pack, default 10"},
					"max_chars": map[string]interface{}{"type": "integer", "description": "Max characters in pack, default 4000"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "knowledge_save_url",
			Description: "Save a URL to the knowledge base. The content will be fetched, parsed, and indexed for future retrieval.",
			Enabled:     c.knowledgeStore != nil,
			DisabledReason: func() string {
				if c.knowledgeStore == nil {
					return "knowledge base is not configured"
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url":        map[string]interface{}{"type": "string", "description": "URL to save"},
					"title":      map[string]interface{}{"type": "string", "description": "Optional title override"},
					"topic_hint": map[string]interface{}{"type": "string", "description": "Optional topic hint for better indexing"},
				},
				"required": []string{"url"},
			},
		},
		{
			Name:        "knowledge_save_text",
			Description: "Save text or markdown content to the knowledge base for future retrieval.",
			Enabled:     c.knowledgeStore != nil,
			DisabledReason: func() string {
				if c.knowledgeStore == nil {
					return "knowledge base is not configured"
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text":       map[string]interface{}{"type": "string", "description": "Text content to save"},
					"title":      map[string]interface{}{"type": "string", "description": "Optional title"},
					"topic_hint": map[string]interface{}{"type": "string", "description": "Optional topic hint for better indexing"},
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        "memory",
			Description: memory.ToolDefinitionSchema().Description,
			Enabled:     c.memory != nil,
			DisabledReason: func() string {
				if c.memory == nil {
					return "memory store is not initialized"
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": memory.ToolDefinitionSchema().Properties,
				"required":   memory.ToolDefinitionSchema().Required,
			},
		},

		{
			Name:        "read_file",
			Description: "Read the contents of a file. Supports line ranges (start_line, lines) and tail reading (offset). Files are scoped to the instance workspace.",
			Enabled:     c.workspace != "",
			DisabledReason: func() string {
				if c.workspace == "" {
					return "no workspace configured for this instance"
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":       map[string]interface{}{"type": "string", "description": "File path (relative to workspace or absolute within workspace)"},
					"start_line": map[string]interface{}{"type": "integer", "description": "Start reading from this line number (1-based)"},
					"lines":      map[string]interface{}{"type": "integer", "description": "Maximum number of lines to return"},
					"offset":     map[string]interface{}{"type": "integer", "description": "Read last N lines from end (like tail -n). Mutually exclusive with start_line/lines."},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "write_file",
			Description: "Write content to a file. Supports overwrite (default) and append mode. Files are scoped to the instance workspace. Content is always UTF-8.",
			Enabled:     c.workspace != "",
			DisabledReason: func() string {
				if c.workspace == "" {
					return "no workspace configured for this instance"
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":    map[string]interface{}{"type": "string", "description": "File path (relative to workspace or absolute within workspace)"},
					"content": map[string]interface{}{"type": "string", "description": "Content to write"},
					"mode":    map[string]interface{}{"type": "string", "description": "Write mode: overwrite (default) or append"},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "edit_file",
			Description: "Edit a file by replacing a specific text occurrence. Use old_string to find the exact text and new_string to replace it. Files are scoped to the instance workspace.",
			Enabled:     c.workspace != "",
			DisabledReason: func() string {
				if c.workspace == "" {
					return "no workspace configured for this instance"
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":        map[string]interface{}{"type": "string", "description": "File path (relative to workspace or absolute within workspace)"},
					"old_string":  map[string]interface{}{"type": "string", "description": "Exact text to find and replace"},
					"new_string":  map[string]interface{}{"type": "string", "description": "Replacement text"},
					"replace_all": map[string]interface{}{"type": "boolean", "description": "Replace all occurrences (default: first only)"},
				},
				"required": []string{"path", "old_string", "new_string"},
			},
		},
		{
			Name:        "list_directory",
			Description: "List the contents of a directory. Shows files and subdirectories with sizes. Scoped to the instance workspace.",
			Enabled:     c.workspace != "",
			DisabledReason: func() string {
				if c.workspace == "" {
					return "no workspace configured for this instance"
				}
				return ""
			}(),
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{"type": "string", "description": "Directory path (relative to workspace or absolute within workspace). Defaults to workspace root."},
				},
			},
		},
		{
			Name:        "web_search",
			Description: "Search the internet for information. Returns a list of results with title, URL, and snippet. Useful for finding documentation, latest information, technical references.",
			Enabled:     true,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query":       map[string]interface{}{"type": "string", "description": "Search keywords"},
					"max_results": map[string]interface{}{"type": "integer", "description": "Maximum results (default 8, max 20)"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "web_fetch",
			Description: "Fetch and extract text content from a URL. Supports automatic encoding detection (GBK/UTF-8), HTML body extraction. Long pages support continuation: when has_more=true, pass offset=next_offset to read more.",
			Enabled:     true,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url":       map[string]interface{}{"type": "string", "description": "URL to fetch (http/https)"},
					"offset":    map[string]interface{}{"type": "integer", "description": "Character offset for continuation reading (from previous next_offset)"},
					"max_chars": map[string]interface{}{"type": "integer", "description": "Maximum characters to return (default 16384)"},
					"timeout":   map[string]interface{}{"type": "integer", "description": "Timeout in seconds (default 30)"},
				},
				"required": []string{"url"},
			},
		},
	}
}

func (c *coreAgentCallbacks) toolCapabilities() []AgentToolCapability {
	specs := c.coreToolSpecs()
	out := make([]AgentToolCapability, 0, len(specs))
	for _, spec := range specs {
		out = append(out, AgentToolCapability{
			Name:           spec.Name,
			Description:    spec.Description,
			Enabled:        spec.Enabled,
			DisabledReason: spec.DisabledReason,
			Parameters:     spec.Parameters,
		})
	}
	return out
}

func (c *coreAgentCallbacks) BuildTools(string) []map[string]interface{} {
	specs := c.coreToolSpecs()
	tools := make([]map[string]interface{}, 0, len(specs))
	for _, spec := range specs {
		if !spec.Enabled {
			continue
		}
		tools = append(tools, functionToolDefinition(spec.Name, spec.Description, spec.Parameters))
	}
	// Append MCP tools from all healthy/running servers.
	// Called on every iteration to pick up newly installed MCP servers.
	if mcpDefs := c.mcpToolDefs(); len(mcpDefs) > 0 {
		tools = append(tools, mcpDefs...)
	}
	// Append manage_skill tool if skill provider is available.
	if skillDefs := c.skillToolDefs(); len(skillDefs) > 0 {
		tools = append(tools, skillDefs...)
	}
	return tools
}

func (c *coreAgentCallbacks) ExecuteTool(name, argsJSON string) string {
	return c.ExecuteToolStructured(name, argsJSON).Result
}

func (c *coreAgentCallbacks) IsToolAllowed(name string) bool {
	return workflow.IsToolAllowedByPolicy(c.toolPolicy, name)
}

func (c *coreAgentCallbacks) IsToolCallAllowed(name, argsJSON string) (bool, string) {
	var args map[string]interface{}
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return false, fmt.Sprintf("invalid tool arguments: %v", err)
		}
	}
	if err := workflow.ValidateToolCallByPolicyWithApproval(c.toolPolicy, strings.TrimSpace(name), args, c.opsApprovedCommands); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func (c *coreAgentCallbacks) ExecuteToolStructured(name, argsJSON string) agent.ToolExecutionResult {
	var args map[string]interface{}
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return agent.ToolExecutionResult{
				Result:  fmt.Sprintf("Error: invalid tool arguments: %v", err),
				Outcome: agent.ToolExecutionOutcomeError,
			}
		}
	}
	if err := workflow.ValidateToolCallByPolicyWithApproval(c.toolPolicy, strings.TrimSpace(name), args, c.opsApprovedCommands); err != nil {
		return agent.ToolExecutionResult{Result: "Error: " + err.Error(), Outcome: agent.ToolExecutionOutcomeError}
	}
	switch strings.TrimSpace(name) {
	case "bash":
		if !c.allowLocalBash {
			return agent.ToolExecutionResult{Result: "Error: local bash is disabled for this MaClawSrv deployment", Outcome: agent.ToolExecutionOutcomeError}
		}
		if !c.canUseLocalBash() {
			return agent.ToolExecutionResult{Result: "Error: " + c.localBashDeniedReason(), Outcome: agent.ToolExecutionOutcomeError}
		}
		return agent.ToolExecutionResult{Result: agent.ToolBash(ensureBashWorkingDir(args, c.workspace), c.OnProgress)}
	case "ssh":
		if !c.canUseSSH() {
			return agent.ToolExecutionResult{Result: "Error: " + c.sshDeniedReason(), Outcome: agent.ToolExecutionOutcomeError}
		}
		validated, err := c.validateSSHArgs(args)
		if err != nil {
			return agent.ToolExecutionResult{Result: fmt.Sprintf("Error: %v", err), Outcome: agent.ToolExecutionOutcomeError}
		}
		return agent.ToolExecutionResult{Result: sshtool.ToolSSH(c.sshDeps, validated)}
	case "ask_user":
		return agent.ToolExecutionResult{Result: agent.ToolAskUser(args), Outcome: agent.ToolExecutionOutcomeOK}
	case "task":
		return agent.ToolExecutionResult{Result: agent.ToolTask(c.tasks, args), Outcome: agent.ToolExecutionOutcomeOK}
	case "memory":
		return agent.ToolExecutionResult{Result: agent.ToolMemory(c.memory, args), Outcome: agent.ToolExecutionOutcomeOK}
	case "read_file":
		return agent.ToolExecutionResult{Result: c.executeReadFile(args), Outcome: agent.ToolExecutionOutcomeOK}
	case "write_file":
		return agent.ToolExecutionResult{Result: c.executeWriteFile(args), Outcome: agent.ToolExecutionOutcomeOK}
	case "edit_file":
		return agent.ToolExecutionResult{Result: c.executeEditFile(args), Outcome: agent.ToolExecutionOutcomeOK}
	case "list_directory":
		return agent.ToolExecutionResult{Result: c.executeListDirectory(args), Outcome: agent.ToolExecutionOutcomeOK}
	case "manage_skill":
		return c.executeManageSkill(args)
	case "web_search":
		return agent.ToolExecutionResult{Result: c.executeWebSearch(args), Outcome: agent.ToolExecutionOutcomeOK}
	case "web_fetch":
		return agent.ToolExecutionResult{Result: c.executeWebFetch(args), Outcome: agent.ToolExecutionOutcomeOK}
	case "knowledge_search":
		return agent.ToolExecutionResult{Result: c.executeKnowledgeSearch(args), Outcome: agent.ToolExecutionOutcomeOK}
	case "knowledge_context_pack":
		return agent.ToolExecutionResult{Result: c.executeKnowledgeContextPack(args), Outcome: agent.ToolExecutionOutcomeOK}
	case "knowledge_save_url":
		return agent.ToolExecutionResult{Result: c.executeKnowledgeSaveURL(args), Outcome: agent.ToolExecutionOutcomeOK}
	case "knowledge_save_text":
		return agent.ToolExecutionResult{Result: c.executeKnowledgeSaveText(args), Outcome: agent.ToolExecutionOutcomeOK}
	default:
		// Try MCP tool dispatch before returning unknown tool error.
		if result, handled := c.executeMCPTool(strings.TrimSpace(name), args); handled {
			outcome := agent.ToolExecutionOutcomeOK
			if strings.HasPrefix(result, "Error:") {
				outcome = agent.ToolExecutionOutcomeError
			}
			return agent.ToolExecutionResult{Result: result, Outcome: outcome}
		}
		return agent.ToolExecutionResult{Result: fmt.Sprintf("Error: unknown tool %s", name), Outcome: agent.ToolExecutionOutcomeError}
	}
}

func (c *coreAgentCallbacks) OnToken(string)      {}
func (c *coreAgentCallbacks) OnProgress(string)   {}
func (c *coreAgentCallbacks) OnToolCall(string)   {}
func (c *coreAgentCallbacks) OnToolResult(string) {}
func (c *coreAgentCallbacks) ShouldStop() bool    { return c.ctx != nil && c.ctx.Err() != nil }

func (c *coreAgentCallbacks) canUseLocalBash() bool {
	if !c.allowLocalBash {
		return false
	}
	if !c.localBashTrustedSingleUser {
		return false
	}
	if strings.TrimSpace(c.localBashTenantID) == "" || strings.TrimSpace(c.localBashUserID) == "" {
		return false
	}
	return c.principal.TenantID == c.localBashTenantID && c.principal.UserID == c.localBashUserID
}

func (c *coreAgentCallbacks) localBashDeniedReason() string {
	if !c.localBashTrustedSingleUser {
		return "local bash requires MACLAW_LOCAL_BASH_TRUSTED_SINGLE_USER=true and should only be enabled for trusted single-user deployments"
	}
	if strings.TrimSpace(c.localBashTenantID) == "" || strings.TrimSpace(c.localBashUserID) == "" {
		return "local bash requires MACLAW_LOCAL_BASH_TENANT_ID and MACLAW_LOCAL_BASH_USER_ID to scope access"
	}
	return fmt.Sprintf("local bash is restricted to tenant=%s user=%s", c.localBashTenantID, c.localBashUserID)
}

func bashToolDescription(tenantID, userID string) string {
	base := "Run a shell command in the current instance workspace on the MaClawSrv host."
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	if tenantID == "" || userID == "" {
		return base + " Disabled unless the deployment is explicitly marked as trusted single-user and binds access to one tenant and one user."
	}
	return base + fmt.Sprintf(" Restricted to tenant=%s user=%s and trusted single-user deployments.", tenantID, userID)
}
func sshAllowedActions(allowFileTransfer bool) []string {
	actions := []string{"connect", "exec", "exec_background", "check_task", "list_tasks", "kill_task", "sudo_prepare", "list", "close", "close_all"}
	if allowFileTransfer {
		actions = append(actions, "upload", "download")
	}
	return actions
}

func sshToolDescription(allowDirectSSH, allowFileTransfer, hasConfiguredHosts bool) string {
	parts := []string{"Manage remote SSH connections and commands for pure agent operations without coding-session orchestration."}
	if !allowDirectSSH {
		if hasConfiguredHosts {
			parts = append(parts, "Direct host credentials are disabled; use a preconfigured SSH host label.")
		} else {
			parts = append(parts, "No SSH access is currently available in this MaClawSrv deployment.")
		}
	}
	if !allowFileTransfer {
		parts = append(parts, "Local file transfer is disabled by default on MaClawSrv.")
	}
	return strings.Join(parts, " ")
}

func (c *coreAgentCallbacks) validateSSHArgs(args map[string]interface{}) (map[string]interface{}, error) {
	if args == nil {
		args = map[string]interface{}{}
	}
	action := strings.TrimSpace(agent.StringArg(args, "action"))
	if action == "" {
		return nil, fmt.Errorf("ssh action is required")
	}
	cloned := cloneToolArgs(args)
	switch action {
	case "connect":
		label := strings.TrimSpace(agent.StringArg(cloned, "label"))
		if !c.allowDirectSSH {
			if label == "" {
				return nil, fmt.Errorf("ssh connect requires a configured label in this MaClawSrv deployment")
			}
			for _, key := range []string{"host", "user", "port", "auth_method", "key_path", "password"} {
				if hasNonEmptyToolArg(cloned, key) {
					return nil, fmt.Errorf("ssh connect via label does not allow overriding %s in this deployment", key)
				}
			}
		}
	case "upload", "download":
		if !c.allowSSHFileTransfer {
			return nil, fmt.Errorf("ssh %s is disabled for this MaClawSrv deployment", action)
		}
		localPath := strings.TrimSpace(agent.StringArg(cloned, "local_path"))
		if localPath == "" {
			return nil, fmt.Errorf("ssh %s requires local_path", action)
		}
		if err := ensurePathWithinBase(localPath, c.workspace); err != nil {
			return nil, fmt.Errorf("local_path must stay within the instance workspace: %w", err)
		}
	}
	return cloned, nil
}

func cloneToolArgs(args map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(args))
	for k, v := range args {
		cloned[k] = v
	}
	return cloned
}

func hasNonEmptyToolArg(args map[string]interface{}, key string) bool {
	if args == nil {
		return false
	}
	v, ok := args[key]
	if !ok || v == nil {
		return false
	}
	switch vv := v.(type) {
	case string:
		return strings.TrimSpace(vv) != ""
	default:
		return true
	}
}

func ensurePathWithinBase(candidate, base string) error {
	base = strings.TrimSpace(base)
	candidate = strings.TrimSpace(candidate)
	if base == "" || candidate == "" {
		return fmt.Errorf("path validation requires both candidate and base")
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(baseAbs, candidateAbs)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("resolved path %q escapes %q", candidateAbs, baseAbs)
	}
	return nil
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
func functionToolDefinition(name, description string, params map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": description,
			"parameters":  params,
		},
	}
}

func ensureBashWorkingDir(args map[string]interface{}, workspace string) map[string]interface{} {
	if args == nil {
		args = map[string]interface{}{}
	}
	if strings.TrimSpace(workspace) == "" {
		return args
	}
	if strings.TrimSpace(agent.StringArg(args, "working_dir")) != "" {
		return args
	}
	cloned := make(map[string]interface{}, len(args)+1)
	for k, v := range args {
		cloned[k] = v
	}
	cloned["working_dir"] = workspace
	return cloned
}

func (c *coreAgentCallbacks) canUseSSH() bool {
	return c.allowDirectSSH || len(c.appCfg.SSHHosts) > 0
}

func (c *coreAgentCallbacks) sshDeniedReason() string {
	return "ssh is unavailable because this MaClawSrv deployment has no direct SSH access and no configured SSH host labels"
}
