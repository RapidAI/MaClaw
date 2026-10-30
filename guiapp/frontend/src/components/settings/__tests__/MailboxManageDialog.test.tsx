// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const { showConfirmMock } = vi.hoisted(() => ({
    showConfirmMock: vi.fn<(message: string) => Promise<boolean>>(),
}));

vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({ showConfirm: showConfirmMock }),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    MailboxBrowseMessages: vi.fn(),
    MailboxBrowseRead: vi.fn(),
    MailboxBrowseStart: vi.fn(),
    MailboxBrowseMarkRead: vi.fn(),
    MailboxBrowseDelete: vi.fn(),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: vi.fn(),
}));

import {
    MailboxBrowseDelete,
    MailboxBrowseMarkRead,
    MailboxBrowseMessages,
    MailboxBrowseRead,
    MailboxBrowseStart,
} from '../../../../wailsjs/go/main/App';
import { intlLocale } from '../../../i18n';
import { MailboxManageDialog, MESSAGE_PAGE } from '../MailboxManageDialog';

function fullPage(overrides: Array<{ uid: number; subject: string; seen?: boolean; from?: string }> = []) {
    return Array.from({ length: MESSAGE_PAGE }, (_, i) => {
        const custom = overrides[i];
        if (custom) {
            return { uid: custom.uid, from: custom.from || 'a@ex.com', subject: custom.subject, seen: custom.seen !== false };
        }
        return { uid: 1000 + i, from: 'a@ex.com', subject: `填充${i}`, seen: true };
    });
}

const account = { id: 'mb1', name: 'Work', address: 'w@ex.com' };

// Keep in sync with DELETE_CHUNK in MailboxManageDialog.tsx.
const DELETE_CHUNK = 100;

function clickRowCheckbox(row: HTMLElement) {
    fireEvent.click(row.closest('.mailbox-manage__item')!.querySelector('.mailbox-manage__check') as HTMLButtonElement);
}

function deferred<T>() {
    let resolve!: (value: T) => void;
    let reject!: (reason?: unknown) => void;
    const promise = new Promise<T>((res, rej) => {
        resolve = res;
        reject = rej;
    });
    return { promise, resolve, reject };
}

function flushMicrotasks() {
    return new Promise(resolve => setTimeout(resolve, 0));
}

