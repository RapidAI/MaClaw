// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { corelib, main } from '../../../../wailsjs/go/models';
import { miniAppLabels } from '../../../i18n/maclawMiniAppLabels';
import { GeneralSettingsPanel } from '../GeneralSettingsPanel';

const PatchConfigFieldsMock = vi.fn((patch: Partial<corelib.AppConfig>) => new corelib.AppConfig({
    default_launch_mode: 'local',
    ...patch,
}));
const LoadConfigMock = vi.fn(() => new corelib.AppConfig({ default_launch_mode: 'local' }));
const SelectWorkingDirMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    PatchConfigFields: (patch: Partial<corelib.AppConfig>) => PatchConfigFieldsMock(patch),
    LoadConfig: () => LoadConfigMock(),
    SelectWorkingDir: () => SelectWorkingDirMock(),
}));

afterEach(() => {
    cleanup();
    vi.clearAllMocks();
});

function renderPanel(configPatch: Partial<corelib.AppConfig> = {}, lang = 'en') {
    const config = new corelib.AppConfig({
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

function panel(config: corelib.AppConfig, setConfig = vi.fn(), lang = 'en') {
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

    it('shows and persists the MiniAPP entry switch after language', () => {
        renderPanel({}, 'zh-Hans');

        const languageSelect = screen.getByRole('combobox', { name: 'language' });
        const toggle = screen.getByLabelText(miniAppLabels.entry.zhHans) as HTMLInputElement;
        // Default-on when field is absent (same as workflow / utilities entry).
        expect(toggle.checked).toBe(true);
        expect(languageSelect.compareDocumentPosition(toggle) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

        fireEvent.click(toggle);

        expect(PatchConfigFieldsMock).toHaveBeenCalledWith({ show_app_entry: false });
    });

    it('persists disabling the MiniAPP entry switch', () => {
        renderPanel({ show_app_entry: true }, 'zh-Hans');

        const toggle = screen.getByLabelText(miniAppLabels.entry.zhHans) as HTMLInputElement;
        expect(toggle.checked).toBe(true);

        fireEvent.click(toggle);

        expect(PatchConfigFieldsMock).toHaveBeenCalledWith({ show_app_entry: false });
    });

    it('persists disabling the Utilities entry switch', () => {
        renderPanel({}, 'en');

        fireEvent.click(screen.getByLabelText('Utilities entry'));

        expect(PatchConfigFieldsMock).toHaveBeenCalledWith({ show_utilities_entry: false });
    });

    it('keeps the Survey IM intercept switch off after the saved config returns', async () => {
        renderPanel({}, 'en');

        const toggle = screen.getByLabelText('Survey IM intercept') as HTMLInputElement;
        fireEvent.click(toggle);

        expect(PatchConfigFieldsMock).toHaveBeenCalledWith({ survey_enabled: false });
        await Promise.resolve();
        await Promise.resolve();
        expect((screen.getByLabelText('Survey IM intercept') as HTMLInputElement).checked).toBe(false);
    });

    it('ignores an older Survey IM intercept save response after a rapid re-enable', async () => {
        let resolveFirst: (value: corelib.AppConfig) => void = () => {};
        let resolveSecond: (value: corelib.AppConfig) => void = () => {};
        PatchConfigFieldsMock
            .mockImplementationOnce(() => new Promise<corelib.AppConfig>((resolve) => { resolveFirst = resolve; }) as any)
            .mockImplementationOnce(() => new Promise<corelib.AppConfig>((resolve) => { resolveSecond = resolve; }) as any);
        const { setConfig } = renderPanel({ survey_enabled: true }, 'en');
        const toggle = screen.getByLabelText('Survey IM intercept') as HTMLInputElement;

        fireEvent.click(toggle);
        fireEvent.click(toggle);
        expect(PatchConfigFieldsMock).toHaveBeenNthCalledWith(1, { survey_enabled: false });
        expect(PatchConfigFieldsMock).toHaveBeenNthCalledWith(2, { survey_enabled: true });

        resolveSecond(new corelib.AppConfig({ default_launch_mode: 'local', survey_enabled: true }));
        await Promise.resolve();
        await Promise.resolve();
        expect(setConfig.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({ survey_enabled: true }));

        resolveFirst(new corelib.AppConfig({ default_launch_mode: 'local', survey_enabled: false }));
        await Promise.resolve();
        await Promise.resolve();
        expect(setConfig.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({ survey_enabled: true }));
    });

    it('restores the persisted Survey IM intercept value when its latest save fails', async () => {
        PatchConfigFieldsMock.mockImplementationOnce(() => Promise.reject(new Error('offline')) as any);
        LoadConfigMock.mockReturnValueOnce(new corelib.AppConfig({ default_launch_mode: 'local', survey_enabled: true }));
        const { setConfig } = renderPanel({ survey_enabled: true }, 'en');

        await act(async () => {
            fireEvent.click(screen.getByLabelText('Survey IM intercept'));
            await Promise.resolve();
            await Promise.resolve();
            await Promise.resolve();
            await Promise.resolve();
        });

        expect(LoadConfigMock).toHaveBeenCalledTimes(1);
        expect(setConfig.mock.calls.at(-1)?.[0]).toEqual(expect.objectContaining({ survey_enabled: true }));
    });

    it('persists chat gossip auto-post changes', () => {
        const { setConfig } = renderPanel({ gossip_auto_publish: true });

        fireEvent.click(screen.getByLabelText('Auto-post Chat Gossip'));

        expect(setConfig).toHaveBeenCalledWith(expect.objectContaining({ gossip_auto_publish: false }));
        expect(PatchConfigFieldsMock).toHaveBeenCalledWith({ gossip_auto_publish: false });
    });

    it('shows the OfficeRead rollout controls and persists their policy', () => {
        renderPanel({
            office_read_engine: 'dual',
            office_read_formats: ['ppt', 'doc'],
            office_read_fallback: true,
            office_read_emit_markdown: false,
        });

        expect((screen.getByLabelText('Office extraction engine') as HTMLSelectElement).value).toBe('dual');
        expect((screen.getByLabelText('Use OfficeRead for Word 97–2003 (.doc)') as HTMLInputElement).checked).toBe(true);
        expect((screen.getByLabelText('Fall back to legacy extraction') as HTMLInputElement).checked).toBe(true);
        expect((screen.getByLabelText('Use structured Markdown in Knowledge') as HTMLInputElement).checked).toBe(false);

        fireEvent.change(screen.getByLabelText('Office extraction engine'), { target: { value: 'officeread' } });
        fireEvent.click(screen.getByLabelText('Use OfficeRead for Word (.docx)'));
        fireEvent.click(screen.getByLabelText('Fall back to legacy extraction'));
        fireEvent.click(screen.getByLabelText('Use structured Markdown in Knowledge'));

        expect(PatchConfigFieldsMock).toHaveBeenNthCalledWith(1, { office_read_engine: 'officeread' });
        expect(PatchConfigFieldsMock).toHaveBeenNthCalledWith(2, { office_read_formats: ['ppt', 'doc', 'docx'] });
        expect(PatchConfigFieldsMock).toHaveBeenNthCalledWith(3, { office_read_fallback: false });
        expect(PatchConfigFieldsMock).toHaveBeenNthCalledWith(4, { office_read_emit_markdown: true });
    });

    it('renders an empty OfficeRead format policy as the full supported default', () => {
        renderPanel({ office_read_engine: 'officeread', office_read_formats: [] });

        for (const label of [
            'Use OfficeRead for Word 97–2003 (.doc)',
            'Use OfficeRead for Word (.docx)',
            'Use OfficeRead for PowerPoint 97–2003 (.ppt)',
            'Use OfficeRead for PowerPoint (.pptx)',
            'Use OfficeRead for Excel 97–2003 (.xls)',
            'Use OfficeRead for Excel (.xlsx)',
        ]) {
            expect((screen.getByLabelText(label) as HTMLInputElement).checked).toBe(true);
        }
        expect(screen.getByText(/Choose Legacy only to disable OfficeRead for every format/i)).toBeTruthy();

        fireEvent.click(screen.getByLabelText('Use OfficeRead for PowerPoint 97–2003 (.ppt)'));
        expect(PatchConfigFieldsMock).toHaveBeenCalledWith({ office_read_formats: ['doc', 'xls', 'docx', 'xlsx', 'pptx'] });
    });

    it('keeps a non-empty OfficeRead allowlist when removing a format', () => {
        renderPanel({ office_read_engine: 'officeread', office_read_formats: ['ppt', 'doc'] });

        fireEvent.click(screen.getByLabelText('Use OfficeRead for PowerPoint 97–2003 (.ppt)'));
        expect(PatchConfigFieldsMock).toHaveBeenCalledWith({ office_read_formats: ['doc'] });

        const doc = screen.getByLabelText('Use OfficeRead for Word 97–2003 (.doc)') as HTMLInputElement;
        expect(doc.checked).toBe(true);
        expect(doc.disabled).toBe(true);
    });

    it('keeps chat gossip auto-post checked during stale config refreshes', () => {
        PatchConfigFieldsMock.mockReturnValueOnce(new Promise(() => {}) as any);
        const staleConfig = new corelib.AppConfig({ default_launch_mode: 'local', gossip_auto_publish: false });
        const setConfig = vi.fn();
        const { rerender } = render(panel(staleConfig, setConfig));

        const toggle = screen.getByLabelText('Auto-post Chat Gossip') as HTMLInputElement;
        expect(toggle.checked).toBe(false);

        fireEvent.click(toggle);
        expect(toggle.checked).toBe(true);

        const optimisticConfig = setConfig.mock.calls[0][0] as corelib.AppConfig;
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
