package agentservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func (s *Service) ExportServiceState(ctx context.Context, in ExportServiceStateInput) (*ExportServiceStateOutput, error) {
	_ = ctx
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.UserID = strings.TrimSpace(in.UserID)
	if in.UserID != "" && in.TenantID == "" {
		return nil, fmt.Errorf("tenant_id is required when user_id is set")
	}
	out := &ExportServiceStateOutput{
		IncludeMessages: in.IncludeMessages,
		IncludeRuns:     in.IncludeRuns,
		IncludeAudit:    in.IncludeAudit,
		IncludeSecrets:  in.IncludeSecrets,
		ExportedAt:      s.now().UTC(),
	}
	var tenants []Tenant
	if in.TenantID != "" {
		tenant, err := s.store.GetTenant(in.TenantID)
		if err != nil {
			return nil, err
		}
		tenants = []Tenant{tenant}
		out.Scope = "tenant"
		out.TenantID = in.TenantID
		if in.UserID != "" {
			out.Scope = "user"
			out.UserID = in.UserID
		}
	} else {
		items, err := s.store.ListTenants()
		if err != nil {
			return nil, err
		}
		tenants = items
		out.Scope = "service"
	}
	out.Tenants = append([]Tenant(nil), tenants...)
	for _, tenant := range tenants {
		users, err := s.store.ListUsers(tenant.ID)
		if err != nil {
			return nil, err
		}
		for _, user := range users {
			if in.UserID != "" && user.ID != in.UserID {
				continue
			}
			exported, err := s.exportUserState(tenant.ID, user.ID, in)
			if err != nil {
				return nil, err
			}
			out.Users = append(out.Users, exported)
		}
	}
	if in.UserID != "" && len(out.Users) == 0 {
		return nil, ErrUserNotFound
	}
	if in.IncludeAudit {
		auditTenantID := in.TenantID
		auditUserID := in.UserID
		items, err := s.store.ListAuditEvents(auditTenantID, auditUserID)
		if err != nil {
			return nil, err
		}
		out.AuditEvents = items
		if !in.IncludeSecrets {
			out.AuditEvents = redactAuditEventsForExport(s.dataRoot, out.AuditEvents)
		}
	}
	return out, nil
}

func (s *Service) exportUserState(tenantID, userID string, in ExportServiceStateInput) (ExportedUser, error) {
	user, err := s.store.GetUser(tenantID, userID)
	if err != nil {
		return ExportedUser{}, err
	}
	out := ExportedUser{User: user}
	cfg, err := s.getOrLoadRawUserConfig(tenantID, userID)
	if err == nil {
		if !in.IncludeSecrets {
			cfg.AppConfig = SanitizeAppConfig(cfg.AppConfig)
		}
		out.Config = &cfg
	} else if err != ErrUserConfigNotFound {
		return ExportedUser{}, err
	}
	creds, err := s.store.ListCredentials(tenantID, userID)
	if err != nil {
		return ExportedUser{}, err
	}
	out.Credentials = make([]ExportedCredential, 0, len(creds))
	for _, cred := range creds {
		out.Credentials = append(out.Credentials, exportCredential(cred, in.IncludeSecrets))
	}
	instances, err := s.store.ListInstances(tenantID, userID)
	if err != nil {
		return ExportedUser{}, err
	}
	out.Instances = make([]ExportedInstance, 0, len(instances))
	for _, inst := range instances {
		inst = s.withInstanceReadiness(inst)
		exportedInst := ExportedInstance{Instance: inst}
		sessions, err := s.store.ListSessions(tenantID, userID, inst.ID)
		if err != nil {
			return ExportedUser{}, err
		}
		exportedInst.Sessions = make([]ExportedSession, 0, len(sessions))
		for _, sess := range sessions {
			sess, err = s.enrichSession(sess)
			if err != nil {
				return ExportedUser{}, err
			}
			exportedSession := ExportedSession{Session: sess}
			if in.IncludeMessages {
				messages, err := s.store.ListMessages(sess.ID)
				if err != nil {
					return ExportedUser{}, err
				}
				exportedSession.Messages = messages
			}
			exportedInst.Sessions = append(exportedInst.Sessions, exportedSession)
		}
		if in.IncludeRuns {
			runs, err := s.store.ListRuns(tenantID, userID, inst.ID)
			if err != nil {
				return ExportedUser{}, err
			}
			runs, err = s.enrichRuns(runs)
			if err != nil {
				return ExportedUser{}, err
			}
			exportedInst.Runs = runs
		}
		out.Instances = append(out.Instances, exportedInst)
	}
	return out, nil
}

