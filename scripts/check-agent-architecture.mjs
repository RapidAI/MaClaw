import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(scriptDir, '..');
const failures = [];

const read = (rel) => fs.readFileSync(path.join(repoRoot, rel), 'utf8');
const exists = (rel) => fs.existsSync(path.join(repoRoot, rel));
const walk = (rel, out = []) => {
  const absolute = path.join(repoRoot, rel);
  if (!fs.existsSync(absolute)) return out;
  for (const entry of fs.readdirSync(absolute, { withFileTypes: true })) {
    const child = path.join(rel, entry.name).replace(/\\/g, '/');
    if (entry.isDirectory()) {
      if (child.endsWith('/node_modules') || child.endsWith('/dist')) continue;
      walk(child, out);
    } else {
      out.push(child);
    }
  }
  return out;
};
const requireFile = (rel) => {
  if (!exists(rel)) failures.push(`missing required file: ${rel}`);
};
const requireText = (rel, needle, label = needle) => {
  if (!exists(rel)) {
    failures.push(`missing ${rel}; cannot check ${label}`);
    return;
  }
  if (!read(rel).includes(needle)) failures.push(`${rel} is missing ${label}`);
};

requireFile('corelib/agentruntime/runtime.go');
requireFile('corelib/agentruntime/capability_surface.go');
requireText('corelib/agentruntime/capability_surface.go', 'CapabilityToolsFromOpenAI', 'shared capability surface projection');
requireText('corelib/agentruntime/capability_surface.go', 'CapabilityToolsFromSpecs', 'shared legacy catalog projection');
requireText('corelib/agentruntime/turn_input.go', 'type TurnInput struct', 'transport-neutral Runtime turn input');
requireText('corelib/agentruntime/turn_input.go', 'type TurnCallbacks struct', 'transport-neutral callback seam');
requireText('corelib/agentruntime/event_sink.go', 'type FanoutEventSink struct', 'shared durable/presentation event fanout');
requireText('corelib/agentruntime/event_types.go', 'EventRunStarted', 'shared event type vocabulary');
requireText('corelib/agentruntime/event_types.go', 'EventProgress', 'shared progress event vocabulary');
requireText('guiapp/runtime_event_projection.go', 'type runtimeEventProjectionSink struct', 'GUI consumes shared event envelope through an adapter');
requireText('guiapp/im_agent_loop_shared.go', 'var eventSequence atomic.Uint64', 'GUI lifecycle events share one monotonic sequence');
requireText('guiapp/app_config_schema.go', 'func (a *App) GetAppConfigSchema', 'GUI configuration uses the canonical core schema');
requireText('guiapp/frontend/wailsjs/go/main/App.js', 'GetAppConfigSchema', 'frontend Wails binding for canonical config schema');
requireText('guiapp/frontend/wailsjs/go/main/App.d.ts', 'GetAppConfigSchema', 'frontend TypeScript binding for canonical config schema');
requireText('guiapp/im_agent_loop_shared.go', 'Input: agentruntime.TurnInput', 'GUI Runtime must submit the neutral turn contract');
requireText('guiapp/im_agent_loop_shared.go', 'runtime_event_sink_failed', 'GUI must fail closed when the shared event sink rejects a turn');
requireText('guiapp/im_handler_wiring.go', 'func NewIMMessageHandlerWithRuntime', 'GUI composition root runtime injection');
requireText('guiapp/im_handler_standalone.go', 'SharedAgentRuntime agentruntime.Runtime', 'standalone host runtime injection');
requireText('corelib/agentservice/runtime_adapter.go', 'runtimeTurnInputPrompt', 'headless capability snapshots consume neutral turn input');
requireText('corelib/agentservice/runtime_adapter.go', 'agentruntime.DecodeTurnInput', 'shared neutral turn input decoder');
requireFile('corelib/agentruntime/reasoning.go');
requireFile('corelib/agentruntime/semantic_history.go');
requireFile('corelib/agentruntime/attachments.go');
requireFile('corelib/agentruntime/job.go');
requireFile('corelib/agentruntime/job_store.go');
requireFile('corelib/agentruntime/job_reporter.go');
requireFile('corelib/agentruntime/job_retry.go');
requireFile('corelib/agentruntime/job_idempotency.go');
requireFile('corelib/agentruntime/job_effect.go');
requireFile('corelib/agentruntime/job_retention.go');
requireFile('corelib/agentruntime/visible_text.go');
requireFile('corelib/agentruntime/parameter_rejection.go');
requireFile('corelib/agentruntime/semantic_outcomes.go');
requireFile('corelib/agentruntime/correlation.go');
requireFile('corelib/agentruntime/prompt_sections.go');
requireFile('corelib/agentruntime/tool_calls.go');
requireFile('corelib/agentruntime/semantic_pdf.go');
requireFile('corelib/agentruntime/semantic_pdf_text.go');
requireFile('corelib/agentruntime/artifact_outcomes.go');
requireFile('corelib/agentruntime/semantic_evidence.go');
requireFile('corelib/agentruntime/tool_result.go');
requireFile('corelib/agentruntime/tool_text_result.go');
requireFile('corelib/database/tool.go');
requireText('corelib/database/tool.go', 'func ToolDescription', 'shared database tool description');
requireText('corelib/database/tool.go', 'func ToolParameters', 'shared database tool schema');
requireText('corelib/database/tool.go', 'func HandleTool', 'shared database tool execution');
requireFile('corelib/tool/semantic_artifact_projection.go');
requireFile('corelib/agentruntime/collections.go');
requireText('corelib/agentruntime/tool_calls.go', 'NormalizeToolArgumentsJSON', 'shared tool argument normalization');
requireText('corelib/agentruntime/tool_calls.go', 'ParseToolArgumentsObject', 'shared tool argument object parser');
requireText('corelib/agentruntime/tool_calls.go', 'CanonicalizeBrowserToolCall', 'shared browser alias canonicalizer');
requireText('corelib/agentruntime/tool_calls.go', 'CanonicalizeLocalFileSearchToolCall', 'shared local search alias canonicalizer');
requireText('corelib/agentruntime/tool_calls.go', 'CanonicalizeToolCallJSON', 'combined shared tool-call canonicalizer');
requireText('corelib/agentruntime/prompt_sections.go', 'BuildLightPrompt', 'shared light prompt builder');
requireText('corelib/agentruntime/prompt_sections.go', 'DesktopWorkflowDocumentDeliveryPrompt', 'shared desktop workflow delivery contract');
requireText('corelib/agentruntime/prompt_sections.go', 'IMWorkflowDocumentDeliveryPrompt', 'shared IM workflow delivery contract');
requireText('corelib/agentruntime/prompt_sections.go', 'EnsureLightSemanticGrantPromptFence', 'shared bounded light semantic fence');
requireText('guiapp/im_system_prompt.go', 'agentruntime.BuildLightPrompt', 'GUI light prompt adapter');
requireText('guiapp/im_system_prompt.go', 'agentruntime.EnsureLightSemanticGrantPromptFence', 'GUI light semantic fence adapter');
requireText('guiapp/im_system_prompt.go', 'agentruntime.DesktopWorkflowDocumentDeliveryPrompt', 'GUI desktop delivery adapter');
requireText('guiapp/im_system_prompt.go', 'agentruntime.IMWorkflowDocumentDeliveryPrompt', 'GUI IM delivery adapter');
requireText('corelib/agentruntime/correlation.go', 'func TraceID', 'shared trace-id context accessor');
requireText('corelib/agentruntime/correlation.go', 'func SpanID', 'shared span-id context accessor');
requireFile('corelib/agentruntime/metrics.go');
requireText('corelib/agentruntime/metrics.go', 'type RuntimeMetrics struct', 'shared Runtime metrics collector');
requireFile('corelib/agentruntime/rate_limiter.go');
requireText('corelib/agentruntime/rate_limiter.go', 'type TenantTokenBucket struct', 'shared tenant token-bucket limiter');
requireText('corelib/agentruntime/metrics.go', 'RecordRateLimited', 'shared rate-limit telemetry');
requireText('corelib/agentservice/service.go', 'RuntimeRateLimitRate', 'Service rate-limit composition config');
requireText('MaClawSrv/runtime_rate_limit_config.go', 'runtimeRateLimitRateEnv', 'srv rate-limit environment wiring');
requireText('corelib/agentservice/service.go', 'type DistributedRateLimiter interface', 'cross-instance rate-limit host port');
requireText('corelib/agentservice/service_messaging.go', 'distributedLimiter', 'distributed rate-limit enforcement');
requireText('corelib/agentservice/service.go', 'metrics: agentruntime.NewRuntimeMetrics', 'Service Runtime metrics composition');
requireText('MaClawSrv/http.go', 'appendRuntimeMetricsPrometheus', 'Prometheus Runtime metrics projection');
requireText('MaClawSrv/jobs.go', 'type asyncJobStatus = agentruntime.JobStatus', 'shared async job status vocabulary');
requireText('guiapp/app_knowledge.go', 'runtime_status', 'GUI background jobs expose shared status vocabulary');
requireText('guiapp/app_user_data_migration.go', 'RuntimeStatus agentruntime.JobStatus', 'GUI migration jobs expose shared status vocabulary');
requireText('guiapp/app_user_data_migration.go', 'agentruntime.WithJobReporter', 'GUI migration workers use shared JobReporter contract');
requireText('guiapp/app_user_data_migration.go', 'agentruntime.NewMemoryJobRepository', 'GUI migration jobs publish through shared repository boundary');
requireText('guiapp/skill_runner.go', 'RuntimeStatus     agentruntime.JobStatus', 'GUI skill runs expose shared status vocabulary');
requireText('guiapp/virtual_repository_operations.go', 'RuntimeStatus agentruntime.JobStatus', 'GUI repository operations expose shared status vocabulary');
requireText('guiapp/task_orchestrator.go', 'RuntimeStatus agentruntime.JobStatus', 'GUI orchestration plans expose shared status vocabulary');
requireText('guiapp/runtime_event_projection.go', 'shouldProjectRuntimeCallbacks', 'GUI callbacks project all shared Runtime events');
requireText('corelib/agentruntime/job.go', 'func ProjectLegacyJobStatus', 'shared legacy job status projection');
requireText('guiapp/remote_mobile_digital_employee_tasks.go', 'RuntimeStatus agentruntime.JobStatus', 'mobile digital employee tasks expose shared status vocabulary');
requireText('guiapp/remote_mobile_document_tasks.go', 'RuntimeStatus     agentruntime.JobStatus', 'mobile document tasks expose shared status vocabulary');
requireText('guiapp/remote_mobile_backend_ssh_sessions.go', 'RuntimeStatus     agentruntime.JobStatus', 'mobile SSH tasks expose shared status vocabulary');
requireText('guiapp/skill_upload_status.go', 'RuntimeStatusValue', 'GUI skill upload queue exposes shared status vocabulary');
requireText('guiapp/download_progress_status.go', 'MarshalJSON adds the canonical Runtime status', 'GUI download progress exposes shared status vocabulary');
requireText('guiapp/app_embedding.go', 'runtime_status', 'model download progress exposes shared status vocabulary');
requireText('MaClawSrv/jobs.go', 'type asyncJobView = agentruntime.Job', 'shared async job envelope');
requireText('corelib/agentruntime/job_store.go', 'type JobRepository interface', 'shared multi-writer async job repository boundary');
requireText('corelib/agentruntime/job_store.go', 'ErrJobRepositoryVersionConflict', 'shared async job CAS conflict');
requireText('corelib/agentruntime/job.go', 'LeaseOwnerID', 'shared transport-private async job lease metadata');
requireText('MaClawSrv/jobs.go', 'repository           agentruntime.JobRepository', 'shared async job repository composition');
requireText('MaClawSrv/jobs.go', 'effectRepository     agentruntime.JobEffectRepository', 'shared protected job effect repository composition');
requireText('MaClawSrv/jobs.go', 'agentruntime.JobReporter', 'shared async job reporter boundary');
requireText('MaClawSrv/jobs.go', 'agentruntime.WithJobReporter', 'worker-scoped shared job reporter');
requireText('MaClawSrv/jobs.go', 'agentruntime.IsJobErrorRetryable', 'explicit shared job retry classification');
requireText('MaClawSrv/jobs.go', 'agentruntime.JobRetryBackoff', 'shared job retry backoff');
requireText('MaClawSrv/jobs.go', 'agentruntime.WithJobAttempt', 'worker-scoped shared job attempt');
requireText('MaClawSrv/jobs.go', 'agentruntime.WithJobIdempotencyIdentity', 'worker-scoped safe job idempotency identity');
requireText('MaClawSrv/jobs.go', 'agentruntime.WithJobEffectRecorder', 'worker-scoped protected job effect recorder');
requireText('MaClawSrv/jobs.go', 'reconcileUserJob', 'non-replaying domain job reconciliation boundary');
requireText('MaClawSrv/jobs.go', 'cleanupDeletedJobEffectsLocked', 'protected effect cleanup on Job deletion');
requireText('MaClawSrv/domain_reconcilers.go', 'domainJobReconcilerFor', 'single domain reconciler mapping for user/admin transports');
requireText('MaClawSrv/admin_runtime.go', 'reconcileUserJob', 'admin Job read uses evidence-backed reconciliation');
requireText('MaClawSrv/request_context.go', 'X-Request-ID', 'request correlation id boundary');
requireText('MaClawSrv/request_context.go', 'agentruntime.WithCorrelationID', 'shared correlation context propagation');
requireText('MaClawSrv/request_context.go', 'traceIDFromTraceParent', 'distributed trace context propagation');
requireText('corelib/agentservice/service.go', 'agentruntime.CorrelationID', 'run/audit/runtime correlation propagation');
requireText('corelib/agentservice/service.go', 'agentruntime.TraceID', 'run/audit trace propagation');
requireText('corelib/agentruntime/job_effect.go', 'type JobEffectRepository interface', 'shared protected job effect repository boundary');
requireText('corelib/agentruntime/job_effect.go', 'type JobReconciler interface', 'shared terminal-only job reconciler boundary');
requireText('corelib/agentservice/job_effect_repository.go', 'var _ agentruntime.JobEffectRepository', 'shared SQLite job effect repository implementation');
requireText('MaClawSrv/jobs.go', 'agentruntime.ErrJobIdempotencyConflict', 'atomic job idempotency conflict handling');
requireText('MaClawSrv/jobs.go', 'agentruntime.NormalizeJobStatus', 'fail-closed async job status normalization');
requireText('MaClawSrv/jobs.go', 'agentruntime.NormalizeJobRecoveryPolicy', 'fail-closed async job recovery policy');
requireText('corelib/agentruntime/job_lifecycle.go', 'JobErrorCodePersistenceFailed', 'shared async job persistence error code');
requireText('corelib/agentruntime/job_lifecycle.go', 'func BeginJobAttempt', 'shared job attempt start');
requireText('corelib/agentruntime/job_lifecycle.go', 'func ScheduleJobRetry', 'shared job retry schedule');
requireText('corelib/agentruntime/job_lifecycle.go', 'func ApplyJobWorkerOutcome', 'shared job worker completion envelope');
requireText('corelib/agentruntime/job_lifecycle.go', 'func StampJobCanceled', 'shared job cancel envelope');
requireText('MaClawSrv/jobs.go', 'agentruntime.StampJobCanceled', 'srv cancel/shutdown stamps through shared envelope');
requireText('corelib/agentruntime/job_lifecycle.go', 'func MarkJobPersistenceFailed', 'shared persist-failure envelope');
requireText('corelib/agentruntime/job_lifecycle.go', 'func MarkJobCompletionPersistFailed', 'shared completion persist-failure envelope');
requireText('MaClawSrv/jobs.go', 'agentruntime.BeginJobAttempt', 'srv starts attempts through shared helper');
requireText('MaClawSrv/jobs.go', 'agentruntime.ScheduleJobRetry', 'srv retries through shared helper');
requireText('MaClawSrv/jobs.go', 'agentruntime.ApplyJobWorkerOutcome', 'srv completes workers through shared helper');
requireText('MaClawSrv/jobs.go', 'agentruntime.MarkJobPersistenceFailed', 'srv persist failures use shared envelope');
requireText('MaClawSrv/jobs.go', 'agentruntime.MarkJobCompletionPersistFailed', 'srv completion persist failures use shared envelope');
requireText('corelib/agentruntime/job_lifecycle.go', 'func StampHostJobStatus', 'shared host DTO job status stamp');
requireText('guiapp/skill_runner.go', 'agentruntime.StampHostJobStatus', 'GUI skill runs stamp jobs through shared worker envelope');
requireText('guiapp/task_orchestrator.go', 'agentruntime.StampHostJobStatus', 'GUI orchestration plans stamp jobs through shared worker envelope');
requireText('corelib/agentruntime/job_lease.go', 'JobErrorCodeServiceRestarted', 'shared async job restart error code');
requireText('MaClawSrv/jobs.go', 'agentruntime.JobErrorCodeReconcileRequired', 'shared async job reconciliation error code');
requireText('MaClawSrv/job_store.go', 'var _ agentruntime.JobStore', 'MaClawSrv JobStore adapter contract');
requireText('MaClawSrv/job_store.go', 'agentruntime.ValidateJobCheckpoint', 'durable checkpoint validation');
requireText('MaClawSrv/job_store.go', 'agentruntime.ValidateJobRetryState', 'durable retry-state validation');
requireText('MaClawSrv/job_store.go', 'agentruntime.ValidateJobIdempotencyState', 'durable job idempotency validation');
requireFile('MaClawSrv/job_repository.go');
requireText('MaClawSrv/job_repository.go', 'var _ agentruntime.JobRepository', 'MaClawSrv JobRepository adapter contract');
requireText('MaClawSrv/job_repository.go', 'NewSQLiteJobRepositoryWithLegacy', 'srv SQLite job repository is a thin wrapper over the shared production schema');
requireText('corelib/agentservice/sqlite_job_repository.go', 'async_jobs_idempotency_digest_uq', 'database-level async job idempotency uniqueness');
requireText('corelib/agentservice/sqlite_job_repository.go', 'WHERE job_id=? AND version=?', 'async job compare-and-swap update');
requireText('corelib/agentservice/sqlite_job_repository.go', 'lease_owner_id', 'durable async job worker lease owner');
requireText('corelib/agentservice/sqlite_job_repository.go', 'func NewSQLiteJobRepositoryWithLegacy', 'shared SQLite job repository accepts a one-time legacy snapshot loader');
requireText('corelib/agentservice/sqlite_job_repository.go', 'importRuntimeJobsOnce', 'GUI runtime_jobs one-time import into production schema');
requireText('corelib/agentruntime/job_envelope.go', 'func ValidateJobEnvelope', 'shared durable job envelope validation');
requireText('corelib/agentruntime/job_envelope.go', 'func CloneJob', 'shared durable job envelope clone');
requireText('MaClawSrv/jobs.go', 'runLeaseCoordinator', 'async job lease heartbeat coordinator');
requireText('MaClawSrv/jobs.go', 'reconcileExpiredJobsLocked', 'expired async job lifecycle reconciliation');
requireText('MaClawSrv/http.go', 'state", "jobs.db"', 'SQLite async job readiness path');
requireText('MaClawSrv/http_migration.go', 'agentruntime.ReportJobUpdate', 'migration shared job reporter consumer');
requireText('MaClawSrv/http_migration.go', 'agentruntime.PrepareJobEffect', 'migration protected effect preparation');
requireText('MaClawSrv/http_migration.go', 'migrationExportJobReconciler', 'migration export domain reconciler');
requireText('MaClawSrv/http_migration.go', 'migrationImportJobReconciler', 'migration import domain reconciler');
requireText('hub/internal/httpapi/migration_handlers.go', 'handleGetExport', 'read-only Hub migration receipt probe');
requireText('corelib/agentservice/mcp.go', 'agentruntime.MarkJobErrorRetryable', 'shared MCP probe retry classification');
requireText('MaClawSrv/http_mcp.go', 'agentruntime.JobRetryPolicy{MaxAttempts: 3', 'MCP health-check retry policy');
requireText('MaClawSrv/job_admission.go', 'agentruntime.NewJobIdempotencyIdentity', 'HTTP shared idempotent admission adapter');
requireText('MaClawSrv/http_knowledge.go', 'public_knowledge_import_text', 'optional idempotent public knowledge text import');
requireText('MaClawSrv/http_mcp.go', 's.admitUserJob', 'MCP health-check idempotent admission');
requireText('MaClawSrv/http_migration.go', 's.admitUserJob', 'migration idempotent admission');
requireText('corelib/reasoning_controls.go', 'ParseGlobalThinkingMode', 'shared thinking-mode parser');
requireFile('corelib/config/app_config_schema.go');
requireFile('corelib/agentservice/runtime_adapter.go');
requireFile('guiapp/shared_agent_runtime.go');
requireText('guiapp/im_agent_loop_shared.go', 'sharedAgentRuntime()', 'GUI Runtime adapter delegation');
requireText('guiapp/shared_agent_runtime.go', 'DescribeCapabilities', 'GUI Runtime capability surface');
requireText('guiapp/shared_agent_runtime.go', 'SetSharedAgentRuntime', 'GUI Runtime composition-root injection');
for (const rel of ['guiapp/shared_agent_runtime.go', 'guiapp/im_agent_loop_shared.go']) {
  if (exists(rel) && read(rel).includes('guiRuntimeTurn')) {
    failures.push(`${rel} still carries the removed private guiRuntimeTurn DTO`);
  }
}
requireText('corelib/agentservice/service.go', 'RuntimeModules []agentruntime.Module', 'Service runtime module composition root');
requireText('corelib/agentservice/service_events.go', 'serviceRuntimeEventSink', 'shared Runtime event sink');
requireText('corelib/agentruntime/runtime.go', 'EventID string', 'producer event id in Runtime event envelope');
requireText('corelib/agentruntime/runtime.go', 'OccurredAt time.Time', 'producer timestamp in Runtime event envelope');
requireText('corelib/agentruntime/runtime.go', 'func NormalizeEventEnvelope', 'shared event envelope normalization');
requireText('corelib/agentservice/run_lifecycle_store.go', 'RunAdmissionEventStore', 'atomic run admission outbox seam');
requireText('corelib/agentservice/run_lifecycle_store.go', 'RunCompletionEventStore', 'atomic run completion outbox seam');
requireText('corelib/agentservice/run_lifecycle_store.go', 'RunTerminalEventStore', 'atomic terminal outbox seam');
requireText('corelib/agentservice/run_lifecycle_store.go', 'RunLifecycleTransactionStore', 'generic custom-store lifecycle transaction seam');
requireText('corelib/agentservice/service.go', 'RequireAtomicLifecycle', 'strict lifecycle persistence option');
requireText('MaClawSrv/main.go', 'RequireAtomicLifecycle:', 'headless host strict lifecycle persistence');
requireText('corelib/agentservice/service_events.go', 'CommitRunLifecycle', 'Service generic lifecycle transaction dispatch');
requireText('corelib/agentruntime/runtime.go', 'ToolsWithExecutability', 'single-pass Runtime tool aggregation');
requireText('corelib/agentruntime/runtime.go', 'PromptDigest', 'capability prompt digest contract');
requireText('corelib/agentruntime/runtime.go', 'SurfaceDigest', 'capability surface parity digest contract');
requireText('corelib/agentruntime/runtime.go', 'DigestCapabilitySurface', 'canonical capability surface digest helper');
requireText('corelib/agentservice/semantic_behavior_snapshot.go', 'func SnapshotSemanticNeedFamilies', 'shared GUI/srv need-family behavior snapshot');
requireText('corelib/agentservice/semantic_behavior_snapshot.go', 'func SnapshotSemanticPlanSurface', 'shared GUI/srv plan and first-wave surface snapshot');
requireText('corelib/agentservice/semantic_behavior_snapshot.go', 'func SemanticBehaviorSnapshotCases', 'shared frozen snapshot cohort');
requireText('corelib/agentservice/semantic_behavior_snapshot.go', 'func EncodeSemanticPlanSurfaceSnapshot', 'shared plan/surface snapshot encoder');
requireText('corelib/agentservice/semantic_behavior_snapshot.go', 'func DiffSnapshotBytes', 'shared snapshot drift reporter');
requireText('corelib/agentservice/semantic_behavior_snapshot.go', 'func SyncSnapshotFile', 'shared snapshot golden sync');
requireText('corelib/agentservice/semantic_behavior_snapshot.go', 'func SemanticBehaviorSnapshotUpdateRequested', 'shared snapshot UPDATE env');
requireText('corelib/agentservice/semantic_behavior_snapshot.go', 'no overlapping first-wave cases compared', 'first-wave parity fails closed on a vacuous peer');
requireText('corelib/agentservice/semantic_behavior_snapshot.go', 'func PlanSurfaceFirstWaveIdentities', 'adapter-stripped first-wave identity');
requireText('corelib/agentservice/semantic_behavior_snapshot.go', 'func PlanSurfaceFirstWaveParityErrors', 'cross-host first-wave identity parity');
requireText('corelib/agentservice/semantic_behavior_snapshot.go', 'func CheckPlanSurfaceFirstWaveParity', 'peer golden first-wave identity check');
requireText('corelib/agentservice/semantic_behavior_snapshot_test.go', 'func TestGUIAndSrvSemanticPlanSurfaceSnapshot', 'srv plan/surface snapshot contract');
requireText('corelib/agentservice/semantic_behavior_snapshot_test.go', 'SyncSnapshotFile', 'srv snapshot uses shared golden sync');
requireText('corelib/agentservice/semantic_behavior_snapshot_test.go', 'CheckPlanSurfaceFirstWaveParity', 'srv first-wave identity vs GUI golden');
requireText('corelib/agentservice/testdata/semantic_plan_surface_snapshot.txt', 'srv\tsearch\tfirst', 'frozen srv search first-wave surface');
requireText('corelib/agentservice/testdata/semantic_plan_surface_snapshot.txt', 'information.search.web|freshness=reference', 'plan/surface snapshot includes search FitProof qualifiers');
requireText('guiapp/semantic_behavior_snapshot_test.go', 'func TestGUIAndSrvSemanticPlanSurfaceSnapshot', 'GUI plan/surface snapshot contract');
requireText('guiapp/semantic_behavior_snapshot_test.go', 'SyncSnapshotFile', 'GUI snapshot uses shared golden sync');
requireText('guiapp/semantic_behavior_snapshot_test.go', 'CheckPlanSurfaceFirstWaveParity', 'GUI first-wave identity vs srv golden');
requireText('guiapp/testdata/semantic_plan_surface_snapshot.txt', 'im\tsearch\tfirst', 'frozen GUI search first-wave surface');
requireText('guiapp/testdata/semantic_plan_surface_snapshot.txt', 'information.search.web|freshness=reference', 'GUI plan/surface snapshot includes search FitProof qualifiers');
requireText('corelib/agentservice/semantic_behavior_snapshot_test.go', 'func TestGUIAndSrvSemanticBehaviorSnapshot', 'GUI vs srv semantic behavior snapshot contract');
requireText('corelib/agentservice/testdata/semantic_behavior_snapshot.txt', 'im\tsearch\tmanaged', 'frozen IM search face in behavior snapshot');
requireText('corelib/agentservice/testdata/semantic_behavior_snapshot.txt', 'srv\tsearch\tmanaged', 'frozen srv search face in behavior snapshot');
requireText('corelib/agentservice/reviewed_dynamic_capabilities.go', 'MaxInvocations: 5', 'reviewed search/fetch share IM repeat budget');
requireText('corelib/agentservice/intent_capability_rules_test.go', 'func TestIMAndReviewedIntentRulesShareRepeatBudgets', 'IM vs reviewed MaxInvocations stay aligned');
requireText('guiapp/semantic_behavior_snapshot_test.go', 'func TestGUIAndSrvSemanticBehaviorSnapshot', 'GUI host IM catalog vs reviewed required-identity snapshot');
requireText('guiapp/semantic_behavior_snapshot_test.go', 'managed drifted', 'GUI need-family snapshot compares overlapping Managed flags');
requireText('.github/workflows/agent-architecture.yml', "go test ./guiapp -run 'Test(SharedAgentLoopCallbacksEmitRuntimeEvents|RunAgentLoopSharedDelegatesThroughRuntimeContract|GUIRuntimeDescribesDeterministicToolSurface|GUIAndSrv)'", 'PR architecture job runs GUI/srv behavior snapshot');
requireText('.github/workflows/main.yml', "go test ./guiapp -run 'Test(SharedAgentLoopCallbacksEmitRuntimeEvents|RunAgentLoopSharedDelegatesThroughRuntimeContract|GUIRuntimeDescribesDeterministicToolSurface|GUIAndSrv)'", 'release architecture job runs GUI/srv behavior snapshot');
requireText('MaClawSrv/http_routes_user.go', 'runtime-capabilities', 'shared runtime capability endpoint');
requireText('MaClawSrv/http.go', 's.jobs.close()', 'async job shutdown ownership');
requireText('MaClawSrv/http_agent.go', 'Last-Event-ID', 'SSE resume contract');
requireFile('MaClawSrv/transport/http/sse.go');
requireText('MaClawSrv/http_agent.go', 'transporthttp.ParseLastEventID', 'HTTP transport SSE parser adapter');
requireFile('MaClawSrv/transport/http/request.go');
requireText('MaClawSrv/http_agent.go', 'transporthttp.WantsAsyncResponse', 'HTTP transport async preference adapter');
requireText('MaClawSrv/http.go', 'transporthttp.ParsePageQuery', 'HTTP transport pagination parser adapter');
requireText('hubcenter/internal/httpapi/correlation.go', 'parseTraceParent', 'HubCenter inbound traceparent parser');
requireText('hubcenter/internal/httpapi/router.go', 'withInboundTraceParent', 'HubCenter traceparent middleware wiring');
requireText('MaClawSrv/openapi.go', 'runtime-capabilities', 'OpenAPI runtime capability endpoint');
requireText('docs/maclaw-srv-gui-architecture-review.zh-CN.md', 'Config.RuntimeModules', 'documented module composition root');
requireText('corelib/agentservice/config.go', 'coreconfig.IsSharedClientField', 'central AppConfig shared-field schema');
requireText('corelib/agentservice/errors.go', 'func ErrorCode', 'shared transport-neutral error code mapping');
requireText('MaClawSrv/http.go', 'agentservice.ErrorCode(err)', 'HTTP error response code contract');
requireText('MaClawSrv/http.go', 'coreconfig.IsUserWebVisibleField', 'central user-web config visibility schema');
requireText('MaClawSrv/http.go', 'coreconfig.CopyAppConfigFields', 'central AppConfig projection helper');
requireText('guiapp/app_user_data_migration.go', 'coreconfig.JSONFieldName', 'central AppConfig JSON tag parser for migration');
requireText('guiapp/config_settings_tab.go', 'coreconfig.JSONFieldName', 'central AppConfig JSON tag parser for settings DTO');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.DisplayReasoning', 'shared reasoning projection adapter');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.RewriteExpiredSemanticGrantNames', 'shared semantic history projection adapter');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.AttachmentsWithinRuntimeLimit', 'shared attachment admission adapter');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.UserFacingText', 'shared visible text projection adapter');
requireText('corelib/agentservice/service_messaging.go', 'agentruntime.UserFacingText', 'service visible text projection');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.ParameterRejectionGuidance', 'shared parameter rejection guidance adapter');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.SelectionOutcomeUnknown', 'shared semantic outcome adapter');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.SelectionFailed', 'shared semantic failure adapter');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.GrantRejectMessage', 'shared semantic rejection adapter');
requireText('corelib/agentruntime/semantic_outcomes.go', 'func PetitionGrantedMessage', 'shared semantic petition guidance');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.PetitionGrantedMessage', 'GUI semantic petition guidance adapter');
requireText('corelib/agentruntime/semantic_outcomes.go', 'func AdvanceAfterSuccess', 'shared semantic success projection');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.AdvanceAfterSuccess', 'GUI semantic success projection adapter');
requireText('corelib/agentruntime/semantic_projections.go', 'func DocumentReadResultProjection', 'shared document result projection');
requireText('corelib/agentruntime/semantic_projections.go', 'func DeliveryInvocationArgs', 'shared delivery argument projection');
requireText('corelib/agentruntime/semantic_pdf.go', 'func NormalizePDFInvocationArgs', 'shared PDF invocation normalization');
requireText('corelib/agentruntime/semantic_pdf.go', 'func PDFArgsTooThin', 'shared PDF argument admission');
requireText('corelib/agentruntime/semantic_pdf_text.go', 'func StripDeferredPDFPromise', 'shared PDF visible-text projection');
requireText('corelib/agentruntime/artifact_outcomes.go', 'func ResponseHasPDF', 'shared artifact type projection');
requireText('corelib/agentruntime/artifact_outcomes.go', 'func KeepVisibleErrorAfterArtifactAttach', 'shared artifact error projection');
requireText('corelib/agentruntime/collections.go', 'func AppendUniqueStrings', 'shared stable string projection');
requireText('corelib/agentruntime/collections.go', 'func FilterToolDefinitionsByName', 'shared tool definition filtering');
requireText('corelib/agentruntime/semantic_pdf.go', 'func HostOwnedPDFReportTitle', 'shared PDF title projection');
requireText('corelib/agentruntime/semantic_pdf.go', 'func SubstantialPDFReportText', 'shared PDF body admission');
requireText('corelib/agentruntime/semantic_evidence.go', 'func TrustedLookupEvidence', 'shared evidence trust projection');
requireText('corelib/agentruntime/tool_result.go', 'func SemanticToolExecutionResult', 'shared semantic tool outcome projection');
requireText('corelib/agentruntime/tool_text_result.go', 'func ToolTextResult', 'shared legacy text tool outcome projection');
requireText('corelib/tool/semantic_effects.go', 'func SelectionRequiresReceipt', 'shared semantic receipt policy');
requireText('corelib/tool/semantic_effects.go', 'func HostObservedExternalSelection', 'shared host receipt policy');
requireText('corelib/tool/semantic_effects.go', 'func PlanWithSelections', 'shared immutable plan filtering');
requireText('corelib/tool/semantic_effects.go', 'func PlanSelectionByID', 'shared plan selection lookup');
requireText('corelib/tool/semantic_effects.go', 'func IsLookupSelection', 'shared lookup selection projection');
requireText('corelib/tool/semantic_plan_executor.go', 'func ExecutionConsumesModelGrant', 'shared grant consumption policy');
requireText('corelib/tool/semantic_plan_executor.go', 'func SelectionExecutionUnsettled', 'shared unsettled selection policy');
requireText('corelib/tool/semantic_repeat.go', 'func RepeatFamilySpentBudgetNote', 'shared spent-budget notice');
requireText('corelib/agentservice/intent_capability_rules.go', 'func IMSemanticIntentCapabilityNeedRules', 'shared IM semantic intent rules');
requireText('corelib/agentservice/intent_capability_rules.go', 'func ReviewedCodingCapabilityNeedRule', 'shared coding capability rule');
requireText('guiapp/semantic_tool_routing.go', 'agentservice.IMSemanticIntentCapabilityNeedRules', 'GUI intent rules adapter');
requireText('guiapp/semantic_tool_routing.go', 'agentservice.ReviewedCodingCapabilityNeedRule', 'GUI coding rule adapter');
requireText('corelib/agentservice/semantic_archetype.go', 'func ExpandArchetypeBundleNeeds', 'shared archetype bundle expansion');
requireText('guiapp/semantic_tool_routing.go', 'agentservice.ExpandArchetypeBundleNeeds', 'GUI archetype bundle adapter');
requireText('corelib/agentruntime/prompt_sections.go', 'const SemanticGrantPromptFence', 'shared full semantic grant fence');
requireText('guiapp/semantic_tool_routing.go', 'agentruntime.EnsureSemanticGrantPromptFence', 'GUI full grant fence adapter');
requireText('corelib/agentservice/core_agent_executor.go', 'agentruntime.EnsureSemanticGrantPromptFence', 'headless full grant fence');
requireText('MaClawSrv/dynamic_semantic_routing.go', 'ArchetypeBundles:  true', 'headless archetype bundle expansion');
requireText('guiapp/semantic_tool_routing.go', 'tool.ExecutionConsumesModelGrant', 'GUI grant consumption adapter');
requireText('guiapp/semantic_tool_routing.go', 'tool.SelectionUnsettled', 'GUI unsettled selection adapter');
requireText('guiapp/semantic_tool_routing.go', 'tool.SpentBudgetNoteForGrants', 'GUI spent-budget adapter');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.RetireConsumedLiveGrants', 'headless recovery retires consumed grants');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.SelectionUnsettled', 'headless unsettled selection adapter');
requireText('corelib/tool/semantic_repeat.go', 'func ApplySpentBudgetNote', 'shared spent-budget result stamp');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.ApplySpentBudgetNote', 'headless spent-budget adapter');
requireText('corelib/agentruntime/desktop_capture.go', 'func ParseDesktopDisplayIndex', 'shared screenshot display parser');
requireText('corelib/agentruntime/builtin.go', 'ParseDesktopDisplayIndex', 'builtin screenshot uses shared display parser');
requireText('guiapp/im_tools_screenshot_args.go', 'agentruntime.ParseDesktopDisplayIndex', 'GUI screenshot display adapter');
requireText('corelib/agent/tool_register_core.go', 'func CoreToolJSONSchema', 'shared RegisterCoreTools schema lookup');
requireText('corelib/agent/tool_register_core.go', 'func OverlayCoreToolSchema', 'hosts overlay core schemas');
requireText('corelib/agentservice/core_agent_executor.go', 'specFromCoreTool', 'headless catalog consumes shared core schemas');
requireText('guiapp/im_tool_definitions.go', 'toolDefFromCore', 'GUI catalog consumes shared core schemas');
requireText('guiapp/tool_registry_builtin.go', 'overlayCoreToolSchema', 'GUI registry consumes shared core schemas');
requireText('corelib/agent/tool_register_core.go', 'Name:        "edit_lines"', 'edit_lines lives in RegisterCoreTools');
requireText('corelib/agent/tool_register_core.go', 'Name:        "tts_render"', 'tts_render lives in RegisterCoreTools');
requireText('corelib/agent/tool_register_core.go', 'Name:        "delegate_task"', 'delegate_task lives in RegisterCoreTools');
requireText('corelib/agentservice/shared_tool_surface.go', 'specFromCoreTool("office"', 'headless office consumes shared core schema');
requireText('corelib/agentservice/shared_tool_surface.go', 'specFromCoreTool("edit_lines"', 'headless edit_lines consumes shared core schema');
requireText('guiapp/tool_registry_builtin.go', 'overlayCoreToolSchema("office"', 'GUI office consumes shared core schema');
requireText('guiapp/im_tool_definitions.go', 'toolDefFromCore("delegate_task"', 'GUI delegate_task consumes shared core schema');
requireText('corelib/tool/semantic_invocation.go', 'DefaultInvocationGrantTTL', 'shared invocation grant TTL');
requireText('corelib/tool/semantic_surface_host.go', 'func PublishCurrentSurface', 'shared surface publish adapter');
requireText('corelib/tool/semantic_surface_host.go', 'func IssueReadySurface', 'shared grant materialization adapter');
requireText('corelib/tool/semantic_surface_host.go', 'func IssueAndBindReadySurface', 'shared issue-and-bind grant adapter');
requireText('corelib/tool/semantic_surface_host.go', 'func BindIssuedGrants', 'shared grant table bind');
requireText('corelib/tool/semantic_surface_host.go', 'func UnrenderedReadyGrants', 'shared incremental unrendered grant filter');
requireText('corelib/tool/semantic_surface_host.go', 'func RenderVisibleReadyDefinitions', 'shared visible-ready renderer');
requireText('corelib/tool/semantic_surface_host.go', 'func MaterializeReadySurface', 'shared ready-surface materialize pass');
requireText('corelib/tool/semantic_plan_executor.go', 'func SelectionUnsettled', 'shared unsettled execution lookup');
requireText('corelib/tool/semantic_surface_host.go', 'func ProjectRenderedDefinitions', 'shared rendered-definition projection');
requireText('corelib/tool/semantic_planner.go', 'func NewPlanningBudget', 'shared planning budget constructor');
requireText('corelib/tool/semantic_replan.go', 'func QualifiersEqual', 'shared qualifier equality');
requireText('corelib/tool/semantic_replan.go', 'func EffectsEqual', 'shared effect equality');
requireText('corelib/tool/semantic_replan.go', 'func ArtifactContractsEqual', 'shared artifact-contract equality');
requireText('corelib/tool/semantic_replan.go', 'func SelectionAuthorityEqualIgnoringProvider', 'shared binding-replacement authority');
requireText('corelib/tool/semantic_surface_host.go', 'func MergeCompletedSelections', 'shared completed-selection merge');
requireText('corelib/tool/semantic_surface_host.go', 'func LoadCompletedSelections', 'shared completed-selection load');
requireText('corelib/tool/semantic_surface_host.go', 'func PrepareReadySurfaceCompletion', 'shared ready-surface completion+facts prepare');
requireText('corelib/tool/semantic_surface_host.go', 'func ApplyReadySurfaceCompletion', 'shared ready-surface completion apply');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'Executor:', 'headless Definitions prepares completion through materialize');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'TrustedFacts:', 'headless Definitions supplies trusted facts to materialize');
requireText('guiapp/semantic_tool_routing.go', 'Executor:', 'GUI refresh prepares completion through materialize');
requireText('corelib/tool/semantic_surface_host.go', 'func PlaceMaterializedGrant', 'shared recovered grant placement');
requireText('corelib/tool/semantic_surface_host.go', 'func RetireLiveGrant', 'shared live grant retirement');
requireText('corelib/tool/semantic_surface_host.go', 'func RetireConsumedGrant', 'shared consumed-grant fail-closed retirement');
requireText('guiapp/semantic_tool_routing.go', 'tool.LoadCompletedSelections', 'GUI uses shared completed-selection load');
requireText('guiapp/semantic_tool_routing.go', 'tool.PlaceMaterializedGrant', 'GUI uses shared recovered grant placement');
requireText('corelib/tool/semantic_surface_host.go', 'func RetireConsumedLiveGrants', 'shared recovery consumed-grant scan');
requireText('guiapp/semantic_tool_routing.go', 'tool.RetireConsumedLiveGrants', 'GUI recovery retires consumed grants fail-closed');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.LoadCompletedSelections', 'headless uses shared completed-selection load');
requireText('corelib/agentservice/sqlite_job_repository.go', 'func OpenGUIRuntimeJobStores', 'GUI durable job store opener');
requireText('guiapp/skill_runner.go', 'UseDurableRuntimeStores', 'GUI skill runner can persist runtime jobs');
requireText('guiapp/task_orchestrator.go', 'UseDurableRuntimeStores', 'GUI orchestrator can persist runtime jobs');
requireText('corelib/tool/semantic_repeat.go', 'func GrantSelectionIDs', 'shared grant selection identity projection');
requireText('corelib/tool/semantic_repeat.go', 'func NextExposedSelections', 'shared next-exposed selection closure');
requireText('corelib/tool/semantic_repeat.go', 'func LiveGrantNames', 'shared live grant name projection');
requireText('corelib/tool/semantic_repeat.go', 'func SpentBudgetNoteForGrants', 'shared spent-budget note from grant tables');
requireText('guiapp/semantic_tool_routing.go', 'tool.LiveGrantNames', 'GUI uses shared live grant names');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.RetireConsumedGrant', 'headless retire uses shared consumed-grant move');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.HasKnownGrant', 'headless uses shared known-grant lookup');
requireText('corelib/tool/semantic_legacy_gateway.go', 'func ClosedManagedDefinitions', 'shared closed managed surface filter');
requireText('corelib/tool/semantic_legacy_gateway.go', 'func ClosedManagedDefinitionsForProfile', 'shared closed managed surface with light filter');
requireText('corelib/tool/semantic_legacy_gateway.go', 'func GrantSelectionIsLightPromptSafe', 'shared light-safe grant selection probe');
requireText('guiapp/semantic_tool_routing.go', 'tool.ClosedManagedDefinitionsForProfile', 'GUI turn close uses shared light-safe managed filter');
requireText('guiapp/im_agent_loop_shared.go', 'tool.GrantSelectionIsLightPromptSafe', 'GUI light authorizer uses shared grant selection probe');
requireText('corelib/agentservice/core_agent_executor.go', 'coretool.ClosedManagedDefinitionsForProfile', 'headless BuildTools uses shared light-safe managed filter');
requireText('corelib/agentservice/core_agent_executor.go', 'coretool.GrantSelectionIsLightPromptSafe', 'headless light authorizer uses shared grant selection probe');
requireText('corelib/tool/semantic_surface_host.go', 'func UnionSatisfiedIDs', 'shared satisfied-id union');
requireText('corelib/tool/semantic_surface_host.go', 'func VisibleReadyGrants', 'shared visible ready grant projection');
requireText('guiapp/semantic_tool_routing.go', 'tool.ClosedManagedDefinitions', 'GUI uses shared closed managed filter');
requireText('guiapp/semantic_tool_routing.go', 'tool.RenderVisibleReadyDefinitions', 'GUI uses shared visible grant projection');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.ClosedManagedDefinitions', 'headless uses shared closed managed filter');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.MaterializeReadySurface', 'headless uses shared visible grant projection');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.UnionSatisfiedIDs', 'headless uses shared satisfied-id union');
requireText('corelib/tool/semantic_plan_executor.go', 'func PlanExecutionStateFromResult', 'shared provider result to plan-execution state');
requireText('corelib/tool/semantic_plan_executor.go', 'func ReplayedSelectionResult', 'shared host-call replay from execution store');
requireText('corelib/tool/semantic_plan_executor.go', 'func RecordedSelectionResultFallback', 'shared recorded-result text fallback');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.PlanExecutionStateFromResult', 'headless complete uses shared execution-state projection');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.ReplayedSelectionResult', 'headless replay uses shared execution-row verdict');
requireText('guiapp/coding_durable_dynamic_surface.go', 'tool.PlanExecutionStateFromResult', 'GUI coding complete uses shared execution-state projection');
requireText('guiapp/im_agent_loop_shared.go', 'tool.PlanExecutionStateFromResult', 'GUI IM complete uses shared execution-state projection');
requireText('corelib/tool/semantic_host_call_journal.go', 'func HostCallAcquireTerminal', 'shared host-call acquire terminal interpretation');
requireText('corelib/tool/semantic_host_call_journal.go', 'func HostCallReplayResult', 'shared host-call replay with conflict fallback');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.HostCallAcquireTerminal', 'headless acquire uses shared host-call terminal');
requireText('guiapp/coding_durable_dynamic_surface.go', 'tool.HostCallAcquireTerminal', 'GUI coding acquire uses shared host-call terminal');
requireText('guiapp/im_agent_loop_shared.go', 'tool.HostCallAcquireTerminal', 'GUI IM acquire uses shared host-call terminal');
requireText('corelib/tool/semantic_planner.go', 'func AliasableLookupSelection', 'shared aliasable web-search selection');
requireText('corelib/tool/semantic_planner.go', 'func SoleLiveLookupGrantName', 'shared sole live lookup grant name');
requireText('guiapp/semantic_tool_routing.go', 'tool.SoleLiveLookupGrantName', 'GUI lookup denial uses shared sole live lookup grant');
requireText('corelib/tool/semantic_planner.go', 'func GrantedNeedsFromPlan', 'shared plan-to-granted-needs projection');
requireText('corelib/tool/semantic_planner.go', 'func CloneCapabilityNeeds', 'shared capability-need clone');
requireText('guiapp/semantic_governed_task.go', 'tool.GrantedNeedsFromPlan', 'GUI session-governed persist uses shared granted needs');
requireText('guiapp/semantic_governed_task.go', 'tool.CloneCapabilityNeeds', 'GUI session-governed persist uses shared need clone');
requireText('corelib/agentservice/dynamic_session_governed_task.go', 'coretool.GrantedNeedsFromPlan', 'headless session-governed persist uses shared granted needs');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.CloneCapabilityNeeds', 'headless need clone uses shared helper');
requireText('corelib/tool/semantic_continuity_projection.go', 'GrantedNeedFromSelection', 'continuity open needs use shared granted-need projection');
requireText('corelib/intent/types.go', 'func (r ClassificationResult) IsGenericContinuationPrimary()', 'shared generic-continuation primary');
requireText('corelib/intent/types.go', 'func (r ClassificationResult) HasLabel(', 'shared classification label membership');
requireText('guiapp/semantic_tool_routing.go', 'return result.HasLabel(label)', 'GUI IM label membership uses shared HasLabel');
requireText('corelib/agentservice/semantic_archetype.go', 'return result.HasLabel(label)', 'headless archetype bundle key uses shared HasLabel');
requireText('guiapp/semantic_governed_task.go', 'IsGenericContinuationPrimary', 'GUI session-governed replay uses shared continuation primary');
requireText('corelib/agentservice/dynamic_session_governed_task.go', 'IsGenericContinuationPrimary', 'headless session-governed replay uses shared continuation primary');
requireText('corelib/tool/semantic_planner.go', 'func CloneNeedQualifiers', 'shared qualifier map clone');
requireText('corelib/tool/semantic_planner.go', 'func NeedQualifierKey', 'shared qualifier identity key');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.NeedQualifierKey', 'headless need sibling key uses shared qualifier identity');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.CloneNeedQualifiers', 'headless qualifier clone uses shared helper');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'func ExpandNeedTemplateSiblings', 'shared reviewed-template sibling expansion');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'func NeedTemplateIdentityKey', 'shared need-template sibling identity');
requireText('guiapp/coding_dynamic_catalog.go', 'agentservice.ExpandNeedTemplates', 'GUI coding policy expands templates through shared sibling helper');
requireText('corelib/agentservice/semantic_archetype.go', 'ExpandNeedTemplateSiblings', 'archetype new families expand through shared sibling helper');
requireText('guiapp/expert_capability_policy.go', 'tool.CloneNeedQualifiers', 'GUI expert policy qualifier clone uses shared helper');
requireText('corelib/tool/semantic_planner.go', 'func FilterGrantedNeedsStillCovered', 'shared granted-need coverage filter');
requireText('corelib/tool/semantic_planner.go', 'func CloneRoutingFacts', 'shared routing-fact clone');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'func CoveredCapabilitiesFromNeedTemplates', 'shared intent-rule coverage projection');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'func IntentRuleCoverageFromClassification', 'shared intent-label managed/unmapped scan');
requireText('guiapp/semantic_tool_routing.go', 'agentservice.IntentRuleCoverageFromClassification', 'GUI IM coverage gate uses shared rule scan');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coverage := IntentRuleCoverageFromClassification', 'headless resolver fail-closed uses shared rule scan');
requireText('corelib/tool/semantic_replan.go', 'func SelectionsByNeed', 'shared plan selection-by-need identity');
requireText('corelib/tool/semantic_replan.go', 'SelectionsByNeed(parent.Selections)', 'replan subset uses shared selection-by-need identity');
requireText('corelib/agentservice/semantic_petition.go', 'func ValidatePetitionExpansion', 'shared petition expansion validator');
requireText('guiapp/semantic_tool_routing.go', 'agentservice.ValidatePetitionExpansion', 'GUI petition expansion uses shared validator');
requireText('corelib/agentservice/semantic_petition.go', 'func PetitionLabelForCapability', 'shared capability-to-petition-label reverse mapping');
requireText('guiapp/semantic_tool_routing.go', 'agentservice.PetitionLabelForCapability', 'GUI petition label resolution uses shared reverse mapping');
requireText('corelib/agentservice/dynamic_session_governed_task.go', 'func ClassificationFromGrantedNeeds', 'shared granted-need to UIC reverse mapping');
requireText('guiapp/semantic_governed_task.go', 'agentservice.ClassificationFromGrantedNeeds', 'GUI session-governed replay reconstructs labels through shared reverse mapping');
requireText('corelib/tool/semantic_replan.go', 'func ReplanTurnID', 'shared replan child turn identity');
requireText('corelib/tool/semantic_replan.go', 'func PetitionTurnID', 'shared petition child turn identity');
requireText('guiapp/semantic_tool_routing.go', 'tool.ReplanTurnID', 'GUI replan child turn uses shared identity');
requireText('guiapp/semantic_tool_routing.go', 'tool.PetitionTurnID', 'GUI petition child turn uses shared identity');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.ReplanTurnID', 'headless replan child turn uses shared identity');
requireText('corelib/tool/trusted_input.go', 'func TrustedAttachmentSourceID', 'shared trusted attachment source identity');
requireText('corelib/tool/trusted_input.go', 'func TrustedInputPlanID', 'shared trusted input plan identity');
requireText('guiapp/semantic_tool_routing.go', 'tool.TrustedAttachmentSourceID', 'GUI document ingress uses shared attachment source identity');
requireText('guiapp/semantic_audio_transcribe.go', 'tool.TrustedAttachmentSourceID', 'GUI audio ingress uses shared attachment source identity');
requireText('corelib/agentservice/dynamic_host_docread.go', 'coretool.TrustedAttachmentSourceID', 'headless document ingress uses shared attachment source identity');
requireText('corelib/agentservice/dynamic_host_imageinput.go', 'coretool.TrustedAttachmentSourceID', 'headless image ingress uses shared attachment source identity');
requireText('corelib/agentservice/dynamic_host_voiceinput.go', 'coretool.TrustedAttachmentSourceID', 'headless voice ingress uses shared attachment source identity');
requireText('corelib/tool/trusted_input.go', 'func UniqueTrustedInputCount', 'shared trusted input uniqueness gate');
requireText('guiapp/semantic_tool_routing.go', 'tool.UniqueTrustedInputCount', 'GUI document binding uses shared uniqueness gate');
requireText('corelib/agentservice/dynamic_host_docread.go', 'coretool.UniqueTrustedInputCount', 'headless document binding uses shared uniqueness gate');
requireText('corelib/agentservice/dynamic_host_imageinput.go', 'coretool.UniqueTrustedInputCount', 'headless image deliver uses shared uniqueness gate');
requireText('corelib/agentservice/dynamic_host_voiceinput.go', 'coretool.UniqueTrustedInputCount', 'headless voice deliver uses shared uniqueness gate');
requireText('guiapp/semantic_tool_routing.go', 'tool.IsTrustedInputMissingOrAmbiguous', 'GUI plan-error classifier uses shared uniqueness errors');
requireText('corelib/tool/semantic_planner.go', 'func CapabilityNeedsContain', 'shared capability-need membership scan');
requireText('corelib/tool/semantic_repeat.go', 'func ExtendRepeatFamily', 'shared repeat-family ceiling extension');
requireText('corelib/agentservice/semantic_archetype.go', 'coretool.ExtendRepeatFamily', 'archetype existing-family upgrade uses shared ceiling extension');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.ExtendRepeatFamily', 'need-template siblings use shared ceiling extension');
requireText('corelib/tool/semantic_planner.go', 'func InTurnArtifactProducerPresent', 'shared in-turn artifact producer predicate');
requireText('guiapp/semantic_tool_routing.go', 'tool.InTurnArtifactProducerPresent', 'GUI file-deliver skip uses shared in-turn producer predicate');
requireText('corelib/agentservice/dynamic_host_imageinput.go', 'coretool.InTurnArtifactProducerPresent', 'headless attachment skip uses shared in-turn producer predicate');
requireText('guiapp/semantic_audio_transcribe.go', 'tool.CapabilityNeedsContain', 'GUI audio transcribe uses shared need membership');
requireText('guiapp/semantic_active_document.go', 'tool.IsDocumentRead', 'GUI document-read reuse uses shared document-read predicate');
requireText('corelib/agentservice/dynamic_host_docgenerate.go', 'coretool.CapabilityNeedsContain', 'headless generate NeedPresent uses shared membership');
requireText('corelib/agentservice/dynamic_host_audiorender.go', 'coretool.CapabilityNeedsContain', 'headless audio-render NeedPresent uses shared membership');
requireText('corelib/agentservice/dynamic_host_visualcapture.go', 'coretool.CapabilityNeedsContain', 'headless visual-capture NeedPresent uses shared membership');
requireText('corelib/agentservice/dynamic_host_audiotranscribe.go', 'coretool.CapabilityNeedsContain', 'headless audio transcribe NeedPresent uses shared membership');
requireText('corelib/tool/semantic_artifact_projection.go', 'func CurrentChannelDeliverAccepts', 'shared current-channel deliver format gate');
requireText('guiapp/semantic_tool_routing.go', 'tool.CurrentChannelDeliverAccepts', 'GUI file-deliver binding uses shared current-channel format gate');
requireText('corelib/agentservice/dynamic_host_docread.go', 'coretool.CurrentChannelDeliverAccepts', 'headless attachment deliver uses shared current-channel format gate');
requireText('corelib/tool/semantic_planner.go', 'func IsDocumentRead', 'shared document-read capability predicate');
requireText('corelib/tool/semantic_planner.go', 'func BindDocumentReadFormat', 'shared document-read format bind');
requireText('guiapp/semantic_tool_routing.go', 'tool.BindDocumentReadFormat', 'GUI trusted-document binding uses shared format stamp');
requireText('corelib/agentservice/dynamic_host_docread.go', 'coretool.BindDocumentReadFormat', 'headless document-read binding uses shared format stamp');
requireText('corelib/agentservice/dynamic_host_docread.go', 'coretool.IsDocumentRead', 'headless document NeedPresent uses shared predicate');
requireText('guiapp/semantic_governed_task.go', 'tool.FilterGrantedNeedsStillCovered', 'GUI session-governed replay uses shared coverage filter');
requireText('corelib/agentservice/dynamic_session_governed_task.go', 'coretool.FilterGrantedNeedsStillCovered', 'headless session-governed replay uses shared coverage filter');
requireText('guiapp/coding_dynamic_catalog.go', 'tool.CloneRoutingFacts', 'GUI coding plan copies facts through shared clone');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.CloneRoutingFact', 'headless policy facts use shared clone');
requireText('corelib/tool/semantic_effects.go', 'func CapabilityNeedHasSideEffect', 'shared session-governed side-effect predicate');
requireText('corelib/tool/semantic_effects.go', 'func CapabilityNeedsHaveSideEffect', 'shared session-governed side-effect set');
requireText('guiapp/semantic_governed_task.go', 'tool.CapabilityNeedHasSideEffect', 'GUI session-governed replay uses shared side-effect predicate');
requireText('corelib/agentservice/dynamic_session_governed_task.go', 'coretool.CapabilityNeedHasSideEffect', 'headless session-governed replay uses shared side-effect predicate');
requireText('corelib/agentruntime/memory_job_effect_repository.go', 'type MemoryJobEffectRepository struct', 'in-process job effect repository');
requireText('guiapp/skill_runner.go', 'func (r *SkillRunner) ReconcileJob', 'GUI skill runs expose a JobReconciler');
requireText('guiapp/task_orchestrator.go', 'func (o *TaskOrchestrator2) ReconcileJob', 'GUI orchestration plans expose a JobReconciler');
requireText('corelib/agentruntime/job_effect.go', 'func ReconcileOpenJobs', 'shared open-job reconciliation scan');
requireText('corelib/agentruntime/job.go', 'func JobStatusIsTerminal', 'shared terminal job status helper');
requireText('corelib/agentruntime/job_effect.go', 'func RecoverAndReconcileOpenJobs', 'shared crash-recovery then reconcile entry');
requireText('guiapp/skill_runner.go', 'agentruntime.RecoverAndReconcileOpenJobs', 'GUI skill runner recovers expired leases then reconciles');
requireText('guiapp/task_orchestrator.go', 'agentruntime.RecoverAndReconcileOpenJobs', 'GUI orchestrator recovers expired leases then reconciles');
requireText('corelib/agentruntime/job_effect.go', 'func ReconcileFromProtectedEffects', 'shared committed/failed effect reconciliation');
requireText('guiapp/skill_runner.go', 'agentruntime.ReconcileFromProtectedEffects', 'GUI skill runs settle from protected effects');
requireText('guiapp/task_orchestrator.go', 'agentruntime.ReconcileFromProtectedEffects', 'GUI orchestrator settles from protected effects');
requireText('MaClawSrv/domain_reconcilers.go', 'agentruntime.ReconcileFromProtectedEffects', 'srv domain reconcilers settle from protected effects');
requireText('MaClawSrv/domain_reconcilers.go', 'agentruntime.ParseJobEffectResourceIDs', 'srv uses shared effect resource-id parser');
requireText('guiapp/semantic_tool_routing.go', 'tool.SelectionUnsettled', 'GUI uses shared unsettled execution lookup');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.SelectionUnsettled', 'headless uses shared unsettled execution lookup');
requireText('guiapp/semantic_tool_routing.go', 'tool.EffectsEqual', 'GUI uses shared effect equality');

