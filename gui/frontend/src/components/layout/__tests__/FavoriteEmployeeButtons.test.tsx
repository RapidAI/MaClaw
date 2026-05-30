// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
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

    it('opens a right-click menu and removes a favorite digital employee', () => {
        const onRemove = vi.fn();
        render(<FavoriteEmployeeButtons slots={slots} veAuthorized onStartConversation={vi.fn()} onReorder={vi.fn()} onRemove={onRemove} />);

        fireEvent.contextMenu(screen.getByTestId('fav-ve-ve-1'), { clientX: 20, clientY: 30 });
        fireEvent.click(screen.getByRole('menuitem', { name: 'Remove' }));

        expect(onRemove).toHaveBeenCalledWith('ve-1');
    });

    it('renames a favorite digital employee with the custom dialog', () => {
        const onRename = vi.fn();
        const promptSpy = vi.spyOn(window, 'prompt');
        render(<FavoriteEmployeeButtons slots={slots} veAuthorized onStartConversation={vi.fn()} onReorder={vi.fn()} onRename={onRename} />);

        fireEvent.contextMenu(screen.getByTestId('fav-ve-ve-1'), { clientX: 20, clientY: 30 });
        fireEvent.click(screen.getByRole('menuitem', { name: 'Rename' }));
        const input = screen.getByLabelText('Display name');
        fireEvent.change(input, { target: { value: 'Lab Lead' } });
        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        expect(onRename).toHaveBeenCalledWith('ve-1', 'Lab Lead');
        expect(promptSpy).not.toHaveBeenCalled();
        promptSpy.mockRestore();
    });

    it('keeps the custom rename dialog open while saving', async () => {
        let resolveRename: (() => void) | undefined;
        const onRename = vi.fn(() => new Promise<void>((resolve) => { resolveRename = resolve; }));
        render(<FavoriteEmployeeButtons slots={slots} veAuthorized onStartConversation={vi.fn()} onReorder={vi.fn()} onRename={onRename} />);

        fireEvent.contextMenu(screen.getByTestId('fav-ve-ve-1'), { clientX: 20, clientY: 30 });
        fireEvent.click(screen.getByRole('menuitem', { name: 'Rename' }));
        fireEvent.change(screen.getByLabelText('Display name'), { target: { value: 'Lab Lead' } });
        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => expect(screen.getByRole('dialog')).toBeTruthy());
        expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByLabelText('Display name') as HTMLInputElement).disabled).toBe(true);

        resolveRename?.();
        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    });

    it('keeps the custom rename dialog open and shows an inline error when saving fails', async () => {
        const onRename = vi.fn().mockRejectedValue(new Error('save failed'));
        const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
        render(<FavoriteEmployeeButtons slots={slots} veAuthorized onStartConversation={vi.fn()} onReorder={vi.fn()} onRename={onRename} />);

        fireEvent.contextMenu(screen.getByTestId('fav-ve-ve-1'), { clientX: 20, clientY: 30 });
        fireEvent.click(screen.getByRole('menuitem', { name: 'Rename' }));
        fireEvent.change(screen.getByLabelText('Display name'), { target: { value: 'Lab Lead' } });
        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('Rename failed'));
        expect(screen.getByRole('dialog')).toBeTruthy();

        consoleSpy.mockRestore();
    });

    it('does not report rename failures after unmounting during save', async () => {
        let rejectRename: ((error: Error) => void) | undefined;
        const onRename = vi.fn(() => new Promise<void>((_, reject) => { rejectRename = reject; }));
        const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
        const { unmount } = render(<FavoriteEmployeeButtons slots={slots} veAuthorized onStartConversation={vi.fn()} onReorder={vi.fn()} onRename={onRename} />);

        fireEvent.contextMenu(screen.getByTestId('fav-ve-ve-1'), { clientX: 20, clientY: 30 });
        fireEvent.click(screen.getByRole('menuitem', { name: 'Rename' }));
        fireEvent.change(screen.getByLabelText('Display name'), { target: { value: 'Lab Lead' } });
        fireEvent.click(screen.getByRole('button', { name: 'Save' }));
        unmount();
        rejectRename?.(new Error('save failed'));
        await Promise.resolve();

        expect(consoleSpy).not.toHaveBeenCalled();
        consoleSpy.mockRestore();
    });

    it('localizes the favorite context menu and offline state', () => {
        render(<FavoriteEmployeeButtons slots={slots} veAuthorized lang="zh-Hans" onStartConversation={vi.fn()} onReorder={vi.fn()} onRemove={vi.fn()} />);

        expect(screen.getByRole('button', { name: 'Analyst \u79bb\u7ebf' })).toBeTruthy();
        fireEvent.contextMenu(screen.getByTestId('fav-ve-ve-1'), { clientX: 20, clientY: 30 });

        expect(screen.getByRole('menuitem', { name: '\u6539\u540d' })).toBeTruthy();
        expect(screen.getByRole('menuitem', { name: '\u79fb\u9664' })).toBeTruthy();
    });
});
