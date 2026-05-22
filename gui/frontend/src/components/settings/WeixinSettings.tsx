import type { Dispatch, SetStateAction } from 'react';
import { LoadConfig, RestartWeixin, SetWeixinLocalMode, StopWeixin } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { channelModeLabel, connectionStatusLabel, localModeOptions, restartLabel, switchFailedLabel, textForLang, watchLabel } from './imSettingsShared';
import { WeixinQRLoginPanel } from './WeixinQRLoginPanel';

type WeixinSettingsProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
    weixinStatus: string;
    setWeixinStatus: Dispatch<SetStateAction<string>>;
    weixinLocalMode: boolean;
    setWeixinLocalModeState: Dispatch<SetStateAction<boolean>>;
    setIMAuditPlatform: Dispatch<SetStateAction<string | null>>;
    weixinQRCode: string;
    setWeixinQRCode: Dispatch<SetStateAction<string>>;
    weixinQRLoading: boolean;
    setWeixinQRLoading: Dispatch<SetStateAction<boolean>>;
    weixinQRWaiting: boolean;
    setWeixinQRWaiting: Dispatch<SetStateAction<boolean>>;
    weixinQRError: string;
    setWeixinQRError: Dispatch<SetStateAction<string>>;
};

export const WeixinSettings = ({
    config,
    setConfig,
    lang,
    weixinStatus,
    setWeixinStatus,
    weixinLocalMode,
    setWeixinLocalModeState,
    setIMAuditPlatform,
    weixinQRCode,
    setWeixinQRCode,
    weixinQRLoading,
    setWeixinQRLoading,
    weixinQRWaiting,
    setWeixinQRWaiting,
    weixinQRError,
    setWeixinQRError,
}: WeixinSettingsProps) => (
    <section className="im-settings-card im-settings-channel">
        <p className="im-settings-description">
            {textForLang(lang, 'Scan QR code to log in to WeChat and chat with MaClaw Agent.', '\u626b\u7801\u767b\u5f55\u5fae\u4fe1\uff0c\u901a\u8fc7\u5fae\u4fe1\u4e0e MaClaw Agent \u5bf9\u8bdd\u3002', '\u6383\u78bc\u767b\u9304\u5fae\u4fe1\uff0c\u900f\u904e\u5fae\u4fe1\u8207 MaClaw Agent \u5c0d\u8a71\u3002')}
        </p>
        <div className="im-settings-toolbar">
            <span className="im-settings-status" data-status={weixinStatus}>{connectionStatusLabel(weixinStatus, lang)}</span>
            {(config as any)?.weixin_account_id && (
                <span className="im-settings-account-id">ID: {(config as any).weixin_account_id}</span>
            )}
            {weixinStatus === 'connected' && (
                <>
                    <button type="button" aria-label="Restart WeChat connection" className="im-settings-button" onClick={() => RestartWeixin().then(setWeixinStatus)}>
                        {restartLabel(lang)}
                    </button>
                    <button type="button" aria-label="Disconnect WeChat" className="im-settings-button im-settings-button--danger" onClick={() => { StopWeixin(); setWeixinStatus('disconnected'); }}>
                        {textForLang(lang, 'Disconnect', '\u65ad\u5f00', '\u65b7\u958b')}
                    </button>
                </>
            )}
            <button type="button" className="im-settings-button im-settings-button--audit" onClick={() => setIMAuditPlatform('weixin')}>
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
                        data-active={weixinLocalMode === opt.value}
                        onClick={() => {
                            const prev = weixinLocalMode;
                            setWeixinLocalModeState(opt.value);
                            SetWeixinLocalMode(opt.value).then(() => {
                                LoadConfig().then((c: any) => setConfig(c)).catch(() => {});
                            }).catch((err: any) => {
                                setWeixinLocalModeState(prev);
                                alert(err?.message || err || switchFailedLabel);
                            });
                        }}
                    >
                        {opt.label}
                    </button>
                ))}
            </div>
        </div>
        {weixinStatus !== 'connected' && (
            <WeixinQRLoginPanel
                lang={lang}
                setConfig={setConfig}
                setWeixinStatus={setWeixinStatus}
                weixinQRCode={weixinQRCode}
                setWeixinQRCode={setWeixinQRCode}
                weixinQRLoading={weixinQRLoading}
                setWeixinQRLoading={setWeixinQRLoading}
                weixinQRWaiting={weixinQRWaiting}
                setWeixinQRWaiting={setWeixinQRWaiting}
                weixinQRError={weixinQRError}
                setWeixinQRError={setWeixinQRError}
            />
        )}
    </section>
);