/**
 * IncrementalMarkdownRenderer — 增量 Markdown 渲染器
 *
 * 根本性解决"流式输出长消息时 UI 冻结"的问题。
 *
 * 问题根因：
 *   renderContentWithCodeBlocks 对整个消息内容逐行做 Markdown 解析。
 *   每 33ms 的 token flush 都会触发整条消息的全量重新解析。
 *   当消息超过 30-50KB 时，单帧解析时间超过 16ms，JS 主线程被阻塞。
 *
 * 解决方案：
 *   将 content 按"稳定段"和"活跃尾部"分离。
 *   - 稳定段：已完成的完整段落/代码块/表格，只解析一次，缓存 React 节点。
 *   - 活跃尾部：最后一个未完成的段落/代码块，每次 flush 重新解析。
 *
 * 性能特征：
 *   - 每次 flush 渲染成本 = O(活跃尾部行数)，通常 < 20 行
 *   - 冻结扩展 = O(新增稳定部分行数)，已冻结部分不重新解析
 *   - 分割点查找 = O(n) 但每 500 字符才触发一次（摊销 O(1)），且从 lastSplit 向后扫描
 *   - 100KB+ 消息，每帧总成本 < 1ms
 */

import React from "react";
import type { Theme } from "./aiAssistantPanelTheme";
import { isSplitTableRowLabel, renderContentWithCodeBlocks, type ChatMarkdownRenderOptions } from "./aiAssistantMarkdown";
import { displayMathAfterLine, isBareHeadingMarkerLine, markdownFenceAfterLine } from "./aiAssistantMarkdownNormalize";
import { isMarkdownTableRow } from "./aiAssistantMarkdownTable";

// ─── 分割点检测 ─────────────────────────────────────────────────

/**
 * 从已冻结位置向后扫描，找到 content 中最远的安全分割点。
 *
 * 安全分割点 = 已闭合的代码围栏或展示公式之后、完整的一行之后，或 \n\n。
 * 不在未闭合围栏、展示公式、表格或裸标题（单独的 ###）内部切开。
 * 单行换行只是分隔符，不交给任一侧渲染，避免多出一个空行。
 *
 * 扫描从 scanFrom 开始，到 maxPos 结束。
 * 正向单次 O(maxPos - scanFrom) 扫描，同时追踪代码围栏和展示公式状态。
 */
function findBestSplitPointForward(
    content: string,
    scanFrom: number,
    maxPos: number,
): number {
    // Frozen segments only ever end at a split point outside a fence or display
    // formula, so the scan can start closed and remain linear in newly appended
    // content.
    // Keep the opener so an embedded shorter fence cannot close a longer one.
    let fenceMarker = "";
    let displayMathDelimiter: "$$" | "\\[" | "" = "";
    // A table is one visual block across single newlines. A row is not a split.
    let tableOpen = false;
    let bestSplit = -1;
    let i = scanFrom;

    while (i < maxPos) {
        const nlIdx = content.indexOf("\n", i);
        if (nlIdx === -1 || nlIdx >= maxPos) break;

        const line = content.slice(i, nlIdx);

        // Match renderContentWithCodeBlocks: while a display formula is open,
        // every line is formula source until its own closing delimiter. Code
        // fences inside TeX are not Markdown fences.
        const fenceBefore = fenceMarker;
        const mathBefore = displayMathDelimiter;
        if (displayMathDelimiter) {
            displayMathDelimiter = displayMathAfterLine(line, displayMathDelimiter);
        } else {
            const nextFence = markdownFenceAfterLine(line, fenceMarker);
            // A glued opener (`heading``` `) must keep the following lines inside
            // the fence. Treating only line-start markers as fences lets a blank
            // line in that block freeze mid-code.
            const fenceTouched = nextFence !== fenceMarker || fenceMarker !== "";
            fenceMarker = nextFence;
            if (!fenceTouched) displayMathDelimiter = displayMathAfterLine(line, "");
        }

        // A closed fence or formula will not change. Leave it out of the tail
        // even when the next paragraph never inserts a blank line.
        const blockClosed = (fenceBefore !== "" && fenceMarker === "")
            || (mathBefore !== "" && displayMathDelimiter === "");
        if (blockClosed && !fenceMarker && !displayMathDelimiter) {
            bestSplit = nlIdx + 1;
            tableOpen = false;
        }

        if (fenceMarker || displayMathDelimiter) {
            // Pipe rows inside a fence are source, not a Markdown table.
            tableOpen = false;
        } else {
            const tableLine = isMarkdownTableRow(line) || (tableOpen && isSplitTableRowLabel(line));
            const bareHeading = isBareHeadingMarkerLine(line);
            const blankFollows = nlIdx + 1 < content.length && content[nlIdx + 1] === "\n";
            if (blankFollows) {
                // A lone "###" still owns the title after the blank line.
                if (!bareHeading) bestSplit = nlIdx + 2;
                tableOpen = false;
            } else if (tableLine) {
                tableOpen = true;
            } else if (!bareHeading) {
                // Lists, headings, and prose are one element per line. The
                // newline is only the separator renderContentWithCodeBlocks splits on.
                bestSplit = nlIdx + 1;
                tableOpen = false;
            } else {
                tableOpen = false;
            }
        }

        i = nlIdx + 1;
    }

    return bestSplit;
}

