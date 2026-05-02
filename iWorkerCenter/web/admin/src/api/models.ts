import { apiGet, apiPost, apiPut } from './client';

export interface ModelEndpoint {
  id: string;
  name: string;
  protocol: string;
  base_url: string;
  api_key?: string;
  model: string;
  priority: number;
  cost_tier: string;
  features?: string;
  status: string;
  created_at?: string;
  updated_at?: string;
}

export interface RoutingPolicy {
  id: string;
  name: string;
  description: string;
  work_type: string;
  role_code: string;
  endpoint_id: string;
  fallback_mode: string;
  priority: number;
  status: string;
}

export function listEndpoints(): Promise<ModelEndpoint[]> {
  return apiGet<{ endpoints: ModelEndpoint[] }>('/admin/model-endpoints').then(d => d.endpoints || []);
}

export function createEndpoint(data: Partial<ModelEndpoint>): Promise<ModelEndpoint> {
  return apiPost('/admin/model-endpoints', data);
}

export function updateEndpoint(id: string, data: Partial<ModelEndpoint>): Promise<void> {
  return apiPut('/admin/model-endpoints/' + encodeURIComponent(id), data);
}

export function listRoutingPolicies(): Promise<RoutingPolicy[]> {
  return apiGet<{ policies: RoutingPolicy[] }>('/admin/model-routing-policies').then(d => d.policies || []);
}

export function createRoutingPolicy(data: Partial<RoutingPolicy>): Promise<RoutingPolicy> {
  return apiPost('/admin/model-routing-policies', data);
}

export function updateRoutingPolicy(id: string, data: Partial<RoutingPolicy>): Promise<void> {
  return apiPut('/admin/model-routing-policies/' + encodeURIComponent(id), data);
}
