// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ConfirmDialog } from './ConfirmDialog';

const t = (key: string) => (
    key === 'confirm' ? 'Delete' :
    key === 'processing' ? 'Processing request…' :
    'Cancel'
);

function DialogHarness({ onCancel, onConfirm }: { onCancel: () => void; onConfirm: () => void }) {
    const [open, setOpen] = useState(false);
    return (
        <>
            <button type="button" onClick={() => setOpen(true)}>open</button>
            {open && (
                <ConfirmDialog
                    title="Delete item"
                    message="This cannot be undone."
                    t={t}
                    onCancel={() => { onCancel(); setOpen(false); }}
                    onConfirm={() => { onConfirm(); setOpen(false); }}
                />
            )}
        </>
    );
}

function PendingDialogHarness() {
    const [pending, setPending] = useState(false);
    return (
        <>
            <button type="button">outside</button>
            <ConfirmDialog
                title="Delete item"
                message="This cannot be undone."
                t={t}
                onCancel={() => {}}
                onConfirm={() => setPending(true)}
                confirmPending={pending}
            />
        </>
    );
}

describe('ConfirmDialog', () => {
    afterEach(cleanup);

    it('exposes modal semantics, traps focus, and restores the invoking control', async () => {
        const onCancel = vi.fn();
        const onConfirm = vi.fn();
        render(<DialogHarness onCancel={onCancel} onConfirm={onConfirm} />);
        const trigger = screen.getByRole('button', { name: 'open' });
        trigger.focus();
        fireEvent.click(trigger);

        const dialog = screen.getByRole('dialog', { name: 'Delete item' });
        expect(dialog.getAttribute('aria-describedby')).toBeTruthy();
        const cancel = screen.getByRole('button', { name: 'Cancel' });
        const confirm = screen.getByRole('button', { name: 'Delete' });
        await waitFor(() => expect(document.activeElement).toBe(cancel));

        fireEvent.keyDown(window, { key: 'Tab' });
        expect(document.activeElement).toBe(confirm);
        fireEvent.keyDown(window, { key: 'Tab' });
        expect(document.activeElement).toBe(cancel);
        fireEvent.keyDown(window, { key: 'Escape' });
        await waitFor(() => expect(onCancel).toHaveBeenCalledTimes(1));
        await waitFor(() => expect(document.activeElement).toBe(trigger));
    });

    it('uses explicit button types so it cannot submit a containing form', () => {
        const onCancel = vi.fn();
        const onConfirm = vi.fn();
        render(
            <form onSubmit={event => event.preventDefault()}>
                <ConfirmDialog title="Delete item" message="This cannot be undone." t={t} onCancel={onCancel} onConfirm={onConfirm} />
            </form>,
        );

        expect(screen.getByRole('button', { name: 'Cancel' }).getAttribute('type')).toBe('button');
        expect(screen.getByRole('button', { name: 'Delete' }).getAttribute('type')).toBe('button');
    });

    it('announces progress and releases focus instead of trapping it on disabled controls', async () => {
        render(<PendingDialogHarness />);

        const confirm = screen.getByRole('button', { name: 'Delete' });
        confirm.focus();
        fireEvent.click(confirm);

        await waitFor(() => expect(confirm.hasAttribute('disabled')).toBe(true));
        expect(document.querySelector('[data-confirm-pending-status]')?.textContent).toBe('Processing request…');
        await waitFor(() => expect(document.activeElement).not.toBe(confirm));

        const outside = screen.getByRole('button', { name: 'outside' });
        outside.focus();
        expect(document.activeElement).toBe(outside);
    });

    it('disables dismissal and confirmation controls while a destructive request is pending', () => {
        const onCancel = vi.fn();
        const onConfirm = vi.fn();
        render(
            <ConfirmDialog
                title="Delete item"
                message="This cannot be undone."
                t={t}
                onCancel={onCancel}
                onConfirm={onConfirm}
                confirmPending
            />,
        );

        const cancel = screen.getByRole('button', { name: 'Cancel' });
        const confirm = screen.getByRole('button', { name: 'Delete' });
        expect(cancel.hasAttribute('disabled')).toBe(true);
        expect(confirm.hasAttribute('disabled')).toBe(true);
        expect(confirm.getAttribute('aria-busy')).toBe('true');
        expect(document.querySelector('[data-confirm-pending-status]')?.textContent).toBe('Processing request…');

        fireEvent.keyDown(window, { key: 'Escape' });
        expect(onCancel).not.toHaveBeenCalled();
    });

    it('returns focus to the dialog when another control tries to receive it', async () => {
        const onCancel = vi.fn();
        const onConfirm = vi.fn();
        render(
            <>
                <button type="button">outside</button>
                <ConfirmDialog title="Delete item" message="This cannot be undone." t={t} onCancel={onCancel} onConfirm={onConfirm} />
            </>,
        );

        const cancel = screen.getByRole('button', { name: 'Cancel' });
        screen.getByRole('button', { name: 'outside' }).focus();
        await waitFor(() => expect(document.activeElement).toBe(cancel));
    });
});
