import { useCallback, useEffect, useRef, useState } from "react";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import type { Theme } from "./aiAssistantPanelTheme";

// --- Types ---

export interface AuthorizationRequest {
    id: string;
    requester_name: string;
    requester_machine_id: string;
    target_ve_id: string;
    target_ve_name: string;
    created_at: string;
    expires_at: string;
}

export interface VEAuthorizationDialogProps {
    theme: Theme;
    lang?: string;
    /** Override for testing — Wails binding */
    respondAuthRequest?: (requestId: string, decision: string) => Promise<void>;
}

// --- Component ---

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
                                <strong>{req.requester_name || req.requester_machine_id}</strong>
                            </div>
                            <div style={{ marginBottom: 4 }}>
                                <span style={{ color: theme.textMuted, fontSize: 12 }}>
                                    {isZh ? "机器ID：" : "Machine ID: "}
                                </span>
                                <span style={{ fontSize: 11, fontFamily: "monospace" }}>
                                    {req.requester_machine_id}
                                </span>
                            </div>
                            <div>
                                <span style={{ color: theme.textMuted, fontSize: 12 }}>
                                    {isZh ? "目标VE：" : "Target VE: "}
                                </span>
                                <strong>{req.target_ve_name}</strong>
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
                                    border: `1px solid ${theme.sendBtnBorder}`,
                                    background: theme.sendBtnColor,
                                    color: "#fff",
                                    cursor: responding === req.id ? "not-allowed" : "pointer",
                                    fontSize: 12,
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
