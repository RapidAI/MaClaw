import { useEffect, useMemo, useState } from 'react';
import { AckGoalPush, ApplyCenterEnrollment, AutoHandleGoalPush, CheckCenterHealth, DeleteWorkerMemory, DiscoverCenterEnrollment, FetchAgentInstances, FetchGoalPushes, FetchInstalledTools, FetchWorkerMemoryStats, GetGoalWatchAutoHandleStatus, HeartbeatAgentRuntime, LoadDiWorkerSettings, LoadTaskHistory, RecallWorkerMemories, SaveDiWorkerSettings, SaveTaskHistory, SaveWorkerMemory, SubmitTask } from '../wailsjs/go/main/App';
import { main } from '../wailsjs/go/models';
import { colleagues } from './mock/colleagues';
import { SideNav } from './components/layout/SideNav';
import { ColleaguesPage } from './pages/ColleaguesPage';
import { HomePage } from './pages/HomePage';
import { NewTaskPage } from './pages/NewTaskPage';
import { SettingsPage } from './pages/SettingsPage';
import { TaskHistoryPage } from './pages/TaskHistoryPage';
import { recentTasks as mockRecentTasks } from './mock/tasks';
import type { CenterAgentInstance, CenterEnrollmentDiscovery, CenterGoalPush, CenterHealthStatus, CenterInstalledTools, DiWorkerSettings, GoalWatchAutoHandleStatus, DiWorkerTab, HistoryTaskItem, SubmitTaskRequest, SubmitTaskResult, SaveWorkerMemoryRequest, TaskAttachment, UpstreamProvider, WorkerMemoryEntry, WorkerMemoryStats } from './types';

const pageMeta: Record<DiWorkerTab, { title: string; subtitle: string }> = {
  home: { title: '新建任务', subtitle: '输入任务内容，快速开始处理。' },
  colleagues: { title: '同事', subtitle: '按分类浏览同事，可呼叫他们为你服务。' },
  'new-task': { title: '任务编辑', subtitle: '编辑任务内容、补充材料并提交处理。' },
  history: { title: '工具', subtitle: '赋予 iWorker 更强大的能力。' },
  settings: { title: '配置中心', subtitle: '管理角色信息、中心连接和上游服务调度。' },
};

const statusCopy: Record<DiWorkerTab, { focus: string }> = {
  home: { focus: '新建任务' },
  colleagues: { focus: '同事' },
  'new-task': { focus: '任务编辑' },
  history: { focus: '工具' },
  settings: { focus: '中心与路由配置' },
};

const hasWailsBridge = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

const defaultSettings: DiWorkerSettings = {
  roleProfile: {
    name: '小迪',
    description: '你的数字办公助理，擅长通知、纪要与汇报整理。',
  },
  center: {
    enabled: false,
    host: '127.0.0.1',
    port: 9377,
    baseUrl: 'http://127.0.0.1:9377',
    tenantId: 'default',
    departmentId: 'default',
    workerId: 'local-iworker',
    timeoutSec: 60,
    goalWatchAutoHandleEnabled: true,
    goalWatchIntervalSec: 30,
    goalWatchMaxDurationSec: 120,
  },
  routing: {
    mode: 'smart',
    defaultProvider: 'office-openai',
    allowFallback: true,
  },
  providers: [
    {
      id: 'office-openai',
      name: '办公写作服务',
      enabled: true,
      protocol: 'openai',
      baseUrl: 'https://office.example.com/v1',
      apiKey: '',
      model: 'gpt-4.1',
      priority: 100,
      features: ['公文', '纪要', '中文'],
      description: '适合通知、纪要、日报与正式文档。',
      capabilities: {
        supportsStream: true,
        supportsVision: false,
        maxContext: 128000,
      },
    },
    {
      id: 'analysis-anthropic',
      name: '分析归因服务',
      enabled: true,
      protocol: 'anthropic',
      baseUrl: 'https://analysis.example.com',
      apiKey: '',
      model: 'claude-sonnet-4-6',
      priority: 90,
      features: ['分析', '归因', '质量'],
      description: '适合异常说明、质量分析与整改建议。',
      capabilities: {
        supportsStream: true,
        supportsVision: false,
        maxContext: 200000,
      },
    },
  ],
};

const submitTaskViaBridge = async (payload: SubmitTaskRequest): Promise<SubmitTaskResult> => {
  return SubmitTask(payload as never) as Promise<SubmitTaskResult>;
};

const initialHistoryTasks: HistoryTaskItem[] = mockRecentTasks.map((task) => ({
  ...task,
  expectedOutput: 'summary',
}));

const fromWailsHistoryTask = (item: main.HistoryTaskItem): HistoryTaskItem => ({
  id: item.id,
  title: item.title,
  owner: item.owner,
  status: item.status,
  updatedAt: item.updated_at,
  description: item.description,
  draft: item.draft,
  expectedOutput: item.expected_output,
  result: item.result,
  model: item.model,
});

const toWailsHistoryTask = (item: HistoryTaskItem): main.HistoryTaskItem => new main.HistoryTaskItem({
  id: item.id,
  title: item.title,
  owner: item.owner,
  status: item.status,
  updated_at: item.updatedAt,
  description: item.description,
  draft: item.draft,
  expected_output: item.expectedOutput,
  result: item.result,
  model: item.model,
});

const fromWailsSettings = (item: main.DiWorkerSettings | null | undefined): DiWorkerSettings => ({
  roleProfile: {
    name: item?.role_profile?.name || defaultSettings.roleProfile.name,
    description: item?.role_profile?.description || defaultSettings.roleProfile.description,
  },
  center: {
    enabled: item?.center?.enabled ?? defaultSettings.center.enabled,
    host: item?.center?.host || defaultSettings.center.host,
    port: item?.center?.port || defaultSettings.center.port,
    baseUrl: item?.center?.base_url || defaultSettings.center.baseUrl,
    tenantId: item?.center?.tenant_id || defaultSettings.center.tenantId,
    departmentId: item?.center?.department_id || defaultSettings.center.departmentId,
    workerId: item?.center?.worker_id || defaultSettings.center.workerId,
    timeoutSec: item?.center?.timeout_sec || defaultSettings.center.timeoutSec,
    goalWatchAutoHandleEnabled: item?.center?.goalwatch_auto_handle_enabled ?? defaultSettings.center.goalWatchAutoHandleEnabled,
    goalWatchIntervalSec: item?.center?.goalwatch_interval_sec || defaultSettings.center.goalWatchIntervalSec,
    goalWatchMaxDurationSec: item?.center?.goalwatch_max_duration_sec || defaultSettings.center.goalWatchMaxDurationSec,
  },
  routing: {
    mode: (item?.routing?.mode as DiWorkerSettings['routing']['mode']) || defaultSettings.routing.mode,
    defaultProvider: item?.routing?.default_provider || defaultSettings.routing.defaultProvider,
    allowFallback: item?.routing?.allow_fallback ?? defaultSettings.routing.allowFallback,
  },
  providers: Array.isArray(item?.providers) && item.providers.length > 0
    ? item.providers.map((provider) => ({
      id: provider.id,
      name: provider.name,
      enabled: provider.enabled,
      protocol: provider.protocol as UpstreamProvider['protocol'],
      baseUrl: provider.base_url,
      apiKey: provider.api_key,
      model: provider.model,
      priority: provider.priority,
      features: provider.features || [],
      description: provider.description,
      capabilities: {
        supportsStream: provider.capabilities?.supports_stream ?? true,
        supportsVision: provider.capabilities?.supports_vision ?? false,
        maxContext: provider.capabilities?.max_context ?? 0,
      },
    }))
    : defaultSettings.providers,
});

