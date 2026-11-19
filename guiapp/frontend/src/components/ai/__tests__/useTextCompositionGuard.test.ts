import { renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { KeyboardEvent } from 'react';
import { useTextCompositionGuard } from '../useTextCompositionGuard';

function keyEvent(key: string, nativeEvent: Partial<KeyboardEvent<HTMLTextAreaElement>['nativeEvent']> = {}) {
    return {
        key,
        nativeEvent: {
            isComposing: false,
            keyCode: 0,
            ...nativeEvent,
        },
    } as KeyboardEvent<HTMLTextAreaElement>;
}

describe('useTextCompositionGuard', () => {
    afterEach(() => {
        vi.useRealTimers();
    });

    it('ignores keydown while composition is active', () => {
        const { result } = renderHook(() => useTextCompositionGuard());

        result.current.onCompositionStart();

        // Real hosts always mark composing keydowns (isComposing / keyCode 229).
        expect(result.current.shouldIgnoreKeyDown(keyEvent('Enter', { isComposing: true }))).toBe(true);
        expect(result.current.shouldIgnoreKeyDown(keyEvent('ArrowUp', { keyCode: 229 }))).toBe(true);
    });

    it('recovers when the host drops compositionend and a plain keydown arrives', () => {
        const { result } = renderHook(() => useTextCompositionGuard());

        result.current.onCompositionStart();

        // No compositionend: an unmarked Enter is the ambiguous commit-Enter,
        // swallowed like the post-compositionend grace window. The guard then
        // un-sticks so later keys are processed normally.
        expect(result.current.shouldIgnoreKeyDown(keyEvent('Enter'))).toBe(true);
        expect(result.current.shouldIgnoreKeyDown(keyEvent('Enter'))).toBe(false);
        expect(result.current.shouldIgnoreKeyDown(keyEvent('ArrowUp'))).toBe(false);
    });

    it('recovers when the host drops compositionend and a plain non-Enter keydown arrives', () => {
        const { result } = renderHook(() => useTextCompositionGuard());

        result.current.onCompositionStart();

        expect(result.current.shouldIgnoreKeyDown(keyEvent('a'))).toBe(false);
        expect(result.current.shouldIgnoreKeyDown(keyEvent('ArrowUp'))).toBe(false);
    });

    it('ignores platform-level IME keydown markers', () => {
        const { result } = renderHook(() => useTextCompositionGuard());

        expect(result.current.shouldIgnoreKeyDown(keyEvent('Enter', { isComposing: true }))).toBe(true);
        expect(result.current.shouldIgnoreKeyDown(keyEvent('Enter', { keyCode: 229 }))).toBe(true);
        expect(result.current.shouldIgnoreKeyDown(keyEvent('Process'))).toBe(true);
    });

    it('consumes only the first Enter shortly after compositionend', () => {
        vi.useFakeTimers();
        vi.setSystemTime(1000);
        const { result } = renderHook(() => useTextCompositionGuard());

        result.current.onCompositionStart();
        result.current.onCompositionEnd();
        vi.setSystemTime(1010);

        expect(result.current.shouldIgnoreKeyDown(keyEvent('Enter'))).toBe(true);
        expect(result.current.shouldIgnoreKeyDown(keyEvent('Enter'))).toBe(false);
    });

    it('consumes the pending commit when the post-composition Enter also has an IME marker', () => {
        vi.useFakeTimers();
        vi.setSystemTime(1000);
        const { result } = renderHook(() => useTextCompositionGuard());

        result.current.onCompositionStart();
        result.current.onCompositionEnd();
        vi.setSystemTime(1010);

        expect(result.current.shouldIgnoreKeyDown(keyEvent('Enter', { keyCode: 229 }))).toBe(true);
        expect(result.current.shouldIgnoreKeyDown(keyEvent('Enter'))).toBe(false);
    });

    it('does not consume Enter after the IME commit window expires', () => {
        vi.useFakeTimers();
        vi.setSystemTime(1000);
        const { result } = renderHook(() => useTextCompositionGuard());

        result.current.onCompositionStart();
        result.current.onCompositionEnd();
        vi.setSystemTime(1070);

        expect(result.current.shouldIgnoreKeyDown(keyEvent('Enter'))).toBe(false);
    });
});
