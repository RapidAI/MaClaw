// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useEffect } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { DialogProvider, useDialog } from './CustomDialog';

vi.mock('../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(() => vi.fn()),
}));

function ConfirmLauncher({ onResult }: { onResult?: (confirmed: boolean) => void }) {
    const { showConfirm } = useDialog();
    return (
        <button
            onClick={() => {
                void showConfirm('中止后当前进度将被清除。', '中止当前工作流？', {
                    confirmText: '中止',
                    cancelText: '取消',
                    confirmVariant: 'danger',
                }).then(result => onResult?.(result));
            }}
        >
            open
        </button>
    );
}

function PromptLauncher({ onResult }: { onResult?: (value: string | null) => void }) {
    const { showPrompt } = useDialog();
    return (
        <button
            onClick={() => {
                void showPrompt('请粘贴浏览器页面中显示的授权码 (Authorization Code):', '授权码', {
                    placeholder: '在此粘贴授权码',
                    confirmText: '确定',
                    cancelText: '取消',
                }).then(result => onResult?.(result));
            }}
        >
            open-prompt
        </button>
    );
}

function FallbackConfirmLauncher({ onResult }: { onResult: (confirmed: boolean) => void }) {
    const { showConfirm } = useDialog();
    return <button onClick={() => { void showConfirm('fallback').then(onResult); }}>open-fallback</button>;
}

function ReplacementLauncher({ onFirstResult, onSecondResult }: {
    onFirstResult: (confirmed: boolean) => void;
    onSecondResult: (confirmed: boolean) => void;
}) {
    const { showConfirm } = useDialog();
    return (
        <>
            <button onClick={() => { void showConfirm('first dialog').then(onFirstResult); }}>open-first</button>
            <button onClick={() => { void showConfirm('replacement dialog').then(onSecondResult); }}>open-second</button>
        </>
    );
}

function PendingPromptLauncher({ onResult }: { onResult: (value: string | null) => void }) {
    const { showPrompt } = useDialog();
    return <button onClick={() => { void showPrompt('pending prompt').then(onResult); }}>open-pending</button>;
}

function FollowUpLauncher({ onFirstResult, onSecondResult }: {
    onFirstResult: (confirmed: boolean) => void;
    onSecondResult: (confirmed: boolean) => void;
}) {
    const { showConfirm } = useDialog();
    return (
        <button
            onClick={() => {
                void showConfirm('first follow-up dialog').then(async firstResult => {
                    onFirstResult(firstResult);
                    const secondResult = await showConfirm('second follow-up dialog');
                    onSecondResult(secondResult);
                });
            }}
        >
            open-follow-up
        </button>
    );
}

/** Bubble-phase Escape listener that nested app dialogs typically register. */
function NestedEscapeProbe({ onEscape }: { onEscape: () => void }) {
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key === 'Escape') onEscape();
        };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, [onEscape]);
    return null;
}

