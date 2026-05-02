export type CloudConfig = {
  base_url: string;
  center_base_url: string;
  registration_name: string;
  registration_email: string;
  cloud_control_mode: string;
};

export type CloudStatus = {
  configured: boolean;
  registered: boolean;
  center_id?: string;
  status: string;
  license_error?: string;
  non_blocking: boolean;
  control_plane: string;
  business_scope: string;
  license?: CloudLicense;
};

export type CloudLicense = {
  id: string;
  center_id: string;
  modules: string;
  type: string;
  expires_at: string;
  is_long_term: boolean;
  certificate: string;
  created_at: string;
};

export type RegisterCloudRequest = {
  company_name: string;
  legal_person?: string;
  admin_phone?: string;
  admin_email: string;
  address?: string;
};
export type RegisterCloudResponse = {
  center_id: string;
  status: string;
  message: string;
};

async function requestJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const text = await resp.text();
  const data = text ? JSON.parse(text) : null;
  if (!resp.ok) {
    throw new Error(data?.error || data?.message || `Request failed: ${resp.status}`);
  }
  return data as T;
}

export function fetchCloudConfig() {
  return requestJSON<CloudConfig>('/admin/cloud/config');
}

export function saveCloudConfig(config: CloudConfig) {
  return requestJSON<CloudConfig>('/admin/cloud/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  });
}

export function fetchCloudStatus() {
  return requestJSON<CloudStatus>('/admin/cloud/status');
}

export function registerCenterToCloud(body: RegisterCloudRequest) {
  return requestJSON<RegisterCloudResponse>('/admin/cloud/register', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

export function fetchCloudLicense() {
  return requestJSON<CloudLicense>('/admin/cloud/license');
}