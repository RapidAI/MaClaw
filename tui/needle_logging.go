package main

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/needledata"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

func (app *TUIApp) logNeedleEvent(e needledata.Event) {
	if app == nil || !app.appConfig.LocalNeedleLogEnabled {
		return
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	logger := needledata.NewLogger(needledata.DefaultLogDir(commands.ResolveDataDir()), true)
	if err := logger.Log(e); err != nil {
		log.Printf("[needledata] log event failed: %v", err)
	}
}

func (app *TUIApp) logNeedleWorkflowReviewEvent(userID, text, raw string, needlePrediction *needledata.Decision, intentName string, success bool, errText string) {
	if app == nil || !app.appConfig.LocalNeedleLogEnabled {
		return
	}
	app.logNeedleEvent(needledata.Event{
		Type: needledata.EventWorkflowReview,
		Input: needledata.EventInput{
			UserText: text,
			Choices:  []string{"confirm", "supplement", "skip", "cancel", "switch_task", "other"},
		},
		LLMPrediction: &needledata.Decision{
			Name:   raw,
			Source: "llm",
		},
		NeedlePrediction: needlePrediction,
		FinalDecision: needledata.Decision{
			Name:   intentName,
			Source: "workflow_review_classifier",
		},
		Outcome: needledata.EventOutcome{
			Success:   success,
			ToolError: errText,
		},
		Privacy: needledata.PrivacyInfo{
			ProjectHash: needledata.HashProject(userID),
		},
	})
}

func (app *TUIApp) logNeedleToolRoutingEvent(userText, toolName, argsJSON, result string, elapsed time.Duration) {
	if app == nil || !app.appConfig.LocalNeedleLogEnabled {
		return
	}
	args := map[string]any{}
	if argsJSON != "" {
		_ = json.Unmarshal([]byte(argsJSON), &args)
	}
	app.logNeedleEvent(needledata.Event{
		Type: needledata.EventToolRouting,
		Input: needledata.EventInput{
			UserText:       userText,
			AvailableTools: summarizeToolDefinitions(app.toolRegistry.BuildDefinitions()),
		},
		FinalDecision: needledata.Decision{
			Name:      toolName,
			Arguments: args,
			Source:    "agent_tool_call",
		},
		Outcome: needledata.EventOutcome{
			Success:   !needleToolResultIndicatesError(result),
			ToolError: toolResultErrorSnippet(result),
		},
		Meta: map[string]any{
			"elapsed_ms": elapsed.Milliseconds(),
		},
	})
}

func summarizeToolDefinitions(defs []map[string]interface{}) []needledata.ToolSummary {
	out := make([]needledata.ToolSummary, 0, len(defs))
	for _, def := range defs {
		fn, _ := def["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if name == "" {
			continue
		}
		desc, _ := fn["description"].(string)
		var required []string
		if params, _ := fn["parameters"].(map[string]interface{}); params != nil {
			if raw, _ := params["required"].([]interface{}); raw != nil {
				for _, item := range raw {
					if s, ok := item.(string); ok {
						required = append(required, s)
					}
				}
			}
		}
		out = append(out, needledata.ToolSummary{Name: name, Description: desc, Required: required})
	}
	return out
}

func toolResultErrorSnippet(result string) string {
	if !needleToolResultIndicatesError(result) {
		return ""
	}
	if len(result) > 400 {
		return result[:400]
	}
	return result
}

func needleToolResultIndicatesError(result string) bool {
	lower := strings.ToLower(strings.TrimSpace(result))
	return strings.HasPrefix(lower, "error:") || strings.HasPrefix(lower, "failed:") || strings.Contains(lower, "\nerror:") || strings.Contains(lower, "\nfailed:")
}
