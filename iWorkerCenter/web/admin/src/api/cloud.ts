import { apiPost } from './client';

export interface CloudRegisterResponse {
  center_id: string;
  status: string;
  message?: string;
}

export function registerCenterToCloud(): Promise<CloudRegisterResponse> {
  return apiPost<CloudRegisterResponse>('/admin/cloud/register');
}
