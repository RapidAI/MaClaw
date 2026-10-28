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
});
