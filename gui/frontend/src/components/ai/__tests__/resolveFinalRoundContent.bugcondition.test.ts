import { describe, it, expect, vi } from 'vitest';
import { resolveFinalRoundContent, type ChatMessage } from '../useAIAssistant';

/**
 * Bug Condition Exploration Test — Agent Loop Content Loss
 *
 * **Validates: Requirements 1.1, 1.3, 1.5, 2.2, 2.3, 2.5**
 *
 * These tests encode the EXPECTED (correct) behavior of resolveFinalRoundContent.
 * On UNFIXED code, they MUST FAIL — failure confirms the bug exists.
 *
 * Bug condition (from design):
 *   streamedContent IS NOT EMPTY
 *   AND finalText IS NOT EMPTY
 *   AND streamedContent.length >= finalText.length * 2
 *   AND NOT streamedContent.endsWith(finalText)
 *   AND (responseSource IS UNDEFINED OR responseSource == 'agent_loop')
 *
 * Current buggy behavior: endsWith fails → falls through to `if (finalText) return finalText`,
 * replacing the complete multi-round streamed output with a short last-round fragment.
 */

function makeMessage(content: string): ChatMessage {
    return {
        id: 'msg-test-1',
        role: 'assistant',
        content,
        timestamp: Date.now(),
    };
}

