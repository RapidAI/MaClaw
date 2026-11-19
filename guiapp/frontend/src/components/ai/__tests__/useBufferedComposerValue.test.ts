import { act, renderHook } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { ChangeEvent, CompositionEvent } from 'react';
import { useBufferedComposerValue } from '../useBufferedComposerValue';

type Target = HTMLTextAreaElement | HTMLInputElement;

function changeEvent(value: string, isComposing?: boolean): ChangeEvent<Target> {
    return {
        target: { value } as Target,
        currentTarget: { value } as Target,
        nativeEvent: isComposing === undefined ? {} : { isComposing },
    } as ChangeEvent<Target>;
}

function compositionEvent(value: string): CompositionEvent<Target> {
    return {
        currentTarget: { value } as Target,
    } as CompositionEvent<Target>;
}

function setup(initial = '') {
    const commit = vi.fn();
    const view = renderHook(
        ({ value }) => useBufferedComposerValue(value, commit),
        { initialProps: { value: initial } },
    );
    return { commit, ...view };
}

describe('useBufferedComposerValue', () => {
    it('commits plain typing on every input', () => {
        const { result, commit } = setup();
        act(() => result.current.onChange(changeEvent('h')));
        act(() => result.current.onChange(changeEvent('he')));
        expect(commit.mock.calls.map(c => c[0])).toEqual(['h', 'he']);
        expect(result.current.isComposing).toBe(false);
        expect(result.current.value).toBe('');
    });

    it('buffers composing inputs and commits once on compositionend', () => {
        const { result, commit } = setup();
        act(() => result.current.onCompositionStart(compositionEvent('')));
        act(() => result.current.onChange(changeEvent('n', true)));
        act(() => result.current.onChange(changeEvent('ni', true)));
        act(() => result.current.onChange(changeEvent('你好', true)));
        expect(commit).not.toHaveBeenCalled();
        expect(result.current.isComposing).toBe(true);
        expect(result.current.value).toBe('你好');
        act(() => result.current.onCompositionEnd(compositionEvent('你好')));
        expect(commit.mock.calls.map(c => c[0])).toEqual(['你好']);
        expect(result.current.isComposing).toBe(false);
    });

    it('mirrors the DOM even when the host leaves isComposing unset', () => {
        const { result, commit } = setup();
        act(() => result.current.onCompositionStart(compositionEvent('')));
        act(() => result.current.onChange(changeEvent('w')));
        expect(commit).not.toHaveBeenCalled();
        expect(result.current.value).toBe('w');
    });

    it('seeds composition from a composing input that precedes compositionstart', () => {
        const { result, commit } = setup();
        act(() => result.current.onChange(changeEvent('n', true)));
        expect(commit).not.toHaveBeenCalled();
        expect(result.current.isComposing).toBe(true);
        act(() => result.current.onCompositionEnd(compositionEvent('你')));
        expect(commit.mock.calls.map(c => c[0])).toEqual(['你']);
    });

    it('flushes when the host drops compositionend and a committed input arrives', () => {
        const { result, commit } = setup();
        act(() => result.current.onCompositionStart(compositionEvent('')));
        act(() => result.current.onChange(changeEvent('ni', true)));
        // WebView2 dropped compositionend: the next input is already committed.
        act(() => result.current.onChange(changeEvent('你好', false)));
        expect(commit.mock.calls.map(c => c[0])).toEqual(['你好']);
        expect(result.current.isComposing).toBe(false);
    });

    it('masks external writes while composing and resyncs after the commit', () => {
        const { result, rerender, commit } = setup('draft');
        act(() => result.current.onCompositionStart(compositionEvent('draft')));
        rerender({ value: 'server overwrite' });
        act(() => result.current.onChange(changeEvent('draft你', true)));
        expect(result.current.value).toBe('draft你');
        act(() => result.current.onCompositionEnd(compositionEvent('draft你好')));
        rerender({ value: 'draft你好' });
        expect(result.current.value).toBe('draft你好');
        expect(commit.mock.calls.map(c => c[0])).toEqual(['draft你好']);
    });

    it('commits the latest text after a WebView2-style early compositionend', () => {
        const { result, commit } = setup();
        act(() => result.current.onCompositionStart(compositionEvent('')));
        act(() => result.current.onChange(changeEvent('ni', true)));
        // compositionend fires before the final DOM settles; a trailing
        // committed input follows with the real text.
        act(() => result.current.onCompositionEnd(compositionEvent('ni')));
        act(() => result.current.onChange(changeEvent('你好')));
        expect(commit.mock.calls.map(c => c[0])).toEqual(['ni', '你好']);
    });
});
