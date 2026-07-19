// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { main } from '../../../../wailsjs/go/models';
import { GeneralSettingsPanel } from '../GeneralSettingsPanel';

const PatchConfigFieldsMock = vi.fn((patch: Partial<main.AppConfig>) => new main.AppConfig({
    default_launch_mode: 'local',
    ...patch,
}));
const SelectWorkingDirMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    PatchConfigFields: (patch: Partial<main.AppConfig>) => PatchConfigFieldsMock(patch),
    SelectWorkingDir: () => SelectWorkingDirMock(),
}));

afterEach(() => {
    vi.clearAllMocks();
});

function renderPanel(configPatch: Partial<main.AppConfig> = {}, lang = 'en') {
    const config = new main.AppConfig({
        default_launch_mode: 'local',
        ...configPatch,
    });
    const setConfig = vi.fn();

    const view = render(
        <GeneralSettingsPanel
            config={config}
            setConfig={setConfig}
            lang={lang}
            t={(key) => key}
            onLanguageChange={vi.fn()}
        />,
    );

    return { config, setConfig, rerender: view.rerender };
}

function panel(config: main.AppConfig, setConfig = vi.fn(), lang = 'en') {
    return <GeneralSettingsPanel config={config} setConfig={setConfig} lang={lang} t={(key) => key} onLanguageChange={vi.fn()} />;
}

describe('GeneralSettingsPanel', () => {
    it('shows the chat gossip auto-post switch in general settings', () => {
        renderPanel({}, 'zh-Hans');

        const toggle = screen.getByLabelText('聊天八卦自动发布');

        expect(toggle).toBeInstanceOf(HTMLInputElement);
        expect((toggle as HTMLInputElement).checked).toBe(true);
    });

    it('shows skill self-evolution enabled by default and persists toggle', () => {
        renderPanel({}, 'zh-Hans');

        const toggle = screen.getByLabelText('技能自进化') as HTMLInputElement;
        expect(toggle.checked).toBe(true);

        fireEvent.click(toggle);

        expect(PatchConfigFieldsMock).toHaveBeenCalledWith({ skill_evolution_enabled: false });
    });

    it('persists re-enabling skill self-evolution', () => {
        renderPanel({ skill_evolution_enabled: false }, 'en');

        const toggle = screen.getByLabelText('Skill self-evolution') as HTMLInputElement;
        expect(toggle.checked).toBe(false);

        fireEvent.click(toggle);

        expect(PatchConfigFieldsMock).toHaveBeenCalledWith({ skill_evolution_enabled: true });
    });

    it('shows and persists the MaClaw app entry switch after language', () => {
        renderPanel({}, 'zh-Hans');

        const languageSelect = screen.getByRole('combobox');
        const toggle = screen.getByLabelText('MaClaw应用入口') as HTMLInputElement;
        // Default-on when field is absent (same as workflow / utilities entry).
        expect(toggle.checked).toBe(true);
        expect(languageSelect.compareDocumentPosition(toggle) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

        fireEvent.click(toggle);

        expect(PatchConfigFieldsMock).toHaveBeenCalledWith({ show_app_entry: false });
    });

    it('persists disabling the MaClaw app entry switch', () => {
        renderPanel({ show_app_entry: true }, 'zh-Hans');

        const toggle = screen.getByLabelText('MaClaw应用入口') as HTMLInputElement;
        expect(toggle.checked).toBe(true);

        fireEvent.click(toggle);

        expect(PatchConfigFieldsMock).toHaveBeenCalledWith({ show_app_entry: false });
    });

    it('persists chat gossip auto-post changes', () => {
        const { setConfig } = renderPanel({ gossip_auto_publish: true });

        fireEvent.click(screen.getByLabelText('Auto-post Chat Gossip'));

        expect(setConfig).toHaveBeenCalledWith(expect.objectContaining({ gossip_auto_publish: false }));
        expect(PatchConfigFieldsMock).toHaveBeenCalledWith({ gossip_auto_publish: false });
    });

    it('keeps chat gossip auto-post checked during stale config refreshes', () => {
        PatchConfigFieldsMock.mockReturnValueOnce(new Promise(() => {}) as any);
        const staleConfig = new main.AppConfig({ default_launch_mode: 'local', gossip_auto_publish: false });
        const setConfig = vi.fn();
        const { rerender } = render(panel(staleConfig, setConfig));

        const toggle = screen.getByLabelText('Auto-post Chat Gossip') as HTMLInputElement;
        expect(toggle.checked).toBe(false);

        fireEvent.click(toggle);
        expect(toggle.checked).toBe(true);

        const optimisticConfig = setConfig.mock.calls[0][0] as main.AppConfig;
        rerender(panel(optimisticConfig, setConfig));
        expect((screen.getByLabelText('Auto-post Chat Gossip') as HTMLInputElement).checked).toBe(true);

        rerender(panel(staleConfig, setConfig));
        expect((screen.getByLabelText('Auto-post Chat Gossip') as HTMLInputElement).checked).toBe(true);
    });

    it('places detailed logs before chat gossip auto-post', () => {
        renderPanel({}, 'en');

        const detailedLogs = screen.getByLabelText('Detailed logs');
        const gossipAutoPost = screen.getByLabelText('Auto-post Chat Gossip');

        expect(detailedLogs.compareDocumentPosition(gossipAutoPost) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    });

    it('preserves earlier general-setting edits when multiple controls save before rerender', () => {
        renderPanel({
            llm_trajectory_logging: false,
            log_detail_enabled: false,
        });

        fireEvent.click(screen.getByLabelText('Record LLM trajectory'));
        fireEvent.click(screen.getByLabelText('Detailed logs'));

        expect(PatchConfigFieldsMock).toHaveBeenNthCalledWith(1, {
            llm_trajectory_logging: true,
        });
        expect(PatchConfigFieldsMock).toHaveBeenNthCalledWith(2, {
            log_detail_enabled: true,
        });
    });

    it('saves the latest working directory value on blur', () => {
        renderPanel({ working_directory: 'D:/old' });
        const input = screen.getByDisplayValue('D:/old');

        fireEvent.change(input, { target: { value: 'D:/new' } });
        fireEvent.blur(input);

        expect(PatchConfigFieldsMock).toHaveBeenLastCalledWith({ working_directory: 'D:/new' });
    });
});
