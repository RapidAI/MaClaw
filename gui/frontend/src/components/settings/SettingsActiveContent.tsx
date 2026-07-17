import { Suspense, type ChangeEvent, type Dispatch, type ReactNode, type SetStateAction } from 'react';
import { main } from '../../../wailsjs/go/models';
import { resolveSettingsTabId, type SettingsTabId } from '../../config/settingsTabs';
import {
    ASRConfigPanel,
	DiarizationConfigPanel,
    EmbeddingConfigPanel,
    HubServiceRedeemPanel,
    KnowledgeSettingsPanel,
    LLMCacheSettingsPanel,
    LLMConfigPanel,
    MemoryManagementPanel,
    MigrationSettingsPanel,
    MISDataSettingsPanel,
    ProgrammingToolsSettingsPanel,
    ProxySettingsPanel,
    SecurityPolicyPanel,
    SystemSettingsPanel,
    TTSConfigPanel,
    UISettingsPanel,
    VirtualEmployeeSettingsPanel,
    WebSearchConfigPanel,
} from '../../appLazyComponents';
import { PetSettingsPanel } from '../PetSettingsPanel';
import type { AssistantDarkSchemeId } from '../ai/assistantDarkSchemes';
import type { AssistantLightSchemeId } from '../ai/assistantLightSchemes';
import type { VirtualEmployeeEntry } from '../ai/VirtualEmployeeTab';
import { FavoriteEmployeeSettingsPanel } from './FavoriteEmployeeSettingsPanel';
import { GeneralAdvancedSettingsPanel } from './GeneralAdvancedSettingsPanel';
import { GeneralSettingsPanel } from './GeneralSettingsPanel';
import { IMSettingsPanel } from './IMSettingsPanel';
import type { IMSubTab } from './IMSubTabs';
import { SettingsPanelErrorBoundary } from './SettingsPanelErrorBoundary';
import { SettingsPanelFallback } from './SettingsPanelFallback';

type AudioDeviceOption = {
    deviceId: string;
    label: string;
};

type AudioDevicesState = {
    inputs: AudioDeviceOption[];
    outputs: AudioDeviceOption[];
    labelsAvailable: boolean;
    requestLabels: () => void;
};

export type SettingsActiveContentProps = {
    settingsTab: SettingsTabId;
    lang: string;
    t: (key: string) => string;
    localizeText: (en: string, zhHans: string, zhHant: string) => string;
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    onLanguageChange: (event: ChangeEvent<HTMLSelectElement>) => void;
    hasWindowsTerminal: boolean;
    envCheckInterval: number;
    setEnvCheckInterval: Dispatch<SetStateAction<number>>;
    isWindows: boolean;
    patchConfigFields: (patch: Record<string, any>) => Promise<any>;
    onLLMStatusChange: (online: boolean, configured: boolean) => void;
    onProviderChanged?: () => void;
    showToastMessage: (message: string, duration?: number) => void;
    memoryTraceFocus: { value: string; seq: number };
    imSubTab: IMSubTab;
    setImSubTab: Dispatch<SetStateAction<IMSubTab>>;
    imAuditPlatform: string | null;
    setIMAuditPlatform: Dispatch<SetStateAction<string | null>>;
    saveRemoteConfigField: (patch: Record<string, any>) => any;
    qqBotStatus: string;
    setQQBotStatus: Dispatch<SetStateAction<string>>;
    qqBotLocalMode: boolean;
    setQQBotLocalModeState: Dispatch<SetStateAction<boolean>>;
    telegramStatus: string;
    setTelegramStatus: Dispatch<SetStateAction<string>>;
    telegramLocalMode: boolean;
    setTelegramLocalModeState: Dispatch<SetStateAction<boolean>>;
    weixinStatus: string;
    setWeixinStatus: Dispatch<SetStateAction<string>>;
    weixinLocalMode: boolean;
    setWeixinLocalModeState: Dispatch<SetStateAction<boolean>>;
    thirdPartyGatewayStatus: string;
    setThirdPartyGatewayStatus: Dispatch<SetStateAction<string>>;
    thirdPartyGatewayLocalMode: boolean;
    setThirdPartyGatewayLocalModeState: Dispatch<SetStateAction<boolean>>;
    showLansenger: boolean;
    lansengerStatus: string;
    setLansengerStatus: Dispatch<SetStateAction<string>>;
    lansengerLocalMode: boolean;
    setLansengerLocalModeState: Dispatch<SetStateAction<boolean>>;
    weixinQRCode: string;
    setWeixinQRCode: Dispatch<SetStateAction<string>>;
    weixinQRLoading: boolean;
    setWeixinQRLoading: Dispatch<SetStateAction<boolean>>;
    weixinQRWaiting: boolean;
    setWeixinQRWaiting: Dispatch<SetStateAction<boolean>>;
    weixinQRError: string;
    setWeixinQRError: Dispatch<SetStateAction<string>>;
    veNavigationAvailable: boolean;
    veSettingsAuthorized: boolean;
    virtualEmployeeLayoutClassName: string;
    userFavoriteEmployeeIds: string[];
    veList: VirtualEmployeeEntry[];
    onAddFavoriteEmployee: (veId: string) => void;
    onRemoveFavoriteEmployee: (veId: string) => void;
    onReorderFavorites: (ids: string[]) => void;
    audioDevices: AudioDevicesState;
    uiZoom: number;
    setUiZoom: Dispatch<SetStateAction<number>>;
    uiZoomAuto: boolean;
    setUiZoomAuto: Dispatch<SetStateAction<boolean>>;
    chatFontSize: number;
    setChatFontSize: Dispatch<SetStateAction<number>>;
    darkSchemeId: AssistantDarkSchemeId;
    setDarkSchemeId: (id: AssistantDarkSchemeId) => void;
    lightSchemeId: AssistantLightSchemeId;
    setLightSchemeId: (id: AssistantLightSchemeId) => void;
};

