export type WatcherPolicy = {
  enabled: boolean;
  single_flight: boolean;
  max_run_seconds: number;
  scale_by_worker_count: boolean;
};

export type BootstrapPlan = {
  tenant_id?: string;
  company_name: string;
  legal_person: string;
  company_address: string;
  contact_email: string;
  business_summary: string;
  priority: string;
  virtual_departments: string[];
  initial_iworkers: string[];
  memory_scopes: string[];
  recurring_tasks: string[];
  requires_executive_confirmation: string[];
  watcher_policy: WatcherPolicy;
  status?: string;
  updated_at?: string;
};

export type ValidationIssue = {
  field: string;
  message: string;
  level: 'error' | 'warning' | string;
};

export type FirstWaveTask = {
  id: string;
  title: string;
  owner_iworker: string;
  expected_output: string;
  memory_scope: string;
  escalation_threshold: string;
  requires_peer_review: boolean;
  recommended_trigger: string;
};

export type AppliedAsset = {
  kind: string;
  id: string;
  name: string;
  status: string;
};

export type BootstrapRun = {
  id: string;
  tenant_id: string;
  status: string;
  plan: BootstrapPlan;
  tasks: FirstWaveTask[];
  applied_assets: AppliedAsset[];
  created_at: string;
  updated_at: string;
};

export type BootstrapStatus = {
  tenant_id: string;
  has_plan: boolean;
  ready_to_start: boolean;
  plan?: BootstrapPlan;
  validation_issues: ValidationIssue[];
  last_run?: BootstrapRun;
  suggested_first_wave: FirstWaveTask[];
  applied_assets: AppliedAsset[];
};

export function isBootstrapComplete(status: BootstrapStatus | null | undefined) {
  if (!status) return false;
  return Boolean(status.last_run || (status.ready_to_start && (status.applied_assets?.length || 0) > 0));
}

async function requestJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
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
    throw new Error(data?.error?.message || data?.message || 'Request failed: ' + resp.status);
  }
  return data as T;
}

export const defaultBootstrapPlan = (): BootstrapPlan => ({
  company_name: '',
  legal_person: '',
  company_address: '',
  contact_email: '',
  business_summary: '',
  priority: 'Stabilize daily operations and customer delivery first, then let digital colleagues run inside auditable boundaries.',
  virtual_departments: ['Sales', 'Operations', 'Customer Success', 'Finance', 'Quality', 'Office', 'Data'],
  initial_iworkers: ['Office iWorker', 'Operations iWorker', 'Data iWorker', 'Quality iWorker'],
  memory_scopes: ['company', 'department', 'personal'],
  recurring_tasks: ['Daily operating brief', 'Customer exception scan', 'Weekly decision summary', 'Policy memory review'],
  requires_executive_confirmation: ['Business priorities', 'Risk boundaries', 'External communication rules'],
  watcher_policy: {
    enabled: true,
    single_flight: true,
    max_run_seconds: 120,
    scale_by_worker_count: true,
  },
});

export function fetchBootstrapStatus() {
  return requestJSON<BootstrapStatus>('/admin/bootstrap/status');
}

export function draftBootstrapPlan(plan: BootstrapPlan) {
  return requestJSON<{ plan: BootstrapPlan; validation_issues: ValidationIssue[]; suggested_first_wave: FirstWaveTask[] }>('/admin/bootstrap/draft-plan', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(plan),
  });
}

export function validateBootstrapPlan(plan: BootstrapPlan) {
  return requestJSON<{ plan: BootstrapPlan; ready_to_start: boolean; validation_issues: ValidationIssue[] }>('/admin/bootstrap/validate-plan', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(plan),
  });
}

export function applyBootstrapPlan(plan: BootstrapPlan) {
  return requestJSON<{ plan: BootstrapPlan; validation_issues: ValidationIssue[]; suggested_first_wave: FirstWaveTask[]; applied_assets: AppliedAsset[] }>('/admin/bootstrap/apply-plan', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(plan),
  });
}

export function startBootstrapFirstWave() {
  return requestJSON<{ run: BootstrapRun }>('/admin/bootstrap/start-first-wave', { method: 'POST' });
}
