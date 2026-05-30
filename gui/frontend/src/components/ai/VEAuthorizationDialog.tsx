import { useCallback, useEffect, useRef, useState } from "react";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import type { Theme } from "./aiAssistantPanelTheme";
import { looksLikeRawParticipantId } from "./localAIIdentity";

// --- Types ---

export interface AuthorizationRequest {
    id: string;
    requester_name: string;
    requester_machine_id: string;
    target_ve_id: string;
    target_ve_name: string;
    created_at: string;
    expires_at: string;
    source?: string;
    message?: string;
    risk_level?: string;
}

export interface VEAuthorizationDialogProps {
    theme: Theme;
    lang?: string;
    /** Override for testing — Wails binding */
    respondAuthRequest?: (requestId: string, decision: string) => Promise<void>;
}

type AuthDecision = "allow_once" | "allow_long" | "deny" | "block" | "allow";

// --- Component ---

function authTextForLang(isZh: boolean, en: string, zh: string): string {
    return isZh ? zh : en;
}

function readableAuthName(
    name: string | undefined,
    id: string | undefined,
    isZh: boolean,
    fallbackEn: string,
    fallbackZh: string,
): string {
    const trimmed = String(name || "").trim();
    const rawId = String(id || "").trim();
    if (trimmed && trimmed !== rawId && !looksLikeRawParticipantId(trimmed)) return trimmed;
    return authTextForLang(isZh, fallbackEn, fallbackZh);
}

