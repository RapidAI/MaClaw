import { describe, expect, it } from 'vitest';
import {
    classifyPictographCluster,
    formatToolProgressStatus,
    isToolProgressMessage,
    prepareChatBodyForDisplay,
    prepareChatBodyLines,
    splitTextByPictographClusters,
    stripDecorativePictographs,
    stripLeadingEmojiCluster,
    stripLineDecorativePictographs,
    stripLineLeadingEmoji,
    type InlineMarkVisual,
} from '../aiAssistantProgressUtils';

describe('aiAssistantProgressUtils', () => {
    it('prepareChatBodyForDisplay strips decorative clusters (leading + mid-sentence), keeps status/star marks', () => {
        expect(prepareChatBodyForDisplay('\u{1F680} Done.')).toBe('Done.');
        expect(prepareChatBodyForDisplay('  \u{1F4A1} Tip: use --force')).toBe('  Tip: use --force');
        // Each non-fence line start is cleaned.
        expect(prepareChatBodyForDisplay('Line one\n\u{1F680} line two')).toBe('Line one\nline two');
        // After markdown structural prefixes.
        expect(prepareChatBodyForDisplay('### \u{1F3AF} Goals')).toBe('### Goals');
        expect(prepareChatBodyForDisplay('- \u{1F4CC} note')).toBe('- note');
        // Mid-sentence decorative "AI flavor" (smile / thumbs-up) stripped.
        expect(prepareChatBodyForDisplay('Great plan \u{1F60A}')).toBe('Great plan');
        expect(prepareChatBodyForDisplay('Fully ok \u{1F44D} go ahead')).toBe('Fully ok go ahead');
        // Semantic status + star marks kept for SVG substitution at render time.
        expect(prepareChatBodyForDisplay('Score \u2B50\u2B50 is high')).toBe('Score \u2B50\u2B50 is high');
        expect(prepareChatBodyForDisplay('Good job \u2705 keep going')).toBe('Good job \u2705 keep going');
        expect(prepareChatBodyForDisplay('\u26A0 oil is high')).toBe('\u26A0 oil is high');
        expect(prepareChatBodyForDisplay('\u274C sugar')).toBe('\u274C sugar');
        // Fenced code keeps leading pictographs.
        expect(prepareChatBodyForDisplay('```\n\u{1F680} not stripped\n```')).toBe('```\n\u{1F680} not stripped\n```');
        expect(prepareChatBodyForDisplay('~~~~\n\u{1F680} not stripped\n~~~\n\u{1F4A1} still source\n~~~~')).toBe('~~~~\n\u{1F680} not stripped\n~~~\n\u{1F4A1} still source\n~~~~');
        expect(prepareChatBodyForDisplay('')).toBe('');
        expect(stripLeadingEmojiCluster('\u{1F50D} **/btw**')).toBe('**/btw**');
        // Idempotent + line-array form matches string form.
        const multi = '### \u{1F3AF} Goals\n- \u{1F4CC} note';
        const once = prepareChatBodyForDisplay(multi);
        expect(prepareChatBodyForDisplay(once)).toBe(once);
        expect(prepareChatBodyLines(multi.split('\n')).join('\n')).toBe(once);
        // Pictograph strip must not leave sticky lastIndex between calls.
        expect(stripDecorativePictographs('A \u{1F324} B')).toBe('A B');
        expect(stripDecorativePictographs('C \u{1F324} D')).toBe('C D');
        // Clean ASCII (no pictograph) returns same string reference when possible.
        const plain = 'plain ### heading\n- item';
        expect(prepareChatBodyForDisplay(plain)).toBe(plain);
        // Status/star-only marks: content preserved, original string identity kept when nothing decorative removed.
        const midOnly = 'Score \u2B50 high';
        expect(prepareChatBodyForDisplay(midOnly)).toBe(midOnly);
        // Heading without decorative mark must not rebuild the line.
        const heading = '### Goals';
        expect(stripLineLeadingEmoji(heading)).toBe(heading);
    });

    it('preserves FE0F on status marks when decorative marks are also stripped', () => {
        // VS16 must survive on the kept warn mark (do not global-strip FE0F after rebuild).
        const warnEmoji = '\u26A0\uFE0F';
        expect(prepareChatBodyForDisplay(`${warnEmoji} oil \u{1F60A}`)).toBe(`${warnEmoji} oil`);
        expect(stripLineDecorativePictographs(`${warnEmoji} keep \u{1F44D} go`)).toBe(`${warnEmoji} keep go`);
    });

    it('strips mid-line ZWJ decorative clusters without leaving joiners', () => {
        const family = '\u{1F468}\u200D\u{1F469}\u200D\u{1F467}';
        expect(prepareChatBodyForDisplay(`Team ${family} ready`)).toBe('Team ready');
        expect(prepareChatBodyForDisplay(`Team ${family} ready`)).not.toMatch(/\u200D/);
    });

    it('classifies and splits consecutive status/star marks for SVG swap', () => {
        expect(classifyPictographCluster('\u2705')).toEqual({ kind: 'status', status: 'ok' });
        expect(classifyPictographCluster('\u26A0\uFE0F')).toEqual({ kind: 'status', status: 'warn' });
        expect(classifyPictographCluster('\u2B50')).toEqual({ kind: 'star' });
        expect(classifyPictographCluster('\u{1F60A}')).toBeNull();
        const parts = splitTextByPictographClusters('A \u2705 B \u274C C \u2B50');
        const visuals = parts.filter((p): p is { cluster: string; visual: InlineMarkVisual } => typeof p !== 'string');
        expect(visuals.map((v) => v.visual.kind)).toEqual(['status', 'status', 'star']);
        // Decorative omitted from split output.
        expect(splitTextByPictographClusters('hi \u{1F60A}')).toEqual(['hi ']);
    });

    it('formats Skill progress into one compact Chinese status without pictographs', () => {
        expect(formatToolProgressStatus('\u{1f680} \u6b63\u5728\u6267\u884c Skill\u300cWeather Query \u{1f324}\u300d...', 'zh-Hans'))
            .toBe('\u6b63\u5728\u6267\u884c Weather Query');
        expect(formatToolProgressStatus('\u{1f680} \u6b63\u5728\u542f\u52a8 Skill\u300cWeather Query \u{1f324}\u300d...', 'zh-Hans'))
            .toBe('\u6b63\u5728\u542f\u52a8 Weather Query');
    });

    it('formats shell-style tool paths into the user-facing tool name without pictographs', () => {
        expect(formatToolProgressStatus('\u{1f680} \u6b63\u5728\u6267\u884c Shell /Weather Query \u{1f324} / ...', 'zh-Hans'))
            .toBe('\u6b63\u5728\u6267\u884c Weather Query');
        expect(formatToolProgressStatus('\u{1f680} executing Shell /Weather Query \u{1f324} / ...', 'en'))
            .toBe('Running Weather Query');
        expect(formatToolProgressStatus('\u{1f680} starting Shell /Weather Query \u{1f324} / ...', 'en'))
            .toBe('Starting Weather Query');
    });

    it('only treats progress rows with known tool prefixes as tool progress', () => {
        expect(isToolProgressMessage({ id: 'p1', role: 'progress', content: '\u{1f680} \u6b63\u5728\u6267\u884c Skill\u300cWeather Query\u300d...', timestamp: 1 })).toBe(true);
        expect(isToolProgressMessage({ id: 'p1-space', role: 'progress', content: '  \u{1f680} \u6b63\u5728\u6267\u884c Skill\u300cWeather Query\u300d...', timestamp: 1 })).toBe(true);
        expect(isToolProgressMessage({ id: 'p1-generic', role: 'progress', content: '\u{1f680} working', timestamp: 1 })).toBe(false);
        expect(isToolProgressMessage({ id: 'p2', role: 'progress', content: 'working', timestamp: 1 })).toBe(false);
        expect(isToolProgressMessage({ id: 'a1', role: 'assistant', content: '\u{1f680} working', timestamp: 1 })).toBe(false);
    });

    it('strips ZWJ sequences from skill labels without leaving joiners', () => {
        // family: man + ZWJ + woman + ZWJ + girl
        const zwjSkill = `\u{1F680} \u6b63\u5728\u6267\u884c Skill\u300cTeam \u{1F468}\u200D\u{1F469}\u200D\u{1F467}\u300d...`;
        expect(formatToolProgressStatus(zwjSkill, 'zh-Hans')).toBe('\u6b63\u5728\u6267\u884c Team');
        expect(formatToolProgressStatus(zwjSkill, 'zh-Hans')).not.toMatch(/\u200D/);
    });
});
