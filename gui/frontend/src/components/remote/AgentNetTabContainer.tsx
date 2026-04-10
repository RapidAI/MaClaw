import React, { useState, Component, ReactNode } from "react";
import { ClawNetTaskBoard } from "./AgentNetTaskBoard";
import { ClawNetKnowledgePanel } from "./AgentNetKnowledgePanel";
import { ClawNetSwarmPanel } from "./AgentNetSwarmPanel";
import { ClawNetChatPanel } from "./AgentNetChatPanel";
import { ClawNetResumePanel } from "./AgentNetResumePanel";
import { ClawNetPredictionPanel } from "./AgentNetPredictionPanel";
import { ClawNetNutshellPanel } from "./AgentNetNutshellPanel";
import { colors } from "./styles";
import { cnTabBtn } from "./agentnetStyles";

const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

type Props = { lang: string; clawNetRunning: boolean };

type ClawNetSubTab = "tasks" | "knowledge" | "swarm" | "chat" | "prediction" | "nutshell" | "resume";

const tabDefs: { id: ClawNetSubTab; icon: string; label: (lang: string) => string }[] = [
    { id: "tasks", icon: "🏪", label: (lang) => localizeText(lang, "Tasks", "任务集市") },
    { id: "knowledge", icon: "📚", label: (lang) => localizeText(lang, "Knowledge", "知识网络") },
    { id: "swarm", icon: "🧠", label: (lang) => localizeText(lang, "Swarm", "群体思考") },
    { id: "chat", icon: "💬", label: (lang) => localizeText(lang, "Chat", "聊天") },
    { id: "prediction", icon: "🔮", label: (lang) => localizeText(lang, "Predict", "预测市场") },
    { id: "nutshell", icon: "📦", label: (lang) => localizeText(lang, "Nutshell", "任务包") },
    { id: "resume", icon: "📋", label: (lang) => localizeText(lang, "Resume", "简历/搜索") },
];

// ErrorBoundary to prevent a single panel crash from white-screening the entire view
class ClawNetErrorBoundary extends Component<
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
        console.error("[ClawNet] Panel crashed:", error, info.componentStack);
    }
    render() {
        if (this.state.hasError) {
            return (
                <div style={{ padding: "40px 20px", textAlign: "center", color: "#94a3b8" }}>
                    <div style={{ fontSize: "2.5rem", marginBottom: "12px" }}>⚠️</div>
                    <div style={{ fontSize: "0.9rem", fontWeight: 600, color: "#ef4444", marginBottom: "6px" }}>
                        {localizeText(this.props.lang, "Panel failed to load", "面板加载出错")}
                    </div>
                    <div style={{ fontSize: "0.78rem", color: "#b0b8c8", maxWidth: "360px", margin: "0 auto 12px" }}>
                        {this.state.error}
                    </div>
                    <button
                        onClick={() => { this.setState({ hasError: false, error: "" }); this.props.onRetry(); }}
                        style={{
                            padding: "6px 16px", borderRadius: "6px", border: `1px solid ${colors.border}`,
                            background: colors.bg, color: colors.text, cursor: "pointer", fontSize: "0.78rem", fontWeight: 600,
                        }}
                    >
                        {localizeText(this.props.lang, "Retry", "重试")}
                    </button>
                </div>
            );
        }
        return this.props.children;
    }
}

function renderSubTab(subTab: ClawNetSubTab, lang: string, clawNetRunning: boolean) {
    switch (subTab) {
        case "tasks": return <ClawNetTaskBoard lang={lang} clawNetRunning={clawNetRunning} />;
        case "knowledge": return <ClawNetKnowledgePanel lang={lang} clawNetRunning={clawNetRunning} />;
        case "swarm": return <ClawNetSwarmPanel lang={lang} clawNetRunning={clawNetRunning} />;
        case "chat": return <ClawNetChatPanel lang={lang} clawNetRunning={clawNetRunning} />;
        case "prediction": return <ClawNetPredictionPanel lang={lang} clawNetRunning={clawNetRunning} />;
        case "nutshell": return <ClawNetNutshellPanel lang={lang} clawNetRunning={clawNetRunning} />;
        case "resume": return <ClawNetResumePanel lang={lang} clawNetRunning={clawNetRunning} />;
        default: return null;
    }
}

export function ClawNetTabContainer({ lang, clawNetRunning }: Props) {
    const [subTab, setSubTab] = useState<ClawNetSubTab>("tasks");
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

            {/* Content – only render the active panel (lazy) to avoid concurrent backend storms */}
            <div style={{ flex: 1, overflow: "auto", position: "relative" }}>
                <ClawNetErrorBoundary key={`${subTab}-${retryKey}`} lang={lang} onRetry={() => setRetryKey(k => k + 1)}>
                    {renderSubTab(subTab, lang, clawNetRunning)}
                </ClawNetErrorBoundary>
            </div>
        </div>
    );
}
