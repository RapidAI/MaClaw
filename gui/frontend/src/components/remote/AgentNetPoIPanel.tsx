import { useCallback, useEffect, useRef, useState } from "react";
import {
    AgentNetGetPoIScores,
    AgentNetListPoIChallenges,
    AgentNetRespondToPoI,
} from "../../../wailsjs/go/main/App";
import { colors } from "./styles";
import { cnActionBtn, cnCard, cnHeading, cnInput, cnLabel, cnTabStyle } from "./agentnetStyles";

const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === "zh-Hans" ? zhHans : lang === "zh-Hant" ? zhHant : en
);

type Props = { lang: string; agentNetRunning: boolean };

type AnyRecord = Record<string, any>;

function firstString(item: AnyRecord, keys: string[], fallback = ""): string {
    for (const key of keys) {
        const value = item[key];
        if (typeof value === "string" && value.trim()) return value;
    }
    return fallback;
}

function firstNumber(item: AnyRecord, keys: string[]): number | undefined {
    for (const key of keys) {
        const value = item[key];
        if (typeof value === "number") return value;
        if (typeof value === "string" && value.trim() && !Number.isNaN(Number(value))) return Number(value);
    }
    return undefined;
}

function challengeID(challenge: AnyRecord, index: number): string {
    return firstString(challenge, ["id", "ID", "challenge_id", "ChallengeID"], `challenge-${index}`);
}

