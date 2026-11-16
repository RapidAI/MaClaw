package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/swarm"
)

// PDF creation is a chat capability, not a computer capability. The legacy
// generate_pdf tool writes to a service workspace and cannot deliver a file
// from a Hub bot (which has no service workspace).
const botPDFToolName = "bot_generate_pdf"

type botPDFModule struct {
	render func(string) ([]byte, error)
}

func (botPDFModule) Descriptor() agentruntime.ModuleDescriptor {
	return agentruntime.ModuleDescriptor{ModuleID: "maclawsrv.botpdf", Version: "1.0.0", HeadlessSupport: true}
}

func (botPDFModule) Tools(_ context.Context, request agentruntime.TurnRequest) ([]agentruntime.ToolDefinition, error) {
	if !desktopHubBotTurn(request) {
		return nil, nil
	}
	return []agentruntime.ToolDefinition{{
		Name:         botPDFToolName,
		Description:  "Generate a PDF from Markdown and deliver it to this bot's chat. Works without a computer or workspace. Supply the complete content and a short title; do not claim a file was attached unless this call succeeds. A plan turn must wait for execution.",
		ChangesState: func(map[string]any) bool { return true },
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":   map[string]any{"type": "string", "description": "File name and document heading, without .pdf"},
				"content": map[string]any{"type": "string", "description": "Complete Markdown document to render as PDF"},
			},
			"required": []string{"content"},
		},
	}}, nil
}

func (m botPDFModule) InvokeTool(ctx context.Context, request agentruntime.TurnRequest, name string, args map[string]any) (string, error) {
	if name != botPDFToolName || !desktopHubBotTurn(request) {
		return "", fmt.Errorf("PDF delivery is not available for this turn")
	}
	if desktopTurnPhase(request) == "plan" {
		return "", fmt.Errorf("PDF delivery waits until the task is in execute phase")
	}
	content := strings.TrimSpace(desktopArg(args, "content"))
	if content == "" || utf8.RuneCountInString(content) > desktopDeliverMaxRunes {
		return "", fmt.Errorf("PDF content is empty or too long")
	}
	title := strings.TrimSpace(desktopArg(args, "title"))
	if title == "" {
		title = "文档"
	}
	if strings.HasSuffix(strings.ToLower(title), ".pdf") {
		title = strings.TrimSpace(title[:len(title)-4])
	}
	name, err := desktopDeliverName(title + ".pdf")
	if err != nil {
		return "", err
	}
	render := m.render
	if render == nil {
		render = func(text string) ([]byte, error) {
			if err := swarm.ValidatePDFContent(text); err != nil {
				return nil, err
			}
			generator := swarm.NewSwarmDocGenerator()
			if generator == nil || !generator.HasFont() {
				return nil, fmt.Errorf("PDF font is unavailable on this server")
			}
			return generator.GenerateSpecDoc("", title, text)
		}
	}
	pdf, err := render(content)
	if err != nil {
		return "", fmt.Errorf("PDF generation failed: %w", err)
	}
	if len(pdf) == 0 {
		return "", fmt.Errorf("PDF generator returned no data")
	}
	if ctx != nil && ctx.Err() != nil {
		return "", ctx.Err()
	}
	if desktopShotSlotFrom(ctx) != nil && base64.StdEncoding.EncodedLen(len(pdf)) <= desktopHubReplyFileMax {
		if err := noteDesktopFile(ctx, name, "application/pdf", pdf); err != nil {
			return "", err
		}
		return desktopDelivered(name, len(pdf)), nil
	}
	scope, _, err := desktopScopeForTurn(request)
	if err != nil {
		return "", err
	}
	return deliverFileOnItsOwn(ctx, scope, name, pdf)
}
