import { useCallback, useEffect, useRef, useState } from "react";
import {
    AgentNetCallService,
    AgentNetDiscoverServices,
    AgentNetListServices,
    AgentNetRegisterService,
    AgentNetUnregisterService,
} from "../../../wailsjs/go/main/App";
import { colors, radius } from "./styles";
import { cnActionBtn, cnCard, cnHeading, cnInput, cnLabel, cnTabStyle } from "./agentnetStyles";

const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === "zh-Hans" ? zhHans : lang === "zh-Hant" ? zhHant : en
);

type Props = { lang: string; agentNetRunning: boolean };

type Service = {
    name?: string;
    url?: string;
    description?: string;
    tags?: string[];
    modes?: string[];
    billing?: string;
    price?: number;
    free_tier?: number;
    Name?: string;
    URL?: string;
    Description?: string;
    Tags?: string[];
    Modes?: string[];
    Billing?: string;
    Price?: number;
    FreeTier?: number;
};

function serviceName(service: Service): string {
    return service.name || service.Name || "";
}

function serviceURL(service: Service): string {
    return service.url || service.URL || "";
}

function serviceDescription(service: Service): string {
    return service.description || service.Description || "";
}

function serviceTags(service: Service): string[] {
    return service.tags || service.Tags || [];
}

function serviceModes(service: Service): string[] {
    return service.modes || service.Modes || [];
}

function serviceBilling(service: Service): string {
    return service.billing || service.Billing || "free";
}

function servicePrice(service: Service): number {
    return service.price ?? service.Price ?? 0;
}

function parseCSV(value: string): string[] {
    return value.split(",").map(v => v.trim()).filter(Boolean);
}

function normalizeDecimalInput(value: string): string {
    const cleaned = value.replace(/[^0-9.]/g, "");
    const [head, ...tail] = cleaned.split(".");
    return tail.length === 0 ? head : head + "." + tail.join("");
}

