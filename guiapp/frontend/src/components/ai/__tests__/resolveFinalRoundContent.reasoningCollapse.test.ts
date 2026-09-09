import { describe, expect, it } from 'vitest';
import { resolveFinalRoundContent, type ChatMessage } from '../useAIAssistant';

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

    it('keeps the Layer 2 fragment guard ahead of the reasoning collapse', () => {
        // streamed >= 2x final → final text is a tail fragment, keep the
        // accumulated body even when a reasoning trail exists.
        const finalText = '尾部片段';
        const streamed = '很长的中间过程内容'.repeat(10) + finalText;
        const message = makeMessage(streamed, '思考过程');
        const result = resolveFinalRoundContent(message, { text: finalText, response_source: 'agent_loop' });
        expect(result).toBe(streamed);
    });
});
