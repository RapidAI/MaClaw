import { readFileSync } from 'node:fs';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { BotLLMSettingsDialog, type BotLLMProviderOption } from './BotLLMSettingsDialog';
import {
    BotNetworkSettingsDialog,
    BotSettingsTabs,
    normalizeDesktopProxyMode,
    type BotNetworkSettingsLabels,
    type DesktopProxyModeState,
} from './BotNetworkSettingsDialog';

const { saveProfiles } = vi.hoisted(() => ({ saveProfiles: vi.fn(async (..._args: unknown[]) => undefined) }));

vi.mock('../../../wailsjs/go/main/App', () => ({
    // Never settles: the cached provider list is what these tests read, and a
    // late answer would land after the test has already asserted.
    FetchProviderModels: () => new Promise(() => { /* pending */ }),
    GetMaclawLLMProfilePanelState: () => Promise.resolve({
        providers: [{ id: 'hub', name: 'MaClaw Official', model: 'auto', models: ['auto'], is_hub_service: true, connection_test_passed: true }],
        profiles: {
            version: 1,
            assistant: { provider_id: 'hub', model: 'auto' },
            coding: { inherit_assistant: true },
            foreground: { inherit_hub: true },
            background: { inherit_hub: true },
            computer_use: { inherit_hub: true },
            grounder: { inherit_hub: true },
        },
        hub_role_models: { foreground: 'auto', background: 'worker', computer_use: 'auto' },
        assistant: { provider_id: 'hub', provider_name: 'MaClaw Official', model: 'auto', health: 'configured' },
        coding: { inherit_assistant: true, provider_id: 'hub', model: 'auto', health: 'configured' },
        foreground: { model: 'auto', inherit_hub: true, provider_name: 'Hub', configured: true },
        background: { model: 'worker', inherit_hub: true, provider_name: 'Hub', configured: true },
        computer_use: { model: 'auto', inherit_hub: true, provider_name: 'Hub', configured: true },
        grounder: { inherit_hub: true },
        revision: 'rev-1',
    }),
    SaveMaclawLLMProfiles: (...args: unknown[]) => saveProfiles(...args),
    TestMaclawLLMProfile: () => Promise.resolve({}),
    FetchMaclawLLMProfileModels: () => Promise.resolve([]),
}));

vi.mock('../../../wailsjs/runtime', () => ({
    EventsOn: () => {},
    EventsOff: () => {},
}));

const llmLabels = {
    title: 'Bot 用的模型',
    sharedHint: '这个账号下的所有 Bot 都跑同一个模型，这里改了会一起生效。',
    useDefault: '默认',
    defaultHint: '交回服务自己决定。',
    useProvider: '从这台电脑的服务商里挑一个',
    providerLabel: '服务商',
    modelLabel: '模型',
    modelLoading: '正在获取模型列表…',
    modelEmpty: '这个服务商暂时没有可选模型。',
    noProviders: '这台电脑上没有可用的服务商。',
    roleSection: '模型配置',
    roleHint: '前台交互、后台执行、电脑操作和定位模型。这个账号下的所有 Bot 共用。',
    save: '保存',
    cancel: '取消',
    close: '关闭',
};

const providers: BotLLMProviderOption[] = [{
    name: 'DeepSeek',
    url: 'https://api.deepseek.com/v1',
    key: 'sk-ds',
    model: 'deepseek-chat',
    protocol: 'openai',
    wireAPI: '',
    models: ['deepseek-chat', 'deepseek-flash'],
    agentType: '',
    isHubService: false,
}];

const networkLabels: BotNetworkSettingsLabels = {
    heading: '设置',
    title: '云桌面网络',
    hint: '云桌面访问网站的方式。',
    current: (mode: string) => `当前：${mode}`,
    modes: {
        proxy: { name: '代理', hint: '全部走代理。' },
        direct: { name: '直连', hint: '全部直连。' },
        smart: { name: '智能', hint: '按站点自动决定。' },
    },
    locked: '管理员锁定了这一项。',
    save: '保存',
    cancel: '取消',
    close: '关闭',
};

const unlockedState: DesktopProxyModeState = { mode: 'smart', locked: false };

function readWorkspaceCss(): string {
    const fromFrontend = join(process.cwd(), 'src/components/bots/DesktopBotWorkspace.css');
    const cssPath = existsSync(fromFrontend) ? fromFrontend : join(process.cwd(), 'guiapp/frontend/src/components/bots/DesktopBotWorkspace.css');
    return readFileSync(cssPath, 'utf8');
}

