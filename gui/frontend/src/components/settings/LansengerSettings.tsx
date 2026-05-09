import type { CSSProperties, Dispatch, SetStateAction } from 'react';
import { LoadConfig, RestartLansenger, SetLansengerLocalMode } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { channelModeLabel, connectionBadgeStyle, connectionStatusLabel, localModeOptions, pillButtonStyle, restartLabel, switchFailedLabel, textForLang, watchLabel } from './imSettingsShared';

type LansengerSettingsProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
    imAuditBtnStyle: CSSProperties;
    saveRemoteConfigField: (patch: Record<string, any>) => any;
    lansengerStatus: string;
    setLansengerStatus: Dispatch<SetStateAction<string>>;
    lansengerLocalMode: boolean;
    setLansengerLocalModeState: Dispatch<SetStateAction<boolean>>;
    setIMAuditPlatform: Dispatch<SetStateAction<string | null>>;
};

export const LansengerSettings = ({
    config,
    setConfig,
    lang,
    imAuditBtnStyle,
    saveRemoteConfigField,
    lansengerStatus,
    setLansengerStatus,
    lansengerLocalMode,
    setLansengerLocalModeState,
    setIMAuditPlatform,
}: LansengerSettingsProps) => (
    <div className="form-group" style={{ marginTop: '0', borderTop: 'none', paddingTop: '0' }}>
        <p style={{ fontSize: '0.72rem', color: 'var(--theme-text-muted)', marginBottom: '12px', marginTop: 0 }}>
            {textForLang(lang, 'Configure Lansenger access for TigerClaw Agent messages.', '\u914d\u7f6e\u84dd\u4fe1\u63a5\u5165\uff0c\u7528\u84dd\u4fe1\u4e0e TigerClaw Agent \u5bf9\u8bdd\u3002', '\u914d\u7f6e\u85cd\u4fe1\u63a5\u5165\uff0c\u7528\u85cd\u4fe1\u8207 TigerClaw Agent \u5c0d\u8a71\u3002')}
        </p>
        <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '12px', flexWrap: 'wrap' }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: '6px', cursor: 'pointer', fontSize: '0.78rem' }}>
                <input
                    type="checkbox"
                    checked={(config as any)?.lansenger_enabled || false}
                    onChange={(e) => saveRemoteConfigField({ lansenger_enabled: e.target.checked } as any)}
                />
                {textForLang(lang, 'Enable Lansenger', '\u542f\u7528\u84dd\u4fe1', '\u555f\u7528\u85cd\u4fe1')}
            </label>
            {(config as any)?.lansenger_enabled && (
                <>
                    <span style={connectionBadgeStyle(lansengerStatus)}>{connectionStatusLabel(lansengerStatus, lang)}</span>
                    <button
                        type="button"
                        style={{ fontSize: '0.68rem', padding: '2px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', background: 'transparent', color: 'var(--theme-text-secondary)', cursor: 'pointer' }}
                        onClick={() => RestartLansenger().then(setLansengerStatus).catch((err: any) => {
                            alert(err?.message || err || restartLabel(lang));
                        })}
                    >
                        {restartLabel(lang)}
                    </button>
                </>
            )}
            <button type="button" onClick={() => setIMAuditPlatform('lansenger')} style={{ ...imAuditBtnStyle, marginLeft: (config as any)?.lansenger_enabled ? '18px' : '0' }}>
                {watchLabel(lang)}
            </button>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '16px' }}>
            <span style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)' }}>{channelModeLabel(lang)}</span>
            {localModeOptions(lang).map((opt) => (
                <button
                    key={String(opt.value)}
                    type="button"
                    aria-label={opt.desc}
                    title={opt.desc}
                    style={pillButtonStyle(lansengerLocalMode === opt.value)}
                    onClick={() => {
                        const prev = lansengerLocalMode;
                        setLansengerLocalModeState(opt.value);
                        SetLansengerLocalMode(opt.value).then(() => {
                            LoadConfig().then((c: any) => setConfig(c)).catch(() => {});
                        }).catch((err: any) => {
                            setLansengerLocalModeState(prev);
                            alert(err?.message || err || switchFailedLabel);
                        });
                    }}
                >
                    {opt.label}
                </button>
            ))}
        </div>
        <div style={{ maxWidth: '620px', display: 'flex', flexDirection: 'column', gap: '10px' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <label style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)', whiteSpace: 'nowrap', minWidth: '92px' }}>App ID</label>
                <input
                    type="text"
                    value={(config as any)?.lansenger_app_id || ''}
                    onChange={(e) => saveRemoteConfigField({ lansenger_app_id: e.target.value } as any)}
                    placeholder="Lansenger App ID"
                    spellCheck={false}
                    style={{ flex: 1, padding: '6px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', fontSize: '0.78rem' }}
                />
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <label style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)', whiteSpace: 'nowrap', minWidth: '92px' }}>App Secret</label>
                <input
                    type="password"
                    value={(config as any)?.lansenger_app_secret || ''}
                    onChange={(e) => saveRemoteConfigField({ lansenger_app_secret: e.target.value } as any)}
                    placeholder="Lansenger App Secret"
                    autoComplete="off"
                    style={{ flex: 1, padding: '6px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', fontSize: '0.78rem' }}
                />
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <label style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)', whiteSpace: 'nowrap', minWidth: '92px' }}>{textForLang(lang, 'Gateway', '\u7f51\u5173', '\u7db2\u95dc')}</label>
                <input
                    type="text"
                    value={(config as any)?.lansenger_gateway_url || ''}
                    onChange={(e) => saveRemoteConfigField({ lansenger_gateway_url: e.target.value } as any)}
                    placeholder="https://apigw.lx.qianxin.com"
                    spellCheck={false}
                    style={{ flex: 1, padding: '6px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', fontSize: '0.78rem' }}
                />
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <label style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)', whiteSpace: 'nowrap', minWidth: '92px' }}>{textForLang(lang, 'WS Gateway', 'WS \u7f51\u5173', 'WS \u7db2\u95dc')}</label>
                <input
                    type="text"
                    value={(config as any)?.lansenger_wss_url || ''}
                    onChange={(e) => saveRemoteConfigField({ lansenger_wss_url: e.target.value } as any)}
                    placeholder={textForLang(lang, 'Optional, usually blank', '\u53ef\u9009\uff0c\u901a\u5e38\u7559\u7a7a', '\u53ef\u9078\uff0c\u901a\u5e38\u7559\u7a7a')}
                    spellCheck={false}
                    style={{ flex: 1, padding: '6px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', fontSize: '0.78rem' }}
                />
            </div>
        </div>
    </div>
);
