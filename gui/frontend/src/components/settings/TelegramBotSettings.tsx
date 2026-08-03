import { Dispatch, SetStateAction } from 'react';
import { BrowserOpenURL } from '../../../wailsjs/runtime';
import { LoadConfig, RestartTelegram, SetTelegramLocalMode } from '../../../wailsjs/go/main/App';
import { corelib, main } from '../../../wailsjs/go/models';
import { ConnectionStatusBadge } from './ConnectionStatusBadge';
import { channelModeLabel, localModeOptions, restartLabel, switchFailedLabel, textForLang, watchLabel } from './imSettingsShared';
import { useDialog } from '../CustomDialog';

type TelegramBotSettingsProps = {
    config: corelib.AppConfig | null;
    setConfig: Dispatch<SetStateAction<corelib.AppConfig | null>>;
    lang: string;
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
    saveRemoteConfigField,
    telegramStatus,
    setTelegramStatus,
    telegramLocalMode,
    setTelegramLocalModeState,
    setIMAuditPlatform,
}: TelegramBotSettingsProps) => {
    const { showAlert } = useDialog();
    return (
    <section className="im-settings-card im-settings-channel">
        <p className="im-settings-description">
            {textForLang(lang, 'Configure your own Telegram Bot to chat with MaClaw Agent via Telegram.', '\u914d\u7f6e\u4f60\u81ea\u5df1\u7684 Telegram Bot\uff0c\u901a\u8fc7 Telegram \u4e0e MaClaw Agent \u5bf9\u8bdd\u3002', '\u914d\u7f6e\u4f60\u81ea\u5df1\u7684 Telegram Bot\uff0c\u900f\u904e Telegram \u8207 MaClaw Agent \u5c0d\u8a71\u3002')}
        </p>
        <div className="im-settings-toolbar">
            <label className="im-settings-toggle">
                <input
                    type="checkbox"
                    aria-label={textForLang(lang, 'Enable Telegram Bot', '\u542f\u7528 Telegram Bot', '\u555f\u7528 Telegram Bot')}
                    checked={(config as any)?.telegram_bot_enabled || false}
                    onChange={(e) => saveRemoteConfigField({ telegram_bot_enabled: e.target.checked } as any)}
                />
                <span>{textForLang(lang, 'Enable Telegram Bot', '\u542f\u7528 Telegram Bot', '\u555f\u7528 Telegram Bot')}</span>
            </label>
            <button type="button" className="im-settings-button im-settings-button--primary" onClick={() => BrowserOpenURL('https://open-claw.bot/docs/channels/telegram/')}>
                {textForLang(lang, 'Tutorial', '\u6559\u7a0b', '\u6559\u7a0b')}
            </button>
            {(config as any)?.telegram_bot_enabled && (
                <>
                    <ConnectionStatusBadge status={telegramStatus} lang={lang} />
                    <button type="button" className="im-settings-button" onClick={() => RestartTelegram().then(setTelegramStatus)}>
                        {restartLabel(lang)}
                    </button>
                </>
            )}
            <button type="button" className="im-settings-button im-settings-button--audit" onClick={() => setIMAuditPlatform('telegram')}>
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
                        data-active={telegramLocalMode === opt.value}
                        onClick={() => {
                            const prev = telegramLocalMode;
                            setTelegramLocalModeState(opt.value);
                            SetTelegramLocalMode(opt.value).then(() => {
                                LoadConfig().then((c: any) => setConfig(c)).catch(() => {});
                            }).catch((err: any) => {
                                setTelegramLocalModeState(prev);
                                void showAlert(String(err?.message || err || switchFailedLabel(lang)));
                            });
                        }}
                    >
                        {opt.label}
                    </button>
                ))}
            </div>
        </div>
        <div className="im-settings-grid">
            <label className="im-settings-field">
                <span>Bot Token</span>
                <input
                    type="password"
                    value={(config as any)?.telegram_bot_token || ''}
                    onChange={(e) => saveRemoteConfigField({ telegram_bot_token: e.target.value } as any)}
                    placeholder="e.g. 123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"
                    autoComplete="off"
                />
            </label>
        </div>
        <label className="im-settings-field" style={{ marginTop: 12 }}>
            <span>
                {textForLang(
                    lang,
                    'Owner Chat ID (proactive / 盯人)',
                    '机主 Chat ID（主动推送 / 盯人）',
                    '機主 Chat ID（主動推送 / 盯人）',
                )}
            </span>
            <input
                type="text"
                inputMode="numeric"
                value={
                    (config as any)?.telegram_owner_chat_id
                        ? String((config as any).telegram_owner_chat_id)
                        : ''
                }
                onChange={(e) => {
                    const raw = e.target.value.trim();
                    if (raw === '') {
                        // Empty clears owner chat id (stored as empty string server-side).
                        saveRemoteConfigField({ telegram_owner_chat_id: '' } as any);
                        return;
                    }
                    // Full integer only (supports negative group chat ids). Keep as string
                    // so values outside JS Number.MAX_SAFE_INTEGER still round-trip.
                    if (!/^-?\d+$/.test(raw)) {
                        return;
                    }
                    saveRemoteConfigField({ telegram_owner_chat_id: raw } as any);
                }}
                placeholder={textForLang(
                    lang,
                    'Numeric chat_id (string) — no prior chat needed when set',
                    '数字 chat_id（字符串保存），填写后无需先私聊即可推送',
                    '數字 chat_id（字串保存），填寫後無需先私聊即可推送',
                )}
                autoComplete="off"
            />
            <p className="im-settings-description" style={{ marginTop: 6, marginBottom: 0 }}>
                {textForLang(
                    lang,
                    'Used for 盯人 forward and scheduled self-push. Get chat_id from the first private-chat log, then paste here so restarts do not require chatting again.',
                    '用于盯人转发与定时任务「推给自己」。可从首次私聊日志复制 chat_id；填好后无需先发消息也能推送。',
                    '用於盯人轉發與定時任務「推給自己」。可從首次私聊日誌複製 chat_id；填好後無需先發訊息也能推送。',
                )}
            </p>
        </label>
    </section>
    );
};
