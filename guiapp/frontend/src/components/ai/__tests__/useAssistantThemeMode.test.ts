import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AI_THEME_MODE_LEGACY_STORAGE_KEY, AI_THEME_MODE_STORAGE_KEY } from '../aiAssistantPanelTheme';
import { readStoredAssistantThemeMode, writeStoredAssistantThemeMode } from '../assistantThemeStorage';
import { useAssistantThemeMode } from '../useAssistantThemeMode';

describe('useAssistantThemeMode', () => {
    beforeEach(() => {
        window.localStorage.clear();
        vi.clearAllMocks();
    });

    it('writes both shared and legacy theme keys', () => {
        writeStoredAssistantThemeMode('dark');

        expect(readStoredAssistantThemeMode()).toBe('dark');
        expect(window.localStorage.getItem(AI_THEME_MODE_STORAGE_KEY)).toBe('dark');
        expect(window.localStorage.getItem(AI_THEME_MODE_LEGACY_STORAGE_KEY)).toBe('dark');
    });

    it('does not revert a local theme toggle while the controlled prop is still stale', async () => {
        const onThemeModeChange = vi.fn();
        const { result } = renderHook(() => useAssistantThemeMode('light', onThemeModeChange));

        expect(result.current.themeMode).toBe('light');

        act(() => {
            result.current.setThemeMode('dark');
        });

        await waitFor(() => {
            expect(result.current.themeMode).toBe('dark');
            expect(window.localStorage.getItem(AI_THEME_MODE_STORAGE_KEY)).toBe('dark');
        });
    });

    it('reads the legacy theme key when the shared key is not present', () => {
        window.localStorage.setItem(AI_THEME_MODE_LEGACY_STORAGE_KEY, 'dark');

        const { result } = renderHook(() => useAssistantThemeMode(undefined));

        expect(result.current.themeMode).toBe('dark');
    });

    it('syncs when the controlled theme prop actually changes', async () => {
        const onThemeModeChange = vi.fn();
        const { result, rerender } = renderHook(
            ({ controlledThemeMode }) => useAssistantThemeMode(controlledThemeMode, onThemeModeChange),
            { initialProps: { controlledThemeMode: 'light' as 'light' | 'dark' } },
        );

        rerender({ controlledThemeMode: 'dark' });

        await waitFor(() => {
            expect(result.current.themeMode).toBe('dark');
        });

        rerender({ controlledThemeMode: 'light' });

        await waitFor(() => {
            expect(result.current.themeMode).toBe('light');
        });
    });
});
