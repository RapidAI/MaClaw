import { fireEvent, render, screen } from '@testing-library/react';
import { useRef } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { useModalFocusTrap } from './useModalFocusTrap';

function Harness({ onEscape = () => {} }: { onEscape?: () => void }) {
    const ref = useRef<HTMLDivElement | null>(null);
    const { onKeyDown } = useModalFocusTrap(ref, { onEscape });
    return (
        <div ref={ref} tabIndex={-1} data-testid="shell" onKeyDown={onKeyDown}>
            <button type="button" data-testid="first">first</button>
            <button type="button" data-testid="last">last</button>
        </div>
    );
}

describe('useModalFocusTrap', () => {
    it('takes focus on mount and hands it back on unmount', () => {
        const opener = document.createElement('button');
        document.body.appendChild(opener);
        opener.focus();
        const { unmount } = render(<Harness />);
        expect(document.activeElement).toBe(screen.getByTestId('shell'));
        unmount();
        expect(document.activeElement).toBe(opener);
        opener.remove();
    });

    it('wraps Tab and Shift+Tab at the ends', () => {
        render(<Harness />);
        const first = screen.getByTestId('first');
        const last = screen.getByTestId('last');
        last.focus();
        fireEvent.keyDown(last, { key: 'Tab' });
        expect(document.activeElement).toBe(first);
        fireEvent.keyDown(first, { key: 'Tab', shiftKey: true });
        expect(document.activeElement).toBe(last);
    });

    it('sends Tab from the container into the content', () => {
        render(<Harness />);
        const shell = screen.getByTestId('shell');
        shell.focus();
        fireEvent.keyDown(shell, { key: 'Tab' });
        expect(document.activeElement).toBe(screen.getByTestId('first'));
        shell.focus();
        fireEvent.keyDown(shell, { key: 'Tab', shiftKey: true });
        expect(document.activeElement).toBe(screen.getByTestId('last'));
    });

    it('pulls focus back when something outside takes it', () => {
        const outside = document.createElement('button');
        document.body.appendChild(outside);
        render(<Harness />);
        outside.focus();
        expect(document.activeElement).toBe(screen.getByTestId('shell'));
        outside.remove();
    });

    // Focus can be parked on <body> with no focusin event — a removed control
    // leaves it there. Escape and Tab must still reach the dialog, or a
    // keyboard user has no way out.
    it('keeps Escape and Tab working when focus has fallen to the body', () => {
        const onEscape = vi.fn();
        render(<Harness onEscape={onEscape} />);
        (document.activeElement as HTMLElement)?.blur();
        expect(document.activeElement).toBe(document.body);

        fireEvent.keyDown(document.body, { key: 'Escape' });
        expect(onEscape).toHaveBeenCalled();

        fireEvent.keyDown(document.body, { key: 'Tab' });
        expect(document.activeElement).toBe(screen.getByTestId('first'));
    });

    it('leaves keys alone while focus is inside the dialog', () => {
        const onEscape = vi.fn();
        render(<Harness onEscape={onEscape} />);
        screen.getByTestId('first').focus();
        // The React handler owns this case; the document fallback must not
        // fire as well, or Escape would run twice.
        fireEvent.keyDown(screen.getByTestId('first'), { key: 'Escape' });
        expect(onEscape).toHaveBeenCalledTimes(1);
    });
});
