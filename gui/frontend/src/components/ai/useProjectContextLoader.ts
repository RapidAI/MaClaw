/**
 * useProjectContextLoader - loads project context when a project tab is first opened.
 *
 * Calls the LoadProjectContext Wails binding and formats the result as a system
 * message to inject into the tab's initial conversation. Handles timeout (>2s)
 * with a placeholder message and automatic retry.
 *
 * Requirements: 4.1, 4.2, 4.3
 */
import { useCallback, useEffect, useRef } from "react";
import { LoadProjectContext } from "../../../wailsjs/go/main/App";
import type { main } from "../../../wailsjs/go/models";

/** Timeout in milliseconds for the LoadProjectContext call */
const LOAD_CONTEXT_TIMEOUT_MS = 2000;
/** Maximum number of retries after timeout */
const MAX_RETRIES = 2;
/** Delay between retries in milliseconds */
const RETRY_DELAY_MS = 1000;

export interface ProjectContextMessage {
    id: string;
    role: "system";
    content: string;
    timestamp: number;
    isProjectContext?: boolean;
}

/**
 * Format a ProjectContextSummary into a compact user-facing system message.
 * Raw paths and evidence references are kept in structured fields for agent use,
 * but are intentionally not rendered into the chat transcript.
 */
function looksLikeTechnicalSource(value: string): boolean {
    const text = value.trim();
    return /^[A-Za-z]:[\\/]/.test(text)
        || /^[-*]?\s*`?[A-Za-z]:[\\/]/.test(text)
        || /^[-*]?\s*`?(\/|~\/)/.test(text)
        || text.includes(".maclaw")
        || text.includes("read_file")
        || text.startsWith("Source task:")
        || text === "Forked from recent task."
        || text === "Opened from task management.";
}

function simplifyProjectProgress(value?: string): string {
    if (!value) return "";
    const lines = value.replace(/\r\n/g, "\n").split("\n");
    const kept: string[] = [];
    let skippingTechnicalSection = false;

    for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed) {
            if (kept.length > 0 && kept[kept.length - 1] !== "") kept.push("");
            continue;
        }
        if (/^\*\*(关键产出物|最近产物来源|Key artifacts|Recent artifact sources)[:：]?\*\*/i.test(trimmed)) {
            skippingTechnicalSection = true;
            continue;
        }
        if (/^\*\*.+\*\*/.test(trimmed)) {
            skippingTechnicalSection = false;
        }
        if (skippingTechnicalSection || looksLikeTechnicalSource(trimmed)) continue;
        kept.push(trimmed);
    }

    const compact = kept.join("\n").replace(/\n{3,}/g, "\n\n").trim();
    if (compact.length <= 520) return compact;
    return `${compact.slice(0, 520).trimEnd()}...`;
}

function formatProjectContextMessage(summary: main.ProjectContextSummary): string {
    const parts: string[] = [];
    const projectName = summary.project_name || "当前任务";
    const progress = simplifyProjectProgress(summary.recent_progress);
    const hasHiddenEvidence = (summary.key_artifacts?.length || 0) > 0 || (summary.recent_artifacts?.length || 0) > 0;

    parts.push(`已恢复任务上下文：${projectName}`);

    if (progress) {
        parts.push("");
        parts.push(`最近进展：${progress}`);
    }

    if (summary.active_workflow) {
        parts.push("");
        parts.push(`当前流程：${summary.active_workflow}`);
    }

    if (hasHiddenEvidence) {
        parts.push("");
        parts.push("相关产物和来源已载入，AI 会参考。可以直接继续问。");
    }

    return parts.join("\n");
}

/**
 * Create a placeholder message shown while context is loading.
 */
function createPlaceholderMessage(projectName: string): ProjectContextMessage {
    return {
        id: `project-context-loading-${Date.now()}`,
        role: "system",
        content: `正在恢复任务上下文：${projectName}...`,
        timestamp: Date.now(),
        isProjectContext: true,
    };
}

/**
 * Create the final context message from a loaded summary.
 */
