import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { EVENT_COMPUTER_USE_OBSERVE } from '../../../constants/events';

const { getStatusMock, listeners } = vi.hoisted(() => ({
    getStatusMock: vi.fn(),
    listeners: new Map<string, (data?: unknown) => void>(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    ComputerUsePause: vi.fn(),
    ComputerUseReset: vi.fn(),
    ComputerUseResume: vi.fn(),
    ComputerUseStop: vi.fn(),
    GetComputerUseStatus: (...args: unknown[]) => getStatusMock(...args),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: (event: string, callback: (data?: unknown) => void) => listeners.set(event, callback),
    EventsOff: (event: string) => listeners.delete(event),
}));

import { ComputerUseQuickBar } from '../ComputerUseQuickBar';

const theme = {} as any;

function setDocumentHidden(hidden: boolean) {
    Object.defineProperty(document, 'hidden', { configurable: true, value: hidden });
    document.dispatchEvent(new Event('visibilitychange'));
}

async function flushStatus() {
    await act(async () => {});
}

describe('ComputerUseQuickBar polling', () => {
    beforeEach(() => {
        vi.useFakeTimers();
        listeners.clear();
        getStatusMock.mockReset().mockResolvedValue({ enabled: true, session_active: false });
        Object.defineProperty(document, 'hidden', { configurable: true, value: false });
    });

    afterEach(() => {
        cleanup();
        vi.useRealTimers();
    });

    it('uses events while idle instead of polling', async () => {
        render(<ComputerUseQuickBar lang="en" theme={theme} />);
        await flushStatus();
        expect(getStatusMock).toHaveBeenCalledTimes(1);

        act(() => {
            vi.advanceTimersByTime(12000);
        });
        expect(getStatusMock).toHaveBeenCalledTimes(1);

        act(() => listeners.get(EVENT_COMPUTER_USE_OBSERVE)?.({ ok: true }));
        await flushStatus();
        expect(getStatusMock).toHaveBeenCalledTimes(2);
    });

    it('pauses while the document is hidden and refreshes immediately when visible', async () => {
        getStatusMock.mockResolvedValue({ enabled: true, session_active: true, step_count: 2 });
        render(<ComputerUseQuickBar lang="en" theme={theme} />);
        await flushStatus();
        expect(screen.getByTestId('computer-use-quick-bar')).not.toBeNull();

        act(() => {
            vi.advanceTimersByTime(4000);
        });
        await flushStatus();
        expect(getStatusMock).toHaveBeenCalledTimes(2);

        setDocumentHidden(true);
        getStatusMock.mockClear();
        act(() => {
            vi.advanceTimersByTime(8000);
            listeners.get(EVENT_COMPUTER_USE_OBSERVE)?.({ ok: true });
        });
        expect(getStatusMock).not.toHaveBeenCalled();

        setDocumentHidden(false);
        await flushStatus();
        expect(getStatusMock).toHaveBeenCalledTimes(1);
    });
});
