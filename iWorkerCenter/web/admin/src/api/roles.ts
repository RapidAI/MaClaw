import { apiGet, apiPost } from './client';

export interface Role {
  id: string;
  code: string;
  name: string;
  description: string;
  created_at: string;
}

export function listRoles(): Promise<Role[]> {
  return apiGet('/admin/roles');
}

export function createRole(data: { code: string; name: string; description?: string }): Promise<Role> {
  return apiPost('/admin/roles', data);
}
