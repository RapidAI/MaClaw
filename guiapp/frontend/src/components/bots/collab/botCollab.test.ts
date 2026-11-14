import { describe, expect, it } from 'vitest';
import { answeredSinceLast, applyBotStatusLine, shownDesktopBotActivity, applyMention, attentionOf, humanPrompt, mentionCandidates, mentionQuery, placeChatSays, saidForChat, statusRowText, stateLabel, type Assignment, type SaidItem } from './botCollab';

const bots = [{ id: 'm', title: '主管', primary: true }, { id: 'a', title: '小A' }, { id: 'b', title: '小B' }, { id: 'c', title: '小C' }, { id: 'd', title: '小D' }];
const asg = (id: string, target: string, state: string, extra: Partial<Assignment> = {}): Assignment =>
    ({ id, owner_bot: 'm', target_bot: target, task: '写一篇文章', state, created_at: '', ...extra });
const said = (id: string, from: string, kind = 'say', target = 'human'): SaidItem => ({ id, from, target, kind, text: id, created_at: '' });

describe('botCollab helpers', () => {
    it('status row caps at three bots and counts the rest', () => {
        const row = statusRowText([asg('1', 'a', 'progress', { last_report: '查资料' }), asg('2', 'b', 'running'), asg('3', 'c', 'queued'), asg('4', 'd', 'progress'), asg('5', 'a', 'completed')], bots);
        expect(row).toBe('小A 正在做：查资料；小B 在想：写一篇文章；小C 排队中：写一篇文章；还有 1 个机器人在忙');
        expect(statusRowText([asg('1', 'a', 'completed')], bots)).toBe('');
    });
    it('labels states in Chinese and prompts the person', () => {
        expect(stateLabel('needs_login')).not.toBe('needs_login');
        expect(humanPrompt(asg('1', 'a', 'needs_login'), bots)?.title).toBe('小A 需要你登录');
        expect(humanPrompt(asg('1', 'a', 'waiting_human', { last_report: '用哪个标题？' }), bots)?.body).toContain('用哪个标题？');
        expect(humanPrompt(asg('1', 'a', 'progress'), bots)).toBeNull();
    });
    it('main chat shows every bot, sub-bot chat only its own', () => {
        const items = [said('1', 'a'), said('2', 'b'), said('3', 'a', 'room', 'r1')];
        expect(saidForChat(items, 'm', true).map(i => i.id)).toEqual(['1', '2']);
        expect(saidForChat(items, 'a', false).map(i => i.id)).toEqual(['1']);
        // A bot with its own chat is heard only there, not again in the main bot's.
        expect(saidForChat(items, 'm', true, 0, ['m', 'a']).map(i => i.id)).toEqual(['2']);
        expect(saidForChat(items, 'a', false, 0, ['m', 'a']).map(i => i.id)).toEqual(['1']);
    });
    it('finds who answered after the last human message', () => {
        expect(answeredSinceLast([said('1', 'a'), said('2', 'human', 'room'), said('3', 'b'), said('4', 'a')])).toEqual(['b', 'a']);
    });
    it('@mention autocomplete', () => {
        const q = mentionQuery('你好 @小', 5);
        expect(q).toEqual({ start: 3, query: '小' });
        expect(mentionCandidates(bots, '小').length).toBe(4);
        const out = applyMention('你好 @小', 3, 5, bots[1]);
        expect(out.text.startsWith('你好 @小A')).toBe(true);
    });
    it('attention defaults by role', () => {
        const room = { id: 'r', name: 'x', members: ['m', 'a'], created_at: '' };
        expect(attentionOf(room, bots[0])).toBe('all');
        expect(attentionOf(room, bots[1])).toBe('mentions');
        expect(attentionOf({ ...room, attention: { a: 'off' } }, bots[1])).toBe('off');
    });
});

