import type { Dispatch, SetStateAction } from 'react';
import type { ProviderEndpoint } from '../../config/providerCatalog';

type ProviderFilter = 'all' | 'china' | 'global';

type HoveredProvider = { provider: ProviderEndpoint; x: number; y: number } | null;

type ProviderSelectorDialogProps = {
    providers: ProviderEndpoint[];
    providerFilter: ProviderFilter;
    setProviderFilter: Dispatch<SetStateAction<ProviderFilter>>;
    selectedProvider: ProviderEndpoint | null;
    setSelectedProvider: Dispatch<SetStateAction<ProviderEndpoint | null>>;
    hoveredProvider: HoveredProvider;
    setHoveredProvider: Dispatch<SetStateAction<HoveredProvider>>;
    lang: string;
    t: (key: string) => string;
    onConfirm: () => void;
    onClose: () => void;
};

export const ProviderSelectorDialog = ({
    providers,
    providerFilter,
    setProviderFilter,
    selectedProvider,
    setSelectedProvider,
    hoveredProvider,
    setHoveredProvider,
    lang,
    t,
    onConfirm,
    onClose,
}: ProviderSelectorDialogProps) => (
    <div className="modal-overlay" style={{ backgroundColor: 'rgba(0,0,0,0.35)', backdropFilter: 'blur(3px)' }} onClick={onClose}>
        <div className="modal-content" style={{ maxWidth: '480px', maxHeight: '70vh', padding: '20px', borderRadius: '16px', border: 'none', boxShadow: '0 20px 40px rgba(0,0,0,0.12)' }} onClick={(e) => e.stopPropagation()}>
            <h2 style={{ margin: '0 0 16px 0', fontSize: '1.1rem', fontWeight: 700, color: '#1e293b', textAlign: 'center' }}>{t("selectProviderTitle")}</h2>

            <div style={{ display: 'flex', gap: '6px', marginBottom: '14px', justifyContent: 'center' }}>
                {(['all', 'china', 'global'] as const).map(f => (
                    <button
                        key={f}
                        onClick={() => setProviderFilter(f)}
                        style={{
                            padding: '5px 16px', fontSize: '0.8rem', borderRadius: '20px', border: 'none', cursor: 'pointer', fontWeight: 600,
                            backgroundColor: providerFilter === f ? '#6366f1' : '#f1f5f9',
                            color: providerFilter === f ? '#fff' : '#64748b',
                            transition: 'all 0.2s'
                        }}
                    >
                        {f === 'all' ? t("allProviders") : f === 'china' ? t("chinaProviders") : t("globalProviders")}
                    </button>
                ))}
            </div>

            <div style={{ maxHeight: 'calc(70vh - 180px)', overflowY: 'auto', padding: '2px' }}>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: '8px' }}>
                    {providers.map((provider, index) => {
                        const isSelected = selectedProvider?.name === provider.name && selectedProvider?.url === provider.url;
                        return (
                            <div
                                key={index}
                                onClick={() => setSelectedProvider(provider)}
                                onDoubleClick={() => { setSelectedProvider(provider); onConfirm(); }}
                                onMouseEnter={(e) => {
                                    const rect = e.currentTarget.getBoundingClientRect();
                                    setHoveredProvider({ provider, x: rect.left + rect.width / 2, y: rect.top - 4 });
                                }}
                                onMouseLeave={() => setHoveredProvider(null)}
                                style={{
                                    padding: '10px 8px', borderRadius: '10px', cursor: 'pointer', textAlign: 'center',
                                    border: isSelected ? '2px solid #6366f1' : '1.5px solid #e8ecf1',
                                    backgroundColor: isSelected ? '#eef2ff' : '#fff',
                                    transition: 'all 0.15s ease',
                                    boxShadow: isSelected ? '0 2px 8px rgba(59,130,246,0.15)' : '0 1px 3px rgba(0,0,0,0.04)',
                                    position: 'relative'
                                }}
                            >
                                <div style={{ fontSize: '0.8rem', fontWeight: 600, color: isSelected ? '#6366f1' : '#334155', lineHeight: 1.3, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', display: 'flex', alignItems: 'center', justifyContent: 'center', gap: '3px' }}>
                                    <span title={provider.region === 'china' ? (lang === 'en' ? 'China' : 'China') : (lang === 'en' ? 'Global' : 'Global')} style={{ fontSize: '0.7rem', flexShrink: 0 }}>{provider.region === 'china' ? 'CN' : 'GL'}</span>
                                    <span style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{provider.name}</span>
                                </div>
                            </div>
                        );
                    })}
                </div>
            </div>

            <div style={{ display: 'flex', gap: '10px', marginTop: '14px' }}>
                <button className="btn-primary" style={{ flex: 1, borderRadius: '10px' }} onClick={onConfirm} disabled={!selectedProvider}>{t("confirm")}</button>
                <button className="btn-hide" style={{ flex: 1, borderRadius: '10px' }} onClick={onClose}>{t("cancel")}</button>
            </div>
        </div>

        {hoveredProvider && (
            <div style={{
                position: 'fixed',
                left: hoveredProvider.x,
                top: hoveredProvider.y,
                transform: 'translate(-50%, -100%)',
                backgroundColor: '#1e293b',
                color: '#1f2937',
                padding: '6px 12px',
                borderRadius: '8px',
                fontSize: '0.75rem',
                fontFamily: 'monospace',
                whiteSpace: 'nowrap',
                zIndex: 9999,
                pointerEvents: 'none',
                boxShadow: '0 4px 12px rgba(0,0,0,0.2)',
                maxWidth: '360px',
                overflow: 'hidden',
                textOverflow: 'ellipsis'
            }}>
                {hoveredProvider.provider.url}
                {hoveredProvider.provider.description && (
                    <div style={{ fontSize: '0.65rem', color: '#94a3b8', marginTop: '2px' }}>{hoveredProvider.provider.description}</div>
                )}
                <div style={{
                    position: 'absolute', bottom: '-5px', left: '50%', transform: 'translateX(-50%)',
                    width: 0, height: 0,
                    borderLeft: '6px solid transparent', borderRight: '6px solid transparent', borderTop: '6px solid #1e293b'
                }} />
            </div>
        )}
    </div>
);
