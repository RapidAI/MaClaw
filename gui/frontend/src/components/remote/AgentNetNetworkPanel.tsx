import { useCallback, useEffect, useRef, useState } from "react";
import {
    AgentNetCreateTopic,
    AgentNetGetTopicMessages,
    AgentNetListTopics,
    AgentNetPostTopicMessage,
} from "../../../wailsjs/go/main/App";
import { colors } from "./styles";
import { cnActionBtn, cnCard, cnHeading, cnInput, cnLabel } from "./agentnetStyles";

const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === "zh-Hans" ? zhHans : lang === "zh-Hant" ? zhHant : en
);

type Props = { lang: string; agentNetRunning: boolean };

type Topic = {
    name?: string;
    description?: string;
    peers?: number;
    Name?: string;
    Description?: string;
    Peers?: number;
};

type TopicMessage = {
    id?: string;
    from?: string;
    body?: string;
    created_at?: string;
    ID?: string;
    From?: string;
    Body?: string;
    CreatedAt?: string;
};

function topicName(topic: Topic): string {
    return topic.name || topic.Name || "";
}

function topicDescription(topic: Topic): string {
    return topic.description || topic.Description || "";
}

function topicPeers(topic: Topic): number | undefined {
    return topic.peers ?? topic.Peers;
}

