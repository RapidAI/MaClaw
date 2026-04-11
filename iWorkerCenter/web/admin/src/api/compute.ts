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

export function getComputeStatus(): Promise<ComputeStatus> {
  return apiGet('/admin/compute/status');
}

export function listComputeProviders(): Promise<ComputeProvider[]> {
  return apiGet('/admin/compute/providers');
}

export function createComputeProvider(data: Partial<ComputeProvider> & { api_key?: string }): Promise<ComputeProvider> {
  return apiPost('/admin/compute/providers', data);
}

export function updateComputeProvider(id: string, data: Partial<ComputeProvider> & { api_key?: string }): Promise<ComputeProvider> {
  return apiPut(`/admin/compute/providers/${id}`, data);
}

export function deleteComputeProvider(id: string): Promise<void> {
  return apiDelete(`/admin/compute/providers/${id}`);
}

export function testComputeProvider(id: string): Promise<{ ok: boolean; latency_ms: number; error?: string }> {
  return apiPost(`/admin/compute/providers/${id}/test`);
}

export function syncFromCloud(): Promise<void> {
  return apiPost('/admin/compute/sync');
}

export function switchComputeSource(source: 'cloud' | 'local'): Promise<void> {
  return apiPost('/admin/compute/source', { source });
}

export function listDiWorkerUsage(period: string, start: string, end: string): Promise<DiWorkerUsage[]> {
  return apiGet(`/admin/compute/usage?period=${period}&start=${start}&end=${end}`);
}
