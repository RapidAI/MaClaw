import type { CodingAgentProgress } from '../ai/CodingAgentProgressStatus';
import { CodingAgentCompactStatus } from './CodingAgentCompactStatus';

type AppStatusMessageBarProps = {
    status: string;
    lang: string;
    config: any;
    qqBotStatus: string;
    telegramStatus: string;
    weixinStatus: string;
    lansengerStatus: string;
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
    codingAgentProgress?: CodingAgentProgress | null;
    /** Prefer explicit theme so coding-agent failure chrome remaps on dark. */
    isDark?: boolean;
};

export const AppStatusMessageBar = ({
    status,
    lang,
    config,
    qqBotStatus,
    telegramStatus,
    weixinStatus,
    lansengerStatus,
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
    codingAgentProgress = null,
    isDark,
}: AppStatusMessageBarProps) => {
    const lansengerConnected = showLansenger && lansengerStatus === 'connected';
    const lansengerConfigured = showLansenger && !!config?.lansenger_enabled;
    const imConnected = qqBotStatus === 'connected' || telegramStatus === 'connected' || weixinStatus === 'connected' || lansengerConnected;
    const anyImConfigured = !!config?.qqbot_enabled || !!config?.telegram_enabled || !!config?.weixin_enabled || lansengerConfigured;
    const showImWarning = anyImConfigured && !imConnected;
    const showWarning = (!maclawLLMOnline || !remoteActivated || showImWarning) && !(navTab === 'settings' && settingsTab === 'llm');
    const isImIssue = maclawLLMOnline && remoteActivated && showImWarning;
    const successMarker = backgroundInstallStatus.startsWith('?') || backgroundInstallStatus.startsWith('??');
    const statusTone = (status.includes("Error") || status.includes("!"))
        ? 'var(--theme-danger, #c43d34)'
        : 'var(--theme-success, #4f7f6f)';
    const noticeTone = 'var(--theme-text-muted, #64748b)';
    const progressTone = successMarker ? 'var(--theme-success, #4f7f6f)' : 'var(--theme-text-muted, #64748b)';

    return (
        <div className="status-message" style={{ padding: '0 20px 4px 20px', minHeight: '20px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <span key={status} style={{ color: statusTone }}>
                {status}
            </span>
            <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                {showWarning && (
                    <span
                        style={{ fontSize: '0.72rem', color: noticeTone, cursor: 'pointer', display: 'flex', alignItems: 'center', gap: '3px' }}
                        onClick={() => { if (isImIssue) { onOpenIMSettings(); } else { onOpenLLMSettings(); } }}
                        title={lang?.startsWith('zh') ? 'Click to configure' : 'Click to configure'}
                    >
                        <img src={(!maclawLLMOnline && !remoteActivated) ? lobsterOffline : lobsterHalf} alt="" style={{ width: '14px', height: '14px' }} />
                        {!maclawLLMOnline
                            ? (maclawLLMConfigured
                                ? (lang?.startsWith('zh') ? 'LLM unreachable, remote commands unavailable' : 'LLM unreachable, remote commands unavailable')
                                : (lang?.startsWith('zh') ? 'LLM not configured, remote commands unavailable' : 'LLM not configured, remote commands unavailable'))
                            : !remoteActivated
                                ? (lang?.startsWith('zh') ? 'Mobile not registered' : 'Mobile not registered')
                                    : (lang?.startsWith('zh') ? 'IM not connected' : 'IM not connected')}
                    </span>
                )}
                {codingAgentProgress && (
                    <CodingAgentCompactStatus
                        progress={codingAgentProgress}
                        lang={lang}
                        testId="app-status-coding-agent"
                        variant="status-bar"
                        isDark={isDark}
                    />
                )}
                {backgroundInstallStatus && (
                    <span style={{
                        fontSize: '0.75rem',
                        color: progressTone,
                        display: 'flex',
                        alignItems: 'center',
                        gap: '4px'
                    }}>
                        {!successMarker && (
                            <span style={{
                                display: 'inline-block',
                                width: '10px',
                                height: '10px',
                                border: '2px solid var(--theme-text-muted, #64748b)',
                                borderTopColor: 'transparent',
                                borderRadius: '50%',
                                animation: 'spin 1s linear infinite'
                            }}></span>
                        )}
                        {backgroundInstallStatus}
                    </span>
                )}
            </div>
        </div>
    );
};
