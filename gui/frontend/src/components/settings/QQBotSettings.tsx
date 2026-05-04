import type { CSSProperties, Dispatch, SetStateAction } from 'react';
import { BrowserOpenURL } from '../../../wailsjs/runtime';
import { LoadConfig, RestartQQBot, SetQQBotLocalMode } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';

type QQBotSettingsProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
    imAuditBtnStyle: CSSProperties;
    saveRemoteConfigField: (patch: Record<string, any>) => any;
    qqBotStatus: string;
    setQQBotStatus: Dispatch<SetStateAction<string>>;
    qqBotLocalMode: boolean;
    setQQBotLocalModeState: Dispatch<SetStateAction<boolean>>;
    setIMAuditPlatform: Dispatch<SetStateAction<string | null>>;
};

import { channelModeLabel, connectionBadgeStyle, connectionStatusLabel, localModeOptions, pillButtonStyle, restartLabel, switchFailedLabel, textForLang } from './imSettingsShared';

export const QQBotSettings = ({
    config,
    setConfig,
    lang,
    imAuditBtnStyle,
    saveRemoteConfigField,
    qqBotStatus,
    setQQBotStatus,
    qqBotLocalMode,
    setQQBotLocalModeState,
    setIMAuditPlatform,
}: QQBotSettingsProps) => (
    <div className="form-group" style={{ marginTop: '0', borderTop: 'none', paddingTop: '0' }}>
        <p style={{ fontSize: '0.72rem', color: 'var(--theme-text-muted)', marginBottom: '12px', marginTop: 0 }}>
            {textForLang(lang, 'Configure your own QQ Bot to chat with MaClaw Agent via QQ.', '\u914d\u7f6e\u4f60\u81ea\u5df1\u7684 QQ \u673a\u5668\u4eba\uff0c\u901a\u8fc7 QQ \u4e0e MaClaw Agent \u5bf9\u8bdd\u3002', '\u914d\u7f6e\u4f60\u81ea\u5df1\u7684 QQ \u6a5f\u5668\u4eba\uff0c\u900f\u904e QQ \u8207 MaClaw Agent \u5c0d\u8a71\u3002')}
        </p>
        <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '12px', flexWrap: 'wrap' }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: '6px', cursor: 'pointer', fontSize: '0.78rem' }}>
                <input
                    type="checkbox"
                    checked={config?.qqbot_enabled || false}
                    onChange={(e) => saveRemoteConfigField({ qqbot_enabled: e.target.checked } as any)}
                />
                {textForLang(lang, 'Enable QQ Bot', '\u542f\u7528 QQ \u673a\u5668\u4eba', '\u555f\u7528 QQ \u6a5f\u5668\u4eba')}
            </label>
            <button
                type="button"
                style={{ fontSize: '0.68rem', padding: '1px 8px', borderRadius: '4px', border: '1px solid var(--theme-primary)', background: 'transparent', color: 'var(--theme-primary)', cursor: 'pointer', whiteSpace: 'nowrap' }}
                onClick={() => BrowserOpenURL('https://q.qq.com/qqbot/openclaw/login.html')}
            >
                {textForLang(lang, 'Get AppID', '\u83b7\u53d6 AppID', '\u53d6\u5f97 AppID')}
            </button>
            {config?.qqbot_enabled && (
                <>
                    <span style={connectionBadgeStyle(qqBotStatus)}>{connectionStatusLabel(qqBotStatus, lang)}</span>
                    <button
                        type="button"
                        style={{ fontSize: '0.68rem', padding: '2px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', background: 'transparent', color: 'var(--theme-text-secondary)', cursor: 'pointer' }}
                        onClick={() => RestartQQBot().then(setQQBotStatus)}
                    >
                        {restartLabel(lang)}
                    </button>
                </>
            )}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '16px' }}>
            <span style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)' }}>{channelModeLabel(lang)}</span>
            {localModeOptions(lang).map((opt) => (
                <button
                    key={String(opt.value)}
                    type="button"
                    aria-label={opt.desc}
                    title={opt.desc}
                    style={pillButtonStyle(qqBotLocalMode === opt.value)}
                    onClick={() => {
                        const prev = qqBotLocalMode;
                        setQQBotLocalModeState(opt.value);
                        SetQQBotLocalMode(opt.value).then(() => {
                            LoadConfig().then((c: any) => setConfig(c)).catch(() => {});
                        }).catch((err: any) => {
                            setQQBotLocalModeState(prev);
                            alert(err?.message || err || switchFailedLabel);
                        });
                    }}
                >
                    {opt.label}
                </button>
            ))}
        </div>
        <button type="button" onClick={() => setIMAuditPlatform('qq')} style={{ ...imAuditBtnStyle, marginLeft: '16px' }}>
            {lang === 'zh-Hans' ? '\u76d1\u770b' : lang === 'zh-Hant' ? '\u76e3\u770b' : 'Watch'}
        </button>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '10px', maxWidth: '520px' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <label style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)', whiteSpace: 'nowrap', minWidth: '62px' }}>App ID</label>
                <input
                    type="text"
                    value={config?.qqbot_app_id || ''}
                    onChange={(e) => saveRemoteConfigField({ qqbot_app_id: e.target.value } as any)}
                    placeholder="e.g. 102012345"
                    style={{ flex: 1, padding: '6px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', fontSize: '0.78rem' }}
                />
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <label style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)', whiteSpace: 'nowrap', minWidth: '62px' }}>App Secret</label>
                <input
                    type="password"
                    value={config?.qqbot_app_secret || ''}
                    onChange={(e) => saveRemoteConfigField({ qqbot_app_secret: e.target.value } as any)}
                    placeholder="••••••••"
                    style={{ flex: 1, padding: '6px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', fontSize: '0.78rem' }}
                />
            </div>
        </div>
    </div>
);
