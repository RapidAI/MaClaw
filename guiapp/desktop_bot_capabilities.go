package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/botcapability"
	"github.com/RapidAI/CodeClaw/corelib/localrelay"
	"github.com/RapidAI/CodeClaw/corelib/mcp"
)

// DesktopBotSkillChoice is one skill the settings screen can turn on.
// Local means this computer has the files. OnServer means the account
// already has the text, so every bot can read it.
type DesktopBotSkillChoice struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
	Local       bool   `json:"local,omitempty"`
	OnServer    bool   `json:"on_server,omitempty"`
}

// DesktopBotCapabilityPage is the Bot settings tab for MCP and skills.
type DesktopBotCapabilityPage struct {
	MCP     []botcapability.ServerView `json:"mcp"`
	Skills  []DesktopBotSkillChoice    `json:"skills"`
	Warning string                     `json:"warning,omitempty"`
}

// DesktopBotCapabilityUpdate is one edit. EnableSkills reads the installed
// text on this computer. AddSkills writes that text here and uploads it.
// A blank HTTP secret keeps the one already stored.
type DesktopBotCapabilityUpdate struct {
	UpsertMCP    []botcapability.Server `json:"upsert_mcp"`
	RemoveMCP    []string               `json:"remove_mcp"`
	SetDisabled  map[string]bool        `json:"set_disabled"`
	EnableSkills []string               `json:"enable_skills"`
	RemoveSkills []string               `json:"remove_skills"`
	AddSkills    []botcapability.Skill  `json:"add_skills"`
}

// GetDesktopBotCapabilityPage reads the account's MCP servers and skills and
// marks which skills are installed on this computer.
func (a *App) GetDesktopBotCapabilityPage() (*DesktopBotCapabilityPage, error) {
	view, err := a.fetchBotCapabilities()
	if err != nil {
		return nil, err
	}
	return a.capabilityPage(view), nil
}

// UpdateDesktopBotCapabilities changes the account-wide set. Every bot of
// this account reads the result.
func (a *App) UpdateDesktopBotCapabilities(in DesktopBotCapabilityUpdate) (*DesktopBotCapabilityPage, error) {
	up := botcapability.Update{
		UpsertMCP:    in.UpsertMCP,
		RemoveMCP:    in.RemoveMCP,
		SetDisabled:  in.SetDisabled,
		RemoveSkills: in.RemoveSkills,
	}
	for _, name := range in.EnableSkills {
		sk, err := a.readLocalSkill(name)
		if err != nil {
			return nil, err
		}
		up.UpsertSkills = append(up.UpsertSkills, sk)
	}
	for _, sk := range in.AddSkills {
		if err := a.writeLocalSkill(sk); err != nil {
			return nil, err
		}
		up.UpsertSkills = append(up.UpsertSkills, sk)
	}
	view, err := a.putBotCapabilities(up)
	if err != nil {
		return nil, err
	}
	return a.capabilityPage(view), nil
}

// ImportDesktopBotMCP copies MCP into the account set. source "gui" copies
// the servers configured in this MaClaw app. Any other value is MCP JSON
// (mcpServers). Command, env, and HTTP secrets are stored for the bots.
func (a *App) ImportDesktopBotMCP(source string) (*DesktopBotCapabilityPage, error) {
	source = strings.TrimSpace(source)
	var servers []botcapability.Server
	var err error
	if source == "" || source == "gui" {
		servers, err = a.mcpServersFromThisComputer()
	} else {
		servers, err = a.mcpServersFromJSON(source)
	}
	if err != nil {
		return nil, err
	}
	if len(servers) == 0 {
		return nil, fmt.Errorf("没有可添加的 MCP")
	}
	return a.UpdateDesktopBotCapabilities(DesktopBotCapabilityUpdate{UpsertMCP: servers})
}

func (a *App) fetchBotCapabilities() (botcapability.View, error) {
	var out botcapability.View
	if err := a.desktopBotCall(context.Background(), httpMethodGet, "/api/v1/bots/capabilities", nil, &out, 20*time.Second); err != nil {
		return botcapability.View{}, err
	}
	return out, nil
}

func (a *App) putBotCapabilities(up botcapability.Update) (botcapability.View, error) {
	var out botcapability.View
	if err := a.desktopBotCall(context.Background(), httpMethodPut, "/api/v1/bots/capabilities", up, &out, 30*time.Second); err != nil {
		return botcapability.View{}, err
	}
	return out, nil
}

