import { Dispatch, SetStateAction, useCallback, useEffect, useRef, useState } from 'react';
import { BrowserOpenURL } from '../../../wailsjs/runtime';
import {
    CreateThirdPartyDevicePairing,
    DeleteThirdPartyHardwareDevice,
    GenerateHardwareWelcomeAudio,
    GetHardwareWelcomeAudioDataURL,
	LoadConfigForUI,
	GetPetPackPreviewDataURL,
	ListThirdPartyHardwareDeviceBindings,
	ListExperts,
    ResetHardwareWelcomeAudio,
    RefreshDeviceAmbientWeather,
    RestartThirdPartyGateway,
    ListPetPacks,
    SelectHardwareWelcomeAudio,
	SendHardwareDevicePetProfile,
	SetHardwareAllowCustomPets,
	SendHardwareDeviceVolume,
	SendHardwareDeviceBrightness,
	SendHardwareDeviceScreenSleepTimeout,
	SetThirdPartyHardwareDeviceAlias,
	SetHardwareAgentBinding,
    SendHardwareWelcomeAudioRemote,
    SetHardwareEnabled,
    SetThirdPartyGatewayLocalMode,
    StopThirdPartyGateway,
} from '../../../wailsjs/go/main/App';
import { corelib } from '../../../wailsjs/go/models';
import { useDialog } from '../CustomDialog';
import { getPetSkinOption, packInfoToSkinOption, petSkinOptions, type PetSkinOption } from '../petSkins';
import { channelModeLabel, textForLang, watchLabel } from './imSettingsShared';
import { WelcomeResetButton } from './WelcomeResetButton';
import { ttsVoiceOptions } from '../../constants/ttsVoices';
import { parseExpertListJSON, type ExpertDefinition } from '../ai/expertTypes';

type ThirdPartyAccessSettingsProps = {
    config: corelib.AppConfig | null;
    setConfig: Dispatch<SetStateAction<corelib.AppConfig | null>>;
    lang: string;
    saveRemoteConfigField: (patch: Record<string, any>) => any;
    showToastMessage: (message: string) => void;
    setIMAuditPlatform: Dispatch<SetStateAction<string | null>>;
    thirdPartyGatewayStatus: string;
    setThirdPartyGatewayStatus: Dispatch<SetStateAction<string>>;
    thirdPartyGatewayLocalMode: boolean;
    setThirdPartyGatewayLocalModeState: Dispatch<SetStateAction<boolean>>;
    mode?: 'im' | 'hardware';
};

type HardwareDevice = {
    clientId: string;
    clientName?: string;
    protocolVersion?: string;
    pairedAt?: string;
    lastSeenAt?: string;
    online?: boolean;
	volume?: number;
	brightness?: number;
	screenSleepSeconds?: number;
	petSkin?: string;
	assistantMode?: string;
	expertId?: string;
	ttsVoiceId?: string;
};

type HardwareDeviceBindings = {
	devices?: HardwareDevice[];
	maxDevices?: number;
	boundCount?: number;
	// Older desktop builds exposed Go field names at the Wails boundary. Keep
	// this narrow compatibility path so an updated frontend never renders an
	// existing Hub binding list as empty during a staged desktop rollout.
	Devices?: HardwareDevice[];
	MaxDevices?: number;
	BoundCount?: number;
};

type HardwareAgentBindingValue = {
	assistantMode: string;
	expertId: string;
	ttsVoiceId: string;
};

const hardwareAgentBindingValue = (device: Pick<HardwareDevice, 'assistantMode' | 'expertId' | 'ttsVoiceId'>): HardwareAgentBindingValue => ({
	assistantMode: device.assistantMode === 'expert' ? 'expert' : 'general',
	expertId: device.assistantMode === 'expert' ? (device.expertId || '') : '',
	ttsVoiceId: device.ttsVoiceId || 'zf_xiaoxiao',
});

const screenSleepOptions = [
	{ seconds: 60, en: '1 minute', zh: '1 分钟' },
	{ seconds: 180, en: '3 minutes', zh: '3 分钟' },
	{ seconds: 300, en: '5 minutes', zh: '5 分钟' },
	{ seconds: 600, en: '10 minutes', zh: '10 分钟' },
	{ seconds: 1800, en: '30 minutes', zh: '30 分钟' },
	{ seconds: 3600, en: '1 hour', zh: '1 小时' },
	{ seconds: 7200, en: '2 hours', zh: '2 小时' },
	{ seconds: 10800, en: '3 hours', zh: '3 小时' },
	{ seconds: 14400, en: '4 hours', zh: '4 小时' },
	{ seconds: 18000, en: '5 hours', zh: '5 小时' },
	{ seconds: 0, en: 'Never', zh: '不关闭' },
] as const;

const hardwareBindingLimit = 5;

const numberOrFallback = (value: unknown, fallback: number) => {
	const parsed = Number(value);
	return Number.isFinite(parsed) && parsed >= 0 ? Math.floor(parsed) : fallback;
};

const normalizeHardwareDeviceBindings = (value: HardwareDeviceBindings | null | undefined) => {
	const devices = Array.isArray(value?.devices)
		? value.devices
		: Array.isArray(value?.Devices)
			? value.Devices
			: [];
	return {
		devices,
		maxDevices: numberOrFallback(value?.maxDevices ?? value?.MaxDevices, hardwareBindingLimit),
		boundCount: numberOrFallback(value?.boundCount ?? value?.BoundCount, devices.length),
	};
};

type WelcomeAction = 'generate' | 'import' | 'reset' | 'preview-local';

type HardwarePairing = {
	pairCode?: string;
	expiresAt?: string;
	transport?: string;
	gatewayURL?: string;
};

const gatewayStatusLabel = (status: string, lang: string) => ({
    connected: lang === 'en' ? 'Running' : '已启动',
    connecting: lang === 'en' ? 'Starting' : '启动中',
    disconnected: lang === 'en' ? 'Stopped' : '未连接',
    disabled: lang === 'en' ? 'Disabled' : '未启用',
    error: lang === 'en' ? 'Error' : '错误',
}[status] || status);

const welcomePreviewErrorMessage = (error: unknown, isZh: boolean) => {
    const message = (error as any)?.message || String(error);
    const errorCode = message.toUpperCase();
    if (message.includes('welcome audio is too long after conversion')) {
        return isZh
            ? '欢迎音频超过硬件约 3 秒的容量。已自动尝试加快语速；请再缩短欢迎词后重试。'
            : 'The welcome audio exceeds the hardware’s roughly 3-second capacity. It was automatically sped up; shorten the message and try again.';
    }
    if (errorCode.includes('HARDWARE_NOT_OWNED')) {
        return isZh
            ? '当前设备的绑定身份与 Hub 不一致。请刷新设备列表并等待设备重连；若问题持续，请检查它是否连接到了正确的 Hub。'
            : 'This device’s binding identity no longer matches the Hub. Refresh the device list and wait for it to reconnect; if this persists, check that it is connected to the correct Hub.';
    }
    if (errorCode.includes('HARDWARE_OFFLINE')) {
        return isZh
            ? '该硬件当前离线或刚断开连接。请等待设备重连后再试。'
            : 'This hardware is offline or was just disconnected. Wait for it to reconnect and try again.';
    }
    if (errorCode.includes('HARDWARE_UNSUPPORTED')) {
        return isZh
            ? '该硬件不支持欢迎音频远程播放。请升级 ESP32 固件并检查播放能力。'
            : 'This hardware does not support remote welcome-audio playback. Update the ESP32 firmware and check its playback capability.';
    }
    if (errorCode.includes('HARDWARE_DISABLED')) {
        return isZh
            ? '当前 GUI 未启用硬件控制。请先启用硬件后重试。'
            : 'Hardware control is disabled in this GUI. Enable hardware and try again.';
    }
    if (errorCode.includes('HARDWARE_STALE_REPLY')) {
        return isZh
            ? '该硬件连接已更新，之前的播放请求已失效。请等待设备稳定连接后再试。'
            : 'The hardware connection changed and the earlier playback request expired. Wait for it to reconnect steadily and try again.';
    }
    if (errorCode.includes('HARDWARE_UNAVAILABLE')) {
        return isZh
            ? 'Hub 暂时无法向该硬件下发命令。请稍后重试；若持续出现，请检查 Hub 与设备连接。'
            : 'The Hub cannot send a command to this hardware right now. Try again shortly; if this persists, check the Hub and device connection.';
    }
    if (!isZh) return message;
    if (message.includes('NO_COMPATIBLE_HARDWARE') || message.includes('no online remote ESP32')) {
        return '没有在线且支持欢迎音频播放的远程 ESP32。请检查设备联网、配对状态和固件能力。';
    }
    if (message.includes('Hub is not connected') || message.includes('Hub disconnected') || message.includes('Hub connection lost')) {
        return 'Hub 当前未连接，无法进行远程硬件测试。请先恢复 MaClaw 与 Hub 的连接。';
    }
    if (message.includes('timed out waiting for ESP32') || message.includes('playback confirmation')) {
        return '已下发音频，但未收到 ESP32 的播放完成确认。请检查设备网络、扬声器和固件日志。';
    }
    if (message.toLowerCase().includes('muted')) {
        return '硬件音量为 0，无法进行远程播放测试。请先调高音量。';
    }
    return message;
};

const welcomeTextCapacityHint = (text: string, isZh: boolean) => {
    const characters = Array.from(text.trim()).length;
    if (characters === 0) return '';
    if (characters > 40) {
        return isZh
            ? `当前 ${characters}/80 字，较长的欢迎词可能超过硬件约 3 秒的容量。`
            : `${characters}/80 characters — a longer greeting may exceed the hardware’s roughly 3-second capacity.`;
    }
    return isZh ? `${characters}/80 字` : `${characters}/80 characters`;
};

