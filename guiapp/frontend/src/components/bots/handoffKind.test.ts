import { describe, expect, it } from 'vitest';
import { handoffKind } from './DesktopBotWorkspace';

describe('handoffKind', () => {
    it('tells a publish handoff from a login', () => {
        // The live 10-31 05:55 reason: shown as a login prompt before.
        expect(handoffKind('标题、正文和两张配图都已填好，发布按钮需要你亲自点：页面右下角「发布」，点完请按交还')).toBe('publish');
        expect(handoffKind('请扫码登录小红书后点交还')).toBe('login');
        expect(handoffKind('login')).toBe('login');
        expect(handoffKind('')).toBe('login');
        expect(handoffKind('请完成滑块验证')).toBe('captcha');
        expect(handoffKind('请确认支付金额')).toBe('other');
    });
});
