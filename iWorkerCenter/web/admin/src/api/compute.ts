import { apiGet, apiPost, apiPut, apiDelete } from './client';

export interface ComputeProvider {
  id: string;
  name: string;
  base_url: string;
  protocol: string;
  user_agent: string;
  compute_type: string;
  model: string;
  enabled: boolean;
  priority: number;
  cost_tier: string;
  description: string;
  has_api_key: boolean;
  input_price_per_mtoken?: number;
  output_price_per_mtoken?: number;
  last_test_ok?: boolean;
  last_test_latency_ms?: number;
}

export interface ComputeStatus {
  compute_source: 'cloud' | 'local';
  effective_source?: 'cloud' | 'local' | 'local_fallback' | string;
  fallback_active?: boolean;
  active_provider_count?: number;
  compute_permission: boolean;
  last_sync_at?: string;
  sync_status?: ComputeSyncStatus;
  provider_count?: number;
}

export interface DiWorkerUsage {
  diworker_id: string;
  diworker_name: string;
  total_input_tokens: number;
  total_output_tokens: number;
  total_tokens: number;
  input_cost: number;
  output_cost: number;
  total_cost: number;
  request_count: number;
}

// Backend: GET /admin/compute/source -> { source, compute_permission, sync_status }
export function getComputeStatus(): Promise<ComputeStatus> {
  return apiGet<{
    source: string;
    compute_permission: boolean;
    effective_source?: string;
    fallback_active?: boolean;
    active_provider_count?: number;
    sync_status?: ComputeSyncStatus;
    last_sync_at?: string;
    provider_count?: number;
  }>('/admin/compute/source').then(d => ({
    compute_source: d.source as 'cloud' | 'local',
    effective_source: d.effective_source,
    fallback_active: d.fallback_active,
    active_provider_count: d.active_provider_count,
    compute_permission: d.compute_permission,
    sync_status: d.sync_status,
    last_sync_at: d.last_sync_at || d.sync_status?.last_sync_at,
    provider_count: d.provider_count ?? d.sync_status?.provider_count,
  }));
}
// Backend: GET /admin/compute/providers → { providers, source }
export function listComputeProviders(): Promise<ComputeProvider[]> {
  return apiGet<{ providers: ComputeProvider[] }>('/admin/compute/providers').then(d => d.providers || []);
}

// Backend: POST /admin/compute/local-providers
export function createComputeProvider(data: Partial<ComputeProvider> & { api_key?: string }): Promise<ComputeProvider> {
  return apiPost('/admin/compute/local-providers', data);
}

// Backend: PUT /admin/compute/local-providers/{id}
export function updateComputeProvider(id: string, data: Partial<ComputeProvider> & { api_key?: string }): Promise<ComputeProvider> {
  return apiPut(`/admin/compute/local-providers/${id}`, data);
}

// Backend: DELETE /admin/compute/local-providers/{id}
export function deleteComputeProvider(id: string): Promise<void> {
  return apiDelete(`/admin/compute/local-providers/${id}`);
}

// Backend: POST /admin/compute/test (body = provider object)
export function testComputeProvider(id: string, provider?: ComputeProvider): Promise<{ ok: boolean; latency_ms: number; error?: string }> {
  return apiPost('/admin/compute/test', provider || { id });
}

// Backend: POST /admin/compute/sync
export function syncFromCloud(): Promise<ComputeSyncStatus> {
  return apiPost('/admin/compute/sync');
}

// Backend: PUT /admin/compute/source (not POST)
export function switchComputeSource(source: 'cloud' | 'local'): Promise<void> {
  return apiPut('/admin/compute/source', { source });
}

// Backend: GET /admin/compute/sync-status
export type ComputeSyncState = 'success' | 'failure' | 'pending' | 'waiting_for_credentials';

export interface ComputeSyncStatus {
  last_sync_at: string;
  status: ComputeSyncState;
  error?: string;
  provider_count: number;
  non_blocking?: boolean;
  runtime_impact?: 'cloud_sync_current' | 'using_cached_cloud_providers' | 'local_settings_fallback' | 'waiting_for_cloud_sync' | string;
}

export function getSyncStatus(): Promise<ComputeSyncStatus> {
  return apiGet('/admin/compute/sync-status');
}

// Usage stats - not yet implemented in backend, will gracefully fail
export function listDiWorkerUsage(period: string, start: string, end: string): Promise<DiWorkerUsage[]> {
  return apiGet<{ summaries: DiWorkerUsage[] }>(`/admin/compute/usage?period=${period}&start=${start}&end=${end}`)
    .then(d => d.summaries || [])
    .catch(() => []);
}
