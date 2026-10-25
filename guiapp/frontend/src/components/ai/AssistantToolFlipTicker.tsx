import React from "react";
import type { Theme } from "./aiAssistantPanelTheme";
import { formatAssistantToolFlip, sessionToolIconKind, type AssistantToolCall } from "./assistantToolCall";
import { CodingAgentProcessIcon } from "./CodingAgentProcessIcon";

const FLIP_MS = 1200;

type FlipLabel = ReturnType<typeof formatAssistantToolFlip>;

function prefersReducedMotion(): boolean {
    return typeof window.matchMedia === "function"
        && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

function flipHoverTitle(detail: string | undefined, shown: FlipLabel): string {
    const full = (detail || "").replace(/\s+/g, " ").trim();
    if (!full || shown.text.includes(full)) return shown.text;
    // The strip already shows the field pulled out of a JSON blob.
    if ((full.startsWith("{") && full.endsWith("}")) || (full.startsWith("[") && full.endsWith("]"))) return shown.text;
    const chars = Array.from(full);
    const clipped = chars.length > 160 ? `${chars.slice(0, 159).join("").trimEnd()}…` : full;
    return `${shown.name} (${clipped})`;
}

function FlipFace({ name, arg }: { name: string; arg: string }) {
    return (
        <>
            <span className="assistant-tool-flip-name">{name}</span>
            {arg ? <span className="assistant-tool-flip-arg">({arg})</span> : null}
        </>
    );
}

function FlapPlate({
    name,
    arg,
    half,
    className,
    testId,
    onAnimationEnd,
}: {
    name: string;
    arg: string;
    half: "top" | "bottom";
    className?: string;
    testId?: string;
    onAnimationEnd?: (event: React.AnimationEvent<HTMLDivElement>) => void;
}) {
    return (
        <div
            className={`assistant-tool-flap-leaf assistant-tool-flap-leaf--${half}${className ? ` ${className}` : ""}`}
            data-testid={testId}
            aria-hidden="true"
            onAnimationEnd={onAnimationEnd}
        >
            <div className="assistant-tool-flap-clip">
                <div className="assistant-tool-flap-leaf-inner">
                    <FlipFace name={name} arg={arg} />
                </div>
            </div>
        </div>
    );
}

/**
 * One-line strip under the thinking panel. A new call falls in like a station
 * split-flap: the top tile drops on the center hinge and the bottom tile clacks
 * into the new label. Argument updates for the same call replace the text in place.
 */
export const AssistantToolFlipTicker = React.memo(function AssistantToolFlipTicker({ call, theme }: { call: AssistantToolCall; theme: Theme }) {
    const formatted = React.useMemo(
        () => formatAssistantToolFlip(call),
        [call.name, call.action, call.detail],
    );
    const formattedRef = React.useRef(formatted);
    formattedRef.current = formatted;
    const [frame, setFrame] = React.useState<{ id: string; current: FlipLabel; previous: FlipLabel | null }>({
        id: call.id,
        current: formatted,
        previous: null,
    });
    const [entered, setEntered] = React.useState(() => prefersReducedMotion());

    React.useEffect(() => {
        if (entered) return;
        const timer = window.setTimeout(() => setEntered(true), FLIP_MS);
        return () => window.clearTimeout(timer);
    }, [entered]);

    React.useEffect(() => {
        const next = formattedRef.current;
        const nextId = call.id;
        setFrame((prev) => {
            if (prev.id === nextId) {
                return prev.current.text === next.text ? prev : { ...prev, current: next };
            }
            if (prefersReducedMotion()) return { id: nextId, current: next, previous: null };
            return { id: nextId, current: next, previous: prev.current };
        });
    }, [call.id, formatted.text]);

    React.useEffect(() => {
        if (!frame.previous) return;
        const token = frame.id;
        const timer = window.setTimeout(() => {
            setFrame((prev) => (prev.previous && prev.id === token ? { ...prev, previous: null } : prev));
        }, FLIP_MS + 60);
        return () => window.clearTimeout(timer);
    }, [frame.id, frame.previous]);

    const finishFlip = (event: React.AnimationEvent<HTMLDivElement>) => {
        if (event.target !== event.currentTarget) return;
        if (event.animationName !== "assistant-tool-flip-in") return;
        setEntered(true);
        const token = frame.id;
        setFrame((prev) => (prev.previous && prev.id === token ? { ...prev, previous: null } : prev));
    };

    const flipping = frame.previous != null;
    const iconKind = sessionToolIconKind(call);
    return (
        <div
            className={`assistant-tool-flip${flipping ? " is-flipping" : ""}`}
            data-testid="assistant-tool-flip"
            data-flipping={flipping ? "true" : "false"}
            data-dark={theme.isDark ? "true" : "false"}
            role="status"
            aria-live="polite"
            aria-label={frame.current.text}
            title={flipHoverTitle(call.detail, frame.current)}
            style={{
                color: theme.text,
                borderColor: theme.fieldBorder,
                background: theme.isDark ? "rgba(255,255,255,0.045)" : "rgba(15, 23, 42, 0.035)",
                ["--assistant-tool-flip-accent" as string]: theme.headingColor || theme.text,
                ["--assistant-tool-flip-muted" as string]: theme.textMuted,
            }}
        >
            {iconKind
                ? <CodingAgentProcessIcon kind={iconKind} className="assistant-tool-flip-icon" />
                : <span aria-hidden="true" className="assistant-tool-flip-dot" />}
            <div className="assistant-tool-flip-window">
                {frame.previous ? (
                    <>
                        <div className="assistant-tool-flip-face assistant-tool-flip-face--sizer" aria-hidden="true">
                            <span className="assistant-tool-flip-line">
                                <FlipFace name={frame.current.name} arg={frame.current.arg} />
                            </span>
                            <span className="assistant-tool-flip-line">
                                <FlipFace name={frame.previous.name} arg={frame.previous.arg} />
                            </span>
                        </div>
                        <div className="assistant-tool-flip-stage">
                            <FlapPlate name={frame.current.name} arg={frame.current.arg} half="top" />
                            <FlapPlate name={frame.previous.name} arg={frame.previous.arg} half="bottom" />
                            <FlapPlate
                                key={`out-${frame.id}`}
                                name={frame.previous.name}
                                arg={frame.previous.arg}
                                half="top"
                                className="assistant-tool-flip-face--out"
                                testId="assistant-tool-flip-out"
                            />
                            <FlapPlate
                                key={`in-${frame.id}`}
                                name={frame.current.name}
                                arg={frame.current.arg}
                                half="bottom"
                                className="assistant-tool-flip-face--in"
                                testId="assistant-tool-flip-in"
                                onAnimationEnd={finishFlip}
                            />
                        </div>
                    </>
                ) : (
                    <div className={`assistant-tool-flip-face${!entered ? " assistant-tool-flip-face--enter" : ""}`} data-testid="assistant-tool-flip-in" aria-hidden="true">
                        <span className="assistant-tool-flip-line">
                            <FlipFace name={frame.current.name} arg={frame.current.arg} />
                        </span>
                    </div>
                )}
            </div>
        </div>
    );
}, (prev, next) => prev.theme === next.theme
    && prev.call.id === next.call.id
    && prev.call.name === next.call.name
    && prev.call.action === next.call.action
    && prev.call.detail === next.call.detail);
