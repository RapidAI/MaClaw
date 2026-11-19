// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { handleDesktopBotResult } from './desktopBotResultStore';
import { saveBotMessages, messagesForBot, type DesktopBotMessage } from './desktopBots';

const speak = vi.hoisted(() => ({ fn: vi.fn((..._args: unknown[]) => Promise.resolve()) }));

vi.mock('../../../wailsjs/go/main/App', () => ({
    SpeakDesktopBotReply: (...args: unknown[]) => speak.fn(...args),
    PostDesktopBotCross: () => Promise.resolve({}),
}));

vi.mock('../../../wailsjs/runtime', () => ({
    EventsOn: () => () => {},
    EventsOff: () => {},
}));

function pendingReply(requestId: string): DesktopBotMessage {
    return { id: 'm1', role: 'assistant', content: '', pending: true, requestId } as DesktopBotMessage;
}

function resultPayload(requestId: string, sendToUser: string) {
    return {
        request_id: requestId,
        session_key: 'alice:bot_1',
        send_to_user: sendToUser,
        text: sendToUser,
        run_id: 'run_1',
    };
}

beforeEach(() => {
    localStorage.clear();
    speak.fn.mockClear();
});

describe('desktop bot voice readback', () => {
    it('speaks the exact bubble text once the reply settles', () => {
        saveBotMessages('alice', 'bot_1', [pendingReply('desktop-bot-r1')]);
        handleDesktopBotResult(resultPayload('desktop-bot-r1', '做好了。'));
        expect(messagesForBot('alice', 'bot_1')[0]?.content).toBe('做好了。');
        expect(speak.fn).toHaveBeenCalledTimes(1);
        expect(speak.fn).toHaveBeenCalledWith('做好了。');
    });

    it('does not speak when the result drops without a bubble', () => {
        saveBotMessages('alice', 'bot_1', [pendingReply('desktop-bot-r2')]);
        handleDesktopBotResult(resultPayload('desktop-bot-r2', ''));
        expect(speak.fn).not.toHaveBeenCalled();
    });

    it('speaks the ask question and options when the bubble stays empty', () => {
        saveBotMessages('alice', 'bot_1', [pendingReply('desktop-bot-r3')]);
        handleDesktopBotResult({
            request_id: 'desktop-bot-r3',
            session_key: 'alice:bot_1',
            send_to_user: '',
            text: '',
            run_id: 'run_3',
            ask_user_question: '选哪个平台发布？',
            ask_user_options_json: '["小红书","B站"]',
        });
        expect(speak.fn).toHaveBeenCalledTimes(1);
        expect(speak.fn).toHaveBeenCalledWith('选哪个平台发布？\n小红书\nB站');
    });
});