const fromWailsMemoryStats = (item: main.WorkerMemoryStats | null | undefined): WorkerMemoryStats => ({
  tenantId: item?.tenant_id || '',
  departmentId: item?.department_id || '',
  workerId: item?.worker_id || '',
  total: item?.total || 0,
  byScope: item?.by_scope || {},
  byCategory: item?.by_category || {},
  visibleScopes: item?.visible_scopes || [],
});

const fromWailsMemoryEntry = (item: main.WorkerMemoryEntry): WorkerMemoryEntry => ({
  id: item.id,
  tenantId: item.tenant_id,
  departmentId: item.department_id,
  workerId: item.worker_id,
  scope: item.scope,
  content: item.content,
  category: item.category,
  tags: item.tags || [],
  sourceType: item.source_type,
  createdAt: item.created_at,
  updatedAt: item.updated_at,
});
type WailsAgentInstanceWithWorkStatus = main.CenterAgentInstance & {
  work_status?: {
    current_task?: string;
    current_detail?: string;
    active_count: number;
    completed_count: number;
    review_count: number;
    blocked_count: number;
    updated_at?: string;
  };
};

const fromWailsAgentInstance = (item: main.CenterAgentInstance): CenterAgentInstance => {
  const source = item as WailsAgentInstanceWithWorkStatus;
  return {
    tenantId: source.tenant_id,
    workerId: source.worker_id,
    instanceId: source.instance_id,
    role: source.role,
    status: source.status,
    orgUnitId: source.org_unit_id,
    capabilities: source.capabilities || [],
    memoryAuthority: source.memory_authority,
    localCacheMode: source.local_cache_mode,
    workStatus: source.work_status ? {
      currentTask: source.work_status.current_task,
      currentDetail: source.work_status.current_detail,
      activeCount: source.work_status.active_count || 0,
      completedCount: source.work_status.completed_count || 0,
      reviewCount: source.work_status.review_count || 0,
      blockedCount: source.work_status.blocked_count || 0,
      updatedAt: source.work_status.updated_at,
    } : undefined,
    hostId: source.host_id,
    processId: source.process_id,
    startedAt: source.started_at,
    lastHeartbeatAt: source.last_heartbeat_at,
    heartbeatAgeSeconds: source.heartbeat_age_seconds || 0,
    effectiveStatus: source.effective_status || source.status,
  };
};

const fromWailsGoalPush = (item: main.CenterGoalPush): CenterGoalPush => ({
  eventId: item.event_id,
  taskId: item.task_id,
  title: item.title,
  toColleagueId: item.to_colleague_id,
  toRoleCode: item.to_role_code,
  status: item.status,
  reason: item.reason,
  recommendedAction: item.recommended_action,
  ageSeconds: item.age_seconds || 0,
  executorStatus: item.executor_status,
  executorHeartbeatAgeSeconds: item.executor_heartbeat_age_seconds,
  createdAt: item.created_at,
});

const fromWailsGoalWatchAutoHandleStatus = (item: main.GoalWatchAutoHandleStatus): GoalWatchAutoHandleStatus => ({
  enabled: Boolean(item.enabled),
  running: Boolean(item.running),
  currentRunId: item.current_run_id || 0,
  runCount: item.run_count || 0,
  skipCount: item.skip_count || 0,
  timeoutCancelCount: item.timeout_cancel_count || 0,
  lastHandledCount: item.last_handled_count || 0,
  totalHandledCount: item.total_handled_count || 0,
  lastError: item.last_error || '',
  lastStartedAt: item.last_started_at || '',
  lastFinishedAt: item.last_finished_at || '',
  lastTimeoutAt: item.last_timeout_at || '',
  intervalSeconds: item.interval_seconds || 30,
  maxDurationSeconds: item.max_duration_seconds || 120,
});
type WailsEnrollmentDiscoveryWithAuth = main.CenterEnrollmentDiscovery & {
  auth_methods?: Array<{ method: string; label: string; enabled: boolean; implemented: boolean; status: string; description: string }>;
};

const fromWailsEnrollmentDiscovery = (item: main.CenterEnrollmentDiscovery | null | undefined): CenterEnrollmentDiscovery => {
  const source = item as WailsEnrollmentDiscoveryWithAuth | null | undefined;
  return {
    baseUrl: source?.base_url || '',
    selectedTenantId: source?.selected_tenant_id || '',
    tenants: (source?.tenants || []).map((tenant) => ({ id: tenant.id, companyName: tenant.company_name })),
    roles: (source?.roles || []).map((role) => ({ id: role.id, name: role.name, code: role.code, description: role.description, defaultStrengths: role.default_strengths || [], applicableTasks: role.applicable_tasks || [] })),
    colleagues: (source?.colleagues || []).map((colleague) => ({ id: colleague.id, name: colleague.name, avatar: colleague.avatar, roleId: colleague.role_id, roleName: colleague.role_name, roleCode: colleague.role_code, description: colleague.description, strengths: colleague.strengths || [], tasks: colleague.tasks || [] })),
    authMethods: (source?.auth_methods || []).map((method) => ({ method: method.method, label: method.label, enabled: method.enabled, implemented: method.implemented, status: method.status, description: method.description })),
  };
};
type WailsCenterHealthWithReadiness = main.CenterHealthStatus & {
  iworker_readiness?: {
    ready: boolean;
    status: string;
    tenant_count: number;
    role_count: number;
    colleague_count: number;
    local_account_count: number;
    agent_instance_count?: number;
    agent_runtime_ready: boolean;
    goalwatch_ready: boolean;
    required_client_paths?: string[];
    checks?: Array<{ name: string; ready: boolean; status: string; detail?: string; count?: number }>;
    auth_methods?: Array<{ method: string; label: string; ready: boolean; implemented: boolean; status: string; detail?: string }>;
  };
};

