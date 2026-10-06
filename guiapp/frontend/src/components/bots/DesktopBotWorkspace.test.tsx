// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

const sendTask = vi.hoisted(() => vi.fn());
const listBots = vi.hoisted(() => vi.fn(async (): Promise<Array<{ id: string; title: string; description: string; instance_id: string }>> => []));
const createBot = vi.hoisted(() => vi.fn(async (name: string, description: string) => ({ id: 'bot_1', title: name, description, instance_id: '' })));
const renameBot = vi.hoisted(() => vi.fn(async (_id: string, _name: string, _description: string) => undefined));
const deleteBot = vi.hoisted(() => vi.fn(async (_id: string) => undefined));
const responseListeners = vi.hoisted(() => ({
    list: [] as Array<(payload: unknown) => void>,
    byEvent: {} as Record<string, (payload: unknown) => void>,
}));

vi.mock('../../../wailsjs/go/main/App', () => ({
    SendDesktopBotTask: (...args: unknown[]) => sendTask(...args),
    ListDesktopBots: () => listBots(),
    CreateDesktopBot: (name: string, description: string) => createBot(name, description),
    RenameDesktopBot: (id: string, name: string, description: string) => renameBot(id, name, description),
    DeleteDesktopBot: (id: string) => deleteBot(id),
}));

vi.mock('../../../wailsjs/runtime', () => ({
    EventsOn: (event: string, handler: (payload: unknown) => void) => {
        responseListeners.list.push(handler);
        responseListeners.byEvent[event] = handler;
        return () => {
            responseListeners.list = responseListeners.list.filter(item => item !== handler);
            if (responseListeners.byEvent[event] === handler) delete responseListeners.byEvent[event];
        };
    },
}));

import { DesktopBotWorkspace, handleDesktopBotResult, handleDesktopBotView } from './DesktopBotWorkspace';

beforeEach(() => {
    localStorage.clear();
    sendTask.mockReset();
    listBots.mockReset();
    listBots.mockResolvedValue([]);
    createBot.mockReset();
    createBot.mockImplementation(async (name: string, description: string) => ({ id: 'bot_1', title: name, description, instance_id: '' }));
    renameBot.mockReset();
    renameBot.mockResolvedValue(undefined);
    deleteBot.mockReset();
    deleteBot.mockResolvedValue(undefined);
    responseListeners.list = [];
    responseListeners.byEvent = {};
});