describe('Bot settings tabs', () => {
    it('renders the tabs and marks only the active one', () => {
        render(<BotSettingsTabs active="model" labels={{ model: '模型', network: '网络', computer: '电脑' }} onSelect={() => {}} />);
        const tabs = screen.getAllByRole('tab');
        expect(tabs.map(tab => tab.textContent)).toEqual(['模型', '网络', '电脑']);
        expect(screen.getByTestId('bot-settings-tab-model').getAttribute('aria-selected')).toBe('true');
        expect(screen.getByTestId('bot-settings-tab-network').getAttribute('aria-selected')).toBe('false');
    });

    it('only reports a tab that is not already active', () => {
        const onSelect = vi.fn();
        render(<BotSettingsTabs active="model" labels={{ model: '模型', network: '网络', computer: '电脑' }} onSelect={onSelect} />);
        fireEvent.click(screen.getByTestId('bot-settings-tab-model'));
        expect(onSelect).not.toHaveBeenCalled();
        fireEvent.click(screen.getByTestId('bot-settings-tab-network'));
        expect(onSelect).toHaveBeenCalledWith('network');
    });

    // The ARIA tabs pattern: the strip is one tab stop, and arrows move between
    // its tabs. Without roving tabindex Tab would stop twice and arrows would
    // scroll the page instead.
    it('is a single tab stop and moves with the arrow keys', () => {
        const onSelect = vi.fn();
        render(<BotSettingsTabs active="model" labels={{ model: '模型', network: '网络', computer: '电脑' }} onSelect={onSelect} />);
        expect(screen.getByTestId('bot-settings-tab-model').getAttribute('tabindex')).toBe('0');
        expect(screen.getByTestId('bot-settings-tab-network').getAttribute('tabindex')).toBe('-1');

        const list = screen.getByTestId('bot-settings-tabs');
        fireEvent.keyDown(list, { key: 'ArrowRight' });
        expect(onSelect).toHaveBeenLastCalledWith('network');
        fireEvent.keyDown(list, { key: 'ArrowLeft' });
        expect(onSelect).toHaveBeenLastCalledWith('computer');
    });

    it('wraps the arrow keys around the ends', () => {
        const onSelect = vi.fn();
        render(<BotSettingsTabs active="model" labels={{ model: '模型', network: '网络', computer: '电脑' }} onSelect={onSelect} />);
        fireEvent.keyDown(screen.getByTestId('bot-settings-tabs'), { key: 'ArrowLeft' });
        expect(onSelect).toHaveBeenCalledWith('computer');
    });

    it('ignores arrows while the strip is disabled', () => {
        const onSelect = vi.fn();
        render(<BotSettingsTabs active="model" disabled labels={{ model: '模型', network: '网络', computer: '电脑' }} onSelect={onSelect} />);
        fireEvent.keyDown(screen.getByTestId('bot-settings-tabs'), { key: 'ArrowRight' });
        expect(onSelect).not.toHaveBeenCalled();
    });
});

