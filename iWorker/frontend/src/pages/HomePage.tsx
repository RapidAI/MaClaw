import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { quickTasks as defaultQuickTasks } from '../mock/tasks';
import type { CenterAgentInstance, CenterGoalPush, CenterHealthStatus, CenterInstalledTools, DiWorkerSettings, GoalWatchAutoHandleStatus, HistoryTaskItem, WorkerMemoryStats } from '../types';

type Props = {
  draft: string;
  selectedTask: string;
  selectedColleagueName: string;
  recentTasks: HistoryTaskItem[];
  settings: DiWorkerSettings;
  centerHealthStatus: CenterHealthStatus | null;
  centerHealthError: string;
  workerMemoryStats: WorkerMemoryStats | null;
  workerMemoryStatsLoading: boolean;
  workerMemoryStatsError: string;
  agentInstances: CenterAgentInstance[];
  agentInstancesLoading: boolean;
  agentInstancesError: string;
  goalPushes: CenterGoalPush[];
  goalPushLoading: boolean;
  goalPushError: string;
  goalPushAckingId: string;
  goalWatchAutoStatus: GoalWatchAutoHandleStatus | null;
  installedTools: CenterInstalledTools;
  installedToolsLoading: boolean;
  installedToolsError: string;
  submitting: boolean;
  onDraftChange: (value: string) => void;
  onExpectedOutputChange: (value: string) => void;
  onPickTask: (task: string, colleagueName?: string) => void;
  onOpenNewTask: () => void;
  onOpenRecentTask: (task: HistoryTaskItem) => void;
  onOpenSettings: () => void;
  onRefreshAgentInstances: () => void | Promise<CenterAgentInstance[] | undefined>;
  onRefreshGoalPushes: () => void | Promise<CenterGoalPush[] | undefined>;
  onRefreshMemoryStats: () => void | Promise<WorkerMemoryStats | undefined>;
  onRefreshInstalledTools: () => void | Promise<CenterInstalledTools | undefined>;
  onCheckCenterHealth: () => void | Promise<CenterHealthStatus | undefined>;
  onAutoHandleGoalPush: (eventId: string) => void | Promise<void>;
  onAckGoalPush: (eventId: string, status: 'resumed' | 'blocked') => void | Promise<void>;
  onOpenGoalPushTask: (push: CenterGoalPush) => void;
};

type WorkMode = 'chat' | 'task' | 'research';
type ComposerTool = 'mention' | 'attach' | 'skill' | 'memory';
type ExpectedOutput = 'summary' | 'document' | 'table';
type CollaborationLane = 'center' | 'human' | 'autonomy';
type WorkStatusKind = 'active' | 'done' | 'review' | 'blocked';

type SelfCheckStatus = {
  state: 'idle' | 'running' | 'done' | 'issue';
  completedAt: string;
  checks: Array<{ label: string; ok: boolean; detail: string }>;
};

const modeCopy: Record<WorkMode, { label: string; title: string; detail: string; placeholder: string }> = {
  chat: {
    label: 'Talk',
    title: 'Conversation first',
    detail: 'Use voice or IM style instructions. The iWorker can clarify intent before opening a structured task.',
    placeholder: 'Talk to your iWorker. Example: help me check today\'s delivery exceptions, ask Quality iWorker if needed, and prepare a short decision brief.',
  },
  task: {
    label: 'Task',
    title: 'Structured execution',
    detail: 'Convert the request into a task workspace with output type, colleague routing, attachments, and audit trail.',
    placeholder: 'Describe the task, expected output, context, deadline, and which digital worker should own it.',
  },
  research: {
    label: 'Deep work',
    title: 'Evidence and synthesis',
    detail: 'Ask iWorker to gather facts, consult skills, discuss with peer agents, and produce reusable memory.',
    placeholder: 'Ask for analysis, options, risk points, evidence, and a recommendation that can be saved as organization memory.',
  },
};

const skillChips = [
  'Voice handoff',
  'Operating brief',
  'Ask peer iWorkers',
  'Customer reply',
  'Policy memory check',
  'Meeting decisions',
  'Data cleanup',
  'Evidence pack',
];

const quickActions: Array<{ id: 'im' | 'continue' | 'memory' | 'selfcheck'; title: string; detail: string }> = [
  { id: 'im', title: 'Start from IM', detail: 'Turn a voice/chat instruction into work.' },
  { id: 'continue', title: 'Continue a task', detail: 'Resume recent evidence and result.' },
  { id: 'memory', title: 'Capture memory', detail: 'Save reusable experience to Center.' },
  { id: 'selfcheck', title: 'Run self-check', detail: 'Check Center, memory, tools, body runtime, and watcher queue.' },
];


const laneCopy: Record<CollaborationLane, { title: string; label: string; detail: string }> = {
  center: {
    title: 'Center assigned work',
    label: 'Push inbox',
    detail: 'Work pushed by iWorkerCenter should be visible before ad-hoc prompting, with clear run, resume, and block decisions.',
  },
  human: {
    title: 'Human collaboration',
    label: 'Clarify and approve',
    detail: 'Human coworkers can hand off context, answer questions, approve risky steps, or become callable human skills.',
  },
  autonomy: {
    title: 'Autonomous runtime',
    label: 'Run state',
    detail: 'The local body executes, reports heartbeat, saves durable memory, and escalates when it cannot safely continue.',
  },
};

const formatPushAge = (seconds: number) => {
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return 'just now';
  }
  const minutes = Math.max(1, Math.round(seconds / 60));
  if (minutes < 60) {
    return `${minutes}m waiting`;
  }
  return `${Math.round(minutes / 60)}h waiting`;
};

const normalizeWorkStatus = (status: string): WorkStatusKind => {
  const normalized = status.toLowerCase().replace(/[_-]/g, ' ');
  if (['done', 'complete', 'completed', 'acked', 'acknowledged', 'resumed', 'resolved'].some((token) => normalized.includes(token))) {
    return 'done';
  }
  if (['block', 'fail', 'error', 'timeout'].some((token) => normalized.includes(token))) {
    return 'blocked';
  }
  if (['review', 'waiting', 'approval', 'human', 'manual', 'clarify'].some((token) => normalized.includes(token))) {
    return 'review';
  }
  return 'active';
};

const normalizeTaskStatus = (status: string): WorkStatusKind => normalizeWorkStatus(status);
const normalizePushStatus = (push: CenterGoalPush): WorkStatusKind => normalizeWorkStatus(`${push.status || ''} ${push.recommendedAction || ''} ${push.reason || ''}`.trim() || 'pushed');
const isCachedPush = (push: CenterGoalPush) => push.source === 'cache' || Boolean(push.stale);
const isOpenPush = (push: CenterGoalPush) => {
  const kind = normalizePushStatus(push);
  return kind === 'active' || kind === 'review';
};
const needsHumanIntervention = (push: CenterGoalPush) => {
  const combined = `${push.status || ''} ${push.recommendedAction || ''} ${push.reason || ''}`.toLowerCase();
  const kind = normalizePushStatus(push);
  return push.recommendedAction === 'ask_human' || kind === 'review' || kind === 'blocked' || ['human', 'manual', 'approval', 'approve', 'missing', 'clarify', 'blocked'].some((token) => combined.includes(token));
};

const workStatusCopyKey: Record<WorkStatusKind, string> = {
  active: 'home.active',
  done: 'home.completed',
  review: 'home.review',
  blocked: 'home.blocked',
};
const formatRuntimeName = (instance: CenterAgentInstance) => {
  const role = instance.role || 'worker';
  return role.replace(/[-_]/g, ' ');
};

const formatScopeCount = (stats: WorkerMemoryStats | null, scope: string) => stats?.byScope?.[scope] || 0;

