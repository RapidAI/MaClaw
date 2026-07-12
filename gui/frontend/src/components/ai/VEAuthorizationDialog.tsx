import { useCallback, useEffect, useRef, useState, type MutableRefObject } from "react";
import { EventsOn, EventsOff, EventsEmit } from "../../../wailsjs/runtime";
import type { Theme } from "./aiAssistantPanelTheme";
import { looksLikeRawParticipantId } from "./localAIIdentity";
import { getWailsAppModule } from "../../utils/wailsAppModule";
import { IconBell } from "./WorkbenchIcons";

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
type AuthRequestSoundPreset = "classic" | "soft" | "bright" | "pulse" | "urgent";

const AUTH_REQUEST_SOUND_DURATION_MS = 9000;
const AUTH_HANDLED_LOCAL_EVENT = "ve:auth_handled:local";
const authRequestSoundPresets: Record<AuthRequestSoundPreset, { freqs: number[]; toneMs: number; gapMs: number; pauseMs: number; gain: number; wave: OscillatorType }> = {
    classic: { freqs: [440, 480], toneMs: 360, gapMs: 110, pauseMs: 920, gain: 0.055, wave: "sine" },
    soft: { freqs: [330, 392], toneMs: 420, gapMs: 130, pauseMs: 1080, gain: 0.04, wave: "sine" },
    bright: { freqs: [660, 740], toneMs: 260, gapMs: 90, pauseMs: 820, gain: 0.045, wave: "triangle" },
    pulse: { freqs: [520, 520, 390], toneMs: 160, gapMs: 80, pauseMs: 720, gain: 0.05, wave: "square" },
    urgent: { freqs: [780, 620, 780], toneMs: 140, gapMs: 70, pauseMs: 520, gain: 0.055, wave: "sawtooth" },
};

let activeAuthRequestSoundStop: (() => void) | null = null;
let authRequestSoundGeneration = 0;

function normalizeAuthRequestSoundPreset(value: unknown): AuthRequestSoundPreset {
    const preset = String(value || "").trim().toLowerCase() as AuthRequestSoundPreset;
    return authRequestSoundPresets[preset] ? preset : "classic";
}

async function loadAuthRequestSoundConfig(): Promise<{ muted: boolean; preset: AuthRequestSoundPreset }> {
    try {
        const mod = await getWailsAppModule();
        if (typeof (mod as any).LoadConfig === "function") {
            const cfg = await (mod as any).LoadConfig();
            const gd = cfg?.group_discussion || cfg?.GroupDiscussion || {};
            return {
                muted: Boolean(gd.auth_request_sound_muted ?? gd.AuthRequestSoundMuted),
                preset: normalizeAuthRequestSoundPreset(gd.auth_request_sound_preset ?? gd.AuthRequestSoundPreset),
            };
        }
    } catch {
        // Missing Wails binding or config read failure should not block auth UI.
    }
    return { muted: false, preset: "classic" };
}

function stopActiveAuthRequestSound() {
    if (activeAuthRequestSoundStop) {
        activeAuthRequestSoundStop();
        activeAuthRequestSoundStop = null;
    }
}

function stopAuthRequestSound() {
    authRequestSoundGeneration += 1;
    stopActiveAuthRequestSound();
}

function scheduleAuthTone(ctx: AudioContext, preset: typeof authRequestSoundPresets[AuthRequestSoundPreset], freq: number, delayMs: number) {
    const start = ctx.currentTime + delayMs / 1000;
    const stop = start + preset.toneMs / 1000;
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.type = preset.wave;
    osc.frequency.setValueAtTime(freq, start);
    gain.gain.setValueAtTime(0.0001, start);
    gain.gain.exponentialRampToValueAtTime(preset.gain, start + 0.025);
    gain.gain.exponentialRampToValueAtTime(0.0001, stop);
    osc.connect(gain);
    gain.connect(ctx.destination);
    osc.start(start);
    osc.stop(stop + 0.03);
}

