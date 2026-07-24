import type { CSSProperties, Dispatch, SetStateAction } from 'react';
import type { ProviderEndpoint } from '../../config/providerCatalog';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';

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
    t: (key: string) => string;
    localizeText: (en: string, zhHans: string, zhHant: string) => string;
    onConfirm: (provider?: ProviderEndpoint) => void;
    onClose: () => void;
};

const localizeProviderDescription = (
    description: string | undefined,
    localizeText: (en: string, zhHans: string, zhHant: string) => string,
) => {
    if (!description) return '';
    const descriptions: Record<string, [string, string]> = {
        'Official Claude API': ['Claude 官方 API', 'Claude 官方 API'],
        'Tencent Cloud Claude-compatible endpoint': ['腾讯云 Claude 兼容端点', '騰訊雲 Claude 相容端點'],
        'Official OpenAI API': ['OpenAI 官方 API', 'OpenAI 官方 API'],
        'xAI Grok Build API': ['xAI Grok Build API', 'xAI Grok Build API'],
        'Tencent Cloud OpenAI-compatible endpoint': ['腾讯云 OpenAI 兼容端点', '騰訊雲 OpenAI 相容端點'],
    };
    const localized = descriptions[description];
    return localized ? localizeText(description, localized[0], localized[1]) : description;
};

export const ProviderSelectorDialog = ({
    providers,
    providerFilter,
    setProviderFilter,
    selectedProvider,
    setSelectedProvider,
    hoveredProvider,
    setHoveredProvider,
    t,
    localizeText,
    onConfirm,
    onClose,
}: ProviderSelectorDialogProps) => {
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose);

    return (
    <div className="modal-overlay provider-selector-overlay" {...backdropProps}>
        <div
            className="modal-content provider-selector-modal"
            {...dialogProps}
        >
            <h2 className="provider-selector-title">{t("selectProviderTitle")}</h2>

            <div className="provider-selector-filter-row" role="tablist" aria-label={t("selectProviderTitle")}>
                {(['all', 'china', 'global'] as const).map(f => (
                    <button
                        key={f}
                        onClick={() => {
                            setProviderFilter(f);
                            setSelectedProvider(null);
                            setHoveredProvider(null);
                        }}
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
                    {providers.length === 0 && (
                        <div className="provider-selector-empty">
                            {localizeText('No providers available for this filter', '当前筛选条件下没有可用服务商', '目前篩選條件下沒有可用服務商')}
                        </div>
                    )}
                    {providers.map((provider, index) => {
                        const isSelected = selectedProvider?.name === provider.name && selectedProvider?.url === provider.url;
                        return (
                            <button
                                key={index}
                                onClick={() => setSelectedProvider(provider)}
                                onDoubleClick={() => { setSelectedProvider(provider); onConfirm(provider); }}
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
                                    <span className="provider-selector-region" title={provider.region === 'china' ? t("chinaProviders") : t("globalProviders")}>{provider.region === 'china' ? 'CN' : 'GL'}</span>
                                    <span className="provider-selector-name">{provider.name}</span>
                                </div>
                            </button>
                        );
                    })}
                </div>
            </div>

            <div className="provider-selector-actions">
                <button className="btn-primary provider-selector-action" onClick={() => onConfirm()} disabled={!selectedProvider}>{t("confirm")}</button>
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
                    <div className="provider-selector-tooltip-desc">
                        {localizeProviderDescription(hoveredProvider.provider.description, localizeText)}
                    </div>
                )}
            </div>
        )}
    </div>
    );
};
