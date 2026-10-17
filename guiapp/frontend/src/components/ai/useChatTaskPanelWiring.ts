import { useCallback, useEffect, type RefObject } from "react";

// Wiring between the chat-task tray (which renders cards and dispatches
// custom events) and the panel's task actions. Kept out of AIAssistantPanel
// so the panel stays inside its size guard: the tray only needs the event
// bridge and the "run in background" handler, not the panel internals.
export const CHAT_TASK_ACTION_EVENT = "ai-chat-task-action";

export interface ChatTaskActionDetail {
    action?: string;
    taskId?: string;
    text?: string;
}

export interface ChatTaskPanelActions {
    dispatchChatTask?: (text: string, options: { session_key: string; project_path?: string }) => void | Promise<void>;
    cancelChatTask?: (taskId: string) => void | Promise<void>;
    steerChatTask?: (taskId: string, text: string) => void | Promise<void>;
    answerChatTask?: (taskId: string, text: string) => void | Promise<void>;
}

// Bridges tray events (cancel/steer/answer) to the panel actions.
export function useChatTaskActionBridge(actions: ChatTaskPanelActions): void {
    const { cancelChatTask, steerChatTask, answerChatTask } = actions;
    useEffect(() => {
        const onAction = (event: Event) => {
            const detail = (event as CustomEvent<ChatTaskActionDetail>).detail || {};
            const taskId = String(detail.taskId || "").trim();
            if (!taskId) return;
            if (detail.action === "cancel") void cancelChatTask?.(taskId);
            if (detail.action === "steer") void steerChatTask?.(taskId, String(detail.text || ""));
            if (detail.action === "answer") void answerChatTask?.(taskId, String(detail.text || ""));
        };
        window.addEventListener(CHAT_TASK_ACTION_EVENT, onAction);
        return () => window.removeEventListener(CHAT_TASK_ACTION_EVENT, onAction);
    }, [answerChatTask, cancelChatTask, steerChatTask]);
}

// Runs the current composer text as a background task. clearComposerDraft is
// injected so the hook does not reach into the panel's draft state.
export function useRunChatTaskInBackground(options: {
    inputRef: RefObject<HTMLInputElement | HTMLTextAreaElement | null>;
    inputValue: string | undefined;
    activeSessionKey: string | undefined;
    projectPath: string;
    dispatchChatTask?: ChatTaskPanelActions["dispatchChatTask"];
    clearComposerDraft: (options: { clearAttachments: boolean }) => void;
}): () => void {
    const { inputRef, inputValue, activeSessionKey, projectPath, dispatchChatTask, clearComposerDraft } = options;
    return useCallback(() => {
        const text = (inputRef.current?.value ?? inputValue ?? "").trim();
        if (!text || !dispatchChatTask) return;
        clearComposerDraft({ clearAttachments: true });
        void dispatchChatTask(text, { session_key: activeSessionKey || "", project_path: projectPath || undefined });
    }, [activeSessionKey, clearComposerDraft, dispatchChatTask, inputRef, inputValue, projectPath]);
}
