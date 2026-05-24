import { useEffect, useRef, type Dispatch, type SetStateAction } from 'react';
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
    const activeQRTokenRef = useRef('');
    const activeQRSessionRef = useRef(0);

    useEffect(() => () => {
        activeQRSessionRef.current += 1;
        activeQRTokenRef.current = '';
    }, []);

    const startQRLogin = async () => {
        const session = activeQRSessionRef.current + 1;
        activeQRSessionRef.current = session;
        activeQRTokenRef.current = '';
        setWeixinQRError('');
        setWeixinQRLoading(true);
        try {
            const res = await StartWeixinQRLogin();
            if (activeQRSessionRef.current !== session) return;
            if (res.error) {
                setWeixinQRError(res.error);
                setWeixinQRLoading(false);
                return;
            }
            const token = res.qrcode_token || '';
            activeQRTokenRef.current = token;
            setWeixinQRCode(res.qrcode_url || '');
            setWeixinQRLoading(false);
            setWeixinQRWaiting(true);
            const pollStart = Date.now();
            const maxMs = 8 * 60 * 1000;
            const poll = async () => {
                if (activeQRSessionRef.current !== session) return;
                if (activeQRTokenRef.current !== token) return;
                if (Date.now() - pollStart > maxMs) {
                    if (activeQRSessionRef.current !== session) return;
                    if (activeQRTokenRef.current !== token) return;
                    activeQRTokenRef.current = '';
                    setWeixinQRWaiting(false);
                    setWeixinQRCode('');
                    setWeixinQRError(textForLang(lang, 'QR expired', '\u4e8c\u7ef4\u7801\u5df2\u8fc7\u671f', '\u4e8c\u7dad\u78bc\u5df2\u904e\u671f'));
                    return;
                }
                try {
                    const p = await PollWeixinQRStatus(token);
                    if (activeQRSessionRef.current !== session) return;
                    if (activeQRTokenRef.current !== token) return;
                    const st = p.status || '';
                    if (st === 'confirmed') {
                        activeQRTokenRef.current = '';
                        setWeixinQRWaiting(false);
                        setWeixinQRCode('');
                        if (p.error) {
                            setWeixinQRError(p.error);
                        } else {
                            setWeixinQRError('');
                            setWeixinStatus('connected');
                            LoadConfig().then((cfg: any) => setConfig(cfg)).catch(() => {});
                        }
                    } else if (st === 'expired') {
                        activeQRTokenRef.current = '';
                        setWeixinQRWaiting(false);
                        setWeixinQRCode('');
                        setWeixinQRError(p.message || textForLang(lang, 'QR expired', '\u4e8c\u7ef4\u7801\u5df2\u8fc7\u671f', '\u4e8c\u7dad\u78bc\u5df2\u904e\u671f'));
                    } else if (p.error) {
                        if (p.retryable === 'true') {
                            setWeixinQRError(p.error);
                            setTimeout(poll, 2000);
                            return;
                        }
                        activeQRTokenRef.current = '';
                        setWeixinQRWaiting(false);
                        setWeixinQRCode('');
                        setWeixinQRError(p.error);
                    } else {
                        setWeixinQRError('');
                        setTimeout(poll, 2000);
                    }
                } catch (err: any) {
                    if (activeQRSessionRef.current !== session) return;
                    if (activeQRTokenRef.current !== token) return;
                    activeQRTokenRef.current = '';
                    setWeixinQRWaiting(false);
                    setWeixinQRCode('');
                    setWeixinQRError(err?.message || String(err));
                }
            };
            poll();
        } catch (e: any) {
            if (activeQRSessionRef.current !== session) return;
            activeQRTokenRef.current = '';
            setWeixinQRError(e?.message || String(e));
            setWeixinQRLoading(false);
            setWeixinQRWaiting(false);
            setWeixinQRCode('');
        }
    };

    const cancelQRLogin = () => {
        activeQRSessionRef.current += 1;
        activeQRTokenRef.current = '';
        setWeixinQRCode('');
        setWeixinQRWaiting(false);
        setWeixinQRLoading(false);
    };

    return (
        <div className="weixin-qr-login-panel">
            {!weixinQRCode && !weixinQRLoading && !weixinQRWaiting && (
                <button type="button" className="weixin-qr-login-panel__primary" aria-label="Scan QR code to login WeChat" onClick={startQRLogin}>
                    {textForLang(lang, 'Scan QR to Login', '\u626b\u7801\u767b\u5f55\u5fae\u4fe1', '\u6383\u78bc\u767b\u9304\u5fae\u4fe1')}
                </button>
            )}
            {weixinQRLoading && (
                <p className="weixin-qr-login-panel__loading">
                    {textForLang(lang, 'Loading QR code...', '\u6b63\u5728\u83b7\u53d6\u4e8c\u7ef4\u7801...', '\u6b63\u5728\u53d6\u5f97\u4e8c\u7dad\u78bc...')}
                </p>
            )}
            {weixinQRCode && (
                <div className="weixin-qr-login-panel__qr-card">
                    <QRCodeSVG value={weixinQRCode} size={220} level="M" bgColor="#ffffff" className="weixin-qr-login-panel__qr" />
                    <p className="weixin-qr-login-panel__hint">
                        {textForLang(lang, 'Scan the QR code with WeChat', '\u8bf7\u7528\u5fae\u4fe1\u626b\u63cf\u4e0a\u65b9\u4e8c\u7ef4\u7801', '\u8acb\u7528\u5fae\u4fe1\u6383\u63cf\u4e0a\u65b9\u4e8c\u7dad\u78bc')}
                    </p>
                    {weixinQRWaiting && (
                        <p className="weixin-qr-login-panel__waiting">
                            {textForLang(lang, 'Waiting for confirmation...', '\u7b49\u5f85\u626b\u7801\u786e\u8ba4\u4e2d...', '\u7b49\u5f85\u6383\u78bc\u78ba\u8a8d\u4e2d...')}
                        </p>
                    )}
                    <button type="button" className="weixin-qr-login-panel__cancel" onClick={cancelQRLogin}>
                        {textForLang(lang, 'Cancel', '\u53d6\u6d88', '\u53d6\u6d88')}
                    </button>
                </div>
            )}
            {weixinQRError && <p className="weixin-qr-login-panel__error">{weixinQRError}</p>}
        </div>
    );
};
