import { apiDelete, apiGet, apiPost, apiPostText, apiPut } from './client';

export interface DiWorkerLDAPConfig {
  enabled: boolean;
  host: string;
  port: number;
  use_tls: boolean;
  base_dn: string;
  bind_fmt: string;
}

export interface DiWorkerAccount {
  id: string;
  username: string;
  identifier: string;
  expires_at?: string | null;
  disabled: boolean;
  created_at: string;
}

export interface DiWorkerAccountList {
  items: DiWorkerAccount[];
  total: number;
}

export function loadLDAPConfig(): Promise<DiWorkerLDAPConfig> {
  return apiGet('/admin/diworker-auth/ldap');
}

export function saveLDAPConfig(config: DiWorkerLDAPConfig): Promise<{ status: string }> {
  return apiPost('/admin/diworker-auth/ldap', config);
}

export function testLDAPLogin(username: string, password: string): Promise<{ success: boolean; error?: string }> {
  return apiPost('/admin/diworker-auth/ldap/test', { username, password });
}

export function listDiWorkerAccounts(limit = 100, offset = 0): Promise<DiWorkerAccountList> {
  return apiGet(`/admin/diworker-auth/accounts?limit=${limit}&offset=${offset}`);
}

export function createDiWorkerAccount(data: {
  username: string;
  password: string;
  identifier?: string;
  expiry_days?: number;
}): Promise<{ id: string; username: string }> {
  return apiPost('/admin/diworker-auth/accounts', data);
}

export function updateDiWorkerAccount(id: string, data: {
  username?: string;
  password?: string;
  identifier?: string;
  expiry_days?: number;
  disabled?: boolean;
}): Promise<{ status: string }> {
  return apiPut(`/admin/diworker-auth/accounts/${encodeURIComponent(id)}`, data);
}

export function deleteDiWorkerAccount(id: string): Promise<{ status: string }> {
  return apiDelete(`/admin/diworker-auth/accounts/${encodeURIComponent(id)}`);
}

export interface DiWorkerAccountImportResult {
  created: number;
  skipped: number;
  errors?: string[];
}

export function importDiWorkerAccounts(csvText: string): Promise<DiWorkerAccountImportResult> {
  return apiPostText('/admin/diworker-auth/import-csv', csvText, 'text/csv');
}
export interface DiWorkerAuthMethodStatus {
  method: string;
  label: string;
  enabled: boolean;
  implemented: boolean;
  status: string;
  description: string;
}

export interface DiWorkerOIDCConfig {
  enabled: boolean;
  issuer_url: string;
  client_id: string;
  client_secret?: string;
  redirect_url: string;
  scopes: string[];
  allowed_domains: string[];
}

export function listDiWorkerAuthMethods(): Promise<{ methods: DiWorkerAuthMethodStatus[] }> {
  return apiGet('/admin/diworker-auth/methods');
}

export function loadOIDCConfig(): Promise<DiWorkerOIDCConfig> {
  return apiGet('/admin/diworker-auth/oidc');
}

export function saveOIDCConfig(config: DiWorkerOIDCConfig): Promise<{ status: string; implemented: boolean }> {
  return apiPost('/admin/diworker-auth/oidc', config);
}