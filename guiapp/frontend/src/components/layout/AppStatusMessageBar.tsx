import type { CSSProperties } from 'react';
import { tailText } from '../../i18n/mnTail'

type AppStatusMessageBarProps = {
    status: string;
    lang: string;
    config: any;
    qqBotStatus: string;
    telegramStatus: string;
    weixinStatus: string;
    lansengerStatus: string;
    messengerStatus?: string;
    maclawLLMOnline: boolean;
    maclawLLMConfigured: boolean;
    remoteActivated: boolean;
    showLansenger?: boolean;
    navTab: string;
    settingsTab: string;
    backgroundInstallStatus: string;
    lobsterOffline: string;
    lobsterHalf: string;
    onOpenIMSettings: () => void;
    onOpenLLMSettings: () => void;
    /**
     * row — full-width strip under main content (tool pages).
     * inline — compact cluster for the AI quick-settings bar (same row as chips).
     */
    variant?: 'row' | 'inline';
};

export const AppStatusMessageBar = ({
    status,
    lang,
    config,
    qqBotStatus,
    telegramStatus,
    weixinStatus,
    lansengerStatus,
    messengerStatus = '',
    maclawLLMOnline,
    maclawLLMConfigured,
    remoteActivated,
    showLansenger = false,
    navTab,
    settingsTab,
    backgroundInstallStatus,
    lobsterOffline,
    lobsterHalf,
    onOpenIMSettings,
    onOpenLLMSettings,
    variant = 'row',
}: AppStatusMessageBarProps) => {
    const lansengerConnected = showLansenger && lansengerStatus === 'connected';
    const lansengerConfigured = showLansenger && !!config?.lansenger_enabled;
    const imConnected = qqBotStatus === 'connected' || telegramStatus === 'connected' || weixinStatus === 'connected' || lansengerConnected || messengerStatus === 'connected';
    const anyImConfigured = !!config?.qqbot_enabled || !!config?.telegram_enabled || !!config?.weixin_enabled || !!config?.messenger_enabled || lansengerConfigured;
    const showImWarning = anyImConfigured && !imConnected;
    const showWarning = (!maclawLLMOnline || !remoteActivated || showImWarning) && !(navTab === 'settings' && settingsTab === 'llm');
    const isImIssue = maclawLLMOnline && remoteActivated && showImWarning;
    const statusText = String(status || '').trim();
    const installStatus = String(backgroundInstallStatus || '').trim();
    // Collapse entirely when idle so neither the tool-page strip nor the AI
    // quick-settings row reserves empty height.
    if (!statusText && !showWarning && !installStatus) {
        return null;
    }
    const successMarker = installStatus.startsWith('?') || installStatus.startsWith('??');
    const statusTone = (statusText.includes("Error") || statusText.includes("!"))
        ? 'var(--theme-danger, #dc1f25)'
        : 'var(--theme-success, #11813a)';
    const noticeTone = 'var(--theme-text-muted, #5b6b80)';
    const progressTone = successMarker ? 'var(--theme-success, #11813a)' : 'var(--theme-text-muted, #5b6b80)';
    const inline = variant === 'inline';

    // Inline sits after chips: take only natural width (can shrink), do not compete
    // with chips for free space (flex-grow would force early chip scrolling).
    const rootStyle: CSSProperties = inline
        ? {
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'flex-end',
            gap: 8,
            minWidth: 0,
            maxWidth: 'min(52%, 28rem)',
            flex: '0 1 auto',
            marginLeft: 'auto',
            overflow: 'hidden',
            fontSize: '0.72rem',
            lineHeight: 1.2,
        }
        : {
            padding: '2px 20px 4px 20px',
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            flexShrink: 0,
        };

    const textStyle: CSSProperties = inline
        ? {
            color: statusTone,
            minWidth: 0,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            flex: '0 1 auto',
        }
        : { color: statusTone };

    const sideStyle: CSSProperties = {
        display: 'flex',
        alignItems: 'center',
        gap: inline ? 8 : 10,
        minWidth: 0,
        flexShrink: inline ? 1 : undefined,
        overflow: inline ? 'hidden' : undefined,
    };
    const hasSide = !!(showWarning || installStatus);

    return (
        <div
            className={inline ? 'status-message status-message--inline' : 'status-message'}
            data-testid="app-status-message-bar"
            data-variant={variant}
            style={rootStyle}
        >
            {statusText ? (
                <span key={statusText} style={textStyle} title={statusText}>
                    {statusText}
                </span>
            ) : null}
            {hasSide ? <div style={sideStyle}>
                {showWarning && (
                    <span
                        style={{
                            fontSize: '0.72rem',
                            color: noticeTone,
                            cursor: 'pointer',
                            display: 'flex',
                            alignItems: 'center',
                            gap: '3px',
                            minWidth: 0,
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                            flexShrink: 1,
                        }}
                        onClick={() => { if (isImIssue) { onOpenIMSettings(); } else { onOpenLLMSettings(); } }}
                        title={tailText(lang ?? "", 'Click to configure', '点击配置')}
                    >
                        <img src={(!maclawLLMOnline && !remoteActivated) ? lobsterOffline : lobsterHalf} alt="" className="asmb-warn-icon" />
                        <span className="asmb-ellipsis">
                            {!maclawLLMOnline
                                ? (maclawLLMConfigured
                                    ? (tailText(lang ?? "", 'LLM unreachable, remote commands unavailable', '大模型不可达，远程指令不可用'))
                                    : (tailText(lang ?? "", 'LLM not configured, remote commands unavailable', '未配置大模型，远程指令不可用')))
                                : !remoteActivated
                                    ? (tailText(lang ?? "", 'Mobile not registered', '手机端未注册'))
                                        : (tailText(lang ?? "", 'IM not connected', 'IM 未连接'))}
                        </span>
                    </span>
                )}
                {installStatus && (
                    <span
                        style={{
                            fontSize: '0.75rem',
                            color: progressTone,
                            display: 'flex',
                            alignItems: 'center',
                            gap: '4px',
                            minWidth: 0,
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                            flexShrink: 1,
                        }}
                        title={installStatus}
                    >
                        {!successMarker && (
                            <span className="asmb-spinner"></span>
                        )}
                        <span className="asmb-ellipsis">{installStatus}</span>
                    </span>
                )}
            </div> : null}
        </div>
    );
};
