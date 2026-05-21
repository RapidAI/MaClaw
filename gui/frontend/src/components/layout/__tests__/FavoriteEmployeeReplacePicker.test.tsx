// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { FavoriteEmployeeReplacePicker } from '../FavoriteEmployeeReplacePicker';

describe('FavoriteEmployeeReplacePicker', () => {
    it('uses accessible buttons for replacement slots', () => {
        const onReplace = vi.fn();
        render(
            <FavoriteEmployeeReplacePicker
                currentSlots={[{ veId: 've-1', name: 'Researcher' }]}
                newVeName="Analyst"
                onReplace={onReplace}
                onCancel={vi.fn()}
                lang="en"
            />,
        );

        fireEvent.click(screen.getByRole('button', { name: 'Replace favorite slot 1: Researcher' }));

        expect(onReplace).toHaveBeenCalledWith(0);
    });

    it('cancels from the dialog button', () => {
        const onCancel = vi.fn();
        render(
            <FavoriteEmployeeReplacePicker
                currentSlots={[{ veId: 've-1', name: 'Researcher' }]}
                newVeName="Analyst"
                onReplace={vi.fn()}
                onCancel={onCancel}
                lang="en"
            />,
        );

        fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

        expect(onCancel).toHaveBeenCalledTimes(1);
    });
});
