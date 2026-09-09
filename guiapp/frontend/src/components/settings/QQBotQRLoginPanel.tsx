import { CancelQQBotQRLogin, LoadConfig, PollQQBotQRStatus, StartQQBotQRLogin } from '../../../wailsjs/go/main/App';
import { type Dispatch, type ReactNode, type SetStateAction, useEffect, useRef, useState } from 'react';
import { QRCodeSVG } from 'qrcode.react';
import { corelib } from '../../../wailsjs/go/models';
import { textForLang } from './imSettingsShared';

const maxQQBotQRRefreshes = 3;

type QQBotQRLoginPanelProps = {
    lang: string;
    boundAppID?: string;
    setConfig: Dispatch<SetStateAction<corelib.AppConfig | null>>;
    setQQBotStatus: Dispatch<SetStateAction<string>>;
    qqBotQRCode: string;
    setQQBotQRCode: Dispatch<SetStateAction<string>>;
    qqBotQRLoading: boolean;
    setQQBotQRLoading: Dispatch<SetStateAction<boolean>>;
    qqBotQRWaiting: boolean;
    setQQBotQRWaiting: Dispatch<SetStateAction<boolean>>;
    qqBotQRError: string;
    setQQBotQRError: Dispatch<SetStateAction<string>>;
    trailingAction?: ReactNode;
};

