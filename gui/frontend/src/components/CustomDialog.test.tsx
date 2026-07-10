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
        expect(overlay!.style.zIndex).toBe('11000');
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
});
