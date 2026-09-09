import type { CSSProperties, ReactNode } from "react";

export type ChatBubbleSide = "left" | "right";
/**
 * The ordinary chat layout keeps the speaker label above the bubble, while
 * the execution layout puts the avatar beside the bubble.  Keep the two tail
 * placements explicit so the decorative pointer can follow the speaker in
 * either layout.
 */
export type ChatBubbleTailPlacement = "top" | "side";

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
    /** Where the tail points: above the bubble (label-above layout) or out of
     * the side toward the adjacent speaker avatar. */
    tailPlacement?: ChatBubbleTailPlacement;
    /** Optional control(s) pinned to the bubble's top-right (e.g. copy reply). */
    topRight?: ReactNode;
}

/** Diamond size — same as AI assistant chat bubbles. */
export const CHAT_BUBBLE_TAIL_SIZE = 10;
/** How far the diamond sits above the bubble top edge (points at the speaker name). */
export const CHAT_BUBBLE_TAIL_TOP = -6;
/**
 * WeChat-style side tail: an outlined up-pointing triangle sitting on the
 * name-side top corner of the bubble. The outer (outline) layer's base lands
 * exactly on the bubble's top border (TOP + HEIGHT === 0) so the outline
 * never crosses below the border line; the inner fill runs 1px past it to
 * hide the border under the tail.
 */
export const CHAT_BUBBLE_SIDE_TAIL_WIDTH = 12;
export const CHAT_BUBBLE_SIDE_TAIL_HEIGHT = 8;
export const CHAT_BUBBLE_SIDE_TAIL_TOP = -8;
/** Distance from the name-aligned corner (left for peer, right for user). */
export const CHAT_BUBBLE_SIDE_TAIL_INSET = 2;
/** Corner-tail outline: a plain up-pointing triangle. */
export const CHAT_BUBBLE_SIDE_TAIL_POLYGON =
    "polygon(0 100%, 50% 0, 100% 100%)";
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
 * Top-pointing diamond under the speaker name ("top") or a WeChat-style
 * outlined triangle on the name-side top corner ("side", used by AI assistant
 * bubbles in `renderMessage`).
 */
export function chatBubbleTailStyle(
    side: ChatBubbleSide,
    background: string,
    borderColor: string,
    placement: ChatBubbleTailPlacement = "top",
): CSSProperties {
    const isUser = side === "right";
    if (placement === "side") {
        // Outer layer of the corner tail: paints the outline in borderColor.
        // The inner fill (chatBubbleSideTailFillStyle) sits 1px in and runs
        // 1px below the base, so the bubble's top border is swallowed and the
        // slopes keep a clean 1px outline.
        const polygon = CHAT_BUBBLE_SIDE_TAIL_POLYGON;
        return {
            position: "absolute",
            top: CHAT_BUBBLE_SIDE_TAIL_TOP,
            width: CHAT_BUBBLE_SIDE_TAIL_WIDTH,
            height: CHAT_BUBBLE_SIDE_TAIL_HEIGHT,
            boxSizing: "border-box",
            background: borderColor,
            clipPath: polygon,
            WebkitClipPath: polygon,
            pointerEvents: "none",
            zIndex: 0,
            ...(isUser
                ? { right: CHAT_BUBBLE_SIDE_TAIL_INSET }
                : { left: CHAT_BUBBLE_SIDE_TAIL_INSET }),
        };
    }
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
        // Keep the legacy label-above tail behind in-flow text; side (corner)
        // tails use zIndex 0 so the fill paints over the bubble's top border.
        zIndex: -1,
        ...(isUser
            ? { right: CHAT_BUBBLE_TAIL_INSET }
            : { left: CHAT_BUBBLE_TAIL_INSET }),
    };
}

/** Inner fill for the side (corner) tail: leaves a 1px outline on the slopes
 * and tip while its base runs 1px past the outer base into the bubble. */
export function chatBubbleSideTailFillStyle(background: string): CSSProperties {
    return {
        position: "absolute",
        left: 1,
        top: 1,
        width: CHAT_BUBBLE_SIDE_TAIL_WIDTH - 2,
        height: CHAT_BUBBLE_SIDE_TAIL_HEIGHT,
        background,
        clipPath: CHAT_BUBBLE_SIDE_TAIL_POLYGON,
        WebkitClipPath: CHAT_BUBBLE_SIDE_TAIL_POLYGON,
        pointerEvents: "none",
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
    tailPlacement = "top",
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
                    className="mc-chat-bubble-tail"
                    style={chatBubbleTailStyle(side, fill, stroke, tailPlacement)}
                >
                    {tailPlacement === "side" && (
                        <span
                            aria-hidden="true"
                            data-testid={resolvedTailTestId ? `${resolvedTailTestId}-fill` : undefined}
                            style={chatBubbleSideTailFillStyle(fill)}
                        />
                    )}
                </span>
            )}
            {topRight ? (
                <div
                    className="mc-chat-bubble-top-right"
                    data-testid={testId ? `${testId}-top-right` : undefined}
                    style={{
                        position: "absolute",
                        top: 4,
                        right: 8,
                        zIndex: 3,
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
