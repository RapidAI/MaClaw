import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { StrictMode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DialogProvider } from '../../CustomDialog';
import { ThirdPartyAccessSettings } from '../ThirdPartyAccessSettings';

const appMocks = vi.hoisted(() => ({
    deleteHardware: vi.fn(),
    generateHardwareWelcomeAudio: vi.fn(),
    getHardwareWelcomeAudioDataURL: vi.fn(),
	refreshDeviceAmbientWeather: vi.fn(),
	    listHardware: vi.fn(),
    loadConfig: vi.fn(),
	resetHardwareWelcomeAudio: vi.fn(),
	    restartThirdPartyGateway: vi.fn(),
	    sendHardwareWelcomeAudioRemote: vi.fn(),
	    sendHardwareVolume: vi.fn(),
	    setHardwareEnabled: vi.fn(),
	    setThirdPartyGatewayLocalMode: vi.fn(),
	    stopThirdPartyGateway: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    CreateThirdPartyDevicePairing: vi.fn(),
    DeleteThirdPartyHardwareDevice: appMocks.deleteHardware,
    GenerateHardwareWelcomeAudio: appMocks.generateHardwareWelcomeAudio,
    GetHardwareWelcomeAudioDataURL: appMocks.getHardwareWelcomeAudioDataURL,
    RefreshDeviceAmbientWeather: appMocks.refreshDeviceAmbientWeather,
    LoadConfig: appMocks.loadConfig,
    ListThirdPartyHardwareDevices: appMocks.listHardware,
	ResetHardwareWelcomeAudio: appMocks.resetHardwareWelcomeAudio,
	    RestartThirdPartyGateway: appMocks.restartThirdPartyGateway,
    SelectHardwareWelcomeAudio: vi.fn(),
    SendHardwareVolume: appMocks.sendHardwareVolume,
    SendHardwareWelcomeAudioRemote: appMocks.sendHardwareWelcomeAudioRemote,
    SetHardwareEnabled: appMocks.setHardwareEnabled,
	    SetThirdPartyGatewayLocalMode: appMocks.setThirdPartyGatewayLocalMode,
	    StopThirdPartyGateway: appMocks.stopThirdPartyGateway,
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: vi.fn(),
    EventsOn: vi.fn(() => vi.fn()),
}));

const renderSettings = (overrides: Record<string, unknown> = {}, propOverrides: Record<string, unknown> = {}) => {
    const config = {
        thirdparty_gateway_enabled: false,
        thirdparty_gateway_local_mode: true,
        hardware_enabled: false,
        hardware_volume: 70,
        ...overrides,
    } as any;
    const props = {
        config,
        setConfig: vi.fn(),
        lang: 'en',
        saveRemoteConfigField: vi.fn(),
        showToastMessage: vi.fn(),
        setIMAuditPlatform: vi.fn(),
        thirdPartyGatewayStatus: 'disconnected',
        setThirdPartyGatewayStatus: vi.fn(),
        thirdPartyGatewayLocalMode: Boolean(config.thirdparty_gateway_local_mode),
        setThirdPartyGatewayLocalModeState: vi.fn(),
        ...propOverrides,
    };
    const view = render(<DialogProvider><ThirdPartyAccessSettings {...props} /></DialogProvider>);
    return {
        ...props,
        rerenderSettings: (nextProps: Record<string, unknown>) => view.rerender(
            <DialogProvider><ThirdPartyAccessSettings {...props} {...nextProps} /></DialogProvider>,
        ),
    };
};

