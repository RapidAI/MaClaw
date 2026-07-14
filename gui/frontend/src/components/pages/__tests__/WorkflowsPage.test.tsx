import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const startWorkflowTemplateMock = vi.hoisted(() => vi.fn());
const startWorkflowDirectMock = vi.hoisted(() => vi.fn());

vi.mock('../../../../wailsjs/go/main/App', () => ({
    StartWorkflowDirect: (...args: unknown[]) => startWorkflowDirectMock(...args),
    StartWorkflowTemplate: (...args: unknown[]) => startWorkflowTemplateMock(...args),
}));

import { WorkflowsPage } from '../WorkflowsPage';

describe('WorkflowsPage', () => {
    beforeEach(() => {
        vi.useFakeTimers();
        sessionStorage.clear();
        startWorkflowTemplateMock.mockReset();
        startWorkflowTemplateMock.mockResolvedValue('req-template');
        startWorkflowDirectMock.mockReset();
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

    it('starts coding workflow tile through the template API', async () => {
        const switchToAI = vi.fn();
        render(<WorkflowsPage lang="en" switchToAI={switchToAI} />);

        fireEvent.click(screen.getByRole('button', { name: /coding/i }));
        expect(switchToAI).toHaveBeenCalledTimes(1);

        await act(async () => {
            vi.advanceTimersByTime(60);
            await Promise.resolve();
        });
        expect(startWorkflowTemplateMock).toHaveBeenCalledWith('coding', '');
        expect(startWorkflowTemplateMock).toHaveBeenCalledTimes(1);
        expect(startWorkflowDirectMock).not.toHaveBeenCalled();

        const starting = JSON.parse(sessionStorage.getItem('maclaw:workflow-starting') || '{}');
        expect(starting.workflowType).toBe('coding');
        expect(starting.activateLocal).toBe(true);
    });

    it('does not expose removed simplified or remote coding workflow tiles', () => {
        render(<WorkflowsPage lang="en" switchToAI={vi.fn()} />);
        expect(screen.queryByRole('button', { name: /quick coding/i })).toBeNull();
        expect(screen.queryByRole('button', { name: /remote coding/i })).toBeNull();
    });
});
