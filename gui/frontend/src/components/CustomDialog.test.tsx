import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
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

describe('CustomDialog', () => {
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
});
