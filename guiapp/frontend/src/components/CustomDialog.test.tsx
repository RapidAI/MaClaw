// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useEffect } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { DialogProvider, presentDialogMessage, useDialog } from './CustomDialog';
import { EVENT_SHOW_CONFIRM } from '../constants/events';

const { eventsOnMock, resolveFrontendConfirmMock } = vi.hoisted(() => ({
    eventsOnMock: vi.fn((_event: string, _handler: (payload: unknown) => unknown) => vi.fn()),
    resolveFrontendConfirmMock: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('../../wailsjs/runtime', () => ({
    EventsOn: eventsOnMock,
}));

vi.mock('../../wailsjs/go/main/App', () => ({
    ResolveFrontendConfirm: resolveFrontendConfirmMock,
}));

function ConfirmLauncher({ onResult }: { onResult?: (confirmed: boolean) => void }) {
    const { showConfirm } = useDialog();
    return (
        <button
            onClick={() => {
                void showConfirm('中止后当前进度将被清除。', '中止当前工作流？', {
                    confirmText: '中止',
                    cancelText: '取消',
                    confirmVariant: 'danger',
                }).then(result => onResult?.(result));
            }}
        >
            open
        </button>
    );
}

function PromptLauncher({ onResult }: { onResult?: (value: string | null) => void }) {
    const { showPrompt } = useDialog();
    return (
        <button
            onClick={() => {
                void showPrompt('请粘贴浏览器页面中显示的授权码 (Authorization Code):', '授权码', {
                    placeholder: '在此粘贴授权码',
                    confirmText: '确定',
                    cancelText: '取消',
                }).then(result => onResult?.(result));
            }}
        >
            open-prompt
        </button>
    );
}

function FallbackConfirmLauncher({ onResult }: { onResult: (confirmed: boolean) => void }) {
    const { showConfirm } = useDialog();
    return <button onClick={() => { void showConfirm('fallback').then(onResult); }}>open-fallback</button>;
}

function ReplacementLauncher({ onFirstResult, onSecondResult }: {
    onFirstResult: (confirmed: boolean) => void;
    onSecondResult: (confirmed: boolean) => void;
}) {
    const { showConfirm } = useDialog();
    return (
        <>
            <button onClick={() => { void showConfirm('first dialog').then(onFirstResult); }}>open-first</button>
            <button onClick={() => { void showConfirm('replacement dialog').then(onSecondResult); }}>open-second</button>
        </>
    );
}

function ConfirmThenAlertLauncher({ onConfirmResult }: { onConfirmResult: (confirmed: boolean) => void }) {
    const { showAlert, showConfirm } = useDialog();
    return (
        <>
            <button onClick={() => { void showConfirm('destructive confirmation').then(onConfirmResult); }}>open-destructive</button>
            <button onClick={() => { void showAlert('backend notice'); }}>open-notice</button>
        </>
    );
}

function PendingPromptLauncher({ onResult }: { onResult: (value: string | null) => void }) {
    const { showPrompt } = useDialog();
    return <button onClick={() => { void showPrompt('pending prompt').then(onResult); }}>open-pending</button>;
}

function PendingConfirmLauncher({ onResult }: { onResult: (confirmed: boolean) => void }) {
    const { showConfirm } = useDialog();
    return <button onClick={() => { void showConfirm('pending confirm').then(onResult); }}>open-pending-confirm</button>;
}

function FollowUpLauncher({ onFirstResult, onSecondResult }: {
    onFirstResult: (confirmed: boolean) => void;
    onSecondResult: (confirmed: boolean) => void;
}) {
    const { showConfirm } = useDialog();
    return (
        <button
            onClick={() => {
                void showConfirm('first follow-up dialog').then(async firstResult => {
                    onFirstResult(firstResult);
                    const secondResult = await showConfirm('second follow-up dialog');
                    onSecondResult(secondResult);
                });
            }}
        >
            open-follow-up
        </button>
    );
}

/** Bubble-phase Escape listener that nested app dialogs typically register. */
function NestedEscapeProbe({ onEscape }: { onEscape: () => void }) {
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key === 'Escape') onEscape();
        };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, [onEscape]);
    return null;
}

