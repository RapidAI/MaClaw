/**
 * useProjectContextLoader — loads project context when a project tab is first opened.
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
 * Format a ProjectContextSummary into a human-readable system message.
 */
function formatProjectContextMessage(summary: main.ProjectContextSummary): string {
    const parts: string[] = [];

    parts.push(`📁 **项目: ${summary.project_name}**`);

    if (summary.recent_progress) {
        parts.push("");
        parts.push("**最近进展:**");
        parts.push(summary.recent_progress);
    }

    if (summary.key_artifacts && summary.key_artifacts.length > 0) {
        parts.push("");
        parts.push("**关键产出物:**");
        for (const artifact of summary.key_artifacts) {
            parts.push(`- \`${artifact}\``);
        }
    }

    if (summary.active_workflow) {
        parts.push("");
        parts.push(`**活跃工作流:** ${summary.active_workflow}`);
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
        content: `⏳ 正在加载项目「${projectName}」的上下文...`,
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
        content: `📁 **项目: ${projectName}**\n\n_项目上下文加载失败，你可以直接开始对话。_`,
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
    ) => {
        // Avoid duplicate loads for the same project path
        if (loadedPathsRef.current.has(projectPath)) {
            return;
        }
        loadedPathsRef.current.add(projectPath);

        const projectName = projectPath.split(/[/\\]/).pop() || projectPath;

        // Attempt to load with timeout
        loadWithTimeout(projectPath).then(async (summary) => {
            if (!mountedRef.current) return;

            if (summary) {
                // Success within timeout — inject the context message
                onMessage(createContextMessage(summary));
                return;
            }

            // Timeout or error — show placeholder and retry
            onMessage(createPlaceholderMessage(projectName));

            // Retry loop
            for (let attempt = 0; attempt < MAX_RETRIES; attempt++) {
                await sleep(RETRY_DELAY_MS);
                if (!mountedRef.current) return;
                try {
                    const retryResult = await LoadProjectContext(projectPath);
                    if (!mountedRef.current) return;
                    if (retryResult) {
                        onMessage(createContextMessage(retryResult));
                        return;
                    }
                } catch {
                    // Continue to next retry
                }
            }

            // All retries exhausted — show failure message
            if (mountedRef.current) {
                onMessage(createFailedMessage(projectName));
            }
        });
    }, []);

    return { loadProjectContext };
}
