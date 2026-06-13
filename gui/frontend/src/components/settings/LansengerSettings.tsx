import type { Dispatch, SetStateAction } from 'react';
import { LoadConfig, RestartLansenger, SetLansengerLocalMode } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { channelModeLabel, connectionStatusLabel, localModeOptions, restartLabel, switchFailedLabel, textForLang, watchLabel } from './imSettingsShared';

type LansengerSettingsProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
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
    saveRemoteConfigField,
    lansengerStatus,
    setLansengerStatus,
    lansengerLocalMode,
    setLansengerLocalModeState,
    setIMAuditPlatform,
}: LansengerSettingsProps) => (
    <section className="im-settings-card im-settings-channel">
        <p className="im-settings-description">
            {textForLang(lang, 'Configure Lansenger access for TigerClaw Agent messages.', '\u914d\u7f6e\u84dd\u4fe1\u63a5\u5165\uff0c\u7528\u84dd\u4fe1\u4e0e TigerClaw Agent \u5bf9\u8bdd\u3002', '\u914d\u7f6e\u85cd\u4fe1\u63a5\u5165\uff0c\u7528\u85cd\u4fe1\u8207 TigerClaw Agent \u5c0d\u8a71\u3002')}
        </p>
        <div className="im-settings-toolbar">
            <label className="im-settings-toggle">
                <input
                    type="checkbox"
                    aria-label={textForLang(lang, 'Enable Lansenger', '\u542f\u7528\u84dd\u4fe1', '\u555f\u7528\u85cd\u4fe1')}
                    checked={(config as any)?.lansenger_enabled || false}
                    onChange={(e) => saveRemoteConfigField({ lansenger_enabled: e.target.checked } as any)}
                />
                <span>{textForLang(lang, 'Enable Lansenger', '\u542f\u7528\u84dd\u4fe1', '\u555f\u7528\u85cd\u4fe1')}</span>
            </label>
            {(config as any)?.lansenger_enabled && (
                <>
                    <span className="im-settings-status" data-status={lansengerStatus}>{connectionStatusLabel(lansengerStatus, lang)}</span>
                    <button
                        type="button"
                        className="im-settings-button"
                        onClick={() => RestartLansenger().then(setLansengerStatus).catch((err: any) => {
                            alert(err?.message || err || restartLabel(lang));
                        })}
                    >
                        {restartLabel(lang)}
                    </button>
                </>
            )}
            <button type="button" className="im-settings-button im-settings-button--audit" onClick={() => setIMAuditPlatform('lansenger')}>
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
                        data-active={lansengerLocalMode === opt.value}
                        onClick={() => {
                            const prev = lansengerLocalMode;
                            setLansengerLocalModeState(opt.value);
                            SetLansengerLocalMode(opt.value).then(() => {
                                LoadConfig().then((c: any) => setConfig(c)).catch(() => {});
                            }).catch((err: any) => {
                                setLansengerLocalModeState(prev);
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
                <input type="text" value={(config as any)?.lansenger_app_id || ''} onChange={(e) => saveRemoteConfigField({ lansenger_app_id: e.target.value } as any)} placeholder="Lansenger App ID" spellCheck={false} />
            </label>
            <label className="im-settings-field">
                <span>App Secret</span>
                <input type="password" value={(config as any)?.lansenger_app_secret || ''} onChange={(e) => saveRemoteConfigField({ lansenger_app_secret: e.target.value } as any)} placeholder="Lansenger App Secret" autoComplete="off" />
            </label>
            <label className="im-settings-field">
                <span>{textForLang(lang, 'Gateway', '\u7f51\u5173', '\u7db2\u95dc')}</span>
                <input type="text" value={(config as any)?.lansenger_gateway_url || ''} onChange={(e) => saveRemoteConfigField({ lansenger_gateway_url: e.target.value } as any)} placeholder="https://apigw.lx.qianxin.com" spellCheck={false} />
            </label>
            <label className="im-settings-field">
                <span>{textForLang(lang, 'WS Gateway', 'WS \u7f51\u5173', 'WS \u7db2\u95dc')}</span>
                <input type="text" value={(config as any)?.lansenger_wss_url || ''} onChange={(e) => saveRemoteConfigField({ lansenger_wss_url: e.target.value } as any)} placeholder={textForLang(lang, 'Optional, usually blank', '\u53ef\u9009\uff0c\u901a\u5e38\u7559\u7a7a', '\u53ef\u9078\uff0c\u901a\u5e38\u7559\u7a7a')} spellCheck={false} />
            </label>
        </div>
    </section>
);