import { apiGet, apiPost } from './client';

export interface WatcherPolicy {
  enabled: boolean;
  single_flight: boolean;
  max_run_seconds: number;
  scale_by_worker_count: boolean;
}


export interface GoalWatcherPolicyStatus extends WatcherPolicy {
  tick_interval_seconds: number;
  stalled_after_seconds: number;
  push_cooldown_seconds: number;
  lease_ttl_seconds: number;
  workers_per_shard: number;
  max_watchers: number;
}

export interface GoalWatcherConfigStatus {
  tick_interval_seconds: number;
  stalled_after_seconds: number;
  push_cooldown_seconds: number;
  lease_ttl_seconds: number;
  workers_per_shard: number;
  max_watchers: number;
}

export interface GoalWatcherShardStatus {
  shard_index: number;
  shard_count: number;
  checked: number;
  pushed: number;
  lease_owner?: string;
  lease_held: boolean;
  error?: string;
}

export interface GoalWatcherTenantStatus {
  tenant_id: string;
  policy_persisted: boolean;
  policy: GoalWatcherPolicyStatus;
  started_at: string;
  finished_at: string;
  iworker_count: number;
  shard_count: number;
  checked: number;
  pushed: number;
  error?: string;
  shards: GoalWatcherShardStatus[];
}

export interface GoalWatcherStatus {
  config: GoalWatcherConfigStatus;
  tenants: GoalWatcherTenantStatus[];
}
export interface BootstrapPlan {
  tenant_id?: string;
  company_name: string;
  business_summary: string;
  priority: string;
  virtual_departments: string[];
  initial_iworkers: string[];
  memory_scopes: string[];
  recurring_tasks: string[];
  requires_executive_confirmation: string[];
  watcher_policy: WatcherPolicy;
  status?: string;
  updated_at?: string;
}

export interface ValidationIssue {
  field: string;
  message: string;
  level: 'warning' | 'error';
}

export interface FirstWaveTask {
  id: string;
  title: string;
  owner_iworker: string;
  expected_output: string;
  memory_scope: string;
  escalation_threshold: string;
  requires_peer_review: boolean;
  recommended_trigger: string;
}

export interface AppliedAsset {
  kind: string;
  id?: string;
  name: string;
  status: string;
}

export interface BootstrapRun {
  id: string;
  tenant_id: string;
  status: string;
  plan: BootstrapPlan;
  tasks: FirstWaveTask[];
  applied_assets?: AppliedAsset[];
  created_at: string;
  updated_at: string;
}

export interface BootstrapStatus {
  tenant_id: string;
  has_plan: boolean;
  ready_to_start: boolean;
  plan?: BootstrapPlan;
  validation_issues: ValidationIssue[];
  last_run?: BootstrapRun;
  suggested_first_wave: FirstWaveTask[];
  applied_assets?: AppliedAsset[];
}

export interface BootstrapPlanResponse {
  plan: BootstrapPlan;
  validation_issues: ValidationIssue[];
  suggested_first_wave: FirstWaveTask[];
  applied_assets?: AppliedAsset[];
}

export interface BootstrapRunResponse {
  run: BootstrapRun;
}

export function fetchBootstrapStatus() {
  return apiGet<BootstrapStatus>('/admin/bootstrap/status');
}

export function draftBootstrapPlan(plan: BootstrapPlan) {
  return apiPost<BootstrapPlanResponse>('/admin/bootstrap/draft-plan', plan);
}

export function applyBootstrapPlan(plan: BootstrapPlan) {
  return apiPost<BootstrapPlanResponse>('/admin/bootstrap/apply-plan', plan);
}

export function startFirstWave() {
  return apiPost<BootstrapRunResponse>('/admin/bootstrap/start-first-wave');
}
export function fetchGoalWatcherStatus() {
  return apiGet<GoalWatcherStatus>('/admin/goalwatch/status');
}
