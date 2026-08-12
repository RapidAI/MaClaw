/** @vitest-environment jsdom */
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { corelib, main } from '../../../wailsjs/go/models';
import { PetSettingsPanel } from '../PetSettingsPanel';
import { DialogProvider } from '../CustomDialog';
import * as AppAPI from '../../../wailsjs/go/main/App';

vi.mock('../../../wailsjs/go/main/App', () => ({
    ListPetPacks: vi.fn().mockResolvedValue([]),
    InstallPetPackZip: vi.fn().mockResolvedValue('cool-pet'),
    SelectPetPackZip: vi.fn().mockResolvedValue(''),
    UninstallPetPack: vi.fn().mockResolvedValue(undefined),
    GetPetPackPreviewDataURL: vi.fn().mockResolvedValue(''),
    GetPetPackStateFrameDataURL: vi.fn().mockResolvedValue(''),
    GetPetPackRuntimeInfo: vi.fn().mockResolvedValue({ pack_id: 'clawmate', variant: 'default', declared_renderer: 'native-character', effective_renderer: 'native-character', degradation_reason: '' }),
    OpenPetPacksDir: vi.fn().mockResolvedValue(undefined),
    GetPetPacksDir: vi.fn().mockResolvedValue('C:\\\\Users\\\\test\\\\.maclaw\\\\pet-packs'),
    GetPetStoreAccount: vi.fn().mockResolvedValue({ uploads: [] }),
    CanPublishPetStorePack: vi.fn().mockResolvedValue(true),
    SubmitPetStorePack: vi.fn().mockResolvedValue({ id: 'pet_listing' }),
    WithdrawPetStorePack: vi.fn().mockResolvedValue(undefined),
	ExportPetPackZip: vi.fn().mockResolvedValue('C:/tmp/creator-pet.zip'),
}));

vi.mock('../../../wailsjs/runtime', () => ({
    BrowserOpenURL: vi.fn(),
    EventsOn: vi.fn().mockReturnValue(() => {}),
}));

// The regenerated bindings type ListPetPacks as petpack.PackInfo; the partial
// fixtures below only carry the fields the component reads.
const mockListPetPacks = (packs: unknown[]) =>
    vi.mocked(AppAPI.ListPetPacks).mockResolvedValue(packs as never);