// ─── 冻结段 ──────────────────────────────────────────────────────

interface FrozenSegment {
    /** content 的字符偏移量（冻结段覆盖 [0, contentUpTo)） */
    contentUpTo: number;
    /** 已解析的 React 节点列表 */
    nodes: React.ReactNode[];
}

export interface IncrementalRenderState {
    /** 已冻结的稳定段 */
    frozen: FrozenSegment | null;
    /** 上一次渲染的完整 content 长度（检测截断/替换） */
    lastContentLen: number;
    /** Exact prior text, used only to catch same-length in-place replacements. */
    lastContent: string;
    /** Theme used to create frozen React nodes. */
    lastTheme: Theme | null;
    /** Blank-spacer policy used to create frozen React nodes. */
    lastOmitBlankSpacers: boolean;
    /** 上一次冻结检查的 content 长度 */
    lastFreezeCheckLen: number;
    /** 上一帧的尾部 content（用于判断是否需要重新渲染尾部） */
    lastTailContent: string;
    /** 上一帧的输出缓存 */
    lastOutput: React.ReactNode[] | null;
}

export function createIncrementalRenderState(): IncrementalRenderState {
    return {
        frozen: null,
        lastContentLen: 0,
        lastContent: '',
        lastTheme: null,
        lastOmitBlankSpacers: false,
        lastFreezeCheckLen: 0,
        lastTailContent: '',
        lastOutput: null,
    };
}

// ─── 核心渲染 ────────────────────────────────────────────────────

/** 冻结检查间隔：每增长 500 字符检查一次 */
const FREEZE_CHECK_INTERVAL = 500;
/**
 * Answers and thoughts shorter than this are still one full parse per flush.
 * 640 is the first size where a paragraph break can freeze a prefix
 * (freeze scan opens after 500 characters and keeps a 200-character tail)
 * while staying under the old 2000-character full-parse floor.
 */
export const STREAMING_MARKDOWN_INCREMENTAL_MIN = 640;

/** True when a streaming markdown body should re-parse only its unfrozen tail. */
export function streamingMarkdownUsesIncremental(length: number): boolean {
    return length >= STREAMING_MARKDOWN_INCREMENTAL_MIN;
}

/** One growing message owns one freeze cache. A different id starts over. */
export function bindStreamingMarkdown(
    bucket: { messageId: string; state: IncrementalRenderState },
    messageId: string,
    theme: Theme,
    options?: ChatMarkdownRenderOptions,
): (formatted: string) => React.ReactNode[] {
    return (formatted: string) => {
        if (bucket.messageId !== messageId) {
            bucket.messageId = messageId;
            bucket.state = createIncrementalRenderState();
        }
        return renderContentIncremental(formatted, theme, bucket.state, options);
    };
}

/** 活跃尾部保留的最小字符数 */
const MIN_TAIL_LEN = 200;
/** display:contents makes wrapper divs invisible to CSS layout while providing
 *  independent React reconciliation subtrees (isolating internal keys). */
const contentsStyle: React.CSSProperties = { display: 'contents' };

/**
 * 增量渲染 Markdown 内容。
 */
