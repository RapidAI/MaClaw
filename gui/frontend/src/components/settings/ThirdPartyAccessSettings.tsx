import type { CSSProperties, Dispatch, SetStateAction } from 'react';
import { BrowserOpenURL } from '../../../wailsjs/runtime';
import { LoadConfig, RestartThirdPartyGateway, SetThirdPartyGatewayLocalMode, StopThirdPartyGateway } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { watchLabel } from './imSettingsShared';

type ThirdPartyAccessSettingsProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
    imAuditBtnStyle: CSSProperties;
    saveRemoteConfigField: (patch: Record<string, any>) => any;
    showToastMessage: (message: string) => void;
    setIMAuditPlatform: Dispatch<SetStateAction<string | null>>;
    thirdPartyGatewayStatus: string;
    setThirdPartyGatewayStatus: Dispatch<SetStateAction<string>>;
    thirdPartyGatewayLocalMode: boolean;
    setThirdPartyGatewayLocalModeState: Dispatch<SetStateAction<boolean>>;
};

const channelModeLabel = (lang: string) => (lang === 'zh-Hans' || lang === 'zh-Hant' ? '\u901a\u9053\uff1a' : 'Mode:');

export const ThirdPartyAccessSettings = ({
    config,
    setConfig,
    lang,
    imAuditBtnStyle,
    saveRemoteConfigField,
    showToastMessage,
    setIMAuditPlatform,
    thirdPartyGatewayStatus,
    setThirdPartyGatewayStatus,
    thirdPartyGatewayLocalMode,
    setThirdPartyGatewayLocalModeState,
}: ThirdPartyAccessSettingsProps) => (
        <div className="form-group" style={{ marginTop: '0', borderTop: 'none', paddingTop: '0' }}>
            <p style={{ fontSize: '0.72rem', color: 'var(--theme-text-muted)', marginBottom: '12px', marginTop: 0 }}>
                {lang === 'zh-Hans'
                    ? '\u5f00\u653e\u672c\u673a HTTP \u6d88\u606f\u63a5\u5165\u7aef\u53e3\uff0c\u7b2c\u4e09\u65b9\u8f6f\u4ef6\u4e3b\u52a8\u8fde\u63a5 MaClaw\uff0c\u65e0\u9700\u63d0\u4f9b\u56de\u8c03\u5730\u5740\u3002'
                    : lang === 'zh-Hant'
                    ? '\u958b\u653e\u672c\u6a5f HTTP \u6d88\u606f\u63a5\u5165\u7aef\u53e3\uff0c\u7b2c\u4e09\u65b9\u8edf\u9ad4\u4e3b\u52d5\u9023\u63a5 MaClaw\uff0c\u7121\u9700\u63d0\u4f9b\u56de\u8abf\u5730\u5740\u3002'
                    : 'Expose a local HTTP message gateway. Third-party software connects to MaClaw without a callback URL.'}
            </p>
            <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '12px', flexWrap: 'wrap' }}>
                <label style={{ display: 'flex', alignItems: 'center', gap: '6px', cursor: 'pointer', fontSize: '0.78rem' }}>
                    <input type="checkbox" checked={(config as any)?.thirdparty_gateway_enabled || false} onChange={async (e) => {
                        const enabled = e.target.checked;
                        const patch: any = { thirdparty_gateway_enabled: enabled };
                        if (enabled && !String((config as any)?.thirdparty_gateway_token || '').trim()) {
                            const bytes = new Uint8Array(32);
                            window.crypto.getRandomValues(bytes);
                            patch.thirdparty_gateway_token = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
                        }
                        await saveRemoteConfigField(patch);
                        if (enabled) {
                            try { const st = await RestartThirdPartyGateway(); setThirdPartyGatewayStatus(typeof st === 'string' ? st : 'disconnected'); }
                            catch (err: any) { showToastMessage(err?.message || String(err)); }
                        } else {
                            try { await StopThirdPartyGateway(); } catch {}
                            setThirdPartyGatewayStatus('disconnected');
                        }
                    }} />
                    {lang === 'zh-Hans' ? '\u5f00\u542f\u7b2c\u4e09\u65b9\u8f6f\u4ef6\u63a5\u5165' : lang === 'zh-Hant' ? '\u958b\u555f\u7b2c\u4e09\u65b9\u8edf\u9ad4\u63a5\u5165' : 'Enable third-party access'}
                </label>
                <span style={{ fontSize: '0.7rem', padding: '2px 8px', borderRadius: '10px', background: thirdPartyGatewayStatus === 'connected' ? 'var(--theme-success-bg)' : thirdPartyGatewayStatus === 'error' ? 'var(--theme-danger-bg)' : 'var(--theme-surface-muted)', color: thirdPartyGatewayStatus === 'connected' ? 'var(--theme-success)' : thirdPartyGatewayStatus === 'error' ? 'var(--theme-danger)' : 'var(--theme-text-secondary)' }}>
                    {{ connected: lang === 'en' ? 'Running' : '\u5df2\u542f\u52a8', connecting: lang === 'en' ? 'Starting' : '\u542f\u52a8\u4e2d', disconnected: lang === 'en' ? 'Stopped' : '\u672a\u8fde\u63a5', disabled: lang === 'en' ? 'Disabled' : '\u672a\u542f\u7528', error: lang === 'en' ? 'Error' : '\u9519\u8bef' }[thirdPartyGatewayStatus] || thirdPartyGatewayStatus}
                </span>
                <button type="button" style={{ fontSize: '0.68rem', padding: '2px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', background: 'transparent', color: 'var(--theme-text-secondary)', cursor: 'pointer' }} disabled={!(config as any)?.thirdparty_gateway_enabled} onClick={async () => {
                    try { const st = await RestartThirdPartyGateway(); setThirdPartyGatewayStatus(typeof st === 'string' ? st : 'disconnected'); }
                    catch (e: any) { showToastMessage(e?.message || String(e)); }
                }}>
                    {lang === 'zh-Hans' ? '\u91cd\u542f\u63a5\u53e3' : lang === 'zh-Hant' ? '\u91cd\u555f\u4ecb\u9762' : 'Restart'}
                </button>
                <button type="button" onClick={() => setIMAuditPlatform('thirdparty')} style={{ ...imAuditBtnStyle, marginLeft: '18px' }}>
                    {watchLabel(lang)}
                </button>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '16px', flexWrap: 'wrap' }}>
                <span style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)' }}>{channelModeLabel(lang)}</span>
                {[{ value: true, label: lang === 'zh-Hans' || lang === 'zh-Hant' ? '\u5355\u673a' : 'Local', desc: lang === 'zh-Hans' || lang === 'zh-Hant' ? '\u672c\u673a Agent \u76f4\u63a5\u5904\u7406' : 'Handle with local Agent' }, { value: false, label: lang === 'zh-Hans' || lang === 'zh-Hant' ? '\u591a\u673a' : 'Hub', desc: lang === 'zh-Hans' || lang === 'zh-Hant' ? '\u901a\u8fc7 Hub \u8f6c\u53d1\u5230\u5728\u7ebf\u8bbe\u5907' : 'Forward through Hub' }].map((opt) => (
                    <button key={String(opt.value)} type="button" aria-label={opt.desc} title={opt.desc} style={{ padding: '4px 14px', borderRadius: '14px', border: thirdPartyGatewayLocalMode === opt.value ? '1.5px solid var(--theme-primary)' : '1px solid var(--theme-border)', background: thirdPartyGatewayLocalMode === opt.value ? 'var(--theme-info-bg)' : 'transparent', color: thirdPartyGatewayLocalMode === opt.value ? 'var(--theme-primary)' : 'var(--theme-text-secondary)', fontWeight: thirdPartyGatewayLocalMode === opt.value ? 600 : 400, fontSize: '0.75rem', cursor: 'pointer' }} onClick={() => {
                        const prev = thirdPartyGatewayLocalMode;
                        setThirdPartyGatewayLocalModeState(opt.value);
                        SetThirdPartyGatewayLocalMode(opt.value).then(() => { LoadConfig().then((c: any) => setConfig(c)).catch(() => {}); }).catch((err: any) => {
                            setThirdPartyGatewayLocalModeState(prev);
                            showToastMessage(err?.message || err || '\u5207\u6362\u5931\u8d25');
                        });
                    }}>{opt.label}</button>
                ))}
            </div>

            <div style={{ maxWidth: '760px', display: 'grid', gap: '10px' }}>
                <div style={{ display: 'grid', gridTemplateColumns: 'minmax(180px, 1fr) 110px', gap: '10px' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                        <label style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)', whiteSpace: 'nowrap', minWidth: '64px' }}>Host</label>
                        <input type="text" value={(config as any)?.thirdparty_gateway_host || '127.0.0.1'} onChange={(e) => saveRemoteConfigField({ thirdparty_gateway_host: e.target.value } as any)} placeholder="127.0.0.1" spellCheck={false} style={{ flex: 1, minWidth: 0, padding: '6px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', fontSize: '0.78rem', background: 'var(--theme-surface)', color: 'var(--theme-text-primary)' }} />
                    </div>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                        <label style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)', whiteSpace: 'nowrap' }}>Port</label>
                        <input type="number" min={1} max={65535} value={(config as any)?.thirdparty_gateway_port || 18777} onChange={(e) => saveRemoteConfigField({ thirdparty_gateway_port: Number(e.target.value || 18777) } as any)} style={{ width: '86px', padding: '6px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', fontSize: '0.78rem', background: 'var(--theme-surface)', color: 'var(--theme-text-primary)' }} />
                    </div>
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                    <label style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)', whiteSpace: 'nowrap', minWidth: '64px' }}>Token</label>
                    <input type="password" value={(config as any)?.thirdparty_gateway_token || ''} onChange={(e) => saveRemoteConfigField({ thirdparty_gateway_token: e.target.value } as any)} placeholder="Bearer token" autoComplete="off" style={{ flex: 1, minWidth: 0, padding: '6px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', fontSize: '0.78rem', background: 'var(--theme-surface)', color: 'var(--theme-text-primary)' }} />
                    <button type="button" style={{ fontSize: '0.68rem', padding: '3px 10px', borderRadius: '4px', border: '1px solid var(--theme-primary)', background: 'transparent', color: 'var(--theme-primary)', cursor: 'pointer', whiteSpace: 'nowrap' }} onClick={async () => {
                        const bytes = new Uint8Array(32);
                        window.crypto.getRandomValues(bytes);
                        const token = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
                        await saveRemoteConfigField({ thirdparty_gateway_token: token } as any);
                        showToastMessage(lang === 'en' ? 'Token generated' : '\u5df2\u751f\u6210 Token');
                    }}>{lang === 'en' ? 'Generate Token' : '\u751f\u6210 Token'}</button>
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap', fontSize: '0.72rem', color: 'var(--theme-text-muted)' }}>
                    <code style={{ padding: '3px 6px', borderRadius: '4px', background: 'var(--theme-surface-muted)', color: 'var(--theme-text-primary)' }}>{`http://${(config as any)?.thirdparty_gateway_host || '127.0.0.1'}:${(config as any)?.thirdparty_gateway_port || 18777}/api/im-gateway/v1`}</code>
                    <button type="button" style={{ fontSize: '0.68rem', padding: '2px 8px', borderRadius: '4px', border: '1px solid var(--theme-primary)', background: 'transparent', color: 'var(--theme-primary)', cursor: 'pointer' }} onClick={() => {
                        const base = String((config as any)?.remote_hub_url || '').replace(/\/+$/, '');
                        BrowserOpenURL(base ? base + '/connector' : '/connector');
                    }}>{lang === 'zh-Hans' ? '\u6253\u5f00\u63a5\u5165\u6587\u6863' : lang === 'zh-Hant' ? '\u958b\u555f\u63a5\u5165\u6587\u4ef6' : 'Open docs'}</button>
                </div>
            </div>
        </div>
);
