/**
 * MessageContentRenderer — 统一的消息内容 Markdown 渲染组件。
 *
 * 所有 tab 类型（local、project、ve、group）中的 assistant/数字员工消息内容
 * 都应通过此组件渲染，确保 Markdown 渲染能力是架构层面的不变量。
 *
 * 用户消息保持 pre-wrap 纯文本（用户输入不含 Markdown 语法）。
 */
import React, { useMemo, useRef } from "react";
import { renderContentWithCodeBlocks, type Theme as MarkdownTheme } from "./aiAssistantMarkdown";
import { createIncrementalRenderState, renderContentIncremental, type IncrementalRenderState } from "./IncrementalMarkdownRenderer";
import { stripRolePrefixForDisplay } from "./rolePrefixDisplay";

export interface MessageContentRendererProps {
    /** 消息文本内容 */
    content: string;
    /** 主题对象（aiAssistantPanelTheme.Theme 是 MarkdownTheme 的超集） */
    theme: MarkdownTheme;
    /** 是否为用户消息。用户消息渲染为纯文本，非用户消息渲染 Markdown。 */
    isUser?: boolean;
    /** 是否正在流式接收内容。启用时使用增量 Markdown 渲染（避免长消息 UI 冻结）。 */
    isStreaming?: boolean;
    /** 消息 ID（用于增量渲染状态的生命周期管理）。不传时禁用增量模式。 */
    messageId?: string;
}

/**
 * 渲染消息内容。非用户消息（assistant/数字员工）使用完整的 Markdown 渲染管线
 * （代码块、表格、标题、加粗、斜体、链接、文件路径等）。
 * 用户消息保持 pre-wrap 纯文本。
 */
export function MessageContentRenderer({ content, theme, isUser, isStreaming, messageId }: MessageContentRendererProps) {
    const displayContent = useMemo(
        () => isUser ? content : stripRolePrefixForDisplay(content),
        [content, isUser]
    );

    // Incremental render state for streaming long messages.
    const incStateRef = useRef<{ id: string; state: IncrementalRenderState }>({
        id: '', state: createIncrementalRenderState(),
    });
    // Reset when message ID changes.
    if (messageId && incStateRef.current.id !== messageId) {
        incStateRef.current = { id: messageId, state: createIncrementalRenderState() };
    }

    // Memoize markdown rendering — only recompute when content or theme changes.
    // During streaming, content changes every chunk but theme is stable,
    // so this prevents redundant renders when parent re-renders for other reasons.
    const rendered = useMemo(() => {
        if (isUser || !displayContent) return null;
        // Use incremental rendering during streaming to avoid O(n) full re-parse
        // every frame for long messages.
        if (isStreaming && messageId && displayContent.length > 2000) {
            const incRef = incStateRef.current;
            if (incRef.id !== messageId) {
                incRef.id = messageId;
                incRef.state = createIncrementalRenderState();
            }
            return renderContentIncremental(displayContent, theme, incRef.state);
        }
        // Non-streaming or short content: full parse (guarantees correctness).
        if (!isStreaming && incStateRef.current.id) {
            // Streaming just ended — reset incremental state for clean final render.
            incStateRef.current = { id: '', state: createIncrementalRenderState() };
        }
        return renderContentWithCodeBlocks(displayContent, theme);
    }, [displayContent, theme, isUser, isStreaming, messageId]);

    if (isUser) {
        return <span style={{ whiteSpace: "pre-wrap" }}>{displayContent}</span>;
    }

    return <>{rendered}</>;
}
