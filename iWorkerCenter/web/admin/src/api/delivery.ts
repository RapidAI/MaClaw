import { apiGet, apiPost } from './client';

export interface ConfigBundle {
  id: string;
  version: number;
  content_type: string;
  payload: string;
  status: string;
  note: string;
  created_at: string;
  published_at?: string;
}

export function listBundles(): Promise<ConfigBundle[]> {
  return apiGet<{ bundles: ConfigBundle[] }>('/admin/config-bundles').then(d => d.bundles || []);
}

export function createBundle(data: { content_type?: string; payload: unknown; note?: string }): Promise<ConfigBundle> {
  return apiPost<ConfigBundle>('/admin/config-bundles', data);
}

export function publishBundle(id: string): Promise<void> {
  return apiPost(`/admin/config-bundles/${id}/publish`);
}
