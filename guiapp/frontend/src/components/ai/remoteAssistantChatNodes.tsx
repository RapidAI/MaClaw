import { Fragment, useCallback, useMemo, useRef } from "react";
import { localizeText } from "../../i18n";
import { IconRocket } from "./WorkbenchIcons";
import { downloadRemoteWorkspace } from "./remoteWorkspaceDownload";
import {
    bindStreamingMarkdown,
    createIncrementalRenderState,
    streamingMarkdownUsesIncremental,
} from "./IncrementalMarkdownRenderer";
import { reasoningTrailMarkdownOptions, renderMessage } from "./aiAssistantMarkdown";
import { isTranscriptToolCallText } from "./assistantToolCall";
import { isCodingAgentProgressContent } from "./CodingAgentProgressStatus";
import { CodingAgentProcessTimeline } from "./CodingAgentProcessTimeline";
import { codingTimelineLastThoughtIndex, codingTimelineLiveThoughtIndex } from "./assistantLiveActivity";
import { remoteAssistantCanSend, remoteAssistantSend, remoteAssistantSending } from "../layout/remoteAssistantSession";
import type { RemoteAssistantSession } from "../layout/remoteAssistantSession";
import type { ChatMessage } from "./useAIAssistant";
import type { Theme } from "./aiAssistantPanelTheme";

/**
 * Everything the main panel paints for a mirrored remote/cloud session:
 * the transcript (ChatMessage projection), the per-message nodes (the
 * mirrored coding round paints the same interleaved process timeline a
 * local coding turn keeps on its bubble), the progress tray, the empty
 * states, and the cloud card-action wiring. Extracted from
 * AIAssistantPanel to keep that file under the UI guard cap.
 */
