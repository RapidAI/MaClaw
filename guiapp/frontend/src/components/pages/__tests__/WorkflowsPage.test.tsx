import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { WorkflowsPage } from '../WorkflowsPage';

describe('WorkflowsPage', () => {
    beforeEach(() => {
        vi.useFakeTimers();
        sessionStorage.clear();
    });

    afterEach(async () => {
        await act(async () => {
            vi.runOnlyPendingTimers();
            await Promise.resolve();
        });
        vi.useRealTimers();
        cleanup();
        sessionStorage.clear();
    });

    it('opens coding workflow tile in a dedicated assistant tab', async () => {
        const onStartWorkflow = vi.fn().mockResolvedValue(undefined);
        render(<WorkflowsPage lang="en" onStartWorkflow={onStartWorkflow} />);

        fireEvent.click(screen.getByRole('button', { name: /coding/i }));
        await act(async () => {
            await Promise.resolve();
        });
        expect(onStartWorkflow).toHaveBeenCalledWith('coding', 'Coding');
    });

    it('ignores a second click while a workflow tab is opening', async () => {
        let resolveStart!: () => void;
        const onStartWorkflow = vi.fn(() => new Promise<void>(resolve => { resolveStart = resolve; }));
        render(<WorkflowsPage lang="en" onStartWorkflow={onStartWorkflow} />);

        const coding = screen.getByRole('button', { name: /coding/i });
        fireEvent.click(coding);
        fireEvent.click(coding);
        expect(onStartWorkflow).toHaveBeenCalledTimes(1);

        await act(async () => {
            resolveStart();
            await Promise.resolve();
        });
        expect((coding as HTMLButtonElement).disabled).toBe(false);
    });

    it('shows an actionable error when opening a workflow tab fails', async () => {
        const onStartWorkflow = vi.fn().mockRejectedValue(new Error('task creation failed'));
        render(<WorkflowsPage lang="en" onStartWorkflow={onStartWorkflow} />);

        fireEvent.click(screen.getByRole('button', { name: /coding/i }));
        await act(async () => {
            await Promise.resolve();
        });
        expect(screen.getByRole('alert').textContent).toContain('Unable to open this workflow. Please try again.');
    });

    it('does not expose removed simplified or remote coding workflow tiles', () => {
        render(<WorkflowsPage lang="en" onStartWorkflow={vi.fn()} />);
        expect(screen.queryByRole('button', { name: /quick coding/i })).toBeNull();
        expect(screen.queryByRole('button', { name: /remote coding/i })).toBeNull();
    });
});
