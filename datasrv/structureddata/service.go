package structureddata

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var identifierPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)

// DatasetStore manages dataset schemas and field definitions.
type DatasetStore interface {
	CreateDataset(ctx context.Context, dataset Dataset) (*Dataset, error)
	ListDatasets(ctx context.Context, tenantID string) ([]Dataset, error)
	GetDataset(ctx context.Context, tenantID, datasetID string) (*Dataset, error)
	UpdateDataset(ctx context.Context, tenantID, datasetID string, in UpdateDatasetInput, now time.Time) (*Dataset, error)
	DeleteDataset(ctx context.Context, tenantID, datasetID string) error
	UpsertFields(ctx context.Context, tenantID, datasetID string, fields []FieldDefinition) ([]FieldDefinition, error)
	ListFields(ctx context.Context, tenantID, datasetID string) ([]FieldDefinition, error)
	CreateSchemaProposal(ctx context.Context, proposal SchemaProposal) (*SchemaProposal, error)
	ListSchemaProposals(ctx context.Context, tenantID, datasetID string, in ListSchemaProposalsInput) ([]SchemaProposal, error)
	GetSchemaProposal(ctx context.Context, tenantID, datasetID, proposalID string) (*SchemaProposal, error)
	UpdateSchemaProposalStatus(ctx context.Context, tenantID, datasetID, proposalID, status, actor string, now time.Time) (*SchemaProposal, error)
}

// RecordStore manages data records, revisions, and approvals.
type RecordStore interface {
	ImportRecords(ctx context.Context, records []Record) ([]Record, error)
	CreateRecord(ctx context.Context, record Record) (*Record, error)
	GetRecord(ctx context.Context, tenantID, datasetID, recordID string) (*Record, error)
	UpdateRecord(ctx context.Context, tenantID, datasetID, recordID string, in UpdateRecordInput, actor string, now time.Time) (*Record, error)
	DeleteRecord(ctx context.Context, tenantID, datasetID, recordID string) error
	QueryRecords(ctx context.Context, tenantID, datasetID string, in QueryRecordsInput) ([]Record, error)
	AppendRecordRevision(ctx context.Context, revision RecordRevision) (*RecordRevision, error)
	QueryRecordRevisions(ctx context.Context, tenantID, datasetID, recordID string, in QueryRecordRevisionsInput) ([]RecordRevision, error)
	CreateRecordApproval(ctx context.Context, approval RecordApproval) (*RecordApproval, error)
	ListRecordApprovals(ctx context.Context, tenantID string, in QueryRecordApprovalsInput) ([]RecordApproval, error)
	GetRecordApproval(ctx context.Context, tenantID, approvalID string) (*RecordApproval, error)
	UpdateRecordApprovalStatus(ctx context.Context, tenantID, approvalID, status, decision, reason, reviewedBy, workflowInstanceID, workflowNodeID string, workflowNodeIDs []string, workflowVersion, workflowDecisionID, detailURL, businessStatus, resultStatus, currentAssignee, currentAssigneeType, fromStatus, toStatus string, resultPayload map[string]any, outputs []RecordApprovalOutput, artifacts []RecordApprovalArtifact, now time.Time) (*RecordApproval, error)
	UpdateRecordApprovalProgress(ctx context.Context, tenantID, approvalID, workflowInstanceID, workflowNodeID string, workflowNodeIDs []string, workflowVersion, workflowDecisionID, detailURL, businessStatus, resultStatus, currentAssignee, currentAssigneeType, fromStatus, toStatus string, resultPayload map[string]any, outputs []RecordApprovalOutput, artifacts []RecordApprovalArtifact, now time.Time) (*RecordApproval, error)
}

// EventStore manages data event ingestion, logs, and dead letters.
type EventStore interface {
	GetDataEventByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string) (*DataEventLog, error)
	QueryDataEvents(ctx context.Context, tenantID string, in QueryDataEventsInput) ([]DataEventLog, error)
	AppendDataEventLog(ctx context.Context, entry DataEventLog) (*DataEventLog, error)
	CreateDataEventDeadLetter(ctx context.Context, entry DataEventDeadLetter) (*DataEventDeadLetter, error)
	QueryDataEventDeadLetters(ctx context.Context, tenantID string, in QueryDataEventDeadLettersInput) ([]DataEventDeadLetter, error)
	GetDataEventDeadLetter(ctx context.Context, tenantID, deadLetterID string) (*DataEventDeadLetter, error)
	UpdateDataEventDeadLetterStatus(ctx context.Context, tenantID, deadLetterID, status, resolvedBy, resolution string, now time.Time) (*DataEventDeadLetter, error)
}

// ConnectorStore manages external system connectors.
type ConnectorStore interface {
	UpsertExternalConnector(ctx context.Context, connector ExternalConnector) (*ExternalConnector, error)
	ListExternalConnectors(ctx context.Context, tenantID string, in QueryExternalConnectorsInput) ([]ExternalConnector, error)
	GetExternalConnector(ctx context.Context, tenantID, connectorID string) (*ExternalConnector, error)
	DeleteExternalConnector(ctx context.Context, tenantID, connectorID string) error
}

// AppInstallationStore manages installed MaClaw Apps and their semantic data bindings.
type AppInstallationStore interface {
	UpsertAppInstallation(ctx context.Context, app AppInstallation) (*AppInstallation, error)
	ListAppInstallations(ctx context.Context, tenantID string, in QueryAppInstallationsInput) ([]AppInstallation, error)
	GetAppInstallation(ctx context.Context, tenantID, appID string) (*AppInstallation, error)
}

