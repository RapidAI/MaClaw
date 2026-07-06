import { BrowserOpenURL } from '../../../wailsjs/runtime';
import type { ApiStoreProvider } from '../../config/apiStoreProviders';

type ApiStoreProviderCardProps = {
    provider: ApiStoreProvider;
    t: (key: string) => string;
};

/* Badge 用固定配色（不跟随主题变量），保证亮/暗模式下白字对比度 ≥ 4.5:1 (WCAG AA) */
const badgeColors = {
    relay: { bg: '#1d4ed8', text: '#ffffff' },       // 深蓝 — 中转服务 (contrast 5.57)
    subscription: { bg: '#0f766e', text: '#ffffff' }, // 深青 — 订阅制 (contrast 4.54)
    billing: { bg: '#92400e', text: '#ffffff' },      // 深琥珀 — 按量计费 (contrast 5.74)
} as const;

const badgeStyle = (bg: string, text: string) => ({
    position: 'absolute' as const,
    top: '-6px',
    right: '-6px',
    backgroundColor: bg,
    color: text,
    padding: '3px 10px',
    borderRadius: '4px',
    fontSize: '0.65rem',
    fontWeight: 'bold',
    boxShadow: '0 2px 4px rgba(0,0,0,0.25)',
});

export const ApiStoreProviderCard = ({ provider, t }: ApiStoreProviderCardProps) => (
    <div
        style={{
            backgroundColor: 'var(--theme-surface)',
            border: '1px solid var(--theme-border)',
            borderRadius: '8px',
            padding: '8px 12px',
            display: 'flex',
            flexDirection: 'column',
            justifyContent: 'center',
            transition: 'all 0.2s ease',
            cursor: 'pointer',
            position: 'relative',
            minHeight: '42px',
        }}
        onMouseEnter={(e) => {
            e.currentTarget.style.boxShadow = '0 4px 12px rgba(0,0,0,0.1)';
            e.currentTarget.style.transform = 'translateY(-2px)';
        }}
        onMouseLeave={(e) => {
            e.currentTarget.style.boxShadow = 'none';
            e.currentTarget.style.transform = 'translateY(0)';
        }}
        onClick={() => BrowserOpenURL(provider.url)}
    >
        {provider.isRelay && <div style={badgeStyle(badgeColors.relay.bg, badgeColors.relay.text)}>{t('relayService')}</div>}
        {provider.hasSubscription && <div style={badgeStyle(badgeColors.subscription.bg, badgeColors.subscription.text)}>{t('subscription')}</div>}
        {provider.isBilling && <div style={badgeStyle(badgeColors.billing.bg, badgeColors.billing.text)}>{t('billing')}</div>}
        <div style={{ fontSize: '0.85rem', fontWeight: 600, color: 'var(--theme-primary)', marginBottom: '8px' }}>
            {provider.name}
        </div>
        <div style={{ fontSize: '0.85rem', color: 'var(--theme-text-muted)' }}>
            {'\u{1F6C5}'}
        </div>
    </div>
);
