import React, { useState, Component, ReactNode } from "react";
import { ClawNetTaskBoard } from "./ClawNetTaskBoard";
import { ClawNetKnowledgePanel } from "./ClawNetKnowledgePanel";
import { ClawNetSwarmPanel } from "./ClawNetSwarmPanel";
import { ClawNetChatPanel } from "./ClawNetChatPanel";
import { ClawNetResumePanel } from "./ClawNetResumePanel";
import { ClawNetPredictionPanel } from "./ClawNetPredictionPanel";
import { ClawNetNutshellPanel } from "./ClawNetNutshellPanel";
import { colors } from "./styles";
import { cnTabBtn } from "./clawnetStyles";

type Props = { lang: string; clawNetRunning: boolean };

type ClawNetSubTab = "tasks" | "knowledge" | "swarm" | "chat" | "prediction" | "nutshell" | "resume";

const tabDefs: { id: ClawNetSubTab; icon: string; zh: string; en: string }[] = [
    { id: "tasks", icon: "🏪", zh: "任务集市", en: "Tasks" },
    { id: "knowledge", icon: "📚", zh: "知识网络", en: "Knowledge" },
    { id: "swarm", icon: "🧠", zh: "群体思考", en: "Swarm" },
    { id: "chat", icon: "💬", zh: "聊天", en: "Chat" },
    { id: "prediction", icon: "🔮", zh: "预测市场", en: "Predict" },
    { id: "nutshell", icon: "📦", zh: "任务包", en: "Nutshell" },
    { id: "resume", icon: "📋", zh: "简历/搜索", en: "Resume" },
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
            const zh = this.props.lang?.startsWith("zh");
            return (
                <div style={{ padding: "40px 20px", textAlign: "center", color: "#94a3b8" }}>
                    <div style={{ fontSize: "2.5rem", marginBottom: "12px" }}>⚠️</div>
                    <div style={{ fontSize: "0.9rem", fontWeight: 600, color: "#ef4444", marginBottom: "6px" }}>
                        {zh ? "面板加载出错" : "Panel failed to load"}
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
                        {zh ? "重试" : "Retry"}
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
    const zh = lang?.startsWith("zh");
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
                        <span>{zh ? t.zh : t.en}</span>
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
