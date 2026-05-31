import { createElement } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { isHistoryDiscussionReadOnly } from '../AIAssistantPanel';
import { AITabBar } from '../AITabBar';

const theme = {
    bg: '#fff',
    text: '#111',
    textMuted: '#666',
    divider: '#ddd',
    btnColor: '#2563eb',
    titleBarBg: '#f8fafc',
} as any;

describe('isHistoryDiscussionReadOnly', () => {
    it('keeps open sessions started by me writable', () => {
        expect(isHistoryDiscussionReadOnly({ status: 'open', local_relation: 'initiated_by_me', readonly: false })).toBe(false);
        expect(isHistoryDiscussionReadOnly({ status: 'open', local_relation: 'initiated_by_me', readonly: true })).toBe(false);
        expect(isHistoryDiscussionReadOnly({ status: 'open', role: 'initiator', readonly: true })).toBe(false);
    });

    it('marks invited and archived sessions as read-only', () => {
        expect(isHistoryDiscussionReadOnly({ status: 'open', local_relation: 'owned_ve_invited' })).toBe(true);
        expect(isHistoryDiscussionReadOnly({ status: 'open', local_relation: 'owned_ve_invited', readonly: false })).toBe(true);
        expect(isHistoryDiscussionReadOnly({ status: 'open', role: 'review', readonly: false })).toBe(true);
        expect(isHistoryDiscussionReadOnly({ status: 'closed', local_relation: 'initiated_by_me', readonly: false })).toBe(true);
        expect(isHistoryDiscussionReadOnly({ status: 'archived', local_relation: 'initiated_by_me', readonly: false })).toBe(true);
    });

    it('keeps unknown relation sessions read-only even when readonly is false', () => {
        expect(isHistoryDiscussionReadOnly({ status: 'open', readonly: false })).toBe(true);
    });
    it('localizes read-only markers on history tabs', () => {
        render(createElement(AITabBar, {
            tabs: [
                { id: 'local', type: 'local', title: 'AI', closable: false },
                { id: 'history-1', type: 'group', title: 'Case review', closable: true, readOnly: true },
            ] as any,
            activeTabId: 'history-1',
            theme,
            onActivate: () => {},
            onClose: () => {},
            lang: 'en',
        }));

        expect(screen.getByText('Read-only')).toBeTruthy();
        expect(screen.getByRole('tab', { name: 'Case review - Read-only' })).toBeTruthy();
    });

    it('shows invite action for VE and live group tabs', () => {
        const onInvite = () => {};
        const { rerender } = render(createElement(AITabBar, {
            tabs: [
                { id: 'local', type: 'local', title: 'AI', closable: false },
                { id: 'group-1', type: 'group', title: 'Existing group', veId: 've-a', participants: ['ve-a'], closable: true },
            ] as any,
            activeTabId: 'group-1',
            theme,
            onActivate: () => {},
            onClose: () => {},
            onInviteToTab: onInvite,
            lang: 'en',
        }));

        fireEvent.contextMenu(screen.getByRole('tab', { name: 'Existing group' }));
        expect(screen.getByTestId('tab-menu-invite-ve')).toBeTruthy();

        rerender(createElement(AITabBar, {
            tabs: [
                { id: 'local', type: 'local', title: 'AI', closable: false },
                { id: 've-1', type: 've', title: 'Solo helper', veId: 've-a', closable: true },
            ] as any,
            activeTabId: 've-1',
            theme,
            onActivate: () => {},
            onClose: () => {},
            onInviteToTab: onInvite,
            lang: 'en',
        }));

        fireEvent.contextMenu(screen.getByRole('tab', { name: 'Solo helper' }));
        expect(screen.getByTestId('tab-menu-invite-ve')).toBeTruthy();
    });

    it('does not show add-local action for writable history group tabs', () => {
        render(createElement(AITabBar, {
            tabs: [
                { id: 'local', type: 'local', title: 'AI', closable: false },
                { id: 'history-1', type: 'group', title: 'Writable history', participants: ['me', 've-a'], closable: true, readOnly: false, discussionId: 'disc-1' },
            ] as any,
            activeTabId: 'history-1',
            theme,
            onActivate: () => {},
            onClose: () => {},
            onAddLocalMaclawToTab: () => {},
            lang: 'en',
        }));

        fireEvent.contextMenu(screen.getByRole('tab', { name: 'Writable history' }));
        expect(screen.queryByTestId('tab-menu-add-local')).toBeNull();
        expect(screen.getByTestId('tab-menu-close')).toBeTruthy();
    });

    it('shows invite action for writable history group tabs with a discussion id', () => {
        render(createElement(AITabBar, {
            tabs: [
                { id: 'local', type: 'local', title: 'AI', closable: false },
                { id: 'history-1', type: 'group', title: 'Writable history', participants: ['me', 've-a'], closable: true, readOnly: false, discussionId: 'disc-1' },
            ] as any,
            activeTabId: 'history-1',
            theme,
            onActivate: () => {},
            onClose: () => {},
            onInviteToTab: () => {},
            lang: 'en',
        }));

        fireEvent.contextMenu(screen.getByRole('tab', { name: 'Writable history' }));
        expect(screen.getByTestId('tab-menu-invite-ve')).toBeTruthy();
    });

    it('shows add-local action for live VE group tabs', () => {
        render(createElement(AITabBar, {
            tabs: [
                { id: 'local', type: 'local', title: 'AI', closable: false },
                { id: 'group-1', type: 'group', title: 'Live helper', veId: 've-a', participants: ['ve-a'], closable: true, readOnly: false },
            ] as any,
            activeTabId: 'group-1',
            theme,
            onActivate: () => {},
            onClose: () => {},
            onAddLocalMaclawToTab: () => {},
            lang: 'en',
        }));

        fireEvent.contextMenu(screen.getByRole('tab', { name: /Live helper/ }));
        expect(screen.getByTestId('tab-menu-add-local')).toBeTruthy();
    });

    it('shows rename action for writable group tabs and calls the handler', () => {
        const onRename = vi.fn();
        render(createElement(AITabBar, {
            tabs: [
                { id: 'local', type: 'local', title: 'AI', closable: false },
                { id: 'group-1', type: 'group', title: 'Live helper', veId: 've-a', participants: ['ve-a'], closable: true, readOnly: false },
            ] as any,
            activeTabId: 'group-1',
            theme,
            onActivate: () => {},
            onClose: () => {},
            onRenameGroupTab: onRename,
            lang: 'zh-Hans',
        }));

        fireEvent.contextMenu(screen.getByRole('tab', { name: /Live helper/ }));
        fireEvent.click(screen.getByTestId('tab-menu-rename-group'));

        expect(onRename).toHaveBeenCalledWith(expect.objectContaining({ id: 'group-1' }));
    });

    it('hides add-local action for read-only VE group tabs', () => {
        render(createElement(AITabBar, {
            tabs: [
                { id: 'local', type: 'local', title: 'AI', closable: false },
                { id: 'group-1', type: 'group', title: 'Read-only helper', veId: 've-a', participants: ['ve-a'], closable: true, readOnly: true },
            ] as any,
            activeTabId: 'group-1',
            theme,
            onActivate: () => {},
            onClose: () => {},
            onAddLocalMaclawToTab: () => {},
            lang: 'en',
        }));

        fireEvent.contextMenu(screen.getByRole('tab', { name: /Read-only helper/ }));
        expect(screen.queryByTestId('tab-menu-add-local')).toBeNull();
        expect(screen.queryByTestId('ai-tab-context-menu')).toBeNull();
    });

    it('hides add-local action when local AI participant id differs only by case', () => {
        render(createElement(AITabBar, {
            tabs: [
                { id: 'local', type: 'local', title: 'AI', closable: false },
                { id: 'group-1', type: 'group', title: 'Live helper', veId: 've-a', participants: ['ve-a', ' LOCAL-MACLAW '], closable: true, readOnly: false },
            ] as any,
            activeTabId: 'group-1',
            theme,
            onActivate: () => {},
            onClose: () => {},
            onAddLocalMaclawToTab: () => {},
            lang: 'en',
        }));

        fireEvent.contextMenu(screen.getByRole('tab', { name: /Live helper/ }));
        expect(screen.queryByTestId('tab-menu-add-local')).toBeNull();
    });

    it('marks read-only history tabs inside the overflow menu', async () => {
        render(createElement(AITabBar, {
            tabs: [
                { id: 'local', type: 'local', title: 'AI', closable: false },
                { id: 've-1', type: 've', title: 'Helper one', closable: true },
                { id: 've-2', type: 've', title: 'Helper two', closable: true },
                { id: 'history-2', type: 'group', title: 'Overflow case', closable: true, readOnly: true },
            ] as any,
            activeTabId: 'local',
            theme,
            onActivate: () => {},
            onClose: () => {},
            lang: 'en',
        }));

        const tabBar = screen.getByTestId('ai-tab-bar');
        expect(tabBar.style.overflowY).toBe('visible');

        fireEvent.click(await screen.findByTestId('ai-tab-overflow-btn'));

        expect(screen.getByText('Overflow case')).toBeTruthy();
        expect(screen.getByText('Read-only')).toBeTruthy();
    });
});
