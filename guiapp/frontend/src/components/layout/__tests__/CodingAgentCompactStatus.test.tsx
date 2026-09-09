// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { CODING_AGENT_FAILURE_ACCENT_DARK } from '../../ai/CodingAgentProgressStatus';
import { CodingAgentCompactStatus } from '../CodingAgentCompactStatus';

describe('CodingAgentCompactStatus', () => {
    afterEach(() => {
        document.getElementById('App')?.removeAttribute('data-ai-theme');
    });

    it('renders the sidebar variant as a dense professional status row', () => {
        render(
            <CodingAgentCompactStatus
                progress={{ phase: 'retrying', taskID: ' t3 ', title: '  Update tests (1/2)  ' }}
                lang="en"
                testId="compact-status"
                variant="sidebar"
            />,
        );

        const status = screen.getByTestId('compact-status');
        expect(status.textContent).toContain('Coding');
        expect(status.textContent).not.toContain('Task status');
        expect(status.textContent).toContain('Retrying');
        expect(status.textContent).toContain('T3');
        expect(status.textContent).toContain('Update tests (1/2)');
        expect(status.getAttribute('aria-label')).toBe('Coding \u00b7 Retrying \u00b7 T3 \u00b7 Update tests (1/2)');
        expect(status.getAttribute('title')).toBe('Coding \u00b7 Retrying \u00b7 T3 \u00b7 Update tests (1/2)');
        expect(status.getAttribute('data-agent')).toBe('coding');
        expect(status.getAttribute('data-active')).toBe('true');
        expect(status.getAttribute('data-phase')).toBe('retrying');
        expect(status.getAttribute('data-terminal')).toBe('false');
        expect(status.getAttribute('data-task-id')).toBe('T3');
        expect(status.getAttribute('data-variant')).toBe('sidebar');
        expect(status.className).toContain('coding-agent-status--sidebar');
        expect(status.className).toContain('coding-agent-status--retrying');
        expect(status.style.display).toBe('grid');
        expect(status.style.border).toBe('1px solid rgba(100, 116, 139, 0.2)');
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
        expect(status.textContent).toContain('\u7f16\u7a0b');
        expect(status.textContent).not.toContain('\u7f16\u7a0b\u667a\u80fd\u4f53');
        expect(status.textContent).not.toContain('\u4efb\u52a1\u72b6\u6001');
        expect(status.textContent).toContain('\u5931\u8d25');
        expect(status.textContent).toContain('T4');
        expect(status.textContent).toContain('Apply patch');
        expect(status.getAttribute('role')).toBe('status');
        expect(status.getAttribute('aria-live')).toBe('polite');
        expect(status.getAttribute('aria-label')).toBe('\u7f16\u7a0b \u00b7 \u5931\u8d25 \u00b7 T4 \u00b7 Apply patch');
        expect(status.getAttribute('data-active')).toBe('false');
        expect(status.getAttribute('data-phase')).toBe('failed');
        expect(status.getAttribute('data-terminal')).toBe('true');
        expect(status.getAttribute('data-variant')).toBe('status-bar');
        expect(status.className).toContain('coding-agent-status--status-bar');
        expect(status.className).toContain('coding-agent-status--failed');
        expect(status.style.display).toBe('flex');
        expect(status.style.whiteSpace).toBe('nowrap');
        // Soft amber failure tone (not alarmist red #c43d34).
        expect(status.style.color).toBe('rgb(161, 98, 7)');
        expect(status.style.color).not.toBe('rgb(196, 61, 52)');
    });

    it('brightens failure amber on dark UI (prop or data-ai-theme)', () => {
        render(
            <CodingAgentCompactStatus
                progress={{ phase: 'failed', taskID: 'T4', title: 'Apply patch' }}
                lang="zh-Hans"
                testId="compact-status-dark"
                variant="status-bar"
                isDark
            />,
        );
        const byProp = screen.getByTestId('compact-status-dark');
        expect(byProp.style.color).toBe('rgb(224, 178, 83)'); // CODING_AGENT_FAILURE_ACCENT_DARK
        expect(byProp.style.color).not.toBe('rgb(161, 98, 7)');
        expect(byProp.style.color).not.toBe('rgb(196, 61, 52)');

        // Document theme fallback when isDark omitted
        let app = document.getElementById('App');
        if (!app) {
            app = document.createElement('div');
            app.id = 'App';
            document.body.appendChild(app);
        }
        app.setAttribute('data-ai-theme', 'dark');
        render(
            <CodingAgentCompactStatus
                progress={{ phase: 'failed', taskID: 'T5', title: 'Link failed' }}
                lang="en"
                testId="compact-status-theme"
                variant="status-bar"
            />,
        );
        const byTheme = screen.getByTestId('compact-status-theme');
        expect(byTheme.style.color).toBe('rgb(224, 178, 83)');
        // sanity: exported dark accent matches rendered rgb
        expect(CODING_AGENT_FAILURE_ACCENT_DARK.toLowerCase()).toBe('#e0b253');
    });

    it('uses a non-alarming tone for expected TDD red-phase tool checks', () => {
        render(
            <CodingAgentCompactStatus
                progress={{
                    phase: 'running',
                    taskID: 'T9',
                    title: 'Run failing tests first',
                    event: 'tool_finished',
                    outcome: 'failed',
                    summary: 'NOTE: These tests expect the driver to be NOT implemented yet. All tests should FAIL (red light) until implementation is complete.',
                }}
                lang="zh-Hans"
                testId="compact-status"
                variant="status-bar"
            />,
        );

        const status = screen.getByTestId('compact-status');
        expect(status.getAttribute('aria-label')).toBe('\u7f16\u7a0b \u00b7 \u5de5\u5177\u68c0\u67e5 \u00b7 T9 \u00b7 Run failing tests first');
        expect(status.style.color).toBe('rgb(100, 116, 139)');
        expect(status.style.border).toBe('1px solid rgba(100, 116, 139, 0.2)');
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
        expect(status.textContent).toContain('\u7f16\u7a0b');
        expect(status.textContent).not.toContain('\u4efb\u52a1\u72b6\u6001');
        expect(status.textContent).toContain('\u6267\u884c\u4e2d');
        expect(status.getAttribute('aria-label')).toBe('\u7f16\u7a0b \u00b7 \u6267\u884c\u4e2d \u00b7 T8 \u00b7 Fix monitor badge');
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
        expect(status.textContent).toContain('Coding');
        expect(status.textContent).toContain('Result');
        expect(status.textContent).toContain('T6');
        expect(status.textContent).toContain('3 files | 1 created');
        expect(status.textContent).toContain('Update parser');
        expect(status.getAttribute('aria-label')).toBe('Coding \u00b7 Result \u00b7 T6 \u00b7 3 files | 1 created \u00b7 Update parser');
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
        expect(status.getAttribute('aria-label')).toBe('Coding \u00b7 Result \u00b7 T6 \u00b7 4 changes \u00b7 Update parser \u00b7 a.go, b.go, c.go +1 more');
        expect(status.getAttribute('data-change-count')).toBe('4');
    });

    it('keeps status-bar copy dense (no Files row, title still visible)', () => {
        render(
            <CodingAgentCompactStatus
                progress={{ phase: 'result', taskID: 'T6', title: 'Update parser', event: 'diff_summary', count: 2, files: ['a.go', 'b.go'] }}
                lang="en"
                testId="compact-status"
                variant="status-bar"
            />,
        );
        const status = screen.getByTestId('compact-status');
        expect(status.textContent).toContain('Update parser');
        expect(status.textContent).not.toMatch(/\bFiles\b/);
        expect(status.getAttribute('aria-label')).toBe('Coding \u00b7 Result \u00b7 T6 \u00b7 2 changes \u00b7 Update parser');
    });
});