const (
	httpMethodGet = "GET"
	httpMethodPut = "PUT"
)

func (a *App) capabilityPage(view botcapability.View) *DesktopBotCapabilityPage {
	page := &DesktopBotCapabilityPage{MCP: view.MCP, Skills: []DesktopBotSkillChoice{}, Warning: view.Warning}
	if page.MCP == nil {
		page.MCP = []botcapability.ServerView{}
	}
	enabled := map[string]botcapability.SkillView{}
	for _, s := range view.Skills {
		enabled[strings.ToLower(s.Name)] = s
	}
	seen := map[string]bool{}
	for _, local := range a.listLocalSkillChoices() {
		key := strings.ToLower(local.Name)
		seen[key] = true
		local.Local = true
		if sv, ok := enabled[key]; ok {
			local.Enabled = true
			local.OnServer = true
			if local.Description == "" {
				local.Description = sv.Description
			}
		}
		page.Skills = append(page.Skills, local)
	}
	for _, s := range view.Skills {
		if seen[strings.ToLower(s.Name)] {
			continue
		}
		page.Skills = append(page.Skills, DesktopBotSkillChoice{
			Name: s.Name, Description: s.Description, Enabled: true, OnServer: true,
		})
	}
	sort.Slice(page.Skills, func(i, j int) bool {
		return strings.ToLower(page.Skills[i].Name) < strings.ToLower(page.Skills[j].Name)
	})
	return page
}

func (a *App) mcpServersFromThisComputer() ([]botcapability.Server, error) {
	if a == nil || a.mcpRegistry == nil {
		return nil, fmt.Errorf("这台电脑还没有 MCP 配置")
	}
	var raw []botcapability.Server
	for _, s := range a.mcpRegistry.ListLocalServers() {
		raw = append(raw, botcapability.Server{
			ID: s.ID, Name: s.Name, Transport: "stdio", Command: s.Command, Args: s.Args, Env: s.Env, Disabled: s.Disabled,
		})
	}
	for _, s := range a.mcpRegistry.ListServers() {
		raw = append(raw, botcapability.Server{
			ID: s.ID, Name: s.Name, Transport: "http", URL: s.EndpointURL,
			Headers: s.Headers, AuthSecret: s.AuthSecret,
		})
	}
	return cleanMCPServers(raw)
}

func (a *App) mcpServersFromJSON(raw string) ([]botcapability.Server, error) {
	local, remote, err := parseMCPImportConfig(raw, mcpImportTargetAuto)
	if err != nil {
		return nil, err
	}
	var servers []botcapability.Server
	for _, s := range local {
		servers = append(servers, botcapability.Server{
			ID: s.ID, Name: s.Name, Transport: "stdio", Command: s.Command, Args: s.Args, Env: s.Env, Disabled: s.Disabled,
		})
	}
	for _, s := range remote {
		servers = append(servers, botcapability.Server{
			ID: s.ID, Name: s.Name, Transport: "http", URL: s.EndpointURL,
			Headers: s.Headers, AuthSecret: s.AuthSecret,
		})
	}
	return cleanMCPServers(servers)
}

func cleanMCPServers(raw []botcapability.Server) ([]botcapability.Server, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	doc, err := botcapability.Apply(botcapability.Document{}, botcapability.Update{UpsertMCP: raw})
	if err != nil {
		return nil, err
	}
	return doc.MCP, nil
}

func (a *App) listLocalSkillChoices() []DesktopBotSkillChoice {
	root := ""
	if a != nil {
		root = a.GetSkillsDir("")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []DesktopBotSkillChoice
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name, description, _, ok := readSkillDir(filepath.Join(root, entry.Name()))
		if !ok {
			continue
		}
		out = append(out, DesktopBotSkillChoice{Name: name, Description: description})
	}
	return out
}

