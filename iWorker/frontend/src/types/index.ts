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
  goalWatchAutoHandleEnabled: boolean;
  goalWatchIntervalSec: number;
  goalWatchMaxDurationSec: number;
};


export type CenterTenantOption = {
  id: string;
  companyName: string;
};

export type CenterEnrollmentRole = {
  id: string;
  name: string;
  code: string;
  description: string;
  defaultStrengths: string[];
  applicableTasks: string[];
};

export type CenterEnrollmentColleague = {
  id: string;
  name: string;
  avatar: string;
  roleId: string;
  roleName: string;
  roleCode: string;
  description: string;
  strengths: string[];
  tasks: string[];
};

export type CenterAuthMethodStatus = {
  method: string;
  label: string;
  enabled: boolean;
  implemented: boolean;
  status: string;
  description: string;
};
export type CenterEnrollmentDiscovery = {
  baseUrl: string;
  selectedTenantId: string;
  tenants: CenterTenantOption[];
  roles: CenterEnrollmentRole[];
  colleagues: CenterEnrollmentColleague[];
  authMethods: CenterAuthMethodStatus[];
};

export type CenterHealthStatus = {
  reachable: boolean;
  status: string;
  providerCount: number;
  configPath: string;
  message: string;
  resolvedBaseUrl: string;
  iWorkerReadiness?: CenterIWorkerReadiness;
  checkedAt: string;
  source: 'manual' | 'auto-after-save';
};

export type CenterReadinessCheck = {
  name: string;
  ready: boolean;
  status: string;
  detail?: string;
  count?: number;
};

export type CenterAuthReadiness = {
  method: string;
  label: string;
  ready: boolean;
  implemented: boolean;
  status: string;
  detail?: string;
};

export type CenterIWorkerReadiness = {
  ready: boolean;
  status: string;
  tenantCount: number;
  roleCount: number;
  colleagueCount: number;
  localAccountCount: number;
  agentInstanceCount: number;
  agentRuntimeReady: boolean;
  goalWatchReady: boolean;
  requiredClientPaths: string[];
  checks: CenterReadinessCheck[];
  authMethods: CenterAuthReadiness[];
};


export type WorkerMemoryStats = {
  tenantId: string;
  departmentId: string;
  workerId: string;
  total: number;
  byScope: Record<string, number>;
  byCategory: Record<string, number>;
  visibleScopes: string[];
  source?: string;
  cachedAt?: string;
  stale?: boolean;
};

export type WorkerMemoryEntry = {
  id: string;
  tenantId: string;
  departmentId?: string;
  workerId?: string;
  scope: string;
  content: string;
  category: string;
  tags: string[];
  sourceType?: string;
  createdAt: string;
  updatedAt: string;
};

export type SaveWorkerMemoryRequest = {
  scope: string;
  content: string;
  category: string;
  tags: string[];
  sourceType: string;
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
  task_title?: string;
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


export type CenterRuntimeSkillEntry = {
  name: string;
  description: string;
  triggers: string[];
};

export type CenterRuntimeCapability = {
  capabilityId: string;
  name: string;
  source: string;
  version: string;
  riskLevel: string;
  entry: CenterRuntimeSkillEntry;
};

export type CenterMCPServer = {
  id: string;
  name: string;
  description: string;
  serverType: string;
  endpoint: string;
  command?: string;
  args: string[];
  envKeys: string[];
  departmentId: string;
  riskLevel: string;
  status: string;
  installedAt: string;
};

export type CenterInstalledTools = {
  skills: CenterRuntimeCapability[];
  mcpServers: CenterMCPServer[];
  source: string;
  cachedAt: string;
  stale: boolean;
};

export type CenterGoalPush = {
  eventId?: string;
  taskId: string;
  title: string;
  toColleagueId: string;
  toRoleCode: string;
  status: string;
  reason: string;
  recommendedAction: string;
  ageSeconds: number;
  executorStatus?: string;
  executorHeartbeatAgeSeconds?: number;
  createdAt: string;
  source?: string;
  cachedAt?: string;
  stale?: boolean;
};
export type CenterWorkStatusSummary = {
  currentTask?: string;
  currentDetail?: string;
  activeCount: number;
  completedCount: number;
  reviewCount: number;
  blockedCount: number;
  updatedAt?: string;
};

export type CenterAgentInstance = {
  tenantId: string;
  workerId: string;
  instanceId: string;
  role: string;
  status: string;
  orgUnitId?: string;
  capabilities: string[];
  memoryAuthority: string;
  localCacheMode: string;
  workStatus?: CenterWorkStatusSummary;
  hostId?: string;
  processId?: number;
  startedAt: string;
  lastHeartbeatAt: string;
  heartbeatAgeSeconds: number;
  effectiveStatus: string;
  source?: string;
  cachedAt?: string;
  stale?: boolean;
};

export type GoalWatchAutoHandleStatus = {
  enabled: boolean;
  running: boolean;
  currentRunId: number;
  runCount: number;
  skipCount: number;
  timeoutCancelCount: number;
  lastHandledCount: number;
  totalHandledCount: number;
  lastError: string;
  lastStartedAt: string;
  lastFinishedAt: string;
  lastTimeoutAt: string;
  intervalSeconds: number;
  maxDurationSeconds: number;
};
