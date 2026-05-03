import React, { useState, Component, ReactNode } from "react";
import { AgentNetTaskBoard } from "./AgentNetTaskBoard";
import { AgentNetKnowledgePanel } from "./AgentNetKnowledgePanel";
import { AgentNetChatPanel } from "./AgentNetChatPanel";
import { AgentNetBundlePanel } from "./AgentNetBundlePanel";
import { AgentNetNetworkPanel } from "./AgentNetNetworkPanel";
import { AgentNetPoIPanel } from "./AgentNetPoIPanel";
import { AgentNetServicesPanel } from "./AgentNetServicesPanel";
import { AgentNetToolsPanel } from "./AgentNetToolsPanel";
import { colors } from "./styles";
import { cnTabBtn } from "./agentnetStyles";

const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

type Props = { lang: string; agentNetRunning: boolean };

type AgentNetSubTab = "tasks" | "knowledge" | "chat" | "network" | "poi" | "services" | "tools" | "bundle";

const tabDefs: { id: AgentNetSubTab; icon: string; label: (lang: string) => string }[] = [
    { id: "tasks", icon: "TASK", label: (lang) => localizeText(lang, "Tasks", "Tasks") },
    { id: "knowledge", icon: "KNOW", label: (lang) => localizeText(lang, "Knowledge", "Knowledge") },
    { id: "chat", icon: "CHAT", label: (lang) => localizeText(lang, "Chat", "Chat") },
    { id: "network", icon: "NET", label: (lang) => localizeText(lang, "Network", "Network") },
    { id: "poi", icon: "POI", label: (lang) => localizeText(lang, "PoI", "PoI") },
    { id: "services", icon: "SVC", label: (lang) => localizeText(lang, "Services", "Services") },
    { id: "tools", icon: "TOOL", label: (lang) => localizeText(lang, "Tools", "Tools") },
    { id: "bundle", icon: "NUT", label: (lang) => localizeText(lang, "Bundle", "Bundle") },
];

// ErrorBoundary to prevent a single panel crash from white-screening the entire view
class AgentNetErrorBoundary extends Component<
    { lang?: string; onRetry: () => void; children: ReactNode },
    { hasError: boolean; error: string }
> {
    constructor(props: any) {
        super(props);
        this.state = { hasError: false, error: "" };
    }
    static getDerivedStateFromError(error: Error) {
        return { hasError: true, error: error?.message || "Unknown error" };
    }
    componentDidCatch(error: Error, info: React.ErrorInfo) {
        console.error("[AgentNet] Panel crashed:", error, info.componentStack);
    }
    render() {
        if (this.state.hasError) {
            return (
                <div style={{ padding: "40px 20px", textAlign: "center", color: colors.textMuted }}>
                    <div style={{ fontSize: "2.5rem", marginBottom: "12px" }}>!</div>
                    <div style={{ fontSize: "0.9rem", fontWeight: 600, color: colors.danger, marginBottom: "6px" }}>
                        {localizeText(this.props.lang, "Panel failed to load", "Panel failed to load")}
                    </div>
                    <div style={{ fontSize: "0.78rem", color: colors.textSecondary, maxWidth: "360px", margin: "0 auto 12px" }}>
                        {this.state.error}
                    </div>
                    <button
                        onClick={() => { this.setState({ hasError: false, error: "" }); this.props.onRetry(); }}
                        style={{
                            padding: "6px 16px", borderRadius: "6px", border: `1px solid ${colors.border}`,
                            background: colors.bg, color: colors.text, cursor: "pointer", fontSize: "0.78rem", fontWeight: 600,
                        }}
                    >
                        {localizeText(this.props.lang, "Retry", "Retry")}
                    </button>
                </div>
            );
        }
        return this.props.children;
    }
}

function renderSubTab(subTab: AgentNetSubTab, lang: string, agentNetRunning: boolean) {
    switch (subTab) {
        case "tasks": return <AgentNetTaskBoard lang={lang} agentNetRunning={agentNetRunning} />;
        case "knowledge": return <AgentNetKnowledgePanel lang={lang} agentNetRunning={agentNetRunning} />;
        case "chat": return <AgentNetChatPanel lang={lang} agentNetRunning={agentNetRunning} />;
        case "network": return <AgentNetNetworkPanel lang={lang} agentNetRunning={agentNetRunning} />;
        case "poi": return <AgentNetPoIPanel lang={lang} agentNetRunning={agentNetRunning} />;
        case "services": return <AgentNetServicesPanel lang={lang} agentNetRunning={agentNetRunning} />;
        case "tools": return <AgentNetToolsPanel lang={lang} agentNetRunning={agentNetRunning} />;
        case "bundle": return <AgentNetBundlePanel lang={lang} agentNetRunning={agentNetRunning} />;
        default: return null;
    }
}

export function AgentNetTabContainer({ lang, agentNetRunning }: Props) {
    const [subTab, setSubTab] = useState<AgentNetSubTab>("tasks");
    // Key to force remount on retry
    const [retryKey, setRetryKey] = useState(0);

    return (
        <div style={{ display: "flex", flexDirection: "column", height: "100%" }}>
            {/* Sub-tab bar */}
            <div style={{
                display: "flex", gap: "6px", padding: "10px 14px 0",
                borderBottom: `1px solid ${colors.border}`, paddingBottom: "10px",
                flexWrap: "wrap",
            }}>
                {tabDefs.map(t => (
                    <button key={t.id} style={cnTabBtn(subTab === t.id)} onClick={() => setSubTab(t.id)}>
                        <span>{t.icon}</span>
                        <span>{t.label(lang)}</span>
                    </button>
                ))}
            </div>

            {/* Content: only render the active panel (lazy) to avoid concurrent backend storms */}
            <div style={{ flex: 1, overflow: "auto", position: "relative" }}>
                <AgentNetErrorBoundary key={`${subTab}-${retryKey}`} lang={lang} onRetry={() => setRetryKey(k => k + 1)}>
                    {renderSubTab(subTab, lang, agentNetRunning)}
                </AgentNetErrorBoundary>
            </div>
        </div>
    );
}
