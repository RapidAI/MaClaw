import { useEffect, useState } from 'react';
import type { CenterEnrollmentDiscovery, CenterHealthStatus, DiWorkerSettings, UpstreamProvider, WorkerMemoryEntry, WorkerMemoryStats } from '../types';

type Props = {
  settings: DiWorkerSettings;
  loading: boolean;
  saving: boolean;
  dirty: boolean;
  error: string;
  saveMessage: string;
  healthChecking: boolean;
  healthStatus: CenterHealthStatus | null;
  healthError: string;
  enrollmentDiscovery: CenterEnrollmentDiscovery | null;
  enrollmentDiscovering: boolean;
  enrollmentApplyingId: string;
  enrollmentMessage: string;
  enrollmentError: string;
  memoryStats: WorkerMemoryStats | null;
  memoryStatsLoading: boolean;
  memoryStatsError: string;
  memoryDraftScope: string;
  memoryDraftContent: string;
  memoryDraftCategory: string;
  memoryDraftTags: string;
  memorySaving: boolean;
  memorySaveMessage: string;
  memorySaveError: string;
  memoryRecallQuery: string;
  memoryRecallItems: WorkerMemoryEntry[];
  memoryRecallLoading: boolean;
  memoryRecallError: string;
  memoryDeletingId: string;
  memoryDeleteError: string;
  onRoleNameChange: (value: string) => void;
  onRoleDescriptionChange: (value: string) => void;
  onCenterEnabledChange: (value: boolean) => void;
  onCenterHostChange: (value: string) => void;
  onCenterPortChange: (value: string) => void;
  onCenterBaseUrlChange: (value: string) => void;
  onCenterTenantIdChange: (value: string) => void;
  onCenterDepartmentIdChange: (value: string) => void;
  onCenterWorkerIdChange: (value: string) => void;
  onCenterTimeoutChange: (value: string) => void;
  onGoalWatchAutoHandleEnabledChange: (value: boolean) => void;
  onGoalWatchIntervalChange: (value: string) => void;
  onGoalWatchMaxDurationChange: (value: string) => void;
  onRoutingModeChange: (value: DiWorkerSettings['routing']['mode']) => void;
  onRoutingDefaultProviderChange: (value: string) => void;
  onRoutingAllowFallbackChange: (value: boolean) => void;
  onProviderChange: (providerId: string, patch: Partial<UpstreamProvider>) => void;
  onProviderFeaturesChange: (providerId: string, value: string) => void;
  onCheckCenterHealth: () => void;
  onDiscoverCenterEnrollment: () => void;
  onApplyCenterEnrollment: (workerId: string, auth: { method: string; username: string; password: string }) => void;
  onRefreshMemoryStats: () => void;
  onMemoryDraftScopeChange: (value: string) => void;
  onMemoryDraftContentChange: (value: string) => void;
  onMemoryDraftCategoryChange: (value: string) => void;
  onMemoryDraftTagsChange: (value: string) => void;
  onSaveWorkerMemory: () => void;
  onMemoryRecallQueryChange: (value: string) => void;
  onRecallWorkerMemories: () => void;
  onDeleteWorkerMemory: (memoryId: string) => void;
  onSave: () => void;
};

const SectionIcon = ({ children }: { children: string }) => <span className="dw-inline-icon" aria-hidden="true">{children}</span>;

const healthBadgeLabel = (healthStatus: CenterHealthStatus | null, healthError: string) => {
  if (healthError) {
    return '探测异常';
  }
  if (!healthStatus) {
    return '未检测';
  }
  return healthStatus.reachable ? '连接正常' : '连接不可达';
};

const healthSummaryTitle = (healthStatus: CenterHealthStatus | null, healthError: string) => {
  if (healthError) {
    return '检测返回异常';
  }
  if (!healthStatus) {
    return '尚未获取中心快照';
  }
  return healthStatus.reachable ? '中心连接正常' : '中心暂不可达';
};

const healthSourceLabel = (source: CenterHealthStatus['source']) => source === 'manual' ? '手动检测' : '保存后自动检测';

