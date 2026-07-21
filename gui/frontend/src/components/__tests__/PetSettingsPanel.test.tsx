/** @vitest-environment jsdom */
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { main } from '../../../wailsjs/go/models';
import { PetSettingsPanel } from '../PetSettingsPanel';
import * as AppAPI from '../../../wailsjs/go/main/App';

vi.mock('../../../wailsjs/go/main/App', () => ({
    ListPetPacks: vi.fn().mockResolvedValue([]),
    InstallPetPackZip: vi.fn().mockResolvedValue('cool-pet'),
    SelectPetPackZip: vi.fn().mockResolvedValue(''),
    UninstallPetPack: vi.fn().mockResolvedValue(undefined),
    GetPetPackPreviewDataURL: vi.fn().mockResolvedValue(''),
    GetPetPackStateFrameDataURL: vi.fn().mockResolvedValue(''),
    OpenPetPacksDir: vi.fn().mockResolvedValue(undefined),
    GetPetPacksDir: vi.fn().mockResolvedValue('C:\\\\Users\\\\test\\\\.maclaw\\\\pet-packs'),
}));

vi.mock('../../../wailsjs/runtime', () => ({
    BrowserOpenURL: vi.fn(),
    EventsOn: vi.fn().mockReturnValue(() => {}),
}));

function renderPetSettings(lang: string, overrides: Partial<main.AppConfig> = {}) {
    const config = new main.AppConfig({
        pet_enabled: true,
        pet_skin: 'clawmate',
        pet_size: 88,
        pet_interaction_mode: 'balanced',
        pet_conversation_mode: 'text-first',
        pet_readback_mode: 'summary',
        pet_motion_enabled: true,
        pet_text_interaction_enabled: true,
        pet_file_drop_enabled: true,
        ...overrides,
    });

    return render(
        <PetSettingsPanel
            config={config}
            lang={lang}
            setConfig={vi.fn()}
            patchConfig={vi.fn().mockResolvedValue(undefined)}
        />,
    );
}