function wrapPanel(className: string, children: ReactNode) {
    return <div className={className}>{children}</div>;
}

/**
 * Renders only the active settings tab body.
 *
 * - Default "general" panels are eager (no Suspense) so opening Settings is never a
 *   blank lazy-chunk race — the common OEM intermittent blank.
 * - Other panels stay React.lazy; Suspense only wraps those so the rail stays visible.
 * - Active tab is resolved at render time so hidden tabs (e.g. virtualEmployee)
 *   never produce an empty body for a frame.
 */
export function SettingsActiveContent(props: SettingsActiveContentProps) {
    const {
        settingsTab,
        lang,
        t,
        localizeText,
        config,
        setConfig,
        onLanguageChange,
        hasWindowsTerminal,
        envCheckInterval,
        setEnvCheckInterval,
        isWindows,
        patchConfigFields,
        onLLMStatusChange,
        onProviderChanged,
        showToastMessage,
        memoryTraceFocus,
        imSubTab,
        setImSubTab,
        imAuditPlatform,
        setIMAuditPlatform,
        saveRemoteConfigField,
        qqBotStatus,
        setQQBotStatus,
        qqBotLocalMode,
        setQQBotLocalModeState,
        telegramStatus,
        setTelegramStatus,
        telegramLocalMode,
        setTelegramLocalModeState,
        weixinStatus,
        setWeixinStatus,
        weixinLocalMode,
        setWeixinLocalModeState,
        thirdPartyGatewayStatus,
        setThirdPartyGatewayStatus,
        thirdPartyGatewayLocalMode,
        setThirdPartyGatewayLocalModeState,
        showLansenger,
        lansengerStatus,
        setLansengerStatus,
        lansengerLocalMode,
        setLansengerLocalModeState,
        weixinQRCode,
        setWeixinQRCode,
        weixinQRLoading,
        setWeixinQRLoading,
        weixinQRWaiting,
        setWeixinQRWaiting,
        weixinQRError,
        setWeixinQRError,
        veNavigationAvailable,
        veSettingsAuthorized,
        virtualEmployeeLayoutClassName,
        userFavoriteEmployeeIds,
        veList,
        onAddFavoriteEmployee,
        onRemoveFavoriteEmployee,
        onReorderFavorites,
        audioDevices,
        uiZoom,
        setUiZoom,
        uiZoomAuto,
        setUiZoomAuto,
        chatFontSize,
        setChatFontSize,
        darkSchemeId,
        setDarkSchemeId,
        lightSchemeId,
        setLightSchemeId,
    } = props;

    // Resolve before switch so invalid / currently-hidden tabs never paint an empty body.
    const activeTab = resolveSettingsTabId(settingsTab, { hideVirtualEmployee: !veNavigationAvailable });

    const renderGeneral = () => wrapPanel('settings-content settings-content--stacked', (
        <>
            <GeneralSettingsPanel
                config={config}
                setConfig={setConfig}
                lang={lang}
                t={t}
                onLanguageChange={onLanguageChange}
            />
            <GeneralAdvancedSettingsPanel
                config={config}
                setConfig={setConfig}
                lang={lang}
                t={t}
                hasWindowsTerminal={hasWindowsTerminal}
                envCheckInterval={envCheckInterval}
                setEnvCheckInterval={setEnvCheckInterval}
            />
        </>
    ));

    // Eager path: opening Settings lands here most of the time — no lazy suspend.
    if (activeTab === 'general') {
        return (
            <SettingsPanelErrorBoundary lang={lang} resetKey="general">
                {renderGeneral()}
            </SettingsPanelErrorBoundary>
        );
    }

    let body: ReactNode = null;
    switch (activeTab) {
        case 'searchEngine':
            body = wrapPanel('settings-content settings-panel', <WebSearchConfigPanel lang={lang} />);
            break;
        case 'pet':
            body = wrapPanel('settings-content settings-panel', (
                <PetSettingsPanel
                    config={config!}
                    lang={lang}
                    setConfig={setConfig}
                    patchConfig={patchConfigFields}
                />
            ));
            break;
        case 'proxy':
            body = wrapPanel('settings-content', (
                <ProxySettingsPanel
                    config={config}
                    setConfig={setConfig}
                    isWindows={isWindows}
                    lang={lang}
                    t={t}
                />
            ));
            break;
        case 'llm':
            body = wrapPanel('settings-content settings-panel', (
                <LLMConfigPanel
                    lang={lang}
                    codexModels={config?.codex?.models}
                    onStatusChange={onLLMStatusChange}
                    onProviderChanged={onProviderChanged}
                />
            ));
            break;
        case 'llmCache':
            body = wrapPanel('settings-content settings-panel', (
                <LLMCacheSettingsPanel config={config} setConfig={setConfig} lang={lang} showToastMessage={showToastMessage} />
            ));
            break;
        case 'redeem':
            body = wrapPanel('settings-content settings-panel', <HubServiceRedeemPanel lang={lang} />);
            break;
        case 'memory':
            body = wrapPanel('settings-content settings-panel', (
                <MemoryManagementPanel lang={lang} traceFocus={memoryTraceFocus} />
            ));
            break;
        case 'knowledge':
            body = wrapPanel('settings-content settings-panel', (
                <KnowledgeSettingsPanel lang={lang} showToastMessage={showToastMessage} />
            ));
            break;
        case 'misData':
            body = wrapPanel('settings-content settings-panel', <MISDataSettingsPanel lang={lang} />);
            break;
        case 'embedding':
            body = wrapPanel('settings-content settings-panel', (
                <>
                    <EmbeddingConfigPanel lang={lang} />
                    <ASRConfigPanel lang={lang} />
					<DiarizationConfigPanel lang={lang} />
                    <TTSConfigPanel lang={lang} />
                </>
            ));
            break;
        case 'im':
            body = (
                <IMSettingsPanel
                    config={config}
                    setConfig={setConfig}
                    lang={lang}
                    imSubTab={imSubTab}
                    setImSubTab={setImSubTab}
                    imAuditPlatform={imAuditPlatform}
                    setIMAuditPlatform={setIMAuditPlatform}
                    saveRemoteConfigField={saveRemoteConfigField}
                    showToastMessage={showToastMessage}
                    qqBotStatus={qqBotStatus}
                    setQQBotStatus={setQQBotStatus}
                    qqBotLocalMode={qqBotLocalMode}
                    setQQBotLocalModeState={setQQBotLocalModeState}
                    telegramStatus={telegramStatus}
                    setTelegramStatus={setTelegramStatus}
                    telegramLocalMode={telegramLocalMode}
                    setTelegramLocalModeState={setTelegramLocalModeState}
                    weixinStatus={weixinStatus}
                    setWeixinStatus={setWeixinStatus}
                    weixinLocalMode={weixinLocalMode}
                    setWeixinLocalModeState={setWeixinLocalModeState}
                    thirdPartyGatewayStatus={thirdPartyGatewayStatus}
                    setThirdPartyGatewayStatus={setThirdPartyGatewayStatus}
                    thirdPartyGatewayLocalMode={thirdPartyGatewayLocalMode}
                    setThirdPartyGatewayLocalModeState={setThirdPartyGatewayLocalModeState}
                    showLansenger={showLansenger}
                    lansengerStatus={lansengerStatus}
                    setLansengerStatus={setLansengerStatus}
                    lansengerLocalMode={lansengerLocalMode}
                    setLansengerLocalModeState={setLansengerLocalModeState}
                    weixinQRCode={weixinQRCode}
                    setWeixinQRCode={setWeixinQRCode}
                    weixinQRLoading={weixinQRLoading}
                    setWeixinQRLoading={setWeixinQRLoading}
                    weixinQRWaiting={weixinQRWaiting}
                    setWeixinQRWaiting={setWeixinQRWaiting}
                    weixinQRError={weixinQRError}
                    setWeixinQRError={setWeixinQRError}
                />
            );
            break;
        case 'security':
            body = wrapPanel('settings-content settings-panel', (
                <SecurityPolicyPanel config={config} saveRemoteConfigField={saveRemoteConfigField} lang={lang} />
            ));
            break;
        case 'migration':
            body = wrapPanel('settings-content', (
                <MigrationSettingsPanel lang={lang} showToastMessage={showToastMessage} />
            ));
            break;
        case 'virtualEmployee':
            body = wrapPanel('settings-content settings-panel settings-content--virtual-employee', (
                <div className={virtualEmployeeLayoutClassName}>
                    {veSettingsAuthorized && (
                        <div className="settings-ve-section settings-ve-section--primary">
                            <VirtualEmployeeSettingsPanel remoteMachineId={config?.remote_machine_id || ''} lang={lang} />
                        </div>
                    )}
                    <div className="settings-ve-section settings-ve-section--side">
                        <FavoriteEmployeeSettingsPanel
                            favoriteEmployeeIds={userFavoriteEmployeeIds}
                            veList={veList}
                            onAdd={onAddFavoriteEmployee}
                            onRemove={onRemoveFavoriteEmployee}
                            onReorder={onReorderFavorites}
                            lang={lang}
                        />
                    </div>
                </div>
            ));
            break;
        case 'system':
            body = wrapPanel('settings-content', (
                <SystemSettingsPanel
                    config={config}
                    setConfig={setConfig}
                    lang={lang}
                    audioDevices={audioDevices}
                    saveRemoteConfigField={saveRemoteConfigField}
                    showToastMessage={showToastMessage}
                />
            ));
            break;
        case 'ui':
            body = wrapPanel('settings-content', (
                <UISettingsPanel
                    config={config}
                    lang={lang}
                    t={t}
                    uiZoom={uiZoom}
                    setUiZoom={setUiZoom}
                    uiZoomAuto={uiZoomAuto}
                    setUiZoomAuto={setUiZoomAuto}
                    chatFontSize={chatFontSize}
                    setChatFontSize={setChatFontSize}
                    darkSchemeId={darkSchemeId}
                    setDarkSchemeId={setDarkSchemeId}
                    lightSchemeId={lightSchemeId}
                    setLightSchemeId={setLightSchemeId}
                />
            ));
            break;
        case 'display':
            body = wrapPanel('settings-content', (
                <ProgrammingToolsSettingsPanel
                    config={config}
                    setConfig={setConfig}
                    lang={lang}
                />
            ));
            break;
        default:
            // Never paint an empty settings body — fall back to the eager general panel.
            body = renderGeneral();
            break;
    }

    // Defensive: if a branch forgot to assign body, still show general (never blank).
    if (!body) body = renderGeneral();

    return (
        <SettingsPanelErrorBoundary lang={lang} resetKey={activeTab}>
            <Suspense
                fallback={
                    <SettingsPanelFallback
                        message={localizeText('Loading settings…', '\u6b63\u5728\u52a0\u8f7d\u8bbe\u7f6e\u2026', '\u6b63\u5728\u8f09\u5165\u8a2d\u5b9a\u2026')}
                    />
                }
            >
                {body}
            </Suspense>
        </SettingsPanelErrorBoundary>
    );
}