async function playAuthRequestSound() {
    const AudioContextCtor = window.AudioContext || (window as any).webkitAudioContext;
    if (!AudioContextCtor) return;
    const generation = authRequestSoundGeneration + 1;
    authRequestSoundGeneration = generation;
    stopActiveAuthRequestSound();
    const cfg = await loadAuthRequestSoundConfig();
    if (generation !== authRequestSoundGeneration) return;
    if (cfg.muted) return;
    let ctx: AudioContext | null = null;
    try {
        const audioCtx = new AudioContextCtor() as AudioContext;
        ctx = audioCtx;
        if (audioCtx.state === "suspended") {
            try { await audioCtx.resume(); } catch { /* browser may block autoplay */ }
        }
        if (generation !== authRequestSoundGeneration) {
            try { void audioCtx.close(); } catch { /* noop */ }
            return;
        }
        const preset = authRequestSoundPresets[cfg.preset];
        let stopped = false;
        const patternMs = preset.freqs.length * (preset.toneMs + preset.gapMs) + preset.pauseMs;
        const playPattern = () => {
            if (stopped) return;
            preset.freqs.forEach((freq, index) => scheduleAuthTone(audioCtx, preset, freq, index * (preset.toneMs + preset.gapMs)));
        };
        playPattern();
        const interval = window.setInterval(playPattern, patternMs);
        const timeout = window.setTimeout(stopAuthRequestSound, AUTH_REQUEST_SOUND_DURATION_MS);
        if (generation !== authRequestSoundGeneration) {
            window.clearInterval(interval);
            window.clearTimeout(timeout);
            try { void audioCtx.close(); } catch { /* noop */ }
            return;
        }
        activeAuthRequestSoundStop = () => {
            stopped = true;
            window.clearInterval(interval);
            window.clearTimeout(timeout);
            try { void audioCtx.close(); } catch { /* noop */ }
        };
    } catch {
        if (ctx) {
            try { void ctx.close(); } catch { /* noop */ }
        }
        // Sound is best-effort; never let audio device errors affect auth handling.
    }
}

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
    const requestIdsRef = useRef<Set<string>>(new Set());

    const isZh = !lang || lang.startsWith("zh");

    // Listen for auth request events
    useEffect(() => {
        mountedRef.current = true;

        const handleAuthRequest = (data: any) => {
            if (!mountedRef.current) return;
            const req = normalizeAuthEvent(data);
            if (req.id) {
                if (requestIdsRef.current.has(req.id)) return;
                requestIdsRef.current.add(req.id);
                void playAuthRequestSound();
                setRequests((prev) => prev.some((r) => r.id === req.id) ? prev : [...prev, req]);
            }
        };

        const unsub = EventsOn("ve:auth_request", handleAuthRequest);

        return () => {
            mountedRef.current = false;
            requestIdsRef.current.clear();
            stopAuthRequestSound();
            if (typeof unsub === "function") unsub();
            else EventsOff("ve:auth_request");
        };
    }, []);

    useEffect(() => {
        if (requests.length === 0) stopAuthRequestSound();
    }, [requests.length]);

    // Handle allow/deny
    const handleDecision = useCallback(
        async (requestId: string, decision: "allow" | "deny") => {
            setResponding(requestId);
            try {
                if (respondAuthRequest) {
                    await respondAuthRequest(requestId, decision);
                } else {
                    const mod = await getWailsAppModule();
                    await (mod as any).RespondAuthRequest(requestId, decision);
                }
                emitAuthHandled(requestId);
                requestIdsRef.current.delete(requestId);
                if (mountedRef.current) setRequests((prev) => prev.filter((r) => r.id !== requestId));
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
                    {isZh ? "授权请求" : "Authorization Request"}
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
                                    border: `1px solid ${theme.errorBorder || "rgba(196, 61, 52, 0.24)"}`,
                                    background: theme.errorBg || "#fbf1f0",
                                    color: theme.errorText || "#c43d34",
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

function emitAuthHandled(requestId: string) {
    try { EventsEmit("ve:auth_handled", { request_id: requestId }); } catch { /* frontend-only fallback */ }
    try { window.dispatchEvent(new CustomEvent(AUTH_HANDLED_LOCAL_EVENT, { detail: { request_id: requestId } })); } catch { /* noop */ }
}

function emitRemovedAuthRequests(previous: AuthorizationRequest[], next: AuthorizationRequest[]) {
    const activeIds = new Set(next.map((request) => request.id));
    previous.forEach((request) => {
        if (request.id && !activeIds.has(request.id)) emitAuthHandled(request.id);
    });
}

function removeTrackedAuthRequestIds(
    trackedIds: MutableRefObject<Set<string>>,
    previous: AuthorizationRequest[],
    next: AuthorizationRequest[],
) {
    const activeIds = new Set(next.map((request) => request.id));
    previous.forEach((request) => {
        if (request.id && !activeIds.has(request.id)) trackedIds.current.delete(request.id);
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
    const [popoverFrame, setPopoverFrame] = useState<{ left: number; top: number; width: number; maxHeight: number } | null>(null);
    const mountedRef = useRef(true);
    const respondingRef = useRef<string | null>(null);
    const requestIdsRef = useRef<Set<string>>(new Set());
    const triggerRef = useRef<HTMLButtonElement | null>(null);
    const isZh = !lang || lang.startsWith("zh");

    const updatePopoverFrame = useCallback(() => {
        const trigger = triggerRef.current;
        if (!trigger) return;
        const rect = trigger.getBoundingClientRect();
        const titleBar = trigger.closest('[data-testid="ai-title-bar"]') as HTMLElement | null;
        const titleBarRect = titleBar?.getBoundingClientRect();
        const margin = 14;
        const minLeft = Math.max(margin, (titleBarRect?.left ?? 0) + 12);
        const viewportWidth = window.innerWidth || document.documentElement.clientWidth || 0;
        const viewportHeight = window.innerHeight || document.documentElement.clientHeight || 0;
        const availableWidth = Math.max(280, viewportWidth - minLeft - margin);
        const width = Math.min(390, availableWidth);
        const maxLeft = Math.max(minLeft, viewportWidth - margin - width);
        const left = Math.min(Math.max(rect.right - width, minLeft), maxLeft);
        const minPopoverHeight = 160;
        const preferredTop = rect.bottom + 8;
        const maxTop = Math.max(margin, viewportHeight - margin - minPopoverHeight);
        const top = Math.min(Math.max(margin, preferredTop), maxTop);
        const maxHeight = Math.max(minPopoverHeight, viewportHeight - top - margin);
        setPopoverFrame({ left, top, width, maxHeight });
    }, []);

    useEffect(() => {
        mountedRef.current = true;
        const handleAuthRequest = (data: any) => {
            if (!mountedRef.current) return;
            const req = normalizeAuthEvent(data);
            if (!req.id) return;
            if (requestIdsRef.current.has(req.id)) return;
            requestIdsRef.current.add(req.id);
            void playAuthRequestSound();
            setRequests((prev) => prev.some((item) => item.id === req.id) ? prev : [...prev, req]);
        };
        const unsub = EventsOn("ve:auth_request", handleAuthRequest);
        return () => {
            mountedRef.current = false;
            requestIdsRef.current.clear();
            stopAuthRequestSound();
            if (typeof unsub === "function") unsub();
            else EventsOff("ve:auth_request");
        };
    }, []);

    useEffect(() => {
        if (requests.length === 0) {
            setOpen(false);
            stopAuthRequestSound();
        }
    }, [requests.length]);

    useEffect(() => {
        if (!open) return undefined;
        updatePopoverFrame();
        window.addEventListener("resize", updatePopoverFrame);
        window.addEventListener("scroll", updatePopoverFrame, true);
        return () => {
            window.removeEventListener("resize", updatePopoverFrame);
            window.removeEventListener("scroll", updatePopoverFrame, true);
        };
    }, [open, requests.length, updatePopoverFrame]);

    useEffect(() => {
        if (requests.length === 0) return undefined;
        const now = Date.now();
        const active = pruneExpiredAuthRequests(requests, now);
        if (active.length !== requests.length) {
            emitRemovedAuthRequests(requests, active);
            removeTrackedAuthRequestIds(requestIdsRef, requests, active);
            setRequests(active);
            return undefined;
        }
        const nextExpiry = active
            .map(authRequestExpiryMs)
            .filter((value): value is number => value !== null && value > now)
            .sort((a, b) => a - b)[0];
        if (!nextExpiry) return undefined;
        const timer = window.setTimeout(() => {
            if (mountedRef.current) {
                setRequests((prev) => {
                    const active = pruneExpiredAuthRequests(prev);
                    emitRemovedAuthRequests(prev, active);
                    removeTrackedAuthRequestIds(requestIdsRef, prev, active);
                    return active;
                });
            }
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
                const mod = await getWailsAppModule();
                await (mod as any).RespondAuthRequest(requestId, decision);
            }
            emitAuthHandled(requestId);
            requestIdsRef.current.delete(requestId);
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
                ref={triggerRef}
                type="button"
                className="ai-titlebar-tool ve-auth-request-trigger ve-auth-request-trigger--blink"
                data-testid="ve-auth-request-trigger"
                aria-label={isZh ? `${requests.length} 个访问请求待确认` : `${requests.length} pending access request(s)`}
                onMouseDown={(e) => { if (inline) { e.preventDefault(); e.stopPropagation(); updatePopoverFrame(); setOpen((value) => !value); } }}
                onClick={(e) => { if (!inline) { e.preventDefault(); e.stopPropagation(); updatePopoverFrame(); setOpen((value) => !value); } }}
                style={{
                    minWidth: 30,
                    height: 28,
                    borderRadius: 6,
                    border: `1px solid ${theme.errorBorder || theme.titleBarBorder}`,
                    background: theme.errorBg || "rgba(196, 61, 52, 0.10)",
                    color: theme.errorText || "#c43d34",
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
                        position: "fixed",
                        left: popoverFrame ? popoverFrame.left : 14,
                        top: popoverFrame ? popoverFrame.top : 52,
                        width: popoverFrame ? popoverFrame.width : 390,
                        maxWidth: "calc(100vw - 28px)",
                        maxHeight: popoverFrame ? Math.min(460, popoverFrame.maxHeight) : "min(460px, calc(100vh - 80px))",
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
                            {respondError && <div role="alert" style={{ fontSize: 12, lineHeight: 1.45, color: theme.errorText || "#c43d34", marginTop: 6 }}>{respondError}</div>}
                        </div>
                        <button type="button" onClick={() => setOpen(false)} aria-label={isZh ? "关闭" : "Close"} style={{ border: "none", background: "transparent", color: theme.textMuted, cursor: "pointer", fontSize: 12, lineHeight: 1, fontWeight: 700 }}>X</button>
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
    const pendingIdsRef = useRef<Set<string>>(new Set());

    const isZh = !lang || lang.startsWith("zh");

    useEffect(() => {
        const requestIdFromEvent = (data: any): string => {
            const detail = data instanceof CustomEvent ? data.detail : data;
            return normalizeAuthEvent(detail).id;
        };
        const handleAuthRequest = (data: any) => {
            const requestId = requestIdFromEvent(data);
            if (requestId) {
                if (pendingIdsRef.current.has(requestId)) return;
                pendingIdsRef.current.add(requestId);
            }
            setPendingCount((prev) => prev + 1);
        };

        // Listen for auth requests being handled (custom event from dialog)
        const handleAuthHandled = (data: any) => {
            const requestId = requestIdFromEvent(data);
            if (requestId) {
                if (!pendingIdsRef.current.delete(requestId)) return;
            } else if (pendingIdsRef.current.size > 0) {
                const firstId = pendingIdsRef.current.values().next().value;
                if (firstId) pendingIdsRef.current.delete(firstId);
            }
            setPendingCount((prev) => Math.max(0, prev - 1));
        };

        const unsub1 = EventsOn("ve:auth_request", handleAuthRequest);
        const unsub2 = EventsOn("ve:auth_handled", handleAuthHandled);
        window.addEventListener(AUTH_HANDLED_LOCAL_EVENT, handleAuthHandled);

        return () => {
            pendingIdsRef.current.clear();
            window.removeEventListener(AUTH_HANDLED_LOCAL_EVENT, handleAuthHandled);
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
                background: visible ? (theme.errorBg || "#fbf1f0") : "transparent",
                border: `1px solid ${visible ? (theme.errorBorder || "rgba(196, 61, 52, 0.24)") : "transparent"}`,
                fontSize: 10,
                color: theme.errorText || "#c43d34",
                transition: "opacity 0.2s",
                opacity: visible ? 1 : 0.3,
            }}
            title={isZh ? `${pendingCount} 个授权请求待处理` : `${pendingCount} pending auth request(s)`}
        >
            <span style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
                <IconBell size={12} color="currentColor" />
                {pendingCount}
            </span>
        </span>
    );
}