const fromWailsIWorkerReadiness = (item: WailsCenterHealthWithReadiness['iworker_readiness']): CenterHealthStatus['iWorkerReadiness'] | undefined => {
  if (!item) {
    return undefined;
  }
  return {
    ready: Boolean(item.ready),
    status: item.status || '',
    tenantCount: item.tenant_count || 0,
    roleCount: item.role_count || 0,
    colleagueCount: item.colleague_count || 0,
    localAccountCount: item.local_account_count || 0,
    agentInstanceCount: item.agent_instance_count || 0,
    agentRuntimeReady: Boolean(item.agent_runtime_ready),
    goalWatchReady: Boolean(item.goalwatch_ready),
    requiredClientPaths: item.required_client_paths || [],
    checks: (item.checks || []).map((check: { name: string; ready: boolean; status: string; detail?: string; count?: number }) => ({ name: check.name, ready: Boolean(check.ready), status: check.status, detail: check.detail, count: check.count })),
    authMethods: (item.auth_methods || []).map((method: { method: string; label: string; ready: boolean; implemented: boolean; status: string; detail?: string }) => ({ method: method.method, label: method.label, ready: Boolean(method.ready), implemented: Boolean(method.implemented), status: method.status, detail: method.detail })),
  };
};
const fromWailsCenterHealth = (
  item: main.CenterHealthStatus | null | undefined,
  source: CenterHealthStatus['source'],
): CenterHealthStatus => {
  const health = item as WailsCenterHealthWithReadiness | null | undefined;
  return {
    reachable: health?.reachable ?? false,
    status: health?.status || '',
    providerCount: health?.provider_count || 0,
    configPath: health?.config_path || '',
    message: health?.message || '',
    resolvedBaseUrl: health?.resolved_base_url || '',
    iWorkerReadiness: fromWailsIWorkerReadiness(health?.iworker_readiness),
    checkedAt: formatTimestamp(),
    source,
  };
};

const toWailsSettings = (item: DiWorkerSettings): main.DiWorkerSettings => new main.DiWorkerSettings({
  role_profile: new main.RoleProfile({
    name: item.roleProfile.name,
    description: item.roleProfile.description,
  }),
  center: new main.CenterConfig({
    enabled: item.center.enabled,
    host: item.center.host,
    port: item.center.port,
    base_url: item.center.baseUrl,
    tenant_id: item.center.tenantId,
    department_id: item.center.departmentId,
    worker_id: item.center.workerId,
    timeout_sec: item.center.timeoutSec,
    goalwatch_auto_handle_enabled: item.center.goalWatchAutoHandleEnabled,
    goalwatch_interval_sec: item.center.goalWatchIntervalSec,
    goalwatch_max_duration_sec: item.center.goalWatchMaxDurationSec,
  }),
  routing: new main.RoutingPolicy({
    mode: item.routing.mode,
    default_provider: item.routing.defaultProvider,
    allow_fallback: item.routing.allowFallback,
  }),
  providers: item.providers.map((provider) => new main.UpstreamProvider({
    id: provider.id,
    name: provider.name,
    enabled: provider.enabled,
    protocol: provider.protocol,
    base_url: provider.baseUrl,
    api_key: provider.apiKey,
    model: provider.model,
    priority: provider.priority,
    features: provider.features,
    description: provider.description,
    capabilities: new main.ProviderCapabilities({
      supports_stream: provider.capabilities.supportsStream,
      supports_vision: provider.capabilities.supportsVision,
      max_context: provider.capabilities.maxContext,
    }),
  })),
});

const createAttachmentId = (index: number) => `attachment-${Date.now()}-${index}`;

const formatFileSize = (size: number) => {
  if (size < 1024) {
    return `${size} B`;
  }
  if (size < 1024 * 1024) {
    return `${(size / 1024).toFixed(1)} KB`;
  }
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
};

const isTextFile = (file: File) => {
  return file.type.startsWith('text/') || /\.(txt|md|csv|json|log|yaml|yml|xml)$/i.test(file.name);
};

const buildAttachmentSummary = (content: string, isText: boolean) => {
  if (!isText) {
    return '非文本材料已上传，可结合文件类型和文件名一起处理。';
  }
  const normalized = content.replace(/\s+/g, ' ').trim();
  if (!normalized) {
    return '文本材料已上传，可结合文件内容一起处理。';
  }
  const excerpt = normalized.slice(0, 80);
  return normalized.length > 80 ? `${excerpt}...` : excerpt;
};

const buildAttachmentPayload = (item: TaskAttachment, index: number) => {
  const meta = `${index + 1}. ${item.name}（${item.type}，${item.sizeLabel}）`;
  return item.isText
    ? `${meta}：${item.summary}\n${item.content}`
    : `${meta}：${item.summary}`;
};

const readFileContent = async (file: File) => {
  if (!isTextFile(file)) {
    return '';
  }
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(typeof reader.result === 'string' ? reader.result : '');
    reader.onerror = () => reject(new Error(`读取材料失败：${file.name}`));
    reader.readAsText(file);
  });
};

const formatTimestamp = () => {
  const now = new Date();
  return `${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')} ${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`;
};

const settingsSnapshot = (item: DiWorkerSettings) => JSON.stringify(item);

const emptyInstalledTools: CenterInstalledTools = { skills: [], mcpServers: [] };

type WailsInstalledTools = {
  skills?: Array<{
    capability_id: string;
    name: string;
    source: string;
    version: string;
    risk_level: string;
    entry?: { name?: string; description?: string; triggers?: string[] };
  }>;
  mcp_servers?: Array<{
    id: string;
    name: string;
    description: string;
    server_type: string;
    endpoint: string;
    command?: string;
    args?: string[];
    env_keys?: string[];
    department_id: string;
    risk_level: string;
    status: string;
    installed_at: string;
  }>;
};

const fromWailsInstalledTools = (item: WailsInstalledTools | null | undefined): CenterInstalledTools => ({
  skills: (item?.skills || []).map((skill) => ({
    capabilityId: skill.capability_id,
    name: skill.name || skill.entry?.name || skill.capability_id,
    source: skill.source || 'iWorkerCenter',
    version: skill.version || '',
    riskLevel: skill.risk_level || 'low',
    entry: {
      name: skill.entry?.name || skill.name || '',
      description: skill.entry?.description || '',
      triggers: skill.entry?.triggers || [],
    },
  })),
  mcpServers: (item?.mcp_servers || []).map((server) => ({
    id: server.id,
    name: server.name,
    description: server.description,
    serverType: server.server_type,
    endpoint: server.endpoint,
    command: server.command,
    args: server.args || [],
    envKeys: server.env_keys || [],
    departmentId: server.department_id || 'all',
    riskLevel: server.risk_level || 'medium',
    status: server.status || 'enabled',
    installedAt: server.installed_at || '',
  })),
});