describe('presentDialogMessage', () => {
    it('lifts a long quoted subject and the irreversibility line out of one sentence', () => {
        expect(presentDialogMessage('确定从列表删除任务「生成纪念小布（布偶 猫）5岁生日的ppt」？此操作不可撤销。')).toEqual({
            lead: '确定从列表删除任务？',
            subject: '生成纪念小布（布偶 猫）5岁生日的ppt',
            detail: undefined,
            notice: '此操作不可撤销',
        });
    });

    it('keeps a readable action when the quoted name sits before 及', () => {
        expect(presentDialogMessage('永久删除「人工智能数学基础书编写」及全部远程文件？此操作不可撤销。\n同时将删除以下关联任务：\n· 草稿')).toEqual({
            lead: '永久删除此项及全部远程文件？',
            subject: '人工智能数学基础书编写',
            detail: '同时将删除以下关联任务：\n· 草稿',
            notice: '此操作不可撤销',
        });
    });

    it('keeps an English question mark when the quoted name ends the question', () => {
        expect(presentDialogMessage('Are you sure you want to delete Skill "release-notes"? This cannot be undone.')).toEqual({
            lead: 'Are you sure you want to delete Skill?',
            subject: 'release-notes',
            detail: undefined,
            notice: 'This cannot be undone',
        });
    });

    it('does not pull a quote out of the middle of a sentence', () => {
        const message = '删除文件夹「项目资料备份」及其全部内容？将同时删除本地缓存和云端文件，此操作不可撤销。';
        const presented = presentDialogMessage(message);
        expect(presented.subject).toBeUndefined();
        expect(presented.lead).toBe('删除文件夹「项目资料备份」及其全部内容？将同时删除本地缓存和云端文件');
        expect(presented.notice).toBe('此操作不可撤销');
    });

    it('does not treat a traditional character inside the name as traditional copy', () => {
        expect(presentDialogMessage('永久删除「用戶後台」及全部远程文件？此操作不可撤销。')).toMatchObject({
            lead: '永久删除此项及全部远程文件？',
            subject: '用戶後台',
            notice: '此操作不可撤销',
        });
    });

    it('keeps a folder name inside Delete folder … and all of its contents', () => {
        const presented = presentDialogMessage('Delete folder “Project Notes” and all of its contents? This removes the local cache. This cannot be undone.');
        expect(presented.subject).toBeUndefined();
        expect(presented.lead).toBe('Delete folder “Project Notes” and all of its contents? This removes the local cache.');
        expect(presented.notice).toBe('This cannot be undone');
    });

    it('keeps words on the same line when the notice sits between them', () => {
        expect(presentDialogMessage('移除，此操作不可撤销。请先导出。')).toMatchObject({
            lead: '移除，请先导出。',
            notice: '此操作不可撤销',
        });
        expect(presentDialogMessage('Remove it. This cannot be undone. Export first.')).toMatchObject({
            lead: 'Remove it. Export first.',
            notice: 'This cannot be undone',
        });
    });

    it('uses a traditional referent when the sentence is traditional Chinese', () => {
        expect(presentDialogMessage('永久刪除「人工智能數學基礎」及全部遠端檔案？此操作不可復原。')).toMatchObject({
            lead: '永久刪除此項及全部遠端檔案？',
            subject: '人工智能數學基礎',
            notice: '此操作不可復原',
        });
    });

    it('keeps a name that is followed by 吗 inside the question', () => {
        const presented = presentDialogMessage('确定要删除 Skill「release-notes」吗？此操作不可撤销。');
        expect(presented.subject).toBeUndefined();
        expect(presented.lead).toBe('确定要删除 Skill「release-notes」吗？');
        expect(presented.notice).toBe('此操作不可撤销');
    });

    it('does not lift a quote that is only followed by another line', () => {
        const presented = presentDialogMessage('读取「配置文件」\n请检查路径后重试。');
        expect(presented.subject).toBeUndefined();
        expect(presented.detail).toBeUndefined();
        expect(presented.lead).toBe('读取「配置文件」\n请检查路径后重试。');
    });

    it('does not treat a hyphen line as a separate list', () => {
        const presented = presentDialogMessage('保存失败。\n- 端口被占用');
        expect(presented.detail).toBeUndefined();
        expect(presented.lead).toBe('保存失败。\n- 端口被占用');
    });

    it('keeps the explanation after a quoted question as body text', () => {
        expect(presentDialogMessage('确认从 MaClaw 列表中移除“仓库甲”？\n\n这不会删除真实文件。')).toEqual({
            lead: '确认从 MaClaw 列表中移除？',
            subject: '仓库甲',
            follow: '这不会删除真实文件。',
            notice: undefined,
        });
    });

    it('keeps an ordinary second paragraph in the lead', () => {
        const presented = presentDialogMessage('保存失败。\n请检查网络后重试。');
        expect(presented.lead).toBe('保存失败。\n请检查网络后重试。');
        expect(presented.detail).toBeUndefined();
        expect(presented.notice).toBeUndefined();
    });

    it('leaves an ordinary sentence intact', () => {
        expect(presentDialogMessage('中止后当前进度将被清除。')).toMatchObject({
            lead: '中止后当前进度将被清除。',
        });
        expect(presentDialogMessage('中止后当前进度将被清除。').subject).toBeUndefined();
        expect(presentDialogMessage('中止后当前进度将被清除。').notice).toBeUndefined();
    });
});

