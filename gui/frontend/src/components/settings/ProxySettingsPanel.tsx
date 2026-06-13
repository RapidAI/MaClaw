import type { Dispatch, SetStateAction } from 'react';
import { main } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';
import { ProxyScopeSettings } from './ProxyScopeSettings';

type ProxySettingsPanelProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    isWindows: boolean;
    lang: string;
    t: (key: string) => string;
};

const textForLang = localizeText;

const buildConfig = (config: main.AppConfig | null, patch: Record<string, any>) => new main.AppConfig({ ...(config || {}), ...patch });

export const ProxySettingsPanel = ({ config, setConfig, isWindows, lang, t }: ProxySettingsPanelProps) => {
    const updateConfig = (patch: Record<string, any>) => setConfig(buildConfig(config, patch));

    return (
    <div className="settings-panel proxy-settings-panel">
        <label className="proxy-settings-enable">
            <span className="proxy-settings-enable__text">{t("proxyEnabled")}</span>
            <span className="proxy-settings-switch">
                <input
                    type="checkbox"
                    aria-label={t("proxyEnabled")}
                    checked={config?.default_proxy_enabled || false}
                    onChange={(e) => updateConfig({ default_proxy_enabled: e.target.checked })}
                />
                <span aria-hidden="true" />
            </span>
        </label>

        <div className="proxy-settings-grid proxy-settings-grid--server">
            <div className="proxy-settings-field proxy-settings-field--protocol">
                <label className="form-label">{t("proxyProtocol")}</label>
                <select
                    className="form-input"
                    value={config?.default_proxy_protocol || 'http'}
                    onChange={(e) => updateConfig({ default_proxy_protocol: e.target.value })}
                >
                    <option value="http">HTTP</option>
                    <option value="https">HTTPS</option>
                    <option value="socks5">SOCKS5</option>
                </select>
            </div>
            <div className="proxy-settings-field">
                <label className="form-label">{t("proxyHost")}</label>
                <input
                    type="text"
                    className="form-input"
                    spellCheck={false}
                    placeholder={t("proxyHostPlaceholder")}
                    value={config?.default_proxy_host || ''}
                    onChange={(e) => updateConfig({ default_proxy_host: e.target.value })}
                />
            </div>
            <div className="proxy-settings-field proxy-settings-field--port">
                <label className="form-label">{t("proxyPort")}</label>
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

        <div className="proxy-settings-grid proxy-settings-grid--auth">
            <div className="proxy-settings-field">
                <label className="form-label">{t("proxyUsername")}</label>
                <input
                    type="text"
                    className="form-input"
                    spellCheck={false}
                    autoComplete="off"
                    value={config?.default_proxy_username || ''}
                    onChange={(e) => updateConfig({ default_proxy_username: e.target.value })}
                />
            </div>
            <div className="proxy-settings-field">
                <label className="form-label">{t("proxyPassword")}</label>
                <input
                    type="password"
                    className="form-input"
                    autoComplete="new-password"
                    value={config?.default_proxy_password || ''}
                    onChange={(e) => updateConfig({ default_proxy_password: e.target.value })}
                />
            </div>
        </div>

        <div className="proxy-settings-field">
            <label className="form-label">{t("proxyBypass")}</label>
            <textarea
                className="form-input"
                rows={2}
                spellCheck={false}
                placeholder={t("proxyBypassPlaceholder")}
                value={config?.default_proxy_bypass || ''}
                onChange={(e) => updateConfig({ default_proxy_bypass: e.target.value })}
            />
            <div className="proxy-settings-hint">{t("proxyBypassHint")}</div>
        </div>

        <ProxyScopeSettings config={config} isWindows={isWindows} t={t} updateConfig={updateConfig} />

        <div className="proxy-settings-actions">
            <button
                className="btn-primary"
                onClick={() => {
                    if (!config) return;
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
            >
                {textForLang(lang, 'Save', '\u4fdd\u5b58', '\u4fdd\u5b58')}
            </button>
        </div>
    </div>
    );
};
