// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { FavoriteEmployeeSettingsPanel } from '../FavoriteEmployeeSettingsPanel';

const veList = [
    { id: 've-1', name: 'Researcher', skill_description: 'Research work', access_policy: 'public' as const, status: 'active', online_status: 'online' as const },
    { id: 've-2', name: 'Analyst', skill_description: 'Data work', access_policy: 'public' as const, status: 'active', online_status: 'offline' as const },
];

describe('FavoriteEmployeeSettingsPanel', () => {
    it('adds a favorite from settings so parent state can update the left rail', () => {
        const onAdd = vi.fn();
        render(<FavoriteEmployeeSettingsPanel favoriteEmployeeIds={[]} veList={veList} onAdd={onAdd} onRemove={vi.fn()} onReorder={vi.fn()} lang="en" />);

        fireEvent.click(screen.getByRole('button', { name: /Add Favorite/i }));
        fireEvent.click(screen.getByRole('button', { name: 'Researcher' }));

        expect(onAdd).toHaveBeenCalledWith('ve-1');
        expect(screen.queryByRole('button', { name: 'Researcher' })).toBeNull();
    });

    it('removes and reorders configured favorites', () => {
        const onRemove = vi.fn();
        const onReorder = vi.fn();
        render(<FavoriteEmployeeSettingsPanel favoriteEmployeeIds={['ve-1', 've-2']} veList={veList} onAdd={vi.fn()} onRemove={onRemove} onReorder={onReorder} lang="en" />);

        fireEvent.click(screen.getByRole('button', { name: 'Remove favorite employee: Researcher' }));
        expect(onRemove).toHaveBeenCalledWith('ve-1');

        const researcherRow = screen.getByText('Researcher').closest('[draggable="true"]') as HTMLElement;
        const analystRow = screen.getByText('Analyst').closest('[draggable="true"]') as HTMLElement;
        fireEvent.dragStart(researcherRow);
        fireEvent.dragOver(analystRow);
        fireEvent.drop(analystRow);
        expect(onReorder).toHaveBeenCalledWith(['ve-2', 've-1']);
    });
});
