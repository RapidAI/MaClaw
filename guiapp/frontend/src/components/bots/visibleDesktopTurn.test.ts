// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../../../wailsjs/go/main/App', () => ({
    PostDesktopBotCross: vi.fn(async () => ({ posted: false, notes: [] })),
}));

import { desktopBotBubbleBody, handleDesktopBotReport, handleDesktopBotResult } from './DesktopBotWorkspace';
import { desktopBotActivityText, desktopTaskStep, messagesForBot, saveBotMessages } from './desktopBots';
import { takeAssistantStreamToken, visibleDesktopTurn } from './visibleDesktopTurn';

const png = 'aGVsbG8=';

function pending(requestId: string) {
    saveBotMessages('alice', 'bot_1', [
        { id: 'u', role: 'user', content: '看看首页' },
        { id: 'a', role: 'assistant', content: '', pending: true, requestId },
    ]);
}

beforeEach(() => {
    localStorage.clear();
});

describe('desktop-bot single exit', () => {
    it('drops a prose-only finish, including 无变化', () => {
        pending('desktop-bot-silent');
        handleDesktopBotResult({
            request_id: 'desktop-bot-silent',
            session_key: 'alice:bot_1',
            text: '无变化',
            reasoning: '\x01secret',
        });
        const stored = JSON.stringify(messagesForBot('alice', 'bot_1'));
        expect(messagesForBot('alice', 'bot_1').map(item => item.id)).toEqual(['u']);
        expect(stored).not.toContain('无变化');
        expect(stored).not.toContain('\x01');
        expect(stored).not.toContain('后台没有返回结果');
    });

    it('drops a placeholder question that only carries an input type', () => {
        pending('desktop-bot-filler-ask');
        handleDesktopBotResult({
            request_id: 'desktop-bot-filler-ask',
            session_key: 'alice:bot_1',
            text: '页面已打开',
            ask_user_question: '无变化',
            ask_user_input_type: 'text',
        });
        expect(messagesForBot('alice', 'bot_1').map(item => item.id)).toEqual(['u']);
        expect(JSON.stringify(messagesForBot('alice', 'bot_1'))).not.toContain('无变化');
        expect(JSON.stringify(messagesForBot('alice', 'bot_1'))).not.toContain('页面已打开');

        pending('desktop-bot-filler-option');
        handleDesktopBotResult({
            request_id: 'desktop-bot-filler-option',
            session_key: 'alice:bot_1',
            text: '页面已打开',
            ask_user_question: '无变化',
            ask_user_options_json: '["无变化"," "]',
        });
        expect(messagesForBot('alice', 'bot_1').map(item => item.id)).toEqual(['u']);
        expect(JSON.stringify(messagesForBot('alice', 'bot_1'))).not.toContain('无变化');
        expect(JSON.stringify(messagesForBot('alice', 'bot_1'))).not.toContain('页面已打开');

        pending('desktop-bot-filler-choices');
        handleDesktopBotResult({
            request_id: 'desktop-bot-filler-choices',
            session_key: 'alice:bot_1',
            ask_user_question: '无变化',
            ask_user_input_type: 'choice',
            ask_user_options_json: '["继续","停下"]',
        });
        const choices = messagesForBot('alice', 'bot_1').find(item => item.id === 'a');
        expect(choices?.content).toBe('继续\n停下');
        expect(choices?.content).not.toContain('无变化');
        expect(choices?.askUser?.options).toEqual(['继续', '停下']);
        expect(choices?.failed).not.toBe(true);
        expect(choices?.pending).toBe(false);

        pending('desktop-bot-filler-label');
        handleDesktopBotResult({
            request_id: 'desktop-bot-filler-label',
            session_key: 'alice:bot_1',
            ask_user_question: '无变化',
            ask_user_options_json: '["继续","无变化"]',
        });
        const mixed = messagesForBot('alice', 'bot_1').find(item => item.id === 'a');
        expect(mixed?.content).toBe('继续');
        expect(mixed?.content).not.toContain('无变化');
        expect(mixed?.askUser?.options).toEqual(['继续']);

        pending('desktop-bot-filler-choices-failed');
        handleDesktopBotResult({
            request_id: 'desktop-bot-filler-choices-failed',
            session_key: 'alice:bot_1',
            error: 'browser broke',
            ask_user_question: '无变化',
            ask_user_options_json: '["继续","停下"]',
        });
        const failedChoices = messagesForBot('alice', 'bot_1').find(item => item.id === 'a');
        expect(failedChoices?.content).toBe('browser broke');
        expect(failedChoices?.failed).toBe(true);
        expect(failedChoices?.askUser?.options).toEqual(['继续', '停下']);
    });

    it('drops a send_to_user that only says nothing changed', () => {
        pending('desktop-bot-filler-send');
        handleDesktopBotResult({
            request_id: 'desktop-bot-filler-send',
            session_key: 'alice:bot_1',
            text: '首页变了',
            send_to_user: '无变化',
        });
        expect(messagesForBot('alice', 'bot_1').map(item => item.id)).toEqual(['u']);
        expect(visibleDesktopTurn({ sendToUser: '(无输出)', prose: '首页变了' }).bubble).toBe(false);
        expect(visibleDesktopTurn({ sendToUser: 'No Change' }).bubble).toBe(false);
        expect(visibleDesktopTurn({ error: 'run run_1 ended as cancelled without a result' }).bubble).toBe(false);
        const wrapped = visibleDesktopTurn({ error: 'certificate failed: run run_1 ended as cancelled without a result' });
        expect(wrapped.bubble).toBe(true);
        expect(wrapped.failed).toBe(true);
        expect(wrapped.sentence).toContain('certificate failed');
    });

    it('keeps a question when send_to_user is empty and leaves the prose off the bubble', () => {
        pending('desktop-bot-ask');
        handleDesktopBotResult({
            request_id: 'desktop-bot-ask',
            session_key: 'alice:bot_1',
            text: '页面已打开',
            ask_user_question: '下一步？',
        });
        const reply = messagesForBot('alice', 'bot_1').find(item => item.id === 'a');
        expect(reply?.content).toBe('下一步？');
        expect(reply?.content).not.toContain('页面已打开');
        expect(reply?.askUser?.question).toBe('下一步？');
        expect(reply?.pending).toBe(false);

        pending('desktop-bot-secret');
        handleDesktopBotResult({
            request_id: 'desktop-bot-secret',
            session_key: 'alice:bot_1',
            text: '安排：先看首页。',
            ask_user_input_type: 'secret',
            ask_user_secret_name: 'SITE_PASSWORD',
            ask_user_question: '保存登录密码',
        });
        const secret = messagesForBot('alice', 'bot_1').find(item => item.id === 'a');
        expect(secret?.content).toBe('保存登录密码');
        expect(secret?.content).not.toContain('先看首页');
        expect(secret?.askUser?.secretName).toBe('SITE_PASSWORD');

        pending('desktop-bot-filler-secret');
        handleDesktopBotResult({
            request_id: 'desktop-bot-filler-secret',
            session_key: 'alice:bot_1',
            text: '安排：先看首页。',
            ask_user_input_type: 'text',
            ask_user_secret_name: 'SITE_PASSWORD',
            ask_user_question: '无变化',
        });
        const fillerSecret = messagesForBot('alice', 'bot_1').find(item => item.id === 'a');
        expect(fillerSecret?.content).toBe('SITE_PASSWORD');
        expect(fillerSecret?.content).not.toContain('无变化');
        expect(fillerSecret?.content).not.toContain('先看首页');
        expect(fillerSecret?.failed).not.toBe(true);
        expect(fillerSecret?.askUser?.secretName).toBe('SITE_PASSWORD');

        pending('desktop-bot-filler-secret-choices');
        handleDesktopBotResult({
            request_id: 'desktop-bot-filler-secret-choices',
            session_key: 'alice:bot_1',
            ask_user_question: '无变化',
            ask_user_secret_name: 'SITE_PASSWORD',
            ask_user_options_json: '["继续","停下"]',
        });
        const secretChoices = messagesForBot('alice', 'bot_1').find(item => item.id === 'a');
        expect(secretChoices?.content).toBe('继续\n停下');
        expect(secretChoices?.content).not.toContain('SITE_PASSWORD');
        expect(secretChoices?.content).not.toContain('无变化');

        pending('desktop-bot-failed-filler-secret');
        handleDesktopBotResult({
            request_id: 'desktop-bot-failed-filler-secret',
            session_key: 'alice:bot_1',
            text: '安装过程写在这里',
            error: 'browser broke',
            ask_user_question: '无变化',
            ask_user_secret_name: 'SITE_PASSWORD',
        });
        const failedSecret = messagesForBot('alice', 'bot_1').find(item => item.id === 'a');
        expect(failedSecret?.content).toBe('browser broke');
        expect(failedSecret?.failed).toBe(true);
        expect(failedSecret?.content).not.toContain('SITE_PASSWORD');
        expect(failedSecret?.content).not.toContain('无变化');
        expect(failedSecret?.content).not.toContain('安装过程');

        handleDesktopBotReport({
            request_id: 'desktop-bot-deliver-ask',
            session_key: 'alice:bot_1',
            delivery_report: true,
            text: '页面已打开',
            ask_user_question: '下一步？',
        });
        const delivered = messagesForBot('alice', 'bot_1').find(item => item.requestId === 'desktop-bot-deliver-ask');
        expect(delivered?.content).toBe('下一步？');
        expect(JSON.stringify(delivered)).not.toContain('页面已打开');

        handleDesktopBotReport({
            request_id: 'desktop-bot-deliver-choice',
            session_key: 'alice:bot_1',
            delivery_report: true,
            phase: 'execute',
            ask_user_options_json: '["继续","停下"]',
        });
        const deliveredChoices = messagesForBot('alice', 'bot_1').find(item => item.requestId === 'desktop-bot-deliver-choice');
        expect(deliveredChoices?.phase).toBe('execute');
        expect(deliveredChoices?.content).toBe('继续\n停下');
        expect(deliveredChoices?.askUser?.options).toEqual(['继续', '停下']);

        handleDesktopBotReport({
            request_id: 'desktop-bot-deliver-phase',
            session_key: 'alice:bot_1',
            delivery_report: true,
            phase: 'later',
            ask_user_question: '下一步？',
        });
        expect(messagesForBot('alice', 'bot_1').find(item => item.requestId === 'desktop-bot-deliver-phase')?.phase).toBeUndefined();

        pending('desktop-bot-failed-ask');
        handleDesktopBotResult({
            request_id: 'desktop-bot-failed-ask',
            session_key: 'alice:bot_1',
            text: '安装过程写在这里',
            error: 'browser broke',
            ask_user_question: '请输入密码。',
            ask_user_secret_name: 'SITE_PASSWORD',
        });
        const failedAsk = messagesForBot('alice', 'bot_1').find(item => item.id === 'a');
        expect(failedAsk?.content).toBe('请输入密码。');
        expect(failedAsk?.failed).toBe(true);
        expect(failedAsk?.content).not.toContain('安装过程');
        expect(failedAsk?.content).not.toContain('browser broke');
        expect(failedAsk?.askUser?.secretName).toBe('SITE_PASSWORD');
    });

    it('shows a failed question as written when it mentions a timeout', () => {
        const question = '页面超时了，要我重试吗？';
        expect(desktopBotBubbleBody(question, true, question, 'zh-Hans')).toBe(question);
        expect(desktopBotBubbleBody('服务器不可达，换一个地址？', true, '服务器不可达，换一个地址？', 'zh-Hans')).toBe('服务器不可达，换一个地址？');
        expect(desktopBotBubbleBody('context deadline exceeded', true, '', 'zh-Hans')).toBe('这个任务等了太久，我这边超时了。稍后再发一次试试。');
        expect(desktopBotBubbleBody('browser broke', true, '请输入密码。', 'zh-Hans')).toBe('browser broke');
    });

    it('shows only the send_to_user sentence', () => {
        pending('desktop-bot-said');
        handleDesktopBotResult({
            request_id: 'desktop-bot-said',
            session_key: 'alice:bot_1',
            text: '无变化',
            reasoning: '\x01secret',
            send_to_user: '首页已打开。',
        });
        const reply = messagesForBot('alice', 'bot_1').find(item => item.id === 'a');
        expect(reply?.content).toBe('首页已打开。');
        expect(reply?.content).not.toContain('无变化');
        expect(JSON.stringify(reply)).not.toContain('\x01');
        expect(reply && 'reasoning' in reply).toBe(false);
    });

    it('does not invent a filler or a task name when send_to_user is missing', () => {
        handleDesktopBotReport({
            request_id: 'desktop-bot-deliver-empty',
            session_key: 'alice:bot_1',
            delivery_report: true,
            task: '截个图',
            text: '',
            error: '后台没有返回结果。',
        });
        const stored = JSON.stringify(messagesForBot('alice', 'bot_1'));
        expect(messagesForBot('alice', 'bot_1')).toEqual([]);
        expect(stored).not.toContain('后台没有返回结果');
        expect(stored).not.toContain('截个图');
    });

    it('keeps a file and a screenshot on the send_to_user sentence', () => {
        pending('desktop-bot-files');
        handleDesktopBotResult({
            request_id: 'desktop-bot-files',
            session_key: 'alice:bot_1',
            text: '过程记录',
            send_to_user: '文件和截图都在。',
            desktop_images: [{ mime: 'image/png', data: png }],
            desktop_files: [{ name: 'note.docx', mime: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', data: png }],
        });
        const reply = messagesForBot('alice', 'bot_1').find(item => item.id === 'a');
        expect(reply?.content).toBe('文件和截图都在。');
        expect(reply?.images).toEqual([{ mime: 'image/png', data: png }]);
        expect(reply?.files?.[0]?.name).toBe('note.docx');
        expect(reply?.content).not.toContain('过程记录');
    });

    it('does not stream desktop-bot prose or thinking into a panel', () => {
        let panel = '';
        const accept = (requestId: string, delta: string) => {
            const taken = takeAssistantStreamToken(requestId, delta);
            if (taken == null) return;
            panel += taken.text;
        };
        accept('desktop-bot-stream', '先打开网站');
        accept('desktop-bot-stream', '\x01secret');
        expect(takeAssistantStreamToken('desktop-bot-stream', '先打开网站')).toBeNull();
        expect(takeAssistantStreamToken('desktop-bot-stream', '\x01secret')).toBeNull();
        expect(panel).toBe('');
        expect(messagesForBot('alice', 'bot_1')).toEqual([]);
        accept('assistant-1', 'hello');
        expect(panel).toBe('hello');
        expect(takeAssistantStreamToken('assistant-1', '\x01plan')).toEqual({ text: '\x01plan', thinking: true });
    });

    it('does not make planning prose an arrange button', () => {
        expect(desktopTaskStep('开始吧', { content: '先打开网站，再告诉你结果。' })).toBe('done');
        const view = visibleDesktopTurn({
            prose: '先打开网站，再告诉你结果。',
            thinking: '\x01secret',
            waiting: true,
        });
        expect(view.arrange).toBe(false);
        expect(view.bubble).toBe(false);
        expect(view.sentence).toBe('');
        expect(view.notify).toBe(false);
    });

    it('keeps a closed working-row phrase off the chat and off a notification', () => {
        pending('desktop-bot-step');
        const before = messagesForBot('alice', 'bot_1');
        expect(desktopBotActivityText('正在点击')).toBe('正在点击');
        expect(desktopBotActivityText('\x01secret')).toBe('');
        expect(desktopBotActivityText('先打开网站')).toBe('');
        const view = visibleDesktopTurn({ activity: '正在点击', thinking: '\x01secret', prose: '无变化' });
        expect(view.activity).toBe('正在点击');
        expect(view.notify).toBe(false);
        expect(view.bubble).toBe(false);
        expect(messagesForBot('alice', 'bot_1')).toEqual(before);
    });
});