describe('resolveFinalRoundContent — Bug Condition Exploration', () => {
    it('Case 1: Long document (3000 chars) + short non-suffix confirmation (200 chars), no response_source → should return streamedContent', () => {
        // Simulate: LLM outputs a 3000-char requirements document via streaming,
        // then agent loop continues and the last round produces a 200-char
        // confirmation prompt with different wording.
        const streamedContent = '# 需求文档\n\n' + '这是一段详细的需求描述内容。'.repeat(150) + '\n\n请确认以上需求文档是否满足您的要求。';
        const finalText = '\u{1F4C4} 已生成需求文档的 PDF 版本，请查看并确认需求是否准确，或提出修改意见。如需修改请告诉我具体的修改点，我会立即更新文档。';

        // Verify bug condition holds
        expect(streamedContent.length).toBeGreaterThanOrEqual(finalText.length * 2);
        expect(streamedContent.endsWith(finalText)).toBe(false);

        const message = makeMessage(streamedContent);
        const response = { text: finalText };

        const result = resolveFinalRoundContent(message, response);

        // Expected: preserve streamedContent (the complete document)
        // Bug: returns finalText (short confirmation) instead
        expect(result).toBe(streamedContent);
    });

    it('Case 2: Wording variation — streamedContent ends with "请确认需求文档", finalText = "请查看并确认上述需求" → should return streamedContent', () => {
        // Simulate: LLM outputs document ending with one confirmation phrase,
        // then agent loop produces a different confirmation phrase.
        const documentBody = '## 功能需求\n\n' + '1. 用户登录功能\n2. 数据导出功能\n3. 权限管理功能\n'.repeat(30);
        const streamedContent = documentBody + '\n\n请确认需求文档';
        const finalText = '请查看并确认上述需求';

        // Verify bug condition holds
        expect(streamedContent.length).toBeGreaterThanOrEqual(finalText.length * 2);
        expect(streamedContent.endsWith(finalText)).toBe(false);

        const message = makeMessage(streamedContent);
        const response = { text: finalText };

        const result = resolveFinalRoundContent(message, response);

        // Expected: preserve streamedContent (the complete document)
        // Bug: returns finalText (short text) instead
        expect(result).toBe(streamedContent);
    });

    it('Case 3: Trailing whitespace difference — streamedContent ends with \\n\\n, finalText ends with \\n → should return streamedContent', () => {
        // Simulate: streaming accumulated content has trailing \n\n,
        // but response.text has the same text with trailing \n only.
        // endsWith fails due to whitespace mismatch.
        const baseContent = '# 技术设计文档\n\n' + '模块设计详细说明。\n'.repeat(200);
        const streamedContent = baseContent + '以上是完整的技术设计方案。\n\n';
        const finalText = '以上是完整的技术设计方案。\n';

        // Verify bug condition holds
        expect(streamedContent.length).toBeGreaterThanOrEqual(finalText.length * 2);
        expect(streamedContent.endsWith(finalText)).toBe(false);

        const message = makeMessage(streamedContent);
        const response = { text: finalText };

        const result = resolveFinalRoundContent(message, response);

        // Expected: preserve streamedContent (the complete document)
        // Bug: returns finalText (short trailing fragment) instead
        expect(result).toBe(streamedContent);
    });

    it('Case 4: response_source = "agent_loop" with long streamed content and short non-suffix final text → should return streamedContent', () => {
        // Simulate: backend explicitly marks response_source as 'agent_loop',
        // confirming this is a normal agent loop last-round output (not ask_user/cancel/etc).
        const streamedContent = '# 任务拆分\n\n' + '- [ ] 任务项描述内容\n'.repeat(200) + '\n确认后开始执行。';
        const finalText = '已为您生成任务列表，请查看并确认是否开始执行。';

        // Verify bug condition holds
        expect(streamedContent.length).toBeGreaterThanOrEqual(finalText.length * 2);
        expect(streamedContent.endsWith(finalText)).toBe(false);

        const message = makeMessage(streamedContent);
        const response = { text: finalText, response_source: 'agent_loop' };

        const result = resolveFinalRoundContent(message, response);

        // Expected: preserve streamedContent (the complete task list)
        // Bug: returns finalText (short confirmation) instead
        expect(result).toBe(streamedContent);
    });

    it('Case 5: streamed Browser role block after a valid answer is truncated', () => {
        const answer = 'Valid overview summary.';
        const duplicateTail = 'Browser: duplicated overview summary.';
        const streamedContent = answer + '\n\n' + duplicateTail;
        const finalText = answer;

        const result = resolveFinalRoundContent(makeMessage(streamedContent), { text: finalText, response_source: 'agent_loop' });

        expect(result).toBe(answer);
        expect(result).not.toContain('Browser:');
    });

    it('Case 6: final response text with Browser prefix is stripped before display', () => {
        const finalText = 'Browser: PPT 已生成并打开！文件 113KB，暗色主题。';

        const result = resolveFinalRoundContent(makeMessage(''), { text: finalText });

        expect(result).toBe('PPT 已生成并打开！文件 113KB，暗色主题。');
        expect(result).not.toContain('Browser:');
    });

    it('Case 7: streamed Browser prefix is stripped even when the final text is empty', () => {
        const streamedContent = 'Browser: PPT 已生成并打开！文件 113KB，暗色主题。';

        const result = resolveFinalRoundContent(makeMessage(streamedContent), { text: '' });

        expect(result).toBe('PPT 已生成并打开！文件 113KB，暗色主题。');
        expect(result).not.toContain('Browser:');
    });

    it('Case 8: file delivery ignores streamed Browser residue and uses final delivery text', () => {
        const streamedContent = 'Browser: generated deck is ready and opened.';

        const result = resolveFinalRoundContent(makeMessage(streamedContent), {
            text: '',
            response_source: 'file_delivery',
            local_file_paths: ['C:/tmp/demo.pptx'],
        });

        expect(result).toBe('');
        expect(result).not.toContain('Browser:');
    });

    it('Case 9: file paths suppress streamed Browser residue even if response_source is missing', () => {
        const streamedContent = 'Browser: generated deck is ready and opened.';

        const result = resolveFinalRoundContent(makeMessage(streamedContent), {
            text: '',
            LocalFilePaths: ['C:/tmp/demo.pptx'],
        });

        expect(result).toBe('');
        expect(result).not.toContain('Browser:');
    });

    it('Case 10: file delivery honors Go-style ResponseSource casing', () => {
        const streamedContent = 'Browser: generated deck is ready and opened.';

        const result = resolveFinalRoundContent(makeMessage(streamedContent), {
            text: '',
            ResponseSource: 'file_delivery',
            LocalFilePath: 'C:/tmp/demo.pptx',
        });

        expect(result).toBe('');
        expect(result).not.toContain('Browser:');
    });

    it('Case 10a: file delivery normalizes response_source before source matching', () => {
        const streamedContent = 'Browser: generated deck is ready and opened.';
        const finalText = 'Deck generated and saved.';

        const result = resolveFinalRoundContent(makeMessage(streamedContent), {
            text: finalText,
            response_source: ' File_Delivery ',
            local_file_paths: ['C:/tmp/demo.pptx'],
        });

        expect(result).toBe(finalText);
        expect(result).not.toContain('Browser:');
    });

    it('Case 10a.1: file delivery canonicalizes non-underscore response_source variants', () => {
        const streamedContent = 'Browser: generated deck is ready and opened.';
        const finalText = 'Deck generated and saved.';

        for (const responseSource of ['file-delivery', 'FileDelivery']) {
            const result = resolveFinalRoundContent(makeMessage(streamedContent), {
                text: finalText,
                response_source: responseSource,
                local_file_paths: ['C:/tmp/demo.pptx'],
            });

            expect(result).toBe(finalText);
            expect(result).not.toContain('Browser:');
        }
    });

    it('Case 10b: inferred local file delivery ignores streamed Browser residue when response_source is missing', () => {
        const streamedContent = 'Browser: generated deck is ready and opened.';
        const finalText = 'Deck generated and saved.';

        const result = resolveFinalRoundContent(makeMessage(streamedContent), {
            text: finalText,
            local_file_paths: ['C:/tmp/demo.pptx'],
        });

        expect(result).toBe(finalText);
        expect(result).not.toContain('Browser:');
    });

    it('Case 10c: inferred screenshot delivery ignores streamed Browser residue when response_source is missing', () => {
        const streamedContent = 'Browser: screenshot captured.';
        const finalText = 'Screenshot saved.';

        const result = resolveFinalRoundContent(makeMessage(streamedContent), {
            text: finalText,
            local_file_path: 'C:/tmp/screenshot.png',
            thumbnail_base64: 'abc123',
        });

        expect(result).toBe(finalText);
        expect(result).not.toContain('Browser:');
    });

    it('Case 10d: inferred Go-style file delivery ignores streamed Browser residue when response_source is missing', () => {
        const streamedContent = 'Browser: generated deck is ready and opened.';
        const finalText = 'Deck generated and saved.';

        const result = resolveFinalRoundContent(makeMessage(streamedContent), {
            text: finalText,
            LocalFilePaths: ['C:/tmp/demo.pptx'],
        });

        expect(result).toBe(finalText);
        expect(result).not.toContain('Browser:');
    });

    it('Case 10e: artifact payload overrides explicit agent_loop source to ignore streamed Browser residue', () => {
        const streamedContent = 'Browser: generated deck is ready and opened with extra streamed residue.';
        const finalText = 'Deck generated and saved.';

        const result = resolveFinalRoundContent(makeMessage(streamedContent), {
            text: finalText,
            response_source: 'agent_loop',
            local_file_paths: ['C:/tmp/demo.pptx'],
        });

        expect(result).toBe(finalText);
        expect(result).not.toContain('Browser:');
    });

    it('Case 10f: Go-style screenshot artifact overrides explicit agent_loop source', () => {
        const streamedContent = 'Browser: screenshot captured with stale stream residue.';
        const finalText = 'Screenshot saved.';

        const result = resolveFinalRoundContent(makeMessage(streamedContent), {
            text: finalText,
            ResponseSource: 'agent_loop',
            LocalFilePath: 'C:/tmp/screenshot.png',
            ThumbnailBase64: 'abc123',
        });

        expect(result).toBe(finalText);
        expect(result).not.toContain('Browser:');
    });

    it('Case 10g: image key artifact overrides explicit agent_loop source', () => {
        const streamedContent = 'Browser: screenshot captured with stale stream residue.';
        const finalText = 'Screenshot saved.';

        const result = resolveFinalRoundContent(makeMessage(streamedContent), {
            text: finalText,
            response_source: 'agent_loop',
            image_key: 'screenshot-123',
        });

        expect(result).toBe(finalText);
        expect(result).not.toContain('Browser:');
    });

    it('Case 11: role-prefix diagnostics do not log content snippets', () => {
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => { });
        try {
            resolveFinalRoundContent(makeMessage('Browser: sensitive generated text'), { text: '' });
            const rolePrefixCall = warnSpy.mock.calls.find(call => call[0] === '[ai-role-prefix]');
            expect(rolePrefixCall).toBeTruthy();
            const payload = rolePrefixCall?.[1] as Record<string, unknown>;
            expect(payload.beforeSnippet).toBeUndefined();
            expect(payload.afterSnippet).toBeUndefined();
            expect(payload.beforeHasRolePrefix).toBe(true);
            expect(payload.beforeRolePrefixKind).toBe('Browser');
        } finally {
            warnSpy.mockRestore();
        }
    });

    it('Case 12: role-prefix diagnostics ignore inline Browser mentions', () => {
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => { });
        try {
            const result = resolveFinalRoundContent(makeMessage('The Browser: tool already closed cleanly.'), { text: '' });

            expect(result).toBe('The Browser: tool already closed cleanly.');
            expect(warnSpy).not.toHaveBeenCalledWith('[ai-role-prefix]', expect.anything());
        } finally {
            warnSpy.mockRestore();
        }
    });
});
