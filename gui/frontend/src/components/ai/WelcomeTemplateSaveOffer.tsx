import { useCallback, useEffect, useRef, useState } from "react";
import type { Theme } from "./aiAssistantPanelTheme";

/** Default auto-dismiss for the post-send save offer (ms). */
export const WELCOME_TEMPLATE_OFFER_AUTO_DISMISS_MS = 12_000;

export type WelcomeTemplateSaveOfferProps = {
    lang: string;
    theme: Theme;
    title: string;
    onSave: () => void;
    onDismiss: () => void;
    /** Auto-dismiss delay; 0 / negative disables. */
    autoDismissMs?: number;
};

/**
 * Compact post-send banner: offer to persist the last welcome scenario prompt
 * as a custom template for one-click reuse later.
 *
 * Auto-dismisses after a short delay; hovering or focusing the banner pauses
 * the countdown so the user can still act.
 */
export function WelcomeTemplateSaveOfferBanner({
    lang,
    theme: t,
    title,
    onSave,
    onDismiss,
    autoDismissMs = WELCOME_TEMPLATE_OFFER_AUTO_DISMISS_MS,
}: WelcomeTemplateSaveOfferProps) {
    const isZh = !lang?.startsWith("en");
    const [paused, setPaused] = useState(false);
    const remainingRef = useRef(autoDismissMs);
    const lastTickRef = useRef(0);
    const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const onDismissRef = useRef(onDismiss);
    onDismissRef.current = onDismiss;

    const clearTimer = useCallback(() => {
        if (timerRef.current != null) {
            clearTimeout(timerRef.current);
            timerRef.current = null;
        }
    }, []);

    // Reset countdown whenever the offer title/body identity or duration changes.
    useEffect(() => {
        remainingRef.current = autoDismissMs > 0 ? autoDismissMs : 0;
        lastTickRef.current = Date.now();
        setPaused(false);
    }, [title, autoDismissMs]);

    useEffect(() => {
        if (autoDismissMs <= 0) return;
        if (paused) {
            // Freeze remaining when pausing mid-countdown.
            if (lastTickRef.current > 0) {
                remainingRef.current = Math.max(0, remainingRef.current - (Date.now() - lastTickRef.current));
                lastTickRef.current = 0;
            }
            clearTimer();
            return;
        }

        lastTickRef.current = Date.now();
        const left = remainingRef.current;
        if (left <= 0) {
            onDismissRef.current();
            return;
        }
        clearTimer();
        timerRef.current = setTimeout(() => {
            timerRef.current = null;
            onDismissRef.current();
        }, left);

        return clearTimer;
    }, [paused, title, autoDismissMs, clearTimer]);

    const pause = useCallback(() => setPaused(true), []);
    const resume = useCallback(() => setPaused(false), []);

    return (
        <div
            data-testid="welcome-template-save-offer"
            role="status"
            onMouseEnter={pause}
            onMouseLeave={resume}
            onFocusCapture={pause}
            onBlurCapture={(e) => {
                // Resume only when focus leaves the whole banner.
                if (!e.currentTarget.contains(e.relatedTarget as Node | null)) {
                    resume();
                }
            }}
            style={{
                display: "flex",
                flexDirection: "column",
                gap: 0,
                margin: "0 12px 8px",
                borderRadius: 8,
                border: `1px solid ${t.fieldBorder}`,
                background: t.fieldBg,
                color: t.text,
                fontFamily: "system-ui, -apple-system, sans-serif",
                overflow: "hidden",
            }}
        >
            <div style={{
                display: "flex",
                flexWrap: "wrap",
                alignItems: "center",
                justifyContent: "space-between",
                gap: 8,
                padding: "8px 12px",
            }}>
                <div style={{ minWidth: 0, flex: "1 1 180px" }}>
                    <div style={{ fontSize: 12, fontWeight: 600, lineHeight: 1.35 }}>
                        {isZh ? "保存为常用模板？" : "Save as a reusable template?"}
                    </div>
                    <div style={{
                        fontSize: 11,
                        color: t.textMuted,
                        lineHeight: 1.35,
                        marginTop: 2,
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                    }}>
                        {title}
                        {autoDismissMs > 0 && (
                            <span style={{ marginLeft: 6, opacity: 0.85 }}>
                                {paused
                                    ? (isZh ? "· 已暂停" : "· paused")
                                    : (isZh ? "· 稍后自动关闭" : "· auto-closes")}
                            </span>
                        )}
                    </div>
                </div>
                <div style={{ display: "flex", gap: 8, flexShrink: 0 }}>
                    <button
                        type="button"
                        data-testid="welcome-template-save-offer-dismiss"
                        onClick={onDismiss}
                        style={{
                            padding: "5px 10px",
                            borderRadius: 6,
                            border: `1px solid ${t.fieldBorder}`,
                            background: "transparent",
                            color: t.textMuted,
                            fontSize: 12,
                            cursor: "pointer",
                        }}
                    >
                        {isZh ? "忽略" : "Dismiss"}
                    </button>
                    <button
                        type="button"
                        data-testid="welcome-template-save-offer-confirm"
                        onClick={onSave}
                        style={{
                            padding: "5px 12px",
                            borderRadius: 6,
                            border: `1px solid ${t.sendBtnBorder || t.sendBtnBg}`,
                            background: t.sendBtnBg || t.btnColor,
                            color: t.sendBtnColor || "#fff",
                            fontSize: 12,
                            fontWeight: 600,
                            cursor: "pointer",
                        }}
                    >
                        {isZh ? "保存" : "Save"}
                    </button>
                </div>
            </div>
            {autoDismissMs > 0 && (
                <div
                    aria-hidden
                    style={{
                        height: 2,
                        background: t.divider || t.fieldBorder,
                    }}
                >
                    <div
                        data-testid="welcome-template-save-offer-progress"
                        style={{
                            height: "100%",
                            width: "100%",
                            transformOrigin: "left center",
                            background: t.sendBtnBg || t.btnColor,
                            opacity: 0.55,
                            animation: `welcomeTemplateOfferShrink ${autoDismissMs}ms linear forwards`,
                            animationPlayState: paused ? "paused" : "running",
                        }}
                    />
                </div>
            )}
            {/* Keyframes once per document; harmless if repeated. */}
            <style>{`
                @keyframes welcomeTemplateOfferShrink {
                    from { transform: scaleX(1); }
                    to { transform: scaleX(0); }
                }
            `}</style>
        </div>
    );
}
