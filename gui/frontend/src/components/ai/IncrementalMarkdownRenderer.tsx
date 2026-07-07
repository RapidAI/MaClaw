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
import { renderContentWithCodeBlocks } from "./aiAssistantMarkdown";

// ─── 分割点检测 ─────────────────────────────────────────────────

/**
 * 从已冻结位置向后扫描，找到 content 中最远的安全分割点。
 *
 * 安全分割点 = \n\n 位置，且该位置不在未闭合代码块内。
 *
 * 扫描从 scanFrom 开始，到 maxPos 结束。
 * 正向单次 O(maxPos - scanFrom) 扫描，同时追踪 fence 奇偶。
 */
function findBestSplitPointForward(
    content: string,
    scanFrom: number,
    maxPos: number,
): number {
    let fenceOpen = false;
    let bestSplit = -1;
    let i = scanFrom;

    while (i < maxPos) {
        const nlIdx = content.indexOf("\n", i);
        if (nlIdx === -1 || nlIdx >= maxPos) break;

        const line = content.slice(i, nlIdx);
        if (/^```/.test(line.trimStart())) {
            fenceOpen = !fenceOpen;
        }

        // 检查是否是 \n\n（当前行结束后紧跟空行）
        if (!fenceOpen && nlIdx + 1 < content.length && content[nlIdx + 1] === '\n') {
            // 分割点在 \n\n 结束之后
            bestSplit = nlIdx + 2;
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
        lastFreezeCheckLen: 0,
        lastTailContent: '',
        lastOutput: null,
    };
}

// ─── 核心渲染 ────────────────────────────────────────────────────

/** 冻结检查间隔：每增长 500 字符检查一次 */
const FREEZE_CHECK_INTERVAL = 500;
/** 最小内容长度（低于此值不启用增量模式） */
const MIN_CONTENT_FOR_INCREMENTAL = 2000;
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
): React.ReactNode[] {
    if (!content) return [];

    // ── 缓存失效检测 ──
    if (content.length < state.lastContentLen) {
        // Content 被截断或替换——完全重置
        resetState(state);
    }
    state.lastContentLen = content.length;

    // 验证冻结段有效性
    if (state.frozen && state.frozen.contentUpTo > content.length) {
        resetState(state);
    }

    // 短内容直接全量渲染
    if (content.length < MIN_CONTENT_FOR_INCREMENTAL) {
        return renderContentWithCodeBlocks(content, t);
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
                const newStableContent = content.slice(currentFrozenEnd, splitPos);
                const newNodes = renderContentWithCodeBlocks(newStableContent, t);
                const prevNodes = state.frozen?.nodes ?? [];

                state.frozen = {
                    contentUpTo: splitPos,
                    // New array reference ensures React detects children change
                    // after freeze expansion. The O(N) copy cost is negligible
                    // (~20μs for 200 freeze cycles with 100 nodes each).
                    nodes: prevNodes.length > 0 ? [...prevNodes, ...newNodes] : newNodes,
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

        const tailNodes = renderContentWithCodeBlocks(tailContent, t);
        // Wrap frozen and tail in display:contents divs to create independent
        // React reconciliation subtrees. This prevents key collisions between
        // the two independently-rendered segments (both start internal keys from 0).
        // display:contents makes the wrapper invisible to CSS layout.
        // Pass children as array (3rd arg) instead of spreading, to avoid
        // V8 argument-list performance degradation with 500+ nodes.
        const output: React.ReactNode[] = [
            React.createElement('div', { key: '__inc_frozen', style: contentsStyle }, state.frozen.nodes),
            React.createElement('div', { key: '__inc_tail', style: contentsStyle }, tailNodes),
        ];
        state.lastOutput = output;
        state.lastTailContent = tailContent;
        return output;
    }

    // 没有冻结段：全量渲染
    return renderContentWithCodeBlocks(content, t);
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