func exportCredential(cred Credential, includeSecrets bool) ExportedCredential {
	out := ExportedCredential{
		ID:           cred.ID,
		TenantID:     cred.TenantID,
		UserID:       cred.UserID,
		Name:         cred.Name,
		APIKey:       cred.APIKey,
		APIKeyPrefix: cred.APIKeyPrefix,
		APIKeyHash:   cred.APIKeyHash,
		Status:       credentialStatus(cred),
		ExpiresAt:    cred.ExpiresAt,
		TokenVersion: credentialTokenVersion(cred),
		CreatedAt:    cred.CreatedAt,
		UpdatedAt:    cred.UpdatedAt,
	}
	if includeSecrets {
		out.SecretDigest = cred.SecretDigest
		return out
	}
	sanitized := sanitizeCredential(cred)
	out.APIKey = sanitized.APIKey
	out.APIKeyHash = ""
	out.SecretDigest = ""
	return out
}

func (s *Service) CreateServiceSnapshot(ctx context.Context, in CreateServiceSnapshotInput) (*ServiceSnapshotEnvelope, error) {
	includeMessages := in.IncludeMessages == nil || *in.IncludeMessages
	includeRuns := in.IncludeRuns == nil || *in.IncludeRuns
	includeAudit := in.IncludeAudit == nil || *in.IncludeAudit
	includeSecrets := in.IncludeSecrets != nil && *in.IncludeSecrets
	export, err := s.ExportServiceState(ctx, ExportServiceStateInput{
		TenantID:        in.TenantID,
		UserID:          in.UserID,
		IncludeMessages: includeMessages,
		IncludeRuns:     includeRuns,
		IncludeAudit:    includeAudit,
		IncludeSecrets:  includeSecrets,
	})
	if err != nil {
		return nil, err
	}
	createdAt := s.now().UTC()
	snapshot := ServiceSnapshot{
		ID:              NewID("snapshot"),
		Name:            strings.TrimSpace(in.Name),
		Scope:           export.Scope,
		TenantID:        export.TenantID,
		UserID:          export.UserID,
		IncludeMessages: export.IncludeMessages,
		IncludeRuns:     export.IncludeRuns,
		IncludeAudit:    export.IncludeAudit,
		IncludeSecrets:  export.IncludeSecrets,
		CreatedAt:       createdAt,
	}
	if snapshot.Name == "" {
		snapshot.Name = snapshot.Scope + " snapshot"
	}
	path, err := s.snapshotPath(snapshot.ID)
	if err != nil {
		return nil, err
	}
	snapshot.Path = path
	envelope := &ServiceSnapshotEnvelope{Snapshot: snapshot, Data: *export}
	data, err := marshalSnapshotEnvelope(envelope)
	if err != nil {
		return nil, err
	}
	snapshot.SizeBytes = int64(len(data))
	envelope.Snapshot = snapshot
	data, err = marshalSnapshotEnvelope(envelope)
	if err != nil {
		return nil, err
	}
	if err := secureMkdirAll(s.snapshotRoot()); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, err
	}
	if stat, err := os.Stat(path); err == nil {
		envelope.Snapshot.SizeBytes = stat.Size()
	}
	_ = s.recordAudit(auditRecord{TenantID: snapshot.TenantID, UserID: snapshot.UserID, Action: "snapshot.created", ResourceType: "snapshot", ResourceID: snapshot.ID, ActorType: "admin", Metadata: map[string]string{"scope": snapshot.Scope}})
	return envelope, nil
}

