import { apiGet, apiPost, apiPut, apiDelete } from './client';

export type CloudControlMode = 'cloud_managed' | 'self_managed' | 'hybrid';

export interface Center {
  id: string;
  company_name: string;
  admin_email: string;
  admin_phone?: string;
  address?: string;
  legal_person?: string;
  base_url?: string;
  supports_multi_tenant?: boolean;
  tenant_count?: number;
  cloud_control_mode?: CloudControlMode;
  last_sync_status?: string;
  status: string;
  created_at: string;
  updated_at?: string;
  last_heartbeat?: string;
}

export interface CenterIntegrationPatch {
  base_url: string;
  supports_multi_tenant: boolean;
  tenant_count: number;
  cloud_control_mode: CloudControlMode;
  last_sync_status: string;
}

export interface ProvisionTenantRequest {
  company_name: string;
  legal_person: string;
  email: string;
  address: string;
  admin_username: string;
  admin_password: string;
}

export interface ProvisionTenantResult {
  tenant_id: string;
  status: string;
  message: string;
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
}

export interface CenterProvisionReadiness {
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
  multi_tenant_centers: number;
  tenant_count: number;
  unlicensed_centers: number;
}

export interface CenterManagement {
  center: Center;
  ready: boolean;
  issues: string[];
  recommended_actions: RecommendedAction[];
  management_posture: string;
  commercial_status: string;
  connectivity: string;
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

export function getProvisionReadiness(id: string): Promise<CenterProvisionReadiness> {
  return apiGet(`/api/admin/centers/${id}/provision-readiness`);
}
export function provisionTenant(id: string, request: ProvisionTenantRequest): Promise<ProvisionTenantResult> {
  return apiPost(`/api/admin/centers/${id}/provision-tenant`, request);
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