func (a *App) readLocalSkill(name string) (botcapability.Skill, error) {
	want, err := botcapability.CleanSkillName(name)
	if err != nil {
		return botcapability.Skill{}, err
	}
	root := ""
	if a != nil {
		root = a.GetSkillsDir("")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return botcapability.Skill{}, fmt.Errorf("技能 %s 还没有说明文件", want)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirName := entry.Name()
		if _, err := botcapability.CleanSkillName(dirName); err != nil {
			continue
		}
		got, description, body, ok := readSkillDir(filepath.Join(root, dirName))
		if !ok {
			continue
		}
		if got == "" {
			got = dirName
		}
		if strings.EqualFold(got, want) || strings.EqualFold(dirName, want) {
			return botcapability.Skill{Name: got, Description: description, Body: body}, nil
		}
	}
	return botcapability.Skill{}, fmt.Errorf("技能 %s 还没有说明文件", want)
}

func (a *App) writeLocalSkill(sk botcapability.Skill) error {
	name, err := botcapability.CleanSkillName(sk.Name)
	if err != nil {
		return err
	}
	dir, err := a.skillDir(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	text := sk.Body
	if !strings.HasPrefix(strings.TrimSpace(text), "---") {
		text = "---\nname: " + yamlScalar(name) + "\ndescription: " + yamlScalar(sk.Description) + "\n---\n\n" + strings.TrimSpace(sk.Body) + "\n"
	}
	return os.WriteFile(filepath.Join(dir, "skill.md"), []byte(text), 0o644)
}

func (a *App) skillDir(name string) (string, error) {
	root := a.GetSkillsDir("")
	dir := filepath.Join(root, name)
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || strings.ContainsAny(rel, `/\`) {
		return "", fmt.Errorf("skill path is not allowed")
	}
	return dir, nil
}

func readSkillDir(dir string) (name, description, body string, ok bool) {
	for _, file := range []string{"skill.md", "SKILL.md", "README.md"} {
		raw, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			continue
		}
		name, description, body = splitSkillText(filepath.Base(dir), string(raw))
		return name, description, body, strings.TrimSpace(body) != ""
	}
	return "", "", "", false
}

func splitSkillText(dirName, raw string) (name, description, body string) {
	name = strings.TrimSpace(dirName)
	text := strings.TrimSpace(raw)
	if strings.HasPrefix(text, "---") {
		rest := strings.TrimPrefix(text, "---")
		end := strings.Index(rest, "\n---")
		if end >= 0 {
			front := rest[:end]
			body = strings.TrimSpace(rest[end+4:])
			for _, line := range strings.Split(front, "\n") {
				key, val, ok := strings.Cut(line, ":")
				if !ok {
					continue
				}
				key = strings.ToLower(strings.TrimSpace(key))
				val = strings.Trim(strings.TrimSpace(val), `"'`)
				switch key {
				case "name":
					if cleaned, err := botcapability.CleanSkillName(val); err == nil {
						name = cleaned
					}
				case "description":
					description = val
				}
			}
			if body == "" {
				body = strings.TrimSpace(raw)
			}
			return name, description, body
		}
	}
	return name, "", strings.TrimSpace(raw)
}

func yamlScalar(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, ":#\n\"'") {
		return yamlQuote(s)
	}
	return s
}

func yamlQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// mcpHostRelayServer is the GUI's own MCP task service, as opposed to a
// stdio server configured on the account. The bot names this server; the
// call stays inside this process and never opens the MCP port.
const mcpHostRelayServer = "maclaw"

// errMCPHostRelayOff is what the bot hears when the person has not turned
// the MCP service on. The call does not start the listener for them.
var errMCPHostRelayOff = errors.New("MCP is turned off in the person's MaClaw app. Ask them to turn the MCP service on; this call does not open a port or start the service")

// errLocalRelayStdioOff is what the bot hears for a stdio MCP server when
// the person turned on the task service only. Those servers run local
// commands, so they stay behind file or network access.
var errLocalRelayStdioOff = errors.New("only the task service on this computer (server maclaw) is open to the bot. Other MCP servers stay unavailable until file or network access is turned on in the MaClaw app")

// localRelayStdioAllowed is file or network access for bots. Tests replace it.
var localRelayStdioAllowed = func() bool {
	s := loadLocalRelaySettings()
	return s.FilesEnabled || s.NetworkEnabled
}

// mcpHostEnabledForRelay reports the MCP service switch. Tests replace it.
var mcpHostEnabledForRelay = func(a *App) bool {
	if a == nil {
		return false
	}
	cfg, err := a.LoadConfig()
	return err == nil && cfg.IsMcpHostEnabled()
}

