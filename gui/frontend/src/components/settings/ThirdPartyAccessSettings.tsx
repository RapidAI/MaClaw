import { Dispatch, SetStateAction, useCallback, useEffect, useRef, useState } from 'react';
import { BrowserOpenURL } from '../../../wailsjs/runtime';
import {
    CreateThirdPartyDevicePairing,
    DeleteThirdPartyHardwareDevice,
    GenerateHardwareWelcomeAudio,
    GetHardwareWelcomeAudioDataURL,
	GetPetPackPreviewDataURL,
	ListThirdPartyHardwareDeviceBindings,
    LoadConfig,
    ResetHardwareWelcomeAudio,
    RefreshDeviceAmbientWeather,
    RestartThirdPartyGateway,
    ListPetPacks,
    SelectHardwareWelcomeAudio,
	SendHardwareDevicePetProfile,
	SetHardwareAllowCustomPets,
	SendHardwareDeviceVolume,
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
};

type HardwareDevice = {
    clientId: string;
    clientName?: string;
    protocolVersion?: string;
    pairedAt?: string;
    lastSeenAt?: string;
    online?: boolean;
	volume?: number;
	petSkin?: string;
};

type HardwareDeviceBindings = {
	devices?: HardwareDevice[];
	maxDevices?: number;
	boundCount?: number;
};

const hardwareBindingLimit = 5;

type WelcomeAction = 'generate' | 'import' | 'reset' | 'preview-local';

const gatewayStatusLabel = (status: string, lang: string) => ({
    connected: lang === 'en' ? 'Running' : '已启动',
    connecting: lang === 'en' ? 'Starting' : '启动中',
    disconnected: lang === 'en' ? 'Stopped' : '未连接',
    disabled: lang === 'en' ? 'Disabled' : '未启用',
    error: lang === 'en' ? 'Error' : '错误',
}[status] || status);