export function AgentNetNetworkPanel({ lang, agentNetRunning }: Props) {
    const [topics, setTopics] = useState<Topic[]>([]);
    const [selectedTopic, setSelectedTopic] = useState("");
    const [messages, setMessages] = useState<TopicMessage[]>([]);
    const [loadingTopics, setLoadingTopics] = useState(false);
    const [loadingMessages, setLoadingMessages] = useState(false);
    const [error, setError] = useState("");
    const [newTopicName, setNewTopicName] = useState("");
    const [newTopicDescription, setNewTopicDescription] = useState("");
    const [newMessage, setNewMessage] = useState("");
    const [busy, setBusy] = useState(false);
    const mountedRef = useRef(true);
    const selectedTopicRef = useRef("");

    useEffect(() => {
        mountedRef.current = true;
        return () => { mountedRef.current = false; };
    }, []);

    const loadMessages = useCallback(async (topic: string) => {
        if (!agentNetRunning || !topic) return;
        setLoadingMessages(true);
        setError("");
        try {
            const res = await AgentNetGetTopicMessages(topic);
            if (!mountedRef.current) return;
            if (res.ok) {
                setMessages((res.messages as TopicMessage[]) || []);
            } else {
                setError(String(res.error || "Failed to load topic messages"));
            }
        } catch (e: any) {
            if (mountedRef.current) setError(e?.message || String(e));
        } finally {
            if (mountedRef.current) setLoadingMessages(false);
        }
    }, [agentNetRunning]);

    const loadTopics = useCallback(async () => {
        if (!agentNetRunning) return;
        setLoadingTopics(true);
        setError("");
        try {
            const res = await AgentNetListTopics();
            if (!mountedRef.current) return;
            if (res.ok) {
                const nextTopics = (res.topics as Topic[]) || [];
                setTopics(nextTopics);
                if (!selectedTopicRef.current && nextTopics.length > 0) {
                    const firstTopic = topicName(nextTopics[0]);
                    setSelectedTopic(firstTopic);
                }
            } else {
                setError(String(res.error || "Failed to load topics"));
            }
        } catch (e: any) {
            if (mountedRef.current) setError(e?.message || String(e));
        } finally {
            if (mountedRef.current) setLoadingTopics(false);
        }
    }, [agentNetRunning]);

    useEffect(() => { selectedTopicRef.current = selectedTopic; }, [selectedTopic]);
    useEffect(() => { void loadTopics(); }, [loadTopics]);
    useEffect(() => { if (selectedTopic) void loadMessages(selectedTopic); }, [selectedTopic, loadMessages]);

    const handleCreateTopic = async () => {
        const name = newTopicName.trim().toLowerCase();
        if (!name || busy) return;
        setBusy(true);
        setError("");
        try {
            const res = await AgentNetCreateTopic(name, newTopicDescription.trim());
            if (res.ok) {
                setNewTopicName("");
                setNewTopicDescription("");
                setSelectedTopic(name);
                await loadTopics();
                await loadMessages(name);
            } else {
                setError(String(res.error || "Failed to create topic"));
            }
        } catch (e: any) {
            setError(e?.message || String(e));
        } finally {
            setBusy(false);
        }
    };

    const handlePostMessage = async () => {
        const body = newMessage.trim();
        if (!selectedTopic || !body || busy) return;
        setBusy(true);
        setError("");
        try {
            const res = await AgentNetPostTopicMessage(selectedTopic, body);
            if (res.ok) {
                setNewMessage("");
                await loadMessages(selectedTopic);
            } else {
                setError(String(res.error || "Failed to post message"));
            }
        } catch (e: any) {
            setError(e?.message || String(e));
        } finally {
            setBusy(false);
        }
    };

    if (!agentNetRunning) {
        return <div style={{ padding: "14px" }}><div style={cnLabel}>{localizeText(lang, "AgentNet not connected", "AgentNet not connected")}</div></div>;
    }

    return (
        <div style={{ padding: "10px 14px", display: "grid", gridTemplateColumns: "minmax(240px, 320px) minmax(0, 1fr)", gap: "12px", minHeight: "100%" }}>
            <div style={{ minWidth: 0 }}>
                <div style={{ ...cnCard, background: colors.bg }}>
                    <div style={cnHeading}>{localizeText(lang, "Topic Rooms", "Topic Rooms")}</div>
                    <button style={cnActionBtn(loadingTopics)} onClick={loadTopics} disabled={loadingTopics}>
                        {loadingTopics ? "..." : localizeText(lang, "Refresh", "Refresh")}
                    </button>
                    {topics.length === 0 && !loadingTopics && <div style={{ ...cnLabel, marginTop: "8px" }}>{localizeText(lang, "No topics yet", "No topics yet")}</div>}
                    <div style={{ display: "flex", flexDirection: "column", gap: "6px", marginTop: "8px" }}>
                        {topics.map((topic) => {
                            const name = topicName(topic);
                            const active = selectedTopic === name;
                            return (
                                <button
                                    key={name}
                                    onClick={() => setSelectedTopic(name)}
                                    style={{
                                        textAlign: "left",
                                        border: `1px solid ${active ? colors.primary : colors.border}`,
                                        background: active ? colors.accentBg : colors.surface,
                                        borderRadius: "8px",
                                        padding: "8px 10px",
                                        cursor: "pointer",
                                        color: colors.text,
                                    }}
                                >
                                    <div style={{ fontSize: "0.76rem", fontWeight: 700 }}>{name}</div>
                                    {topicDescription(topic) && <div style={{ fontSize: "0.68rem", color: colors.textSecondary, marginTop: "3px" }}>{topicDescription(topic)}</div>}
                                    {typeof topicPeers(topic) === "number" && <div style={{ fontSize: "0.64rem", color: colors.textMuted, marginTop: "3px" }}>{topicPeers(topic)} peers</div>}
                                </button>
                            );
                        })}
                    </div>
                </div>

                <div style={{ ...cnCard, background: colors.bg }}>
                    <div style={cnHeading}>{localizeText(lang, "Create Topic", "Create Topic")}</div>
                    <input
                        value={newTopicName}
                        onChange={(e) => setNewTopicName(e.target.value.replace(/[^a-zA-Z0-9-]/g, "").toLowerCase())}
                        placeholder="topic-name"
                        style={{ ...cnInput, marginBottom: "6px" }}
                    />
                    <textarea
                        value={newTopicDescription}
                        onChange={(e) => setNewTopicDescription(e.target.value)}
                        placeholder={localizeText(lang, "Description", "Description")}
                        style={{ ...cnInput, minHeight: "62px", resize: "vertical", marginBottom: "8px" }}
                    />
                    <button style={cnActionBtn(busy || !newTopicName.trim())} disabled={busy || !newTopicName.trim()} onClick={handleCreateTopic}>
                        {busy ? "..." : localizeText(lang, "Create", "Create")}
                    </button>
                </div>
            </div>

            <div style={{ ...cnCard, minWidth: 0, marginBottom: 0, display: "flex", flexDirection: "column", background: colors.bg }}>
                <div style={{ display: "flex", justifyContent: "space-between", gap: "10px", alignItems: "center", marginBottom: "8px" }}>
                    <div>
                        <div style={cnHeading}>{selectedTopic || localizeText(lang, "Select a topic", "Select a topic")}</div>
                        <div style={cnLabel}>{localizeText(lang, "Topic rooms are shared AgentNet discussions.", "Topic rooms are shared AgentNet discussions.")}</div>
                    </div>
                    <button style={cnActionBtn(loadingMessages || !selectedTopic)} disabled={loadingMessages || !selectedTopic} onClick={() => loadMessages(selectedTopic)}>
                        {loadingMessages ? "..." : localizeText(lang, "Reload", "Reload")}
                    </button>
                </div>

                {error && <div style={{ fontSize: "0.72rem", color: colors.danger, marginBottom: "8px" }}>{error}</div>}

                <div style={{ flex: 1, overflow: "auto", border: `1px solid ${colors.border}`, borderRadius: "8px", padding: "8px", background: colors.surface, minHeight: "260px" }}>
                    {!selectedTopic && <div style={cnLabel}>{localizeText(lang, "Choose or create a topic to begin.", "Choose or create a topic to begin.")}</div>}
                    {selectedTopic && messages.length === 0 && !loadingMessages && <div style={cnLabel}>{localizeText(lang, "No messages yet", "No messages yet")}</div>}
                    {messages.map((message, index) => {
                        const id = message.id || message.ID || `${index}`;
                        const from = message.from || message.From || "agent";
                        const body = message.body || message.Body || "";
                        const createdAt = message.created_at || message.CreatedAt || "";
                        return (
                            <div key={id} style={{ padding: "8px 0", borderBottom: index === messages.length - 1 ? "none" : `1px solid ${colors.border}` }}>
                                <div style={{ display: "flex", justifyContent: "space-between", gap: "8px", marginBottom: "4px" }}>
                                    <span style={{ fontSize: "0.7rem", fontWeight: 700, color: colors.text }}>{from}</span>
                                    {createdAt && <span style={{ fontSize: "0.64rem", color: colors.textMuted }}>{createdAt}</span>}
                                </div>
                                <div style={{ fontSize: "0.74rem", color: colors.textSecondary, whiteSpace: "pre-wrap", lineHeight: 1.5 }}>{body}</div>
                            </div>
                        );
                    })}
                </div>

                <div style={{ display: "flex", gap: "8px", marginTop: "10px" }}>
                    <textarea
                        value={newMessage}
                        onChange={(e) => setNewMessage(e.target.value)}
                        placeholder={localizeText(lang, "Write a message...", "Write a message...")}
                        disabled={!selectedTopic}
                        style={{ ...cnInput, minHeight: "54px", resize: "vertical", flex: 1 }}
                        onKeyDown={(e) => {
                            if ((e.ctrlKey || e.metaKey) && e.key === "Enter") {
                                void handlePostMessage();
                            }
                        }}
                    />
                    <button style={cnActionBtn(busy || !selectedTopic || !newMessage.trim())} disabled={busy || !selectedTopic || !newMessage.trim()} onClick={handlePostMessage}>
                        {busy ? "..." : localizeText(lang, "Send", "Send")}
                    </button>
                </div>
            </div>
        </div>
    );
}
