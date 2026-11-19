package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/botcapability"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
	"github.com/RapidAI/CodeClaw/corelib/mcp"
)

const desktopCapabilityResultRunes = 24000

// desktopCapability serves the account-wide MCP servers and skills. The
// document is on this turn's user config, pushed by Hub. HTTP MCP is called
// from here. Stdio MCP runs in the account cloud desktop from the saved
// command and env, so the person's own computer can be off.
func desktopCapability(ctx context.Context, scope agentruntime.Scope, action string, args map[string]any) (string, error) {
	if action == "cap_mcp" {
		// Listing tools is a read. Calling one waits for the execute phase.
		if phase, keyed := desktopPhase(ctx); keyed && phase != "execute" && strings.TrimSpace(desktopArg(args, "mcp_tool")) != "" {
			return "", fmt.Errorf("%s", desktopbot.PlanPhaseNextStep)
		}
	}
	doc, err := desktopCapabilityDoc(ctx)
	if err != nil {
		return "", err
	}
	switch action {
	case "cap_list":
		return botcapability.Catalog(doc), nil
	case "cap_skill":
		skill, err := botcapability.LookupSkill(doc, desktopArg(args, "name"))
		if err != nil {
			return "", err
		}
		text := skill.Body
		if skill.Description != "" {
			withDesc := skill.Description + "\n\n" + skill.Body
			if utf8.RuneCountInString(withDesc) <= desktopCapabilityResultRunes {
				text = withDesc
			}
		}
		return text, nil
	case "cap_mcp":
		return desktopCapabilityMCP(ctx, scope, doc, args)
	default:
		return "", fmt.Errorf("unknown capability action %s", action)
	}
}

// desktopCapabilityPrompt names the MCP servers and skills on this turn.
// Skill bodies stay out of the prompt; the bot reads one with cap_skill.
func desktopCapabilityPrompt(request agentruntime.TurnRequest) string {
	cfg, ok := turnAppConfig(request)
	if !ok {
		return ""
	}
	doc, err := botcapability.Parse(cfg.DesktopBotCapabilities)
	if err != nil || doc.Empty() {
		return ""
	}
	catalog := botcapability.Catalog(doc)
	if !strings.Contains(catalog, "action=cap_") {
		return ""
	}
	return "This account's MCP servers and skills, shared by every bot of this user: " + catalog + " " +
		"Follow a skill after action=cap_skill. action=cap_mcp with only mcp_server lists that server's tools. Call one with mcp_tool and mcp_arguments. "
}

func desktopCapabilityDoc(ctx context.Context) (botcapability.Document, error) {
	cfg, _ := ctx.Value(desktopAppConfigKey{}).(corelib.AppConfig)
	doc, err := botcapability.Parse(cfg.DesktopBotCapabilities)
	if err != nil {
		log.Printf("[desktop-cap] read settings: %v", err)
		return botcapability.Document{}, fmt.Errorf("bot MCP and skill settings could not be read")
	}
	return doc, nil
}

func desktopCapabilityMCP(ctx context.Context, scope agentruntime.Scope, doc botcapability.Document, args map[string]any) (string, error) {
	server, err := botcapability.LookupMCP(doc, desktopArg(args, "mcp_server"))
	if err != nil {
		return "", err
	}
	tool := desktopArg(args, "mcp_tool")
	if tool == "" {
		return desktopCapabilityMCPList(ctx, scope, server)
	}
	rawArgs, err := desktopCapabilityArgs(args["mcp_arguments"])
	if err != nil {
		return "", err
	}
	var text string
	switch server.Transport {
	case "http":
		callCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		text, err = mcp.CallTool(callCtx, mcp.MCPServerConfig{
			EndpointURL: server.URL,
			Headers:     server.Headers,
			APIKey:      server.AuthSecret,
		}, tool, rawArgs)
	case "stdio":
		text, err = desktopMCPStdioCall(ctx, scope, server, tool, rawArgs)
	default:
		return "", fmt.Errorf("MCP %s has an unknown transport", server.Name)
	}
	if err != nil {
		return "", err
	}
	return trimCapabilityResult(text), nil
}

func desktopCapabilityMCPList(ctx context.Context, scope agentruntime.Scope, server botcapability.Server) (string, error) {
	var text string
	var err error
	switch server.Transport {
	case "http":
		callCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
		defer cancel()
		text, err = mcp.ListTools(callCtx, mcp.MCPServerConfig{
			EndpointURL: server.URL,
			Headers:     server.Headers,
			APIKey:      server.AuthSecret,
		})
	case "stdio":
		text, err = desktopMCPStdioList(ctx, scope, server)
	default:
		return "", fmt.Errorf("MCP %s has an unknown transport", server.Name)
	}
	if err != nil {
		return "", err
	}
	return trimCapabilityResult(text), nil
}

func desktopCapabilityArgs(v any) (map[string]any, error) {
	if v == nil {
		return map[string]any{}, nil
	}
	switch t := v.(type) {
	case map[string]any:
		return t, nil
	case string:
		t = strings.TrimSpace(t)
		if t == "" {
			return map[string]any{}, nil
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(t), &out); err != nil || out == nil {
			return nil, fmt.Errorf("mcp_arguments must be a JSON object")
		}
		return out, nil
	default:
		raw, err := json.Marshal(t)
		if err != nil {
			return nil, fmt.Errorf("mcp_arguments must be an object")
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil || out == nil {
			return nil, fmt.Errorf("mcp_arguments must be an object")
		}
		return out, nil
	}
}

func trimCapabilityResult(text string) string {
	if utf8.RuneCountInString(text) <= desktopCapabilityResultRunes {
		return text
	}
	return string([]rune(text)[:desktopCapabilityResultRunes]) + "\n…truncated"
}
