import { apiGet, apiPost } from './client';

export interface Colleague {
  id: string;
  name: string;
  code: string;
  role_id: string;
  role_name: string;
  role_code: string;
  status: string;
  created_at: string;
}

export function listColleagues(): Promise<Colleague[]> {
  return apiGet<{ colleagues: Colleague[] }>('/admin/colleagues').then(d => d.colleagues || []);
}

export function createColleague(data: { name: string; code: string; role_id?: string }): Promise<Colleague> {
  return apiPost('/admin/colleagues', data);
}

export function assignRole(colleagueID: string, roleID: string, reason: string): Promise<void> {
  return apiPost(`/admin/colleagues/${colleagueID}/assign-role`, { role_id: roleID, reason });
}