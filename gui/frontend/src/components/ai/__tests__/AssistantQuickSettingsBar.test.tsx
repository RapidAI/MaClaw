// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup } from '@testing-library/react';
import { AssistantQuickSettingsBar } from '../AssistantQuickSettingsBar';
import { overlayTheme } from '../aiAssistantPanelTheme';
import { LoadConfig, PatchConfigFields } from '../../../../wailsjs/go/main/App';

let rectSpy: ReturnType<typeof vi.spyOn> | undefined;

afterEach(() => {
    cleanup();
    rectSpy?.mockRestore();
    rectSpy = undefined;
});

vi.mock('../../../../wailsjs/go/main/App', () => ({
    LoadConfig: vi.fn(),
    PatchConfigFields: vi.fn(),
}));

const loadConfigMock = vi.mocked(LoadConfig);
const patchConfigFieldsMock = vi.mocked(PatchConfigFields);

const renderBar = (overrides: Partial<Parameters<typeof AssistantQuickSettingsBar>[0]> = {}) => {
    const props: Parameters<typeof AssistantQuickSettingsBar>[0] = {
        lang: 'zh-Hans',
        theme: overlayTheme,
        themeMode: 'light',
        onToggleTheme: vi.fn(),
        workflowEnabled: false,
        onToggleWorkflow: vi.fn(),
        ttsEnabled: false,
        ttsPlaying: false,
        onToggleTts: vi.fn(),
        onLanguageChange: vi.fn(),
        ...overrides,
    };
    return { props, ...render(<AssistantQuickSettingsBar {...props} />) };
};

