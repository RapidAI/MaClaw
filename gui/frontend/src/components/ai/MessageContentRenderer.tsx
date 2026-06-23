/**
 * MessageContentRenderer — 统一的消息内容 Markdown 渲染组件。
 *
 * 所有 tab 类型（local、project、ve、group）中的 assistant/数字员工消息内容
 * 都应通过此组件渲染，确保 Markdown 渲染能力是架构层面的不变量。
 *
 * 用户消息保持 pre-wrap 纯文本（用户输入不含 Markdown 语法）。
 */
import React, { useMemo } from "react";
import { renderContentWithCodeBlocks, type Theme as MarkdownTheme } from "./aiAssistantMarkdown";
import { stripRolePrefixForDisplay } from "./rolePrefixDisplay";

export interface MessageContentRendererProps {
    /** 消息文本内容 */
    content: string;
    /** 主题对象（aiAssistantPanelTheme.Theme 是 MarkdownTheme 的超集） */
    theme: MarkdownTheme;
    /** 是否为用户消息。用户消息渲染为纯文本，非用户消息渲染 Markdown。 */
    isUser?: boolean;
}

/**
 * 渲染消息内容。非用户消息（assistant/数字员工）使用完整的 Markdown 渲染管线
 * （代码块、表格、标题、加粗、斜体、链接、文件路径等）。
 * 用户消息保持 pre-wrap 纯文本。
 */
export function MessageContentRenderer({ content, theme, isUser }: MessageContentRendererProps) {
    const displayContent = useMemo(
        () => isUser ? content : stripRolePrefixForDisplay(content),
        [content, isUser]
    );

    // Memoize markdown rendering — only recompute when content or theme changes.
    // During streaming, content changes every chunk but theme is stable,
    // so this prevents redundant renders when parent re-renders for other reasons.
    const rendered = useMemo(
        () => !isUser && displayContent ? renderContentWithCodeBlocks(displayContent, theme) : null,
        [displayContent, theme, isUser]
    );

    if (isUser) {
        return <span style={{ whiteSpace: "pre-wrap" }}>{displayContent}</span>;
    }

    return <>{rendered}</>;
}
