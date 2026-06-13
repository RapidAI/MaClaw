import type { Dispatch, SetStateAction } from 'react';
import { BrowserOpenURL } from '../../../wailsjs/runtime';
import { LoadConfig, RestartQQBot, SetQQBotLocalMode } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { channelModeLabel, connectionStatusLabel, localModeOptions, restartLabel, switchFailedLabel, textForLang, watchLabel } from './imSettingsShared';

type QQBotSettingsProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
    saveRemoteConfigField: (patch: Record<string, any>) => any;
    qqBotStatus: string;
    setQQBotStatus: Dispatch<SetStateAction<string>>;
    qqBotLocalMode: boolean;
    setQQBotLocalModeState: Dispatch<SetStateAction<boolean>>;
    setIMAuditPlatform: Dispatch<SetStateAction<string | null>>;
};

export const QQBotSettings = ({
    config,
    setConfig,
    lang,
    saveRemoteConfigField,
    qqBotStatus,
    setQQBotStatus,
    qqBotLocalMode,
    setQQBotLocalModeState,
    setIMAuditPlatform,
}: QQBotSettingsProps) => (
    <section className="im-settings-card im-settings-channel">
        <p className="im-settings-description">
            {textForLang(lang, 'Configure your own QQ Bot to chat with MaClaw Agent via QQ.', '\u914d\u7f6e\u4f60\u81ea\u5df1\u7684 QQ \u673a\u5668\u4eba\uff0c\u901a\u8fc7 QQ \u4e0e MaClaw Agent \u5bf9\u8bdd\u3002', '\u914d\u7f6e\u4f60\u81ea\u5df1\u7684 QQ \u6a5f\u5668\u4eba\uff0c\u900f\u904e QQ \u8207 MaClaw Agent \u5c0d\u8a71\u3002')}
        </p>
        <div className="im-settings-toolbar">
            <label className="im-settings-toggle">
                <input
                    type="checkbox"
                    aria-label={textForLang(lang, 'Enable QQ Bot', '\u542f\u7528 QQ \u673a\u5668\u4eba', '\u555f\u7528 QQ \u6a5f\u5668\u4eba')}
                    checked={config?.qqbot_enabled || false}
                    onChange={(e) => saveRemoteConfigField({ qqbot_enabled: e.target.checked } as any)}
                />
                <span>{textForLang(lang, 'Enable QQ Bot', '\u542f\u7528 QQ \u673a\u5668\u4eba', '\u555f\u7528 QQ \u6a5f\u5668\u4eba')}</span>
            </label>
            <button type="button" className="im-settings-button im-settings-button--primary" onClick={() => BrowserOpenURL('https://q.qq.com/qqbot/openclaw/login.html')}>
                {textForLang(lang, 'Get AppID', '\u83b7\u53d6 AppID', '\u53d6\u5f97 AppID')}
            </button>
            {config?.qqbot_enabled && (
                <>
                    <span className="im-settings-status" data-status={qqBotStatus}>{connectionStatusLabel(qqBotStatus, lang)}</span>
                    <button type="button" className="im-settings-button" onClick={() => RestartQQBot().then(setQQBotStatus)}>
                        {restartLabel(lang)}
                    </button>
                </>
            )}
            <button type="button" className="im-settings-button im-settings-button--audit" onClick={() => setIMAuditPlatform('qq')}>
                {watchLabel(lang)}
            </button>
        </div>
        <div className="im-settings-mode-row">
            <span>{channelModeLabel(lang)}</span>
            <div className="im-settings-segmented">
                {localModeOptions(lang).map((opt) => (
                    <button
                        key={String(opt.value)}
                        type="button"
                        aria-label={opt.desc}
                        title={opt.desc}
                        data-active={qqBotLocalMode === opt.value}
                        onClick={() => {
                            const prev = qqBotLocalMode;
                            setQQBotLocalModeState(opt.value);
                            SetQQBotLocalMode(opt.value).then(() => {
                                LoadConfig().then((c: any) => setConfig(c)).catch(() => {});
                            }).catch((err: any) => {
                                setQQBotLocalModeState(prev);
                                alert(err?.message || err || switchFailedLabel(lang));
                            });
                        }}
                    >
                        {opt.label}
                    </button>
                ))}
            </div>
        </div>
        <div className="im-settings-grid im-settings-grid--two">
            <label className="im-settings-field">
                <span>App ID</span>
                <input
                    type="text"
                    value={config?.qqbot_app_id || ''}
                    onChange={(e) => saveRemoteConfigField({ qqbot_app_id: e.target.value } as any)}
                    placeholder="e.g. 102012345"
                />
            </label>
            <label className="im-settings-field">
                <span>App Secret</span>
                <input
                    type="password"
                    value={config?.qqbot_app_secret || ''}
                    onChange={(e) => saveRemoteConfigField({ qqbot_app_secret: e.target.value } as any)}
                    placeholder="************"
                    autoComplete="off"
                />
            </label>
        </div>
    </section>
);