import React from "react";
import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantPanelTheme";
import { chatCodeWellStyle, codeFenceInnerStyle, codeFenceScrollUnderLangStyle } from "./assistantCodeFence";

let mermaidModule: any = null;
let mermaidModulePromise: Promise<any> | null = null;
// Mermaid stores its configuration globally. Serialising initialise + render
// keeps diagrams in different chat messages from borrowing each other's theme.
let renderQueue: Promise<void> = Promise.resolve();
const MAX_MERMAID_SOURCE_LENGTH = 50_000;
const MAX_MERMAID_NODE_COUNT = 750;
const MAX_MERMAID_EDGE_COUNT = 1_500;

// Mermaid draws at its intrinsic size, which reads as too small inside a wide
// chat column, so the first paint is already one step above 100%. The bounds
// keep the controls from shrinking a diagram into noise or growing it until the
// scroll container becomes useless.
export const DEFAULT_MERMAID_ZOOM = 1.25;
export const MIN_MERMAID_ZOOM = 0.5;
export const MAX_MERMAID_ZOOM = 3;
const MERMAID_ZOOM_STEP = 0.25;

function round2(value: number): number {
    return Math.round(value * 100) / 100;
}

export function clampMermaidZoom(zoom: number): number {
    if (!Number.isFinite(zoom)) return DEFAULT_MERMAID_ZOOM;
    return Math.min(MAX_MERMAID_ZOOM, Math.max(MIN_MERMAID_ZOOM, zoom));
}

/** One press of +/-. Rounding to two decimals stops 0.25 steps from drifting. */
export function stepMermaidZoom(current: number, direction: 1 | -1): number {
    return clampMermaidZoom(round2(clampMermaidZoom(current) + direction * MERMAID_ZOOM_STEP));
}

/**
 * Rewrite the root <svg> so the reader's zoom factor actually resizes the
 * drawing.
 *
 * Mermaid emits one of two size shapes: `width="100%"` plus an inline
 * `max-width: <natural>px` (the `useMaxWidth` default), or numeric width/height
 * attributes. The first pins a small diagram to its intrinsic width no matter
 * how wide the column is, so zoom must replace that cap with an explicit pixel
 * width; the second has to scale both axes or the aspect ratio breaks. Doing
 * this on the produced markup keeps zoom a string re-render instead of a second
 * Mermaid pass.
 */
export function scaleMermaidSvg(svg: string, zoom: number): string {
    const factor = clampMermaidZoom(zoom);
    const rootMatch = svg.match(/^<svg\b[^>]*>/);
    if (!rootMatch) return svg;
    const root = rootMatch[0];
    const naturalWidth = Number.parseFloat(
        root.match(/max-width:\s*([0-9.]+)px/i)?.[1] ?? root.match(/\swidth="([0-9.]+)"/)?.[1] ?? "",
    );
    const width = Number.isFinite(naturalWidth) && naturalWidth > 0
        ? `${round2(naturalWidth * factor)}px`
        : `${Math.round(factor * 100)}%`;
    let next = root.replace(/\swidth="[^"]*"/, ` width="${width}"`);
    if (next === root) next = root.replace(/^<svg\b/, `<svg width="${width}"`);
    const naturalHeight = Number.parseFloat(next.match(/\sheight="([0-9.]+)"/)?.[1] ?? "");
    if (Number.isFinite(naturalHeight) && naturalHeight > 0) {
        next = next.replace(/\sheight="[^"]*"/, ` height="${round2(naturalHeight * factor)}"`);
    }
    // The cap lives in the inline style; leaving it in place would clamp the
    // enlarged width straight back down.
    next = next.replace(/style="([^"]*)"/, (_match, style: string) => {
        const kept = String(style)
            .split(";")
            .map((part) => part.trim())
            .filter((part) => part && !/^max-width\s*:/i.test(part));
        return `style="${kept.join("; ")}"`;
    });
    return next + svg.slice(root.length);
}

