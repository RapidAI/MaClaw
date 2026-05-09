// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { CodingAgentCompactStatus } from '../CodingAgentCompactStatus';

describe('CodingAgentCompactStatus', () => {
    it('renders the sidebar variant as a monitor-style status row', () => {
        render(
            <CodingAgentCompactStatus
                progress={{ phase: 'retrying', taskID: ' t3 ', title: '  Update tests (1/2)  ' }}
                lang="en"
                testId="compact-status"
                variant="sidebar"
            />,
        );

        const status = screen.getByTestId('compact-status');
        expect(status.textContent).toContain('Coding Agent');
        expect(status.textContent).toContain('Task status');
        expect(status.textContent).toContain('Retrying');
        expect(status.textContent).toContain('T3');
        expect(status.textContent).toContain('Update tests (1/2)');
        expect(status.getAttribute('aria-label')).toBe('Coding Agent | Task status | Retrying | T3 | Update tests (1/2)');
        expect(status.getAttribute('title')).toBe('Coding Agent | Task status | Retrying | T3 | Update tests (1/2)');
        expect(status.getAttribute('data-agent')).toBe('coding');
        expect(status.getAttribute('data-active')).toBe('true');
        expect(status.getAttribute('data-phase')).toBe('retrying');
        expect(status.getAttribute('data-terminal')).toBe('false');
        expect(status.getAttribute('data-task-id')).toBe('T3');
        expect(status.getAttribute('data-variant')).toBe('sidebar');
        expect(status.className).toContain('coding-agent-status--sidebar');
        expect(status.className).toContain('coding-agent-status--retrying');
        expect(status.style.display).toBe('grid');
        expect(status.style.borderLeft).toBe('3px solid rgb(217, 119, 6)');
        expect(status.style.whiteSpace).toBe('');
    });

    it('renders the bottom status-bar variant as a single-line compact badge', () => {
        render(
            <CodingAgentCompactStatus
                progress={{ phase: 'failed', taskID: 'T4', title: 'Apply patch' }}
                lang="zh-Hans"
                testId="compact-status"
                variant="status-bar"
            />,
        );

        const status = screen.getByTestId('compact-status');
        expect(status.textContent).toContain('\u7f16\u7a0b\u667a\u80fd\u4f53');
        expect(status.textContent).not.toContain('\u4efb\u52a1\u72b6\u6001');
        expect(status.textContent).toContain('\u5931\u8d25');
        expect(status.textContent).toContain('T4');
        expect(status.textContent).toContain('Apply patch');
        expect(status.getAttribute('role')).toBe('status');
        expect(status.getAttribute('aria-live')).toBe('polite');
        expect(status.getAttribute('aria-label')).toBe('\u7f16\u7a0b\u667a\u80fd\u4f53 | \u5931\u8d25 | T4 | Apply patch');
        expect(status.getAttribute('data-active')).toBe('false');
        expect(status.getAttribute('data-phase')).toBe('failed');
        expect(status.getAttribute('data-terminal')).toBe('true');
        expect(status.getAttribute('data-variant')).toBe('status-bar');
        expect(status.className).toContain('coding-agent-status--status-bar');
        expect(status.className).toContain('coding-agent-status--failed');
        expect(status.style.display).toBe('flex');
        expect(status.style.whiteSpace).toBe('nowrap');
        expect(status.style.color).toBe('rgb(220, 38, 38)');
    });

    it('renders a Chinese sidebar monitor label for coding-agent task status', () => {
        render(
            <CodingAgentCompactStatus
                progress={{ phase: 'running', taskID: 'T8', title: 'Fix monitor badge' }}
                lang="zh-Hans"
                testId="compact-status"
                variant="sidebar"
            />,
        );

        const status = screen.getByTestId('compact-status');
        expect(status.textContent).toContain('\u7f16\u7a0b\u667a\u80fd\u4f53');
        expect(status.textContent).toContain('\u4efb\u52a1\u72b6\u6001');
        expect(status.textContent).toContain('\u6267\u884c\u4e2d');
        expect(status.getAttribute('aria-label')).toBe('\u7f16\u7a0b\u667a\u80fd\u4f53 | \u4efb\u52a1\u72b6\u6001 | \u6267\u884c\u4e2d | T8 | Fix monitor badge');
    });

    it('renders compact coding-agent event metadata for diff summaries', () => {
        render(
            <CodingAgentCompactStatus
                progress={{ phase: 'result', taskID: 'T6', title: 'Update parser', event: 'diff_summary', detail: '3 files | 1 created', count: 3, files: ['a.go', 'b.go', 'new.go'] }}
                lang="en"
                testId="compact-status"
                variant="status-bar"
            />,
        );

        const status = screen.getByTestId('compact-status');
        expect(status.textContent).toContain('Coding Agent');
        expect(status.textContent).toContain('Result');
        expect(status.textContent).toContain('T6');
        expect(status.textContent).toContain('3 files | 1 created');
        expect(status.textContent).toContain('Update parser');
        expect(status.getAttribute('aria-label')).toBe('Coding Agent | Result | T6 | 3 files | 1 created | Update parser');
        expect(status.getAttribute('data-event')).toBe('diff_summary');
        expect(status.getAttribute('data-change-count')).toBe('3');
    });

    it('renders sidebar file previews for coding-agent diff summaries', () => {
        render(
            <CodingAgentCompactStatus
                progress={{ phase: 'result', taskID: 'T6', title: 'Update parser', event: 'diff_summary', count: 4, files: ['a.go', 'b.go', 'c.go', 'd.go'] }}
                lang="en"
                testId="compact-status"
                variant="sidebar"
            />,
        );

        const status = screen.getByTestId('compact-status');
        expect(status.textContent).toContain('Files');
        expect(status.textContent).toContain('a.go, b.go, c.go +1 more');
        expect(status.getAttribute('aria-label')).toBe('Coding Agent | Task status | Result | T6 | 4 changes | Update parser | a.go, b.go, c.go +1 more');
        expect(status.getAttribute('data-change-count')).toBe('4');
    });
});
