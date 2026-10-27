import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { KnowledgeImportProvider, useKnowledgeImport } from '../KnowledgeImportContext';

const mocks = vi.hoisted(() => ({
    status: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', async importOriginal => ({
    ...(await importOriginal() as object),
    KnowledgeImportJobStatus: mocks.status,
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(() => vi.fn()),
    EventsOff: vi.fn(),
}));

function ImportHarness() {
    const { job, publishJob } = useKnowledgeImport();
    return (
        <button onClick={() => publishJob({ id: 'job_1', status: 'running' })}>
            job:{job?.status || 'none'}
        </button>
    );
}

function renderProvider() {
    return render(
        <KnowledgeImportProvider>
            <ImportHarness />
        </KnowledgeImportProvider>,
    );
}

describe('KnowledgeImportProvider polling', () => {
    beforeEach(() => {
        vi.useFakeTimers();
        Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
        mocks.status.mockResolvedValue({ id: 'job_1', status: 'running' });
    });

    afterEach(() => {
        cleanup();
        vi.useRealTimers();
        vi.clearAllMocks();
        Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
    });

    it('pauses while hidden and refreshes immediately when visible', async () => {
        renderProvider();
        fireEvent.click(screen.getByRole('button'));
        expect(mocks.status).not.toHaveBeenCalled();
        await act(async () => {
            vi.advanceTimersByTime(2000);
            await Promise.resolve();
        });
        expect(mocks.status).toHaveBeenCalledTimes(1);

        act(() => {
            Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
            document.dispatchEvent(new Event('visibilitychange'));
            vi.advanceTimersByTime(6000);
        });
        expect(mocks.status).toHaveBeenCalledTimes(1);

        await act(async () => {
            Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
            document.dispatchEvent(new Event('visibilitychange'));
            await Promise.resolve();
        });
        expect(mocks.status).toHaveBeenCalledTimes(2);
    });

    it('stops polling after a terminal snapshot', async () => {
        mocks.status.mockResolvedValue({ id: 'job_1', status: 'completed' });
        renderProvider();
        fireEvent.click(screen.getByRole('button'));
        await act(async () => {
            vi.advanceTimersByTime(2000);
            await Promise.resolve();
        });
        expect(mocks.status).toHaveBeenCalledTimes(1);
        expect(screen.getByRole('button', { name: /job:/ }).textContent).toBe('job:completed');
        await act(async () => {
            vi.advanceTimersByTime(10000);
            await Promise.resolve();
        });
        expect(mocks.status).toHaveBeenCalledTimes(1);
    });
});