describe('Bot model settings dialog', () => {
    beforeEach(() => { saveProfiles.mockClear(); });

    function renderDialog(overrides: Partial<Parameters<typeof BotLLMSettingsDialog>[0]> = {}) {
        const onSubmit = vi.fn();
        const utils = render(
            <BotLLMSettingsDialog
                labels={llmLabels}
                providers={providers}
                initialMode="default"
                initialName=""
                initialModel=""
                notice=""
                saving={false}
                onClose={() => {}}
                onSubmit={onSubmit}
                heading="设置"
                tabs={<BotSettingsTabs active="model" labels={{ model: '模型', network: '网络', computer: '电脑' }} onSelect={() => {}} />}
                {...overrides}
            />,
        );
        return { onSubmit, ...utils };
    }

    // The card is the control: clicking the description selects it, not just
    // the 16px radio. The selected card also has to look selected.
    it('selects the provider card from anywhere on the card and shows the state', () => {
        renderDialog();
        const defaultCard = screen.getByTestId('bot-llm-mode-default').closest('.bot-llm-dialog__option') as HTMLElement;
        const providerCard = screen.getByTestId('bot-llm-mode-custom').closest('.bot-llm-dialog__option') as HTMLElement;
        expect(defaultCard.className).toContain('is-selected');
        expect(providerCard.className).not.toContain('is-selected');
        fireEvent.click(within(providerCard).getByText(llmLabels.useProvider));
        expect(screen.getByTestId('bot-llm-mode-custom')).toHaveProperty('checked', true);
        expect(providerCard.className).toContain('is-selected');
        expect(defaultCard.className).not.toContain('is-selected');
    });

    // Working inside the card's own fields must not be read as "pick this card":
    // a select change keeps the mode where it is and only moves the model.
    it('keeps the mode while the owner works inside the card fields', () => {
        const { onSubmit } = renderDialog({ initialMode: 'custom', initialName: 'DeepSeek', initialModel: 'deepseek-flash' });
        fireEvent.click(within(screen.getByTestId('bot-llm-provider').closest('.bot-llm-dialog__field') as HTMLElement).getByText('服务商'));
        fireEvent.click(screen.getByTestId('bot-llm-model'));
        fireEvent.change(screen.getByTestId('bot-llm-model'), { target: { value: 'deepseek-chat' } });
        expect(screen.getByTestId('bot-llm-mode-custom')).toHaveProperty('checked', true);
        fireEvent.click(screen.getByTestId('bot-llm-save'));
        expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ mode: 'custom', model: 'deepseek-chat' }));
    });

    it('groups the provider and model fields under the selected card', () => {
        renderDialog();
        const providerCard = screen.getByTestId('bot-llm-mode-custom').closest('.bot-llm-dialog__option') as HTMLElement;
        expect(providerCard.querySelector('.bot-llm-dialog__fields')?.contains(screen.getByTestId('bot-llm-provider'))).toBe(true);
        expect(providerCard.querySelector('.bot-llm-dialog__fields')?.contains(screen.getByTestId('bot-llm-model'))).toBe(true);
    });

    it('marks the card unavailable when there is no provider to pick', () => {
        renderDialog({ providers: [] });
        const card = screen.getByTestId('bot-llm-mode-custom').closest('.bot-llm-dialog__option') as HTMLElement;
        expect(card.className).toContain('is-disabled');
        fireEvent.click(within(card).getByText(llmLabels.useProvider));
        expect(screen.getByTestId('bot-llm-mode-custom')).toHaveProperty('checked', false);
    });

    it('shows the shared role models in a 模型配置 section', async () => {
        renderDialog({ lang: 'zh-Hans' });
        const section = await screen.findByTestId('bot-llm-role-section');
        expect(within(section).getByText('模型配置')).toBeTruthy();
        expect(await screen.findByTestId('bot-role-foreground')).toBeTruthy();
        expect(screen.getByTestId('bot-role-background')).toBeTruthy();
        expect(screen.getByTestId('bot-role-computer_use')).toBeTruthy();
        expect(screen.getByTestId('bot-role-grounder')).toBeTruthy();
        expect(screen.getByRole('checkbox', { name: '前台跟随 Hub' })).toHaveProperty('checked', true);
        expect(screen.getByRole('checkbox', { name: '后台跟随 Hub' })).toHaveProperty('checked', true);
        expect(screen.getByRole('checkbox', { name: '电脑操作跟随 Hub' })).toHaveProperty('checked', true);
        expect(screen.getByRole('checkbox', { name: '不使用定位模型' })).toHaveProperty('checked', true);
        // One-line labels: the hint sits beside the title, not in a following paragraph.
        const label = screen.getByTestId('bot-role-foreground').querySelector('.bot-role-assignments__label') as HTMLElement;
        expect(label.className).toContain('bot-role-assignments__label');
        expect(label.querySelector('p')).toBeNull();
        expect(within(label).getByText('前台交互')).toBeTruthy();
        expect(within(label).getByText('规划与对话')).toBeTruthy();
    });

    it('saves a pinned role through the existing profile API and keeps assistant assignments', async () => {
        const { onSubmit } = renderDialog({ lang: 'en' });
        const follow = await screen.findByRole('checkbox', { name: 'Foreground follows Hub' }) as HTMLInputElement;
        fireEvent.click(follow);
        fireEvent.click(screen.getByTestId('bot-role-assignments-save'));
        await waitFor(() => expect(saveProfiles).toHaveBeenCalled());
        const draft = saveProfiles.mock.calls[0][0] as unknown as { foreground: { inherit_hub?: boolean; model?: string }; assistant: { provider_id?: string; model?: string } };
        expect(draft.foreground.inherit_hub).toBe(false);
        expect(draft.foreground.model).toBe('auto');
        expect(draft.assistant.provider_id).toBe('hub');
        expect(draft.assistant.model).toBe('auto');
        expect(onSubmit).not.toHaveBeenCalled();
    });

    it('does not submit the provider setting when Enter is pressed in a role row', async () => {
        const { onSubmit } = renderDialog({ lang: 'en' });
        const follow = await screen.findByRole('checkbox', { name: 'Foreground follows Hub' });
        fireEvent.keyDown(follow, { key: 'Enter' });
        expect(onSubmit).not.toHaveBeenCalled();
    });
});