// GovernanceStore manages quality checks, operation plans, import/export jobs, and audit logs.
type GovernanceStore interface {
	AppendQualityRun(ctx context.Context, run QualityCheckResult) (*QualityCheckResult, error)
	ListQualityRuns(ctx context.Context, tenantID, datasetID string, in QueryQualityRunsInput) ([]QualityCheckResult, error)
	GetQualityRun(ctx context.Context, tenantID, datasetID, runID string) (*QualityCheckResult, error)
	UpsertImportJob(ctx context.Context, job ImportJob) (*ImportJob, error)
	ListImportJobs(ctx context.Context, tenantID string, in QueryImportJobsInput) ([]ImportJob, error)
	GetImportJob(ctx context.Context, tenantID, jobID string) (*ImportJob, error)
	UpsertExportJob(ctx context.Context, job ExportJob) (*ExportJob, error)
	ListExportJobs(ctx context.Context, tenantID string, in QueryExportJobsInput) ([]ExportJob, error)
	GetExportJob(ctx context.Context, tenantID, jobID string) (*ExportJob, error)
	CreateOperationPlan(ctx context.Context, plan OperationPlan) (*OperationPlan, error)
	ListOperationPlans(ctx context.Context, tenantID string, in QueryOperationPlansInput) ([]OperationPlan, error)
	GetOperationPlan(ctx context.Context, tenantID, planID string) (*OperationPlan, error)
	UpdateOperationPlanStatus(ctx context.Context, tenantID, planID, status, reviewedBy, appliedBy string, now time.Time) (*OperationPlan, error)
	AppendAuditLog(ctx context.Context, entry AuditLog) (*AuditLog, error)
	QueryAuditLogs(ctx context.Context, tenantID string, in QueryAuditLogsInput) ([]AuditLog, error)
}

// AdminStore manages admin users, sessions, API keys, backups, and system maintenance.
type AdminStore interface {
	SchemaVersion(ctx context.Context) (int, error)
	RunMaintenance(ctx context.Context, in RunMaintenanceInput, now time.Time) (*MaintenanceResult, error)
	SystemStats(ctx context.Context, tenantID string) (*SystemStats, error)
	CreateAPIKeyPolicy(ctx context.Context, record APIKeyPolicyRecord, keyHash string) (*APIKeyPolicyRecord, error)
	ListAPIKeyPolicies(ctx context.Context, tenantID string, in QueryAPIKeyPoliciesInput) ([]APIKeyPolicyRecord, error)
	GetAPIKeyPolicy(ctx context.Context, tenantID, keyID string) (*APIKeyPolicyRecord, error)
	FindAPIKeyPolicyByHash(ctx context.Context, keyHash string) (*APIKeyPolicyRecord, error)
	UpdateAPIKeyPolicy(ctx context.Context, record APIKeyPolicyRecord) (*APIKeyPolicyRecord, error)
	RotateAPIKeyPolicySecret(ctx context.Context, tenantID, keyID, keyHash, keyPrefix string, now time.Time) (*APIKeyPolicyRecord, error)
	TouchAPIKeyPolicyUse(ctx context.Context, tenantID, keyID, ip, userAgent string, now time.Time) error
	DisableAPIKeyPolicy(ctx context.Context, tenantID, keyID, actor string, now time.Time) (*APIKeyPolicyRecord, error)
	AdminInitialized(ctx context.Context) (bool, error)
	CreateAdminUser(ctx context.Context, record adminUserRecord) (*adminUserRecord, error)
	ListAdminUsers(ctx context.Context, tenantID string) ([]adminUserRecord, error)
	FindAdminUser(ctx context.Context, tenantID, username string) (*adminUserRecord, error)
	UpdateAdminUser(ctx context.Context, tenantID, username string, in UpdateAdminAccountInput, now time.Time) (*adminUserRecord, error)
	UpdateAdminPassword(ctx context.Context, tenantID, username, passwordHash string, now time.Time) (*adminUserRecord, error)
	TouchAdminLogin(ctx context.Context, tenantID, userID string, now time.Time) error
	RecordAdminLoginFailure(ctx context.Context, tenantID, username string, now time.Time, maxFailures int, lockout time.Duration) (*adminUserRecord, error)
	ClearAdminLoginFailure(ctx context.Context, tenantID, username string, now time.Time) error
	CreateAdminSession(ctx context.Context, record adminSessionRecord) (*adminSessionRecord, error)
	ListAdminSessions(ctx context.Context, tenantID string, now time.Time) ([]adminSessionRecord, error)
	FindAdminSessionByHash(ctx context.Context, tokenHash string, now time.Time) (*adminSessionRecord, error)
	UpdateAdminSessionExpiresAt(ctx context.Context, tenantID, sessionID string, expiresAt time.Time, now time.Time) (*adminSessionRecord, error)
	DeleteAdminSession(ctx context.Context, tenantID, sessionID string) error
	DeleteAdminSessionsForUser(ctx context.Context, tenantID, userID string) error
	DeleteExpiredAdminSessions(ctx context.Context, now time.Time) error
	UpsertDataTenants(ctx context.Context, tenants []DataTenantInfo, source string, now time.Time) ([]DataTenantInfo, error)
	ListDataTenants(ctx context.Context) ([]DataTenantInfo, error)
	GetHubRegistration(ctx context.Context) (*hubRegistrationRecord, error)
	SaveHubRegistration(ctx context.Context, record hubRegistrationRecord) (*hubRegistrationRecord, error)
	CreateBackup(ctx context.Context, in CreateBackupInput, actor string, now time.Time) (*BackupInfo, error)
	ListBackups(ctx context.Context, in QueryBackupsInput) ([]BackupInfo, error)
	GetBackup(ctx context.Context, backupID string) (*BackupInfo, error)
	ReadBackup(ctx context.Context, backupID string) ([]byte, *BackupInfo, error)
	RestoreBackup(ctx context.Context, backupID string, in RestoreBackupInput, actor string, now time.Time) (*RestoreResult, error)
}

