// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';
import { addDesktopBot, appendDesktopBotReport, botsForUser, botTaskText, botTranscriptGeneration, clearLiveBotTurn, confirmedWorkOrder, currentInProgressBotTasks, deleteDesktopBot, desktopBotAccountId, desktopBotActivityText, desktopBotAdmittingSentence, desktopBotAskingLabel, desktopBotCrossTargetText, desktopBotDestructive, desktopBotFiles, desktopBotImages, desktopBotLocalControl, desktopBotLocalPaths, desktopBotNoticeSeen, desktopBotRunningSentence, desktopBotStatusUnreachableSentence, desktopBotStopFailedSentence, desktopBotStopIdleSentence, desktopBotPauseAskSentence, desktopBotStillStoppingSentence, desktopBotStopIntent, desktopBotStoppedSentence, desktopFalseStopClaim, desktopReplyClaimsStopped, desktopBotTaskHint, desktopBotTypedConfirmSentence, desktopBotWorkingLabel, desktopCreditReadout, desktopTaskStep, inProgressBotTasks, markDesktopBotNotice, messageNeedsComputerTakeover, messagesForBot, namesLaterTime, noteLiveBotTurn, renameDesktopBot, saveBotMessages, settlePendingBotReply, userRequestBefore, BOT_PENDING_REPLY_WAIT_MS } from './desktopBots';

beforeEach(() => {
    localStorage.clear();
});