describe('Bot network settings dialog', () => {
    function renderDialog(state: DesktopProxyModeState, overrides: Partial<Parameters<typeof BotNetworkSettingsDialog>[0]> = {}) {
        const onSubmit = vi.fn();
        render(
            <BotNetworkSettingsDialog
                labels={networkLabels}
                state={state}
                notice=""
                saving={false}
                tabs={<BotSettingsTabs active="network" labels={{ model: '模型', network: '网络', computer: '电脑' }} onSelect={() => {}} />}
                onClose={() => {}}
                onSubmit={onSubmit}
                {...overrides}
            />,
        );
        return onSubmit;
    }

    it('normalizes what the hub stores into the three known modes', () => {
        expect(normalizeDesktopProxyMode('always')).toBe('proxy');
        expect(normalizeDesktopProxyMode('direct')).toBe('direct');
        expect(normalizeDesktopProxyMode('smart')).toBe('smart');
        expect(normalizeDesktopProxyMode('')).toBe('smart');
        expect(normalizeDesktopProxyMode(null)).toBe('smart');
    });

    it('selects a mode by clicking its card and saves it', () => {
        const onSubmit = renderDialog(unlockedState);
        const direct = screen.getByTestId('bot-network-mode-direct').closest('.bot-llm-dialog__option') as HTMLElement;
        expect(direct.className).not.toContain('is-selected');
        fireEvent.click(within(direct).getByText('直连'));
        expect(direct.className).toContain('is-selected');
        fireEvent.click(screen.getByTestId('bot-network-save'));
        expect(onSubmit).toHaveBeenCalledWith('direct');
    });

    it('leaves the cards alone when a tenant lock is in force', () => {
        const onSubmit = renderDialog({ mode: 'direct', locked: true });
        const proxy = screen.getByTestId('bot-network-mode-proxy').closest('.bot-llm-dialog__option') as HTMLElement;
        expect(proxy.className).toContain('is-disabled');
        fireEvent.click(within(proxy).getByText('代理'));
        expect(screen.getByTestId('bot-network-mode-proxy')).toHaveProperty('checked', false);
        expect(screen.getByTestId('bot-network-save')).toHaveProperty('disabled', true);
        expect(onSubmit).not.toHaveBeenCalled();
    });

    // A lock is a fact about the account, not a failure: it reads as a note.
    it('states the lock as a note instead of an error', () => {
        renderDialog({ mode: 'direct', locked: true });
        const locked = screen.getByTestId('bot-network-locked');
        expect(locked.className).toContain('bot-llm-dialog__note');
        expect(locked.className).not.toContain('bot-llm-dialog__notice');
    });
});

