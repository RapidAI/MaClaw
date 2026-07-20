import type { CSSProperties, ReactNode } from "react";

export type ChatBubbleSide = "left" | "right";

export interface ChatBubbleFrameProps {
    /**
     * left  = peer/assistant (name on the left  → top tail inset from left)
     * right = user           (name on the right → top tail inset from right)
     * Geometry matches AI assistant `renderMessage` in aiAssistantMarkdown.tsx.
     */
    side: ChatBubbleSide;
    background: string;
    borderColor: string;
    style?: CSSProperties;
    className?: string;
    "data-testid"?: string;
    /** Override for the decorative tail node (AI assistant uses a legacy id). */
    tailTestId?: string;
    children?: ReactNode;
    /** Hide the name-pointing tail (status chips with no speaker label). Default true. */
    showTail?: boolean;
    /** Optional control(s) pinned to the bubble's top-right (e.g. copy reply). */
    topRight?: ReactNode;
}

/** Diamond size — same as AI assistant chat bubbles. */
export const CHAT_BUBBLE_TAIL_SIZE = 10;
/** How far the diamond sits above the bubble top edge (points at the speaker name). */
export const CHAT_BUBBLE_TAIL_TOP = -6;
/** Horizontal inset from the name-aligned edge (left for peer, right for user). */
export const CHAT_BUBBLE_TAIL_INSET = 13;
/**
 * Vertical gap between the speaker name row and the bubble.
 * Sized so the top tail (CHAT_BUBBLE_TAIL_TOP) sits cleanly under the label.
 */
export const CHAT_SPEAKER_LABEL_GAP = 3;

const DEFAULT_PADDING: CSSProperties["padding"] = "8px 12px";

/** Props that would break tail geometry if left to the caller. */
const CHROME_STYLE_KEYS = new Set([
    "position",
    "isolation",
    "boxSizing",
    "background",
    "backgroundColor",
    "backgroundImage",
    "border",
    "borderColor",
    "borderWidth",
    "borderStyle",
    "borderTop",
    "borderRight",
    "borderBottom",
    "borderLeft",
    "borderTopColor",
    "borderRightColor",
    "borderBottomColor",
    "borderLeftColor",
]);

const OVERFLOW_STYLE_KEYS = new Set(["overflow", "overflowX", "overflowY"]);

/** Split caller styles into safe layout vs controlled chrome inputs. */
export function sanitizeChatBubbleLayoutStyle(
    style: CSSProperties | undefined,
    options: { showTail: boolean },
): {
    layoutStyle: CSSProperties;
    padding: CSSProperties["padding"];
    borderRadius: CSSProperties["borderRadius"];
} {
    const { padding, borderRadius, ...rest } = style || {};
    const layoutStyle: CSSProperties = {};

    for (const [key, value] of Object.entries(rest)) {
        if (value === undefined) continue;
        if (CHROME_STYLE_KEYS.has(key)) continue;
        // Absolute tail paints above the box; clipping would hide it.
        if (options.showTail && OVERFLOW_STYLE_KEYS.has(key)) continue;
        (layoutStyle as Record<string, unknown>)[key] = value;
    }

    return { layoutStyle, padding, borderRadius };
}

/**
 * Top-pointing diamond under the speaker name — identical geometry to
 * AI assistant bubbles in `renderMessage` (aiAssistantMarkdown.tsx).
 */
export function chatBubbleTailStyle(
    side: ChatBubbleSide,
    background: string,
    borderColor: string,
): CSSProperties {
    const isUser = side === "right";
    return {
        position: "absolute",
        top: CHAT_BUBBLE_TAIL_TOP,
        width: CHAT_BUBBLE_TAIL_SIZE,
        height: CHAT_BUBBLE_TAIL_SIZE,
        boxSizing: "border-box",
        background,
        borderTop: `1px solid ${borderColor}`,
        borderLeft: `1px solid ${borderColor}`,
        transform: "rotate(45deg)",
        transformOrigin: "center",
        borderRadius: "1px 0 0 0",
        pointerEvents: "none",
        // Under in-flow text; above this bubble's background/border (see isolation).
        zIndex: -1,
        ...(isUser
            ? { right: CHAT_BUBBLE_TAIL_INSET }
            : { left: CHAT_BUBBLE_TAIL_INSET }),
    };
}

/** Soft tinted fill: `color-mix(in srgb, accent p%, fieldBg)`. */
export function tintedChatBubbleBackground(accent: string, fieldBg: string, percent: number): string {
    return `color-mix(in srgb, ${accent} ${percent}%, ${fieldBg})`;
}

/** Shared fill for user bubbles — same formula as AI assistant user messages. */
export function userChatBubbleBackground(sendBtnBg: string, fieldBg: string): string {
    return tintedChatBubbleBackground(sendBtnBg, fieldBg, 12);
}

/** Softer tint for local employee intro / welcome bubbles. */
export function localIntroChatBubbleBackground(sendBtnBg: string, fieldBg: string): string {
    return tintedChatBubbleBackground(sendBtnBg, fieldBg, 5);
}

/**
 * Chat bubble shell with a small top tail pointing at the speaker name label.
 * Shared by AI assistant, digital-employee direct chat, and group chat.
 */
export function ChatBubbleFrame({
    side,
    background,
    borderColor,
    style,
    className,
    "data-testid": testId,
    tailTestId,
    children,
    showTail = true,
    topRight,
}: ChatBubbleFrameProps) {
    const { layoutStyle, padding, borderRadius } = sanitizeChatBubbleLayoutStyle(style, { showTail });
    const resolvedTailTestId = tailTestId ?? (testId ? `${testId}-tail` : undefined);
    // Empty tokens would produce invalid CSS (`1px solid ` / missing fill); keep paint stable.
    const fill = background?.trim() || "transparent";
    const stroke = borderColor?.trim() || "transparent";

    return (
        <div
            data-testid={testId}
            className={className}
            style={{
                ...layoutStyle,
                position: "relative",
                isolation: "isolate",
                boxSizing: "border-box",
                overflow: showTail ? "visible" : layoutStyle.overflow,
                padding: padding ?? DEFAULT_PADDING,
                borderRadius: borderRadius ?? 8,
                background: fill,
                border: `1px solid ${stroke}`,
            }}
        >
            {showTail && (
                <span
                    aria-hidden="true"
                    data-testid={resolvedTailTestId}
                    data-side={side}
                    style={chatBubbleTailStyle(side, fill, stroke)}
                />
            )}
            {topRight ? (
                <div
                    data-testid={testId ? `${testId}-top-right` : undefined}
                    style={{
                        position: "absolute",
                        top: 4,
                        right: 4,
                        zIndex: 2,
                        display: "flex",
                        alignItems: "center",
                        gap: 2,
                        pointerEvents: "auto",
                    }}
                >
                    {topRight}
                </div>
            ) : null}
            {children}
        </div>
    );
}