requireText('guiapp/semantic_tool_routing.go', 'Unsettled:', 'GUI materialize supplies unsettled lookup');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'Unsettled:', 'headless materialize supplies unsettled lookup');
requireText('corelib/agentruntime/job_lease.go', 'DefaultJobLeaseTTL', 'shared production job lease TTL');
requireText('corelib/agentruntime/job_lease.go', 'func JobLeaseDeadline', 'shared job lease deadline');
requireText('corelib/agentruntime/job_lease.go', 'func PrepareJobLeaseForPersist', 'shared persist-time lease stamp');
requireText('corelib/agentruntime/job_lease.go', 'func JobShouldRenewLease', 'shared local lease renewal predicate');
requireText('corelib/agentruntime/job_lease.go', 'func RecoverExpiredJobLease', 'shared expired-lease recovery policy');
requireText('corelib/agentruntime/job_lease.go', 'func RecoverExpiredJobs', 'shared expired-lease scan');
requireText('corelib/agentruntime/job_lease.go', 'func RecoverExpiredJobsInRepository', 'shared expired-lease repository persist');
requireText('corelib/agentruntime/job_lease.go', 'DefaultJobLeaseTick', 'shared job lease heartbeat interval');
requireText('corelib/agentruntime/job_lease.go', 'func ShouldRenewLocalJobLease', 'shared local lease heartbeat selection');
requireText('MaClawSrv/jobs.go', 'agentruntime.RecoverExpiredJobs', 'srv expired-lease scan uses shared helper');
requireText('MaClawSrv/jobs.go', 'agentruntime.JobLeaseExpired', 'srv lease expiry uses shared helper');
requireText('MaClawSrv/jobs.go', 'agentruntime.DefaultJobLeaseTTL', 'srv lease TTL uses shared default');
requireText('MaClawSrv/jobs.go', 'agentruntime.DefaultJobLeaseTick', 'srv lease tick uses shared default');
requireText('MaClawSrv/jobs.go', 'agentruntime.PrepareJobLeaseForPersist', 'srv persist stamps leases through shared helper');
requireText('MaClawSrv/jobs.go', 'agentruntime.ShouldRenewLocalJobLease', 'srv heartbeat renews local leases through shared predicate');
requireText('corelib/agentruntime/job_retention.go', 'func SelectJobsForRetentionPrune', 'shared job retention prune selector');
requireText('corelib/agentruntime/job_retention.go', 'func PruneRetainedJobsInRepository', 'shared job retention repository prune');
requireText('corelib/agentruntime/job_retention.go', 'DefaultJobRetention', 'shared job retention window');
requireText('corelib/agentruntime/job_effect.go', 'PruneRetainedJobsInRepository', 'start-of-process recovery prunes retained jobs');
requireText('MaClawSrv/jobs.go', 'agentruntime.SelectJobsForRetentionPrune', 'srv housekeeping prunes through shared selector');
requireText('MaClawSrv/jobs.go', 'agentruntime.DefaultJobRetention', 'srv retention window uses shared default');
requireText('guiapp/app_user_data_migration.go', 'agentruntime.DefaultJobRetention', 'GUI migration jobs use shared retention window');
requireText('corelib/tool/semantic_surface_host.go', 'func RetireLiveGrantsForSelection', 'shared retire-all-grants-for-selection');
requireText('guiapp/semantic_tool_routing.go', 'tool.RetireLiveGrantsForSelection', 'GUI complete/retire uses shared selection retire');
requireText('corelib/agentruntime/job_effect.go', 'func JobEffectMatchesJob', 'shared job/effect identity match');
requireText('corelib/agentruntime/job_effect.go', 'func ApplyResolvedJobReconcile', 'shared resolved-job settlement envelope');
requireText('MaClawSrv/jobs.go', 'agentruntime.ApplyResolvedJobReconcile', 'srv unknown-job CAS uses shared settlement envelope');
requireText('MaClawSrv/jobs.go', 'agentruntime.JobEffectMatchesJob', 'srv effect identity uses shared matcher');
requireText('corelib/agentruntime/job_store.go', 'func UpsertJob', 'shared JobRepository admit-or-update');
requireText('corelib/agentruntime/job_store.go', 'func IsJobRepositoryConcurrencyError', 'shared job repository CAS/not-found predicate');
requireText('MaClawSrv/jobs.go', 'agentruntime.IsJobRepositoryConcurrencyError', 'srv persist-fail-closed uses shared concurrency predicate');
requireText('guiapp/skill_runner.go', 'agentruntime.UpsertJob', 'GUI skill runs publish through shared JobRepository');
requireText('guiapp/task_orchestrator.go', 'agentruntime.UpsertJob', 'GUI orchestration plans publish through shared JobRepository');
requireText('guiapp/app_user_data_migration.go', 'agentruntime.UpsertJob', 'GUI migration jobs publish through shared JobRepository');
requireText('guiapp/semantic_tool_routing.go', 'tool.DefaultInvocationGrantTTL', 'GUI uses shared grant TTL');
requireText('guiapp/semantic_tool_routing.go', 'tool.PublishCurrentSurface', 'GUI uses shared surface publish adapter');
requireText('guiapp/semantic_tool_routing.go', 'tool.MaterializeReadySurface', 'GUI uses shared issue-and-bind adapter');
requireText('guiapp/semantic_tool_routing.go', 'tool.BindIssuedGrant', 'GUI uses shared grant table bind');
requireText('guiapp/semantic_tool_routing.go', 'tool.MaterializeReadySurface', 'GUI incremental refresh uses shared unrendered grant filter');
requireText('guiapp/semantic_tool_routing.go', 'tool.RenderVisibleReadyDefinitions', 'GUI visible surface uses shared renderer');
requireText('guiapp/semantic_tool_routing.go', 'tool.NewPlanningBudget', 'GUI uses shared planning budget');
requireText('guiapp/semantic_tool_routing.go', 'tool.QualifiersEqual', 'GUI uses shared qualifier equality');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.PublishCurrentSurface', 'headless uses shared surface publish adapter');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.MaterializeReadySurface', 'headless uses shared issue-and-bind adapter');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.BindIssuedGrant', 'headless uses shared grant table bind');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.MaterializeReadySurface', 'headless Definitions uses shared renderer');
requireText('corelib/agent/shared_capabilities.go', 'func ExtraSharedHostCapabilityNames() []string {', 'ExtraSharedHost stays a shared empty hook');
requireText('corelib/agent/shared_capabilities.go', 'func HostPrivateCapabilityNames()', 'GUI-only tools stay host-private');
requireText('corelib/agentservice/dynamic_host_websearch.go', 'func AttachReviewedHostWebSearchProvider', 'reviewed host web search provider');
requireText('corelib/agentservice/reviewed_dynamic_capabilities.go', 'QualifierSearchFreshness: SearchFreshnessReference', 'reviewed search uses information.search.web');
requireText('corelib/agentservice/distributed_rate_limiter.go', 'type SQLiteDistributedRateLimiter struct', 'shared-file distributed rate limiter');
requireText('MaClawSrv/domain_reconcilers.go', 'case "migration.export":', 'migration jobs use the domain reconciler map');
requireText('MaClawSrv/main.go', 'NewSQLiteDistributedRateLimiter', 'srv can wire the shared-file rate limiter');
requireText('MaClawSrv/dynamic_semantic_routing.go', 'coretool.DefaultInvocationGrantTTL', 'srv uses shared grant TTL');
requireText('guiapp/shared_agent_runtime.go', 'host.ctx.Host = request.Host', 'GUI Runtime applies request Host');
requireText('guiapp/btw_subagent.go', 'agentruntime.ResolveRole', 'BTW identity uses shared role resolver');
requireText('corelib/tool/semantic_artifact_projection.go', 'func ArtifactFileName', 'shared artifact file-name projection');
requireText('corelib/tool/semantic_artifact_projection.go', 'func DocumentTempSuffixForSelection', 'shared document materialization suffix');
requireText('corelib/tool/semantic_artifact_projection.go', 'func DocumentGenerateSelection', 'shared document-generate selection filter');
requireText('corelib/tool/semantic_planner.go', 'func IsDocumentGenerateFile', 'shared document.generate.file capability predicate');
requireText('guiapp/im_agent_loop_shared.go', 'tool.DocumentGenerateSelection', 'GUI generate skip uses shared document-generate filter');
requireText('guiapp/im_agent_loop_shared.go', 'tool.HasUnissuedReadyDocumentGenerate', 'GUI hold-dependant generate uses shared unissued check');
requireText('corelib/tool/semantic_artifact_projection.go', 'func ForEachHostSatisfiedLookupForGenerate', 'shared generate-after-lookup satisfaction walk');
requireText('guiapp/im_agent_loop_shared.go', 'tool.ForEachHostSatisfiedLookupForGenerate', 'GUI generate-after-lookup uses shared satisfaction walk');
requireText('corelib/tool/semantic_repeat.go', 'func SoleLiveGrantByAdapter', 'shared sole live grant by adapter');
requireText('corelib/tool/semantic_repeat.go', 'func HasRetiredGrantByAdapter', 'shared retired grant by adapter');
requireText('guiapp/im_agent_loop_shared.go', 'tool.SoleLiveGrantByAdapter', 'GUI live grant lookup uses shared adapter projection');
requireText('guiapp/im_agent_loop_shared.go', 'tool.HasRetiredGrantByAdapter', 'GUI retired generate lookup uses shared adapter projection');
requireText('corelib/tool/semantic_artifact_projection.go', 'func CurrentChannelDeliveryDependency', 'shared current-channel delivery dependency');
requireText('guiapp/im_agent_loop_shared.go', 'tool.CurrentChannelDeliveryDependency', 'GUI auto-delivery uses shared delivery dependency');
requireText('corelib/tool/semantic_artifact_projection.go', 'func ArtifactContractMatches', 'shared artifact contract wildcard match');
requireText('corelib/tool/semantic_planner.go', 'ArtifactContractMatches', 'planner producer match uses shared artifact contract wildcard');
requireText('corelib/tool/semantic_planner.go', 'ArtifactBindingMatchesContract', 'planner trusted binding uses shared contract match');
requireText('corelib/tool/semantic_artifact_projection.go', 'func ArtifactBindingMatchesContract', 'shared artifact binding contract match');
requireText('corelib/tool/semantic_artifact_projection.go', 'func ProducerArtifactPublished', 'shared producer-published artifact probe');
requireText('corelib/tool/semantic_artifact_projection.go', 'func UniqueMatchingArtifactDependency', 'shared unique matching artifact dependency');
requireText('corelib/tool/semantic_artifact_projection.go', 'func UniqueBoundArtifactDependency', 'shared unique bound artifact dependency');
requireText('corelib/tool/semantic_artifact_projection.go', 'func ValidateBoundArtifactDependency', 'shared bound artifact dependency validation');
requireText('corelib/tool/semantic_artifact_projection.go', 'func NewestFamilyProducerArtifact', 'shared family-newest producer artifact');
requireText('guiapp/semantic_artifacts.go', 'tool.UniqueBoundArtifactDependency', 'GUI trusted input uses shared unique bound dependency');
requireText('guiapp/semantic_artifacts.go', 'tool.ValidateBoundArtifactDependency', 'GUI trusted input uses shared bound validation');
requireText('guiapp/semantic_artifacts.go', 'tool.UniqueMatchingArtifactDependency', 'GUI planned consume uses shared unique matching dependency');
requireText('guiapp/semantic_artifacts.go', 'tool.NewestFamilyProducerArtifact', 'GUI planned consume uses shared family-newest producer');
requireText('corelib/tool/semantic_artifact_store.go', 'ArtifactContractMatches', 'artifact store contract match uses shared wildcard');
requireText('guiapp/im_agent_loop_shared.go', 'tool.ProducerArtifactPublished', 'GUI auto-delivery uses shared producer publication probe');
requireText('corelib/tool/semantic_repeat.go', 'func LiveGrantNameForCapability', 'shared live grant by capability');
requireText('guiapp/im_agent_loop_shared.go', 'tool.LiveGrantNameForCapability', 'GUI petition expose uses shared live grant by capability');
requireText('corelib/tool/semantic_artifact_projection.go', 'func FirstRequiredArtifactContract', 'shared artifact contract lookup');
requireText('corelib/tool/semantic_artifact_projection.go', 'func TurnRequiredDeliveryComplete', 'shared required-delivery turn completion');
requireText('guiapp/semantic_tool_routing.go', 'tool.TurnRequiredDeliveryComplete', 'GUI early-stop uses shared delivery completion');
requireText('guiapp/semantic_tool_routing.go', 'tool.MaterializeReadySurface', 'GUI shared plan filtering adapter');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.MaterializeReadySurface', 'headless shared plan filtering adapter');
requireText('guiapp/im_agent_loop_shared.go', 'tool.PlanSelectionByID', 'GUI shared plan lookup adapter');
requireText('guiapp/im_agent_loop_shared.go', 'tool.IsLookupSelection', 'GUI shared lookup selection adapter');
requireText('corelib/agentservice/dynamic_semantic_routing.go', 'coretool.PlanSelectionByID', 'headless shared plan lookup adapter');
requireText('guiapp/im_agent_loop_shared.go', 'tool.ArtifactFileName', 'GUI shared artifact file-name adapter');
requireText('guiapp/im_agent_loop_shared.go', 'tool.DocumentTempSuffixForSelection', 'GUI shared document suffix adapter');
requireText('guiapp/im_agent_loop_shared.go', 'tool.FirstRequiredArtifactContract', 'GUI shared artifact contract adapter');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.HostOwnedPDFReportTitle', 'GUI shared PDF title adapter');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.SubstantialPDFReportText', 'GUI shared PDF body adapter');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.TrustedLookupEvidence', 'GUI shared evidence trust adapter');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.SemanticToolExecutionResult', 'GUI shared semantic tool outcome adapter');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.ToolTextResult', 'GUI shared legacy text tool outcome adapter');
requireText('corelib/agentservice/shared_tool_surface.go', 'agentruntime.ToolTextResult', 'headless shared legacy text tool outcome adapter');
requireText('corelib/agentservice/shared_tool_surface.go', 'database.ToolDescription', 'headless shared database tool description');
requireText('corelib/agentservice/shared_tool_surface.go', 'database.HandleTool', 'headless shared database tool execution');
requireText('guiapp/tool_database.go', 'database.HandleTool', 'GUI shared database tool execution');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.AppendUniqueStrings', 'GUI shared stable string adapter');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.FilterToolDefinitionsByName', 'GUI shared tool definition filtering adapter');
requireText('guiapp/im_agent_loop_shared.go', 'tool.SelectionRequiresReceipt', 'GUI shared receipt policy adapter');
requireText('corelib/agentservice/dynamic_semantic_execution.go', 'coretool.SelectionRequiresExternalReceipt', 'headless shared receipt policy adapter');
requireText('guiapp/semantic_dynamic_providers.go', 'tool.SelectionRequiresExternalReceipt', 'GUI dynamic shared receipt policy adapter');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.NormalizePDFInvocationArgs', 'GUI shared PDF invocation adapter');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.PDFArgsTooThin', 'GUI shared PDF admission adapter');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.StripDeferredPDFPromise', 'GUI shared PDF visible-text adapter');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.ResponseHasPDF', 'GUI shared artifact type adapter');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.ShouldClearStaleErrorAfterArtifactAttach', 'GUI shared artifact error adapter');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.DocumentReadResultProjection', 'GUI document result projection adapter');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.DeliveryInvocationArgs', 'GUI delivery argument projection adapter');
requireText('guiapp/im_tool_execution.go', 'agentruntime.NormalizeToolArgumentsJSON', 'GUI shared tool argument adapter');
requireText('guiapp/im_tool_execution.go', 'agentruntime.CanonicalizeBrowserToolCall', 'GUI shared browser alias adapter');
requireText('guiapp/im_tool_execution.go', 'agentruntime.UnsupportedBrowserAction', 'GUI shared browser policy adapter');
requireText('guiapp/tools_browser_merged.go', 'agentruntime.UnsupportedBrowserAction', 'GUI merged browser policy adapter');
requireText('guiapp/im_tool_execution.go', 'agentruntime.CanonicalizeToolCallJSON', 'GUI combined tool-call adapter');
requireText('guiapp/im_tools_local.go', 'agentruntime.CanonicalizeLocalFileSearchToolCall', 'GUI shared local search alias adapter');
requireText('corelib/agentservice/core_agent_executor.go', 'agentruntime.ParseToolArgumentsObject', 'headless shared tool argument adapter');
requireText('corelib/agentservice/core_agent_executor.go', 'agentruntime.CanonicalizeToolCallJSON', 'headless combined tool-call adapter');
requireText('guiapp/app_maclaw_llm.go', 'corelib.NormalizeGlobalThinkingMode', 'shared thinking-mode normalization adapter');
requireText('corelib/agentservice/dynamic_host_config.go', 'corelib.ParseGlobalThinkingMode', 'headless thinking-mode parser adapter');
requireText('corelib/agentservice/config.go', 'corelib.EffectiveGlobalThinkingMode', 'headless thinking-mode default adapter');
requireText('guiapp/semantic_tool_routing.go', 'agent.DocumentAttachmentFormat', 'GUI shared document format adapter');
requireText('corelib/agentservice/dynamic_host_docread.go', 'agent.DocumentAttachmentFormat', 'headless shared document format adapter');
requireText('corelib/agent/attachment.go', 'func DocumentAttachmentFormat', 'shared document format classification');
requireText('guiapp/im_system_prompt.go', 'agentruntime.TruncateToTokenBudget', 'GUI shared prompt budget adapter');
requireText('corelib/agentruntime/prompt_budget.go', 'func TruncateToTokenBudget', 'shared prompt budget truncation');
requireText('guiapp/im_tool_execution.go', 'agentruntime.NormalizeMCPToolCallArgsForAgentLoop', 'GUI shared MCP call-envelope adapter');
requireText('corelib/agentruntime/tool_calls.go', 'func NormalizeMCPToolCallArgsForAgentLoop', 'shared MCP call-envelope normalization');
requireText('guiapp/im_tool_execution.go', 'agentruntime.ManageScheduleCreateBlockReason', 'GUI shared schedule admission adapter');
requireText('corelib/agentruntime/tool_admission.go', 'func ManageScheduleCreateBlockReason', 'shared schedule admission policy');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.HostOwnedPDFReportContent', 'GUI shared PDF report content adapter');
requireText('corelib/agentruntime/semantic_pdf.go', 'func HostOwnedPDFReportContent', 'shared PDF report content synthesis');
requireText('guiapp/im_system_prompt.go', 'agentruntime.SkillDocMatchScore', 'GUI shared skill-doc matcher adapter');
requireText('guiapp/im_system_prompt.go', 'agentruntime.CountTriggerMatches', 'GUI shared trigger match adapter');
requireText('guiapp/im_system_prompt.go', 'agentruntime.SkillDocMatchLooksLikePathOperand', 'GUI shared path-operand guard adapter');
requireText('corelib/agentruntime/skill_doc_match.go', 'func SkillDocPhraseOccurs', 'shared skill-doc phrase matcher');
requireText('corelib/agentruntime/adaptive_retry.go', 'type AdaptiveRetry struct', 'shared adaptive retry controller');
requireText('corelib/agentruntime/adaptive_retry.go', 'type TrajectoryRecordSink interface', 'adaptive retry trajectory sink port');
requireText('guiapp/adaptive_retry.go', 'agentruntime.AdaptiveRetry', 'GUI adaptive retry alias');
requireText('corelib/agentruntime/llm_retry_error_kind.go', 'func ClassifyLLMRetryError', 'shared LLM retry classification');
requireText('guiapp/llm_retry_error_kind.go', 'agentruntime.ClassifyLLMRetryError', 'GUI LLM retry classification wrapper');
requireText('corelib/agentruntime/experience_vocabulary.go', 'ExperienceReviewStatusTagPrefix', 'shared experience review vocabulary');
requireText('guiapp/experience_review_status.go', 'agentruntime.ExperienceReviewStatus', 'GUI experience review status alias');
requireText('guiapp/experience_trace_kind.go', 'agentruntime.ExperienceTraceSourceToolUsage', 'GUI experience trace source alias');
requireText('corelib/logx/redact.go', 'func RedactString', 'shared log redaction boundary');
requireText('corelib/logx/logx.go', 'func WithCorrelation', 'shared log correlation helper');
requireText('MaClawSrv/main.go', 'logx.NewFromEnv', 'srv structured logger composition');
requireText('MaClawSrv/request_context.go', 'logx.WithCorrelation', 'srv request completion structured log');
requireText('corelib/agentruntime/registered_tool_outcome.go', 'ToolTextFailure(text)', 'registered tool outcome must share the ToolTextFailure vocabulary');
requireText('corelib/agentruntime/correlation.go', 'func TraceParentHeader', 'shared outbound traceparent contract');
requireText('MaClawSrv/http_migration.go', 'agentruntime.TraceParentHeader', 'srv-to-hub span link injection');
requireText('corelib/agentservice/knowledge_import_share.go', 'agentruntime.TraceParentHeader', 'knowledge share span link injection');
requireText('corelib/agentruntime/registered_tool_outcome.go', 'func RegisteredToolTextOutcome', 'shared registered tool outcome classifier');
requireText('guiapp/im_tool_execution.go', 'agentruntime.RegisteredToolTextOutcome', 'GUI shared registered tool outcome adapter');
requireText('corelib/agentruntime/channel_delivery.go', 'func SemanticFileDeliveryPublished', 'shared channel delivery policy');
requireText('corelib/agentruntime/channel_delivery.go', 'func NormalizeIMMessagePlatformKind', 'shared IM platform vocabulary');
requireText('guiapp/im_message_platform_kind.go', 'agentruntime.IMMessagePlatformKind', 'GUI shared platform kind alias');
requireText('guiapp/semantic_tool_routing.go', 'agentruntime.SemanticScheduleDispatchPublished', 'GUI shared schedule dispatch adapter');
requireText('guiapp/semantic_tool_routing.go', 'agentruntime.SemanticVoiceDeliveryPublished', 'GUI shared voice delivery adapter');
requireText('guiapp/semantic_tool_routing.go', 'agentruntime.SemanticAudioSynthesizeLocalPublished', 'GUI shared local audio adapter');
requireText('guiapp/agent_loop_state.go', 'func (s LoopState) RuntimeStatusValue()', 'GUI background loop Runtime status projection');
requireText('guiapp/background_loop_manager.go', 'RuntimeStatus agentruntime.JobStatus', 'GUI background loop runtime_status field');
requireText('guiapp/remote_experiment_orchestrator.go', 'RuntimeStatus agentruntime.JobStatus', 'GUI experiment orchestrator runtime_status field');
requireText('guiapp/remote_experiment_orchestrator.go', 'func experimentRoundRuntimeStatus', 'GUI experiment round Runtime status projection');
requireText('guiapp/app_digital_asset_contribute.go', 'RuntimeStatus agentruntime.JobStatus', 'GUI digital-asset submission runtime_status field');
requireText('guiapp/coding_subagent_rollout.go', 'RuntimeStatus agentruntime.JobStatus', 'GUI coding rollout runtime_status field');
requireText('guiapp/remote_types.go', 'RuntimeStatus   agentruntime.JobStatus', 'GUI session summary runtime_status field');
requireText('guiapp/remote_status.go', 'func sessionSummaryRuntimeStatus', 'GUI session summary Runtime status projection');
requireText('guiapp/remote_status.go', 'RuntimeStatus  agentruntime.JobStatus', 'GUI remote session runtime_status field');
requireText('guiapp/remote_types.go', 'func (status SessionStatus) RuntimeStatusValue()', 'GUI session status Runtime projection');
requireText('guiapp/browser_agent_manager.go', 'RuntimeStatus  agentruntime.JobStatus', 'GUI browser session runtime_status field');
requireText('guiapp/im_tool_agent_status.go', 'RuntimeStatus agentruntime.JobStatus', 'GUI agent status runtime_status field');
requireText('guiapp/workflow_adapter_persistence.go', 'RuntimeStatus agentruntime.JobStatus', 'GUI workflow manifest runtime_status field');
requireText('guiapp/coding_workbench_align.go', 'RuntimeStatus agentruntime.JobStatus', 'GUI coding workbench step runtime_status field');
requireText('guiapp/llm_trajectory.go', 'RuntimeStatus agentruntime.JobStatus', 'GUI trajectory runtime_status field');
requireText('guiapp/llm_trajectory.go', 'func trajectoryRuntimeStatus', 'GUI trajectory Runtime status projection');
requireFile('corelib/agentruntime/builtin.go');
requireFile('corelib/agentruntime/turn.go');
requireFile('corelib/agentruntime/role.go');
requireText('corelib/agentruntime/builtin.go', 'func BuiltinModules', 'compiled-in Runtime builtin modules');
requireText('corelib/agentruntime/builtin.go', 'func RegisterBuiltinModules', 'composition-root builtin registry');
requireText('corelib/agentruntime/turn.go', 'func RunAgentTurn', 'single Agent loop entry');
requireText('corelib/agentruntime/role.go', 'DefaultRoleDescription', 'shared default Agent role');
requireText('corelib/agentservice/core_agent_executor.go', 'agentruntime.ResolveRole', 'srv uses shared default role');
requireText('corelib/agentservice/core_agent_executor.go', 'agentruntime.RunAgentTurnWithUserContent', 'srv loop goes through Runtime');
requireText('corelib/agentservice/core_agent_executor.go', 'bindRequestHostCapabilities', 'request HostCapabilities govern execution');
requireText('corelib/agentservice/core_agent_executor.go', 'GuardDisabledTool', 'srv AdaptiveRetry gate');
requireText('guiapp/im_agent_loop_shared.go', 'agentruntime.RunAgentTurnWithUserContent', 'GUI shared loop goes through Runtime');
requireText('guiapp/im_agent_loop_shared.go', 'guiHostCapabilities{}', 'GUI TurnRequest injects HostCapabilities');
requireText('guiapp/shared_agent_runtime.go', 'agentruntime.RegisterBuiltinModules', 'GUI composition root loads builtin modules');
requireText('guiapp/shared_agent_runtime.go', 'MergeCapabilityTools', 'GUI capability snapshot merges host catalog with Runtime modules');
requireText('corelib/agentruntime/runtime.go', 'host.DesktopCapture()', 'typed host ports gate required capabilities');
requireText('guiapp/app_ve_handler.go', 'agentruntime.RunAgentTurn', 'VE loop goes through Runtime');
requireText('guiapp/btw_subagent.go', 'agentruntime.RunAgentTurn', 'BTW loop goes through Runtime');
requireText('guiapp/im_loop_command_callbacks.go', 'agentruntime.RunAgentTurn', 'loop-command goes through Runtime');
requireText('guiapp/coding_subagent_verify.go', 'agentruntime.RunAgentTurn', 'coding verify loop goes through Runtime');
requireText('cmd/maclaw-gui/main.go', 'RegisterAgentHandlerFactory', 'GUI main registers handler factory');
requireText('MaClawSrv/http.go', 's.registerOpsRoutes()', 'HTTP ops routes extracted from http.go');
requireFile('MaClawSrv/http_routes_ops.go');
requireFile('MaClawSrv/http_routes_admin.go');
requireFile('MaClawSrv/http_routes_user.go');

