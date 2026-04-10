import { useState, useEffect, useCallback, useRef } from "react";
import {
    ClawNetGetDMInbox,
    ClawNetGetDMThread,
    ClawNetSendDM,
    ClawNetListTopics,
    ClawNetCreateTopic,
    ClawNetGetTopicMessages,
    ClawNetPostTopicMessage,
} from "../../../wailsjs/go/main/App";
import { colors } from "./styles";
import { cnCard, cnLabel, cnInput, cnActionBtn, cnTabStyle } from "./agentnetStyles";
import { useDialog } from "../CustomDialog";

const FAV_STORAGE_KEY = "clawnet_fav_topics";
const FAV_LIMIT = 5;

function readFavs(): string[] {
    try { return JSON.parse(localStorage.getItem(FAV_STORAGE_KEY) || "[]"); } catch { return []; }
}

const msgBodyStyle: React.CSSProperties = { color: colors.textSecondary, marginTop: "2px", whiteSpace: "pre-wrap", wordBreak: "break-word" };
const msgListStyle: React.CSSProperties = { maxHeight: "300px", overflowY: "auto", marginBottom: "8px", textAlign: "left" };
const textareaStyle: React.CSSProperties = { ...cnInput, flex: 1, resize: "vertical", fontFamily: "inherit" };
const inputBarStyle: React.CSSProperties = { display: "flex", gap: "4px", alignItems: "flex-end" };

function enterToSend(send: () => void): React.KeyboardEventHandler<HTMLTextAreaElement> {
    return e => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); send(); } };
}

const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

type Props = { lang: string; clawNetRunning: boolean };

