import { useState, useEffect, useCallback, useRef } from "react";
import {
    AgentNetListPredictions,
    AgentNetCreatePrediction,
    AgentNetPlaceBet,
    AgentNetResolvePrediction,
    AgentNetAppealPrediction,
    AgentNetGetPredictionLeaderboard,
} from "../../../wailsjs/go/main/App";
import { colors, radius } from "./styles";
import { cnCard, cnLabel, cnHeading, cnInput, cnActionBtn, cnTabStyle } from "./agentnetStyles";

const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

type Props = { lang: string; agentNetRunning: boolean };

interface Prediction {
    id: string;
    question: string;
    options: string[];
    status: string;
    creator: string;
    created_at: string;
    resolved_option?: string;
}

export function AgentNetPredictionPanel({ lang, agentNetRunning }: Props) {
    const [tab, setTab] = useState<"market" | "create" | "leaderboard">("market");
    const [preds, setPreds] = useState<Prediction[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [msg, setMsg] = useState("");
    const [actionBusy, setActionBusy] = useState("");

    // Create form
    const [newQuestion, setNewQuestion] = useState("");
    const [newOptions, setNewOptions] = useState("yes, no");

    // Bet form
    const [betPredId, setBetPredId] = useState<string | null>(null);
    const [betOption, setBetOption] = useState("");
    const [betAmount, setBetAmount] = useState(10);

    // Leaderboard
    const [leaderboard, setLeaderboard] = useState<any[]>([]);

    const mountedRef = useRef(true);
    const msgTimerRef = useRef<ReturnType<typeof setTimeout>>();
    useEffect(() => { mountedRef.current = true; return () => { mountedRef.current = false; clearTimeout(msgTimerRef.current); }; }, []);

    const showMsg = (m: string, dur = 4000) => {
        setMsg(m);
        clearTimeout(msgTimerRef.current);
        msgTimerRef.current = setTimeout(() => { if (mountedRef.current) setMsg(""); }, dur);
    };

    const loadPredictions = useCallback(async () => {
        if (!agentNetRunning) return;
        setLoading(true); setError("");
        try {
            const res = await AgentNetListPredictions();
            if (mountedRef.current && res.ok) setPreds(res.predictions as Prediction[] || []);
        } catch (e: any) { if (mountedRef.current) setError(e.message); }
        if (mountedRef.current) setLoading(false);
    }, [agentNetRunning]);

    const loadLeaderboard = useCallback(async () => {
        if (!agentNetRunning) return;
        setLoading(true);
        try {
            const res = await AgentNetGetPredictionLeaderboard();
            if (mountedRef.current && res.ok) setLeaderboard(res.leaderboard as any[] || []);
        } catch {}
        if (mountedRef.current) setLoading(false);
    }, [agentNetRunning]);

    useEffect(() => {
        if (tab === "market") loadPredictions();
        else if (tab === "leaderboard") loadLeaderboard();
    }, [tab, loadPredictions, loadLeaderboard]);

    const handleCreate = async () => {
        if (!newQuestion.trim()) return;
        const opts = newOptions.split(",").map(s => s.trim()).filter(Boolean);
        if (opts.length < 2) { showMsg(localizeText(lang, "Need at least 2 options", "至少需要2个选项")); return; }
        setActionBusy("create");
        try {
            const res = await AgentNetCreatePrediction(newQuestion.trim(), opts);
            if (!mountedRef.current) return;
            if (res.ok) { setNewQuestion(""); setNewOptions("yes, no"); showMsg(localizeText(lang, "✅ Prediction created", "✅ 预测已创建")); setTab("market"); loadPredictions(); }
            else showMsg(`❌ ${res.error}`);
        } catch (e: any) { showMsg(`❌ ${e.message}`); }
        if (mountedRef.current) setActionBusy("");
    };

    const handleBet = async () => {
        if (!betPredId || !betOption || betAmount <= 0) return;
        setActionBusy("bet-" + betPredId);
        try {
            const res = await AgentNetPlaceBet(betPredId, betOption, betAmount);
            if (!mountedRef.current) return;
            if (res.ok) { showMsg(localizeText(lang, "✅ Bet placed", "✅ 下注成功")); setBetPredId(null); loadPredictions(); }
            else showMsg(`❌ ${res.error}`);
        } catch (e: any) { showMsg(`❌ ${e.message}`); }
        if (mountedRef.current) setActionBusy("");
    };

    const handleResolve = async (predId: string, option: string) => {
        setActionBusy("resolve-" + predId);
        try {
            const res = await AgentNetResolvePrediction(predId, option);
            if (!mountedRef.current) return;
            if (res.ok) { showMsg(localizeText(lang, "✅ Resolved", "✅ 已结算")); loadPredictions(); }
            else showMsg(`❌ ${res.error}`);
        } catch (e: any) { showMsg(`❌ ${e.message}`); }
        if (mountedRef.current) setActionBusy("");
    };

    const handleAppeal = async (predId: string) => {
        const reason = prompt(localizeText(lang, "Appeal reason:", "申诉理由："));
        if (!reason) return;
        setActionBusy("appeal-" + predId);
        try {
            const res = await AgentNetAppealPrediction(predId, reason);
            if (!mountedRef.current) return;
            if (res.ok) showMsg(localizeText(lang, "✅ Appeal submitted", "✅ 申诉已提交"));
            else showMsg(`❌ ${res.error}`);
        } catch (e: any) { showMsg(`❌ ${e.message}`); }
        if (mountedRef.current) setActionBusy("");
    };

    if (!agentNetRunning) return <div style={cnLabel}>{localizeText(lang, "AgentNet not connected", "智网未连接")}</div>;

    return (
        <div style={{ padding: "10px 14px" }}>
            <div style={{ display: "flex", gap: "6px", marginBottom: "10px" }}>
                <button style={cnTabStyle(tab === "market")} onClick={() => setTab("market")}>🔮 {localizeText(lang, "Market", "市场")}</button>
                <button style={cnTabStyle(tab === "create")} onClick={() => setTab("create")}>➕ {localizeText(lang, "Create", "创建")}</button>
                <button style={cnTabStyle(tab === "leaderboard")} onClick={() => setTab("leaderboard")}>🏆 {localizeText(lang, "Leaderboard", "排行榜")}</button>
            </div>
            {msg && <div style={{ fontSize: "0.72rem", marginBottom: "8px", color: msg.startsWith("✅") ? colors.success : colors.danger }}>{msg}</div>}
            {error && <div style={{ fontSize: "0.72rem", color: colors.danger, marginBottom: "8px" }}>{error}</div>}

            {tab === "market" && (
                <>
                    <div style={{ display: "flex", justifyContent: "flex-end", marginBottom: "8px" }}>
                        <button style={cnActionBtn(loading)} onClick={loadPredictions} disabled={loading}>{localizeText(lang, "Refresh", "刷新")}</button>
                    </div>
                    {loading && <div style={cnLabel}>{localizeText(lang, "Loading...", "加载中...")}</div>}
                    {preds.map((p) => (
                        <div key={p.id} style={cnCard}>
                            <div style={{ fontSize: "0.76rem", fontWeight: 600, color: colors.text }}>{p.question}</div>
                            <div style={{ display: "flex", gap: "6px", marginTop: "6px", flexWrap: "wrap" }}>
                                {(p.options || []).map((opt) => (
                                    <span key={opt} style={{
                                        fontSize: "0.68rem", padding: "2px 8px",
                                        background: p.resolved_option === opt ? colors.success + "33" : colors.accentBg,
                                        borderRadius: radius.pill, color: p.resolved_option === opt ? colors.success : colors.textSecondary,
                                        border: p.resolved_option === opt ? `1px solid ${colors.success}` : "none",
                                    }}>{opt}</span>
                                ))}
                            </div>
                            <div style={{ fontSize: "0.65rem", color: colors.textMuted, marginTop: "4px" }}>
                                {p.status} · {(p.creator || "").slice(0, 12)}… · {p.created_at || ""}
                            </div>
                            {p.status === "open" && (
                                <div style={{ display: "flex", gap: "4px", marginTop: "6px" }}>
                                    {betPredId === p.id ? (
                                        <>
                                            <select value={betOption} onChange={e => setBetOption(e.target.value)}
                                                style={{ ...cnInput, width: "auto", flex: 1 }}>
                                                <option value="">{localizeText(lang, "Pick...", "选择...")}</option>
                                                {(p.options || []).map(o => <option key={o} value={o}>{o}</option>)}
                                            </select>
                                            <input type="number" value={betAmount} onChange={e => setBetAmount(Number(e.target.value))}
                                                style={{ ...cnInput, width: "60px" }} min={1} />
                                            <button style={cnActionBtn(!!actionBusy || !betOption)} onClick={handleBet}
                                                disabled={!!actionBusy || !betOption}>🐚</button>
                                            <button style={cnActionBtn()} onClick={() => setBetPredId(null)}>✕</button>
                                        </>
                                    ) : (
                                        <button style={cnActionBtn(!!actionBusy)} onClick={() => { setBetPredId(p.id); setBetOption(""); }}
                                            disabled={!!actionBusy}>{localizeText(lang, "Bet", "下注")}</button>
                                    )}
                                </div>
                            )}
                            {p.status === "resolved" && (
                                <button style={{ ...cnActionBtn(!!actionBusy), marginTop: "6px" }}
                                    onClick={() => handleAppeal(p.id)} disabled={!!actionBusy}>
                                    {localizeText(lang, "Appeal", "申诉")}
                                </button>
                            )}
                            {p.status === "open" && (
                                <div style={{ display: "flex", gap: "4px", marginTop: "4px" }}>
                                    {(p.options || []).map(opt => (
                                        <button key={opt} style={{ ...cnActionBtn(!!actionBusy), fontSize: "0.65rem" }}
                                            onClick={() => handleResolve(p.id, opt)} disabled={!!actionBusy}>
                                            {localizeText(lang, `Resolve→${opt}`, `结算→${opt}`)}
                                        </button>
                                    ))}
                                </div>
                            )}
                        </div>
                    ))}
                    {!loading && preds.length === 0 && <div style={cnLabel}>{localizeText(lang, "No predictions", "暂无预测")}</div>}
                </>
            )}

            {tab === "create" && (
                <div style={cnCard}>
                    <div style={cnHeading}>🔮 {localizeText(lang, "Create Prediction", "创建预测")}</div>
                    <div style={{ marginBottom: "6px" }}>
                        <div style={cnLabel}>{localizeText(lang, "Question", "问题")}</div>
                        <input value={newQuestion} onChange={e => setNewQuestion(e.target.value)}
                            placeholder={localizeText(lang, "What will happen?", "会发生什么？")} style={cnInput} />
                    </div>
                    <div style={{ marginBottom: "8px" }}>
                        <div style={cnLabel}>{localizeText(lang, "Options (comma separated)", "选项（逗号分隔）")}</div>
                        <input value={newOptions} onChange={e => setNewOptions(e.target.value)}
                            placeholder="yes, no" style={cnInput} />
                    </div>
                    <button style={cnActionBtn(!!actionBusy || !newQuestion.trim())} onClick={handleCreate}
                        disabled={!!actionBusy || !newQuestion.trim()}>
                        {actionBusy === "create" ? "..." : localizeText(lang, "Create", "创建")}
                    </button>
                </div>
            )}

            {tab === "leaderboard" && (
                <>
                    <div style={{ display: "flex", justifyContent: "flex-end", marginBottom: "8px" }}>
                        <button style={cnActionBtn(loading)} onClick={loadLeaderboard} disabled={loading}>{localizeText(lang, "Refresh", "刷新")}</button>
                    </div>
                    {loading && <div style={cnLabel}>{localizeText(lang, "Loading...", "加载中...")}</div>}
                    {leaderboard.map((entry: any, i: number) => (
                        <div key={i} style={cnCard}>
                            <div style={{ display: "flex", justifyContent: "space-between" }}>
                                <span style={{ fontSize: "0.74rem", fontWeight: 600, color: colors.text }}>
                                    #{i + 1} {((entry.peer_id || entry.name || "") as string).slice(0, 16)}…
                                </span>
                                <span style={{ fontSize: "0.72rem", color: colors.primary }}>
                                    🐚 {entry.earnings ?? entry.score ?? 0}
                                </span>
                            </div>
                            {entry.wins !== undefined && (
                                <div style={{ fontSize: "0.65rem", color: colors.textMuted, marginTop: "2px" }}>
                                    {localizeText(lang, `W: ${entry.wins}  L: ${entry.losses}`, `胜: ${entry.wins}  负: ${entry.losses}`)}
                                </div>
                            )}
                        </div>
                    ))}
                    {!loading && leaderboard.length === 0 && <div style={cnLabel}>{localizeText(lang, "No data", "暂无数据")}</div>}
                </>
            )}
        </div>
    );
}