function createContextMessage(summary: main.ProjectContextSummary): ProjectContextMessage {
    return {
        id: `project-context-${Date.now()}`,
        role: "system",
        content: formatProjectContextMessage(summary),
        timestamp: Date.now(),
        isProjectContext: true,
    };
}

/**
 * Create a fallback message when context loading fails.
 */
function createFailedMessage(projectName: string): ProjectContextMessage {
    return {
        id: `project-context-failed-${Date.now()}`,
        role: "system",
        content: `已打开任务：${projectName}\n\n上下文暂未恢复，可以直接继续问。`,
        timestamp: Date.now(),
        isProjectContext: true,
    };
}

/**
 * Call LoadProjectContext with a timeout. Returns the summary or null on timeout/error.
 */
async function loadWithTimeout(projectPath: string): Promise<main.ProjectContextSummary | null> {
    return new Promise<main.ProjectContextSummary | null>((resolve) => {
        let settled = false;
        const timer = setTimeout(() => {
            if (!settled) {
                settled = true;
                resolve(null);
            }
        }, LOAD_CONTEXT_TIMEOUT_MS);

        LoadProjectContext(projectPath)
            .then((result) => {
                if (!settled) {
                    settled = true;
                    clearTimeout(timer);
                    resolve(result);
                }
            })
            .catch(() => {
                if (!settled) {
                    settled = true;
                    clearTimeout(timer);
                    resolve(null);
                }
            });
    });
}

/**
 * Sleep utility for retry delays.
 */
function sleep(ms: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms));
}

export interface UseProjectContextLoaderResult {
    /**
     * Load project context for a given path. Returns the system message to inject
     * into the tab's conversation, or a placeholder + schedules retry.
     *
     * @param projectPath - The project directory path
     * @param onMessage - Callback to inject/replace the context message in the tab
     */
    loadProjectContext: (
        projectPath: string,
        onMessage: (msg: ProjectContextMessage) => void,
        onSettled?: () => void,
    ) => void;
}

/**
 * Hook that provides project context loading functionality.
 * When called, it attempts to load the project context within 2s.
 * If it times out, it shows a placeholder and retries in the background.
 */
export function useProjectContextLoader(): UseProjectContextLoaderResult {
    // Track which project paths have already been loaded to avoid duplicate loads
    const loadedPathsRef = useRef<Set<string>>(new Set());
    // Track whether the component is still mounted to avoid calling onMessage after unmount
    const mountedRef = useRef(true);
    mountedRef.current = true;
    useEffect(() => () => { mountedRef.current = false; }, []);

    const loadProjectContext = useCallback((
        projectPath: string,
        onMessage: (msg: ProjectContextMessage) => void,
        onSettled?: () => void,
    ) => {
        // Avoid duplicate loads for the same project path
        if (loadedPathsRef.current.has(projectPath)) {
            queueMicrotask(() => onSettled?.());
            return;
        }
        loadedPathsRef.current.add(projectPath);

        const projectName = projectPath.split(/[/\\]/).pop() || projectPath;
        let settled = false;
        const settleOnce = () => {
            if (settled) return;
            settled = true;
            onSettled?.();
        };

        // Attempt to load with timeout
        loadWithTimeout(projectPath).then(async (summary) => {
            if (!mountedRef.current) return;

            if (summary) {
                // Success within timeout - inject the context message
                onMessage(createContextMessage(summary));
                settleOnce();
                return;
            }

            // Timeout or error - show placeholder and release the tab. Retries
            // continue in the background so a slow context recall cannot keep
            // the input area stuck in restore mode.
            onMessage(createPlaceholderMessage(projectName));
            settleOnce();

            // Retry loop
            for (let attempt = 0; attempt < MAX_RETRIES; attempt++) {
                await sleep(RETRY_DELAY_MS);
                if (!mountedRef.current) return;
                const retryResult = await loadWithTimeout(projectPath);
                if (!mountedRef.current) return;
                if (retryResult) {
                    onMessage(createContextMessage(retryResult));
                    return;
                }
            }

            // All retries exhausted - show failure message
            if (mountedRef.current) {
                onMessage(createFailedMessage(projectName));
            }
        });
    }, []);

    return { loadProjectContext };
}