function mermaidZoomBarStyle(t: Theme): React.CSSProperties {
    return {
        display: "flex",
        alignItems: "center",
        justifyContent: "flex-end",
        gap: 4,
        color: t.textMuted,
    };
}

function mermaidZoomButtonStyle(t: Theme, hovered: boolean, disabled: boolean): React.CSSProperties {
    return {
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        minWidth: 22,
        height: 22,
        padding: "0 6px",
        boxSizing: "border-box",
        borderRadius: 6,
        border: `1px solid ${t.codeBlockBorder}`,
        background: hovered && !disabled ? t.codeBlockBg : "transparent",
        color: t.text,
        fontFamily: "inherit",
        fontSize: 12,
        fontWeight: 600,
        lineHeight: 1,
        cursor: disabled ? "default" : "pointer",
        opacity: disabled ? 0.4 : hovered ? 1 : 0.75,
        transition: "opacity 120ms ease, background 120ms ease",
    };
}

function MermaidZoomButton({ testId, label, disabled, onClick, theme, children }: {
    testId: string;
    label: string;
    disabled?: boolean;
    onClick: () => void;
    theme: Theme;
    children: React.ReactNode;
}) {
    const [hovered, setHovered] = React.useState(false);
    return (
        <button
            type="button"
            data-testid={testId}
            aria-label={label}
            title={label}
            disabled={disabled}
            onMouseEnter={() => setHovered(true)}
            onMouseLeave={() => setHovered(false)}
            onFocus={() => setHovered(true)}
            onBlur={() => setHovered(false)}
            onClick={onClick}
            style={mermaidZoomButtonStyle(theme, hovered, disabled === true)}
        >
            {children}
        </button>
    );
}

function getMermaid(): Promise<any> {
    if (mermaidModule) return Promise.resolve(mermaidModule);
    if (mermaidModulePromise) return mermaidModulePromise;
    mermaidModulePromise = import("mermaid")
        .then((module) => {
            mermaidModule = module.default || module;
            return mermaidModule;
        })
        .catch((error) => {
            // A transient chunk/network failure should not permanently disable
            // diagrams for the rest of the assistant session.
            mermaidModulePromise = null;
            throw error;
        });
    return mermaidModulePromise;
}

function getMermaidTheme(theme: Theme) {
    return {
        startOnLoad: false,
        theme: "base",
        // Chat responses are untrusted model output. Strict mode asks Mermaid to
        // sanitise diagram labels and disables executable click handlers.
        securityLevel: "strict",
        suppressErrorRendering: true,
        themeVariables: {
            background: theme.bg,
            primaryColor: theme.fieldBg,
            primaryTextColor: theme.text,
            primaryBorderColor: theme.codeBlockBorder,
            secondaryColor: theme.codeBlockBg,
            tertiaryColor: theme.inputBarBg,
            lineColor: theme.textMuted,
            textColor: theme.text,
            mainBkg: theme.fieldBg,
            nodeBorder: theme.codeBlockBorder,
            clusterBkg: theme.codeBlockBg,
            clusterBorder: theme.codeBlockBorder,
        },
    };
}

/** Repair harmless casing errors commonly emitted by language models. */
export function sanitizeMermaidCode(raw: string): string {
    const diagramType = /^(\s*)(Graph|Flowchart|SequenceDiagram|StateDiagram|StateDiagram-v2|ClassDiagram|ErDiagram|Gantt|Pie|Gitgraph|Journey|Mindmap|Timeline|Quadrantchart|Sankey-beta|Xychart-beta)\b/i;
    const keyword = /^(\s*)(Subgraph|ClassDef|Style|Click|Note|Loop|Alt|Else|Opt|Par|Critical|Break|Rect|Activate|Deactivate|Direction)\b/i;
    return raw.split("\n").map((line) => {
        const diagramMatch = line.match(diagramType);
        if (diagramMatch) return `${diagramMatch[1]}${diagramMatch[2].toLowerCase()}${line.slice(diagramMatch[0].length)}`;
        if (/^(\s*)End\s*$/.test(line)) return line.replace(/End/i, "end");
        const keywordMatch = line.match(keyword);
        if (!keywordMatch) return line;
        const normalized = keywordMatch[2].toLowerCase() === "classdef" ? "classDef" : keywordMatch[2].toLowerCase();
        return `${keywordMatch[1]}${normalized}${line.slice(keywordMatch[0].length)}`;
    }).join("\n");
}

