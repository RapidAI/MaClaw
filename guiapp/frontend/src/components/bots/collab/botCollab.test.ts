import { describe, expect, it } from 'vitest';
import { answeredSinceLast, applyMention, attentionOf, humanPrompt, mentionCandidates, mentionQuery, saidForChat, statusRowText, stateLabel, type Assignment, type SaidItem } from './botCollab';

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
