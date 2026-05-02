import { apiGet, apiPost, apiPut } from './client';

export interface SecurityPolicy {
  id: string;
  name: string;
  policy_type: string;
  description: string;
  rules: string;
  scope: string;
  priority: number;
  status: string;
  created_at: string;
  updated_at?: string;
}

export interface SecurityHit {
  id: string;
  policy_id: string;
  policy_name: string;
  actor_id: string;
  action: string;
  detail: string;
  created_at: string;
}

export function listPolicies(): Promise<SecurityPolicy[]> {
  return apiGet<{ policies: SecurityPolicy[] }>('/admin/security/policies').then(d => d.policies || []);
}

export function createPolicy(data: Partial<SecurityPolicy> & { rules?: unknown }): Promise<SecurityPolicy> {
  return apiPost('/admin/security/policies', data);
}

export function listHits(): Promise<SecurityHit[]> {
  return apiGet<{ hits: SecurityHit[] }>('/admin/security/hits').then(d => d.hits || []);
}

export function updatePolicy(id: string, data: Partial<SecurityPolicy> & { rules?: unknown }): Promise<void> {
  return apiPut('/admin/security/policies/' + encodeURIComponent(id), data);
}
