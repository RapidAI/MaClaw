type AppStatusMessageBarProps = {
    status: string;
    lang: string;
    config: any;
    qqBotStatus: string;
    telegramStatus: string;
    weixinStatus: string;
    maclawLLMOnline: boolean;
    maclawLLMConfigured: boolean;
    remoteActivated: boolean;
    agentNetRunning: boolean;
    navTab: string;
    settingsTab: string;
    backgroundInstallStatus: string;
    lobsterOffline: string;
    lobsterHalf: string;
    onOpenIMSettings: () => void;
    onOpenLLMSettings: () => void;
};

export const AppStatusMessageBar = ({
    status,
    lang,
    config,
    qqBotStatus,
    telegramStatus,
    weixinStatus,
    maclawLLMOnline,
    maclawLLMConfigured,
    remoteActivated,
    agentNetRunning,
    navTab,
    settingsTab,
    backgroundInstallStatus,
    lobsterOffline,
    lobsterHalf,
    onOpenIMSettings,
    onOpenLLMSettings,
}: AppStatusMessageBarProps) => {
    const imConnected = qqBotStatus === 'connected' || telegramStatus === 'connected' || weixinStatus === 'connected';
    const anyImConfigured = !!config?.qqbot_enabled || !!config?.telegram_enabled || !!config?.weixin_enabled;
    const showImWarning = anyImConfigured && !imConnected;
    const showWarning = (!maclawLLMOnline || !remoteActivated || !agentNetRunning || showImWarning) && !(navTab === 'settings' && settingsTab === 'llm');
    const isImIssue = maclawLLMOnline && remoteActivated && agentNetRunning && showImWarning;
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
                        <img src={(!maclawLLMOnline && !remoteActivated && !agentNetRunning) ? lobsterOffline : lobsterHalf} alt="" style={{ width: '14px', height: '14px' }} />
                        {!maclawLLMOnline
                            ? (maclawLLMConfigured
                                ? (lang?.startsWith('zh') ? 'LLM unreachable, remote commands unavailable' : 'LLM unreachable, remote commands unavailable')
                                : (lang?.startsWith('zh') ? 'LLM not configured, remote commands unavailable' : 'LLM not configured, remote commands unavailable'))
                            : !remoteActivated
                                ? (lang?.startsWith('zh') ? 'Mobile not registered' : 'Mobile not registered')
                                : !agentNetRunning
                                    ? (lang?.startsWith('zh') ? 'AgentNet not connected' : 'AgentNet not connected')
                                    : (lang?.startsWith('zh') ? 'IM not connected' : 'IM not connected')}
                    </span>
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
