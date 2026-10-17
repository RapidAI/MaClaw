// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../../../../wailsjs/go/main/App', () => ({
    MailboxBrowseMessages: vi.fn(),
    MailboxBrowseRead: vi.fn(),
    MailboxBrowseStart: vi.fn(),
}));

import {
    MailboxBrowseMessages,
    MailboxBrowseRead,
    MailboxBrowseStart,
} from '../../../../wailsjs/go/main/App';
import { MailboxManageDialog } from '../MailboxManageDialog';

const account = { id: 'mb1', name: 'Work', address: 'w@ex.com' };

function deferred<T>() {
    let resolve!: (value: T) => void;
    const promise = new Promise<T>(res => { resolve = res; });
    return { promise, resolve };
}

function flushMicrotasks() {
    return new Promise(resolve => setTimeout(resolve, 0));
}

describe('MailboxManageDialog', () => {
    const onClose = vi.fn();

    beforeEach(() => {
        onClose.mockReset();
        vi.mocked(MailboxBrowseStart).mockReset();
        vi.mocked(MailboxBrowseMessages).mockReset();
        vi.mocked(MailboxBrowseRead).mockReset();
    });

    it('shows folders, message rows and the reading placeholder when nothing is selected', async () => {
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [
                { name: 'INBOX', special: 'inbox' },
                { name: 'Junk', special: 'junk' },
            ],
            box: 'INBOX',
            messages: [
                { uid: 1, from: '"小明" <ming@ex.com>', subject: '报表已发', date: '2026-10-10T08:00:00Z', seen: true },
                { uid: 2, from: 'spam@ex.com', subject: '中奖通知', date: '2026-10-14T09:30:00Z', seen: false, has_attachment: true },
            ],
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        expect(await screen.findByRole('dialog', { name: '邮箱管理' })).toBeTruthy();
        expect(await screen.findByRole('button', { name: '收件箱' })).toBeTruthy();
        expect(screen.getByRole('button', { name: '垃圾邮件' })).toBeTruthy();
        // The display name wins over the raw address in the list.
        expect(screen.getByText('小明')).toBeTruthy();
        expect(screen.getByText('附件')).toBeTruthy();
        const spamRow = screen.getByRole('button', { name: /中奖通知/ });
        expect(spamRow.className).toContain('mailbox-manage__message--unread');
        expect(screen.getByText('选择一封邮件，在这里查看内容。')).toBeTruthy();
    });

    it('opens a message and renders its full header in the fixed pane', async () => {
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: [
                { uid: 2, from: 'spam@ex.com', subject: '中奖通知', date: '2026-10-14T09:30:00Z', seen: false, has_attachment: true },
            ],
        } as never);
        vi.mocked(MailboxBrowseRead).mockResolvedValue({
            uid: 2, from: 'spam@ex.com', to: 'w@ex.com', subject: '中奖通知', date: '2026-10-14T09:30:00Z',
            body: '恭喜中奖', attachments: ['lucky.pdf'],
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('button', { name: /中奖通知/ }));
        expect(await screen.findByText('恭喜中奖')).toBeTruthy();
        expect(MailboxBrowseRead).toHaveBeenCalledWith('mb1', 'INBOX', 2);
        expect(screen.getByText(/收件人: w@ex\.com/)).toBeTruthy();
        expect(screen.getByText(/附件: lucky\.pdf/)).toBeTruthy();
        expect(screen.queryByText('选择一封邮件，在这里查看内容。')).toBeNull();
    });

    it('keeps the newest folder load when folders are switched while one is in flight', async () => {
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [
                { name: 'INBOX', special: 'inbox' },
                { name: 'Junk', special: 'junk' },
            ],
            box: 'INBOX',
            messages: [],
        } as never);
        const junk = deferred<unknown[]>();
        const inbox = deferred<unknown[]>();
        vi.mocked(MailboxBrowseMessages).mockImplementation((async (_id: string, box: string, _limit: number) => (
            box === 'Junk' ? junk.promise : inbox.promise
        )) as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);
        await screen.findByRole('dialog', { name: '邮箱管理' });

        fireEvent.click(screen.getByRole('button', { name: '垃圾邮件' }));
        expect(await screen.findByText('正在读取邮件…')).toBeTruthy();
        // A second folder click must not be swallowed by the first load.
        fireEvent.click(screen.getByRole('button', { name: '收件箱' }));
        inbox.resolve([{ uid: 3, from: 'a@ex.com', subject: '来自INBOX', seen: true }]);
        expect(await screen.findByText('来自INBOX')).toBeTruthy();

        junk.resolve([{ uid: 9, from: 'j@ex.com', subject: '来自Junk', seen: false }]);
        await flushMicrotasks();
        expect(screen.getByText('来自INBOX')).toBeTruthy();
        expect(screen.queryByText('来自Junk')).toBeNull();
        expect(MailboxBrowseMessages).toHaveBeenNthCalledWith(1, 'mb1', 'Junk', 30);
        expect(MailboxBrowseMessages).toHaveBeenNthCalledWith(2, 'mb1', 'INBOX', 30);
    });

    it('falls back to the placeholder and reports the error when reading fails', async () => {
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: [{ uid: 2, from: 'spam@ex.com', subject: '中奖通知', seen: false }],
        } as never);
        vi.mocked(MailboxBrowseRead).mockRejectedValue(new Error('连接超时'));

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('button', { name: /中奖通知/ }));
        expect((await screen.findByRole('alert')).textContent).toContain('连接超时');
        expect(screen.getByText('选择一封邮件，在这里查看内容。')).toBeTruthy();
    });

    it('closes on Escape', async () => {
        vi.mocked(MailboxBrowseStart).mockResolvedValue({ folders: [], box: '', messages: [] } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);
        await screen.findByRole('dialog', { name: '邮箱管理' });

        fireEvent.keyDown(window, { key: 'Escape' });
        await waitFor(() => expect(onClose).toHaveBeenCalled());
    });
});
