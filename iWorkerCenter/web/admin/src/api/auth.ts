import { apiGet, apiPost } from './client';

export interface TenantStatus {
  needs_setup: boolean;
  tenant_id?: string;
}

export interface LoginResult {
  status: string;
  username: string;
  email?: string;
  tenant_id: string;
}

export function checkTenantStatus(): Promise<TenantStatus> {
  return apiGet('/auth/tenant-status');
}

export function checkSession(): Promise<{ ok: boolean }> {
  return apiGet('/auth/check');
}

export function login(username: string, password: string): Promise<LoginResult> {
  return apiPost('/auth/login', { username, password });
}

export function setupTenant(data: { company_name: string; email: string; admin_username: string; admin_password: string }): Promise<void> {
  return apiPost('/auth/setup-tenant', data);
}
