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
  compute_permission: boolean;
  last_sync_at?: string;
  sync_status?: string;
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

// Backend: GET /admin/compute/source → { source, compute_permission }
export function getComputeStatus(): Promise<ComputeStatus> {
  return apiGet<{ source: string; compute_permission: boolean }>('/admin/compute/source').then(d => ({
    compute_source: d.source as 'cloud' | 'local',
    compute_permission: d.compute_permission,
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
export function syncFromCloud(): Promise<void> {
  return apiPost('/admin/compute/sync');
}

// Backend: PUT /admin/compute/source (not POST)
export function switchComputeSource(source: 'cloud' | 'local'): Promise<void> {
  return apiPut('/admin/compute/source', { source });
}

// Backend: GET /admin/compute/sync-status
export function getSyncStatus(): Promise<{ last_sync_at: string; status: string; error?: string; provider_count: number }> {
  return apiGet('/admin/compute/sync-status');
}

// Usage stats - not yet implemented in backend, will gracefully fail
export function listDiWorkerUsage(period: string, start: string, end: string): Promise<DiWorkerUsage[]> {
  return apiGet<{ summaries: DiWorkerUsage[] }>(`/admin/compute/usage?period=${period}&start=${start}&end=${end}`)
    .then(d => d.summaries || [])
    .catch(() => []);
}
