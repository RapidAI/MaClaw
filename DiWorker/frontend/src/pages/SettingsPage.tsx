import { useEffect, useState } from 'react';
import type { CenterHealthStatus, DiWorkerSettings, UpstreamProvider } from '../types';

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
  onRoleNameChange: (value: string) => void;
  onRoleDescriptionChange: (value: string) => void;
  onCenterEnabledChange: (value: boolean) => void;
  onCenterHostChange: (value: string) => void;
  onCenterPortChange: (value: string) => void;
  onCenterBaseUrlChange: (value: string) => void;
  onCenterTimeoutChange: (value: string) => void;
  onRoutingModeChange: (value: DiWorkerSettings['routing']['mode']) => void;
  onRoutingDefaultProviderChange: (value: string) => void;
  onRoutingAllowFallbackChange: (value: boolean) => void;
  onProviderChange: (providerId: string, patch: Partial<UpstreamProvider>) => void;
  onProviderFeaturesChange: (providerId: string, value: string) => void;
  onCheckCenterHealth: () => void;
  onSave: () => void;
};

const SectionIcon = ({ children }: { children: string }) => <span className="dw-inline-icon" aria-hidden="true">{children}</span>;

const healthBadgeLabel = (healthStatus: CenterHealthStatus | null, healthError: string) => {
  if (healthError) {
    return '鎺㈡祴寮傚父';
  }
  if (!healthStatus) {
    return '鏈娴?;
  }
  return healthStatus.reachable ? '杩炴帴姝ｅ父' : '杩炴帴涓嶅彲杈?;
};

const healthSummaryTitle = (healthStatus: CenterHealthStatus | null, healthError: string) => {
  if (healthError) {
    return '妫€娴嬭繑鍥炲紓甯?;
  }
  if (!healthStatus) {
    return '灏氭湭鑾峰彇涓績蹇収';
  }
  return healthStatus.reachable ? '涓績杩炴帴姝ｅ父' : '涓績鏆備笉鍙揪';
};

