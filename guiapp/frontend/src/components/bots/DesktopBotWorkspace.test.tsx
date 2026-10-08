// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

const sendTask = vi.hoisted(() => vi.fn());
const watchBot = vi.hoisted(() => vi.fn(async (_botId?: string): Promise<{ novnc_url: string; user_control: boolean } | undefined> => ({ novnc_url: '', user_control: false })));
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
    WatchDesktopBot: (arg: string) => watchBot(arg),
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
    watchBot.mockReset();
    watchBot.mockResolvedValue({ novnc_url: '', user_control: false });
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
        expect((await screen.findAllByText('Bot 1')).length).toBeGreaterThan(0);
        const list = screen.getByLabelText('Bot');
        expect(list.textContent).toContain('当前用户在 MaClawSrv 上的一个实例，共用云端桌面。');
        expect(screen.getByTestId('desktop-bot-command')).toBeTruthy();

        fireEvent.click(screen.getByText('改名'));
        fireEvent.change(screen.getByLabelText('名称'), { target: { value: '值班' } });
        fireEvent.change(screen.getByLabelText('描述'), { target: { value: '晚上值守' } });
        fireEvent.click(screen.getByText('保存'));
        expect(await screen.findByText('晚上值守')).toBeTruthy();
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
        // The desktop stays on screen after a finished task while the human
        // watches it, so the picture no longer flashes away.
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('view_only=1');
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-bot');

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
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('view_only=1');
    });

    it('replies like a colleague first and posts the result later', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-ack',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '帮我看看北京天气' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        // The first thing that comes back is the instant colleague reply.
        expect(await screen.findByText('收到，我来处理「帮我看看北京天气」，完成后我会在这里告诉你。')).toBeTruthy();
        // The composer is never locked while the task is running.
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '再看看上海' } });
        expect((screen.getByTestId('desktop-bot-send') as HTMLButtonElement).disabled).toBe(false);
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-ack',
            session_key: 'alice:bot_1',
            text: '北京今天晴，25 度。',
        });
        expect(await screen.findByText('北京今天晴，25 度。')).toBeTruthy();
        // The accepted-task reply stays in the history; the result is its own bubble.
        expect(screen.getByText('收到，我来处理「帮我看看北京天气」，完成后我会在这里告诉你。')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('待命');
    });

    it('reports delivery failures the way a colleague would', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockRejectedValue(new Error('bot service is unavailable, contact the administrator'));
        render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('这条没送到——我这边暂时连不上我的服务器。麻烦稍后再发一次。')).toBeTruthy();
        expect(screen.queryByText('bot service is unavailable, contact the administrator')).toBeNull();
    });

    it('grays out takeover while the bot is driving and fullscreens when idle', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        // While the human watches, the hub hold keeps the desktop alive, so the
        // watch poll keeps serving the same live picture.
        const liveDesktop = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1';
        watchBot.mockResolvedValue({ novnc_url: liveDesktop, user_control: false });
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-operating',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-operating',
            session_key: 'alice:bot_1',
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
        });
        expect(await screen.findByTestId('desktop-bot-handoff')).toBeTruthy();
        // While the agent is driving, takeover is greyed out and the reason is
        // written out next to the button.
        const takeover = screen.getByTestId('desktop-bot-takeover');
        expect((takeover as HTMLButtonElement).disabled).toBe(true);
        expect(screen.getByTestId('desktop-bot-ops').textContent).toContain('Bot 正在操作桌面');
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-operating',
            session_key: 'alice:bot_1',
            text: '已打开 example.org',
        });
        const idle = await screen.findByTestId('desktop-bot-takeover');
        expect((idle as HTMLButtonElement).disabled).toBe(false);
        fireEvent.click(idle);
        // Takeover = fullscreen + this person's keyboard on the desktop.
        const stage = await screen.findByTestId('desktop-bot-stage');
        expect(stage.className).toContain('is-fullscreen');
        expect(stage.className).toContain('is-user');
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').not.toContain('view_only=1');
        fireEvent.click(screen.getByTestId('desktop-bot-exit-takeover'));
        expect(screen.getByTestId('desktop-bot-stage').className).not.toContain('is-fullscreen');
        expect(screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '').toContain('view_only=1');
    });

    it('keeps a watched desktop alive by polling the desktop watch', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        // The hold keeps this desktop up for the watcher, so every poll answers
        // with the live picture until the panel is closed.
        const liveDesktop = 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1';
        watchBot.mockResolvedValue({ novnc_url: liveDesktop, user_control: false });
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-watch',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-watch',
            session_key: 'alice:bot_1',
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
        });
        expect(await screen.findByTestId('desktop-bot-handoff')).toBeTruthy();
        await waitFor(() => expect(watchBot).toHaveBeenCalledWith('bot_1'));
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-watch',
            session_key: 'alice:bot_1',
            text: '已打开 example.org',
            desktop_handoff_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
        });
        // The panel stays open and keeps refreshing the hold after the reply.
        await waitFor(() => expect(watchBot.mock.calls.length).toBeGreaterThanOrEqual(1));
        expect(screen.getByTestId('desktop-bot-handoff')).toBeTruthy();
        // Collapsing the panel stops the watch loop.
        fireEvent.click(screen.getByTestId('desktop-bot-stage-close'));
        await waitFor(() => expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull());
        const callsAfterClose = watchBot.mock.calls.length;
        await new Promise(resolve => setTimeout(resolve, 1000));
        expect(watchBot.mock.calls.length).toBe(callsAfterClose);
    });

    it('queues further messages while one task runs and dispatches them in order', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        let counter = 0;
        sendTask.mockImplementation(async (botId: string) => {
            counter += 1;
            return { request_id: `desktop-bot-q${counter}`, deferred: true, session_key: `alice:${botId}` };
        });
        render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('收到，我来处理「打开示例网站」，完成后我会在这里告诉你。')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('正在工作');

        // The bot is still busy: this one is accepted immediately, but the
        // dispatch waits so one bot never runs two turns at once.
        fireEvent.change(screen.getByTestId('desktop-bot-command'), { target: { value: '顺便截一张桌面' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        expect(await screen.findByText('收到，「顺便截一张桌面」记下了。等手头这件做完我就来处理这个。')).toBeTruthy();
        expect(sendTask).toHaveBeenCalledTimes(1);

        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-q1',
            session_key: 'alice:bot_1',
            text: '已打开 example.org',
        });
        expect(await screen.findByText('已打开 example.org')).toBeTruthy();
        await waitFor(() => expect(sendTask).toHaveBeenCalledTimes(2));
        expect(sendTask).toHaveBeenLastCalledWith('bot_1', '顺便截一张桌面');
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-q2',
            session_key: 'alice:bot_1',
            text: '已截图',
        });
        expect(await screen.findByText('已截图')).toBeTruthy();
        expect(screen.getByTestId('desktop-bot-status').textContent).toContain('待命');
    });

    it('keeps a collapsed desktop panel closed until a keyboard handoff', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockImplementation(async (botId: string) => ({
            request_id: 'desktop-bot-collapse',
            deferred: true,
            session_key: `alice:${botId}`,
        }));
        render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        fireEvent.change(await screen.findByTestId('desktop-bot-command'), { target: { value: '打开示例网站' } });
        fireEvent.click(screen.getByTestId('desktop-bot-send'));
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-collapse',
            session_key: 'alice:bot_1',
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
        });
        expect(await screen.findByTestId('desktop-bot-handoff')).toBeTruthy();
        // 收起 is a standing decision: more view traffic stays hidden.
        fireEvent.click(screen.getByTestId('desktop-bot-stage-close'));
        await waitFor(() => expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull());
        responseListeners.byEvent['desktop-bot-view']?.({
            request_id: 'desktop-bot-collapse',
            session_key: 'alice:bot_1',
            novnc_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
        });
        expect(screen.queryByTestId('desktop-bot-handoff')).toBeNull();
        // A keyboard handoff is an attention request and reopens the panel.
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-collapse',
            session_key: 'alice:bot_1',
            text: '需要你登录',
            desktop_handoff_url: 'http://hub.example/api/v1/desktop-handoff/abc/vnc.html?autoconnect=1',
            desktop_user_control: true,
        });
        const stage = await screen.findByTestId('desktop-bot-stage');
        expect(stage.className).toContain('is-user');
        const src = screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '';
        expect(src).toContain('view_only=');
        expect(src).not.toContain('view_only=1');
    });

    it('re-dispatches accepted tasks that survived a page reload', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        sendTask.mockResolvedValue({ request_id: 'desktop-bot-recover', deferred: true, session_key: 'alice:bot_1' });
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: {
                bot_1: [
                    { id: 'm-rec1', role: 'user', content: '打开示例网站', queued: true },
                    { id: 'm-rec2', role: 'assistant', content: '收到，「打开示例网站」记下了。等手头这件做完我就来处理这个。', ack: true },
                ],
            },
        }));
        render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        // No user action needed: an accepted-but-unsent task dispatches on its
        // own when the page comes back.
        await waitFor(() => expect(sendTask).toHaveBeenCalledWith('bot_1', '打开示例网站'));
        // The queue marker clears so a later reload cannot send it twice.
        const stored = JSON.parse(localStorage.getItem('maclaw.desktopBotMessages.v1') || '{}') as Record<string, Record<string, Array<{ id: string; queued?: boolean }>>>;
        const accepted = (stored.alice.bot_1 || []).find(item => item.id === 'm-rec1');
        expect(accepted?.queued).toBe(false);
        responseListeners.byEvent['ai-assistant-response']?.({
            request_id: 'desktop-bot-recover',
            session_key: 'alice:bot_1',
            text: '已打开 example.org',
        });
        // The recovery dispatch happened without selecting the bot; opening its
        // chat shows the recorded acceptance and the settled result.
        fireEvent.click(await screen.findByText('值班'));
        expect(await screen.findByText('已打开 example.org')).toBeTruthy();
        expect(screen.getByText('收到，「打开示例网站」记下了。等手头这件做完我就来处理这个。')).toBeTruthy();
    });

    it('clears local history when a bot is deleted', async () => {
        listBots.mockResolvedValue([{ id: 'bot_1', title: '值班', description: '共用桌面', instance_id: 'inst_1' }]);
        localStorage.setItem('maclaw.desktopBotMessages.v1', JSON.stringify({
            alice: { bot_1: [{ id: 'm-1', role: 'user', content: '旧任务' }] },
        }));
        render(<DesktopBotWorkspace lang="zh-Hans" userId="alice" />);
        fireEvent.click(await screen.findByText('值班'));
        const row = screen.getByText('共用桌面').closest('li');
        const removeButton = row?.querySelector('button[data-testid^="desktop-bot-delete-"]') as HTMLButtonElement;
        fireEvent.click(removeButton);
        fireEvent.click(removeButton);
        expect(await screen.findByTestId('desktop-bot-empty')).toBeTruthy();
        const stored = JSON.parse(localStorage.getItem('maclaw.desktopBotMessages.v1') || '{}') as Record<string, Record<string, unknown[]>>;
        expect((stored.alice?.bot_1 || []).length).toBe(0);
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
        // The timeout wording differs from the unreachable wording so the
        // colleague excuse matches what actually happened.
        expect(await screen.findByText('这个任务等了太久，我这边超时了。稍后再发一次试试。')).toBeTruthy();
        expect(screen.queryByText('MaClawSrv 没有在时限内返回结果')).toBeNull();
        const src = screen.getByTestId('desktop-bot-handoff').getAttribute('src') || '';
        expect(src).toContain('view_only=1');
        expect(screen.getByTestId('desktop-bot-stage').className).toContain('is-bot');
        expect(screen.getByTestId('desktop-bot-stage').textContent).toContain('网站登录还在这个浏览器里');
        expect(screen.queryByTestId('desktop-bot-return-control')).toBeNull();
    });
});
