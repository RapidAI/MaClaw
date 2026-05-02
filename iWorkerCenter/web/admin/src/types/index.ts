export type CenterTab =
  | 'overview'
  | 'bootstrap'
  | 'employees'
  | 'communications'
  | 'workflows'
  | 'knowledge'
  | 'packages'
  | 'models'
  | 'compute'
  | 'security'
  | 'delivery'
  | 'usage'
  | 'im'
  | 'auth'
  | 'settings';

export interface CommunicationsNavigationTarget {
  task_id?: string;
  role_code?: string;
  source?: string;
}

export interface OverviewNavigationTarget {
  role_code?: string;
  source?: string;
}

export interface AssetNavigationTarget {
  role_code?: string;
  role_label?: string;
  draft_id?: string;
  draft_name?: string;
  source?: string;
}

export interface Metric {
  label: string;
  value: string;
  hint: string;
}

export interface DashboardItem {
  title: string;
  description: string;
  status: string;
  signal_priority?: number;
  role_code?: string;
  role_label?: string;
}

export interface ExecutiveAction {
  title: string;
  owner: string;
  owner_role_code: string;
  owner_role_label: string;
  description: string;
  linked_task_id?: string;
  linked_task_status?: string;
  linked_task_result?: string;
}

export interface ExecutiveBriefing extends DashboardItem {}

export interface ExecutiveBoardFocus {
  title: string;
  summary: string;
  description: string;
  status: string;
  signal_priority?: number;
  role_code?: string;
  role_label?: string;
}

export interface ExecutiveBoardHistoryItem {
  id: string;
  title: string;
  detail: string;
  timestamp: string;
  tone: 'ok' | 'info' | 'warn';
  navigationTarget?: CommunicationsNavigationTarget;
  detailLines?: string[];
  isCluster?: boolean;
  clusterSkillTitle?: string;
  clusterFocusTitle?: string;
  clusterTaskTitle?: string;
  clusterRoleCode?: string;
  clusterExecutionStatus?: string;
  clusterExecutionResult?: string;
}
export interface ExecutiveSkill {
  id: string;
  title: string;
  question: string;
  description: string;
}

export interface ExecutiveSkillResult {
  skill_id: string;
  title: string;
  summary: string;
  focus: ExecutiveBoardFocus;
  findings: string[];
  recommendations: ExecutiveAction[];
}

export interface DashboardData {
  metrics: Metric[];
  alerts: DashboardItem[];
  recent: DashboardItem[];
  briefing?: ExecutiveBriefing;
  board_summary?: string;
  board_focus?: ExecutiveBoardFocus;
  priority_decision?: ExecutiveBoardFocus;
  priority_summary?: string;
  board_signals?: DashboardItem[];
  board_history?: ExecutiveBoardHistoryItem[];
  risks?: DashboardItem[];
  actions?: ExecutiveAction[];
  updated_at?: string;
}

export interface CenterCloudHeartbeat {
  configured: boolean;
  status: string;
  center_id?: string;
  last_attempt_at?: string;
  last_success_at?: string;
  last_error?: string;
  consecutive_failures: number;
  runtime_type: string;
  product_kind: string;
  admin_console: string;
}

export interface CenterComputeSyncStatus {
  last_sync_at: string;
  status: 'success' | 'failure' | 'pending' | 'waiting_for_credentials';
  error?: string;
  provider_count: number;
  non_blocking?: boolean;
  runtime_impact?: 'cloud_sync_current' | 'using_cached_cloud_providers' | 'local_settings_fallback' | 'waiting_for_cloud_sync' | string;
}


export interface CenterReadinessCheck {
  name: string;
  ready: boolean;
  status: string;
  detail?: string;
  count?: number;
}

export interface CenterAuthReadiness {
  method: string;
  label: string;
  ready: boolean;
  implemented: boolean;
  status: string;
  detail?: string;
}

export interface CenterIWorkerReadiness {
  ready: boolean;
  status: string;
  tenant_count: number;
  role_count: number;
  colleague_count: number;
  local_account_count: number;
  agent_instance_count: number;
  agent_runtime_ready: boolean;
  goalwatch_ready: boolean;
  required_client_paths: string[];
  checks: CenterReadinessCheck[];
  auth_methods: CenterAuthReadiness[];
}

export interface IWorkerWorkStatusSummary {
  current_task?: string;
  current_detail?: string;
  active_count: number;
  completed_count: number;
  review_count: number;
  blocked_count: number;
  updated_at?: string;
}

export interface IWorkerAgentInstance {
  tenant_id: string;
  worker_id: string;
  instance_id: string;
  role: string;
  status: string;
  org_unit_id?: string;
  capabilities: string[];
  memory_authority: string;
  local_cache_mode: string;
  work_status?: IWorkerWorkStatusSummary;
  host_id?: string;
  process_id?: number;
  started_at: string;
  last_heartbeat_at: string;
  heartbeat_age_seconds: number;
  effective_status: string;
}

export interface CenterStatus {
  status: string;
  runtime_type?: string;
  product_kind?: string;
  admin_console?: string;
  provider_count: number;
  config_path: string;
  compute_source?: 'cloud' | 'local';
  compute_permission?: boolean;
  compute_sync_status?: CenterComputeSyncStatus;
  cloud_provider_count?: number;
  runtime_provider_mode?: 'settings' | 'cloud_sync' | 'local_self_managed';
  cloud_heartbeat?: CenterCloudHeartbeat;
  iworker_readiness?: CenterIWorkerReadiness;
}

export interface CenterSettings {
  providers: unknown[];
  work_type_keywords?: Record<string, string[]>;
  work_type_tier?: Record<string, string>;
  role_provider_boost?: Record<string, string[]>;
}