// Runtime must remain transport-neutral and cannot acquire imports from the
// GUI, HTTP server, or service persistence layer.
for (const rel of walk('corelib/agentruntime').filter((file) => file.endsWith('.go') && !file.endsWith('_test.go'))) {
  const text = read(rel);
  if (/"github\.com\/RapidAI\/CodeClaw\/(?:guiapp|MaClawSrv)(?:\/|")/.test(text) || /"github\.com\/RapidAI\/CodeClaw\/corelib\/agentservice"/.test(text)) {
    failures.push(`${rel} imports a transport or persistence package`);
  }
}

// Hosts must consume the Runtime facade; they may not construct a second
// module registry or adapter with host-specific Agent behavior.
for (const root of ['guiapp', 'MaClawSrv']) {
  for (const rel of walk(root).filter((file) => file.endsWith('.go') && !file.endsWith('_test.go'))) {
    const text = read(rel);
    if (/\bNewRuntimeExecutor(?:WithModules)?\s*\(/.test(text) || /\bNewModuleRegistry\s*\(/.test(text)) {
      failures.push(`${rel} constructs Runtime/module registries outside corelib composition root`);
    }
  }
}

// Domain workers must report through the context-injected Runtime contract.
// Direct calls into the MaClawSrv manager recreate host-specific closures and
// prevent the same non-UI worker from running unchanged under GUI/TUI hosts.
for (const rel of walk('MaClawSrv')
  .filter((file) => file.endsWith('.go') && !file.endsWith('_test.go') && file !== 'MaClawSrv/jobs.go' && file !== 'MaClawSrv/job_admission.go')) {
  const source = read(rel);
  if (/\.updateProgress\s*\(/.test(source)) {
    failures.push(`${rel} reports job progress through the MaClawSrv manager; use agentruntime.ReportJobUpdate from the worker context`);
  }
  if (/\.createUserJob(?:WithPolicies|WithRecoveryPolicy|Idempotent)?\s*\(/.test(source)) {
    failures.push(`${rel} creates async Jobs outside the shared HTTP admission boundary; use admitUserJob so idempotency and recovery policy are explicit`);
  }
}

const allowedRegistryCallers = new Set([
  'corelib/agentruntime/runtime.go',
  'corelib/agentruntime/builtin.go',
  'corelib/agentservice/service.go',
  'guiapp/shared_agent_runtime.go',
]);
const productionRegistryConstructors = ['corelib/agentruntime', 'corelib/agentservice', 'guiapp', 'MaClawSrv']
  .flatMap((root) => walk(root))
  .filter((file) => file.endsWith('.go') && !file.endsWith('_test.go'))
  .filter((rel) => !allowedRegistryCallers.has(rel))
  .filter((rel) => /\b(?:NewModuleRegistry|RegisterBuiltinModules)\s*\(/.test(read(rel)));
if (productionRegistryConstructors.length) {
  failures.push(`Runtime module registry constructed outside composition root: ${productionRegistryConstructors.join(', ')}`);
}

for (const root of ['guiapp', 'MaClawSrv', 'corelib/agentservice']) {
  for (const rel of walk(root).filter((file) => file.endsWith('.go') && !file.endsWith('_test.go'))) {
    const source = read(rel);
    if (/\bagent\.RunLoop(?:WithUserContent)?\s*\(/.test(source)) {
      failures.push(`${rel} calls agent.RunLoop directly; use agentruntime.RunAgentTurn`);
    }
  }
}

if (/\bfunc init\(\)\s*\{[\s\S]*RegisterHandlerFactory/.test(read('guiapp/agent_handler_bridge.go'))) {
  failures.push('guiapp/agent_handler_bridge.go must not register the handler factory from init()');
}

// AppConfig ownership/visibility sets belong exclusively to the shared
// schema package. A transport-local map is a silent drift point: a new GUI
// field could otherwise be accepted by one host and stripped by the other.
for (const rel of [...walk('guiapp'), ...walk('MaClawSrv'), ...walk('corelib/agentservice')]
  .filter((file) => file.endsWith('.go') && !file.endsWith('_test.go'))) {
  const source = read(rel);
  if (/\b(?:sharedClientConfigKeys|maclawSrvHidden(?:AppConfig)?Keys|userHiddenConfigKeys|userComplexConfigKeys)\s*=\s*map\s*\[/.test(source)) {
    failures.push(`${rel} defines a transport-local AppConfig policy map; use corelib/config schema`);
  }

  // AppConfig JSON tags are part of the shared schema contract.  A local
  // reflection parser silently creates a second source of truth (migration,
  // settings DTOs, and srv projections can then disagree on renamed fields).
  // Keep generic reflection available for unrelated structs, but require the
  // canonical helper whenever a host/service file reflects AppConfig.
  if (/reflect\.TypeOf\(\s*corelib\.AppConfig\s*\{/.test(source) &&
      /\.Tag\.Get\(\s*["']json["']\s*\)/.test(source) &&
      !source.includes('coreconfig.JSONFieldName')) {
    failures.push(`${rel} parses AppConfig JSON tags locally; use corelib/config.JSONFieldName`);
  }
}

// The desktop app code lives in the importable guiapp package; the only
// package-main entry point is cmd/maclaw-gui. A `package main` file at the
// guiapp top level would silently reintroduce the unimportable-app problem.
for (const rel of walk('guiapp').filter((file) => file.endsWith('.go') && !file.includes('/'))) {
  if (/^package main$/m.test(read(rel))) {
    failures.push(`${rel} declares package main; guiapp top level must stay package guiapp (entry point: cmd/maclaw-gui)`);
  }
}
requireText('cmd/maclaw-gui/main.go', 'guiapp.Main(version)', 'cmd/maclaw-gui thin entry point');

// corelib must stay host-agnostic: nothing under corelib may import the
// desktop host package (same direction rule as MaClawSrv above).
for (const rel of walk('corelib').filter((file) => file.endsWith('.go'))) {
  if (/"github\.com\/RapidAI\/CodeClaw\/guiapp(?:\/|")/.test(read(rel))) {
    failures.push(`${rel} imports the desktop host package guiapp; corelib must not depend on GUI hosts`);
  }
}

if (failures.length) {
  console.error('Agent architecture guard failed:');
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}
console.log('Agent architecture guard passed.');