describe('AssistantQuickSettingsBar', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        // Portal menu uses getBoundingClientRect; give the chip a non-zero box in jsdom.
        rectSpy = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
            x: 12, y: 600, top: 600, left: 12, bottom: 620, right: 120, width: 108, height: 20, toJSON: () => ({}),
        } as DOMRect);
        loadConfigMock.mockResolvedValue({
            workstation_mode: true,
            log_detail_enabled: false,
            llm_prompt_cache: { enabled: true },
        } as any);
        patchConfigFieldsMock.mockResolvedValue({} as any);
    });

    it('renders all quick settings chips', async () => {
        renderBar();
        expect(screen.getByTestId('assistant-quick-settings-bar')).toBeTruthy();
        expect(screen.getByTestId('assistant-quick-settings-chips')).toBeTruthy();
        expect(screen.getByTestId('qs-workflow-toggle')).toBeTruthy();
        expect(screen.getByTestId('qs-tts-toggle')).toBeTruthy();
        expect(screen.getByTestId('qs-theme-toggle')).toBeTruthy();
        expect(screen.getByTestId('qs-workstation-toggle')).toBeTruthy();
        expect(screen.getByTestId('qs-logdetail-toggle')).toBeTruthy();
        expect(screen.getByTestId('qs-llmcache-toggle')).toBeTruthy();
        expect(screen.getByTestId('qs-lang-toggle')).toBeTruthy();
        // Initial config values are loaded on mount.
        await waitFor(() => {
            expect(screen.getByTestId('qs-workstation-toggle').getAttribute('aria-checked')).toBe('true');
            expect(screen.getByTestId('qs-llmcache-toggle').getAttribute('aria-checked')).toBe('true');
        });
    });

    it('places an optional status slot on the same row as the chips', () => {
        renderBar({
            statusSlot: <span data-testid="inline-status-probe">status-here</span>,
        });
        const bar = screen.getByTestId('assistant-quick-settings-bar');
        const chips = screen.getByTestId('assistant-quick-settings-chips');
        const status = screen.getByTestId('inline-status-probe');
        expect(bar.contains(chips)).toBe(true);
        expect(bar.contains(status)).toBe(true);
        expect(status.textContent).toBe('status-here');
        // Chips scroll; status stays a sibling of the chips scroller (pinned right).
        expect(chips.contains(status)).toBe(false);
        expect(status.parentElement).toBe(bar);
    });

    it('invokes the theme callback when the theme chip is clicked', () => {
        const { props } = renderBar();
        fireEvent.click(screen.getByTestId('qs-theme-toggle'));
        expect(props.onToggleTheme).toHaveBeenCalledTimes(1);
    });

    it('cycles language zh-Hans -> en when the language chip is clicked', () => {
        const { props } = renderBar();
        fireEvent.click(screen.getByTestId('qs-lang-toggle'));
        expect(props.onLanguageChange).toHaveBeenCalledWith('en');
    });

    it('patches config when the log detail switch is toggled', async () => {
        renderBar();
        await waitFor(() => expect(loadConfigMock).toHaveBeenCalled());
        const toggle = screen.getByTestId('qs-logdetail-toggle');
        expect(toggle.getAttribute('aria-checked')).toBe('false');
        fireEvent.click(toggle);
        expect(toggle.getAttribute('aria-checked')).toBe('true');
        expect(patchConfigFieldsMock).toHaveBeenCalledWith({ log_detail_enabled: true });
    });

    it('keeps other llm_prompt_cache fields when toggling the cache switch', async () => {
        loadConfigMock.mockResolvedValue({
            workstation_mode: false,
            log_detail_enabled: false,
            llm_prompt_cache: { enabled: false, openai_enabled: true, ttl_seconds: 300 },
        } as any);
        renderBar();
        await waitFor(() => expect(loadConfigMock).toHaveBeenCalled());
        fireEvent.click(screen.getByTestId('qs-llmcache-toggle'));
        expect(patchConfigFieldsMock).toHaveBeenCalledWith({
            llm_prompt_cache: { enabled: true, openai_enabled: true, ttl_seconds: 300 },
        });
    });

    it('shows provider and model entries in the model menu', () => {
        const onOpenModelMenu = vi.fn();
        renderBar({
            availableProviders: [
                { name: 'hub-official', url: '', isHubService: true, configured: true },
                { name: 'openai-custom', url: 'https://api.example.com', isHubService: false, configured: true },
            ],
            currentModel: 'gpt-5',
            modelOptions: ['gpt-5', 'gpt-4o'],
            onSwitchProvider: vi.fn(),
            onSwitchModel: vi.fn(),
            onOpenModelMenu,
        });
        const chip = screen.getByTestId('qs-model-chip');
        expect(chip.textContent).toContain('gpt-5');
        fireEvent.click(chip);
        expect(onOpenModelMenu).toHaveBeenCalledTimes(1);
        expect(screen.getByRole('listbox')).toBeTruthy();
        expect(screen.getByTestId('qs-model-menu')).toBeTruthy();
        expect(screen.getByText('openai-custom')).toBeTruthy();
        expect(screen.getByText('gpt-4o')).toBeTruthy();
    });

    it('keeps the model chip neutral (not green ON style) when the menu is open', () => {
        renderBar({
            availableProviders: [{ name: 'xAI-Grok', url: '', isHubService: false, configured: true }],
            currentModel: 'grok-4.5',
            modelOptions: ['grok-4.5', 'grok-3'],
            onSwitchModel: vi.fn(),
        });
        const chip = screen.getByTestId('qs-model-chip');
        fireEvent.click(chip);
        expect(screen.getByRole('listbox')).toBeTruthy();
        // Must not reuse the boolean-switch green active palette.
        expect(chip.style.color).not.toBe('rgb(79, 127, 111)');
        expect(chip.style.background).not.toContain('79, 127, 111');
        expect(chip.style.borderColor || chip.style.border).not.toContain('79, 127, 111');
    });

    it('portals the model menu to document.body so parent overflow cannot clip it', () => {
        renderBar({
            availableProviders: [{ name: 'xAI-Grok', url: '', isHubService: false, configured: true }],
            currentModel: 'grok-4.5',
            modelOptions: ['grok-4.5', 'grok-3'],
            onSwitchModel: vi.fn(),
        });
        fireEvent.click(screen.getByTestId('qs-model-chip'));
        const menu = screen.getByTestId('qs-model-menu');
        expect(menu.parentElement).toBe(document.body);
        expect(menu.style.position).toBe('fixed');
    });

    it('closes the portaled model menu when app navigation hides the retained panel', () => {
        const { rerender } = renderBar({
            availableProviders: [{ name: 'xAI-Grok', url: '', isHubService: false, configured: true }],
            currentModel: 'grok-4.5',
            modelOptions: ['grok-4.5', 'grok-3'],
            onSwitchModel: vi.fn(),
        });
        fireEvent.click(screen.getByTestId('qs-model-chip'));
        expect(screen.getByRole('listbox')).toBeTruthy();

        rerender(
            <AssistantQuickSettingsBar
                active={false}
                lang="zh-Hans"
                theme={overlayTheme}
                themeMode="light"
                onToggleTheme={vi.fn()}
                workflowEnabled={false}
                onToggleWorkflow={vi.fn()}
                ttsEnabled={false}
                ttsPlaying={false}
                onToggleTts={vi.fn()}
                availableProviders={[{ name: 'xAI-Grok', url: '', isHubService: false, configured: true }]}
                currentModel="grok-4.5"
                modelOptions={['grok-4.5', 'grok-3']}
                onSwitchModel={vi.fn()}
                onLanguageChange={vi.fn()}
            />,
        );

        expect(screen.queryByRole('listbox')).toBeNull();
    });

    it('shows only the model list when there is a single provider', () => {
        renderBar({
            availableProviders: [{ name: 'xAI-Grok', url: '', isHubService: false, configured: true }],
            currentModel: 'grok-4.5',
            modelOptions: ['grok-4.5', 'grok-3'],
            onSwitchProvider: vi.fn(),
            onSwitchModel: vi.fn(),
        });
        fireEvent.click(screen.getByTestId('qs-model-chip'));
        // Default bar lang is zh-Hans.
        expect(screen.getByText('模型')).toBeTruthy();
        expect(screen.getByText('grok-3')).toBeTruthy();
        expect(screen.queryByText('服务商')).toBeNull();
        expect(screen.queryByText('xAI-Grok')).toBeNull();
    });

    it('switches model when a list item is clicked', () => {
        const onSwitchModel = vi.fn();
        renderBar({
            availableProviders: [{ name: 'xAI-Grok', url: '', isHubService: false, configured: true }],
            currentModel: 'grok-4.5',
            modelOptions: ['grok-4.5', 'grok-3'],
            onSwitchModel,
        });
        fireEvent.click(screen.getByTestId('qs-model-chip'));
        fireEvent.click(screen.getByText('grok-3'));
        expect(onSwitchModel).toHaveBeenCalledWith('grok-3');
        expect(screen.queryByRole('listbox')).toBeNull();
    });

    it('does not re-fire switch when the current model row is clicked', () => {
        const onSwitchModel = vi.fn();
        renderBar({
            availableProviders: [{ name: 'xAI-Grok', url: '', isHubService: false, configured: true }],
            currentModel: 'grok-4.5',
            modelOptions: ['grok-4.5', 'grok-3'],
            onSwitchModel,
        });
        fireEvent.click(screen.getByTestId('qs-model-chip'));
        // Current row is the selected option; click should only dismiss the menu.
        fireEvent.click(screen.getByRole('option', { selected: true }));
        expect(onSwitchModel).not.toHaveBeenCalled();
        expect(screen.queryByRole('listbox')).toBeNull();
    });

    it('uses the model name as the accessible name (not a generic switch label)', () => {
        renderBar({
            availableProviders: [{ name: 'xAI-Grok', url: '', isHubService: false, configured: true }],
            currentModel: 'grok-4.5',
            modelOptions: ['grok-4.5'],
            onSwitchModel: vi.fn(),
        });
        const chip = screen.getByTestId('qs-model-chip');
        // No generic aria-label override — SR name comes from visible model text.
        expect(chip.getAttribute('aria-label')).toBeNull();
        expect(chip.textContent || '').toContain('grok-4.5');
        expect(screen.getByRole('button', { name: /grok-4\.5/ })).toBe(chip);
    });

    it('closes the menu when model picker data disappears', () => {
        const { rerender } = renderBar({
            availableProviders: [{ name: 'xAI-Grok', url: '', isHubService: false, configured: true }],
            currentModel: 'grok-4.5',
            modelOptions: ['grok-4.5'],
            onSwitchModel: vi.fn(),
        });
        fireEvent.click(screen.getByTestId('qs-model-chip'));
        expect(screen.getByRole('listbox')).toBeTruthy();
        rerender(
            <AssistantQuickSettingsBar
                lang="zh-Hans"
                theme={overlayTheme}
                themeMode="light"
                onToggleTheme={vi.fn()}
                workflowEnabled={false}
                onToggleWorkflow={vi.fn()}
                ttsEnabled={false}
                ttsPlaying={false}
                onToggleTts={vi.fn()}
                onLanguageChange={vi.fn()}
            />,
        );
        expect(screen.queryByRole('listbox')).toBeNull();
        expect(screen.queryByTestId('qs-model-chip')).toBeNull();
    });

    it('re-syncs switches when maclaw-config-changed is dispatched (e.g. from settings panels)', async () => {
        renderBar();
        await waitFor(() => expect(loadConfigMock).toHaveBeenCalled());
        const logToggle = screen.getByTestId('qs-logdetail-toggle');
        const workstationToggle = screen.getByTestId('qs-workstation-toggle');
        await waitFor(() => {
            expect(logToggle.getAttribute('aria-checked')).toBe('false');
            expect(workstationToggle.getAttribute('aria-checked')).toBe('true');
        });
        // Simulate the settings page flipping the same fields.
        fireEvent(window, new CustomEvent('maclaw-config-changed', {
            detail: { workstation_mode: false, log_detail_enabled: true, llm_prompt_cache: { enabled: false } },
        }));
        await waitFor(() => {
            expect(logToggle.getAttribute('aria-checked')).toBe('true');
            expect(workstationToggle.getAttribute('aria-checked')).toBe('false');
            expect(screen.getByTestId('qs-llmcache-toggle').getAttribute('aria-checked')).toBe('false');
        });
    });

    it('ignores out-of-order patch responses so the latest click wins', async () => {
        let resolveFirst!: (v: any) => void;
        let resolveSecond!: (v: any) => void;
        patchConfigFieldsMock
            .mockImplementationOnce(() => new Promise((r) => { resolveFirst = r; }))
            .mockImplementationOnce(() => new Promise((r) => { resolveSecond = r; }));
        renderBar();
        await waitFor(() => expect(loadConfigMock).toHaveBeenCalled());
        const toggle = screen.getByTestId('qs-logdetail-toggle');
        // Two rapid toggles: off->on (first request), on->off (second request).
        fireEvent.click(toggle);
        fireEvent.click(toggle);
        expect(patchConfigFieldsMock).toHaveBeenNthCalledWith(1, { log_detail_enabled: true });
        expect(patchConfigFieldsMock).toHaveBeenNthCalledWith(2, { log_detail_enabled: false });
        // Second (latest) response resolves first.
        resolveSecond({ log_detail_enabled: false });
        await waitFor(() => expect(toggle.getAttribute('aria-checked')).toBe('false'));
        // Stale first response arrives last; it must not flip the chip back on.
        resolveFirst({ log_detail_enabled: true });
        await new Promise((r) => setTimeout(r, 0));
        expect(toggle.getAttribute('aria-checked')).toBe('false');
    });

    it('closes the model menu on Escape', () => {
        renderBar({
            availableProviders: [{ name: 'hub-official', url: '', isHubService: true, configured: true }],
            currentModel: 'gpt-5',
            modelOptions: ['gpt-5'],
            onSwitchModel: vi.fn(),
        });
        fireEvent.click(screen.getByTestId('qs-model-chip'));
        expect(screen.getByRole('listbox')).toBeTruthy();
        fireEvent.keyDown(document, { key: 'Escape' });
        expect(screen.queryByRole('listbox')).toBeNull();
    });

    it('marks only the current model as a selected option (current provider is context)', () => {
        renderBar({
            availableProviders: [
                { name: 'hub-official', url: '', isHubService: true, configured: true },
                { name: 'openai-custom', url: 'https://api.example.com', isHubService: false, configured: true },
            ],
            currentModel: 'gpt-5',
            modelOptions: ['gpt-5', 'gpt-4o'],
            onSwitchProvider: vi.fn(),
            onSwitchModel: vi.fn(),
        });
        fireEvent.click(screen.getByTestId('qs-model-chip'));
        expect(screen.getByTestId('qs-model-menu-current-provider').textContent).toContain('hub-official');
        const selected = screen.getAllByRole('option', { selected: true });
        expect(selected).toHaveLength(1);
        expect(selected[0].textContent || '').toContain('gpt-5');
    });

    it('selects a model with keyboard arrows + Enter', () => {
        const onSwitchModel = vi.fn();
        renderBar({
            availableProviders: [{ name: 'xAI-Grok', url: '', isHubService: false, configured: true }],
            currentModel: 'grok-4.5',
            modelOptions: ['grok-4.5', 'grok-3'],
            onSwitchModel,
        });
        fireEvent.click(screen.getByTestId('qs-model-chip'));
        const listbox = screen.getByRole('listbox');
        // Rapid arrow + enter must use the post-arrow index (not a stale closure).
        fireEvent.keyDown(listbox, { key: 'ArrowDown' });
        fireEvent.keyDown(listbox, { key: 'ArrowDown' }); // wraps to grok-4.5
        fireEvent.keyDown(listbox, { key: 'ArrowDown' }); // grok-3 again
        fireEvent.keyDown(listbox, { key: 'Enter' });
        expect(onSwitchModel).toHaveBeenCalledWith('grok-3');
        expect(screen.queryByRole('listbox')).toBeNull();
    });

    it('closes on outside mousedown without relying on querySelector timing', () => {
        renderBar({
            availableProviders: [{ name: 'xAI-Grok', url: '', isHubService: false, configured: true }],
            currentModel: 'grok-4.5',
            modelOptions: ['grok-4.5'],
            onSwitchModel: vi.fn(),
        });
        fireEvent.click(screen.getByTestId('qs-model-chip'));
        expect(screen.getByRole('listbox')).toBeTruthy();
        fireEvent.mouseDown(document.body);
        expect(screen.queryByRole('listbox')).toBeNull();
    });
});
