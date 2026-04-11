import { useEffect, useMemo, useState } from 'react';
import { CheckCenterHealth, LoadDiWorkerSettings, LoadTaskHistory, SaveDiWorkerSettings, SaveTaskHistory, SubmitTask } from '../wailsjs/go/main/App';
import { main } from '../wailsjs/go/models';
import { colleagues } from './mock/colleagues';
import { SideNav } from './components/layout/SideNav';
import { ColleaguesPage } from './pages/ColleaguesPage';
import { HomePage } from './pages/HomePage';
import { NewTaskPage } from './pages/NewTaskPage';
import { SettingsPage } from './pages/SettingsPage';
import { TaskHistoryPage } from './pages/TaskHistoryPage';
import { recentTasks as mockRecentTasks } from './mock/tasks';
import type { CenterHealthStatus, DiWorkerSettings, DiWorkerTab, HistoryTaskItem, SubmitTaskRequest, SubmitTaskResult, TaskAttachment, UpstreamProvider } from './types';

const pageMeta: Record<DiWorkerTab, { title: string; subtitle: string }> = {
  home: { title: '鏂板缓浠诲姟', subtitle: '杈撳叆浠诲姟鍐呭锛屽揩閫熷紑濮嬪鐞嗐€? },
  colleagues: { title: '鍚屼簨', subtitle: '鎸夊垎绫绘祻瑙堝悓浜嬶紝鍙敜浠栦滑涓轰綘鏈嶅姟銆? },
  'new-task': { title: '浠诲姟缂栬緫', subtitle: '缂栬緫浠诲姟鍐呭銆佽ˉ鍏呮潗鏂欏苟鎻愪氦澶勭悊銆? },
  history: { title: '宸ュ叿', subtitle: '璧嬩簣 DiWorker 鏇村己澶х殑鑳藉姏銆? },
  settings: { title: '閰嶇疆涓績', subtitle: '绠＄悊瑙掕壊淇℃伅銆佷腑蹇冭繛鎺ュ拰涓婃父鏈嶅姟璋冨害銆? },
};

const statusCopy: Record<DiWorkerTab, { focus: string }> = {
  home: { focus: '鏂板缓浠诲姟' },
  colleagues: { focus: '鍚屼簨' },
  'new-task': { focus: '浠诲姟缂栬緫' },
  history: { focus: '宸ュ叿' },
  settings: { focus: '涓績涓庤矾鐢遍厤缃? },
};

const hasWailsBridge = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

const defaultSettings: DiWorkerSettings = {
  roleProfile: {
    name: '灏忚开',
    description: '浣犵殑鏁板瓧鍔炲叕鍔╃悊锛屾搮闀块€氱煡銆佺邯瑕佷笌姹囨姤鏁寸悊銆?,
  },
  center: {
    enabled: false,
    host: '127.0.0.1',
    port: 9377,
    baseUrl: 'http://127.0.0.1:9377',
    timeoutSec: 60,
  },
  routing: {
    mode: 'smart',
    defaultProvider: 'office-openai',
    allowFallback: true,
  },
  providers: [
    {
      id: 'office-openai',
      name: '鍔炲叕鍐欎綔鏈嶅姟',
      enabled: true,
      protocol: 'openai',
      baseUrl: 'https://office.example.com/v1',
      apiKey: '',
      model: 'gpt-4.1',
      priority: 100,
      features: ['鍏枃', '绾', '涓枃'],
      description: '閫傚悎閫氱煡銆佺邯瑕併€佹棩鎶ヤ笌姝ｅ紡鏂囨。銆?,
      capabilities: {
        supportsStream: true,
        supportsVision: false,
        maxContext: 128000,
      },
    },
    {
      id: 'analysis-anthropic',
      name: '鍒嗘瀽褰掑洜鏈嶅姟',
      enabled: true,
      protocol: 'anthropic',
      baseUrl: 'https://analysis.example.com',
      apiKey: '',
      model: 'claude-sonnet-4-6',
      priority: 90,
      features: ['鍒嗘瀽', '褰掑洜', '璐ㄩ噺'],
      description: '閫傚悎寮傚父璇存槑銆佽川閲忓垎鏋愪笌鏁存敼寤鸿銆?,
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
    timeoutSec: item?.center?.timeout_sec || defaultSettings.center.timeoutSec,
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

const fromWailsCenterHealth = (
  item: main.CenterHealthStatus | null | undefined,
  source: CenterHealthStatus['source'],
): CenterHealthStatus => ({
  reachable: item?.reachable ?? false,
  status: item?.status || '',
  providerCount: item?.provider_count || 0,
  configPath: item?.config_path || '',
  message: item?.message || '',
  resolvedBaseUrl: item?.resolved_base_url || '',
  checkedAt: formatTimestamp(),
  source,
});

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
    timeout_sec: item.center.timeoutSec,
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
    return '闈炴枃鏈潗鏂欏凡涓婁紶锛屽彲缁撳悎鏂囦欢绫诲瀷鍜屾枃浠跺悕涓€璧峰鐞嗐€?;
  }
  const normalized = content.replace(/\s+/g, ' ').trim();
  if (!normalized) {
    return '鏂囨湰鏉愭枡宸蹭笂浼狅紝鍙粨鍚堟枃浠跺唴瀹逛竴璧峰鐞嗐€?;
  }
  const excerpt = normalized.slice(0, 80);
  return normalized.length > 80 ? `${excerpt}...` : excerpt;
};

const buildAttachmentPayload = (item: TaskAttachment, index: number) => {
  const meta = `${index + 1}. ${item.name}锛?{item.type}锛?{item.sizeLabel}锛塦;
  return item.isText
    ? `${meta}锛?{item.summary}\n${item.content}`
    : `${meta}锛?{item.summary}`;
};

const readFileContent = async (file: File) => {
  if (!isTextFile(file)) {
    return '';
  }
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(typeof reader.result === 'string' ? reader.result : '');
    reader.onerror = () => reject(new Error(`璇诲彇鏉愭枡澶辫触锛?{file.name}`));
    reader.readAsText(file);
  });
};

const formatTimestamp = () => {
  const now = new Date();
  return `${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')} ${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`;
};

const settingsSnapshot = (item: DiWorkerSettings) => JSON.stringify(item);

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
          type: file.type || '鏈煡绫诲瀷',
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
      ? `\n\n琛ュ厖鏉愭枡锛歕n${attachments.map(buildAttachmentPayload).join('\n\n')}`
      : '';
    const effectiveDraft = `${draft}${attachmentSummary}`.trim();
    const payload: SubmitTaskRequest = {
      task_type: selectedTask || '鑷敱杈撳叆',
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
          title: result.task_type,
          owner: result.colleague_name,
          status: '宸插畬鎴?,
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

  const handleSaveSettings = async () => {
    setSettingsError('');
    setSettingsSaveMessage('');
    if (!hasWailsBridge()) {
      setSettingsSaveMessage('褰撳墠鏈繛鎺?Wails锛岄厤缃粎淇濈暀鍦ㄥ綋鍓嶇晫闈€?);
      return;
    }
    setSettingsSaving(true);
    try {
      await SaveDiWorkerSettings(toWailsSettings(settings) as never);
      setSavedSettingsSnapshot(settingsSnapshot(settings));
      setSettingsSaveMessage('閰嶇疆宸蹭繚瀛?);
      try {
        const status = await CheckCenterHealth();
        setCenterHealthError('');
        setCenterHealthStatus(fromWailsCenterHealth(status as main.CenterHealthStatus, 'auto-after-save'));
      } catch (error) {
        setCenterHealthStatus(null);
        setCenterHealthError(error instanceof Error ? error.message : '涓績杩炴帴妫€娴嬪け璐?);
      }
    } catch (error) {
      setSettingsError(error instanceof Error ? error.message : '淇濆瓨閰嶇疆澶辫触');
    } finally {
      setSettingsSaving(false);
    }
  };

  const handleCheckCenterHealth = async () => {
    setCenterHealthError('');
    setCenterHealthStatus(null);
    if (!hasWailsBridge()) {
      setCenterHealthError('褰撳墠鏈繛鎺?Wails锛屾棤娉曟祴璇曚腑蹇冭繛鎺ャ€?);
      return;
    }
    setCenterHealthChecking(true);
    try {
      const status = await CheckCenterHealth();
      setCenterHealthStatus(fromWailsCenterHealth(status as main.CenterHealthStatus, 'manual'));
    } catch (error) {
      setCenterHealthError(error instanceof Error ? error.message : '涓績杩炴帴妫€娴嬪け璐?);
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
            onRoleNameChange={(value) => updateSettings((current) => ({ ...current, roleProfile: { ...current.roleProfile, name: value } }))}
            onRoleDescriptionChange={(value) => updateSettings((current) => ({ ...current, roleProfile: { ...current.roleProfile, description: value } }))}
            onCenterEnabledChange={(value) => updateSettings((current) => ({ ...current, center: { ...current.center, enabled: value } }))}
            onCenterHostChange={(value) => updateSettings((current) => ({ ...current, center: { ...current.center, host: value } }))}
            onCenterPortChange={(value) => updateSettings((current) => ({ ...current, center: { ...current.center, port: Number(value) || 0 } }))}
            onCenterBaseUrlChange={(value) => updateSettings((current) => ({ ...current, center: { ...current.center, baseUrl: value } }))}
            onCenterTimeoutChange={(value) => updateSettings((current) => ({ ...current, center: { ...current.center, timeoutSec: Number(value) || 0 } }))}
            onRoutingModeChange={(value) => updateSettings((current) => ({ ...current, routing: { ...current.routing, mode: value } }))}
            onRoutingDefaultProviderChange={(value) => updateSettings((current) => ({ ...current, routing: { ...current.routing, defaultProvider: value } }))}
            onRoutingAllowFallbackChange={(value) => updateSettings((current) => ({ ...current, routing: { ...current.routing, allowFallback: value } }))}
            onProviderChange={updateProvider}
            onProviderFeaturesChange={(providerId, value) => updateProvider(providerId, { features: value.split(/[锛?]/).map((item) => item.trim()).filter(Boolean) })}
            onCheckCenterHealth={handleCheckCenterHealth}
            onSave={handleSaveSettings}
          />
        );
      case 'home':
      default:
        return <HomePage draft={draft} selectedTask={selectedTask} selectedColleagueName={selectedColleagueName} recentTasks={historyTasks} onDraftChange={setDraft} onPickTask={handlePickTask} onOpenNewTask={handleOpenNewTask} onOpenRecentTask={handleOpenHistoryTask} />;
    }
  }, [activeTab, attachments, centerHealthChecking, centerHealthError, centerHealthStatus, draft, expectedOutput, historyTasks, selectedColleagueName, selectedTask, settings, settingsError, settingsLoading, settingsSaveMessage, settingsSaving, submitError, submitResult, submitting, viewedHistoryTask]);

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
                    <span className="dw-topbar-window-label">DiWorker</span>
                  </div>
                  <div className="dw-topbar-heading-copy dw-topbar-heading-copy-compact">
                    <h1>{meta.title}</h1>
                    <span className="dw-toolbar-meta">{status.focus}</span>
                    {settings.center.enabled ? <span className="dw-toolbar-meta is-online">涓績璺敱宸插惎鐢?/span> : null}
                    {hasWailsBridge() ? <span className="dw-toolbar-meta is-online">鏈湴閾捐矾宸茶繛鎺?/span> : <span className="dw-toolbar-meta">绛夊緟 Wails 缁戝畾</span>}
                  </div>
                  <div className="dw-top-actions">
                    <button type="button" className="secondary" aria-label="鍒囨崲鍚屼簨" onClick={handleSwitchColleague}>鍚屼簨</button>
                    <button type="button" className="primary" aria-label="寮€濮嬫柊浠诲姟" onClick={handleOpenNewTask}>鏂颁换鍔?/button>
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