// Store is the full storage interface combining all sub-stores.
// SQLiteStore implements this composite interface. Service methods that only
// need a subset of capabilities can accept the narrower sub-interfaces above
// for easier testing and future storage engine substitution.
type Store interface {
	DatasetStore
	RecordStore
	EventStore
	ConnectorStore
	AppInstallationStore
	GovernanceStore
	AdminStore
}

type Service struct {
	mu                     sync.Mutex // serialize writes only; reads go through WAL read pool
	store                  Store
	writeBatcher           *WriteBatcher // optional: batches concurrent writes for throughput
	now                    func() time.Time
	engine                 string
	adminPasswordMinLength int
	adminLoginMaxFailures  int
	adminLoginLockout      time.Duration
}

func NewService(store Store, engine string) *Service {
	return &Service{
		store:                  store,
		engine:                 strings.TrimSpace(engine),
		now:                    time.Now,
		adminPasswordMinLength: adminPasswordMinLengthFromEnv(),
		adminLoginMaxFailures:  adminLoginMaxFailuresFromEnv(),
		adminLoginLockout:      adminLoginLockoutFromEnv(),
	}
}

// NewServiceWithBatcher creates a Service with write batching enabled.
// This merges concurrent writes into fewer transactions for higher throughput.
// Call Close() when done to flush pending writes.
func NewServiceWithBatcher(store Store, engine string) *Service {
	svc := NewService(store, engine)
	if sqlStore, ok := store.(*SQLiteStore); ok {
		svc.writeBatcher = NewWriteBatcher(sqlStore, 64, 2*time.Millisecond)
	}
	return svc
}

// Close shuts down background resources (write batcher).
func (s *Service) Close() {
	if s.writeBatcher != nil {
		s.writeBatcher.Stop()
	}
}

func (s *Service) Ready(ctx context.Context) (*Readiness, error) {
	version, err := s.store.SchemaVersion(ctx)
	if err != nil {
		return nil, err
	}
	return &Readiness{Status: "ready", Engine: s.engine, SchemaVersion: version}, nil
}

func (s *Service) SystemStats(ctx context.Context, p Principal) (*SystemStats, error) {
	out, err := s.store.SystemStats(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}
	out.Engine = s.engine
	out.TenantID = p.TenantID
	out.GeneratedAt = s.now().UTC()
	return out, nil
}

func (s *Service) RunMaintenance(ctx context.Context, p Principal, in RunMaintenanceInput) (*MaintenanceResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.store.RunMaintenance(ctx, in, s.now().UTC())
	if err != nil {
		return nil, err
	}
	out.Engine = s.engine
	out.TenantID = p.TenantID
	s.audit(ctx, p, "maintenance.run", "", "system", "maintenance", "Ran database maintenance", map[string]any{"task_count": len(out.Tasks), "valid": out.Valid})
	return out, nil
}

func (s *Service) CreateDataset(ctx context.Context, p Principal, in CreateDatasetInput) (*Dataset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	domain, err := normalizeIdentifier(in.Domain, "domain")
	if err != nil {
		return nil, err
	}
	name, err := normalizeIdentifier(in.Name, "name")
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = domain + "." + name
	}
	if err := validateDatasetID(id); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	dataset := Dataset{ID: id, TenantID: p.TenantID, Domain: domain, Name: name, Title: strings.TrimSpace(in.Title), Description: strings.TrimSpace(in.Description), SchemaVersion: 1, CreatedAt: now, UpdatedAt: now}
	out, err := s.store.CreateDataset(ctx, dataset)
	if err == nil {
		s.audit(ctx, p, "dataset.create", id, "dataset", id, "Created dataset "+id, map[string]any{"domain": domain, "name": name})
	}
	return out, err
}

func (s *Service) ListDatasets(ctx context.Context, p Principal, in QueryDatasetsInput) ([]Dataset, error) {
	items, err := s.store.ListDatasets(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}
	return paginateDatasets(items, in), nil
}