func mcpHostRelayServerID(id string) bool {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case mcpHostRelayServer, "maclaw-gui":
		return true
	}
	return false
}

// mcpHostRelayTools is the task surface a bot may drive. Browser checks and
// artifact downloads stay on a local MCP client; the bot schedules tasks.
func mcpHostRelayTool(name string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "tools":
		return "", true
	case "list_models", "knowledge_search", "create_task", "get_task", "list_tasks", "send_command", "stop_task", "cancel_task", "get_request", "retry_steps", "read_log":
		return strings.ToLower(strings.TrimSpace(name)), true
	default:
		return "", false
	}
}

// localRelayMCP runs one MCP tool for a cloud bot.
// Server "maclaw" is this app's task MCP (create a programming task here);
// task tools run only when that service is switched on, while the read-only
// knowledge_search rides the local-access consent envelope like file reads.
// Any other id is a stdio server stored on the account; its command and env
// stay in this app.
func (a *App) localRelayMCP(ctx context.Context, serverID, toolName, argsJSON string) (string, error) {
	if mcpHostRelayServerID(serverID) {
		return a.localRelayMCPHost(ctx, toolName, argsJSON)
	}
	if a == nil || a.imHandler == nil {
		return "", fmt.Errorf("MCP is not running in this app")
	}
	if !localRelayStdioAllowed() {
		return "", errLocalRelayStdioOff
	}
	mgr := a.imHandler.getLocalMCPManager()
	if mgr == nil {
		return "", fmt.Errorf("MCP is not running in this app")
	}
	id, err := a.resolveLocalMCPServer(serverID)
	if err != nil {
		return "", err
	}
	callCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if strings.TrimSpace(toolName) == "" {
		return localRelayMCPList(callCtx, mgr, id)
	}
	var args map[string]interface{}
	if strings.TrimSpace(argsJSON) != "" && strings.TrimSpace(argsJSON) != "{}" && strings.TrimSpace(argsJSON) != "null" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("arguments must be a JSON object")
		}
	}
	type result struct {
		text string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		text, callErr := mgr.CallTool(id, toolName, args)
		ch <- result{text, callErr}
	}()
	select {
	case <-callCtx.Done():
		return "", fmt.Errorf("MCP %s timed out", serverID)
	case got := <-ch:
		return got.text, got.err
	}
}

func (a *App) localRelayMCPHost(ctx context.Context, toolName, argsJSON string) (string, error) {
	// knowledge_search is read-only access to the person's own knowledge base:
	// it rides the same consent envelope as the relay's file access rather than
	// the MCP task-service switch, which gates the power to create and drive tasks.
	isKnowledgeRead := strings.EqualFold(strings.TrimSpace(toolName), "knowledge_search")
	if !mcpHostEnabledForRelay(a) && !isKnowledgeRead {
		return "", errMCPHostRelayOff
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// The access log shows who started the task. No bearer key crosses Hub.
	ctx = context.WithValue(ctx, mcpClientInfoContextKey{}, mcpClientInfo{IP: "desktop-bot"})
	text, err := mcpCallHostTool(ctx, a, toolName, argsJSON)
	if err != nil {
		msg := mcpHostRelayRedact(err.Error(), argsJSON)
		if msg == err.Error() {
			return "", err
		}
		return "", errors.New(msg)
	}
	return mcpHostRelayRedact(text, argsJSON), nil
}

// mcpHostRelayRedact removes an SSH password from a result before it is
// written back across Hub. JSON escapes & and <, so the raw password and
// that encoding are both covered.
func mcpHostRelayRedact(text, argsJSON string) string {
	var obj map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(argsJSON)), &obj) != nil {
		return text
	}
	for _, key := range []string{"ssh_password", "sshPassword", "password"} {
		secret, _ := obj[key].(string)
		text = localrelay.RedactSecret(text, secret)
	}
	return text
}

