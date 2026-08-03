import React from "react";
import type { Theme } from "./aiAssistantPanelTheme";

let mermaidModule: any = null;
let mermaidModulePromise: Promise<any> | null = null;
// Mermaid stores its configuration globally. Serialising initialise + render
// keeps diagrams in different chat messages from borrowing each other's theme.
let renderQueue: Promise<void> = Promise.resolve();
const MAX_MERMAID_SOURCE_LENGTH = 50_000;
const MAX_MERMAID_NODE_COUNT = 750;
const MAX_MERMAID_EDGE_COUNT = 1_500;

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
export function AssistantMermaidDiagram({ code, theme }: { code: string; theme: Theme }) {
    const [svg, setSvg] = React.useState("");
    const [error, setError] = React.useState("");
    const workspaceRef = React.useRef<HTMLDivElement>(null);
    const idRef = React.useRef(`assistant-mermaid-${Math.random().toString(36).slice(2, 10)}`);

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
            <div ref={workspaceRef} aria-hidden="true" style={{ position: "fixed", inset: 0, width: 0, height: 0, overflow: "hidden", pointerEvents: "none" }} />
            {svg ? (
                <div
                    data-testid="assistant-mermaid-diagram"
                    role="img"
                    aria-label="Mermaid diagram"
                    style={{ margin: "8px 0", maxWidth: "100%", overflow: "auto", contain: "content", color: theme.text }}
                    dangerouslySetInnerHTML={{ __html: svg }}
                />
            ) : error ? (
                <pre data-testid="assistant-mermaid-fallback" style={{ background: theme.codeBlockBg, border: `1px solid ${theme.codeBlockBorder}`, borderRadius: "6px", padding: "10px 12px", margin: "6px 0", overflowX: "auto", color: theme.codeText, lineHeight: 1.6 }}>
                    <div role="status" style={{ marginBottom: 6, color: theme.textMuted, fontFamily: "inherit", fontSize: "0.9em" }}>{getMermaidFallbackMessage(error)}</div>
                    <code>{code}</code>
                </pre>
            ) : (
                <div data-testid="assistant-mermaid-loading" style={{ margin: "8px 0", padding: "10px 12px", color: theme.textMuted, fontSize: "0.9em" }}>Rendering diagram…</div>
            )}
        </>
    );
}