export function renderContentIncremental(
    content: string,
    t: Theme,
    state: IncrementalRenderState,
    options?: ChatMarkdownRenderOptions,
): React.ReactNode[] {
    if (!content) return [];
    const omitBlankSpacers = options?.omitBlankSpacers === true;

    // ── 缓存失效检测 ──
    // A live round only appends. A new round can replace the bubble with a
    // longer string that does not extend the frozen prefix; keeping those
    // nodes would paint the previous answer in front of the new one.
    if (
        state.lastTheme !== t ||
        state.lastOmitBlankSpacers !== omitBlankSpacers ||
        content.length < state.lastContentLen ||
        (state.lastContent.length > 0 && !content.startsWith(state.lastContent))
    ) {
        resetState(state);
    }
    state.lastContentLen = content.length;
    state.lastContent = content;
    state.lastTheme = t;
    state.lastOmitBlankSpacers = omitBlankSpacers;

    // 验证冻结段有效性
    if (state.frozen && state.frozen.contentUpTo > content.length) {
        resetState(state);
    }

    // Short replies stay one full parse. Longer streams freeze stable paragraphs.
    if (!streamingMarkdownUsesIncremental(content.length)) {
        return renderContentWithCodeBlocks(content, t, options);
    }

    // ── 冻结扩展（每 500 字符检查一次） ──
    if (content.length - state.lastFreezeCheckLen > FREEZE_CHECK_INTERVAL) {
        state.lastFreezeCheckLen = content.length;

        // 搜索范围：从冻结点向后扫描到保留尾部之前。
        // 从冻结点开始保证 fenceOpen=false（冻结点定义为不在代码块内）。
        const currentFrozenEnd = state.frozen?.contentUpTo ?? 0;
        const maxPos = content.length - MIN_TAIL_LEN;

        if (maxPos > currentFrozenEnd) {
            const splitPos = findBestSplitPointForward(content, currentFrozenEnd, maxPos);

            if (splitPos > currentFrozenEnd) {
                // 增量冻结：只解析新增的稳定部分
                // The newline that ends a closer is only a line separator.
                // Giving it to either slice makes that slice render a blank spacer
                // the full parse never had. A blank line (\n\n) stays with the slice.
                let renderEnd = splitPos;
                if (renderEnd > currentFrozenEnd && content[renderEnd - 1] === "\n" && content[renderEnd - 2] !== "\n") {
                    renderEnd -= 1;
                }
                const newStableContent = content.slice(currentFrozenEnd, renderEnd);
                const newNodes = renderContentWithCodeBlocks(newStableContent, t, options);
                const prevNodes = state.frozen?.nodes ?? [];
                // Each slice numbers its own md-0. Concatenating those arrays
                // makes them siblings, and React then reuses the wrong row.
                const slice = React.createElement(
                    "div",
                    { key: `freeze-${splitPos}`, className: "assistant-reasoning-segment", style: contentsStyle },
                    newNodes,
                );

                state.frozen = {
                    contentUpTo: splitPos,
                    // New array reference ensures React detects children change
                    // after freeze expansion. The O(N) copy cost is negligible
                    // (~20μs for 200 freeze cycles with 100 nodes each).
                    nodes: prevNodes.length > 0 ? [...prevNodes, slice] : [slice],
                };
                // 输出缓存失效
                state.lastOutput = null;
                state.lastTailContent = '';
            }
        }
    }

    // ── 渲染输出 ──
    if (state.frozen && state.frozen.contentUpTo < content.length) {
        const tailContent = content.slice(state.frozen.contentUpTo);

        // 尾部没变 → 复用上一帧输出
        if (tailContent === state.lastTailContent && state.lastOutput) {
            return state.lastOutput;
        }

        const tailNodes = renderContentWithCodeBlocks(tailContent, t, options);
        // Wrap frozen and tail in display:contents divs to create independent
        // React reconciliation subtrees. This prevents key collisions between
        // the two independently-rendered segments (both start internal keys from 0).
        // display:contents makes the wrapper invisible to CSS layout.
        // Pass children as array (3rd arg) instead of spreading, to avoid
        // V8 argument-list performance degradation with 500+ nodes.
        const output: React.ReactNode[] = [
            React.createElement('div', { key: '__inc_frozen', className: 'assistant-reasoning-segment', style: contentsStyle }, state.frozen.nodes),
            React.createElement('div', { key: '__inc_tail', className: 'assistant-reasoning-segment', style: contentsStyle }, tailNodes),
        ];
        state.lastOutput = output;
        state.lastTailContent = tailContent;
        return output;
    }

    // 没有冻结段：全量渲染
    return renderContentWithCodeBlocks(content, t, options);
}

function resetState(state: IncrementalRenderState): void {
    state.frozen = null;
    state.lastFreezeCheckLen = 0;
    state.lastTailContent = '';
    state.lastOutput = null;
}

// ─── Hook API（备用） ────────────────────────────────────────────

/**
 * React hook：为单条消息维护增量渲染状态。
 */
export function useIncrementalMarkdown(
    messageId: string,
    content: string | undefined,
    theme: Theme,
    isStreaming: boolean,
): React.ReactNode[] {
    const stateRef = React.useRef<{ id: string; state: IncrementalRenderState }>({
        id: '',
        state: createIncrementalRenderState(),
    });

    if (stateRef.current.id !== messageId) {
        stateRef.current = { id: messageId, state: createIncrementalRenderState() };
    }

    if (!content) return [];

    if (!isStreaming) {
        stateRef.current.state = createIncrementalRenderState();
        return renderContentWithCodeBlocks(content, theme);
    }

    return renderContentIncremental(content, theme, stateRef.current.state);
}
