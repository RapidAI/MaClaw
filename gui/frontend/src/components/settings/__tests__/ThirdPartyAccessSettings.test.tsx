import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { StrictMode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DialogProvider } from '../../CustomDialog';
import { ThirdPartyAccessSettings } from '../ThirdPartyAccessSettings';

const appMocks = vi.hoisted(() => ({
	createPairing: vi.fn(),
    deleteHardware: vi.fn(),
    generateHardwareWelcomeAudio: vi.fn(),
    getHardwareWelcomeAudioDataURL: vi.fn(),
	refreshDeviceAmbientWeather: vi.fn(),
	    listHardwareBindings: vi.fn(),
	    listExperts: vi.fn(),
    loadConfig: vi.fn(),
	resetHardwareWelcomeAudio: vi.fn(),
	    restartThirdPartyGateway: vi.fn(),
	    sendHardwareWelcomeAudioRemote: vi.fn(),
	    sendHardwareDeviceVolume: vi.fn(),
	    sendHardwareDeviceBrightness: vi.fn(),
	    sendHardwareDeviceScreenSleepTimeout: vi.fn(),
	    sendHardwareDevicePetProfile: vi.fn(),
	    listPetPacks: vi.fn(),
	    getPetPackPreviewDataURL: vi.fn(),
	    setHardwareEnabled: vi.fn(),
	    setHardwareAllowCustomPets: vi.fn(),
	    setHardwareAgentBinding: vi.fn(),
	    setThirdPartyHardwareDeviceAlias: vi.fn(),
	    setThirdPartyGatewayLocalMode: vi.fn(),
	    stopThirdPartyGateway: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    CreateThirdPartyDevicePairing: appMocks.createPairing,
    DeleteThirdPartyHardwareDevice: appMocks.deleteHardware,
    GenerateHardwareWelcomeAudio: appMocks.generateHardwareWelcomeAudio,
    GetHardwareWelcomeAudioDataURL: appMocks.getHardwareWelcomeAudioDataURL,
	GetPetPackPreviewDataURL: appMocks.getPetPackPreviewDataURL,
    RefreshDeviceAmbientWeather: appMocks.refreshDeviceAmbientWeather,
	LoadConfigForUI: appMocks.loadConfig,
	ListThirdPartyHardwareDeviceBindings: appMocks.listHardwareBindings,
	ListExperts: appMocks.listExperts,
	ListPetPacks: appMocks.listPetPacks,
	ResetHardwareWelcomeAudio: appMocks.resetHardwareWelcomeAudio,
	    RestartThirdPartyGateway: appMocks.restartThirdPartyGateway,
    SelectHardwareWelcomeAudio: vi.fn(),
	SendHardwareDeviceVolume: appMocks.sendHardwareDeviceVolume,
	SendHardwareDeviceBrightness: appMocks.sendHardwareDeviceBrightness,
	SendHardwareDeviceScreenSleepTimeout: appMocks.sendHardwareDeviceScreenSleepTimeout,
	SendHardwareDevicePetProfile: appMocks.sendHardwareDevicePetProfile,
	    SendHardwareWelcomeAudioRemote: appMocks.sendHardwareWelcomeAudioRemote,
	    SetHardwareEnabled: appMocks.setHardwareEnabled,
	    SetHardwareAllowCustomPets: appMocks.setHardwareAllowCustomPets,
	    SetHardwareAgentBinding: appMocks.setHardwareAgentBinding,
	    SetThirdPartyHardwareDeviceAlias: appMocks.setThirdPartyHardwareDeviceAlias,
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
        mode: 'hardware' as const,
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
		appMocks.createPairing.mockReset();
        appMocks.deleteHardware.mockReset();
        appMocks.getHardwareWelcomeAudioDataURL.mockReset();
        appMocks.generateHardwareWelcomeAudio.mockReset();
		appMocks.refreshDeviceAmbientWeather.mockReset();
		appMocks.refreshDeviceAmbientWeather.mockResolvedValue('');
	        appMocks.listHardwareBindings.mockReset();
	        appMocks.listHardwareBindings.mockResolvedValue({ devices: [], maxDevices: 5, boundCount: 0 });
	        appMocks.listExperts.mockReset();
	        appMocks.listExperts.mockResolvedValue('[]');
	        appMocks.restartThirdPartyGateway.mockReset();
	    appMocks.setHardwareEnabled.mockReset();
	        appMocks.setThirdPartyGatewayLocalMode.mockReset();
	        appMocks.stopThirdPartyGateway.mockReset();
		appMocks.sendHardwareWelcomeAudioRemote.mockReset();
		appMocks.sendHardwareDeviceVolume.mockReset();
		appMocks.sendHardwareDeviceBrightness.mockReset();
		appMocks.sendHardwareDeviceScreenSleepTimeout.mockReset();
		appMocks.sendHardwareDevicePetProfile.mockReset();
		appMocks.setHardwareAllowCustomPets.mockReset();
		appMocks.setHardwareAllowCustomPets.mockResolvedValue(undefined);
		appMocks.setHardwareAgentBinding.mockReset();
		appMocks.setHardwareAgentBinding.mockResolvedValue(undefined);
		appMocks.setThirdPartyHardwareDeviceAlias.mockReset();
		appMocks.setThirdPartyHardwareDeviceAlias.mockResolvedValue(undefined);
		appMocks.listPetPacks.mockReset();
		appMocks.listPetPacks.mockResolvedValue([]);
		appMocks.getPetPackPreviewDataURL.mockReset();
		appMocks.getPetPackPreviewDataURL.mockResolvedValue('');
        appMocks.loadConfig.mockReset();
		appMocks.resetHardwareWelcomeAudio.mockReset();
    });
    afterEach(() => {
        cleanup();
        vi.useRealTimers();
        vi.unstubAllGlobals();
    });

	it('keeps hardware controls available when the IM gateway is disabled', async () => {
		appMocks.listHardwareBindings.mockResolvedValue({
			devices: [{ clientId: 'hub-device', clientName: 'Hub Device', online: true }],
			maxDevices: 5,
			boundCount: 1,
		});
		renderSettings({ hardware_enabled: true, thirdparty_gateway_enabled: false }, { mode: 'hardware' });

		const refresh = await screen.findByRole('button', { name: 'Refresh' });
		expect((refresh as HTMLButtonElement).disabled).toBe(false);
		expect((screen.getByRole('button', { name: 'Get code' }) as HTMLButtonElement).disabled).toBe(false);
		expect(screen.queryByRole('checkbox', { name: 'Enable third-party access' })).toBeNull();
		expect(screen.queryByRole('button', { name: 'Generate Token' })).toBeNull();
	});

	it('does not issue a pairing code when the Hub-reported hardware limit is reached', async () => {
		appMocks.listHardwareBindings.mockResolvedValue({
			devices: [{ clientId: 'hub-device', clientName: 'Hub Device', online: true }],
			maxDevices: 5,
			boundCount: 5,
		});
		renderSettings({ hardware_enabled: true });

		const button = await screen.findByRole('button', { name: 'Get code' });
		expect((button as HTMLButtonElement).disabled).toBe(true);
		expect(appMocks.createPairing).not.toHaveBeenCalled();
	});

	it('prevents duplicate pairing-code requests while one is pending', async () => {
		let resolvePairing!: (value: { pairCode: string }) => void;
		appMocks.createPairing.mockImplementationOnce(() => new Promise((resolve) => { resolvePairing = resolve; }));
		renderSettings({ hardware_enabled: true });

		const button = await screen.findByRole('button', { name: 'Get code' });
		fireEvent.click(button);
		fireEvent.click(button);
		await waitFor(() => expect(appMocks.createPairing).toHaveBeenCalledTimes(1));
		expect(button).toHaveProperty('disabled', true);
		expect(button.textContent).toBe('Generating…');
		expect(screen.getByRole('button', { name: 'Refresh' })).toHaveProperty('disabled', false);
		expect(screen.getByRole('checkbox', { name: 'Enable hardware' })).toHaveProperty('disabled', false);

		await act(async () => resolvePairing({ pairCode: 'PAIR-1234' }));
		await waitFor(() => expect(screen.getByText('PAIR-1234')).toBeTruthy());
		expect(screen.getByRole('button', { name: 'Regenerate' })).toHaveProperty('disabled', false);
	});

	it('discards a pairing code that returns after hardware is disabled', async () => {
		let resolvePairing!: (value: { pairCode: string }) => void;
		appMocks.createPairing.mockImplementationOnce(() => new Promise((resolve) => { resolvePairing = resolve; }));
		const props = renderSettings({ hardware_enabled: true });

		fireEvent.click(screen.getByRole('button', { name: 'Get code' }));
		await waitFor(() => expect(appMocks.createPairing).toHaveBeenCalledTimes(1));
		props.rerenderSettings({ config: { ...props.config, hardware_enabled: false } });
		await act(async () => resolvePairing({ pairCode: 'STALE-CODE' }));

		await waitFor(() => expect(screen.queryByText('STALE-CODE')).toBeNull());
	});

	it('discards a pairing code when the hardware page is no longer active', async () => {
		let resolvePairing!: (value: { pairCode: string }) => void;
		appMocks.createPairing.mockImplementationOnce(() => new Promise((resolve) => { resolvePairing = resolve; }));
		const props = renderSettings({ hardware_enabled: true });

		fireEvent.click(screen.getByRole('button', { name: 'Get code' }));
		await waitFor(() => expect(appMocks.createPairing).toHaveBeenCalledTimes(1));
		props.rerenderSettings({ mode: 'im' });
		await act(async () => resolvePairing({ pairCode: 'HIDDEN-CODE' }));

		await waitFor(() => expect(screen.queryByText('HIDDEN-CODE')).toBeNull());
	});

	it('removes an expired pairing code and tells the user to generate another', async () => {
		vi.useFakeTimers();
		appMocks.createPairing.mockResolvedValue({
			pairCode: 'EXPIRING-CODE',
			expiresAt: new Date(Date.now() + 1000).toISOString(),
		});
		const props = renderSettings({ hardware_enabled: true });

		fireEvent.click(screen.getByRole('button', { name: 'Get code' }));
		await act(async () => { await vi.runAllTicks(); });
		expect(screen.getByText('EXPIRING-CODE')).toBeTruthy();
		await act(async () => { await vi.advanceTimersByTimeAsync(1000); });

		expect(screen.queryByText('EXPIRING-CODE')).toBeNull();
		expect(screen.getByRole('button', { name: 'Get code' })).toBeTruthy();
		expect(props.showToastMessage).toHaveBeenCalledWith('Pairing code expired. Generate a new one.');
	});

	it('does not load Hub hardware data while the IM third-party page is open', async () => {
		renderSettings({ hardware_enabled: true }, { mode: 'im' });

		await screen.findByRole('button', { name: 'Generate Token' });
		expect(appMocks.listHardwareBindings).not.toHaveBeenCalled();
		expect(appMocks.listPetPacks).not.toHaveBeenCalled();
	});

	it('loads pet packs only after per-device pets are enabled', async () => {
		renderSettings({ hardware_enabled: true, hardware_allow_custom_pets: false });

		await screen.findByRole('button', { name: 'Get code' });
		expect(appMocks.listPetPacks).not.toHaveBeenCalled();
	});

	it('refreshes the confirmed pet preference after changing it', async () => {
		appMocks.setHardwareAllowCustomPets.mockResolvedValue(undefined);
		appMocks.loadConfig.mockResolvedValue({ hardware_enabled: true, hardware_allow_custom_pets: true });
		const props = renderSettings({ hardware_enabled: true, hardware_allow_custom_pets: false });

		fireEvent.click(screen.getByRole('checkbox', { name: 'Allow individual pets' }));

		await waitFor(() => expect(appMocks.setHardwareAllowCustomPets).toHaveBeenCalledWith(true));
		await waitFor(() => expect(appMocks.loadConfig).toHaveBeenCalled());
		expect(props.setConfig).toHaveBeenCalledWith(expect.objectContaining({ hardware_allow_custom_pets: true }));
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

    it('sends the latest volume to only the changed device without relying on pointerup', async () => {
        appMocks.sendHardwareDeviceVolume.mockResolvedValue(undefined);
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [{ clientId: 'device-a', clientName: 'Desk' }, { clientId: 'device-b', clientName: 'Kitchen' }], maxDevices: 5, boundCount: 2 });
        renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		const slider = await screen.findByRole('slider', { name: 'Volume Desk' });
        fireEvent.input(slider, { target: { value: '81' } });
        fireEvent.input(slider, { target: { value: '82' } });
		await waitFor(() => expect(appMocks.sendHardwareDeviceVolume).toHaveBeenCalledWith('device-a', 82));
		expect(appMocks.sendHardwareDeviceVolume).toHaveBeenCalledTimes(1);
		expect(screen.getByRole('slider', { name: 'Volume Kitchen' })).toHaveProperty('value', '70');
    });

	it('restores only the changed device volume when its durable update fails', async () => {
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [
			{ clientId: 'device-a', clientName: 'Desk', volume: 31 },
			{ clientId: 'device-b', clientName: 'Kitchen', volume: 74 },
		], maxDevices: 5, boundCount: 2 });
		appMocks.sendHardwareDeviceVolume.mockRejectedValue(new Error('Hub is not connected'));
		renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		const desk = await screen.findByRole('slider', { name: 'Volume Desk' });
		fireEvent.pointerDown(desk);
		fireEvent.input(desk, { target: { value: '82' } });
		await waitFor(() => expect(appMocks.sendHardwareDeviceVolume).toHaveBeenCalledWith('device-a', 82));
		await waitFor(() => expect(screen.getByRole('slider', { name: 'Volume Desk' })).toHaveProperty('value', '31'));
		expect(screen.getByRole('slider', { name: 'Volume Kitchen' })).toHaveProperty('value', '74');
	});

	it('sends the latest brightness to only the changed device', async () => {
		appMocks.sendHardwareDeviceBrightness.mockResolvedValue(undefined);
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [
			{ clientId: 'device-a', clientName: 'Desk', brightness: 43 },
			{ clientId: 'device-b', clientName: 'Kitchen', brightness: 74 },
		], maxDevices: 5, boundCount: 2 });
		renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		const slider = await screen.findByRole('slider', { name: 'Brightness Desk' });
		fireEvent.input(slider, { target: { value: '65' } });
		fireEvent.input(slider, { target: { value: '66' } });

		await waitFor(() => expect(appMocks.sendHardwareDeviceBrightness).toHaveBeenCalledWith('device-a', 66));
		expect(appMocks.sendHardwareDeviceBrightness).toHaveBeenCalledTimes(1);
		expect(screen.getByRole('slider', { name: 'Brightness Kitchen' })).toHaveProperty('value', '74');
	});

	it('defaults each device screen sleep timeout to one minute and saves only the changed device', async () => {
		appMocks.sendHardwareDeviceScreenSleepTimeout.mockResolvedValue(undefined);
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [
			{ clientId: 'device-a', clientName: 'Desk' },
			{ clientId: 'device-b', clientName: 'Kitchen', screenSleepSeconds: 300 },
		], maxDevices: 5, boundCount: 2 });
		renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		const desk = await screen.findByRole('combobox', { name: 'Screen sleep Desk' });
		expect(desk).toHaveProperty('value', '60');
		expect(screen.getByRole('combobox', { name: 'Screen sleep Kitchen' })).toHaveProperty('value', '300');
		fireEvent.change(desk, { target: { value: '1800' } });
		await waitFor(() => expect(appMocks.sendHardwareDeviceScreenSleepTimeout).toHaveBeenCalledWith('device-a', 1800));
	});

	it('sends a later screen-sleep selection made while the previous save is pending', async () => {
		let finishFirstSave!: () => void;
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [
			{ clientId: 'device-a', clientName: 'Desk', screenSleepSeconds: 60 },
		], maxDevices: 5, boundCount: 1 });
		appMocks.sendHardwareDeviceScreenSleepTimeout
			.mockImplementationOnce(() => new Promise<void>((resolve) => { finishFirstSave = resolve; }))
			.mockResolvedValueOnce(undefined);
		renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		const sleep = await screen.findByRole('combobox', { name: 'Screen sleep Desk' });
		fireEvent.change(sleep, { target: { value: '180' } });
		await waitFor(() => expect(appMocks.sendHardwareDeviceScreenSleepTimeout).toHaveBeenCalledWith('device-a', 180));
		fireEvent.change(sleep, { target: { value: '1800' } });
		expect(sleep).toHaveProperty('value', '1800');

		await act(async () => { finishFirstSave(); await Promise.resolve(); });
		await waitFor(() => expect(appMocks.sendHardwareDeviceScreenSleepTimeout).toHaveBeenLastCalledWith('device-a', 1800));
		expect(appMocks.sendHardwareDeviceScreenSleepTimeout).toHaveBeenCalledTimes(2);
	});

	it('keeps an optimistic screen-sleep selection when a refresh returns the older Hub value', async () => {
		let finishSave!: () => void;
		let finishRefresh!: (value: unknown) => void;
		appMocks.listHardwareBindings
			.mockResolvedValueOnce({ devices: [{ clientId: 'device-a', clientName: 'Desk', screenSleepSeconds: 60 }], maxDevices: 5, boundCount: 1 })
			.mockImplementationOnce(() => new Promise((resolve) => { finishRefresh = resolve; }));
		appMocks.sendHardwareDeviceScreenSleepTimeout.mockImplementationOnce(() => new Promise<void>((resolve) => { finishSave = resolve; }));
		renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		const sleep = await screen.findByRole('combobox', { name: 'Screen sleep Desk' });
		fireEvent.change(sleep, { target: { value: '1800' } });
		await waitFor(() => expect(appMocks.sendHardwareDeviceScreenSleepTimeout).toHaveBeenCalledWith('device-a', 1800));
		fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
		finishRefresh({ devices: [{ clientId: 'device-a', clientName: 'Desk', screenSleepSeconds: 60 }], maxDevices: 5, boundCount: 1 });
		await waitFor(() => expect(screen.getByRole('combobox', { name: 'Screen sleep Desk' })).toHaveProperty('value', '1800'));

		await act(async () => { finishSave(); await Promise.resolve(); });
	});

	it('keeps a completed screen-sleep selection when an older refresh finishes late', async () => {
		let finishSave!: () => void;
		let finishRefresh!: (value: unknown) => void;
		appMocks.listHardwareBindings
			.mockResolvedValueOnce({ devices: [{ clientId: 'device-a', clientName: 'Desk', screenSleepSeconds: 60 }], maxDevices: 5, boundCount: 1 })
			.mockImplementationOnce(() => new Promise((resolve) => { finishRefresh = resolve; }));
		appMocks.sendHardwareDeviceScreenSleepTimeout.mockImplementationOnce(() => new Promise<void>((resolve) => { finishSave = resolve; }));
		renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		const sleep = await screen.findByRole('combobox', { name: 'Screen sleep Desk' });
		fireEvent.change(sleep, { target: { value: '1800' } });
		await waitFor(() => expect(appMocks.sendHardwareDeviceScreenSleepTimeout).toHaveBeenCalledWith('device-a', 1800));
		fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
		await act(async () => { finishSave(); await Promise.resolve(); });
		finishRefresh({ devices: [{ clientId: 'device-a', clientName: 'Desk', screenSleepSeconds: 60 }], maxDevices: 5, boundCount: 1 });
		await waitFor(() => expect(screen.getByRole('combobox', { name: 'Screen sleep Desk' })).toHaveProperty('value', '1800'));
	});

	it('keeps the later screen-sleep selection through an older refresh after the first save completes', async () => {
		let finishFirstSave!: () => void;
		let finishRefresh!: (value: unknown) => void;
		appMocks.listHardwareBindings
			.mockResolvedValueOnce({ devices: [{ clientId: 'device-a', clientName: 'Desk', screenSleepSeconds: 60 }], maxDevices: 5, boundCount: 1 })
			.mockImplementationOnce(() => new Promise((resolve) => { finishRefresh = resolve; }));
		appMocks.sendHardwareDeviceScreenSleepTimeout
			.mockImplementationOnce(() => new Promise<void>((resolve) => { finishFirstSave = resolve; }))
			.mockResolvedValueOnce(undefined);
		renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		const sleep = await screen.findByRole('combobox', { name: 'Screen sleep Desk' });
		fireEvent.change(sleep, { target: { value: '180' } });
		await waitFor(() => expect(appMocks.sendHardwareDeviceScreenSleepTimeout).toHaveBeenCalledWith('device-a', 180));
		fireEvent.change(sleep, { target: { value: '1800' } });
		fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
		await act(async () => { finishFirstSave(); await Promise.resolve(); });
		await waitFor(() => expect(appMocks.sendHardwareDeviceScreenSleepTimeout).toHaveBeenLastCalledWith('device-a', 1800));
		finishRefresh({ devices: [{ clientId: 'device-a', clientName: 'Desk', screenSleepSeconds: 180 }], maxDevices: 5, boundCount: 1 });
		await waitFor(() => expect(screen.getByRole('combobox', { name: 'Screen sleep Desk' })).toHaveProperty('value', '1800'));
	});

	it('restores only the changed device screen sleep timeout after a failed save', async () => {
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [
			{ clientId: 'device-a', clientName: 'Desk', screenSleepSeconds: 300 },
			{ clientId: 'device-b', clientName: 'Kitchen', screenSleepSeconds: 600 },
		], maxDevices: 5, boundCount: 2 });
		appMocks.sendHardwareDeviceScreenSleepTimeout.mockRejectedValue(new Error('Hub is not connected'));
		const props = renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		const desk = await screen.findByRole('combobox', { name: 'Screen sleep Desk' });
		fireEvent.change(desk, { target: { value: '1800' } });
		await waitFor(() => expect(appMocks.sendHardwareDeviceScreenSleepTimeout).toHaveBeenCalledWith('device-a', 1800));
		await waitFor(() => expect(screen.getByRole('combobox', { name: 'Screen sleep Desk' })).toHaveProperty('value', '300'));
		expect(screen.getByRole('combobox', { name: 'Screen sleep Kitchen' })).toHaveProperty('value', '600');
		expect(props.showToastMessage).toHaveBeenCalledWith('Hub is not connected');
	});

	it('does not roll back or report a failed screen-sleep save after that device is unbound', async () => {
		let failSave!: (error: Error) => void;
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [
			{ clientId: 'device-a', clientName: 'Desk', screenSleepSeconds: 300 },
		], maxDevices: 5, boundCount: 1 });
		appMocks.deleteHardware.mockResolvedValue(undefined);
		appMocks.sendHardwareDeviceScreenSleepTimeout.mockImplementationOnce(() => new Promise<void>((_, reject) => { failSave = reject; }));
		const props = renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		fireEvent.change(await screen.findByRole('combobox', { name: 'Screen sleep Desk' }), { target: { value: '1800' } });
		await waitFor(() => expect(appMocks.sendHardwareDeviceScreenSleepTimeout).toHaveBeenCalledWith('device-a', 1800));
		fireEvent.click(screen.getByRole('button', { name: 'Remove Desk' }));
		fireEvent.click(await screen.findByRole('button', { name: 'Remove' }));
		await waitFor(() => expect(appMocks.deleteHardware).toHaveBeenCalledWith('device-a'));

		await act(async () => { failSave(new Error('Hub is not connected')); await Promise.resolve(); });
		expect(props.showToastMessage).not.toHaveBeenCalledWith('Hub is not connected');
		expect(screen.queryByRole('combobox', { name: 'Screen sleep Desk' })).toBeNull();
	});

	it('restores only the changed device brightness when its durable update fails', async () => {
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [
			{ clientId: 'device-a', clientName: 'Desk', brightness: 31 },
			{ clientId: 'device-b', clientName: 'Kitchen', brightness: 74 },
		], maxDevices: 5, boundCount: 2 });
		appMocks.sendHardwareDeviceBrightness.mockRejectedValue(new Error('Hub is not connected'));
		renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		const desk = await screen.findByRole('slider', { name: 'Brightness Desk' });
		fireEvent.pointerDown(desk);
		fireEvent.input(desk, { target: { value: '82' } });

		await waitFor(() => expect(appMocks.sendHardwareDeviceBrightness).toHaveBeenCalledWith('device-a', 82));
		await waitFor(() => expect(screen.getByRole('slider', { name: 'Brightness Desk' })).toHaveProperty('value', '31'));
		expect(screen.getByRole('slider', { name: 'Brightness Kitchen' })).toHaveProperty('value', '74');
	});

	it('uses the system pet by default and exposes isolated pet selectors only when enabled', async () => {
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [{ clientId: 'device-a', clientName: 'Desk', petSkin: 'clawmate' }], maxDevices: 5, boundCount: 1 });
		appMocks.listPetPacks.mockResolvedValue([{ id: 'clawmate', name: 'ClawMate' }, { id: 'focus-claw', name: 'Focus Claw' }]);
		const props = renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true, pet_skin: 'clawmate' });

		expect(await screen.findByText('Desk')).toBeTruthy();
		expect(screen.queryByRole('combobox', { name: 'Pet Desk' })).toBeNull();
		fireEvent.click(screen.getByRole('checkbox', { name: 'Allow individual pets' }));
		await waitFor(() => expect(appMocks.setHardwareAllowCustomPets).toHaveBeenCalledWith(true));

		props.rerenderSettings({ config: { ...props.config, hardware_allow_custom_pets: true } });
		const pet = await screen.findByRole('combobox', { name: 'Pet Desk' });
		fireEvent.click(pet);
		fireEvent.click(await screen.findByTestId('hardware-pet-option-device-a-focus-claw'));
		await waitFor(() => expect(appMocks.sendHardwareDevicePetProfile).toHaveBeenCalledWith('device-a', 'focus-claw'));
	});

	it('keeps custom-pet controls disabled while the preference update is pending and restores them after failure', async () => {
		let rejectUpdate!: (error: Error) => void;
		appMocks.setHardwareAllowCustomPets.mockImplementationOnce(() => new Promise<void>((_, reject) => { rejectUpdate = reject; }));
		const props = renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		const toggle = screen.getByRole('checkbox', { name: 'Allow individual pets' });
		fireEvent.click(toggle);
		await waitFor(() => expect(appMocks.setHardwareAllowCustomPets).toHaveBeenCalledWith(true));
		expect(toggle).toHaveProperty('disabled', true);
		expect(screen.getByLabelText('Hardware configuration').getAttribute('aria-busy')).toBe('true');

		await act(async () => rejectUpdate(new Error('Hub is not connected')));
		await waitFor(() => expect(props.showToastMessage).toHaveBeenCalledWith('Hub is not connected'));
		expect(toggle).toHaveProperty('disabled', false);
		expect(toggle).toHaveProperty('checked', false);
		expect(screen.queryByRole('combobox', { name: 'Pet Desk' })).toBeNull();
		expect(screen.getByLabelText('Hardware configuration').getAttribute('aria-busy')).toBe('false');
	});

	it('uses the installed pack preview and does not block another device while a pet update is in flight', async () => {
		let resolveDesk!: () => void;
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [
			{ clientId: 'device-a', clientName: 'Desk', petSkin: 'clawmate' },
			{ clientId: 'device-b', clientName: 'Kitchen', petSkin: 'focus-claw' },
		], maxDevices: 5, boundCount: 2 });
		appMocks.listPetPacks.mockResolvedValue([{ id: 'clawmate', name: 'ClawMate' }, { id: 'focus-claw', name: 'Focus Claw' }]);
		appMocks.getPetPackPreviewDataURL.mockImplementation((id: string) => Promise.resolve(id === 'focus-claw' ? 'data:image/png;base64,preview' : ''));
		appMocks.sendHardwareDevicePetProfile.mockImplementationOnce(() => new Promise<void>((resolve) => { resolveDesk = resolve; })).mockResolvedValue(undefined);
		renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true, hardware_allow_custom_pets: true });

		const desk = await screen.findByRole('combobox', { name: 'Pet Desk' });
		const kitchen = screen.getByRole('combobox', { name: 'Pet Kitchen' });
		await waitFor(() => expect(appMocks.getPetPackPreviewDataURL).toHaveBeenCalledWith('focus-claw'));
		fireEvent.click(desk);
		const focusOption = await screen.findByTestId('hardware-pet-option-device-a-focus-claw');
		expect((focusOption.querySelector('img') as HTMLImageElement).src).toBe('data:image/png;base64,preview');
		fireEvent.click(focusOption);
		expect((desk as HTMLButtonElement).disabled).toBe(true);
		expect((kitchen as HTMLButtonElement).disabled).toBe(false);
		fireEvent.click(kitchen);
		fireEvent.click(await screen.findByTestId('hardware-pet-option-device-b-clawmate'));
		await waitFor(() => expect(appMocks.sendHardwareDevicePetProfile).toHaveBeenCalledWith('device-b', 'clawmate'));
		resolveDesk();
	});

	it('does not restore or report a stale pet update after that device is removed', async () => {
		let rejectPet!: (error: Error) => void;
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [{
			clientId: 'device-a', clientName: 'Desk', petSkin: 'clawmate', online: true,
		}], maxDevices: 5, boundCount: 1 });
		appMocks.listPetPacks.mockResolvedValue([{ id: 'clawmate', name: 'ClawMate' }, { id: 'focus-claw', name: 'Focus Claw' }]);
		appMocks.sendHardwareDevicePetProfile.mockImplementationOnce(() => new Promise<void>((_, reject) => { rejectPet = reject; }));
		appMocks.deleteHardware.mockResolvedValue(undefined);
		renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true, hardware_allow_custom_pets: true });

		const pet = await screen.findByRole('combobox', { name: 'Pet Desk' });
		fireEvent.click(pet);
		fireEvent.click(await screen.findByTestId('hardware-pet-option-device-a-focus-claw'));
		await waitFor(() => expect(appMocks.sendHardwareDevicePetProfile).toHaveBeenCalledWith('device-a', 'focus-claw'));
		fireEvent.click(screen.getByRole('button', { name: 'Remove Desk' }));
		fireEvent.click(await screen.findByRole('button', { name: 'Remove' }));
		await waitFor(() => expect(appMocks.deleteHardware).toHaveBeenCalledWith('device-a'));
		rejectPet(new Error('stale pet failure'));
		await waitFor(() => expect(screen.queryByText('Desk')).toBeNull());
	});

	it('does not report a stale successful remote playback after that device is removed', async () => {
		let finishPlayback!: () => void;
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [{
			clientId: 'device-a', clientName: 'Desk', online: true,
		}], maxDevices: 5, boundCount: 1 });
		appMocks.sendHardwareWelcomeAudioRemote.mockImplementationOnce(() => new Promise<void>((resolve) => { finishPlayback = resolve; }));
		appMocks.deleteHardware.mockResolvedValue(undefined);
		const props = renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true, hardware_welcome_audio_path: 'C:/welcome.wav' });

		fireEvent.click(await screen.findByRole('button', { name: 'Play remotely Desk' }));
		fireEvent.click(screen.getByRole('button', { name: 'Remove Desk' }));
		fireEvent.click(await screen.findByRole('button', { name: 'Remove' }));
		await waitFor(() => expect(appMocks.deleteHardware).toHaveBeenCalledWith('device-a'));
		finishPlayback();
		await waitFor(() => expect(screen.queryByText('Desk')).toBeNull());
		expect(props.showToastMessage).not.toHaveBeenCalledWith('Desk confirmed playback.');
	});

    it('shows the fixed five-device binding limit without a limit-setting control', async () => {
        appMocks.listHardwareBindings.mockResolvedValue({
            devices: [{ clientId: 'device-a', clientName: 'Desk' }, { clientId: 'device-b', clientName: 'Kitchen' }],
            maxDevices: 5,
            boundCount: 2,
        });
        renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });


        const limit = await screen.findByText('2 / 5');
        expect(limit.parentElement?.textContent).toMatch(/up to 2 \/ 5 devices/i);
        expect(screen.queryByText(/remove a device before binding a new one/i)).toBeNull();
        expect(screen.queryByRole('combobox', { name: 'Independent hardware limit' })).toBeNull();
    });

	it('keeps existing bindings visible when an older desktop returns Go-style field names', async () => {
		appMocks.listHardwareBindings.mockResolvedValue({
			Devices: [{ clientId: 'esp32s3-legacy', clientName: 'Legacy ESP32' }],
			MaxDevices: 5,
			BoundCount: 3,
		});
		renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		expect(await screen.findByText('Legacy ESP32')).toBeTruthy();
		expect(screen.getByText('3 / 5')).toBeTruthy();
	});

    it('shows the unbind instruction when all five hardware bindings are occupied', async () => {
        appMocks.listHardwareBindings.mockResolvedValue({ devices: [
            { clientId: 'device-a', clientName: 'Desk' }, { clientId: 'device-b', clientName: 'Kitchen' },
            { clientId: 'device-c', clientName: 'Hall' }, { clientId: 'device-d', clientName: 'Study' },
            { clientId: 'device-e', clientName: 'Bedroom' },
        ], maxDevices: 5, boundCount: 5 });
        renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

        expect(await screen.findByText('5 / 5')).toBeTruthy();
        expect(screen.getByText(/remove a device before binding a new one/i)).toBeTruthy();
    });

    it('flushes a release value that arrives while an earlier volume write is still running', async () => {
        let finishFirstWrite!: () => void;
        appMocks.sendHardwareDeviceVolume
            .mockImplementationOnce(() => new Promise<void>((resolve) => { finishFirstWrite = resolve; }))
            .mockResolvedValue(undefined);
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [{ clientId: 'device-a', clientName: 'Desk' }], maxDevices: 5, boundCount: 1 });
        renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });
		const slider = await screen.findByRole('slider', { name: 'Volume Desk' });

        fireEvent.pointerUp(slider, { target: { value: '40' } });
		await waitFor(() => expect(appMocks.sendHardwareDeviceVolume).toHaveBeenCalledWith('device-a', 40));
        fireEvent.pointerUp(slider, { target: { value: '73' } });
        finishFirstWrite();

		await waitFor(() => expect(appMocks.sendHardwareDeviceVolume).toHaveBeenLastCalledWith('device-a', 73));
		expect(appMocks.sendHardwareDeviceVolume).toHaveBeenCalledTimes(2);
    });

    it('enables hardware through the atomic backend operation without changing IM gateway mode', async () => {
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
        expect(props.setThirdPartyGatewayLocalModeState).not.toHaveBeenCalled();
        expect(props.showToastMessage).toHaveBeenCalledWith(expect.stringContaining('hardware connections through Hub'));
    });

    it('keeps IM gateway token and mode controls independent while hardware is enabled', () => {
        renderSettings({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
        }, { mode: 'im' });

        expect((screen.getByRole('checkbox', { name: 'Enable third-party access' }) as HTMLInputElement).disabled).toBe(false);
        expect((screen.getByRole('button', { name: 'Handle with local Agent' }) as HTMLButtonElement).disabled).toBe(false);
        expect((screen.getByPlaceholderText('Bearer token') as HTMLInputElement).disabled).toBe(false);
        expect((screen.getByRole('button', { name: 'Generate Token' }) as HTMLButtonElement).disabled).toBe(false);
        expect((screen.getByDisplayValue('127.0.0.1') as HTMLInputElement).disabled).toBe(false);
        expect((screen.getByDisplayValue('18777') as HTMLInputElement).disabled).toBe(false);
    });

    it('keeps hardware controls disabled until hardware is enabled', () => {
        renderSettings({ thirdparty_gateway_enabled: true, hardware_enabled: false });
        expect((screen.getByRole('button', { name: 'Get code' }) as HTMLButtonElement).disabled).toBe(true);
		expect(screen.queryByRole('slider', { name: /Volume/ })).toBeNull();
        expect(screen.queryByRole('button', { name: 'Test remote hardware' })).toBeNull();
    });

    it('allows Welcome setup and local generation before hardware is enabled', () => {
        renderSettings({ thirdparty_gateway_enabled: false, hardware_enabled: false });

        expect((screen.getByRole('checkbox', { name: 'Enabled' }) as HTMLInputElement).disabled).toBe(false);
        expect((screen.getByRole('combobox', { name: 'Voice model' }) as HTMLSelectElement).disabled).toBe(false);
        expect((screen.getByPlaceholderText('For example: Hello, Maclaw') as HTMLTextAreaElement).disabled).toBe(false);
        expect((screen.getByRole('button', { name: 'Generate audio' }) as HTMLButtonElement).disabled).toBe(false);
        expect((screen.getByRole('button', { name: 'Choose audio' }) as HTMLButtonElement).disabled).toBe(false);
    });


        it('restores confirmed config and unlocks the gateway toggle after a save failure', async () => {
            const props = renderSettings({}, { mode: 'im' });
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
            const props = renderSettings({ thirdparty_gateway_enabled: true }, { mode: 'im' });
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
        expect(appMocks.getHardwareWelcomeAudioDataURL).not.toHaveBeenCalled();
        appMocks.getHardwareWelcomeAudioDataURL.mockResolvedValue('data:audio/wav;base64,DEFAULT');

        fireEvent.click(screen.getByRole('button', { name: 'Reset' }));

        await waitFor(() => expect(appMocks.resetHardwareWelcomeAudio).toHaveBeenCalledTimes(1));
        await waitFor(() => expect(appMocks.getHardwareWelcomeAudioDataURL).toHaveBeenCalledTimes(1));
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

        expect((screen.getByRole('combobox', { name: 'Voice model' }) as HTMLSelectElement).value).toBe('af_heart');
        fireEvent.click(screen.getByRole('button', { name: 'Generate audio' }));

        await waitFor(() => expect(appMocks.generateHardwareWelcomeAudio).toHaveBeenCalledWith('Hello, Maclaw', 'af_heart'));
    });

    it('explains how to recover when generated welcome audio exceeds device capacity', async () => {
        appMocks.generateHardwareWelcomeAudio.mockRejectedValue(new Error('welcome audio is too long after conversion (3.6 seconds; maximum 3.1 seconds); shorten the welcome text'));
        const props = renderSettings({ hardware_enabled: true, hardware_welcome_text: 'A greeting that exceeds the device capacity' });

        fireEvent.click(screen.getByRole('button', { name: 'Generate audio' }));

        await waitFor(() => expect(props.showToastMessage).toHaveBeenCalledWith('The welcome audio exceeds the hardware’s roughly 3-second capacity. It was automatically sped up; shorten the message and try again.'));
    });

    it('shows a live capacity warning before a longer welcome message is generated', () => {
        const longGreeting = 'A welcome message intentionally long enough to cross the early capacity warning threshold.';
        renderSettings({ hardware_enabled: true, hardware_welcome_text: longGreeting });

        expect(screen.getByText(`${Array.from(longGreeting).length}/80 characters — a longer greeting may exceed the hardware’s roughly 3-second capacity.`)).toBeTruthy();
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

        fireEvent.change(screen.getByRole('combobox', { name: 'Voice model' }), { target: { value: 'am_adam' } });
        expect(props.saveRemoteConfigField).toHaveBeenCalledWith({ hardware_welcome_voice_id: 'am_adam' });
        expect((screen.getByRole('button', { name: 'Generate audio' }) as HTMLButtonElement).disabled).toBe(true);
        finishVoiceSave();
        await waitFor(() => expect((screen.getByRole('button', { name: 'Generate audio' }) as HTMLButtonElement).disabled).toBe(false));
        fireEvent.click(screen.getByRole('button', { name: 'Generate audio' }));

        await waitFor(() => expect(appMocks.generateHardwareWelcomeAudio).toHaveBeenCalledWith('Hello, Maclaw', 'am_adam'));
    });

    it('allows a Chinese voice model for a Chinese Welcome message', async () => {
        appMocks.generateHardwareWelcomeAudio.mockResolvedValue('C:/hardware/welcome.wav');
        const props = renderSettings({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
            hardware_welcome_text: '大哥，你轻点？',
            hardware_welcome_voice_id: 'af_heart',
        });

        fireEvent.change(screen.getByRole('combobox', { name: 'Voice model' }), { target: { value: 'zf_xiaoyi' } });

        await waitFor(() => expect(props.saveRemoteConfigField).toHaveBeenCalledWith({ hardware_welcome_voice_id: 'zf_xiaoyi' }));
        fireEvent.click(screen.getByRole('button', { name: 'Generate audio' }));
        await waitFor(() => expect(appMocks.generateHardwareWelcomeAudio).toHaveBeenCalledWith('大哥，你轻点？', 'zf_xiaoyi'));
    });

    it('lists every bundled voice model for Welcome generation', async () => {
        renderSettings({ hardware_enabled: true });

        const options = Array.from(screen.getByRole('combobox', { name: 'Voice model' }).querySelectorAll('option'));
        expect(options.map((option) => option.value)).toEqual([
            'zf_xiaoyi', 'zf_xiaoxiao', 'zm_yunxi', 'zm_yunyang', 'am_adam', 'af_heart',
        ]);
    });

    it('keeps Chinese Welcome voice labels in Traditional Chinese mode', () => {
        renderSettings({ hardware_enabled: true }, { lang: 'zh-Hant' });

        const options = Array.from(screen.getByRole('combobox', { name: '發音模型' }).querySelectorAll('option'));
        expect(options.map((option) => option.textContent)).toEqual([
            '小藝（中文女聲）', '曉曉（中文女聲）', '雲希（中文男聲）', '雲揚（中文男聲）', '自然男聲（美式英語）', '甜美女聲（美式英語）',
        ]);
    });

    it('restores the confirmed voice when saving a voice selection fails', async () => {
        const props = renderSettings({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
            hardware_welcome_voice_id: 'af_heart',
        });
        props.saveRemoteConfigField.mockRejectedValue(new Error('voice save failed'));

        fireEvent.change(screen.getByRole('combobox', { name: 'Voice model' }), { target: { value: 'am_adam' } });

        await waitFor(() => expect((screen.getByRole('combobox', { name: 'Voice model' }) as HTMLSelectElement).value).toBe('af_heart'));
    });

    it('loads the welcome WAV only when the user starts a GUI preview', async () => {
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
        expect(appMocks.getHardwareWelcomeAudioDataURL).not.toHaveBeenCalled();

        fireEvent.click(screen.getByRole('button', { name: 'Preview in GUI' }));

        await waitFor(() => expect(play).toHaveBeenCalledTimes(1));
        expect(appMocks.getHardwareWelcomeAudioDataURL).toHaveBeenCalledTimes(1);
    });

	it('does not reuse an old welcome WAV after the configured audio changes mid-request', async () => {
		let resolveOldSource!: (source: string) => void;
		appMocks.getHardwareWelcomeAudioDataURL
			.mockImplementationOnce(() => new Promise<string>((resolve) => { resolveOldSource = resolve; }))
			.mockResolvedValueOnce('data:audio/wav;base64,NEW');
		const playedSources: string[] = [];
		vi.stubGlobal('Audio', class {
			onended: ((event: Event) => void) | null = null;
			onerror: ((event: Event) => void) | null = null;
			constructor(source: string) { playedSources.push(source); }
			pause = vi.fn();
			play = vi.fn().mockImplementation(() => {
				queueMicrotask(() => this.onended?.(new Event('ended')));
				return Promise.resolve();
			});
		});
		const props = renderSettings({ hardware_welcome_audio_path: 'C:/old.wav' });

		fireEvent.click(screen.getByRole('button', { name: 'Preview in GUI' }));
		await waitFor(() => expect(appMocks.getHardwareWelcomeAudioDataURL).toHaveBeenCalledTimes(1));
		props.rerenderSettings({ config: { ...props.config, hardware_welcome_audio_path: 'C:/new.wav' } });
		resolveOldSource('data:audio/wav;base64,OLD');
		await waitFor(() => expect(playedSources).toEqual(['data:audio/wav;base64,OLD']));

		fireEvent.click(screen.getByRole('button', { name: 'Preview in GUI' }));
		await waitFor(() => expect(appMocks.getHardwareWelcomeAudioDataURL).toHaveBeenCalledTimes(2));
		await waitFor(() => expect(playedSources).toEqual(['data:audio/wav;base64,OLD', 'data:audio/wav;base64,NEW']));
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

	    it('sends remote playback only to the selected bound hardware', async () => {
	        appMocks.sendHardwareWelcomeAudioRemote.mockResolvedValue(undefined);
	        appMocks.listHardwareBindings.mockResolvedValue({ devices: [{ clientId: 'esp32s3-a1', clientName: 'Desk Pet', online: true }], maxDevices: 5, boundCount: 1 });
	        const props = renderSettings({
	            thirdparty_gateway_enabled: true,
	            thirdparty_gateway_local_mode: false,
	            hardware_enabled: true,
	            hardware_welcome_audio_path: 'C:/welcome.wav',
	        });

	        fireEvent.click(await screen.findByRole('button', { name: 'Play remotely Desk Pet' }));

	        await waitFor(() => expect(appMocks.sendHardwareWelcomeAudioRemote).toHaveBeenCalledWith('esp32s3-a1'));
		// The remote button must never fetch or play a desktop audio preview.
        expect(appMocks.getHardwareWelcomeAudioDataURL).not.toHaveBeenCalled();
	        expect(props.showToastMessage).toHaveBeenCalledWith('Desk Pet confirmed playback.');
    });

	it('keeps other hardware controls available while one device is playing remotely', async () => {
		let finishDeskPlayback!: () => void;
		appMocks.sendHardwareWelcomeAudioRemote
			.mockImplementationOnce(() => new Promise<void>((resolve) => { finishDeskPlayback = resolve; }))
			.mockResolvedValueOnce(undefined);
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [
			{ clientId: 'esp32s3-desk', clientName: 'Desk Pet', online: true },
			{ clientId: 'esp32s3-kitchen', clientName: 'Kitchen Pet', online: true },
		], maxDevices: 5, boundCount: 2 });
		renderSettings({
			thirdparty_gateway_enabled: true,
			thirdparty_gateway_local_mode: false,
			hardware_enabled: true,
			hardware_welcome_audio_path: 'C:/welcome.wav',
		});

		const deskPlayback = await screen.findByRole('button', { name: 'Play remotely Desk Pet' }) as HTMLButtonElement;
		const kitchenPlayback = screen.getByRole('button', { name: 'Play remotely Kitchen Pet' }) as HTMLButtonElement;
		const kitchenRemoval = screen.getByRole('button', { name: 'Remove Kitchen Pet' }) as HTMLButtonElement;
		fireEvent.click(deskPlayback);

		await waitFor(() => expect(appMocks.sendHardwareWelcomeAudioRemote).toHaveBeenCalledWith('esp32s3-desk'));
		expect(deskPlayback.disabled).toBe(true);
		expect(kitchenPlayback.disabled).toBe(false);
		expect(kitchenRemoval.disabled).toBe(false);

		fireEvent.click(kitchenPlayback);
		await waitFor(() => expect(appMocks.sendHardwareWelcomeAudioRemote).toHaveBeenCalledWith('esp32s3-kitchen'));
		finishDeskPlayback();
	});

    it('shows an actionable Chinese error when no compatible remote ESP32 is online', async () => {
        appMocks.sendHardwareWelcomeAudioRemote.mockRejectedValue(new Error('Hub rejected request (NO_COMPATIBLE_HARDWARE): no online remote ESP32 supports welcome audio playback'));
        appMocks.listHardwareBindings.mockResolvedValue({ devices: [{ clientId: 'esp32s3-a1', clientName: 'Desk Pet', online: true }], maxDevices: 5, boundCount: 1 });
        const props = renderSettings({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
            hardware_welcome_audio_path: 'C:/welcome.wav',
        }, { lang: 'zh-Hans' });

        fireEvent.click(await screen.findByRole('button', { name: '远程播放 Desk Pet' }));

        await waitFor(() => expect(appMocks.sendHardwareWelcomeAudioRemote).toHaveBeenCalledWith('esp32s3-a1'));
        await waitFor(() => expect(props.showToastMessage).toHaveBeenCalledWith(expect.stringContaining('没有在线且支持欢迎音频播放')));
        expect(props.showToastMessage).toHaveBeenCalledWith(expect.stringContaining('检查设备联网、配对状态和固件能力'));
    });

    it('explains that an apparently online device is currently offline instead of suggesting re-pairing', async () => {
        appMocks.sendHardwareWelcomeAudioRemote.mockRejectedValue(new Error('Hub rejected request (HARDWARE_OFFLINE): selected hardware is offline'));
        appMocks.listHardwareBindings.mockResolvedValue({ devices: [{ clientId: 'esp32s3-a1', clientName: 'Desk Pet', online: true }], maxDevices: 5, boundCount: 1 });
        const props = renderSettings({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
            hardware_welcome_audio_path: 'C:/welcome.wav',
        }, { lang: 'zh-Hans' });

        fireEvent.click(await screen.findByRole('button', { name: '远程播放 Desk Pet' }));

        await waitFor(() => expect(props.showToastMessage).toHaveBeenCalledWith('该硬件当前离线或刚断开连接。请等待设备重连后再试。'));
    });

    it('explains a Hub binding-identity mismatch without telling the user to re-pair blindly', async () => {
        appMocks.sendHardwareWelcomeAudioRemote.mockRejectedValue(new Error('Hub rejected request (HARDWARE_NOT_OWNED): hardware client is not bound to this machine or cannot accept the reply'));
        appMocks.listHardwareBindings.mockResolvedValue({ devices: [{ clientId: 'esp32s3-a1', clientName: 'Desk Pet', online: true }], maxDevices: 5, boundCount: 1 });
        const props = renderSettings({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
            hardware_welcome_audio_path: 'C:/welcome.wav',
        }, { lang: 'zh-Hans' });

        fireEvent.click(await screen.findByRole('button', { name: '远程播放 Desk Pet' }));

        await waitFor(() => expect(props.showToastMessage).toHaveBeenCalledWith('当前设备的绑定身份与 Hub 不一致。请刷新设备列表并等待设备重连；若问题持续，请检查它是否连接到了正确的 Hub。'));
    });

    it('explains temporary Hub routing unavailability without exposing the protocol error', async () => {
        appMocks.sendHardwareWelcomeAudioRemote.mockRejectedValue(new Error('Hub rejected request (HARDWARE_UNAVAILABLE): hardware cannot accept the reply right now'));
        appMocks.listHardwareBindings.mockResolvedValue({ devices: [{ clientId: 'esp32s3-a1', clientName: 'Desk Pet', online: true }], maxDevices: 5, boundCount: 1 });
        const props = renderSettings({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
            hardware_welcome_audio_path: 'C:/welcome.wav',
        }, { lang: 'zh-Hans' });

        fireEvent.click(await screen.findByRole('button', { name: '远程播放 Desk Pet' }));

        await waitFor(() => expect(props.showToastMessage).toHaveBeenCalledWith('Hub 暂时无法向该硬件下发命令。请稍后重试；若持续出现，请检查 Hub 与设备连接。'));
    });

	it('lists independent clients and removes one binding', async () => {
        appMocks.listHardwareBindings.mockResolvedValue({ devices: [
            { clientId: 'esp32s3-a1', clientName: 'Desk Pet', protocolVersion: '1.1', online: true },
            { clientId: 'esp32s3-b2', clientName: 'Kitchen Pet', protocolVersion: '1.1', online: false },
        ], maxDevices: 5, boundCount: 2 });
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

	it('explains a failed removal caused by a Hub binding-identity mismatch', async () => {
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [{ clientId: 'esp32s3-a1', clientName: 'Desk Pet', online: true }], maxDevices: 5, boundCount: 1 });
		appMocks.deleteHardware.mockRejectedValue(new Error('Hub rejected request (HARDWARE_NOT_OWNED): hardware client is not bound to this machine'));
		const props = renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		fireEvent.click(await screen.findByRole('button', { name: 'Remove Desk Pet' }));
		fireEvent.click(await screen.findByRole('button', { name: 'Remove' }));

		await waitFor(() => expect(props.showToastMessage).toHaveBeenCalledWith('This device’s binding identity no longer matches the Hub. Refresh the device list and wait for it to reconnect; if this persists, check that it is connected to the correct Hub.'));
		expect(screen.getByText('Desk Pet')).toBeTruthy();
	});

	it('does not let an older refresh restore a binding already being removed', async () => {
		let resolveRefresh!: (value: unknown) => void;
		appMocks.listHardwareBindings
			.mockResolvedValueOnce({ devices: [{ clientId: 'esp32s3-a1', clientName: 'Desk Pet', online: true }], maxDevices: 5, boundCount: 1 })
			.mockImplementationOnce(() => new Promise((resolve) => { resolveRefresh = resolve; }));
		appMocks.deleteHardware.mockResolvedValue(undefined);
		renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		await screen.findByText('Desk Pet');
		fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
		fireEvent.click(screen.getByRole('button', { name: 'Remove Desk Pet' }));
		fireEvent.click(await screen.findByRole('button', { name: 'Remove' }));
		await waitFor(() => expect(appMocks.deleteHardware).toHaveBeenCalledWith('esp32s3-a1'));
		resolveRefresh({ devices: [{ clientId: 'esp32s3-a1', clientName: 'Desk Pet', online: true }], maxDevices: 5, boundCount: 1 });
		await waitFor(() => expect(screen.queryByText('Desk Pet')).toBeNull());
	});

	it('shows a same-ID device again when it was re-paired right after removal', async () => {
		const oldPairing = new Date(Date.now() - 3600000).toISOString();
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [
			{ clientId: 'esp32s3-a1', clientName: 'Desk Pet', online: true, pairedAt: oldPairing },
		], maxDevices: 5, boundCount: 1 });
		appMocks.deleteHardware.mockResolvedValue(undefined);
		renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		await screen.findByText('Desk Pet');
		// The re-pairing lands before any post-delete refresh can observe the
		// binding as absent: the very next list already contains the new binding.
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [
			{ clientId: 'esp32s3-a1', clientName: 'Desk Pet', online: true, pairedAt: new Date().toISOString() },
		], maxDevices: 5, boundCount: 1 });
		fireEvent.click(screen.getByRole('button', { name: 'Remove Desk Pet' }));
		fireEvent.click(await screen.findByRole('button', { name: 'Remove' }));

		await waitFor(() => expect(appMocks.deleteHardware).toHaveBeenCalledWith('esp32s3-a1'));
		// The post-delete refresh must treat the newer pairedAt as a fresh pairing
		// and show the device again instead of hiding it as a stale entry.
		await screen.findByText('Desk Pet');
	});

	it('renames a hardware binding locally and rejects a duplicate device name', async () => {
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [
			{ clientId: 'esp32s3-a1', clientName: 'Desk Pet', online: true },
			{ clientId: 'esp32s3-b2', clientName: 'Kitchen Pet', online: true },
		], maxDevices: 5, boundCount: 2 });
		const props = renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		fireEvent.click(await screen.findByRole('button', { name: 'Rename Desk Pet' }));
		const input = screen.getByRole('textbox', { name: 'Hardware name esp32s3-a1' });
		fireEvent.change(input, { target: { value: 'Studio Pet' } });
		fireEvent.click(screen.getByRole('button', { name: 'Save name Desk Pet' }));
		await waitFor(() => expect(appMocks.setThirdPartyHardwareDeviceAlias).toHaveBeenCalledWith('esp32s3-a1', 'Studio Pet'));
		expect(await screen.findByText('Studio Pet')).toBeTruthy();

		fireEvent.click(screen.getByRole('button', { name: 'Rename Kitchen Pet' }));
		const duplicate = screen.getByRole('textbox', { name: 'Hardware name esp32s3-b2' });
		fireEvent.change(duplicate, { target: { value: 'Studio Pet' } });
		fireEvent.click(screen.getByRole('button', { name: 'Save name Kitchen Pet' }));
		expect(appMocks.setThirdPartyHardwareDeviceAlias).toHaveBeenCalledTimes(1);
		expect(props.showToastMessage).toHaveBeenCalledWith('Hardware names must be unique.');
	});

	it('disables removal only for the device whose local name is being saved', async () => {
		let finishRename!: () => void;
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [
			{ clientId: 'esp32s3-a1', clientName: 'Desk Pet', online: true },
			{ clientId: 'esp32s3-b2', clientName: 'Kitchen Pet', online: true },
		], maxDevices: 5, boundCount: 2 });
		appMocks.setThirdPartyHardwareDeviceAlias.mockImplementationOnce(() => new Promise<void>((resolve) => { finishRename = resolve; }));
		renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		fireEvent.click(await screen.findByRole('button', { name: 'Rename Desk Pet' }));
		fireEvent.change(screen.getByRole('textbox', { name: 'Hardware name esp32s3-a1' }), { target: { value: 'Studio Pet' } });
		fireEvent.click(screen.getByRole('button', { name: 'Save name Desk Pet' }));
		await waitFor(() => expect(appMocks.setThirdPartyHardwareDeviceAlias).toHaveBeenCalledWith('esp32s3-a1', 'Studio Pet'));

		expect(screen.getByRole('button', { name: 'Remove Desk Pet' })).toHaveProperty('disabled', true);
		expect(screen.getByRole('button', { name: 'Remove Kitchen Pet' })).toHaveProperty('disabled', false);
		finishRename();
	});

    it('cancels a pending volume write when that device is unbound', async () => {
        appMocks.listHardwareBindings.mockResolvedValue({ devices: [
            { clientId: 'esp32s3-volume-remove', clientName: 'Volume Pet', online: true, volume: 42 },
        ], maxDevices: 5, boundCount: 1 });
        appMocks.deleteHardware.mockResolvedValue(undefined);
        appMocks.sendHardwareDeviceVolume.mockResolvedValue(undefined);
        renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

        const slider = await screen.findByRole('slider', { name: 'Volume Volume Pet' });
        fireEvent.input(slider, { target: { value: '87' } });
        fireEvent.click(screen.getByRole('button', { name: 'Remove Volume Pet' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Remove' }));

        await waitFor(() => expect(appMocks.deleteHardware).toHaveBeenCalledWith('esp32s3-volume-remove'));
        await new Promise((resolve) => setTimeout(resolve, 150));
        expect(appMocks.sendHardwareDeviceVolume).not.toHaveBeenCalled();
    });

	 it('cancels a pending brightness write when that device is unbound', async () => {
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [
			{ clientId: 'esp32s3-brightness-remove', clientName: 'Brightness Pet', online: true, brightness: 42 },
		], maxDevices: 5, boundCount: 1 });
		appMocks.deleteHardware.mockResolvedValue(undefined);
		appMocks.sendHardwareDeviceBrightness.mockResolvedValue(undefined);
		renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		const slider = await screen.findByRole('slider', { name: 'Brightness Brightness Pet' });
		fireEvent.input(slider, { target: { value: '87' } });
		fireEvent.click(screen.getByRole('button', { name: 'Remove Brightness Pet' }));
		fireEvent.click(await screen.findByRole('button', { name: 'Remove' }));

		await waitFor(() => expect(appMocks.deleteHardware).toHaveBeenCalledWith('esp32s3-brightness-remove'));
		await new Promise((resolve) => setTimeout(resolve, 150));
		expect(appMocks.sendHardwareDeviceBrightness).not.toHaveBeenCalled();
	});

    it('separates per-device adjustments from playback and removal actions', async () => {
        appMocks.listHardwareBindings.mockResolvedValue({ devices: [
            { clientId: 'esp32s3-actions', clientName: 'Action Pet', online: true },
        ], maxDevices: 5, boundCount: 2 });
        renderSettings({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
            hardware_welcome_audio_path: 'C:/welcome.wav',
        });

        await screen.findByText('Action Pet');
        const playback = screen.getByRole('button', { name: 'Play remotely Action Pet' });
        const removal = screen.getByRole('button', { name: 'Remove Action Pet' });
        const volume = screen.getByRole('slider', { name: 'Volume Action Pet' });
        expect(playback.parentElement).toBe(removal.parentElement);
        expect(playback.parentElement?.classList.contains('im-settings-hardware__device-command-actions')).toBe(true);
        expect(playback.parentElement?.querySelectorAll(':scope > .im-settings-button')).toHaveLength(2);
        expect(volume.closest('.im-settings-hardware__device-adjustments')).not.toBeNull();
        expect(volume.closest('.im-settings-hardware__device-adjustments')).not.toBe(playback.parentElement);
    });

    it('keeps all device controls in their dedicated layout group', async () => {
        appMocks.listHardwareBindings.mockResolvedValue({ devices: [
            { clientId: 'esp32s3-adjustments', clientName: 'Adjustment Pet', online: true },
        ], maxDevices: 5, boundCount: 1 });
        renderSettings({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
            hardware_welcome_audio_path: 'C:/welcome.wav',
        });

        await screen.findByText('Adjustment Pet');
        const sleep = screen.getByRole('combobox', { name: 'Screen sleep Adjustment Pet' });
        const brightness = screen.getByRole('slider', { name: 'Brightness Adjustment Pet' });
        const volume = screen.getByRole('slider', { name: 'Volume Adjustment Pet' });
        const adjustments = volume.closest('.im-settings-hardware__device-adjustments');

        expect(adjustments).not.toBeNull();
        expect(sleep.closest('.im-settings-hardware__device-adjustments')).toBe(adjustments);
        expect(brightness.closest('.im-settings-hardware__device-adjustments')).toBe(adjustments);
        expect(adjustments?.querySelectorAll(':scope > .im-settings-hardware__device-agent, :scope > .im-settings-hardware__device-screen-sleep, :scope > .im-settings-hardware__device-brightness, :scope > .im-settings-hardware__device-volume')).toHaveLength(5);
    });

    it('uses the same scoped action group when the device name is long', async () => {
        const longName = 'ESP32-S3 living-room hardware with a deliberately long device name';
        appMocks.listHardwareBindings.mockResolvedValue({ devices: [
            { clientId: 'esp32s3-long', clientName: longName, online: true },
        ], maxDevices: 5, boundCount: 2 });
        renderSettings({
            thirdparty_gateway_enabled: true,
            thirdparty_gateway_local_mode: false,
            hardware_enabled: true,
            hardware_welcome_audio_path: 'C:/welcome.wav',
        });

        await screen.findByText(longName);
        const playback = screen.getByRole('button', { name: `Play remotely ${longName}` });
        const removal = screen.getByRole('button', { name: `Remove ${longName}` });
        expect(playback.parentElement).toBe(removal.parentElement);
        expect(playback.parentElement?.classList.contains('im-settings-hardware__device-command-actions')).toBe(true);
        expect(playback.closest('.im-settings-hardware__device')?.querySelector('.im-settings-hardware__device-details')).not.toBeNull();
    });

    it('loads and removes hardware under React StrictMode', async () => {
        appMocks.listHardwareBindings.mockResolvedValue({ devices: [
            { clientId: 'esp32s3-strict', clientName: 'Strict Pet', online: true },
        ], maxDevices: 5, boundCount: 1 });
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
            mode: 'hardware' as const,
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
        let finishRefresh!: (bindings: unknown) => void;
        appMocks.listHardwareBindings
            .mockResolvedValueOnce({ devices: [{ clientId: 'esp32s3-race', clientName: 'Race Pet', online: true }], maxDevices: 5, boundCount: 1 })
            .mockImplementationOnce(() => new Promise((resolve) => { finishRefresh = resolve; }));
        appMocks.deleteHardware.mockResolvedValue(undefined);
        const props = renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

        expect(await screen.findByText('Race Pet')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
        await waitFor(() => expect(appMocks.listHardwareBindings).toHaveBeenCalledTimes(2));
        fireEvent.click(screen.getByRole('button', { name: 'Remove Race Pet' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Remove' }));
        await waitFor(() => expect(appMocks.deleteHardware).toHaveBeenCalledWith('esp32s3-race'));

        finishRefresh({ devices: [{ clientId: 'esp32s3-race', clientName: 'Race Pet', online: true }], maxDevices: 5, boundCount: 1 });
        await act(async () => { await Promise.resolve(); });
        expect(screen.queryByText('Race Pet')).toBeNull();
        expect((screen.getByRole('button', { name: 'Refresh' }) as HTMLButtonElement).disabled).toBe(false);
    });

    it('keeps the binding when the custom removal dialog is cancelled', async () => {
	        appMocks.listHardwareBindings.mockResolvedValue({ devices: [{ clientId: 'esp32s3-stay', clientName: 'Stay Pet', online: true }], maxDevices: 5, boundCount: 1 });
        renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

        expect(await screen.findByText('Stay Pet')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Remove Stay Pet' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }));

        expect(appMocks.deleteHardware).not.toHaveBeenCalled();
        expect(screen.getByText('Stay Pet')).toBeTruthy();
    });

    it('shows a retry state instead of a false empty state when listing fails', async () => {
	        appMocks.listHardwareBindings.mockRejectedValueOnce(new Error('hub unavailable')).mockResolvedValueOnce({ devices: [
            { clientId: 'esp32s3-retry', clientName: 'Recovered Pet', online: true },
        ], maxDevices: 5, boundCount: 1 });
        const props = renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

        expect((await screen.findByRole('alert')).textContent).toContain('Could not load hardware');
        expect(screen.queryByText('No hardware is bound yet.')).toBeNull();
        expect(props.showToastMessage).toHaveBeenCalledWith('hub unavailable');

        fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
        expect(await screen.findByText('Recovered Pet')).toBeTruthy();
    });

    it('ignores errors from an obsolete hardware-list request', async () => {
        let failFirst!: (error: Error) => void;
        appMocks.listHardwareBindings
            .mockImplementationOnce(() => new Promise((_, reject) => { failFirst = reject; }))
            .mockResolvedValueOnce({ devices: [{ clientId: 'esp32s3-current', clientName: 'Current Pet', online: true }], maxDevices: 5, boundCount: 1 });
        const props = renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

        props.rerenderSettings({ config: { ...props.config, hardware_enabled: false } });
        props.rerenderSettings({ config: { ...props.config, hardware_enabled: true } });
        expect(await screen.findByText('Current Pet')).toBeTruthy();
        failFirst(new Error('obsolete failure'));
        await act(async () => { await Promise.resolve(); });

        expect(props.showToastMessage).not.toHaveBeenCalledWith('obsolete failure');
        expect(screen.queryByRole('alert')).toBeNull();
    });

    it('keeps a device visible when unlinking fails', async () => {
        appMocks.listHardwareBindings.mockResolvedValue({ devices: [{ clientId: 'esp32s3-safe', clientName: 'Safe Pet', online: false }], maxDevices: 5, boundCount: 1 });
        appMocks.deleteHardware.mockRejectedValue(new Error('delete failed'));
        const props = renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

        expect(await screen.findByText('Safe Pet')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Remove Safe Pet' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Remove' }));
        await waitFor(() => expect(props.showToastMessage).toHaveBeenCalledWith('delete failed'));
        expect(screen.getByText('Safe Pet')).toBeTruthy();
    });

    it('lets each hardware device select an AI expert and reply voice', async () => {
        appMocks.listHardwareBindings.mockResolvedValue({ devices: [{
            clientId: 'esp32s3-policy', clientName: 'Policy Pet', online: true,
        }], maxDevices: 5, boundCount: 1 });
        appMocks.listExperts.mockResolvedValue(JSON.stringify([{
            id: 'expert-support', name: 'Support expert', icon: 'S', tools: [], skills: [],
        }]));
        renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

        expect(await screen.findByText('Policy Pet')).toBeTruthy();
        const assistant = screen.getByRole('combobox', { name: 'AI assistant Policy Pet' });
        expect((assistant as HTMLSelectElement).value).toBe('__general__');
        fireEvent.change(assistant, { target: { value: 'expert-support' } });
        await waitFor(() => expect(appMocks.setHardwareAgentBinding).toHaveBeenCalledWith('esp32s3-policy', {
            assistant_mode: 'expert', expert_id: 'expert-support', tts_voice_id: 'zf_xiaoxiao',
        }));

        const voice = screen.getByRole('combobox', { name: 'Reply voice Policy Pet' });
        expect((voice as HTMLSelectElement).value).toBe('zf_xiaoxiao');
        fireEvent.change(voice, { target: { value: 'zm_yunxi' } });
        await waitFor(() => expect(appMocks.setHardwareAgentBinding).toHaveBeenLastCalledWith('esp32s3-policy', {
            assistant_mode: 'expert', expert_id: 'expert-support', tts_voice_id: 'zm_yunxi',
        }));
    });

	 it('keeps a deleted hardware expert visible so it can be replaced', async () => {
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [{
			clientId: 'esp32s3-stale-expert', clientName: 'Stale Expert Pet', online: true,
			assistantMode: 'expert', expertId: 'expert-removed', ttsVoiceId: 'zf_xiaoxiao',
		}], maxDevices: 5, boundCount: 1 });
		appMocks.listExperts.mockResolvedValue('[]');
		renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		const assistant = await screen.findByRole('combobox', { name: 'AI assistant Stale Expert Pet' }) as HTMLSelectElement;
		expect(assistant.value).toBe('expert-removed');
		expect(screen.getByRole('option', { name: 'Unavailable: expert-removed' })).toBeTruthy();

		fireEvent.change(assistant, { target: { value: '__general__' } });
		await waitFor(() => expect(appMocks.setHardwareAgentBinding).toHaveBeenCalledWith('esp32s3-stale-expert', {
			assistant_mode: 'general', expert_id: '', tts_voice_id: 'zf_xiaoxiao',
		}));
	});

	it('allows a reply-voice update while the expert selection is being saved', async () => {
        let finishExpertSave!: () => void;
        appMocks.listHardwareBindings.mockResolvedValue({ devices: [{
            clientId: 'esp32s3-agent-race', clientName: 'Agent Race Pet', online: true,
        }], maxDevices: 5, boundCount: 1 });
        appMocks.listExperts.mockResolvedValue(JSON.stringify([{
            id: 'expert-support', name: 'Support expert', tools: [], skills: [],
        }]));
        appMocks.setHardwareAgentBinding
            .mockImplementationOnce(() => new Promise<void>((resolve) => { finishExpertSave = resolve; }))
            .mockResolvedValueOnce(undefined);
        renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

        const assistant = await screen.findByRole('combobox', { name: 'AI assistant Agent Race Pet' });
        fireEvent.change(assistant, { target: { value: 'expert-support' } });
        await waitFor(() => expect(appMocks.setHardwareAgentBinding).toHaveBeenCalledTimes(1));
		await waitFor(() => expect(assistant).toHaveProperty('value', 'expert-support'));
		const voice = screen.getByRole('combobox', { name: 'Reply voice Agent Race Pet' }) as HTMLSelectElement;
		expect(voice.disabled).toBe(false);
		fireEvent.change(voice, { target: { value: 'zm_yunxi' } });
		expect(appMocks.setHardwareAgentBinding).toHaveBeenCalledTimes(1);

		finishExpertSave();
		await waitFor(() => expect(appMocks.setHardwareAgentBinding).toHaveBeenLastCalledWith('esp32s3-agent-race', {
			assistant_mode: 'expert', expert_id: 'expert-support', tts_voice_id: 'zm_yunxi',
		}));
    });

	it('serializes rapid per-device policy saves so the latest expert and voice persist', async () => {
		let finishFirst!: () => void;
		let finishSecond!: () => void;
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [{
			clientId: 'esp32s3-agent-save-order', clientName: 'Save Order Pet', online: true,
		}], maxDevices: 5, boundCount: 1 });
		appMocks.listExperts.mockResolvedValue(JSON.stringify([{
			id: 'expert-support', name: 'Support expert', tools: [], skills: [],
		}]));
		appMocks.setHardwareAgentBinding
			.mockImplementationOnce(() => new Promise<void>((resolve) => { finishFirst = resolve; }))
			.mockImplementationOnce(() => new Promise<void>((resolve) => { finishSecond = resolve; }));
		renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		const assistant = await screen.findByRole('combobox', { name: 'AI assistant Save Order Pet' });
		fireEvent.change(assistant, { target: { value: 'expert-support' } });
		await waitFor(() => expect(appMocks.setHardwareAgentBinding).toHaveBeenCalledTimes(1));

		fireEvent.change(screen.getByRole('combobox', { name: 'Reply voice Save Order Pet' }), { target: { value: 'zm_yunxi' } });
		expect(appMocks.setHardwareAgentBinding).toHaveBeenCalledTimes(1);

		finishFirst();
		await waitFor(() => expect(appMocks.setHardwareAgentBinding).toHaveBeenCalledTimes(2));
		expect(appMocks.setHardwareAgentBinding).toHaveBeenLastCalledWith('esp32s3-agent-save-order', {
			assistant_mode: 'expert', expert_id: 'expert-support', tts_voice_id: 'zm_yunxi',
		});
		finishSecond();
	});

	it('restores the last confirmed policy if the latest queued save fails', async () => {
		let finishFirst!: () => void;
		appMocks.listHardwareBindings.mockResolvedValue({ devices: [{
			clientId: 'esp32s3-agent-queued-failure', clientName: 'Queued Failure Pet', online: true,
		}], maxDevices: 5, boundCount: 1 });
		appMocks.listExperts.mockResolvedValue(JSON.stringify([{
			id: 'expert-support', name: 'Support expert', tools: [], skills: [],
		}]));
		appMocks.setHardwareAgentBinding
			.mockImplementationOnce(() => new Promise<void>((resolve) => { finishFirst = resolve; }))
			.mockRejectedValueOnce(new Error('latest policy failed'));
		const props = renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

		const assistant = await screen.findByRole('combobox', { name: 'AI assistant Queued Failure Pet' });
		fireEvent.change(assistant, { target: { value: 'expert-support' } });
		await waitFor(() => expect(appMocks.setHardwareAgentBinding).toHaveBeenCalledTimes(1));
		fireEvent.change(screen.getByRole('combobox', { name: 'Reply voice Queued Failure Pet' }), { target: { value: 'zm_yunxi' } });
		finishFirst();

		await waitFor(() => expect(appMocks.setHardwareAgentBinding).toHaveBeenCalledTimes(2));
		await waitFor(() => expect(screen.getByRole('combobox', { name: 'AI assistant Queued Failure Pet' })).toHaveProperty('value', 'expert-support'));
		expect(screen.getByRole('combobox', { name: 'Reply voice Queued Failure Pet' })).toHaveProperty('value', 'zf_xiaoxiao');
		expect(props.showToastMessage).toHaveBeenCalledWith('latest policy failed');
	});

    it('keeps an optimistic agent binding when a refresh returns the older Hub record', async () => {
        let finishRefresh!: (value: unknown) => void;
        appMocks.listHardwareBindings
            .mockResolvedValueOnce({ devices: [{
                clientId: 'esp32s3-agent-refresh', clientName: 'Agent Refresh Pet', online: true,
                assistantMode: 'general', ttsVoiceId: 'zf_xiaoxiao',
            }], maxDevices: 5, boundCount: 1 })
            .mockImplementationOnce(() => new Promise((resolve) => { finishRefresh = resolve; }));
        appMocks.listExperts.mockResolvedValue(JSON.stringify([{
            id: 'expert-support', name: 'Support expert', tools: [], skills: [],
        }]));
        renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

        const assistant = await screen.findByRole('combobox', { name: 'AI assistant Agent Refresh Pet' });
        fireEvent.change(assistant, { target: { value: 'expert-support' } });
        await waitFor(() => expect(appMocks.setHardwareAgentBinding).toHaveBeenCalledWith('esp32s3-agent-refresh', {
            assistant_mode: 'expert', expert_id: 'expert-support', tts_voice_id: 'zf_xiaoxiao',
        }));

        fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
        finishRefresh({ devices: [{
            clientId: 'esp32s3-agent-refresh', clientName: 'Agent Refresh Pet', online: true,
            assistantMode: 'general', ttsVoiceId: 'zf_xiaoxiao',
        }], maxDevices: 5, boundCount: 1 });

        await waitFor(() => expect(screen.getByRole('combobox', { name: 'AI assistant Agent Refresh Pet' })).toHaveProperty('value', 'expert-support'));
    });

    it('restores the previous agent binding when its save fails', async () => {
        appMocks.listHardwareBindings.mockResolvedValue({ devices: [{
            clientId: 'esp32s3-agent-failure', clientName: 'Agent Failure Pet', online: true,
            assistantMode: 'general', ttsVoiceId: 'zf_xiaoxiao',
        }], maxDevices: 5, boundCount: 1 });
        appMocks.listExperts.mockResolvedValue(JSON.stringify([{
            id: 'expert-support', name: 'Support expert', tools: [], skills: [],
        }]));
        appMocks.setHardwareAgentBinding.mockRejectedValue(new Error('agent binding failed'));
        const props = renderSettings({ thirdparty_gateway_enabled: true, thirdparty_gateway_local_mode: false, hardware_enabled: true });

        const assistant = await screen.findByRole('combobox', { name: 'AI assistant Agent Failure Pet' });
        fireEvent.change(assistant, { target: { value: 'expert-support' } });

        await waitFor(() => expect(assistant).toHaveProperty('value', '__general__'));
        expect(props.showToastMessage).toHaveBeenCalledWith('agent binding failed');
    });

});
