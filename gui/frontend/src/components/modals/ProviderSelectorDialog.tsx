import type { CSSProperties, Dispatch, SetStateAction } from 'react';
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
    <div className="modal-overlay provider-selector-overlay" onClick={onClose}>
        <div className="modal-content provider-selector-modal" onClick={(e) => e.stopPropagation()}>
            <h2 className="provider-selector-title">{t("selectProviderTitle")}</h2>

            <div className="provider-selector-filter-row" role="tablist" aria-label={t("selectProviderTitle")}>
                {(['all', 'china', 'global'] as const).map(f => (
                    <button
                        key={f}
                        onClick={() => setProviderFilter(f)}
                        className="provider-selector-filter"
                        data-active={providerFilter === f ? 'true' : 'false'}
                        role="tab"
                        aria-selected={providerFilter === f}
                    >
                        {f === 'all' ? t("allProviders") : f === 'china' ? t("chinaProviders") : t("globalProviders")}
                    </button>
                ))}
            </div>

            <div className="provider-selector-scroll elegant-scrollbar">
                <div className="provider-selector-grid">
                    {providers.map((provider, index) => {
                        const isSelected = selectedProvider?.name === provider.name && selectedProvider?.url === provider.url;
                        return (
                            <button
                                key={index}
                                onClick={() => setSelectedProvider(provider)}
                                onDoubleClick={() => { setSelectedProvider(provider); onConfirm(); }}
                                onMouseEnter={(e) => {
                                    const rect = e.currentTarget.getBoundingClientRect();
                                    setHoveredProvider({ provider, x: rect.left + rect.width / 2, y: rect.top - 4 });
                                }}
                                onMouseLeave={() => setHoveredProvider(null)}
                                className="provider-selector-card"
                                data-selected={isSelected ? 'true' : 'false'}
                                type="button"
                            >
                                <div className="provider-selector-card-title">
                                    <span className="provider-selector-region" title={provider.region === 'china' ? (lang === 'en' ? 'China' : 'China') : (lang === 'en' ? 'Global' : 'Global')}>{provider.region === 'china' ? 'CN' : 'GL'}</span>
                                    <span className="provider-selector-name">{provider.name}</span>
                                </div>
                            </button>
                        );
                    })}
                </div>
            </div>

            <div className="provider-selector-actions">
                <button className="btn-primary provider-selector-action" onClick={onConfirm} disabled={!selectedProvider}>{t("confirm")}</button>
                <button className="btn-hide provider-selector-action" onClick={onClose}>{t("cancel")}</button>
            </div>
        </div>

        {hoveredProvider && (
            <div
                className="provider-selector-tooltip"
                style={{
                    '--provider-selector-tooltip-x': `${hoveredProvider.x}px`,
                    '--provider-selector-tooltip-y': `${hoveredProvider.y}px`,
                } as CSSProperties}
            >
                {hoveredProvider.provider.url}
                {hoveredProvider.provider.description && (
                    <div className="provider-selector-tooltip-desc">{hoveredProvider.provider.description}</div>
                )}
            </div>
        )}
    </div>
);