export function AgentNetPoIPanel({ lang, agentNetRunning }: Props) {
    const [tab, setTab] = useState<"challenges" | "scores">("challenges");
    const [challenges, setChallenges] = useState<AnyRecord[]>([]);
    const [scores, setScores] = useState<AnyRecord[]>([]);
    const [selectedID, setSelectedID] = useState("");
    const [responses, setResponses] = useState<Record<string, string>>({});
    const [loading, setLoading] = useState(false);
    const [submittingID, setSubmittingID] = useState("");
    const [error, setError] = useState("");
    const [message, setMessage] = useState("");
    const mountedRef = useRef(true);
    const selectedIDRef = useRef("");

    useEffect(() => {
        mountedRef.current = true;
        return () => { mountedRef.current = false; };
    }, []);

    const loadChallenges = useCallback(async () => {
        if (!agentNetRunning) return;
        setLoading(true);
        setError("");
        setMessage("");
        try {
            const res = await AgentNetListPoIChallenges();
            if (!mountedRef.current) return;
            if (res.ok) {
                const next = (res.challenges as AnyRecord[]) || [];
                setChallenges(next);
                if (!selectedIDRef.current && next.length > 0) setSelectedID(challengeID(next[0], 0));
            } else {
                setError(String(res.error || "Failed to load PoI challenges"));
            }
        } catch (e: any) {
            if (mountedRef.current) setError(e?.message || String(e));
        } finally {
            if (mountedRef.current) setLoading(false);
        }
    }, [agentNetRunning]);

    const loadScores = useCallback(async () => {
        if (!agentNetRunning) return;
        setLoading(true);
        setError("");
        setMessage("");
        try {
            const res = await AgentNetGetPoIScores();
            if (!mountedRef.current) return;
            if (res.ok) {
                setScores((res.scores as AnyRecord[]) || []);
            } else {
                setError(String(res.error || "Failed to load PoI scores"));
            }
        } catch (e: any) {
            if (mountedRef.current) setError(e?.message || String(e));
        } finally {
            if (mountedRef.current) setLoading(false);
        }
    }, [agentNetRunning]);

    useEffect(() => { selectedIDRef.current = selectedID; }, [selectedID]);

    useEffect(() => {
        if (tab === "challenges") void loadChallenges();
        if (tab === "scores") void loadScores();
    }, [tab, loadChallenges, loadScores]);

    const submitResponse = async (id: string) => {
        const response = (responses[id] || "").trim();
        if (!response || submittingID) return;
        setSubmittingID(id);
        setError("");
        setMessage("");
        try {
            const res = await AgentNetRespondToPoI(id, response);
            if (res.ok) {
                setResponses(prev => ({ ...prev, [id]: "" }));
                setMessage(localizeText(lang, "Response submitted", "Response submitted"));
            } else {
                setError(String(res.error || "Failed to submit response"));
            }
        } catch (e: any) {
            setError(e?.message || String(e));
        } finally {
            setSubmittingID("");
        }
    };

    if (!agentNetRunning) {
        return <div style={{ padding: "14px" }}><div style={cnLabel}>{localizeText(lang, "AgentNet not connected", "AgentNet not connected")}</div></div>;
    }

    return (
        <div style={{ padding: "10px 14px" }}>
            <div style={{ display: "flex", gap: "6px", marginBottom: "10px", flexWrap: "wrap" }}>
                <button style={cnTabStyle(tab === "challenges")} onClick={() => setTab("challenges")}>{localizeText(lang, "Challenges", "Challenges")}</button>
                <button style={cnTabStyle(tab === "scores")} onClick={() => setTab("scores")}>{localizeText(lang, "Scores", "Scores")}</button>
                <button style={cnActionBtn(loading)} disabled={loading} onClick={() => tab === "challenges" ? loadChallenges() : loadScores()}>
                    {loading ? "..." : localizeText(lang, "Refresh", "Refresh")}
                </button>
            </div>

            {error && <div style={{ fontSize: "0.72rem", color: colors.danger, marginBottom: "8px" }}>{error}</div>}
            {message && <div style={{ fontSize: "0.72rem", color: colors.success, marginBottom: "8px" }}>{message}</div>}

            {tab === "challenges" && (
                <div style={{ display: "grid", gridTemplateColumns: "minmax(240px, 320px) minmax(0, 1fr)", gap: "12px" }}>
                    <div style={{ ...cnCard, background: colors.bg }}>
                        <div style={cnHeading}>{localizeText(lang, "Available Challenges", "Available Challenges")}</div>
                        {loading && <div style={cnLabel}>{localizeText(lang, "Loading...", "Loading...")}</div>}
                        {!loading && challenges.length === 0 && <div style={cnLabel}>{localizeText(lang, "No challenges yet", "No challenges yet")}</div>}
                        <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
                            {challenges.map((challenge, index) => {
                                const id = challengeID(challenge, index);
                                const title = firstString(challenge, ["title", "Title", "name", "Name", "prompt", "Prompt"], id);
                                const active = selectedID === id;
                                return (
                                    <button
                                        key={id}
                                        onClick={() => setSelectedID(id)}
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
                                        <div style={{ fontSize: "0.75rem", fontWeight: 700 }}>{title}</div>
                                        <div style={{ fontSize: "0.64rem", color: colors.textMuted, marginTop: "3px" }}>{id}</div>
                                    </button>
                                );
                            })}
                        </div>
                    </div>

                    <div style={{ ...cnCard, minWidth: 0, background: colors.bg }}>
                        {(() => {
                            const selected = challenges.find((challenge, index) => challengeID(challenge, index) === selectedID);
                            if (!selected) return <div style={cnLabel}>{localizeText(lang, "Select a challenge to respond.", "Select a challenge to respond.")}</div>;
                            const title = firstString(selected, ["title", "Title", "name", "Name"], selectedID);
                            const prompt = firstString(selected, ["prompt", "Prompt", "description", "Description", "body", "Body", "question", "Question"]);
                            const category = firstString(selected, ["category", "Category", "domain", "Domain", "topic", "Topic"]);
                            const reward = firstNumber(selected, ["reward", "Reward", "shells", "Shells"]);
                            const response = responses[selectedID] || "";
                            return (
                                <>
                                    <div style={{ ...cnHeading, marginBottom: "4px" }}>{title}</div>
                                    <div style={{ display: "flex", gap: "8px", flexWrap: "wrap", marginBottom: "8px" }}>
                                        {category && <span style={{ fontSize: "0.66rem", color: colors.textSecondary, background: colors.accentBg, padding: "2px 8px", borderRadius: "999px" }}>{category}</span>}
                                        {typeof reward === "number" && <span style={{ fontSize: "0.66rem", color: colors.textSecondary, background: colors.accentBg, padding: "2px 8px", borderRadius: "999px" }}>{reward} Shells</span>}
                                    </div>
                                    <div style={{ border: `1px solid ${colors.border}`, borderRadius: "8px", padding: "10px", background: colors.surface, color: colors.textSecondary, fontSize: "0.74rem", whiteSpace: "pre-wrap", lineHeight: 1.55, marginBottom: "10px" }}>
                                        {prompt || localizeText(lang, "No prompt text provided by this challenge.", "No prompt text provided by this challenge.")}
                                    </div>
                                    <textarea
                                        value={response}
                                        onChange={(e) => setResponses(prev => ({ ...prev, [selectedID]: e.target.value }))}
                                        placeholder={localizeText(lang, "Write your step-by-step response...", "Write your step-by-step response...")}
                                        style={{ ...cnInput, minHeight: "160px", resize: "vertical", marginBottom: "8px" }}
                                    />
                                    <button style={cnActionBtn(!response.trim() || submittingID === selectedID)} disabled={!response.trim() || submittingID === selectedID} onClick={() => submitResponse(selectedID)}>
                                        {submittingID === selectedID ? "..." : localizeText(lang, "Submit Response", "Submit Response")}
                                    </button>
                                </>
                            );
                        })()}
                    </div>
                </div>
            )}

            {tab === "scores" && (
                <div style={cnCard}>
                    <div style={cnHeading}>{localizeText(lang, "Intelligence Scores", "Intelligence Scores")}</div>
                    {loading && <div style={cnLabel}>{localizeText(lang, "Loading...", "Loading...")}</div>}
                    {!loading && scores.length === 0 && <div style={cnLabel}>{localizeText(lang, "No scores yet", "No scores yet")}</div>}
                    {scores.map((score, index) => {
                        const agent = firstString(score, ["agent", "Agent", "did", "DID", "peer", "Peer", "peer_id", "PeerID"], `#${index + 1}`);
                        const value = firstNumber(score, ["score", "Score", "points", "Points", "rating", "Rating"]);
                        const tier = firstString(score, ["tier", "Tier", "rank", "Rank"]);
                        return (
                            <div key={`${agent}-${index}`} style={{ display: "grid", gridTemplateColumns: "48px minmax(0, 1fr) auto", gap: "10px", alignItems: "center", padding: "8px 0", borderBottom: index === scores.length - 1 ? "none" : `1px solid ${colors.border}` }}>
                                <div style={{ fontSize: "0.72rem", fontWeight: 700, color: colors.textMuted }}>#{index + 1}</div>
                                <div style={{ minWidth: 0 }}>
                                    <div style={{ fontSize: "0.74rem", fontWeight: 700, color: colors.text, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{agent}</div>
                                    {tier && <div style={{ fontSize: "0.65rem", color: colors.textMuted }}>{tier}</div>}
                                </div>
                                {typeof value === "number" && <div style={{ fontSize: "0.74rem", fontWeight: 700, color: colors.primary }}>{value}</div>}
                            </div>
                        );
                    })}
                </div>
            )}
        </div>
    );
}
