export type DiWorkerTab = 'home' | 'colleagues' | 'new-task' | 'history' | 'settings';

export type iWorkerTab = DiWorkerTab;

export type Colleague = {
  id: string;
  name: string;
  role: string;
  description: string;
  strengths: string[];
  tasks: string[];
};

export type RoleProfile = {
  name: string;
  description: string;
};

export type CenterConfig = {
  enabled: boolean;
  host: string;
  port: number;
  baseUrl: string;
  tenantId: string;
  departmentId: string;
  workerId: string;
  timeoutSec: number;
};

export type CenterHealthStatus = {
  reachable: boolean;
  status: string;
  providerCount: number;
  configPath: string;
  message: string;
  resolvedBaseUrl: string;
  checkedAt: string;
  source: 'manual' | 'auto-after-save';
};


export type WorkerMemoryStats = {
  tenantId: string;
  departmentId: string;
  workerId: string;
  total: number;
  byScope: Record<string, number>;
  byCategory: Record<string, number>;
  visibleScopes: string[];
};
export type RoutingPolicy = {
  mode: 'smart' | 'priority' | 'manual';
  defaultProvider: string;
  allowFallback: boolean;
};

export type ProviderCapabilities = {
  supportsStream: boolean;
  supportsVision: boolean;
  maxContext: number;
};

export type UpstreamProvider = {
  id: string;
  name: string;
  enabled: boolean;
  protocol: 'openai' | 'anthropic';
  baseUrl: string;
  apiKey: string;
  model: string;
  priority: number;
  features: string[];
  description: string;
  capabilities: ProviderCapabilities;
};

export type DiWorkerSettings = {
  roleProfile: RoleProfile;
  center: CenterConfig;
  routing: RoutingPolicy;
  providers: UpstreamProvider[];
};

export type iWorkerSettings = DiWorkerSettings;

export type TaskItem = {
  id: string;
  title: string;
  owner: string;
  status: string;
  updatedAt: string;
  description: string;
};

export type SubmitTaskRequest = {
  task_type: string;
  selected_colleague_name: string;
  draft: string;
  expected_output: string;
};

export type SubmitTaskResult = {
  task_type: string;
  colleague_name: string;
  expected_output: string;
  model: string;
  content: string;
};

export type TaskAttachment = {
  id: string;
  name: string;
  type: string;
  sizeLabel: string;
  isText: boolean;
  summary: string;
  content: string;
};

export type HistoryTaskItem = TaskItem & {
  draft?: string;
  expectedOutput?: string;
  result?: string;
  model?: string;
};
