import type { Dispatch, SetStateAction } from 'react';
import { SaveConfig } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { ProxyScopeSettings } from './ProxyScopeSettings';

type ProxySettingsPanelProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    isWindows: boolean;
    lang: string;
    t: (key: string) => string;
};

const textForLang = (lang: string, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' || lang === 'zh' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

const buildConfig = (config: main.AppConfig | null, patch: Record<string, any>) => new main.AppConfig({ ...(config || {}), ...patch });

export const ProxySettingsPanel = ({ config, setConfig, isWindows, lang, t }: ProxySettingsPanelProps) => {
    const updateConfig = (patch: Record<string, any>) => setConfig(buildConfig(config, patch));

    return (
    <div className="settings-panel">
        <div style={{ marginBottom: '15px', display: 'flex', alignItems: 'center', gap: '10px' }}>
            <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', gap: '8px' }}>
                <input
                    type="checkbox"
                    checked={config?.default_proxy_enabled || false}
                    onChange={(e) => updateConfig({ default_proxy_enabled: e.target.checked })}
                />
                <span style={{ fontWeight: 500, color: 'var(--theme-text-primary)' }}>{t("proxyEnabled")}</span>
            </label>
        </div>

        <div style={{ display: 'flex', gap: '10px', marginBottom: '12px' }}>
            <div style={{ width: '110px', flexShrink: 0 }}>
                <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyProtocol")}</label>
                <select
                    className="form-input"
                    style={{ height: '34px' }}
                    value={config?.default_proxy_protocol || 'http'}
                    onChange={(e) => updateConfig({ default_proxy_protocol: e.target.value })}
                >
                    <option value="http">HTTP</option>
                    <option value="https">HTTPS</option>
                    <option value="socks5">SOCKS5</option>
                </select>
            </div>
            <div style={{ flex: 1 }}>
                <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyHost")}</label>
                <input
                    type="text"
                    className="form-input"
                    spellCheck={false}
                    placeholder={t("proxyHostPlaceholder")}
                    value={config?.default_proxy_host || ''}
                    onChange={(e) => updateConfig({ default_proxy_host: e.target.value })}
                />
            </div>
            <div style={{ width: '90px', flexShrink: 0 }}>
                <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyPort")}</label>
                <input
                    type="text"
                    className="form-input"
                    spellCheck={false}
                    placeholder={t("proxyPortPlaceholder")}
                    value={config?.default_proxy_port || ''}
                    onChange={(e) => updateConfig({ default_proxy_port: e.target.value })}
                />
            </div>
        </div>

        <div style={{ display: 'flex', gap: '10px', marginBottom: '12px' }}>
            <div style={{ flex: 1 }}>
                <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyUsername")}</label>
                <input
                    type="text"
                    className="form-input"
                    spellCheck={false}
                    autoComplete="off"
                    value={config?.default_proxy_username || ''}
                    onChange={(e) => updateConfig({ default_proxy_username: e.target.value })}
                />
            </div>
            <div style={{ flex: 1 }}>
                <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyPassword")}</label>
                <input
                    type="password"
                    className="form-input"
                    autoComplete="new-password"
                    value={config?.default_proxy_password || ''}
                    onChange={(e) => updateConfig({ default_proxy_password: e.target.value })}
                />
            </div>
        </div>

        <div style={{ marginBottom: '12px' }}>
            <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyBypass")}</label>
            <textarea
                className="form-input"
                rows={2}
                spellCheck={false}
                placeholder={t("proxyBypassPlaceholder")}
                value={config?.default_proxy_bypass || ''}
                onChange={(e) => updateConfig({ default_proxy_bypass: e.target.value })}
                style={{ resize: 'vertical', minHeight: '40px', fontFamily: 'monospace', fontSize: '0.78rem' }}
            />
            <div style={{ fontSize: '0.7rem', color: 'var(--theme-text-muted)', marginTop: '3px' }}>{t("proxyBypassHint")}</div>
        </div>

        <ProxyScopeSettings config={config} isWindows={isWindows} t={t} updateConfig={updateConfig} />

        <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: '20px' }}>
            <button
                className="btn-primary"
                onClick={() => {
                    if (!config) return;
                    SaveConfig(config);
                    try {
                        (window as any).go?.main?.App?.SaveProxyConfig?.({
                            enabled: config.default_proxy_enabled || false,
                            protocol: config.default_proxy_protocol || 'http',
                            host: config.default_proxy_host || '',
                            port: config.default_proxy_port || '',
                            username: config.default_proxy_username || '',
                            password: config.default_proxy_password || '',
                            bypass: config.default_proxy_bypass || '',
                            scope_maclaw: config.default_proxy_scope_maclaw || false,
                            scope_coding_tools: config.default_proxy_scope_coding_tools || false,
                            scope_agent: config.default_proxy_scope_agent || false,
                        });
                    } catch {}
                }}
                style={{ padding: '8px 16px' }}
            >
                {textForLang(lang, 'Save', '\u4fdd\u5b58', '\u4fdd\u5b58')}
            </button>
        </div>
    </div>
    );
};
