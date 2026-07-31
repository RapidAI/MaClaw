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
    GetPetStoreAccount: vi.fn().mockResolvedValue({ uploads: [] }),
    CanPublishPetStorePack: vi.fn().mockResolvedValue(true),
    WithdrawPetStorePack: vi.fn().mockResolvedValue(undefined),
    ExportPetPackZip: vi.fn().mockResolvedValue('C:/tmp/creator-pet.zip'),
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
        vi.mocked(AppAPI.GetPetStoreAccount).mockResolvedValue({ uploads: [] });
        vi.mocked(AppAPI.CanPublishPetStorePack).mockResolvedValue(true);
        vi.mocked(AppAPI.WithdrawPetStorePack).mockResolvedValue(undefined);
        vi.mocked(AppAPI.ExportPetPackZip).mockResolvedValue('C:/tmp/creator-pet.zip');
    });

    afterEach(() => cleanup());

    it('renders pet-specific controls in Simplified Chinese', async () => {
        await act(async () => {
            renderPetSettings('zh-Hans');
        });

        expect(screen.getByRole('button', { name: '帮助：宠物包创建指南' })).toBeTruthy();
        expect(screen.getByRole('button', { name: '打开宠物市场' })).toBeTruthy();
        expect(screen.getByRole('button', { name: '创建指南：宠物包说明' })).toBeTruthy();
        expect(screen.getByRole('button', { name: '选择 Zip 安装' })).toBeTruthy();
        expect(screen.getByRole('button', { name: '浏览用户宠物包目录' })).toBeTruthy();

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
        expect(screen.getByRole('button', { name: 'Help: pet pack creation guide' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Open Pet Store' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Guide: pet pack docs' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Install Zip' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Browse user pet packs folder' })).toBeTruthy();
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
        const btn = screen.getByRole('button', { name: 'Browse user pet packs folder' });
        await act(async () => {
            fireEvent.click(btn);
        });
        await waitFor(() => {
            expect(AppAPI.OpenPetPacksDir).toHaveBeenCalled();
        });
    });

    it('opens the Pet Store from the header action', async () => {
        await act(async () => {
            renderPetSettings('en');
        });

        const openMarket = screen.getByRole('button', { name: 'Open Pet Store' });
        expect(openMarket.nextElementSibling?.classList.contains('pet-switch-row')).toBe(true);
        expect(openMarket.getAttribute('aria-haspopup')).toBe('dialog');
        expect(openMarket.getAttribute('aria-expanded')).toBe('false');

        await act(async () => {
            fireEvent.click(openMarket);
        });

        await waitFor(() => expect(screen.getByRole('dialog', { name: 'Pet Store' })).toBeTruthy());
        expect(openMarket.getAttribute('aria-expanded')).toBe('true');
    });

    it('keeps focus inside the share setup and restores the invoker on close', async () => {
        vi.mocked(AppAPI.ListPetPacks).mockResolvedValue([
            { id: 'custom-pet', name: 'Custom pet', scope: 'user', source: 'local', can_uninstall: true },
        ]);
        await act(async () => {
            renderPetSettings('en', { pet_skin: 'custom-pet' });
        });

        await waitFor(() => expect(screen.getByRole('button', { name: 'Share…' })).toBeTruthy());
        const share = screen.getByRole('button', { name: 'Share…' });
        share.focus();
        await act(async () => { fireEvent.click(share); });

        const dialog = await screen.findByRole('dialog', { name: 'Share pet pack' });
        const close = screen.getByRole('button', { name: 'Close' });
        await waitFor(() => expect(document.activeElement).toBe(close));
        await act(async () => { fireEvent.keyDown(dialog, { key: 'Escape' }); });

        await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Share pet pack' })).toBeNull());
        expect(document.activeElement).toBe(share);
    });

    it('loads state frames from the selected pack', async () => {
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

    it('uses pack frames even when a legacy classic value is stored', async () => {
        const frameURL = 'data:image/png;base64,abc';
        vi.mocked(AppAPI.GetPetPackStateFrameDataURL).mockResolvedValue(frameURL);
        await act(async () => {
            renderPetSettings('en', { pet_variant: 'classic', pet_skin: 'clawmate' });
        });
        await waitFor(() => {
            expect(AppAPI.GetPetPackStateFrameDataURL).toHaveBeenCalledWith('clawmate', 'idle', 'default');
        });
    });

    it('shows share and uninstall for a locally installed Zip', async () => {
        vi.mocked(AppAPI.ListPetPacks).mockResolvedValue([{
            id: 'creator-pet', name: 'Creator pet', label: 'Creator pet', scope: 'user', source: 'created', can_uninstall: true,
        }]);
        await act(async () => { renderPetSettings('en', { pet_skin: 'creator-pet' }); });
        await waitFor(() => expect(screen.getByRole('button', { name: /Creator pet/i })).toBeTruthy());
        const skin = screen.getByRole('button', { name: /Creator pet/i });
        await act(async () => { fireEvent.contextMenu(skin, { clientX: 100, clientY: 120 }); });
        expect(screen.getByRole('menuitem', { name: 'Share…' })).toBeTruthy();
        expect(screen.getByRole('menuitem', { name: 'Uninstall' })).toBeTruthy();
    });

    it('shows unlist instead of share after the created pack is published', async () => {
        vi.mocked(AppAPI.ListPetPacks).mockResolvedValue([{
            id: 'creator-pet', name: 'Creator pet', label: 'Creator pet', scope: 'user', source: 'created', can_uninstall: true,
        }]);
        vi.mocked(AppAPI.GetPetStoreAccount).mockResolvedValue({ uploads: [{ id: 'pet_listing', source_pack_id: 'creator-pet', status: 'active' }] });
        await act(async () => { renderPetSettings('en', { pet_skin: 'creator-pet' }); });
        await waitFor(() => expect(screen.getByRole('button', { name: /Creator pet/i })).toBeTruthy());
        await waitFor(() => expect(AppAPI.GetPetStoreAccount).toHaveBeenCalled());
        const skin = screen.getByRole('button', { name: /Creator pet/i });
        await act(async () => { fireEvent.contextMenu(skin, { clientX: 100, clientY: 120 }); });
        expect(screen.getByRole('menuitem', { name: 'Unlist' })).toBeTruthy();
        expect(screen.queryByRole('menuitem', { name: 'Share…' })).toBeNull();
        expect(screen.getByRole('menuitem', { name: 'Uninstall' })).toBeTruthy();
    });

    it('shows share for an imported local Zip and only uninstall for a market pack', async () => {
        vi.mocked(AppAPI.ListPetPacks).mockResolvedValue([{
            id: 'imported-pet', name: 'Imported pet', label: 'Imported pet', scope: 'user', source: 'imported', can_uninstall: true,
        }]);
        await act(async () => { renderPetSettings('en', { pet_skin: 'imported-pet' }); });
        await waitFor(() => expect(screen.getByRole('button', { name: /Imported pet/i })).toBeTruthy());
        await act(async () => { fireEvent.contextMenu(screen.getByRole('button', { name: /Imported pet/i }), { clientX: 100, clientY: 120 }); });
        expect(screen.getByRole('menuitem', { name: 'Share…' })).toBeTruthy();
        expect(screen.getByRole('menuitem', { name: 'Uninstall' })).toBeTruthy();
        await act(async () => { fireEvent.keyDown(window, { key: 'Escape' }); });

        vi.mocked(AppAPI.ListPetPacks).mockResolvedValue([{
            id: 'market-pet', name: 'Market pet', label: 'Market pet', scope: 'user', source: 'market', can_uninstall: true,
        }]);
        cleanup();
        await act(async () => { renderPetSettings('en', { pet_skin: 'market-pet' }); });
        await waitFor(() => expect(screen.getByRole('button', { name: /Market pet/i })).toBeTruthy());
        const skin = screen.getByRole('button', { name: /Market pet/i });
        await act(async () => { fireEvent.contextMenu(skin, { clientX: 100, clientY: 120 }); });
        expect(screen.queryByRole('menuitem', { name: 'Share…' })).toBeNull();
        expect(screen.queryByRole('menuitem', { name: 'Unlist' })).toBeNull();
        expect(screen.getByRole('menuitem', { name: 'Uninstall' })).toBeTruthy();
    });
});
