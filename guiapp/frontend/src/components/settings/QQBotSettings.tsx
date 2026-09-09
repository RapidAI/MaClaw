import { LoadConfig, RestartQQBot, SetQQBotLocalMode } from '../../../wailsjs/go/main/App';
import { Dispatch, SetStateAction, useState } from 'react';
import { corelib } from '../../../wailsjs/go/models';
import { ConnectionStatusBadge } from './ConnectionStatusBadge';
import { channelModeLabel, localModeOptions, restartLabel, switchFailedLabel, textForLang, watchLabel } from './imSettingsShared';
import { QQBotQRLoginPanel } from './QQBotQRLoginPanel';
import { useDialog } from '../CustomDialog';

type QQBotSettingsProps = {
    config: corelib.AppConfig | null;
    setConfig: Dispatch<SetStateAction<corelib.AppConfig | null>>;
    lang: string;
    saveRemoteConfigField: (patch: Record<string, any>) => any;
    qqBotStatus: string;
    setQQBotStatus: Dispatch<SetStateAction<string>>;
    qqBotLocalMode: boolean;
    setQQBotLocalModeState: Dispatch<SetStateAction<boolean>>;
    setIMAuditPlatform: Dispatch<SetStateAction<string | null>>;
    qqBotQRCode: string;
    setQQBotQRCode: Dispatch<SetStateAction<string>>;
    qqBotQRLoading: boolean;
    setQQBotQRLoading: Dispatch<SetStateAction<boolean>>;
    qqBotQRWaiting: boolean;
    setQQBotQRWaiting: Dispatch<SetStateAction<boolean>>;
    qqBotQRError: string;
    setQQBotQRError: Dispatch<SetStateAction<string>>;
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
    qqBotQRCode,
    setQQBotQRCode,
    qqBotQRLoading,
    setQQBotQRLoading,
    qqBotQRWaiting,
    setQQBotQRWaiting,
    qqBotQRError,
    setQQBotQRError,
}: QQBotSettingsProps) => {
    const { showAlert } = useDialog();
    const [showManual, setShowManual] = useState(false);
    const boundAppID = (config?.qqbot_app_id || '').trim();
    const maskedAppID = boundAppID.length <= 6 ? boundAppID : `${boundAppID.slice(0, 3)}***${boundAppID.slice(-3)}`;
    const manualToggleLabel = textForLang(lang, showManual ? 'Hide manual credentials' : 'Enter AppID manually', showManual ? '\u6536\u8d77\u624b\u52a8\u586b\u5199' : '\u624b\u52a8\u586b\u5199 AppID', showManual ? '\u6536\u8d77\u624b\u52d5\u586b\u5beb' : '\u624b\u52d5\u586b\u5beb AppID');
    return (
    <section className="im-settings-card im-settings-channel">
        <p className="im-settings-description">
            {textForLang(lang, 'Scan the QR code with QQ to bind your bot. AppID and AppSecret are saved automatically.', '\u7528\u624b\u673a QQ \u626b\u7801\u7ed1\u5b9a\u673a\u5668\u4eba\uff0cAppID / AppSecret \u4f1a\u81ea\u52a8\u4fdd\u5b58\u3002', '\u7528\u624b\u6a5f QQ \u6383\u78bc\u7d81\u5b9a\u6a5f\u5668\u4eba\uff0cAppID / AppSecret \u6703\u81ea\u52d5\u4fdd\u5b58\u3002')}
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
            {boundAppID && (
                <span className="im-settings-account-id">AppID: {maskedAppID}</span>
            )}
            {config?.qqbot_enabled && (
                <>
                    <ConnectionStatusBadge status={qqBotStatus} lang={lang} />
                    <button type="button" className="im-settings-button" onClick={() => RestartQQBot().then(setQQBotStatus)}>
                        {restartLabel(lang)}
                    </button>
                </>
            )}
            <button type="button" className="im-settings-button im-settings-button--audit" onClick={() => setIMAuditPlatform('qq')}>
                {watchLabel(lang)}
            </button>
        </div>
        <QQBotQRLoginPanel
            lang={lang}
            boundAppID={boundAppID}
            setConfig={setConfig}
            setQQBotStatus={setQQBotStatus}
            qqBotQRCode={qqBotQRCode}
            setQQBotQRCode={setQQBotQRCode}
            qqBotQRLoading={qqBotQRLoading}
            setQQBotQRLoading={setQQBotQRLoading}
            qqBotQRWaiting={qqBotQRWaiting}
            setQQBotQRWaiting={setQQBotQRWaiting}
            qqBotQRError={qqBotQRError}
            setQQBotQRError={setQQBotQRError}
            trailingAction={
                <button
                    type="button"
                    className="weixin-qr-login-panel__primary"
                    aria-label={manualToggleLabel}
                    onClick={() => setShowManual((v) => !v)}
                >
                    {manualToggleLabel}
                </button>
            }
        />
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
                                void showAlert(String(err?.message || err || switchFailedLabel(lang)));
                            });
                        }}
                    >
                        {opt.label}
                    </button>
                ))}
            </div>
        </div>
        {showManual && (
        <div className="im-settings-grid im-settings-grid--two" style={{ marginTop: 12 }}>
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
        )}
        <label className="im-settings-field" style={{ marginTop: 12 }}>
            <span>
                {textForLang(
                    lang,
                    'Owner OpenID (proactive / 盯人)',
                    '机主 OpenID（主动推送 / 盯人）',
                    '機主 OpenID（主動推送 / 盯人）',
                )}
            </span>
            <input
                type="text"
                value={(config as any)?.qqbot_owner_openid || ''}
                onChange={(e) => saveRemoteConfigField({ qqbot_owner_openid: e.target.value } as any)}
                placeholder={textForLang(
                    lang,
                    'C2C user_openid — filled automatically after scan when available',
                    'C2C user_openid，扫码成功后会自动填入；也可手动粘贴',
                    'C2C user_openid，掃碼成功後會自動填入；也可手動貼上',
                )}
                autoComplete="off"
            />
            <p className="im-settings-description" style={{ marginTop: 6, marginBottom: 0 }}>
                {textForLang(
                    lang,
                    'Used for 盯人 forward and scheduled self-push. Scan bind fills this when QQ returns user_openid.',
                    '用于盯人转发与定时任务「推给自己」。扫码绑定成功时若返回 user_openid 会自动填写。',
                    '用於盯人轉發與定時任務「推給自己」。掃碼綁定成功時若返回 user_openid 會自動填寫。',
                )}
            </p>
        </label>
    </section>
    );
};