describe('desktop bots', () => {
    it('turns an approved arrangement into the execute order', () => {
        const plan = '下一轮就去跑 apt get install evince，把 PDF 阅读器装上。';
        expect(confirmedWorkOrder(plan)).toBe(plan);
        expect(confirmedWorkOrder(`  ${plan}  `)).toBe(plan);
        expect(confirmedWorkOrder(plan)).not.toContain('现在就执行下面这项安排');
        expect(confirmedWorkOrder('  ')).toBe('');
        expect(namesLaterTime(plan)).toBe(false);
        expect(namesLaterTime('安装一个pdf阅读器')).toBe(false);
        expect(namesLaterTime('每天凌晨删掉下载目录')).toBe(true);
        expect(namesLaterTime('每个工作日早上 8:30 做简报')).toBe(true);
    });

    it('keeps each user on a separate bot list and desktop identity', () => {
        addDesktopBot('alice', 1);
        addDesktopBot('alice', 2);
        addDesktopBot('bob', 3);

        expect(botsForUser('alice').map(bot => bot.title)).toEqual(['Bot 1', 'Bot 2']);
        expect(botsForUser('bob').map(bot => bot.title)).toEqual(['Bot 1']);
        expect(botsForUser('alice').every(bot => bot.id.startsWith('bot-'))).toBe(true);
    });

    it('renames and deletes only the selected bot', () => {
        const first = addDesktopBot('alice', 1);
        const second = addDesktopBot('alice', 2);

        renameDesktopBot('alice', first.id, '  值班  ', '晚上值守');
        expect(botsForUser('alice').map(bot => bot.title)).toEqual(['值班', 'Bot 2']);
        expect(botsForUser('alice')[0].description).toBe('晚上值守');

        deleteDesktopBot('alice', second.id);
        expect(botsForUser('alice').map(bot => bot.id)).toEqual([first.id]);
        expect(botsForUser('bob')).toEqual([]);
    });

    it('writes a result onto the request that produced it', () => {
        saveBotMessages('alice', 'bot_1', [
            { id: 'a', role: 'assistant', content: '', pending: true, requestId: 'desktop-bot-1' },
            { id: 'b', role: 'assistant', content: '', pending: true, requestId: 'desktop-bot-2' },
        ]);
        settlePendingBotReply('alice', 'bot_1', { content: '第一句', failed: false, requestId: 'desktop-bot-1' });
        const items = messagesForBot('alice', 'bot_1');
        expect(items[0].content).toBe('第一句');
        expect(items[0].pending).toBe(false);
        expect(items[1].pending).toBe(true);
        expect(items[1].content).toBe('');
    });

    it('leaves the next command alone when an earlier reply arrives late', () => {
        saveBotMessages('alice', 'bot_1', [
            { id: 'a', role: 'assistant', content: '请在这个浏览器里登录', pending: false, requestId: 'desktop-bot-1' },
            { id: 'b', role: 'assistant', content: '', pending: true },
        ]);
        settlePendingBotReply('alice', 'bot_1', { content: '迟到的旧结果', failed: false, requestId: 'desktop-bot-1', userControl: true });
        const items = messagesForBot('alice', 'bot_1');
        expect(items[0].content).toBe('请在这个浏览器里登录');
        expect(items[0].userControl).toBeUndefined();
        expect(items[1].pending).toBe(true);
        expect(items[1].content).toBe('');
    });

    it('keeps a screenshot on the reply and drops anything that is not an image', () => {
        saveBotMessages('alice', 'bot_1', [
            { id: 'a', role: 'assistant', content: '', pending: true, requestId: 'desktop-bot-shot' },
        ]);
        settlePendingBotReply('alice', 'bot_1', {
            content: '桌面截图已生成',
            failed: false,
            requestId: 'desktop-bot-shot',
            images: desktopBotImages([
                { mime: 'text/html', data: 'iVBORw0KGgo=' },
                { mime: 'image/png', data: 'iVBORw0KGgo=' },
            ]),
        });
        const shot = messagesForBot('alice', 'bot_1')[0];
        expect(shot.content).toBe('桌面截图已生成');
        expect(shot.images).toEqual([{ mime: 'image/png', data: 'iVBORw0KGgo=' }]);

        saveBotMessages('alice', 'bot_1', [
            { id: 'b', role: 'assistant', content: '', pending: true, requestId: 'desktop-bot-bad' },
        ]);
        settlePendingBotReply('alice', 'bot_1', {
            content: '失败',
            failed: true,
            requestId: 'desktop-bot-bad',
            images: [{ mime: 'image/png', data: 'iVBORw0KGgo=' }],
        });
        expect(messagesForBot('alice', 'bot_1').find(item => item.id === 'b')?.images).toBeUndefined();
    });

    it('hides the confirm step once the requested screenshot is already delivered', () => {
        const messages = [
            { id: 'u', role: 'user' as const, content: '截屏发我' },
            { id: 'ack', role: 'assistant' as const, content: '我先看一下环境，再告诉你打算怎么做。', ack: true },
            { id: 'a', role: 'assistant' as const, content: '已截屏并发送（1440×900）。图片已附在上方，可直接查看。', images: [{ mime: 'image/png' as const, data: 'iVBORw0KGgo=' }] },
        ];
        expect(userRequestBefore(messages, 2)).toBe('截屏发我');
        expect(userRequestBefore([
            { id: 'u2', role: 'user', content: '查询 北京天所，生成pdf' },
            { id: 'c', role: 'assistant', content: '我是你在这台云桌面上的同事。', chat: true },
            { id: 'a2', role: 'assistant', content: '确认后我会整理成 PDF。', phase: 'plan' },
        ], 2)).toBe('查询 北京天所，生成pdf');
        expect(userRequestBefore([
            { id: 'u3', role: 'user', content: '查询 北京天所，生成pdf' },
            { id: 'f', role: 'assistant', content: '这句没整理成可执行的安排，没有发出。', failed: true },
            { id: 'q', role: 'assistant', content: '这份要发给谁？', askUser: { question: '这份要发给谁？' } },
            { id: 'a3', role: 'assistant', content: '确认后我会整理成 PDF。', phase: 'plan' },
        ], 3)).toBe('查询 北京天所，生成pdf');
        expect(userRequestBefore([
            { id: 'u4', role: 'user', content: '查询 北京天所，生成pdf' },
            { id: 'f2', role: 'assistant', content: '这句没发出。', failed: true, phase: 'plan' },
            { id: 'a4', role: 'assistant', content: '确认后我会整理成 PDF。', phase: 'plan' },
        ], 2)).toBe('');
        expect(desktopTaskStep('截屏发我', messages[2])).toBe('done');
        expect(desktopTaskStep('开始吧', { content: '先打开网站，再告诉你结果。' })).toBe('done');
        expect(desktopTaskStep('改成只看首页', { content: '只看首页，然后停。' })).toBe('done');
        expect(desktopTaskStep('生成一份word文档发我', { content: '确认后我会把自述写成文档附在这条对话里。' })).toBe('arrange');
        expect(desktopTaskStep('这个页面打不开', { content: '要我换一种方式再试吗？' })).toBe('ask');
        expect(desktopTaskStep('打开网站', { content: '需要你在这个浏览器里登录。', userControl: true })).toBe('assist');
        expect(desktopTaskStep('生成文档', { content: '文件已附上。', files: [{ name: '自述.docx', mime: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', data: 'd29yZA==' }] })).toBe('done');
        expect(desktopTaskStep('生成文档', { content: '文件已放在本机。', localPaths: ['C:\\Users\\alice\\MaClaw\\bot-files\\自述.docx'] })).toBe('done');
        expect(desktopTaskStep('开发个c++版的hello world程序，运行后发我运行屏幕截图', {
            content: '图片已附在上方，可直接查看。',
            images: [{ mime: 'image/png', data: 'iVBORw0KGgo=' }],
        })).toBe('arrange');
    });

    it('keeps one confirmation when the arrangement follows the reading', () => {
        const ask = '查询 北京天所，生成pdf';
        const reading = '你要「查询 北京天所，生成pdf」。确认后我按这个做，做完把结果发在这里。';
        const arrangement = '确认后我会检索"北京天所"相关资料，整理成 PDF 文件附在这条对话里发给你。';
        saveBotMessages('alice', 'bot_1', [
            { id: 'u', role: 'user', content: ask },
            { id: 'q', role: 'assistant', content: '收到，「查询 北京天所」记下了。等手头这件做完我就来处理这个。', ack: true },
            { id: 'ack', role: 'assistant', content: reading, ack: true, understood: true },
            { id: 'a', role: 'assistant', content: '', pending: true, phase: 'plan', requestId: 'desktop-bot-pdf' },
        ]);
        settlePendingBotReply('alice', 'bot_1', { content: arrangement, failed: false, requestId: 'desktop-bot-pdf' });
        const items = messagesForBot('alice', 'bot_1');
        expect(items.map(item => item.id)).toEqual(['u', 'q', 'a']);
        expect(items[1].content).toContain('收到');
        expect(items[2].content).toBe(arrangement);
        expect(items[2].ack).toBeUndefined();
        expect(items[2].pending).toBe(false);
        expect(desktopTaskStep(ask, items[2])).toBe('arrange');
    });

    it('keeps the reading when the reply is a result, a question, a handoff, or a failure', () => {
        const reading = '你要当前桌面的截屏。我现在截，截到就发在这里。';
        saveBotMessages('alice', 'bot_1', [
            { id: 'u', role: 'user', content: '截屏发我' },
            { id: 'ack', role: 'assistant', content: reading, ack: true, understood: true },
            { id: 'a', role: 'assistant', content: '', pending: true, phase: 'plan', requestId: 'desktop-bot-shot' },
        ]);
        settlePendingBotReply('alice', 'bot_1', {
            content: '已截屏并发送。图片已附在上方，可直接查看。',
            failed: false,
            requestId: 'desktop-bot-shot',
            images: [{ mime: 'image/png', data: 'iVBORw0KGgo=' }],
        });
        expect(messagesForBot('alice', 'bot_1').map(item => item.id)).toEqual(['u', 'ack', 'a']);

        saveBotMessages('alice', 'bot_1', [
            { id: 'u', role: 'user', content: '这个页面打不开' },
            { id: 'ack', role: 'assistant', content: '你要换一种打开方式。', ack: true, understood: true },
            { id: 'a', role: 'assistant', content: '', pending: true, phase: 'plan', requestId: 'desktop-bot-ask' },
        ]);
        settlePendingBotReply('alice', 'bot_1', { content: '要我换一种方式再试吗？', failed: false, requestId: 'desktop-bot-ask' });
        expect(messagesForBot('alice', 'bot_1').find(item => item.id === 'ack')?.ack).toBe(true);

        saveBotMessages('alice', 'bot_1', [
            { id: 'u', role: 'user', content: '打开网站' },
            { id: 'ack', role: 'assistant', content: '你要打开这个网站。', ack: true, understood: true },
            { id: 'a', role: 'assistant', content: '', pending: true, phase: 'plan', requestId: 'desktop-bot-login' },
        ]);
        settlePendingBotReply('alice', 'bot_1', { content: '需要你在这个浏览器里登录。', failed: false, requestId: 'desktop-bot-login', userControl: true });
        expect(messagesForBot('alice', 'bot_1').map(item => item.id)).toEqual(['u', 'ack', 'a']);

        saveBotMessages('alice', 'bot_1', [
            { id: 'u', role: 'user', content: '查询 北京天所，生成pdf' },
            { id: 'ack', role: 'assistant', content: '你要一份 PDF。确认后我按这个做。', ack: true, understood: true },
            { id: 'a', role: 'assistant', content: '', pending: true, phase: 'plan', requestId: 'desktop-bot-fail' },
        ]);
        settlePendingBotReply('alice', 'bot_1', { content: '失败了', failed: true, requestId: 'desktop-bot-fail' });
        const failed = messagesForBot('alice', 'bot_1');
        expect(failed.map(item => item.id)).toEqual(['u', 'ack', 'a']);
        expect(failed[2].failed).toBe(true);

        saveBotMessages('alice', 'bot_1', [
            { id: 'u', role: 'user', content: '安排已确认。请按你上一条安排执行。做完、失败或需要我时，在对话里告诉我。' },
            { id: 'ack', role: 'assistant', content: '我按这个安排做。', ack: true, understood: true },
            { id: 'a', role: 'assistant', content: '', pending: true, phase: 'execute', requestId: 'desktop-bot-run' },
        ]);
        settlePendingBotReply('alice', 'bot_1', { content: '确认后我会把文件附在这里。', failed: false, requestId: 'desktop-bot-run' });
        expect(messagesForBot('alice', 'bot_1').map(item => item.id)).toEqual(['u', 'ack', 'a']);
    });

    it('keeps the reading beside a result that is not another confirmation', () => {
        saveBotMessages('alice', 'bot_1', [
            { id: 'u', role: 'user', content: '帮我看看北京天气' },
            { id: 'ack', role: 'assistant', content: '你想看北京天气。我去查，查到了直接告诉你。', ack: true, understood: true },
            { id: 'a', role: 'assistant', content: '', pending: true, phase: 'plan', requestId: 'desktop-bot-weather' },
        ]);
        settlePendingBotReply('alice', 'bot_1', { content: '北京今天晴，25 度。', failed: false, requestId: 'desktop-bot-weather' });
        const items = messagesForBot('alice', 'bot_1');
        expect(items.map(item => item.id)).toEqual(['u', 'ack', 'a']);
        expect(items[2].content).toBe('北京今天晴，25 度。');
    });

    it('keeps a document on the reply and drops a path', () => {
        const docx = 'application/vnd.openxmlformats-officedocument.wordprocessingml.document';
        const data = 'd29yZA==';
        saveBotMessages('alice', 'bot_1', [
            { id: 'a', role: 'assistant', content: '', pending: true, requestId: 'desktop-bot-file' },
        ]);
        settlePendingBotReply('alice', 'bot_1', {
            content: '文件已放在这条回复里',
            failed: false,
            requestId: 'desktop-bot-file',
            files: desktopBotFiles([
                { name: '../secret.docx', mime: docx, data },
                { name: '自我描述.docx', mime: docx, data },
            ]),
        });
        expect(messagesForBot('alice', 'bot_1')[0].files).toEqual([
            { name: '自我描述.docx', mime: docx, data },
        ]);
    });

    it('keeps a pdf on the reply and drops a path', () => {
        const data = 'd29yZA==';
        expect(desktopBotFiles([
            { name: '../secret.pdf', mime: 'application/pdf', data },
            { name: '北京天气.pdf', mime: 'application/pdf', data },
        ])).toEqual([{ name: '北京天气.pdf', mime: 'application/pdf', data }]);
        expect(desktopBotFiles([{ name: 'Makefile', mime: '', data }])).toEqual([
            { name: 'Makefile', mime: 'application/octet-stream', data },
        ]);
        expect(desktopBotFiles([{ name: 'note.pdf', mime: 'not a type', data }])).toBeUndefined();
    });

    it('keeps a saved document path and drops the file bytes', () => {
        const docx = 'application/vnd.openxmlformats-officedocument.wordprocessingml.document';
        const saved = 'C:\\Users\\alice\\MaClaw\\bot-files\\自我描述.docx';
        saveBotMessages('alice', 'bot_1', [
            { id: 'a', role: 'assistant', content: '', pending: true, requestId: 'desktop-bot-file' },
        ]);
        settlePendingBotReply('alice', 'bot_1', {
            content: '文件已放在本机',
            failed: false,
            requestId: 'desktop-bot-file',
            localPaths: desktopBotLocalPaths([
                '../secret.docx',
                'C:\\Users\\alice\\..\\secret.docx',
                'notes.txt',
                saved,
                '/tmp/also.txt',
            ]),
            files: [{ name: '自我描述.docx', mime: docx, data: 'd29yZA==' }],
        });
        const message = messagesForBot('alice', 'bot_1')[0];
        expect(message.localPaths).toEqual([saved, '/tmp/also.txt']);
        expect(message.files).toBeUndefined();
        expect(desktopTaskStep('生成文档', message)).toBe('done');
        expect(localStorage.getItem('maclaw.desktopBotMessages.v1') || '').not.toContain('d29yZA==');
    });

    it('keeps a screenshot path and drops the image bytes', () => {
        const saved = 'C:\\Users\\alice\\.maclaw\\data\\bot-files\\bot_1\\screenshot.png';
        const png = 'iVBORw0KGgo=';
        saveBotMessages('alice', 'bot_1', [
            { id: 'a', role: 'assistant', content: '', pending: true, requestId: 'desktop-bot-shot' },
        ]);
        settlePendingBotReply('alice', 'bot_1', {
            content: '截图已附上',
            failed: false,
            requestId: 'desktop-bot-shot',
            shotPaths: ['../secret.png', saved],
            images: [{ mime: 'image/png', data: png }],
        });
        const message = messagesForBot('alice', 'bot_1')[0];
        expect(message.shotPaths).toEqual([saved]);
        expect(message.images).toBeUndefined();
        expect(localStorage.getItem('maclaw.desktopBotMessages.v1') || '').not.toContain(png);
        expect(localStorage.getItem('maclaw.desktopBotMessages.v1') || '').toContain(JSON.stringify(saved));
    });

    it('keeps the newest screenshot when older ones fill the store', () => {
        const png = 'A'.repeat(1_200_000);
        saveBotMessages('alice', 'bot_old', [
            { id: 'o1', role: 'assistant', content: '更早', images: [{ mime: 'image/png', data: png }] },
            { id: 'o2', role: 'assistant', content: '较早', images: [{ mime: 'image/png', data: png }] },
        ]);
        saveBotMessages('alice', 'bot_new', [
            { id: 'a', role: 'assistant', content: '上一张', images: [{ mime: 'image/png', data: png }] },
            { id: 'b', role: 'assistant', content: '最新', images: [{ mime: 'image/jpeg', data: png }] },
        ]);
        const newer = messagesForBot('alice', 'bot_new');
        expect(newer.find(item => item.id === 'b')?.images?.[0].mime).toBe('image/jpeg');
        expect(newer.find(item => item.id === 'b')?.content).toBe('最新');
        expect(newer.find(item => item.id === 'a')?.images).toBeUndefined();
        expect(newer.find(item => item.id === 'a')?.content).toBe('上一张');
        const older = messagesForBot('alice', 'bot_old');
        expect(older.find(item => item.id === 'o1')?.images).toBeUndefined();
        expect(older.find(item => item.id === 'o1')?.content).toBe('更早');
        expect(older.find(item => item.id === 'o2')?.images?.[0].mime).toBe('image/png');
    });

    it('lists a running bot turn and a queued follow-up, and drops a finished or stale one', () => {
        const now = 1_700_000_000_000;
        const id = (stamp: number, suffix: string) => `m-${stamp.toString(36)}-${suffix}`;
        saveBotMessages('alice', 'bot_live', [
            { id: id(now, 'user'), role: 'user', content: '看看北京天气', phase: 'execute' },
            { id: id(now, 'ack'), role: 'assistant', content: '收到', ack: true },
            { id: id(now, 'run'), role: 'assistant', content: '', pending: true, phase: 'execute' },
            { id: id(now + 1, 'next'), role: 'user', content: '再看上海', queued: true, phase: 'plan' },
        ]);
        saveBotMessages('alice', 'bot_wait', [
            { id: id(now + 2, 'user'), role: 'user', content: '登录邮箱' },
            { id: id(now + 2, 'run'), role: 'assistant', content: '', pending: true, userControl: true, attentionReason: 'login' },
        ]);
        saveBotMessages('alice', 'bot_done', [
            { id: id(now, 'user'), role: 'user', content: '已经做完' },
            { id: id(now, 'run'), role: 'assistant', content: '好了', pending: false },
        ]);
        const stale = now - BOT_PENDING_REPLY_WAIT_MS - 1000;
        saveBotMessages('alice', 'bot_stale', [
            { id: id(stale, 'user'), role: 'user', content: '很久以前' },
            { id: id(stale, 'run'), role: 'assistant', content: '', pending: true },
        ]);
        saveBotMessages('bob', 'bot_other', [
            { id: id(now, 'run'), role: 'assistant', content: '', pending: true },
        ]);

        const tasks = inProgressBotTasks('alice', now);
        expect(tasks.map(task => [task.botId, task.status, task.text, task.phase])).toEqual([
            ['bot_live', 'running', '看看北京天气', 'execute'],
            ['bot_live', 'queued', '再看上海', 'plan'],
            ['bot_wait', 'waiting', '登录邮箱', undefined],
        ]);
        expect(inProgressBotTasks('bob', now)).toEqual([
            expect.objectContaining({ botId: 'bot_other', status: 'running' }),
        ]);
    });

    it('shows the confirmed arrangement instead of the confirm, return, or secret control', () => {
        const now = 1_700_000_000_000;
        const id = (stamp: number, suffix: string) => `m-${stamp.toString(36)}-${suffix}`;
        const confirm = '安排已确认。请按你上一条安排执行。做完、失败或需要我时，在对话里告诉我。';
        const arrangement = '打开小红书，写三篇产品笔记并配封面图。确认后按这个安排执行。';
        saveBotMessages('alice', 'bot_promo', [
            { id: id(now, 'user'), role: 'user', content: '做小红书宣传，产品是台灯，发三篇' },
            { id: id(now, 'ack'), role: 'assistant', content: '你要做三篇台灯笔记。', ack: true, understood: true },
            { id: id(now, 'old'), role: 'assistant', content: '更早的安排，不要用这句。', phase: 'plan' },
            { id: id(now + 1, 'more'), role: 'user', content: '封面用产品图' },
            { id: id(now + 1, 'plan'), role: 'assistant', content: arrangement, phase: 'plan' },
            { id: id(now + 2, 'confirm'), role: 'user', content: confirm, phase: 'execute' },
            { id: id(now + 2, 'told'), role: 'assistant', content: '我按这个安排做。', ack: true, understood: true },
            { id: id(now + 2, 'run'), role: 'assistant', content: '', pending: true, phase: 'execute' },
        ]);
        expect(inProgressBotTasks('alice', now).map(task => task.text)).toEqual([arrangement]);

        const returned = '登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。';
        saveBotMessages('alice', 'bot_login', [
            { id: id(now, 'user'), role: 'user', content: '登录邮箱后导出账单' },
            { id: id(now, 'hand'), role: 'assistant', content: '请在这个浏览器里登录。', userControl: true, attentionReason: 'login' },
            { id: id(now + 3, 'back'), role: 'user', content: returned, phase: 'execute' },
            { id: id(now + 3, 'run'), role: 'assistant', content: '', pending: true, phase: 'execute' },
        ]);
        expect(inProgressBotTasks('alice', now).find(task => task.botId === 'bot_login')?.text).toBe('登录邮箱后导出账单');

        saveBotMessages('alice', 'bot_secret', [
            { id: id(now, 'user'), role: 'user', content: '打开后台并登录' },
            { id: id(now + 4, 'saved'), role: 'user', content: '已在本机保存 SITE_PASSWORD。', phase: 'execute' },
            { id: id(now + 4, 'run'), role: 'assistant', content: '', pending: true, phase: 'execute' },
        ]);
        expect(inProgressBotTasks('alice', now).find(task => task.botId === 'bot_secret')?.text).toBe('打开后台并登录');

        const filled = [
            { id: id(now, 'user'), role: 'user' as const, content: '把密码填进登录框' },
            { id: id(now + 5, 'filled'), role: 'user' as const, content: 'SITE_PASSWORD 已在本地填入当前密码框。请沿用这个结果继续，不要向我索要它的内容。' },
            { id: id(now + 5, 'run'), role: 'assistant' as const, content: '', pending: true, phase: 'execute' as const },
        ];
        expect(botTaskText(filled, 2)).toBe('把密码填进登录框');

        const queued = [
            { id: id(now, 'user'), role: 'user' as const, content: '做小红书宣传' },
            { id: id(now, 'plan'), role: 'assistant' as const, content: arrangement, phase: 'plan' as const },
            { id: id(now + 6, 'wait'), role: 'user' as const, content: confirm, queued: true, phase: 'execute' as const },
        ];
        expect(botTaskText(queued, 2)).toBe(arrangement);
        expect(botTaskText([
            { id: 'u', role: 'user', content: '做小红书宣传' },
            { id: 'c', role: 'user', content: confirm },
            { id: 'a', role: 'assistant', content: '', pending: true, phase: 'execute' },
        ], 2)).toBe('做小红书宣传');

        expect(botTaskText([
            { id: 'old', role: 'assistant', content: '北京今天晴，25 度。' },
            { id: 'u', role: 'user', content: '做小红书宣传，产品是台灯，发三篇' },
            { id: 'more', role: 'user', content: '封面用产品图' },
            { id: 'ack', role: 'assistant', content: '你要发三篇，封面用产品图。', ack: true, understood: true },
            { id: 'c', role: 'user', content: confirm },
            { id: 'run', role: 'assistant', content: '', pending: true, phase: 'execute' },
        ], 5)).toBe('做小红书宣传，产品是台灯，发三篇；封面用产品图');

        expect(botTaskText([
            { id: 'plan', role: 'assistant', content: arrangement, phase: 'plan' },
            { id: 'who', role: 'user', content: '你是谁呀?' },
            { id: 'chat', role: 'assistant', content: '我是你在这台云桌面上的同事。', chat: true },
            { id: 'c', role: 'user', content: confirm },
            { id: 'run', role: 'assistant', content: '', pending: true, phase: 'execute' },
        ], 4)).toBe(arrangement);

        expect(botTaskText([
            { id: 'u', role: 'user', content: '登录邮箱后导出账单' },
            { id: 'hand', role: 'assistant', content: '请在这个浏览器里登录。', userControl: true, attentionReason: 'login' },
            { id: 'c', role: 'user', content: confirm },
            { id: 'run', role: 'assistant', content: '', pending: true, phase: 'execute' },
        ], 3)).toBe('登录邮箱后导出账单');

        expect(botTaskText([
            { id: 'old-user', role: 'user', content: '上一件已经做完的事' },
            { id: 'old-plan', role: 'assistant', content: '更早的安排，确认后执行。', phase: 'plan' },
            { id: 'u', role: 'user', content: '看看北京天气' },
            { id: 'result', role: 'assistant', content: '北京今天晴，25 度。', phase: 'execute' },
            { id: 'c', role: 'user', content: confirm },
            { id: 'run', role: 'assistant', content: '', pending: true, phase: 'execute' },
        ], 5)).toBe('看看北京天气');

        expect(botTaskText([
            { id: 'u', role: 'user', content: '截屏发我' },
            { id: 'shot', role: 'assistant', content: '已截屏并发送。图片已附在上方，可直接查看。', phase: 'plan' },
            { id: 'c', role: 'user', content: confirm },
            { id: 'run', role: 'assistant', content: '', pending: true, phase: 'execute' },
        ], 3)).toBe('截屏发我');
    });

    it('keeps a handoff after the turn settles, until a later reply takes over', () => {
        const now = 1_700_000_000_000;
        const id = (stamp: number, suffix: string) => `m-${stamp.toString(36)}-${suffix}`;
        saveBotMessages('alice', 'bot_login', [
            { id: id(now, 'user'), role: 'user', content: '登录邮箱' },
            { id: id(now, 'run'), role: 'assistant', content: '请在这个浏览器里登录', pending: false, userControl: true, attentionReason: 'login' },
            { id: id(now + 1, 'next'), role: 'user', content: '然后再看天气', queued: true },
        ]);
        expect(inProgressBotTasks('alice', now).map(task => [task.status, task.text])).toEqual([
            ['waiting', '登录邮箱'],
            ['queued', '然后再看天气'],
        ]);

        saveBotMessages('alice', 'bot_login', [
            { id: id(now, 'user'), role: 'user', content: '登录邮箱' },
            { id: id(now, 'run'), role: 'assistant', content: '请在这个浏览器里登录', pending: false, userControl: true, attentionReason: 'login' },
            { id: id(now + 2, 'done'), role: 'assistant', content: '已经登录并看完天气', pending: false },
        ]);
        expect(inProgressBotTasks('alice', now)).toEqual([]);
    });

    it('keeps a handoff when a side answer, failure, or question follows', () => {
        const now = 1_700_000_000_000;
        const id = (stamp: number, suffix: string) => `m-${stamp.toString(36)}-${suffix}`;
        saveBotMessages('alice', 'bot_login', [
            { id: id(now, 'user'), role: 'user', content: '登录邮箱' },
            { id: id(now, 'run'), role: 'assistant', content: '请在这个浏览器里登录', pending: false, userControl: true, attentionReason: 'login' },
            { id: id(now + 1, 'who'), role: 'user', content: '你是谁呀?' },
            { id: id(now + 2, 'chat'), role: 'assistant', content: '我是你在这台云桌面上的同事。', chat: true },
            { id: id(now + 3, 'miss'), role: 'user', content: '打开百度' },
            { id: id(now + 4, 'fail'), role: 'assistant', content: '这句没整理成可执行的安排，没有发出。', failed: true },
            { id: id(now + 5, 'asku'), role: 'user', content: '收件人是谁' },
            { id: id(now + 6, 'ask'), role: 'assistant', content: '这份要发给谁？', askUser: { question: '这份要发给谁？' } },
        ]);
        expect(inProgressBotTasks('alice', now).map(task => [task.status, task.text])).toEqual([
            ['waiting', '登录邮箱'],
        ]);
    });

    it('keeps a turn this process admitted after the stale clock', () => {
        const now = 1_700_000_000_000;
        const stale = now - BOT_PENDING_REPLY_WAIT_MS - 1000;
        const messageId = `m-${stale.toString(36)}-run`;
        clearLiveBotTurn('alice', 'bot_long', messageId);
        saveBotMessages('alice', 'bot_long', [
            { id: `m-${stale.toString(36)}-user`, role: 'user', content: '还在做' },
            { id: messageId, role: 'assistant', content: '', pending: true },
        ]);
        expect(inProgressBotTasks('alice', now)).toEqual([]);
        noteLiveBotTurn('alice', 'bot_long', messageId);
        expect(inProgressBotTasks('alice', now).map(task => task.text)).toEqual(['还在做']);
        clearLiveBotTurn('alice', 'bot_long', messageId);
        expect(inProgressBotTasks('alice', now)).toEqual([]);
    });

    it('advances the transcript generation only after storage accepts the write', () => {
        const before = botTranscriptGeneration();
        saveBotMessages('alice', 'bot_gen', [
            { id: 'm-1-user', role: 'user', content: '你好' },
        ]);
        expect(botTranscriptGeneration()).toBe(before + 1);

        const proto = Object.getPrototypeOf(window.localStorage) as Storage;
        const setItem = proto.setItem;
        proto.setItem = () => { throw new Error('quota'); };
        try {
            saveBotMessages('alice', 'bot_gen', [
                { id: 'm-1-user', role: 'user', content: '你好' },
                { id: 'm-2-shot', role: 'assistant', content: '', images: [{ mime: 'image/png', data: 'aaaa' }] },
            ]);
        } finally {
            proto.setItem = setItem;
        }
        expect(botTranscriptGeneration()).toBe(before + 1);
        expect(messagesForBot('alice', 'bot_gen').map(item => item.id)).toEqual(['m-1-user']);

        deleteDesktopBot('alice', 'bot_gen');
        expect(botTranscriptGeneration()).toBe(before + 2);
        expect(messagesForBot('alice', 'bot_gen')).toEqual([]);
    });

    it('reuses one in-progress snapshot until the transcript changes or the window expires', () => {
        const now = Date.now();
        saveBotMessages('alice', 'bot_snap', [
            { id: `m-${now.toString(36)}-user`, role: 'user', content: '第一件' },
            { id: `m-${now.toString(36)}-run`, role: 'assistant', content: '', pending: true },
        ]);
        const first = currentInProgressBotTasks('alice', now);
        expect(currentInProgressBotTasks('alice', now + 1000)).toBe(first);
        saveBotMessages('alice', 'bot_snap', [
            { id: `m-${now.toString(36)}-user`, role: 'user', content: '第一件' },
            { id: `m-${now.toString(36)}-run`, role: 'assistant', content: '', pending: true },
            { id: `m-${(now + 1).toString(36)}-next`, role: 'user', content: '第二件', queued: true },
        ]);
        const next = currentInProgressBotTasks('alice', now + 1000);
        expect(next).not.toBe(first);
        expect(next.map(task => task.text)).toEqual(['第一件', '第二件']);
        const aged = currentInProgressBotTasks('alice', now + 1000 + 30_000);
        expect(aged).not.toBe(next);
        expect(aged.map(task => task.text)).toEqual(['第一件', '第二件']);
        const empty = currentInProgressBotTasks('nobody', now);
        expect(empty).toEqual([]);
        expect(currentInProgressBotTasks('nobody', now + 60_000)).toBe(empty);
        expect(desktopBotAccountId({ remote_user_id: 'alice', remote_email: 'a@b.c' })).toBe('alice');
        expect(desktopBotAccountId({ remote_user_id: '  alice  ', remote_email: 'a@b.c' })).toBe('alice');
        expect(desktopBotAccountId({ remote_email: '  a@b.c  ' })).toBe('a@b.c');
        expect(desktopBotAccountId(null)).toBe('local');
    });

    it('asks for the computer when a reply needs a person at the desktop', () => {
        expect(messageNeedsComputerTakeover({ content: '缺少用户扫描二维码登录小红书，笔记尚未发布。' })).toBe(true);
        expect(messageNeedsComputerTakeover({ content: '请在这个桌面的浏览器里完成登录。', attentionReason: 'login_wall' })).toBe(true);
        expect(messageNeedsComputerTakeover({ content: '页面已登录，账号 Dr.Daniel。标题和图片都已就位。' })).toBe(false);
        expect(messageNeedsComputerTakeover({ content: '已通过扫码登录并发布成功。' })).toBe(false);
        expect(messageNeedsComputerTakeover({ content: '笔记配图使用了二维码。' })).toBe(false);
        expect(messageNeedsComputerTakeover({ content: '登录失败', failed: true, userControl: true })).toBe(false);
    });

    it('warns while official credits can still stop a desktop task', () => {
        expect(desktopCreditReadout({
            mode: 'default',
            authorized: true,
            windows: [{ limit: 400, used: 320 }],
        })).toEqual({ total: 400, remaining: 80, notice: 'low' });
        expect(desktopCreditReadout({
            mode: 'default',
            authorized: true,
            status: 'period_limited',
            windows: [{ limit: 400, used: 400 }],
        })?.notice).toBe('empty');
        expect(desktopCreditReadout({
            mode: 'default',
            authorized: true,
            windows: [
                { limit: 1000, used: 100 },
                { limit: 200, used: 170, held: 10 },
            ],
        })).toEqual({ total: 200, remaining: 20, notice: 'low' });
        expect(desktopCreditReadout({
            mode: 'default',
            authorized: true,
            windows: [{ limit: 400, used: 100 }],
        })?.notice).toBe('');
        expect(desktopCreditReadout({
            mode: 'default',
            authorized: true,
            total: 6399.52,
            remaining: 462.56,
        })?.notice).toBe('');
        expect(desktopCreditReadout({
            mode: 'default',
            authorized: true,
            total: 6399.52,
            remaining: 100,
        })?.notice).toBe('');
        expect(desktopCreditReadout({
            mode: 'default',
            authorized: true,
            total: 6399.52,
            remaining: 99.99,
        })?.notice).toBe('low');
        expect(desktopCreditReadout({
            mode: 'default',
            authorized: true,
            windows: [
                { group: 'welcome', limit: 100, used: 99 },
                { group: 'system-free', limit: 400, used: 10 },
            ],
        })).toEqual({ total: 400, remaining: 390, notice: '' });
        expect(desktopCreditReadout({
            mode: 'default',
            authorized: true,
            status: 'period_limited',
            windows: [{ group: 'system-free', limit: 400, used: 80 }],
        })).toEqual({ total: 400, remaining: 0, notice: 'empty' });
        expect(desktopCreditReadout({ mode: 'custom', authorized: true, windows: [{ limit: 400, used: 400 }] })).toBeNull();
        expect(desktopCreditReadout({ mode: 'default', unlimited: true, authorized: true })).toBeNull();
        expect(desktopCreditReadout({
            mode: 'default',
            unlimited: true,
            authorized: true,
            windows: [{ limit: 400, used: 320 }],
        })?.notice).toBe('low');
    });

    it('treats a generic how-is-it-going question as status and an exact stop as stop', () => {
        expect(desktopBotLocalControl('后台在运行吗？')).toBe('status');
        expect(desktopBotLocalControl('任务现在怎么样了')).toBe('status');
        expect(desktopBotLocalControl('后台在跑吗')).toBe('status');
        expect(desktopBotTaskHint('后台在运行吗？')).toBe('');
        expect(desktopBotTaskHint('那个下载报表怎么样了')).toBe('下载报表');
        expect(desktopBotLocalControl('那个下载报表怎么样了')).toBe('');
        expect(desktopBotTaskHint('运行窗口')).toBe('运行窗口');
        expect(desktopBotLocalControl('运行窗口')).toBe('');
        expect(desktopBotLocalControl('把 Evince 装上')).toBe('');
        for (const word of ['停', '停下', '别做了', '取消', '不干了', '算了', 'stop', '停下，不干了。']) expect(desktopBotLocalControl(word)).toBe('stop');
        expect(desktopBotStopIntent('停下，不干了。')).toBe(true);
        expect(desktopBotLocalControl('  取消  ')).toBe('stop');
        expect(desktopBotLocalControl('别装了')).toBe('');
        expect(desktopBotLocalControl('取消任务')).toBe('');
        expect(desktopBotStillStoppingSentence('zh-Hans')).toBe('还在停。停稳了再跟你说。');
        expect(desktopBotPauseAskSentence('zh-Hans')).toBe('我先停一下。是整个停下，还是只改正在做的内容？');
        expect(desktopBotStopIntent('yamete')).toBe(false);
        expect(desktopBotStopIntent('算了别弄了')).toBe(false);
        expect(desktopReplyClaimsStopped('好的，已停下，不会继续了。')).toBe(true);
        expect(desktopReplyClaimsStopped('stop using bold text')).toBe(false);
        expect(desktopReplyClaimsStopped("stop the music in the video I'm editing")).toBe(false);
        expect(desktopReplyClaimsStopped('STOP_WORK')).toBe(false);
        expect(desktopFalseStopClaim('unknown', 'STOP_WORK')).toBe(true);
        expect(desktopFalseStopClaim('continue_other', 'STOP_WORK')).toBe(false);
        expect(desktopFalseStopClaim('continue_other', '好的，已停下。')).toBe(true);
        expect(desktopFalseStopClaim('skip', '已停下')).toBe(false);
        expect(desktopBotRunningSentence(['安装 Evince'], 'zh-Hans')).toBe('在做。这件是「安装 Evince」。');
        expect(desktopBotRunningSentence(['安装 Evince', '下载报表'], 'zh-Hans')).toBe('在做这几件：\n安装 Evince\n下载报表');
        expect(desktopBotRunningSentence(['安装 Evince'], 'zh-Hant')).toBe('在做。這件是「安装 Evince」。');
        expect(desktopBotAdmittingSentence('zh-Hans')).toBe('刚接下，还在交给后台。');
        expect(desktopBotStoppedSentence(['安装 Evince'], false, 'zh-Hans')).toBe('停了。安装 Evince 不再继续。');
        expect(desktopBotStoppedSentence(['安装 Evince'], true, 'zh-Hans')).toBe('停了。后面还有一件记下的，你要的话我再做。');
        expect(desktopBotStopIdleSentence('zh-Hans')).toBe('现在没有在做的事，不用停。');
        expect(desktopBotStopFailedSentence('还在交给后台', 'zh-Hans')).toBe('没停下来：还在交给后台。');
        expect(desktopBotTypedConfirmSentence('zh-Hans')).toBe('点安排下面那个按钮就行，打这句不会开始。');
        expect(desktopBotStatusUnreachableSentence('zh-Hans')).toBe('刚才没问到后台，你再说一次。');
    });

    it('treats install, delete, pay, and wipe as destructive and keeps a cross note once', () => {
        expect(desktopBotDestructive('安装一个pdf阅读器')).toBe(true);
        expect(desktopBotDestructive('请 uninstall 这个包')).toBe(true);
        expect(desktopBotDestructive('please install it')).toBe(true);
        expect(desktopBotDestructive('打开示例网站')).toBe(false);
        expect(desktopBotDestructive('安装包的说明')).toBe(true);
        expect(desktopBotCrossTargetText('notify', '到了')).toBe('主 Bot 说：到了');
        expect(desktopBotCrossTargetText('delegate', '装一下')).toBe('主 Bot 交代：装一下');
        expect(desktopBotAskingLabel('all', [])).toBe('这些 Bot');
        expect(desktopBotAskingLabel('bot_6', [{ id: 'bot_6', title: 'Bot 6' }])).toBe('Bot 6');
        expect(desktopBotActivityText('正在输入')).toBe('正在输入');
        expect(desktopBotActivityText('正在点击')).toBe('正在点击');
        expect(desktopBotActivityText('正在处理')).toBe('');
        expect(desktopBotActivityText('正在一点点推进')).toBe('');
        expect(desktopBotActivityText('正在输入 hello')).toBe('');
        expect(desktopBotWorkingLabel('', '')).toBe('正在一点点推进');
        expect(desktopBotWorkingLabel('', '正在输入')).toBe('正在输入');
        expect(desktopBotWorkingLabel('这些 Bot', '正在输入')).toBe('正在询问 这些 Bot');
        expect(desktopBotWorkingLabel('', '正在处理')).toBe('正在一点点推进');
        appendDesktopBotReport('alice', 'bot_6', {
            content: '主 Bot 交代：装一下',
            failed: false,
            requestId: 'n1:target',
            phase: 'plan',
            workOrder: '安装一个pdf阅读器',
            crossNoteId: 'n1',
        });
        appendDesktopBotReport('alice', 'bot_6', {
            content: '主 Bot 交代：装一下',
            failed: false,
            requestId: 'n1:target',
            phase: 'plan',
            workOrder: '安装一个pdf阅读器',
            crossNoteId: 'n1',
        });
        const landed = messagesForBot('alice', 'bot_6');
        expect(landed).toHaveLength(1);
        expect(landed[0]?.phase).toBe('plan');
        expect(landed[0]?.workOrder).toBe('安装一个pdf阅读器');
        expect(landed[0]?.crossNoteId).toBe('n1');
        expect(landed[0]?.chat).toBeUndefined();
        appendDesktopBotReport('alice', 'bot_1', { content: '到点了', failed: false, requestId: 'desktop-bot-sched-1' });
        expect(messagesForBot('alice', 'bot_1')[0]?.phase).toBeUndefined();
        expect(desktopBotNoticeSeen('alice', 'n1:from')).toBe(false);
        markDesktopBotNotice('alice', 'n1:from');
        expect(desktopBotNoticeSeen('alice', 'n1:from')).toBe(true);
        expect(desktopBotNoticeSeen('alice', 'n1:target')).toBe(false);
    });
});
