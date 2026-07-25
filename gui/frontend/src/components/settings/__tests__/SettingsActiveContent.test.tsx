import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { SettingsActiveContent } from '../SettingsActiveContent';
import type { AssistantDarkSchemeId } from '../../ai/assistantDarkSchemes';
import type { AssistantLightSchemeId } from '../../ai/assistantLightSchemes';

const GetSettingsTabConfigMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetSettingsTabConfig: (...args: unknown[]) => GetSettingsTabConfigMock(...args),
}));

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
    DiarizationConfigPanel: () => <div>Diarization</div>,
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
    // Warm config so mount tests don't block on the per-tab DTO fetch.
    config: { language: 'en' } as any,
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
    uiZoomAuto: true,
    setUiZoomAuto: vi.fn(),
    chatFontSize: 14,
    setChatFontSize: vi.fn(),
    darkSchemeId: 'default' as AssistantDarkSchemeId,
    setDarkSchemeId: vi.fn(),
    lightSchemeId: 'default' as AssistantLightSchemeId,
    setLightSchemeId: vi.fn(),
};

describe('SettingsActiveContent', () => {
    beforeEach(() => {
        GetSettingsTabConfigMock.mockReset();
        GetSettingsTabConfigMock.mockResolvedValue({});
    });

    it('mounts only the active general tab body', async () => {
        render(<SettingsActiveContent {...baseProps} settingsTab="general" />);
        expect(await screen.findByTestId('general-settings')).toBeTruthy();
        expect(screen.getByTestId('general-advanced')).toBeTruthy();
        expect(screen.queryByTestId('llm-settings')).toBeNull();
        expect(screen.queryByTestId('im-settings')).toBeNull();
        await waitFor(() => expect(GetSettingsTabConfigMock).toHaveBeenCalledWith('general'));
    });

    it('switches to the llm tab without keeping general mounted', async () => {
        const { rerender } = render(<SettingsActiveContent {...baseProps} settingsTab="general" />);
        expect(await screen.findByTestId('general-settings')).toBeTruthy();

        rerender(<SettingsActiveContent {...baseProps} settingsTab="llm" />);
        expect(await screen.findByTestId('llm-settings')).toBeTruthy();
        expect(screen.queryByTestId('general-settings')).toBeNull();
        await waitFor(() => expect(GetSettingsTabConfigMock).toHaveBeenCalledWith('llm'));
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
        await waitFor(() => expect(GetSettingsTabConfigMock).toHaveBeenCalledWith('general'));
    });

    it('merges per-tab DTO into setConfig without stomping local edits', async () => {
        const setConfig = vi.fn();
        GetSettingsTabConfigMock.mockResolvedValue({
            default_proxy_host: '10.0.0.1',
            default_proxy_port: '8080',
        });
        render(
            <SettingsActiveContent
                {...baseProps}
                settingsTab="proxy"
                config={{ language: 'en', default_proxy_host: 'stale', default_proxy_port: '1' } as any}
                setConfig={setConfig}
            />,
        );
        await waitFor(() => expect(GetSettingsTabConfigMock).toHaveBeenCalledWith('proxy'));
        await waitFor(() => expect(setConfig).toHaveBeenCalled());
        const updater = setConfig.mock.calls.find((c) => typeof c[0] === 'function')?.[0];
        expect(typeof updater).toBe('function');
        // prev still matches snapshot → apply server values.
        const merged = updater({ language: 'en', default_proxy_host: 'stale', default_proxy_port: '1', projects: [] });
        expect(merged.default_proxy_host).toBe('10.0.0.1');
        expect(merged.default_proxy_port).toBe('8080');
        expect(merged.language).toBe('en');
        // User edited host after snapshot → keep edit.
        const kept = updater({ language: 'en', default_proxy_host: 'typed-by-user', default_proxy_port: '1' });
        expect(kept.default_proxy_host).toBe('typed-by-user');
        expect(kept.default_proxy_port).toBe('8080');
    });

    it('skips GetSettingsTabConfig for self-loading tabs like memory', async () => {
        render(<SettingsActiveContent {...baseProps} settingsTab="memory" />);
        expect(await screen.findByText('Memory')).toBeTruthy();
        // Allow microtasks; must remain uncalled.
        await new Promise((r) => setTimeout(r, 20));
        expect(GetSettingsTabConfigMock).not.toHaveBeenCalled();
    });

    it('does not re-fetch the same tab after it has loaded once', async () => {
        GetSettingsTabConfigMock.mockResolvedValue({ language: 'en' });
        const { rerender } = render(<SettingsActiveContent {...baseProps} settingsTab="general" />);
        await waitFor(() => expect(GetSettingsTabConfigMock).toHaveBeenCalledTimes(1));

        rerender(<SettingsActiveContent {...baseProps} settingsTab="proxy" />);
        await waitFor(() => expect(GetSettingsTabConfigMock).toHaveBeenCalledWith('proxy'));
        const afterProxy = GetSettingsTabConfigMock.mock.calls.length;

        rerender(<SettingsActiveContent {...baseProps} settingsTab="general" />);
        await new Promise((r) => setTimeout(r, 30));
        // Revisit general — cached, no extra call.
        expect(GetSettingsTabConfigMock.mock.calls.length).toBe(afterProxy);
    });

    it('re-fetches the active tab after signal-only maclaw-config-changed', async () => {
        GetSettingsTabConfigMock.mockResolvedValue({ language: 'en' });
        render(<SettingsActiveContent {...baseProps} settingsTab="general" />);
        await waitFor(() => expect(GetSettingsTabConfigMock).toHaveBeenCalledTimes(1));

        // Empty detail = invalidate without a full config payload → re-fetch.
        window.dispatchEvent(new CustomEvent('maclaw-config-changed', { detail: {} }));
        await waitFor(() => expect(GetSettingsTabConfigMock).toHaveBeenCalledTimes(2));
        expect(GetSettingsTabConfigMock).toHaveBeenLastCalledWith('general');
    });

    it('does not re-fetch active tab when config-changed already carries payload', async () => {
        GetSettingsTabConfigMock.mockResolvedValue({ language: 'en' });
        render(<SettingsActiveContent {...baseProps} settingsTab="general" />);
        await waitFor(() => expect(GetSettingsTabConfigMock).toHaveBeenCalledTimes(1));

        // Saver already applied full config via setConfig before this event.
        window.dispatchEvent(new CustomEvent('maclaw-config-changed', {
            detail: { language: 'en', pause_env_check: true },
        }));
        await new Promise((r) => setTimeout(r, 40));
        expect(GetSettingsTabConfigMock).toHaveBeenCalledTimes(1);
    });

    it('re-fetches a different tab after payload event cleared its cache', async () => {
        GetSettingsTabConfigMock.mockResolvedValue({ language: 'en' });
        const { rerender } = render(<SettingsActiveContent {...baseProps} settingsTab="general" />);
        await waitFor(() => expect(GetSettingsTabConfigMock).toHaveBeenCalledWith('general'));

        rerender(<SettingsActiveContent {...baseProps} settingsTab="proxy" />);
        await waitFor(() => expect(GetSettingsTabConfigMock).toHaveBeenCalledWith('proxy'));
        const afterBoth = GetSettingsTabConfigMock.mock.calls.length;

        window.dispatchEvent(new CustomEvent('maclaw-config-changed', {
            detail: { language: 'en' },
        }));
        await new Promise((r) => setTimeout(r, 20));
        // Active tab (proxy) kept warm — no extra call yet.
        expect(GetSettingsTabConfigMock.mock.calls.length).toBe(afterBoth);

        // Leaving and re-entering general should re-fetch (cache was cleared for it).
        rerender(<SettingsActiveContent {...baseProps} settingsTab="general" />);
        await waitFor(() => expect(GetSettingsTabConfigMock.mock.calls.length).toBeGreaterThan(afterBoth));
        expect(GetSettingsTabConfigMock).toHaveBeenLastCalledWith('general');
    });

    it('does not apply a stale tab DTO after unmount', async () => {
        let resolveDto: (v: Record<string, any>) => void = () => {};
        GetSettingsTabConfigMock.mockImplementation(
            () => new Promise((resolve) => {
                resolveDto = resolve;
            }),
        );
        const setConfig = vi.fn();
        const { unmount } = render(
            <SettingsActiveContent
                {...baseProps}
                settingsTab="proxy"
                config={{ language: 'en' } as any}
                setConfig={setConfig}
            />,
        );
        await waitFor(() => expect(GetSettingsTabConfigMock).toHaveBeenCalled());
        unmount();
        resolveDto({ default_proxy_host: 'should-not-apply' });
        await new Promise((r) => setTimeout(r, 30));
        expect(setConfig).not.toHaveBeenCalled();
    });
});
