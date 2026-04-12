import { apiGet, apiPost } from './client';

export interface ConfigBundle {
  id: string;
  name: string;
  version: string;
  status: string;
  created_at: string;
}

export function listBundles(): Promise<ConfigBundle[]> {
  return apiGet<{ bundles: ConfigBundle[] }>('/admin/config-bundles').then(d => d.bundles || []);
}

export function createBundle(data: { name: string; version?: string }): Promise<ConfigBundle> {
  return apiPost('/admin/config-bundles', data);
}

export function publishBundle(id: string): Promise<void> {
  return apiPost(`/admin/config-bundles/${id}/publish`);
}