export function SettingsPage({
  settings,
  loading,
  saving,
  dirty,
  error,
  saveMessage,
  healthChecking,
  healthStatus,
  healthError,
  enrollmentDiscovery,
  enrollmentDiscovering,
  enrollmentApplyingId,
  enrollmentMessage,
  enrollmentError,
  memoryStats,
  memoryStatsLoading,
  memoryStatsError,
  memoryDraftScope,
  memoryDraftContent,
  memoryDraftCategory,
  memoryDraftTags,
  memorySaving,
  memorySaveMessage,
  memorySaveError,
  memoryRecallQuery,
  memoryRecallItems,
  memoryRecallLoading,
  memoryRecallError,
  memoryDeletingId,
  memoryDeleteError,
  onRoleNameChange,
  onRoleDescriptionChange,
  onCenterEnabledChange,
  onCenterHostChange,
  onCenterPortChange,
  onCenterBaseUrlChange,
  onCenterTenantIdChange,
  onCenterDepartmentIdChange,
  onCenterWorkerIdChange,
  onCenterTimeoutChange,
  onGoalWatchAutoHandleEnabledChange,
  onGoalWatchIntervalChange,
  onGoalWatchMaxDurationChange,
  onRoutingModeChange,
  onRoutingDefaultProviderChange,
  onRoutingAllowFallbackChange,
  onProviderChange,
  onProviderFeaturesChange,
  onCheckCenterHealth,
  onDiscoverCenterEnrollment,
  onApplyCenterEnrollment,
  onRefreshMemoryStats,
  onMemoryDraftScopeChange,
  onMemoryDraftContentChange,
  onMemoryDraftCategoryChange,
  onMemoryDraftTagsChange,
  onSaveWorkerMemory,
  onMemoryRecallQueryChange,
  onRecallWorkerMemories,
  onDeleteWorkerMemory,
  onSave,
}: Props) {
  const [expandedProviderId, setExpandedProviderId] = useState<string | null>(null);
  const [enrollmentAuthMethod, setEnrollmentAuthMethod] = useState('local');
  const [enrollmentAuthUsername, setEnrollmentAuthUsername] = useState('');
  const [enrollmentAuthPassword, setEnrollmentAuthPassword] = useState('');
  const discoveredAuthMethods = enrollmentDiscovery?.authMethods?.length
    ? enrollmentDiscovery.authMethods
    : [
      { method: 'local', label: 'Local account', enabled: true, implemented: true, status: 'ready', description: 'Manual or imported username/password account.' },
      { method: 'ldap', label: 'LDAP', enabled: true, implemented: true, status: 'available', description: 'Enterprise directory account.' },
      { method: 'oidc', label: 'OIDC / OAuth SSO', enabled: false, implemented: false, status: 'reserved', description: 'Reserved for zero-trust SSO.' },
    ];
  const selectedAuthMethod = discoveredAuthMethods.find((item) => item.method === enrollmentAuthMethod);
  const centerReadiness = healthStatus?.iWorkerReadiness;

  useEffect(() => {
    if (expandedProviderId && !settings.providers.some((provider) => provider.id === expandedProviderId)) {
      setExpandedProviderId(null);
    }
  }, [expandedProviderId, settings.providers]);

  return (
    <div className="dw-page-stack">
      <section className="card dw-page-panel">
        <div className="dw-panel-header dw-panel-header-compact">
          <div>
            <span className="eyebrow">配置</span>
            <h2>数字员工中心配置</h2>
          </div>
          <div className="dw-settings-header-meta">
            {dirty ? <span className="dw-settings-dirty-badge">有未保存更改</span> : <span className="dw-settings-clean-badge">当前已保存</span>}
            <small>管理左下角角色信息、中心地址与多上游服务调度策略。</small>
          </div>
        </div>
        <div className="dw-task-layout dw-settings-layout">
          <div className="dw-task-main dw-editor-main">
            <section className="card-subtle dw-editor-section dw-settings-section dw-settings-section-compact">
              <div className="dw-pane-head">
                <strong><SectionIcon>①</SectionIcon>角色信息</strong>
                <span>用于左下角展示</span>
              </div>
              <div className="dw-form-grid">
                <label>
                  角色名
                  <input value={settings.roleProfile.name} onChange={(event) => onRoleNameChange(event.target.value)} placeholder="例如：小迪" />
                </label>
                <label>
                  特点描述
                  <input value={settings.roleProfile.description} onChange={(event) => onRoleDescriptionChange(event.target.value)} placeholder="例如：擅长纪要、通知与汇报整理" />
                </label>
              </div>
            </section>

            <section className="card-subtle dw-editor-section dw-settings-section dw-settings-section-compact">
              <div className="dw-pane-head">
                <strong><SectionIcon>②</SectionIcon>中心连接</strong>
                <span>Wails 提交任务优先走这里</span>
              </div>
              <div className="dw-settings-group-list">
                <section className="dw-settings-group">
                  <div className="dw-settings-group-head">
                    <strong>基础连接</strong>
                    <span>中心地址与请求超时</span>
                  </div>
                  <div className="dw-form-grid">
                    <label>
                      启用中心
                      <select value={settings.center.enabled ? 'enabled' : 'disabled'} onChange={(event) => onCenterEnabledChange(event.target.value === 'enabled')}>
                        <option value="enabled">启用</option>
                        <option value="disabled">关闭</option>
                      </select>
                    </label>
                    <label>
                      地址
                      <input value={settings.center.host} onChange={(event) => onCenterHostChange(event.target.value)} placeholder="127.0.0.1" />
                    </label>
                    <label>
                      端口
                      <input value={String(settings.center.port)} onChange={(event) => onCenterPortChange(event.target.value)} placeholder="9377" />
                    </label>
                    <label>
                      Base URL
                      <input value={settings.center.baseUrl} onChange={(event) => onCenterBaseUrlChange(event.target.value)} placeholder="http://127.0.0.1:9377" />
                    </label>
                    <label>
                      Tenant ID
                      <input value={settings.center.tenantId} onChange={(event) => onCenterTenantIdChange(event.target.value)} placeholder="default" />
                    </label>
                    <label>
                      Department ID
                      <input value={settings.center.departmentId} onChange={(event) => onCenterDepartmentIdChange(event.target.value)} placeholder="default" />
                    </label>
                    <label>
                      Worker ID
                      <input value={settings.center.workerId} onChange={(event) => onCenterWorkerIdChange(event.target.value)} placeholder="local-iworker" />
                    </label>
                    <label>
                      超时（秒）
                      <input value={String(settings.center.timeoutSec)} onChange={(event) => onCenterTimeoutChange(event.target.value)} placeholder="60" />
                    </label>
                    <label>
                      自动守护
                      <select value={settings.center.goalWatchAutoHandleEnabled ? 'enabled' : 'disabled'} onChange={(event) => onGoalWatchAutoHandleEnabledChange(event.target.value === 'enabled')}>
                        <option value="enabled">启用</option>
                        <option value="disabled">关闭</option>
                      </select>
                    </label>
                    <label>
                      守护周期（秒）
                      <input value={String(settings.center.goalWatchIntervalSec)} onChange={(event) => onGoalWatchIntervalChange(event.target.value)} placeholder="30" />
                    </label>
                    <label>
                      卡死超时（秒）
                      <input value={String(settings.center.goalWatchMaxDurationSec)} onChange={(event) => onGoalWatchMaxDurationChange(event.target.value)} placeholder="120" />
                    </label>
                  </div>
                  <div className="dw-settings-enrollment-card">
                    <div className="dw-settings-group-head">
                      <strong>Center Enrollment</strong>
                      <span>Discover tenants and bind this local body to a Center iWorker.</span>
                    </div>
                    <div className="dw-settings-enrollment-actions">
                      <button type="button" className="secondary" onClick={onDiscoverCenterEnrollment} disabled={loading || enrollmentDiscovering || !settings.center.baseUrl.trim()}>
                        {enrollmentDiscovering ? 'Discovering...' : 'Discover Center'}
                      </button>
                      <small>{settings.center.baseUrl || 'Set Base URL first'}</small>
                    </div>
                    <div className="dw-settings-enrollment-auth">
                      <label>
                        Human identity
                        <select value={enrollmentAuthMethod} onChange={(event) => setEnrollmentAuthMethod(event.target.value)}>
                          {discoveredAuthMethods.map((method) => (
                            <option key={method.method} value={method.method} disabled={!method.enabled && method.implemented}>
                              {method.label}{method.implemented ? '' : ' (reserved)'}
                            </option>
                          ))}
                        </select>
                      </label>
                      <label>
                        Username / email / phone
                        <input value={enrollmentAuthUsername} onChange={(event) => setEnrollmentAuthUsername(event.target.value)} placeholder="alice@example.com" />
                      </label>
                      <label>
                        Password / verification code
                        <input type="password" value={enrollmentAuthPassword} onChange={(event) => setEnrollmentAuthPassword(event.target.value)} placeholder="Required before binding" />
                      </label>
                    </div>
                    {enrollmentMessage ? <p>{enrollmentMessage}</p> : null}
                    {enrollmentError ? <p>{enrollmentError}</p> : null}
                    {enrollmentDiscovery ? (
                      <div className="dw-settings-enrollment-results">
                        <div className="dw-settings-kv-list">
                          <div className="dw-settings-kv-item"><span>Tenant</span><strong>{enrollmentDiscovery.selectedTenantId || '-'}</strong></div>
                          <div className="dw-settings-kv-item"><span>Companies</span><strong>{enrollmentDiscovery.tenants.length}</strong></div>
                          <div className="dw-settings-kv-item"><span>Roles</span><strong>{enrollmentDiscovery.roles.length}</strong></div>
                          <div className="dw-settings-kv-item"><span>iWorkers</span><strong>{enrollmentDiscovery.colleagues.length}</strong></div>
                        </div>
                        <div className="dw-settings-enrollment-list">
                          {enrollmentDiscovery.colleagues.length > 0 ? enrollmentDiscovery.colleagues.map((worker) => (
                            <article key={worker.id} className="dw-settings-enrollment-worker">
                              <div>
                                <strong>{worker.name}</strong>
                                <span>{worker.roleName || worker.roleCode || 'iWorker'}</span>
                                <p>{worker.description || 'No description from Center yet.'}</p>
                              </div>
                              <button type="button" className="primary" onClick={() => onApplyCenterEnrollment(worker.id, { method: enrollmentAuthMethod, username: enrollmentAuthUsername, password: enrollmentAuthPassword })} disabled={Boolean(enrollmentApplyingId) || !enrollmentAuthUsername.trim() || !enrollmentAuthPassword.trim()}>
                                {enrollmentApplyingId === worker.id ? 'Binding...' : 'Bind here'}
                              </button>
                            </article>
                          )) : <p>No active iWorkers discovered. Apply the enterprise bootstrap plan in iWorkerCenter first.</p>}
                        </div>
                      </div>
                    ) : null}
                  </div>
                </section>
                <section className="dw-settings-group">
                  <div className="dw-settings-group-head">
                    <strong>路由策略</strong>
                    <span>控制默认上游与失败回退</span>
                  </div>
                  <div className="dw-form-grid">
                    <label>
                      路由模式
                      <select value={settings.routing.mode} onChange={(event) => onRoutingModeChange(event.target.value as DiWorkerSettings['routing']['mode'])}>
                        <option value="smart">智能调度</option>
                        <option value="priority">优先级优选</option>
                        <option value="manual">手动默认</option>
                      </select>
                    </label>
                    <label>
                      默认服务
                      <input value={settings.routing.defaultProvider} onChange={(event) => onRoutingDefaultProviderChange(event.target.value)} placeholder="office-openai" />
                    </label>
                    <label className="dw-settings-field-span-2">
                      失败回退
                      <select value={settings.routing.allowFallback ? 'enabled' : 'disabled'} onChange={(event) => onRoutingAllowFallbackChange(event.target.value === 'enabled')}>
                        <option value="enabled">允许</option>
                        <option value="disabled">关闭</option>
                      </select>
                    </label>
                  </div>
                </section>
              </div>
            </section>

            <section className="card-subtle dw-editor-section dw-settings-section dw-settings-section-compact">
              <div className="dw-pane-head">
                <strong><SectionIcon>③</SectionIcon>上游服务</strong>
                <span>{settings.providers.length} 个服务</span>
              </div>
              <div className="dw-provider-list">
                {settings.providers.map((provider) => {
                  const isExpanded = expandedProviderId === provider.id;
                  const detailsId = `provider-details-${provider.id}`;

                  return (
                    <article key={provider.id} className={`dw-provider-card${isExpanded ? ' is-expanded' : ''}`}>
                      <div className="dw-provider-summary">
                        <div className="dw-provider-card-head">
                          <div className="dw-provider-card-title">
                            <strong>{provider.name}</strong>
                            <p>{provider.description || '未设置说明'}</p>
                          </div>
                          <div className="dw-provider-card-badges">
                            <span>{provider.protocol.toUpperCase()}</span>
                            <span>{provider.enabled ? '已启用' : '已关闭'}</span>
                          </div>
                        </div>
                        <div className="dw-provider-meta-row">
                          <span>模型：{provider.model || '未设置'}</span>
                          <span>优先级：{provider.priority}</span>
                          <span>上下文：{provider.capabilities.maxContext || 0}</span>
                        </div>
                        <div className="dw-provider-feature-row">
                          <span>{provider.capabilities.supportsStream ? '流式输出' : '非流式'}</span>
                          <span>{provider.capabilities.supportsVision ? '支持视觉' : '纯文本'}</span>
                          <span>{provider.features.length ? provider.features.join(' / ') : '未设置特点'}</span>
                        </div>
                        <div className="dw-provider-summary-foot">
                          <span>{isExpanded ? '正在编辑当前服务' : '默认仅显示摘要'}</span>
                          <button
                            type="button"
                            className="secondary dw-provider-toggle"
                            aria-expanded={isExpanded}
                            aria-controls={detailsId}
                            onClick={() => setExpandedProviderId((current) => current === provider.id ? null : provider.id)}
                          >
                            {isExpanded ? `收起编辑 ${provider.name}` : `展开编辑 ${provider.name}`}
                          </button>
                        </div>
                      </div>
                      {isExpanded ? (
                        <div id={detailsId} className="dw-provider-details">
                          <div className="dw-form-grid dw-provider-grid">
                            <label>
                              启用
                              <select value={provider.enabled ? 'enabled' : 'disabled'} onChange={(event) => onProviderChange(provider.id, { enabled: event.target.value === 'enabled' })}>
                                <option value="enabled">启用</option>
                                <option value="disabled">关闭</option>
                              </select>
                            </label>
                            <label>
                              名称
                              <input value={provider.name} onChange={(event) => onProviderChange(provider.id, { name: event.target.value })} />
                            </label>
                            <label>
                              协议
                              <select value={provider.protocol} onChange={(event) => onProviderChange(provider.id, { protocol: event.target.value as UpstreamProvider['protocol'] })}>
                                <option value="openai">openai</option>
                                <option value="anthropic">anthropic</option>
                              </select>
                            </label>
                            <label className="dw-provider-field-span-3">
                              Base URL
                              <input value={provider.baseUrl} onChange={(event) => onProviderChange(provider.id, { baseUrl: event.target.value })} />
                            </label>
                            <label className="dw-provider-field-span-3">
                              API Key
                              <input value={provider.apiKey} onChange={(event) => onProviderChange(provider.id, { apiKey: event.target.value })} />
                            </label>
                          </div>
                          <div className="dw-provider-form-divider" />
                          <div className="dw-form-grid dw-provider-grid dw-provider-grid-secondary">
                            <label>
                              模型
                              <input value={provider.model} onChange={(event) => onProviderChange(provider.id, { model: event.target.value })} />
                            </label>
                            <label>
                              优先级
                              <input value={String(provider.priority)} onChange={(event) => onProviderChange(provider.id, { priority: Number(event.target.value) || 0 })} />
                            </label>
                            <label>
                              最大上下文
                              <input value={String(provider.capabilities.maxContext)} onChange={(event) => onProviderChange(provider.id, { capabilities: { ...provider.capabilities, maxContext: Number(event.target.value) || 0 } })} />
                            </label>
                            <label className="dw-provider-field-span-3">
                              服务特点
                              <input value={provider.features.join('，')} onChange={(event) => onProviderFeaturesChange(provider.id, event.target.value)} placeholder="公文，中文，结构化" />
                            </label>
                            <label>
                              支持流式
                              <select value={provider.capabilities.supportsStream ? 'enabled' : 'disabled'} onChange={(event) => onProviderChange(provider.id, { capabilities: { ...provider.capabilities, supportsStream: event.target.value === 'enabled' } })}>
                                <option value="enabled">支持</option>
                                <option value="disabled">关闭</option>
                              </select>
                            </label>
                            <label>
                              支持视觉
                              <select value={provider.capabilities.supportsVision ? 'enabled' : 'disabled'} onChange={(event) => onProviderChange(provider.id, { capabilities: { ...provider.capabilities, supportsVision: event.target.value === 'enabled' } })}>
                                <option value="enabled">支持</option>
                                <option value="disabled">关闭</option>
                              </select>
                            </label>
                            <label>
                              说明
                              <input value={provider.description} onChange={(event) => onProviderChange(provider.id, { description: event.target.value })} />
                            </label>
                          </div>
                        </div>
                      ) : null}
                    </article>
                  );
                })}
              </div>
            </section>
          </div>

          <aside className="dw-task-side dw-settings-side">
            <div className="card-subtle dw-side-panel-block dw-settings-summary-card">
              <div className="dw-settings-summary-head">
                <div>
                  <label>记忆沉淀</label>
                  <strong>{memoryStats ? `${memoryStats.total} 条` : '未加载'}</strong>
                </div>
                <button type="button" className="secondary" onClick={onRefreshMemoryStats} disabled={loading || memoryStatsLoading || !settings.center.enabled}>
                  {memoryStatsLoading ? '刷新中...' : '刷新记忆'}
                </button>
              </div>
              <p>记忆源存放在注册的 iWorkerCenter，本地只作为访问缓存。</p>
              <div className="dw-settings-kv-list">
                <div className="dw-settings-kv-item">
                  <span>公司记忆</span>
                  <strong>{memoryStats?.byScope.company ?? 0}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>部门记忆</span>
                  <strong>{memoryStats?.byScope.department ?? 0}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>个人记忆</span>
                  <strong>{memoryStats?.byScope.personal ?? 0}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>当前上下文</span>
                  <strong>{`${settings.center.tenantId || '-'} / ${settings.center.departmentId || '-'} / ${settings.center.workerId || '-'}`}</strong>
                </div>
              </div>
              {memoryStatsError ? <p>{memoryStatsError}</p> : null}
              <div className="dw-settings-save-row">
                <label>
                  Memory Capture
                  <select value={memoryDraftScope} onChange={(event) => onMemoryDraftScopeChange(event.target.value)} disabled={!settings.center.enabled || memorySaving}>
                    <option value="personal">Personal memory</option>
                    <option value="department">Department memory</option>
                    <option value="company">Company memory</option>
                  </select>
                </label>
                <label>
                  Category
                  <input value={memoryDraftCategory} onChange={(event) => onMemoryDraftCategoryChange(event.target.value)} placeholder="note" disabled={!settings.center.enabled || memorySaving} />
                </label>
                <label>
                  Tags
                  <input value={memoryDraftTags} onChange={(event) => onMemoryDraftTagsChange(event.target.value)} placeholder="policy, preference" disabled={!settings.center.enabled || memorySaving} />
                </label>
                <label>
                  Content
                  <textarea value={memoryDraftContent} onChange={(event) => onMemoryDraftContentChange(event.target.value)} placeholder="Write a reusable fact, rule, preference, or handoff note." disabled={!settings.center.enabled || memorySaving} rows={4} />
                </label>
                <button type="button" className="primary" onClick={onSaveWorkerMemory} disabled={!settings.center.enabled || memorySaving || !memoryDraftContent.trim()}>
                  {memorySaving ? 'Saving memory...' : 'Save memory'}
                </button>
                <p>{memorySaveMessage || memorySaveError || 'Saved memories are canonical in iWorkerCenter; this computer keeps cache only.'}</p>
              </div>
              <div className="dw-settings-save-row">
                <label>
                  Memory Browser
                  <input value={memoryRecallQuery} onChange={(event) => onMemoryRecallQueryChange(event.target.value)} placeholder="Search registered center memory" disabled={!settings.center.enabled || memoryRecallLoading} />
                </label>
                <button type="button" className="secondary" onClick={onRecallWorkerMemories} disabled={!settings.center.enabled || memoryRecallLoading}>
                  {memoryRecallLoading ? 'Recalling...' : 'Recall memories'}
                </button>
                {memoryRecallError ? <p>{memoryRecallError}</p> : null}
                {memoryDeleteError ? <p>{memoryDeleteError}</p> : null}
                {memoryRecallItems.length > 0 ? (
                  <div className="dw-settings-kv-list">
                    {memoryRecallItems.map((item) => (
                      <div key={item.id || `${item.scope}-${item.content}`} className="dw-settings-kv-item">
                        <span>{`${item.scope || 'memory'} / ${item.category || 'note'}`}</span>
                        <strong>{item.content}</strong>
                        <button type="button" className="secondary" onClick={() => onDeleteWorkerMemory(item.id)} disabled={!item.id || memoryDeletingId === item.id}>
                          {memoryDeletingId === item.id ? 'Forgetting...' : 'Forget'}
                        </button>                      </div>
                    ))}
                  </div>
                ) : <p>No recalled memories yet.</p>}
              </div>            </div>
            <div className="card-subtle dw-side-panel-block dw-settings-summary-card">
              <div className="dw-settings-summary-head">
                <div>
                  <label>连接与状态</label>
                  <strong>{healthBadgeLabel(healthStatus, healthError)}</strong>
                </div>
                <button type="button" className="secondary" onClick={onCheckCenterHealth} disabled={loading || healthChecking}>
                  {healthChecking ? '检测中...' : '测试中心连接'}
                </button>
              </div>
              <p>{loading ? '正在加载配置…' : settings.center.enabled ? '中心路由已启用' : '当前使用本地直连链路'}</p>
              <div className="dw-settings-kv-list">
                <div className="dw-settings-kv-item">
                  <span>状态</span>
                  <strong>{healthSummaryTitle(healthStatus, healthError)}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>地址</span>
                  <strong>{healthStatus?.resolvedBaseUrl || settings.center.baseUrl || '未设置中心地址'}</strong>
                </div>
                {healthStatus ? (
                  <>
                    <div className="dw-settings-kv-item">
                      <span>来源</span>
                      <strong>{healthSourceLabel(healthStatus.source)}</strong>
                    </div>
                    <div className="dw-settings-kv-item">
                      <span>最近检测</span>
                      <strong>{healthStatus.checkedAt}</strong>
                    </div>
                    <div className="dw-settings-kv-item">
                      <span>Provider 数量</span>
                      <strong>{healthStatus.providerCount}</strong>
                    </div>
                    {healthStatus.configPath ? (
                      <div className="dw-settings-kv-item">
                        <span>配置文件</span>
                        <strong>{healthStatus.configPath}</strong>
                      </div>
                    ) : null}
                    {centerReadiness ? (
                      <>
                        <div className="dw-settings-kv-item">
                          <span>iWorker readiness</span>
                          <strong>{centerReadiness.ready ? 'ready' : centerReadiness.status || 'needs setup'}</strong>
                        </div>
                        <div className="dw-settings-kv-item">
                          <span>Center assets</span>
                          <strong>{`${centerReadiness.tenantCount} tenants / ${centerReadiness.roleCount} roles / ${centerReadiness.colleagueCount} iWorkers`}</strong>
                        </div>
                        <div className="dw-settings-kv-item">
                          <span>Human auth</span>
                          <strong>{centerReadiness.authMethods.map((item) => `${item.method}:${item.status}`).join(' / ') || 'not reported'}</strong>
                        </div>
                      </>
                    ) : null}
                  </>
                ) : null}
              </div>
              {centerReadiness?.checks?.length ? (
                <div className="dw-settings-kv-list">
                  {centerReadiness.checks.map((check) => (
                    <div key={check.name} className="dw-settings-kv-item">
                      <span>{check.name}</span>
                      <strong>{check.ready ? 'ready' : check.status}{typeof check.count === 'number' ? ` / ${check.count}` : ''}</strong>
                    </div>
                  ))}
                </div>
              ) : null}
              {healthStatus?.message ? <p>{healthStatus.message}</p> : null}
              {healthError ? <p>{healthError}</p> : null}
            </div>
            <div className="card-subtle dw-side-panel-block dw-settings-summary-card">
              <label>当前配置</label>
              <div className="dw-settings-kv-list">
                <div className="dw-settings-kv-item">
                  <span>保存状态</span>
                  <strong>{dirty ? '有未保存更改' : '当前已保存'}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>角色</span>
                  <strong>{settings.roleProfile.name || '未设置角色名'}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>特点</span>
                  <strong>{settings.roleProfile.description || '未设置特点描述'}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>默认服务</span>
                  <strong>{settings.routing.defaultProvider || '未设置'}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>上游数量</span>
                  <strong>{settings.providers.length} 个</strong>
                </div>
              </div>
              <div className="dw-settings-save-row">
                <button type="button" className="primary" onClick={onSave} disabled={loading || saving || !dirty}>{saving ? '保存中...' : dirty ? '保存配置' : '已保存'}</button>
                <p>{saveMessage || error || (dirty ? '当前修改尚未保存。' : '当前配置与已保存版本一致。')}</p>
              </div>
            </div>
          </aside>
        </div>
      </section>
    </div>
  );
}