export const QQBotQRLoginPanel = ({
    lang,
    boundAppID,
    setConfig,
    setQQBotStatus,
    qqBotQRCode,
    setQQBotQRCode,
    qqBotQRLoading,
    setQQBotQRLoading,
    qqBotQRWaiting,
    setQQBotQRWaiting,
    qqBotQRError,
    setQQBotQRError,
    trailingAction,
}: QQBotQRLoginPanelProps) => {
    const activeQRTokenRef = useRef('');
    const activeQRSessionRef = useRef(0);
    const refreshCountRef = useRef(0);
    const [scannedPending, setScannedPending] = useState(false);

    useEffect(() => () => {
        activeQRSessionRef.current += 1;
        const token = activeQRTokenRef.current;
        activeQRTokenRef.current = '';
        if (token) {
            CancelQQBotQRLogin(token).catch(() => {});
        }
    }, []);

    const dropToken = (token: string) => {
        if (token) {
            CancelQQBotQRLogin(token).catch(() => {});
        }
    };

    const startQRLogin = async (resetRefreshes = true) => {
        const session = activeQRSessionRef.current + 1;
        activeQRSessionRef.current = session;
        if (resetRefreshes) {
            refreshCountRef.current = 0;
        }
        const prevToken = activeQRTokenRef.current;
        activeQRTokenRef.current = '';
        setScannedPending(false);
        setQQBotQRError('');
        setQQBotQRCode('');
        setQQBotQRWaiting(false);
        setQQBotQRLoading(true);
        try {
            const res = await StartQQBotQRLogin();
            if (activeQRSessionRef.current !== session) {
                dropToken(res?.qrcode_token || '');
                dropToken(prevToken);
                return;
            }
            const token = (res.qrcode_token || '').trim();
            const qrURL = (res.qrcode_url || '').trim();
            if (res.error || !token || !qrURL) {
                dropToken(token);
                dropToken(prevToken);
                setQQBotQRError(res.error || textForLang(lang, 'Failed to start QQ scan bind', '\u83b7\u53d6\u4e8c\u7ef4\u7801\u5931\u8d25', '\u53d6\u5f97\u4e8c\u7dad\u78bc\u5931\u6557'));
                setQQBotQRLoading(false);
                setQQBotQRWaiting(false);
                return;
            }
            if (prevToken && prevToken !== token) {
                dropToken(prevToken);
            }
            activeQRTokenRef.current = token;
            setQQBotQRCode(qrURL);
            setQQBotQRLoading(false);
            setQQBotQRWaiting(true);
            const poll = async () => {
                if (activeQRSessionRef.current !== session) return;
                if (activeQRTokenRef.current !== token) return;
                try {
                    const p = await PollQQBotQRStatus(token);
                    if (activeQRSessionRef.current !== session || activeQRTokenRef.current !== token) {
                        if ((p.status || '') === 'confirmed' && !p.error) {
                            LoadConfig().then((cfg: any) => setConfig(cfg)).catch(() => {});
                        }
                        return;
                    }
                    const st = p.status || '';
                    if (st === 'confirmed') {
                        if (p.error) {
                            setQQBotQRError(p.error);
                            setTimeout(poll, 2000);
                            return;
                        }
                        activeQRTokenRef.current = '';
                        setScannedPending(false);
                        setQQBotQRWaiting(false);
                        setQQBotQRCode('');
                        setQQBotQRError('');
                        setQQBotStatus('connected');
                        LoadConfig().then((cfg: any) => setConfig(cfg)).catch(() => {});
                    } else if (st === 'expired') {
                        if (refreshCountRef.current < maxQQBotQRRefreshes) {
                            refreshCountRef.current += 1;
                            void startQRLogin(false);
                            return;
                        }
                        activeQRTokenRef.current = '';
                        dropToken(token);
                        setScannedPending(false);
                        setQQBotQRWaiting(false);
                        setQQBotQRCode('');
                        setQQBotQRError(p.error || p.message || textForLang(lang, 'QR expired', '\u4e8c\u7ef4\u7801\u5df2\u8fc7\u671f', '\u4e8c\u7dad\u78bc\u5df2\u904e\u671f'));
                    } else if (p.error) {
                        if (p.retryable === 'true') {
                            setQQBotQRError(p.error);
                            setTimeout(poll, 2000);
                            return;
                        }
                        activeQRTokenRef.current = '';
                        dropToken(token);
                        setScannedPending(false);
                        setQQBotQRWaiting(false);
                        setQQBotQRCode('');
                        setQQBotQRError(p.error);
                    } else {
                        setQQBotQRError('');
                        setScannedPending(st === 'pending');
                        setTimeout(poll, 2000);
                    }
                } catch (err: any) {
                    if (activeQRSessionRef.current !== session) return;
                    if (activeQRTokenRef.current !== token) return;
                    activeQRTokenRef.current = '';
                    dropToken(token);
                    setScannedPending(false);
                    setQQBotQRWaiting(false);
                    setQQBotQRCode('');
                    setQQBotQRError(err?.message || String(err));
                }
            };
            poll();
        } catch (e: any) {
            dropToken(prevToken);
            if (activeQRSessionRef.current !== session) return;
            const started = activeQRTokenRef.current;
            activeQRTokenRef.current = '';
            dropToken(started);
            setScannedPending(false);
            setQQBotQRError(e?.message || String(e));
            setQQBotQRLoading(false);
            setQQBotQRWaiting(false);
            setQQBotQRCode('');
        }
    };

    const cancelQRLogin = () => {
        activeQRSessionRef.current += 1;
        refreshCountRef.current = 0;
        const token = activeQRTokenRef.current;
        activeQRTokenRef.current = '';
        if (token) {
            CancelQQBotQRLogin(token).catch(() => {});
        }
        setScannedPending(false);
        setQQBotQRError('');
        setQQBotQRCode('');
        setQQBotQRWaiting(false);
        setQQBotQRLoading(false);
    };

    const idleLabel = boundAppID
        ? textForLang(lang, 'Scan again to rebind', '\u91cd\u65b0\u626b\u7801\u7ed1\u5b9a', '\u91cd\u65b0\u6383\u78bc\u7d81\u5b9a')
        : textForLang(lang, 'Scan QR to bind QQ Bot', '\u626b\u7801\u7ed1\u5b9a QQ \u673a\u5668\u4eba', '\u6383\u78bc\u7d81\u5b9a QQ \u6a5f\u5668\u4eba');

    const showScanButton = !qqBotQRCode && !qqBotQRLoading && !qqBotQRWaiting;

    return (
        <div className="weixin-qr-login-panel">
            {(showScanButton || trailingAction) && (
                <div className="weixin-qr-login-panel__actions">
                    {showScanButton && (
                        <button type="button" className="weixin-qr-login-panel__primary" aria-label={idleLabel} onClick={() => void startQRLogin()}>
                            {idleLabel}
                        </button>
                    )}
                    {trailingAction}
                </div>
            )}
            {qqBotQRLoading && (
                <div className="weixin-qr-login-panel__qr-card">
                    <p className="weixin-qr-login-panel__loading">
                        {textForLang(lang, 'Loading QR code...', '\u6b63\u5728\u83b7\u53d6\u4e8c\u7ef4\u7801...', '\u6b63\u5728\u53d6\u5f97\u4e8c\u7dad\u78bc...')}
                    </p>
                    <button type="button" className="weixin-qr-login-panel__cancel" onClick={cancelQRLogin}>
                        {textForLang(lang, 'Cancel', '\u53d6\u6d88', '\u53d6\u6d88')}
                    </button>
                </div>
            )}
            {qqBotQRCode && !qqBotQRLoading && (
                <div className="weixin-qr-login-panel__qr-card">
                    <QRCodeSVG value={qqBotQRCode} size={220} level="M" bgColor="#ffffff" className="weixin-qr-login-panel__qr" />
                    <p className="weixin-qr-login-panel__hint">
                        {textForLang(lang, 'Scan the QR code with QQ', '\u8bf7\u7528\u624b\u673a QQ \u626b\u63cf\u4e0a\u65b9\u4e8c\u7ef4\u7801', '\u8acb\u7528\u624b\u6a5f QQ \u6383\u63cf\u4e0a\u65b9\u4e8c\u7dad\u78bc')}
                    </p>
                    {qqBotQRWaiting && (
                        <p className="weixin-qr-login-panel__waiting">
                            {scannedPending
                                ? textForLang(lang, 'Scanned. Confirm on phone.', '\u5df2\u626b\u7801\uff0c\u8bf7\u5728\u624b\u673a\u4e0a\u786e\u8ba4\u3002', '\u5df2\u6383\u78bc\uff0c\u8acb\u5728\u624b\u6a5f\u4e0a\u78ba\u8a8d\u3002')
                                : textForLang(lang, 'Waiting for confirmation...', '\u7b49\u5f85\u626b\u7801\u786e\u8ba4\u4e2d...', '\u7b49\u5f85\u6383\u78bc\u78ba\u8a8d\u4e2d...')}
                        </p>
                    )}
                    <button type="button" className="weixin-qr-login-panel__cancel" onClick={cancelQRLogin}>
                        {textForLang(lang, 'Cancel', '\u53d6\u6d88', '\u53d6\u6d88')}
                    </button>
                </div>
            )}
            {qqBotQRError && <p className="weixin-qr-login-panel__error">{qqBotQRError}</p>}
        </div>
    );
};