describe('CustomDialog', () => {
    afterEach(() => {
        cleanup();
        eventsOnMock.mockReset();
        eventsOnMock.mockImplementation(() => vi.fn());
        resolveFrontendConfirmMock.mockReset();
        resolveFrontendConfirmMock.mockResolvedValue(undefined);
    });

    it('renders destructive confirmations with the danger button style', async () => {
        const onResult = vi.fn();
        render(
            <DialogProvider>
                <ConfirmLauncher onResult={onResult} />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open' }));

        const confirmButton = await screen.findByRole('button', { name: '中止' });
        expect(confirmButton.className).toContain('btn-danger');
        expect(confirmButton.closest('.custom-dialog')?.className).toContain('custom-dialog--danger');
        expect(screen.getByRole('button', { name: '取消' })).toBeTruthy();
        expect(screen.getByText('中止后当前进度将被清除。')).toBeTruthy();

        fireEvent.keyDown(window, { key: 'Enter' });
        expect(screen.getByRole('button', { name: '中止' })).toBeTruthy();
        expect(onResult).not.toHaveBeenCalled();

        fireEvent.click(confirmButton);
        await waitFor(() => expect(onResult).toHaveBeenCalledWith(true));
    });

    it('shows a custom prompt dialog and returns the entered value', async () => {
        const onResult = vi.fn();
        render(
            <DialogProvider>
                <PromptLauncher onResult={onResult} />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open-prompt' }));

        expect(await screen.findByText('请粘贴浏览器页面中显示的授权码 (Authorization Code):')).toBeTruthy();
        const input = screen.getByPlaceholderText('在此粘贴授权码') as HTMLInputElement;
        fireEvent.change(input, { target: { value: 'auth-code-123' } });
        fireEvent.click(screen.getByRole('button', { name: '确定' }));

        await waitFor(() => expect(onResult).toHaveBeenCalledWith('auth-code-123'));
    });

    it('returns null when the prompt dialog is cancelled', async () => {
        const onResult = vi.fn();
        render(
            <DialogProvider>
                <PromptLauncher onResult={onResult} />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open-prompt' }));
        fireEvent.click(await screen.findByRole('button', { name: '取消' }));

        await waitFor(() => expect(onResult).toHaveBeenCalledWith(null));
    });

    it('submits the prompt value on Enter without rebinding every keystroke', async () => {
        const onResult = vi.fn();
        render(
            <DialogProvider>
                <PromptLauncher onResult={onResult} />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open-prompt' }));
        const input = await screen.findByPlaceholderText('在此粘贴授权码');
        fireEvent.change(input, { target: { value: 'enter-code' } });
        fireEvent.keyDown(window, { key: 'Enter' });

        await waitFor(() => expect(onResult).toHaveBeenCalledWith('enter-code'));
    });

    it('stacks above nested app dialogs (LLM config overlay uses z-index 9999)', async () => {
        render(
            <DialogProvider>
                <PromptLauncher />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open-prompt' }));
        const backdrop = await screen.findByText('请粘贴浏览器页面中显示的授权码 (Authorization Code):');
        const overlay = backdrop.closest('.modal-backdrop') as HTMLElement | null;
        expect(overlay).toBeTruthy();
        expect(overlay!.style.zIndex).toBe('120000');
    });

    it('does not submit prompt on Enter while IME is composing', async () => {
        const onResult = vi.fn();
        render(
            <DialogProvider>
                <PromptLauncher onResult={onResult} />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open-prompt' }));
        const input = await screen.findByPlaceholderText('在此粘贴授权码');
        fireEvent.change(input, { target: { value: 'partial' } });
        fireEvent.keyDown(window, { key: 'Enter', isComposing: true });
        expect(onResult).not.toHaveBeenCalled();
        expect(screen.getByPlaceholderText('在此粘贴授权码')).toBeTruthy();
    });

    it('handles Escape in capture phase so nested bubble listeners do not also run', async () => {
        const nestedEscapes: string[] = [];
        const onResult = vi.fn();
        render(
            <DialogProvider>
                <NestedEscapeProbe onEscape={() => nestedEscapes.push('nested')} />
                <PromptLauncher onResult={onResult} />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open-prompt' }));
        await screen.findByPlaceholderText('在此粘贴授权码');

        // Native KeyboardEvent so capture + stopImmediatePropagation behave like the browser.
        window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));

        await waitFor(() => expect(onResult).toHaveBeenCalledWith(null));
        expect(nestedEscapes).toEqual([]);
    });

    it('keeps keyboard focus inside the dialog and restores the invoking control after close', async () => {
        render(
            <DialogProvider>
                <ConfirmLauncher />
            </DialogProvider>,
        );

        const trigger = screen.getByRole('button', { name: 'open' });
        trigger.focus();
        fireEvent.click(trigger);
        const confirmButton = await screen.findByRole('button', { name: '中止' });
        await waitFor(() => expect(document.activeElement).toBe(confirmButton));

        fireEvent.keyDown(window, { key: 'Tab' });
        expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Close' }));
        fireEvent.keyDown(window, { key: 'Tab' });
        expect(document.activeElement).toBe(screen.getByRole('button', { name: '取消' }));
        fireEvent.keyDown(window, { key: 'Escape' });
        await waitFor(() => expect(document.activeElement).toBe(trigger));
    });

    it('lets a focused cancel button handle Enter instead of confirming the dialog', async () => {
        const onResult = vi.fn();
        render(
            <DialogProvider>
                <ConfirmLauncher onResult={onResult} />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open' }));
        const cancelButton = await screen.findByRole('button', { name: '取消' });
        cancelButton.focus();

        fireEvent.keyDown(cancelButton, { key: 'Enter' });
        expect(screen.getByRole('button', { name: '中止' })).toBeTruthy();
        fireEvent.click(cancelButton);

        await waitFor(() => expect(onResult).toHaveBeenCalledWith(false));
    });

    it('restores focus to the original control when a dialog replaces another dialog', async () => {
        const onFirstResult = vi.fn();
        const onSecondResult = vi.fn();
        render(
            <DialogProvider>
                <ReplacementLauncher onFirstResult={onFirstResult} onSecondResult={onSecondResult} />
            </DialogProvider>,
        );

        const firstTrigger = screen.getByRole('button', { name: 'open-first' });
        firstTrigger.focus();
        fireEvent.click(firstTrigger);
        await screen.findByText('first dialog');

        fireEvent.click(screen.getByRole('button', { name: 'open-second' }));
        await screen.findByText('replacement dialog');
        fireEvent.keyDown(window, { key: 'Escape' });

        await waitFor(() => expect(onFirstResult).toHaveBeenCalledWith(false));
        await waitFor(() => expect(onSecondResult).toHaveBeenCalledWith(false));
        await waitFor(() => expect(document.activeElement).toBe(firstTrigger));
    });

    it('safely cancels a confirmation when an alert replaces it', async () => {
        const onConfirmResult = vi.fn();
        render(
            <DialogProvider>
                <ConfirmThenAlertLauncher onConfirmResult={onConfirmResult} />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open-destructive' }));
        await screen.findByText('destructive confirmation');
        fireEvent.click(screen.getByRole('button', { name: 'open-notice' }));

        expect(await screen.findByText('backend notice')).toBeTruthy();
        await waitFor(() => expect(onConfirmResult).toHaveBeenCalledWith(false));
    });

    it('keeps the original focus target when a resolved dialog immediately opens a follow-up', async () => {
        const onFirstResult = vi.fn();
        const onSecondResult = vi.fn();
        render(
            <DialogProvider>
                <FollowUpLauncher onFirstResult={onFirstResult} onSecondResult={onSecondResult} />
            </DialogProvider>,
        );

        const trigger = screen.getByRole('button', { name: 'open-follow-up' });
        trigger.focus();
        fireEvent.click(trigger);
        fireEvent.keyDown(window, { key: 'Escape' });
        await screen.findByText('second follow-up dialog');
        expect(onFirstResult).toHaveBeenCalledWith(false);

        fireEvent.keyDown(window, { key: 'Escape' });
        await waitFor(() => expect(onSecondResult).toHaveBeenCalledWith(false));
        await waitFor(() => expect(document.activeElement).toBe(trigger));
    });

    it('safely cancels a pending dialog when the provider unmounts', async () => {
        const onResult = vi.fn();
        const view = render(
            <DialogProvider>
                <PendingPromptLauncher onResult={onResult} />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open-pending' }));
        await screen.findByText('pending prompt');
        view.unmount();

        await waitFor(() => expect(onResult).toHaveBeenCalledWith(null));
    });

    it('does not confirm a destructive action when the provider unmounts', async () => {
        const onResult = vi.fn();
        const view = render(
            <DialogProvider>
                <PendingConfirmLauncher onResult={onResult} />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open-pending-confirm' }));
        await screen.findByText('pending confirm');
        view.unmount();

        await waitFor(() => expect(onResult).toHaveBeenCalledWith(false));
    });

    it('keeps focus inside the dialog when another control tries to receive it', async () => {
        render(
            <DialogProvider>
                <button type="button">outside</button>
                <ConfirmLauncher />
            </DialogProvider>,
        );

        fireEvent.click(screen.getByRole('button', { name: 'open' }));
        const confirmButton = await screen.findByRole('button', { name: '中止' });
        const outsideButton = screen.getByRole('button', { name: 'outside' });
        outsideButton.focus();

        await waitFor(() => expect(document.activeElement).toBe(confirmButton));
    });

    it('safely cancels requests made outside the dialog provider', async () => {
        const onResult = vi.fn();
        render(<FallbackConfirmLauncher onResult={onResult} />);
        fireEvent.click(screen.getByRole('button', { name: 'open-fallback' }));
        await waitFor(() => expect(onResult).toHaveBeenCalledWith(false));
    });

    it('resolves backend show-confirm events through the custom dialog', async () => {
        const handlers = new Map<string, (payload: unknown) => unknown>();
        eventsOnMock.mockImplementation((event: string, handler: (payload: unknown) => unknown) => {
            handlers.set(event, handler);
            return vi.fn(() => { handlers.delete(event); });
        });
        render(
            <DialogProvider>
                <div>host</div>
            </DialogProvider>,
        );
        const handler = handlers.get(EVENT_SHOW_CONFIRM);
        expect(handler).toBeTruthy();
        const pending = Promise.resolve(handler?.({
            id: 'frontend_confirm_1',
            title: '本地缓存与云端不一致',
            message: '保留本地会把当前文件同步到云端',
            confirmText: '保留本地并同步',
            cancelText: '取消',
        }));
        fireEvent.click(await screen.findByRole('button', { name: '保留本地并同步' }));
        await pending;
        await waitFor(() => expect(resolveFrontendConfirmMock).toHaveBeenCalledWith('frontend_confirm_1', true));
    });
});
