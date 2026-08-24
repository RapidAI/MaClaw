import React from "react";
import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantPanelTheme";
import { IconCheck, IconClipboard } from "./WorkbenchIcons";

/** Copy plain text to the system clipboard (with execCommand fallback). */
export async function copyTextToClipboard(text: string): Promise<boolean> {
    const value = String(text ?? "");
    // Reject empty / whitespace-only so the control never pastes blank noise.
    if (!value.trim()) return false;
    try {
        if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
            await navigator.clipboard.writeText(value);
            return true;
        }
    } catch {
        // fall through to legacy path
    }
    try {
        if (typeof document === "undefined") return false;
        const ta = document.createElement("textarea");
        ta.value = value;
        ta.setAttribute("readonly", "");
        ta.style.position = "fixed";
        ta.style.left = "-9999px";
        ta.style.top = "0";
        document.body.appendChild(ta);
        ta.focus();
        ta.select();
        ta.setSelectionRange(0, value.length);
        const ok = document.execCommand("copy");
        document.body.removeChild(ta);
        return ok;
    } catch {
        return false;
    }
}

/**
 * Top-right control on AI reply bubbles: copy the full reply text.
 * Shown only when there is non-empty content (hidden while empty streaming placeholder).
 */
export function AssistantReplyCopyButton({
    text,
    theme: t,
    lang = "en",
    messageId,
}: {
    text: string;
    theme: Theme;
    lang?: string;
    messageId?: string;
}) {
    const [state, setState] = React.useState<"idle" | "busy" | "ok" | "err">("idle");
    const [hovered, setHovered] = React.useState(false);
    const timerRef = React.useRef<ReturnType<typeof setTimeout> | null>(null);
    const mountedRef = React.useRef(true);
    const copyLabel = localizeText(lang, "Copy reply", "复制回复", "複製回覆");
    const copiedLabel = localizeText(lang, "Copied", "已复制", "已複製");
    const failedLabel = localizeText(lang, "Copy failed", "复制失败", "複製失敗");

    React.useEffect(() => {
        mountedRef.current = true;
        return () => {
            mountedRef.current = false;
            if (timerRef.current) clearTimeout(timerRef.current);
        };
    }, []);

    // Hooks must run unconditionally; hide after hooks when there is nothing to copy.
    const body = String(text ?? "");
    const hasBody = body.trim().length > 0;
    const title = state === "ok" ? copiedLabel : state === "err" ? failedLabel : copyLabel;

    if (!hasBody) return null;

    const emphasize = hovered || state === "ok" || state === "err";

    return (
        <button
            type="button"
            data-testid={messageId ? `assistant-chat-copy-${messageId}` : "assistant-chat-copy"}
            aria-label={title}
            title={title}
            disabled={state === "busy"}
            onMouseEnter={() => setHovered(true)}
            onMouseLeave={() => setHovered(false)}
            onFocus={() => setHovered(true)}
            onBlur={() => setHovered(false)}
            onClick={async (e) => {
                e.preventDefault();
                e.stopPropagation();
                if (state === "busy") return;
                setState("busy");
                const ok = await copyTextToClipboard(body);
                if (!mountedRef.current) return;
                setState(ok ? "ok" : "err");
                if (timerRef.current) clearTimeout(timerRef.current);
                timerRef.current = setTimeout(() => {
                    if (mountedRef.current) setState("idle");
                }, 1600);
            }}
            style={{
                position: "relative",
                display: "inline-flex",
                alignItems: "center",
                justifyContent: "center",
                width: 22,
                height: 22,
                padding: 0,
                borderRadius: 6,
                border: `1px solid ${t.fieldBorder}`,
                background: state === "ok"
                    ? "color-mix(in srgb, var(--theme-success, #16a34a) 14%, transparent)"
                    : "color-mix(in srgb, var(--theme-surface, #fff) 92%, transparent)",
                color: state === "ok"
                    ? "var(--theme-success, #16a34a)"
                    : state === "err"
                        ? "var(--theme-danger, #dc2626)"
                        : t.textMuted,
                cursor: state === "busy" ? "wait" : "pointer",
                opacity: state === "busy" ? 0.65 : emphasize ? 1 : 0.72,
                boxShadow: emphasize
                    ? "0 1px 3px color-mix(in srgb, #000 12%, transparent)"
                    : "0 1px 2px color-mix(in srgb, #000 6%, transparent)",
                transition: "background 120ms ease, color 120ms ease, opacity 120ms ease, box-shadow 120ms ease",
            }}
        >
            {/* Live region announces copy result without moving focus. */}
            <span
                aria-live="polite"
                style={{
                    position: "absolute",
                    width: 1,
                    height: 1,
                    padding: 0,
                    margin: -1,
                    overflow: "hidden",
                    clip: "rect(0, 0, 0, 0)",
                    whiteSpace: "nowrap",
                    border: 0,
                }}
            >
                {state === "ok" || state === "err" ? title : ""}
            </span>
            {state === "ok"
                ? <IconCheck size={13} color="currentColor" />
                : <IconClipboard size={13} color="currentColor" />}
        </button>
    );
}