describe('inline says only after the latest message', () => {
    it('drops an old say that would sit pinned under newer replies', async () => {
        const { saidForChat, saidSince, messageIdTime } = await import('./botCollab');
        const now = Date.parse('2026-10-31T05:56:00+08:00');
        const old = { id: 'n1', kind: 'say', target: 'human', from: 'b', text: '今天适合喝乌龙茶', created_at: '2026-10-28T07:26:02Z' } as unknown as SaidItem;
        const fresh = { id: 'n2', kind: 'say', target: 'human', from: 'b', text: 'new', created_at: '2026-10-30T21:55:30Z' } as unknown as SaidItem;
        const msg = `m-${(now - 60000).toString(36)}-abc`;
        expect(messageIdTime(msg)).toBe(now - 60000);
        const since = saidSince([msg, 'x'], now);
        expect(saidForChat([old, fresh], 'b', false, since).map(i => i.id)).toEqual(['n2']);
        expect(saidForChat([old, fresh], 'b', false).map(i => i.id)).toEqual(['n1', 'n2']);
    });
});

describe('placeChatSays', () => {
    it('keeps an earlier weather report where it was said and drops a later reask', () => {
        const ackAt = Date.parse('2026-11-14T08:20:00Z');
        const userAt = Date.parse('2026-11-14T08:25:00Z');
        const weatherAt = '2026-11-14T08:22:00Z';
        const againAt = '2026-11-14T08:31:00Z';
        const messages = [
            { id: `m-${ackAt.toString(36)}-ack`, role: 'assistant', content: '好的，我会定时向你报告天气情况。' },
            { id: `m-${userAt.toString(36)}-u`, role: 'user', content: '北京，下次再生成pdf文件' },
        ];
        const items = [
            { id: 'w', kind: 'say', target: 'human', from: 'b', text: '北京晴 12℃', created_at: weatherAt },
            { id: 'q', kind: 'say', target: 'human', from: 'b', text: '这轮的天气：北京晴 12℃，上海阴有小雨 19℃。你要报自己城市的话，说一声我就固定下来。', created_at: againAt },
        ] as SaidItem[];
        const placed = placeChatSays(items, 'b', false, messages);
        expect(placed.after.get(messages[0].id)?.map(item => item.id)).toEqual(['w']);
        expect(placed.trailing).toEqual([]);
    });
});

describe('live step in the header', () => {
    it('shows each busy bot with its current step', () => {
        const row = statusRowText([], bots, 3, 'zh-Hans', [{ bot: 'a', phase: 'execute', task: '正在登录 CSDN' }, { bot: 'b', phase: 'execute', task: '正在发布 小红书' }]);
        expect(row).toBe('小A 正在登录 CSDN；小B 正在发布 小红书');
    });
});

describe('applyBotStatusLine', () => {
    it('keeps one line per bot and clears only its own run', () => {
        let st = applyBotStatusLine({}, 'a', 'run_1', '单号 run_1 · 第 3 步');
        st = applyBotStatusLine(st, 'b', 'run_2', '单号 run_2 · 后台任务运行中，下次查看 21:40');
        expect(st.a.text).toBe('单号 run_1 · 第 3 步');
        expect(applyBotStatusLine(st, 'a', 'run_old', '')).toBe(st);
        expect(applyBotStatusLine(st, 'a', 'run_1', '').a).toBeUndefined();
        expect(applyBotStatusLine(st, 'a', 'run_1', '').b.text).toContain('下次查看');
    });
});

describe('shownDesktopBotActivity', () => {
    it('drops a stale wake label only', () => {
        expect(shownDesktopBotActivity({ text: '正在唤醒电脑', at: 1000 }, 2000)).toBe('正在唤醒电脑');
        expect(shownDesktopBotActivity({ text: '正在唤醒电脑', at: 1000 }, 1000 + 121000)).toBe('');
        expect(shownDesktopBotActivity({ text: '正在执行命令', at: 1000 }, 1000 + 999999)).toBe('正在执行命令');
        expect(shownDesktopBotActivity(undefined)).toBe('');
    });
});
