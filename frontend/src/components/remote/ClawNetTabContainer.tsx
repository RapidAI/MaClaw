import { useState } from "react";
import { ClawNetTaskBoard } from "./ClawNetTaskBoard";
import { ClawNetKnowledgePanel } from "./ClawNetKnowledgePanel";
import { ClawNetSwarmPanel } from "./ClawNetSwarmPanel";
import { ClawNetChatPanel } from "./ClawNetChatPanel";
import { ClawNetResumePanel } from "./ClawNetResumePanel";
import { colors } from "./styles";
import { cnTabBtn } from "./clawnetStyles";

type Props = { lang: string; clawNetRunning: boolean };

type ClawNetSubTab = "tasks" | "knowledge" | "swarm" | "chat" | "resume";

const tabDefs: { id: ClawNetSubTab; icon: string; zh: string; en: string }[] = [
    { id: "tasks", icon: "🏪", zh: "任务集市", en: "Tasks" },
    { id: "knowledge", icon: "📚", zh: "知识网络", en: "Knowledge" },
    { id: "swarm", icon: "🧠", zh: "群体思考", en: "Swarm" },
    { id: "chat", icon: "💬", zh: "聊天", en: "Chat" },
    { id: "resume", icon: "📋", zh: "简历/搜索", en: "Resume" },
];

export function ClawNetTabContainer({ lang, clawNetRunning }: Props) {
    const zh = lang?.startsWith("zh");
    const [subTab, setSubTab] = useState<ClawNetSubTab>("tasks");

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

            {/* Content – keep all panels mounted so state survives tab switches */}
            <div style={{ flex: 1, overflow: "auto", position: "relative" }}>
                <div style={{ display: subTab === "tasks" ? "block" : "none" }}><ClawNetTaskBoard lang={lang} clawNetRunning={clawNetRunning} /></div>
                <div style={{ display: subTab === "knowledge" ? "block" : "none" }}><ClawNetKnowledgePanel lang={lang} clawNetRunning={clawNetRunning} /></div>
                <div style={{ display: subTab === "swarm" ? "block" : "none" }}><ClawNetSwarmPanel lang={lang} clawNetRunning={clawNetRunning} /></div>
                <div style={{ display: subTab === "chat" ? "block" : "none" }}><ClawNetChatPanel lang={lang} clawNetRunning={clawNetRunning} /></div>
                <div style={{ display: subTab === "resume" ? "block" : "none" }}><ClawNetResumePanel lang={lang} clawNetRunning={clawNetRunning} /></div>
            </div>
        </div>
    );
}
