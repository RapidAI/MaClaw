// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { BotVoiceSettingsDialog } from './BotVoiceSettingsDialog';

const calls = vi.hoisted(() => ({
    patch: vi.fn(async (..._args: unknown[]) => ({})),
    setEnabled: vi.fn(async (..._args: unknown[]) => undefined),
    setVoice: vi.fn(async (..._args: unknown[]) => undefined),
}));

vi.mock('../../../wailsjs/go/main/App', () => ({
    GetTTSEnabled: () => Promise.resolve(true),
    GetTTSVoiceID: () => Promise.resolve('zf_xiaoxiao'),
    LoadConfig: () => Promise.resolve({ bot_voice_readback_enabled: true }),
    PatchConfigFields: (...args: unknown[]) => calls.patch(...args),
    SetTTSEnabled: (...args: unknown[]) => calls.setEnabled(...args),
    SetTTSVoiceID: (...args: unknown[]) => calls.setVoice(...args),
    SynthesizeTTSPreview: () => Promise.resolve('data:audio/wav;base64,'),
}));

describe('Bot voice settings dialog', () => {
    it('loads TTS + readback state and patches readback on toggle', async () => {
        render(<BotVoiceSettingsDialog heading="设置" lang="zh-Hans" onClose={() => undefined} />);
        const enabled = await screen.findByTestId('bot-voice-enabled') as HTMLInputElement;
        const readback = screen.getByTestId('bot-voice-readback') as HTMLInputElement;
        const select = screen.getByTestId('bot-voice-select') as HTMLSelectElement;
        await waitFor(() => {
            expect(enabled.checked).toBe(true);
            expect(readback.checked).toBe(true);
            expect(select.value).toBe('zf_xiaoxiao');
        });
        fireEvent.click(readback);
        await waitFor(() => expect(calls.patch).toHaveBeenCalledWith({ bot_voice_readback_enabled: false }));
        fireEvent.click(enabled);
        await waitFor(() => expect(calls.setEnabled).toHaveBeenCalledWith(false));
        fireEvent.change(select, { target: { value: 'am_adam' } });
        await waitFor(() => expect(calls.setVoice).toHaveBeenCalledWith('am_adam'));
    });
});