func (s *Service) ListServiceSnapshots(ctx context.Context, in ListServiceSnapshotsInput) ([]ServiceSnapshot, error) {
	_ = ctx
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.UserID = strings.TrimSpace(in.UserID)
	in.Scope = strings.TrimSpace(in.Scope)
	name := strings.ToLower(strings.TrimSpace(in.Name))
	if in.UserID != "" && in.TenantID == "" {
		return nil, fmt.Errorf("tenant_id is required when user_id is set")
	}
	if in.Scope != "" && in.Scope != "service" && in.Scope != "tenant" && in.Scope != "user" {
		return nil, fmt.Errorf("scope must be service, tenant, or user")
	}
	entries, err := os.ReadDir(s.snapshotRoot())
	if errors.Is(err, os.ErrNotExist) {
		return []ServiceSnapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]ServiceSnapshot, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		envelope, err := s.readServiceSnapshot(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		snapshot := envelope.Snapshot
		if in.TenantID != "" && snapshot.TenantID != in.TenantID {
			continue
		}
		if in.UserID != "" && snapshot.UserID != in.UserID {
			continue
		}
		if in.Scope != "" && snapshot.Scope != in.Scope {
			continue
		}
		if name != "" && !strings.Contains(strings.ToLower(snapshot.Name), name) {
			continue
		}
		if in.Since != nil && snapshot.CreatedAt.Before(*in.Since) {
			continue
		}
		if in.Until != nil && snapshot.CreatedAt.After(*in.Until) {
			continue
		}
		out = append(out, snapshot)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Service) GetServiceSnapshot(ctx context.Context, snapshotID string) (*ServiceSnapshotEnvelope, error) {
	_ = ctx
	return s.readServiceSnapshot(snapshotID)
}

func (s *Service) PruneServiceSnapshots(ctx context.Context, in PruneServiceSnapshotsInput) (*PruneServiceSnapshotsOutput, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.UserID = strings.TrimSpace(in.UserID)
	if in.UserID != "" && in.TenantID == "" {
		return nil, fmt.Errorf("tenant_id is required when user_id is set")
	}
	if in.KeepLatest < 0 {
		return nil, fmt.Errorf("keep_latest must be greater than or equal to 0")
	}
	if in.OlderThan == nil && in.KeepLatest == 0 {
		return nil, fmt.Errorf("older_than or keep_latest is required")
	}
	items, err := s.ListServiceSnapshots(ctx, ListServiceSnapshotsInput{TenantID: in.TenantID, UserID: in.UserID})
	if err != nil {
		return nil, err
	}
	out := &PruneServiceSnapshotsOutput{
		TenantID:    in.TenantID,
		UserID:      in.UserID,
		OlderThan:   in.OlderThan,
		KeepLatest:  in.KeepLatest,
		DryRun:      in.DryRun,
		Matched:     len(items),
		GeneratedAt: s.now().UTC(),
	}
	protected := map[string]struct{}{}
	if in.KeepLatest > 0 {
		newest := append([]ServiceSnapshot(nil), items...)
		sort.Slice(newest, func(i, j int) bool { return newest[i].CreatedAt.After(newest[j].CreatedAt) })
		for i, item := range newest {
			if i >= in.KeepLatest {
				break
			}
			protected[item.ID] = struct{}{}
			out.KeptSnapshots = append(out.KeptSnapshots, item)
		}
	}
	for _, item := range items {
		if _, ok := protected[item.ID]; ok {
			continue
		}
		if in.OlderThan != nil && !item.CreatedAt.Before(*in.OlderThan) {
			continue
		}
		out.Snapshots = append(out.Snapshots, item)
		out.FreedBytes += item.SizeBytes
		if in.DryRun {
			continue
		}
		path, err := s.snapshotPath(item.ID)
		if err != nil {
			return nil, err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		out.Deleted++
	}
	_ = s.recordAudit(auditRecord{TenantID: in.TenantID, UserID: in.UserID, Action: "snapshot.pruned", ResourceType: "snapshot", ActorType: "admin", Metadata: map[string]string{"dry_run": fmt.Sprintf("%v", in.DryRun), "deleted": fmt.Sprintf("%d", out.Deleted), "candidates": fmt.Sprintf("%d", len(out.Snapshots)), "keep_latest": fmt.Sprintf("%d", in.KeepLatest)}})
	return out, nil
}

func (s *Service) RestoreServiceSnapshot(ctx context.Context, snapshotID string, in RestoreServiceSnapshotInput) (*RestoreServiceSnapshotOutput, error) {
	envelope, err := s.readServiceSnapshot(snapshotID)
	if err != nil {
		return nil, err
	}
	imported, err := s.ImportServiceState(ctx, ImportServiceStateRequest{Data: envelope.Data, Overwrite: in.Overwrite, DryRun: in.DryRun})
	if err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: envelope.Snapshot.TenantID, UserID: envelope.Snapshot.UserID, Action: "snapshot.restored", ResourceType: "snapshot", ResourceID: envelope.Snapshot.ID, ActorType: "admin", Metadata: map[string]string{"dry_run": fmt.Sprintf("%v", in.DryRun), "overwrite": fmt.Sprintf("%v", in.Overwrite), "scope": envelope.Snapshot.Scope}})
	return &RestoreServiceSnapshotOutput{Snapshot: envelope.Snapshot, Import: *imported}, nil
}

