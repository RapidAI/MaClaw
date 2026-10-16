import { describe, expect, it } from 'vitest';
import { buildPromptLineDiff, changedNames, omitOptimizationReviewFields, summarizePromptDiff } from '../expertOptimizationDiff';

describe('expertOptimizationDiff', () => {
    it('keeps context and presents a replacement as removed before added', () => {
        expect(buildPromptLineDiff('角色\n旧规则\n输出', '角色\n新规则\n输出')).toEqual([
            { kind: 'same', text: '角色' },
            { kind: 'removed', text: '旧规则' },
            { kind: 'added', text: '新规则' },
            { kind: 'same', text: '输出' },
        ]);
    });

    it('normalizes CRLF prompts and exposes only changed capabilities', () => {
        expect(buildPromptLineDiff('A\r\nB', 'A\nB')).toEqual([
            { kind: 'same', text: 'A' },
            { kind: 'same', text: 'B' },
        ]);
        expect(changedNames(['read_file', 'ssh'], ['read_file', 'web_search'])).toEqual({
            added: ['web_search'],
            removed: ['ssh'],
        });
    });

    it('takes the linear no-change path for a normalized prompt', () => {
        const prompt = Array.from({ length: 800 }, (_, index) => `规则 ${index}`).join('\n');
        const diff = buildPromptLineDiff(prompt.replace(/\n/g, '\r\n'), prompt);
        expect(diff).toHaveLength(800);
        expect(diff.every((line) => line.kind === 'same')).toBe(true);
    });

    it('keeps surrounding context for a small edit in a long prompt', () => {
        const source = Array.from({ length: 800 }, (_, index) => `rule ${index}`);
        const optimized = [...source];
        optimized[400] = 'optimized rule 400';

        const diff = buildPromptLineDiff(source.join('\n'), optimized.join('\n'));

        expect(diff).toHaveLength(801);
        expect(diff[399]).toEqual({ kind: 'same', text: 'rule 399' });
        expect(diff[400]).toEqual({ kind: 'removed', text: 'rule 400' });
        expect(diff[401]).toEqual({ kind: 'added', text: 'optimized rule 400' });
        expect(diff[402]).toEqual({ kind: 'same', text: 'rule 401' });
    });

    it('summarizes added and removed lines without counting retained context', () => {
        expect(summarizePromptDiff([
            { kind: 'same', text: '角色' },
            { kind: 'removed', text: '旧规则' },
            { kind: 'added', text: '新规则' },
            { kind: 'added', text: '补充约束' },
        ])).toEqual({ added: 2, removed: 1 });
    });

    it('strips optimization-review-only metadata at the save boundary', () => {
        expect(omitOptimizationReviewFields({
            name: '优化专家',
            system_prompt: '正式专家提示词',
            source_name: '原专家',
            source_system_prompt: '差异展示原提示词',
            source_tools: ['ssh'],
            source_skills: ['pptx-gen'],
            update_existing: true,
        })).toEqual({
            name: '优化专家',
            system_prompt: '正式专家提示词',
        });
    });
});
