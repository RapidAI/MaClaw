export type CloudConfig = {
  base_url: string;
  center_base_url: string;
  registration_name: string;
  registration_email: string;
  cloud_control_mode: string;
};


type RawCloudConfig = Partial<CloudConfig> & {
  BaseURL?: string;
  CenterBaseURL?: string;
  RegistrationName?: string;
  RegistrationEmail?: string;
  CloudControlMode?: string;
};

const normalizeCloudConfig = (raw: RawCloudConfig | null | undefined): CloudConfig => ({
  base_url: raw?.base_url || raw?.BaseURL || '',
  center_base_url: raw?.center_base_url || raw?.CenterBaseURL || '',
  registration_name: raw?.registration_name || raw?.RegistrationName || '',
  registration_email: raw?.registration_email || raw?.RegistrationEmail || '',
  cloud_control_mode: raw?.cloud_control_mode || raw?.CloudControlMode || 'cloud_managed',
});

const serializeCloudConfig = (config: CloudConfig): CloudConfig => ({
  base_url: config.base_url || '',
  center_base_url: config.center_base_url || '',
  registration_name: config.registration_name || '',
  registration_email: config.registration_email || '',
  cloud_control_mode: config.cloud_control_mode || 'cloud_managed',
});

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

export async function fetchCloudConfig() {
  const raw = await requestJSON<RawCloudConfig>('/admin/cloud/config');
  return normalizeCloudConfig(raw);
}

export async function saveCloudConfig(config: CloudConfig) {
  const raw = await requestJSON<RawCloudConfig>('/admin/cloud/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(serializeCloudConfig(config)),
  });
  return normalizeCloudConfig(raw);
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