const inferExpectedOutput = (task: string): ExpectedOutput => {
  const normalized = task.toLowerCase();
  if (normalized.includes('data') || normalized.includes('table') || normalized.includes('cleanup')) {
    return 'table';
  }
  if (normalized.includes('reply') || normalized.includes('report') || normalized.includes('brief') || normalized.includes('handoff') || normalized.includes('meeting')) {
    return 'document';
  }
  return 'summary';
};

const outputBadgeCopy: Record<ExpectedOutput, string> = {
  summary: 'Brief',
  document: 'Doc',
  table: 'Table',
};

const inferColleagueName = (task: string) => {
  const normalized = task.toLowerCase();
  if (normalized.includes('data') || normalized.includes('table') || normalized.includes('cleanup') || normalized.includes('chart')) {
    return 'Data iWorker';
  }
  if (normalized.includes('quality') || normalized.includes('root cause') || normalized.includes('corrective') || normalized.includes('issue')) {
    return 'Quality iWorker';
  }
  if (normalized.includes('operating') || normalized.includes('operation') || normalized.includes('production') || normalized.includes('handoff') || normalized.includes('exception') || normalized.includes('delivery')) {
    return 'Operations iWorker';
  }
  if (normalized.includes('reply') || normalized.includes('meeting') || normalized.includes('report') || normalized.includes('brief') || normalized.includes('email')) {
    return 'Office iWorker';
  }
  return '';
};

const colleagueBadgeCopy = (name: string) => name ? name.replace(' iWorker', '') : 'Auto';
const outputTypeCopy: Record<ExpectedOutput, string> = {
  summary: 'short brief / action summary',
  document: 'stakeholder-ready document',
  table: 'structured table / validation plan',
};

const buildCollaborationHint = (task: string) => {
  const normalized = task.toLowerCase();
  if (normalized.includes('peer') || normalized.includes('ask') || normalized.includes('decision')) {
    return 'Center-mediated A2A discussion is recommended before finalizing the answer.';
  }
  if (normalized.includes('memory') || normalized.includes('policy')) {
    return 'Use company, department, and personal memory from iWorkerCenter before writing new conclusions.';
  }
  if (normalized.includes('data') || normalized.includes('table') || normalized.includes('cleanup')) {
    return 'Keep raw local evidence temporary, then save reusable cleanup rules back to department memory.';
  }
  return 'Start directly, then call peer iWorkers, cloud-approved skills, or human skills only when the task needs it.';
};

const inferRouteReason = (task: string) => {
  const colleagueName = inferColleagueName(task);
  if (colleagueName === 'Data iWorker') {
    return 'Route reason: data/table/cleanup work needs structured data preparation and validation.';
  }
  if (colleagueName === 'Quality iWorker') {
    return 'Route reason: quality/root-cause/corrective work needs issue classification and cause analysis.';
  }
  if (colleagueName === 'Operations iWorker') {
    return 'Route reason: operations/production/handoff work needs execution context and delivery continuity.';
  }
  if (colleagueName === 'Office iWorker') {
    return 'Route reason: communication/report/document work needs clear written output and stakeholder-ready language.';
  }
  return 'Route reason: no specialized route matched, use automatic routing through iWorkerCenter.';
};

const readinessCheckLabel = (name: string) => {
  const labels: Record<string, string> = {
    database: 'Center database',
    tenant: 'Tenant bootstrap',
    roles: 'Role setup',
    iworkers: 'Digital colleagues',
    local_accounts: 'Local auth accounts',
    agent_runtime: 'Center agent runtime',
    goalwatch: 'Center GoalWatch push loop',
    routes: 'Client routes',
  };
  return labels[name] || name.replace(/[_-]/g, ' ');
};



const memoryStatsSourceCopy = (stats: WorkerMemoryStats | null) => {
  if (!stats) {
    return 'Not loaded';
  }
  if (stats.source === 'center') {
    return stats.stale ? 'Center stale' : 'Center live';
  }
  if (stats.source === 'cache') {
    return 'Cached fallback';
  }
  if (stats.source === 'local') {
    return 'Local cache';
  }
  if (stats.source === 'unavailable') {
    return 'Unavailable';
  }
  return 'Unknown source';
};

const memoryStatsOperable = (stats: WorkerMemoryStats | null, centerEnabled: boolean) => {
  if (!centerEnabled) {
    return true;
  }
  return Boolean(stats) && stats?.source !== 'unavailable';
};

const memoryStatsDetail = (stats: WorkerMemoryStats | null) => {
  if (!stats) {
    return 'not loaded';
  }
  const cached = stats.cachedAt ? ' / cached ' + stats.cachedAt : '';
  return (stats.total || 0) + ' memories / ' + memoryStatsSourceCopy(stats) + cached;
};

const installedToolsSourceCopy = (source: string, stale: boolean) => {
  if (source === 'center') {
    return stale ? 'Center stale' : 'Center live';
  }
  if (source === 'cache') {
    return 'Cached fallback';
  }
  if (source === 'partial-cache') {
    return 'Partial Center snapshot';
  }
  if (source === 'unavailable') {
    return 'Unavailable';
  }
  return 'Local only';
};

const installedToolsDetail = (tools: CenterInstalledTools) => {
  const total = tools.skills.length + tools.mcpServers.length;
  const source = installedToolsSourceCopy(tools.source, tools.stale);
  const cached = tools.cachedAt ? ' / cached ' + tools.cachedAt : '';
  return total + ' tools / ' + source + cached;
};

const runtimeSnapshotSourceCopy = (source?: string, stale?: boolean) => {
  if (source === 'center') {
    return stale ? 'Center stale' : 'Center live';
  }
  if (source === 'cache') {
    return 'Cached snapshot';
  }
  return 'Center live';
};

const runtimeSnapshotDetail = (source?: string, cachedAt?: string, stale?: boolean) => {
  const label = runtimeSnapshotSourceCopy(source, stale);
  return cachedAt ? label + ' / cached ' + cachedAt : label;
};

const installedToolsOperable = (tools: CenterInstalledTools, centerEnabled: boolean) => {
  if (!centerEnabled) {
    return true;
  }
  return tools.source !== 'unavailable';
};

const readinessSelfChecks = (health: CenterHealthStatus | null | undefined) => {
  const readiness = health?.iWorkerReadiness;
  if (!readiness) {
    return [{ label: 'Center iWorker readiness', ok: false, detail: health?.reachable ? 'Center did not report iWorker readiness' : 'Center health has not been checked' }];
  }
  const checks = readiness.checks.map((item) => ({
    label: readinessCheckLabel(item.name),
    ok: item.ready,
    detail: `${item.status}${typeof item.count === 'number' ? ` / ${item.count}` : ''}${item.detail ? ` / ${item.detail}` : ''}`,
  }));
  const hasReadyAuth = readiness.authMethods.some((item) => item.ready);
  const authChecks = readiness.authMethods.map((item) => ({
    label: `Auth ${item.method}`,
    ok: item.ready || !item.implemented || (hasReadyAuth && item.status === 'not_configured'),
    detail: `${item.status}${item.detail ? ` / ${item.detail}` : ''}`,
  }));
  return [...checks, ...authChecks];
};

