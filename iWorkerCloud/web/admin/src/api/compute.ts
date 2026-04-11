import { apiGet, apiPost, apiDelete } from './client';

export interface LLMProvider {
  id: string;
  name: string;
  base_url: string;
  protocol: string;
  user_agent: string;
  compute_type: string;
  model: string;
  enabled: boolean;
  priority: number;
  description: string;
  has_api_key: boolean;
  input_price_per_mtoken?: number;
  output_price_per_mtoken?: number;
}

export interface CenterPermission {
  center_id: string;
  company_name: string;
  compute_permission: boolean;
}

export interface CenterCostReport {
  center_id: string;
  center_name: string;
  total_input_tokens: number;
  total_output_tokens: number;
  total_tokens: number;
  input_cost: number;
  output_cost: number;
  total_cost: number;
}

export function listProviders(): Promise<LLMProvider[]> {
  return apiGet('/api/admin/compute/providers');
}

export function createProvider(data: Partial<LLMProvider> & { api_key?: string }): Promise<LLMProvider> {
  return apiPost('/api/admin/compute/providers', data);
}

export function deleteProvider(id: string): Promise<void> {
  return apiDelete(`/api/admin/compute/providers/${id}`);
}

export function testProvider(id: string): Promise<{ ok: boolean; latency_ms: number; error?: string }> {
  return apiPost(`/api/admin/compute/providers/${id}/test`);
}

export function listCenterPermissions(): Promise<CenterPermission[]> {
  return apiGet('/api/admin/compute/permissions');
}

export function toggleCenterPermission(centerId: string, enabled: boolean): Promise<void> {
  return apiPost(`/api/admin/compute/permissions/${centerId}`, { compute_permission: enabled });
}

export function listCenterCosts(period: string, start: string, end: string): Promise<CenterCostReport[]> {
  return apiGet(`/api/stats/center-costs?period=${period}&start=${start}&end=${end}`);
}
