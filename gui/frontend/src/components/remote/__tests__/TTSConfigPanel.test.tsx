import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { TTSConfigPanel } from '../TTSConfigPanel';

const mocks = vi.hoisted(() => ({
    check: vi.fn(),
    enabled: vi.fn(),
    voice: vi.fn(),
    setVoice: vi.fn(),
    preview: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    CheckTTSModel: mocks.check,
    DownloadTTSModel: vi.fn(),
    GetTTSEnabled: mocks.enabled,
    GetTTSVoiceID: mocks.voice,
    SetTTSEnabled: vi.fn(),
    SetTTSVoiceID: mocks.setVoice,
    SynthesizeTTSPreview: mocks.preview,
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(),
    EventsOff: vi.fn(),
}));

describe('TTSConfigPanel English voices', () => {
    beforeEach(() => {
        mocks.check.mockResolvedValue({ exists: true, size: 1024 });
        mocks.enabled.mockResolvedValue(true);
        mocks.voice.mockResolvedValue('af_heart');
        mocks.setVoice.mockResolvedValue(undefined);
        mocks.preview.mockResolvedValue('data:audio/wav;base64,UklGRg==');
    });

    afterEach(() => {
        cleanup();
        vi.clearAllMocks();
        vi.unstubAllGlobals();
    });

    it('shows and preserves the sweet female English voice', async () => {
        render(<TTSConfigPanel lang="en" />);

        const select = await screen.findByRole('combobox', { name: 'Voice' }) as HTMLSelectElement;
        expect(select.value).toBe('af_heart');
        expect(screen.getByRole('option', { name: 'Heart · Sweet American English' })).toBeTruthy();

        fireEvent.change(select, { target: { value: 'am_adam' } });
        await waitFor(() => expect(mocks.setVoice).toHaveBeenCalledWith('am_adam'));
    });

    it('keeps the preview busy until desktop playback ends', async () => {
        let currentAudio: any;
        vi.stubGlobal('Audio', class {
            onended: (() => void) | null = null;
            onerror: (() => void) | null = null;
            pause = vi.fn();
            play = vi.fn().mockResolvedValue(undefined);
            constructor() { currentAudio = this; }
        });
        render(<TTSConfigPanel lang="en" />);
        const preview = await screen.findByRole('button', { name: 'Preview' });

        fireEvent.click(preview);
        expect(await screen.findByRole('button', { name: 'Generating...' })).toBeTruthy();
        currentAudio.onended?.();

        await waitFor(() => expect(screen.getByRole('button', { name: 'Preview' })).toBeTruthy());
    });

    it('stops preview playback when the panel unmounts', async () => {
        const pause = vi.fn();
        vi.stubGlobal('Audio', class {
            onended: (() => void) | null = null;
            onerror: (() => void) | null = null;
            pause = pause;
            play = vi.fn().mockResolvedValue(undefined);
        });
        const view = render(<TTSConfigPanel lang="en" />);
        fireEvent.click(await screen.findByRole('button', { name: 'Preview' }));
        expect(await screen.findByRole('button', { name: 'Generating...' })).toBeTruthy();

        view.unmount();

        expect(pause).toHaveBeenCalledTimes(1);
    });
});
