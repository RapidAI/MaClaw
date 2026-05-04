import type { Dispatch, SetStateAction } from 'react';
import { QRCodeSVG } from 'qrcode.react';
import { LoadConfig, PollWeixinQRStatus, StartWeixinQRLogin } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { textForLang } from './imSettingsShared';

type WeixinQRLoginPanelProps = {
    lang: string;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    setWeixinStatus: Dispatch<SetStateAction<string>>;
    weixinQRCode: string;
    setWeixinQRCode: Dispatch<SetStateAction<string>>;
    weixinQRLoading: boolean;
    setWeixinQRLoading: Dispatch<SetStateAction<boolean>>;
    weixinQRWaiting: boolean;
    setWeixinQRWaiting: Dispatch<SetStateAction<boolean>>;
    weixinQRError: string;
    setWeixinQRError: Dispatch<SetStateAction<string>>;
};

export const WeixinQRLoginPanel = ({
    lang,
    setConfig,
    setWeixinStatus,
    weixinQRCode,
    setWeixinQRCode,
    weixinQRLoading,
    setWeixinQRLoading,
    weixinQRWaiting,
    setWeixinQRWaiting,
    weixinQRError,
    setWeixinQRError,
}: WeixinQRLoginPanelProps) => {
    const startQRLogin = async () => {
        setWeixinQRError('');
        setWeixinQRLoading(true);
        try {
            const res = await StartWeixinQRLogin();
            if (res.error) {
                setWeixinQRError(res.error);
                setWeixinQRLoading(false);
                return;
            }
            const token = res.qrcode_token || '';
            setWeixinQRCode(res.qrcode_url || '');
            setWeixinQRLoading(false);
            setWeixinQRWaiting(true);
            const pollStart = Date.now();
            const maxMs = 8 * 60 * 1000;
            const poll = async () => {
                if (Date.now() - pollStart > maxMs) {
                    setWeixinQRWaiting(false);
                    setWeixinQRCode('');
                    setWeixinQRError(lang === 'zh-Hans' ? '\u4e8c\u7ef4\u7801\u5df2\u8fc7\u671f' : 'QR expired');
                    return;
                }
                try {
                    const p = await PollWeixinQRStatus(token);
                    const st = p.status || '';
                    if (st === 'confirmed') {
                        setWeixinQRWaiting(false);
                        setWeixinQRCode('');
                        if (p.error) {
                            setWeixinQRError(p.error);
                        } else {
                            setWeixinStatus('connected');
                            LoadConfig().then((cfg: any) => setConfig(cfg)).catch(() => {});
                        }
                    } else if (st === 'expired') {
                        setWeixinQRWaiting(false);
                        setWeixinQRCode('');
                        setWeixinQRError(p.message || 'QR expired');
                    } else if (p.error) {
                        setWeixinQRWaiting(false);
                        setWeixinQRCode('');
                        setWeixinQRError(p.error);
                    } else {
                        setTimeout(poll, 2000);
                    }
                } catch (err: any) {
                    setWeixinQRWaiting(false);
                    setWeixinQRCode('');
                    setWeixinQRError(err?.message || String(err));
                }
            };
            poll();
        } catch (e: any) {
            setWeixinQRError(e?.message || String(e));
            setWeixinQRLoading(false);
            setWeixinQRWaiting(false);
            setWeixinQRCode('');
        }
    };

    const cancelQRLogin = () => {
        setWeixinQRCode('');
        setWeixinQRWaiting(false);
        setWeixinQRLoading(false);
    };

    return (
        <div style={{ marginTop: '4px' }}>
            {!weixinQRCode && !weixinQRLoading && !weixinQRWaiting && (
                <button
                    type="button"
                    aria-label="Scan QR code to login WeChat"
                    style={{ padding: '6px 18px', borderRadius: '6px', border: '1.5px solid var(--theme-primary)', background: 'var(--theme-info-bg)', color: 'var(--theme-primary)', fontWeight: 600, fontSize: '0.78rem', cursor: 'pointer' }}
                    onClick={startQRLogin}
                >
                    {'\uD83D\uDD11 ' + textForLang(lang, 'Scan QR to Login', '\u626b\u7801\u767b\u5f55\u5fae\u4fe1', '\u6383\u78bc\u767b\u9304\u5fae\u4fe1')}
                </button>
            )}
            {weixinQRLoading && (
                <p style={{ fontSize: '0.75rem', color: 'var(--theme-primary)' }}>
                    {textForLang(lang, 'Loading QR code...', '\u6b63\u5728\u83b7\u53d6\u4e8c\u7ef4\u7801...', '\u6b63\u5728\u53d6\u5f97\u4e8c\u7dad\u78bc...')}
                </p>
            )}
            {weixinQRCode && (
                <div style={{ textAlign: 'center', maxWidth: '280px' }}>
                    <QRCodeSVG
                        value={weixinQRCode}
                        size={220}
                        level="M"
                        bgColor="#ffffff"
                        style={{ borderRadius: '8px', border: '1px solid var(--theme-border)', padding: '8px', background: '#fff' }}
                    />
                    <p style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)', marginTop: '8px' }}>
                        {textForLang(lang, 'Scan the QR code with WeChat', '\u8bf7\u7528\u5fae\u4fe1\u626b\u63cf\u4e0a\u65b9\u4e8c\u7ef4\u7801', '\u8acb\u7528\u5fae\u4fe1\u6383\u63cf\u4e0a\u65b9\u4e8c\u7dad\u78bc')}
                    </p>
                    {weixinQRWaiting && (
                        <p style={{ fontSize: '0.68rem', color: 'var(--theme-text-muted)' }}>
                            {textForLang(lang, 'Waiting for confirmation...', '\u7b49\u5f85\u626b\u7801\u786e\u8ba4\u4e2d...', '\u7b49\u5f85\u6383\u78bc\u78ba\u8a8d\u4e2d...')}
                        </p>
                    )}
                    <button
                        type="button"
                        style={{ marginTop: '10px', fontSize: '0.72rem', padding: '3px 14px', borderRadius: '4px', border: '1px solid var(--theme-border)', background: 'transparent', color: 'var(--theme-text-muted)', cursor: 'pointer' }}
                        onClick={cancelQRLogin}
                    >
                        {textForLang(lang, 'Cancel', '\u53d6\u6d88', '\u53d6\u6d88')}
                    </button>
                </div>
            )}
            {weixinQRError && (
                <p style={{ fontSize: '0.72rem', color: 'var(--theme-danger)', marginTop: '8px' }}>{weixinQRError}</p>
            )}
        </div>
    );
};
