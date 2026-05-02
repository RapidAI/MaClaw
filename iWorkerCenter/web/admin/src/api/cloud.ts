import { apiGet, apiPost } from './client';

export interface CloudRegisterResponse {
  center_id: string;
  status: string;
  message?: string;
}

export interface CloudLicense {
  id: string;
  center_id: string;
  modules: string;
  type: string;
  expires_at: string;
  is_long_term: boolean;
  certificate: string;
  created_at: string;
  revoked_at?: string;
}

export interface CloudRegistrationStatus {
  configured: boolean;
  registered: boolean;
  center_id?: string;
  status: 'licensed' | 'pending' | 'unregistered' | 'not_configured' | 'offline' | 'error' | string;
  license?: CloudLicense;
  license_error?: string;
  non_blocking: boolean;
  control_plane: string;
  business_scope: string;
}

export function registerCenterToCloud(): Promise<CloudRegisterResponse> {
  return apiPost<CloudRegisterResponse>('/admin/cloud/register');
}

export function fetchCloudLicense(): Promise<CloudLicense> {
  return apiGet<CloudLicense>('/admin/cloud/license');
}

export function fetchCloudStatus(): Promise<CloudRegistrationStatus> {
  return apiGet<CloudRegistrationStatus>('/admin/cloud/status');
}
