import { apiGet, apiPost, apiDelete } from './client';

export interface Center {
  id: string;
  company_name: string;
  admin_email: string;
  admin_phone?: string;
  address?: string;
  legal_person?: string;
  status: string;
  created_at: string;
  last_heartbeat?: string;
}

export function listCenters(): Promise<Center[]> {
  return apiGet('/api/admin/centers');
}

export function confirmTrial(id: string): Promise<void> {
  return apiPost(`/api/admin/centers/${id}/confirm-trial`);
}

export function confirmManual(id: string, modules: string[], days: number): Promise<void> {
  return apiPost(`/api/admin/centers/${id}/confirm`, { modules, days });
}

export function disableCenter(id: string): Promise<void> {
  return apiPost(`/api/admin/centers/${id}/disable`);
}

export function enableCenter(id: string): Promise<void> {
  return apiPost(`/api/admin/centers/${id}/enable`);
}

export function deleteCenter(id: string): Promise<void> {
  return apiDelete(`/api/admin/centers/${id}`);
}
