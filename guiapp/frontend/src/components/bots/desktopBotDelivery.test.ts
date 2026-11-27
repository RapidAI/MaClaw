import { describe, expect, it } from 'vitest';
import { deliveryAnswering } from './desktopBotResultStore';

describe('deliveryAnswering', () => {
    it('names the run request when a different message was said last', () => {
        const out = deliveryAnswering('泡一杯绿茶，水温80度。', '怎么泡茶', [
            { role: 'user', content: '今天天气怎样？' },
        ]);
        expect(out).toBe('「怎么泡茶」\n泡一杯绿茶，水温80度。');
    });
    it('stays plain when the result answers the latest message', () => {
        expect(deliveryAnswering('晴，25度。', '今天天气怎样？', [
            { role: 'user', content: '今天天气怎样？' },
            { role: 'assistant', content: '' },
        ])).toBe('晴，25度。');
    });
    it('stays plain without a task', () => {
        expect(deliveryAnswering('done', '', [{ role: 'user', content: 'x' }])).toBe('done');
    });
    it('does not quote the hand-back control instruction as the request', () => {
        const handBack = '我已经在桌面上完成了需要我做的操作（登录/验证），桌面交还给你，请继续刚才的任务。';
        expect(deliveryAnswering('做成了。\n已发布《真实世界，一键变仿真》', handBack, [
            { role: 'user', content: '我按这个做。' },
        ])).toBe('做成了。\n已发布《真实世界，一键变仿真》');
    });
    it('does not quote an automatic hand-back resume instruction', () => {
        const resume = '页面已显示登录成功，桌面已自动交还给你，请沿用这个登录状态继续刚才的任务；不要重新打开或刷新页面。做完后用你自己的话给用户一句结果。';
        expect(deliveryAnswering('做成了。', resume, [
            { role: 'user', content: '发小红书' },
        ])).toBe('做成了。');
    });
    it('does not quote the confirm protocol sentence', () => {
        const confirm = '安排已确认。请按你上一条安排执行。做完、失败或需要我时，在对话里告诉我。';
        expect(deliveryAnswering('已经买好。', confirm, [
            { role: 'user', content: '帮我买一杯咖啡' },
        ])).toBe('已经买好。');
    });
    it('does not quote a saved-secret resume or a typed continue label', () => {
        expect(deliveryAnswering('登录好了。', '已在本机保存 XIAOHONGSHU_PASSWORD。', [
            { role: 'user', content: '发小红书' },
        ])).toBe('登录好了。');
        expect(deliveryAnswering('发好了。', '验证完成，继续', [
            { role: 'user', content: '发小红书' },
        ])).toBe('发好了。');
    });
    it('still quotes a real request', () => {
        expect(deliveryAnswering('做完了。', '帮我整理桌面上的文件', [
            { role: 'user', content: '今天天气怎样？' },
        ])).toBe('「帮我整理桌面上的文件」\n做完了。');
    });
});