describe('DesktopBotWorkspace', () => {
    it('lets the same bot continue after a reply never came back', async () => {
        const started = Date.now() - 10 * 60 * 1000;
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: {
                bot_1: [{
                    id: `m-${started.toString(36)}-old`,
                    role: 'assistant',
                    content: '',
                    pending: true,
                    handoffUrl: 'https://hub.example/api/v1/desktop-handoff/token/vnc.html?autoconnect=1',
                    userControl: true,
                }],
            },
        }));
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-2', deferred: true, session_key: 'alice:bot_1' });
        render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        expect((await screen.findByTestId('desktop-bot-handoff')).getAttribute('src')).toContain('view_only=');
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src')).not.toContain('view_only=1');
        const back = await screen.findByTestId('desktop-bot-return-control');
        fireEvent.click(back);
        expect(sendTask).toHaveBeenCalledWith('bot_1', '登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。');
        expect((await screen.findByTestId('desktop-bot-handoff')).getAttribute('src')).toContain('view_only=1');
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-2',
            session_key: 'alice:bot_1',
            text: '已在这个浏览器里继续',
        });
        expect(await screen.findByText('已在这个浏览器里继续')).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
    });

    it('keeps a late earlier reply off the command still using the browser', async () => {
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: {
                bot_1: [{
                    id: 'm-old',
                    role: 'assistant',
                    content: '请在这个浏览器里登录',
                    pending: false,
                    requestId: 'desktop-bot-1',
                }],
            },
        }));
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '晚上', instance_id: 'inst_1' }]);
        let release: (value: unknown) => void = () => {};
        sendTask.mockImplementation(() => new Promise(resolve => { release = resolve; }));
        render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '继续操作' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('继续操作')).toBeTruthy();
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-1',
            session_key: 'alice:bot_1',
            text: '迟到的旧结果',
            desktop_handoff_url: 'https://hub.example/api/v1/desktop-handoff/token/vnc.html?autoconnect=1',
            desktop_user_control: true,
        });
        expect(screen.queryByText('迟到的旧结果')).toBeNull();
        expect(screen.getByText('请在这个浏览器里登录')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('正在工作');
        release({ request_id: 'desktop-bot-2', deferred: true, session_key: 'alice:bot_1' });
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-2',
            session_key: 'alice:bot_1',
            text: '已在这个浏览器里继续',
        });
        expect(await screen.findByText('已在这个浏览器里继续')).toBeTruthy();
        expect(screen.getByText('请在这个浏览器里登录')).toBeTruthy();
    });

    it('lists a bot with its description and opens the chat on the right', async () => {
        render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);

        expect(screen.getByText('每个 Bot 都是你在 MaClawSrv 上的一个实例，共用你的云端桌面。')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-pick').textContent).toContain('选择一个 Bot');

        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        expect(await screen.findByText('Bot 1')).toBeTruthy();
        const list = screen.getByLabelText('Bot');
        expect(list.textContent).toContain('当前用户在 MaClawSrv 上的一个实例，共用云端桌面。');
        expect(screen.getByTestId('desktop-bot-command')).toBeTruthy();

        fireEvent.click(screen.getByText('改名'));
        fireEvent.change(screen.getByLabelText('名称'), { target: { value: '值班' } });
        fireEvent.change(screen.getByLabelText('描述'), { target: { value: '晚上值守' } });
        fireEvent.click(screen.getByText('保存'));
        expect(await screen.findByText('值班')).toBeTruthy();
        expect(list.textContent).toContain('值班');
        expect(list.textContent).toContain('晚上值守');

        const row = screen.getByText('晚上值守').closest('li');
        const remove = row?.querySelector('button[data-testid^="desktop-bot-delete-"]') as HTMLButtonElement;
        fireEvent.click(remove);
        fireEvent.click(remove);
        expect(await screen.findByTestId('desktop-bot-empty')).toBeTruthy();
        expect(screen.queryByText('晚上值守')).toBeNull();
        expect(screen.getByTestId('desktop-bot-pick')).toBeTruthy();
    });

    it('sends a command to the backend agent and shows the returned result', async () => {
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'req-1',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        expect(await screen.findByTestId('desktop-bot-command')).toBeTruthy();

        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));

        expect(await screen.findByText('打开示例网站')).toBeTruthy();
        expect(sendTask).toHaveBeenCalledWith(expect.any(String), '打开示例网站');
        expect(screen.getByTestId('desktop-bot-log').textContent).not.toContain('正在工作');
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('正在工作');

        const botId = String(sendTask.mock.calls[0][0]);
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'req-1',
            session_key: `alice:${botId}`,
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
        });
        expect((await screen.findByTestId('desktop-bot-handoff')).getAttribute('src')).toContain('view_only=1');
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-bot');
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('正在工作');
        expect(screen.queryByTestId('desktop-bot-return-control')).toBeNull();

        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'req-1',
            session_key: `alice:${botId}`,
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            user_control: true,
        });
        await waitFor(() => {
            const loginSrc = screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '';
            expect(loginSrc).toContain('view_only=');
            expect(loginSrc).not.toContain('view_only=1');
        });
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-user');
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('正在工作');
        expect(screen.queryByTestId('desktop-bot-return-control')).toBeNull();

        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'req-1',
            session_key: `alice:${botId}`,
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            user_control: false,
        });
        await waitFor(() => {
            expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src')).toContain('view_only=1');
        });
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-bot');
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('正在工作');

        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'req-1',
            session_key: `alice:${botId}`,
            text: '已打开 example.org',
            desktop_handoff_url: 'http://dockerd.example/vnc.html?autoconnect=1',
            desktop_user_control: true,
        });

        expect(await screen.findByText('已打开 example.org')).toBeTruthy();
        const userSrc = screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '';
        expect(userSrc).toContain('vnc.html');
        expect(userSrc).toContain('view_only=');
        expect(userSrc).not.toContain('view_only=1');
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-user');
        fireEvent.click(screen.getByTestId('desktop-bot-return-control'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith(botId, '登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。'));
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-bot');
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src')).toContain('view_only=1');
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('正在工作');
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'req-1',
            session_key: `alice:${botId}`,
            text: '已继续操作',
            desktop_handoff_url: 'http://dockerd.example/vnc.html?autoconnect=1',
        });
        expect(await screen.findByText('已继续操作')).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-return-control')).toBeNull();
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();

        sendTask.mockImplementationOnce(async () => ({ request_id: 'req-2', deferred: true }));
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '继续' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(3));
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'req-2',
            session_key: `alice:${botId}`,
            text: '已继续',
        });
        expect(await screen.findByText('已继续')).toBeTruthy();
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
    });

    it('shows the Hub error when the bot list cannot be loaded', async () => {
        listBots.mockRejectedValueOnce(new Error('MaClawSrv 不可达'));
        render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect((await screen.findByTestId('desktop-bot-list-error')).textContent).toBe('MaClawSrv 不可达');
        expect(screen.queryByTestId('desktop-bot-empty')).toBeNull();
    });

    it('keeps the bot when renaming fails', async () => {
        listBots.mockResolvedValueOnce([{ id: 'bot_1', title: 'Bot 1', description: '值班', instance_id: 'inst_1' }]);
        renameBot.mockRejectedValueOnce(new Error('MaClawSrv 不可达'));
        render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        expect(await screen.findByText('Bot 1')).toBeTruthy();
        fireEvent.click(screen.getByText('改名'));
        fireEvent.change(screen.getByLabelText('名称'), { target: { value: '新名字' } });
        fireEvent.click(screen.getByText('保存'));
        expect((await screen.findByTestId('desktop-bot-list-error')).textContent).toBe('MaClawSrv 不可达');
        fireEvent.click(screen.getByText('取消'));
        expect(screen.getByText('Bot 1')).toBeTruthy();
        expect(screen.queryByText('新名字')).toBeNull();
    });

    it('shows the result that arrived after leaving the bots page', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-1',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        const view = render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('打开示例网站')).toBeTruthy();
        view.unmount();
        handleDesktopBotResult({
            request_id: 'desktop-bot-1',
            session_key: 'alice:bot_1',
            text: '已打开 example.org',
        });
        render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        expect(await screen.findByText('已打开 example.org')).toBeTruthy();
        expect(screen.queryByText('正在工作')).toBeNull();
    });

    it('shows the login desktop again after leaving the bots page', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-2',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        const view = render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('打开示例网站')).toBeTruthy();
        view.unmount();
        handleDesktopBotView({
            request_id: 'desktop-bot-2',
            session_key: 'alice:bot_1',
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            user_control: true,
        });
        render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        const loginSrc = (await screen.findByTestId('desktop-bot-handoff')).getAttribute('src') || '';
        expect(loginSrc).toContain('view_only=');
        expect(loginSrc).not.toContain('view_only=1');
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-user');
        expect(screen.queryByTestId('desktop-bot-return-control')).toBeNull();
    });

    it('shows a result that arrives after the bot page is open again', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-3',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        const view = render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('打开示例网站')).toBeTruthy();
        view.unmount();
        render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        expect(await screen.findByText('打开示例网站')).toBeTruthy();
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-3',
            session_key: 'alice:bot_1',
            text: '已打开 example.org',
            desktop_handoff_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            desktop_user_control: true,
        });
        expect(await screen.findByText('已打开 example.org')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-return-control')).toBeTruthy();
    });

    it('shows the Hub error when a bot cannot be created', async () => {
        createBot.mockRejectedValueOnce(new Error('MaClawSrv 不可达'));
        render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(screen.getByTestId('desktop-bot-create'));
        expect((await screen.findByTestId('desktop-bot-list-error')).textContent).toBe('MaClawSrv 不可达');
        expect(screen.getByTestId('desktop-bot-empty')).toBeTruthy();
    });

    it('keeps the browser on screen when the command times out', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: 'Bot 1', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-4',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('Bot 1'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('打开示例网站')).toBeTruthy();
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-4',
            session_key: 'alice:bot_1',
            error: 'MaClawSrv 没有在时限内返回结果',
            desktop_handoff_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
        });
        expect(await screen.findByText('MaClawSrv 没有在时限内返回结果')).toBeTruthy();
        const src = screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '';
        expect(src).toContain('view_only=1');
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-bot');
        expect(screen.getByTestId('desktop-bot-stage').textContent).toContain('网站登录还在这个浏览器里');
        expect(screen.queryByTestId('desktop-bot-return-control')).toBeNull();
    });
});
