// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { BuddySettingsSection } from '../BuddySettingsSection';
import type { SoundConfig } from '../BuddySettingsSection';

const BuddyGetStatusMock = vi.fn();
const BuddyListPortsMock = vi.fn();
const BuddyGetSoundConfigMock = vi.fn();
const BuddySetSoundConfigMock = vi.fn();
const BuddyStartMock = vi.fn();
const BuddyStopMock = vi.fn();
const BuddyRefreshWeatherMock = vi.fn();
const BuddyRefreshPetMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    BuddyGetStatus: () => BuddyGetStatusMock(),
    BuddyListPorts: () => BuddyListPortsMock(),
    BuddyGetSoundConfig: () => BuddyGetSoundConfigMock(),
    BuddySetSoundConfig: (config: SoundConfig) => BuddySetSoundConfigMock(config),
    BuddyStart: (port: string) => BuddyStartMock(port),
    BuddyStop: () => BuddyStopMock(),
    BuddyRefreshWeather: () => BuddyRefreshWeatherMock(),
    BuddyRefreshPet: () => BuddyRefreshPetMock(),
}));

afterEach(() => {
    cleanup();
    vi.clearAllMocks();
});

const connectedStatus = {
    enabled: true,
    connected: true,
    port: 'COM3',
    device_board: 'esp32-s3',
    device_fw: '1.2.0',
    device_touch: false,
    device_key: true,
    device_sound: true,
    device_width: 240,
    device_height: 280,
    sound_master: true,
    state_latency_ms: 12,
    page: 'tasks',
    snapshot_age_s: 3,
};

const ports = [
    { name: 'COM3', description: 'USB Serial Device', is_buddy: true },
    { name: 'COM7', description: 'USB Serial Device', is_buddy: false },
];

const soundConfig: SoundConfig = {
    master: true,
    running: { enabled: true, effect: 0 },
    complete: { enabled: true, effect: 1 },
    standby: { enabled: false, effect: 2 },
    attention: { enabled: true, effect: 3 },
};

function renderSection(status = connectedStatus, lang = 'zh-Hans') {
    BuddyGetStatusMock.mockResolvedValue(status);
    BuddyListPortsMock.mockResolvedValue(ports);
    BuddyGetSoundConfigMock.mockResolvedValue(soundConfig);
    return render(<BuddySettingsSection lang={lang} />);
}

describe('BuddySettingsSection', () => {
    it('renders the link state strip with device chips when connected', async () => {
        renderSection();

        expect(await screen.findByText('已连接')).toBeTruthy();
        expect(screen.getByText('COM3')).toBeTruthy();
        expect(screen.getByText('固件 1.2.0')).toBeTruthy();
        expect(screen.getByText('240×280')).toBeTruthy();
        expect(screen.getByText('12 ms')).toBeTruthy();
    });

    it('hides the port toolbar and action rows while the link is disabled', async () => {
        renderSection({ ...connectedStatus, enabled: false, connected: false });

        expect(await screen.findByText('未连接')).toBeTruthy();
        expect(screen.queryByLabelText('伴侣屏端口')).not.toBeTruthy();
        expect(screen.queryByRole('button', { name: '立即推送' })).not.toBeTruthy();
    });

    it('starts the service on the selected port when the switch is turned on', async () => {
        renderSection({ ...connectedStatus, enabled: false, connected: false });

        fireEvent.click(await screen.findByLabelText('启用伴侣屏连接'));

        await waitFor(() => expect(BuddyStartMock).toHaveBeenCalledWith('COM3'));
    });

    it('starts the service again when the port changes while enabled', async () => {
        renderSection();

        fireEvent.change(await screen.findByLabelText('伴侣屏端口'), { target: { value: 'COM7' } });

        await waitFor(() => expect(BuddyStartMock).toHaveBeenCalledWith('COM7'));
    });

    it('saves the sound config optimistically when the master switch is turned off', async () => {
        renderSection();

        fireEvent.click(await screen.findByLabelText('启用音效'));

        await waitFor(() =>
            expect(BuddySetSoundConfigMock).toHaveBeenCalledWith(expect.objectContaining({ master: false })),
        );
    });

    it('marks warning chips when touch is missing and audio is unavailable', async () => {
        renderSection({ ...connectedStatus, device_key: false, device_sound: false });

        expect(await screen.findByText('音效不可用')).toBeTruthy();
        expect(screen.getByText('触摸未检测到')).toBeTruthy();
    });
});
