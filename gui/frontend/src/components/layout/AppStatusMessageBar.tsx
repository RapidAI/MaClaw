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
    agentNetRunning: boolean;
    hideAgentNet?: boolean;
    showLansenger?: boolean;
    navTab: string;
    settingsTab: string;
    backgroundInstallStatus: string;
    lobsterOffline: string;
    lobsterHalf: string;
    onOpenIMSettings: () => void;
    onOpenLLMSettings: () => void;
    codingAgentProgress?: CodingAgentProgress | null;
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
    agentNetRunning,
    hideAgentNet = false,
    showLansenger = false,
    navTab,
    settingsTab,
    backgroundInstallStatus,
    lobsterOffline,
    lobsterHalf,
    onOpenIMSettings,
    onOpenLLMSettings,
    codingAgentProgress = null,
}: AppStatusMessageBarProps) => {
    const lansengerConnected = showLansenger && lansengerStatus === 'connected';
    const lansengerConfigured = showLansenger && !!config?.lansenger_enabled;
    const imConnected = qqBotStatus === 'connected' || telegramStatus === 'connected' || weixinStatus === 'connected' || lansengerConnected;
    const anyImConfigured = !!config?.qqbot_enabled || !!config?.telegram_enabled || !!config?.weixin_enabled || lansengerConfigured;
    const showImWarning = anyImConfigured && !imConnected;
    const agentNetRequired = !hideAgentNet && !!config?.agentnet_enabled;
    const agentNetIssue = agentNetRequired && !agentNetRunning;
    const showWarning = (!maclawLLMOnline || !remoteActivated || agentNetIssue || showImWarning) && !(navTab === 'settings' && settingsTab === 'llm');
    const isImIssue = maclawLLMOnline && remoteActivated && !agentNetIssue && showImWarning;
    const successMarker = backgroundInstallStatus.startsWith('?') || backgroundInstallStatus.startsWith('??');

    return (
        <div className="status-message" style={{ padding: '0 20px 4px 20px', minHeight: '20px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <span key={status} style={{ color: (status.includes("Error") || status.includes("!") || status.includes("first")) ? '#ef4444' : '#10b981' }}>
                {status}
            </span>
            <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                {showWarning && (
                    <span
                        style={{ fontSize: '0.72rem', color: '#f59e0b', cursor: 'pointer', display: 'flex', alignItems: 'center', gap: '3px' }}
                        onClick={() => { if (isImIssue) { onOpenIMSettings(); } else { onOpenLLMSettings(); } }}
                        title={lang?.startsWith('zh') ? 'Click to configure' : 'Click to configure'}
                    >
                        <img src={(!maclawLLMOnline && !remoteActivated && agentNetIssue) ? lobsterOffline : lobsterHalf} alt="" style={{ width: '14px', height: '14px' }} />
                        {!maclawLLMOnline
                            ? (maclawLLMConfigured
                                ? (lang?.startsWith('zh') ? 'LLM unreachable, remote commands unavailable' : 'LLM unreachable, remote commands unavailable')
                                : (lang?.startsWith('zh') ? 'LLM not configured, remote commands unavailable' : 'LLM not configured, remote commands unavailable'))
                            : !remoteActivated
                                ? (lang?.startsWith('zh') ? 'Mobile not registered' : 'Mobile not registered')
                                : agentNetIssue
                                    ? (lang?.startsWith('zh') ? 'AgentNet not connected' : 'AgentNet not connected')
                                    : (lang?.startsWith('zh') ? 'IM not connected' : 'IM not connected')}
                    </span>
                )}
                {codingAgentProgress && (
                    <CodingAgentCompactStatus progress={codingAgentProgress} lang={lang} testId="app-status-coding-agent" variant="status-bar" />
                )}
                {backgroundInstallStatus && (
                    <span style={{
                        fontSize: '0.75rem',
                        color: successMarker ? '#10b981' : '#9ca3af',
                        display: 'flex',
                        alignItems: 'center',
                        gap: '4px'
                    }}>
                        {!successMarker && (
                            <span style={{
                                display: 'inline-block',
                                width: '10px',
                                height: '10px',
                                border: '2px solid #9ca3af',
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