describe('MailboxManageDialog', () => {
    const onClose = vi.fn();

    beforeEach(() => {
        onClose.mockReset();
        showConfirmMock.mockReset().mockResolvedValue(false);
        vi.mocked(MailboxBrowseStart).mockReset();
        vi.mocked(MailboxBrowseMessages).mockReset();
        vi.mocked(MailboxBrowseRead).mockReset();
        vi.mocked(MailboxBrowseMarkRead).mockReset().mockResolvedValue({ updated: 1 });
        vi.mocked(MailboxBrowseDelete).mockReset().mockResolvedValue({ moved: 1 });
    });

    it('shows folders with unread badges, message rows and the reading placeholder', async () => {
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [
                { name: 'INBOX', special: 'inbox' },
                { name: 'Junk', special: 'junk' },
            ],
            box: 'INBOX',
            unseen: { INBOX: 1 },
            messages: [
                { uid: 1, from: '"小明" <ming@ex.com>', subject: '报表已发', date: '2026-10-10T08:00:00Z', seen: true },
                { uid: 2, from: 'spam@ex.com', subject: '中奖通知', date: '2026-10-14T09:30:00Z', seen: false, has_attachment: true },
            ],
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        expect(await screen.findByRole('dialog', { name: '邮箱管理' })).toBeTruthy();
        expect(await screen.findByRole('button', { name: /收件箱/ })).toBeTruthy();
        expect(screen.getByRole('button', { name: '垃圾邮件' })).toBeTruthy();
        // The inbox badge shows the server-provided unread count.
        expect(screen.getByText('1')).toBeTruthy();
        // The display name wins over the raw address in the list.
        expect(screen.getByText('小明')).toBeTruthy();
        // An attachment shows as a paperclip glyph on its row.
        const attachRow = screen.getByRole('button', { name: /中奖通知/ });
        expect(attachRow.querySelector('.mailbox-manage__attach')).toBeTruthy();
        const spamRow = screen.getByRole('button', { name: /中奖通知/ });
        expect(spamRow.className).toContain('mailbox-manage__message--unread');
        expect(screen.getByText('选择一封邮件，在这里查看内容。')).toBeTruthy();
    });

    it('labels the mail window in Mongolian', async () => {
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [
                { name: 'INBOX', special: 'inbox' },
                { name: 'Junk', special: 'junk' },
                { name: 'Archive', special: 'archive' },
            ],
            box: 'INBOX',
            unseen: { INBOX: 2 },
            messages: [
                { uid: 1, from: 'a@ex.com', subject: 'Тайлан', date: '2024-03-02T08:00:00Z', seen: true },
            ],
        } as never);

        render(<MailboxManageDialog lang="mn" account={account} onClose={onClose} />);

        expect(await screen.findByRole('dialog', { name: 'Имэйл удирдах' })).toBeTruthy();
        expect(screen.getByRole('button', { name: /Ирсэн/ })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Спам' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Архив' })).toBeTruthy();
        expect(screen.getByLabelText('2 уншаагүй')).toBeTruthy();
        expect(screen.getByText('Энд унших захидлаа сонгоно уу.')).toBeTruthy();
        const when = new Date('2024-03-02T08:00:00Z').toLocaleDateString(intlLocale('mn'), {
            month: 'short', day: 'numeric', year: 'numeric',
        });
        expect(screen.getByText(when)).toBeTruthy();
        expect(screen.queryByText('邮箱管理')).toBeNull();
        expect(screen.queryByText('收件箱')).toBeNull();
        expect(screen.queryByText('Архивлах')).toBeNull();
    });

    it('opens a message, sanitizes the HTML body and marks it read automatically', async () => {
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: [
                { uid: 2, from: 'spam@ex.com', subject: '中奖通知', date: '2026-10-14T09:30:00Z', seen: false, has_attachment: true },
            ],
        } as never);
        vi.mocked(MailboxBrowseRead).mockResolvedValue({
            uid: 2, from: '"活动组" <ads@ex.com>', to: 'w@ex.com', subject: '中奖通知', date: '2026-10-14T09:30:00Z',
            body: '恭喜中奖', html: '<p>恭喜 <b>中奖</b><script>alert(1)</script><a href="javascript:alert(2)">x</a></p>',
            attachments: ['lucky.pdf'],
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('button', { name: /中奖通知/ }));
        expect(await screen.findByTestId('mailbox-manage-html')).toBeTruthy();
        expect(MailboxBrowseRead).toHaveBeenCalledWith('mb1', 'INBOX', 2);
        // The HTML body renders with scripts and javascript: URLs stripped.
        const htmlPane = screen.getByTestId('mailbox-manage-html');
        expect(htmlPane.innerHTML).toContain('<b>中奖</b>');
        expect(htmlPane.innerHTML).not.toContain('script');
        expect(htmlPane.innerHTML).not.toContain('javascript');
        // Reading an unread message marks it read without asking.
        await waitFor(() => expect(MailboxBrowseMarkRead).toHaveBeenCalledWith('mb1', 'INBOX', [2], true));
        expect(screen.getByText(/活动组/)).toBeTruthy();
        expect(screen.getByText('ads@ex.com')).toBeTruthy();
        expect(screen.getByText(/收件人: w@ex\.com/)).toBeTruthy();
        expect(screen.getByText('lucky.pdf')).toBeTruthy();
        expect(screen.queryByText('选择一封邮件，在这里查看内容。')).toBeNull();
    });

    it('renders markdown bodies with headings and lists', async () => {
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: [{ uid: 4, from: 'a@ex.com', subject: '周报', seen: true }],
        } as never);
        vi.mocked(MailboxBrowseRead).mockResolvedValue({
            uid: 4, from: 'a@ex.com', subject: '周报', seen: true,
            body: '# 本周进展\n\n- 完成登录\n- 修复崩溃\n\n**结论**：可用',
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('button', { name: /周报/ }));
        const pane = await screen.findByTestId('mailbox-manage-markdown');
        expect(pane.querySelector('h1')?.textContent).toBe('本周进展');
        expect(pane.querySelectorAll('li').length).toBe(2);
        expect(pane.querySelector('strong')?.textContent).toBe('结论');
    });

    it('keeps marker-free plain text pre-wrapped and offers no format toggle', async () => {
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: [{ uid: 5, from: 'b@ex.com', subject: '便条', seen: true }],
        } as never);
        vi.mocked(MailboxBrowseRead).mockResolvedValue({
            uid: 5, from: 'b@ex.com', subject: '便条', seen: true,
            body: '第一行\n第二行',
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('button', { name: /便条/ }));
        const pane = await screen.findByTestId('mailbox-manage-plaintext');
        expect(pane.textContent).toBe('第一行\n第二行');
        expect(screen.queryByRole('group', { name: '正文格式' })).toBeNull();
    });

    it('switches between rendered HTML and plain text when the mail has an HTML part', async () => {
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: [{ uid: 6, from: 'c@ex.com', subject: '图文', seen: true }],
        } as never);
        vi.mocked(MailboxBrowseRead).mockResolvedValue({
            uid: 6, from: 'c@ex.com', subject: '图文', seen: true,
            body: '纯文本版本', html: '<p>富文本版本</p><img src="https://track.example/p.gif" alt="活动图">',
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('button', { name: /图文/ }));
        expect(await screen.findByTestId('mailbox-manage-html')).toBeTruthy();
        // Remote pictures start blocked, with a way to show them.
        expect(screen.getByText('已隐藏远程图片。')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: '显示图片' }));
        await waitFor(() => expect(screen.queryByText('已隐藏远程图片。')).toBeNull());

        fireEvent.click(screen.getByRole('button', { name: '纯文本' }));
        expect(await screen.findByTestId('mailbox-manage-plaintext')).toBeTruthy();
        expect(screen.queryByTestId('mailbox-manage-html')).toBeNull();
        fireEvent.click(screen.getByRole('button', { name: '渲染' }));
        expect(await screen.findByTestId('mailbox-manage-html')).toBeTruthy();
    });

    it('deletes a message after confirmation and keeps the rest of a short folder', async () => {
        showConfirmMock.mockResolvedValue(true);
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: [
                { uid: 2, from: 'a@ex.com', subject: '中奖通知', seen: true },
                { uid: 3, from: 'b@ex.com', subject: '保留邮件', seen: true },
            ],
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        const row = await screen.findByRole('button', { name: /中奖通知/ });
        // Hovering a row reveals its quick actions; delete carries the trash icon.
        const rowActions = row.closest('.mailbox-manage__item')!.querySelector('.mailbox-manage__row-actions')!;
        const deleteBtn = rowActions.querySelector('button[aria-label="删除"]') as HTMLButtonElement;
        fireEvent.click(deleteBtn);
        await waitFor(() => expect(showConfirmMock).toHaveBeenCalled());
        expect(String(showConfirmMock.mock.calls[0][0])).toContain('中奖通知');
        expect(String(showConfirmMock.mock.calls[0][0])).toContain('移到已删除');
        await waitFor(() => expect(MailboxBrowseDelete).toHaveBeenCalledWith('mb1', 'INBOX', [2]));
        await flushMicrotasks();
        // Fewer than a page means the folder has nothing older, so no second login.
        expect(MailboxBrowseMessages).not.toHaveBeenCalled();
        expect(screen.getByRole('button', { name: /保留邮件/ })).toBeTruthy();
        expect(screen.queryByRole('button', { name: /中奖通知/ })).toBeNull();
        expect(screen.queryByText('这个文件夹里没有邮件。')).toBeNull();
    });

    it('refills from the server after deleting from a full page', async () => {
        showConfirmMock.mockResolvedValue(true);
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: fullPage([
                { uid: 2, subject: '中奖通知' },
                { uid: 3, subject: '保留邮件' },
            ]),
        } as never);
        vi.mocked(MailboxBrowseMessages).mockResolvedValue([
            { uid: 3, from: 'b@ex.com', subject: '保留邮件', seen: true },
            { uid: 4, from: 'c@ex.com', subject: '下一页的邮件', seen: false },
        ] as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        const row = await screen.findByRole('button', { name: /中奖通知/ });
        const rowActions = row.closest('.mailbox-manage__item')!.querySelector('.mailbox-manage__row-actions')!;
        fireEvent.click(rowActions.querySelector('button[aria-label="删除"]') as HTMLButtonElement);
        await waitFor(() => expect(MailboxBrowseDelete).toHaveBeenCalledWith('mb1', 'INBOX', [2]));
        expect(MailboxBrowseMessages).toHaveBeenCalledWith('mb1', 'INBOX', MESSAGE_PAGE);
        expect(await screen.findByRole('button', { name: /下一页的邮件/ })).toBeTruthy();
        expect(screen.getByRole('button', { name: /保留邮件/ })).toBeTruthy();
        expect(screen.queryByRole('button', { name: /中奖通知/ })).toBeNull();
        expect(screen.queryByText('这个文件夹里没有邮件。')).toBeNull();
    });

    it('shows the page returned with the delete without another login', async () => {
        showConfirmMock.mockResolvedValue(true);
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: fullPage([
                { uid: 2, subject: '中奖通知' },
                { uid: 3, subject: '保留邮件' },
            ]),
        } as never);
        vi.mocked(MailboxBrowseDelete).mockResolvedValue({
            moved: 1,
            messages: [
                { uid: 3, from: 'b@ex.com', subject: '保留邮件', seen: true },
                { uid: 4, from: 'c@ex.com', subject: '下一页的邮件', seen: false },
            ],
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        const row = await screen.findByRole('button', { name: /中奖通知/ });
        fireEvent.click(row.closest('.mailbox-manage__item')!.querySelector('button[aria-label="删除"]') as HTMLButtonElement);
        expect(await screen.findByRole('button', { name: /下一页的邮件/ })).toBeTruthy();
        expect(MailboxBrowseMessages).not.toHaveBeenCalled();
        expect(screen.getByRole('button', { name: /保留邮件/ })).toBeTruthy();
        expect(screen.queryByRole('button', { name: /中奖通知/ })).toBeNull();
        expect(screen.queryByText('这个文件夹里没有邮件。')).toBeNull();
        expect(screen.queryByText('正在读取邮件…')).toBeNull();
    });

    it('keeps the remaining rows when the delete response could not list the next page', async () => {
        showConfirmMock.mockResolvedValue(true);
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: fullPage([
                { uid: 2, subject: '中奖通知' },
                { uid: 3, subject: '保留邮件' },
            ]),
        } as never);
        vi.mocked(MailboxBrowseDelete)
            .mockResolvedValueOnce({ moved: 1, list_error: '列表刷新失败' } as never)
            .mockResolvedValueOnce({ moved: 1 } as never);
        vi.mocked(MailboxBrowseMessages).mockResolvedValue([
            { uid: 9, from: 'd@ex.com', subject: '下一页的邮件', seen: true },
        ] as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        const first = await screen.findByRole('button', { name: /中奖通知/ });
        fireEvent.click(first.closest('.mailbox-manage__item')!.querySelector('button[aria-label="删除"]') as HTMLButtonElement);
        expect((await screen.findByRole('alert')).textContent).toContain('列表刷新失败');
        expect(MailboxBrowseMessages).not.toHaveBeenCalled();
        expect(screen.getByRole('button', { name: /保留邮件/ })).toBeTruthy();
        expect(screen.queryByRole('button', { name: /中奖通知/ })).toBeNull();

        const second = screen.getByRole('button', { name: /保留邮件/ });
        fireEvent.click(second.closest('.mailbox-manage__item')!.querySelector('button[aria-label="删除"]') as HTMLButtonElement);
        expect(await screen.findByRole('button', { name: /下一页的邮件/ })).toBeTruthy();
        expect(MailboxBrowseMessages).toHaveBeenCalledTimes(1);
    });

    it('does not count an unread message twice when delete overlaps mark-read', async () => {
        showConfirmMock.mockResolvedValue(true);
        const read = deferred<{ uid: number; from: string; subject: string; seen: boolean; body: string }>();
        vi.mocked(MailboxBrowseRead).mockReturnValue(read.promise as never);
        vi.mocked(MailboxBrowseDelete).mockImplementation(async () => {
            read.resolve({ uid: 2, from: 'a@ex.com', subject: '中奖通知', seen: false, body: '正文' });
            return { moved: 1 };
        });
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            unseen: { INBOX: 2 },
            messages: [
                { uid: 2, from: 'a@ex.com', subject: '中奖通知', seen: false },
                { uid: 3, from: 'b@ex.com', subject: '保留邮件', seen: true },
            ],
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('button', { name: /中奖通知/ }));
        const row = screen.getByRole('button', { name: /中奖通知/ });
        fireEvent.click(row.closest('.mailbox-manage__item')!.querySelector('button[aria-label="删除"]') as HTMLButtonElement);
        await waitFor(() => expect(screen.queryByRole('button', { name: /中奖通知/ })).toBeNull());
        expect(document.querySelector('.mailbox-manage__badge')?.textContent).toBe('1');
        expect(screen.getByRole('button', { name: /保留邮件/ })).toBeTruthy();
    });

    it('keeps the unread count when the returned page still contains the deleted message', async () => {
        showConfirmMock.mockResolvedValue(true);
        const deleting = deferred<{ moved: number; messages: Array<{ uid: number; from: string; subject: string; seen: boolean }> }>();
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            unseen: { INBOX: 2 },
            messages: [
                { uid: 2, from: 'a@ex.com', subject: '中奖通知', seen: false },
                { uid: 3, from: 'b@ex.com', subject: '保留邮件', seen: true },
            ],
        } as never);
        vi.mocked(MailboxBrowseDelete).mockReturnValue(deleting.promise as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        const row = await screen.findByRole('button', { name: /中奖通知/ });
        const deleteBtn = row.closest('.mailbox-manage__item')!.querySelector('button[aria-label="删除"]') as HTMLButtonElement;
        fireEvent.click(deleteBtn);
        await waitFor(() => expect(deleteBtn.disabled).toBe(true));
        deleting.resolve({
            moved: 1,
            messages: [
                { uid: 2, from: 'a@ex.com', subject: '中奖通知', seen: false },
                { uid: 3, from: 'b@ex.com', subject: '保留邮件', seen: true },
            ],
        });
        await waitFor(() => expect(deleteBtn.disabled).toBe(false));
        expect(document.querySelector('.mailbox-manage__badge')?.textContent).toBe('2');
        const kept = screen.getByRole('button', { name: /中奖通知/ });
        fireEvent.click(kept.closest('.mailbox-manage__item')!.querySelector('button[aria-label="标为已读"]') as HTMLButtonElement);
        await waitFor(() => expect(MailboxBrowseMarkRead).toHaveBeenCalledWith('mb1', 'INBOX', [2], true));
    });

    it('does not restore the unread count when the returned page shows that message read', async () => {
        showConfirmMock.mockResolvedValue(true);
        const deleting = deferred<{ moved: number; messages: Array<{ uid: number; from: string; subject: string; seen: boolean }> }>();
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            unseen: { INBOX: 1 },
            messages: [{ uid: 2, from: 'a@ex.com', subject: '中奖通知', seen: false }],
        } as never);
        vi.mocked(MailboxBrowseDelete).mockReturnValue(deleting.promise as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        const row = await screen.findByRole('button', { name: /中奖通知/ });
        const deleteBtn = row.closest('.mailbox-manage__item')!.querySelector('button[aria-label="删除"]') as HTMLButtonElement;
        fireEvent.click(deleteBtn);
        await waitFor(() => expect(deleteBtn.disabled).toBe(true));
        deleting.resolve({
            moved: 1,
            messages: [{ uid: 2, from: 'a@ex.com', subject: '中奖通知', seen: true }],
        });
        await waitFor(() => expect(deleteBtn.disabled).toBe(false));
        expect(document.querySelector('.mailbox-manage__badge')).toBeNull();
        expect(screen.getByRole('button', { name: /中奖通知/ }).className).not.toContain('mailbox-manage__message--unread');
    });

    it('keeps an in-flight mark-read when the delete page still lists that message', async () => {
        showConfirmMock.mockResolvedValue(true);
        const marking = deferred<never>();
        vi.mocked(MailboxBrowseMarkRead).mockReturnValue(marking.promise as never);
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            unseen: { INBOX: 2 },
            messages: [
                { uid: 2, from: 'a@ex.com', subject: '刚打开的', seen: false },
                { uid: 3, from: 'b@ex.com', subject: '要删的', seen: false },
            ],
        } as never);
        vi.mocked(MailboxBrowseRead).mockResolvedValue({
            uid: 2, from: 'a@ex.com', subject: '刚打开的', seen: false, body: '正文',
        } as never);
        vi.mocked(MailboxBrowseDelete).mockResolvedValue({
            moved: 1,
            messages: [{ uid: 2, from: 'a@ex.com', subject: '刚打开的', seen: false }],
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('button', { name: /刚打开的/ }));
        await waitFor(() => {
            expect(screen.getByRole('button', { name: /刚打开的/ }).className).not.toContain('mailbox-manage__message--unread');
        });
        const victim = screen.getByRole('button', { name: /要删的/ });
        fireEvent.click(victim.closest('.mailbox-manage__item')!.querySelector('button[aria-label="删除"]') as HTMLButtonElement);
        await waitFor(() => expect(screen.queryByRole('button', { name: /要删的/ })).toBeNull());
        expect(screen.getByRole('button', { name: /刚打开的/ }).className).not.toContain('mailbox-manage__message--unread');
        expect(document.querySelector('.mailbox-manage__badge')).toBeNull();

        marking.reject(new Error('标已读失败'));
        await waitFor(() => {
            expect(screen.getByRole('button', { name: /刚打开的/ }).className).toContain('mailbox-manage__message--unread');
        });
        expect(document.querySelector('.mailbox-manage__badge')?.textContent).toBe('1');
    });

    it('keeps the open message read when delete leaves it on the page', async () => {
        showConfirmMock.mockResolvedValue(true);
        const marking = deferred<never>();
        const deleting = deferred<{ moved: number; messages: Array<{ uid: number; from: string; subject: string; seen: boolean }> }>();
        vi.mocked(MailboxBrowseMarkRead).mockReturnValue(marking.promise as never);
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            unseen: { INBOX: 1 },
            messages: [{ uid: 2, from: 'a@ex.com', subject: '刚打开的', seen: false }],
        } as never);
        vi.mocked(MailboxBrowseRead).mockResolvedValue({
            uid: 2, from: 'a@ex.com', subject: '刚打开的', seen: false, body: '正文',
        } as never);
        vi.mocked(MailboxBrowseDelete).mockReturnValue(deleting.promise as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('button', { name: /刚打开的/ }));
        await waitFor(() => {
            expect(screen.getByRole('button', { name: /刚打开的/ }).className).not.toContain('mailbox-manage__message--unread');
        });
        expect(document.querySelector('.mailbox-manage__badge')).toBeNull();
        const row = screen.getByRole('button', { name: /刚打开的/ });
        const deleteBtn = row.closest('.mailbox-manage__item')!.querySelector('button[aria-label="删除"]') as HTMLButtonElement;
        fireEvent.click(deleteBtn);
        await waitFor(() => expect(deleteBtn.disabled).toBe(true));
        deleting.resolve({
            moved: 1,
            messages: [{ uid: 2, from: 'a@ex.com', subject: '刚打开的', seen: false }],
        });
        await waitFor(() => {
            const kept = screen.getByRole('button', { name: /刚打开的/ });
            const button = kept.closest('.mailbox-manage__item')!.querySelector('button[aria-label="删除"]') as HTMLButtonElement;
            expect(button.disabled).toBe(false);
            expect(kept.className).not.toContain('mailbox-manage__message--unread');
        });
        expect(document.querySelector('.mailbox-manage__badge')).toBeNull();
        expect(screen.getByText('正文')).toBeTruthy();

        marking.reject(new Error('标已读失败'));
        await waitFor(() => {
            expect(screen.getByRole('button', { name: /刚打开的/ }).className).toContain('mailbox-manage__message--unread');
        });
        expect(document.querySelector('.mailbox-manage__badge')?.textContent).toBe('1');
    });

    it('says a delete inside the trash folder is permanent', async () => {
        showConfirmMock.mockResolvedValue(true);
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [
                { name: 'INBOX', special: 'inbox' },
                { name: 'Trash', special: 'trash' },
            ],
            box: 'INBOX',
            trash_box: 'Trash',
            messages: [{ uid: 2, from: 'a@ex.com', subject: '收件里的信', seen: true }],
        } as never);
        vi.mocked(MailboxBrowseMessages).mockResolvedValue([
            { uid: 4, from: 'a@ex.com', subject: '已在垃圾箱', seen: true },
        ] as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        const inbox = await screen.findByRole('button', { name: /收件里的信/ });
        fireEvent.click(inbox.closest('.mailbox-manage__item')!.querySelector('button[aria-label="删除"]') as HTMLButtonElement);
        await waitFor(() => expect(showConfirmMock).toHaveBeenCalled());
        expect(String(showConfirmMock.mock.calls[0][0])).toContain('移到已删除');
        await waitFor(() => expect(screen.queryByRole('button', { name: /收件里的信/ })).toBeNull());

        fireEvent.click(screen.getByRole('button', { name: '已删除' }));
        const trashed = await screen.findByRole('button', { name: /已在垃圾箱/ });
        fireEvent.click(trashed.closest('.mailbox-manage__item')!.querySelector('button[aria-label="删除"]') as HTMLButtonElement);
        await waitFor(() => expect(showConfirmMock).toHaveBeenCalledTimes(2));
        expect(String(showConfirmMock.mock.calls[1][0])).toContain('彻底删除');
        expect(String(showConfirmMock.mock.calls[1][0])).toContain('已在垃圾箱');
    });

    it('says every delete is permanent when the account has no trash folder', async () => {
        showConfirmMock.mockResolvedValue(false);
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            trash_box: '',
            messages: [{ uid: 2, from: 'a@ex.com', subject: '唯一的信', seen: true }],
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        const row = await screen.findByRole('button', { name: /唯一的信/ });
        fireEvent.click(row.closest('.mailbox-manage__item')!.querySelector('button[aria-label="删除"]') as HTMLButtonElement);
        await waitFor(() => expect(showConfirmMock).toHaveBeenCalled());
        expect(String(showConfirmMock.mock.calls[0][0])).toContain('彻底删除');
        expect(MailboxBrowseDelete).not.toHaveBeenCalled();
    });

    it('keeps the message when the delete confirmation is dismissed', async () => {
        showConfirmMock.mockResolvedValue(false);
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: [{ uid: 2, from: 'a@ex.com', subject: '中奖通知', seen: true }],
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        const row = await screen.findByRole('button', { name: /中奖通知/ });
        const rowActions = row.closest('.mailbox-manage__item')!.querySelector('.mailbox-manage__row-actions')!;
        fireEvent.click(rowActions.querySelector('button[aria-label="删除"]') as HTMLButtonElement);
        await waitFor(() => expect(showConfirmMock).toHaveBeenCalled());
        await flushMicrotasks();
        expect(MailboxBrowseDelete).not.toHaveBeenCalled();
        expect(screen.getByRole('button', { name: /中奖通知/ })).toBeTruthy();
    });

    it('marks a message unread from its row actions', async () => {
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: [{ uid: 8, from: 'a@ex.com', subject: '已读的邮件', seen: true }],
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        const row = await screen.findByRole('button', { name: /已读的邮件/ });
        const rowActions = row.closest('.mailbox-manage__item')!.querySelector('.mailbox-manage__row-actions')!;
        fireEvent.click(rowActions.querySelector('button[aria-label="标为未读"]') as HTMLButtonElement);
        await waitFor(() => expect(MailboxBrowseMarkRead).toHaveBeenCalledWith('mb1', 'INBOX', [8], false));
        await waitFor(() => expect(
            row.closest('.mailbox-manage__item')!.querySelector('.mailbox-manage__message--unread'),
        ).toBeTruthy());
    });

    it('selects messages by checkbox without opening them', async () => {
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: [
                { uid: 1, from: 'a@ex.com', subject: '第一封', seen: true },
                { uid: 2, from: 'b@ex.com', subject: '第二封', seen: true },
            ],
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        const first = await screen.findByRole('button', { name: /第一封/ });
        fireEvent.click(first.closest('.mailbox-manage__item')!.querySelector('.mailbox-manage__check') as HTMLButtonElement);
        await waitFor(() => expect(screen.getByTestId('mailbox-manage-batchbar').textContent).toContain('已选 1 封'));
        // Checkbox selection never opens the reading pane.
        expect(MailboxBrowseRead).not.toHaveBeenCalled();
        const second = screen.getByRole('button', { name: /第二封/ });
        fireEvent.click(second.closest('.mailbox-manage__item')!.querySelector('.mailbox-manage__check') as HTMLButtonElement);
        await waitFor(() => expect(screen.getByTestId('mailbox-manage-batchbar').textContent).toContain('已选 2 封'));
    });

    it('selects all messages at once and clears the selection', async () => {
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: [
                { uid: 1, from: 'a@ex.com', subject: '第一封', seen: true },
                { uid: 2, from: 'b@ex.com', subject: '第二封', seen: false },
            ],
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('checkbox', { name: '全选' }));
        await waitFor(() => expect(screen.getByTestId('mailbox-manage-batchbar').textContent).toContain('已选 2 封'));
        expect(screen.getAllByRole('checkbox', { checked: true }).length).toBeGreaterThanOrEqual(2);
        fireEvent.click(screen.getByRole('button', { name: '取消选择' }));
        expect(screen.getByTestId('mailbox-manage-batchbar').textContent).not.toContain('已选');
    });

    it('deletes the selected messages in one batch call after confirmation', async () => {
        showConfirmMock.mockResolvedValue(true);
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            unseen: { INBOX: 1 },
            messages: [
                { uid: 1, from: 'a@ex.com', subject: '第一封', seen: true },
                { uid: 2, from: 'b@ex.com', subject: '第二封', seen: false },
                { uid: 3, from: 'c@ex.com', subject: '第三封', seen: true },
            ],
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('checkbox', { name: '全选' }));
        fireEvent.click(screen.getByTestId('mailbox-manage-batch-delete'));
        expect(String((await waitFor(() => showConfirmMock.mock.calls[0]))[0])).toContain('3 封');
        await waitFor(() => expect(MailboxBrowseDelete).toHaveBeenCalledWith('mb1', 'INBOX', [1, 2, 3]));
        await flushMicrotasks();
        expect(MailboxBrowseMessages).not.toHaveBeenCalled();
        expect(screen.queryByRole('button', { name: /第一封/ })).toBeNull();
        expect(screen.queryByRole('button', { name: /第二封/ })).toBeNull();
        expect(screen.queryByRole('button', { name: /第三封/ })).toBeNull();
        expect(screen.getByText('这个文件夹里没有邮件。')).toBeTruthy();
        expect(screen.getByTestId('mailbox-manage-batchbar').textContent).not.toContain('已选');
    });

    it('replaces a deleted full page with the next server page', async () => {
        showConfirmMock.mockResolvedValue(true);
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: fullPage([{ uid: 1, subject: '第一封' }]),
        } as never);
        vi.mocked(MailboxBrowseMessages).mockResolvedValue([
            { uid: 9, from: 'd@ex.com', subject: '下一页的邮件', seen: true },
        ] as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('checkbox', { name: '全选' }));
        fireEvent.click(screen.getByTestId('mailbox-manage-batch-delete'));
        await waitFor(() => expect(MailboxBrowseDelete).toHaveBeenCalled());
        const deleted = vi.mocked(MailboxBrowseDelete).mock.calls[0][2] as number[];
        expect(deleted).toHaveLength(MESSAGE_PAGE);
        expect(await screen.findByRole('button', { name: /下一页的邮件/ })).toBeTruthy();
        expect(screen.queryByRole('button', { name: /第一封/ })).toBeNull();
        expect(screen.queryByText('这个文件夹里没有邮件。')).toBeNull();
        expect(screen.getByTestId('mailbox-manage-batchbar').textContent).not.toContain('已选');
    });

    it('relists when a full-page delete response still contains the selected messages', async () => {
        showConfirmMock.mockResolvedValue(true);
        const page = fullPage([{ uid: 1, subject: '第一封' }]);
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: page,
        } as never);
        vi.mocked(MailboxBrowseDelete).mockResolvedValue({
            moved: MESSAGE_PAGE,
            messages: page,
        } as never);
        vi.mocked(MailboxBrowseMessages).mockResolvedValue([
            { uid: 9, from: 'd@ex.com', subject: '下一页的邮件', seen: true },
        ] as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('checkbox', { name: '全选' }));
        fireEvent.click(screen.getByTestId('mailbox-manage-batch-delete'));
        expect(await screen.findByRole('button', { name: /下一页的邮件/ })).toBeTruthy();
        expect(screen.queryByRole('button', { name: /第一封/ })).toBeNull();
        expect(MailboxBrowseMessages).toHaveBeenCalledTimes(1);
        expect(screen.getByTestId('mailbox-manage-batchbar').textContent).not.toContain('已选');
    });

    it('keeps the unread count when the reloaded full page still lists the selection', async () => {
        showConfirmMock.mockResolvedValue(true);
        const page = fullPage([{ uid: 1, subject: '第一封', seen: false }]);
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            unseen: { INBOX: 1 },
            messages: page,
        } as never);
        vi.mocked(MailboxBrowseDelete).mockResolvedValue({
            moved: MESSAGE_PAGE,
            messages: page,
        } as never);
        vi.mocked(MailboxBrowseMessages).mockResolvedValue(page as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('checkbox', { name: '全选' }));
        fireEvent.click(screen.getByTestId('mailbox-manage-batch-delete'));
        await waitFor(() => expect(MailboxBrowseMessages).toHaveBeenCalledTimes(1));
        expect(screen.getByRole('button', { name: /第一封/ })).toBeTruthy();
        expect(document.querySelector('.mailbox-manage__badge')?.textContent).toBe('1');
        expect(screen.getByTestId('mailbox-manage-batchbar').textContent).toContain('已选');
    });

    it('keeps the list when reloading a repeated full page fails', async () => {
        showConfirmMock.mockResolvedValue(true);
        const page = fullPage([{ uid: 1, subject: '第一封', seen: false }]);
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            unseen: { INBOX: 1 },
            messages: page,
        } as never);
        vi.mocked(MailboxBrowseDelete).mockResolvedValue({
            moved: MESSAGE_PAGE,
            messages: page,
        } as never);
        vi.mocked(MailboxBrowseMessages).mockRejectedValue(new Error('列表刷新失败'));

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('checkbox', { name: '全选' }));
        fireEvent.click(screen.getByTestId('mailbox-manage-batch-delete'));
        expect((await screen.findByRole('alert')).textContent).toContain('列表刷新失败');
        expect(screen.getByRole('button', { name: /第一封/ })).toBeTruthy();
        expect(document.querySelector('.mailbox-manage__badge')?.textContent).toBe('1');
        expect(screen.queryByText('这个文件夹里没有邮件。')).toBeNull();
    });

    it('marks the selected messages read in one batch call', async () => {
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            unseen: { INBOX: 2 },
            messages: [
                { uid: 1, from: 'a@ex.com', subject: '第一封', seen: false },
                { uid: 2, from: 'b@ex.com', subject: '第二封', seen: false },
            ],
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('checkbox', { name: '全选' }));
        fireEvent.click(screen.getByTestId('mailbox-manage-batchbar').querySelector("button[aria-label='标为已读']") as HTMLButtonElement);
        await waitFor(() => expect(MailboxBrowseMarkRead).toHaveBeenCalledWith('mb1', 'INBOX', [1, 2], true));
        await waitFor(() => expect(document.querySelectorAll('.mailbox-manage__message--unread').length).toBe(0));
    });

    it('splits selections beyond the server delete cap into sequential calls', async () => {
        showConfirmMock.mockResolvedValue(true);
        const many = Array.from({ length: DELETE_CHUNK + 2 }, (_, i) => ({
            uid: i + 1, from: 'a@ex.com', subject: `邮件${i + 1}`, seen: true,
        }));
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: many,
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('checkbox', { name: '全选' }));
        fireEvent.click(screen.getByTestId('mailbox-manage-batch-delete'));
        await waitFor(() => expect(MailboxBrowseDelete).toHaveBeenCalledTimes(2));
        expect(MailboxBrowseDelete).toHaveBeenNthCalledWith(1, 'mb1', 'INBOX', Array.from({ length: DELETE_CHUNK }, (_, i) => i + 1));
        expect(MailboxBrowseDelete).toHaveBeenNthCalledWith(2, 'mb1', 'INBOX', [DELETE_CHUNK + 1, DELETE_CHUNK + 2]);
    });

    it('keeps the page from the last delete call when a selection is split', async () => {
        showConfirmMock.mockResolvedValue(true);
        const many = Array.from({ length: DELETE_CHUNK + 2 }, (_, i) => ({
            uid: i + 1, from: 'a@ex.com', subject: `邮件${i + 1}`, seen: true,
        }));
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: many,
        } as never);
        vi.mocked(MailboxBrowseDelete)
            .mockResolvedValueOnce({
                moved: DELETE_CHUNK,
                messages: [{ uid: 1, from: 'a@ex.com', subject: '过期页', seen: true }],
            } as never)
            .mockResolvedValueOnce({
                moved: 2,
                messages: [{ uid: 9, from: 'd@ex.com', subject: '最后一页', seen: true }],
            } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('checkbox', { name: '全选' }));
        fireEvent.click(screen.getByTestId('mailbox-manage-batch-delete'));
        expect(await screen.findByRole('button', { name: /最后一页/ })).toBeTruthy();
        expect(screen.queryByRole('button', { name: /过期页/ })).toBeNull();
        expect(MailboxBrowseMessages).not.toHaveBeenCalled();
    });

    it('keeps the remaining rows when the reload after delete fails', async () => {
        showConfirmMock.mockResolvedValue(true);
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: fullPage([
                { uid: 2, subject: '中奖通知' },
                { uid: 3, subject: '保留邮件' },
            ]),
        } as never);
        vi.mocked(MailboxBrowseMessages).mockRejectedValue(new Error('列表刷新失败'));

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        const row = await screen.findByRole('button', { name: /中奖通知/ });
        const rowActions = row.closest('.mailbox-manage__item')!.querySelector('.mailbox-manage__row-actions')!;
        fireEvent.click(rowActions.querySelector('button[aria-label="删除"]') as HTMLButtonElement);
        expect((await screen.findByRole('alert')).textContent).toContain('列表刷新失败');
        expect(screen.getByRole('button', { name: /保留邮件/ })).toBeTruthy();
        expect(screen.queryByRole('button', { name: /中奖通知/ })).toBeNull();
        expect(screen.queryByText('这个文件夹里没有邮件。')).toBeNull();
    });

    it('refills again when an earlier reload failed and left a short list', async () => {
        showConfirmMock.mockResolvedValue(true);
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: fullPage([
                { uid: 2, subject: '中奖通知' },
                { uid: 3, subject: '保留邮件' },
            ]),
        } as never);
        vi.mocked(MailboxBrowseMessages)
            .mockRejectedValueOnce(new Error('列表刷新失败'))
            .mockResolvedValue([{ uid: 9, from: 'd@ex.com', subject: '下一页的邮件', seen: true }] as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        const first = await screen.findByRole('button', { name: /中奖通知/ });
        fireEvent.click(first.closest('.mailbox-manage__item')!.querySelector('button[aria-label="删除"]') as HTMLButtonElement);
        expect((await screen.findByRole('alert')).textContent).toContain('列表刷新失败');
        expect(screen.getByRole('button', { name: /保留邮件/ })).toBeTruthy();

        const second = screen.getByRole('button', { name: /保留邮件/ });
        fireEvent.click(second.closest('.mailbox-manage__item')!.querySelector('button[aria-label="删除"]') as HTMLButtonElement);
        expect(await screen.findByRole('button', { name: /下一页的邮件/ })).toBeTruthy();
        expect(MailboxBrowseMessages).toHaveBeenCalledTimes(2);
        expect(screen.queryByRole('button', { name: /保留邮件/ })).toBeNull();
    });

    it('keeps the remaining rows visible while the next page loads', async () => {
        showConfirmMock.mockResolvedValue(true);
        const listed = deferred<unknown[]>();
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: fullPage([
                { uid: 2, subject: '中奖通知' },
                { uid: 3, subject: '保留邮件' },
            ]),
        } as never);
        vi.mocked(MailboxBrowseMessages).mockReturnValue(listed.promise as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        const row = await screen.findByRole('button', { name: /中奖通知/ });
        const rowActions = row.closest('.mailbox-manage__item')!.querySelector('.mailbox-manage__row-actions')!;
        fireEvent.click(rowActions.querySelector('button[aria-label="删除"]') as HTMLButtonElement);
        await waitFor(() => expect(screen.queryByRole('button', { name: /中奖通知/ })).toBeNull());
        // A partial delete must not flash the empty-folder line or a full reload.
        expect(screen.getByRole('button', { name: /保留邮件/ })).toBeTruthy();
        expect(screen.queryByText('正在读取邮件…')).toBeNull();
        expect(screen.queryByText('这个文件夹里没有邮件。')).toBeNull();
        listed.resolve([
            { uid: 3, from: 'b@ex.com', subject: '保留邮件', seen: true },
            { uid: 4, from: 'c@ex.com', subject: '下一页的邮件', seen: true },
        ]);
        expect(await screen.findByRole('button', { name: /下一页的邮件/ })).toBeTruthy();
    });

    it('drops a read that lands after its message was deleted', async () => {
        showConfirmMock.mockResolvedValue(true);
        const read = deferred<Record<string, unknown>>();
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: [
                { uid: 2, from: 'a@ex.com', subject: '中奖通知', seen: true },
                { uid: 3, from: 'b@ex.com', subject: '保留邮件', seen: true },
            ],
        } as never);
        vi.mocked(MailboxBrowseRead).mockReturnValue(read.promise as never);
        vi.mocked(MailboxBrowseMessages).mockResolvedValue([
            { uid: 3, from: 'b@ex.com', subject: '保留邮件', seen: true },
        ] as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('button', { name: /中奖通知/ }));
        expect(await screen.findByText('正在打开…')).toBeTruthy();
        const row = screen.getByRole('button', { name: /中奖通知/ });
        const rowActions = row.closest('.mailbox-manage__item')!.querySelector('.mailbox-manage__row-actions')!;
        fireEvent.click(rowActions.querySelector('button[aria-label="删除"]') as HTMLButtonElement);
        await waitFor(() => expect(screen.queryByRole('button', { name: /中奖通知/ })).toBeNull());
        expect(screen.getByRole('button', { name: /保留邮件/ })).toBeTruthy();
        read.resolve({ uid: 2, from: 'a@ex.com', subject: '中奖通知', seen: true, body: '不该出现' });
        await flushMicrotasks();
        expect(screen.queryByText('不该出现')).toBeNull();
        expect(screen.getByText('选择一封邮件，在这里查看内容。')).toBeTruthy();
    });

    it('ignores a folder switch while a delete is still running', async () => {
        showConfirmMock.mockResolvedValue(true);
        const deleting = deferred<{ moved: number }>();
        vi.mocked(MailboxBrowseDelete).mockReturnValue(deleting.promise as never);
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [
                { name: 'INBOX', special: 'inbox' },
                { name: 'Junk', special: 'junk' },
            ],
            box: 'INBOX',
            messages: fullPage([{ uid: 1, subject: '第一封' }]),
        } as never);
        vi.mocked(MailboxBrowseMessages).mockResolvedValue([
            { uid: 8, from: 'n@ex.com', subject: '下一页的邮件', seen: true },
        ] as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        const row = await screen.findByRole('button', { name: /第一封/ });
        const rowActions = row.closest('.mailbox-manage__item')!.querySelector('.mailbox-manage__row-actions')!;
        fireEvent.click(rowActions.querySelector('button[aria-label="删除"]') as HTMLButtonElement);
        await waitFor(() => expect(MailboxBrowseDelete).toHaveBeenCalledWith('mb1', 'INBOX', [1]));
        fireEvent.click(screen.getByRole('button', { name: '垃圾邮件' }));
        expect(MailboxBrowseMessages).not.toHaveBeenCalled();
        deleting.resolve({ moved: 1 });
        expect(await screen.findByRole('button', { name: /下一页的邮件/ })).toBeTruthy();
        expect(screen.getByRole('button', { name: '收件箱' }).getAttribute('aria-current')).toBe('true');
        expect(MailboxBrowseMessages).toHaveBeenCalledTimes(1);
        expect(MailboxBrowseMessages).toHaveBeenCalledWith('mb1', 'INBOX', 30);
    });

    it('keeps the selection when a batch delete is dismissed', async () => {
        showConfirmMock.mockResolvedValue(false);
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: [{ uid: 1, from: 'a@ex.com', subject: '第一封', seen: true }],
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        clickRowCheckbox(await screen.findByRole('button', { name: /第一封/ }));
        expect(screen.getByTestId('mailbox-manage-batchbar').textContent).toContain('已选 1 封');
        fireEvent.click(screen.getByTestId('mailbox-manage-batch-delete'));
        await waitFor(() => expect(showConfirmMock).toHaveBeenCalled());
        expect(MailboxBrowseDelete).not.toHaveBeenCalled();
        expect(screen.getByTestId('mailbox-manage-batchbar').textContent).toContain('已选 1 封');
        expect(screen.getByRole('button', { name: /第一封/ })).toBeTruthy();
    });

    it('clears the selection when the folder changes', async () => {
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [
                { name: 'INBOX', special: 'inbox' },
                { name: 'Junk', special: 'junk' },
            ],
            box: 'INBOX',
            messages: [{ uid: 1, from: 'a@ex.com', subject: '第一封', seen: true }],
        } as never);
        vi.mocked(MailboxBrowseMessages).mockResolvedValue([]);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        clickRowCheckbox(await screen.findByRole('button', { name: /第一封/ }));
        expect(screen.getByTestId('mailbox-manage-batchbar').textContent).toContain('已选 1 封');
        fireEvent.click(screen.getByRole('button', { name: '垃圾邮件' }));
        await waitFor(() => expect(screen.getByTestId('mailbox-manage-batchbar').textContent).not.toContain('已选'));
    });

    it('rolls an automatic mark-read back silently when the server refuses it', async () => {
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: [{ uid: 9, from: 'a@ex.com', subject: '待读邮件', seen: false }],
        } as never);
        vi.mocked(MailboxBrowseRead).mockResolvedValue({
            uid: 9, from: 'a@ex.com', subject: '待读邮件', seen: false, body: '正文',
        } as never);
        vi.mocked(MailboxBrowseMarkRead).mockRejectedValue(new Error('无法连接服务器'));

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        fireEvent.click(await screen.findByRole('button', { name: /待读邮件/ }));
        await waitFor(() => expect(MailboxBrowseMarkRead).toHaveBeenCalled());
        await flushMicrotasks();
        // The optimistic flip is undone quietly: POP3 mailboxes have no server
        // seen flag, so opening mail must not spam an error banner.
        expect(screen.getByRole('button', { name: /待读邮件/ }).className).toContain('mailbox-manage__message--unread');
        expect(screen.queryByRole('alert')).toBeNull();
    });

    it('reports a manual seen toggle that the server refuses', async () => {
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: [{ uid: 9, from: 'a@ex.com', subject: '已读的邮件', seen: true }],
        } as never);
        vi.mocked(MailboxBrowseMarkRead).mockRejectedValue(new Error('无法连接服务器'));

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        const row = await screen.findByRole('button', { name: /已读的邮件/ });
        const rowActions = row.closest('.mailbox-manage__item')!.querySelector('.mailbox-manage__row-actions')!;
        fireEvent.click(rowActions.querySelector('button[aria-label="标为未读"]') as HTMLButtonElement);
        expect((await screen.findByRole('alert')).textContent).toContain('已回退');
        // The flip rolled back to the server-side state.
        expect(row.className).not.toContain('mailbox-manage__message--unread');
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