/**
 * Keep chat-supplied diagrams within a predictable rendering budget. This is
 * intentionally a simple source-level estimate: it runs before Mermaid parses
 * the diagram and protects the UI from accidentally generated giant graphs.
 */
export function exceedsMermaidRenderBudget(source: string): boolean {
    const nodeIds = new Set<string>();
    for (const match of source.matchAll(/(?:^|[\s;])([A-Za-z][\w-]*)(?:\s*(?:\[|\(|\{|-->|---|==>))/gm)) {
        nodeIds.add(match[1]);
        if (nodeIds.size > MAX_MERMAID_NODE_COUNT) return true;
    }
    const edgeCount = (source.match(/(?:-->|---|==>|-.->|==>)/g) || []).length;
    return edgeCount > MAX_MERMAID_EDGE_COUNT;
}

function getMermaidFallbackMessage(error: string): string {
    if (error.startsWith("The Mermaid") || error.startsWith("Unable to render this Mermaid diagram.")) return error;
    return `Unable to render this Mermaid diagram. ${error}`;
}

function errorMessage(reason: unknown): string {
    return reason instanceof Error && reason.message ? reason.message : "Unable to render this Mermaid diagram.";
}

async function renderDiagram(
    mermaid: any,
    id: string,
    code: string,
    workspace: HTMLDivElement | null,
    theme: Theme,
    cancelled: () => boolean,
): Promise<string | null> {
    let resolveResult!: (svg: string | null) => void;
    let rejectResult!: (reason: unknown) => void;
    const result = new Promise<string | null>((resolve, reject) => {
        resolveResult = resolve;
        rejectResult = reject;
    });

    renderQueue = renderQueue.catch(() => undefined).then(async () => {
        if (cancelled()) {
            resolveResult(null);
            return;
        }
        try {
            mermaid.initialize(getMermaidTheme(theme));
            const { svg } = await mermaid.render(id, code, workspace ?? undefined);
            resolveResult(svg);
        } catch (error) {
            rejectResult(error);
        }
    });
    return result;
}

export function isMermaidCodeFence(language: string): boolean {
    return language.trim().split(/\s+/, 1)[0]?.toLowerCase() === "mermaid";
}

/** Render a Mermaid fenced block while keeping an inspectable source fallback. */
export function AssistantMermaidDiagram({ code, theme, lang }: { code: string; theme: Theme; lang?: string }) {
    const [svg, setSvg] = React.useState("");
    const [error, setError] = React.useState("");
    const [zoom, setZoom] = React.useState(DEFAULT_MERMAID_ZOOM);
    const workspaceRef = React.useRef<HTMLDivElement>(null);
    const idRef = React.useRef(`assistant-mermaid-${Math.random().toString(36).slice(2, 10)}`);
    const zoomedSvg = React.useMemo(() => (svg ? scaleMermaidSvg(svg, zoom) : ""), [svg, zoom]);
    const zoomGroupLabel = localizeText(lang, "Diagram zoom", "图表缩放", "圖表縮放");
    const zoomOutLabel = localizeText(lang, "Zoom out", "缩小图表", "縮小圖表");
    const zoomInLabel = localizeText(lang, "Zoom in", "放大图表", "放大圖表");
    const zoomResetLabel = localizeText(lang, "Reset diagram size", "重置图表大小", "重設圖表大小");

    React.useEffect(() => {
        let cancelled = false;
        setSvg("");
        setError("");
        const source = sanitizeMermaidCode(code).trim();
        if (!source) {
            setError("The Mermaid code block is empty.");
            return () => { cancelled = true; };
        }
        if (source.length > MAX_MERMAID_SOURCE_LENGTH) {
            setError(`The Mermaid source exceeds the ${MAX_MERMAID_SOURCE_LENGTH.toLocaleString()} character rendering limit.`);
            return () => { cancelled = true; };
        }
        if (exceedsMermaidRenderBudget(source)) {
            setError(`The Mermaid diagram exceeds the ${MAX_MERMAID_NODE_COUNT} node or ${MAX_MERMAID_EDGE_COUNT.toLocaleString()} edge rendering limit.`);
            return () => { cancelled = true; };
        }
        void getMermaid().then(async (mermaid) => {
            const rendered = await renderDiagram(
                mermaid,
                idRef.current,
                source,
                workspaceRef.current,
                theme,
                () => cancelled,
            );
            if (!cancelled && rendered) setSvg(rendered);
        }).catch((reason: unknown) => {
            if (!cancelled) {
                setError(errorMessage(reason));
                if (workspaceRef.current) workspaceRef.current.innerHTML = "";
            }
        });
        return () => { cancelled = true; };
    }, [code, theme]);

    return (
        <>
            <div ref={workspaceRef} aria-hidden="true" className="amd-measure-host" />
            {svg ? (
                <div data-testid="assistant-mermaid-figure" style={{ margin: "8px 0" }}>
                    <div role="group" aria-label={zoomGroupLabel} style={mermaidZoomBarStyle(theme)}>
                        <MermaidZoomButton
                            testId="assistant-mermaid-zoom-out"
                            label={zoomOutLabel}
                            disabled={zoom <= MIN_MERMAID_ZOOM}
                            onClick={() => setZoom((current) => stepMermaidZoom(current, -1))}
                            theme={theme}
                        >
                            {"−"}
                        </MermaidZoomButton>
                        <MermaidZoomButton
                            testId="assistant-mermaid-zoom-reset"
                            label={zoomResetLabel}
                            onClick={() => setZoom(DEFAULT_MERMAID_ZOOM)}
                            theme={theme}
                        >
                            {`${Math.round(zoom * 100)}%`}
                        </MermaidZoomButton>
                        <MermaidZoomButton
                            testId="assistant-mermaid-zoom-in"
                            label={zoomInLabel}
                            disabled={zoom >= MAX_MERMAID_ZOOM}
                            onClick={() => setZoom((current) => stepMermaidZoom(current, 1))}
                            theme={theme}
                        >
                            {"+"}
                        </MermaidZoomButton>
                    </div>
                    <div
                        data-testid="assistant-mermaid-diagram"
                        role="img"
                        aria-label="Mermaid diagram"
                        style={{ maxWidth: "100%", overflow: "auto", contain: "content", color: theme.text }}
                        dangerouslySetInnerHTML={{ __html: zoomedSvg }}
                    />
                </div>
            ) : error ? (
                <div data-testid="assistant-mermaid-fallback" style={chatCodeWellStyle(theme)}>
                    <div role="status" style={{ margin: 0, padding: "8px 12px 0", color: theme.text, fontSize: "12px", lineHeight: 1.4, whiteSpace: "normal", overflowWrap: "anywhere" }}>{getMermaidFallbackMessage(error)}</div>
                    <pre style={codeFenceScrollUnderLangStyle}>
                        <code style={codeFenceInnerStyle}>{code}</code>
                    </pre>
                </div>
            ) : (
                <div data-testid="assistant-mermaid-loading" style={{ margin: "8px 0", padding: "10px 12px", color: theme.textMuted, fontSize: "0.9em" }}>Rendering diagram…</div>
            )}
        </>
    );
}