describe('CustomDialog', () => {
    afterEach(() => {
        cleanup();
    });

    it('renders destructive confirmations with the danger button style', async () => {
        const onResult = vi.fn();
        render(
            <DialogProvider>
                <ConfirmLauncher onResult={onResult} />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open' }));

        const confirmButton = await screen.findByRole('button', { name: '中止' });
        expect(confirmButton.className).toContain('btn-danger');
        expect(screen.getByRole('button', { name: '取消' })).toBeTruthy();
        expect(screen.getByText('中止后当前进度将被清除。')).toBeTruthy();

        fireEvent.keyDown(window, { key: 'Enter' });
        expect(screen.getByRole('button', { name: '中止' })).toBeTruthy();
        expect(onResult).not.toHaveBeenCalled();

        fireEvent.click(confirmButton);
        await waitFor(() => expect(onResult).toHaveBeenCalledWith(true));
    });

    it('shows a custom prompt dialog and returns the entered value', async () => {
        const onResult = vi.fn();
        render(
            <DialogProvider>
                <PromptLauncher onResult={onResult} />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open-prompt' }));

        expect(await screen.findByText('请粘贴浏览器页面中显示的授权码 (Authorization Code):')).toBeTruthy();
        const input = screen.getByPlaceholderText('在此粘贴授权码') as HTMLInputElement;
        fireEvent.change(input, { target: { value: 'auth-code-123' } });
        fireEvent.click(screen.getByRole('button', { name: '确定' }));

        await waitFor(() => expect(onResult).toHaveBeenCalledWith('auth-code-123'));
    });

    it('returns null when the prompt dialog is cancelled', async () => {
        const onResult = vi.fn();
        render(
            <DialogProvider>
                <PromptLauncher onResult={onResult} />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open-prompt' }));
        fireEvent.click(await screen.findByRole('button', { name: '取消' }));

        await waitFor(() => expect(onResult).toHaveBeenCalledWith(null));
    });

    it('submits the prompt value on Enter without rebinding every keystroke', async () => {
        const onResult = vi.fn();
        render(
            <DialogProvider>
                <PromptLauncher onResult={onResult} />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open-prompt' }));
        const input = await screen.findByPlaceholderText('在此粘贴授权码');
        fireEvent.change(input, { target: { value: 'enter-code' } });
        fireEvent.keyDown(window, { key: 'Enter' });

        await waitFor(() => expect(onResult).toHaveBeenCalledWith('enter-code'));
    });

    it('stacks above nested app dialogs (LLM config overlay uses z-index 9999)', async () => {
        render(
            <DialogProvider>
                <PromptLauncher />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open-prompt' }));
        const backdrop = await screen.findByText('请粘贴浏览器页面中显示的授权码 (Authorization Code):');
        const overlay = backdrop.closest('.modal-backdrop') as HTMLElement | null;
        expect(overlay).toBeTruthy();
        expect(overlay!.style.zIndex).toBe('120000');
    });

    it('does not submit prompt on Enter while IME is composing', async () => {
        const onResult = vi.fn();
        render(
            <DialogProvider>
                <PromptLauncher onResult={onResult} />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open-prompt' }));
        const input = await screen.findByPlaceholderText('在此粘贴授权码');
        fireEvent.change(input, { target: { value: 'partial' } });
        fireEvent.keyDown(window, { key: 'Enter', isComposing: true });
        expect(onResult).not.toHaveBeenCalled();
        expect(screen.getByPlaceholderText('在此粘贴授权码')).toBeTruthy();
    });

    it('handles Escape in capture phase so nested bubble listeners do not also run', async () => {
        const nestedEscapes: string[] = [];
        const onResult = vi.fn();
        render(
            <DialogProvider>
                <NestedEscapeProbe onEscape={() => nestedEscapes.push('nested')} />
                <PromptLauncher onResult={onResult} />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open-prompt' }));
        await screen.findByPlaceholderText('在此粘贴授权码');

        // Native KeyboardEvent so capture + stopImmediatePropagation behave like the browser.
        window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));

        await waitFor(() => expect(onResult).toHaveBeenCalledWith(null));
        expect(nestedEscapes).toEqual([]);
    });

    it('keeps keyboard focus inside the dialog and restores the invoking control after close', async () => {
        render(
            <DialogProvider>
                <ConfirmLauncher />
            </DialogProvider>,
        );

        const trigger = screen.getByRole('button', { name: 'open' });
        trigger.focus();
        fireEvent.click(trigger);
        const confirmButton = await screen.findByRole('button', { name: '中止' });
        await waitFor(() => expect(document.activeElement).toBe(confirmButton));

        fireEvent.keyDown(window, { key: 'Tab' });
        expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Close' }));
        fireEvent.keyDown(window, { key: 'Tab' });
        expect(document.activeElement).toBe(screen.getByRole('button', { name: '取消' }));
        fireEvent.keyDown(window, { key: 'Escape' });
        await waitFor(() => expect(document.activeElement).toBe(trigger));
    });

    it('lets a focused cancel button handle Enter instead of confirming the dialog', async () => {
        const onResult = vi.fn();
        render(
            <DialogProvider>
                <ConfirmLauncher onResult={onResult} />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open' }));
        const cancelButton = await screen.findByRole('button', { name: '取消' });
        cancelButton.focus();

        fireEvent.keyDown(cancelButton, { key: 'Enter' });
        expect(screen.getByRole('button', { name: '中止' })).toBeTruthy();
        fireEvent.click(cancelButton);

        await waitFor(() => expect(onResult).toHaveBeenCalledWith(false));
    });

    it('restores focus to the original control when a dialog replaces another dialog', async () => {
        const onFirstResult = vi.fn();
        const onSecondResult = vi.fn();
        render(
            <DialogProvider>
                <ReplacementLauncher onFirstResult={onFirstResult} onSecondResult={onSecondResult} />
            </DialogProvider>,
        );

        const firstTrigger = screen.getByRole('button', { name: 'open-first' });
        firstTrigger.focus();
        fireEvent.click(firstTrigger);
        await screen.findByText('first dialog');

        fireEvent.click(screen.getByRole('button', { name: 'open-second' }));
        await screen.findByText('replacement dialog');
        fireEvent.keyDown(window, { key: 'Escape' });

        await waitFor(() => expect(onFirstResult).toHaveBeenCalledWith(false));
        await waitFor(() => expect(onSecondResult).toHaveBeenCalledWith(false));
        await waitFor(() => expect(document.activeElement).toBe(firstTrigger));
    });

    it('keeps the original focus target when a resolved dialog immediately opens a follow-up', async () => {
        const onFirstResult = vi.fn();
        const onSecondResult = vi.fn();
        render(
            <DialogProvider>
                <FollowUpLauncher onFirstResult={onFirstResult} onSecondResult={onSecondResult} />
            </DialogProvider>,
        );

        const trigger = screen.getByRole('button', { name: 'open-follow-up' });
        trigger.focus();
        fireEvent.click(trigger);
        fireEvent.keyDown(window, { key: 'Escape' });
        await screen.findByText('second follow-up dialog');
        expect(onFirstResult).toHaveBeenCalledWith(false);

        fireEvent.keyDown(window, { key: 'Escape' });
        await waitFor(() => expect(onSecondResult).toHaveBeenCalledWith(false));
        await waitFor(() => expect(document.activeElement).toBe(trigger));
    });

    it('safely cancels a pending dialog when the provider unmounts', async () => {
        const onResult = vi.fn();
        const view = render(
            <DialogProvider>
                <PendingPromptLauncher onResult={onResult} />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open-pending' }));
        await screen.findByText('pending prompt');
        view.unmount();

        await waitFor(() => expect(onResult).toHaveBeenCalledWith(null));
    });

    it('keeps focus inside the dialog when another control tries to receive it', async () => {
        render(
            <DialogProvider>
                <button type="button">outside</button>
                <ConfirmLauncher />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open' }));
        const confirmButton = await screen.findByRole('button', { name: '中止' });
        const outsideButton = screen.getByRole('button', { name: 'outside' });
        outsideButton.focus();

        await waitFor(() => expect(document.activeElement).toBe(confirmButton));
    });

    it('safely cancels requests made outside the dialog provider', async () => {
        const onResult = vi.fn();
        render(<FallbackConfirmLauncher onResult={onResult} />);
        fireEvent.click(screen.getByRole('button', { name: 'open-fallback' }));
        await waitFor(() => expect(onResult).toHaveBeenCalledWith(false));
    });
});
