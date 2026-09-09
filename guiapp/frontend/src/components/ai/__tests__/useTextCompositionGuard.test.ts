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

        expect(result.current.shouldIgnoreKeyDown(keyEvent('Enter'))).toBe(true);
        expect(result.current.shouldIgnoreKeyDown(keyEvent('ArrowUp'))).toBe(true);
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