func (s *Service) DeleteServiceSnapshot(ctx context.Context, snapshotID string) (*ServiceSnapshot, error) {
	_ = ctx
	envelope, err := s.readServiceSnapshot(snapshotID)
	if err != nil {
		return nil, err
	}
	path, err := s.snapshotPath(snapshotID)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(path); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: envelope.Snapshot.TenantID, UserID: envelope.Snapshot.UserID, Action: "snapshot.deleted", ResourceType: "snapshot", ResourceID: envelope.Snapshot.ID, ActorType: "admin", Metadata: map[string]string{"scope": envelope.Snapshot.Scope}})
	return &envelope.Snapshot, nil
}

func (s *Service) readServiceSnapshot(snapshotID string) (*ServiceSnapshotEnvelope, error) {
	path, err := s.snapshotPath(snapshotID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrSnapshotNotFound
	}
	if err != nil {
		return nil, err
	}
	var envelope ServiceSnapshotEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	if envelope.Snapshot.ID == "" {
		return nil, ErrSnapshotNotFound
	}
	envelope.Snapshot.Path = path
	if stat, err := os.Stat(path); err == nil {
		envelope.Snapshot.SizeBytes = stat.Size()
	}
	return &envelope, nil
}

func (s *Service) snapshotRoot() string {
	return filepath.Join(s.dataRoot, "snapshots")
}

func (s *Service) snapshotPath(snapshotID string) (string, error) {
	id := strings.TrimSpace(snapshotID)
	if id == "" || strings.ContainsAny(id, `/\\`) || strings.Contains(id, "..") {
		return "", ErrSnapshotNotFound
	}
	return filepath.Join(s.snapshotRoot(), id+".json"), nil
}

