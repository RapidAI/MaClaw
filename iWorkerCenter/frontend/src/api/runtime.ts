export type ComputeSyncStatus = {
  status?: string;
  error?: string;
  non_blocking?: boolean;
  runtime_impact?: string;
  provider_count?: number;
  last_sync_at?: string;
};

export type CloudHeartbeatSnapshot = {
  configured?: boolean;
  status?: string;
  center_id?: string;
  last_attempt_at?: string;
  last_success_at?: string;
  last_error?: string;
  consecutive_failures?: number;
  runtime_type?: string;
  product_kind?: string;
  admin_console?: string;
  non_blocking?: boolean;
  business_impact?: string;
};

export type IWorkerReadinessStatus = {
  ready?: boolean;
  status?: string;
  tenant_count?: number;
  role_count?: number;
  colleague_count?: number;
  local_account_count?: number;
  agent_instance_count?: number;
  agent_runtime_ready?: boolean;
  goalwatch_ready?: boolean;
};

export type RuntimeStatus = {
  status?: string;
  runtime_type?: string;
  product_kind?: string;
  admin_console?: string;
  provider_count?: number;
  compute_source?: string;
  compute_permission?: boolean;
  cloud_provider_count?: number;
  runtime_provider_mode?: string;
  compute_sync_status?: ComputeSyncStatus;
  cloud_heartbeat?: CloudHeartbeatSnapshot;
  iworker_readiness?: IWorkerReadinessStatus;
};

async function requestJSON<T>(url: string): Promise<T> {
  const resp = await fetch(url);
  const text = await resp.text();
  let data: any = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = { message: text.trim() };
    }
  }
  if (!resp.ok) {
    throw new Error(data?.error || data?.message || `Request failed: ${resp.status}`);
  }
  return data as T;
}

export function fetchRuntimeStatus() {
  return requestJSON<RuntimeStatus>('/health');
}
