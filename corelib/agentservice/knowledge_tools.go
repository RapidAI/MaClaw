package agentservice

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// knowledge_tools.go implements the knowledge-base management tools that align
// the MaClawSrv agent's capabilities with the MaClaw GUI knowledge tool
// surface. Handlers are thin wrappers over the shared KnowledgeStore
// interface; all scope fields (tenant/owner) are injected from the caller's
// principal, never from tool arguments.

// knowledgeToolJSONResult marshals a tool result map to JSON, or returns an
// "Error: ..." string (the prefix marks a failed outcome for knowledgeToolResult).
func knowledgeToolJSONResult(payload map[string]interface{}, err error) string {
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	data, mErr := json.Marshal(payload)
	if mErr != nil {
		return fmt.Sprintf("Error: failed to encode result: %v", mErr)
	}
	return string(data)
}

// knowledgeListSourcesOptionsFromArgs builds ListSourcesOptions from tool
// arguments, injecting the caller's tenant/owner scope. maxLimit caps the
// caller-supplied limit (500 for interactive lists, 5000 for bulk operations).
func knowledgeListSourcesOptionsFromArgs(args map[string]interface{}, tenantID, ownerID string, defaultLimit, maxLimit int) knowledge.ListSourcesOptions {
	limit := intArg(args, "limit", defaultLimit)
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return knowledge.ListSourcesOptions{
		TenantID:       tenantID,
		OwnerID:        ownerID,
		BatchID:        stringArg(args, "batch_id"),
		ProjectPath:    stringArg(args, "project_path"),
		SourceIDs:      toStringSlice(args["source_ids"]),
		SourceID:       firstStringArg(args, "source_id", "id"),
		Status:         stringArg(args, "status"),
		Kind:           stringArg(args, "kind"),
		SourceKinds:    toStringSlice(args["source_kinds"]),
		Domain:         stringArg(args, "domain"),
		Label:          stringArg(args, "label"),
		Labels:         toStringSlice(args["labels"]),
		Query:          stringArg(args, "query"),
		CoverageFilter: stringArg(args, "coverage_filter"),
		Limit:          limit,
	}
}

func (c *coreAgentCallbacks) executeKnowledgeListSources(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	opts := knowledgeListSourcesOptionsFromArgs(args, c.principal.TenantID, c.principal.UserID, 50, 500)
	sources, err := c.knowledgeStore.ListSources(c.parentContext(), opts)
	return knowledgeToolJSONResult(map[string]interface{}{"count": len(sources), "sources": sources}, err)
}

func (c *coreAgentCallbacks) executeKnowledgeSourceDetail(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	id := firstStringArg(args, "source_id", "id")
	if id == "" {
		return "Error: source_id is required"
	}
	source, err := c.knowledgeSourceForRead(id)
	return knowledgeToolJSONResult(map[string]interface{}{"source": source}, err)
}

func (c *coreAgentCallbacks) executeKnowledgeStats(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	// Multi-tenant hosts must not see global aggregate counts: aggregate from
	// the caller's readable sources instead of the store-wide Stats.
	if strings.TrimSpace(c.principal.TenantID) != "" || strings.TrimSpace(c.principal.UserID) != "" {
		sources, err := c.knowledgeStore.ListSources(c.parentContext(), knowledge.ListSourcesOptions{
			TenantID: c.principal.TenantID,
			OwnerID:  c.principal.UserID,
			Limit:    500,
		})
		if err != nil {
			return knowledgeToolJSONResult(nil, err)
		}
		return knowledgeToolJSONResult(map[string]interface{}{"stats": knowledgeStatsFromSourcesList(sources)}, nil)
	}
	stats, err := c.knowledgeStore.Stats(c.parentContext())
	return knowledgeToolJSONResult(map[string]interface{}{"stats": stats}, err)
}

