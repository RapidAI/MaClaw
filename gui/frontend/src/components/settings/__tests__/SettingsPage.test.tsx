import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { SettingsPage } from '../SettingsPage';
import type { SettingsTabOption } from '../../../config/settingsTabs';
import type { AssistantDarkSchemeId } from '../../ai/assistantDarkSchemes';
import type { AssistantLightSchemeId } from '../../ai/assistantLightSchemes';

vi.mock('../SettingsActiveContent', () => ({
    SettingsActiveContent: ({ settingsTab }: { settingsTab: string }) => (
        <div data-testid="settings-body" data-tab={settingsTab}>body:{settingsTab}</div>
    ),
}));

vi.mock('../SettingsTabsRail', () => ({
    SettingsTabsRail: ({ activeTab, tabs }: { activeTab: string; tabs: SettingsTabOption[] }) => (
        <div data-testid="settings-rail" data-active={activeTab} data-count={tabs.length}>
            rail
        </div>
    ),
}));

const tabs: SettingsTabOption[] = [
    { id: 'general', label: 'General', desc: 'g', icon: '' },
    { id: 'llm', label: 'LLM', desc: 'l', icon: '' },
];

const baseContentProps = {
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
    darkSchemeId: 'graphite' as AssistantDarkSchemeId,
    setDarkSchemeId: vi.fn(),
    lightSchemeId: 'default' as AssistantLightSchemeId,
    setLightSchemeId: vi.fn(),
};

describe('SettingsPage', () => {
    it('keeps rail and body on the same resolved active tab', () => {
        const { container } = render(
            <SettingsPage
                {...baseContentProps}
                tabs={tabs}
                activeTab="llm"
                onChangeTab={vi.fn()}
            />,
        );
        expect(screen.getByTestId('settings-rail').getAttribute('data-active')).toBe('llm');
        expect(screen.getByTestId('settings-body').getAttribute('data-tab')).toBe('llm');
        expect(screen.getByTestId('settings-rail').getAttribute('data-count')).toBe('2');
        // Stable CSS grid content slot — prevents blank layout when Suspense wraps panels.
        expect(container.querySelector('.settings-shell__body')).toBeTruthy();
    });
});
