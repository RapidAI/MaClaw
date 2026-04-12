import { useState, useEffect, useCallback, useRef } from "react";
import {
    AgentNetGetKnowledgeFeed,
    AgentNetSearchKnowledge,
    AgentNetPublishKnowledgeFull,
} from "../../../wailsjs/go/main/App";
import { colors, radius } from "./styles";
import { cnCard, cnLabel, cnInput, cnActionBtn, cnTabStyle } from "./agentnetStyles";

const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

type Props = { lang: string; agentNetRunning: boolean };

interface KnowledgeEntry {
    id: string; title?: string; intent?: string; body?: string; author?: string;
    peer_id?: string; source_id?: string;
    domains?: string[]; tags?: string[]; skills?: string;
    node_type?: string; node_count?: number;
    created_at?: string;
    // Wails uppercase variants
    Title?: string; Intent?: string; Body?: string; Author?: string;
    PeerID?: string; SourceID?: string; Skills?: string;
    NodeType?: string; NodeCount?: number;
    CreatedAt?: string; DisplayTitle?: string;
}

export function AgentNetKnowledgePanel({ lang, agentNetRunning }: Props) {
    const [tab, setTab] = useState<"feed" | "search" | "publish">("feed");
    const [entries, setEntries] = useState<KnowledgeEntry[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [query, setQuery] = useState("");
    const [pubTitle, setPubTitle] = useState("");
    const [pubBody, setPubBody] = useState("");
    const [pubTags, setPubTags] = useState("");
    const [pubBusy, setPubBusy] = useState(false);
    const [pubMsg, setPubMsg] = useState("");
    const mountedRef = useRef(true);
    useEffect(() => { mountedRef.current = true; return () => { mountedRef.current = false; }; }, []);

    const loadFeed = useCallback(async () => {
        if (!agentNetRunning) return;
        setLoading(true); setError("");
        try {
            const res = await AgentNetGetKnowledgeFeed("", 30);
            if (mountedRef.current && res.ok) setEntries((res.entries as KnowledgeEntry[]) || []);
            else if (mountedRef.current && res.error) setError(res.error as string);
        } catch (e: any) { if (mountedRef.current) setError(e.message); }
        if (mountedRef.current) setLoading(false);
    }, [agentNetRunning]);

    const doSearch = useCallback(async () => {
        if (!agentNetRunning || !query.trim()) return;
        setLoading(true); setError("");
        try {
            const res = await AgentNetSearchKnowledge(query.trim());
            if (mountedRef.current && res.ok) setEntries((res.entries as KnowledgeEntry[]) || []);
            else if (mountedRef.current && res.error) setError(res.error as string);
        } catch (e: any) { if (mountedRef.current) setError(e.message); }
        if (mountedRef.current) setLoading(false);
    }, [agentNetRunning, query]);

    useEffect(() => { if (tab === "feed") loadFeed(); }, [tab, loadFeed]);

    const handlePublish = async () => {
        if (!pubTitle.trim() || !pubBody.trim()) return;
        setPubBusy(true); setPubMsg("");
        try {
            const tags = pubTags.split(",").map(s => s.trim()).filter(Boolean);
            const res = await AgentNetPublishKnowledgeFull(pubTitle.trim(), pubBody.trim(), tags);
            if (res.ok) { setPubMsg(localizeText(lang, "✅ Published", "✅ 发布成功")); setPubTitle(""); setPubBody(""); setPubTags(""); }
            else setPubMsg(`❌ ${res.error}`);
        } catch (e: any) { setPubMsg(`❌ ${e.message}`); }
        setPubBusy(false);
    };

    if (!agentNetRunning) return <div style={cnLabel}>{localizeText(lang, "AgentNet not connected", "智网未连接")}</div>;

    return (
        <div style={{ padding: "10px 14px" }}>
            <div style={{ display: "flex", gap: "6px", marginBottom: "10px" }}>
                <button style={cnTabStyle(tab === "feed")} onClick={() => setTab("feed")}>{localizeText(lang, "Feed", "知识流")}</button>
                <button style={cnTabStyle(tab === "search")} onClick={() => setTab("search")}>{localizeText(lang, "Search", "搜索")}</button>
                <button style={cnTabStyle(tab === "publish")} onClick={() => setTab("publish")}>{localizeText(lang, "Publish", "发布")}</button>
            </div>

            {tab === "search" && (
                <div style={{ display: "flex", gap: "6px", marginBottom: "10px" }}>
                    <input value={query} onChange={e => setQuery(e.target.value)} placeholder={localizeText(lang, "Search knowledge...", "搜索知识...")}
                        style={{ ...cnInput, flex: 1 }} onKeyDown={e => e.key === "Enter" && doSearch()} />
                    <button style={cnActionBtn(loading || !query.trim())} onClick={doSearch} disabled={loading || !query.trim()}>
                        {loading ? "..." : localizeText(lang, "Search", "搜索")}
                    </button>
                </div>
            )}

            {tab === "publish" && (
                <div style={{ ...cnCard, background: colors.bg }}>
                    <input value={pubTitle} onChange={e => setPubTitle(e.target.value)} placeholder={localizeText(lang, "Title", "标题")} style={{ ...cnInput, marginBottom: "6px" }} />
                    <textarea value={pubBody} onChange={e => setPubBody(e.target.value)} placeholder={localizeText(lang, "Body (Markdown supported)", "内容（支持 Markdown）")}
                        style={{ ...cnInput, minHeight: "80px", resize: "vertical", marginBottom: "6px" }} />
                    <input value={pubTags} onChange={e => setPubTags(e.target.value)} placeholder={localizeText(lang, "Tags (comma separated)", "标签（逗号分隔）")} style={{ ...cnInput, marginBottom: "8px" }} />
                    <button style={cnActionBtn(pubBusy || !pubTitle.trim() || !pubBody.trim())} onClick={handlePublish} disabled={pubBusy || !pubTitle.trim() || !pubBody.trim()}>
                        {pubBusy ? "..." : localizeText(lang, "Publish", "发布知识")}
                    </button>
                    {pubMsg && <div style={{ fontSize: "0.72rem", marginTop: "6px", color: pubMsg.startsWith("✅") ? colors.success : colors.danger }}>{pubMsg}</div>}
                </div>
            )}

            {error && <div style={{ fontSize: "0.72rem", color: colors.danger, marginBottom: "8px" }}>{error}</div>}
            {(tab === "feed" || tab === "search") && loading && <div style={cnLabel}>{localizeText(lang, "Loading...", "加载中...")}</div>}

            {(tab === "feed" || tab === "search") && entries.map(e => {
                const title = e.DisplayTitle || e.title || e.Title || e.intent || e.Intent || e.id || "—";
                const body = e.body || e.Body || "";
                const author = e.author || e.Author || e.peer_id || e.PeerID || e.source_id || e.SourceID || "";
                const skills = e.skills || e.Skills || "";
                const createdAt = e.created_at || e.CreatedAt || "";
                return (
                <div key={e.id} style={cnCard}>
                    <div style={{ fontSize: "0.76rem", fontWeight: 600, color: colors.text, marginBottom: "4px" }}>{title}</div>
                    {body && <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginBottom: "6px", whiteSpace: "pre-wrap", maxHeight: "120px", overflow: "auto" }}>{body}</div>}
                    <div style={{ display: "flex", gap: "8px", alignItems: "center", flexWrap: "wrap" }}>
                        {author && <span style={{ fontSize: "0.68rem", color: colors.textMuted }}>{author.slice(0, 20)}{author.length > 20 ? "…" : ""}</span>}
                        {skills && skills.split(",").map(s => s.trim()).filter(Boolean).map(s => <span key={s} style={{ fontSize: "0.65rem", padding: "1px 6px", background: colors.accentBg, borderRadius: radius.pill, color: colors.textSecondary }}>{s}</span>)}
                        {e.domains?.map(d => <span key={d} style={{ fontSize: "0.65rem", padding: "1px 6px", background: colors.accentBg, borderRadius: radius.pill, color: colors.textSecondary }}>{d}</span>)}
                        {createdAt && <span style={{ fontSize: "0.65rem", color: colors.textMuted }}>{createdAt}</span>}
                    </div>
                </div>
                );
            })}
            {(tab === "feed" || tab === "search") && !loading && entries.length === 0 && (
                <div style={cnLabel}>{localizeText(lang, "No entries yet", "暂无内容")}</div>
            )}
        </div>
    );
}