// knowledgeStatsFromSourcesList aggregates per-source counts into knowledge.Stats
// (mirrors MaClawSrv's knowledgeStatsFromSources for the HTTP stats endpoint).
func knowledgeStatsFromSourcesList(sources []knowledge.Source) knowledge.Stats {
	stats := knowledge.Stats{
		SourcesByKind:   make(map[string]int),
		SourcesByStatus: make(map[string]int),
		SourcesByDomain: make(map[string]int),
		SourcesByLabel:  make(map[string]int),
	}
	for _, source := range sources {
		stats.Sources++
		stats.DocumentNodes += source.NodeCount
		stats.Cards += source.CardCount
		stats.Facts += source.FactCount
		key := func(v string) string {
			if v = strings.TrimSpace(v); v != "" {
				return v
			}
			return "unknown"
		}
		stats.SourcesByKind[key(source.Kind)]++
		stats.SourcesByStatus[key(source.Status)]++
		if domain := strings.ToLower(strings.TrimSpace(source.SiteName)); domain != "" {
			stats.SourcesByDomain[domain]++
		}
		for _, label := range source.Labels {
			if label = strings.TrimSpace(label); label != "" {
				stats.SourcesByLabel[label]++
			}
		}
	}
	return stats
}

func (c *coreAgentCallbacks) executeKnowledgeListSourceLabels(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	opts := knowledgeListSourcesOptionsFromArgs(args, c.principal.TenantID, c.principal.UserID, 1000, 5000)
	labels, err := c.knowledgeStore.ListSourceLabels(c.parentContext(), opts)
	return knowledgeToolJSONResult(map[string]interface{}{"count": len(labels), "labels": labels}, err)
}

func (c *coreAgentCallbacks) executeKnowledgeUpdateSourceLabels(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	limit := intArg(args, "limit", 1000)
	if limit <= 0 {
		limit = 1000
	}
	if limit > 5000 {
		limit = 5000
	}
	req := knowledge.SourceLabelUpdateRequest{
		SourceIDs:     toStringSlice(args["source_ids"]),
		Filter:        knowledgeListSourcesOptionsFromArgs(args, c.principal.TenantID, c.principal.UserID, limit, 5000),
		AddLabels:     toStringSlice(args["add_labels"]),
		RemoveLabels:  toStringSlice(args["remove_labels"]),
		ReplaceLabels: toStringSlice(args["replace_labels"]),
		RenameFrom:    stringArg(args, "rename_from"),
		RenameTo:      stringArg(args, "rename_to"),
		ClearLabels:   boolArg(args, "clear_labels", false),
		DryRun:        boolArg(args, "dry_run", false),
		Limit:         limit,
	}
	if len(req.SourceIDs) == 0 && strings.TrimSpace(req.RenameFrom) == "" && len(req.RemoveLabels) == 0 && !knowledgeHasSourceFilterArgs(args) {
		return "Error: provide source_ids or at least one source filter before bulk label updates"
	}
	// Explicit source IDs must belong to the caller's own scope.
	for _, id := range req.SourceIDs {
		if _, err := c.knowledgeSourceForWrite(id); err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
	}
	if strings.TrimSpace(req.RenameFrom) != "" && strings.TrimSpace(req.RenameTo) == "" {
		return "Error: provide rename_to when rename_from is set"
	}
	if len(req.AddLabels) == 0 && len(req.RemoveLabels) == 0 && len(req.ReplaceLabels) == 0 && strings.TrimSpace(req.RenameFrom) == "" && !req.ClearLabels {
		return "Error: provide add_labels, remove_labels, replace_labels, rename_from/rename_to, or clear_labels"
	}
	result, err := c.knowledgeStore.UpdateSourceLabels(c.parentContext(), req)
	return knowledgeToolJSONResult(map[string]interface{}{"result": result}, err)
}

// knowledgeHasSourceFilterArgs reports whether args carry any source filter
// (mirrors the GUI's hasKnowledgeSourceFilterArgs semantics, trimmed to the
// filters supported by knowledgeListSourcesOptionsFromArgs).
func knowledgeHasSourceFilterArgs(args map[string]interface{}) bool {
	for _, key := range []string{"query", "kind", "status", "domain", "label", "batch_id", "source_id", "id", "coverage_filter", "project_path"} {
		if stringArg(args, key) != "" {
			return true
		}
	}
	for _, key := range []string{"source_kinds", "labels", "source_ids"} {
		if len(toStringSlice(args[key])) > 0 {
			return true
		}
	}
	return false
}