export function VEAuthorizationDialog({
    theme,
    lang,
    respondAuthRequest,
}: VEAuthorizationDialogProps) {
    const [requests, setRequests] = useState<AuthorizationRequest[]>([]);
    const [responding, setResponding] = useState<string | null>(null);
    const mountedRef = useRef(true);

    const isZh = !lang || lang.startsWith("zh");

    // Listen for auth request events
    useEffect(() => {
        mountedRef.current = true;

        const handleAuthRequest = (data: any) => {
            if (!mountedRef.current) return;
            const req: AuthorizationRequest = {
                id: data?.id || data?.request_id || "",
                requester_name: data?.requester_name || "",
                requester_machine_id: data?.requester_machine_id || "",
                target_ve_id: data?.target_ve_id || "",
                target_ve_name: data?.target_ve_name || "",
                created_at: data?.created_at || "",
                expires_at: data?.expires_at || "",
            };
            if (req.id) {
                setRequests((prev) => {
                    // Avoid duplicates
                    if (prev.some((r) => r.id === req.id)) return prev;
                    return [...prev, req];
                });
            }
        };

        const unsub = EventsOn("ve:auth_request", handleAuthRequest);

        return () => {
            mountedRef.current = false;
            if (typeof unsub === "function") unsub();
            else EventsOff("ve:auth_request");
        };
    }, []);

    // Handle allow/deny
    const handleDecision = useCallback(
        async (requestId: string, decision: "allow" | "deny") => {
            setResponding(requestId);
            try {
                if (respondAuthRequest) {
                    await respondAuthRequest(requestId, decision);
                } else {
                    const mod = await import("../../../wailsjs/go/main/App");
                    await (mod as any).RespondAuthRequest(requestId, decision);
                }
                if (mountedRef.current) {
                    setRequests((prev) => prev.filter((r) => r.id !== requestId));
                }
            } catch (err) {
                // Keep the request in the list on error
                console.error("Failed to respond to auth request:", err);
            } finally {
                if (mountedRef.current) {
                    setResponding(null);
                }
            }
        },
        [respondAuthRequest]
    );

    // Don't render if no pending requests
    if (requests.length === 0) return null;

    return (
        <div
            data-testid="ve-auth-dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="ve-auth-dialog-title"
            style={{
                position: "fixed",
                top: 0,
                left: 0,
                right: 0,
                bottom: 0,
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                background: "rgba(0,0,0,0.4)",
                zIndex: 10000,
            }}
        >
            <div
                style={{
                    background: theme.bg,
                    borderRadius: 10,
                    boxShadow: "0 8px 32px rgba(0,0,0,0.2)",
                    padding: "20px 24px",
                    maxWidth: 420,
                    width: "90%",
                }}
            >
                <h3
                    id="ve-auth-dialog-title"
                    style={{
                        margin: "0 0 16px 0",
                        fontSize: 15,
                        color: theme.headingColor || theme.text,
                    }}
                >
                    🔒 {isZh ? "授权请求" : "Authorization Request"}
                </h3>

                {requests.map((req) => (
                    <div
                        key={req.id}
                        data-testid={`ve-auth-request-${req.id}`}
                        style={{
                            padding: "12px",
                            marginBottom: 12,
                            borderRadius: 6,
                            border: `1px solid ${theme.divider}`,
                            background: theme.fieldBg,
                        }}
                    >
                        <div style={{ fontSize: 13, color: theme.text, marginBottom: 8 }}>
                            <div style={{ marginBottom: 4 }}>
                                <span style={{ color: theme.textMuted, fontSize: 12 }}>
                                    {isZh ? "请求者：" : "Requester: "}
                                </span>
                                <strong>{readableAuthName(req.requester_name, req.requester_machine_id, isZh, "Requester", "请求者")}</strong>
                            </div>
                            <div>
                                <span style={{ color: theme.textMuted, fontSize: 12 }}>
                                    {isZh ? "目标数字员工：" : "Target digital employee: "}
                                </span>
                                <strong>{readableAuthName(req.target_ve_name, req.target_ve_id, isZh, "Digital employee", "数字员工")}</strong>
                            </div>
                        </div>

                        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
                            <button
                                data-testid={`ve-auth-deny-${req.id}`}
                                onClick={() => handleDecision(req.id, "deny")}
                                disabled={responding === req.id}
                                style={{
                                    padding: "5px 14px",
                                    borderRadius: 5,
                                    border: `1px solid ${theme.errorBorder || "#fecaca"}`,
                                    background: theme.errorBg || "#fef2f2",
                                    color: theme.errorText || "#dc2626",
                                    cursor: responding === req.id ? "not-allowed" : "pointer",
                                    fontSize: 12,
                                    opacity: responding === req.id ? 0.5 : 1,
                                }}
                            >
                                {isZh ? "拒绝" : "Deny"}
                            </button>
                            <button
                                data-testid={`ve-auth-allow-${req.id}`}
                                onClick={() => handleDecision(req.id, "allow")}
                                disabled={responding === req.id}
                                style={{
                                    padding: "5px 14px",
                                    borderRadius: 5,
                                    border: "none",
                                    background: theme.sendBtnBg,
                                    color: theme.sendBtnColor,
                                    cursor: responding === req.id ? "not-allowed" : "pointer",
                                    fontSize: 12,
                                    fontWeight: 500,
                                    opacity: responding === req.id ? 0.5 : 1,
                                }}
                            >
                                {isZh ? "允许" : "Allow"}
                            </button>
                        </div>
                    </div>
                ))}
            </div>
        </div>
    );
}

function normalizeAuthEvent(data: any): AuthorizationRequest {
    const payload = data?.payload && typeof data.payload === "object" ? data.payload : data;
    return {
        id: payload?.id || payload?.request_id || "",
        requester_name: payload?.requester_name || payload?.requester_display_name || "",
        requester_machine_id: payload?.requester_machine_id || payload?.from_machine_id || "",
        target_ve_id: payload?.target_ve_id || payload?.ve_id || "",
        target_ve_name: payload?.target_ve_name || payload?.ve_name || "",
        created_at: payload?.created_at || "",
        expires_at: payload?.expires_at || "",
        source: payload?.source || payload?.channel || "",
        message: payload?.message || payload?.reason || payload?.request_message || "",
        risk_level: payload?.risk_level || "",
    };
}

function authRequestExpiryMs(request: AuthorizationRequest): number | null {
    const expiresAt = String(request.expires_at || "").trim();
    if (!expiresAt) return null;
    const ms = Date.parse(expiresAt);
    return Number.isFinite(ms) ? ms : null;
}

