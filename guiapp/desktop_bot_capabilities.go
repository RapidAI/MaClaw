package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/botcapability"
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

// localRelayMCP runs one stdio MCP tool for a cloud bot. The server id is the
// one stored on the account; the command and env stay in this app's config.
func (a *App) localRelayMCP(ctx context.Context, serverID, toolName, argsJSON string) (string, error) {
	if a == nil || a.imHandler == nil {
		return "", fmt.Errorf("MCP is not running in this app")
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