func (c *coreAgentCallbacks) executeKnowledgeUpdateSourceMetadata(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	id := firstStringArg(args, "source_id", "id")
	if id == "" {
		return "Error: source_id is required"
	}
	if _, err := c.knowledgeSourceForWrite(id); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	req := knowledge.SourceUpdateRequest{
		ID:        id,
		Title:     stringArg(args, "title"),
		TopicHint: stringArg(args, "topic_hint"),
		Labels:    toStringSlice(args["labels"]),
	}
	source, err := c.knowledgeStore.UpdateSourceMetadata(c.parentContext(), req)
	return knowledgeToolJSONResult(map[string]interface{}{"source": source}, err)
}

// executeKnowledgeSetSourceStatus handles knowledge_enable_source and
// knowledge_disable_source. Accepts a single source_id or a source_ids list.
func (c *coreAgentCallbacks) executeKnowledgeSetSourceStatus(args map[string]interface{}, enable bool) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	ids := toStringSlice(args["source_ids"])
	if single := firstStringArg(args, "source_id", "id"); single != "" {
		ids = append(ids, single)
	}
	ids = compactUniqueStrings(ids)
	if len(ids) == 0 {
		return "Error: source_id or source_ids is required"
	}
	updated := make([]knowledge.Source, 0, len(ids))
	failures := map[string]string{}
	for _, id := range ids {
		if _, err := c.knowledgeSourceForWrite(id); err != nil {
			failures[id] = err.Error()
			continue
		}
		var (
			source knowledge.Source
			err    error
		)
		if enable {
			source, err = c.knowledgeStore.EnableSource(c.parentContext(), id)
		} else {
			source, err = c.knowledgeStore.DisableSource(c.parentContext(), id)
		}
		if err != nil {
			failures[id] = err.Error()
			continue
		}
		updated = append(updated, source)
	}
	payload := map[string]interface{}{
		"updated_count": len(updated),
		"updated":       updated,
	}
	if len(failures) > 0 {
		payload["failures"] = failures
	}
	if len(updated) == 0 && len(failures) > 0 {
		return fmt.Sprintf("Error: no sources were updated: %s", knowledgeFailureSummary(failures))
	}
	return knowledgeToolJSONResult(payload, nil)
}

func (c *coreAgentCallbacks) executeKnowledgeDeleteSource(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	ids := toStringSlice(args["source_ids"])
	if single := firstStringArg(args, "source_id", "id"); single != "" {
		ids = append(ids, single)
	}
	ids = compactUniqueStrings(ids)
	if len(ids) == 0 {
		return "Error: source_id or source_ids is required"
	}
	deleted := make([]string, 0, len(ids))
	failures := map[string]string{}
	for _, id := range ids {
		if _, err := c.knowledgeSourceForWrite(id); err != nil {
			failures[id] = err.Error()
			continue
		}
		if err := c.knowledgeStore.DeleteSource(c.parentContext(), id); err != nil {
			failures[id] = err.Error()
			continue
		}
		deleted = append(deleted, id)
	}
	payload := map[string]interface{}{
		"deleted_count": len(deleted),
		"deleted":       deleted,
	}
	if len(failures) > 0 {
		payload["failures"] = failures
	}
	if len(deleted) == 0 && len(failures) > 0 {
		return fmt.Sprintf("Error: no sources were deleted: %s", knowledgeFailureSummary(failures))
	}
	return knowledgeToolJSONResult(payload, nil)
}

// executeKnowledgeRefreshSource refreshes a source from its origin. dry_run
// defaults to true (aligned with the GUI): preview the change without writing.
func (c *coreAgentCallbacks) executeKnowledgeRefreshSource(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	id := firstStringArg(args, "source_id", "id")
	if id == "" {
		return "Error: source_id is required"
	}
	if _, err := c.knowledgeSourceForWrite(id); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	if boolArg(args, "dry_run", true) {
		preview, err := c.knowledgeStore.PreviewSourceRefresh(c.parentContext(), id)
		return knowledgeToolJSONResult(map[string]interface{}{"dry_run": true, "preview": preview}, err)
	}
	source, err := c.knowledgeStore.RefreshSource(c.parentContext(), id)
	return knowledgeToolJSONResult(map[string]interface{}{"dry_run": false, "source": source}, err)
}

