import { BrowserOpenURL } from '../../../wailsjs/runtime';
import type { ApiStoreProvider } from '../../config/apiStoreProviders';

type ApiStoreProviderCardProps = {
    provider: ApiStoreProvider;
    t: (key: string) => string;
};

const badgeStyle = (backgroundColor: string) => ({
    position: 'absolute' as const,
    top: '-6px',
    right: '-6px',
    backgroundColor,
    color: '#fff',
    padding: '3px 10px',
    borderRadius: '4px',
    fontSize: '0.65rem',
    fontWeight: 'bold',
    boxShadow: '0 2px 4px rgba(0,0,0,0.15)',
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
        {provider.isRelay && <div style={badgeStyle('var(--theme-primary)')}>{t('relayService')}</div>}
        {provider.hasSubscription && <div style={badgeStyle('var(--theme-primary-strong, #183b63)')}>{t('subscription')}</div>}
        {provider.isBilling && <div style={badgeStyle('var(--theme-warning)')}>{t('billing')}</div>}
        <div style={{ fontSize: '0.85rem', fontWeight: 600, color: 'var(--theme-primary)', marginBottom: '8px' }}>
            {provider.name}
        </div>
        <div style={{ fontSize: '0.85rem', color: 'var(--theme-text-muted)' }}>
            {'\u{1F6C5}'}
        </div>
    </div>
);
