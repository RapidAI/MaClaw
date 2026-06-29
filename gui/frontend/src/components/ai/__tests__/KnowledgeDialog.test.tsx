import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { KnowledgeDialog } from '../KnowledgeDialog';

vi.mock('../../settings/KnowledgeSettingsPanel', () => ({
    KnowledgeSettingsPanel: () => (
        <label>
            Sync password
            <input aria-label="Sync password" type="password" />
        </label>
    ),
}));

const theme = {
    bg: '#111827',
    divider: '#374151',
    text: '#f9fafb',
    textMuted: '#9ca3af',
} as any;

describe('KnowledgeDialog', () => {
    it('does not close when text selection starts inside the dialog and ends on the backdrop', () => {
        const onClose = vi.fn();
        render(<KnowledgeDialog open onClose={onClose} lang="en" theme={theme} />);

        const dialog = screen.getByRole('dialog', { name: 'Knowledge Base' });
        const input = screen.getByLabelText('Sync password');

        fireEvent.mouseDown(input);
        fireEvent.click(dialog);

        expect(onClose).not.toHaveBeenCalled();
    });

    it('closes only when the backdrop receives the full click gesture', () => {
        const onClose = vi.fn();
        render(<KnowledgeDialog open onClose={onClose} lang="en" theme={theme} />);

        const dialog = screen.getByRole('dialog', { name: 'Knowledge Base' });
        fireEvent.mouseDown(dialog);
        fireEvent.click(dialog);

        expect(onClose).toHaveBeenCalledTimes(1);
    });
});