func (c *coreAgentCallbacks) executeKnowledgeListImportBatches(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	limit := intArg(args, "limit", 20)
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	batches, err := c.knowledgeStore.ListImportBatches(c.parentContext(), limit)
	if err != nil {
		return knowledgeToolJSONResult(nil, err)
	}
	// Only expose the caller's own import batches (multi-tenant safety).
	tenantID := strings.TrimSpace(c.principal.TenantID)
	ownerID := strings.TrimSpace(c.principal.UserID)
	if tenantID != "" || ownerID != "" {
		own := batches[:0]
		for _, batch := range batches {
			if strings.TrimSpace(batch.TenantID) == tenantID && strings.TrimSpace(batch.OwnerID) == ownerID {
				own = append(own, batch)
			}
		}
		batches = own
	}
	return knowledgeToolJSONResult(map[string]interface{}{"count": len(batches), "batches": batches}, nil)
}

func (c *coreAgentCallbacks) executeKnowledgeListImportItems(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	batchID := firstStringArg(args, "batch_id", "id")
	if batchID == "" {
		return "Error: batch_id is required"
	}
	if _, err := c.knowledgeImportBatchForWrite(batchID); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	limit := intArg(args, "limit", 100)
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	items, err := c.knowledgeStore.ListImportItems(c.parentContext(), batchID, limit)
	return knowledgeToolJSONResult(map[string]interface{}{"count": len(items), "items": items}, err)
}

func (c *coreAgentCallbacks) executeKnowledgeRetryImportBatch(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	batchID := firstStringArg(args, "batch_id", "id")
	if batchID == "" {
		return "Error: batch_id is required"
	}
	if _, err := c.knowledgeImportBatchForWrite(batchID); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	req := knowledge.ImportRetryRequest{BatchID: batchID}
	result, err := c.knowledgeStore.RetryImportBatch(c.parentContext(), req)
	return knowledgeToolJSONResult(map[string]interface{}{"result": result}, err)
}

func (c *coreAgentCallbacks) executeKnowledgeDeleteImportBatch(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	batchID := firstStringArg(args, "batch_id", "id")
	if batchID == "" {
		return "Error: batch_id is required"
	}
	if _, err := c.knowledgeImportBatchForWrite(batchID); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	req := knowledge.ImportBatchDeleteRequest{BatchID: batchID}
	result, err := c.knowledgeStore.DeleteImportBatch(c.parentContext(), req)
	return knowledgeToolJSONResult(map[string]interface{}{"result": result}, err)
}

// executeKnowledgeSaveURLs saves multiple URLs into the knowledge base in one
// call (aligned with the GUI knowledge_save_urls tool).
func (c *coreAgentCallbacks) executeKnowledgeSaveURLs(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	urls := toStringSlice(args["urls"])
	if single := firstStringArg(args, "url"); single != "" {
		urls = append(urls, single)
	}
	urls = compactUniqueStrings(urls)
	if len(urls) == 0 {
		return "Error: urls is required"
	}
	if len(urls) > 20 {
		return "Error: at most 20 urls per call"
	}
	saved := make([]knowledge.Source, 0, len(urls))
	failures := map[string]string{}
	for _, rawURL := range urls {
		source, err := c.knowledgeStore.SaveURL(c.parentContext(), knowledge.URLSaveRequest{
			URL:       rawURL,
			TenantID:  c.principal.TenantID,
			OwnerID:   c.principal.UserID,
			TopicHint: stringArg(args, "topic_hint"),
			Labels:    toStringSlice(args["labels"]),
		})
		if err != nil {
			failures[rawURL] = err.Error()
			continue
		}
		saved = append(saved, source)
	}
	payload := map[string]interface{}{
		"saved_count": len(saved),
		"saved":       saved,
	}
	if len(failures) > 0 {
		payload["failures"] = failures
	}
	if len(saved) == 0 && len(failures) > 0 {
		return fmt.Sprintf("Error: no URLs were saved: %s", knowledgeFailureSummary(failures))
	}
	return knowledgeToolJSONResult(payload, nil)
}