export function useRemoteAssistantChatNodes({ remoteChat, lang, theme: t }: {
    remoteChat: RemoteAssistantSession | null;
    lang: string;
    theme: Theme;
}): {
    remoteTranscript: ChatMessage[];
    remoteRenderedMessages: React.ReactNode[] | null;
    remoteRenderedProgress: React.ReactNode[] | null;
    remoteEmptyContent: React.ReactNode | undefined;
} {
    // A remote transcript file lives on the controlled machine. Opening one
    // asks that desktop for the bytes (fetch_file op), saves them into this
    // computer's Downloads folder, and returns the local path for the card.
    const remoteFileFetch = useCallback(async (filePath: string): Promise<string> => {
        const chat = remoteChat;
        if (!chat) return '';
        const bindings = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go?.main?.App;
        const op = bindings?.['RemoteControlOp'] as ((...args: unknown[]) => Promise<unknown>) | undefined;
        const save = bindings?.['SaveRemoteControlFile'] as ((...args: unknown[]) => Promise<unknown>) | undefined;
        if (typeof op !== 'function' || typeof save !== 'function') {
            throw new Error(localizeText(lang, 'Remote control is not ready', '远控尚未就绪'));
        }
        // Cloud artifacts live on the srv instance (no desktop project_path);
        // cloud_fetch_file serves only paths a recorded turn produced.
        const opName = chat.cloud ? 'cloud_fetch_file' : 'fetch_file';
        const request = chat.cloud
            ? JSON.stringify({ path: filePath })
            : JSON.stringify({ project_path: chat.projectPath, path: filePath });
        console.debug('[remote-artifact] fetch', { op: opName, path: filePath });
        const raw = String(await op(chat.grantId, opName, request) || '{}');
        let parsed: { name?: string; data_base64?: string; error?: string };
        try {
            parsed = JSON.parse(raw) as { name?: string; data_base64?: string; error?: string };
        } catch {
            throw new Error(localizeText(lang, 'The remote file could not be read', '远端文件暂时无法读取'));
        }
        if (parsed.error) throw new Error(parsed.error);
        if (!parsed.data_base64) {
            throw new Error(localizeText(lang, 'The remote file could not be read', '远端文件暂时无法读取'));
        }
        const saved = String(await save(parsed.name || 'remote-file', parsed.data_base64) || '');
        if (!saved) {
            throw new Error(localizeText(lang, 'The file could not be saved', '文件保存失败'));
        }
        console.debug('[remote-artifact] saved', { op: opName, path: filePath, saved });
        return saved;
    }, [remoteChat, lang]);
    // The card-head 导出 packs the whole cloud workspace into one .tar.gz —
    // the same srv call the task list's 打包下载 context entry runs. The
    // session's projectPath carries the instance id as "cloud-agent:<id>".
    const remoteWorkspaceDownload = useCallback((): Promise<string> => downloadRemoteWorkspace(remoteChat, lang), [remoteChat, lang]);
    // Incremental markdown state for the mirrored round's live row — the
    // same freeze-completed-paragraphs treatment a local streaming bubble
    // gets, so a cloud token batch does not re-parse the whole transcript.
    const remoteIncrementalStateRef = useRef({ messageId: '', state: createIncrementalRenderState() });
    const remoteReasoningIncrementalStateRef = useRef({ messageId: '', state: createIncrementalRenderState() });
    // Cloud ask/confirm card buttons send their command through the same
    // composer path a typed answer would take. Desktop-remote cards stay
    // display-only here (the controlled desktop owns its own session UI).
    const remoteExecuteAction = useCallback((command: string) => {
        if (!remoteChat?.cloud || !remoteAssistantCanSend() || remoteAssistantSending()) return;
        void remoteAssistantSend(command);
    }, [remoteChat?.cloud]);
    const remoteTranscript = useMemo(() => {
        if (!remoteChat) return [];
        return remoteChat.messages.map((message, index): ChatMessage => {
            let role: ChatMessage["role"] = "assistant";
            if (message.role === "user" || message.role === "system" || message.role === "error") role = message.role;
            const localFilePaths = (message.localFilePaths || []).map(path => String(path || '').trim()).filter(Boolean);
            return {
                id: `remote-${remoteChat.grantId}-${index}`,
                role,
                content: message.content || '',
                reasoning: message.reasoning || '',
                localFilePaths: localFilePaths.length ? localFilePaths : undefined,
                remoteFileFetch: localFilePaths.length ? remoteFileFetch : undefined,
                // A grantless cloud session (grantId fell back to the item id)
                // cannot serve cloud_fetch_file — keep the manual fetch
                // button instead of painting an error on every artifact.
                remoteArtifactsAutoFetch: localFilePaths.length && remoteChat.cloud && remoteChat.canSend ? true : undefined,
                remoteWorkspaceDownload: localFilePaths.length && remoteChat.cloud ? remoteWorkspaceDownload : undefined,
                fields: message.fields,
                queued: message.queued || undefined,
                toolCalls: message.toolCalls,
                codingTimeline: message.codingTimeline,
                confirmation: message.confirmation ? { id: message.confirmation.id, summary: message.confirmation.summary || message.content || '' } : undefined,
                actions: message.actions?.map(action => ({ label: action.label, command: action.command, style: 'primary' as const })),
                timestamp: index + 1,
            };
        });
    }, [remoteChat, remoteFileFetch, remoteWorkspaceDownload]);
    const remoteRenderedMessages = useMemo(() => {
        if (!remoteTranscript.length) return null;
        const last = remoteTranscript.length - 1;
        const live = remoteChat?.live;
        return remoteTranscript.map((message, index) => {
            const isLastAssistant = index === last && message.role === "assistant";
            // The mirrored round streams like a local one: tokens land live
            // and the thinking panel shows a live box until the block closes.
            const streaming = isLastAssistant && !!live && !live.responded;
            const liveThinking = streaming && live.parts.some(part => part.kind === "thinking" && !part.done);
            // Incremental markdown on the live row: completed paragraphs
            // freeze and only the tail re-parses, matching the local panel.
            const longStreamingContent = !!(streaming && message.content && streamingMarkdownUsesIncremental(message.content.length));
            const longStreamingReasoning = !!(streaming && message.reasoning && streamingMarkdownUsesIncremental(message.reasoning.length));
            const contentRenderer = longStreamingContent
                ? bindStreamingMarkdown(remoteIncrementalStateRef.current, message.id, t)
                : undefined;
            const reasoningRenderer = longStreamingReasoning
                ? bindStreamingMarkdown(remoteReasoningIncrementalStateRef.current, message.id, t, reasoningTrailMarkdownOptions)
                : undefined;
            if (isLastAssistant && !streaming) {
                // Reset incremental state when streaming ends so the final
                // render is a clean full parse (100% correct).
                if (remoteIncrementalStateRef.current.messageId === message.id) {
                    remoteIncrementalStateRef.current = { messageId: '', state: createIncrementalRenderState() };
                }
                if (remoteReasoningIncrementalStateRef.current.messageId === message.id) {
                    remoteReasoningIncrementalStateRef.current = { messageId: '', state: createIncrementalRenderState() };
                }
            }
            // Events of a turn that never replied sit on the user line.
            const codingTimeline = message.codingTimeline;
            if (codingTimeline?.length) {
                // The mirrored coding round paints the same interleaved
                // thought/tool transcript a local coding turn keeps on its
                // bubble — live rows open, settled rows fold.
                const liveLabel = liveThinking
                    ? localizeText(lang, "Thinking", "正在思考", "正在思考")
                    : undefined;
                const lastThinkingIndex = codingTimelineLiveThoughtIndex(codingTimeline, !!liveLabel, liveThinking ? "thinking" : null);
                const openThoughtIndex = isLastAssistant ? codingTimelineLastThoughtIndex(codingTimeline) : -1;
                const reply = renderMessage(
                    message,
                    remoteExecuteAction,
                    t,
                    isLastAssistant,
                    "",
                    lang,
                    streaming,
                    contentRenderer,
                    undefined,
                    true,
                    reasoningRenderer,
                    liveLabel,
                    undefined,
                    streaming,
                );
                return (
                    <Fragment key={message.id}>
                        <CodingAgentProcessTimeline
                            items={codingTimeline}
                            theme={t}
                            lang={lang}
                            lastThinkingIndex={lastThinkingIndex}
                            openThoughtIndex={openThoughtIndex}
                            liveLabel={liveLabel}
                            live={streaming}
                        />
                        {reply}
                    </Fragment>
                );
            }
            return (
                <Fragment key={message.id}>
                    {renderMessage(
                        message,
                        remoteExecuteAction,
                        t,
                        isLastAssistant,
                        "",
                        lang,
                        streaming,
                        contentRenderer,
                        undefined,
                        false,
                        reasoningRenderer,
                        liveThinking ? (remoteChat?.cloud
                            ? localizeText(lang, "Thinking in the cloud…", "云端正在思考…", "雲端正在思考…")
                            : localizeText(lang, "Thinking on the other computer…", "对方正在思考…", "對方正在思考…")) : undefined,
                        undefined,
                        streaming,
                    )}
                </Fragment>
            );
        });
    }, [lang, remoteChat, remoteExecuteAction, remoteTranscript, t]);
    const remoteRenderedProgress = useMemo(() => {
        // Styled tool-status rows live on the assistant bubble's toolCalls
        // timeline, and coding-event rows live on its process timeline —
        // rendering them here too doubled every call and leaked the raw
        // "Coding Agent Event:" JSON. Only plain non-coding lines stay.
        const parts = (remoteChat?.live?.parts || []).filter(part =>
            part.kind === "progress" && part.text
            && !isTranscriptToolCallText(part.text)
            && !isCodingAgentProgressContent(part.text));
        if (!remoteChat || parts.length === 0) return null;
        return parts.map((part, index) => (
            <Fragment key={`remote-progress-${remoteChat.grantId}-${part.seq || index}`}>
                {renderMessage({
                    id: `remote-progress-${remoteChat.grantId}-${part.seq || index}`,
                    role: "progress",
                    content: part.text,
                    timestamp: index + 1,
                }, () => undefined, t, false, "", lang, false)}
            </Fragment>
        ));
    }, [lang, remoteChat, t]);
    const remoteEmptyContent = useMemo(() => {
        if (!remoteChat) return undefined;
        if (remoteChat.loading) {
            return (
                <div data-testid="remote-chat-loading" className="aap-remote-loading">
                    <div className="aap-wf-icon"><IconRocket size={28} color="currentColor" /></div>
                    <div className="aap-wf-title">{remoteChat.cloud
                        ? localizeText(lang, "Opening cloud task…", "正在打开云端任务…", "正在開啟雲端任務…")
                        : localizeText(lang, "Opening remote task…", "正在打开远端任务…", "正在開啟遠端任務…")}</div>
                    <div className="aap-wf-sub">{localizeText(lang, "Please wait...", "请稍候...", "請稍候...")}</div>
                </div>
            );
        }
        if (remoteChat.offline) {
            return (
                <span data-testid="remote-chat-offline" className="aap-remote-empty-hint">
                    {localizeText(lang, "That computer is offline. Its conversation stays here for viewing.", "对方离线。会话仍可查看，暂时不能发送。", "對方離線。會話仍可查看，暫時不能傳送。")}
                </span>
            );
        }
        if (!remoteChat.canSend) {
            return (
                <span data-testid="remote-chat-readonly" className="aap-remote-empty-hint">
                    {remoteChat.cloud
                        ? localizeText(lang, "View only. You cannot send messages to this cloud task.", "仅查看。不能向云端任务发送消息。", "唯讀。不能向雲端任務傳送訊息。")
                        : localizeText(lang, "View only. You cannot send messages to that computer.", "仅查看。不能向对方电脑发送消息。", "唯讀。不能向對方電腦傳送訊息。")}
                </span>
            );
        }
        // The live activity panel owns the bottom of an empty busy transcript;
        // an idle "connected" hint beside it reads as a contradiction.
        if (remoteChat.busy) return null;
        return (
            <span data-testid="remote-chat-idle" className="aap-remote-empty-hint">
                {remoteChat.cloud
                    ? localizeText(lang, "Connected. Messages sent here run on the cloud server.", "已连接云端任务，输入的消息会在云端服务器上执行。", "已連接雲端任務，輸入的訊息會在雲端伺服器上執行。")
                    : localizeText(lang, "Connected. Messages sent here run on that computer.", "已连接远端任务，输入的消息会在对方电脑上执行。", "已連接遠端任務，輸入的訊息會在對方電腦上執行。")}
            </span>
        );
    }, [lang, remoteChat, t]);
    return { remoteTranscript, remoteRenderedMessages, remoteRenderedProgress, remoteEmptyContent };
}
