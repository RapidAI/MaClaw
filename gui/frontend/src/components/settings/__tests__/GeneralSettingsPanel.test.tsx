// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { main } from '../../../../wailsjs/go/models';
import { GeneralSettingsPanel } from '../GeneralSettingsPanel';

const SaveConfigMock = vi.fn();
const SelectWorkingDirMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    SaveConfig: (...args: unknown[]) => SaveConfigMock(...args),
    SelectWorkingDir: (...args: unknown[]) => SelectWorkingDirMock(...args),
}));

function renderPanel(configPatch: Partial<main.AppConfig> = {}, lang = 'en') {
    const config = new main.AppConfig({
        default_launch_mode: 'local',
        ...configPatch,
    });
    const setConfig = vi.fn();

    render(
        <GeneralSettingsPanel
            config={config}
            setConfig={setConfig}
            lang={lang}
            t={(key) => key}
            onLanguageChange={vi.fn()}
        />,
    );

    return { config, setConfig };
}

describe('GeneralSettingsPanel', () => {
    it('shows the chat gossip auto-post switch in general settings', () => {
        renderPanel({}, 'zh-Hans');

        const toggle = screen.getByLabelText('聊天八卦自动发布');

        expect(toggle).toBeInstanceOf(HTMLInputElement);
        expect((toggle as HTMLInputElement).checked).toBe(true);
    });

    it('persists chat gossip auto-post changes', () => {
        const { setConfig } = renderPanel({ gossip_auto_publish: true });

        fireEvent.click(screen.getByLabelText('Auto-post Chat Gossip'));

        expect(setConfig).toHaveBeenCalledWith(expect.objectContaining({ gossip_auto_publish: false }));
        expect(SaveConfigMock).toHaveBeenCalledWith(expect.objectContaining({ gossip_auto_publish: false }));
    });
});