export function ClawNetChatPanel({ lang, clawNetRunning }: Props) {
    const { showAlert } = useDialog();
    const [tab, setTab] = useState<"dm" | "topics">("dm");

    // DM state
    const [inbox, setInbox] = useState<any[]>([]);
    const [activePeer, setActivePeer] = useState<string | null>(null);
    const [thread, setThread] = useState<any[]>([]);
    const [dmText, setDmText] = useState("");
    const [dmBusy, setDmBusy] = useState(false);
    const [newPeer, setNewPeer] = useState("");

    // Topics state
    const [topics, setTopics] = useState<any[]>([]);
    const [activeTopic, setActiveTopic] = useState<string | null>(null);
    const [topicMsgs, setTopicMsgs] = useState<any[]>([]);
    const [topicText, setTopicText] = useState("");
    const [topicBusy, setTopicBusy] = useState(false);
    const [showNewTopic, setShowNewTopic] = useState(false);
    const [newTopicName, setNewTopicName] = useState("");
    const [newTopicDesc, setNewTopicDesc] = useState("");

    // Favorites state
    const [favTopics, setFavTopics] = useState<string[]>(readFavs);

    const toggleFav = useCallback((name: string) => {
        setFavTopics(prev => {
            if (prev.includes(name)) {
                const next = prev.filter(n => n !== name);
                localStorage.setItem(FAV_STORAGE_KEY, JSON.stringify(next));
                return next;
            }
            if (prev.length >= FAV_LIMIT) {
                showAlert(localizeText(lang,
                    `Favorites full (max ${FAV_LIMIT}). Remove an old one first.`,
                    `收藏已满（最多${FAV_LIMIT}个），请先去掉旧的收藏`));
                return prev;
            }
            const next = [...prev, name];
            localStorage.setItem(FAV_STORAGE_KEY, JSON.stringify(next));
            return next;
        });
    }, [lang, showAlert]);

    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const mountedRef = useRef(true);
    useEffect(() => { mountedRef.current = true; return () => { mountedRef.current = false; }; }, []);

    // DM
    const loadInbox = useCallback(async () => {
        if (!clawNetRunning) return;
        setLoading(true); setError("");
        try {
            const res = await ClawNetGetDMInbox();
            if (mountedRef.current && res.ok) setInbox(res.inbox as any[] || []);
        } catch (e: any) { if (mountedRef.current) setError(e.message); }
        if (mountedRef.current) setLoading(false);
    }, [clawNetRunning]);

    const loadThread = useCallback(async (peerID: string) => {
        try {
            const res = await ClawNetGetDMThread(peerID, 50);
            if (mountedRef.current && res.ok) setThread(res.thread as any[] || []);
        } catch {}
    }, []);

    const openPeer = (peerID: string) => { setActivePeer(peerID); loadThread(peerID); };

    const sendDM = async () => {
        const target = activePeer || newPeer.trim();
        if (!target || !dmText.trim()) return;
        setDmBusy(true);
        try {
            const res = await ClawNetSendDM(target, dmText.trim());
            if (!mountedRef.current) return;
            if (res.ok) { setDmText(""); if (activePeer) loadThread(activePeer); else { setNewPeer(""); setActivePeer(target); loadThread(target); } loadInbox(); }
        } catch {}
        if (mountedRef.current) setDmBusy(false);
    };

    // Topics
    const loadTopics = useCallback(async () => {
        if (!clawNetRunning) return;
        setLoading(true);
        try {
            const res = await ClawNetListTopics();
            if (mountedRef.current && res.ok) setTopics(res.topics as any[] || []);
        } catch {}
        if (mountedRef.current) setLoading(false);
    }, [clawNetRunning]);

    const loadTopicMsgs = async (name: string) => {
        setActiveTopic(name); setTopicMsgs([]);
        try {
            const res = await ClawNetGetTopicMessages(name);
            if (mountedRef.current && res.ok) setTopicMsgs(res.messages as any[] || []);
        } catch {}
    };

    const postToTopic = async () => {
        if (!activeTopic || !topicText.trim()) return;
        setTopicBusy(true);
        try {
            const res = await ClawNetPostTopicMessage(activeTopic, topicText.trim());
            if (!mountedRef.current) return;
            if (res.ok) { setTopicText(""); loadTopicMsgs(activeTopic); }
        } catch {}
        if (mountedRef.current) setTopicBusy(false);
    };

    const createTopic = async () => {
        if (!newTopicName.trim()) return;
        try {
            const res = await ClawNetCreateTopic(newTopicName.trim(), newTopicDesc.trim());
            if (!mountedRef.current) return;
            if (res.ok) { setNewTopicName(""); setNewTopicDesc(""); setShowNewTopic(false); loadTopics(); }
        } catch {}
    };

    useEffect(() => { if (tab === "dm") loadInbox(); else loadTopics(); }, [tab, loadInbox, loadTopics]);

    if (!clawNetRunning) return <div style={cnLabel}>{localizeText(lang, "ClawNet not connected", "智网未连接")}</div>;

    return (
        <div style={{ padding: "10px 14px" }}>
            <div style={{ display: "flex", gap: "6px", marginBottom: "10px" }}>
                <button style={cnTabStyle(tab === "dm")} onClick={() => { setTab("dm"); setActivePeer(null); }}>💬 {localizeText(lang, "DM", "私信")}</button>
                <button style={cnTabStyle(tab === "topics")} onClick={() => { setTab("topics"); setActiveTopic(null); }}>📢 {localizeText(lang, "Topics", "话题频道")}</button>
            </div>
            {error && <div style={{ fontSize: "0.72rem", color: colors.danger, marginBottom: "8px" }}>{error}</div>}

            {tab === "dm" && !activePeer && (
                <>
                    <div style={{ display: "flex", gap: "4px", marginBottom: "10px" }}>
                        <input value={newPeer} onChange={e => setNewPeer(e.target.value)} placeholder={localizeText(lang, "Enter Peer ID to start chat...", "输入 Peer ID 发起对话...")} style={{ ...cnInput, flex: 1 }} />
                        <button style={cnActionBtn(!newPeer.trim())} onClick={() => newPeer.trim() && openPeer(newPeer.trim())} disabled={!newPeer.trim()}>{localizeText(lang, "Open", "打开")}</button>
                    </div>
                    {loading && <div style={cnLabel}>{localizeText(lang, "Loading...", "加载中...")}</div>}
                    {inbox.map((m: any, i: number) => (
                        <div key={i} style={{ ...cnCard, cursor: "pointer" }} onClick={() => openPeer(m.peer_id || m.from || "")}>
                            <div style={{ display: "flex", justifyContent: "space-between" }}>
                                <span style={{ fontSize: "0.74rem", fontWeight: 600, color: colors.text }}>{(m.peer_id || m.from || "").slice(0, 16)}…</span>
                                <span style={{ fontSize: "0.65rem", color: colors.textMuted }}>{m.created_at || m.time || ""}</span>
                            </div>
                            {m.body && <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginTop: "2px", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{m.body}</div>}
                        </div>
                    ))}
                    {!loading && inbox.length === 0 && <div style={cnLabel}>{localizeText(lang, "No messages", "暂无私信")}</div>}
                </>
            )}

            {tab === "dm" && activePeer && (
                <>
                    <div style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: "8px" }}>
                        <button style={{ ...cnActionBtn(), padding: "2px 8px" }} onClick={() => setActivePeer(null)}>← {localizeText(lang, "Back", "返回")}</button>
                        <span style={{ fontSize: "0.74rem", fontFamily: "monospace", color: colors.textSecondary }}>{activePeer.slice(0, 20)}…</span>
                    </div>
                    <div style={msgListStyle}>
                        {thread.map((m: any, i: number) => (
                            <div key={i} style={{ marginBottom: "6px", fontSize: "0.72rem" }}>
                                <span style={{ color: colors.textMuted, fontSize: "0.65rem" }}>{(m.from || "").slice(0, 10)} · {m.created_at || ""}</span>
                                <div style={msgBodyStyle}>{m.body}</div>
                            </div>
                        ))}
                        {thread.length === 0 && <div style={cnLabel}>{localizeText(lang, "No messages yet", "暂无消息")}</div>}
                    </div>
                    <div style={inputBarStyle}>
                        <textarea value={dmText} onChange={e => setDmText(e.target.value)} placeholder={localizeText(lang, "Type a message...", "输入消息...")}
                            rows={3} style={textareaStyle} onKeyDown={enterToSend(sendDM)} />
                        <button style={cnActionBtn(dmBusy || !dmText.trim())} onClick={sendDM} disabled={dmBusy}>{localizeText(lang, "Send", "发送")}</button>
                    </div>
                </>
            )}

            {tab === "topics" && !activeTopic && (
                <>
                    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: "8px", gap: "6px" }}>
                        <button style={cnActionBtn()} onClick={() => setShowNewTopic(!showNewTopic)}>{showNewTopic ? localizeText(lang, "Cancel", "取消") : localizeText(lang, "New Topic", "创建频道")}</button>
                        <div style={{ flex: 1, display: "flex", gap: "4px", overflow: "hidden", flexWrap: "wrap" }}>
                            {favTopics.map(fn => (
                                <button key={fn} style={{ padding: "2px 8px", fontSize: "0.68rem", borderRadius: "999px", border: "1px solid rgba(47,128,237,.25)", background: "rgba(47,128,237,.08)", color: colors.primary || "#2c6fca", cursor: "pointer", whiteSpace: "nowrap", lineHeight: "1.6" }}
                                    onClick={() => loadTopicMsgs(fn)} title={fn}>
                                    ★ {fn.length > 10 ? fn.slice(0, 10) + "…" : fn}
                                </button>
                            ))}
                        </div>
                        <button style={cnActionBtn(loading)} onClick={loadTopics} disabled={loading}>{localizeText(lang, "Refresh", "刷新")}</button>
                    </div>
                    {showNewTopic && (
                        <div style={{ ...cnCard, background: colors.bg }}>
                            <input value={newTopicName} onChange={e => setNewTopicName(e.target.value)} placeholder={localizeText(lang, "Topic name", "频道名称")} style={{ ...cnInput, marginBottom: "6px" }} />
                            <input value={newTopicDesc} onChange={e => setNewTopicDesc(e.target.value)} placeholder={localizeText(lang, "Description (optional)", "描述（可选）")} style={{ ...cnInput, marginBottom: "6px" }} />
                            <button style={cnActionBtn(!newTopicName.trim())} onClick={createTopic} disabled={!newTopicName.trim()}>{localizeText(lang, "Create", "创建")}</button>
                        </div>
                    )}
                    {loading && <div style={cnLabel}>{localizeText(lang, "Loading...", "加载中...")}</div>}
                    {topics.map((t: any, i: number) => {
                        const topicKey = t.name || t.id || "";
                        const isFav = favTopics.includes(topicKey);
                        return (
                            <div key={i} style={{ ...cnCard, cursor: "pointer", display: "flex", alignItems: "center", gap: "6px" }}>
                                <div style={{ flex: 1, overflow: "hidden" }} onClick={() => loadTopicMsgs(topicKey)}>
                                    <div style={{ fontSize: "0.74rem", fontWeight: 600, color: colors.text }}>{t.name || t.id}</div>
                                    {t.description && <div style={{ fontSize: "0.7rem", color: colors.textMuted }}>{t.description}</div>}
                                </div>
                                <button onClick={(e) => { e.stopPropagation(); toggleFav(topicKey); }}
                                    style={{ background: "none", border: "none", cursor: "pointer", fontSize: "0.85rem", padding: "2px 4px", flexShrink: 0, color: isFav ? "#e6a817" : colors.textMuted || "#999", lineHeight: 1 }}
                                    title={isFav ? localizeText(lang, "Unfavorite", "取消收藏") : localizeText(lang, "Favorite", "收藏")}>
                                    {isFav ? "★" : "☆"}
                                </button>
                            </div>
                        );
                    })}
                    {!loading && topics.length === 0 && <div style={cnLabel}>{localizeText(lang, "No topics", "暂无频道")}</div>}
                </>
            )}

            {tab === "topics" && activeTopic && (
                <>
                    <div style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: "8px" }}>
                        <button style={{ ...cnActionBtn(), padding: "2px 8px" }} onClick={() => setActiveTopic(null)}>← {localizeText(lang, "Back", "返回")}</button>
                        <span style={{ fontSize: "0.74rem", fontWeight: 600, color: colors.text }}>#{activeTopic}</span>
                    </div>
                    <div style={msgListStyle}>
                        {topicMsgs.map((m: any, i: number) => (
                            <div key={i} style={{ marginBottom: "6px", fontSize: "0.72rem" }}>
                                <span style={{ color: colors.textMuted, fontSize: "0.65rem" }}>{(m.author || m.from || "").slice(0, 10)} · {m.created_at || ""}</span>
                                <div style={msgBodyStyle}>{m.body}</div>
                            </div>
                        ))}
                        {topicMsgs.length === 0 && <div style={cnLabel}>{localizeText(lang, "No messages", "暂无消息")}</div>}
                    </div>
                    <div style={inputBarStyle}>
                        <textarea value={topicText} onChange={e => setTopicText(e.target.value)} placeholder={localizeText(lang, "Post a message...", "发言...")}
                            rows={3} style={textareaStyle} onKeyDown={enterToSend(postToTopic)} />
                        <button style={cnActionBtn(topicBusy || !topicText.trim())} onClick={postToTopic} disabled={topicBusy}>{localizeText(lang, "Send", "发送")}</button>
                    </div>
                </>
            )}
        </div>
    );
}
