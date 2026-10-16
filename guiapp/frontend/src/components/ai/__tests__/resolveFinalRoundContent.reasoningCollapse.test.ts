import { describe, expect, it } from 'vitest';
import { resolveFinalRoundContent, terminalAssistantResult, terminalAssistantResultStatus, type ChatMessage } from '../useAIAssistant';

/**
 * Reasoning-trail collapse: when a turn carries a reasoning trail (the
 * collapsible 思考过程 panel), the completed bubble must shrink to the clean
 * final answer instead of preserving the full multi-round streamed
 * accumulation (Layer 3 endsWith). The intermediate round chatter piled up
 * as widely-spaced stale paragraphs in the completed message.
 */
describe('resolveFinalRoundContent — reasoning-trail collapse', () => {
    const makeMessage = (content: string, reasoning?: string): ChatMessage => ({
        id: 'msg-reasoning-collapse',
        role: 'assistant',
        content,
        reasoning,
        timestamp: Date.now(),
    });

    it('keeps tool-call markers when the official answer replaces the running transcript', () => {
        const finalText = '接口需要登录，这一步已经改走浏览器。后面补上完整说明，避免被当成短片段而整段保留中间过程。';
        const streamed = '先用命令试一次。\n\n<!--maclaw-tool:call-1-->\n\n命令被拦住了。';
        const result = resolveFinalRoundContent(
            makeMessage(streamed, 'The user wants a video download. Let me try bash first.'),
            { text: finalText, response_source: 'agent_loop' },
        );
        expect(result).toContain('<!--maclaw-tool:call-1-->');
        expect(result).toContain('先用命令试一次。');
        expect(result).toContain(finalText);
        expect(terminalAssistantResult({
            ...makeMessage(streamed, 'The user wants a video download. Let me try bash first.'),
            toolCalls: [{ id: 'call-1', name: 'bash', action: '执行命令', detail: 'curl' }],
        }, { text: finalText, response_source: 'agent_loop' })).toBe(finalText);
        const withCall = {
            ...makeMessage(streamed),
            toolCalls: [{ id: 'call-1', name: 'bash', action: '执行命令', detail: 'curl' }],
        };
        expect(terminalAssistantResultStatus(withCall, { text: finalText, trace_status: 'failed' })).toBe('incomplete');
        expect(terminalAssistantResultStatus(withCall, { text: finalText, trace_status: 'ok' })).toBe('completed');
        expect(terminalAssistantResult(withCall, { text: '', error: '写入日志失败' })).toBe('写入日志失败');
        expect(terminalAssistantResultStatus(withCall, { text: '日志已保存一半。', error: '写入日志失败' })).toBe('incomplete');
        expect(terminalAssistantResult(withCall, { text: '日志已保存一半。', error: '写入日志失败' })).toBe('日志已保存一半。\n\n写入日志失败');
    });

    it('does not repeat the official answer when it is already in the tool transcript', () => {
        const finalText = '接口需要登录，这一步已经改走浏览器。';
        const streamed = `先用命令试一次。\n\n<!--maclaw-tool:call-1-->\n\n${finalText}`;
        const result = resolveFinalRoundContent(
            makeMessage(streamed, 'The user wants a video download.'),
            { text: finalText, response_source: 'agent_loop' },
        );
        expect(result).toBe(streamed);
    });

    it('collapses to finalText when the turn streamed a reasoning trail', () => {
        const finalText = '看来生成PPT的工具当前不可用。让我为你整理一份完整的布偶宝宝5岁生日PPT内容，你可以直接复制到PowerPoint或Canva中制作：……（完整长答复）';
        const streamed = '找到了布偶猫图片资源。\n\noffice工具被拒绝了。\n\n' + finalText;
        const message = makeMessage(streamed, '先搜索图片资源。再尝试生成 PPT。');
        const result = resolveFinalRoundContent(message, { text: finalText, response_source: 'agent_loop' });
        expect(result).toBe(finalText);
    });

    it('collapses when only the terminal response carries reasoning', () => {
        const finalText = '这是最终答复，包含完整的交付内容与后续建议，足够长以避免触发片段保护。';
        const streamed = '中间过程叙述\n\n' + finalText;
        const message = makeMessage(streamed);
        const result = resolveFinalRoundContent(message, { text: finalText, reasoning: '最终一轮的思考' });
        expect(result).toBe(finalText);
    });

    it('still preserves accumulated streamed content when no reasoning trail exists', () => {
        const finalText = '这是最终答复，包含完整的交付内容与后续建议，足够长以避免触发片段保护。';
        const streamed = '中间过程\n\n' + finalText;
        const message = makeMessage(streamed);
        const result = resolveFinalRoundContent(message, { text: finalText, response_source: 'agent_loop' });
        expect(result).toBe(streamed);
    });

    it('does not treat host status bullets as a reasoning trail for Layer 3 collapse', () => {
        const finalText = '这是最终答复，包含完整的交付内容与后续建议，足够长以避免触发片段保护。';
        const streamed = '中间过程\n\n' + finalText;
        const message = makeMessage(streamed, '• 已接收任务\n• 正在准备执行路径');
        const result = resolveFinalRoundContent(message, { text: finalText, response_source: 'agent_loop' });
        expect(result).toBe(streamed);
    });

    it('does not lift reasoning into the body on a coding-agent turn', () => {
        const finalText = '评审意见上一条已给出。是否需要英文版？';
        const reasoning = 'Major comment: missing scratch-prompt baseline. '.repeat(20);
        const message: ChatMessage = {
            ...makeMessage(finalText, reasoning),
            pendingCodingThoughts: [{
                id: 't1',
                sequence: 1,
                kind: 'thinking',
                content: reasoning,
                timestamp: Date.now(),
            }],
        };
        const result = resolveFinalRoundContent(message, { text: finalText, response_source: 'agent_loop' });
        expect(result).toBe(finalText);
        expect(result).not.toContain('missing scratch-prompt baseline');
    });

    it('keeps a long streamed deliverable when shared_agent_loop final text is a short later fragment', () => {
        const finalText = '已收到该查询优先级指令。';
        const streamed = '## 译文\n' + '检索增强生成通过引入外部知识来提升模型。'.repeat(20) + '\n\n' + finalText;
        expect(streamed.length).toBeGreaterThanOrEqual(finalText.length * 2);
        const result = resolveFinalRoundContent(
            makeMessage(streamed, 'The user wants an English abstract translated into Chinese.'),
            { text: finalText, response_source: 'shared_agent_loop' },
        );
        expect(result).toBe(streamed);
        expect(result).toContain('## 译文');
    });

    it('keeps a restored deliverable when the next round appended a lookup receipt', () => {
        const finalText = '## 译文\n检索增强生成。';
        const streamed = `${finalText}\n\n${'已收到该查询优先级指令。'.repeat(8)}`;
        expect(streamed.length).toBeGreaterThanOrEqual(finalText.length * 2);
        const result = resolveFinalRoundContent(
            makeMessage(streamed, 'The user wants an English abstract translated into Chinese.'),
            { text: finalText, response_source: 'shared_agent_loop' },
        );
        expect(result).toBe(finalText);
    });

    it('keeps a longer continuation that does not acknowledge the lookup nudge', () => {
        const finalText = '## 译文\n检索增强生成。';
        const streamed = `${finalText}\n\n${'补充了一段不涉及检索指令的说明。'.repeat(8)}`;
        expect(streamed.length).toBeGreaterThanOrEqual(finalText.length * 2);
        const result = resolveFinalRoundContent(
            makeMessage(streamed, 'The user wants an English abstract translated into Chinese.'),
            { text: finalText, response_source: 'shared_agent_loop' },
        );
        expect(result).toBe(streamed);
    });

    it('keeps the Layer 2 fragment guard ahead of the reasoning collapse', () => {
        // streamed >= 2x final → final text is a tail fragment, keep the
        // accumulated body even when a reasoning trail exists.
        const finalText = '尾部片段';
        const streamed = '很长的中间过程内容'.repeat(10) + finalText;
        const message = makeMessage(streamed, '思考过程');
        const result = resolveFinalRoundContent(message, { text: finalText, response_source: 'agent_loop' });
        expect(result).toBe(streamed);
    });

    it('keeps a continuation that mentions the lookup phrase after its opening', () => {
        const finalText = '## 译文\n检索增强生成。';
        const opening = '这里补上实验设置和指标的中文说明。'.repeat(5);
        const streamed = `${finalText}\n\n${opening}查询优先级按原文保留。`;
        expect(opening.length).toBeGreaterThanOrEqual(80);
        expect(streamed.length).toBeGreaterThanOrEqual(finalText.length * 2);
        const result = resolveFinalRoundContent(
            makeMessage(streamed, 'The user wants an English abstract translated into Chinese.'),
            { text: finalText, response_source: 'shared_agent_loop' },
        );
        expect(result).toBe(streamed);
    });

    it('leaves a reasoning-only deliverable in reasoning; the render path owns display lift', () => {
        const finalText = '评审意见上一条已给出。是否需要英文版？';
        const reasoning = 'Major comment: missing scratch-prompt baseline. '.repeat(20);
        const message = makeMessage(finalText, reasoning);
        const result = resolveFinalRoundContent(message, { text: finalText, response_source: 'agent_loop' });
        expect(result).toBe(finalText);
        expect(result).not.toContain('missing scratch-prompt baseline');
    });

    it('does not prepend a long trail onto Layer 2 streamed content at persist time', () => {
        const finalText = '请确认以上方案。';
        const streamed = '这是一份已经足够长的正式答复正文，不应当被思考过程覆盖。'.repeat(30) + '\n\n' + finalText;
        const reasoning = 'Major comment: missing scratch-prompt baseline. '.repeat(20);
        expect(streamed.length).toBeGreaterThanOrEqual(finalText.length * 2);
        const result = resolveFinalRoundContent(makeMessage(streamed, reasoning), {
            text: finalText,
            response_source: 'agent_loop',
        });
        expect(result).toBe(streamed);
        expect(result.startsWith('Major comment:')).toBe(false);
    });
});
