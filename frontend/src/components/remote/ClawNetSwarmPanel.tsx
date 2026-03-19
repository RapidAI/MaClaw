import { useState, useEffect, useCallback, useRef } from "react";
import {
    ClawNetListSwarmSessions,
    ClawNetCreateSwarmSession,
    ClawNetJoinSwarm,
    ClawNetContributeToSwarm,
    ClawNetSynthesizeSwarm,
} from "../../../wailsjs/go/main/App";
import { colors, radius } from "./styles";
import { cnCard, cnLabel, cnInput, cnActionBtn } from "./clawnetStyles";

type Props = { lang: string; clawNetRunning: boolean };

interface SwarmSession {
    id: string; topic: string; question?: string; status?: string;
    participants?: number; contributions?: number; created_at?: string;
    synthesis?: string;
}

const STANCES = [
    { value: "support", zh: "支持", en: "Support", color: colors.success },
    { value: "oppose", zh: "反对", en: "Oppose", color: colors.danger },
    { value: "neutral", zh: "中立", en: "Neutral", color: colors.textSecondary },
] as const;

const ACTION_MSG_TTL = 4000;

export function ClawNetSwarmPanel({ lang, clawNetRunning }: Props) {
    const zh = lang?.startsWith("zh");
    const [sessions, setSessions] = useState<SwarmSession[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [showCreate, setShowCreate] = useState(false);
    const [newTopic, setNewTopic] = useState("");
    const [newQuestion, setNewQuestion] = useState("");
    const [createBusy, setCreateBusy] = useState(false);
    const [activeSession, setActiveSession] = useState<string | null>(null);
    const [contribText, setContribText] = useState("");
    const [contribStance, setContribStance] = useState("neutral");
    const [contribBusy, setContribBusy] = useState(false);
    const [actionMsg, setActionMsg] = useState("");
    const msgTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
    const mountedRef = useRef(true);
    useEffect(() => { mountedRef.current = true; return () => { mountedRef.current = false; }; }, []);

    // Fix: auto-clear action messages after a few seconds
    const showMsg = useCallback((msg: string) => {
        setActionMsg(msg);
        if (msgTimer.current) clearTimeout(msgTimer.current);
        msgTimer.current = setTimeout(() => { if (mountedRef.current) setActionMsg(""); }, ACTION_MSG_TTL);
    }, []);

    const refresh = useCallback(async () => {
        if (!clawNetRunning) return;
        setLoading(true); setError("");
        try {
            const res = await ClawNetListSwarmSessions();
            if (mountedRef.current && res.ok) setSessions((res.sessions as SwarmSession[]) || []);
            else if (mountedRef.current && res.error) setError(res.error as string);
        } catch (e: any) { if (mountedRef.current) setError(e.message); }
        if (mountedRef.current) setLoading(false);
    }, [clawNetRunning]);

    useEffect(() => { refresh(); }, [refresh]);

    const handleCreate = async () => {
        if (!newTopic.trim()) return;
        setCreateBusy(true);
        try {
            const res = await ClawNetCreateSwarmSession(newTopic.trim(), newQuestion.trim());
            if (res.ok) { setNewTopic(""); setNewQuestion(""); setShowCreate(false); refresh(); }
            else showMsg(`❌ ${res.error}`);
        } catch (e: any) { showMsg(`❌ ${e.message}`); }
        setCreateBusy(false);
    };

    const handleJoin = async (id: string) => {
        try {
            const res = await ClawNetJoinSwarm(id);
            if (!res.ok) showMsg(`❌ ${res.error}`);
            else { showMsg(zh ? "✅ 已加入" : "✅ Joined"); refresh(); }
        } catch (e: any) { showMsg(`❌ ${e.message}`); }
    };

    const handleContribute = async () => {
        if (!activeSession || !contribText.trim()) return;
        setContribBusy(true);
        try {
            const res = await ClawNetContributeToSwarm(activeSession, contribText.trim(), contribStance);
            if (res.ok) { setContribText(""); showMsg(zh ? "✅ 已贡献" : "✅ Contributed"); refresh(); }
            else showMsg(`❌ ${res.error}`);
        } catch (e: any) { showMsg(`❌ ${e.message}`); }
        setContribBusy(false);
    };

    const handleSynthesize = async (id: string) => {
        try {
            const res = await ClawNetSynthesizeSwarm(id);
            if (res.ok) { showMsg(zh ? "✅ 综合完成" : "✅ Synthesized"); refresh(); }
            else showMsg(`❌ ${res.error}`);
        } catch (e: any) { showMsg(`❌ ${e.message}`); }
    };

    if (!clawNetRunning) return <div style={cnLabel}>{zh ? "虾网未连接" : "ClawNet not connected"}</div>;

    return (
        <div style={{ padding: "10px 14px" }}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: "10px" }}>
                <span style={{ fontSize: "0.78rem", fontWeight: 600, color: colors.text }}>🧠 {zh ? "群体思考" : "Swarm Think"}</span>
                <div style={{ display: "flex", gap: "6px" }}>
                    <button style={cnActionBtn()} onClick={() => setShowCreate(!showCreate)}>{showCreate ? (zh ? "取消" : "Cancel") : (zh ? "发起讨论" : "New Swarm")}</button>
                    <button style={cnActionBtn(loading)} onClick={refresh} disabled={loading}>{zh ? "刷新" : "Refresh"}</button>
                </div>
            </div>

            {actionMsg && <div style={{ fontSize: "0.72rem", marginBottom: "8px", color: actionMsg.startsWith("✅") ? colors.success : colors.danger }}>{actionMsg}</div>}

            {showCreate && (
                <div style={{ ...cnCard, background: colors.bg }}>
                    <input value={newTopic} onChange={e => setNewTopic(e.target.value)} placeholder={zh ? "讨论主题" : "Topic"} style={{ ...cnInput, marginBottom: "6px" }} />
                    <textarea value={newQuestion} onChange={e => setNewQuestion(e.target.value)} placeholder={zh ? "核心问题（可选）" : "Question (optional)"}
                        style={{ ...cnInput, minHeight: "60px", resize: "vertical", marginBottom: "8px" }} />
                    <button style={cnActionBtn(createBusy || !newTopic.trim())} onClick={handleCreate} disabled={createBusy || !newTopic.trim()}>
                        {createBusy ? "..." : (zh ? "创建" : "Create")}
                    </button>
                </div>
            )}

            {error && <div style={{ fontSize: "0.72rem", color: colors.danger, marginBottom: "8px" }}>{error}</div>}
            {loading && <div style={cnLabel}>{zh ? "加载中..." : "Loading..."}</div>}

            {sessions.map(s => (
                <div key={s.id} style={cnCard}>
                    <div style={{ fontSize: "0.76rem", fontWeight: 600, color: colors.text, marginBottom: "4px" }}>{s.topic}</div>
                    {s.question && <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginBottom: "6px" }}>{s.question}</div>}
                    <div style={{ display: "flex", gap: "10px", fontSize: "0.68rem", color: colors.textMuted, marginBottom: "6px" }}>
                        {s.status && <span>📊 {s.status}</span>}
                        {s.participants != null && <span>👥 {s.participants}</span>}
                        {s.contributions != null && <span>💬 {s.contributions}</span>}
                        {s.created_at && <span>{s.created_at}</span>}
                    </div>
                    {s.synthesis && (
                        <div style={{ fontSize: "0.72rem", color: colors.textSecondary, background: colors.accentBg, padding: "6px 8px", borderRadius: radius.md, marginBottom: "6px", whiteSpace: "pre-wrap" }}>
                            <span style={{ fontWeight: 600 }}>📝 {zh ? "综合结论" : "Synthesis"}:</span> {s.synthesis}
                        </div>
                    )}
                    <div style={{ display: "flex", gap: "6px", flexWrap: "wrap" }}>
                        <button style={{ ...cnActionBtn(), padding: "2px 8px", fontSize: "0.68rem" }} onClick={() => handleJoin(s.id)}>{zh ? "加入" : "Join"}</button>
                        <button style={{ ...cnActionBtn(), padding: "2px 8px", fontSize: "0.68rem" }} onClick={() => setActiveSession(activeSession === s.id ? null : s.id)}>
                            {zh ? "贡献观点" : "Contribute"}
                        </button>
                        <button style={{ ...cnActionBtn(), padding: "2px 8px", fontSize: "0.68rem" }} onClick={() => handleSynthesize(s.id)}>{zh ? "综合" : "Synthesize"}</button>
                    </div>
                    {activeSession === s.id && (
                        <div style={{ marginTop: "8px", paddingTop: "6px", borderTop: `1px solid ${colors.border}` }}>
                            <div style={{ display: "flex", gap: "6px", marginBottom: "6px" }}>
                                {STANCES.map(st => (
                                    <button key={st.value} onClick={() => setContribStance(st.value)}
                                        style={{ background: contribStance === st.value ? st.color : "transparent", color: contribStance === st.value ? "#fff" : st.color,
                                            border: `1px solid ${st.color}`, borderRadius: radius.md, padding: "2px 8px", fontSize: "0.68rem", cursor: "pointer" }}>
                                        {zh ? st.zh : st.en}
                                    </button>
                                ))}
                            </div>
                            <div style={{ display: "flex", gap: "4px" }}>
                                <input value={contribText} onChange={e => setContribText(e.target.value)} placeholder={zh ? "你的观点..." : "Your contribution..."}
                                    style={{ ...cnInput, flex: 1 }} onKeyDown={e => e.key === "Enter" && handleContribute()} />
                                <button style={cnActionBtn(contribBusy || !contribText.trim())} onClick={handleContribute} disabled={contribBusy}>
                                    {zh ? "发送" : "Send"}
                                </button>
                            </div>
                        </div>
                    )}
                </div>
            ))}
            {!loading && sessions.length === 0 && <div style={cnLabel}>{zh ? "暂无活跃讨论" : "No active swarms"}</div>}
        </div>
    );
}