describe('PetSettingsPanel localization', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(AppAPI.GetPetPackStateFrameDataURL).mockResolvedValue('');
        vi.mocked(AppAPI.GetPetPackPreviewDataURL).mockResolvedValue('');
        vi.mocked(AppAPI.GetPetPacksDir).mockResolvedValue('C:/Users/test/.maclaw/pet-packs');
        vi.mocked(AppAPI.OpenPetPacksDir).mockResolvedValue(undefined);
        vi.mocked(AppAPI.ListPetPacks).mockResolvedValue([]);
        vi.mocked(AppAPI.InstallPetPackZip).mockResolvedValue('cool-pet');
        vi.mocked(AppAPI.SelectPetPackZip).mockResolvedValue('');
        vi.mocked(AppAPI.UninstallPetPack).mockResolvedValue(undefined);
    });

    afterEach(() => cleanup());

    it('renders pet-specific controls in Simplified Chinese', async () => {
        await act(async () => {
            renderPetSettings('zh-Hans');
        });

        expect(screen.getByRole('button', { name: '帮助' })).toBeTruthy();
        expect(screen.getByRole('button', { name: '打开宠物包创建指南' })).toBeTruthy();
        expect(screen.getByRole('button', { name: '选择 Zip 安装' })).toBeTruthy();
        expect(screen.getByRole('button', { name: '打开 packs 目录' })).toBeTruthy();

        [
            '桌面宠物',
            '启用桌面宠物',
            '待机',
            '聆听',
            '思考',
            '说话',
            '完成',
            '提醒',
            '安静',
            '平衡',
            '活跃',
            '高级交互',
            '文字优先',
            '语音轮次',
            '连续对话',
            '关闭',
            '摘要',
            '全文',
            '仅完成时',
        ].forEach((label) => {
            expect(screen.queryAllByText(label).length).toBeGreaterThan(0);
        });

        [
            'Idle',
            'Listen',
            'Think',
            'Speak',
            'Done',
            'Alert',
            'Quiet',
            'Active',
            'Text First',
            'Voice Turn',
            'Continuous',
            'Done Only',
        ].forEach((label) => {
            expect(screen.queryByText(label)).toBeNull();
        });
    });

    it('keeps pet-specific controls in English when English is selected', async () => {
        await act(async () => {
            renderPetSettings('en');
        });

        expect(screen.getByText('Desktop Pet')).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Help' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Open pet pack creation guide' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Choose Zip to Install' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Open packs folder' })).toBeTruthy();
        expect(screen.getByText('Enable Desktop Pet')).toBeTruthy();
        expect(screen.getByText('Idle')).toBeTruthy();
        expect(screen.getByText('Done')).toBeTruthy();
        expect(screen.getByText('Alert')).toBeTruthy();
        expect(screen.getByText('Text First')).toBeTruthy();
        expect(screen.getByText('Done Only')).toBeTruthy();
        expect(screen.getByText('Desktop Entry')).toBeTruthy();
        expect(screen.getByText('Advanced Interaction')).toBeTruthy();
        expect(screen.getByLabelText('ASR not enabled')).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Balanced' }).getAttribute('aria-pressed')).toBe('true');
        expect(screen.getByRole('button', { name: 'Classic: A restrained default motion cue.' }).getAttribute('aria-pressed')).toBe('true');
        expect(screen.getByLabelText('Size').getAttribute('aria-valuetext')).toBe('88px');
        expect(screen.getByLabelText('Continuous Timeout').getAttribute('aria-valuetext')).toBe('30s');
    });

    it('shows user packs path hint from GetPetPacksDir', async () => {
        await act(async () => {
            renderPetSettings('en');
        });
        await waitFor(() => {
            expect(AppAPI.GetPetPacksDir).toHaveBeenCalled();
            const hint = document.querySelector('.pet-packs-dir-hint');
            expect(hint?.textContent || '').toMatch(/User packs:/i);
            expect(hint?.textContent || '').toMatch(/pet-packs/);
        });
    });

    it('opens packs folder via OpenPetPacksDir', async () => {
        await act(async () => {
            renderPetSettings('en');
        });
        const btn = screen.getByRole('button', { name: 'Open packs folder' });
        await act(async () => {
            fireEvent.click(btn);
        });
        await waitFor(() => {
            expect(AppAPI.OpenPetPacksDir).toHaveBeenCalled();
        });
    });

    it('loads figurative state frames when variant is default', async () => {
        const frameURL = 'data:image/png;base64,abc';
        vi.mocked(AppAPI.GetPetPackStateFrameDataURL).mockResolvedValue(frameURL);

        await act(async () => {
            renderPetSettings('en', { pet_variant: 'default', pet_skin: 'clawmate' });
        });

        await waitFor(() => {
            expect(AppAPI.GetPetPackStateFrameDataURL).toHaveBeenCalledWith('clawmate', 'idle', 'default');
        });

        // Switch preview state → reloads speaking frame
        const speakBtn = screen.getByRole('button', { name: 'Speak' });
        await act(async () => {
            fireEvent.click(speakBtn);
        });
        await waitFor(() => {
            expect(AppAPI.GetPetPackStateFrameDataURL).toHaveBeenCalledWith('clawmate', 'speaking', 'default');
        });
    });

    it('does not request raster frames for classic variant', async () => {
        vi.mocked(AppAPI.GetPetPackStateFrameDataURL).mockClear();
        await act(async () => {
            renderPetSettings('en', { pet_variant: 'classic' });
        });
        // Give effects a tick (packs list + stage effect)
        await act(async () => {
            await Promise.resolve();
            await Promise.resolve();
        });
        expect(AppAPI.GetPetPackStateFrameDataURL).not.toHaveBeenCalled();
    });
});
