package main

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// sanitizeCommittedEffectPayload is the final transport boundary for
// evidence-backed reconciliation. Effects are intentionally private and may
// contain provider URLs, paths, or credentials even when a worker normally
// stores a sanitized result. Never project a committed payload verbatim.
func sanitizeCommittedEffectPayload(dataRoot string, raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	var sanitize func(string, any) any
	sanitize = func(key string, current any) any {
		if supportBundleSensitiveKey(key) {
			return "[redacted]"
		}
		switch item := current.(type) {
		case map[string]any:
			out := make(map[string]any, len(item))
			for childKey, childValue := range item {
				out[childKey] = sanitize(childKey, childValue)
			}
			return out
		case []any:
			out := make([]any, len(item))
			for i, child := range item {
				out[i] = sanitize(key, child)
			}
			return out
		case string:
			lowerKey := strings.ToLower(key)
			if strings.Contains(lowerKey, "endpoint") || strings.HasSuffix(lowerKey, "url") || strings.HasSuffix(lowerKey, "uri") {
				return redactEndpointForAPI(dataRoot, item)
			}
			if strings.Contains(lowerKey, "path") || strings.Contains(lowerKey, "file") || strings.Contains(lowerKey, "workspace") || strings.Contains(lowerKey, "command") || strings.HasSuffix(lowerKey, "dir") {
				return redactSupportBundleValue(dataRoot, item)
			}
			return redactSupportBundleText(dataRoot, item)
		default:
			return current
		}
	}
	value = sanitize("", value)
	out, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return out
}

// domainJobReconcilerFor returns the read-only reconciler for a durable Job
// kind. Keeping this mapping in one place prevents user and admin transports
// from drifting and, importantly, never exposes the original worker closure
// as a replay path.
func domainJobReconcilerFor(server *HTTPServer, kind string) agentruntime.JobReconciler {
	if server == nil {
		return nil
	}
	switch strings.TrimSpace(kind) {
	case "mcp.create", "mcp.update", "mcp.start", "mcp.stop":
		return mcpJobReconciler{server: server}
	case "skill.install", "skill.import", "skill.upload":
		return skillJobReconciler{server: server}
	case "knowledge_import_file", "knowledge_import_urls", "knowledge_import_directory", "knowledge_import_share", "knowledge_import_package", "public_knowledge_import_urls", "public_knowledge_import_file", "public_knowledge_import_text":
		return knowledgeJobReconciler{server: server}
	case "migration.export":
		return migrationExportJobReconciler{server: server}
	case "migration.import":
		return migrationImportJobReconciler{server: server}
	default:
		return nil
	}
}

// mcpJobReconciler projects only read-only MCP observations. It never invokes
// Start/Stop/Create/Update again, which keeps a recovered unknown Job from
// replaying a provider or process side effect.
type mcpJobReconciler struct{ server *HTTPServer }

func (r mcpJobReconciler) ReconcileJob(ctx context.Context, job agentruntime.Job, effects []agentruntime.JobEffect) (agentruntime.JobReconcileResult, error) {
	if r.server == nil || r.server.svc == nil || !strings.HasPrefix(job.Kind, "mcp.") {
		return agentruntime.JobReconcileResult{}, nil
	}
	if result := agentruntime.ReconcileFromProtectedEffects(effects, job.Kind, "MCP effect failed", func(raw json.RawMessage) json.RawMessage {
		return sanitizeCommittedEffectPayload(r.server.svc.DataRoot(), raw)
	}); result.Resolved {
		return result, nil
	}
	for _, effect := range effects {
		if effect.Kind != job.Kind {
			continue
		}
		if effect.State != agentruntime.JobEffectPrepared && effect.State != agentruntime.JobEffectUnknown {
			continue
		}
		serverID := strings.TrimSpace(effect.ResourceID)
		if serverID == "" {
			continue
		}
		view, err := r.server.svc.GetMCPServer(ctx, agentservice.Principal{TenantID: job.TenantID, UserID: job.UserID}, serverID)
		if err != nil || view == nil {
			continue
		}
		op := agentruntime.JobEffectOperation(effect)
		switch op {
		case "start":
			if !view.Running {
				continue
			}
		case "stop":
			if view.Running {
				continue
			}
		case "update":
			// Existence alone cannot prove that the requested fields were
			// applied; the effect payload intentionally does not retain the
			// full secret-bearing update. Keep this case unresolved unless a
			// committed receipt is available.
			continue
		}
		payload, _ := json.Marshal(sanitizeMCPServerViewForAPI(r.server.svc.DataRoot(), *view))
		return agentruntime.JobReconcileResult{Resolved: true, Status: agentruntime.JobStatusSucceeded, Result: payload}, nil
	}
	return agentruntime.JobReconcileResult{}, nil
}