const welcomePreviewErrorMessage = (error: unknown, isZh: boolean) => {
    const message = (error as any)?.message || String(error);
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
}: ThirdPartyAccessSettingsProps) => {
    const { showConfirm } = useDialog();
    const [pairing, setPairing] = useState<any>(null);
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
	const [deletingClientId, setDeletingClientId] = useState<string | null>(null);
	const [petOptions, setPetOptions] = useState<PetSkinOption[]>(petSkinOptions);
	const [savingPetClientIds, setSavingPetClientIds] = useState<Set<string>>(() => new Set());
	const [previewingClientId, setPreviewingClientId] = useState<string | null>(null);
    const [ambientCityDraft, setAmbientCityDraft] = useState(() => String((config as any)?.pet_ambient_city || ''));
    const [detectedAmbientCity, setDetectedAmbientCity] = useState<string | null>(null);
    const [ambientRefreshState, setAmbientRefreshState] = useState<'idle' | 'refreshing' | 'done' | 'error'>('idle');
    const localPreviewAudioRef = useRef<HTMLAudioElement | null>(null);
    const localPreviewCleanupRef = useRef<(() => void) | null>(null);
    const localPreviewSourceRef = useRef('');
    const localPreviewSourceLoadRef = useRef<Promise<string> | null>(null);
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
    const ambientRefreshRequestRef = useRef(0);
    const ambientSaveTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const pendingAmbientCityRef = useRef<string | null>(null);
    const ambientSaveVersionRef = useRef(0);
    const ambientSaveTailRef = useRef<Promise<void>>(Promise.resolve());
    const ambientQueuedVersionRef = useRef(0);
    const ambientQueuedPromiseRef = useRef<Promise<void> | null>(null);
    const confirmedAmbientCityRef = useRef(String((config as any)?.pet_ambient_city || ''));
    const saveRemoteConfigFieldRef = useRef(saveRemoteConfigField);

    showToastMessageRef.current = showToastMessage;
    saveRemoteConfigFieldRef.current = saveRemoteConfigField;

    const isZh = lang === 'zh-Hans' || lang === 'zh-Hant';
    const enabled = Boolean((config as any)?.thirdparty_gateway_enabled);
    const hardwareEnabled = Boolean((config as any)?.hardware_enabled);
    const settingsBusy = busy || gatewayBusy;
    const hardwareControlsDisabled = settingsBusy || !hardwareEnabled || !enabled;
    // Welcome content can be prepared and previewed locally before hardware is
    // enabled. Only transport-dependent controls use hardwareControlsDisabled.
    const welcomeEditingDisabled = settingsBusy;
	const defaultVolume = Number((config as any)?.hardware_volume ?? 70);
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

    const refreshConfig = async () => setConfig(await LoadConfig() as any);

    const refreshDevices = useCallback(async () => {
        const requestID = ++devicesRequestRef.current;
        if (thirdPartyGatewayLocalMode || !hardwareEnabled) {
            setDevices([]);
			setBoundDeviceCount(0);
            setDevicesError('');
            setDevicesLoading(false);
            return;
        }
        setDevicesLoading(true);
        setDevicesError('');
        try {
			const bindings = await ListThirdPartyHardwareDeviceBindings() as HardwareDeviceBindings;
			const next = bindings?.devices || [];
			if (mountedRef.current && requestID === devicesRequestRef.current) {
				setDevices(next);
				setBoundDeviceCount(Math.max(0, Number(bindings?.boundCount ?? next.length)));
			}
        } catch (err: any) {
            const message = err?.message || String(err);
            if (mountedRef.current && requestID === devicesRequestRef.current) {
                setDevicesError(message);
                showToastMessageRef.current(message);
            }
        } finally {
            if (mountedRef.current && requestID === devicesRequestRef.current) setDevicesLoading(false);
        }
    }, [hardwareEnabled, thirdPartyGatewayLocalMode]);
    refreshDevicesRef.current = refreshDevices;

    useEffect(() => { void refreshDevicesRef.current(); }, [hardwareEnabled, thirdPartyGatewayLocalMode, thirdPartyGatewayStatus]);
	useEffect(() => {
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
	}, [lang]);
    useEffect(() => {
        // React StrictMode runs an effect setup/cleanup/setup cycle in development.
        // Restore the guard during setup so the live second mount is not mistaken
        // for an unmounted component.
        mountedRef.current = true;
        return () => {
            mountedRef.current = false;
            devicesRequestRef.current += 1;
            previewRunRef.current += 1;
			volumeSendTimersRef.current.forEach((timer) => clearTimeout(timer));
			volumeSendTimersRef.current.clear();
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
        const request = Promise.resolve(GetHardwareWelcomeAudioDataURL()).then((source) => {
            if (mountedRef.current) localPreviewSourceRef.current = source;
            return source;
        });
        localPreviewSourceLoadRef.current = request;
        void request.finally(() => {
            if (localPreviewSourceLoadRef.current === request) localPreviewSourceLoadRef.current = null;
        }).catch(() => undefined);
        return request;
    }, [welcomeAudioPath]);

    useEffect(() => {
        localPreviewSourceRef.current = '';
        if (welcomeAudioPath) void refreshLocalWelcomePreviewSource().catch(() => undefined);
    }, [refreshLocalWelcomePreviewSource, welcomeAudioPath]);

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

	const sendDeviceVolume = useCallback(async (clientId: string, value: number) => {
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
					deviceVolumeBeforeEditRef.current.delete(clientId);
				} catch (err: any) {
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
				void sendDeviceVolume(clientId, pendingVolumesRef.current.get(clientId)!);
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
		const next = Math.max(0, Math.min(100, Math.round(value)));
		pendingVolumesRef.current.set(clientId, next);
		const timer = volumeSendTimersRef.current.get(clientId);
		if (timer !== undefined) {
			clearTimeout(timer);
			volumeSendTimersRef.current.delete(clientId);
		}
		if (immediate) {
			if (volumeSendsInFlightRef.current.has(clientId)) volumeFlushPendingRef.current.add(clientId);
			void sendDeviceVolume(clientId, next);
			return;
		}
		volumeSendTimersRef.current.set(clientId, setTimeout(() => {
			volumeSendTimersRef.current.delete(clientId);
			void sendDeviceVolume(clientId, pendingVolumesRef.current.get(clientId) ?? next);
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

	const setDevicePet = async (device: HardwareDevice, skin: string) => {
		const previous = device.petSkin || String((config as any)?.pet_skin || 'clawmate');
		setDevices((current) => current.map((item) => item.clientId === device.clientId ? { ...item, petSkin: skin } : item));
		setSavingPetClientIds((current) => new Set(current).add(device.clientId));
		try {
			await SendHardwareDevicePetProfile(device.clientId, skin);
			showToastMessage(isZh ? `${device.clientName || device.clientId} 的宠物已更新。` : `Pet updated for ${device.clientName || device.clientId}.`);
		} catch (err: any) {
			setDevices((current) => current.map((item) => item.clientId === device.clientId ? { ...item, petSkin: previous } : item));
			showToastMessage(err?.message || String(err));
		} finally {
			if (mountedRef.current) setSavingPetClientIds((current) => {
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
			setConfig((current: any) => current ? { ...current, hardware_allow_custom_pets: enabled } : current);
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
		setPreviewingClientId(device.clientId);
		try {
			await SendHardwareWelcomeAudioRemote(device.clientId);
			showToastMessage(isZh ? `${device.clientName || device.clientId} 已确认播放完成。` : `${device.clientName || device.clientId} confirmed playback.`);
		} catch (err: any) {
			showToastMessage(welcomePreviewErrorMessage(err, isZh));
		} finally {
			if (mountedRef.current) setPreviewingClientId(null);
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
            if (next) {
                setThirdPartyGatewayLocalModeState(false);
                setPairing(null);
            }
            await refreshConfig();
            showToastMessage(next
                ? (isZh ? '硬件已启用，第三方接入已切换为多机模式并重新启动。' : 'Hardware enabled. Third-party access switched to Hub mode and restarted.')
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
        <p className="im-settings-description">
            {isZh ? '开放本机 HTTP 消息接入端口，第三方软件主动连接 MaClaw，无需提供回调地址。' : 'Expose a local HTTP message gateway. Third-party software connects to MaClaw without a callback URL.'}
        </p>
        <div className="im-settings-toolbar">
            <label className="im-settings-toggle" title={hardwareEnabled ? (isZh ? '请先停用硬件' : 'Disable hardware first') : undefined}>
                <input
                    type="checkbox"
                    disabled={settingsBusy || hardwareEnabled}
                    aria-label={textForLang(lang, 'Enable third-party access', '开启第三方软件接入', '開啟第三方軟體接入')}
                    checked={enabled}
                    onChange={(e) => void changeGatewayEnabled(e.target.checked)}
                />
                <span>{textForLang(lang, 'Enable third-party access', '开启第三方软件接入', '開啟第三方軟體接入')}</span>
            </label>
            <span className="im-settings-status" data-status={thirdPartyGatewayStatus} aria-live="polite">
                {gatewayStatusLabel(thirdPartyGatewayStatus, lang)}
            </span>
            <button type="button" className="im-settings-button" disabled={!enabled || settingsBusy} onClick={() => void restartGateway()}>
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
                    title={hardwareEnabled && option.value ? (isZh ? '启用硬件时必须使用多机模式' : 'Hardware requires Hub mode') : option.desc}
                    disabled={settingsBusy || (hardwareEnabled && option.value)}
                    data-active={thirdPartyGatewayLocalMode === option.value}
                    onClick={() => void changeGatewayMode(option.value)}
                >{option.label}</button>)}
            </div>
        </div>

        <div className="im-settings-grid im-settings-grid--gateway">
            <label className="im-settings-field">
                <span>Host</span>
                <input type="text" disabled={settingsBusy || hardwareEnabled} title={hardwareEnabled ? (isZh ? '停用硬件后才能更改监听地址' : 'Disable hardware before changing the listener address') : undefined} value={(config as any)?.thirdparty_gateway_host || '127.0.0.1'} onChange={(e) => saveRemoteConfigField({ thirdparty_gateway_host: e.target.value })} placeholder="127.0.0.1" spellCheck={false} />
            </label>
            <label className="im-settings-field im-settings-field--port">
                <span>Port</span>
                <input type="number" disabled={settingsBusy || hardwareEnabled} title={hardwareEnabled ? (isZh ? '停用硬件后才能更改监听端口' : 'Disable hardware before changing the listener port') : undefined} min={1} max={65535} value={(config as any)?.thirdparty_gateway_port || 18777} onChange={(e) => saveRemoteConfigField({ thirdparty_gateway_port: Number(e.target.value || 18777) })} />
            </label>
            <label className="im-settings-field im-settings-field--token">
                <span>Token</span>
                <span className="im-settings-token-row">
                    <input type="password" disabled={settingsBusy || hardwareEnabled} title={hardwareEnabled ? (isZh ? '停用硬件后才能更改 Token' : 'Disable hardware before changing the token') : undefined} value={(config as any)?.thirdparty_gateway_token || ''} onChange={(e) => saveRemoteConfigField({ thirdparty_gateway_token: e.target.value })} placeholder="Bearer token" autoComplete="off" />
                    <button type="button" className="im-settings-button im-settings-button--primary" disabled={settingsBusy || hardwareEnabled} title={hardwareEnabled ? (isZh ? '停用硬件后才能更改 Token' : 'Disable hardware before changing the token') : undefined} onClick={() => void generateToken()}>
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

        <div className="im-settings-hardware" aria-label={isZh ? '硬件配置' : 'Hardware configuration'}>
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
                {isZh ? '启用后将自动切换为多机模式，并重新启动第三方接入。' : 'Enabling switches third-party access to Hub mode and restarts the gateway.'}
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
                    <button type="button" className="im-settings-button im-settings-button--primary" disabled={hardwareControlsDisabled} onClick={async () => {
                        try {
                            setPairing(await CreateThirdPartyDevicePairing());
                            showToastMessage(isZh ? '已生成配对码，有效期 30 分钟。' : 'Pairing code generated; valid for 30 minutes.');
                        } catch (err: any) {
                            showToastMessage(err?.message || String(err));
                        }
                    }}>{pairing?.pairCode ? (isZh ? '重新生成' : 'Regenerate') : (isZh ? '获取配对码' : 'Get code')}</button>
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
								const devicePetSkin = device.petSkin || String((config as any)?.pet_skin || 'clawmate');
								const devicePet = getPetSkinOption(devicePetSkin, petOptions);
								const availablePetOptions = petOptions.some((pet) => pet.id === devicePetSkin) ? petOptions : [devicePet, ...petOptions];
                                return <div className="im-settings-hardware__device" key={device.clientId}>
                                    <span className="im-settings-hardware__device-status" data-online={Boolean(device.online)} aria-hidden="true" />
                                    <div className="im-settings-hardware__device-details">
                                        <strong title={name}>{name}</strong>
                                        <code title={device.clientId}>{device.clientId}</code>
                                        <small>
                                            {device.online ? (isZh ? '在线' : 'Online') : (lastSeen ? `${isZh ? '最后连接' : 'Last seen'} ${lastSeen}` : (isZh ? '尚未连接' : 'Not seen yet'))}
                                            {device.protocolVersion ? ` · v${device.protocolVersion}` : ''}
                                        </small>
                                    </div>
									<div className="im-settings-hardware__device-actions">
										{allowCustomHardwarePets && <label className="im-settings-hardware__device-pet" title={devicePet.label}>
											<span className="im-settings-hardware__device-pet-preview"><img src={devicePet.image} alt="" /></span>
											<span>{isZh ? '宠物' : 'Pet'}</span>
											<select
												aria-label={`${isZh ? '宠物' : 'Pet'} ${name}`}
												value={devicePetSkin}
												disabled={hardwareControlsDisabled || deletingClientId === device.clientId || savingPetClientIds.has(device.clientId)}
												onChange={(event) => void setDevicePet(device, event.target.value)}
											>
												{availablePetOptions.map((pet) => <option key={pet.id} value={pet.id}>{pet.label}</option>)}
											</select>
										</label>}
										<div className="im-settings-hardware__device-volume">
										<label htmlFor={`hardware-volume-${device.clientId}`}>{isZh ? '音量' : 'Volume'} <strong>{deviceVolume}%</strong></label>
										<input id={`hardware-volume-${device.clientId}`} aria-label={`${isZh ? '音量' : 'Volume'} ${name}`} type="range" min={0} max={100} step={1} value={deviceVolume} disabled={hardwareControlsDisabled || deletingClientId === device.clientId} onPointerDown={() => rememberDeviceVolumeBeforeEdit(device.clientId)} onKeyDown={(event) => {
											if (['ArrowLeft', 'ArrowRight', 'Home', 'End', 'PageUp', 'PageDown'].includes(event.key)) rememberDeviceVolumeBeforeEdit(device.clientId);
										}} onChange={(event) => {
											const next = Number(event.target.value);
											setDevices((current) => current.map((item) => item.clientId === device.clientId ? { ...item, volume: next } : item));
											scheduleDeviceVolume(device.clientId, next);
										}} onPointerUp={(event) => scheduleDeviceVolume(device.clientId, Number((event.target as HTMLInputElement).value), true)} onKeyUp={(event) => {
											if (['ArrowLeft', 'ArrowRight', 'Home', 'End', 'PageUp', 'PageDown'].includes(event.key)) scheduleDeviceVolume(device.clientId, Number((event.target as HTMLInputElement).value), true);
										}} />
										<small>{device.online ? (isZh ? '调整后仅发送到此设备。' : 'Changes apply only to this device.') : (isZh ? '设备离线；设置会在下次连接时生效。' : 'Offline; applied when this device reconnects.')}</small>
									</div>
	                                        <button type="button" className="im-settings-button im-settings-button--primary" aria-label={`${isZh ? '远程播放' : 'Play remotely'} ${name}`} disabled={hardwareControlsDisabled || !welcomeAudioPath || !device.online || deletingClientId !== null || previewingClientId !== null} title={!device.online ? (isZh ? '设备离线，无法远程播放。' : 'This device is offline.') : undefined} onClick={() => void previewRemoteHardware(device)}>{previewingClientId === device.clientId ? (isZh ? '播放中…' : 'Playing…') : (isZh ? '远程播放' : 'Play remotely')}</button>
	                                    <button type="button" className="im-settings-button im-settings-button--danger" aria-label={`${isZh ? '解绑' : 'Remove'} ${name}`} disabled={deletingClientId !== null || previewingClientId !== null} onClick={async () => {
                                        const confirmed = await showConfirm(
                                            isZh ? `解绑后，${name} 的 Token 将立即失效；如需再次使用，必须重新配对。` : `${name}'s token will be revoked immediately. The device must pair again before it can reconnect.`,
                                            isZh ? '解绑硬件？' : 'Remove hardware?',
                                            { confirmText: isZh ? '解绑' : 'Remove', cancelText: isZh ? '取消' : 'Cancel', confirmVariant: 'danger' },
                                        );
	                                        if (!confirmed || !mountedRef.current) return;
	                                        setDeletingClientId(device.clientId);
	                                        try {
							discardPendingDeviceVolume(device.clientId);
                                            await DeleteThirdPartyHardwareDevice(device.clientId);
                                            if (mountedRef.current) {
                                                devicesRequestRef.current += 1;
                                                setDevicesLoading(false);
                                                setDevices((current) => current.filter((item) => item.clientId !== device.clientId));
												setBoundDeviceCount((count) => Math.max(0, count - 1));
                                                showToastMessage(isZh ? '硬件已解绑。' : 'Hardware removed.');
                                            }
                                        } catch (err: any) {
                                            if (mountedRef.current) showToastMessage(err?.message || String(err));
                                        } finally {
                                            if (mountedRef.current) setDeletingClientId(null);
                                        }
	                                    }}>{deletingClientId === device.clientId ? (isZh ? '解绑中…' : 'Removing…') : (isZh ? '解绑' : 'Remove')}</button>
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
                    <span>{isZh ? '英文发音' : 'English voice'}</span>
                    <select
                        aria-label={isZh ? '英文发音' : 'English voice'}
                        disabled={welcomeEditingDisabled || welcomeVoiceSaving}
                        value={welcomeVoiceID}
                        onChange={(event) => void changeWelcomeVoice(event.target.value)}
                    >
                        <option value="af_heart">{isZh ? '甜美女声' : 'Sweet female'}</option>
                        <option value="am_adam">{isZh ? '自然男声' : 'Natural male'}</option>
                    </select>
                    <small>{isZh ? '选择后重新生成音频；默认使用温暖、清晰的美式女声。' : 'Regenerate after changing the voice. The warm, clear American female voice is the default.'}</small>
                </label>
                <div className="im-settings-hardware__welcome-controls">
                    <textarea disabled={welcomeEditingDisabled} value={welcomeText} maxLength={80} onChange={(e) => setWelcomeText(e.target.value)} placeholder={isZh ? '例如：Hello, Maclaw' : 'For example: Hello, Maclaw'} />
                    <button type="button" className="im-settings-button" disabled={welcomeEditingDisabled || welcomeVoiceSaving || !welcomeText.trim()} onClick={() => void runWelcomeAction('generate', async () => {
                        try {
                            await GenerateHardwareWelcomeAudio(welcomeText, welcomeVoiceID);
                            await refreshConfig();
                            await refreshLocalWelcomePreviewSource(true).catch(() => undefined);
                            showToastMessage(isZh ? '欢迎音频已生成。' : 'Welcome audio generated.');
                        } catch (err: any) {
                            showToastMessage(err?.message || String(err));
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
	                <small className="im-settings-hardware__preview-note">{isZh ? 'GUI 本地试听用于检查生成质量；请在上方对应的已绑定硬件旁点击“远程播放”，仅向该 ESP32 下发并等待“播放完成”确认。' : 'GUI preview checks generated audio quality. Use the remote-play button beside a bound device to send only to that ESP32 and wait for its playback receipt.'}</small>
            </div>
        </div>
    </section>;
};