export default function App() {
  const [activeTab, setActiveTab] = useState<DiWorkerTab>('home');
  const [selectedTask, setSelectedTask] = useState('');
  const [selectedColleagueName, setSelectedColleagueName] = useState('');
  const [draft, setDraft] = useState('');
  const [expectedOutput, setExpectedOutput] = useState('summary');
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState('');
  const [submitResult, setSubmitResult] = useState<SubmitTaskResult | null>(null);
  const [attachments, setAttachments] = useState<TaskAttachment[]>([]);
  const [historyTasks, setHistoryTasks] = useState<HistoryTaskItem[]>(initialHistoryTasks);
  const [viewedHistoryTask, setViewedHistoryTask] = useState<HistoryTaskItem | null>(null);
  const [settings, setSettings] = useState<DiWorkerSettings>(defaultSettings);
  const [savedSettingsSnapshot, setSavedSettingsSnapshot] = useState(() => settingsSnapshot(defaultSettings));
  const [settingsLoading, setSettingsLoading] = useState(false);
  const [settingsSaving, setSettingsSaving] = useState(false);
  const [settingsError, setSettingsError] = useState('');
  const [settingsSaveMessage, setSettingsSaveMessage] = useState('');
  const [centerHealthChecking, setCenterHealthChecking] = useState(false);
  const [centerHealthStatus, setCenterHealthStatus] = useState<CenterHealthStatus | null>(null);
  const [centerHealthError, setCenterHealthError] = useState('');
  const [centerEnrollmentDiscovery, setCenterEnrollmentDiscovery] = useState<CenterEnrollmentDiscovery | null>(null);
  const [centerEnrollmentDiscovering, setCenterEnrollmentDiscovering] = useState(false);
  const [centerEnrollmentApplyingId, setCenterEnrollmentApplyingId] = useState('');
  const [centerEnrollmentMessage, setCenterEnrollmentMessage] = useState('');
  const [centerEnrollmentError, setCenterEnrollmentError] = useState('');
  const [workerMemoryStats, setWorkerMemoryStats] = useState<WorkerMemoryStats | null>(null);
  const [workerMemoryStatsLoading, setWorkerMemoryStatsLoading] = useState(false);
  const [workerMemoryStatsError, setWorkerMemoryStatsError] = useState('');
  const [workerMemoryDraftScope, setWorkerMemoryDraftScope] = useState('personal');
  const [workerMemoryDraftContent, setWorkerMemoryDraftContent] = useState('');
  const [workerMemoryDraftCategory, setWorkerMemoryDraftCategory] = useState('note');
  const [workerMemoryDraftTags, setWorkerMemoryDraftTags] = useState('');
  const [workerMemorySaving, setWorkerMemorySaving] = useState(false);
  const [workerMemorySaveMessage, setWorkerMemorySaveMessage] = useState('');
  const [workerMemorySaveError, setWorkerMemorySaveError] = useState('');
  const [workerMemoryRecallQuery, setWorkerMemoryRecallQuery] = useState('');
  const [workerMemoryRecallItems, setWorkerMemoryRecallItems] = useState<WorkerMemoryEntry[]>([]);
  const [workerMemoryRecallLoading, setWorkerMemoryRecallLoading] = useState(false);
  const [workerMemoryRecallError, setWorkerMemoryRecallError] = useState('');
  const [workerMemoryDeletingId, setWorkerMemoryDeletingId] = useState('');
  const [workerMemoryDeleteError, setWorkerMemoryDeleteError] = useState('');
  const [goalPushes, setGoalPushes] = useState<CenterGoalPush[]>([]);
  const [goalPushLoading, setGoalPushLoading] = useState(false);
  const [goalPushError, setGoalPushError] = useState('');
  const [goalPushAckingId, setGoalPushAckingId] = useState('');
  const [agentInstances, setAgentInstances] = useState<CenterAgentInstance[]>([]);
  const [agentInstancesLoading, setAgentInstancesLoading] = useState(false);
  const [agentInstancesError, setAgentInstancesError] = useState('');
  const [goalWatchAutoStatus, setGoalWatchAutoStatus] = useState<GoalWatchAutoHandleStatus | null>(null);
  const [installedTools, setInstalledTools] = useState<CenterInstalledTools>(emptyInstalledTools);
  const [installedToolsLoading, setInstalledToolsLoading] = useState(false);
  const [installedToolsError, setInstalledToolsError] = useState('');

  useEffect(() => {
    if (!hasWailsBridge()) {
      return;
    }
    void LoadTaskHistory()
      .then((items) => {
        if (Array.isArray(items) && items.length > 0) {
          setHistoryTasks(items.map(fromWailsHistoryTask));
        }
      })
      .catch(() => undefined);
  }, []);

  useEffect(() => {
    if (!hasWailsBridge()) {
      return;
    }
    setSettingsLoading(true);
    void LoadDiWorkerSettings()
      .then((value) => {
        const nextSettings = fromWailsSettings(value);
        setSettings(nextSettings);
        setSavedSettingsSnapshot(settingsSnapshot(nextSettings));
      })
      .catch(() => undefined)
      .finally(() => {
        setSettingsLoading(false);
      });
  }, []);

  const refreshInstalledTools = async () => {
    if (!hasWailsBridge() || !settings.center.enabled) {
      setInstalledTools(emptyInstalledTools);
      setInstalledToolsError('');
      return;
    }
    setInstalledToolsLoading(true);
    setInstalledToolsError('');
    try {
      const tools = await FetchInstalledTools();
      setInstalledTools(fromWailsInstalledTools(tools as unknown as WailsInstalledTools));
    } catch (error) {
      setInstalledToolsError(error instanceof Error ? error.message : 'Failed to fetch installed tools.');
    } finally {
      setInstalledToolsLoading(false);
    }
  };

  useEffect(() => {
    if (!hasWailsBridge() || !settings.center.enabled) {
      setInstalledTools(emptyInstalledTools);
      return;
    }
    void refreshInstalledTools();
  }, [settings.center.enabled, settings.center.workerId, settings.center.tenantId, settings.center.departmentId, settings.center.baseUrl]);

  const refreshGoalWatchAutoStatus = async () => {
    if (!hasWailsBridge()) {
      setGoalWatchAutoStatus(null);
      return;
    }
    try {
      const status = await GetGoalWatchAutoHandleStatus();
      setGoalWatchAutoStatus(fromWailsGoalWatchAutoHandleStatus(status));
    } catch {
      setGoalWatchAutoStatus(null);
    }
  };

  useEffect(() => {
    if (!hasWailsBridge()) {
      return;
    }
    void refreshGoalWatchAutoStatus();
    const timer = window.setInterval(() => {
      void refreshGoalWatchAutoStatus();
    }, 5000);
    return () => window.clearInterval(timer);
  }, []);
  const refreshAgentInstances = async () => {
    if (!hasWailsBridge() || !settings.center.enabled) {
      setAgentInstances([]);
      setAgentInstancesError('');
      return;
    }
    setAgentInstancesLoading(true);
    setAgentInstancesError('');
    try {
      await HeartbeatAgentRuntime();
      const instances = await FetchAgentInstances();
      setAgentInstances((instances || []).map(fromWailsAgentInstance));
    } catch (error) {
      setAgentInstancesError(error instanceof Error ? error.message : 'Failed to sync agent runtime.');
    } finally {
      setAgentInstancesLoading(false);
    }
  };

  useEffect(() => {
    if (!hasWailsBridge() || !settings.center.enabled) {
      setAgentInstances([]);
      return;
    }
    void refreshAgentInstances();
    const timer = window.setInterval(() => {
      void refreshAgentInstances();
      void refreshGoalWatchAutoStatus();
      void refreshInstalledTools();
    }, 30000);
    return () => window.clearInterval(timer);
  }, [settings.center.enabled, settings.center.workerId, settings.center.tenantId, settings.center.baseUrl, settings.center.goalWatchAutoHandleEnabled, settings.center.goalWatchIntervalSec, settings.center.goalWatchMaxDurationSec]);

  const refreshGoalPushes = async () => {
    if (!hasWailsBridge() || !settings.center.enabled) {
      setGoalPushes([]);
      setGoalPushError('');
      return;
    }
    setGoalPushLoading(true);
    setGoalPushError('');
    try {
      const pushes = await FetchGoalPushes(20);
      setGoalPushes((pushes || []).map(fromWailsGoalPush));
    } catch (error) {
      setGoalPushError(error instanceof Error ? error.message : 'Failed to fetch GoalWatch pushes.');
    } finally {
      setGoalPushLoading(false);
    }
  };

  useEffect(() => {
    if (!hasWailsBridge() || !settings.center.enabled) {
      setGoalPushes([]);
      return;
    }
    void refreshGoalPushes();
    const timer = window.setInterval(() => {
      void refreshGoalPushes();
    }, 30000);
    return () => window.clearInterval(timer);
  }, [settings.center.enabled, settings.center.workerId, settings.center.tenantId, settings.center.baseUrl, settings.center.goalWatchAutoHandleEnabled, settings.center.goalWatchIntervalSec, settings.center.goalWatchMaxDurationSec]);

  const handleAutoHandleGoalPush = async (eventId: string) => {
    if (!eventId || !hasWailsBridge()) {
      return;
    }
    setGoalPushAckingId(eventId);
    setGoalPushError('');
    try {
      await AutoHandleGoalPush(eventId);
      setGoalPushes((items) => items.filter((item) => item.eventId !== eventId));
      void refreshAgentInstances();
      void refreshGoalWatchAutoStatus();
    } catch (error) {
      setGoalPushError(error instanceof Error ? error.message : 'Failed to auto-handle GoalWatch push.');
    } finally {
      setGoalPushAckingId('');
    }
  };

  const handleAckGoalPush = async (eventId: string, status: 'resumed' | 'blocked') => {
    if (!eventId || !hasWailsBridge()) {
      return;
    }
    setGoalPushAckingId(eventId);
    setGoalPushError('');
    try {
      await AckGoalPush(eventId, status, status === 'resumed' ? 'interaction agent confirmed resume' : 'interaction agent reported blocked');
      setGoalPushes((items) => items.filter((item) => item.eventId !== eventId));
    } catch (error) {
      setGoalPushError(error instanceof Error ? error.message : 'Failed to acknowledge GoalWatch push.');
    } finally {
      setGoalPushAckingId('');
    }
  };

  const persistHistoryTasks = async (items: HistoryTaskItem[]) => {
    if (!hasWailsBridge()) {
      return;
    }
    await SaveTaskHistory(items.map(toWailsHistoryTask));
  };

  const handlePickTask = (task: string, colleagueName?: string) => {
    setSelectedTask(task);
    if (colleagueName) {
      setSelectedColleagueName(colleagueName);
    }
    setAttachments([]);
    setViewedHistoryTask(null);
    setSubmitResult(null);
    setSubmitError('');
    setActiveTab('new-task');
  };

  const handleOpenNewTask = () => {
    setAttachments([]);
    setViewedHistoryTask(null);
    setSubmitResult(null);
    setSubmitError('');
    setActiveTab('new-task');
  };

  const handleAddAttachment = async (files: FileList | null) => {
    if (!files || files.length === 0) {
      return;
    }
    try {
      const nextAttachments = await Promise.all(Array.from(files).map(async (file, index) => {
        const content = await readFileContent(file);
        const textFile = isTextFile(file);
        return {
          id: createAttachmentId(index),
          name: file.name,
          type: file.type || '未知类型',
          sizeLabel: formatFileSize(file.size),
          isText: textFile,
          summary: buildAttachmentSummary(content, textFile),
          content,
        };
      }));
      setAttachments((current) => [...current, ...nextAttachments]);
      setSubmitError('');
      setSubmitResult(null);
    } catch (error) {
      setSubmitError(error instanceof Error ? error.message : '璇诲彇鏉愭枡澶辫触锛岃绋嶅悗鍐嶈瘯');
    }
  };

  const handleApplySuggestion = (suggestion: string) => {
    setDraft((current) => current.trim() ? `${current.trim()}\n\n${suggestion}` : suggestion);
    setSubmitError('');
    setSubmitResult(null);
  };

  const handleRemoveAttachment = (attachmentId: string) => {
    setAttachments((current) => current.filter((item) => item.id !== attachmentId));
    setSubmitError('');
    setSubmitResult(null);
  };

  const handleSwitchColleague = () => {
    setActiveTab('colleagues');
  };

  const handleOpenHistoryTask = (task: HistoryTaskItem) => {
    setSelectedTask(task.title);
    setSelectedColleagueName(task.owner);
    setDraft(task.draft || task.description);
    setExpectedOutput(task.expectedOutput || 'summary');
    setAttachments([]);
    setSubmitError('');
    setViewedHistoryTask(null);
    setSubmitResult(task.result ? {
      task_type: task.title,
      colleague_name: task.owner,
      expected_output: task.expectedOutput || 'summary',
      model: task.model || '',
      content: task.result,
    } : null);
    setActiveTab('new-task');
  };

  const handleViewHistoryResult = (task: HistoryTaskItem) => {
    setViewedHistoryTask(task);
    setAttachments([]);
    setSubmitResult(null);
    setSubmitError('');
    setActiveTab('history');
  };

  const handleCloneHistoryTask = (task: HistoryTaskItem) => {
    setSelectedTask(task.title);
    setSelectedColleagueName('');
    setDraft(task.draft || task.description);
    setExpectedOutput(task.expectedOutput || 'summary');
    setAttachments([]);
    setViewedHistoryTask(null);
    setSubmitResult(null);
    setSubmitError('');
    setActiveTab('new-task');
  };

  const handleDeleteHistoryTask = async (task: HistoryTaskItem) => {
    const nextHistory = historyTasks.filter((item) => item.id !== task.id);
    setHistoryTasks(nextHistory);
    setViewedHistoryTask((current) => current?.id === task.id ? null : current);
    await persistHistoryTasks(nextHistory);
  };

  const handleClearTask = () => {
    setSelectedTask('');
    setSelectedColleagueName('');
    setDraft('');
    setExpectedOutput('summary');
    setAttachments([]);
    setViewedHistoryTask(null);
    setSubmitResult(null);
    setSubmitError('');
  };

  const handleSubmitTask = async () => {
    const attachmentSummary = attachments.length > 0
      ? `\n\n补充材料：\n${attachments.map(buildAttachmentPayload).join('\n\n')}`
      : '';
    const effectiveDraft = `${draft}${attachmentSummary}`.trim();
    const payload: SubmitTaskRequest = {
      task_type: selectedTask || '自由输入',
      selected_colleague_name: selectedColleagueName,
      draft: effectiveDraft,
      expected_output: expectedOutput,
    };

    setSubmitting(true);
    setSubmitError('');
    setSubmitResult(null);
    try {
      const result = await submitTaskViaBridge(payload);
      setSubmitResult(result);
      const nextHistory = [
        {
          id: `task-${Date.now()}`,
          title: result.task_title || result.task_type,
          owner: result.colleague_name,
          status: '已完成',
          updatedAt: formatTimestamp(),
          description: draft.trim() || result.content.slice(0, 60),
          draft: effectiveDraft,
          expectedOutput,
          result: result.content,
          model: result.model,
        },
        ...historyTasks,
      ].slice(0, 8);
      setViewedHistoryTask(nextHistory[0]);
      setHistoryTasks(nextHistory);
      await persistHistoryTasks(nextHistory);
      setActiveTab('history');
    } catch (error) {
      setSubmitError(error instanceof Error ? error.message : '鎻愪氦澶辫触锛岃绋嶅悗鍐嶈瘯');
    } finally {
      setSubmitting(false);
    }
  };

  const handleRefreshWorkerMemoryStats = async () => {
    setWorkerMemoryStatsError('');
    if (!hasWailsBridge()) {
      setWorkerMemoryStats(null);
      setWorkerMemoryStatsError('Wails bridge is not connected.');
      return;
    }
    setWorkerMemoryStatsLoading(true);
    try {
      const stats = await FetchWorkerMemoryStats();
      setWorkerMemoryStats(fromWailsMemoryStats(stats as main.WorkerMemoryStats));
    } catch (error) {
      setWorkerMemoryStats(null);
      setWorkerMemoryStatsError(error instanceof Error ? error.message : 'Failed to load memory stats');
    } finally {
      setWorkerMemoryStatsLoading(false);
    }
  };


  const handleSaveWorkerMemory = async () => {
    setWorkerMemorySaveMessage('');
    setWorkerMemorySaveError('');
    const content = workerMemoryDraftContent.trim();
    if (!content) {
      setWorkerMemorySaveError('Memory content is required.');
      return;
    }
    if (!hasWailsBridge()) {
      setWorkerMemorySaveError('Wails bridge is not connected.');
      return;
    }
    const payload: SaveWorkerMemoryRequest = {
      scope: workerMemoryDraftScope,
      content,
      category: workerMemoryDraftCategory.trim() || 'note',
      tags: workerMemoryDraftTags.split(/[，,]/).map((item) => item.trim()).filter(Boolean),
      sourceType: 'iworker-gui',
    };
    setWorkerMemorySaving(true);
    try {
      await SaveWorkerMemory(new main.SaveWorkerMemoryRequest({
        scope: payload.scope,
        content: payload.content,
        category: payload.category,
        tags: payload.tags,
        source_type: payload.sourceType,
      }) as never);
      setWorkerMemoryDraftContent('');
      setWorkerMemorySaveMessage('Memory saved to iWorkerCenter.');
      void handleRefreshWorkerMemoryStats();
      void handleRecallWorkerMemories();
    } catch (error) {
      setWorkerMemorySaveError(error instanceof Error ? error.message : 'Failed to save memory.');
    } finally {
      setWorkerMemorySaving(false);
    }
  };

  const handleRecallWorkerMemories = async () => {
    setWorkerMemoryRecallError('');
    if (!hasWailsBridge()) {
      setWorkerMemoryRecallItems([]);
      setWorkerMemoryRecallError('Wails bridge is not connected.');
      return;
    }
    setWorkerMemoryRecallLoading(true);
    try {
      const memories = await RecallWorkerMemories(workerMemoryRecallQuery.trim());
      setWorkerMemoryRecallItems(Array.isArray(memories) ? memories.map((item) => fromWailsMemoryEntry(item as main.WorkerMemoryEntry)) : []);
    } catch (error) {
      setWorkerMemoryRecallItems([]);
      setWorkerMemoryRecallError(error instanceof Error ? error.message : 'Failed to recall memories.');
    } finally {
      setWorkerMemoryRecallLoading(false);
    }
  };

  const handleDeleteWorkerMemory = async (memoryId: string) => {
    const id = memoryId.trim();
    setWorkerMemoryDeleteError('');
    if (!id) {
      setWorkerMemoryDeleteError('Memory id is required.');
      return;
    }
    if (!hasWailsBridge()) {
      setWorkerMemoryDeleteError('Wails bridge is not connected.');
      return;
    }
    setWorkerMemoryDeletingId(id);
    try {
      await DeleteWorkerMemory(id);
      setWorkerMemoryRecallItems((current) => current.filter((item) => item.id !== id));
      void handleRefreshWorkerMemoryStats();
    } catch (error) {
      setWorkerMemoryDeleteError(error instanceof Error ? error.message : 'Failed to delete memory.');
    } finally {
      setWorkerMemoryDeletingId('');
    }
  };
  const handleSaveSettings = async () => {
    setSettingsError('');
    setSettingsSaveMessage('');
    if (!hasWailsBridge()) {
      setSettingsSaveMessage('当前未连接 Wails，配置仅保留在当前界面。');
      return;
    }
    setSettingsSaving(true);
    try {
      await SaveDiWorkerSettings(toWailsSettings(settings) as never);
      setSavedSettingsSnapshot(settingsSnapshot(settings));
      setSettingsSaveMessage('配置已保存');
      try {
        const status = await CheckCenterHealth();
        setCenterHealthError('');
        setCenterHealthStatus(fromWailsCenterHealth(status as main.CenterHealthStatus, 'auto-after-save'));
        void handleRefreshWorkerMemoryStats();
      } catch (error) {
        setCenterHealthStatus(null);
        setCenterHealthError(error instanceof Error ? error.message : '中心连接检测失败');
      }
    } catch (error) {
      setSettingsError(error instanceof Error ? error.message : '淇濆瓨閰嶇疆澶辫触');
    } finally {
      setSettingsSaving(false);
    }
  };

  const handleDiscoverCenterEnrollment = async () => {
    setCenterEnrollmentError('');
    setCenterEnrollmentMessage('');
    setCenterEnrollmentDiscovery(null);
    if (!hasWailsBridge()) {
      setCenterEnrollmentError('Wails bridge is not connected.');
      return;
    }
    setCenterEnrollmentDiscovering(true);
    try {
      const discovery = await DiscoverCenterEnrollment(new main.CenterEnrollmentRequest({ base_url: settings.center.baseUrl, preferred_tenant_id: settings.center.tenantId, timeout_sec: settings.center.timeoutSec }));
      const next = fromWailsEnrollmentDiscovery(discovery as main.CenterEnrollmentDiscovery);
      setCenterEnrollmentDiscovery(next);
      setCenterEnrollmentMessage('Found ' + next.colleagues.length + ' iWorker candidates and ' + next.authMethods.length + ' auth methods in ' + (next.selectedTenantId || 'default') + '.');
    } catch (error) {
      setCenterEnrollmentError(error instanceof Error ? error.message : 'Center enrollment discovery failed.');
    } finally {
      setCenterEnrollmentDiscovering(false);
    }
  };

  const handleApplyCenterEnrollment = async (workerId: string, auth: { method: string; username: string; password: string }) => {
    const worker = centerEnrollmentDiscovery?.colleagues.find((item) => item.id === workerId);
    if (!worker || !centerEnrollmentDiscovery) {
      setCenterEnrollmentError('Please discover and select a Center iWorker first.');
      return;
    }
    if (!hasWailsBridge()) {
      setCenterEnrollmentError('Wails bridge is not connected.');
      return;
    }
    setCenterEnrollmentApplyingId(workerId);
    setCenterEnrollmentError('');
    setCenterEnrollmentMessage('Applying Center enrollment...');
    try {
      const applied = await ApplyCenterEnrollment(new main.ApplyCenterEnrollmentRequest({ base_url: centerEnrollmentDiscovery.baseUrl || settings.center.baseUrl, tenant_id: centerEnrollmentDiscovery.selectedTenantId || settings.center.tenantId, department_id: worker.roleCode || settings.center.departmentId, worker_id: worker.id, role_name: worker.name, role_description: worker.description || worker.roleName, timeout_sec: settings.center.timeoutSec, auth_method: auth.method, auth_username: auth.username, auth_password: auth.password }));
      const nextSettings = fromWailsSettings(applied as main.DiWorkerSettings);
      setSettings(nextSettings);
      setSavedSettingsSnapshot(settingsSnapshot(nextSettings));
      setCenterEnrollmentMessage('Bound to ' + worker.name + '. Local iWorker is ready to use iWorkerCenter memory and GoalWatcher.');
      try {
        const status = await CheckCenterHealth();
        setCenterHealthError('');
        setCenterHealthStatus(fromWailsCenterHealth(status as main.CenterHealthStatus, 'auto-after-save'));
        void handleRefreshWorkerMemoryStats();
        void refreshAgentInstances();
      } catch (error) {
        setCenterHealthStatus(null);
        setCenterHealthError(error instanceof Error ? error.message : 'Center health check failed after enrollment.');
      }
    } catch (error) {
      setCenterEnrollmentError(error instanceof Error ? error.message : 'Center enrollment apply failed.');
    } finally {
      setCenterEnrollmentApplyingId('');
    }
  };

  const handleCheckCenterHealth = async (): Promise<CenterHealthStatus | undefined> => {
    setCenterHealthError('');
    setCenterHealthStatus(null);
    if (!hasWailsBridge()) {
      setCenterHealthError('当前未连接 Wails，无法测试中心连接。');
      return undefined;
    }
    setCenterHealthChecking(true);
    try {
      const status = await CheckCenterHealth();
      const nextStatus = fromWailsCenterHealth(status as main.CenterHealthStatus, 'manual');
      setCenterHealthStatus(nextStatus);
      void handleRefreshWorkerMemoryStats();
      return nextStatus;
    } catch (error) {
      setCenterHealthError(error instanceof Error ? error.message : '中心连接检测失败');
      return undefined;
    } finally {
      setCenterHealthChecking(false);
    }
  };

  const updateSettings = (updater: (current: DiWorkerSettings) => DiWorkerSettings) => {
    setSettings((current) => updater(current));
    setSettingsSaveMessage('');
    setSettingsError('');
    setCenterHealthError('');
    setCenterHealthStatus(null);
  };

  const updateProvider = (providerId: string, patch: Partial<UpstreamProvider>) => {
    updateSettings((current) => ({
      ...current,
      providers: current.providers.map((provider) => provider.id === providerId ? { ...provider, ...patch } : provider),
    }));
  };

  const settingsDirty = settingsSnapshot(settings) !== savedSettingsSnapshot;

  const currentRole = useMemo(() => {
    const selected = colleagues.find((item) => item.name === selectedColleagueName);
    if (selected) {
      return {
        name: selected.name,
        description: selected.description,
      };
    }
    return settings.roleProfile;
  }, [selectedColleagueName, settings.roleProfile]);

  const page = useMemo(() => {
    switch (activeTab) {
      case 'colleagues':
        return <ColleaguesPage selectedColleagueName={selectedColleagueName} onPickColleagueTask={handlePickTask} />;
      case 'new-task':
        return (
          <NewTaskPage
            selectedTask={selectedTask}
            selectedColleagueName={selectedColleagueName}
            draft={draft}
            expectedOutput={expectedOutput}
            attachments={attachments}
            submitting={submitting}
            submitError={submitError}
            submitResult={submitResult}
            onDraftChange={setDraft}
            onExpectedOutputChange={setExpectedOutput}
            onApplySuggestion={handleApplySuggestion}
            onAddAttachment={handleAddAttachment}
            onRemoveAttachment={handleRemoveAttachment}
            onClearTask={handleClearTask}
            onSubmit={handleSubmitTask}
            onOpenColleagues={handleSwitchColleague}
          />
        );
      case 'history':
        return <TaskHistoryPage tasks={historyTasks} viewedTask={viewedHistoryTask} onResumeTask={handleOpenHistoryTask} onViewResult={handleViewHistoryResult} onCloneTask={handleCloneHistoryTask} onDeleteTask={handleDeleteHistoryTask} />;
      case 'settings':
        return (
          <SettingsPage
            settings={settings}
            loading={settingsLoading}
            saving={settingsSaving}
            dirty={settingsDirty}
            error={settingsError}
            saveMessage={settingsSaveMessage}
            healthChecking={centerHealthChecking}
            healthStatus={centerHealthStatus}
            healthError={centerHealthError}
            enrollmentDiscovery={centerEnrollmentDiscovery}
            enrollmentDiscovering={centerEnrollmentDiscovering}
            enrollmentApplyingId={centerEnrollmentApplyingId}
            enrollmentMessage={centerEnrollmentMessage}
            enrollmentError={centerEnrollmentError}
            memoryStats={workerMemoryStats}
            memoryStatsLoading={workerMemoryStatsLoading}
            memoryStatsError={workerMemoryStatsError}
            onRefreshMemoryStats={handleRefreshWorkerMemoryStats}
            memoryDraftScope={workerMemoryDraftScope}
            memoryDraftContent={workerMemoryDraftContent}
            memoryDraftCategory={workerMemoryDraftCategory}
            memoryDraftTags={workerMemoryDraftTags}
            memorySaving={workerMemorySaving}
            memorySaveMessage={workerMemorySaveMessage}
            memorySaveError={workerMemorySaveError}
            onMemoryDraftScopeChange={setWorkerMemoryDraftScope}
            onMemoryDraftContentChange={setWorkerMemoryDraftContent}
            onMemoryDraftCategoryChange={setWorkerMemoryDraftCategory}
            onMemoryDraftTagsChange={setWorkerMemoryDraftTags}
            onSaveWorkerMemory={handleSaveWorkerMemory}
            memoryRecallQuery={workerMemoryRecallQuery}
            memoryRecallItems={workerMemoryRecallItems}
            memoryRecallLoading={workerMemoryRecallLoading}
            memoryRecallError={workerMemoryRecallError}
            onMemoryRecallQueryChange={setWorkerMemoryRecallQuery}
            onRecallWorkerMemories={handleRecallWorkerMemories}
            memoryDeletingId={workerMemoryDeletingId}
            memoryDeleteError={workerMemoryDeleteError}
            onDeleteWorkerMemory={handleDeleteWorkerMemory}
            onRoleNameChange={(value) => updateSettings((current) => ({ ...current, roleProfile: { ...current.roleProfile, name: value } }))}
            onRoleDescriptionChange={(value) => updateSettings((current) => ({ ...current, roleProfile: { ...current.roleProfile, description: value } }))}
            onCenterEnabledChange={(value) => updateSettings((current) => ({ ...current, center: { ...current.center, enabled: value } }))}
            onCenterHostChange={(value) => updateSettings((current) => ({ ...current, center: { ...current.center, host: value } }))}
            onCenterPortChange={(value) => updateSettings((current) => ({ ...current, center: { ...current.center, port: Number(value) || 0 } }))}
            onCenterBaseUrlChange={(value) => updateSettings((current) => ({ ...current, center: { ...current.center, baseUrl: value } }))}
            onCenterTenantIdChange={(value) => updateSettings((current) => ({ ...current, center: { ...current.center, tenantId: value } }))}
            onCenterDepartmentIdChange={(value) => updateSettings((current) => ({ ...current, center: { ...current.center, departmentId: value } }))}
            onCenterWorkerIdChange={(value) => updateSettings((current) => ({ ...current, center: { ...current.center, workerId: value } }))}
            onCenterTimeoutChange={(value) => updateSettings((current) => ({ ...current, center: { ...current.center, timeoutSec: Number(value) || 0 } }))}
            onGoalWatchAutoHandleEnabledChange={(value) => updateSettings((current) => ({ ...current, center: { ...current.center, goalWatchAutoHandleEnabled: value } }))}
            onGoalWatchIntervalChange={(value) => updateSettings((current) => ({ ...current, center: { ...current.center, goalWatchIntervalSec: Number(value) || 0 } }))}
            onGoalWatchMaxDurationChange={(value) => updateSettings((current) => ({ ...current, center: { ...current.center, goalWatchMaxDurationSec: Number(value) || 0 } }))}
            onRoutingModeChange={(value) => updateSettings((current) => ({ ...current, routing: { ...current.routing, mode: value } }))}
            onRoutingDefaultProviderChange={(value) => updateSettings((current) => ({ ...current, routing: { ...current.routing, defaultProvider: value } }))}
            onRoutingAllowFallbackChange={(value) => updateSettings((current) => ({ ...current, routing: { ...current.routing, allowFallback: value } }))}
            onProviderChange={updateProvider}
            onProviderFeaturesChange={(providerId, value) => updateProvider(providerId, { features: value.split(/[，,]/).map((item) => item.trim()).filter(Boolean) })}
            onCheckCenterHealth={handleCheckCenterHealth}
            onDiscoverCenterEnrollment={handleDiscoverCenterEnrollment}
            onApplyCenterEnrollment={handleApplyCenterEnrollment}
            onSave={handleSaveSettings}
          />
        );
      case 'home':
      default:
        return <HomePage
          draft={draft}
          selectedTask={selectedTask}
          selectedColleagueName={selectedColleagueName}
          recentTasks={historyTasks}
          settings={settings}
          centerHealthStatus={centerHealthStatus}
          centerHealthError={centerHealthError}
          workerMemoryStats={workerMemoryStats}
          workerMemoryStatsLoading={workerMemoryStatsLoading}
          workerMemoryStatsError={workerMemoryStatsError}
          agentInstances={agentInstances}
          agentInstancesLoading={agentInstancesLoading}
          agentInstancesError={agentInstancesError}
          onRefreshAgentInstances={refreshAgentInstances}
          goalPushes={goalPushes}
          goalPushLoading={goalPushLoading}
          goalPushError={goalPushError}
          goalPushAckingId={goalPushAckingId}
          goalWatchAutoStatus={goalWatchAutoStatus}
          installedTools={installedTools}
          installedToolsLoading={installedToolsLoading}
          installedToolsError={installedToolsError}
          submitting={submitting}
          onRefreshGoalPushes={refreshGoalPushes}
          onRefreshMemoryStats={handleRefreshWorkerMemoryStats}
          onCheckCenterHealth={handleCheckCenterHealth}
          onAutoHandleGoalPush={handleAutoHandleGoalPush}
          onAckGoalPush={handleAckGoalPush}
          onDraftChange={setDraft}
          onExpectedOutputChange={setExpectedOutput}
          onPickTask={handlePickTask}
          onOpenNewTask={handleOpenNewTask}
          onOpenRecentTask={handleOpenHistoryTask}
          onOpenSettings={() => setActiveTab('settings')}
        />;
    }
  }, [activeTab, attachments, centerHealthChecking, centerHealthError, centerHealthStatus, workerMemoryStats, workerMemoryStatsLoading, workerMemoryStatsError, workerMemoryDraftScope, workerMemoryDraftContent, workerMemoryDraftCategory, workerMemoryDraftTags, workerMemorySaving, workerMemorySaveMessage, workerMemorySaveError, workerMemoryRecallQuery, workerMemoryRecallItems, workerMemoryRecallLoading, workerMemoryRecallError, workerMemoryDeletingId, workerMemoryDeleteError, agentInstances, agentInstancesLoading, agentInstancesError, goalPushes, goalPushLoading, goalPushError, goalPushAckingId, goalWatchAutoStatus, draft, expectedOutput, historyTasks, selectedColleagueName, selectedTask, settings, settingsError, settingsLoading, settingsSaveMessage, settingsSaving, submitError, submitResult, submitting, viewedHistoryTask, centerEnrollmentDiscovery, centerEnrollmentDiscovering, centerEnrollmentApplyingId, centerEnrollmentMessage, centerEnrollmentError]);

  const meta = pageMeta[activeTab];
  const status = statusCopy[activeTab];

  return (
    <div className="dw-shell">
      <SideNav activeTab={activeTab} roleName={currentRole.name} roleDescription={currentRole.description} recentTasks={historyTasks} onChange={setActiveTab} />
      <main className="dw-main">
        <div className="dw-main-shell">
          {activeTab !== 'home' && (
            <header className="dw-topbar">
              <div className="dw-topbar-main card">
                <div className="dw-topbar-row">
                  <div className="dw-topbar-window dw-topbar-window-compact">
                    <span className="dw-window-dot is-red" aria-hidden="true" />
                    <span className="dw-window-dot is-yellow" aria-hidden="true" />
                    <span className="dw-window-dot is-green" aria-hidden="true" />
                    <span className="dw-topbar-window-label">iWorker</span>
                  </div>
                  <div className="dw-topbar-heading-copy dw-topbar-heading-copy-compact">
                    <h1>{meta.title}</h1>
                    <span className="dw-toolbar-meta">{status.focus}</span>
                    {settings.center.enabled ? <span className="dw-toolbar-meta is-online">中心路由已启用</span> : null}
                    {hasWailsBridge() ? <span className="dw-toolbar-meta is-online">本地链路已连接</span> : <span className="dw-toolbar-meta">等待 Wails 绑定</span>}
                  </div>
                  <div className="dw-top-actions">
                    <button type="button" className="secondary" aria-label="切换同事" onClick={handleSwitchColleague}>同事</button>
                    <button type="button" className="primary" aria-label="开始新任务" onClick={handleOpenNewTask}>新任务</button>
                  </div>
                </div>
              </div>
            </header>
          )}
          <section className="dw-content" style={activeTab === 'home' ? { padding: 0, background: '#f9fafb' } : undefined}>{page}</section>
        </div>
      </main>
    </div>
  );
}