// knowledgeFailureSummary renders an id→error map as a compact one-line
// summary for tool error results.
func knowledgeFailureSummary(failures map[string]string) string {
	parts := make([]string, 0, len(failures))
	for id, msg := range failures {
		parts = append(parts, fmt.Sprintf("%s (%s)", id, msg))
	}
	if len(parts) > 3 {
		return fmt.Sprintf("%s, and %d more", strings.Join(parts[:3], "; "), len(parts)-3)
	}
	return strings.Join(parts, "; ")
}

// compactUniqueStrings trims, drops empties, and de-duplicates preserving order.
func compactUniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// knowledgePrincipalOwnsSource reports whether the source belongs to the
// caller's own scope. Empty principal (single-user hosts) owns everything.
func (c *coreAgentCallbacks) knowledgePrincipalOwnsSource(source knowledge.Source) bool {
	tenantID := strings.TrimSpace(c.principal.TenantID)
	ownerID := strings.TrimSpace(c.principal.UserID)
	if tenantID == "" && ownerID == "" {
		return true
	}
	return strings.TrimSpace(source.TenantID) == tenantID && strings.TrimSpace(source.OwnerID) == ownerID
}

// knowledgeSourceForRead loads a source after verifying it is visible to the
// caller (own scope or a readable shared/public scope).
func (c *coreAgentCallbacks) knowledgeSourceForRead(id string) (knowledge.Source, error) {
	source, err := c.knowledgeStore.GetSource(c.parentContext(), id)
	if err != nil {
		return knowledge.Source{}, err
	}
	if c.knowledgePrincipalOwnsSource(source) {
		return source, nil
	}
	visible, err := c.knowledgeStore.ListSources(c.parentContext(), knowledge.ListSourcesOptions{
		TenantID: c.principal.TenantID,
		OwnerID:  c.principal.UserID,
		SourceID: id,
		Limit:    1,
	})
	if err != nil {
		return knowledge.Source{}, err
	}
	if len(visible) == 0 {
		return knowledge.Source{}, fmt.Errorf("source %s not found or not accessible", id)
	}
	return source, nil
}

// knowledgeSourceForWrite loads a source after verifying it belongs to the
// caller's own scope. Management/write operations never touch shared scopes.
func (c *coreAgentCallbacks) knowledgeSourceForWrite(id string) (knowledge.Source, error) {
	source, err := c.knowledgeStore.GetSource(c.parentContext(), id)
	if err != nil {
		return knowledge.Source{}, err
	}
	if !c.knowledgePrincipalOwnsSource(source) {
		return knowledge.Source{}, fmt.Errorf("source %s belongs to another user or shared scope; only own knowledge can be modified", id)
	}
	return source, nil
}

// knowledgeImportBatchForWrite loads an import batch after verifying it
// belongs to the caller's own scope.
func (c *coreAgentCallbacks) knowledgeImportBatchForWrite(batchID string) (knowledge.ImportBatch, error) {
	batch, err := c.knowledgeStore.GetImportBatch(c.parentContext(), batchID)
	if err != nil {
		return knowledge.ImportBatch{}, err
	}
	tenantID := strings.TrimSpace(c.principal.TenantID)
	ownerID := strings.TrimSpace(c.principal.UserID)
	if tenantID == "" && ownerID == "" {
		return batch, nil
	}
	if strings.TrimSpace(batch.TenantID) != tenantID || strings.TrimSpace(batch.OwnerID) != ownerID {
		return knowledge.ImportBatch{}, fmt.Errorf("import batch %s belongs to another user; only own batches can be managed", batchID)
	}
	return batch, nil
}

