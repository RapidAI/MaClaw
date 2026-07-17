import { describe, expect, it } from 'vitest';
import { diarizedVoiceTurns } from '../useVoiceInput';

describe('diarizedVoiceTurns', () => {
    it('sorts speaker turns chronologically and labels only multi-speaker audio', () => {
        expect(diarizedVoiceTurns([
            { speaker: 1, start: 2, text: 'second turn' },
            { speaker: 0, start: 0.5, text: 'first turn' },
        ], '')).toEqual(['[Speaker 1] first turn', '[Speaker 2] second turn']);
    });

    it('retains the existing single-ASR fallback when diarization has no usable turns', () => {
        expect(diarizedVoiceTurns([{ speaker: 0, start: 0, text: '...' }], 'fallback text')).toEqual(['fallback text']);
    });

    it('does not add a speaker prefix for a single local speaker', () => {
        expect(diarizedVoiceTurns([{ speaker: 3, start: 0, text: 'one person speaks' }], '')).toEqual(['one person speaks']);
    });

    it('filters punctuation-only diarized turns before deciding whether fallback is needed', () => {
        expect(diarizedVoiceTurns([
            { speaker: 0, start: 0, text: '...' },
            { speaker: 1, start: 1, text: 'usable turn' },
        ], 'fallback text')).toEqual(['usable turn']);
    });

    it('makes fallback eligibility observable when every diarized turn is filtered', () => {
        expect(diarizedVoiceTurns([{ speaker: 0, start: 0, text: '...' }], '')).toEqual([]);
    });

    it('retains a speaker label as a separate prefix from the text sent to correction', () => {
        const turns = diarizedVoiceTurns([
            { speaker: 1, start: 0, text: 'please review this' },
            { speaker: 0, start: 1, text: 'I agree' },
        ], '');
        expect(turns[0]).toMatch(/^\[Speaker 2\] /);
        expect(turns[1]).toMatch(/^\[Speaker 1\] /);
    });

    it('keeps equal-timestamp turns in the backend order', () => {
        expect(diarizedVoiceTurns([
            { speaker: 1, start: 2, text: 'first at the same moment' },
            { speaker: 0, start: 2, text: 'second at the same moment' },
        ], '')).toEqual([
            '[Speaker 2] first at the same moment',
            '[Speaker 1] second at the same moment',
        ]);
    });
});
