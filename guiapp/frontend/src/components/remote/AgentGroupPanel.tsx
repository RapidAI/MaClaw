import { useCallback, useEffect, useState } from "react";
import { localizeText } from "../../i18n";
import {
    StartAgentGroupRun,
    ListAgentGroupRuns,
    ListDesktopBots,
} from "../../../wailsjs/go/main/App";
import { colors, radius } from "./styles";

type Run = {
    id: string;
    goal: string;
    status: string;
    summary?: string;
    progress?: string[];
    created_at?: string;
};

/** Auto-teaming: plan → pick workers by capability → parallel → one summary. */
export function AgentGroupPanel({ lang }: { lang: string }) {
    const t = useCallback((en: string, zh: string) => localizeText(lang, en, zh, zh), [lang]);
    const [goal, setGoal] = useState("");
    const [runs, setRuns] = useState<Run[]>([]);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const [latest, setLatest] = useState<Run | null>(null);

    const refresh = useCallback(async () => {
        try {
            const list = await ListAgentGroupRuns();
            setRuns(Array.isArray(list) ? list : []);
        } catch (e: any) {
            setError(String(e?.message || e || "list failed"));
        }
    }, []);

    useEffect(() => { void refresh(); }, [refresh]);

    const start = async () => {
        const g = goal.trim();
        if (!g) return;
        setBusy(true);
        setError("");
        try {
            let bots: any[] = [];
            try {
                const local = await ListDesktopBots();
                bots = (Array.isArray(local) ? local : []).map((b: any) => ({
                    id: String(b.id || ""),
                    name: String(b.title || b.name || b.id || ""),
                    role: "worker",
                    capabilities: Array.isArray(b.capabilities) ? b.capabilities : ["search", "writing", "browser", "coding"],
                })).filter((b) => b.id);
            } catch { /* offline demo uses empty → backend errors clearly */ }
            const run = await StartAgentGroupRun(g, "", bots);
            setLatest(run as Run);
            setGoal("");
            await refresh();
        } catch (e: any) {
            setError(String(e?.message || e || "start failed"));
        } finally {
            setBusy(false);
        }
    };

    return (
        <div style={{ display: "flex", flexDirection: "column", gap: 12, padding: 16 }}>
            <div style={{ fontWeight: 600 }}>{t("Agent Groups", "智能体编组")}</div>
            <div style={{ color: colors.textSecondary, fontSize: 13 }}>
                {t(
                    "Router plans sub-tasks, picks workers by capability, runs in parallel, and returns one summary. Workers never talk to you directly.",
                    "路由将任务拆成子任务，按能力挑选工人机器人并行执行，再汇总成一条回复。工人机器人不直接对用户说话。",
                )}
            </div>
            <textarea
                value={goal}
                onChange={(e) => setGoal(e.target.value)}
                rows={3}
                placeholder={t("e.g. search papers; write a summary", "例如：搜索论文；写一份摘要")}
                style={{ width: "100%", borderRadius: radius.md, padding: 8, border: `1px solid ${colors.border}` }}
            />
            <button disabled={busy || !goal.trim()} onClick={() => void start()} style={{ alignSelf: "flex-start", padding: "6px 12px", borderRadius: radius.md }}>
                {busy ? t("Running…", "执行中…") : t("Start teaming", "开始编组协作")}
            </button>
            {error ? <div style={{ color: colors.danger }}>{error}</div> : null}
            {latest ? (
                <div style={{ border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: 12 }}>
                    <div style={{ fontWeight: 600 }}>{t("Latest summary", "最新汇总")}</div>
                    <div style={{ marginTop: 6 }}>{latest.summary}</div>
                    <div style={{ marginTop: 8, fontSize: 12, color: colors.textSecondary }}>
                        {(latest.progress || []).map((line, i) => <div key={i}>{line}</div>)}
                    </div>
                </div>
            ) : null}
            <div style={{ fontWeight: 600 }}>{t("Recent runs", "最近运行")}</div>
            <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
                {runs.map((r) => (
                    <div key={r.id} style={{ border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: 10 }}>
                        <div style={{ fontSize: 12, color: colors.textSecondary }}>{r.status} · {r.created_at}</div>
                        <div>{r.goal}</div>
                        <div style={{ marginTop: 4, fontSize: 13 }}>{r.summary}</div>
                    </div>
                ))}
                {runs.length === 0 ? <div style={{ color: colors.textSecondary }}>{t("No runs yet", "暂无运行记录")}</div> : null}
            </div>
        </div>
    );
}
