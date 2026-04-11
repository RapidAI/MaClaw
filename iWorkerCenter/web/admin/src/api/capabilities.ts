import { apiGet, apiPost } from './client';

export interface Capability {
  id: string;
  name: string;
  description: string;
  status: string;
  created_at: string;
}

export function listCapabilities(): Promise<Capability[]> {
  return apiGet('/admin/capabilities');
}

export function createCapability(data: { name: string; description?: string }): Promise<Capability> {
  return apiPost('/admin/capabilities', data);
}

export function approveCapability(id: string): Promise<void> {
  return apiPost(`/admin/capabilities/${id}/approve`);
}

export function rejectCapability(id: string): Promise<void> {
  return apiPost(`/admin/capabilities/${id}/reject`);
}

export function bindCapability(capabilityID: string, colleagueID: string): Promise<void> {
  return apiPost(`/admin/capabilities/${capabilityID}/bind`, { colleague_id: colleagueID });
}