type skillJobReconciler struct{ server *HTTPServer }

func (r skillJobReconciler) ReconcileJob(ctx context.Context, job agentruntime.Job, effects []agentruntime.JobEffect) (agentruntime.JobReconcileResult, error) {
	if r.server == nil || r.server.svc == nil || !strings.HasPrefix(job.Kind, "skill.") {
		return agentruntime.JobReconcileResult{}, nil
	}
	if result := agentruntime.ReconcileFromProtectedEffects(effects, job.Kind, "Skill effect failed", func(raw json.RawMessage) json.RawMessage {
		return sanitizeCommittedEffectPayload(r.server.svc.DataRoot(), raw)
	}); result.Resolved {
		return result, nil
	}
	for _, effect := range effects {
		if effect.Kind != job.Kind {
			continue
		}
		if effect.State != agentruntime.JobEffectPrepared && effect.State != agentruntime.JobEffectUnknown {
			continue
		}
		if agentruntime.JobEffectOperation(effect) == "upload" {
			// Submission IDs are remote-market identities, not local Skill names.
			// Without a receipt probe (which may require a caller credential),
			// leave the Job unknown instead of guessing from local state.
			continue
		}
		names := agentruntime.ParseJobEffectResourceIDs(effect.ResourceID)
		if len(names) == 0 {
			continue
		}
		items := make([]any, 0, len(names))
		found := 0
		for _, name := range names {
			entry, err := r.server.svc.GetSkill(ctx, agentservice.Principal{TenantID: job.TenantID, UserID: job.UserID}, name)
			if err != nil || entry == nil {
				continue
			}
			items = append(items, sanitizeSkillEntryForAPI(r.server.svc.DataRoot(), *entry))
			found++
		}
		if found == 0 {
			continue
		}
		payload, _ := json.Marshal(map[string]any{"items": items, "reconciled": true})
		return agentruntime.JobReconcileResult{Resolved: true, Status: agentruntime.JobStatusSucceeded, Result: payload}, nil
	}
	return agentruntime.JobReconcileResult{}, nil
}

type knowledgeJobReconciler struct{ server *HTTPServer }

func (r knowledgeJobReconciler) ReconcileJob(ctx context.Context, job agentruntime.Job, effects []agentruntime.JobEffect) (agentruntime.JobReconcileResult, error) {
	if r.server == nil || r.server.knowledgeMgr == nil {
		return agentruntime.JobReconcileResult{}, nil
	}
	expectedKind := map[string]string{
		"knowledge_import_file":        "knowledge.import.file",
		"knowledge_import_urls":        "knowledge.import.urls",
		"knowledge_import_directory":   "knowledge.import.directory",
		"knowledge_import_share":       "knowledge.import.package",
		"knowledge_import_package":     "knowledge.import.package",
		"public_knowledge_import_urls": "knowledge.public.import.urls",
		"public_knowledge_import_file": "knowledge.public.import.file",
		"public_knowledge_import_text": "knowledge.public.import.text",
	}[job.Kind]
	if expectedKind == "" {
		return agentruntime.JobReconcileResult{}, nil
	}
	if result := agentruntime.ReconcileFromProtectedEffects(effects, expectedKind, "knowledge effect failed", func(raw json.RawMessage) json.RawMessage {
		return sanitizeCommittedEffectPayload(r.server.svc.DataRoot(), raw)
	}); result.Resolved {
		return result, nil
	}
	store := r.server.knowledgeMgr.Store()
	if store == nil {
		return agentruntime.JobReconcileResult{}, nil
	}
	for _, effect := range effects {
		if effect.Kind != expectedKind {
			continue
		}
		if effect.State != agentruntime.JobEffectPrepared && effect.State != agentruntime.JobEffectUnknown {
			continue
		}
		ids := agentruntime.ParseJobEffectResourceIDs(effect.ResourceID)
		if len(ids) == 0 {
			continue
		}
		found := make([]string, 0, len(ids))
		for _, id := range ids {
			source, err := store.GetSource(ctx, id)
			if err == nil && strings.TrimSpace(source.ID) != "" {
				found = append(found, source.ID)
			}
		}
		if len(found) == 0 {
			continue
		}
		payload, _ := json.Marshal(map[string]any{"status": "reconciled", "source_ids": found})
		return agentruntime.JobReconcileResult{Resolved: true, Status: agentruntime.JobStatusSucceeded, Result: payload}, nil
	}
	return agentruntime.JobReconcileResult{}, nil
}
