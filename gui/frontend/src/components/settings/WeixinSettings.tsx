import type { CSSProperties, Dispatch, SetStateAction } from 'react';
import { LoadConfig, RestartWeixin, SetWeixinLocalMode, StopWeixin } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { channelModeLabel, connectionBadgeStyle, connectionStatusLabel, localModeOptions, pillButtonStyle, restartLabel, switchFailedLabel, textForLang } from './imSettingsShared';
import { WeixinQRLoginPanel } from './WeixinQRLoginPanel';

type WeixinSettingsProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
    imAuditBtnStyle: CSSProperties;
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
    imAuditBtnStyle,
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
    <div className="form-group" style={{ marginTop: '0', borderTop: 'none', paddingTop: '0' }}>
        <p style={{ fontSize: '0.72rem', color: 'var(--theme-text-muted)', marginBottom: '12px', marginTop: 0 }}>
            {textForLang(lang, 'Scan QR code to log in to WeChat and chat with MaClaw Agent.', '\u626b\u7801\u767b\u5f55\u5fae\u4fe1\uff0c\u901a\u8fc7\u5fae\u4fe1\u4e0e MaClaw Agent \u5bf9\u8bdd\u3002', '\u6383\u78bc\u767b\u9304\u5fae\u4fe1\uff0c\u900f\u904e\u5fae\u4fe1\u8207 MaClaw Agent \u5c0d\u8a71\u3002')}
        </p>
        <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '12px', flexWrap: 'wrap' }}>
            <span style={connectionBadgeStyle(weixinStatus)}>{connectionStatusLabel(weixinStatus, lang)}</span>
            {(config as any)?.weixin_account_id && (
                <span style={{ fontSize: '0.7rem', color: 'var(--theme-text-muted)' }}>ID: {(config as any).weixin_account_id}</span>
            )}
            {weixinStatus === 'connected' && (
                <>
                    <button
                        type="button"
                        aria-label="Restart WeChat connection"
                        style={{ fontSize: '0.68rem', padding: '2px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', background: 'transparent', color: 'var(--theme-text-secondary)', cursor: 'pointer' }}
                        onClick={() => RestartWeixin().then(setWeixinStatus)}
                    >
                        {restartLabel(lang)}
                    </button>
                    <button
                        type="button"
                        aria-label="Disconnect WeChat"
                        style={{ fontSize: '0.68rem', padding: '2px 8px', borderRadius: '4px', border: '1px solid var(--theme-danger)', background: 'transparent', color: 'var(--theme-danger)', cursor: 'pointer' }}
                        onClick={() => { StopWeixin(); setWeixinStatus('disconnected'); }}
                    >
                        {textForLang(lang, 'Disconnect', '\u65ad\u5f00', '\u65b7\u958b')}
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
                    style={pillButtonStyle(weixinLocalMode === opt.value)}
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
        <button type="button" onClick={() => setIMAuditPlatform('weixin')} style={{ ...imAuditBtnStyle, marginLeft: '16px' }}>
            {lang === 'zh-Hans' ? '\u76d1\u770b' : lang === 'zh-Hant' ? '\u76e3\u770b' : 'Watch'}
        </button>
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
    </div>
);