// knowledgeManagementToolSpecs returns the knowledge-base management tool
// definitions that align the executor's tool surface with the MaClaw GUI.
// All are enabled only when a knowledge store is configured.
func (c *coreAgentCallbacks) knowledgeManagementToolSpecs() []coreToolSpec {
	enabled := c.knowledgeStore != nil
	disabledReason := ""
	if !enabled {
		disabledReason = "knowledge base is not configured"
	}
	strParam := func(desc string) map[string]interface{} {
		return map[string]interface{}{"type": "string", "description": desc}
	}
	strListParam := func(desc string) map[string]interface{} {
		return map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": desc}
	}
	intParam := func(desc string) map[string]interface{} {
		return map[string]interface{}{"type": "integer", "description": desc}
	}
	boolParam := func(desc string) map[string]interface{} {
		return map[string]interface{}{"type": "boolean", "description": desc}
	}
	obj := func(props map[string]interface{}, required ...string) map[string]interface{} {
		out := map[string]interface{}{"type": "object", "properties": props}
		if len(required) > 0 {
			out["required"] = required
		}
		return out
	}
	// sourceFilterProps is the shared filter vocabulary for list-style tools.
	sourceFilterProps := func() map[string]interface{} {
		return map[string]interface{}{
			"query":           strParam("Optional source text filter query."),
			"kind":            strParam("Optional source kind filter."),
			"source_kinds":    strListParam("Optional source kind list filter."),
			"status":          strParam("Optional source status filter: parsed, distilled, failed, stale, disabled."),
			"domain":          strParam("Optional URL/site domain filter."),
			"label":           strParam("Optional single source label/collection filter."),
			"labels":          strListParam("Optional labels/collections filter. Sources must have every supplied label."),
			"source_ids":      strListParam("Optional exact source IDs to restrict to."),
			"batch_id":        strParam("Optional import batch ID filter."),
			"coverage_filter": strParam("Optional coverage filter, e.g. missing_nodes, missing_cards, missing_facts, complete."),
			"project_path":    strParam("Optional project path filter."),
			"limit":           intParam("Max results, default 50, max 500."),
		}
	}
	withFilters := func(extra map[string]interface{}) map[string]interface{} {
		props := sourceFilterProps()
		for k, v := range extra {
			props[k] = v
		}
		return props
	}
	spec := func(name, desc string, params map[string]interface{}) coreToolSpec {
		return coreToolSpec{
			Name:           name,
			Description:    desc,
			Enabled:        enabled,
			DisabledReason: disabledReason,
			Parameters:     params,
		}
	}
	return []coreToolSpec{
		spec("knowledge_list_sources",
			"List saved knowledge sources from the local store without calling an LLM. Use when the user asks what has been saved, imported, indexed, or grouped by label. Supports query, kind, status, domain, label, and limit filters.",
			obj(sourceFilterProps())),
		spec("knowledge_source_detail",
			"Get full details of one knowledge source by ID: title, URI, kind, status, labels, timestamps, and error message.",
			obj(map[string]interface{}{
				"source_id": strParam("Source ID to inspect."),
				"id":        strParam("Alias for source_id."),
			})),
		spec("knowledge_stats",
			"Get aggregate knowledge base statistics: source/node/card/fact counts grouped by kind and status.",
			obj(map[string]interface{}{})),
		spec("knowledge_list_source_labels",
			"List source labels/collections in the local knowledge base with counts. Supports the same filters as knowledge_list_sources.",
			obj(sourceFilterProps())),
		spec("knowledge_update_source_labels",
			"Add, remove, replace, or rename labels on knowledge sources, either by explicit source_ids or by a source filter. Supports dry_run to preview affected sources before writing.",
			obj(withFilters(map[string]interface{}{
				"add_labels":     strListParam("Labels to add."),
				"remove_labels":  strListParam("Labels to remove."),
				"replace_labels": strListParam("Replace all labels with this set."),
				"rename_from":    strParam("Rename this label..."),
				"rename_to":      strParam("...to this label."),
				"clear_labels":   boolParam("Remove all labels from matched sources."),
				"dry_run":        boolParam("Preview affected sources without writing. Default false."),
			}))),
		spec("knowledge_update_source_metadata",
			"Update a knowledge source's title, topic hint, or labels.",
			obj(map[string]interface{}{
				"source_id":  strParam("Source ID to update."),
				"id":         strParam("Alias for source_id."),
				"title":      strParam("New title."),
				"topic_hint": strParam("New topic hint."),
				"labels":     strListParam("Replacement label set."),
			})),
		spec("knowledge_enable_source",
			"Re-enable one or more disabled knowledge sources so they participate in retrieval again.",
			obj(map[string]interface{}{
				"source_id":  strParam("Source ID to enable."),
				"id":         strParam("Alias for source_id."),
				"source_ids": strListParam("Multiple source IDs to enable."),
			})),
		spec("knowledge_disable_source",
			"Disable one or more knowledge sources so they are excluded from retrieval without deleting them.",
			obj(map[string]interface{}{
				"source_id":  strParam("Source ID to disable."),
				"id":         strParam("Alias for source_id."),
				"source_ids": strListParam("Multiple source IDs to disable."),
			})),
		spec("knowledge_delete_source",
			"Permanently delete one or more knowledge sources and their derived content. Only use after the user explicitly confirms deletion.",
			obj(map[string]interface{}{
				"source_id":  strParam("Source ID to delete."),
				"id":         strParam("Alias for source_id."),
				"source_ids": strListParam("Multiple source IDs to delete."),
			})),
		spec("knowledge_refresh_source",
			"Re-fetch a knowledge source from its origin (URL/directory) and update derived content. dry_run defaults to true: previews whether the source changed without writing.",
			obj(map[string]interface{}{
				"source_id": strParam("Source ID to refresh."),
				"id":        strParam("Alias for source_id."),
				"dry_run":   boolParam("Preview changes without writing. Default true."),
			})),
		spec("knowledge_list_import_batches",
			"List recent knowledge import batches (directory/file/URL/share imports) with per-batch counts and status.",
			obj(map[string]interface{}{
				"limit": intParam("Max batches, default 20, max 100."),
			})),
		spec("knowledge_list_import_items",
			"List the individual items (files/URLs) of one knowledge import batch, including per-item status and errors.",
			obj(map[string]interface{}{
				"batch_id": strParam("Import batch ID."),
				"id":       strParam("Alias for batch_id."),
				"limit":    intParam("Max items, default 100, max 1000."),
			})),
		spec("knowledge_retry_import_batch",
			"Retry the failed items of a knowledge import batch.",
			obj(map[string]interface{}{
				"batch_id": strParam("Import batch ID to retry."),
				"id":       strParam("Alias for batch_id."),
			})),
		spec("knowledge_delete_import_batch",
			"Delete a knowledge import batch record. Only use after the user explicitly confirms.",
			obj(map[string]interface{}{
				"batch_id": strParam("Import batch ID to delete."),
				"id":       strParam("Alias for batch_id."),
			})),
		spec("knowledge_save_urls",
			"Save multiple URLs into the knowledge base in one call. Each page is fetched, parsed, and indexed for future retrieval. Max 20 URLs per call.",
			obj(map[string]interface{}{
				"urls":       strListParam("URLs to save (max 20)."),
				"topic_hint": strParam("Optional topic hint applied to all URLs."),
				"labels":     strListParam("Labels applied to all saved sources."),
			}, "urls")),
		spec("knowledge_import_hub_share",
			"Import a Hub knowledge share by share link or knowledge_id — same capability as knowledge_import_share, with a dry_run preview mode (default true) that reports importable/skipped counts before writing. Call with dry_run=false to actually import.",
			obj(map[string]interface{}{
				"knowledge_id": strParam("Unique shared knowledge ID."),
				"share_link":   strParam("Human-readable share link (e.g. https://hub.example.com/hub/knowledge/shares/kn_xxx)."),
				"link":         strParam("Alias for share_link."),
				"url":          strParam("Alias for share_link."),
				"hub_url":      strParam("Optional Hub URL hint."),
				"hub_token":    strParam("Optional Hub viewer token for private shares."),
				"dry_run":      boolParam("Preview importable/skipped items without writing. Default true."),
			})),
	}
}
