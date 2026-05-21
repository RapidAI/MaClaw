// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { FavoriteEmployeeButtons } from '../FavoriteEmployeeButtons';

const slots = [
    { veId: 've-1', name: 'Researcher', online: true, skillDescription: 'Research tasks' },
    { veId: 've-2', name: 'Analyst', online: false, skillDescription: 'Data analysis' },
];

describe('FavoriteEmployeeButtons', () => {
    it('hides favorite employees when digital employee navigation is unavailable', () => {
        render(<FavoriteEmployeeButtons slots={slots} veAuthorized={false} onStartConversation={vi.fn()} onReorder={vi.fn()} />);

        expect(screen.queryByTestId('fav-ve-ve-1')).toBeNull();
    });

    it('starts a favorite digital employee conversation from the left rail button', () => {
        const onStartConversation = vi.fn();
        render(<FavoriteEmployeeButtons slots={slots} veAuthorized onStartConversation={onStartConversation} onReorder={vi.fn()} />);

        fireEvent.click(screen.getByTestId('fav-ve-ve-1'));

        expect(onStartConversation).toHaveBeenCalledWith('ve-1');
        expect(screen.getByRole('button', { name: 'Researcher' })).toBeTruthy();
    });

    it('reorders favorites by drag and drop', () => {
        const onReorder = vi.fn();
        render(<FavoriteEmployeeButtons slots={slots} veAuthorized onStartConversation={vi.fn()} onReorder={onReorder} />);

        fireEvent.dragStart(screen.getByTestId('fav-ve-ve-1'));
        fireEvent.dragOver(screen.getByTestId('fav-ve-ve-2'));
        fireEvent.drop(screen.getByTestId('fav-ve-ve-2'));

        expect(onReorder).toHaveBeenCalledWith(['ve-2', 've-1']);
    });
});