const buildTaskTemplate = (task: string, mode: WorkMode) => {
  const normalized = task.toLowerCase();
  if (normalized.includes('peer') || normalized.includes('ask')) {
    return `Task: ${task}\n\nPlease discuss this with the relevant peer iWorkers through iWorkerCenter, summarize the agreed decision, and list any human skill that must be called.\n\nExpected output: decision, owner, evidence, and next action.`;
  }
  if (normalized.includes('memory') || normalized.includes('policy')) {
    return `Task: ${task}\n\nUse company, department, and personal memory from the registered iWorkerCenter. Clearly separate remembered facts from new assumptions.\n\nExpected output: matched memory, gaps, and recommended action.`;
  }
  if (normalized.includes('customer') || normalized.includes('reply')) {
    return `Task: ${task}\n\nDraft a customer-ready response. Check policy memory first, keep the tone professional, and include any missing information that a human or peer iWorker should confirm.\n\nExpected output: final reply plus internal notes.`;
  }
  if (normalized.includes('data') || normalized.includes('table')) {
    return `Task: ${task}\n\nClean and structure the data. Identify missing fields, suspicious values, and reusable rules that should be saved as department memory.\n\nExpected output: cleaned table plan, validation notes, and follow-up actions.`;
  }
  if (normalized.includes('research') || normalized.includes('market') || normalized.includes('risk')) {
    return `Task: ${task}\n\nRun deep work mode: gather evidence, compare options, discuss uncertainty, and produce a recommendation suitable for organization memory.\n\nExpected output: findings, evidence, risks, recommendation, and reusable playbook notes.`;
  }
  if (mode === 'chat') {
    return `Task: ${task}\n\nStart as an IM/voice handoff. Clarify intent first if needed, then convert the request into executable work.\n\nExpected output: clarified task, routing decision, and next action.`;
  }
  return `Task: ${task}\n\nTurn this into structured iWorker work. Use Center memory when helpful, route to peer iWorkers or human skills when needed, and preserve reusable experience.\n\nExpected output: result, evidence, and memory suggestions.`;
};

