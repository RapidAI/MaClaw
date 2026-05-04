import type { CSSProperties, Dispatch, SetStateAction } from 'react';
import { BrowserOpenURL } from '../../../wailsjs/runtime';
import { LoadConfig, RestartTelegram, SetTelegramLocalMode } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { channelModeLabel, connectionBadgeStyle, connectionStatusLabel, localModeOptions, pillButtonStyle, restartLabel, switchFailedLabel, textForLang, watchLabel } from './imSettingsShared';

type TelegramBotSettingsProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
    imAuditBtnStyle: CSSProperties;
    saveRemoteConfigField: (patch: Record<string, any>) => any;
    telegramStatus: string;
    setTelegramStatus: Dispatch<SetStateAction<string>>;
    telegramLocalMode: boolean;
    setTelegramLocalModeState: Dispatch<SetStateAction<boolean>>;
    setIMAuditPlatform: Dispatch<SetStateAction<string | null>>;
};

export const TelegramBotSettings = ({
    config,
    setConfig,
    lang,
    imAuditBtnStyle,
    saveRemoteConfigField,
    telegramStatus,
    setTelegramStatus,
    telegramLocalMode,
    setTelegramLocalModeState,
    setIMAuditPlatform,
}: TelegramBotSettingsProps) => (
    <div className="form-group" style={{ marginTop: '0', borderTop: 'none', paddingTop: '0' }}>
        <p style={{ fontSize: '0.72rem', color: 'var(--theme-text-muted)', marginBottom: '12px', marginTop: 0 }}>
            {textForLang(lang, 'Configure your own Telegram Bot to chat with MaClaw Agent via Telegram.', '\u914d\u7f6e\u4f60\u81ea\u5df1\u7684 Telegram Bot\uff0c\u901a\u8fc7 Telegram \u4e0e MaClaw Agent \u5bf9\u8bdd\u3002', '\u914d\u7f6e\u4f60\u81ea\u5df1\u7684 Telegram Bot\uff0c\u900f\u904e Telegram \u8207 MaClaw Agent \u5c0d\u8a71\u3002')}
        </p>
        <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '12px', flexWrap: 'wrap' }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: '6px', cursor: 'pointer', fontSize: '0.78rem' }}>
                <input
                    type="checkbox"
                    checked={(config as any)?.telegram_bot_enabled || false}
                    onChange={(e) => saveRemoteConfigField({ telegram_bot_enabled: e.target.checked } as any)}
                />
                {textForLang(lang, 'Enable Telegram Bot', '\u542f\u7528 Telegram Bot', '\u555f\u7528 Telegram Bot')}
            </label>
            <button
                type="button"
                style={{ fontSize: '0.68rem', padding: '1px 8px', borderRadius: '4px', border: '1px solid var(--theme-primary)', background: 'transparent', color: 'var(--theme-primary)', cursor: 'pointer', whiteSpace: 'nowrap' }}
                onClick={() => BrowserOpenURL('https://open-claw.bot/docs/channels/telegram/')}
            >
                {textForLang(lang, 'Tutorial', '\u6559\u7a0b', '\u6559\u7a0b')}
            </button>
            {(config as any)?.telegram_bot_enabled && (
                <>
                    <span style={connectionBadgeStyle(telegramStatus)}>{connectionStatusLabel(telegramStatus, lang)}</span>
                    <button
                        type="button"
                        style={{ fontSize: '0.68rem', padding: '2px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', background: 'transparent', color: 'var(--theme-text-secondary)', cursor: 'pointer' }}
                        onClick={() => RestartTelegram().then(setTelegramStatus)}
                    >
                        {restartLabel(lang)}
                    </button>
                </>
            )}
            <button type="button" onClick={() => setIMAuditPlatform('telegram')} style={{ ...imAuditBtnStyle, marginLeft: (config as any)?.telegram_bot_enabled ? '18px' : '0' }}>
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
                    style={pillButtonStyle(telegramLocalMode === opt.value)}
                    onClick={() => {
                        const prev = telegramLocalMode;
                        setTelegramLocalModeState(opt.value);
                        SetTelegramLocalMode(opt.value).then(() => {
                            LoadConfig().then((c: any) => setConfig(c)).catch(() => {});
                        }).catch((err: any) => {
                            setTelegramLocalModeState(prev);
                            alert(err?.message || err || switchFailedLabel);
                        });
                    }}
                >
                    {opt.label}
                </button>
            ))}
        </div>
        <div style={{ maxWidth: '520px' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <label style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)', whiteSpace: 'nowrap', minWidth: '62px' }}>Bot Token</label>
                <input
                    type="password"
                    value={(config as any)?.telegram_bot_token || ''}
                    onChange={(e) => saveRemoteConfigField({ telegram_bot_token: e.target.value } as any)}
                    placeholder="e.g. 123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"
                    style={{ flex: 1, padding: '6px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', fontSize: '0.78rem' }}
                />
            </div>
        </div>
    </div>
);
