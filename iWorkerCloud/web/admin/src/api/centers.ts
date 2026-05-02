import { apiGet, apiPost, apiPut, apiDelete } from './client';

export type CloudControlMode = 'cloud_managed' | 'self_managed' | 'hybrid';

export interface Center {
  id: string;
  company_name: string;
  base_url?: string;
  cloud_control_mode?: CloudControlMode;
  last_sync_status?: string;
  iworker_ready?: boolean;
  iworker_readiness_status?: string;
  iworker_agent_instance_count?: number;
  status: string;
  created_at: string;
  updated_at?: string;
  last_heartbeat?: string;
}

export interface CenterIntegrationPatch {
  base_url: string;
  cloud_control_mode: CloudControlMode;
  last_sync_status: string;
}



export interface CenterWorkloadSummary {
  agent_instance_count: number;
  active_count: number;
  completed_count: number;
  review_count: number;
  blocked_count: number;
  updated_at?: string;
}

export interface CenterIWorkerReadiness {
  ready: boolean;
  status: string;
  agent_instance_count?: number;
  agent_runtime_ready?: boolean;
  goalwatch_ready?: boolean;
  workload_summary?: CenterWorkloadSummary;
}

export interface CenterComputeSyncStatus {
  last_sync_at: string;
  status: 'success' | 'failure' | 'pending' | 'waiting_for_credentials';
  error?: string;
  provider_count: number;
}

export interface CenterProbeResult {
  ok: boolean;
  status_code: number;
  message: string;
  base_url: string;
  runtime_type?: string;
  product_kind?: string;
  admin_console?: string;
  provider_count?: number;
  runtime_provider_mode?: 'settings' | 'cloud_sync' | 'local_self_managed';
  compute_source?: 'cloud' | 'local';
  compute_permission?: boolean;
  cloud_provider_count?: number;
  compute_sync_status?: CenterComputeSyncStatus;
  iworker_readiness?: CenterIWorkerReadiness;
}

export interface CenterServiceReadiness {
  allowed: boolean;
  center: Center;
  active_license?: unknown;
  issues: string[];
  recommended_actions: RecommendedAction[];
}
export interface CenterProbeResponse {
  probe: CenterProbeResult;
  center: Center;
}

export interface ManagementSummary {
  total_centers: number;
  pending_centers: number;
  active_licenses: number;
  ready_centers: number;
  needs_setup: number;
  probe_failures: number;
  unlicensed_centers: number;
  workload_agent_instances?: number;
  workload_active_tasks?: number;
  workload_completed_tasks?: number;
  workload_review_tasks?: number;
  workload_blocked_tasks?: number;
}

export interface CenterManagement {
  center: Center;
  ready: boolean;
  issues: string[];
  recommended_actions: RecommendedAction[];
  management_posture: string;
  commercial_status: string;
  connectivity: string;
  iworker_operational_ready?: boolean;
  iworker_readiness_status?: string;
  iworker_readiness?: CenterIWorkerReadiness;
}

export interface RecommendedAction {
  code: string;
  label: string;
  description: string;
  priority: string;
}

export interface CenterManagementReport {
  summary: ManagementSummary;
  items: CenterManagement[];
}

export function listCenters(): Promise<Center[]> {
  return apiGet('/api/admin/centers');
}

export function getCenterManagement(): Promise<CenterManagementReport> {
  return apiGet('/api/admin/centers/management');
}

export function updateCenterIntegration(id: string, patch: CenterIntegrationPatch): Promise<Center> {
  return apiPut(`/api/admin/centers/${id}/integration`, patch);
}

export function getServiceReadiness(id: string): Promise<CenterServiceReadiness> {
  return apiGet(`/api/admin/centers/${id}/service-readiness`);
}

export function probeCenter(id: string): Promise<CenterProbeResponse> {
  return apiPost(`/api/admin/centers/${id}/probe`);
}

export function fetchCenterRuntimeSnapshot(id: string): Promise<CenterProbeResult> {
  return apiGet(`/api/admin/centers/${id}/runtime-snapshot`);
}

export function confirmTrial(id: string): Promise<void> {
  return apiPost(`/api/admin/centers/${id}/confirm-trial`);
}

export function confirmManual(id: string, modules: string[], days: number): Promise<void> {
  return apiPost(`/api/admin/centers/${id}/confirm`, { modules, days });
}

export function disableCenter(id: string): Promise<void> {
  return apiPost(`/api/admin/centers/${id}/disable`);
}

export function enableCenter(id: string): Promise<void> {
  return apiPost(`/api/admin/centers/${id}/enable`);
}

export function deleteCenter(id: string): Promise<void> {
  return apiDelete(`/api/admin/centers/${id}`);
}


