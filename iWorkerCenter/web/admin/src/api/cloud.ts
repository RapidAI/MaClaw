import { apiGet, apiPost, apiPut } from './client';

export interface CloudConfig {
  base_url: string;
  center_base_url?: string;
  registration_name?: string;
  registration_email?: string;
  cloud_control_mode?: string;
}

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

export function fetchCloudConfig(): Promise<CloudConfig> {
  return apiGet<CloudConfig>('/admin/cloud/config');
}

export function updateCloudConfig(config: CloudConfig): Promise<CloudConfig> {
  return apiPut<CloudConfig>('/admin/cloud/config', config);
}