func marshalSnapshotEnvelope(envelope *ServiceSnapshotEnvelope) ([]byte, error) {
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func (s *Service) ImportServiceState(ctx context.Context, in ImportServiceStateRequest) (*ImportServiceStateOutput, error) {
	_ = ctx
	data := in.Data
	out := &ImportServiceStateOutput{
		Scope:      strings.TrimSpace(data.Scope),
		TenantID:   strings.TrimSpace(data.TenantID),
		UserID:     strings.TrimSpace(data.UserID),
		Overwrite:  in.Overwrite,
		DryRun:     in.DryRun,
		ImportedAt: s.now().UTC(),
	}
	if out.Scope == "" {
		out.Scope = "service"
	}
	if err := s.assessImportState(data, out); err != nil {
		return nil, err
	}
	if in.DryRun {
		return out, nil
	}
	if len(out.Conflicts) > 0 && !in.Overwrite {
		return nil, fmt.Errorf("import conflicts detected: %w", ErrAlreadyExists)
	}
	for _, tenant := range data.Tenants {
		if err := s.store.SaveTenant(tenant); err != nil {
			return nil, err
		}
		if err := secureMkdirAll(filepath.Join(s.dataRoot, "tenants", slugID(tenant.ID))); err != nil {
			return nil, err
		}
	}
	for _, exportedUser := range data.Users {
		currentAppConfig := corelib.AppConfig{}
		if _, err := s.store.GetUser(exportedUser.User.TenantID, exportedUser.User.ID); err == nil {
			if currentConfig, cfgErr := s.getOrLoadRawUserConfig(exportedUser.User.TenantID, exportedUser.User.ID); cfgErr == nil {
				currentAppConfig = currentConfig.AppConfig
			} else if cfgErr != ErrUserConfigNotFound {
				return nil, cfgErr
			}
			if err := s.deleteUserStateForImport(exportedUser.User.TenantID, exportedUser.User.ID); err != nil {
				return nil, err
			}
		} else if err != ErrUserNotFound {
			return nil, err
		}
		if err := s.store.SaveUser(exportedUser.User); err != nil {
			return nil, err
		}
		if err := secureMkdirAll(s.userDataRoot(exportedUser.User.TenantID, exportedUser.User.ID)); err != nil {
			return nil, err
		}
		if err := secureMkdirAll(filepath.Join(s.userRoot(exportedUser.User.TenantID, exportedUser.User.ID), "instances")); err != nil {
			return nil, err
		}
		if exportedUser.Config != nil {
			cfg := *exportedUser.Config
			cfg.TenantID = exportedUser.User.TenantID
			cfg.UserID = exportedUser.User.ID
			cfg.AppConfig = normalizeLLMConfigForSave(currentAppConfig, mergeSecretPreserving(currentAppConfig, cloneAppConfig(cfg.AppConfig)))
			if err := s.store.SaveUserConfig(cfg); err != nil {
				return nil, err
			}
			if err := saveUserConfigToFile(s.userConfigPath(exportedUser.User.TenantID, exportedUser.User.ID), cfg); err != nil {
				return nil, err
			}
		}
		for _, cred := range exportedUser.Credentials {
			storedCred, err := importCredential(exportedUser.User.TenantID, exportedUser.User.ID, cred)
			if err != nil {
				return nil, err
			}
			if err := s.store.SaveCredential(storedCred); err != nil {
				return nil, err
			}
		}
		for _, exportedInst := range exportedUser.Instances {
			inst := s.remapImportedInstance(exportedUser.User.TenantID, exportedUser.User.ID, exportedInst.Instance)
			if err := secureMkdirAll(inst.Workspace); err != nil {
				return nil, err
			}
			inst = s.withInstanceReadiness(inst)
			if err := s.store.SaveInstance(inst); err != nil {
				return nil, err
			}
			for _, exportedSession := range exportedInst.Sessions {
				sess := exportedSession.Session
				sess.TenantID = exportedUser.User.TenantID
				sess.UserID = exportedUser.User.ID
				sess.InstanceID = inst.ID
				if err := s.store.SaveSession(sess); err != nil {
					return nil, err
				}
				for _, msg := range exportedSession.Messages {
					msg.TenantID = exportedUser.User.TenantID
					msg.UserID = exportedUser.User.ID
					msg.InstanceID = inst.ID
					msg.SessionID = sess.ID
					if err := s.store.SaveMessage(msg); err != nil {
						return nil, err
					}
				}
			}
			for _, run := range exportedInst.Runs {
				run.TenantID = exportedUser.User.TenantID
				run.UserID = exportedUser.User.ID
				run.InstanceID = inst.ID
				if err := s.store.SaveRun(run); err != nil {
					return nil, err
				}
			}
		}
	}
	if len(data.AuditEvents) > 0 {
		existingAudit, err := s.store.ListAuditEvents("", "")
		if err != nil {
			return nil, err
		}
		existingByID := make(map[string]struct{}, len(existingAudit))
		for _, item := range existingAudit {
			if strings.TrimSpace(item.ID) != "" {
				existingByID[item.ID] = struct{}{}
			}
		}
		for _, event := range data.AuditEvents {
			if strings.TrimSpace(event.ID) != "" {
				if _, ok := existingByID[event.ID]; ok {
					continue
				}
				existingByID[event.ID] = struct{}{}
			}
			if err := s.store.SaveAuditEvent(event); err != nil {
				return nil, err
			}
		}
	}
	_ = s.recordAudit(auditRecord{TenantID: out.TenantID, UserID: out.UserID, Action: "admin.imported", ResourceType: "service_state", ResourceID: out.Scope, ActorType: "admin", Metadata: map[string]string{"overwrite": strconv.FormatBool(in.Overwrite), "dry_run": strconv.FormatBool(in.DryRun)}})
	return out, nil
}

func (s *Service) assessImportState(data ExportServiceStateOutput, out *ImportServiceStateOutput) error {
	incomingTenants := map[string]struct{}{}
	for _, tenant := range data.Tenants {
		if strings.TrimSpace(tenant.ID) == "" {
			return fmt.Errorf("tenant id is required")
		}
		incomingTenants[tenant.ID] = struct{}{}
		if existing, err := s.store.GetTenant(tenant.ID); err == nil && existing.ID != "" {
			out.Conflicts = append(out.Conflicts, fmt.Sprintf("tenant %s already exists", tenant.ID))
			appendImportPlan(out, "tenant", tenant.ID, "overwrite", "tenant already exists and would be updated")
		} else {
			appendImportPlan(out, "tenant", tenant.ID, "create", "tenant would be imported")
		}
		out.Tenants++
	}
	for _, exportedUser := range data.Users {
		if strings.TrimSpace(exportedUser.User.TenantID) == "" || strings.TrimSpace(exportedUser.User.ID) == "" {
			return fmt.Errorf("user tenant_id and id are required")
		}
		if _, ok := incomingTenants[exportedUser.User.TenantID]; !ok {
			if _, err := s.store.GetTenant(exportedUser.User.TenantID); err != nil {
				return err
			}
		}
		if _, err := s.store.GetUser(exportedUser.User.TenantID, exportedUser.User.ID); err == nil {
			out.Conflicts = append(out.Conflicts, fmt.Sprintf("user %s/%s already exists", exportedUser.User.TenantID, exportedUser.User.ID))
			appendImportPlan(out, "user", exportedUser.User.TenantID+"/"+exportedUser.User.ID, "overwrite", "user already exists and would be replaced with imported state")
		} else if err != ErrUserNotFound {
			return err
		} else {
			appendImportPlan(out, "user", exportedUser.User.TenantID+"/"+exportedUser.User.ID, "create", "user would be imported")
		}
		out.Users++
		if exportedUser.Config != nil && appConfigContainsMaskedSecrets(exportedUser.Config.AppConfig) {
			out.Warnings = append(out.Warnings, fmt.Sprintf("user %s/%s config contains masked secrets and may need manual repair", exportedUser.User.TenantID, exportedUser.User.ID))
		}
		for _, cred := range exportedUser.Credentials {
			storedCred, err := importCredential(exportedUser.User.TenantID, exportedUser.User.ID, cred)
			if err != nil {
				return nilOrErr(out, err)
			}
			_ = storedCred
			appendImportPlan(out, "credential", exportedUser.User.TenantID+"/"+exportedUser.User.ID+"/"+cred.ID, "create", "credential would be imported")
			out.Credentials++
		}
		for _, exportedInst := range exportedUser.Instances {
			appendImportPlan(out, "instance", exportedUser.User.TenantID+"/"+exportedUser.User.ID+"/"+exportedInst.Instance.ID, "create", "instance would be imported and remapped into current data root")
			out.Instances++
			for _, exportedSession := range exportedInst.Sessions {
				appendImportPlan(out, "session", exportedUser.User.TenantID+"/"+exportedUser.User.ID+"/"+exportedSession.Session.ID, "create", "session would be imported")
				out.Sessions++
				if len(exportedSession.Messages) > 0 {
					appendImportPlan(out, "message_batch", exportedUser.User.TenantID+"/"+exportedUser.User.ID+"/"+exportedSession.Session.ID, "create", fmt.Sprintf("%d messages would be imported", len(exportedSession.Messages)))
				}
				out.Messages += len(exportedSession.Messages)
			}
			if len(exportedInst.Runs) > 0 {
				appendImportPlan(out, "run_batch", exportedUser.User.TenantID+"/"+exportedUser.User.ID+"/"+exportedInst.Instance.ID, "create", fmt.Sprintf("%d runs would be imported", len(exportedInst.Runs)))
			}
			out.Runs += len(exportedInst.Runs)
		}
	}
	if len(data.AuditEvents) > 0 {
		existingAudit, err := s.store.ListAuditEvents("", "")
		if err != nil {
			return err
		}
		existingByID := make(map[string]struct{}, len(existingAudit))
		for _, item := range existingAudit {
			if strings.TrimSpace(item.ID) != "" {
				existingByID[item.ID] = struct{}{}
			}
		}
		for _, event := range data.AuditEvents {
			if strings.TrimSpace(event.ID) != "" {
				if _, ok := existingByID[event.ID]; ok {
					out.Warnings = append(out.Warnings, fmt.Sprintf("audit event %s already exists and would be skipped", event.ID))
					continue
				}
				existingByID[event.ID] = struct{}{}
			}
			appendImportPlan(out, "audit_event", event.ID, "create", "audit event would be imported")
			out.AuditEvents++
		}
	}
	return nil
}

func appendImportPlan(out *ImportServiceStateOutput, resourceType, resourceID, action, message string) {
	out.Plan = append(out.Plan, ImportPlanItem{
		ResourceType: strings.TrimSpace(resourceType),
		ResourceID:   strings.TrimSpace(resourceID),
		Action:       strings.TrimSpace(action),
		Message:      strings.TrimSpace(message),
	})
}

func nilOrErr(out *ImportServiceStateOutput, err error) error {
	return err
}

func (s *Service) remapImportedInstance(tenantID, userID string, inst Instance) Instance {
	inst.TenantID = tenantID
	inst.UserID = userID
	inst.DataDir = s.userDataRoot(tenantID, userID)
	inst.RuntimeDir = filepath.Join(s.userRoot(tenantID, userID), "instances", slugID(inst.ID))
	inst.Workspace = filepath.Join(inst.RuntimeDir, "workspace")
	return inst
}

func importCredential(tenantID, userID string, in ExportedCredential) (Credential, error) {
	cred := Credential{
		ID:           strings.TrimSpace(in.ID),
		TenantID:     tenantID,
		UserID:       userID,
		Name:         strings.TrimSpace(in.Name),
		APIKey:       strings.TrimSpace(in.APIKey),
		APIKeyPrefix: strings.TrimSpace(in.APIKeyPrefix),
		APIKeyHash:   strings.TrimSpace(in.APIKeyHash),
		Status:       in.Status,
		ExpiresAt:    in.ExpiresAt,
		TokenVersion: in.TokenVersion,
		SecretDigest: strings.TrimSpace(in.SecretDigest),
		CreatedAt:    in.CreatedAt,
		UpdatedAt:    in.UpdatedAt,
	}
	if cred.ID == "" {
		return Credential{}, fmt.Errorf("credential id is required")
	}
	if cred.Status == "" {
		cred.Status = CredentialStatusActive
	}
	cred.TokenVersion = credentialTokenVersion(cred)
	if cred.APIKeyHash == "" && cred.APIKey != "" {
		cred.APIKeyHash = hashAPIKey(cred.APIKey)
	}
	if cred.APIKeyPrefix == "" && cred.APIKey != "" {
		cred.APIKeyPrefix = deriveAPIKeyPrefix(cred.APIKey)
	}
	if cred.APIKeyHash == "" || cred.APIKeyPrefix == "" {
		return Credential{}, fmt.Errorf("credential api key hash and prefix are required for import")
	}
	if cred.SecretDigest == "" {
		return Credential{}, fmt.Errorf("credential secret_digest is required for import; export with include_secrets=true")
	}
	return cred, nil
}

func (s *Service) deleteUserStateForImport(tenantID, userID string) error {
	instances, err := s.store.ListInstances(tenantID, userID)
	if err != nil {
		return err
	}
	for _, inst := range instances {
		busy, err := s.hasRunningRuns(tenantID, userID, inst.ID, "")
		if err != nil {
			return err
		}
		if busy {
			return ErrUserBusy
		}
	}
	if err := s.store.DeleteUser(tenantID, userID); err != nil {
		return err
	}
	if err := secureRemoveAllWithin(filepath.Join(s.dataRoot, "tenants", slugID(tenantID), "users"), s.userRoot(tenantID, userID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
