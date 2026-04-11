import { apiGet, apiPost } from './client';

export interface SecurityPolicy {
  id: string;
  name: string;
  rule_type: string;
  pattern: string;
  action: string;
  enabled: boolean;
  created_at: string;
}

export interface SecurityHit {
  id: string;
  policy_id: string;
  content_snippet: string;
  action_taken: string;
  created_at: string;
}

export function listPolicies(): Promise<SecurityPolicy[]> {
  return apiGet('/admin/security/policies');
}

export function createPolicy(data: Partial<SecurityPolicy>): Promise<SecurityPolicy> {
  return apiPost('/admin/security/policies', data);
}

export function listHits(): Promise<SecurityHit[]> {
  return apiGet('/admin/security/hits');
}