describe('ThirdPartyAccessSettings hardware enablement', () => {
    beforeEach(() => {
        appMocks.deleteHardware.mockReset();
        appMocks.getHardwareWelcomeAudioDataURL.mockReset();
        appMocks.generateHardwareWelcomeAudio.mockReset();
		appMocks.refreshDeviceAmbientWeather.mockReset();
		appMocks.refreshDeviceAmbientWeather.mockResolvedValue('');
        appMocks.listHardware.mockReset();
	        appMocks.listHardware.mockResolvedValue([]);
	        appMocks.restartThirdPartyGateway.mockReset();
	        appMocks.setHardwareEnabled.mockReset();
	        appMocks.setThirdPartyGatewayLocalMode.mockReset();
	        appMocks.stopThirdPartyGateway.mockReset();
        appMocks.sendHardwareWelcomeAudioRemote.mockReset();
        appMocks.sendHardwareVolume.mockReset();
        appMocks.loadConfig.mockReset();
		appMocks.resetHardwareWelcomeAudio.mockReset();
    });
    afterEach(() => {
        cleanup();
        vi.useRealTimers();
        vi.unstubAllGlobals();
    });

    it('keeps hardware weather settings in the hardware section and syncs a newly typed city', async () => {
        appMocks.refreshDeviceAmbientWeather.mockResolvedValue('Beijing');
        const props = renderSettings({ thirdparty_gateway_enabled: true, hardware_enabled: true });

        const city = screen.getByLabelText('Hardware weather city');
        fireEvent.change(city, { target: { value: 'Beijing' } });
        fireEvent.click(screen.getByRole('button', { name: 'Sync now' }));

        await waitFor(() => expect(props.saveRemoteConfigField).toHaveBeenCalledWith({ pet_ambient_city: 'Beijing' }));
        await waitFor(() => expect(appMocks.refreshDeviceAmbientWeather).toHaveBeenCalled());
        expect(await screen.findByText('Weather sent to hardware')).toBeTruthy();
    });

    it('does not let an earlier city save clear a newer pending city', async () => {
        let resolveFirst!: () => void;
        const saveRemoteConfigField = vi.fn()
            .mockImplementationOnce(() => new Promise<void>((resolve) => { resolveFirst = resolve; }))
            .mockResolvedValue(undefined);
        const props = renderSettings({ thirdparty_gateway_enabled: true, hardware_enabled: true }, { saveRemoteConfigField });

        const city = screen.getByLabelText('Hardware weather city');
        fireEvent.change(city, { target: { value: 'Bei' } });
        await waitFor(() => expect(saveRemoteConfigField).toHaveBeenCalledWith({ pet_ambient_city: 'Bei' }), { timeout: 1000 });
        fireEvent.change(city, { target: { value: 'Beijing' } });
        resolveFirst();
        fireEvent.click(screen.getByRole('button', { name: 'Sync now' }));

        await waitFor(() => expect(saveRemoteConfigField).toHaveBeenCalledWith({ pet_ambient_city: 'Beijing' }));
        await waitFor(() => expect(appMocks.refreshDeviceAmbientWeather).toHaveBeenCalled());
        expect(props.setConfig).not.toHaveBeenCalled();
    });

    it('keeps city keystrokes local and emits one debounced config write', async () => {
        vi.useFakeTimers();
        const saveRemoteConfigField = vi.fn().mockResolvedValue(undefined);
        const props = renderSettings(
            { thirdparty_gateway_enabled: true, hardware_enabled: true },
            { saveRemoteConfigField },
        );

        const city = screen.getByLabelText('Hardware weather city');
        fireEvent.change(city, { target: { value: 'B' } });
        fireEvent.change(city, { target: { value: 'Bei' } });
        fireEvent.change(city, { target: { value: 'Beijing' } });

        expect(city).toHaveProperty('value', 'Beijing');
        expect(props.setConfig).not.toHaveBeenCalled();
        expect(saveRemoteConfigField).not.toHaveBeenCalled();

        await act(async () => { await vi.advanceTimersByTimeAsync(350); });
        expect(saveRemoteConfigField).toHaveBeenCalledTimes(1);
        expect(saveRemoteConfigField).toHaveBeenCalledWith({ pet_ambient_city: 'Beijing' });
    });

    it('does not duplicate an already queued city save while unmounting', async () => {
        let resolveSave!: () => void;
        const saveRemoteConfigField = vi.fn(() => new Promise<void>((resolve) => { resolveSave = resolve; }));
        const view = renderSettings(
            { thirdparty_gateway_enabled: true, hardware_enabled: true },
            { saveRemoteConfigField },
        );

        fireEvent.change(screen.getByLabelText('Hardware weather city'), { target: { value: 'Beijing' } });
        await waitFor(() => expect(saveRemoteConfigField).toHaveBeenCalledTimes(1), { timeout: 1000 });
        view.rerenderSettings({ config: null });
        cleanup();

        expect(saveRemoteConfigField).toHaveBeenCalledTimes(1);
        resolveSave();
    });

    it('serializes city saves so an older backend response cannot overwrite the latest city', async () => {
        let resolveFirst!: () => void;
        const saveRemoteConfigField = vi.fn()
            .mockImplementationOnce(() => new Promise<void>((resolve) => { resolveFirst = resolve; }))
            .mockResolvedValue(undefined);
        renderSettings({ thirdparty_gateway_enabled: true, hardware_enabled: true }, { saveRemoteConfigField });

        const city = screen.getByLabelText('Hardware weather city');
        fireEvent.change(city, { target: { value: 'Bei' } });
        await waitFor(() => expect(saveRemoteConfigField).toHaveBeenCalledTimes(1), { timeout: 1000 });
        fireEvent.change(city, { target: { value: 'Beijing' } });
        await new Promise((resolve) => setTimeout(resolve, 400));
        expect(saveRemoteConfigField).toHaveBeenCalledTimes(1);

        resolveFirst();
        await waitFor(() => expect(saveRemoteConfigField).toHaveBeenNthCalledWith(2, { pet_ambient_city: 'Beijing' }));
    });

    it('restores the confirmed city and shows an error when saving fails', async () => {
        const saveRemoteConfigField = vi.fn().mockRejectedValue(new Error('save failed'));
        renderSettings(
            { thirdparty_gateway_enabled: true, hardware_enabled: true, pet_ambient_city: 'Shanghai' },
            { saveRemoteConfigField },
        );

        fireEvent.change(screen.getByLabelText('Hardware weather city'), { target: { value: 'Beijing' } });

        await waitFor(() => expect(saveRemoteConfigField).toHaveBeenCalled(), { timeout: 1000 });
        await waitFor(() => expect(screen.getByLabelText('Hardware weather city')).toHaveProperty('value', 'Shanghai'));
        expect(screen.getByRole('alert').textContent).toContain('Weather sync failed');
    });

    it('sends the latest volume after the slider changes without relying on pointerup', async () => {
        vi.useFakeTimers();
        appMocks.sendHardwareVolume.mockResolvedValue(undefined);
        appMocks.loadConfig.mockResolvedValue({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
            hardware_volume: 82,
        });
        renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

        const slider = screen.getByRole('slider', { name: /Volume/ });
        fireEvent.input(slider, { target: { value: '81' } });
        fireEvent.input(slider, { target: { value: '82' } });
        await act(async () => { await vi.advanceTimersByTimeAsync(120); });

        expect(appMocks.sendHardwareVolume).toHaveBeenCalledTimes(1);
        expect(appMocks.sendHardwareVolume).toHaveBeenCalledWith(82);
    });

    it('flushes a release value that arrives while an earlier volume write is still running', async () => {
        let finishFirstWrite!: () => void;
        appMocks.sendHardwareVolume
            .mockImplementationOnce(() => new Promise<void>((resolve) => { finishFirstWrite = resolve; }))
            .mockResolvedValue(undefined);
        renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });
        const slider = screen.getByRole('slider', { name: /Volume/ });

        fireEvent.pointerUp(slider, { target: { value: '40' } });
        await waitFor(() => expect(appMocks.sendHardwareVolume).toHaveBeenCalledWith(40));
        fireEvent.pointerUp(slider, { target: { value: '73' } });
        finishFirstWrite();

        await waitFor(() => expect(appMocks.sendHardwareVolume).toHaveBeenLastCalledWith(73));
        expect(appMocks.sendHardwareVolume).toHaveBeenCalledTimes(2);
    });

    it('enables hardware through the atomic backend operation and reflects Hub mode', async () => {
        appMocks.setHardwareEnabled.mockResolvedValue('connected');
        appMocks.loadConfig.mockResolvedValue({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
        });
        const props = renderSettings();

        fireEvent.click(screen.getByRole('checkbox', { name: 'Enable hardware' }));

        await waitFor(() => expect(appMocks.setHardwareEnabled).toHaveBeenCalledWith(true));
        expect(props.setThirdPartyGatewayStatus).toHaveBeenCalledWith('connected');
        expect(props.setThirdPartyGatewayLocalModeState).toHaveBeenCalledWith(false);
        expect(props.showToastMessage).toHaveBeenCalledWith(expect.stringContaining('Hub mode'));
    });

    it('locks gateway disable and local mode while hardware is enabled', () => {
        renderSettings({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
        });

        expect((screen.getByRole('checkbox', { name: 'Enable third-party access' }) as HTMLInputElement).disabled).toBe(true);
        expect((screen.getByRole('button', { name: 'Handle with local Agent' }) as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByRole('button', { name: 'Get code' }) as HTMLButtonElement).disabled).toBe(false);
        expect((screen.getByPlaceholderText('Bearer token') as HTMLInputElement).disabled).toBe(true);
        expect((screen.getByRole('button', { name: 'Generate Token' }) as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByDisplayValue('127.0.0.1') as HTMLInputElement).disabled).toBe(true);
        expect((screen.getByDisplayValue('18777') as HTMLInputElement).disabled).toBe(true);
    });

	    it('keeps hardware controls disabled until hardware is enabled', () => {
        renderSettings({ thirdparty_gateway_enabled: true, hardware_enabled: false });
        expect((screen.getByRole('button', { name: 'Get code' }) as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByRole('slider', { name: /Volume/ }) as HTMLInputElement).disabled).toBe(true);
	        expect((screen.getByRole('button', { name: 'Test remote hardware' }) as HTMLButtonElement).disabled).toBe(true);
	    });

    it('allows Welcome setup and local generation before hardware is enabled', () => {
        renderSettings({ thirdparty_gateway_enabled: false, hardware_enabled: false });

        expect((screen.getByRole('checkbox', { name: 'Enabled' }) as HTMLInputElement).disabled).toBe(false);
        expect((screen.getByRole('combobox', { name: 'English voice' }) as HTMLSelectElement).disabled).toBe(false);
        expect((screen.getByPlaceholderText('For example: Hello, Maclaw') as HTMLTextAreaElement).disabled).toBe(false);
        expect((screen.getByRole('button', { name: 'Generate audio' }) as HTMLButtonElement).disabled).toBe(false);
        expect((screen.getByRole('button', { name: 'Choose audio' }) as HTMLButtonElement).disabled).toBe(false);
    });


	    it('restores confirmed config and unlocks the gateway toggle after a save failure', async () => {
	        const props = renderSettings();
	        props.saveRemoteConfigField.mockRejectedValue(new Error('save failed'));
	        appMocks.loadConfig.mockResolvedValue({ thirdparty_gateway_enabled: false, thirdparty_gateway_local_mode: true, hardware_enabled: false });
	        const toggle = screen.getByRole('checkbox', { name: 'Enable third-party access' }) as HTMLInputElement;

	        fireEvent.click(toggle);
	        expect(toggle.disabled).toBe(true);

	        await waitFor(() => expect(props.showToastMessage).toHaveBeenCalledWith('save failed'));
	        await waitFor(() => expect(toggle.disabled).toBe(false));
	        expect(appMocks.restartThirdPartyGateway).not.toHaveBeenCalled();
	    });

	    it('serializes mode changes and restores the confirmed mode after failure', async () => {
	        appMocks.setThirdPartyGatewayLocalMode.mockRejectedValue(new Error('mode failed'));
	        appMocks.loadConfig.mockResolvedValue({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: true, hardware_enabled: false });
	        const props = renderSettings({ thirdparty_gateway_enabled: true });
	        const hubButton = screen.getByRole('button', { name: 'Forward through Hub' }) as HTMLButtonElement;

	        fireEvent.click(hubButton);
	        expect(hubButton.disabled).toBe(true);

	        await waitFor(() => expect(props.setThirdPartyGatewayLocalModeState).toHaveBeenLastCalledWith(true));
	        await waitFor(() => expect(hubButton.disabled).toBe(false));
	        expect(props.showToastMessage).toHaveBeenCalledWith('mode failed');
	    });

    it('plays a prepared welcome WAV in the GUI without requiring hardware enablement', async () => {
        appMocks.getHardwareWelcomeAudioDataURL.mockResolvedValue('data:audio/wav;base64,UklGRg==');
        const play = vi.fn().mockImplementation(function (this: HTMLAudioElement) {
            queueMicrotask(() => this.onended?.(new Event('ended')));
            return Promise.resolve();
        });
        vi.stubGlobal('Audio', class {
            onended: ((event: Event) => void) | null = null;
            onerror: ((event: Event) => void) | null = null;
            pause = vi.fn();
            play = play;
        });
        const props = renderSettings({
            hardware_enabled: false,
            hardware_welcome_audio_path: 'C:/welcome.wav',
        });

        fireEvent.click(screen.getByRole('button', { name: 'Preview in GUI' }));

        await waitFor(() => expect(appMocks.getHardwareWelcomeAudioDataURL).toHaveBeenCalledTimes(1));
        await waitFor(() => expect(props.showToastMessage).toHaveBeenCalledWith('Local GUI playback completed.'));
        expect(appMocks.sendHardwareWelcomeAudioRemote).not.toHaveBeenCalled();
    });

    it('resets Welcome audio to the embedded recording and refreshes its preview', async () => {
        appMocks.getHardwareWelcomeAudioDataURL.mockResolvedValue('data:audio/wav;base64,OLD');
        appMocks.resetHardwareWelcomeAudio.mockResolvedValue('C:/hardware/welcome.wav');
        appMocks.loadConfig.mockResolvedValue({
            thirdparty_gateway_enabled: true,
            hardware_enabled: true,
            hardware_welcome_text: 'Hello, Maclaw',
            hardware_welcome_audio_path: 'C:/hardware/welcome.wav',
        });
        const props = renderSettings({
            thirdparty_gateway_enabled: true,
            hardware_enabled: true,
            hardware_welcome_audio_path: 'C:/custom.wav',
        });
        await waitFor(() => expect(appMocks.getHardwareWelcomeAudioDataURL).toHaveBeenCalledTimes(1));
        appMocks.getHardwareWelcomeAudioDataURL.mockResolvedValue('data:audio/wav;base64,DEFAULT');

        fireEvent.click(screen.getByRole('button', { name: 'Reset' }));

        await waitFor(() => expect(appMocks.resetHardwareWelcomeAudio).toHaveBeenCalledTimes(1));
        await waitFor(() => expect(appMocks.getHardwareWelcomeAudioDataURL).toHaveBeenCalledTimes(2));
        expect(props.setConfig).toHaveBeenCalledWith(expect.objectContaining({ hardware_welcome_audio_path: 'C:/hardware/welcome.wav' }));
        expect(props.showToastMessage).toHaveBeenCalledWith('Default Welcome recording restored.');
    });

    it('defaults to the sweet female English voice and passes it directly to generation', async () => {
        appMocks.generateHardwareWelcomeAudio.mockResolvedValue('C:/hardware/welcome.wav');
        appMocks.loadConfig.mockResolvedValue({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
            hardware_welcome_text: 'Hello, Maclaw',
            hardware_welcome_voice_id: 'af_heart',
            hardware_welcome_audio_path: 'C:/hardware/welcome.wav',
        });
        renderSettings({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
            hardware_welcome_text: 'Hello, Maclaw',
        });

        expect((screen.getByRole('combobox', { name: 'English voice' }) as HTMLSelectElement).value).toBe('af_heart');
        fireEvent.click(screen.getByRole('button', { name: 'Generate audio' }));

        await waitFor(() => expect(appMocks.generateHardwareWelcomeAudio).toHaveBeenCalledWith('Hello, Maclaw', 'af_heart'));
    });

    it('selects and generates with the natural male voice without a save race', async () => {
        appMocks.generateHardwareWelcomeAudio.mockResolvedValue('C:/hardware/welcome.wav');
        let finishVoiceSave!: () => void;
        appMocks.loadConfig.mockResolvedValue({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
            hardware_welcome_text: 'Hello, Maclaw',
            hardware_welcome_voice_id: 'am_adam',
            hardware_welcome_audio_path: 'C:/hardware/welcome.wav',
        });
        const props = renderSettings({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
            hardware_welcome_text: 'Hello, Maclaw',
            hardware_welcome_voice_id: 'af_heart',
        });
        props.saveRemoteConfigField.mockImplementation(() => new Promise<void>((resolve) => { finishVoiceSave = resolve; }));

        fireEvent.change(screen.getByRole('combobox', { name: 'English voice' }), { target: { value: 'am_adam' } });
        expect(props.saveRemoteConfigField).toHaveBeenCalledWith({ hardware_welcome_voice_id: 'am_adam' });
        expect((screen.getByRole('button', { name: 'Generate audio' }) as HTMLButtonElement).disabled).toBe(true);
        finishVoiceSave();
        await waitFor(() => expect((screen.getByRole('button', { name: 'Generate audio' }) as HTMLButtonElement).disabled).toBe(false));
        fireEvent.click(screen.getByRole('button', { name: 'Generate audio' }));

        await waitFor(() => expect(appMocks.generateHardwareWelcomeAudio).toHaveBeenCalledWith('Hello, Maclaw', 'am_adam'));
    });

    it('restores the confirmed voice when saving a voice selection fails', async () => {
        const props = renderSettings({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
            hardware_welcome_voice_id: 'af_heart',
        });
        props.saveRemoteConfigField.mockRejectedValue(new Error('voice save failed'));

        fireEvent.change(screen.getByRole('combobox', { name: 'English voice' }), { target: { value: 'am_adam' } });

        await waitFor(() => expect((screen.getByRole('combobox', { name: 'English voice' }) as HTMLSelectElement).value).toBe('af_heart'));
    });

    it('reuses the preloaded WAV source for GUI playback', async () => {
        appMocks.getHardwareWelcomeAudioDataURL.mockResolvedValue('data:audio/wav;base64,UklGRg==');
        const play = vi.fn().mockImplementation(function (this: HTMLAudioElement) {
            queueMicrotask(() => this.onended?.(new Event('ended')));
            return Promise.resolve();
        });
        vi.stubGlobal('Audio', class {
            onended: ((event: Event) => void) | null = null;
            onerror: ((event: Event) => void) | null = null;
            pause = vi.fn();
            play = play;
        });
        renderSettings({ hardware_welcome_audio_path: 'C:/welcome.wav' });
        await waitFor(() => expect(appMocks.getHardwareWelcomeAudioDataURL).toHaveBeenCalledTimes(1));

        fireEvent.click(screen.getByRole('button', { name: 'Preview in GUI' }));

        await waitFor(() => expect(play).toHaveBeenCalledTimes(1));
        expect(appMocks.getHardwareWelcomeAudioDataURL).toHaveBeenCalledTimes(1);
    });

    it('releases an interrupted GUI preview and does not report false completion', async () => {
        appMocks.getHardwareWelcomeAudioDataURL.mockResolvedValue('data:audio/wav;base64,UklGRg==');
        const pause = vi.fn();
        vi.stubGlobal('Audio', class {
            onended: ((event: Event) => void) | null = null;
            onerror: ((event: Event) => void) | null = null;
            pause = pause;
            play = vi.fn().mockResolvedValue(undefined);
        });
        const props = renderSettings({ hardware_welcome_audio_path: 'C:/welcome.wav' });

        fireEvent.click(screen.getByRole('button', { name: 'Preview in GUI' }));
        await waitFor(() => expect(appMocks.getHardwareWelcomeAudioDataURL).toHaveBeenCalledTimes(1));
        cleanup();

        expect(pause).toHaveBeenCalledTimes(1);
        expect(props.showToastMessage).not.toHaveBeenCalledWith('Local GUI playback completed.');
    });

    it('keeps remote hardware testing on the Hub path', async () => {
        appMocks.sendHardwareWelcomeAudioRemote.mockResolvedValue(undefined);
        const props = renderSettings({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
            hardware_welcome_audio_path: 'C:/welcome.wav',
        });

        fireEvent.click(screen.getByRole('button', { name: 'Test remote hardware' }));

        await waitFor(() => expect(appMocks.sendHardwareWelcomeAudioRemote).toHaveBeenCalledTimes(1));
        // The GUI source may be prefetched, but the remote button must never
        // create or play a desktop Audio element.
        expect(appMocks.getHardwareWelcomeAudioDataURL).toHaveBeenCalledTimes(1);
        expect(props.showToastMessage).toHaveBeenCalledWith('Remote ESP32 confirmed playback.');
    });

    it('shows an actionable Chinese error when no compatible remote ESP32 is online', async () => {
        appMocks.sendHardwareWelcomeAudioRemote.mockRejectedValue(new Error('Hub rejected request (NO_COMPATIBLE_HARDWARE): no online remote ESP32 supports welcome audio playback'));
        const props = renderSettings({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
            hardware_welcome_audio_path: 'C:/welcome.wav',
        }, { lang: 'zh-Hans' });

        fireEvent.click(screen.getByRole('button', { name: '远程硬件测试' }));

        await waitFor(() => expect(props.showToastMessage).toHaveBeenCalledWith(expect.stringContaining('没有在线且支持欢迎音频播放')));
        expect(props.showToastMessage).toHaveBeenCalledWith(expect.stringContaining('检查设备联网、配对状态和固件能力'));
    });

    it('lists independent clients and removes one binding', async () => {
        appMocks.listHardware.mockResolvedValue([
            { clientId: 'esp32s3-a1', clientName: 'Desk Pet', protocolVersion: '1.1', online: true },
            { clientId: 'esp32s3-b2', clientName: 'Kitchen Pet', protocolVersion: '1.1', online: false },
        ]);
        appMocks.deleteHardware.mockResolvedValue(undefined);
        renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

        expect(await screen.findByText('Desk Pet')).toBeTruthy();
        expect(screen.getByText('Kitchen Pet')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Remove Desk Pet' }));
        expect(await screen.findByRole('heading', { name: 'Remove hardware?' })).toBeTruthy();
        expect(screen.getByText(/token will be revoked immediately/i)).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Remove' }));
        await waitFor(() => expect(appMocks.deleteHardware).toHaveBeenCalledWith('esp32s3-a1'));
        await waitFor(() => expect(screen.queryByText('Desk Pet')).toBeNull());
    });

    it('loads and removes hardware under React StrictMode', async () => {
        appMocks.listHardware.mockResolvedValue([
            { clientId: 'esp32s3-strict', clientName: 'Strict Pet', online: true },
        ]);
        appMocks.deleteHardware.mockResolvedValue(undefined);
        const config = {
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
            hardware_volume: 70,
        } as any;
        const props = {
            config,
            setConfig: vi.fn(),
            lang: 'en',
            saveRemoteConfigField: vi.fn(),
            showToastMessage: vi.fn(),
            setIMAuditPlatform: vi.fn(),
            thirdPartyGatewayStatus: 'connected',
            setThirdPartyGatewayStatus: vi.fn(),
            thirdPartyGatewayLocalMode: false,
            setThirdPartyGatewayLocalModeState: vi.fn(),
        };

        render(
            <StrictMode>
                <DialogProvider><ThirdPartyAccessSettings {...props} /></DialogProvider>
            </StrictMode>,
        );

        expect(await screen.findByText('Strict Pet')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Remove Strict Pet' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Remove' }));
        await waitFor(() => expect(appMocks.deleteHardware).toHaveBeenCalledWith('esp32s3-strict'));
        await waitFor(() => expect(screen.queryByText('Strict Pet')).toBeNull());
    });

    it('does not restore an unbound device when an older list request finishes late', async () => {
        let finishRefresh!: (devices: unknown[]) => void;
        appMocks.listHardware
            .mockResolvedValueOnce([{ clientId: 'esp32s3-race', clientName: 'Race Pet', online: true }])
            .mockImplementationOnce(() => new Promise((resolve) => { finishRefresh = resolve; }));
        appMocks.deleteHardware.mockResolvedValue(undefined);
        renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

        expect(await screen.findByText('Race Pet')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
        await waitFor(() => expect(appMocks.listHardware).toHaveBeenCalledTimes(2));
        fireEvent.click(screen.getByRole('button', { name: 'Remove Race Pet' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Remove' }));
        await waitFor(() => expect(appMocks.deleteHardware).toHaveBeenCalledWith('esp32s3-race'));

        finishRefresh([{ clientId: 'esp32s3-race', clientName: 'Race Pet', online: true }]);
        await act(async () => { await Promise.resolve(); });
        expect(screen.queryByText('Race Pet')).toBeNull();
        expect((screen.getByRole('button', { name: 'Refresh' }) as HTMLButtonElement).disabled).toBe(false);
    });

    it('keeps the binding when the custom removal dialog is cancelled', async () => {
        appMocks.listHardware.mockResolvedValue([{ clientId: 'esp32s3-stay', clientName: 'Stay Pet', online: true }]);
        renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

        expect(await screen.findByText('Stay Pet')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Remove Stay Pet' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }));

        expect(appMocks.deleteHardware).not.toHaveBeenCalled();
        expect(screen.getByText('Stay Pet')).toBeTruthy();
    });

    it('shows a retry state instead of a false empty state when listing fails', async () => {
        appMocks.listHardware.mockRejectedValueOnce(new Error('hub unavailable')).mockResolvedValueOnce([
            { clientId: 'esp32s3-retry', clientName: 'Recovered Pet', online: true },
        ]);
        const props = renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

        expect((await screen.findByRole('alert')).textContent).toContain('Could not load hardware');
        expect(screen.queryByText('No hardware is bound yet.')).toBeNull();
        expect(props.showToastMessage).toHaveBeenCalledWith('hub unavailable');

        fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
        expect(await screen.findByText('Recovered Pet')).toBeTruthy();
    });

    it('ignores errors from an obsolete hardware-list request', async () => {
        let failFirst!: (error: Error) => void;
        appMocks.listHardware
            .mockImplementationOnce(() => new Promise((_, reject) => { failFirst = reject; }))
            .mockResolvedValueOnce([{ clientId: 'esp32s3-current', clientName: 'Current Pet', online: true }]);
        const props = renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

        props.rerenderSettings({ thirdPartyGatewayStatus: 'connected' });
        expect(await screen.findByText('Current Pet')).toBeTruthy();
        failFirst(new Error('obsolete failure'));
        await act(async () => { await Promise.resolve(); });

        expect(props.showToastMessage).not.toHaveBeenCalledWith('obsolete failure');
        expect(screen.queryByRole('alert')).toBeNull();
    });

    it('keeps a device visible when unlinking fails', async () => {
        appMocks.listHardware.mockResolvedValue([{ clientId: 'esp32s3-safe', clientName: 'Safe Pet', online: false }]);
        appMocks.deleteHardware.mockRejectedValue(new Error('delete failed'));
        const props = renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

        expect(await screen.findByText('Safe Pet')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Remove Safe Pet' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Remove' }));
        await waitFor(() => expect(props.showToastMessage).toHaveBeenCalledWith('delete failed'));
        expect(screen.getByText('Safe Pet')).toBeTruthy();
    });
});
