import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { useSafeBackdropDismiss } from './useSafeBackdropDismiss';

function TestDialog({ enabled = true, onDismiss }: { enabled?: boolean; onDismiss: () => void }) {
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onDismiss, { enabled });

    return (
        <div data-testid="backdrop" {...backdropProps}>
            <div data-testid="dialog" {...dialogProps}>
                <input aria-label="Field" />
            </div>
        </div>
    );
}

describe('useSafeBackdropDismiss', () => {
    it('dismisses when the full click gesture starts and ends on the backdrop', () => {
        const onDismiss = vi.fn();
        render(<TestDialog onDismiss={onDismiss} />);

        const backdrop = screen.getByTestId('backdrop');
        fireEvent.mouseDown(backdrop);
        fireEvent.click(backdrop);

        expect(onDismiss).toHaveBeenCalledTimes(1);
    });

    it('does not dismiss when selection starts inside the dialog and ends on the backdrop', () => {
        const onDismiss = vi.fn();
        render(<TestDialog onDismiss={onDismiss} />);

        fireEvent.mouseDown(screen.getByLabelText('Field'));
        fireEvent.click(screen.getByTestId('backdrop'));

        expect(onDismiss).not.toHaveBeenCalled();
    });

    it('does not dismiss while disabled', () => {
        const onDismiss = vi.fn();
        render(<TestDialog enabled={false} onDismiss={onDismiss} />);

        const backdrop = screen.getByTestId('backdrop');
        fireEvent.mouseDown(backdrop);
        fireEvent.click(backdrop);

        expect(onDismiss).not.toHaveBeenCalled();
    });
});