function renderPetSettings(lang: string, overrides: Partial<corelib.AppConfig> = {}) {
    const config = new corelib.AppConfig({
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
        <DialogProvider>
            <PetSettingsPanel
                config={config}
                lang={lang}
                setConfig={vi.fn()}
                patchConfig={vi.fn().mockResolvedValue(undefined)}
            />
        </DialogProvider>,
    );
}

describe('PetSettingsPanel localization', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        // Motion/sound controls render only on Windows; jsdom reports an empty
        // platform, so pin it for these tests.
        Object.defineProperty(window.navigator, 'platform', { value: 'Win32', configurable: true });
        vi.mocked(AppAPI.GetPetPackStateFrameDataURL).mockResolvedValue('');
        vi.mocked(AppAPI.GetPetPackPreviewDataURL).mockResolvedValue('');
        vi.mocked(AppAPI.GetPetPacksDir).mockResolvedValue('C:/Users/test/.maclaw/pet-packs');
        vi.mocked(AppAPI.OpenPetPacksDir).mockResolvedValue(undefined);
        mockListPetPacks([]);
        vi.mocked(AppAPI.InstallPetPackZip).mockResolvedValue('cool-pet');
        vi.mocked(AppAPI.SelectPetPackZip).mockResolvedValue('');
        vi.mocked(AppAPI.UninstallPetPack).mockResolvedValue(undefined);
        vi.mocked(AppAPI.GetPetStoreAccount).mockResolvedValue({ uploads: [] });
        vi.mocked(AppAPI.CanPublishPetStorePack).mockResolvedValue(true);
        vi.mocked(AppAPI.SubmitPetStorePack).mockResolvedValue({ id: 'pet_listing' });
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
    });

    it('replaces an active motion-sound preview and closes it when the panel unmounts', async () => {
        const originalAudioContext = Object.getOwnPropertyDescriptor(window, 'AudioContext');
        const contexts: Array<{ resume: ReturnType<typeof vi.fn>; close: ReturnType<typeof vi.fn> }> = [];
        const audioParam = () => ({
            setValueAtTime: vi.fn(),
            exponentialRampToValueAtTime: vi.fn(),
        });
        const audioNode = () => ({ connect: vi.fn(), disconnect: vi.fn() });
        const AudioContextCtor = vi.fn(function () {
            const context = {
                currentTime: 0,
                destination: {},
                resume: vi.fn().mockResolvedValue(undefined),
                close: vi.fn().mockResolvedValue(undefined),
                createDynamicsCompressor: () => ({
                    ...audioNode(),
                    threshold: audioParam(),
                    knee: audioParam(),
                    ratio: audioParam(),
                    attack: audioParam(),
                    release: audioParam(),
                }),
                createBiquadFilter: () => ({ ...audioNode(), type: 'bandpass', frequency: audioParam(), Q: audioParam() }),
                createGain: () => ({ ...audioNode(), gain: audioParam() }),
                createDelay: () => ({ ...audioNode(), delayTime: audioParam() }),
                createOscillator: () => ({ ...audioNode(), type: 'sine', frequency: audioParam(), start: vi.fn(), stop: vi.fn() }),
            };
            contexts.push(context);
            return context;
        });
        Object.defineProperty(window, 'AudioContext', { configurable: true, writable: true, value: AudioContextCtor });

        try {
            const view = renderPetSettings('en');
            await act(async () => {
                fireEvent.click(screen.getByRole('button', { name: 'Bubble: Short, rounded, and good for small sizes.' }));
            });
            expect(contexts).toHaveLength(1);
            expect(contexts[0].resume).toHaveBeenCalledTimes(1);

            await act(async () => {
                fireEvent.click(screen.getByRole('button', { name: 'Chime: Clear with a light notification tail.' }));
            });
            expect(contexts).toHaveLength(2);
            expect(contexts[0].close).toHaveBeenCalledTimes(1);

            view.unmount();
            expect(contexts[1].close).toHaveBeenCalledTimes(1);
        } finally {
            if (originalAudioContext) {
                Object.defineProperty(window, 'AudioContext', originalAudioContext);
            } else {
                delete (window as { AudioContext?: unknown }).AudioContext;
            }
        }
    });

    it('stops an active preview when SFX has been disabled before a preset is selected', async () => {
        const originalAudioContext = Object.getOwnPropertyDescriptor(window, 'AudioContext');
        const context = {
            currentTime: 0,
            destination: {},
            resume: vi.fn().mockResolvedValue(undefined),
            close: vi.fn().mockResolvedValue(undefined),
            createDynamicsCompressor: () => ({ connect: vi.fn(), disconnect: vi.fn(), threshold: { setValueAtTime: vi.fn() }, knee: { setValueAtTime: vi.fn() }, ratio: { setValueAtTime: vi.fn() }, attack: { setValueAtTime: vi.fn() }, release: { setValueAtTime: vi.fn() } }),
            createBiquadFilter: () => ({ connect: vi.fn(), disconnect: vi.fn(), type: 'bandpass', frequency: { setValueAtTime: vi.fn(), exponentialRampToValueAtTime: vi.fn() }, Q: { setValueAtTime: vi.fn() } }),
            createGain: () => ({ connect: vi.fn(), disconnect: vi.fn(), gain: { setValueAtTime: vi.fn(), exponentialRampToValueAtTime: vi.fn() } }),
            createDelay: () => ({ connect: vi.fn(), disconnect: vi.fn(), delayTime: { setValueAtTime: vi.fn() } }),
            createOscillator: () => ({ connect: vi.fn(), disconnect: vi.fn(), type: 'sine', frequency: { setValueAtTime: vi.fn() }, start: vi.fn(), stop: vi.fn() }),
        };
        const AudioContextCtor = vi.fn(function () { return context; });
        Object.defineProperty(window, 'AudioContext', { configurable: true, writable: true, value: AudioContextCtor });

        try {
            const view = renderPetSettings('en');
            await act(async () => {
                fireEvent.click(screen.getByRole('button', { name: 'Bubble: Short, rounded, and good for small sizes.' }));
            });
            expect(context.close).not.toHaveBeenCalled();

            view.rerender(
                <DialogProvider>
                    <PetSettingsPanel
                        config={new corelib.AppConfig({ pet_enabled: true, pet_skin: 'clawmate', pet_size: 88, pet_interaction_mode: 'balanced', pet_motion_enabled: true, pet_motion_sound_enabled: false })}
                        lang="en"
                        setConfig={vi.fn()}
                        patchConfig={vi.fn().mockResolvedValue(undefined)}
                    />
                </DialogProvider>,
            );
            await act(async () => {
                fireEvent.click(screen.getByRole('button', { name: 'Chime: Clear with a light notification tail.' }));
            });
            expect(context.close).toHaveBeenCalledTimes(1);
            expect(AudioContextCtor).toHaveBeenCalledTimes(1);
        } finally {
            if (originalAudioContext) {
                Object.defineProperty(window, 'AudioContext', originalAudioContext);
            } else {
                delete (window as { AudioContext?: unknown }).AudioContext;
            }
        }
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
        mockListPetPacks([
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
        const close = screen.getByRole('button', { name: 'Close dialog' });
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
        mockListPetPacks([{
            id: 'creator-pet', name: 'Creator pet', label: 'Creator pet', scope: 'user', source: 'created', can_uninstall: true,
        }]);
        await act(async () => { renderPetSettings('en', { pet_skin: 'creator-pet' }); });
        await waitFor(() => expect(screen.getByRole('button', { name: /Creator pet/i })).toBeTruthy());
        const skin = screen.getByRole('button', { name: /Creator pet/i });
        await act(async () => { fireEvent.contextMenu(skin, { clientX: 100, clientY: 120 }); });
        expect(screen.getByRole('menuitem', { name: 'Share…' })).toBeTruthy();
        expect(screen.getByRole('menuitem', { name: 'Uninstall' })).toBeTruthy();
    });

    it('opens share setup when the context-menu Share action is clicked', async () => {
        mockListPetPacks([{
            id: 'creator-pet', name: 'Creator pet', label: 'Creator pet', description: 'A friendly creator-made pet.', scope: 'user', source: 'created', can_uninstall: true,
        }]);
        await act(async () => { renderPetSettings('en', { pet_skin: 'creator-pet' }); });
        const skin = await screen.findByRole('button', { name: /Creator pet/i });
        await act(async () => { fireEvent.contextMenu(skin, { clientX: 100, clientY: 120 }); });
        const share = screen.getByRole('menuitem', { name: 'Share…' });
        await act(async () => { fireEvent.pointerDown(share); fireEvent.click(share); });
        const dialog = await screen.findByRole('dialog', { name: 'Share pet pack' });
        expect(dialog.textContent).toContain('A friendly creator-made pet.');
        expect(screen.queryByRole('textbox', { name: 'Description' })).toBeNull();
    });

    it('publishes the manifest description instead of its localized display text', async () => {
        mockListPetPacks([{
            id: 'creator-pet',
            name: 'Creator pet',
            label: 'Creator pet',
            description: 'Original pet-pack.yaml description.',
            description_i18n: { en: 'Localized description for the settings card.' },
            scope: 'user',
            source: 'created',
            can_uninstall: true,
        }]);
        await act(async () => { renderPetSettings('en', { pet_skin: 'creator-pet' }); });
        const skin = await screen.findByRole('button', { name: /Creator pet/i });
        await act(async () => { fireEvent.contextMenu(skin, { clientX: 100, clientY: 120 }); });
        await act(async () => { fireEvent.click(screen.getByRole('menuitem', { name: 'Share…' })); });

        const dialog = await screen.findByRole('dialog', { name: 'Share pet pack' });
        expect(dialog.textContent).toContain('Localized description for the settings card.');
        await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Publish pet pack' })); });

        await waitFor(() => expect(AppAPI.SubmitPetStorePack).toHaveBeenCalledWith(
            'C:/tmp/creator-pet.zip',
            'Creator pet',
            'Original pet-pack.yaml description.',
            '1.0.0',
            0,
            'creator-pet',
        ));
        await waitFor(() => expect(screen.getByText('Pet pack published. Manage it in My Uploads.').getAttribute('role')).toBe('status'));
        expect(screen.getByRole('dialog', { name: 'Share pet pack' })).toBeTruthy();
        expect(screen.queryByRole('button', { name: 'Publish pet pack' })).toBeNull();
        expect(screen.getByRole('button', { name: 'Close' }).textContent).toBe('Close');
    });

    it('runs uninstall after the context-menu Uninstall action is clicked', async () => {
        mockListPetPacks([{
            id: 'creator-pet', name: 'Creator pet', label: 'Creator pet', scope: 'user', source: 'created', can_uninstall: true,
        }]);
        await act(async () => { renderPetSettings('en', { pet_skin: 'creator-pet' }); });
        const skin = await screen.findByRole('button', { name: /Creator pet/i });
        await act(async () => { fireEvent.contextMenu(skin, { clientX: 100, clientY: 120 }); });
        const uninstall = screen.getByRole('menuitem', { name: 'Uninstall' });
        await act(async () => { fireEvent.pointerDown(uninstall); fireEvent.click(uninstall); });
        expect(await screen.findByRole('dialog', { name: 'Uninstall pet pack' })).toBeTruthy();
    });

    it('shows unlist instead of share after the created pack is published', async () => {
        mockListPetPacks([{
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

    it('does not offer a paused listing for re-sharing', async () => {
        mockListPetPacks([{
            id: 'creator-pet', name: 'Creator pet', label: 'Creator pet', scope: 'user', source: 'created', can_uninstall: true,
        }]);
        vi.mocked(AppAPI.GetPetStoreAccount).mockResolvedValue({ uploads: [{ id: 'pet_listing', source_pack_id: 'creator-pet', status: 'paused' }] });
        await act(async () => { renderPetSettings('en', { pet_skin: 'creator-pet' }); });
        const skin = await screen.findByRole('button', { name: /Creator pet/i });
        await waitFor(() => expect(AppAPI.GetPetStoreAccount).toHaveBeenCalled());
        await act(async () => { fireEvent.contextMenu(skin, { clientX: 100, clientY: 120 }); });

        const paused = screen.getByRole('menuitem', { name: 'Publishing paused' });
        expect(paused).toHaveProperty('disabled', true);
        expect(screen.queryByRole('menuitem', { name: 'Share…' })).toBeNull();
        expect(screen.queryByRole('menuitem', { name: 'Unlist' })).toBeNull();
        expect(screen.getByRole('menuitem', { name: 'Uninstall' })).toBeTruthy();
    });

    it('surfaces an export failure instead of silently keeping the share setup open', async () => {
        mockListPetPacks([{
            id: 'creator-pet', name: 'Creator pet', label: 'Creator pet', description: 'A friendly creator-made pet.', scope: 'user', source: 'created', can_uninstall: true,
        }]);
        vi.mocked(AppAPI.ExportPetPackZip).mockResolvedValue('');
        await act(async () => { renderPetSettings('en', { pet_skin: 'creator-pet' }); });
        const skin = await screen.findByRole('button', { name: /Creator pet/i });
        await act(async () => { fireEvent.contextMenu(skin, { clientX: 100, clientY: 120 }); });
        await act(async () => { fireEvent.click(screen.getByRole('menuitem', { name: 'Share…' })); });
        await screen.findByRole('dialog', { name: 'Share pet pack' });
        await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Publish pet pack' })); });

        await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('Failed to export the pet pack'));
        expect(AppAPI.SubmitPetStorePack).not.toHaveBeenCalled();
        expect(screen.getByRole('dialog', { name: 'Share pet pack' })).toBeTruthy();
    });

    it('shows a publishability rejection inside the share dialog', async () => {
        mockListPetPacks([{
            id: 'creator-pet', name: 'Creator pet', label: 'Creator pet', scope: 'user', source: 'created', can_uninstall: true,
        }]);
        vi.mocked(AppAPI.CanPublishPetStorePack).mockResolvedValue(false);
        await act(async () => { renderPetSettings('en', { pet_skin: 'creator-pet' }); });
        const skin = await screen.findByRole('button', { name: /Creator pet/i });
        await act(async () => { fireEvent.contextMenu(skin, { clientX: 100, clientY: 120 }); });
        await act(async () => { fireEvent.click(screen.getByRole('menuitem', { name: 'Share…' })); });

        await screen.findByRole('dialog', { name: 'Share pet pack' });
        await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Publish pet pack' })); });

        await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('already published by another creator'));
        expect(AppAPI.ExportPetPackZip).not.toHaveBeenCalled();
    });

    it('clears a local validation error as the share form is corrected', async () => {
        mockListPetPacks([{
            id: 'creator-pet', name: 'Creator pet', label: 'Creator pet', scope: 'user', source: 'created', can_uninstall: true,
        }]);
        await act(async () => { renderPetSettings('en', { pet_skin: 'creator-pet' }); });
        const skin = await screen.findByRole('button', { name: /Creator pet/i });
        await act(async () => { fireEvent.contextMenu(skin, { clientX: 100, clientY: 120 }); });
        await act(async () => { fireEvent.click(screen.getByRole('menuitem', { name: 'Share…' })); });

        await screen.findByRole('dialog', { name: 'Share pet pack' });
        const name = screen.getByRole('textbox', { name: 'Name' });
        await act(async () => { fireEvent.change(name, { target: { value: '' } }); });
        await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Publish pet pack' })); });
        expect(screen.getByRole('alert').textContent).toContain('Enter a pet-pack name.');

        await act(async () => { fireEvent.change(name, { target: { value: 'Fixed pet name' } }); });
        expect(screen.queryByRole('alert')).toBeNull();
    });

    it('publishes when Enter is pressed in the share form', async () => {
        mockListPetPacks([{
            id: 'creator-pet', name: 'Creator pet', label: 'Creator pet', scope: 'user', source: 'created', can_uninstall: true,
        }]);
        await act(async () => { renderPetSettings('en', { pet_skin: 'creator-pet' }); });
        const skin = await screen.findByRole('button', { name: /Creator pet/i });
        await act(async () => { fireEvent.contextMenu(skin, { clientX: 100, clientY: 120 }); });
        await act(async () => { fireEvent.click(screen.getByRole('menuitem', { name: 'Share…' })); });

        const dialog = await screen.findByRole('dialog', { name: 'Share pet pack' });
        await act(async () => { fireEvent.keyDown(screen.getByRole('textbox', { name: 'Name' }), { key: 'Enter' }); });
        await act(async () => { fireEvent.submit(dialog); });

        await waitFor(() => expect(AppAPI.SubmitPetStorePack).toHaveBeenCalledWith(
            'C:/tmp/creator-pet.zip', 'Creator pet', 'Creator pet', '1.0.0', 0, 'creator-pet',
        ));
    });

    it('shows share for an imported local Zip and only uninstall for a market pack', async () => {
        mockListPetPacks([{
            id: 'imported-pet', name: 'Imported pet', label: 'Imported pet', scope: 'user', source: 'imported', can_uninstall: true,
        }]);
        await act(async () => { renderPetSettings('en', { pet_skin: 'imported-pet' }); });
        await waitFor(() => expect(screen.getByRole('button', { name: /Imported pet/i })).toBeTruthy());
        await act(async () => { fireEvent.contextMenu(screen.getByRole('button', { name: /Imported pet/i }), { clientX: 100, clientY: 120 }); });
        expect(screen.getByRole('menuitem', { name: 'Share…' })).toBeTruthy();
        expect(screen.getByRole('menuitem', { name: 'Uninstall' })).toBeTruthy();
        await act(async () => { fireEvent.keyDown(window, { key: 'Escape' }); });

        mockListPetPacks([{
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
