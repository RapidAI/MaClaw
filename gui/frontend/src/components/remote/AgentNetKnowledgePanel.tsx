import { useState, useEffect, useCallback, useRef } from "react";
import {
    AgentNetGetKnowledgeFeed,
    AgentNetSearchKnowledge,
    AgentNetPublishKnowledgeFull,
    AgentNetReactKnowledge,
    AgentNetReplyKnowledge,
    AgentNetGetKnowledgeReplies,
} from "../../../wailsjs/go/main/App";
import { colors, radius } from "./styles";
import { cnCard, cnLabel, cnInput, cnActionBtn, cnTabStyle } from "./agentnetStyles";

const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

type Props = { lang: string; agentNetRunning: boolean };

interface KnowledgeEntry {
    id: string; title: string; body?: string; author?: string;
    domains?: string[]; tags?: string[]; reactions?: Record<string, number>;
    created_at?: string;
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
    const [expandedId, setExpandedId] = useState<string | null>(null);
    const [replies, setReplies] = useState<any[]>([]);
    const [replyText, setReplyText] = useState("");
    const [replyBusy, setReplyBusy] = useState(false);
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

    // Fix: refresh current view (feed or search) after reacting, not always loadFeed
    const refreshCurrentView = useCallback(() => {
        if (tab === "search" && query.trim()) doSearch();
        else loadFeed();
    }, [tab, query, doSearch, loadFeed]);

    const handleReact = async (id: string, reaction: string) => {
        try { await AgentNetReactKnowledge(id, reaction); refreshCurrentView(); } catch {}
    };

    const toggleReplies = async (id: string) => {
        if (expandedId === id) { setExpandedId(null); return; }
        setExpandedId(id); setReplies([]); setReplyText("");
        try {
            const res = await AgentNetGetKnowledgeReplies(id);
            if (res.ok) setReplies(res.replies as any[] || []);
        } catch {}
    };

    const handleReply = async () => {
        if (!expandedId || !replyText.trim()) return;
        setReplyBusy(true);
        try {
            const res = await AgentNetReplyKnowledge(expandedId, replyText.trim());
            if (res.ok) { setReplyText(""); toggleReplies(expandedId); }
        } catch {}
        setReplyBusy(false);
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

            {(tab === "feed" || tab === "search") && entries.map(e => (
                <div key={e.id} style={cnCard}>
                    <div style={{ fontSize: "0.76rem", fontWeight: 600, color: colors.text, marginBottom: "4px" }}>{e.title}</div>
                    {e.body && <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginBottom: "6px", whiteSpace: "pre-wrap", maxHeight: "120px", overflow: "auto" }}>{e.body}</div>}
                    <div style={{ display: "flex", gap: "8px", alignItems: "center", flexWrap: "wrap" }}>
                        {e.author && <span style={{ fontSize: "0.68rem", color: colors.textMuted }}>{e.author.slice(0, 12)}…</span>}
                        {e.domains?.map(d => <span key={d} style={{ fontSize: "0.65rem", padding: "1px 6px", background: colors.accentBg, borderRadius: radius.pill, color: colors.textSecondary }}>{d}</span>)}
                        {e.created_at && <span style={{ fontSize: "0.65rem", color: colors.textMuted }}>{e.created_at}</span>}
                    </div>
                    <div style={{ display: "flex", gap: "6px", marginTop: "6px", alignItems: "center" }}>
                        {["👍", "🔥", "💡"].map(r => (
                            <button key={r} onClick={() => handleReact(e.id, r)} style={{ background: "none", border: "none", cursor: "pointer", fontSize: "0.8rem", padding: "2px 4px" }}>
                                {r} {e.reactions?.[r] || ""}
                            </button>
                        ))}
                        <button onClick={() => toggleReplies(e.id)} style={{ ...cnActionBtn(), padding: "2px 8px", fontSize: "0.68rem" }}>
                            {localizeText(lang, "Replies", "回复")}
                        </button>
                    </div>
                    {expandedId === e.id && (
                        <div style={{ marginTop: "8px", paddingTop: "6px", borderTop: `1px solid ${colors.border}` }}>
                            {replies.map((r: any, i: number) => (
                                <div key={i} style={{ fontSize: "0.7rem", color: colors.textSecondary, marginBottom: "4px" }}>
                                    <span style={{ color: colors.textMuted }}>{(r.author || "").slice(0, 10)}:</span> {r.body}
                                </div>
                            ))}
                            <div style={{ display: "flex", gap: "4px", marginTop: "4px" }}>
                                <input value={replyText} onChange={e => setReplyText(e.target.value)} placeholder={localizeText(lang, "Reply...", "回复...")}
                                    style={{ ...cnInput, flex: 1 }} onKeyDown={e => e.key === "Enter" && handleReply()} />
                                <button style={cnActionBtn(replyBusy || !replyText.trim())} onClick={handleReply} disabled={replyBusy}>
                                    {localizeText(lang, "Send", "发送")}
                                </button>
                            </div>
                        </div>
                    )}
                </div>
            ))}
            {(tab === "feed" || tab === "search") && !loading && entries.length === 0 && (
                <div style={cnLabel}>{localizeText(lang, "No entries yet", "暂无内容")}</div>
            )}
        </div>
    );
}