// mcpCallHostTool runs one MCP task tool in-process, the same dispatcher the
// HTTP listener uses. The bot reaches it through the app's outbound Hub link,
// so the MCP port can stay on loopback.
func mcpCallHostTool(ctx context.Context, api mcpTaskAPI, toolName, argsJSON string) (string, error) {
	if api == nil {
		return "", fmt.Errorf("MCP is not running in this app")
	}
	canonical, ok := mcpHostRelayTool(toolName)
	if !ok {
		return "", fmt.Errorf("this computer's MCP service does not expose %s to a bot. Use list_models, knowledge_search, create_task, get_task, list_tasks, send_command, stop_task, get_request, retry_steps, or read_log", strings.TrimSpace(toolName))
	}
	if canonical == "" {
		return mcpHostRelayCatalog(), nil
	}
	argsJSON = strings.TrimSpace(argsJSON)
	if argsJSON == "" || argsJSON == "null" {
		argsJSON = "{}"
	}
	var probe any
	if err := json.Unmarshal([]byte(argsJSON), &probe); err != nil {
		return "", fmt.Errorf("arguments must be a JSON object")
	}
	if _, isObj := probe.(map[string]any); !isObj && probe != nil {
		return "", fmt.Errorf("arguments must be a JSON object")
	}
	params, err := json.Marshal(map[string]any{
		"name":      canonical,
		"arguments": json.RawMessage(argsJSON),
	})
	if err != nil {
		return "", err
	}
	result, rpcErr := (&mcpHTTP{api: api}).callTool(ctx, params)
	if rpcErr != nil {
		return "", errors.New(rpcErr.Message)
	}
	if err := mcpHostRelayFailure(result); err != nil {
		return "", err
	}
	return mcpToolResultText(result), nil
}

// mcpHostRelayFailure turns a failed MCP result into an error when no task
// was created. A failed view that already has a project_path stays as text,
// so the bot can correct that task instead of starting another.
func mcpHostRelayFailure(result any) error {
	body, ok := result.(map[string]any)
	if !ok {
		return nil
	}
	failed, _ := body["isError"].(bool)
	if !failed {
		return nil
	}
	text := strings.TrimSpace(mcpToolResultText(result))
	var parsed map[string]any
	if json.Unmarshal([]byte(text), &parsed) == nil {
		if path, _ := parsed["project_path"].(string); strings.TrimSpace(path) != "" {
			return nil
		}
		if msg, _ := parsed["error"].(string); strings.TrimSpace(msg) != "" {
			return errors.New(strings.TrimSpace(msg))
		}
	}
	if text == "" {
		return errors.New("the MCP call failed")
	}
	return errors.New(text)
}

func mcpHostRelayCatalog() string {
	var b strings.Builder
	b.WriteString("MCP tools on this computer:\n")
	for _, name := range []string{"list_models", "knowledge_search", "create_task", "get_task", "list_tasks", "send_command", "stop_task", "get_request", "retry_steps", "read_log"} {
		fmt.Fprintf(&b, "- %s\n", name)
	}
	return b.String()
}

func mcpToolResultText(result any) string {
	body, ok := result.(map[string]any)
	if !ok {
		raw, err := json.Marshal(result)
		if err != nil {
			return ""
		}
		return string(raw)
	}
	var b strings.Builder
	switch items := body["content"].(type) {
	case []map[string]any:
		for _, item := range items {
			if text, ok := item["text"].(string); ok {
				b.WriteString(text)
			}
		}
	}
	if b.Len() == 0 {
		raw, err := json.Marshal(result)
		if err != nil {
			return ""
		}
		return string(raw)
	}
	return b.String()
}

func localRelayMCPList(ctx context.Context, mgr *LocalMCPManager, serverID string) (string, error) {
	type result struct {
		tools []MCPToolView
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		tools, err := mgr.ListTools(serverID)
		ch <- result{tools, err}
	}()
	var tools []MCPToolView
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("MCP %s timed out", serverID)
	case got := <-ch:
		if got.err != nil {
			return "", got.err
		}
		tools = got.tools
	}
	entries := make([]mcp.ToolEntry, 0, len(tools))
	for _, tool := range tools {
		entries = append(entries, mcp.ToolEntry{
			ToolName:    tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}
	return mcp.FormatListedTools(entries), nil
}

func (a *App) resolveLocalMCPServer(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if a == nil || a.mcpRegistry == nil {
		return "", fmt.Errorf("this computer has no MCP named %s", ref)
	}
	for _, s := range a.mcpRegistry.ListLocalServers() {
		if s.ID == ref || strings.EqualFold(s.Name, ref) {
			if s.Disabled {
				return "", fmt.Errorf("MCP %s is turned off on this computer", s.Name)
			}
			return s.ID, nil
		}
	}
	return "", fmt.Errorf("this computer has no MCP named %s", ref)
}