const healthSourceLabel = (source: CenterHealthStatus['source']) => source === 'manual' ? '鎵嬪姩妫€娴? : '淇濆瓨鍚庤嚜鍔ㄦ娴?;

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
  onRoleNameChange,
  onRoleDescriptionChange,
  onCenterEnabledChange,
  onCenterHostChange,
  onCenterPortChange,
  onCenterBaseUrlChange,
  onCenterTimeoutChange,
  onRoutingModeChange,
  onRoutingDefaultProviderChange,
  onRoutingAllowFallbackChange,
  onProviderChange,
  onProviderFeaturesChange,
  onCheckCenterHealth,
  onSave,
}: Props) {
  const [expandedProviderId, setExpandedProviderId] = useState<string | null>(null);

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
            <span className="eyebrow">閰嶇疆</span>
            <h2>鏁板瓧鍛樺伐涓績閰嶇疆</h2>
          </div>
          <div className="dw-settings-header-meta">
            {dirty ? <span className="dw-settings-dirty-badge">鏈夋湭淇濆瓨鏇存敼</span> : <span className="dw-settings-clean-badge">褰撳墠宸蹭繚瀛?/span>}
            <small>绠＄悊宸︿笅瑙掕鑹蹭俊鎭€佷腑蹇冨湴鍧€涓庡涓婃父鏈嶅姟璋冨害绛栫暐銆?/small>
          </div>
        </div>
        <div className="dw-task-layout dw-settings-layout">
          <div className="dw-task-main dw-editor-main">
            <section className="card-subtle dw-editor-section dw-settings-section dw-settings-section-compact">
              <div className="dw-pane-head">
                <strong><SectionIcon>鈼?/SectionIcon>瑙掕壊淇℃伅</strong>
                <span>鐢ㄤ簬宸︿笅瑙掑睍绀?/span>
              </div>
              <div className="dw-form-grid">
                <label>
                  瑙掕壊鍚?                  <input value={settings.roleProfile.name} onChange={(event) => onRoleNameChange(event.target.value)} placeholder="渚嬪锛氬皬杩? />
                </label>
                <label>
                  鐗圭偣鎻忚堪
                  <input value={settings.roleProfile.description} onChange={(event) => onRoleDescriptionChange(event.target.value)} placeholder="渚嬪锛氭搮闀跨邯瑕併€侀€氱煡涓庢眹鎶ユ暣鐞? />
                </label>
              </div>
            </section>

            <section className="card-subtle dw-editor-section dw-settings-section dw-settings-section-compact">
              <div className="dw-pane-head">
                <strong><SectionIcon>鈱?/SectionIcon>涓績杩炴帴</strong>
                <span>Wails 鎻愪氦浠诲姟浼樺厛璧拌繖閲?/span>
              </div>
              <div className="dw-settings-group-list">
                <section className="dw-settings-group">
                  <div className="dw-settings-group-head">
                    <strong>鍩虹杩炴帴</strong>
                    <span>涓績鍦板潃涓庤姹傝秴鏃?/span>
                  </div>
                  <div className="dw-form-grid">
                    <label>
                      鍚敤涓績
                      <select value={settings.center.enabled ? 'enabled' : 'disabled'} onChange={(event) => onCenterEnabledChange(event.target.value === 'enabled')}>
                        <option value="enabled">鍚敤</option>
                        <option value="disabled">鍏抽棴</option>
                      </select>
                    </label>
                    <label>
                      鍦板潃
                      <input value={settings.center.host} onChange={(event) => onCenterHostChange(event.target.value)} placeholder="127.0.0.1" />
                    </label>
                    <label>
                      绔彛
                      <input value={String(settings.center.port)} onChange={(event) => onCenterPortChange(event.target.value)} placeholder="9377" />
                    </label>
                    <label>
                      Base URL
                      <input value={settings.center.baseUrl} onChange={(event) => onCenterBaseUrlChange(event.target.value)} placeholder="http://127.0.0.1:9377" />
                    </label>
                    <label className="dw-settings-field-span-2">
                      瓒呮椂锛堢锛?                      <input value={String(settings.center.timeoutSec)} onChange={(event) => onCenterTimeoutChange(event.target.value)} placeholder="60" />
                    </label>
                  </div>
                </section>
                <section className="dw-settings-group">
                  <div className="dw-settings-group-head">
                    <strong>璺敱绛栫暐</strong>
                    <span>鎺у埗榛樿涓婃父涓庡け璐ュ洖閫€</span>
                  </div>
                  <div className="dw-form-grid">
                    <label>
                      璺敱妯″紡
                      <select value={settings.routing.mode} onChange={(event) => onRoutingModeChange(event.target.value as DiWorkerSettings['routing']['mode'])}>
                        <option value="smart">鏅鸿兘璋冨害</option>
                        <option value="priority">浼樺厛绾т紭鍏?/option>
                        <option value="manual">鎵嬪姩榛樿</option>
                      </select>
                    </label>
                    <label>
                      榛樿鏈嶅姟
                      <input value={settings.routing.defaultProvider} onChange={(event) => onRoutingDefaultProviderChange(event.target.value)} placeholder="office-openai" />
                    </label>
                    <label className="dw-settings-field-span-2">
                      澶辫触鍥為€€
                      <select value={settings.routing.allowFallback ? 'enabled' : 'disabled'} onChange={(event) => onRoutingAllowFallbackChange(event.target.value === 'enabled')}>
                        <option value="enabled">鍏佽</option>
                        <option value="disabled">鍏抽棴</option>
                      </select>
                    </label>
                  </div>
                </section>
              </div>
            </section>

            <section className="card-subtle dw-editor-section dw-settings-section dw-settings-section-compact">
              <div className="dw-pane-head">
                <strong><SectionIcon>鈬?/SectionIcon>涓婃父鏈嶅姟</strong>
                <span>{settings.providers.length} 涓湇鍔?/span>
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
                            <p>{provider.description || '鏈缃鏄?}</p>
                          </div>
                          <div className="dw-provider-card-badges">
                            <span>{provider.protocol.toUpperCase()}</span>
                            <span>{provider.enabled ? '宸插惎鐢? : '宸插叧闂?}</span>
                          </div>
                        </div>
                        <div className="dw-provider-meta-row">
                          <span>妯″瀷锛歿provider.model || '鏈缃?}</span>
                          <span>浼樺厛绾э細{provider.priority}</span>
                          <span>涓婁笅鏂囷細{provider.capabilities.maxContext || 0}</span>
                        </div>
                        <div className="dw-provider-feature-row">
                          <span>{provider.capabilities.supportsStream ? '娴佸紡杈撳嚭' : '闈炴祦寮?}</span>
                          <span>{provider.capabilities.supportsVision ? '鏀寔瑙嗚' : '绾枃鏈?}</span>
                          <span>{provider.features.length ? provider.features.join(' / ') : '鏈缃壒鐐?}</span>
                        </div>
                        <div className="dw-provider-summary-foot">
                          <span>{isExpanded ? '姝ｅ湪缂栬緫褰撳墠鏈嶅姟' : '榛樿浠呮樉绀烘憳瑕?}</span>
                          <button
                            type="button"
                            className="secondary dw-provider-toggle"
                            aria-expanded={isExpanded}
                            aria-controls={detailsId}
                            onClick={() => setExpandedProviderId((current) => current === provider.id ? null : provider.id)}
                          >
                            {isExpanded ? `鏀惰捣缂栬緫 ${provider.name}` : `灞曞紑缂栬緫 ${provider.name}`}
                          </button>
                        </div>
                      </div>
                      {isExpanded ? (
                        <div id={detailsId} className="dw-provider-details">
                          <div className="dw-form-grid dw-provider-grid">
                            <label>
                              鍚敤
                              <select value={provider.enabled ? 'enabled' : 'disabled'} onChange={(event) => onProviderChange(provider.id, { enabled: event.target.value === 'enabled' })}>
                                <option value="enabled">鍚敤</option>
                                <option value="disabled">鍏抽棴</option>
                              </select>
                            </label>
                            <label>
                              鍚嶇О
                              <input value={provider.name} onChange={(event) => onProviderChange(provider.id, { name: event.target.value })} />
                            </label>
                            <label>
                              鍗忚
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
                              妯″瀷
                              <input value={provider.model} onChange={(event) => onProviderChange(provider.id, { model: event.target.value })} />
                            </label>
                            <label>
                              浼樺厛绾?                              <input value={String(provider.priority)} onChange={(event) => onProviderChange(provider.id, { priority: Number(event.target.value) || 0 })} />
                            </label>
                            <label>
                              鏈€澶т笂涓嬫枃
                              <input value={String(provider.capabilities.maxContext)} onChange={(event) => onProviderChange(provider.id, { capabilities: { ...provider.capabilities, maxContext: Number(event.target.value) || 0 } })} />
                            </label>
                            <label className="dw-provider-field-span-3">
                              鏈嶅姟鐗圭偣
                              <input value={provider.features.join('锛?)} onChange={(event) => onProviderFeaturesChange(provider.id, event.target.value)} placeholder="鍏枃锛屼腑鏂囷紝缁撴瀯鍖? />
                            </label>
                            <label>
                              鏀寔娴佸紡
                              <select value={provider.capabilities.supportsStream ? 'enabled' : 'disabled'} onChange={(event) => onProviderChange(provider.id, { capabilities: { ...provider.capabilities, supportsStream: event.target.value === 'enabled' } })}>
                                <option value="enabled">鏀寔</option>
                                <option value="disabled">鍏抽棴</option>
                              </select>
                            </label>
                            <label>
                              鏀寔瑙嗚
                              <select value={provider.capabilities.supportsVision ? 'enabled' : 'disabled'} onChange={(event) => onProviderChange(provider.id, { capabilities: { ...provider.capabilities, supportsVision: event.target.value === 'enabled' } })}>
                                <option value="enabled">鏀寔</option>
                                <option value="disabled">鍏抽棴</option>
                              </select>
                            </label>
                            <label>
                              璇存槑
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
                  <label>杩炴帴涓庣姸鎬?/label>
                  <strong>{healthBadgeLabel(healthStatus, healthError)}</strong>
                </div>
                <button type="button" className="secondary" onClick={onCheckCenterHealth} disabled={loading || healthChecking}>
                  {healthChecking ? '妫€娴嬩腑...' : '娴嬭瘯涓績杩炴帴'}
                </button>
              </div>
              <p>{loading ? '姝ｅ湪鍔犺浇閰嶇疆鈥? : settings.center.enabled ? '涓績璺敱宸插惎鐢? : '褰撳墠浣跨敤鏈湴鐩磋繛閾捐矾'}</p>
              <div className="dw-settings-kv-list">
                <div className="dw-settings-kv-item">
                  <span>鐘舵€?/span>
                  <strong>{healthSummaryTitle(healthStatus, healthError)}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>鍦板潃</span>
                  <strong>{healthStatus?.resolvedBaseUrl || settings.center.baseUrl || '鏈缃腑蹇冨湴鍧€'}</strong>
                </div>
                {healthStatus ? (
                  <>
                    <div className="dw-settings-kv-item">
                      <span>鏉ユ簮</span>
                      <strong>{healthSourceLabel(healthStatus.source)}</strong>
                    </div>
                    <div className="dw-settings-kv-item">
                      <span>鏈€杩戞娴?/span>
                      <strong>{healthStatus.checkedAt}</strong>
                    </div>
                    <div className="dw-settings-kv-item">
                      <span>Provider 鏁伴噺</span>
                      <strong>{healthStatus.providerCount}</strong>
                    </div>
                    {healthStatus.configPath ? (
                      <div className="dw-settings-kv-item">
                        <span>閰嶇疆鏂囦欢</span>
                        <strong>{healthStatus.configPath}</strong>
                      </div>
                    ) : null}
                  </>
                ) : null}
              </div>
              {healthStatus?.message ? <p>{healthStatus.message}</p> : null}
              {healthError ? <p>{healthError}</p> : null}
            </div>
            <div className="card-subtle dw-side-panel-block dw-settings-summary-card">
              <label>褰撳墠閰嶇疆</label>
              <div className="dw-settings-kv-list">
                <div className="dw-settings-kv-item">
                  <span>淇濆瓨鐘舵€?/span>
                  <strong>{dirty ? '鏈夋湭淇濆瓨鏇存敼' : '褰撳墠宸蹭繚瀛?}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>瑙掕壊</span>
                  <strong>{settings.roleProfile.name || '鏈缃鑹插悕'}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>鐗圭偣</span>
                  <strong>{settings.roleProfile.description || '鏈缃壒鐐规弿杩?}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>榛樿鏈嶅姟</span>
                  <strong>{settings.routing.defaultProvider || '鏈缃?}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>涓婃父鏁伴噺</span>
                  <strong>{settings.providers.length} 涓?/strong>
                </div>
              </div>
              <div className="dw-settings-save-row">
                <button type="button" className="primary" onClick={onSave} disabled={loading || saving || !dirty}>{saving ? '淇濆瓨涓?..' : dirty ? '淇濆瓨閰嶇疆' : '宸蹭繚瀛?}</button>
                <p>{saveMessage || error || (dirty ? '褰撳墠淇敼灏氭湭淇濆瓨銆? : '褰撳墠閰嶇疆涓庡凡淇濆瓨鐗堟湰涓€鑷淬€?)}</p>
              </div>
            </div>
          </aside>
        </div>
      </section>
    </div>
  );
}
