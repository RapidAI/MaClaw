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
import { MailboxManageDialog } from '../MailboxManageDialog';

const account = { id: 'mb1', name: 'Work', address: 'w@ex.com' };

// Keep in sync with DELETE_CHUNK in MailboxManageDialog.tsx.
const DELETE_CHUNK = 100;

function clickRowCheckbox(row: HTMLElement) {
    fireEvent.click(row.closest('.mailbox-manage__item')!.querySelector('.mailbox-manage__check') as HTMLButtonElement);
}

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

    it('deletes a message after confirmation and drops it from the list', async () => {
        showConfirmMock.mockResolvedValue(true);
        vi.mocked(MailboxBrowseStart).mockResolvedValue({
            folders: [{ name: 'INBOX', special: 'inbox' }],
            box: 'INBOX',
            messages: [
                { uid: 2, from: 'a@ex.com', subject: '中奖通知', seen: true },
                { uid: 3, from: 'b@ex.com', subject: '保留邮件', seen: true },
            ],
        } as never);
        vi.mocked(MailboxBrowseRead).mockResolvedValue({
            uid: 2, from: 'a@ex.com', subject: '中奖通知', seen: true, body: '内容',
        } as never);

        render(<MailboxManageDialog lang="zh-Hans" account={account} onClose={onClose} />);

        const row = await screen.findByRole('button', { name: /中奖通知/ });
        // Hovering a row reveals its quick actions; delete carries the trash icon.
        const rowActions = row.closest('.mailbox-manage__item')!.querySelector('.mailbox-manage__row-actions')!;
        const deleteBtn = rowActions.querySelector('button[aria-label="删除"]') as HTMLButtonElement;
        fireEvent.click(deleteBtn);
        await waitFor(() => expect(showConfirmMock).toHaveBeenCalled());
        expect(String(showConfirmMock.mock.calls[0][0])).toContain('中奖通知');
        await waitFor(() => expect(MailboxBrowseDelete).toHaveBeenCalledWith('mb1', 'INBOX', [2]));
        expect(await screen.findByRole('button', { name: /保留邮件/ })).toBeTruthy();
        expect(screen.queryByRole('button', { name: /中奖通知/ })).toBeNull();
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
        // All three rows leave the list and the unread badge loses the one unread row.
        expect(screen.queryByRole('button', { name: /第一封/ })).toBeNull();
        expect(screen.queryByRole('button', { name: /第二封/ })).toBeNull();
        expect(screen.queryByRole('button', { name: /第三封/ })).toBeNull();
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
