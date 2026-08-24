import { Dispatch, SetStateAction, useRef, useState } from 'react';
import { SaveProxyConfig, TestProxyConfig } from '../../../wailsjs/go/main/App';
import { corelib } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';
import { ProxySettingsFields } from './ProxySettingsFields';
import { ProxyScopeSettings } from './ProxyScopeSettings';
import { buildConfig, errorText, proxyFormPayload } from './proxySettingsHelpers';

type ProxySettingsPanelProps = {
    config: corelib.AppConfig | null;
    setConfig: Dispatch<SetStateAction<corelib.AppConfig | null>>;
    isWindows: boolean;
    lang: string;
    t: (key: string) => string;
    showToastMessage?: (message: string, duration?: number) => void;
};

type ProxyTestState = {
    ok: boolean;
    text: string;
};

const textForLang = localizeText;

export const ProxySettingsPanel = ({ config, setConfig, isWindows, lang, t, showToastMessage }: ProxySettingsPanelProps) => {
    const [saving, setSaving] = useState(false);
    const [testing, setTesting] = useState(false);
    const [banner, setBanner] = useState<ProxyTestState | null>(null);
    const inflight = useRef(false);
    const updateConfig = (patch: Record<string, any>) => {
        setBanner(null);
        setConfig(buildConfig(config, patch));
    };
    const busy = saving || testing;
    const port = Number(config?.default_proxy_port?.trim());
    const canTest = Boolean(
        config?.default_proxy_enabled &&
        config.default_proxy_host?.trim() &&
        Number.isInteger(port) &&
        port >= 1 &&
        port <= 65535,
    );

    const saveProxy = async () => {
        if (!config || inflight.current) return;
        inflight.current = true;
        setSaving(true);
        try {
            await SaveProxyConfig(proxyFormPayload(config));
            setBanner({ ok: true, text: t('saved') });
            showToastMessage?.(t('saved'));
        } catch (err) {
            const message = errorText(err) || t('proxySaveFailed');
            const text = `${t('proxySaveFailed')}: ${message}`;
            setBanner({ ok: false, text });
            showToastMessage?.(text, 5000);
        } finally {
            inflight.current = false;
            setSaving(false);
        }
    };

    const testProxy = async () => {
        if (!config || inflight.current) return;
        if (!canTest) {
            setBanner({ ok: false, text: t('proxyTestNeedConfig') });
            return;
        }
        inflight.current = true;
        setTesting(true);
        setBanner(null);
        try {
            const result = await TestProxyConfig(proxyFormPayload(config));
            const latency = Number(result?.latency_ms || 0);
            const ip = typeof result?.egress_ip === 'string' ? result.egress_ip : '';
            if (result?.ok) {
                const text = [
                    t('proxyTestOK'),
                    result.status ? `HTTP ${result.status}` : '',
                    latency > 0 ? `${latency}ms` : '',
                    ip ? `IP ${ip}` : '',
                ].filter(Boolean).join(' · ');
                setBanner({ ok: true, text });
            } else {
                const detail = typeof result?.message === 'string' && result.message ? result.message : t('proxyTestFailed');
                setBanner({ ok: false, text: `${t('proxyTestFailed')}: ${detail}` });
            }
        } catch (err) {
            const message = errorText(err) || t('proxyTestFailed');
            setBanner({ ok: false, text: `${t('proxyTestFailed')}: ${message}` });
        } finally {
            inflight.current = false;
            setTesting(false);
        }
    };

    return (
    <div className="settings-panel proxy-settings-panel">
        <label className="proxy-settings-enable">
            <span className="proxy-settings-enable__text">{t("proxyEnabled")}</span>
            <span className="proxy-settings-switch">
                <input
                    type="checkbox"
                    aria-label={t("proxyEnabled")}
                    checked={config?.default_proxy_enabled || false}
                    onChange={(e) => {
                        const enabled = e.target.checked;
                        const patch: Record<string, any> = { default_proxy_enabled: enabled };
                        if (
                            enabled &&
                            !config?.default_proxy_scope_maclaw &&
                            !config?.default_proxy_scope_coding_tools &&
                            !config?.default_proxy_scope_agent
                        ) {
                            patch.default_proxy_scope_maclaw = true;
                        }
                        updateConfig(patch);
                    }}
                />
                <span aria-hidden="true" />
            </span>
        </label>

        <ProxySettingsFields config={config} t={t} updateConfig={updateConfig} />
        <ProxyScopeSettings config={config} isWindows={isWindows} t={t} updateConfig={updateConfig} />
        <div className="proxy-settings-hint">{t("proxyTestHint")}</div>
        {banner && (
            <div className="proxy-settings-status" data-ok={banner.ok ? 'true' : 'false'} role="status">
                {banner.text}
            </div>
        )}
        <div className="proxy-settings-actions">
            <button type="button" className="btn-secondary" disabled={busy || !canTest} aria-busy={testing} onClick={() => { void testProxy(); }}>
                {testing ? t('proxyTesting') : t('proxyTest')}
            </button>
            <button type="button" className="btn-primary" disabled={busy || !config} aria-busy={saving} onClick={() => { void saveProxy(); }}>
                {saving ? t('saving') : textForLang(lang, 'Save', '\u4fdd\u5b58', '\u4fdd\u5b58')}
            </button>
        </div>
    </div>
    );
};
