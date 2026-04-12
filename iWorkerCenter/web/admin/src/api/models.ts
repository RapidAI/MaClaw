import { apiGet, apiPost } from './client';

export interface ModelEndpoint {
  id: string;
  name: string;
  protocol: string;
  base_url: string;
  model: string;
  priority: number;
  cost_tier: string;
  enabled: boolean;
}

export interface RoutingPolicy {
  id: string;
  name: string;
  work_type: string;
  cost_tier: string;
  enabled: boolean;
}

export function listEndpoints(): Promise<ModelEndpoint[]> {
  return apiGet<{ endpoints: ModelEndpoint[] }>('/admin/model-endpoints').then(d => d.endpoints || []);
}

export function createEndpoint(data: Partial<ModelEndpoint>): Promise<ModelEndpoint> {
  return apiPost('/admin/model-endpoints', data);
}

export function listRoutingPolicies(): Promise<RoutingPolicy[]> {
  return apiGet<{ policies: RoutingPolicy[] }>('/admin/model-routing-policies').then(d => d.policies || []);
}

export function createRoutingPolicy(data: Partial<RoutingPolicy>): Promise<RoutingPolicy> {
  return apiPost('/admin/model-routing-policies', data);
}