function nonNegativeNumber(value: string): number {
    const parsed = Number(value);
    return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

function nonNegativeInteger(value: string): number {
    const parsed = Number.parseInt(value, 10);
    return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

function parseHeaders(value: string): Record<string, string> {
    const headers: Record<string, string> = {};
    for (const line of value.split(/\r?\n/)) {
        const idx = line.indexOf(":");
        if (idx <= 0) continue;
        const key = line.slice(0, idx).trim();
        const val = line.slice(idx + 1).trim();
        if (key) headers[key] = val;
    }
    return headers;
}

export function AgentNetServicesPanel({ lang, agentNetRunning }: Props) {
    const [tab, setTab] = useState<"local" | "remote">("local");
    const [localServices, setLocalServices] = useState<Service[]>([]);
    const [remoteServices, setRemoteServices] = useState<Service[]>([]);
    const [loading, setLoading] = useState(false);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const [message, setMessage] = useState("");
    const [name, setName] = useState("");
    const [url, setURL] = useState("http://127.0.0.1:8080");
    const [description, setDescription] = useState("");
    const [tags, setTags] = useState("");
    const [modes, setModes] = useState("rr");
    const [billing, setBilling] = useState("free");
    const [price, setPrice] = useState("0");
    const [freeTier, setFreeTier] = useState("0");
    const [peer, setPeer] = useState("");
    const [callServiceName, setCallServiceName] = useState("");
    const [method, setMethod] = useState("POST");
    const [path, setPath] = useState("/");
    const [headersText, setHeadersText] = useState("Content-Type: application/json");
    const [body, setBody] = useState("");
    const [callResult, setCallResult] = useState("");
    const mountedRef = useRef(true);

    useEffect(() => {
        mountedRef.current = true;
        return () => { mountedRef.current = false; };
    }, []);

    const loadLocalServices = useCallback(async () => {
        if (!agentNetRunning) return;
        setLoading(true);
        setError("");
        try {
            const res = await AgentNetListServices();
            if (!mountedRef.current) return;
            if (res.ok) setLocalServices((res.services as Service[]) || []);
            else setError(String(res.error || "Failed to load local services"));
        } catch (e: any) {
            if (mountedRef.current) setError(e?.message || String(e));
        } finally {
            if (mountedRef.current) setLoading(false);
        }
    }, [agentNetRunning]);

    useEffect(() => { void loadLocalServices(); }, [loadLocalServices]);

    const handleRegister = async () => {
        if (!name.trim() || !url.trim() || busy) return;
        setBusy(true);
        setError("");
        setMessage("");
        try {
            const res = await AgentNetRegisterService(
                name.trim().toLowerCase(),
                url.trim(),
                description.trim(),
                parseCSV(tags),
                parseCSV(modes || "rr"),
                billing,
                nonNegativeNumber(price),
                nonNegativeInteger(freeTier),
            );
            if (res.ok) {
                setMessage(localizeText(lang, "Service registered", "Service registered"));
                setName("");
                setDescription("");
                await loadLocalServices();
            } else {
                setError(String(res.error || "Failed to register service"));
            }
        } catch (e: any) {
            setError(e?.message || String(e));
        } finally {
            setBusy(false);
        }
    };

    const handleUnregister = async (service: string) => {
        if (!service || busy) return;
        setBusy(true);
        setError("");
        setMessage("");
        try {
            const res = await AgentNetUnregisterService(service);
            if (res.ok) {
                setMessage(localizeText(lang, "Service unregistered", "Service unregistered"));
                await loadLocalServices();
            } else {
                setError(String(res.error || "Failed to unregister service"));
            }
        } catch (e: any) {
            setError(e?.message || String(e));
        } finally {
            setBusy(false);
        }
    };

    const handleDiscover = async () => {
        if (!peer.trim() || busy) return;
        setBusy(true);
        setError("");
        setMessage("");
        try {
            const res = await AgentNetDiscoverServices(peer.trim());
            if (res.ok) setRemoteServices((res.services as Service[]) || []);
            else setError(String(res.error || "Failed to discover services"));
        } catch (e: any) {
            setError(e?.message || String(e));
        } finally {
            setBusy(false);
        }
    };

    const handleCall = async () => {
        if (!peer.trim() || !callServiceName.trim() || busy) return;
        setBusy(true);
        setError("");
        setCallResult("");
        try {
            const res = await AgentNetCallService(peer.trim(), callServiceName.trim(), method.trim() || "POST", path.trim() || "/", parseHeaders(headersText), body);
            if (res.ok) setCallResult(JSON.stringify(res.result ?? {}, null, 2));
            else setError(String(res.error || "Failed to call service"));
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
                <button style={cnTabStyle(tab === "local")} onClick={() => setTab("local")}>{localizeText(lang, "Local Services", "Local Services")}</button>
                <button style={cnTabStyle(tab === "remote")} onClick={() => setTab("remote")}>{localizeText(lang, "Remote Call", "Remote Call")}</button>
                {tab === "local" && <button style={cnActionBtn(loading)} disabled={loading} onClick={loadLocalServices}>{loading ? "..." : localizeText(lang, "Refresh", "Refresh")}</button>}
            </div>

            {error && <div style={{ fontSize: "0.72rem", color: colors.danger, marginBottom: "8px" }}>{error}</div>}
            {message && <div style={{ fontSize: "0.72rem", color: colors.success, marginBottom: "8px" }}>{message}</div>}

            {tab === "local" && (
                <div style={{ display: "grid", gridTemplateColumns: "minmax(280px, 380px) minmax(0, 1fr)", gap: "12px" }}>
                    <div style={{ ...cnCard, background: colors.bg }}>
                        <div style={cnHeading}>{localizeText(lang, "Register Local HTTP Service", "Register Local HTTP Service")}</div>
                        <div style={{ ...cnLabel, marginBottom: "8px" }}>{localizeText(lang, "Only localhost HTTP URLs are allowed, for example http://127.0.0.1:8080.", "Only localhost HTTP URLs are allowed, for example http://127.0.0.1:8080.")}</div>
                        <input value={name} onChange={e => setName(e.target.value.replace(/[^a-zA-Z0-9]/g, "").toLowerCase())} placeholder="service" style={{ ...cnInput, marginBottom: "6px" }} />
                        <input value={url} onChange={e => setURL(e.target.value)} placeholder="http://127.0.0.1:8080" style={{ ...cnInput, marginBottom: "6px" }} />
                        <textarea value={description} onChange={e => setDescription(e.target.value)} placeholder={localizeText(lang, "Description", "Description")} style={{ ...cnInput, minHeight: "56px", resize: "vertical", marginBottom: "6px" }} />
                        <input value={tags} onChange={e => setTags(e.target.value)} placeholder="api,search" style={{ ...cnInput, marginBottom: "6px" }} />
                        <input value={modes} onChange={e => setModes(e.target.value)} placeholder="rr,server-stream" style={{ ...cnInput, marginBottom: "6px" }} />
                        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr", gap: "6px", marginBottom: "8px" }}>
                            <select value={billing} onChange={e => setBilling(e.target.value)} style={cnInput}>
                                <option value="free">free</option>
                                <option value="per_call">per_call</option>
                                <option value="per_kb">per_kb</option>
                            </select>
                            <input value={price} onChange={e => setPrice(normalizeDecimalInput(e.target.value))} placeholder="price" style={cnInput} />
                            <input value={freeTier} onChange={e => setFreeTier(e.target.value.replace(/[^0-9]/g, ""))} placeholder="free tier" style={cnInput} />
                        </div>
                        <button style={cnActionBtn(busy || !name.trim() || !url.trim())} disabled={busy || !name.trim() || !url.trim()} onClick={handleRegister}>{busy ? "..." : localizeText(lang, "Register", "Register")}</button>
                    </div>

                    <div style={cnCard}>
                        <div style={cnHeading}>{localizeText(lang, "Registered Services", "Registered Services")}</div>
                        {!loading && localServices.length === 0 && <div style={cnLabel}>{localizeText(lang, "No local services registered", "No local services registered")}</div>}
                        {localServices.map(service => {
                            const svcName = serviceName(service);
                            return (
                                <div key={svcName} style={{ border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: "10px", marginBottom: "8px", background: colors.bg }}>
                                    <div style={{ display: "flex", justifyContent: "space-between", gap: "8px", alignItems: "center" }}>
                                        <div style={{ minWidth: 0 }}>
                                            <div style={{ fontSize: "0.78rem", fontWeight: 700, color: colors.text }}>{svcName}</div>
                                            <div style={{ fontSize: "0.68rem", color: colors.textMuted, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{serviceURL(service)}</div>
                                        </div>
                                        <button style={cnActionBtn(busy)} disabled={busy} onClick={() => handleUnregister(svcName)}>{localizeText(lang, "Remove", "Remove")}</button>
                                    </div>
                                    {serviceDescription(service) && <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginTop: "6px" }}>{serviceDescription(service)}</div>}
                                    <div style={{ display: "flex", gap: "6px", flexWrap: "wrap", marginTop: "6px" }}>
                                        <span style={{ fontSize: "0.64rem", color: colors.textSecondary, background: colors.accentBg, borderRadius: radius.pill, padding: "1px 6px" }}>{serviceBilling(service)} / {servicePrice(service)}</span>
                                        {serviceModes(service).map(mode => <span key={mode} style={{ fontSize: "0.64rem", color: colors.textSecondary, background: colors.accentBg, borderRadius: radius.pill, padding: "1px 6px" }}>{mode}</span>)}
                                        {serviceTags(service).map(tag => <span key={tag} style={{ fontSize: "0.64rem", color: colors.textSecondary, background: colors.accentBg, borderRadius: radius.pill, padding: "1px 6px" }}>{tag}</span>)}
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                </div>
            )}

            {tab === "remote" && (
                <div style={{ display: "grid", gridTemplateColumns: "minmax(280px, 380px) minmax(0, 1fr)", gap: "12px" }}>
                    <div style={{ ...cnCard, background: colors.bg }}>
                        <div style={cnHeading}>{localizeText(lang, "Discover Remote Services", "Discover Remote Services")}</div>
                        <input value={peer} onChange={e => setPeer(e.target.value)} placeholder="peer id" style={{ ...cnInput, marginBottom: "8px" }} />
                        <button style={cnActionBtn(busy || !peer.trim())} disabled={busy || !peer.trim()} onClick={handleDiscover}>{busy ? "..." : localizeText(lang, "Discover", "Discover")}</button>
                        <div style={{ marginTop: "10px" }}>
                            {remoteServices.map(service => (
                                <button key={serviceName(service)} onClick={() => setCallServiceName(serviceName(service))} style={{ width: "100%", textAlign: "left", border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: "8px", marginBottom: "6px", background: colors.surface, color: colors.text, cursor: "pointer" }}>
                                    <div style={{ fontSize: "0.74rem", fontWeight: 700 }}>{serviceName(service)}</div>
                                    <div style={{ fontSize: "0.66rem", color: colors.textMuted }}>{serviceBilling(service)} / {servicePrice(service)}</div>
                                </button>
                            ))}
                        </div>
                    </div>

                    <div style={cnCard}>
                        <div style={cnHeading}>{localizeText(lang, "Call Service", "Call Service")}</div>
                        <div style={{ display: "grid", gridTemplateColumns: "110px minmax(0, 1fr)", gap: "6px", marginBottom: "6px" }}>
                            <select value={method} onChange={e => setMethod(e.target.value)} style={cnInput}>
                                <option>GET</option>
                                <option>POST</option>
                                <option>PUT</option>
                                <option>DELETE</option>
                            </select>
                            <input value={path} onChange={e => setPath(e.target.value)} placeholder="/search" style={cnInput} />
                        </div>
                        <input value={callServiceName} onChange={e => setCallServiceName(e.target.value)} placeholder="service" style={{ ...cnInput, marginBottom: "6px" }} />
                        <textarea value={headersText} onChange={e => setHeadersText(e.target.value)} placeholder="Header: value" style={{ ...cnInput, minHeight: "56px", resize: "vertical", marginBottom: "6px" }} />
                        <textarea value={body} onChange={e => setBody(e.target.value)} placeholder={localizeText(lang, "Request body", "Request body")} style={{ ...cnInput, minHeight: "100px", resize: "vertical", marginBottom: "8px", fontFamily: "monospace" }} />
                        <button style={cnActionBtn(busy || !peer.trim() || !callServiceName.trim())} disabled={busy || !peer.trim() || !callServiceName.trim()} onClick={handleCall}>{busy ? "..." : localizeText(lang, "Call", "Call")}</button>
                        {callResult && <pre style={{ marginTop: "10px", padding: "10px", border: `1px solid ${colors.border}`, borderRadius: radius.md, background: colors.bg, color: colors.textSecondary, fontSize: "0.7rem", overflow: "auto", maxHeight: "260px" }}>{callResult}</pre>}
                    </div>
                </div>
            )}
        </div>
    );
}
