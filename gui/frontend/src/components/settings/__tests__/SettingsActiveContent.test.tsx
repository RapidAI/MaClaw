import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { SettingsActiveContent } from '../SettingsActiveContent';
import type { AssistantDarkSchemeId } from '../../ai/assistantDarkSchemes';
import type { AssistantLightSchemeId } from '../../ai/assistantLightSchemes';

vi.mock('../GeneralSettingsPanel', () => ({
    GeneralSettingsPanel: () => <div data-testid="general-settings">General</div>,
}));

vi.mock('../GeneralAdvancedSettingsPanel', () => ({
    GeneralAdvancedSettingsPanel: () => <div data-testid="general-advanced">Advanced</div>,
}));

vi.mock('../../../appLazyComponents', () => ({
    WebSearchConfigPanel: () => <div>WebSearch</div>,
    SecurityPolicyPanel: () => <div>Security</div>,
    LLMConfigPanel: () => <div data-testid="llm-settings">LLM</div>,
    HubServiceRedeemPanel: () => <div>Redeem</div>,
    EmbeddingConfigPanel: () => <div>Embedding</div>,
    ASRConfigPanel: () => <div>ASR</div>,
    TTSConfigPanel: () => <div>TTS</div>,
    MemoryManagementPanel: () => <div>Memory</div>,
    KnowledgeSettingsPanel: () => <div>Knowledge</div>,
    MISDataSettingsPanel: () => <div>MIS</div>,
    UISettingsPanel: () => <div>UI</div>,
    ProgrammingToolsSettingsPanel: () => <div>Display</div>,
    SystemSettingsPanel: () => <div>System</div>,
    MigrationSettingsPanel: () => <div>Migration</div>,
    ProxySettingsPanel: () => <div>Proxy</div>,
    LLMCacheSettingsPanel: () => <div>Cache</div>,
    VirtualEmployeeSettingsPanel: () => <div>VE</div>,
}));

vi.mock('../../PetSettingsPanel', () => ({
    PetSettingsPanel: () => <div>Pet</div>,
}));

vi.mock('../IMSettingsPanel', () => ({
    IMSettingsPanel: () => <div data-testid="im-settings">IM</div>,
}));

vi.mock('../FavoriteEmployeeSettingsPanel', () => ({
    FavoriteEmployeeSettingsPanel: () => <div>Favorites</div>,
}));

const baseProps = {
    lang: 'en',
    t: (key: string) => key,
    localizeText: (en: string) => en,
    config: null,
    setConfig: vi.fn(),
    onLanguageChange: vi.fn(),
    hasWindowsTerminal: false,
    envCheckInterval: 0,
    setEnvCheckInterval: vi.fn(),
    isWindows: false,
    patchConfigFields: vi.fn(async () => ({})),
    onLLMStatusChange: vi.fn(),
    showToastMessage: vi.fn(),
    memoryTraceFocus: { value: '', seq: 0 },
    imSubTab: 'qq' as const,
    setImSubTab: vi.fn(),
    imAuditPlatform: null,
    setIMAuditPlatform: vi.fn(),
    saveRemoteConfigField: vi.fn(),
    qqBotStatus: 'disconnected',
    setQQBotStatus: vi.fn(),
    qqBotLocalMode: true,
    setQQBotLocalModeState: vi.fn(),
    telegramStatus: 'disconnected',
    setTelegramStatus: vi.fn(),
    telegramLocalMode: true,
    setTelegramLocalModeState: vi.fn(),
    weixinStatus: 'disconnected',
    setWeixinStatus: vi.fn(),
    weixinLocalMode: true,
    setWeixinLocalModeState: vi.fn(),
    thirdPartyGatewayStatus: 'disconnected',
    setThirdPartyGatewayStatus: vi.fn(),
    thirdPartyGatewayLocalMode: true,
    setThirdPartyGatewayLocalModeState: vi.fn(),
    showLansenger: false,
    lansengerStatus: 'disconnected',
    setLansengerStatus: vi.fn(),
    lansengerLocalMode: true,
    setLansengerLocalModeState: vi.fn(),
    weixinQRCode: '',
    setWeixinQRCode: vi.fn(),
    weixinQRLoading: false,
    setWeixinQRLoading: vi.fn(),
    weixinQRWaiting: false,
    setWeixinQRWaiting: vi.fn(),
    weixinQRError: '',
    setWeixinQRError: vi.fn(),
    veNavigationAvailable: false,
    veSettingsAuthorized: false,
    virtualEmployeeLayoutClassName: 'settings-ve-layout',
    userFavoriteEmployeeIds: [] as string[],
    veList: [] as any[],
    onAddFavoriteEmployee: vi.fn(),
    onRemoveFavoriteEmployee: vi.fn(),
    onReorderFavorites: vi.fn(),
    audioDevices: {
        inputs: [],
        outputs: [],
        labelsAvailable: false,
        requestLabels: vi.fn(),
    },
    uiZoom: 1,
    setUiZoom: vi.fn(),
    chatFontSize: 14,
    setChatFontSize: vi.fn(),
    darkSchemeId: 'default' as AssistantDarkSchemeId,
    setDarkSchemeId: vi.fn(),
    lightSchemeId: 'default' as AssistantLightSchemeId,
    setLightSchemeId: vi.fn(),
};

describe('SettingsActiveContent', () => {
    it('mounts only the active general tab body', async () => {
        render(<SettingsActiveContent {...baseProps} settingsTab="general" />);
        expect(await screen.findByTestId('general-settings')).toBeTruthy();
        expect(screen.getByTestId('general-advanced')).toBeTruthy();
        expect(screen.queryByTestId('llm-settings')).toBeNull();
        expect(screen.queryByTestId('im-settings')).toBeNull();
    });

    it('switches to the llm tab without keeping general mounted', async () => {
        const { rerender } = render(<SettingsActiveContent {...baseProps} settingsTab="general" />);
        expect(await screen.findByTestId('general-settings')).toBeTruthy();

        rerender(<SettingsActiveContent {...baseProps} settingsTab="llm" />);
        expect(await screen.findByTestId('llm-settings')).toBeTruthy();
        expect(screen.queryByTestId('general-settings')).toBeNull();
    });

    it('falls back to general when virtualEmployee is requested but unavailable', async () => {
        render(
            <SettingsActiveContent
                {...baseProps}
                settingsTab="virtualEmployee"
                veNavigationAvailable={false}
            />,
        );
        expect(await screen.findByTestId('general-settings')).toBeTruthy();
    });
});
