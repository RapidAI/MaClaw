// @vitest-environment jsdom
import { useState } from 'react';
import { beforeEach, describe, expect, it } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { DesktopBotComposer } from './DesktopBotComposer';
import { DESKTOP_BOT_INPUT_HISTORY_KEY, rememberBotInput } from './desktopBotInputHistory';

const ASSISTANT_PROMPT_HISTORY_KEY = 'ai-assistant-prompt-history';

function Harness({ userId = 'alice', botId = 'bot_1', disabled = false }: { userId?: string; botId?: string; disabled?: boolean }) {
    const [value, setValue] = useState('');
    const [sent, setSent] = useState<string[]>([]);
    return (
        <>
            <DesktopBotComposer
                userId={userId}
                botId={botId}
                lang="zh-Hans"
                value={value}
                onChange={setValue}
                onSubmit={() => {
                    setSent(prev => [...prev, value.trim()]);
                    setValue('');
                }}
                disabled={disabled}
                placeholder="给 值班 发消息"
                ariaLabel="给 值班 发消息"
                sendLabel="发送"
            />
            <div data-testid="sent">{sent.join('|')}</div>
        </>
    );
}

function input(): HTMLTextAreaElement {
    return screen.getByTestId('desktop-bot-command') as HTMLTextAreaElement;
}

function typeAndSend(text: string) {
    fireEvent.change(input(), { target: { value: text } });
    fireEvent.click(screen.getByTestId('desktop-bot-send'));
}

beforeEach(() => {
    localStorage.clear();
});

describe('DesktopBotComposer', () => {
    it('walks sent commands with the arrow keys and restores the draft on Escape', async () => {
        render(<Harness />);
        typeAndSend('打开示例网站');
        typeAndSend('再截一张桌面');
        expect(screen.getByTestId('sent').textContent).toBe('打开示例网站|再截一张桌面');
        expect(input().value).toBe('');

        fireEvent.change(input(), { target: { value: '草稿' } });
        input().setSelectionRange(2, 2);
        fireEvent.keyDown(input(), { key: 'ArrowUp' });
        await waitFor(() => expect(input().value).toBe('再截一张桌面'));
        input().setSelectionRange(input().value.length, input().value.length);
        fireEvent.keyDown(input(), { key: 'ArrowUp' });
        await waitFor(() => expect(input().value).toBe('打开示例网站'));
        input().setSelectionRange(input().value.length, input().value.length);
        fireEvent.keyDown(input(), { key: 'ArrowDown' });
        await waitFor(() => expect(input().value).toBe('再截一张桌面'));
        fireEvent.keyDown(input(), { key: 'Escape' });
        await waitFor(() => expect(input().value).toBe('草稿'));
    });

    it('completes a prefix without sending, then sends the completed line', async () => {
        render(<Harness />);
        typeAndSend('打开示例网站');
        fireEvent.change(input(), { target: { value: '打开示' } });
        const list = await screen.findByTestId('desktop-bot-input-history');
        expect(list.textContent).toContain('打开示例网站');
        fireEvent.keyDown(input(), { key: 'Enter' });
        await waitFor(() => expect(input().value).toBe('打开示例网站'));
        expect(screen.getByTestId('sent').textContent).toBe('打开示例网站');
        expect(screen.queryByTestId('desktop-bot-input-history')).toBeNull();
        fireEvent.keyDown(input(), { key: 'Enter' });
        await waitFor(() => expect(screen.getByTestId('sent').textContent).toBe('打开示例网站|打开示例网站'));
    });

    it('does not recall history from the middle of a multiline draft', async () => {
        rememberBotInput('alice', '历史命令');
        render(<Harness />);
        fireEvent.change(input(), { target: { value: '第一行\n第二行' } });
        input().setSelectionRange(input().value.length, input().value.length);
        fireEvent.keyDown(input(), { key: 'ArrowUp' });
        expect(input().value).toBe('第一行\n第二行');
        input().setSelectionRange(0, 0);
        fireEvent.keyDown(input(), { key: 'ArrowUp' });
        await waitFor(() => expect(input().value).toBe('历史命令'));
    });

    it('keeps Shift+Enter and an IME Enter from sending, and still sends on Ctrl+Enter', () => {
        render(<Harness />);
        fireEvent.change(input(), { target: { value: '打开示例网站' } });
        fireEvent.keyDown(input(), { key: 'Enter', shiftKey: true });
        expect(screen.getByTestId('sent').textContent).toBe('');
        fireEvent.keyDown(input(), { key: 'Enter', ctrlKey: true });
        expect(screen.getByTestId('sent').textContent).toBe('打开示例网站');

        fireEvent.change(input(), { target: { value: '下一句' } });
        fireEvent.compositionStart(input());
        fireEvent.keyDown(input(), { key: 'Enter' });
        fireEvent.compositionEnd(input());
        fireEvent.keyDown(input(), { key: 'Enter' });
        expect(screen.getByTestId('sent').textContent).toBe('打开示例网站');
        expect(input().value).toBe('下一句');
    });

    it('does not store a command while sending is blocked', () => {
        localStorage.setItem(ASSISTANT_PROMPT_HISTORY_KEY, JSON.stringify(['助手里的一句']));
        render(<Harness disabled />);
        fireEvent.change(input(), { target: { value: '密码不要进历史' } });
        fireEvent.keyDown(input(), { key: 'Enter' });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(screen.getByTestId('sent').textContent).toBe('');
        expect(input().value).toBe('密码不要进历史');
        expect(localStorage.getItem(DESKTOP_BOT_INPUT_HISTORY_KEY)).toBeNull();
        expect(JSON.parse(localStorage.getItem(ASSISTANT_PROMPT_HISTORY_KEY) || '[]')).toEqual(['助手里的一句']);
    });

    it('uses this account\'s cache and ignores the assistant prompt history', async () => {
        localStorage.setItem(ASSISTANT_PROMPT_HISTORY_KEY, JSON.stringify(['助手里的一句']));
        rememberBotInput('alice', '打开示例网站');
        rememberBotInput('bob', '鲍勃的任务');
        const view = render(<Harness userId="bob" />);
        fireEvent.keyDown(input(), { key: 'ArrowUp' });
        await waitFor(() => expect(input().value).toBe('鲍勃的任务'));
        fireEvent.change(input(), { target: { value: '打开' } });
        expect(screen.queryByTestId('desktop-bot-input-history')).toBeNull();

        view.rerender(<Harness userId="alice" />);
        fireEvent.change(input(), { target: { value: '' } });
        input().setSelectionRange(0, 0);
        fireEvent.keyDown(input(), { key: 'ArrowUp' });
        await waitFor(() => expect(input().value).toBe('打开示例网站'));
        expect(JSON.parse(localStorage.getItem(ASSISTANT_PROMPT_HISTORY_KEY) || '[]')).toEqual(['助手里的一句']);
    });

    it('puts the pre-history draft back when the bot changes', async () => {
        const view = render(<Harness botId="bot_1" />);
        typeAndSend('打开示例网站');
        fireEvent.change(input(), { target: { value: '草稿' } });
        input().setSelectionRange(2, 2);
        fireEvent.keyDown(input(), { key: 'ArrowUp' });
        await waitFor(() => expect(input().value).toBe('打开示例网站'));
        view.rerender(<Harness botId="bot_2" />);
        await waitFor(() => expect(input().value).toBe('草稿'));
    });
});