// Both halves of the dialog are modals, so keyboard focus has to stay inside
// one and come back to the page when it closes. Neither dialog had this before.
describe('bot settings dialog focus', () => {
    function renderNetwork(onClose = vi.fn()) {
        render(
            <BotNetworkSettingsDialog
                labels={networkLabels}
                state={unlockedState}
                notice=""
                saving={false}
                onClose={onClose}
                onSubmit={() => {}}
            />,
        );
        return { dialog: screen.getByTestId('bot-network-dialog'), onClose };
    }

    it('moves focus into the dialog when it opens and restores it on unmount', () => {
        const opener = document.createElement('button');
        document.body.appendChild(opener);
        opener.focus();

        const { unmount } = render(
            <BotNetworkSettingsDialog
                labels={networkLabels}
                state={unlockedState}
                notice=""
                saving={false}
                onClose={() => {}}
                onSubmit={() => {}}
            />,
        );
        expect(document.activeElement).toBe(screen.getByTestId('bot-network-dialog'));

        unmount();
        expect(document.activeElement).toBe(opener);
        opener.remove();
    });

    it('wraps Tab and Shift+Tab inside the dialog', () => {
        const { dialog } = renderNetwork();
        const items = Array.from(dialog.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), select:not([disabled])'));
        const first = items[0];
        const last = items[items.length - 1];

        last.focus();
        fireEvent.keyDown(last, { key: 'Tab' });
        expect(document.activeElement).toBe(first);

        first.focus();
        fireEvent.keyDown(first, { key: 'Tab', shiftKey: true });
        expect(document.activeElement).toBe(last);
    });

    it('pulls focus back when something outside the dialog takes it', () => {
        const outside = document.createElement('button');
        document.body.appendChild(outside);
        const { dialog } = renderNetwork();
        outside.focus();
        expect(document.activeElement).toBe(dialog);
        outside.remove();
    });

    it('closes on Escape', () => {
        const { dialog, onClose } = renderNetwork();
        fireEvent.keyDown(dialog, { key: 'Escape' });
        expect(onClose).toHaveBeenCalled();
    });

    it('does not close on Escape while saving', () => {
        const onClose = vi.fn();
        render(
            <BotNetworkSettingsDialog
                labels={networkLabels}
                state={unlockedState}
                notice=""
                saving
                onClose={onClose}
                onSubmit={() => {}}
            />,
        );
        fireEvent.keyDown(screen.getByTestId('bot-network-dialog'), { key: 'Escape' });
        expect(onClose).not.toHaveBeenCalled();
    });
});

// The dialog used to render without most of its own styles: one rule was left
// unclosed, so the parser swallowed everything after it. Keep the braces honest
// and the pieces the screenshot showed missing in place.
describe('bot settings dialog stylesheet', () => {
    const css = readWorkspaceCss();
    // Rule bodies are matched by regex, so strip comments first: several of the
    // rules carry Chinese comments that mention the very properties under test.
    const stripped = css.replace(/\/\*[\s\S]*?\*\//g, '');
    const bodyOf = (selector: string) =>
        stripped.match(new RegExp(`${selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\s*\\{([^}]*)\\}`))?.[1] ?? '';

    it('has balanced braces', () => {
        const open = (css.match(/\{/g) || []).length;
        const close = (css.match(/\}/g) || []).length;
        expect(open).toBe(close);
    });

    it('styles the tab strip and the selected tab', () => {
        expect(css).toMatch(/\.bot-settings-tabs\s*\{[^}]*display:\s*flex/);
        expect(css).toMatch(/\.bot-settings-tabs__tab\.is-active\s*\{[^}]*background:/);
    });

    it('styles the option cards, their selected state and the field group', () => {
        expect(css).toMatch(/\.bot-llm-dialog__option\.is-selected\s*\{[^}]*border-color:/);
        expect(css).toMatch(/\.bot-llm-dialog__option\s*\{[^}]*cursor:\s*pointer/);
        expect(css).toMatch(/\.bot-llm-dialog__fields\s*\{/);
        expect(css).toMatch(/\.form-input\.bot-llm-dialog__select\s*\{/);
    });

    it('keeps the header and the tab strip on one bordered block', () => {
        expect(css).toMatch(/\.bot-llm-dialog__head\s*\{[^}]*border-bottom:/);
        expect(css).toMatch(/\.bot-llm-dialog \.custom-dialog__header\s*\{[^}]*border-bottom:\s*none/);
    });

    // App.css pins .form-input's border and background with !important as the
    // global form baseline. Declaring either here would be dead code that reads
    // like it works — it silently lost to that rule before.
    it('does not declare form properties the global baseline pins', () => {
        const body = bodyOf('.form-input.bot-llm-dialog__select');
        expect(body).not.toMatch(/^\s*border\s*:/m);
        expect(body).not.toMatch(/^\s*border-color\s*:/m);
        expect(body).not.toMatch(/^\s*background(-color)?\s*:/m);
    });

    // The dialog container takes focus on open, so the global :focus-visible
    // ring drew a blue border around the whole window. Keyboard focus belongs
    // on the controls inside, not on the frame.
    it('does not paint a focus ring around the dialog frame', () => {
        expect(bodyOf('.modal-content.custom-dialog.bot-llm-dialog')).toMatch(/^\s*outline:\s*none\s*;/m);
    });

    it('keeps role labels on one line and ellipsizes a long hint', () => {
        expect(bodyOf('.bot-role-assignments__label')).toMatch(/white-space:\s*nowrap/);
        expect(bodyOf('.bot-role-assignments__hint')).toMatch(/text-overflow:\s*ellipsis/);
    });
});