func paginateDatasets(items []Dataset, in QueryDatasetsInput) []Dataset {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := append([]Dataset(nil), items...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	before := strings.TrimSpace(in.Before)
	beforeID := strings.TrimSpace(in.BeforeID)
	if before != "" {
		filtered := out[:0]
		for _, item := range out {
			updatedAt := item.UpdatedAt.Format(time.RFC3339Nano)
			if updatedAt < before || (beforeID != "" && updatedAt == before && item.ID < beforeID) {
				filtered = append(filtered, item)
			}
		}
		out = filtered
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Service) GetDataset(ctx context.Context, p Principal, datasetID string) (*Dataset, error) {
	return s.store.GetDataset(ctx, p.TenantID, strings.TrimSpace(datasetID))
}

func (s *Service) UpdateDataset(ctx context.Context, p Principal, datasetID string, in UpdateDatasetInput) (*Dataset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	datasetID = strings.TrimSpace(datasetID)
	out, err := s.store.UpdateDataset(ctx, p.TenantID, datasetID, in, s.now().UTC())
	if err == nil {
		s.audit(ctx, p, "dataset.update", datasetID, "dataset", datasetID, "Updated dataset "+datasetID, nil)
	}
	return out, err
}

func (s *Service) DeleteDataset(ctx context.Context, p Principal, datasetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	datasetID = strings.TrimSpace(datasetID)
	err := s.store.DeleteDataset(ctx, p.TenantID, datasetID)
	if err == nil {
		s.audit(ctx, p, "dataset.delete", datasetID, "dataset", datasetID, "Deleted dataset "+datasetID, nil)
	}
	return err
}

func (s *Service) UpsertFields(ctx context.Context, p Principal, datasetID string, in UpsertFieldsInput) ([]FieldDefinition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.store.GetDataset(ctx, p.TenantID, strings.TrimSpace(datasetID)); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	fields := make([]FieldDefinition, 0, len(in.Fields))
	for _, field := range in.Fields {
		key, err := normalizeIdentifier(field.Key, "field key")
		if err != nil {
			return nil, err
		}
		fieldType := strings.ToLower(strings.TrimSpace(field.Type))
		field.Type = fieldType
		if err := validateFieldDefinition(field); err != nil {
			return nil, err
		}
		field.ID = strings.TrimSpace(field.ID)
		if field.ID == "" {
			field.ID = newID("field")
		}
		field.TenantID = p.TenantID
		field.DatasetID = strings.TrimSpace(datasetID)
		field.Key = key
		field.Type = fieldType
		field.Title = strings.TrimSpace(field.Title)
		field.CreatedAt = now
		field.UpdatedAt = now
		fields = append(fields, field)
	}
	out, err := s.store.UpsertFields(ctx, p.TenantID, strings.TrimSpace(datasetID), fields)
	if err == nil {
		s.audit(ctx, p, "schema.fields_upsert", strings.TrimSpace(datasetID), "dataset", strings.TrimSpace(datasetID), "Upserted field definitions", map[string]any{"field_count": len(fields)})
	}
	return out, err
}

func (s *Service) ListFields(ctx context.Context, p Principal, datasetID string, query ...QueryFieldsInput) ([]FieldDefinition, error) {
	if _, err := s.store.GetDataset(ctx, p.TenantID, strings.TrimSpace(datasetID)); err != nil {
		return nil, err
	}
	items, err := s.store.ListFields(ctx, p.TenantID, strings.TrimSpace(datasetID))
	if err != nil {
		return nil, err
	}
	if len(query) == 0 {
		return items, nil
	}
	return paginateFields(items, query[0]), nil
}

func paginateFields(items []FieldDefinition, in QueryFieldsInput) []FieldDefinition {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := append([]FieldDefinition(nil), items...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	before := strings.TrimSpace(in.Before)
	beforeID := strings.TrimSpace(in.BeforeID)
	if before != "" {
		filtered := out[:0]
		for _, item := range out {
			updatedAt := item.UpdatedAt.Format(time.RFC3339Nano)
			if updatedAt < before || (beforeID != "" && updatedAt == before && item.ID < beforeID) {
				filtered = append(filtered, item)
			}
		}
		out = filtered
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Service) ValidateRecord(ctx context.Context, p Principal, datasetID string, in ValidateRecordInput) (*ValidateRecordResult, error) {
	datasetID = strings.TrimSpace(datasetID)
	if _, err := s.store.GetDataset(ctx, p.TenantID, datasetID); err != nil {
		return nil, err
	}
	fields, err := s.store.ListFields(ctx, p.TenantID, datasetID)
	if err != nil {
		return nil, err
	}
	result := validateRecordDataResult(datasetID, fields, in.Data)
	uniqueErrors, err := s.uniqueConstraintErrors(ctx, p, datasetID, fields, in.Data, "")
	if err != nil {
		return nil, err
	}
	if len(uniqueErrors) > 0 {
		result.Valid = false
		result.Errors = append(result.Errors, uniqueErrors...)
	}
	return &result, nil
}

func (s *Service) BatchImportRecords(ctx context.Context, p Principal, datasetID string, in BatchImportRecordsInput) (*BatchImportRecordsResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	datasetID = strings.TrimSpace(datasetID)
	if _, err := s.store.GetDataset(ctx, p.TenantID, datasetID); err != nil {
		return nil, err
	}
	if err := validateBatchImportRecordCount(len(in.Records)); err != nil {
		return nil, err
	}
	fields, err := s.store.ListFields(ctx, p.TenantID, datasetID)
	if err != nil {
		return nil, err
	}
	result := &BatchImportRecordsResult{DatasetID: datasetID, DryRun: in.DryRun, Valid: true, Total: len(in.Records), Validations: make([]BatchRecordValidation, len(in.Records))}
	now := s.now().UTC()
	records := make([]Record, 0, len(in.Records))
	for index, item := range in.Records {
		recordID := strings.TrimSpace(item.ID)
		if recordID == "" {
			recordID = newID("record")
		}
		validation := validateRecordDataResult(datasetID, fields, item.Data)
		result.Validations[index] = BatchRecordValidation{Index: index, ID: recordID, Valid: validation.Valid, Errors: validation.Errors, UnknownFields: validation.UnknownFields}
		records = append(records, Record{ID: recordID, TenantID: p.TenantID, DatasetID: datasetID, Title: strings.TrimSpace(item.Title), Tags: normalizeTags(item.Tags), Data: cloneJSONMap(item.Data), SourceID: strings.TrimSpace(item.SourceID), CreatedBy: p.UserID, UpdatedBy: p.UserID, CreatedAt: now, UpdatedAt: now})
	}
	if err := appendBatchUniqueValidationErrors(ctx, s, p, datasetID, fields, in.Records, result.Validations); err != nil {
		return nil, err
	}
	result.Valid = true
	for _, validation := range result.Validations {
		if !validation.Valid {
			result.Valid = false
			break
		}
	}
	if !result.Valid || in.DryRun {
		return result, nil
	}
	imported, err := s.store.ImportRecords(ctx, records)
	if err != nil {
		return nil, err
	}
	result.Imported = len(imported)
	result.Records = maskSensitiveRecords(imported, fields, p)
	for _, record := range imported {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := s.appendRecordRevision(ctx, p, "import", record); err != nil {
			return nil, err
		}
	}
	s.audit(ctx, p, "record.batch_import", datasetID, "dataset", datasetID, "Batch imported records", map[string]any{"record_count": len(imported)})
	return result, nil
}

const maxBatchImportRecords = 1000

func validateBatchImportRecordCount(count int) error {
	if count == 0 {
		return fmt.Errorf("%w: records are required", ErrInvalidInput)
	}
	if count > maxBatchImportRecords {
		return fmt.Errorf("%w: batch import supports at most %d records", ErrInvalidInput, maxBatchImportRecords)
	}
	return nil
}

func (s *Service) CreateRecord(ctx context.Context, p Principal, datasetID string, in CreateRecordInput) (*Record, error) {
	datasetID = strings.TrimSpace(datasetID)
	if _, err := s.store.GetDataset(ctx, p.TenantID, datasetID); err != nil {
		return nil, err
	}
	if len(in.Data) == 0 {
		return nil, fmt.Errorf("%w: data is required", ErrInvalidInput)
	}
	fields, err := s.store.ListFields(ctx, p.TenantID, datasetID)
	if err != nil {
		return nil, err
	}
	if err := validateRecordData(fields, in.Data); err != nil {
		return nil, err
	}
	if err := s.validateUniqueConstraints(ctx, p, datasetID, fields, in.Data, ""); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	recordID := strings.TrimSpace(in.ID)
	if recordID == "" {
		recordID = newID("record")
	}
	record := Record{ID: recordID, TenantID: p.TenantID, DatasetID: datasetID, Title: strings.TrimSpace(in.Title), Tags: normalizeTags(in.Tags), Data: cloneJSONMap(in.Data), SourceID: strings.TrimSpace(in.SourceID), CreatedBy: p.UserID, UpdatedBy: p.UserID, CreatedAt: now, UpdatedAt: now}

	// Use write batcher if available (eliminates mutex contention).
	// All write operations (create + revision + audit) go through the batcher
	// to avoid competing for the single write connection.
	var out *Record
	if s.writeBatcher != nil {
		var createErr error
		batchErr := s.writeBatcher.Submit(ctx, func(bctx context.Context, store *SQLiteStore) error {
			out, createErr = store.CreateRecord(bctx, record)
			if createErr != nil {
				return createErr
			}
			// Revision and audit within same serialized slot — no connection contention.
			_ = s.appendRecordRevision(bctx, p, "create", *out)
			s.audit(bctx, p, "record.create", datasetID, "record", recordID, "Created record "+recordID, map[string]any{"source_id": record.SourceID})
			return nil
		})
		if batchErr != nil && createErr == nil {
			createErr = batchErr
		}
		err = createErr
	} else {
		s.mu.Lock()
		out, err = s.store.CreateRecord(ctx, record)
		if err == nil {
			_ = s.appendRecordRevision(ctx, p, "create", *out)
			s.audit(ctx, p, "record.create", datasetID, "record", recordID, "Created record "+recordID, map[string]any{"source_id": record.SourceID})
		}
		s.mu.Unlock()
	}

	if err == nil && out != nil {
		out = maskSensitiveRecord(out, fields, p)
	}
	return out, err
}

func (s *Service) GetRecord(ctx context.Context, p Principal, datasetID, recordID string) (*Record, error) {
	datasetID = strings.TrimSpace(datasetID)
	record, err := s.store.GetRecord(ctx, p.TenantID, datasetID, strings.TrimSpace(recordID))
	if err != nil {
		return nil, err
	}
	fields, err := s.store.ListFields(ctx, p.TenantID, datasetID)
	if err != nil {
		return nil, err
	}
	return maskSensitiveRecord(record, fields, p), nil
}

func (s *Service) QueryRecordRevisions(ctx context.Context, p Principal, datasetID, recordID string, in QueryRecordRevisionsInput) ([]RecordRevision, error) {
	datasetID = strings.TrimSpace(datasetID)
	recordID = strings.TrimSpace(recordID)
	if _, err := s.store.GetDataset(ctx, p.TenantID, datasetID); err != nil {
		return nil, err
	}
	fields, err := s.store.ListFields(ctx, p.TenantID, datasetID)
	if err != nil {
		return nil, err
	}
	revisions, err := s.store.QueryRecordRevisions(ctx, p.TenantID, datasetID, recordID, in)
	if err != nil {
		return nil, err
	}
	return maskSensitiveRevisions(revisions, fields, p), nil
}

func (s *Service) UpdateRecord(ctx context.Context, p Principal, datasetID, recordID string, in UpdateRecordInput) (*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	datasetID = strings.TrimSpace(datasetID)
	recordID = strings.TrimSpace(recordID)
	if _, err := s.store.GetRecord(ctx, p.TenantID, datasetID, recordID); err != nil {
		return nil, err
	}
	if in.Data != nil {
		fields, err := s.store.ListFields(ctx, p.TenantID, datasetID)
		if err != nil {
			return nil, err
		}
		if err := validateRecordData(fields, in.Data); err != nil {
			return nil, err
		}
		if err := s.validateUniqueConstraints(ctx, p, datasetID, fields, in.Data, recordID); err != nil {
			return nil, err
		}
	}
	out, err := s.store.UpdateRecord(ctx, p.TenantID, datasetID, recordID, in, p.UserID, s.now().UTC())
	if err == nil {
		s.audit(ctx, p, "record.update", datasetID, "record", recordID, "Updated record "+recordID, nil)
		if revErr := s.appendRecordRevision(ctx, p, "update", *out); revErr != nil {
			return nil, revErr
		}
	}
	if err == nil {
		fields, fieldErr := s.store.ListFields(ctx, p.TenantID, datasetID)
		if fieldErr != nil {
			return nil, fieldErr
		}
		out = maskSensitiveRecord(out, fields, p)
	}
	return out, err
}

func (s *Service) BulkUpdateRecords(ctx context.Context, p Principal, datasetID string, in BulkUpdateRecordsInput) (*BulkUpdateRecordsResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	datasetID = strings.TrimSpace(datasetID)
	if _, err := s.store.GetDataset(ctx, p.TenantID, datasetID); err != nil {
		return nil, err
	}
	if len(in.SetData) == 0 && len(in.UnsetFields) == 0 && in.Title == nil && in.Tags == nil {
		return nil, fmt.Errorf("%w: bulk update requires set, unset, title, or tags", ErrInvalidInput)
	}
	limit := in.Limit
	if limit <= 0 {
		limit = in.Query.Limit
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if !in.DryRun && !in.Confirm {
		return nil, fmt.Errorf("%w: bulk update requires confirm=true when dry_run is false", ErrInvalidInput)
	}
	query := in.Query
	query.Limit = limit
	records, err := s.store.QueryRecords(ctx, p.TenantID, datasetID, query)
	if err != nil {
		return nil, err
	}
	fields, err := s.store.ListFields(ctx, p.TenantID, datasetID)
	if err != nil {
		return nil, err
	}
	result := &BulkUpdateRecordsResult{DatasetID: datasetID, DryRun: in.DryRun, Valid: true, Total: len(records), Validations: make([]BulkUpdateRecordValidation, len(records))}
	uniqueSeen := map[string]string{}
	for index, record := range records {
		nextData := applyBulkDataPatch(record.Data, in.SetData, in.UnsetFields)
		validation := validateRecordDataResult(datasetID, fields, nextData)
		result.Validations[index] = BulkUpdateRecordValidation{Index: index, ID: record.ID, Valid: validation.Valid, Errors: validation.Errors, UnknownFields: validation.UnknownFields}
		if errs := bulkUniqueErrors(ctx, s, p, datasetID, fields, record.ID, nextData, uniqueSeen); len(errs) > 0 {
			result.Validations[index].Valid = false
			result.Validations[index].Errors = append(result.Validations[index].Errors, errs...)
		}
		if !result.Validations[index].Valid {
			result.Valid = false
		}
		preview := record
		preview.Data = nextData
		if in.Title != nil {
			preview.Title = strings.TrimSpace(*in.Title)
		}
		if in.Tags != nil {
			preview.Tags = normalizeTags(in.Tags)
		}
		result.Records = append(result.Records, preview)
	}
	result.Records = maskSensitiveRecords(result.Records, fields, p)
	if !result.Valid || in.DryRun {
		return result, nil
	}
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		nextData := applyBulkDataPatch(record.Data, in.SetData, in.UnsetFields)
		update := UpdateRecordInput{Data: nextData}
		if in.Title != nil {
			title := strings.TrimSpace(*in.Title)
			update.Title = &title
		}
		if in.Tags != nil {
			update.Tags = normalizeTags(in.Tags)
		}
		out, err := s.store.UpdateRecord(ctx, p.TenantID, datasetID, record.ID, update, p.UserID, s.now().UTC())
		if err != nil {
			return nil, err
		}
		if err := s.appendRecordRevision(ctx, p, "bulk_update", *out); err != nil {
			return nil, err
		}
		result.Updated++
	}
	s.audit(ctx, p, "record.bulk_update", datasetID, "dataset", datasetID, "Bulk updated records", map[string]any{"record_count": result.Updated, "reason": strings.TrimSpace(in.Reason), "dry_run": false})
	return result, nil
}

func (s *Service) BulkDeleteRecords(ctx context.Context, p Principal, datasetID string, in BulkDeleteRecordsInput) (*BulkDeleteRecordsResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	datasetID = strings.TrimSpace(datasetID)
	if _, err := s.store.GetDataset(ctx, p.TenantID, datasetID); err != nil {
		return nil, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = in.Query.Limit
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if !in.DryRun && !in.Confirm {
		return nil, fmt.Errorf("%w: bulk delete requires confirm=true when dry_run is false", ErrInvalidInput)
	}
	query := in.Query
	query.Limit = limit
	records, err := s.store.QueryRecords(ctx, p.TenantID, datasetID, query)
	if err != nil {
		return nil, err
	}
	fields, err := s.store.ListFields(ctx, p.TenantID, datasetID)
	if err != nil {
		return nil, err
	}
	result := &BulkDeleteRecordsResult{DatasetID: datasetID, DryRun: in.DryRun, Total: len(records), RecordIDs: make([]string, 0, len(records)), Records: make([]Record, 0, len(records))}
	for _, record := range records {
		result.RecordIDs = append(result.RecordIDs, record.ID)
		result.Records = append(result.Records, record)
	}
	result.Records = maskSensitiveRecords(result.Records, fields, p)
	if in.DryRun {
		return result, nil
	}
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := s.store.DeleteRecord(ctx, p.TenantID, datasetID, record.ID); err != nil {
			return nil, err
		}
		if err := s.appendRecordRevision(ctx, p, "bulk_delete", record); err != nil {
			return nil, err
		}
		result.Deleted++
	}
	s.audit(ctx, p, "record.bulk_delete", datasetID, "dataset", datasetID, "Bulk deleted records", map[string]any{"record_count": result.Deleted, "reason": strings.TrimSpace(in.Reason), "dry_run": false})
	return result, nil
}

func (s *Service) DeleteRecord(ctx context.Context, p Principal, datasetID, recordID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	datasetID = strings.TrimSpace(datasetID)
	recordID = strings.TrimSpace(recordID)
	before, err := s.store.GetRecord(ctx, p.TenantID, datasetID, recordID)
	if err != nil {
		return err
	}
	err = s.store.DeleteRecord(ctx, p.TenantID, datasetID, recordID)
	if err == nil {
		if revErr := s.appendRecordRevision(ctx, p, "delete", *before); revErr != nil {
			return revErr
		}
		s.audit(ctx, p, "record.delete", datasetID, "record", recordID, "Deleted record "+recordID, nil)
	}
	return err
}

func (s *Service) RestoreRecord(ctx context.Context, p Principal, datasetID, recordID string, in RestoreRecordInput) (*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !in.Confirm {
		return nil, fmt.Errorf("%w: restore record requires confirm=true", ErrInvalidInput)
	}
	datasetID = strings.TrimSpace(datasetID)
	recordID = strings.TrimSpace(recordID)
	if _, err := s.store.GetDataset(ctx, p.TenantID, datasetID); err != nil {
		return nil, err
	}
	if existing, err := s.store.GetRecord(ctx, p.TenantID, datasetID, recordID); err == nil && existing != nil {
		return nil, fmt.Errorf("%w: record already exists", ErrAlreadyExists)
	} else if err != nil && !errors.Is(err, ErrRecordNotFound) {
		return nil, err
	}
	revisions, err := s.store.QueryRecordRevisions(ctx, p.TenantID, datasetID, recordID, QueryRecordRevisionsInput{Limit: 500})
	if err != nil {
		return nil, err
	}
	revisionID := strings.TrimSpace(in.RevisionID)
	var revision *RecordRevision
	for i := range revisions {
		item := &revisions[i]
		if revisionID != "" && item.ID != revisionID {
			continue
		}
		if revisionID == "" && item.Action != "delete" && item.Action != "bulk_delete" {
			continue
		}
		revision = item
		break
	}
	if revision == nil {
		return nil, fmt.Errorf("%w: no matching deleted record revision found", ErrRecordNotFound)
	}
	fields, err := s.store.ListFields(ctx, p.TenantID, datasetID)
	if err != nil {
		return nil, err
	}
	if err := validateRecordData(fields, revision.Data); err != nil {
		return nil, err
	}
	if err := s.validateUniqueConstraints(ctx, p, datasetID, fields, revision.Data, recordID); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	record := Record{
		ID:        recordID,
		TenantID:  p.TenantID,
		DatasetID: datasetID,
		Title:     revision.Title,
		Tags:      append([]string(nil), revision.Tags...),
		Data:      cloneJSONMap(revision.Data),
		SourceID:  revision.SourceID,
		CreatedBy: p.UserID,
		UpdatedBy: p.UserID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	out, err := s.store.CreateRecord(ctx, record)
	if err != nil {
		return nil, err
	}
	if err := s.appendRecordRevision(ctx, p, "restore", *out); err != nil {
		return nil, err
	}
	s.audit(ctx, p, "record.restore", datasetID, "record", recordID, "Restored record "+recordID, map[string]any{"revision_id": revision.ID, "reason": strings.TrimSpace(in.Reason)})
	return maskSensitiveRecord(out, fields, p), nil
}

func (s *Service) QueryRecords(ctx context.Context, p Principal, datasetID string, in QueryRecordsInput) ([]Record, error) {
	if _, err := s.store.GetDataset(ctx, p.TenantID, strings.TrimSpace(datasetID)); err != nil {
		return nil, err
	}
	datasetID = strings.TrimSpace(datasetID)
	fields, err := s.store.ListFields(ctx, p.TenantID, datasetID)
	if err != nil {
		return nil, err
	}
	records, err := s.store.QueryRecords(ctx, p.TenantID, datasetID, in)
	if err != nil {
		return nil, err
	}
	return maskSensitiveRecords(records, fields, p), nil
}

func (s *Service) QueryAuditLogs(ctx context.Context, p Principal, in QueryAuditLogsInput) ([]AuditLog, error) {
	return s.store.QueryAuditLogs(ctx, p.TenantID, in)
}

func (s *Service) CreateBackup(ctx context.Context, p Principal, in CreateBackupInput) (*BackupInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.store.CreateBackup(ctx, in, p.UserID, s.now().UTC())
	if err == nil {
		s.audit(ctx, p, "backup.create", "", "backup", out.ID, "Created backup "+out.ID, backupAuditMetadata(*out, ""))
	}
	return out, err
}

func (s *Service) ListBackups(ctx context.Context, p Principal, in QueryBackupsInput) ([]BackupInfo, error) {
	in.Before = strings.TrimSpace(in.Before)
	in.BeforeID = strings.TrimSpace(in.BeforeID)
	return s.store.ListBackups(ctx, in)
}

func (s *Service) GetBackup(ctx context.Context, p Principal, backupID string) (*BackupInfo, error) {
	return s.store.GetBackup(ctx, strings.TrimSpace(backupID))
}

func (s *Service) ReadBackup(ctx context.Context, p Principal, backupID string) ([]byte, *BackupInfo, error) {
	backupID = strings.TrimSpace(backupID)
	data, info, err := s.store.ReadBackup(ctx, backupID)
	if err == nil {
		metadata := backupAuditMetadata(*info, "")
		metadata["downloaded_size_bytes"] = len(data)
		s.audit(ctx, p, "backup.download", "", "backup", backupID, "Downloaded backup "+backupID, metadata)
	}
	return data, info, err
}

func (s *Service) RestoreBackup(ctx context.Context, p Principal, backupID string, in RestoreBackupInput) (*RestoreResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	backupID = strings.TrimSpace(backupID)
	out, err := s.store.RestoreBackup(ctx, backupID, in, p.UserID, s.now().UTC())
	if err == nil {
		metadata := backupAuditMetadata(out.Backup, strings.TrimSpace(in.Reason))
		metadata["status"] = out.Status
		metadata["restored_by"] = out.RestoredBy
		metadata["restored_at"] = out.RestoredAt.UTC().Format(time.RFC3339Nano)
		s.audit(ctx, p, "backup.restore", "", "backup", backupID, "Restored backup "+backupID, metadata)
	}
	return out, err
}

func backupAuditMetadata(info BackupInfo, reason string) map[string]any {
	metadata := map[string]any{
		"backup_id":    info.ID,
		"name":         info.Name,
		"engine":       info.Engine,
		"size_bytes":   info.SizeBytes,
		"sha256":       info.SHA256,
		"download_url": info.DownloadURL,
		"created_by":   info.CreatedBy,
		"created_at":   info.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if strings.TrimSpace(reason) != "" {
		metadata["reason"] = strings.TrimSpace(reason)
	}
	return metadata
}

func (s *Service) audit(ctx context.Context, p Principal, action, datasetID, targetType, targetID, summary string, metadata map[string]any) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	_, _ = s.store.AppendAuditLog(ctx, AuditLog{ID: newID("audit"), TenantID: p.TenantID, UserID: p.UserID, Action: strings.TrimSpace(action), DatasetID: strings.TrimSpace(datasetID), TargetType: strings.TrimSpace(targetType), TargetID: strings.TrimSpace(targetID), Summary: strings.TrimSpace(summary), Metadata: metadata, CreatedAt: s.now().UTC()})
}

func recordPageCursor(records []Record, sortSpecs []SortSpec) (string, string) {
	if len(records) == 0 {
		return "", ""
	}
	last := records[len(records)-1]
	nextBefore := last.CreatedAt.Format(time.RFC3339Nano)
	if len(sortSpecs) > 0 {
		return nextBefore, ""
	}
	return nextBefore, last.ID
}
func normalizeIdentifier(value, name string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if !identifierPattern.MatchString(value) {
		return "", fmt.Errorf("%w: %s must use letters, numbers, underscores, or hyphens", ErrInvalidInput, name)
	}
	return value, nil
}

func validateDatasetID(id string) error {
	parts := strings.Split(id, ".")
	if len(parts) != 2 {
		return fmt.Errorf("%w: dataset id must be domain.name", ErrInvalidInput)
	}
	for _, part := range parts {
		if _, err := normalizeIdentifier(part, "dataset id"); err != nil {
			return err
		}
	}
	return nil
}

func normalizeTags(tags []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
	}
	return out
}

func applyBulkDataPatch(current, setData map[string]any, unsetFields []string) map[string]any {
	out := cloneJSONMap(current)
	if out == nil {
		out = map[string]any{}
	}
	for _, key := range unsetFields {
		key = strings.TrimSpace(key)
		if key != "" {
			delete(out, key)
		}
	}
	for key, value := range setData {
		key = strings.TrimSpace(key)
		if key != "" {
			out[key] = value
		}
	}
	return out
}

func bulkUniqueErrors(ctx context.Context, s *Service, p Principal, datasetID string, fields []FieldDefinition, recordID string, data map[string]any, seen map[string]string) []string {
	out := []string{}
	for _, field := range uniqueFieldDefinitions(fields) {
		key := strings.TrimSpace(field.Key)
		value, exists := data[key]
		if !exists || isEmptyValue(value) {
			continue
		}
		seenKey := key + "\x00" + comparableJSONValue(value)
		if otherID, ok := seen[seenKey]; ok && otherID != recordID {
			out = append(out, fmt.Sprintf("field %s value %s duplicates bulk update record %s", key, comparableJSONValue(value), otherID))
			continue
		}
		seen[seenKey] = recordID
	}
	errs, err := s.uniqueConstraintErrors(ctx, p, datasetID, fields, data, recordID)
	if err != nil {
		out = append(out, err.Error())
		return out
	}
	out = append(out, errs...)
	return out
}