export function HomePage({ draft, selectedTask, selectedColleagueName, recentTasks, settings, centerHealthStatus, centerHealthError, workerMemoryStats, workerMemoryStatsLoading, workerMemoryStatsError, agentInstances, agentInstancesLoading, agentInstancesError, goalPushes, goalPushLoading, goalPushError, goalPushAckingId, goalWatchAutoStatus, installedTools, installedToolsLoading, installedToolsError, submitting, onDraftChange, onExpectedOutputChange, onPickTask, onOpenNewTask, onOpenRecentTask, onOpenSettings, onRefreshAgentInstances, onRefreshGoalPushes, onRefreshMemoryStats, onRefreshInstalledTools, onCheckCenterHealth, onAutoHandleGoalPush, onAckGoalPush, onOpenGoalPushTask }: Props) {
  const { t } = useTranslation();
  const [workMode, setWorkMode] = useState<WorkMode>('chat');
  const [quickTasks, setQuickTasks] = useState<string[]>(defaultQuickTasks);
  const [activeSuggestion, setActiveSuggestion] = useState('');
  const [selfCheckStatus, setSelfCheckStatus] = useState<SelfCheckStatus>({ state: 'idle', completedAt: '', checks: [] });

  useEffect(() => {
    const welcomeLoader = (window as Window & {
      go?: {
        main?: {
          App?: {
            GetWelcomeData?: () => Promise<{ quick_tasks?: string[] }>;
          };
        };
      };
    }).go?.main?.App?.GetWelcomeData;

    if (!welcomeLoader) {
      return;
    }

    welcomeLoader()
      .then((data: { quick_tasks?: string[] }) => {
        if (data?.quick_tasks && data.quick_tasks.length > 0) {
          setQuickTasks(data.quick_tasks);
        }
      })
      .catch(() => undefined);
  }, []);

  const currentMode = modeCopy[workMode];
  const visibleChips = useMemo(() => {
    if (workMode === 'task') {
      return quickTasks;
    }
    if (workMode === 'research') {
      return ['Market scan', 'Root cause analysis', 'Decision options', 'Skill search', 'Risk review', 'Reusable playbook'];
    }
    return skillChips;
  }, [quickTasks, workMode]);

  const liveAgentInstances = agentInstances.filter((item) => item.source !== 'cache' && !item.stale);
  const cachedAgentInstances = agentInstances.filter((item) => item.source === 'cache' || item.stale);
  const onlineAgents = agentInstances.filter((item) => item.effectiveStatus === 'online').length;
  const cachedGoalPushes = goalPushes.filter((item) => item.source === 'cache' || item.stale);
  const openPushes = goalPushes.filter(isOpenPush);
  const pendingPushes = openPushes.length;
  const centerEnabled = settings.center.enabled;
  const centerReady = Boolean(centerHealthStatus?.iWorkerReadiness?.ready);
  const centerStatusLabel = centerEnabled ? (centerHealthStatus?.reachable ? (centerReady ? 'Center ready' : 'Center online') : 'Center configured') : 'Local body only';
  const readiness = centerHealthStatus?.iWorkerReadiness;
  const readinessLine = readiness
    ? `${readiness.tenantCount} tenants / ${readiness.roleCount} roles / ${readiness.colleagueCount} iWorkers / ${readiness.localAccountCount} local accounts`
    : 'Run Center check to load iWorker readiness.';
  const installedToolCount = installedTools.skills.length + installedTools.mcpServers.length;
  const installedToolsSourceLabel = installedToolsSourceCopy(installedTools.source, installedTools.stale);
  const centerAvailabilityOk = !centerEnabled || (Boolean(centerHealthStatus?.reachable) && !centerHealthError);
  const runtimeSnapshotOperable = onlineAgents > 0 || agentInstances.length > 0;
  const runtimeAvailabilityOk = !centerEnabled || runtimeSnapshotOperable;
  const memorySourceLabel = memoryStatsSourceCopy(workerMemoryStats);
  const memoryAvailabilityOk = memoryStatsOperable(workerMemoryStats, centerEnabled) && !workerMemoryStatsError;
  const toolsAvailabilityOk = installedToolsOperable(installedTools, centerEnabled) && !installedToolsError;
  const watcherAvailabilityOk = !settings.center.goalWatchAutoHandleEnabled || (!goalWatchAutoStatus?.lastError && !goalPushError);
  const availabilityChecks = [centerAvailabilityOk, runtimeAvailabilityOk, memoryAvailabilityOk, toolsAvailabilityOk, watcherAvailabilityOk];
  const availabilityScore = availabilityChecks.filter(Boolean).length;
  const availabilityState = !centerEnabled ? 'Local mode' : availabilityScore === availabilityChecks.length ? 'Ready' : availabilityScore >= 3 ? 'Degraded' : 'Needs repair';
  const availabilityDetail = availabilityState === 'Ready'
    ? 'Center work, memory, runtime heartbeat, and installed tools are available.'
    : availabilityState === 'Degraded'
      ? 'iWorker can keep working with cached or partial capabilities while Center is repaired.'
      : availabilityState === 'Local mode'
        ? 'Center is disabled; local execution stays available without shared memory or pushes.'
        : 'Core registration or local capability state needs attention before business work.';
  const memoryAuthority = centerEnabled ? 'iWorkerCenter' : 'Not registered';
  const previewTask = activeSuggestion || visibleChips[0] || '';
  const previewOutput = inferExpectedOutput(previewTask);
  const previewColleague = inferColleagueName(previewTask);
  const primaryPush = openPushes[0] || goalPushes[0];
  const interventionPush = openPushes.find(needsHumanIntervention) || goalPushes.find(needsHumanIntervention);
  const interventionPushActionable = Boolean(interventionPush?.eventId) && !isCachedPush(interventionPush as CenterGoalPush);
  const runningPush = goalPushAckingId ? goalPushes.find((item) => item.eventId === goalPushAckingId) : undefined;
  const humanQueueLabel = pendingPushes > 0 ? `${pendingPushes} pending handoff${pendingPushes === 1 ? '' : 's'}` : 'Ready for coworker input';
  const centerSnapshotState = cachedAgentInstances.length || cachedGoalPushes.length ? 'Cached snapshot' : centerHealthStatus?.reachable ? 'Live sync' : centerEnabled ? 'Configured' : 'Local only';
  const centerSyncIssue = centerEnabled && Boolean(centerHealthError || agentInstancesError || goalPushError || workerMemoryStatsError || installedToolsError);
  const hasCachedContinuity = cachedAgentInstances.length > 0 || cachedGoalPushes.length > 0 || Boolean(workerMemoryStats?.stale || installedTools.stale);
  const showContinuityNotice = !centerEnabled || centerSyncIssue || hasCachedContinuity;
  const continuityTitle = !centerEnabled ? t('home.localContinuity', 'Local continuity active') : centerSyncIssue ? t('home.centerReconnecting', 'Center reconnecting') : t('home.cachedContinuity', 'Cached continuity active');
  const continuityDetail = !centerEnabled
    ? 'This iWorker can keep local conversations, drafts, attachments, and task history running without Cloud or Center.'
    : centerSyncIssue
      ? 'Local work can continue. Cached Center snapshots stay visible, but Resume, Block, Run, memory sync, and tool changes wait for iWorkerCenter to reconnect.'
      : 'Cached Center context is available for review. Live mutations still go through iWorkerCenter before they affect shared work.';
  const continuitySignals = [
    !centerEnabled ? 'Local mode' : '',
    cachedAgentInstances.length ? `${cachedAgentInstances.length} cached runtime` : '',
    cachedGoalPushes.length ? `${cachedGoalPushes.length} cached push${cachedGoalPushes.length === 1 ? '' : 'es'}` : '',
    workerMemoryStats?.stale ? 'stale memory snapshot' : '',
    installedTools.stale ? 'stale tools snapshot' : '',
    centerSyncIssue ? 'reconnect required for Center writes' : '',
  ].filter(Boolean);
  const primaryPushNeedsHuman = primaryPush ? needsHumanIntervention(primaryPush) : false;
  const primaryPushIsCached = primaryPush ? isCachedPush(primaryPush) : false;
  const primaryPushOpensTask = primaryPushNeedsHuman || primaryPushIsCached;
  const primaryRunDisabled = Boolean(primaryPush?.eventId) && goalPushAckingId === primaryPush.eventId;
  const primaryActionLabel = primaryPush?.eventId ? (primaryPushIsCached ? 'Open cached task' : primaryPushNeedsHuman ? 'Open review task' : 'Run Center push') : 'Check inbox';
  const toolSummaryLabel = installedToolCount > 0 ? `${installedTools.skills.length} skills / ${installedTools.mcpServers.length} MCP` : 'No tools enabled';
  const localActiveWork = submitting ? {
    id: 'local-submit',
    title: selectedTask || draft.trim().split('\n')[0] || 'Direct human handoff',
    owner: selectedColleagueName || 'Auto-routing iWorker',
    status: 'running',
    updatedAt: 'running now',
    kind: 'active' as WorkStatusKind,
    source: 'Local run',
    onOpen: onOpenNewTask,
  } : null;

  const activeTasks = recentTasks.filter((task) => normalizeTaskStatus(task.status) === 'active');
  const completedTasks = recentTasks.filter((task) => normalizeTaskStatus(task.status) === 'done');
  const reviewTasks = recentTasks.filter((task) => normalizeTaskStatus(task.status) === 'review');
  const blockedTasks = recentTasks.filter((task) => normalizeTaskStatus(task.status) === 'blocked');
  const completedPushes = goalPushes.filter((push) => normalizePushStatus(push) === 'done');
  const reviewPushes = goalPushes.filter((push) => normalizePushStatus(push) === 'review');
  const blockedPushes = goalPushes.filter((push) => normalizePushStatus(push) === 'blocked');
  const visibleWorkItems = [
    ...(localActiveWork ? [localActiveWork] : []),
    ...goalPushes.slice(0, 2).map((push) => ({
      id: push.eventId || push.taskId,
      title: push.title || push.taskId,
      owner: push.toRoleCode || push.toColleagueId || 'Center assigned',
      status: push.status || 'pushed',
      updatedAt: goalPushAckingId === push.eventId ? 'running now' : formatPushAge(push.ageSeconds),
      kind: normalizePushStatus(push),
      source: push.source === 'cache' || push.stale ? 'Cached Center push' : goalPushAckingId === push.eventId ? 'Center run' : 'Center push',
      onOpen: () => onOpenGoalPushTask(push),
    })),
    ...recentTasks.slice(0, 4).map((task) => ({
      id: task.id,
      title: task.title,
      owner: task.owner,
      status: task.status,
      updatedAt: task.updatedAt,
      kind: normalizeTaskStatus(task.status),
      source: 'Task history',
      onOpen: () => onOpenRecentTask(task),
    })),
  ].slice(0, 5);
  const currentWorkTitle = runningPush?.title || localActiveWork?.title || primaryPush?.title || activeTasks[0]?.title || (goalWatchAutoStatus?.running ? 'Goal watcher is handling pushed work' : 'No active task');
  const currentWorkDetail = runningPush
    ? `${runningPush.reason || 'Center push'} / running now`
    : localActiveWork
      ? `${localActiveWork.owner} / local execution`
      : primaryPush
        ? `${primaryPush.reason || 'Center push'} / ${formatPushAge(primaryPush.ageSeconds)} / ${runtimeSnapshotSourceCopy(primaryPush.source, primaryPush.stale)}`
        : activeTasks[0]
          ? `${activeTasks[0].owner} / ${activeTasks[0].updatedAt}`
          : goalWatchAutoStatus?.running
            ? `Run ${goalWatchAutoStatus.currentRunId || goalWatchAutoStatus.runCount} / single-flight watcher`
            : 'Ready for Center push or human handoff.';
  const appendDraft = (snippet: string) => {
    onDraftChange(draft.trim() ? `${draft.trim()}\n\n${snippet}` : snippet);
  };

  const handleComposerTool = (tool: ComposerTool) => {
    if (tool === 'mention') {
      appendDraft('@Operations iWorker please collaborate with the right peer worker or human skill when needed.');
      return;
    }
    if (tool === 'attach') {
      appendDraft('I will attach local evidence in the structured task workspace. Please use it as temporary body data, not durable memory unless I confirm.');
      onOpenNewTask();
      return;
    }
    if (tool === 'skill') {
      appendDraft('Search or call the best available skill for this task. Prefer enterprise-approved skills first, then request cloud/market skills only when allowed.');
      return;
    }
    appendDraft(`Use Center memory for context: company memory, department memory, and personal memory. Current authority: ${memoryAuthority}.`);
  };

  const handleHumanHandoff = (push?: CenterGoalPush) => {
    setWorkMode('chat');
    const prefix = push ? `Human intervention needed for Center push: ${push.title || push.taskId}. Reason: ${push.reason || push.status || 'needs review'}.` : 'Human collaboration needed.';
    appendDraft(`${prefix} Ask the coworker for missing context, approval, or a decision. Keep the autonomous run paused until the human answer is captured and saved as task evidence.`);
  };

  const runSelfCheck = async () => {
    appendDraft('Run iWorker body self-check: verify Center registration, memory authority, agent heartbeat, and GoalWatcher push queue. If any check fails, create a repair task before starting business work.');
    setSelfCheckStatus({ state: 'running', completedAt: '', checks: [] });
    const checks = await Promise.allSettled([
      onCheckCenterHealth(),
      onRefreshMemoryStats(),
      onRefreshAgentInstances(),
      onRefreshGoalPushes(),
      onRefreshInstalledTools(),
    ]);
    const labels = ['Center registration', 'Memory authority', 'Agent runtime', 'Goal watcher queue', 'Installed tools'];
    const baseChecks = checks.map((result, index) => ({
      label: labels[index],
      ok: result.status === 'fulfilled' && (index === 0 ? Boolean((result.value as CenterHealthStatus | undefined)?.reachable) : index === 1 ? memoryStatsOperable((result.value as WorkerMemoryStats | undefined) || workerMemoryStats, centerEnabled) : index === 2 ? Array.isArray(result.value) : index === 3 ? Array.isArray(result.value) : index === 4 ? installedToolsOperable((result.value as CenterInstalledTools | undefined) || installedTools, centerEnabled) : true),
      detail: result.status === 'fulfilled' ? (index === 0 ? (result.value as CenterHealthStatus | undefined)?.message || 'Checked' : index === 1 ? memoryStatsDetail((result.value as WorkerMemoryStats | undefined) || workerMemoryStats) : index === 2 ? (Array.isArray(result.value) ? `${result.value.length} runtime bodies checked` : agentInstancesError || 'Runtime sync failed') : index === 3 ? (Array.isArray(result.value) ? `${result.value.length} pushes checked` : goalPushError || 'Goal watcher sync failed') : index === 4 ? installedToolsDetail((result.value as CenterInstalledTools | undefined) || installedTools) : 'Checked') : result.reason instanceof Error ? result.reason.message : 'Check failed',
    }));
    const healthFromCheck = checks[0].status === 'fulfilled' ? checks[0].value : undefined;
    const nextChecks = [...baseChecks, ...readinessSelfChecks(healthFromCheck || centerHealthStatus)];
    setSelfCheckStatus({
      state: nextChecks.every((item) => item.ok) ? 'done' : 'issue',
      completedAt: new Date().toLocaleTimeString(),
      checks: nextChecks,
    });
  };

  const handleQuickAction = (actionId: 'im' | 'continue' | 'memory' | 'selfcheck') => {
    if (actionId === 'im') {
      setWorkMode('chat');
      appendDraft('I want to start from an IM or voice instruction. Please clarify intent first, then turn it into a runnable task if needed.');
      return;
    }
    if (actionId === 'continue') {
      const [latestTask] = recentTasks;
      if (latestTask) {
        onOpenRecentTask(latestTask);
        return;
      }
      appendDraft('Continue the most recent unfinished work and reconstruct context from Center memory and task history.');
      onOpenNewTask();
      return;
    }
    if (actionId === 'memory') {
      appendDraft('Capture this as reusable memory. Classify whether it belongs to company, department, or personal memory before saving to iWorkerCenter.');
      onOpenSettings();
      return;
    }
    void runSelfCheck();
  };
  const handleCreateReadinessTask = () => {
    const checkLines = selfCheckStatus.checks.length > 0
      ? selfCheckStatus.checks.map((item) => `- ${item.label}: ${item.ok ? 'OK' : 'ISSUE'} (${item.detail})`).join('\n')
      : '- Self-check has not run yet.';
    const diagnostics = [
      'Create an iWorker readiness repair task.',
      '',
      `Center: ${settings.center.enabled ? settings.center.baseUrl || `${settings.center.host}:${settings.center.port}` : 'disabled'}`,
      `Tenant / Department / Worker: ${settings.center.tenantId || 'default'} / ${settings.center.departmentId || 'default'} / ${settings.center.workerId || 'local-iworker'}`,
      `Memory authority: ${settings.center.enabled ? 'iWorkerCenter' : 'not registered'}`,
      `Memory count: ${workerMemoryStats?.total ?? 0}`,
      `Memory source: ${memoryStatsDetail(workerMemoryStats)}`,
      `Installed tools: ${installedToolsDetail(installedTools)}`,
      `Availability: ${availabilityState} (${availabilityScore}/${availabilityChecks.length})`,
      `Known errors: ${[centerHealthError, workerMemoryStatsError, agentInstancesError, goalPushError, installedToolsError].filter(Boolean).join(' | ') || 'none reported by UI'}`,
      '',
      'Self-check results:',
      checkLines,
      '',
      'Expected output: diagnose whether this local body can safely start business work. If not, propose concrete repair steps before execution.',
    ].join('\n');
    onDraftChange(diagnostics);
    onExpectedOutputChange('summary');
    onPickTask('iWorker readiness repair');
  };
  const handlePickTemplateTask = (task: string) => {
    const template = `${buildTaskTemplate(task, workMode)}\n\n${inferRouteReason(task)}`;
    onDraftChange(draft.trim() ? `${draft.trim()}\n\n${template}` : template);
    onExpectedOutputChange(inferExpectedOutput(task));
    onPickTask(task, inferColleagueName(task));
  };
  const handleSubmit = () => {
    if (draft.trim() || selectedTask) {
      onOpenNewTask();
    }
  };

  const handleKeyDown = (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault();
      handleSubmit();
    }
  };

  return (
    <div className="iw-workbuddy-shell">
      <section className="iw-workbuddy-stage" aria-label="iWorker workspace">
        <header className="iw-stage-topline">
          <div>
            <span className="iw-kicker">{t('home.workspaceKicker', 'iWorker workspace')}</span>
            <h2 aria-label="Digital coworker workbench">{t('home.workbenchTitle', 'Digital coworker workbench')}</h2>
            <p>{t('home.workbenchDesc', 'Work from Center-assigned tasks, human handoffs, and local instructions while keeping status, memory, and tools visible.')}</p>
          </div>
          <div className="iw-stage-snapshot" aria-label="runtime snapshot">
            <span>{centerSnapshotState}</span>
            <strong>{settings.center.tenantId || 'default'} / {settings.center.departmentId || 'default'}</strong>
            <small>{onlineAgents}/{Math.max(agentInstances.length, 1)} runtime bodies / {toolSummaryLabel}</small>
          </div>
        </header>

        <section className="iw-ops-strip" aria-label="Operating summary">
          <div><span>{t('home.current', 'Current')}</span><strong>{currentWorkTitle}</strong></div>
          <div><span>{t('home.inbox', 'Inbox')}</span><strong>{pendingPushes} push{pendingPushes === 1 ? '' : 'es'}</strong></div>
          <div><span>{t('home.availability', 'Availability')}</span><strong>{availabilityState}</strong></div>
          <div><span>{t('home.memory', 'Memory')}</span><strong>{workerMemoryStats?.total ?? 0} items</strong></div>
          <div><span>{t('home.tools', 'Tools')}</span><strong>{toolSummaryLabel}</strong></div>
        </section>

        {showContinuityNotice ? (
          <section className={"iw-continuity-banner " + (centerSyncIssue || hasCachedContinuity ? 'is-degraded' : 'is-local')} aria-label="Local continuity status">
            <div>
              <span>{centerSnapshotState}</span>
              <strong>{continuityTitle}</strong>
              <p>{continuityDetail}</p>
            </div>
            <div className="iw-continuity-signals">
              {(continuitySignals.length ? continuitySignals : ['ready']).slice(0, 4).map((item) => <span key={item}>{item}</span>)}
            </div>
          </section>
        ) : null}

        {interventionPush ? (
          <section className={"iw-intervention-banner " + (isCachedPush(interventionPush) ? 'is-cached' : '')} aria-label="Human input needed" role="status">
            <div className="iw-intervention-main">
              <span>{isCachedPush(interventionPush) ? t('home.cachedNotice', 'Cached notice') : t('home.humanInputNeeded', 'Human input needed')}</span>
              <strong>{interventionPush.title || interventionPush.taskId}</strong>
              <p>{isCachedPush(interventionPush) ? t('home.cachedPushDetail', 'This Center push is from a cached snapshot. Reconnect iWorkerCenter before resuming, blocking, or auto-running it.') : (interventionPush.reason || interventionPush.status || 'The autonomous run needs a human decision.') + ' / ' + formatPushAge(interventionPush.ageSeconds)}</p>
            </div>
            <div className="iw-intervention-meta">
              <span>{interventionPush.toRoleCode || interventionPush.toColleagueId || 'assigned iWorker'}</span>
              <span>{runtimeSnapshotSourceCopy(interventionPush.source, interventionPush.stale)}</span>
            </div>
            <div className="iw-intervention-actions">
              <button type="button" onClick={() => onOpenGoalPushTask(interventionPush)}>{t('home.openTask', 'Open task')}</button>
              <button type="button" onClick={() => handleHumanHandoff(interventionPush)}>{t('home.takeOver', 'Take over')}</button>
              <button type="button" disabled={!interventionPushActionable || goalPushAckingId === interventionPush.eventId} onClick={() => { void onAckGoalPush(interventionPush.eventId || '', 'resumed'); }}>{t('home.resume', 'Resume')}</button>
              <button type="button" disabled={!interventionPushActionable || goalPushAckingId === interventionPush.eventId} onClick={() => { void onAckGoalPush(interventionPush.eventId || '', 'blocked'); }}>{t('home.block', 'Block')}</button>
              <button type="button" onClick={() => { void onRefreshGoalPushes(); }} disabled={goalPushLoading}>{goalPushLoading ? t('home.syncing', 'Syncing') : t('home.refresh', 'Refresh')}</button>
            </div>
          </section>
        ) : null}

        <section className="iw-command-rail" aria-label="Command inbox lanes">
          <article className="iw-command-card is-center">
            <span>{laneCopy.center.label}</span>
            <strong>{primaryPush?.title || 'No Center push waiting'}</strong>
            <p>{primaryPush ? `${primaryPush.reason || 'goal push'} / ${formatPushAge(primaryPush.ageSeconds)}` : laneCopy.center.detail}</p>
            <div className="iw-command-actions">
              {primaryPush?.eventId ? (
                <>
                  <button type="button" disabled={primaryRunDisabled} onClick={() => { primaryPushOpensTask ? onOpenGoalPushTask(primaryPush) : void onAutoHandleGoalPush(primaryPush.eventId || ''); }}>{primaryActionLabel}</button>
                  <button type="button" disabled={goalPushAckingId === primaryPush.eventId} onClick={() => handleHumanHandoff(primaryPush)}>{t('home.askHuman', 'Ask human')}</button>
                </>
              ) : (
                <button type="button" onClick={() => { void onRefreshGoalPushes(); }} disabled={goalPushLoading}>{goalPushLoading ? 'Syncing' : primaryActionLabel}</button>
              )}
            </div>
          </article>

          <article className="iw-command-card">
            <span>{laneCopy.human.label}</span>
            <strong>{humanQueueLabel}</strong>
            <p>{laneCopy.human.detail}</p>
            <div className="iw-command-actions">
              <button type="button" onClick={() => handleHumanHandoff()}>{t('home.startHandoff', 'Start handoff')}</button>
              <button type="button" onClick={() => handleComposerTool('mention')}>{t('home.mention', 'Mention')}</button>
            </div>
          </article>

          <article className="iw-command-card">
            <span>{laneCopy.autonomy.label}</span>
            <strong>{goalWatchAutoStatus?.running ? 'Handling push now' : `${onlineAgents} runtime body${onlineAgents === 1 ? '' : 'ies'} online`}</strong>
            <p>{goalWatchAutoStatus?.lastError || laneCopy.autonomy.detail}</p>
            <div className="iw-command-actions">
              <button type="button" onClick={() => { void runSelfCheck(); }}>Self-check</button>
              <button type="button" onClick={() => { void onRefreshAgentInstances(); }}>Heartbeat</button>
            </div>
          </article>
        </section>

        <section className="iw-work-status-board" aria-label={t('home.workStatusAria', 'Digital coworker work status')}>
          <div className="iw-work-status-main">
            <span>{t('home.currentWork', 'Current work')}</span>
            <strong>{currentWorkTitle}</strong>
            <p>{currentWorkDetail}</p>
          </div>
          <div className="iw-work-status-counts" aria-label={t('home.workStatusSummary', 'Work status summary')}>
            <div><strong>{activeTasks.length + pendingPushes + (localActiveWork ? 1 : 0)}</strong><span>{t('home.active', 'Active')}</span></div>
            <div><strong>{completedTasks.length + completedPushes.length}</strong><span>{t('home.completed', 'Completed')}</span></div>
            <div><strong>{reviewTasks.length + reviewPushes.length}</strong><span>{t('home.review', 'Review')}</span></div>
            <div><strong>{blockedTasks.length + blockedPushes.length}</strong><span>{t('home.blocked', 'Blocked')}</span></div>
          </div>
          <div className="iw-work-status-feed" aria-label={t('home.recentStatus', 'Recent work status')}>
            {visibleWorkItems.length === 0 ? (
              <div className="iw-work-status-empty">{t('home.noTaskHistory', 'No task history yet.')}</div>
            ) : visibleWorkItems.map((item) => (
              <button key={`${item.source}-${item.id}`} type="button" onClick={item.onOpen} disabled={!item.onOpen}>
                <span className={`iw-work-status-dot is-${item.kind}`} aria-hidden="true" />
                <span className="iw-work-status-copy">
                  <strong>{item.title}</strong>
                  <small>{t(workStatusCopyKey[item.kind])} / {item.owner} / {item.updatedAt}</small>
                </span>
                <em>{item.source}</em>
              </button>
            ))}
          </div>
        </section>
        <section className="iw-workbench-panel" aria-label="Work controls">
          <div className="iw-agent-activity-card">
            <span>{t('home.activePartner', 'Active partner')}</span>
            <strong>{selectedColleagueName || t('home.autoRoutingWorker', 'Auto-routing iWorker')}</strong>
            <p>{selectedTask || 'Waiting for Center push, coworker handoff, or a direct instruction, then routing to the right worker, skill, or human approval.'}</p>
          </div>
          <div className={`iw-self-check-card is-${selfCheckStatus.state}`} aria-live="polite">
            <div className="iw-self-check-head">
              <span>{selfCheckStatus.state === 'idle' ? t('home.readinessNotChecked', 'Readiness not checked') : selfCheckStatus.state === 'running' ? t('home.checkingReadiness', 'Checking readiness') : selfCheckStatus.state === 'done' ? t('home.readyToWork', 'Ready to work') : t('home.needsAttention', 'Needs attention')}</span>
              {selfCheckStatus.completedAt ? <strong>{selfCheckStatus.completedAt}</strong> : null}
            </div>
            {selfCheckStatus.checks.length === 0 ? (
              <p>{t('home.runSelfCheck', 'Run a quick readiness check before starting business work.')}</p>
            ) : (
              <>
                <div className="iw-self-check-grid">
                  {selfCheckStatus.checks.map((item, index) => (
                    <span key={`${item.label}-${index}`} className={item.ok ? 'is-ok' : 'is-fail'}>{item.label}</span>
                  ))}
                </div>
                <button type="button" className="iw-self-check-action" onClick={handleCreateReadinessTask}>
                  {selfCheckStatus.state === 'issue' ? t('home.createRepairTask', 'Create repair task') : t('home.createReadinessTask', 'Create readiness task')}
                </button>
              </>
            )}
          </div>
          <div className="iw-workbench-notes">
            <span>{t('home.operatingScope', 'Operating scope')}</span>
            <strong>{centerEnabled ? t('home.centerGoverned', 'Center governed') : t('home.localMode', 'Local mode')}</strong>
            <p>{centerEnabled ? 'Tasks, memory, skills, and MCP visibility are governed by iWorkerCenter. Cloud stays outside business execution.' : 'Local execution is available; Center memory, pushes, and shared tools are disabled.'}</p>
          </div>
        </section>
        <div className="iw-mode-pill" role="tablist" aria-label={t('home.workMode', 'Work mode')}>
          {(Object.keys(modeCopy) as WorkMode[]).map((mode) => (
            <button key={mode} type="button" role="tab" aria-selected={workMode === mode} className={workMode === mode ? 'is-active' : ''} onClick={() => setWorkMode(mode)}>
              {modeCopy[mode].label}
            </button>
          ))}
        </div>

        <div className="iw-suggestion-strip" aria-label={t('home.quickSuggestions', 'Quick task suggestions')}>
          {visibleChips.map((task) => {
            const outputType = inferExpectedOutput(task);
            const colleagueName = inferColleagueName(task);
            return (
              <button key={task} type="button" aria-label={task} className={previewTask === task ? 'is-previewed' : ''} onMouseEnter={() => setActiveSuggestion(task)} onFocus={() => setActiveSuggestion(task)} onClick={() => handlePickTemplateTask(task)}>
                <span>{task}</span>
                <small aria-hidden="true">{outputBadgeCopy[outputType]}</small>
                <small aria-hidden="true" className="iw-route-badge">{colleagueBadgeCopy(colleagueName)}</small>
              </button>
            );
          })}
        </div>

        {previewTask ? (
          <section className="iw-route-preview-card" aria-label={t('home.routePreview', 'Route preview')}>
            <div>
              <span>{t('home.routePreview', 'Route preview')}</span>
              <strong>{previewTask}</strong>
              <p>{inferRouteReason(previewTask)}</p>
            </div>
            <div className="iw-route-preview-meta">
              <span>{colleagueBadgeCopy(previewColleague)} route</span>
              <span>{outputTypeCopy[previewOutput]}</span>
              <span>{buildCollaborationHint(previewTask)}</span>
            </div>
            <button type="button" onClick={() => handlePickTemplateTask(previewTask)}>{t('home.useThisRoute', 'Use this route')}</button>
          </section>
        ) : null}
        <div className="iw-bottom-composer">
          <textarea
            value={draft}
            onChange={(event) => onDraftChange(event.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={currentMode.placeholder}
            rows={3}
          />
          <div className="iw-composer-toolbar">
            <div className="iw-tool-group" aria-label={t('home.composerTools', 'Composer tools')}>
              <button type="button" title="Mention a worker or human skill" onClick={() => handleComposerTool('mention')}>@</button>
              <button type="button" title="Attach local evidence" onClick={() => handleComposerTool('attach')}>Attach</button>
              <button type="button" title="Select skill" onClick={() => handleComposerTool('skill')}>Skill</button>
              <button type="button" title="Use Center memory" onClick={() => handleComposerTool('memory')}>Memory</button>
            </div>
            <div className="iw-mode-summary">
              <strong>{currentMode.title}</strong>
              <span>{currentMode.detail}</span>
            </div>
            <button type="button" className="iw-send-button" onClick={handleSubmit} aria-label="Open task workspace">{t('home.send', 'Send')}</button>
          </div>
        </div>
      </section>

      <aside className="iw-workbuddy-inspector" aria-label="iWorker operating status">

        <section className="iw-inspector-card iw-availability-card">
          <div className="iw-inspector-title-row">
            <h3>{t('home.availability', 'Availability')}</h3>
            <span className={availabilityState === 'Ready' ? 'iw-watch-pill is-running' : 'iw-watch-pill'}>{availabilityState}</span>
          </div>
          <strong>{availabilityScore}/{availabilityChecks.length} operating signals ready</strong>
          <p>{availabilityDetail}</p>
          <div className="iw-availability-grid">
            <span className={centerAvailabilityOk ? 'is-ok' : 'is-warn'}>Center</span>
            <span className={runtimeAvailabilityOk ? 'is-ok' : 'is-warn'}>Runtime</span>
            <span className={memoryAvailabilityOk ? 'is-ok' : 'is-warn'}>Memory</span>
            <span className={toolsAvailabilityOk ? 'is-ok' : 'is-warn'}>Tools</span>
            <span className={watcherAvailabilityOk ? 'is-ok' : 'is-warn'}>Watcher</span>
          </div>
        </section>

        <section className="iw-inspector-card iw-inspector-card-dark">
          <div className="iw-inspector-title-row">
            <span>{t('home.centerRuntime', 'Center runtime')}</span>
            <button type="button" onClick={() => { void onRefreshAgentInstances(); }} disabled={agentInstancesLoading}>{agentInstancesLoading ? t('home.pinging', 'Pinging') : t('home.heartbeat', 'Heartbeat')}</button>
          </div>
          <strong>{onlineAgents} online agent instance{onlineAgents === 1 ? '' : 's'}</strong>
          <p>{cachedAgentInstances.length ? `${cachedAgentInstances.length} cached runtime snapshot${cachedAgentInstances.length === 1 ? '' : 's'} available while Center reconnects.` : liveAgentInstances.length ? 'Live Center runtime snapshot is available.' : 'Multiple agent instances can share Center memory while this desktop stays a replaceable body.'}</p>
          {agentInstancesError ? <p className="iw-panel-error">{agentInstancesError}</p> : null}
        </section>

        <section className="iw-inspector-card">
          <div className="iw-inspector-title-row">
            <h3>{t('home.centerRegistration', 'Center registration')}</h3>
            <button type="button" onClick={() => { void onCheckCenterHealth(); }}>{centerHealthStatus?.reachable ? t('home.recheck', 'Recheck') : t('home.check', 'Check')}</button>
          </div>
          <div className="iw-center-card">
            <span className={centerReady ? 'iw-status-dot is-online' : centerHealthStatus?.reachable || centerEnabled ? 'iw-status-dot is-warn' : 'iw-status-dot'} />
            <div>
              <strong>{centerStatusLabel}</strong>
              <p>{settings.center.baseUrl || `${settings.center.host}:${settings.center.port}`}</p>
              <p>{readinessLine}</p>
            </div>
          </div>
          <div className="iw-context-grid">
            <div><span>{t('home.tenant', 'Tenant')}</span><strong>{settings.center.tenantId || 'default'}</strong></div>
            <div><span>{t('home.department', 'Department')}</span><strong>{settings.center.departmentId || 'default'}</strong></div>
            <div><span>{t('home.worker', 'Worker')}</span><strong>{settings.center.workerId || 'local-iworker'}</strong></div>
          </div>
          {centerHealthError ? <p className="iw-panel-error">{centerHealthError}</p> : null}
        </section>

        <section className="iw-inspector-card">
          <div className="iw-inspector-title-row">
            <h3>{t('home.memoryAuthority', 'Memory authority')}</h3>
            <button type="button" onClick={() => { void onRefreshMemoryStats(); }} disabled={workerMemoryStatsLoading}>{workerMemoryStatsLoading ? t('home.loading', 'Loading') : t('home.refresh', 'Refresh')}</button>
          </div>
          <div className="iw-memory-meter">
            <strong>{workerMemoryStats?.total ?? 0}</strong>
            <span>{memorySourceLabel} on {memoryAuthority}</span>
          </div>
          <div className="iw-context-grid iw-memory-scopes">
            <div><span>{t('home.company', 'Company')}</span><strong>{formatScopeCount(workerMemoryStats, 'company')}</strong></div>
            <div><span>{t('home.department', 'Department')}</span><strong>{formatScopeCount(workerMemoryStats, 'department')}</strong></div>
            <div><span>{t('home.personal', 'Personal')}</span><strong>{formatScopeCount(workerMemoryStats, 'personal')}</strong></div>
          </div>
          {workerMemoryStats?.cachedAt ? <p>Last memory snapshot: {workerMemoryStats.cachedAt}</p> : null}
          {workerMemoryStatsError ? <p className="iw-panel-error">{workerMemoryStatsError}</p> : null}
        </section>

        <section className="iw-inspector-card">
          <h3>{t('home.operatingModel', 'Practical operating model')}</h3>
          <div className="iw-capability-grid">
            <article>
              <span>{t('home.body', 'Body')}</span>
              <strong>{t('home.localContainer', 'Local container')}</strong>
              <p>This computer provides screen, files, browser, and tool access.</p>
            </article>
            <article>
              <span>{t('home.memory', 'Memory')}</span>
              <strong>{t('home.centerOwned', 'Center owned')}</strong>
              <p>Company, department, and personal memory persist in iWorkerCenter.</p>
            </article>
            <article>
              <span>{t('home.people', 'People')}</span>
              <strong>{t('home.callableSkill', 'Callable skill')}</strong>
              <p>{t('home.peopleModelDetail', 'Human staff remain available as skills without becoming the control center.')}</p>
            </article>
          </div>
        </section>

        <section className="iw-inspector-card">
          <div className="iw-inspector-title-row">
            <h3>{t('home.installedTools', 'Installed tools')}</h3>
            <button type="button" onClick={() => { void onRefreshInstalledTools(); }} disabled={installedToolsLoading}>{installedToolsLoading ? t('home.syncing', 'Syncing') : t('home.refresh', 'Refresh')}</button>
          </div>
          <div className="iw-tool-summary-grid">
            <div><strong>{installedTools.skills.length}</strong><span>{t('home.skills', 'Skills')}</span></div>
            <div><strong>{installedTools.mcpServers.length}</strong><span>MCP</span></div>
          </div>
          <p>{installedToolsSourceLabel}{installedTools.cachedAt ? ` / cached ${installedTools.cachedAt}` : ''}</p>
          {installedToolsError ? <p className="iw-panel-error">{installedToolsError}</p> : null}
          <div className="iw-installed-tool-list">
            {[...installedTools.skills.map((skill) => ({ key: `skill-${skill.capabilityId}`, kind: 'Skill', name: skill.name, detail: `${skill.source || 'iWorkerCenter'} / ${skill.riskLevel || 'low'}` })), ...installedTools.mcpServers.map((server) => ({ key: `mcp-${server.id}`, kind: 'MCP', name: server.name, detail: `${server.serverType || 'mcp'} / ${server.departmentId || 'all'} / ${server.riskLevel || 'medium'}` }))].slice(0, 4).map((tool) => (
              <article key={tool.key} className="iw-installed-tool-card">
                <span>{tool.kind}</span>
                <strong>{tool.name}</strong>
                <p>{tool.detail}</p>
              </article>
            ))}
            {installedToolCount === 0 ? <div className="iw-goal-empty">{t('home.noTools', 'No Center-installed skill or MCP is enabled for this iWorker yet.')}</div> : null}
          </div>
        </section>
        <section className="iw-inspector-card">
          <div className="iw-inspector-title-row">
            <h3>{t('home.quickStarts', 'Quick starts')}</h3>
            <span className="iw-soft-pill">{t('home.usableNow', 'usable now')}</span>
          </div>
          <div className="iw-quick-action-list">
            {quickActions.map((item) => (
              <button key={item.title} type="button" onClick={() => handleQuickAction(item.id)}>
                <strong>{item.title}</strong>
                <span>{item.detail}</span>
              </button>
            ))}
          </div>
        </section>

        <section className="iw-inspector-card">
          <div className="iw-inspector-title-row">
            <h3>{t('home.goalWatcher', 'Goal watcher')}</h3>
            <span className={goalWatchAutoStatus?.running ? 'iw-watch-pill is-running' : 'iw-watch-pill'}>{goalWatchAutoStatus?.running ? 'Running' : 'Single-flight'}</span>
          </div>
          <div className="iw-watch-grid">
            <div><strong>{goalWatchAutoStatus?.runCount || 0}</strong><span>runs</span></div>
            <div><strong>{goalWatchAutoStatus?.skipCount || 0}</strong><span>skips</span></div>
            <div><strong>{goalWatchAutoStatus?.timeoutCancelCount || 0}</strong><span>timeouts</span></div>
            <div><strong>{goalWatchAutoStatus?.lastHandledCount || 0}</strong><span>handled</span></div>
          </div>
          <p className="iw-watch-note">Interval {goalWatchAutoStatus?.intervalSeconds || 30}s, max run {goalWatchAutoStatus?.maxDurationSeconds || 120}s.</p>
          {goalWatchAutoStatus?.lastError ? <p className="iw-panel-error">{goalWatchAutoStatus.lastError}</p> : null}
        </section>

        <section className="iw-inspector-card">
          <div className="iw-inspector-title-row">
            <h3>{t('home.pushQueue', 'Push queue')}</h3>
            <button type="button" onClick={() => { void onRefreshGoalPushes(); }} disabled={goalPushLoading}>{goalPushLoading ? t('home.syncing', 'Syncing') : t('home.refresh', 'Refresh')}</button>
          </div>
          {goalPushError ? <p className="iw-panel-error">{goalPushError}</p> : null}
          <div className="iw-goal-list">
            {goalPushes.length === 0 ? (
              <div className="iw-goal-empty">{t('home.noPush', 'No Center push is waiting. The inbox is clear.')}</div>
            ) : goalPushes.slice(0, 3).map((push) => (
              <article key={push.eventId || push.taskId} className="iw-goal-card">
                <span>{push.reason || 'goal_push'} / {Math.max(1, Math.round(push.ageSeconds / 60))}m / {runtimeSnapshotSourceCopy(push.source, push.stale)}</span>
                {push.recommendedAction ? <span className="iw-goal-action-pill">{push.recommendedAction}</span> : null}
                <strong>{push.title || push.taskId}</strong>
                <p>{push.status} / {push.toRoleCode || push.toColleagueId || 'assigned iWorker'}{push.cachedAt ? ` / ${runtimeSnapshotDetail(push.source, push.cachedAt, push.stale)}` : ''}</p>
                {push.eventId ? (
                  <div className="iw-goal-actions">
                    <button type="button" onClick={() => onOpenGoalPushTask(push)}>Open task</button>
                    <button type="button" disabled={goalPushAckingId === push.eventId || isCachedPush(push) || needsHumanIntervention(push)} onClick={() => { void onAutoHandleGoalPush(push.eventId || ''); }}>{t('home.run', 'Run')}</button>
                    <button type="button" disabled={goalPushAckingId === push.eventId || isCachedPush(push)} onClick={() => { void onAckGoalPush(push.eventId || '', 'resumed'); }}>{t('home.resume', 'Resume')}</button>
                    <button type="button" disabled={goalPushAckingId === push.eventId || isCachedPush(push)} onClick={() => { void onAckGoalPush(push.eventId || '', 'blocked'); }}>{t('home.block', 'Block')}</button>
                  </div>
                ) : null}
              </article>
            ))}
          </div>
        </section>

        <section className="iw-inspector-card">
          <h3>{t('home.recentWork', 'Recent work')}</h3>
          <div className="iw-recent-list">
            {recentTasks.slice(0, 4).map((task) => (
              <button key={task.id} type="button" onClick={() => onOpenRecentTask(task)}>
                <strong>{task.title}</strong>
                <span>{task.owner} / {task.status}</span>
              </button>
            ))}
          </div>
        </section>

        <section className="iw-inspector-card">
          <h3>{t('home.agentInstances', 'Agent instances')}</h3>
          <div className="iw-agent-list">
            {agentInstances.length === 0 ? (
              <div className="iw-goal-empty">{t('home.noAgent', 'Center has not seen this iWorker body yet.')}</div>
            ) : agentInstances.map((instance) => (
              <article key={instance.instanceId} className="iw-agent-card">
                <div>
                  <strong>{formatRuntimeName(instance)}</strong>
                  <span className={instance.effectiveStatus === 'online' ? 'is-online' : instance.effectiveStatus === 'offline' ? 'is-offline' : ''}>{instance.effectiveStatus || instance.status}</span>
                </div>
                <p>{instance.memoryAuthority || 'iWorkerCenter'} / {instance.localCacheMode || 'cache_only'} / {instance.heartbeatAgeSeconds || 0}s ago / {runtimeSnapshotDetail(instance.source, instance.cachedAt, instance.stale)}</p>
                {instance.workStatus ? (
                  <p>{instance.workStatus.currentTask || 'No active task'} / active {instance.workStatus.activeCount} / done {instance.workStatus.completedCount} / review {instance.workStatus.reviewCount} / blocked {instance.workStatus.blockedCount}</p>
                ) : null}
                <small>{instance.hostId || 'local body'} / {instance.capabilities.slice(0, 3).join(', ') || 'base tools'}</small>
              </article>
            ))}
          </div>
        </section>
      </aside>
    </div>
  );
}
