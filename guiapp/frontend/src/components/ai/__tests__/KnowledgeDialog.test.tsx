import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { KnowledgeDialog } from '../KnowledgeDialog';

const mockPanelState = vi.hoisted(() => ({ showNestedDialog: false }));

vi.mock('../../settings/KnowledgeSettingsPanel', () => ({
    KnowledgeSettingsPanel: () => (
        <>
            <label>
                Sync password
                <input aria-label="Sync password" type="password" />
            </label>
            {mockPanelState.showNestedDialog && (
                <div role="dialog" aria-label="Import to Knowledge Base">
                    <button type="button">Nested import action</button>
                </div>
            )}
        </>
    ),
}));

const theme = {
    bg: '#111827',
    divider: '#374151',
    text: '#f9fafb',
    textMuted: '#9ca3af',
} as any;

describe('KnowledgeDialog', () => {
    afterEach(() => {
        mockPanelState.showNestedDialog = false;
    });

    it('does not close when text selection starts inside the dialog and ends on the backdrop', () => {
        const onClose = vi.fn();
        render(<KnowledgeDialog open onClose={onClose} lang="en" theme={theme} />);

        const dialog = screen.getByRole('dialog', { name: 'Knowledge Base' });
        const overlay = document.body.querySelector<HTMLElement>('.knowledge-dialog-overlay');
        const input = screen.getByLabelText('Sync password');

        fireEvent.mouseDown(input);
        fireEvent.click(overlay!);

        expect(onClose).not.toHaveBeenCalled();
    });

    it('closes only when the backdrop receives the full click gesture', () => {
        const onClose = vi.fn();
        render(<KnowledgeDialog open onClose={onClose} lang="en" theme={theme} />);

        const dialog = screen.getByRole('dialog', { name: 'Knowledge Base' });
        const overlay = document.body.querySelector<HTMLElement>('.knowledge-dialog-overlay');
        expect(dialog).not.toBe(overlay);
        fireEvent.mouseDown(overlay!);
        fireEvent.click(overlay!);

        expect(onClose).toHaveBeenCalledTimes(1);
    });

    it('keeps keyboard focus in the dialog and restores focus after closing', async () => {
        const onClose = vi.fn();
        const trigger = document.createElement('button');
        document.body.append(trigger);
        trigger.focus();
        try {
            const { unmount } = render(<KnowledgeDialog open onClose={onClose} lang="en" theme={theme} />);
            const closeButton = screen.getByRole('button', { name: 'Close' });
            const input = screen.getByLabelText('Sync password');

            await waitFor(() => expect(document.activeElement).toBe(closeButton));
            fireEvent.keyDown(closeButton, { key: 'Tab', shiftKey: true });
            expect(document.activeElement).toBe(input);
            fireEvent.keyDown(input, { key: 'Tab' });
            expect(document.activeElement).toBe(closeButton);
            fireEvent.keyDown(closeButton, { key: 'Escape' });
            expect(onClose).toHaveBeenCalledTimes(1);

            unmount();
            expect(document.activeElement).toBe(trigger);
        } finally {
            trigger.remove();
        }
    });

    it('keeps the modal lifecycle stable when the parent passes a new close callback', async () => {
        const initialClose = vi.fn();
        const nextClose = vi.fn();
        const { rerender } = render(<KnowledgeDialog open onClose={initialClose} lang="en" theme={theme} />);
        const closeButton = screen.getByRole('button', { name: 'Close' });

        await waitFor(() => expect(document.activeElement).toBe(closeButton));
        rerender(<KnowledgeDialog open onClose={nextClose} lang="en" theme={theme} />);
        expect(document.activeElement).toBe(closeButton);

        fireEvent.keyDown(closeButton, { key: 'Escape' });
        expect(initialClose).not.toHaveBeenCalled();
        expect(nextClose).toHaveBeenCalledTimes(1);
    });

    it('leaves keyboard handling to an active nested dialog', async () => {
        const onClose = vi.fn();
        mockPanelState.showNestedDialog = true;
        render(<KnowledgeDialog open onClose={onClose} lang="en" theme={theme} />);
        const nestedAction = screen.getByRole('button', { name: 'Nested import action' });

        nestedAction.focus();
        fireEvent.keyDown(nestedAction, { key: 'Escape' });
        fireEvent.keyDown(nestedAction, { key: 'Tab' });

        expect(onClose).not.toHaveBeenCalled();
        expect(document.activeElement).toBe(nestedAction);
    });

    it('inherits and live-updates the app theme when rendered in a portal', async () => {
        const app = document.createElement('div');
        app.id = 'App';
        app.dataset.aiTheme = 'dark';
        app.dataset.aiDarkScheme = 'aurora';
        document.body.append(app);
        try {
            render(<KnowledgeDialog open onClose={vi.fn()} lang="en" theme={theme} />);
            const dialog = screen.getByRole('dialog', { name: 'Knowledge Base' });
            const overlay = document.body.querySelector<HTMLElement>('.knowledge-dialog-overlay');
            expect(overlay?.dataset.portalTheme).toBe('true');
            expect(overlay?.dataset.aiTheme).toBe('dark');
            expect(overlay?.dataset.aiDarkScheme).toBe('aurora');

            app.dataset.aiTheme = 'light';
            app.dataset.aiDarkScheme = '';
            app.dataset.aiLightScheme = 'linear';
            await waitFor(() => {
                expect(overlay?.dataset.aiTheme).toBe('light');
                expect(overlay?.dataset.aiDarkScheme).toBeUndefined();
                expect(overlay?.dataset.aiLightScheme).toBe('linear');
            });
        } finally {
            app.remove();
        }
    });
});
