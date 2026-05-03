import { useState } from "react";
import {
    AgentNetExtractDAG,
    AgentNetFileDispute,
    AgentNetQueryOntology,
} from "../../../wailsjs/go/main/App";
import { colors } from "./styles";
import { cnActionBtn, cnCard, cnHeading, cnInput, cnLabel, cnTabStyle } from "./agentnetStyles";

const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === "zh-Hans" ? zhHans : lang === "zh-Hant" ? zhHant : en
);

type Props = { lang: string; agentNetRunning: boolean };

function splitLines(value: string): string[] {
    return value.split(/\r?\n/).map(v => v.trim()).filter(Boolean);
}

function pretty(value: unknown): string {
    return JSON.stringify(value ?? {}, null, 2);
}

function normalizeDepth(value: string): number {
    const parsed = Number.parseInt(value, 10);
    if (!Number.isFinite(parsed)) return 2;
    return Math.min(10, Math.max(1, parsed));
}

export function AgentNetToolsPanel({ lang, agentNetRunning }: Props) {
    const [tab, setTab] = useState<"dispute" | "dag" | "ontology">("dispute");
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const [message, setMessage] = useState("");
    const [taskID, setTaskID] = useState("");
    const [reason, setReason] = useState("");
    const [intent, setIntent] = useState("");
    const [steps, setSteps] = useState("");
    const [outputs, setOutputs] = useState("");
    const [dagResult, setDagResult] = useState("");
    const [query, setQuery] = useState("");
    const [depth, setDepth] = useState("2");
    const [ontologyResult, setOntologyResult] = useState("");

    const resetStatus = () => {
        setError("");
        setMessage("");
    };

    const handleDispute = async () => {
        if (!taskID.trim() || !reason.trim() || busy) return;
        setBusy(true);
        resetStatus();
        try {
            const res = await AgentNetFileDispute(taskID.trim(), reason.trim());
            if (res.ok) {
                setMessage(localizeText(lang, "Dispute filed", "Dispute filed"));
                setTaskID("");
                setReason("");
            } else {
                setError(String(res.error || "Failed to file dispute"));
            }
        } catch (e: any) {
            setError(e?.message || String(e));
        } finally {
            setBusy(false);
        }
    };

    const handleExtractDAG = async () => {
        if (!intent.trim() || busy) return;
        setBusy(true);
        resetStatus();
        setDagResult("");
        try {
            const res = await AgentNetExtractDAG(intent.trim(), splitLines(steps), splitLines(outputs));
            if (res.ok) {
                setDagResult(pretty(res.nodes));
            } else {
                setError(String(res.error || "Failed to extract DAG"));
            }
        } catch (e: any) {
            setError(e?.message || String(e));
        } finally {
            setBusy(false);
        }
    };

    const handleQueryOntology = async () => {
        if (!query.trim() || busy) return;
        setBusy(true);
        resetStatus();
        setOntologyResult("");
        try {
            const res = await AgentNetQueryOntology(query.trim(), normalizeDepth(depth));
            if (res.ok) {
                setOntologyResult(pretty(res.result));
            } else {
                setError(String(res.error || "Failed to query ontology"));
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
        <div style={{ padding: "10px 14px" }}>
            <div style={{ display: "flex", gap: "6px", marginBottom: "10px", flexWrap: "wrap" }}>
                <button style={cnTabStyle(tab === "dispute")} onClick={() => setTab("dispute")}>{localizeText(lang, "Disputes", "Disputes")}</button>
                <button style={cnTabStyle(tab === "dag")} onClick={() => setTab("dag")}>{localizeText(lang, "DAG", "DAG")}</button>
                <button style={cnTabStyle(tab === "ontology")} onClick={() => setTab("ontology")}>{localizeText(lang, "Ontology", "Ontology")}</button>
            </div>

            {error && <div style={{ fontSize: "0.72rem", color: colors.danger, marginBottom: "8px" }}>{error}</div>}
            {message && <div style={{ fontSize: "0.72rem", color: colors.success, marginBottom: "8px" }}>{message}</div>}

            {tab === "dispute" && (
                <div style={{ ...cnCard, maxWidth: "760px", background: colors.bg }}>
                    <div style={cnHeading}>{localizeText(lang, "File a Task Dispute", "File a Task Dispute")}</div>
                    <div style={{ ...cnLabel, marginBottom: "8px" }}>{localizeText(lang, "Use this only when a rejected task was completed according to its acceptance criteria.", "Use this only when a rejected task was completed according to its acceptance criteria.")}</div>
                    <input value={taskID} onChange={e => setTaskID(e.target.value)} placeholder="task id" style={{ ...cnInput, marginBottom: "8px" }} />
                    <textarea value={reason} onChange={e => setReason(e.target.value)} placeholder={localizeText(lang, "Reason and evidence summary", "Reason and evidence summary")} style={{ ...cnInput, minHeight: "140px", resize: "vertical", marginBottom: "8px" }} />
                    <button style={cnActionBtn(busy || !taskID.trim() || !reason.trim())} disabled={busy || !taskID.trim() || !reason.trim()} onClick={handleDispute}>{busy ? "..." : localizeText(lang, "File Dispute", "File Dispute")}</button>
                </div>
            )}

            {tab === "dag" && (
                <div style={{ display: "grid", gridTemplateColumns: "minmax(280px, 420px) minmax(0, 1fr)", gap: "12px" }}>
                    <div style={{ ...cnCard, background: colors.bg }}>
                        <div style={cnHeading}>{localizeText(lang, "Extract Task DAG", "Extract Task DAG")}</div>
                        <input value={intent} onChange={e => setIntent(e.target.value)} placeholder={localizeText(lang, "Intent", "Intent")} style={{ ...cnInput, marginBottom: "8px" }} />
                        <textarea value={steps} onChange={e => setSteps(e.target.value)} placeholder={localizeText(lang, "Steps, one per line", "Steps, one per line")} style={{ ...cnInput, minHeight: "130px", resize: "vertical", marginBottom: "8px" }} />
                        <textarea value={outputs} onChange={e => setOutputs(e.target.value)} placeholder={localizeText(lang, "Outputs, one per line", "Outputs, one per line")} style={{ ...cnInput, minHeight: "80px", resize: "vertical", marginBottom: "8px" }} />
                        <button style={cnActionBtn(busy || !intent.trim())} disabled={busy || !intent.trim()} onClick={handleExtractDAG}>{busy ? "..." : localizeText(lang, "Extract", "Extract")}</button>
                    </div>
                    <pre style={{ ...cnCard, margin: 0, background: colors.bg, color: colors.textSecondary, fontSize: "0.72rem", overflow: "auto", minHeight: "320px" }}>{dagResult || localizeText(lang, "DAG result will appear here.", "DAG result will appear here.")}</pre>
                </div>
            )}

            {tab === "ontology" && (
                <div style={{ display: "grid", gridTemplateColumns: "minmax(280px, 420px) minmax(0, 1fr)", gap: "12px" }}>
                    <div style={{ ...cnCard, background: colors.bg }}>
                        <div style={cnHeading}>{localizeText(lang, "Query Knowledge Graph", "Query Knowledge Graph")}</div>
                        <input value={query} onChange={e => setQuery(e.target.value)} placeholder={localizeText(lang, "Query", "Query")} style={{ ...cnInput, marginBottom: "8px" }} />
                        <input value={depth} onChange={e => setDepth(e.target.value.replace(/[^0-9]/g, ""))} placeholder="depth" style={{ ...cnInput, marginBottom: "8px" }} />
                        <button style={cnActionBtn(busy || !query.trim())} disabled={busy || !query.trim()} onClick={handleQueryOntology}>{busy ? "..." : localizeText(lang, "Query", "Query")}</button>
                    </div>
                    <pre style={{ ...cnCard, margin: 0, background: colors.bg, color: colors.textSecondary, fontSize: "0.72rem", overflow: "auto", minHeight: "320px" }}>{ontologyResult || localizeText(lang, "Ontology result will appear here.", "Ontology result will appear here.")}</pre>
                </div>
            )}
        </div>
    );
}