function pruneExpiredAuthRequests(requests: AuthorizationRequest[], nowMs: number = Date.now()): AuthorizationRequest[] {
    return requests.filter((request) => {
        const expiresAt = authRequestExpiryMs(request);
        return expiresAt === null || expiresAt > nowMs;
    });
}

function AuthBellIcon({ size = 14 }: { size?: number }) {
    return (
        <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" focusable="false" style={{ display: "block" }}>
            <path d="M15 17h5l-1.4-1.4A2 2 0 0 1 18 14.2V11a6 6 0 1 0-12 0v3.2a2 2 0 0 1-.6 1.4L4 17h5" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
            <path d="M9 17a3 3 0 0 0 6 0" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
    );
}

export interface VEAuthorizationRequestCenterProps extends VEAuthorizationDialogProps {
    inline?: boolean;
}

export function VEAuthorizationRequestCenter({ theme, lang, respondAuthRequest, inline }: VEAuthorizationRequestCenterProps) {
    const [requests, setRequests] = useState<AuthorizationRequest[]>([]);
    const [open, setOpen] = useState(false);
    const [responding, setResponding] = useState<string | null>(null);
    const [respondError, setRespondError] = useState<string | null>(null);
    const mountedRef = useRef(true);
    const respondingRef = useRef<string | null>(null);
    const isZh = !lang || lang.startsWith("zh");

    useEffect(() => {
        mountedRef.current = true;
        const handleAuthRequest = (data: any) => {
            if (!mountedRef.current) return;
            const req = normalizeAuthEvent(data);
            if (!req.id) return;
            setRequests((prev) => prev.some((item) => item.id === req.id) ? prev : [...prev, req]);
        };
        const unsub = EventsOn("ve:auth_request", handleAuthRequest);
        return () => {
            mountedRef.current = false;
            if (typeof unsub === "function") unsub();
            else EventsOff("ve:auth_request");
        };
    }, []);

    useEffect(() => {
        if (requests.length === 0) setOpen(false);
    }, [requests.length]);

    useEffect(() => {
        if (requests.length === 0) return undefined;
        const now = Date.now();
        const active = pruneExpiredAuthRequests(requests, now);
        if (active.length !== requests.length) {
            setRequests(active);
            return undefined;
        }
        const nextExpiry = active
            .map(authRequestExpiryMs)
            .filter((value): value is number => value !== null && value > now)
            .sort((a, b) => a - b)[0];
        if (!nextExpiry) return undefined;
        const timer = window.setTimeout(() => {
            if (mountedRef.current) setRequests((prev) => pruneExpiredAuthRequests(prev));
        }, Math.max(0, nextExpiry - now));
        return () => window.clearTimeout(timer);
    }, [requests]);

    const respond = useCallback(async (requestId: string, decision: AuthDecision) => {
        if (respondingRef.current) return;
        respondingRef.current = requestId;
        setResponding(requestId);
        setRespondError(null);
        try {
            if (respondAuthRequest) {
                await respondAuthRequest(requestId, decision);
            } else {
                const mod = await import("../../../wailsjs/go/main/App");
                await (mod as any).RespondAuthRequest(requestId, decision);
            }
            if (mountedRef.current) setRequests((prev) => prev.filter((item) => item.id !== requestId));
        } catch (err) {
            console.error("Failed to respond to auth request:", err);
            if (mountedRef.current) {
                const message = err instanceof Error ? err.message : String(err || "");
                setRespondError(isZh ? `处理失败：${message || "请稍后重试"}` : `Access request failed: ${message || "try again later"}`);
            }
        } finally {
            respondingRef.current = null;
            if (mountedRef.current) setResponding(null);
        }
    }, [isZh, respondAuthRequest]);

    if (requests.length === 0) return null;

    const title = isZh ? "访问请求" : "Access requests";
    const hint = isZh ? "首次访问需确认。处理后对端会收到通过、拒绝或无法访问状态。" : "First access requires confirmation. The requester is updated after your decision.";

    return (
        <div className="ve-auth-request-center" data-testid="ve-auth-request-center" style={{ position: "relative", display: "inline-flex", ...(inline ? { WebkitAppRegion: "no-drag" } as any : {}) }}>
            <button
                type="button"
                className="ai-titlebar-tool ve-auth-request-trigger ve-auth-request-trigger--blink"
                data-testid="ve-auth-request-trigger"
                aria-label={isZh ? `${requests.length} 个访问请求待确认` : `${requests.length} pending access request(s)`}
                onMouseDown={(e) => { if (inline) { e.preventDefault(); e.stopPropagation(); setOpen((value) => !value); } }}
                onClick={(e) => { if (!inline) { e.preventDefault(); e.stopPropagation(); setOpen((value) => !value); } }}
                style={{
                    minWidth: 30,
                    height: 28,
                    borderRadius: 6,
                    border: `1px solid ${theme.errorBorder || theme.titleBarBorder}`,
                    background: theme.errorBg || "rgba(248,113,113,0.14)",
                    color: theme.errorText || "#ef4444",
                    display: "inline-flex",
                    alignItems: "center",
                    justifyContent: "center",
                    gap: 3,
                    cursor: "pointer",
                    fontSize: 12,
                    fontWeight: 800,
                }}
                title={title}
            >
                <AuthBellIcon />
                <span>{requests.length}</span>
            </button>
            {open && (
                <div
                    className="ve-auth-request-popover"
                    data-testid="ve-auth-request-popover"
                    role="dialog"
                    aria-label={title}
                    style={{
                        position: "absolute",
                        right: 0,
                        top: "calc(100% + 8px)",
                        width: 390,
                        maxWidth: "calc(100vw - 28px)",
                        maxHeight: "min(460px, calc(100vh - 80px))",
                        overflow: "auto",
                        padding: 12,
                        borderRadius: 8,
                        border: `1px solid ${theme.titleBarBorder || theme.divider}`,
                        background: theme.bg,
                        color: theme.text,
                        boxShadow: "0 18px 48px rgba(0,0,0,0.28)",
                        zIndex: 40000,
                    }}
                >
                    <div style={{ display: "flex", justifyContent: "space-between", gap: 10, alignItems: "flex-start", marginBottom: 10 }}>
                        <div>
                            <div style={{ fontSize: 14, fontWeight: 800, color: theme.headingColor || theme.text }}>{title}</div>
                            <div style={{ fontSize: 12, lineHeight: 1.45, color: theme.textMuted, marginTop: 3 }}>{hint}</div>
                            {respondError && <div role="alert" style={{ fontSize: 12, lineHeight: 1.45, color: theme.errorText || "#dc2626", marginTop: 6 }}>{respondError}</div>}
                        </div>
                        <button type="button" onClick={() => setOpen(false)} aria-label={isZh ? "关闭" : "Close"} style={{ border: "none", background: "transparent", color: theme.textMuted, cursor: "pointer", fontSize: 16, lineHeight: 1 }}>×</button>
                    </div>
                    <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
                        {requests.map((req) => {
                            const requester = readableAuthName(req.requester_name, req.requester_machine_id, isZh, "Requester", "请求者");
                            const target = readableAuthName(req.target_ve_name, req.target_ve_id, isZh, "Digital employee", "数字员工");
                            const busy = responding !== null;
                            return (
                                <div key={req.id} data-testid={`ve-auth-request-${req.id}`} style={{ border: `1px solid ${theme.divider}`, background: theme.fieldBg, borderRadius: 8, padding: 10 }}>
                                    <div style={{ fontSize: 13, lineHeight: 1.45, color: theme.text }}>
                                        <strong>{requester}</strong>{isZh ? " 请求访问 " : " requests access to "}<strong>{target}</strong>
                                    </div>
                                    <div style={{ display: "grid", gridTemplateColumns: "72px 1fr", rowGap: 4, columnGap: 8, marginTop: 8, fontSize: 12, color: theme.textMuted }}>
                                        <span>{isZh ? "来源" : "Source"}</span><span>{req.source || (isZh ? "数字员工会话" : "Digital employee chat")}</span>
                                        <span>{isZh ? "关系" : "Relation"}</span><span>{isZh ? "首次访问" : "First access"}</span>
                                        {req.message && <><span>{isZh ? "留言" : "Message"}</span><span>{req.message}</span></>}
                                    </div>
                                    <div style={{ display: "flex", flexWrap: "wrap", gap: 6, justifyContent: "flex-end", marginTop: 10 }}>
                                        <button data-testid={`ve-auth-deny-${req.id}`} disabled={busy} onClick={() => respond(req.id, "deny")} className="ve-auth-action ve-auth-action--ghost">{isZh ? "拒绝" : "Deny"}</button>
                                        <button data-testid={`ve-auth-block-${req.id}`} disabled={busy} onClick={() => respond(req.id, "block")} className="ve-auth-action ve-auth-action--danger">{isZh ? "拒绝并拉黑" : "Block"}</button>
                                        <button data-testid={`ve-auth-allow-once-${req.id}`} disabled={busy} onClick={() => respond(req.id, "allow_once")} className="ve-auth-action ve-auth-action--secondary">{isZh ? "允许一次" : "Allow once"}</button>
                                        <button data-testid={`ve-auth-allow-long-${req.id}`} disabled={busy} onClick={() => respond(req.id, "allow_long")} className="ve-auth-action ve-auth-action--primary">{isZh ? "允许长期" : "Allow always"}</button>
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                </div>
            )}
        </div>
    );
}

// --- Blinking Indicator ---

export interface VEAuthBlinkingIndicatorProps {
    theme: Theme;
    lang?: string;
}

/**
 * Blinking indicator shown in the VE Tab top-right corner.
 * Starts blinking on ve:auth_request event, stops when all requests are handled.
 */
export function VEAuthBlinkingIndicator({ theme, lang }: VEAuthBlinkingIndicatorProps) {
    const [pendingCount, setPendingCount] = useState(0);
    const [visible, setVisible] = useState(true);
    const blinkRef = useRef<ReturnType<typeof setInterval> | null>(null);

    const isZh = !lang || lang.startsWith("zh");

    useEffect(() => {
        const handleAuthRequest = () => {
            setPendingCount((prev) => prev + 1);
        };

        // Listen for auth requests being handled (custom event from dialog)
        const handleAuthHandled = () => {
            setPendingCount((prev) => Math.max(0, prev - 1));
        };

        const unsub1 = EventsOn("ve:auth_request", handleAuthRequest);
        const unsub2 = EventsOn("ve:auth_handled", handleAuthHandled);

        return () => {
            if (typeof unsub1 === "function") unsub1();
            else EventsOff("ve:auth_request");
            if (typeof unsub2 === "function") unsub2();
            else EventsOff("ve:auth_handled");
        };
    }, []);

    // Blink animation
    useEffect(() => {
        if (pendingCount > 0) {
            blinkRef.current = setInterval(() => {
                setVisible((v) => !v);
            }, 500);
            return () => {
                if (blinkRef.current) {
                    clearInterval(blinkRef.current);
                    blinkRef.current = null;
                }
            };
        } else {
            if (blinkRef.current) {
                clearInterval(blinkRef.current);
                blinkRef.current = null;
            }
            setVisible(true);
            return undefined;
        }
    }, [pendingCount]);

    if (pendingCount === 0) return null;

    return (
        <span
            data-testid="ve-auth-blink-indicator"
            style={{
                display: "inline-flex",
                alignItems: "center",
                gap: 3,
                padding: "2px 6px",
                borderRadius: 10,
                background: visible ? (theme.errorBg || "#fef2f2") : "transparent",
                border: `1px solid ${visible ? (theme.errorBorder || "#fecaca") : "transparent"}`,
                fontSize: 10,
                color: theme.errorText || "#dc2626",
                transition: "opacity 0.2s",
                opacity: visible ? 1 : 0.3,
            }}
            title={isZh ? `${pendingCount} 个授权请求待处理` : `${pendingCount} pending auth request(s)`}
        >
            🔔 {pendingCount}
        </span>
    );
}
