package structureddata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

var connectorIDPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]{0,95}$`)

func (s *Service) UpsertExternalConnector(ctx context.Context, p Principal, connectorID string, in UpsertExternalConnectorInput) (*ExternalConnector, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := strings.TrimSpace(connectorID)
	if id == "" {
		id = strings.TrimSpace(in.ID)
	}
	if id == "" {
		id = connectorIDFromInput(in)
	}
	if !connectorIDPattern.MatchString(id) {
		return nil, fmt.Errorf("%w: connector id must use letters, numbers, dot, underscore, or hyphen", ErrInvalidInput)
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: connector name is required", ErrInvalidInput)
	}
	domain := strings.ToLower(strings.TrimSpace(in.Domain))
	if domain != "" {
		if _, err := normalizeIdentifier(domain, "domain"); err != nil {
			return nil, err
		}
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	createdAt := s.now().UTC()
	if existing, err := s.store.GetExternalConnector(ctx, p.TenantID, id); err == nil && existing != nil {
		createdAt = existing.CreatedAt
	} else if err != nil && !errors.Is(err, ErrRecordNotFound) {
		return nil, err
	}
	actions, err := normalizeConnectorActions(in.SubscribedActions)
	if err != nil {
		return nil, err
	}
	connector := ExternalConnector{
		ID:                id,
		TenantID:          p.TenantID,
		Domain:            domain,
		Name:              name,
		Kind:              strings.ToLower(strings.TrimSpace(in.Kind)),
		BaseURL:           strings.TrimSpace(in.BaseURL),
		AuthType:          strings.ToLower(strings.TrimSpace(in.AuthType)),
		TokenRef:          strings.TrimSpace(in.TokenRef),
		Enabled:           enabled,
		SubscribedActions: actions,
		Config:            cloneJSONMap(in.Config),
		CreatedBy:         p.UserID,
		CreatedAt:         createdAt,
		UpdatedAt:         s.now().UTC(),
	}
	out, err := s.store.UpsertExternalConnector(ctx, connector)
	if err == nil {
		s.audit(ctx, p, "connector.upsert", "", "connector", id, "Upserted external connector "+id, map[string]any{"domain": connector.Domain, "kind": connector.Kind, "action_count": len(actions)})
	}
	return out, err
}

func (s *Service) ListExternalConnectors(ctx context.Context, p Principal, in QueryExternalConnectorsInput) ([]ExternalConnector, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.store.ListExternalConnectors(ctx, p.TenantID, in)
}

func (s *Service) GetExternalConnector(ctx context.Context, p Principal, connectorID string) (*ExternalConnector, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.store.GetExternalConnector(ctx, p.TenantID, strings.TrimSpace(connectorID))
}

func (s *Service) DeleteExternalConnector(ctx context.Context, p Principal, connectorID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	connectorID = strings.TrimSpace(connectorID)
	err := s.store.DeleteExternalConnector(ctx, p.TenantID, connectorID)
	if err == nil {
		s.audit(ctx, p, "connector.delete", "", "connector", connectorID, "Deleted external connector "+connectorID, nil)
	}
	return err
}

func (s *Service) PatchExternalConnectorConfig(ctx context.Context, p Principal, connectorID string, in PatchConnectorConfigInput) (*ConnectorConfigPatchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	connectorID = strings.TrimSpace(connectorID)
	if connectorID == "" {
		return nil, fmt.Errorf("%w: connector_id is required", ErrInvalidInput)
	}
	if len(in.Patch) == 0 {
		return nil, fmt.Errorf("%w: patch is required", ErrInvalidInput)
	}
	connector, err := s.store.GetExternalConnector(ctx, p.TenantID, connectorID)
	if err != nil {
		return nil, err
	}
	previous := cloneJSONMap(connector.Config)
	patched := mergeJSONPatchMap(connector.Config, in.Patch)
	out := &ConnectorConfigPatchResult{
		Connector:      *connector,
		DryRun:         in.DryRun,
		PreviousConfig: previous,
		PatchedConfig:  patched,
	}
	if in.DryRun {
		return out, nil
	}
	connector.Config = patched
	connector.UpdatedAt = s.now().UTC()
	saved, err := s.store.UpsertExternalConnector(ctx, *connector)
	if err != nil {
		return nil, err
	}
	out.Connector = *saved
	s.audit(ctx, p, "connector.config.patch", "", "connector", connector.ID, "Patched external connector config "+connector.ID, map[string]any{"patch_keys": sortedKeys(in.Patch)})
	return out, nil
}

func (s *Service) TestExternalConnector(ctx context.Context, p Principal, connectorID string) (*ConnectorTestResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	connector, err := s.store.GetExternalConnector(ctx, p.TenantID, strings.TrimSpace(connectorID))
	if err != nil {
		return nil, err
	}
	result := &ConnectorTestResult{
		Connector: *connector,
		Valid:     true,
		EventAPI:  "/api/v1/data/events",
		Flow: []string{
			"GET /api/v1/data/event-contracts/{businessActionId}",
			"POST /api/v1/data/events with dry_run=true",
			"POST /api/v1/data/events with dry_run=false after validation",
			"GET /api/v1/data/events?business_action_id={businessActionId}",
		},
		Metadata: map[string]any{"does_not_call_remote": true},
	}
	for _, actionID := range connector.SubscribedActions {
		action, ok := findBusinessAction(actionID)
		binding := ConnectorContractBinding{ActionID: actionID, Valid: ok}
		if ok {
			contract := eventContractFromAction(*action)
			binding.Contract = &contract
		} else {
			binding.Error = "unknown business action"
			result.Valid = false
		}
		result.Bindings = append(result.Bindings, binding)
	}
	if len(result.Bindings) == 0 {
		result.Valid = false
		result.Metadata["warning"] = "connector has no subscribed business actions"
	}
	return result, nil
}

func (s *Service) ValidateExternalConnectorConfig(ctx context.Context, p Principal, connectorID string) (*ConnectorConfigValidationResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	connector, err := s.store.GetExternalConnector(ctx, p.TenantID, strings.TrimSpace(connectorID))
	if err != nil {
		return nil, err
	}
	result := &ConnectorConfigValidationResult{
		Connector: *connector,
		Valid:     true,
	}
	mappings, mappingIssues := connectorFieldMappingsFromConfig(connector.Config)
	for _, issue := range mappingIssues {
		result.Issues = append(result.Issues, issue)
	}
	for _, actionID := range connector.SubscribedActions {
		action, ok := findBusinessAction(actionID)
		if !ok {
			result.Issues = append(result.Issues, ConnectorConfigValidationIssue{Severity: "error", Code: "unknown_action", BusinessAction: actionID, Message: "Subscribed business action is not defined"})
			continue
		}
		allowed := map[string]struct{}{}
		for _, field := range connectorMappingTargetFields(*action) {
			allowed[field] = struct{}{}
		}
		required := map[string]struct{}{}
		for _, field := range action.RequiredFields {
			required[field] = struct{}{}
		}
		actionMapping := mappings[actionID]
		summary := ConnectorConfigValidationAction{ActionID: actionID, DatasetID: action.DatasetID, RequiredFields: append([]string(nil), action.RequiredFields...), Mapping: actionMapping}
		for field, source := range actionMapping {
			if strings.TrimSpace(source) == "" {
				result.Issues = append(result.Issues, ConnectorConfigValidationIssue{Severity: "error", Code: "empty_source_path", BusinessAction: actionID, Field: field, Message: "Connector field mapping source path is empty"})
			}
			if !validConnectorSourcePath(source) {
				result.Issues = append(result.Issues, ConnectorConfigValidationIssue{Severity: "error", Code: "invalid_source_path", BusinessAction: actionID, Field: field, Path: source, Message: "Connector field mapping source path must use dot-separated object keys"})
			}
			if _, ok := allowed[field]; !ok {
				summary.ExtraFields = append(summary.ExtraFields, field)
				result.Warnings = append(result.Warnings, ConnectorConfigValidationIssue{Severity: "warning", Code: "unknown_target_field", BusinessAction: actionID, Field: field, Path: source, Message: "Mapping target is not part of the business action input fields"})
				continue
			}
			summary.MappedFields = append(summary.MappedFields, field)
		}
		for field := range required {
			if _, ok := actionMapping[field]; !ok {
				summary.MissingFields = append(summary.MissingFields, field)
				result.Issues = append(result.Issues, ConnectorConfigValidationIssue{Severity: "error", Code: "missing_required_mapping", BusinessAction: actionID, Field: field, Message: "Required business field has no connector field mapping"})
			}
		}
		sort.Strings(summary.MappedFields)
		sort.Strings(summary.MissingFields)
		sort.Strings(summary.ExtraFields)
		result.Actions = append(result.Actions, summary)
	}
	for actionID := range mappings {
		if !connectorSubscribesAction(connector, actionID) {
			result.Warnings = append(result.Warnings, ConnectorConfigValidationIssue{Severity: "warning", Code: "unsubscribed_mapping", BusinessAction: actionID, Message: "Field mapping is configured for an action this connector is not subscribed to"})
		}
	}
	sort.Slice(result.Actions, func(i, j int) bool { return result.Actions[i].ActionID < result.Actions[j].ActionID })
	if len(result.Issues) > 0 {
		result.Valid = false
		result.RecommendedNext = []string{"fix connector config issues before enabling sync", "use suggest_connector_mapping with a representative sample payload", "run preview_connector_event after fixing mappings"}
	} else if len(result.Warnings) > 0 {
		result.RecommendedNext = []string{"review warnings, then run preview_connector_event before production sync"}
	} else {
		result.RecommendedNext = []string{"run preview_connector_event with a representative payload", "use sync_connector_batch dry_run before the first full sync"}
	}
	return result, nil
}

func (s *Service) CheckExternalConnectorReadiness(ctx context.Context, p Principal, connectorID string, in ConnectorReadinessInput) (*ConnectorReadinessResult, error) {
	connector, err := s.GetExternalConnector(ctx, p, connectorID)
	if err != nil {
		return nil, err
	}
	out := &ConnectorReadinessResult{Connector: *connector, Ready: true}
	addCheck := func(name, status, message string) {
		out.Checks = append(out.Checks, ConnectorReadinessCheck{Name: name, Status: status, Message: message})
		if status == "failed" {
			out.Ready = false
		}
	}
	test, err := s.TestExternalConnector(ctx, p, connectorID)
	if err != nil {
		return nil, err
	}
	out.Test = test
	if test.Valid {
		addCheck("contracts", "passed", "all subscribed business actions have event contracts")
	} else {
		addCheck("contracts", "failed", "one or more subscribed business actions cannot be bound to event contracts")
	}
	config, err := s.ValidateExternalConnectorConfig(ctx, p, connectorID)
	if err != nil {
		return nil, err
	}
	out.Config = config
	if config.Valid {
		addCheck("config", "passed", "connector config is valid for subscribed business actions")
	} else {
		addCheck("config", "failed", "connector config has blocking issues")
	}
	health, err := s.ConnectorHealth(ctx, p, connectorID)
	if err != nil {
		return nil, err
	}
	out.Health = health
	switch health.Status {
	case "ok":
		addCheck("health", "passed", "connector has no current health blockers")
	case "degraded":
		addCheck("health", "warning", "connector health is degraded; review sync state and dead letters")
	default:
		addCheck("health", "warning", "connector health status is "+health.Status)
	}
	if in.SampleEvent != nil {
		sample := *in.SampleEvent
		if strings.TrimSpace(sample.BusinessAction) == "" && len(connector.SubscribedActions) == 1 {
			sample.BusinessAction = connector.SubscribedActions[0]
		}
		preview, err := s.PreviewConnectorEvent(ctx, p, connectorID, sample)
		if err != nil {
			addCheck("sample_preview", "failed", err.Error())
		} else {
			out.Preview = preview
			if preview.DryRunResult != nil && preview.DryRunResult.Valid {
				addCheck("sample_preview", "passed", "sample event maps and validates successfully")
			} else {
				addCheck("sample_preview", "failed", "sample event did not pass dry-run validation")
			}
		}
	} else {
		addCheck("sample_preview", "warning", "no sample_event provided; run preview_connector_event before production sync")
	}
	if out.Ready {
		out.RecommendedNext = []string{"run sync_connector_batch with dry_run=true for the first page", "monitor connector health and dead letters after enabling sync"}
	} else {
		out.RecommendedNext = []string{"fix failed readiness checks", "run validate_connector_config and preview_connector_event again before enabling sync"}
	}
	return out, nil
}

func (s *Service) PlanConnectorSync(ctx context.Context, p Principal, connectorID string, in ConnectorSyncPlanInput) (*ConnectorSyncPlanResult, error) {
	connector, err := s.GetExternalConnector(ctx, p, connectorID)
	if err != nil {
		return nil, err
	}
	pageSize := in.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 500 {
		return nil, fmt.Errorf("%w: connector sync page_size must be less than or equal to 500", ErrInvalidInput)
	}
	readinessInput := ConnectorReadinessInput{SampleEvent: in.SampleEvent}
	if readinessInput.SampleEvent == nil && len(in.FirstPageEvents) > 0 {
		sample := in.FirstPageEvents[0]
		readinessInput.SampleEvent = &sample
	}
	readiness, err := s.CheckExternalConnectorReadiness(ctx, p, connectorID, readinessInput)
	if err != nil {
		return nil, err
	}
	out := &ConnectorSyncPlanResult{
		Connector: *connector,
		Ready:     readiness.Ready,
		Readiness: readiness,
		PageSize:  pageSize,
		Cursor:    strings.TrimSpace(in.Cursor),
	}
	addStep := func(name, action, endpoint, method string, required bool, status string, description string, body map[string]any) {
		out.Steps = append(out.Steps, ConnectorSyncPlanStep{
			Order:            len(out.Steps) + 1,
			Name:             name,
			Action:           action,
			Endpoint:         endpoint,
			Method:           method,
			Required:         required,
			Status:           status,
			Description:      description,
			Body:             body,
			ToolCallTemplate: connectorSyncToolCall(action, connector.ID, body),
		})
	}
	basePath := "/api/v1/data/connectors/" + connector.ID
	addStep("Read connector sync state", "get_connector_sync_state", basePath+"/sync-state", "GET", true, "planned", "Read cursor/checkpoint before pulling a page from the external system.", nil)
	addStep("Run readiness checks", "check_connector_readiness", basePath+"/readiness", "POST", true, readinessStatus(readiness.Ready), "Confirm contracts, config, health, and sample preview before syncing.", nil)
	addStep("Pull external page", "external_pull", "", "", true, "manual", "Connector implementation pulls at most page_size records from the external system using the stored cursor/checkpoint.", map[string]any{"page_size": pageSize, "cursor": out.Cursor})
	dryRunBody := map[string]any{"dry_run": true, "stop_on_error": true, "events": []any{}}
	if len(in.FirstPageEvents) > 0 {
		events := make([]any, 0, len(in.FirstPageEvents))
		for _, event := range in.FirstPageEvents {
			events = append(events, event)
		}
		dryRunBody["events"] = events
	}
	addStep("Dry-run first sync page", "sync_connector_batch", basePath+"/sync-batch", "POST", true, "planned", "Validate the first page without writing records or event logs.", dryRunBody)
	commitBody := map[string]any{"dry_run": false, "stop_on_error": true, "events": "<same events after dry-run passes>", "sync_state": map[string]any{"status": "success", "cursor": "<next_cursor>"}}
	addStep("Commit first sync page", "sync_connector_batch", basePath+"/sync-batch", "POST", true, "blocked_until_dry_run_passes", "Write the page only after readiness and dry-run pass.", commitBody)
	addStep("Update sync checkpoint", "update_connector_sync_state", basePath+"/sync-state", "POST", true, "planned", "Persist cursor/checkpoint after each successful page.", map[string]any{"status": "success", "cursor": "<next_cursor>", "synced_records": "<page_count>"})
	addStep("Monitor failures", "list_event_dead_letters", "/api/v1/data/events/dead-letter", "GET", true, "planned", "Inspect dead letters and connector health after enabling sync.", map[string]any{"source": firstNonEmpty(connector.Kind, connector.ID), "status": "open"})
	out.Rollback = []ConnectorSyncPlanStep{
		{Order: 1, Name: "Pause connector sync", Action: "update_connector_sync_state", Endpoint: basePath + "/sync-state", Method: "POST", Required: true, Status: "planned", Description: "Mark sync paused or failed so the agent does not continue pulling pages.", Body: map[string]any{"status": "paused", "message": "paused by sync plan rollback"}},
		{Order: 2, Name: "Inspect dead letters", Action: "list_event_dead_letters", Endpoint: "/api/v1/data/events/dead-letter", Method: "GET", Required: true, Status: "planned", Description: "Review failed payloads before retrying or resolving.", Body: map[string]any{"source": firstNonEmpty(connector.Kind, connector.ID), "status": "open"}},
		{Order: 3, Name: "Restore from backup if needed", Action: "restore_backup", Endpoint: "/api/v1/data/backups/{backupId}", Method: "POST", Required: false, Status: "manual", Description: "Use only for explicit admin recovery after a risky production sync.", Body: map[string]any{"confirm": true}},
	}
	for i := range out.Rollback {
		out.Rollback[i].ToolCallTemplate = connectorSyncToolCall(out.Rollback[i].Action, connector.ID, out.Rollback[i].Body)
	}
	if len(in.FirstPageEvents) > 0 {
		dryRun, err := s.SyncConnectorBatch(ctx, p, connectorID, ConnectorSyncBatchInput{Events: in.FirstPageEvents, DryRun: true, StopOnError: true})
		if err != nil {
			addStep("Dry-run execution result", "sync_connector_batch", basePath+"/sync-batch", "POST", true, "failed", err.Error(), nil)
			out.Ready = false
		} else {
			out.DryRunBatch = dryRun
			if dryRun.Failed > 0 {
				out.Ready = false
			}
		}
	}
	if out.Ready {
		out.RecommendedNext = []string{"execute the dry-run first-page step with current external data", "commit only pages whose dry-run succeeded", "monitor connector health and dead letters after each page"}
	} else {
		out.RecommendedNext = []string{"fix readiness or dry-run failures before production sync", "run check_connector_readiness again with a representative sample_event"}
	}
	return out, nil
}

func readinessStatus(ready bool) string {
	if ready {
		return "passed"
	}
	return "failed"
}

func connectorSyncToolCall(action, connectorID string, body map[string]any) map[string]any {
	action = strings.TrimSpace(action)
	if action == "" || action == "external_pull" {
		return nil
	}
	out := map[string]any{"action": action}
	switch action {
	case "get_connector_sync_state", "check_connector_readiness", "update_connector_sync_state", "sync_connector_batch":
		out["connector_id"] = connectorID
	case "restore_backup":
		out["backup_id"] = "<backup_id>"
	}
	for key, value := range body {
		out[key] = value
	}
	return out
}

func (s *Service) GetConnectorSyncState(ctx context.Context, p Principal, connectorID string) (*ConnectorSyncState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	connector, err := s.store.GetExternalConnector(ctx, p.TenantID, strings.TrimSpace(connectorID))
	if err != nil {
		return nil, err
	}
	state := connectorSyncStateFromConfig(connector.Config)
	return &state, nil
}

func (s *Service) ListConnectorSyncRuns(ctx context.Context, p Principal, connectorID string, in QueryConnectorSyncRunsInput) ([]ConnectorSyncRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	connector, err := s.store.GetExternalConnector(ctx, p.TenantID, strings.TrimSpace(connectorID))
	if err != nil {
		return nil, err
	}
	return paginateConnectorSyncRuns(connectorSyncRunsFromConfig(connector.Config), in), nil
}

func (s *Service) UpdateConnectorSyncState(ctx context.Context, p Principal, connectorID string, in UpdateConnectorSyncStateInput) (*ConnectorSyncState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	connector, err := s.store.GetExternalConnector(ctx, p.TenantID, strings.TrimSpace(connectorID))
	if err != nil {
		return nil, err
	}
	state := connectorSyncStateFromConfig(connector.Config)
	if status := strings.ToLower(strings.TrimSpace(in.Status)); status != "" {
		switch status {
		case "idle", "running", "success", "failed", "paused":
			state.Status = status
		default:
			return nil, fmt.Errorf("%w: sync status must be idle, running, success, failed, or paused", ErrInvalidInput)
		}
	}
	if strings.TrimSpace(state.Status) == "" || state.Status == "unknown" {
		state.Status = "idle"
	}
	if strings.TrimSpace(in.Cursor) != "" {
		state.Cursor = strings.TrimSpace(in.Cursor)
	}
	if in.Checkpoint != nil {
		state.Checkpoint = cloneJSONMap(in.Checkpoint)
	}
	if strings.TrimSpace(in.LastError) != "" || state.Status == "failed" {
		state.LastError = strings.TrimSpace(in.LastError)
	}
	if state.Status == "success" {
		state.LastError = ""
	}
	if strings.TrimSpace(in.Message) != "" {
		state.Message = strings.TrimSpace(in.Message)
	}
	if in.SyncedRecords != nil {
		if *in.SyncedRecords < 0 {
			return nil, fmt.Errorf("%w: synced_records must be greater than or equal to 0", ErrInvalidInput)
		}
		state.SyncedRecords = *in.SyncedRecords
	}
	parsed, err := parseConnectorSyncTime("started_at", in.StartedAt)
	if err != nil {
		return nil, err
	}
	if !parsed.IsZero() {
		state.StartedAt = parsed
	}
	parsed, err = parseConnectorSyncTime("finished_at", in.FinishedAt)
	if err != nil {
		return nil, err
	}
	if !parsed.IsZero() {
		state.FinishedAt = parsed
	}
	state.UpdatedBy = p.UserID
	state.UpdatedAt = s.now().UTC()
	config := cloneJSONMap(connector.Config)
	config["sync_state"] = connectorSyncStateToMap(state)
	connector.Config = config
	connector.UpdatedAt = state.UpdatedAt
	if _, err := s.store.UpsertExternalConnector(ctx, *connector); err != nil {
		return nil, err
	}
	s.audit(ctx, p, "connector.sync_state.update", "", "connector", connector.ID, "Updated connector sync state", map[string]any{"status": state.Status, "cursor": state.Cursor, "synced_records": state.SyncedRecords})
	return &state, nil
}

func (s *Service) ConnectorHealth(ctx context.Context, p Principal, connectorID string) (*ConnectorHealth, error) {
	s.mu.RLock()
	connector, err := s.store.GetExternalConnector(ctx, p.TenantID, strings.TrimSpace(connectorID))
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	source := firstNonEmpty(connector.Kind, connector.ID)
	events, err := s.QueryDataEvents(ctx, p, QueryDataEventsInput{Source: source, Limit: 500})
	if err != nil {
		return nil, err
	}
	deadLetters, err := s.QueryDataEventDeadLetters(ctx, p, QueryDataEventDeadLettersInput{Source: source, Limit: 500})
	if err != nil {
		return nil, err
	}
	out := &ConnectorHealth{
		Connector:       *connector,
		Status:          "ok",
		Source:          source,
		Enabled:         connector.Enabled,
		SubscribedCount: len(connector.SubscribedActions),
		CheckedAt:       s.now().UTC(),
	}
	if len(events) > 0 {
		event := events[0]
		out.LastEvent = &event
	}
	syncState := connectorSyncStateFromConfig(connector.Config)
	if syncState.Status != "unknown" {
		out.SyncState = &syncState
	}
	openDeadLetters := 0
	for _, item := range deadLetters {
		if strings.TrimSpace(item.Status) == "" || item.Status == "open" {
			openDeadLetters++
		}
	}
	out.RecentEvents = len(events)
	out.RecentFailures = len(deadLetters)
	out.OpenDeadLetters = openDeadLetters
	if !connector.Enabled {
		out.Status = "disabled"
		out.Recommendations = append(out.Recommendations, "enable connector before expecting event writes")
	}
	if len(connector.SubscribedActions) == 0 {
		out.Status = "needs_attention"
		out.Recommendations = append(out.Recommendations, "add subscribed business actions")
	}
	if openDeadLetters > 0 {
		out.Status = "degraded"
		out.Recommendations = append(out.Recommendations, "review open dead letters and retry or resolve")
	}
	if out.SyncState != nil {
		switch out.SyncState.Status {
		case "failed":
			out.Status = "degraded"
			out.Recommendations = append(out.Recommendations, "inspect connector sync_state.last_error and resume from saved cursor")
		case "running":
			if out.Status == "ok" {
				out.Status = "syncing"
			}
		case "paused":
			if out.Status == "ok" || out.Status == "idle" {
				out.Status = "paused"
			}
		}
	}
	for _, actionID := range connector.SubscribedActions {
		actionHealth := ConnectorHealthAction{ActionID: actionID, Status: "ok"}
		for i := range events {
			if events[i].BusinessAction != actionID {
				continue
			}
			actionHealth.RecentEvents++
			if actionHealth.LastEvent == nil {
				event := events[i]
				actionHealth.LastEvent = &event
			}
		}
		for i := range deadLetters {
			if deadLetters[i].BusinessAction != actionID {
				continue
			}
			actionHealth.RecentFailures++
			if strings.TrimSpace(deadLetters[i].Status) == "" || deadLetters[i].Status == "open" {
				actionHealth.OpenDeadLetters++
			}
			if actionHealth.LastFailure == nil {
				actionHealth.LastFailure = map[string]any{
					"id":         deadLetters[i].ID,
					"status":     deadLetters[i].Status,
					"error":      deadLetters[i].Error,
					"created_at": deadLetters[i].CreatedAt,
				}
			}
		}
		if actionHealth.OpenDeadLetters > 0 {
			actionHealth.Status = "degraded"
		} else if actionHealth.RecentEvents == 0 && actionHealth.RecentFailures == 0 {
			actionHealth.Status = "idle"
		}
		out.Actions = append(out.Actions, actionHealth)
	}
	if out.Status == "ok" && out.RecentEvents == 0 && out.RecentFailures == 0 {
		out.Status = "idle"
		out.Recommendations = append(out.Recommendations, "no recent connector events observed")
	}
	return out, nil
}

func (s *Service) ListConnectorHealth(ctx context.Context, p Principal, in QueryExternalConnectorsInput) ([]ConnectorHealth, error) {
	connectors, err := s.ListExternalConnectors(ctx, p, in)
	if err != nil {
		return nil, err
	}
	out := []ConnectorHealth{}
	for _, connector := range connectors {
		health, err := s.ConnectorHealth(ctx, p, connector.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, *health)
	}
	return out, nil
}

func connectorSyncStateFromConfig(config map[string]any) ConnectorSyncState {
	state := ConnectorSyncState{Status: "unknown"}
	if config == nil {
		return state
	}
	raw, ok := config["sync_state"]
	if !ok {
		return state
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return state
	}
	_ = json.Unmarshal(data, &state)
	if strings.TrimSpace(state.Status) == "" {
		state.Status = "unknown"
	}
	if state.Checkpoint == nil {
		state.Checkpoint = map[string]any{}
	}
	return state
}

func connectorSyncStateToMap(state ConnectorSyncState) map[string]any {
	data, err := json.Marshal(state)
	if err != nil {
		return map[string]any{"status": state.Status}
	}
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	return out
}

func connectorSyncRunsFromConfig(config map[string]any) []ConnectorSyncRun {
	if config == nil {
		return []ConnectorSyncRun{}
	}
	raw, ok := config["sync_runs"]
	if !ok {
		return []ConnectorSyncRun{}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return []ConnectorSyncRun{}
	}
	out := []ConnectorSyncRun{}
	_ = json.Unmarshal(data, &out)
	return out
}

func paginateConnectorSyncRuns(runs []ConnectorSyncRun, in QueryConnectorSyncRunsInput) []ConnectorSyncRun {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := append([]ConnectorSyncRun(nil), runs...)
	sort.Slice(out, func(i, j int) bool {
		left := firstNonZeroTime(out[i].FinishedAt, out[i].StartedAt)
		right := firstNonZeroTime(out[j].FinishedAt, out[j].StartedAt)
		if left.Equal(right) {
			return out[i].ID > out[j].ID
		}
		return left.After(right)
	})
	before := strings.TrimSpace(in.Before)
	beforeID := strings.TrimSpace(in.BeforeID)
	if before != "" {
		filtered := out[:0]
		for _, run := range out {
			createdAt := firstNonZeroTime(run.FinishedAt, run.StartedAt).Format(time.RFC3339Nano)
			if createdAt < before || (beforeID != "" && createdAt == before && run.ID < beforeID) {
				filtered = append(filtered, run)
			}
		}
		out = filtered
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
func connectorSyncRunsToConfig(runs []ConnectorSyncRun) []any {
	if len(runs) > 20 {
		runs = runs[:20]
	}
	out := make([]any, 0, len(runs))
	for _, run := range runs {
		data, err := json.Marshal(run)
		if err != nil {
			continue
		}
		obj := map[string]any{}
		_ = json.Unmarshal(data, &obj)
		out = append(out, obj)
	}
	return out
}

func (s *Service) appendConnectorSyncRun(ctx context.Context, p Principal, connectorID string, run ConnectorSyncRun) (*ConnectorSyncRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	connector, err := s.store.GetExternalConnector(ctx, p.TenantID, strings.TrimSpace(connectorID))
	if err != nil {
		return nil, err
	}
	runs := connectorSyncRunsFromConfig(connector.Config)
	runs = append([]ConnectorSyncRun{run}, runs...)
	if len(runs) > 20 {
		runs = runs[:20]
	}
	config := cloneJSONMap(connector.Config)
	config["sync_runs"] = connectorSyncRunsToConfig(runs)
	connector.Config = config
	connector.UpdatedAt = s.now().UTC()
	if _, err := s.store.UpsertExternalConnector(ctx, *connector); err != nil {
		return nil, err
	}
	s.audit(ctx, p, "connector.sync_run.append", "", "connector", connector.ID, "Appended connector sync run", map[string]any{"run_id": run.ID, "status": run.Status, "total": run.Total, "succeeded": run.Succeeded, "failed": run.Failed})
	return &run, nil
}

func parseConnectorSyncTime(field, raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("%w: %s must be RFC3339 or YYYY-MM-DD", ErrInvalidInput, field)
}

func (s *Service) IngestConnectorEvent(ctx context.Context, p Principal, connectorID string, in DataEventInput) (*DataEventResult, error) {
	connector, normalized, _, err := s.normalizeConnectorEvent(ctx, p, connectorID, in)
	if err != nil {
		return nil, err
	}
	result, err := s.IngestEvent(ctx, p, normalized)
	if err == nil {
		s.audit(ctx, p, "connector.event_ingest", result.DatasetID, "connector", connector.ID, "Ingested connector event", map[string]any{"business_action_id": result.BusinessAction, "event_type": result.EventType, "record_id": result.RecordID, "status": result.Status})
	}
	return result, err
}

func (s *Service) PreviewConnectorEvent(ctx context.Context, p Principal, connectorID string, in DataEventInput) (*ConnectorEventPreview, error) {
	connector, normalized, mapping, err := s.normalizeConnectorEvent(ctx, p, connectorID, in)
	if err != nil {
		return nil, err
	}
	previewEvent := normalized
	previewEvent.DryRun = true
	dryRun, err := s.IngestEvent(ctx, p, previewEvent)
	if err != nil {
		return nil, err
	}
	out := &ConnectorEventPreview{
		Connector:        *connector,
		BusinessAction:   firstNonEmpty(dryRun.BusinessAction, normalized.BusinessAction),
		Source:           firstNonEmpty(dryRun.Source, normalized.Source),
		EventType:        firstNonEmpty(dryRun.EventType, normalized.EventType),
		Operation:        firstNonEmpty(dryRun.Operation, normalized.Operation),
		DatasetID:        firstNonEmpty(dryRun.DatasetID, normalized.DatasetID),
		RecordID:         normalized.RecordID,
		IdempotencyKey:   normalized.IdempotencyKey,
		OriginalData:     cloneJSONMap(in.Data),
		MappedData:       cloneJSONMap(normalized.Data),
		MappingApplied:   mapping.Applied,
		MissingMappings:  mapping.Missing,
		NormalizedEvent:  normalized,
		DryRunResult:     dryRun,
		RecommendedWrite: "/api/v1/data/connectors/" + connector.ID + "/events",
	}
	return out, nil
}

func (s *Service) SuggestConnectorMapping(ctx context.Context, p Principal, connectorID string, in SuggestConnectorMappingInput) (*ConnectorMappingSuggestion, error) {
	connector, err := s.GetExternalConnector(ctx, p, connectorID)
	if err != nil {
		return nil, err
	}
	actionID := strings.TrimSpace(in.BusinessAction)
	if actionID == "" && len(connector.SubscribedActions) == 1 {
		actionID = connector.SubscribedActions[0]
	}
	if actionID == "" {
		return nil, fmt.Errorf("%w: business_action_id is required", ErrInvalidInput)
	}
	if !connectorSubscribesAction(connector, actionID) {
		return nil, fmt.Errorf("%w: connector is not subscribed to business action %s", ErrForbidden, actionID)
	}
	action, ok := findBusinessAction(actionID)
	if !ok {
		return nil, fmt.Errorf("%w: unknown business action %s", ErrInvalidInput, actionID)
	}
	if len(in.SampleData) == 0 {
		return nil, fmt.Errorf("%w: sample_data is required", ErrInvalidInput)
	}
	paths := flattenConnectorSamplePaths(in.SampleData)
	sort.Strings(paths)
	targets := connectorMappingTargetFields(*action)
	mapping := map[string]string{}
	confidence := map[string]float64{}
	unmatched := []string{}
	usedPaths := map[string]struct{}{}
	for _, target := range targets {
		path, score := bestConnectorMappingPath(target, paths, usedPaths)
		if path == "" || score < 0.45 {
			unmatched = append(unmatched, target)
			continue
		}
		mapping[target] = path
		confidence[target] = score
		usedPaths[path] = struct{}{}
	}
	configPatch := map[string]any{
		"field_mappings": map[string]any{
			actionID: mapping,
		},
	}
	out := &ConnectorMappingSuggestion{
		Connector:        *connector,
		BusinessAction:   actionID,
		DatasetID:        action.DatasetID,
		RequiredFields:   append([]string{}, action.RequiredFields...),
		SuggestedMapping: mapping,
		Confidence:       confidence,
		UnmatchedFields:  unmatched,
		SamplePaths:      paths,
		ConfigPatch:      configPatch,
		RecommendedNext: []string{
			"merge config_patch into connector config.field_mappings after review",
			"run preview_connector_event with the same sample before committing writes",
		},
	}
	return out, nil
}

func (s *Service) SyncConnectorBatch(ctx context.Context, p Principal, connectorID string, in ConnectorSyncBatchInput) (*ConnectorSyncBatchResult, error) {
	if len(in.Events) == 0 {
		return nil, fmt.Errorf("%w: events are required", ErrInvalidInput)
	}
	if len(in.Events) > 500 {
		return nil, fmt.Errorf("%w: connector sync batch supports at most 500 events", ErrInvalidInput)
	}
	connector, err := s.GetExternalConnector(ctx, p, connectorID)
	if err != nil {
		return nil, err
	}
	out := &ConnectorSyncBatchResult{
		Connector: *connector,
		Total:     len(in.Events),
		DryRun:    in.DryRun,
		Items:     []ConnectorSyncBatchItem{},
	}
	startedAt := s.now().UTC()
	for i, event := range in.Events {
		event.DryRun = in.DryRun || event.DryRun
		result, err := s.IngestConnectorEvent(ctx, p, connectorID, event)
		item := ConnectorSyncBatchItem{Index: i}
		if err != nil {
			item.Status = "failed"
			item.Error = err.Error()
			out.Failed++
			if !event.DryRun {
				if deadLetter, dlqErr := s.CreateDataEventDeadLetter(ctx, p, event, err); dlqErr == nil {
					item.DeadLetter = deadLetter
				}
			}
			out.Items = append(out.Items, item)
			if in.StopOnError {
				out.StoppedOnError = true
				break
			}
			continue
		}
		item.Status = firstNonEmpty(result.Status, "ok")
		item.Result = result
		out.Succeeded++
		out.Items = append(out.Items, item)
	}
	if in.SyncState != nil && !in.DryRun {
		stateInput := *in.SyncState
		if strings.TrimSpace(stateInput.Status) == "" {
			if out.Failed > 0 {
				stateInput.Status = "failed"
			} else {
				stateInput.Status = "success"
			}
		}
		if stateInput.SyncedRecords == nil {
			count := out.Succeeded
			stateInput.SyncedRecords = &count
		}
		if strings.TrimSpace(stateInput.LastError) == "" && out.Failed > 0 {
			for _, item := range out.Items {
				if item.Error != "" {
					stateInput.LastError = item.Error
					break
				}
			}
		}
		state, err := s.UpdateConnectorSyncState(ctx, p, connectorID, stateInput)
		if err != nil {
			return nil, err
		}
		out.SyncState = state
	}
	if !in.DryRun {
		runStatus := "success"
		if out.Failed > 0 {
			runStatus = "failed"
		}
		errorSummary := ""
		for _, item := range out.Items {
			if item.Error != "" {
				errorSummary = item.Error
				break
			}
		}
		run := ConnectorSyncRun{
			ID:             newID("syncrun"),
			Status:         runStatus,
			Total:          out.Total,
			Succeeded:      out.Succeeded,
			Failed:         out.Failed,
			DryRun:         out.DryRun,
			StoppedOnError: out.StoppedOnError,
			ErrorSummary:   errorSummary,
			StartedAt:      startedAt,
			FinishedAt:     s.now().UTC(),
			CreatedBy:      p.UserID,
		}
		if out.SyncState != nil {
			run.Cursor = out.SyncState.Cursor
		}
		if appended, err := s.appendConnectorSyncRun(ctx, p, connectorID, run); err != nil {
			return nil, err
		} else {
			out.Run = appended
		}
	}
	if out.Failed > 0 {
		out.RecommendedNext = append(out.RecommendedNext, "inspect batch item errors and open dead letters")
	}
	if out.SyncState != nil && out.SyncState.Cursor != "" {
		out.RecommendedNext = append(out.RecommendedNext, "resume next sync from cursor "+out.SyncState.Cursor)
	}
	return out, nil
}

type connectorMappingResult struct {
	Applied bool
	Missing []string
}

func (s *Service) normalizeConnectorEvent(ctx context.Context, p Principal, connectorID string, in DataEventInput) (*ExternalConnector, DataEventInput, connectorMappingResult, error) {
	s.mu.RLock()
	connector, err := s.store.GetExternalConnector(ctx, p.TenantID, strings.TrimSpace(connectorID))
	s.mu.RUnlock()
	if err != nil {
		return nil, DataEventInput{}, connectorMappingResult{}, err
	}
	if !connector.Enabled {
		return nil, DataEventInput{}, connectorMappingResult{}, fmt.Errorf("%w: connector is disabled", ErrForbidden)
	}
	actionID := strings.TrimSpace(in.BusinessAction)
	if actionID == "" && len(connector.SubscribedActions) == 1 {
		actionID = connector.SubscribedActions[0]
		in.BusinessAction = actionID
	}
	if actionID == "" {
		return nil, DataEventInput{}, connectorMappingResult{}, fmt.Errorf("%w: business_action_id is required for connector event", ErrInvalidInput)
	}
	if !connectorSubscribesAction(connector, actionID) {
		return nil, DataEventInput{}, connectorMappingResult{}, fmt.Errorf("%w: connector is not subscribed to business action %s", ErrForbidden, actionID)
	}
	if strings.TrimSpace(in.Source) == "" {
		in.Source = firstNonEmpty(connector.Kind, connector.ID)
	}
	mapped, changed, missing := applyConnectorFieldMapping(connector, actionID, in.Data)
	mapping := connectorMappingResult{Applied: changed, Missing: missing}
	if changed {
		in.Data = mapped
	}
	if strings.TrimSpace(in.RecordID) == "" {
		if action, ok := findBusinessAction(actionID); ok {
			in.RecordID = fillTemplateFromData(recordIDTemplateForAction(*action), in.Data)
		}
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		if action, ok := findBusinessAction(actionID); ok {
			in.IdempotencyKey = firstNonEmpty(fillTemplateFromData(idempotencyTemplateForAction(*action), in.Data), in.Source+":"+actionID+":"+strings.TrimSpace(in.RecordID)+":v1")
		}
	}
	return connector, in, mapping, nil
}

func connectorIDFromInput(in UpsertExternalConnectorInput) string {
	kind := strings.TrimSpace(in.Kind)
	if kind == "" {
		kind = "connector"
	}
	domain := strings.TrimSpace(in.Domain)
	if domain == "" {
		return strings.ToLower(kind)
	}
	return strings.ToLower(domain + "." + kind)
}

func normalizeConnectorActions(in []string) ([]string, error) {
	seen := map[string]struct{}{}
	out := []string{}
	for _, actionID := range in {
		actionID = strings.TrimSpace(actionID)
		if actionID == "" {
			continue
		}
		if _, ok := findBusinessAction(actionID); !ok {
			return nil, fmt.Errorf("%w: unknown business action %s", ErrInvalidInput, actionID)
		}
		if _, ok := seen[actionID]; ok {
			continue
		}
		seen[actionID] = struct{}{}
		out = append(out, actionID)
	}
	sort.Strings(out)
	return out, nil
}

func connectorSubscribesAction(connector *ExternalConnector, actionID string) bool {
	if connector == nil {
		return false
	}
	actionID = strings.TrimSpace(actionID)
	for _, subscribed := range connector.SubscribedActions {
		if subscribed == actionID {
			return true
		}
	}
	return false
}

func applyConnectorFieldMapping(connector *ExternalConnector, actionID string, data map[string]any) (map[string]any, bool, []string) {
	if connector == nil || len(data) == 0 || connector.Config == nil {
		return data, false, nil
	}
	mappingsAny, ok := connector.Config["field_mappings"]
	if !ok {
		return data, false, nil
	}
	mappings, ok := mappingsAny.(map[string]any)
	if !ok {
		return data, false, nil
	}
	actionMappingAny, ok := mappings[strings.TrimSpace(actionID)]
	if !ok {
		return data, false, nil
	}
	actionMapping, ok := actionMappingAny.(map[string]any)
	if !ok || len(actionMapping) == 0 {
		return data, false, nil
	}
	out := map[string]any{}
	missing := []string{}
	for target, sourcePathAny := range actionMapping {
		target = strings.TrimSpace(target)
		sourcePath := strings.TrimSpace(fmt.Sprint(sourcePathAny))
		if target == "" || sourcePath == "" {
			continue
		}
		if value, ok := valueAtConnectorPath(data, sourcePath); ok {
			out[target] = value
		} else {
			missing = append(missing, target+":"+sourcePath)
		}
	}
	if len(out) == 0 {
		return data, false, missing
	}
	if preserve, ok := connector.Config["preserve_unmapped_data"].(bool); ok && preserve {
		for key, value := range data {
			if _, exists := out[key]; !exists {
				out[key] = value
			}
		}
	}
	sort.Strings(missing)
	return out, true, missing
}

func connectorFieldMappingsFromConfig(config map[string]any) (map[string]map[string]string, []ConnectorConfigValidationIssue) {
	out := map[string]map[string]string{}
	issues := []ConnectorConfigValidationIssue{}
	if config == nil {
		return out, issues
	}
	mappingsAny, ok := config["field_mappings"]
	if !ok {
		return out, issues
	}
	mappings, ok := mappingsAny.(map[string]any)
	if !ok {
		return out, []ConnectorConfigValidationIssue{{Severity: "error", Code: "invalid_field_mappings", Message: "config.field_mappings must be an object keyed by business_action_id"}}
	}
	for actionID, actionMappingAny := range mappings {
		actionID = strings.TrimSpace(actionID)
		if actionID == "" {
			issues = append(issues, ConnectorConfigValidationIssue{Severity: "error", Code: "empty_mapping_action", Message: "config.field_mappings contains an empty business action key"})
			continue
		}
		actionMapping, ok := actionMappingAny.(map[string]any)
		if !ok {
			issues = append(issues, ConnectorConfigValidationIssue{Severity: "error", Code: "invalid_action_mapping", BusinessAction: actionID, Message: "Field mapping for a business action must be an object"})
			continue
		}
		out[actionID] = map[string]string{}
		for target, sourceAny := range actionMapping {
			target = strings.TrimSpace(target)
			source := strings.TrimSpace(fmt.Sprint(sourceAny))
			if target == "" {
				issues = append(issues, ConnectorConfigValidationIssue{Severity: "error", Code: "empty_target_field", BusinessAction: actionID, Path: source, Message: "Connector field mapping target field is empty"})
				continue
			}
			out[actionID][target] = source
		}
	}
	return out, issues
}

func validConnectorSourcePath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	parts := strings.Split(path, ".")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return false
		}
		for _, r := range part {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func connectorMappingTargetFields(action BusinessAction) []string {
	seen := map[string]struct{}{}
	out := []string{}
	add := func(key string) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	for _, key := range action.RequiredFields {
		add(key)
	}
	for _, field := range action.InputFields {
		add(field.Key)
	}
	return out
}

func flattenConnectorSamplePaths(data map[string]any) []string {
	out := []string{}
	var walk func(prefix string, value any)
	walk = func(prefix string, value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, child := range typed {
				key = strings.TrimSpace(key)
				if key == "" {
					continue
				}
				next := key
				if prefix != "" {
					next = prefix + "." + key
				}
				walk(next, child)
			}
		case []any:
			if prefix != "" {
				out = append(out, prefix)
			}
		default:
			if prefix != "" {
				out = append(out, prefix)
			}
		}
	}
	walk("", data)
	return out
}

func bestConnectorMappingPath(target string, paths []string, used map[string]struct{}) (string, float64) {
	targetNorm := normalizeMappingToken(target)
	bestPath := ""
	bestScore := 0.0
	for _, path := range paths {
		if _, ok := used[path]; ok {
			continue
		}
		leaf := path
		if idx := strings.LastIndex(path, "."); idx >= 0 {
			leaf = path[idx+1:]
		}
		leafNorm := normalizeMappingToken(leaf)
		pathNorm := normalizeMappingToken(path)
		score := mappingTokenScore(targetNorm, leafNorm)
		if special := mappingSpecialScore(target, leaf, path); special > score {
			score = special
		}
		if full := mappingTokenScore(targetNorm, pathNorm) - 0.08; full > score {
			score = full
		}
		if score > bestScore {
			bestScore = score
			bestPath = path
		}
	}
	return bestPath, bestScore
}

func normalizeMappingToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer("_", "", "-", "", ".", "", " ", "")
	return replacer.Replace(value)
}

func mappingSpecialScore(target, leaf, path string) float64 {
	target = strings.ToLower(strings.TrimSpace(target))
	leaf = strings.ToLower(strings.TrimSpace(leaf))
	path = strings.ToLower(strings.TrimSpace(path))
	if strings.HasSuffix(target, "_no") || strings.HasSuffix(target, "_id") || strings.Contains(target, "number") {
		for _, token := range []string{"id", "crm_id", "external_id", "number", "no", "code"} {
			if leaf == token || strings.HasSuffix(path, "."+token) {
				return 0.78
			}
		}
	}
	if target == "customer" || target == "counterparty" || target == "applicant" || target == "employee" {
		for _, token := range []string{"name", "account.name", "customer.name", "employee.name", "applicant.name"} {
			if leaf == token || strings.HasSuffix(path, token) {
				return 0.74
			}
		}
	}
	if target == "amount" {
		for _, token := range []string{"amount", "total", "total_amount", "totals.amount", "money", "value"} {
			if leaf == token || strings.HasSuffix(path, token) {
				return 0.86
			}
		}
	}
	return 0
}

func mappingTokenScore(target, candidate string) float64 {
	if target == "" || candidate == "" {
		return 0
	}
	if target == candidate {
		return 1
	}
	if strings.Contains(candidate, target) || strings.Contains(target, candidate) {
		shorter := len(target)
		longer := len(candidate)
		if shorter > longer {
			shorter, longer = longer, shorter
		}
		if longer == 0 {
			return 0
		}
		return 0.72 + 0.2*float64(shorter)/float64(longer)
	}
	return mappingBigramScore(target, candidate)
}

func mappingBigramScore(a, b string) float64 {
	if len(a) < 2 || len(b) < 2 {
		if a == b {
			return 1
		}
		return 0
	}
	grams := map[string]struct{}{}
	for i := 0; i < len(a)-1; i++ {
		grams[a[i:i+2]] = struct{}{}
	}
	matches := 0
	for i := 0; i < len(b)-1; i++ {
		if _, ok := grams[b[i:i+2]]; ok {
			matches++
		}
	}
	total := len(a) + len(b) - 2
	if total <= 0 {
		return 0
	}
	return 2 * float64(matches) / float64(total)
}

func valueAtConnectorPath(data map[string]any, path string) (any, bool) {
	if data == nil {
		return nil, false
	}
	var current any = data
	for _, part := range strings.Split(path, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = obj[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func fillTemplateFromData(template string, data map[string]any) string {
	template = strings.TrimSpace(template)
	if template == "" || data == nil {
		return ""
	}
	out := template
	for key, value := range data {
		out = strings.ReplaceAll(out, "{"+key+"}", strings.TrimSpace(fmt.Sprint(value)))
	}
	if strings.Contains(out, "{") || strings.Contains(out, "}") {
		return ""
	}
	return strings.TrimSpace(out)
}

func findBusinessAction(actionID string) (*BusinessAction, bool) {
	actionID = strings.TrimSpace(actionID)
	for i := range businessActions {
		if businessActions[i].ID == actionID {
			action := businessActions[i]
			return &action, true
		}
	}
	return nil, false
}
