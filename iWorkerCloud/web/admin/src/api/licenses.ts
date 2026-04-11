import { apiGet, apiPost } from './client';

export interface License {
  id: string;
  center_id: string;
  modules: string;
  type: string;
  is_long_term: boolean;
  expires_at: string;
  created_at: string;
  revoked_at?: string;
  certificate?: string;
}

export function listLicenses(): Promise<License[]> {
  return apiGet('/api/admin/licenses');
}

export function issueLicense(centerId: string, modules: string[], days: number): Promise<void> {
  return apiPost('/api/admin/licenses', { center_id: centerId, modules, days });
}

export function revokeLicense(id: string): Promise<void> {
  return apiPost(`/api/admin/licenses/${id}/revoke`);
}
