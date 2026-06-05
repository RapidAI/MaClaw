import { describe, expect, it } from 'vitest';
import { localizeByLang, localizeHubServiceReason, localizeHubServiceRedeemError, localizeHubServiceStatusError } from '../hubServiceI18n';

describe('hubServiceI18n', () => {
    it('selects localized text by UI language', () => {
        expect(localizeByLang('en', 'English', '简体', '繁體')).toBe('English');
        expect(localizeByLang('zh-Hans', 'English', '简体', '繁體')).toBe('简体');
        expect(localizeByLang('zh-Hant', 'English', '简体', '繁體')).toBe('繁體');
    });

    it('localizes known Hub service reasons and strips Error prefix', () => {
        expect(localizeHubServiceReason('Error: grant credits are exhausted', 'zh-Hans')).toBe('授权额度已用尽。');
        expect(localizeHubServiceReason('grant has expired', 'zh-Hant')).toBe('授權已過期。');
        expect(localizeHubServiceReason('hub access token is missing', 'en')).toBe('Hub access token is missing. Reconnect Hub and try again.');
    });

    it('keeps unknown reasons readable', () => {
        expect(localizeHubServiceReason('Error: custom backend reason', 'zh-Hans')).toBe('custom backend reason');
    });

    it('localizes known service redeem errors', () => {
        expect(localizeHubServiceRedeemError('Error: invalid redeem code', 'zh-Hans')).toBe('兑换码无效，请核对后重试。');
        expect(localizeHubServiceRedeemError('redeem code must be 8 letters or digits', 'zh-Hant')).toBe('兌換碼必須是 8 位字母或數字。');
        expect(localizeHubServiceRedeemError('redeem failed: 502 Bad Gateway', 'en')).toBe('Redeem failed: 502 Bad Gateway');
    });

    it('localizes service status query errors', () => {
        expect(localizeHubServiceStatusError('account status query failed: 401 Unauthorized: viewer token expired', 'zh-Hans')).toBe('Hub 授权已过期，请重新连接 Hub 后重试。');
        expect(localizeHubServiceStatusError('account status query failed: 400 Bad Request: invalid tenant', 'zh-Hant')).toBe('Hub 租戶資訊無效，請重新完成 Hub 啟用。');
        expect(localizeHubServiceStatusError('status query failed: 502 Bad Gateway', 'zh-Hans')).toBe('服务状态查询失败：502 Bad Gateway');
    });
});