export const ThirdPartyAccessSettings = ({
    config,
    setConfig,
    lang,
    saveRemoteConfigField,
    showToastMessage,
    setIMAuditPlatform,
    thirdPartyGatewayStatus,
    setThirdPartyGatewayStatus,
    thirdPartyGatewayLocalMode,
    setThirdPartyGatewayLocalModeState,
    mode = 'im',
}: ThirdPartyAccessSettingsProps) => {
    const { showConfirm } = useDialog();
	const [pairing, setPairing] = useState<HardwarePairing | null>(null);
	const [pairingBusy, setPairingBusy] = useState(false);
	const pairingRequestRef = useRef(0);
    const [welcomeText, setWelcomeText] = useState('');
    const [welcomeVoiceID, setWelcomeVoiceID] = useState('af_heart');
    const [welcomeVoiceSaving, setWelcomeVoiceSaving] = useState(false);
    const [busy, setBusy] = useState(false);
    const [gatewayBusy, setGatewayBusy] = useState(false);
    const [welcomeAction, setWelcomeAction] = useState<WelcomeAction | null>(null);
    const [devices, setDevices] = useState<HardwareDevice[]>([]);
    const [devicesLoading, setDevicesLoading] = useState(false);
    const [devicesError, setDevicesError] = useState('');
	const [boundDeviceCount, setBoundDeviceCount] = useState(0);
	const [deletingClientIds, setDeletingClientIds] = useState<Set<string>>(() => new Set());
	const [petOptions, setPetOptions] = useState<PetSkinOption[]>(petSkinOptions);
	const [savingPetClientIds, setSavingPetClientIds] = useState<Set<string>>(() => new Set());
	const [editingDeviceNameClientId, setEditingDeviceNameClientId] = useState<string | null>(null);
	const [deviceNameDraft, setDeviceNameDraft] = useState('');
	const [savingDeviceNameClientIds, setSavingDeviceNameClientIds] = useState<Set<string>>(() => new Set());
	const [openPetPickerClientId, setOpenPetPickerClientId] = useState<string | null>(null);
	const [previewingClientIds, setPreviewingClientIds] = useState<Set<string>>(() => new Set());
	const [experts, setExperts] = useState<ExpertDefinition[]>([]);
    const [ambientCityDraft, setAmbientCityDraft] = useState(() => String((config as any)?.pet_ambient_city || ''));
    const [detectedAmbientCity, setDetectedAmbientCity] = useState<string | null>(null);
    const [ambientRefreshState, setAmbientRefreshState] = useState<'idle' | 'refreshing' | 'done' | 'error'>('idle');
    const localPreviewAudioRef = useRef<HTMLAudioElement | null>(null);
    const localPreviewCleanupRef = useRef<(() => void) | null>(null);
    const localPreviewSourceRef = useRef('');
    const localPreviewSourceLoadRef = useRef<Promise<string> | null>(null);
	// A config refresh can replace the welcome WAV while an older preview
	// request is still in flight. Keep a generation so its late response cannot
	// become the cache used by the next preview.
	const localPreviewSourceVersionRef = useRef(0);
    const mountedRef = useRef(true);
    const devicesRequestRef = useRef(0);
    const refreshDevicesRef = useRef<() => Promise<void>>(async () => undefined);
    const showToastMessageRef = useRef(showToastMessage);
	const previewRunRef = useRef(0);
	const volumeSendTimersRef = useRef(new Map<string, ReturnType<typeof setTimeout>>());
	const pendingVolumesRef = useRef(new Map<string, number>());
	const volumeSendsInFlightRef = useRef(new Set<string>());
	const volumeFlushPendingRef = useRef(new Set<string>());
		const deviceVolumeBeforeEditRef = useRef(new Map<string, number>());
		const brightnessSendTimersRef = useRef(new Map<string, ReturnType<typeof setTimeout>>());
		const pendingBrightnessRef = useRef(new Map<string, number>());
		const brightnessSendsInFlightRef = useRef(new Set<string>());
	const brightnessFlushPendingRef = useRef(new Set<string>());
	const deviceBrightnessBeforeEditRef = useRef(new Map<string, number>());
	const pendingScreenSleepsRef = useRef(new Map<string, number>());
	// Keep the optimistic selection separate from the send queue. The queued
	// value is removed just before its request starts, whereas a concurrent
	// device-list refresh must continue to render the user's latest choice
	// until Hub confirms or rejects it.
	const optimisticScreenSleepsRef = useRef(new Map<string, number>());
	const screenSleepSendsInFlightRef = useRef(new Set<string>());
	const screenSleepFlushPendingRef = useRef(new Set<string>());
	const confirmedScreenSleepsRef = useRef(new Map<string, number>());
	// Preserve a just-selected expert or reply voice while a device-list refresh
	// is still returning the older Hub record.
	type AgentBindingDraft = { value: HardwareAgentBindingValue; request: number };
	const optimisticAgentBindingsRef = useRef(new Map<string, AgentBindingDraft>());
	const confirmedAgentBindingsRef = useRef(new Map<string, HardwareAgentBindingValue>());
	const agentBindingRequestRef = useRef(new Map<string, number>());
	const agentBindingSaveTailsRef = useRef(new Map<string, Promise<void>>());
		const deletedClientIdsRef = useRef(new Set<string>());
		const pendingDeletedClientIdsRef = useRef(new Set<string>());
		// A client ID can be paired again after it was removed. Keep a small
		// lifecycle counter so completions from the previous binding cannot mutate
		// or notify against its newly-paired replacement.
		const deviceLifecycleRef = useRef(new Map<string, number>());
		const confirmedAbsentDeletedClientIdsRef = useRef(new Set<string>());
		// Local deletion time per client ID. A Hub binding whose pairedAt is newer
		// than this mark is a fresh re-pairing, not a stale pre-delete list entry,
		// so the pending-delete filter below must release it again.
		const deletedAtClientIdsRef = useRef(new Map<string, number>());
    const ambientRefreshRequestRef = useRef(0);
    const ambientSaveTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const pendingAmbientCityRef = useRef<string | null>(null);
    const ambientSaveVersionRef = useRef(0);
    const ambientSaveTailRef = useRef<Promise<void>>(Promise.resolve());
    const ambientQueuedVersionRef = useRef(0);
    const ambientQueuedPromiseRef = useRef<Promise<void> | null>(null);
    const confirmedAmbientCityRef = useRef(String((config as any)?.pet_ambient_city || ''));
	const saveRemoteConfigFieldRef = useRef(saveRemoteConfigField);
	const petPickerRootRef = useRef<HTMLDivElement | null>(null);

    showToastMessageRef.current = showToastMessage;
    saveRemoteConfigFieldRef.current = saveRemoteConfigField;

    const isZh = lang === 'zh-Hans' || lang === 'zh-Hant';
    const isZhHant = lang === 'zh-Hant';
    const hardwareOnly = mode === 'hardware';
    const imGatewayEnabled = Boolean((config as any)?.thirdparty_gateway_enabled);
    const hardwareEnabled = Boolean((config as any)?.hardware_enabled);
    const settingsBusy = busy || gatewayBusy;
    const hardwareControlsDisabled = settingsBusy || !hardwareEnabled;
    // Welcome content can be prepared and previewed locally before hardware is
    // enabled. Only transport-dependent controls use hardwareControlsDisabled.
    const welcomeEditingDisabled = settingsBusy;
	const defaultVolume = Number((config as any)?.hardware_volume ?? 70);
	const defaultBrightness = Number((config as any)?.hardware_brightness ?? 70);
	const allowCustomHardwarePets = Boolean((config as any)?.hardware_allow_custom_pets);
	const hardwareBindingAtCapacity = boundDeviceCount >= hardwareBindingLimit;
    const welcomeAudioPath = String((config as any)?.hardware_welcome_audio_path || '').trim();

    useEffect(() => {
        setWelcomeText(String((config as any)?.hardware_welcome_text || 'Hello, Maclaw'));
    }, [(config as any)?.hardware_welcome_text]);

    useEffect(() => {
        setWelcomeVoiceID(String((config as any)?.hardware_welcome_voice_id || 'af_heart'));
    }, [(config as any)?.hardware_welcome_voice_id]);

    useEffect(() => {
		if (hardwareEnabled) return;
		pairingRequestRef.current += 1;
		setPairingBusy(false);
		setPairing(null);
	}, [hardwareEnabled]);

	useEffect(() => {
		const expiresAt = String(pairing?.expiresAt || '');
		const expiresAtMs = Date.parse(expiresAt);
		if (!expiresAt || Number.isNaN(expiresAtMs)) return;
		const remaining = expiresAtMs - Date.now();
		if (remaining <= 0) {
			setPairing(null);
			return;
		}
		const timer = window.setTimeout(() => {
			setPairing((current) => current?.expiresAt === expiresAt ? null : current);
			showToastMessageRef.current(isZh ? '配对码已过期，请重新生成。' : 'Pairing code expired. Generate a new one.');
		}, remaining);
		return () => window.clearTimeout(timer);
	}, [isZh, pairing?.expiresAt]);

	useEffect(() => {
		if (hardwareOnly) return;
		pairingRequestRef.current += 1;
		setPairingBusy(false);
		setPairing(null);
	}, [hardwareOnly]);

    useEffect(() => {
        const configuredCity = String((config as any)?.pet_ambient_city || '');
        // saveRemoteConfigField writes optimistically before the backend confirms.
        // Keep the local dirty marker until that promise resolves, otherwise a
        // quick "Sync now" can send weather for the previously persisted city.
        if (pendingAmbientCityRef.current !== null) return;
        confirmedAmbientCityRef.current = configuredCity;
        setAmbientCityDraft(configuredCity);
        ambientRefreshRequestRef.current += 1;
        if (configuredCity) setDetectedAmbientCity(null);
        setAmbientRefreshState('idle');
    }, [(config as any)?.pet_ambient_city]);

    const refreshConfig = async () => setConfig(await LoadConfigForUI() as any);

		const refreshDevices = useCallback(async () => {
        const requestID = ++devicesRequestRef.current;
        if (!hardwareEnabled) {
            setDevices([]);
			setBoundDeviceCount(0);
            setDevicesError('');
            setDevicesLoading(false);
            return;
        }
        setDevicesLoading(true);
		setDevicesError('');
		try {
			const bindings = normalizeHardwareDeviceBindings(await ListThirdPartyHardwareDeviceBindings() as HardwareDeviceBindings);
			if (!mountedRef.current || requestID !== devicesRequestRef.current) return;
				const pendingDeletedIDs = pendingDeletedClientIdsRef.current;
				const confirmedAbsentIDs = confirmedAbsentDeletedClientIdsRef.current;
				const incomingIDs = new Set(bindings.devices.map((device) => device.clientId));
				for (const clientId of pendingDeletedIDs) {
					if (!incomingIDs.has(clientId)) confirmedAbsentIDs.add(clientId);
				}
				const next = bindings.devices.filter((device) => {
					if (!pendingDeletedIDs.has(device.clientId)) return true;
					// A binding paired after our delete is a fresh re-pairing, not
					// a stale pre-delete list entry. The tolerance absorbs clock
					// skew between this desktop and the Hub.
					const deletedAt = deletedAtClientIdsRef.current.get(device.clientId);
					const pairedAtMs = Date.parse(device.pairedAt || '');
					if (deletedAt !== undefined && !Number.isNaN(pairedAtMs) && pairedAtMs + 15000 > deletedAt) {
						pendingDeletedIDs.delete(device.clientId);
						deletedClientIdsRef.current.delete(device.clientId);
						confirmedAbsentIDs.delete(device.clientId);
						deletedAtClientIdsRef.current.delete(device.clientId);
						return true;
					}
					// Hub first confirmed that the old binding disappeared. A later
					// appearance of this ID is a new pairing, not a stale list entry.
					if (!confirmedAbsentIDs.has(device.clientId)) return false;
					pendingDeletedIDs.delete(device.clientId);
					deletedClientIdsRef.current.delete(device.clientId);
					confirmedAbsentIDs.delete(device.clientId);
					deletedAtClientIdsRef.current.delete(device.clientId);
					return true;
				});
				setDevices(next.map((device) => {
					const optimisticSleep = optimisticScreenSleepsRef.current.get(device.clientId);
					const optimisticAgentBinding = optimisticAgentBindingsRef.current.get(device.clientId);
				let withOptimisticAgentBinding = device;
				if (optimisticAgentBinding !== undefined) {
					const bindingMatches = device.assistantMode === optimisticAgentBinding.value.assistantMode
						&& (device.expertId || '') === optimisticAgentBinding.value.expertId
						&& (device.ttsVoiceId || '') === optimisticAgentBinding.value.ttsVoiceId;
					if (bindingMatches) {
						optimisticAgentBindingsRef.current.delete(device.clientId);
						confirmedAgentBindingsRef.current.set(device.clientId, optimisticAgentBinding.value);
					}
					else withOptimisticAgentBinding = { ...device, ...optimisticAgentBinding.value };
				} else confirmedAgentBindingsRef.current.set(device.clientId, hardwareAgentBindingValue(device));
					if (optimisticSleep === undefined) return withOptimisticAgentBinding;
					// Clear only after Hub's authoritative list has caught up. This
					// also protects against a list request that began before a save
					// succeeded but returned afterwards with the old timeout.
					if (device.screenSleepSeconds === optimisticSleep) {
						optimisticScreenSleepsRef.current.delete(device.clientId);
						return withOptimisticAgentBinding;
					}
					return { ...withOptimisticAgentBinding, screenSleepSeconds: optimisticSleep };
				}));
				const pendingBoundCount = bindings.devices.filter((device) => pendingDeletedIDs.has(device.clientId)).length;
				setBoundDeviceCount(Math.max(0, bindings.boundCount - pendingBoundCount));
        } catch (err: any) {
            const message = err?.message || String(err);
            if (mountedRef.current && requestID === devicesRequestRef.current) {
                setDevicesError(message);
                showToastMessageRef.current(message);
            }
        } finally {
            if (mountedRef.current && requestID === devicesRequestRef.current) setDevicesLoading(false);
        }
    }, [hardwareEnabled]);
    refreshDevicesRef.current = refreshDevices;

    useEffect(() => {
		if (!hardwareOnly) return;
		void refreshDevicesRef.current();
	}, [hardwareEnabled, hardwareOnly]);
	useEffect(() => {
		if (!hardwareOnly) return;
		let cancelled = false;
		void ListExperts().then((raw) => {
			if (!cancelled) setExperts(parseExpertListJSON(raw));
		}).catch(() => {
			if (!cancelled) setExperts([]);
		});
		return () => { cancelled = true; };
	}, [hardwareOnly]);
	useEffect(() => {
		if (!hardwareOnly || !allowCustomHardwarePets) return;
		let cancelled = false;
		void ListPetPacks()
			.then(async (packs) => {
				if (!Array.isArray(packs) || packs.length === 0) return;
				const options = packs.map((pack) => packInfoToSkinOption(pack as unknown as Record<string, unknown>, lang));
				if (cancelled) return;
				setPetOptions(options);
				const withPreviews = await Promise.all(options.map(async (option) => {
					try {
						const preview = await GetPetPackPreviewDataURL(option.id);
						return preview?.startsWith('data:image/') ? { ...option, image: preview, hasPreview: true } : option;
					} catch {
						return option;
					}
				}));
				if (!cancelled) setPetOptions(withPreviews);
			})
			.catch(() => undefined);
		return () => { cancelled = true; };
	}, [allowCustomHardwarePets, hardwareOnly, lang]);
	useEffect(() => {
		if (!openPetPickerClientId) return;
		const closeWhenOutside = (event: PointerEvent) => {
			if (!petPickerRootRef.current?.contains(event.target as Node)) setOpenPetPickerClientId(null);
		};
		const closeOnEscape = (event: KeyboardEvent) => {
			if (event.key === 'Escape') setOpenPetPickerClientId(null);
		};
		document.addEventListener('pointerdown', closeWhenOutside);
		document.addEventListener('keydown', closeOnEscape);
		return () => {
			document.removeEventListener('pointerdown', closeWhenOutside);
			document.removeEventListener('keydown', closeOnEscape);
		};
	}, [openPetPickerClientId]);
    useEffect(() => {
        // React StrictMode runs an effect setup/cleanup/setup cycle in development.
        // Restore the guard during setup so the live second mount is not mistaken
        // for an unmounted component.
        mountedRef.current = true;
		return () => {
			mountedRef.current = false;
			pairingRequestRef.current += 1;
			devicesRequestRef.current += 1;
			previewRunRef.current += 1;
			volumeSendTimersRef.current.forEach((timer) => clearTimeout(timer));
			volumeSendTimersRef.current.clear();
			brightnessSendTimersRef.current.forEach((timer) => clearTimeout(timer));
			brightnessSendTimersRef.current.clear();
            if (ambientSaveTimerRef.current !== null) clearTimeout(ambientSaveTimerRef.current);
            if (pendingAmbientCityRef.current !== null) {
                const city = pendingAmbientCityRef.current;
                const version = ambientSaveVersionRef.current;
                // A save for this exact draft may already be queued or running.
                // Only append a final write when the debounce never got a chance
                // to enqueue it, or when a newer draft arrived behind that write.
                if (ambientQueuedVersionRef.current !== version || !ambientQueuedPromiseRef.current) {
                    void ambientSaveTailRef.current
                        .catch(() => undefined)
                        .then(() => saveRemoteConfigFieldRef.current({ pet_ambient_city: city }))
                        .catch(() => undefined);
                }
            }
			pendingVolumesRef.current.clear();
			volumeFlushPendingRef.current.clear();
			deviceVolumeBeforeEditRef.current.clear();
			pendingBrightnessRef.current.clear();
			brightnessFlushPendingRef.current.clear();
			deviceBrightnessBeforeEditRef.current.clear();
			pendingScreenSleepsRef.current.clear();
			optimisticScreenSleepsRef.current.clear();
			screenSleepFlushPendingRef.current.clear();
			confirmedScreenSleepsRef.current.clear();
			optimisticAgentBindingsRef.current.clear();
			confirmedAgentBindingsRef.current.clear();
			agentBindingRequestRef.current.clear();
			agentBindingSaveTailsRef.current.clear();
            localPreviewCleanupRef.current?.();
        };
    }, []);

    const refreshLocalWelcomePreviewSource = useCallback((force = false) => {
        if (!welcomeAudioPath) {
            localPreviewSourceRef.current = '';
            return Promise.resolve('');
        }
        if (!force && localPreviewSourceRef.current) return Promise.resolve(localPreviewSourceRef.current);
        if (!force && localPreviewSourceLoadRef.current) return localPreviewSourceLoadRef.current;
        if (force) {
			localPreviewSourceVersionRef.current += 1;
			localPreviewSourceRef.current = '';
			localPreviewSourceLoadRef.current = null;
		}
		const version = localPreviewSourceVersionRef.current;
        const request = Promise.resolve(GetHardwareWelcomeAudioDataURL()).then((source) => {
			if (mountedRef.current && version === localPreviewSourceVersionRef.current) localPreviewSourceRef.current = source;
            return source;
        });
        localPreviewSourceLoadRef.current = request;
        void request.finally(() => {
            if (localPreviewSourceLoadRef.current === request) localPreviewSourceLoadRef.current = null;
        }).catch(() => undefined);
        return request;
    }, [welcomeAudioPath]);

    useEffect(() => {
        if (!hardwareOnly) return;
        // Invalidate only. Fetch the WAV when the user asks to preview it so
        // opening Hardware Access does not eagerly move an audio payload.
		localPreviewSourceVersionRef.current += 1;
        localPreviewSourceRef.current = '';
		localPreviewSourceLoadRef.current = null;
    }, [hardwareOnly, welcomeAudioPath]);

    const stopLocalWelcomePreview = () => {
        previewRunRef.current += 1;
        localPreviewCleanupRef.current?.();
    };

	    const playLocalWelcomePreview = async () => {
        stopLocalWelcomePreview();
        const runID = previewRunRef.current;
        const source = localPreviewSourceRef.current || await refreshLocalWelcomePreviewSource();
        if (!mountedRef.current || runID !== previewRunRef.current) return false;
        const audio = new Audio(source);
        localPreviewAudioRef.current = audio;
        return new Promise<boolean>((resolve, reject) => {
            let settled = false;
            const finish = (completed: boolean, error?: Error) => {
                if (settled) return;
                settled = true;
                audio.onended = null;
                audio.onerror = null;
                if (localPreviewAudioRef.current === audio) localPreviewAudioRef.current = null;
                if (localPreviewCleanupRef.current === cleanup) localPreviewCleanupRef.current = null;
                error ? reject(error) : resolve(completed);
            };
            const cleanup = () => {
                audio.pause();
                finish(false);
            };
            localPreviewCleanupRef.current = cleanup;
            audio.onended = () => finish(true);
            audio.onerror = () => finish(false, new Error(isZh ? 'GUI 无法播放该欢迎音频。' : 'The GUI could not play this welcome audio.'));
            try {
                const playback = audio.play();
                if (playback && typeof playback.catch === 'function') {
                    playback.catch((err) => finish(false, err instanceof Error ? err : new Error(String(err))));
                }
            } catch (err) {
                finish(false, err instanceof Error ? err : new Error(String(err)));
            }
        });
	    };

		const deviceLifecycle = (clientId: string) => deviceLifecycleRef.current.get(clientId) ?? 0;
		const isCurrentDeviceLifecycle = (clientId: string, lifecycle: number) => deviceLifecycle(clientId) === lifecycle;
		const invalidateDeviceLifecycle = (clientId: string) => {
			deviceLifecycleRef.current.set(clientId, deviceLifecycle(clientId) + 1);
		};

		const sendDeviceVolume = useCallback(async (clientId: string, value: number, lifecycle: number) => {
		const timer = volumeSendTimersRef.current.get(clientId);
		if (timer !== undefined) {
			clearTimeout(timer);
			volumeSendTimersRef.current.delete(clientId);
		}
		pendingVolumesRef.current.set(clientId, Math.max(0, Math.min(100, Math.round(value))));
		if (volumeSendsInFlightRef.current.has(clientId)) return;

		volumeSendsInFlightRef.current.add(clientId);
		try {
			while (pendingVolumesRef.current.has(clientId)) {
				const next = pendingVolumesRef.current.get(clientId)!;
				pendingVolumesRef.current.delete(clientId);
				try {
					await SendHardwareDeviceVolume(clientId, next);
					if (deletedClientIdsRef.current.has(clientId) || !isCurrentDeviceLifecycle(clientId, lifecycle)) break;
					deviceVolumeBeforeEditRef.current.delete(clientId);
				} catch (err: any) {
					if (deletedClientIdsRef.current.has(clientId) || !isCurrentDeviceLifecycle(clientId, lifecycle)) break;
					pendingVolumesRef.current.delete(clientId);
					const previous = deviceVolumeBeforeEditRef.current.get(clientId);
					deviceVolumeBeforeEditRef.current.delete(clientId);
					if (previous !== undefined && mountedRef.current) {
						setDevices((current) => current.map((item) => item.clientId === clientId ? { ...item, volume: previous } : item));
					}
					showToastMessageRef.current(err?.message || String(err));
					await refreshDevicesRef.current().catch(() => undefined);
					break;
				}
			}
		} finally {
			volumeSendsInFlightRef.current.delete(clientId);
			if (volumeFlushPendingRef.current.delete(clientId) && pendingVolumesRef.current.has(clientId) && mountedRef.current) {
					void sendDeviceVolume(clientId, pendingVolumesRef.current.get(clientId)!, deviceLifecycle(clientId));
				}
			}
		}, []);

	const rememberDeviceVolumeBeforeEdit = useCallback((clientId: string) => {
		if (deviceVolumeBeforeEditRef.current.has(clientId)) return;
		setDevices((current) => {
			const currentDevice = current.find((item) => item.clientId === clientId);
			if (currentDevice) {
				deviceVolumeBeforeEditRef.current.set(clientId, Math.max(0, Math.min(100, Math.round(Number(currentDevice.volume ?? defaultVolume)))));
			}
			return current;
		});
	}, [defaultVolume]);

		const scheduleDeviceVolume = useCallback((clientId: string, value: number, immediate = false) => {
			const lifecycle = deviceLifecycle(clientId);
		const next = Math.max(0, Math.min(100, Math.round(value)));
		pendingVolumesRef.current.set(clientId, next);
		const timer = volumeSendTimersRef.current.get(clientId);
		if (timer !== undefined) {
			clearTimeout(timer);
			volumeSendTimersRef.current.delete(clientId);
		}
		if (immediate) {
			if (volumeSendsInFlightRef.current.has(clientId)) volumeFlushPendingRef.current.add(clientId);
				void sendDeviceVolume(clientId, next, lifecycle);
			return;
		}
		volumeSendTimersRef.current.set(clientId, setTimeout(() => {
			volumeSendTimersRef.current.delete(clientId);
				void sendDeviceVolume(clientId, pendingVolumesRef.current.get(clientId) ?? next, lifecycle);
			}, 100));
	}, [sendDeviceVolume]);

		const discardPendingDeviceVolume = useCallback((clientId: string) => {
		const timer = volumeSendTimersRef.current.get(clientId);
		if (timer !== undefined) {
			clearTimeout(timer);
			volumeSendTimersRef.current.delete(clientId);
		}
		pendingVolumesRef.current.delete(clientId);
		volumeFlushPendingRef.current.delete(clientId);
		deviceVolumeBeforeEditRef.current.delete(clientId);
		}, []);

		const sendDeviceBrightness = useCallback(async (clientId: string, value: number, lifecycle: number) => {
		const timer = brightnessSendTimersRef.current.get(clientId);
		if (timer !== undefined) {
			clearTimeout(timer);
			brightnessSendTimersRef.current.delete(clientId);
		}
		pendingBrightnessRef.current.set(clientId, Math.max(0, Math.min(100, Math.round(value))));
		if (brightnessSendsInFlightRef.current.has(clientId)) return;

		brightnessSendsInFlightRef.current.add(clientId);
		try {
			while (pendingBrightnessRef.current.has(clientId)) {
				const next = pendingBrightnessRef.current.get(clientId)!;
				pendingBrightnessRef.current.delete(clientId);
				try {
					await SendHardwareDeviceBrightness(clientId, next);
					if (deletedClientIdsRef.current.has(clientId) || !isCurrentDeviceLifecycle(clientId, lifecycle)) break;
					deviceBrightnessBeforeEditRef.current.delete(clientId);
				} catch (err: any) {
					if (deletedClientIdsRef.current.has(clientId) || !isCurrentDeviceLifecycle(clientId, lifecycle)) break;
					pendingBrightnessRef.current.delete(clientId);
					const previous = deviceBrightnessBeforeEditRef.current.get(clientId);
					deviceBrightnessBeforeEditRef.current.delete(clientId);
					if (previous !== undefined && mountedRef.current) {
						setDevices((current) => current.map((item) => item.clientId === clientId ? { ...item, brightness: previous } : item));
					}
					showToastMessageRef.current(err?.message || String(err));
					await refreshDevicesRef.current().catch(() => undefined);
					break;
				}
			}
		} finally {
			brightnessSendsInFlightRef.current.delete(clientId);
			if (brightnessFlushPendingRef.current.delete(clientId) && pendingBrightnessRef.current.has(clientId) && mountedRef.current) {
					void sendDeviceBrightness(clientId, pendingBrightnessRef.current.get(clientId)!, deviceLifecycle(clientId));
				}
			}
		}, []);

	const rememberDeviceBrightnessBeforeEdit = useCallback((clientId: string) => {
		if (deviceBrightnessBeforeEditRef.current.has(clientId)) return;
		setDevices((current) => {
			const currentDevice = current.find((item) => item.clientId === clientId);
			if (currentDevice) {
				deviceBrightnessBeforeEditRef.current.set(clientId, Math.max(0, Math.min(100, Math.round(Number(currentDevice.brightness ?? defaultBrightness)))));
			}
			return current;
		});
	}, [defaultBrightness]);

		const scheduleDeviceBrightness = useCallback((clientId: string, value: number, immediate = false) => {
			const lifecycle = deviceLifecycle(clientId);
		const next = Math.max(0, Math.min(100, Math.round(value)));
		pendingBrightnessRef.current.set(clientId, next);
		const timer = brightnessSendTimersRef.current.get(clientId);
		if (timer !== undefined) {
			clearTimeout(timer);
			brightnessSendTimersRef.current.delete(clientId);
		}
		if (immediate) {
			if (brightnessSendsInFlightRef.current.has(clientId)) brightnessFlushPendingRef.current.add(clientId);
				void sendDeviceBrightness(clientId, next, lifecycle);
			return;
		}
		brightnessSendTimersRef.current.set(clientId, setTimeout(() => {
			brightnessSendTimersRef.current.delete(clientId);
				void sendDeviceBrightness(clientId, pendingBrightnessRef.current.get(clientId) ?? next, lifecycle);
			}, 100));
	}, [sendDeviceBrightness]);

	const discardPendingDeviceBrightness = useCallback((clientId: string) => {
		const timer = brightnessSendTimersRef.current.get(clientId);
		if (timer !== undefined) {
			clearTimeout(timer);
			brightnessSendTimersRef.current.delete(clientId);
		}
		pendingBrightnessRef.current.delete(clientId);
		brightnessFlushPendingRef.current.delete(clientId);
		deviceBrightnessBeforeEditRef.current.delete(clientId);
	}, []);

	const sendDeviceScreenSleepTimeout = useCallback(async (clientId: string, seconds: number, lifecycle: number) => {
		pendingScreenSleepsRef.current.set(clientId, seconds);
		if (screenSleepSendsInFlightRef.current.has(clientId)) return;

		screenSleepSendsInFlightRef.current.add(clientId);
		try {
			while (pendingScreenSleepsRef.current.has(clientId)) {
				const next = pendingScreenSleepsRef.current.get(clientId)!;
				pendingScreenSleepsRef.current.delete(clientId);
				try {
					await SendHardwareDeviceScreenSleepTimeout(clientId, next);
					if (deletedClientIdsRef.current.has(clientId) || !isCurrentDeviceLifecycle(clientId, lifecycle)) break;
					confirmedScreenSleepsRef.current.set(clientId, next);
				} catch (err: any) {
					if (deletedClientIdsRef.current.has(clientId) || !isCurrentDeviceLifecycle(clientId, lifecycle)) break;
					pendingScreenSleepsRef.current.delete(clientId);
					optimisticScreenSleepsRef.current.delete(clientId);
					const previous = confirmedScreenSleepsRef.current.get(clientId);
					if (previous !== undefined && mountedRef.current) {
						setDevices((current) => current.map((device) => device.clientId === clientId ? { ...device, screenSleepSeconds: previous } : device));
					}
					showToastMessageRef.current(err?.message || String(err));
					await refreshDevicesRef.current().catch(() => undefined);
					break;
				}
			}
		} finally {
			screenSleepSendsInFlightRef.current.delete(clientId);
			if (screenSleepFlushPendingRef.current.delete(clientId) && pendingScreenSleepsRef.current.has(clientId) && mountedRef.current) {
				void sendDeviceScreenSleepTimeout(clientId, pendingScreenSleepsRef.current.get(clientId)!, deviceLifecycle(clientId));
			} else if (!pendingScreenSleepsRef.current.has(clientId)) {
				confirmedScreenSleepsRef.current.delete(clientId);
			}
		}
	}, []);

	const changeDeviceScreenSleepTimeout = useCallback((clientId: string, seconds: number) => {
		if (!screenSleepOptions.some((option) => option.seconds === seconds)) return;
		const lifecycle = deviceLifecycle(clientId);
		if (!confirmedScreenSleepsRef.current.has(clientId)) {
			setDevices((current) => {
				const device = current.find((item) => item.clientId === clientId);
				if (device) confirmedScreenSleepsRef.current.set(clientId, device.screenSleepSeconds ?? 60);
				return current;
			});
		}
		setDevices((current) => current.map((device) => device.clientId === clientId ? { ...device, screenSleepSeconds: seconds } : device));
		optimisticScreenSleepsRef.current.set(clientId, seconds);
		if (screenSleepSendsInFlightRef.current.has(clientId)) screenSleepFlushPendingRef.current.add(clientId);
		void sendDeviceScreenSleepTimeout(clientId, seconds, lifecycle);
	}, [sendDeviceScreenSleepTimeout]);

	const discardPendingDeviceScreenSleep = useCallback((clientId: string) => {
		pendingScreenSleepsRef.current.delete(clientId);
		optimisticScreenSleepsRef.current.delete(clientId);
		screenSleepFlushPendingRef.current.delete(clientId);
		confirmedScreenSleepsRef.current.delete(clientId);
	}, []);

	const saveDeviceAgentBinding = (device: HardwareDevice, next: HardwareAgentBindingValue) => {
		const lifecycle = deviceLifecycle(device.clientId);
		if (next.assistantMode === 'expert' && !next.expertId) {
			showToastMessage(isZh ? '请先选择 AI 专家。' : 'Select an AI expert first.');
			return;
		}
		const request = (agentBindingRequestRef.current.get(device.clientId) ?? 0) + 1;
		agentBindingRequestRef.current.set(device.clientId, request);
		optimisticAgentBindingsRef.current.set(device.clientId, { value: next, request });
		setDevices((current) => current.map((item) => item.clientId === device.clientId ? { ...item, ...next } : item));
		// Wails accepts concurrent calls, but the backend persists each whole
		// binding. Serialize saves for one device so a delayed older selection
		// cannot overwrite a later expert/voice choice on disk.
		const previousSave = agentBindingSaveTailsRef.current.get(device.clientId) ?? Promise.resolve();
		const save = previousSave.catch(() => undefined).then(async () => {
			if (deletedClientIdsRef.current.has(device.clientId) || !isCurrentDeviceLifecycle(device.clientId, lifecycle)) return;
			try {
				await SetHardwareAgentBinding(device.clientId, {
					assistant_mode: next.assistantMode,
					expert_id: next.assistantMode === 'expert' ? next.expertId : '',
					tts_voice_id: next.ttsVoiceId,
				});
				if (!deletedClientIdsRef.current.has(device.clientId) && isCurrentDeviceLifecycle(device.clientId, lifecycle)) {
					confirmedAgentBindingsRef.current.set(device.clientId, next);
				}
			} catch (err: any) {
				if (agentBindingRequestRef.current.get(device.clientId) !== request) return;
				optimisticAgentBindingsRef.current.delete(device.clientId);
				if (mountedRef.current && isCurrentDeviceLifecycle(device.clientId, lifecycle)) {
					const confirmed = confirmedAgentBindingsRef.current.get(device.clientId) ?? hardwareAgentBindingValue(device);
					setDevices((current) => current.map((item) => item.clientId === device.clientId ? { ...item, ...confirmed } : item));
					showToastMessage(err?.message || String(err));
				}
			}
		});
		const tail = save.catch(() => undefined);
		agentBindingSaveTailsRef.current.set(device.clientId, tail);
		void tail.finally(() => {
			if (agentBindingSaveTailsRef.current.get(device.clientId) === tail) {
				agentBindingSaveTailsRef.current.delete(device.clientId);
			}
		});
		return save;
	};

		const setDevicePet = async (device: HardwareDevice, skin: string) => {
			const lifecycle = deviceLifecycle(device.clientId);
			const previous = device.petSkin || String((config as any)?.pet_skin || 'clawmate');
		setDevices((current) => current.map((item) => item.clientId === device.clientId ? { ...item, petSkin: skin } : item));
		setSavingPetClientIds((current) => new Set(current).add(device.clientId));
		try {
			await SendHardwareDevicePetProfile(device.clientId, skin);
				if (deletedClientIdsRef.current.has(device.clientId) || !isCurrentDeviceLifecycle(device.clientId, lifecycle)) return;
			showToastMessage(isZh ? `${device.clientName || device.clientId} 的宠物已更新。` : `Pet updated for ${device.clientName || device.clientId}.`);
		} catch (err: any) {
				if (deletedClientIdsRef.current.has(device.clientId) || !isCurrentDeviceLifecycle(device.clientId, lifecycle)) return;
			setDevices((current) => current.map((item) => item.clientId === device.clientId ? { ...item, petSkin: previous } : item));
			showToastMessage(err?.message || String(err));
		} finally {
			if (mountedRef.current && isCurrentDeviceLifecycle(device.clientId, lifecycle)) setSavingPetClientIds((current) => {
				const next = new Set(current);
				next.delete(device.clientId);
				return next;
			});
		}
	};

	const beginDeviceNameEdit = (device: HardwareDevice) => {
		setDeviceNameDraft(device.clientName || device.clientId);
		setEditingDeviceNameClientId(device.clientId);
	};

		const saveDeviceName = async (device: HardwareDevice) => {
			const lifecycle = deviceLifecycle(device.clientId);
		const alias = deviceNameDraft.trim();
		if (!alias) {
			showToastMessage(isZh ? '硬件名称不能为空。' : 'Hardware name cannot be empty.');
			return;
		}
		if ([...alias].length > 48) {
			showToastMessage(isZh ? '硬件名称不能超过 48 个字符。' : 'Hardware name must be at most 48 characters.');
			return;
		}
		const normalizedAlias = alias.toLocaleLowerCase();
		const duplicate = devices.some((item) => item.clientId !== device.clientId && (item.clientName || item.clientId).trim().toLocaleLowerCase() === normalizedAlias);
		if (duplicate) {
			showToastMessage(isZh ? '硬件名称不能重复。' : 'Hardware names must be unique.');
			return;
		}
		setSavingDeviceNameClientIds((current) => new Set(current).add(device.clientId));
		try {
			await SetThirdPartyHardwareDeviceAlias(device.clientId, alias);
				if (!mountedRef.current || deletedClientIdsRef.current.has(device.clientId) || !isCurrentDeviceLifecycle(device.clientId, lifecycle)) return;
			setDevices((current) => current.map((item) => item.clientId === device.clientId ? { ...item, clientName: alias } : item));
			setEditingDeviceNameClientId(null);
			showToastMessage(isZh ? '硬件名称已保存，仅在本机显示。' : 'Hardware name saved locally.');
		} catch (err: any) {
				if (deletedClientIdsRef.current.has(device.clientId) || !isCurrentDeviceLifecycle(device.clientId, lifecycle)) return;
			showToastMessage(err?.message || String(err));
		} finally {
			if (mountedRef.current && isCurrentDeviceLifecycle(device.clientId, lifecycle)) setSavingDeviceNameClientIds((current) => {
				const next = new Set(current);
				next.delete(device.clientId);
				return next;
			});
		}
	};

	const changeAllowCustomHardwarePets = async (enabled: boolean) => {
		setBusy(true);
		try {
			await SetHardwareAllowCustomPets(enabled);
			await refreshConfig();
			if (!enabled) await refreshDevicesRef.current();
		} catch (err: any) {
			showToastMessage(err?.message || String(err));
		} finally {
			if (mountedRef.current) setBusy(false);
		}
	};

    const runWelcomeAction = async (action: WelcomeAction, operation: () => Promise<any>) => {
        setBusy(true);
        setWelcomeAction(action);
        try {
            await operation();
        } finally {
            if (mountedRef.current) {
                setWelcomeAction(null);
                setBusy(false);
            }
        }
    };

		const previewRemoteHardware = async (device: HardwareDevice) => {
			if (!device.online) return;
			const lifecycle = deviceLifecycle(device.clientId);
		setPreviewingClientIds((current) => new Set(current).add(device.clientId));
		try {
			await SendHardwareWelcomeAudioRemote(device.clientId);
				if (deletedClientIdsRef.current.has(device.clientId) || !isCurrentDeviceLifecycle(device.clientId, lifecycle)) return;
			showToastMessage(isZh ? `${device.clientName || device.clientId} 已确认播放完成。` : `${device.clientName || device.clientId} confirmed playback.`);
		} catch (err: any) {
				if (deletedClientIdsRef.current.has(device.clientId) || !isCurrentDeviceLifecycle(device.clientId, lifecycle)) return;
			showToastMessage(welcomePreviewErrorMessage(err, isZh));
		} finally {
			if (mountedRef.current && isCurrentDeviceLifecycle(device.clientId, lifecycle)) setPreviewingClientIds((current) => {
				const next = new Set(current);
				next.delete(device.clientId);
				return next;
			});
		}
	};

    const setWelcomeEnabled = async (next: boolean) => {
        setBusy(true);
        try {
            await saveRemoteConfigField({ hardware_welcome_enabled: next });
        } catch {
            // The shared save helper restores confirmed config and reports the error.
        } finally {
            if (mountedRef.current) setBusy(false);
        }
    };

    const resetWelcomeAudio = () => void runWelcomeAction('reset', async () => {
        try {
            await ResetHardwareWelcomeAudio();
            await refreshConfig();
            await refreshLocalWelcomePreviewSource(true).catch(() => undefined);
            showToastMessage(isZh ? '已恢复默认 Welcome 录音。' : 'Default Welcome recording restored.');
        } catch (err: any) {
            showToastMessage(err?.message || String(err));
        }
    });

    const changeWelcomeVoice = async (nextVoiceID: string) => {
        const previousVoiceID = welcomeVoiceID;
        setWelcomeVoiceID(nextVoiceID);
        setConfig((current: any) => current ? { ...current, hardware_welcome_voice_id: nextVoiceID } : current);
        setWelcomeVoiceSaving(true);
        try {
            await saveRemoteConfigField({ hardware_welcome_voice_id: nextVoiceID });
        } catch {
            // The shared helper reports the failure. Restore this component's
            // local state as well so the selector matches confirmed config.
            if (mountedRef.current) setWelcomeVoiceID(previousVoiceID);
        } finally {
            if (mountedRef.current) setWelcomeVoiceSaving(false);
        }
    };

    const changeHardwareEnabled = async (next: boolean) => {
        setBusy(true);
		try {
			const status = await SetHardwareEnabled(next);
			setThirdPartyGatewayStatus(status);
            await refreshConfig();
            showToastMessage(next
                ? (isZh ? '硬件已启用，系统将通过 Hub 管理硬件连接。' : 'Hardware enabled. The system now manages hardware connections through Hub.')
                : (isZh ? '硬件已停用。第三方接入保持当前状态。' : 'Hardware disabled. Third-party access remains unchanged.'));
        } catch (err: any) {
            await refreshConfig().catch(() => undefined);
            showToastMessage(err?.message || String(err));
        } finally {
            if (mountedRef.current) setBusy(false);
        }
    };

    const changeGatewayEnabled = async (next: boolean) => {
        setGatewayBusy(true);
        try {
            const patch: any = { thirdparty_gateway_enabled: next };
            if (next && !String((config as any)?.thirdparty_gateway_token || '').trim()) {
                const bytes = new Uint8Array(32);
                window.crypto.getRandomValues(bytes);
                patch.thirdparty_gateway_token = Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
            }
            await saveRemoteConfigField(patch);
            if (next) {
                setThirdPartyGatewayStatus(await RestartThirdPartyGateway());
            } else {
                await StopThirdPartyGateway();
                setThirdPartyGatewayStatus('disconnected');
            }
        } catch (err: any) {
            await refreshConfig().catch(() => undefined);
            showToastMessage(err?.message || String(err));
        } finally {
            if (mountedRef.current) setGatewayBusy(false);
        }
    };

    const changeGatewayMode = async (nextLocalMode: boolean) => {
        const previous = thirdPartyGatewayLocalMode;
        setGatewayBusy(true);
        setThirdPartyGatewayLocalModeState(nextLocalMode);
        try {
            await SetThirdPartyGatewayLocalMode(nextLocalMode);
            await refreshConfig();
        } catch (err: any) {
            setThirdPartyGatewayLocalModeState(previous);
            await refreshConfig().catch(() => undefined);
            showToastMessage(err?.message || String(err));
        } finally {
            if (mountedRef.current) setGatewayBusy(false);
        }
    };

    const restartGateway = async () => {
        setGatewayBusy(true);
        try {
            setThirdPartyGatewayStatus(await RestartThirdPartyGateway());
        } catch (err: any) {
            showToastMessage(err?.message || String(err));
        } finally {
            if (mountedRef.current) setGatewayBusy(false);
        }
    };

    const generateToken = async () => {
        const bytes = new Uint8Array(32);
        window.crypto.getRandomValues(bytes);
        await saveRemoteConfigField({
            thirdparty_gateway_token: Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join(''),
        });
        showToastMessage(isZh ? '已生成 Token' : 'Token generated');
    };

    const saveAmbientCity = useCallback(async (city: string, version: number) => {
        if (ambientQueuedVersionRef.current === version && ambientQueuedPromiseRef.current) {
            return ambientQueuedPromiseRef.current;
        }
        // PatchConfigFields updates the shared config optimistically. Serialize
        // writes so a slow older request can never overwrite a newer city.
        const save = ambientSaveTailRef.current
            .catch(() => undefined)
            .then(() => Promise.resolve(saveRemoteConfigFieldRef.current({ pet_ambient_city: city })))
            .then(() => undefined);
        ambientSaveTailRef.current = save.catch(() => undefined);
        ambientQueuedVersionRef.current = version;
        ambientQueuedPromiseRef.current = save;
        try {
            await save;
            if (ambientSaveVersionRef.current === version && pendingAmbientCityRef.current === city) {
                pendingAmbientCityRef.current = null;
                confirmedAmbientCityRef.current = city.trim();
                if (mountedRef.current) setAmbientCityDraft(city.trim());
            }
        } catch (error) {
            if (ambientSaveVersionRef.current === version && pendingAmbientCityRef.current === city) {
                pendingAmbientCityRef.current = null;
                if (mountedRef.current) {
                    setAmbientCityDraft(confirmedAmbientCityRef.current);
                    setDetectedAmbientCity(null);
                    setAmbientRefreshState('error');
                }
            }
            throw error;
        } finally {
            if (ambientQueuedPromiseRef.current === save) {
                ambientQueuedPromiseRef.current = null;
                ambientQueuedVersionRef.current = 0;
            }
        }
    }, []);

    const changeAmbientCity = (city: string) => {
        ambientRefreshRequestRef.current += 1;
        setAmbientRefreshState('idle');
        setDetectedAmbientCity(null);
        setAmbientCityDraft(city);
        pendingAmbientCityRef.current = city;
        const version = ++ambientSaveVersionRef.current;
        // Keep keystrokes local. Updating the shared App config here caused the
        // entire application shell to render once per character; the debounced
        // atomic save below already performs the confirmed shared update.
        if (ambientSaveTimerRef.current !== null) clearTimeout(ambientSaveTimerRef.current);
        ambientSaveTimerRef.current = setTimeout(() => {
            ambientSaveTimerRef.current = null;
            void saveAmbientCity(city, version).catch(() => undefined);
        }, 350);
    };

    const refreshAmbientWeather = async () => {
        const requestID = ++ambientRefreshRequestRef.current;
        setDetectedAmbientCity(null);
        setAmbientRefreshState('refreshing');
        try {
            if (ambientSaveTimerRef.current !== null) {
                clearTimeout(ambientSaveTimerRef.current);
                ambientSaveTimerRef.current = null;
            }
            if (pendingAmbientCityRef.current !== null) {
                await saveAmbientCity(pendingAmbientCityRef.current, ambientSaveVersionRef.current);
                if (!mountedRef.current || requestID !== ambientRefreshRequestRef.current) return;
            }
            const resolvedCity = String(await RefreshDeviceAmbientWeather() || '').trim();
            if (!mountedRef.current || requestID !== ambientRefreshRequestRef.current) return;
            if (!ambientCityDraft.trim()) setDetectedAmbientCity(resolvedCity || null);
            setAmbientRefreshState('done');
        } catch (error) {
            if (!mountedRef.current || requestID !== ambientRefreshRequestRef.current) return;
            console.error('[ThirdPartyAccessSettings] RefreshDeviceAmbientWeather failed:', error);
            setAmbientRefreshState('error');
        }
    };

    return <section className="im-settings-card im-settings-channel">
        {!hardwareOnly && <>
        <p className="im-settings-description">
            {isZh ? '开放本机 HTTP 消息接入端口，第三方软件主动连接 MaClaw，无需提供回调地址。' : 'Expose a local HTTP message gateway. Third-party software connects to MaClaw without a callback URL.'}
        </p>
        <div className="im-settings-toolbar">
            <label className="im-settings-toggle">
                <input
                    type="checkbox"
                    disabled={settingsBusy}
                    aria-label={textForLang(lang, 'Enable third-party access', '开启第三方软件接入', '開啟第三方軟體接入')}
                    checked={imGatewayEnabled}
                    onChange={(e) => void changeGatewayEnabled(e.target.checked)}
                />
                <span>{textForLang(lang, 'Enable third-party access', '开启第三方软件接入', '開啟第三方軟體接入')}</span>
            </label>
            <span className="im-settings-status" data-status={thirdPartyGatewayStatus} aria-live="polite">
                {gatewayStatusLabel(thirdPartyGatewayStatus, lang)}
            </span>
            <button type="button" className="im-settings-button" disabled={!imGatewayEnabled || settingsBusy} onClick={() => void restartGateway()}>
                {gatewayBusy ? (isZh ? '处理中…' : 'Working…') : textForLang(lang, 'Restart', '重启接口', '重啟介面')}
            </button>
            <button type="button" className="im-settings-button im-settings-button--audit" onClick={() => setIMAuditPlatform('thirdparty')}>
                {watchLabel(lang)}
            </button>
        </div>

        <div className="im-settings-mode-row">
            <span>{channelModeLabel(lang)}</span>
            <div className="im-settings-segmented">
                {[
                    { value: true, label: isZh ? '单机' : 'Local', desc: isZh ? '本机 Agent 直接处理' : 'Handle with local Agent' },
                    { value: false, label: isZh ? '多机' : 'Hub', desc: isZh ? '通过 Hub 转发到在线设备' : 'Forward through Hub' },
                ].map((option) => <button
                    key={String(option.value)}
                    type="button"
                    aria-label={option.desc}
                    title={option.desc}
                    disabled={settingsBusy}
                    data-active={thirdPartyGatewayLocalMode === option.value}
                    onClick={() => void changeGatewayMode(option.value)}
                >{option.label}</button>)}
            </div>
        </div>

        <div className="im-settings-grid im-settings-grid--gateway">
            <label className="im-settings-field">
                <span>Host</span>
                <input type="text" disabled={settingsBusy} value={(config as any)?.thirdparty_gateway_host || '127.0.0.1'} onChange={(e) => saveRemoteConfigField({ thirdparty_gateway_host: e.target.value })} placeholder="127.0.0.1" spellCheck={false} />
            </label>
            <label className="im-settings-field im-settings-field--port">
                <span>Port</span>
                <input type="number" disabled={settingsBusy} min={1} max={65535} value={(config as any)?.thirdparty_gateway_port || 18777} onChange={(e) => saveRemoteConfigField({ thirdparty_gateway_port: Number(e.target.value || 18777) })} />
            </label>
            <label className="im-settings-field im-settings-field--token">
                <span>Token</span>
                <span className="im-settings-token-row">
                    <input type="password" disabled={settingsBusy} value={(config as any)?.thirdparty_gateway_token || ''} onChange={(e) => saveRemoteConfigField({ thirdparty_gateway_token: e.target.value })} placeholder="Bearer token" autoComplete="off" />
                    <button type="button" className="im-settings-button im-settings-button--primary" disabled={settingsBusy} onClick={() => void generateToken()}>
                        {isZh ? '生成 Token' : 'Generate Token'}
                    </button>
                </span>
            </label>
        </div>

        <div className="im-settings-endpoint-row">
            <code>{`http://${(config as any)?.thirdparty_gateway_host || '127.0.0.1'}:${(config as any)?.thirdparty_gateway_port || 18777}/api/im-gateway/v1`}</code>
            <button type="button" className="im-settings-button im-settings-button--primary" onClick={() => {
                const base = String((config as any)?.remote_hub_url || '').replace(/\/+$/, '');
                BrowserOpenURL(base ? base + '/connector' : '/connector');
            }}>{textForLang(lang, 'Open docs', '打开接入文档', '開啟接入文件')}</button>
        </div>
        </>}

		{hardwareOnly && <div className="im-settings-hardware" aria-label={isZh ? '硬件配置' : 'Hardware configuration'} aria-busy={settingsBusy}>
            <div className="im-settings-hardware__heading">
                <div>
                    <strong>{isZh ? '硬件配置' : 'Hardware configuration'}</strong>
                    <span>{isZh ? '玛卡龙 ESP32 的配对、欢迎音频与扬声器设置。' : 'Pairing, welcome audio, and speaker settings for Macaron ESP32.'}</span>
                </div>
                <label className="im-settings-toggle">
                    <input type="checkbox" aria-label={isZh ? '启用硬件' : 'Enable hardware'} disabled={settingsBusy} checked={hardwareEnabled} onChange={(e) => void changeHardwareEnabled(e.target.checked)} />
                    <span>{isZh ? '启用硬件' : 'Enable hardware'}</span>
                </label>
            </div>
            <p className="im-settings-hardware__mode-note">
                {isZh ? '硬件连接由系统通过 Hub 管理，不会修改第三方接入的模式或 Token。' : 'Hardware connections are system-managed through Hub and do not change the IM gateway mode or token.'}
            </p>
			<label className="im-settings-toggle im-settings-hardware__custom-pets">
				<input
					type="checkbox"
					aria-label={isZh ? '允许个性宠物' : 'Allow individual pets'}
					disabled={settingsBusy}
					checked={allowCustomHardwarePets}
					onChange={(event) => void changeAllowCustomHardwarePets(event.target.checked)}
				/>
				<span>{isZh ? '允许个性宠物' : 'Allow individual pets'}</span>
				<small>{isZh ? '开启后可为每台硬件单独选择宠物；关闭时全部跟随系统宠物设置。' : 'When enabled, each device can use its own pet. Otherwise all devices follow the system pet setting.'}</small>
			</label>

            <div className="im-settings-hardware__weather">
                <div>
                    <label htmlFor="hardware-weather-city">{textForLang(lang, 'Hardware weather city', '硬件天气城市', '硬體天氣城市')}</label>
                    <small>{textForLang(lang, 'Leave blank to detect from the desktop network', '留空时按电脑当前网络位置自动识别', '留空時按電腦目前網路位置自動辨識')}</small>
                </div>
                <div className="im-settings-hardware__weather-controls">
                    <input
                        id="hardware-weather-city"
                        type="text"
                        value={ambientCityDraft !== '' ? ambientCityDraft : (detectedAmbientCity || '')}
                        disabled={ambientRefreshState === 'refreshing'}
                        maxLength={32}
                        placeholder={textForLang(lang, 'e.g. Shanghai', '例如：上海', '例如：上海')}
                        onChange={(event) => changeAmbientCity(event.target.value)}
                    />
                    <button type="button" className="im-settings-button" disabled={ambientRefreshState === 'refreshing'} onClick={() => void refreshAmbientWeather()}>
                        {ambientRefreshState === 'refreshing'
                            ? textForLang(lang, 'Checking…', '查询中…', '查詢中…')
                            : textForLang(lang, 'Sync now', '立即同步', '立即同步')}
                    </button>
                </div>
                {ambientRefreshState === 'done' && <small className="im-settings-hardware__weather-status" role="status">{textForLang(lang, 'Weather sent to hardware', '天气已推送到硬件', '天氣已推送到硬體')}</small>}
                {ambientRefreshState === 'error' && <small className="im-settings-hardware__weather-error" role="alert">{textForLang(lang, 'Weather sync failed; check Hub and network', '天气同步失败，请检查 Hub 与网络连接', '天氣同步失敗，請檢查 Hub 與網路連線')}</small>}
            </div>

            <div className="im-settings-hardware__pairing">
                <div>
                    <span>{isZh ? '配对码' : 'Pairing code'}</span>
                    {pairing?.pairCode
                        ? <strong className="im-settings-pair-code">{pairing.pairCode}</strong>
                        : <small>{isZh ? '点击获取后，在硬件配置页输入。' : 'Get a code, then enter it in the hardware setup portal.'}</small>}
                </div>
                <div className="im-settings-hardware__actions">
                    <button type="button" className="im-settings-button im-settings-button--primary" disabled={hardwareControlsDisabled || hardwareBindingAtCapacity || pairingBusy} onClick={async () => {
						const requestID = ++pairingRequestRef.current;
						setPairingBusy(true);
                        try {
							const nextPairing = await CreateThirdPartyDevicePairing() as HardwarePairing;
							if (!mountedRef.current || requestID !== pairingRequestRef.current) return;
							setPairing(nextPairing);
							showToastMessage(isZh ? '已生成配对码，有效期 30 分钟。' : 'Pairing code generated; valid for 30 minutes.');
                        } catch (err: any) {
							if (!mountedRef.current || requestID !== pairingRequestRef.current) return;
                            showToastMessage(err?.message || String(err));
						} finally {
							if (mountedRef.current && requestID === pairingRequestRef.current) setPairingBusy(false);
                        }
					}}>{pairingBusy
						? (isZh ? '生成中…' : 'Generating…')
						: (pairing?.pairCode ? (isZh ? '重新生成' : 'Regenerate') : (isZh ? '获取配对码' : 'Get code'))}</button>
					{pairing?.gatewayURL && <code>{pairing.gatewayURL}</code>}
                </div>
            </div>

            <div className="im-settings-hardware__devices" aria-label={isZh ? '接入硬件列表' : 'Connected hardware list'}>
                <div className="im-settings-hardware__devices-heading">
                    <div>
                        <strong>{isZh ? '接入硬件' : 'Connected hardware'}</strong>
                        <span>{isZh ? '每台 ESP32 使用独立身份与 Token。解绑后设备须重新配对。' : 'Each ESP32 has its own identity and token. Removed devices must pair again.'}</span>
                    </div>
					<span className="im-settings-hardware__device-limit" data-at-capacity={hardwareBindingAtCapacity}>
						{isZh ? '最多绑定' : 'Up to'} <strong>{boundDeviceCount} / {hardwareBindingLimit}</strong>
						{isZh
							? (hardwareBindingAtCapacity ? ' 台；已满额，请先解绑再绑定新设备。' : ' 台。')
							: (hardwareBindingAtCapacity ? '; limit reached — remove a device before binding a new one.' : ' devices.')}
					</span>
                    <button type="button" className="im-settings-button" disabled={devicesLoading || hardwareControlsDisabled} onClick={() => void refreshDevices()}>
                        {devicesLoading ? (isZh ? '刷新中…' : 'Refreshing…') : (isZh ? '刷新' : 'Refresh')}
                    </button>
                </div>
                {devicesError && devices.length > 0 && <div className="im-settings-hardware__devices-error" role="status">
                    <span>{isZh ? '刷新失败，当前显示上次成功读取的列表。' : 'Refresh failed. Showing the last successfully loaded list.'}</span>
                    <button type="button" className="im-settings-button" onClick={() => void refreshDevices()}>{isZh ? '重试' : 'Retry'}</button>
                </div>}
                {devicesLoading && devices.length === 0
                    ? <div className="im-settings-hardware__devices-loading" aria-live="polite">{isZh ? '正在读取硬件…' : 'Loading hardware…'}</div>
                    : devicesError && devices.length === 0
                        ? <div className="im-settings-hardware__devices-error" role="alert">
                            <span>{isZh ? '无法读取硬件列表。请检查 Hub 连接后重试。' : 'Could not load hardware. Check the Hub connection and try again.'}</span>
                            <button type="button" className="im-settings-button" onClick={() => void refreshDevices()}>{isZh ? '重试' : 'Retry'}</button>
                        </div>
                        : devices.length === 0
                            ? <div className="im-settings-hardware__devices-empty">{isZh ? '暂无已绑定硬件。生成配对码并完成一次配对后，设备会显示在这里。' : 'No hardware is bound yet. Generate a pairing code and complete pairing to see the device here.'}</div>
                            : <div className="im-settings-hardware__device-list">{devices.map((device) => {
                                const name = device.clientName || device.clientId;
                                const parsedLastSeen = device.lastSeenAt ? new Date(device.lastSeenAt) : null;
                                const lastSeen = parsedLastSeen && !Number.isNaN(parsedLastSeen.getTime()) ? parsedLastSeen.toLocaleString() : '';
								const deviceVolume = Math.max(0, Math.min(100, Math.round(Number(device.volume ?? defaultVolume))));
								const deviceBrightness = Math.max(0, Math.min(100, Math.round(Number(device.brightness ?? defaultBrightness))));
								const deviceScreenSleepSeconds = screenSleepOptions.some((option) => option.seconds === device.screenSleepSeconds) ? device.screenSleepSeconds! : 60;
								const devicePetSkin = device.petSkin || String((config as any)?.pet_skin || 'clawmate');
								const devicePet = getPetSkinOption(devicePetSkin, petOptions);
								const availablePetOptions = petOptions.some((pet) => pet.id === devicePetSkin) ? petOptions : [devicePet, ...petOptions];
								const assistantMode = device.assistantMode === 'expert' ? 'expert' : 'general';
								const selectedExpertID = device.expertId || '';
								// An expert can be deleted after a hardware device was bound to
								// it. Keep that persisted selection visible instead of letting
								// the native select render blank; the user can then deliberately
								// fall back to AI assistant or pick a replacement expert.
								const selectedExpertUnavailable = assistantMode === 'expert'
									&& selectedExpertID !== ''
									&& !experts.some((expert) => expert.id === selectedExpertID);
								const replyVoiceID = device.ttsVoiceId || 'zf_xiaoxiao';
                                return <div className="im-settings-hardware__device" key={device.clientId}>
                                    <span className="im-settings-hardware__device-status" data-online={Boolean(device.online)} aria-hidden="true" />
									<div className="im-settings-hardware__device-details">
										<div className="im-settings-hardware__device-name">
											{editingDeviceNameClientId === device.clientId ? <input
													aria-label={`${isZh ? '硬件名称' : 'Hardware name'} ${device.clientId}`}
													value={deviceNameDraft}
													maxLength={48}
													disabled={savingDeviceNameClientIds.has(device.clientId)}
													onChange={(event) => setDeviceNameDraft(event.target.value)}
													onKeyDown={(event) => {
													if (event.key === 'Enter') { event.preventDefault(); void saveDeviceName(device); }
													if (event.key === 'Escape') setEditingDeviceNameClientId(null);
												}}
												autoFocus
												/> : <strong title={name}>{name}</strong>}
											<button
												type="button"
												className="im-settings-hardware__device-rename"
												aria-label={`${editingDeviceNameClientId === device.clientId ? (isZh ? '保存名称' : 'Save name') : (isZh ? '更名' : 'Rename')} ${name}`}
												disabled={hardwareControlsDisabled || deletingClientIds.has(device.clientId) || savingDeviceNameClientIds.has(device.clientId)}
												onClick={() => editingDeviceNameClientId === device.clientId ? void saveDeviceName(device) : beginDeviceNameEdit(device)}
											>{editingDeviceNameClientId === device.clientId ? (isZh ? '保存' : 'Save') : (isZh ? '更名' : 'Rename')}</button>
										</div>
                                        <code title={device.clientId}>{device.clientId}</code>
                                        <small>
                                            {device.online ? (isZh ? '在线' : 'Online') : (lastSeen ? `${isZh ? '最后连接' : 'Last seen'} ${lastSeen}` : (isZh ? '尚未连接' : 'Not seen yet'))}
                                            {device.protocolVersion ? ` · v${device.protocolVersion}` : ''}
                                        </small>
                                    </div>
									<div className="im-settings-hardware__device-actions">
										<div className="im-settings-hardware__device-adjustments">
										<div className="im-settings-hardware__device-screen-sleep">
											<label htmlFor={`hardware-screen-sleep-${device.clientId}`}>{isZh ? '休眠时间' : 'Screen sleep'}</label>
											<select
												id={`hardware-screen-sleep-${device.clientId}`}
												aria-label={`${isZh ? '休眠时间' : 'Screen sleep'} ${name}`}
												title={isZh ? '无操作后关闭屏幕。' : 'Turns the screen off after inactivity.'}
												value={deviceScreenSleepSeconds}
												disabled={hardwareControlsDisabled || deletingClientIds.has(device.clientId)}
												onChange={(event) => changeDeviceScreenSleepTimeout(device.clientId, Number(event.target.value))}
											>
												{screenSleepOptions.map((option) => <option key={option.seconds} value={option.seconds}>{isZh ? option.zh : option.en}</option>)}
											</select>
										</div>
										<div className="im-settings-hardware__device-brightness">
										<label htmlFor={`hardware-brightness-${device.clientId}`}><span>{isZh ? '亮度' : 'Brightness'}</span> <strong>{deviceBrightness}%</strong></label>
											<input id={`hardware-brightness-${device.clientId}`} aria-label={`${isZh ? '亮度' : 'Brightness'} ${name}`} title={device.online ? (isZh ? '调整仅应用于此设备。' : 'Changes apply only to this device.') : (isZh ? '设备离线；设置会在下次连接时生效。' : 'Offline; applied when this device reconnects.')} type="range" min={0} max={100} step={1} value={deviceBrightness} disabled={hardwareControlsDisabled || deletingClientIds.has(device.clientId)} onPointerDown={() => rememberDeviceBrightnessBeforeEdit(device.clientId)} onKeyDown={(event) => {
											if (['ArrowLeft', 'ArrowRight', 'Home', 'End', 'PageUp', 'PageDown'].includes(event.key)) rememberDeviceBrightnessBeforeEdit(device.clientId);
										}} onChange={(event) => {
											const next = Number(event.target.value);
											setDevices((current) => current.map((item) => item.clientId === device.clientId ? { ...item, brightness: next } : item));
											scheduleDeviceBrightness(device.clientId, next);
										}} onPointerUp={(event) => scheduleDeviceBrightness(device.clientId, Number((event.target as HTMLInputElement).value), true)} onKeyUp={(event) => {
											if (['ArrowLeft', 'ArrowRight', 'Home', 'End', 'PageUp', 'PageDown'].includes(event.key)) scheduleDeviceBrightness(device.clientId, Number((event.target as HTMLInputElement).value), true);
										}} />
										</div>
										<div className="im-settings-hardware__device-volume">
										<label htmlFor={`hardware-volume-${device.clientId}`}><span>{isZh ? '音量' : 'Volume'}</span> <strong>{deviceVolume}%</strong></label>
											<input id={`hardware-volume-${device.clientId}`} aria-label={`${isZh ? '音量' : 'Volume'} ${name}`} title={device.online ? (isZh ? '调整仅应用于此设备。' : 'Changes apply only to this device.') : (isZh ? '设备离线；设置会在下次连接时生效。' : 'Offline; applied when this device reconnects.')} type="range" min={0} max={100} step={1} value={deviceVolume} disabled={hardwareControlsDisabled || deletingClientIds.has(device.clientId)} onPointerDown={() => rememberDeviceVolumeBeforeEdit(device.clientId)} onKeyDown={(event) => {
											if (['ArrowLeft', 'ArrowRight', 'Home', 'End', 'PageUp', 'PageDown'].includes(event.key)) rememberDeviceVolumeBeforeEdit(device.clientId);
										}} onChange={(event) => {
											const next = Number(event.target.value);
											setDevices((current) => current.map((item) => item.clientId === device.clientId ? { ...item, volume: next } : item));
											scheduleDeviceVolume(device.clientId, next);
										}} onPointerUp={(event) => scheduleDeviceVolume(device.clientId, Number((event.target as HTMLInputElement).value), true)} onKeyUp={(event) => {
											if (['ArrowLeft', 'ArrowRight', 'Home', 'End', 'PageUp', 'PageDown'].includes(event.key)) scheduleDeviceVolume(device.clientId, Number((event.target as HTMLInputElement).value), true);
										}} />
										</div>
										<div className="im-settings-hardware__device-agent">
											<label htmlFor={`hardware-agent-${device.clientId}`}>{isZh ? 'AI 专家' : 'AI assistant'}</label>
											<select
												id={`hardware-agent-${device.clientId}`}
												aria-label={`${isZh ? 'AI 专家' : 'AI assistant'} ${name}`}
												value={assistantMode === 'expert' ? selectedExpertID : '__general__'}
												disabled={hardwareControlsDisabled || deletingClientIds.has(device.clientId)}
												onChange={(event) => {
													const selected = event.target.value;
													void saveDeviceAgentBinding(device, {
														assistantMode: selected === '__general__' ? 'general' : 'expert',
														expertId: selected === '__general__' ? '' : selected,
														ttsVoiceId: replyVoiceID,
													});
												}}
									>
										<option value="__general__">{isZh ? 'AI 助手（默认）' : 'AI assistant (default)'}</option>
										{selectedExpertUnavailable && <option value={selectedExpertID} disabled>{isZh ? `已不可用：${selectedExpertID}` : `Unavailable: ${selectedExpertID}`}</option>}
										{experts.map((expert) => <option key={expert.id} value={expert.id}>{expert.icon ? `${expert.icon} ` : ''}{expert.name || expert.id}</option>)}
									</select>
										</div>
										{allowCustomHardwarePets && <div className="im-settings-hardware__device-pet" title={devicePet.label} ref={openPetPickerClientId === device.clientId ? petPickerRootRef : undefined}>
											<span className="im-settings-hardware__device-pet-preview"><img src={devicePet.image} alt="" /></span>
											<span>{isZh ? '宠物' : 'Pet'}</span>
											<button
												id={`hardware-pet-trigger-${device.clientId}`}
												type="button"
												className="im-settings-hardware__device-pet-trigger"
												role="combobox"
												aria-label={`${isZh ? '宠物' : 'Pet'} ${name}`}
												aria-controls={`hardware-pet-list-${device.clientId}`}
												aria-expanded={openPetPickerClientId === device.clientId}
												aria-haspopup="listbox"
												disabled={hardwareControlsDisabled || deletingClientIds.has(device.clientId) || savingPetClientIds.has(device.clientId)}
												onClick={() => setOpenPetPickerClientId((current) => current === device.clientId ? null : device.clientId)}
												onKeyDown={(event) => {
													if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
														event.preventDefault();
														setOpenPetPickerClientId(device.clientId);
													}
												}}
											>
												<span>{devicePet.label}</span><span className="im-settings-hardware__device-pet-chevron" aria-hidden="true">⌄</span>
											</button>
											{openPetPickerClientId === device.clientId && <div id={`hardware-pet-list-${device.clientId}`} className="im-settings-hardware__device-pet-menu" role="listbox" aria-label={`${isZh ? '宠物' : 'Pet'} ${name}`}>
												{availablePetOptions.map((pet) => <button
													key={pet.id}
													type="button"
													className="im-settings-hardware__device-pet-option"
													role="option"
													aria-selected={pet.id === devicePetSkin}
													data-testid={`hardware-pet-option-${device.clientId}-${pet.id}`}
													onClick={() => {
														setOpenPetPickerClientId(null);
														if (pet.id !== devicePetSkin) void setDevicePet(device, pet.id);
													}}
												><img src={pet.image} alt="" /><span>{pet.label}</span></button>)}
											</div>}
										</div>}
										<div className="im-settings-hardware__device-agent">
											<label htmlFor={`hardware-reply-voice-${device.clientId}`}>{isZh ? '回复音色' : 'Reply voice'}</label>
											<select
												id={`hardware-reply-voice-${device.clientId}`}
												aria-label={`${isZh ? '回复音色' : 'Reply voice'} ${name}`}
												value={replyVoiceID}
												disabled={hardwareControlsDisabled || deletingClientIds.has(device.clientId)}
												onChange={(event) => void saveDeviceAgentBinding(device, { assistantMode, expertId: selectedExpertID, ttsVoiceId: event.target.value })}
											>
												{ttsVoiceOptions.map((voice) => <option key={voice.id} value={voice.id}>{isZh ? voice.zh : voice.label}</option>)}
											</select>
										</div>
										</div>
										<div className="im-settings-hardware__device-command-actions">
		                                        <button type="button" className="im-settings-button im-settings-button--primary" aria-label={`${isZh ? '远程播放' : 'Play remotely'} ${name}`} disabled={hardwareControlsDisabled || !welcomeAudioPath || !device.online || deletingClientIds.has(device.clientId) || previewingClientIds.has(device.clientId)} title={!device.online ? (isZh ? '设备离线，无法远程播放。' : 'This device is offline.') : undefined} onClick={() => void previewRemoteHardware(device)}>{previewingClientIds.has(device.clientId) ? (isZh ? '播放中…' : 'Playing…') : (isZh ? '远程播放' : 'Play remotely')}</button>
		                                    <button type="button" className="im-settings-button im-settings-button--danger" aria-label={`${isZh ? '解绑' : 'Remove'} ${name}`} disabled={deletingClientIds.has(device.clientId) || savingDeviceNameClientIds.has(device.clientId)} onClick={async () => {
                                        const confirmed = await showConfirm(
                                            isZh ? `解绑后，${name} 的 Token 将立即失效；如需再次使用，必须重新配对。` : `${name}'s token will be revoked immediately. The device must pair again before it can reconnect.`,
                                            isZh ? '解绑硬件？' : 'Remove hardware?',
                                            { confirmText: isZh ? '解绑' : 'Remove', cancelText: isZh ? '取消' : 'Cancel', confirmVariant: 'danger' },
                                        );
	                                        if (!confirmed || !mountedRef.current) return;
	                                        pendingDeletedClientIdsRef.current.add(device.clientId);
	                                        deletedClientIdsRef.current.add(device.clientId);
	                                        deletedAtClientIdsRef.current.set(device.clientId, Date.now());
										invalidateDeviceLifecycle(device.clientId);
										// Detach stale in-flight UI state before a same-ID device can
										// be paired again. The guarded finalizers above cannot then
										// clear state belonging to the replacement lifecycle.
										setPreviewingClientIds((current) => {
											const next = new Set(current);
											next.delete(device.clientId);
											return next;
										});
												setSavingPetClientIds((current) => {
											const next = new Set(current);
											next.delete(device.clientId);
											return next;
												});
										setDeletingClientIds((current) => new Set(current).add(device.clientId));
										setEditingDeviceNameClientId((current) => current === device.clientId ? null : current);
										discardPendingDeviceVolume(device.clientId);
										discardPendingDeviceBrightness(device.clientId);
										discardPendingDeviceScreenSleep(device.clientId);
										optimisticAgentBindingsRef.current.delete(device.clientId);
										confirmedAgentBindingsRef.current.delete(device.clientId);
										agentBindingRequestRef.current.delete(device.clientId);
										agentBindingSaveTailsRef.current.delete(device.clientId);
	                                        try {
                                            await DeleteThirdPartyHardwareDevice(device.clientId);
                                            if (mountedRef.current) {
                                                devicesRequestRef.current += 1;
                                                setDevicesLoading(false);
                                                setDevices((current) => current.filter((item) => item.clientId !== device.clientId));
												setBoundDeviceCount((count) => Math.max(0, count - 1));
                                                showToastMessage(isZh ? '硬件已解绑。' : 'Hardware removed.');
																		void refreshDevicesRef.current();
                                            }
	                                        } catch (err: any) {
										pendingDeletedClientIdsRef.current.delete(device.clientId);
										deletedClientIdsRef.current.delete(device.clientId);
										deletedAtClientIdsRef.current.delete(device.clientId);
										if (mountedRef.current) showToastMessage(welcomePreviewErrorMessage(err, isZh));
                                        } finally {
	                                            if (mountedRef.current) setDeletingClientIds((current) => {
												const next = new Set(current);
												next.delete(device.clientId);
												return next;
											});
	                                        }
		                                    }}>{deletingClientIds.has(device.clientId) ? (isZh ? '解绑中…' : 'Removing…') : (isZh ? '解绑' : 'Remove')}</button>
										</div>
									</div>
                                </div>;
                            })}</div>}
            </div>

            <div className="im-settings-hardware__welcome" aria-busy={welcomeAction !== null}>
                <div className="im-settings-hardware__welcome-title">
                    <strong>{isZh ? 'Welcome 信息' : 'Welcome message'}</strong>
                    <div className="im-settings-hardware__welcome-title-actions">
                        <WelcomeResetButton disabled={settingsBusy} isZh={isZh} resetting={welcomeAction === 'reset'} onReset={resetWelcomeAudio} />
                        <label className="im-settings-toggle">
                            <input type="checkbox" disabled={welcomeEditingDisabled} checked={Boolean((config as any)?.hardware_welcome_enabled)} onChange={(e) => void setWelcomeEnabled(e.target.checked)} />
                            <span>{isZh ? '启用' : 'Enabled'}</span>
                        </label>
                    </div>
                </div>
                <p>{isZh ? '设备每次开机初始化完成后仅播放一次。输入文字生成 WAV，或选择 MP3、WAV、Ogg / Opus 自动转换为硬件可播放的 16 kHz 单声道 WAV。' : 'Plays once after each device boot finishes initializing. Generate WAV from text, or import MP3, WAV, Ogg / Opus and convert it to 16 kHz mono WAV.'}</p>
                <label className="im-settings-field im-settings-hardware__welcome-voice">
                    <span>{isZhHant ? '發音模型' : isZh ? '发音模型' : 'Voice model'}</span>
                    <select
                        aria-label={isZhHant ? '發音模型' : isZh ? '发音模型' : 'Voice model'}
                        disabled={welcomeEditingDisabled || welcomeVoiceSaving}
                        value={welcomeVoiceID}
                        onChange={(event) => void changeWelcomeVoice(event.target.value)}
                    >
                        {ttsVoiceOptions.map((voice) => (
                            <option key={voice.id} value={voice.id}>{isZhHant ? voice.welcomeZhHant : isZh ? voice.welcomeZh : voice.welcomeEn}</option>
                        ))}
                    </select>
                    <small>{isZhHant ? '可選擇內置的全部發音模型；中文歡迎詞建議使用中文音色。選擇後請重新生成音頻。' : isZh ? '可选择内置的全部发音模型；中文欢迎词建议使用中文音色。选择后请重新生成音频。' : 'All bundled voice models are available. For Chinese welcome text, choose a Chinese voice and regenerate the audio.'}</small>
                </label>
                <div className="im-settings-hardware__welcome-controls">
					<div className="im-settings-hardware__welcome-text-field">
						<textarea disabled={welcomeEditingDisabled} value={welcomeText} maxLength={80} onChange={(e) => setWelcomeText(e.target.value)} placeholder={isZh ? '例如：Hello, Maclaw' : 'For example: Hello, Maclaw'} aria-describedby="hardware-welcome-capacity" />
						<small id="hardware-welcome-capacity" className="im-settings-hardware__welcome-text-status">{welcomeTextCapacityHint(welcomeText, isZh)}</small>
					</div>
                    <button type="button" className="im-settings-button" disabled={welcomeEditingDisabled || welcomeVoiceSaving || !welcomeText.trim()} onClick={() => void runWelcomeAction('generate', async () => {
                        try {
                            await GenerateHardwareWelcomeAudio(welcomeText, welcomeVoiceID);
                            await refreshConfig();
                            await refreshLocalWelcomePreviewSource(true).catch(() => undefined);
                            showToastMessage(isZh ? '欢迎音频已生成。' : 'Welcome audio generated.');
                        } catch (err: any) {
                            showToastMessage(welcomePreviewErrorMessage(err, isZh));
                        }
                    })}>{welcomeAction === 'generate' ? (isZh ? '生成中…' : 'Generating…') : (isZh ? '生成音频' : 'Generate audio')}</button>
                    <button type="button" className="im-settings-button" disabled={welcomeEditingDisabled} onClick={() => void runWelcomeAction('import', async () => {
                        try {
                            await SelectHardwareWelcomeAudio();
                            await refreshConfig();
                            await refreshLocalWelcomePreviewSource(true).catch(() => undefined);
                            showToastMessage(isZh ? '欢迎音频已导入。' : 'Welcome audio imported.');
                        } catch (err: any) {
                            showToastMessage(err?.message || String(err));
                        }
                    })}>{welcomeAction === 'import' ? (isZh ? '导入中…' : 'Importing…') : (isZh ? '选择音频文件' : 'Choose audio')}</button>
                    <button type="button" className="im-settings-button im-settings-button--primary" disabled={busy || !welcomeAudioPath} onClick={() => void runWelcomeAction('preview-local', async () => {
                        try {
                            if (await playLocalWelcomePreview()) showToastMessage(isZh ? 'GUI 本地播放完成。' : 'Local GUI playback completed.');
                        } catch (err: any) {
                            showToastMessage(welcomePreviewErrorMessage(err, isZh));
                        }
                    })}>{welcomeAction === 'preview-local' ? (isZh ? 'GUI 播放中…' : 'Playing in GUI…') : (isZh ? 'GUI 本地试听' : 'Preview in GUI')}</button>
	                </div>
                {welcomeAudioPath && <small className="im-settings-hardware__audio-status">{isZh ? '已准备硬件 WAV：' : 'Hardware WAV ready: '}{welcomeAudioPath.split(/[\\/]/).pop()}</small>}
				<small className="im-settings-hardware__welcome-capacity">{isZh ? '硬件容量约 3 秒；接近上限时会自动加快语速，仍超出时请缩短欢迎词。' : 'Hardware capacity is roughly 3 seconds. Near the limit, speech is automatically sped up; shorten the message if it still does not fit.'}</small>
	                <small className="im-settings-hardware__preview-note">{isZh ? 'GUI 本地试听用于检查生成质量；请在上方对应的已绑定硬件旁点击“远程播放”，仅向该 ESP32 下发并等待“播放完成”确认。' : 'GUI preview checks generated audio quality. Use the remote-play button beside a bound device to send only to that ESP32 and wait for its playback receipt.'}</small>
            </div>
        </div>}
    </section>;
};
