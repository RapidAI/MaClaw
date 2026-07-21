import { useCallback, useEffect, useRef, useState } from 'react';
import { BrowserOpenURL, EventsOn } from '../../wailsjs/runtime';
import { main } from '../../wailsjs/go/models';
import {
    ListPetPacks,
    InstallPetPackZip,
    SelectPetPackZip,
    UninstallPetPack,
    GetPetPackPreviewDataURL,
    GetPetPackStateFrameDataURL,
    OpenPetPacksDir,
    GetPetPacksDir,
} from '../../wailsjs/go/main/App';
import { buildHubPetPackHelpURL } from '../utils/hubCredits';
import {
    defaultPetSize,
    getPetSkinOption,
    packInfoToSkinOption,
    petSkinOptions,
    type PetSkinOption,
} from './petSkins';
import { motionSoundPresetOptionIds, normalizeMotionSoundPreset, type MotionSoundPreset } from './petMotionSounds';

type Lang = 'en' | 'zh-Hans' | 'zh-Hant' | string;

function orderPackOptions(packs: unknown[], lang: Lang): PetSkinOption[] {
    const options = packs.map((p) => packInfoToSkinOption(p as Record<string, unknown>, lang));
    const byId = new Map(options.map((o) => [o.id, o]));
    const ordered: PetSkinOption[] = [];
    for (const b of petSkinOptions) {
        ordered.push(byId.get(b.id) || b);
        byId.delete(b.id);
    }
    const rest = Array.from(byId.values()).sort((a, b) => a.id.localeCompare(b.id));
    ordered.push(...rest);
    return ordered;
}

function openExternalURL(url: string): void {
    try {
        BrowserOpenURL(url);
    } catch {
        window.open(url, '_blank', 'noopener,noreferrer');
    }
}

function formatUnknownError(err: unknown): string {
    if (err instanceof Error && err.message) return err.message;
    if (typeof err === 'string' && err.trim()) return err.trim();
    if (err && typeof err === 'object' && 'message' in err) {
        const msg = String((err as { message?: unknown }).message || '').trim();
        if (msg) return msg;
    }
    return String(err ?? 'unknown error');
}
type PetPreviewState = 'idle' | 'listening' | 'thinking' | 'speaking' | 'done' | 'alert';
type DebouncedFieldKey = 'pet-size' | 'continuous-timeout';
type SaveState = 'idle' | 'pending' | 'saving' | 'saved' | 'error';
type PetToggleKey =
    | 'pet_motion_enabled'
    | 'pet_motion_sound_enabled'
    | 'pet_text_interaction_enabled'
    | 'pet_voice_input_enabled'
    | 'pet_voice_readback_enabled'
    | 'pet_file_drop_enabled'
    | 'pet_quiet_mode'
    | 'pet_reduced_motion';

interface PetSettingsPanelProps {
    config: main.AppConfig;
    lang: Lang;
    setConfig: (config: main.AppConfig) => void;
    patchConfig: (patch: Record<string, unknown>) => Promise<main.AppConfig | void>;
}

const modeOptionIds = ['quiet', 'balanced', 'active'] as const;
const conversationModeOptionIds = ['text-first', 'voice-turn', 'continuous'] as const;
const readbackModeOptionIds = ['off', 'summary', 'full', 'done-only'] as const;
// Six semantic states with official pack frames (idle…alert); quiet is interaction-mode, not staged here.
const previewStateOptionIds: PetPreviewState[] = ['idle', 'listening', 'thinking', 'speaking', 'done', 'alert'];
const defaultEnabledToggleKeys = new Set<PetToggleKey>(['pet_motion_enabled', 'pet_motion_sound_enabled', 'pet_text_interaction_enabled', 'pet_file_drop_enabled']);

function text(lang: Lang, zhHans: string, zhHant: string, en: string): string {
    if (lang === 'zh-Hans') return zhHans;
    if (lang === 'zh-Hant') return zhHant;
    return en;
}

function interactionModeLabel(lang: Lang, id: string): string {
    switch (id) {
        case 'quiet':
            return text(lang, '安静', '安靜', 'Quiet');
        case 'active':
            return text(lang, '活跃', '活躍', 'Active');
        case 'balanced':
        default:
            return text(lang, '平衡', '平衡', 'Balanced');
    }
}

function conversationModeLabel(lang: Lang, id: string): string {
    switch (id) {
        case 'voice-turn':
            return text(lang, '语音轮次', '語音輪次', 'Voice Turn');
        case 'continuous':
            return text(lang, '连续对话', '連續對話', 'Continuous');
        case 'text-first':
        default:
            return text(lang, '文字优先', '文字優先', 'Text First');
    }
}

function readbackModeLabel(lang: Lang, id: string): string {
    switch (id) {
        case 'summary':
            return text(lang, '摘要', '摘要', 'Summary');
        case 'full':
            return text(lang, '全文', '全文', 'Full');
        case 'done-only':
            return text(lang, '仅完成时', '僅完成時', 'Done Only');
        case 'off':
        default:
            return text(lang, '关闭', '關閉', 'Off');
    }
}

function motionSoundPresetLabel(lang: Lang, id: string): string {
    switch (id) {
        case 'bubble':
            return text(lang, '气泡', '氣泡', 'Bubble');
        case 'chime':
            return text(lang, '铃音', '鈴音', 'Chime');
        case 'synth':
            return text(lang, '合成器', '合成器', 'Synth');
        case 'soft':
            return text(lang, '柔和', '柔和', 'Soft');
        case 'classic':
        default:
            return text(lang, '经典', '經典', 'Classic');
    }
}

function motionSoundPresetDescription(lang: Lang, id: string): string {
    switch (id) {
        case 'bubble':
            return text(lang, '短促、圆润，适合 mini 形象。', '短促、圓潤，適合 mini 形象。', 'Short, rounded, and good for small sizes.');
        case 'chime':
            return text(lang, '清晰、有轻尾音，适合提醒。', '清晰、有輕尾音，適合提醒。', 'Clear with a light notification tail.');
        case 'synth':
            return text(lang, '干净电子质感，适合开发者风格。', '乾淨電子質感，適合開發者風格。', 'Clean electronic texture for the developer skin.');
        case 'soft':
            return text(lang, '更低音量和更长淡出，适合专注。', '更低音量和更長淡出，適合專注。', 'Lower gain with a longer fade for focus.');
        case 'classic':
        default:
            return text(lang, '克制的默认动作提示音。', '克制的預設動作提示音。', 'A restrained default motion cue.');
    }
}

function optionAriaLabel(label: string, description: string): string {
    return `${label}: ${description}`;
}

function playMotionSoundPresetPreview(preset: MotionSoundPreset): void {
    try {
        const AudioContextCtor = window.AudioContext || (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
        if (!AudioContextCtor) return;
        const ctx = new AudioContextCtor();
        const now = ctx.currentTime;
        const compressor = ctx.createDynamicsCompressor();
        compressor.threshold.setValueAtTime(-26, now);
        compressor.knee.setValueAtTime(18, now);
        compressor.ratio.setValueAtTime(5, now);
        compressor.attack.setValueAtTime(0.003, now);
        compressor.release.setValueAtTime(0.12, now);
        compressor.connect(ctx.destination);

        const filter = ctx.createBiquadFilter();
        filter.type = preset === 'soft' ? 'lowpass' : 'bandpass';
        filter.frequency.setValueAtTime(preset === 'chime' ? 3600 : preset === 'synth' ? 1600 : preset === 'soft' ? 1200 : 2600, now);
        filter.Q.setValueAtTime(preset === 'bubble' ? 0.45 : 0.72, now);
        filter.connect(compressor);

        const output = ctx.createGain();
        output.gain.setValueAtTime(0.0001, now);
        output.gain.exponentialRampToValueAtTime(preset === 'soft' ? 0.012 : preset === 'chime' ? 0.016 : 0.018, now + 0.018);
        output.gain.exponentialRampToValueAtTime(0.0001, now + (preset === 'soft' || preset === 'chime' ? 0.28 : 0.2));
        output.connect(filter);

        const delay = ctx.createDelay(0.12);
        const delayGain = ctx.createGain();
        delay.delayTime.setValueAtTime(preset === 'chime' ? 0.058 : preset === 'soft' ? 0.048 : 0.032, now);
        delayGain.gain.setValueAtTime(preset === 'chime' ? 0.12 : preset === 'soft' ? 0.06 : 0.04, now);
        output.connect(delay);
        delay.connect(delayGain);
        delayGain.connect(filter);

        const tones: Array<[number, number, OscillatorType]> = preset === 'bubble'
            ? [[560, 0, 'sine'], [860, 0.048, 'triangle']]
            : preset === 'chime'
                ? [[880, 0, 'sine'], [1320, 0.07, 'sine']]
                : preset === 'synth'
                    ? [[640, 0, 'square'], [480, 0.042, 'triangle']]
                    : preset === 'soft'
                        ? [[392, 0, 'sine'], [588, 0.082, 'triangle']]
                        : [[620, 0, 'sine'], [930, 0.052, 'triangle']];

        tones.forEach(([hz, delay, type]) => {
            const osc = ctx.createOscillator();
            osc.type = type;
            osc.frequency.setValueAtTime(hz, now + delay);
            osc.connect(output);
            osc.start(now + delay);
            osc.stop(now + delay + (preset === 'chime' || preset === 'soft' ? 0.22 : 0.13));
        });

        window.setTimeout(() => {
            output.disconnect();
            delay.disconnect();
            delayGain.disconnect();
            filter.disconnect();
            compressor.disconnect();
            void ctx.close().catch(() => undefined);
        }, 420);
    } catch {
        // Preview sound is best-effort and should not block saving settings.
    }
}

function previewStateLabel(lang: Lang, id: PetPreviewState): string {
    switch (id) {
        case 'listening':
            return text(lang, '聆听', '聆聽', 'Listen');
        case 'thinking':
            return text(lang, '思考', '思考', 'Think');
        case 'speaking':
            return text(lang, '说话', '說話', 'Speak');
        case 'done':
            return text(lang, '完成', '完成', 'Done');
        case 'alert':
            return text(lang, '提醒', '提醒', 'Alert');
        case 'idle':
        default:
            return text(lang, '待机', '待機', 'Idle');
    }
}

function skinToneLabel(lang: Lang, tone: string): string {
    switch (tone) {
        case 'Compact':
            return text(lang, '紧凑', '緊湊', 'Compact');
        case 'Developer':
            return text(lang, '开发者', '開發者', 'Developer');
        case 'Focus':
            return text(lang, '专注', '專注', 'Focus');
        case 'Balanced':
        default:
            return text(lang, '平衡', '平衡', 'Balanced');
    }
}

function skinDescription(lang: Lang, id: string): string {
    switch (id) {
        case 'mini-claw':
            return text(lang, '小巧外壳、短耳和小靴子的贴边助手', '小巧外殼、短耳和小靴子的貼邊助手', 'Compact shell, short ears, and tiny boots');
        case 'dev-claw':
            return text(lang, '带护目镜和终端胸牌的编码助手', '帶護目鏡和終端胸牌的編碼助手', 'Coding helper with a visor and terminal badge');
        case 'focus-claw':
            return text(lang, '低动作、柔和表情的专注陪伴', '低動作、柔和表情的專注陪伴', 'Low-motion companion with a softer expression');
        case 'clawmate':
        default:
            return text(lang, '带耳朵、爪子和信号标记的默认助手', '帶耳朵、爪子和訊號標記的預設助手', 'Default helper with ears, paws, and a signal tag');
    }
}

function skinPreviewLine(lang: Lang, id: string): string {
    switch (id) {
        case 'mini-claw':
            return text(lang, '小巧、快速，适合贴边常驻。', '小巧、快速，適合貼邊常駐。', 'Small, fast, and easy to keep near the edge.');
        case 'dev-claw':
            return text(lang, '更技术、更直接，随时准备进入编码轮次。', '更技術、更直接，隨時準備進入編碼輪次。', 'More technical, direct, and ready for coding turns.');
        case 'focus-claw':
            return text(lang, '动作更克制，适合长时间专注。', '動作更克制，適合長時間專注。', 'Calmer motion for long focus sessions.');
        case 'clawmate':
        default:
            return text(lang, '抓住问题，把有效信号拎出来。', '抓住問題，把有效訊號拎出來。', 'Catches the problem and pulls out the signal.');
    }
}

function capabilityLabel(lang: Lang, name: string, ready: boolean): string {
    if (ready) return text(lang, `${name} 已就绪`, `${name} 已就緒`, `${name} ready`);
    return text(lang, `${name} 未启用`, `${name} 未啟用`, `${name} not enabled`);
}

function clampPetSize(value: number): number {
    if (!Number.isFinite(value)) return defaultPetSize;
    return Math.min(120, Math.max(56, Math.round(value)));
}

export function PetSettingsPanel({ config, lang, setConfig, patchConfig }: PetSettingsPanelProps) {
    const [previewState, setPreviewState] = useState<PetPreviewState>('idle');
    const [saveState, setSaveState] = useState<SaveState>('idle');
    const [packOptions, setPackOptions] = useState<PetSkinOption[]>(petSkinOptions);
    const [installBusy, setInstallBusy] = useState(false);
    const [installError, setInstallError] = useState('');
    const [installNotice, setInstallNotice] = useState('');
    const [stageImage, setStageImage] = useState('');
    const [packsDirLabel, setPacksDirLabel] = useState('');
    const latestConfigRef = useRef<main.AppConfig>(config);
    const saveTimersRef = useRef<Partial<Record<DebouncedFieldKey, number>>>({});
    const savedTimerRef = useRef<number | undefined>(undefined);
    const installNoticeTimerRef = useRef<number | undefined>(undefined);
    const mountedRef = useRef(true);
    const saveSeqRef = useRef(0);
    const pendingPatchRef = useRef<Record<string, unknown>>({});
    const petSize = clampPetSize(config.pet_size || defaultPetSize);
    const selectedSkin = config.pet_skin || 'clawmate';
    const selectedVariant = config.pet_variant || 'classic';
    const interactionMode = config.pet_interaction_mode || 'balanced';
    const motionSoundPreset = normalizeMotionSoundPreset(config.pet_motion_sound_preset);
    const conversationMode = config.pet_conversation_mode || 'text-first';
    const readbackMode = config.pet_readback_mode || (config.pet_voice_readback_enabled ? 'summary' : 'off');
    const continuousTimeout = Math.min(120, Math.max(5, Number(config.pet_continuous_timeout_sec || 30)));
    const asrReady = !!config.asr_enabled;
    const ttsReady = !!config.tts_enabled;
    const petEnabled = !!config.pet_enabled;
    const quietMode = !!config.pet_quiet_mode;
    const upgradePrompt = !!config.pet_figurative_upgrade_prompt_pending;
    const voiceReady = asrReady && ttsReady;
    const selectedSkinOption = getPetSkinOption(selectedSkin, packOptions);
    const motionEnabled = config.pet_motion_enabled !== false;
    const motionSoundPreviewEnabled = config.pet_motion_sound_enabled !== false && !config.pet_quiet_mode;
    const toggleOptions: ReadonlyArray<readonly [PetToggleKey, string]> = [
        ['pet_motion_enabled', text(lang, '\u52a8\u4f5c\u52a8\u753b', '\u52d5\u4f5c\u52d5\u756b', 'Motion')],
        ['pet_motion_sound_enabled', text(lang, '\u52a8\u4f5c\u97f3\u6548', '\u52d5\u4f5c\u97f3\u6548', 'Motion SFX')],
        ['pet_text_interaction_enabled', text(lang, '\u6587\u5b57\u4ea4\u6d41', '\u6587\u5b57\u4ea4\u6d41', 'Text Chat')],
        ['pet_voice_input_enabled', text(lang, '\u8bed\u97f3\u8f93\u5165', '\u8a9e\u97f3\u8f38\u5165', 'Voice Input')],
        ['pet_voice_readback_enabled', text(lang, '\u8bed\u97f3\u64ad\u62a5', '\u8a9e\u97f3\u64ad\u5831', 'Voice Readback')],
        ['pet_file_drop_enabled', text(lang, '\u6587\u4ef6\u62d6\u62fd', '\u6587\u4ef6\u62d6\u66f3', 'File Drop')],
        ['pet_quiet_mode', text(lang, '\u52ff\u6270\u6a21\u5f0f', '\u52ff\u64fe\u6a21\u5f0f', 'Do Not Disturb')],
        ['pet_reduced_motion', text(lang, '\u51cf\u5c11\u52a8\u6548', '\u6e1b\u5c11\u52d5\u6548', 'Reduced Motion')],
    ];
    const isToggleChecked = (key: PetToggleKey) => {
        const value = config[key];
        return defaultEnabledToggleKeys.has(key) ? value !== false : !!value;
    };
    const saveStateLabel = saveState === 'pending'
        ? text(lang, '\u5f85\u4fdd\u5b58', '\u5f85\u5132\u5b58', 'Pending')
        : saveState === 'saving'
            ? text(lang, '\u4fdd\u5b58\u4e2d', '\u5132\u5b58\u4e2d', 'Saving')
            : saveState === 'saved'
                ? text(lang, '\u5df2\u4fdd\u5b58', '\u5df2\u5132\u5b58', 'Saved')
                : saveState === 'error'
                    ? text(lang, '\u4fdd\u5b58\u5931\u8d25', '\u5132\u5b58\u5931\u6557', 'Save failed')
                    : '';

    useEffect(() => {
        latestConfigRef.current = config;
    }, [config]);

    const enrichPreviews = useCallback(async (options: PetSkinOption[]): Promise<PetSkinOption[]> => {
        return Promise.all(
            options.map(async (opt) => {
                // Always try pack raster preview (official + user). Builtin SVG is fallback only.
                try {
                    const dataURL = await GetPetPackPreviewDataURL(opt.id);
                    if (dataURL && dataURL.startsWith('data:image/')) {
                        return { ...opt, image: dataURL, hasPreview: true };
                    }
                } catch {
                    // keep fallback image
                }
                return opt;
            }),
        );
    }, []);

    const applyPackList = useCallback(async (packs: unknown[], isCancelled?: () => boolean) => {
        if (!Array.isArray(packs) || packs.length === 0) return;
        const ordered = orderPackOptions(packs, lang);
        if (isCancelled?.()) return;
        setPackOptions(ordered);
        // Async thumb enrich (native frames / user previews)
        const enriched = await enrichPreviews(ordered);
        if (isCancelled?.()) return;
        setPackOptions(enriched);
    }, [lang, enrichPreviews]);

    const refreshPackOptions = useCallback(async () => {
        try {
            const packs = await ListPetPacks();
            await applyPackList(packs);
        } catch {
            // Fallback to built-in catalog when Go binding unavailable.
        }
    }, [applyPackList]);

    useEffect(() => {
        let cancelled = false;
        void ListPetPacks()
            .then(async (packs) => {
                await applyPackList(packs, () => cancelled);
            })
            .catch(() => {
                // Fallback to built-in catalog when Go binding unavailable.
            });
        return () => {
            cancelled = true;
        };
    }, [applyPackList]);

    useEffect(() => {
        let off: (() => void) | undefined;
        let debounceTimer: number | undefined;
        try {
            // Debounce: install/uninstall also call refresh locally; event is for external drops.
            const unsub = EventsOn('pet:packs-changed', () => {
                if (debounceTimer) window.clearTimeout(debounceTimer);
                debounceTimer = window.setTimeout(() => {
                    debounceTimer = undefined;
                    void refreshPackOptions();
                }, 120);
            });
            off = typeof unsub === 'function' ? unsub : undefined;
        } catch {
            // runtime events optional in browser-only tests
        }
        return () => {
            if (debounceTimer) window.clearTimeout(debounceTimer);
            off?.();
        };
    }, [refreshPackOptions]);

    // Classic stage uses pack thumbs / builtin SVG (cheap, can follow packOptions enrich).
    useEffect(() => {
        if ((selectedVariant || 'classic') !== 'classic') return;
        setStageImage(getPetSkinOption(selectedSkin, packOptions).image);
    }, [selectedSkin, selectedVariant, packOptions]);

    // Figurative stage loads real state frames. Intentionally does NOT depend on packOptions
    // so thumb enrich does not re-fetch every pack's state frame via IPC.
    useEffect(() => {
        let cancelled = false;
        const skin = selectedSkin;
        const variant = selectedVariant || 'classic';
        const state = previewState;
        if (variant === 'classic') {
            return () => {
                cancelled = true;
            };
        }
        const fallback = getPetSkinOption(skin, packOptions).image;

        void GetPetPackStateFrameDataURL(skin, state, variant)
            .then((url) => {
                if (cancelled) return;
                if (url && url.startsWith('data:image/')) {
                    setStageImage(url);
                    return;
                }
                // fallback to pack thumb / builtin
                return GetPetPackPreviewDataURL(skin).then((preview) => {
                    if (cancelled) return;
                    setStageImage(preview && preview.startsWith('data:image/') ? preview : fallback);
                });
            })
            .catch(() => {
                if (!cancelled) setStageImage(fallback);
            });

        return () => {
            cancelled = true;
        };
        // packOptions omitted on purpose (see comment above); skin/variant/state are the load keys.
        // eslint-disable-next-line react-hooks/exhaustive-deps -- fallback image is best-effort only
    }, [selectedSkin, selectedVariant, previewState]);

    useEffect(() => {
        void GetPetPacksDir()
            .then((dir) => {
                if (dir) setPacksDirLabel(dir);
            })
            .catch(() => {
                /* optional */
            });
    }, []);

    const openPetPacksFolder = useCallback(async () => {
        try {
            await OpenPetPacksDir();
        } catch (err) {
            setInstallError(formatUnknownError(err));
        }
    }, []);

    const openPetPackHelp = useCallback(() => {
        const hubURL = String(config.remote_hub_url || '').trim();
        const url = buildHubPetPackHelpURL(hubURL, lang);
        if (!url) {
            window.alert(
                text(
                    lang,
                    '未配置 Hub 地址，无法打开宠物包创建指南。请先连接 Hub，或在浏览器访问 Hub 的 /pet-pack-help。',
                    '未設定 Hub 位址，無法開啟寵物包建立指南。請先連線 Hub，或在瀏覽器造訪 Hub 的 /pet-pack-help。',
                    'No Hub URL configured. Connect to a Hub, or open /pet-pack-help on your Hub in a browser.'
                )
            );
            return;
        }
        openExternalURL(url);
    }, [config.remote_hub_url, lang]);

    useEffect(() => {
        mountedRef.current = true;
        return () => {
            mountedRef.current = false;
            let hadPendingSave = false;
            Object.values(saveTimersRef.current).forEach((timer) => {
                if (timer) {
                    hadPendingSave = true;
                    window.clearTimeout(timer);
                }
            });
            if (savedTimerRef.current) window.clearTimeout(savedTimerRef.current);
            if (installNoticeTimerRef.current) {
                window.clearTimeout(installNoticeTimerRef.current);
                installNoticeTimerRef.current = undefined;
            }
            if (hadPendingSave && Object.keys(pendingPatchRef.current).length > 0) {
                void patchConfig(pendingPatchRef.current);
                pendingPatchRef.current = {};
            }
        };
    }, [patchConfig]);

    const persistPetConfig = useCallback((patch: Record<string, unknown>, next: main.AppConfig) => {
        if (savedTimerRef.current) window.clearTimeout(savedTimerRef.current);
        const saveSeq = ++saveSeqRef.current;
        setSaveState('saving');
        void patchConfig(patch)
            .then((saved) => {
                if (!mountedRef.current || saveSeq !== saveSeqRef.current) return;
                if (saved) {
                    const confirmed = new main.AppConfig(saved);
                    latestConfigRef.current = confirmed;
                    setConfig(confirmed);
                } else {
                    latestConfigRef.current = next;
                }
                pendingPatchRef.current = {};
                setSaveState('saved');
                savedTimerRef.current = window.setTimeout(() => {
                    if (mountedRef.current && saveSeq === saveSeqRef.current) setSaveState('idle');
                }, 1400);
            })
            .catch((err) => {
                console.error('[PetSettingsPanel] PatchConfigFields failed:', err);
                if (!mountedRef.current || saveSeq !== saveSeqRef.current) return;
                setSaveState('error');
            });
    }, [patchConfig, setConfig]);

    const clearPendingSaveTimers = useCallback(() => {
        Object.entries(saveTimersRef.current).forEach(([key, timer]) => {
            if (timer) {
                window.clearTimeout(timer);
                saveTimersRef.current[key as DebouncedFieldKey] = undefined;
            }
        });
    }, []);

    const updatePetConfig = useCallback((patch: Record<string, unknown>, debounceKey?: DebouncedFieldKey) => {
        const next = new main.AppConfig({ ...latestConfigRef.current, ...patch });
        latestConfigRef.current = next;
        setConfig(next);
        pendingPatchRef.current = { ...pendingPatchRef.current, ...patch };
        if (debounceKey) {
            clearPendingSaveTimers();
            setSaveState('pending');
            saveTimersRef.current[debounceKey] = window.setTimeout(() => {
                saveTimersRef.current[debounceKey] = undefined;
                persistPetConfig({ ...pendingPatchRef.current }, latestConfigRef.current);
            }, 350);
            return;
        }
        clearPendingSaveTimers();
        persistPetConfig({ ...pendingPatchRef.current }, next);
    }, [setConfig, clearPendingSaveTimers, persistPetConfig]);

    const installBusyRef = useRef(false);

    const uninstallSelectedPack = useCallback(async () => {
        const skin = selectedSkin;
        const opt = packOptions.find((p) => p.id === skin);
        if (!opt?.canUninstall) {
            setInstallError(text(lang, '官方内置包不能卸载', '官方內建包不能卸載', 'Official bundled packs cannot be uninstalled'));
            return;
        }
        const ok = window.confirm(
            text(
                lang,
                `确定卸载宠物包「${opt.label}」(${skin})？`,
                `確定卸載寵物包「${opt.label}」(${skin})？`,
                `Uninstall pet pack "${opt.label}" (${skin})?`
            )
        );
        if (!ok) return;
        setInstallError('');
        setInstallNotice('');
        setInstallBusy(true);
        installBusyRef.current = true;
        try {
            await UninstallPetPack(skin);
            await refreshPackOptions();
            // Backend UninstallPetPack already patches config when this skin was active.
            // Mirror locally without a second PatchConfigFields round-trip, and drop any
            // pending debounced patch that still references the removed skin.
            if (latestConfigRef.current.pet_skin === skin) {
                const next = new main.AppConfig({
                    ...latestConfigRef.current,
                    pet_skin: 'clawmate',
                    pet_variant: 'classic',
                    pet_figurative_upgrade_prompt_pending: false,
                });
                latestConfigRef.current = next;
                setConfig(next);
            }
            if (pendingPatchRef.current.pet_skin === skin) {
                pendingPatchRef.current = {
                    ...pendingPatchRef.current,
                    pet_skin: 'clawmate',
                    pet_variant: 'classic',
                    pet_figurative_upgrade_prompt_pending: false,
                };
            }
            setInstallNotice(text(lang, `已卸载：${skin}`, `已卸載：${skin}`, `Uninstalled: ${skin}`));
            if (installNoticeTimerRef.current) window.clearTimeout(installNoticeTimerRef.current);
            installNoticeTimerRef.current = window.setTimeout(() => {
                if (mountedRef.current) setInstallNotice('');
                installNoticeTimerRef.current = undefined;
            }, 2400);
        } catch (err) {
            setInstallError(formatUnknownError(err));
        } finally {
            installBusyRef.current = false;
            setInstallBusy(false);
        }
    }, [selectedSkin, packOptions, lang, refreshPackOptions, setConfig]);

    const installPetPackFromDialog = useCallback(async () => {
        if (installBusyRef.current) return;
        installBusyRef.current = true;
        setInstallError('');
        setInstallNotice('');
        if (installNoticeTimerRef.current) {
            window.clearTimeout(installNoticeTimerRef.current);
            installNoticeTimerRef.current = undefined;
        }
        setInstallBusy(true);
        try {
            const path = await SelectPetPackZip();
            if (!path) {
                return; // cancelled
            }
            const installedId = await InstallPetPackZip(path);
            await refreshPackOptions();
            if (installedId) {
                // Prefer figurative frames for newly installed packs.
                updatePetConfig({
                    pet_skin: installedId,
                    pet_variant: 'default',
                    pet_figurative_upgrade_prompt_pending: false,
                });
                setInstallNotice(
                    text(lang, `已安装并选用：${installedId}`, `已安裝並選用：${installedId}`, `Installed and selected: ${installedId}`)
                );
                installNoticeTimerRef.current = window.setTimeout(() => {
                    if (mountedRef.current) setInstallNotice('');
                    installNoticeTimerRef.current = undefined;
                }, 2400);
            }
        } catch (err) {
            const message = formatUnknownError(err);
            console.error('[PetSettingsPanel] InstallPetPackZip failed:', err);
            setInstallError(message);
        } finally {
            installBusyRef.current = false;
            setInstallBusy(false);
        }
    }, [refreshPackOptions, lang, updatePetConfig]);

    return (
        <div className="pet-settings-panel">
            <div className="settings-panel-header pet-settings-header">
                <div>
                    <h3 className="settings-panel-title">
                        {text(lang, '\u684c\u9762\u5ba0\u7269', '\u684c\u9762\u5bf5\u7269', 'Desktop Pet')}
                    </h3>
                    <p className="settings-panel-desc">
                        {text(
                            lang,
                            '先设置形象、尺寸、动作和音效；语音与高级能力可按需展开。',
                            '先設定形象、尺寸、動作和音效；語音與進階能力可按需展開。',
                            'Start with look, size, motion, and sound. Voice and advanced abilities stay tucked away until needed.'
                        )}
                    </p>
                </div>
                <div className="pet-header-actions">
                    {saveStateLabel && (
                        <span className="pet-save-state" data-state={saveState} role="status" aria-live="polite">{saveStateLabel}</span>
                    )}
                    <button
                        type="button"
                        className="pet-help-button"
                        title={text(lang, '宠物包创建指南', '寵物包建立指南', 'Pet pack creation guide')}
                        aria-label={text(lang, '帮助：宠物包创建指南', '幫助：寵物包建立指南', 'Help: pet pack creation guide')}
                        onClick={openPetPackHelp}
                    >
                        {text(lang, '帮助', '幫助', 'Help')}
                    </button>
                    <label className="pet-switch-row" data-enabled={petEnabled ? 'true' : 'false'}>
                        <input
                            type="checkbox"
                            checked={petEnabled}
                            onChange={(event) => updatePetConfig({ pet_enabled: event.target.checked })}
                        />
                        <span className="pet-switch-track" aria-hidden="true"><span /></span>
                        <span>{text(lang, '\u542f\u7528\u684c\u9762\u5ba0\u7269', '\u555f\u7528\u684c\u9762\u5bf5\u7269', 'Enable Desktop Pet')}</span>
                    </label>
                </div>
            </div>

            <div className="pet-status-strip" role="status" aria-live="polite" aria-label={text(lang, '宠物设置状态', '寵物設定狀態', 'Pet settings status')}>
                <div className="pet-status-chip" data-tone={petEnabled ? 'ready' : 'muted'}>
                    <span className="pet-status-dot" aria-hidden="true" />
                    <div>
                        <strong>{text(lang, '桌面入口', '桌面入口', 'Desktop Entry')}</strong>
                        <span>{petEnabled ? text(lang, '已显示，可点击唤起主窗口', '已顯示，可點擊喚起主視窗', 'Visible and opens the main window') : text(lang, '已关闭，不占用桌面空间', '已關閉，不佔用桌面空間', 'Hidden and out of the way')}</span>
                    </div>
                </div>
                <div className="pet-status-chip" data-tone={voiceReady ? 'ready' : 'warn'}>
                    <span className="pet-status-dot" aria-hidden="true" />
                    <div>
                        <strong>{text(lang, '语音能力', '語音能力', 'Voice Stack')}</strong>
                        <span>{voiceReady ? text(lang, 'ASR/TTS 已就绪', 'ASR/TTS 已就緒', 'ASR/TTS ready') : text(lang, '需要在语音设置中启用 ASR/TTS', '需要在語音設定中啟用 ASR/TTS', 'Enable ASR/TTS in voice settings')}</span>
                    </div>
                </div>
                <div className="pet-status-chip" data-tone={quietMode ? 'quiet' : 'ready'}>
                    <span className="pet-status-dot" aria-hidden="true" />
                    <div>
                        <strong>{text(lang, '打扰级别', '打擾級別', 'Interruption')}</strong>
                        <span>{quietMode ? text(lang, '勿扰中，动作音效会降低存在感', '勿擾中，動作音效會降低存在感', 'Quiet mode reduces motion sound') : text(lang, '按当前交互风格响应', '按目前互動風格回應', 'Responds by the selected style')}</span>
                    </div>
                </div>
            </div>

            <div className="pet-settings-grid">
                <section className="pet-preview-card" aria-label={text(lang, 'MaClaw 宠物预览', 'MaClaw 寵物預覽', 'MaClaw pet preview')}>
                    <div className="pet-section-heading">
                        <strong>{text(lang, '实时预览', '即時預覽', 'Live Preview')}</strong>
                        <span>{text(lang, '检查形象、动作节奏与桌面尺寸。', '檢查形象、動作節奏與桌面尺寸。', 'Check character, motion rhythm, and desktop size.')}</span>
                    </div>
                    <div
                        className="pet-preview-stage"
                        data-pet-skin={selectedSkinOption.id}
                        data-pet-state={previewState}
                        data-interaction-mode={interactionMode}
                        data-motion={motionEnabled ? 'on' : 'off'}
                    >
                        <div
                            className="pet-preview-avatar"
                            style={{ width: petSize * 1.45, height: petSize * 1.45 }}
                            data-pet-variant={selectedVariant}
                        >
                            <img
                                src={stageImage || selectedSkinOption.image}
                                alt={selectedSkinOption.label}
                                className="pet-preview-image"
                            />
                            {/* CSS face overlay only for classic / packs that request it */}
                            {(selectedVariant === 'classic' || selectedSkinOption.faceOverlay) && (
                                <>
                                    <span className="pet-preview-face pet-preview-face--eye-left" aria-hidden="true" />
                                    <span className="pet-preview-face pet-preview-face--eye-right" aria-hidden="true" />
                                    <span className="pet-preview-face pet-preview-face--mouth" aria-hidden="true" />
                                </>
                            )}
                            <span className="pet-preview-motion-mark pet-preview-motion-mark--a" aria-hidden="true" />
                            <span className="pet-preview-motion-mark pet-preview-motion-mark--b" aria-hidden="true" />
                        </div>
                    </div>
                    <div className="pet-preview-state-row" aria-label={text(lang, '宠物动画预览状态', '寵物動畫預覽狀態', 'Pet animation preview state')}>
                        {previewStateOptionIds.map((state) => (
                            <button
                                key={state}
                                type="button"
                                className={previewState === state ? 'active' : ''}
                                aria-pressed={previewState === state}
                                onClick={() => setPreviewState(state)}
                            >
                                {previewStateLabel(lang, state)}
                            </button>
                        ))}
                    </div>
                    <div className="pet-preview-meta">
                        <strong>{selectedSkinOption.label}</strong>
                        <span>
                            {selectedSkinOption.desc || skinPreviewLine(lang, selectedSkinOption.id)}
                            {selectedSkinOption.canUninstall
                                ? text(lang, ' · 用户安装', ' · 使用者安裝', ' · User install')
                                : text(lang, ' · 官方', ' · 官方', ' · Official')}
                            {selectedVariant === 'default'
                                ? text(lang, ' · 具象帧预览', ' · 具象幀預覽', ' · Figurative frame preview')
                                : text(lang, ' · 经典预览', ' · 經典預覽', ' · Classic preview')}
                        </span>
                    </div>
                </section>

                <section className="pet-config-card">
                    <div className="pet-form-section">
                        <div className="pet-section-heading">
                            <strong>{text(lang, '\u5f62\u8c61', '\u5f62\u8c61', 'Skin')}</strong>
                            <span>{text(lang, '每个形象都有明确身份，不只是抽象图标。', '每個形象都有明確身份，不只是抽象圖示。', 'Each skin has a clear role, not just an abstract icon.')}</span>
                        </div>
                        <div className="pet-skin-grid">
                            {packOptions.map((skin) => {
                                const isInvalid = skin.status === 'invalid';
                                return (
                                <button
                                    key={skin.id}
                                    type="button"
                                    className={`pet-skin-option ${selectedSkin === skin.id ? 'active' : ''}${isInvalid ? ' pet-skin-option--invalid' : ''}`}
                                    aria-pressed={selectedSkin === skin.id}
                                    title={isInvalid ? (skin.desc || text(lang, '包无效，可卸载后重装', '包無效，可卸載後重裝', 'Invalid pack — uninstall and reinstall')) : undefined}
                                    onClick={() => updatePetConfig({ pet_skin: skin.id })}
                                >
                                    <img src={skin.image} alt="" className="pet-skin-thumb" aria-hidden="true" />
                                    <span className="pet-skin-title-row">
                                        <span>{skin.label}</span>
                                        <em>{skinToneLabel(lang, skin.tone)}</em>
                                    </span>
                                    <span className="pet-skin-badges">
                                        <span className={`pet-skin-badge ${skin.canUninstall ? 'pet-skin-badge--user' : 'pet-skin-badge--official'}`}>
                                            {skin.canUninstall
                                                ? text(lang, '用户', '使用者', 'User')
                                                : text(lang, '官方', '官方', 'Official')}
                                        </span>
                                        {isInvalid ? (
                                            <span className="pet-skin-badge pet-skin-badge--invalid">
                                                {text(lang, '无效', '無效', 'Invalid')}
                                            </span>
                                        ) : null}
                                        {skin.version ? <span className="pet-skin-badge pet-skin-badge--ver">v{skin.version}</span> : null}
                                    </span>
                                    <small>{skin.desc || skinDescription(lang, skin.id)}</small>
                                </button>
                                );
                            })}
                        </div>
                        {selectedSkinOption.canUninstall && (
                            <div className="pet-pack-uninstall-row pet-section-spacer">
                                <button
                                    type="button"
                                    className="pet-uninstall-button"
                                    disabled={installBusy}
                                    onClick={() => { void uninstallSelectedPack(); }}
                                >
                                    {text(lang, '卸载当前用户包', '卸載目前使用者包', 'Uninstall selected user pack')}
                                </button>
                            </div>
                        )}
                        <div className="pet-section-heading pet-section-heading--inline pet-section-spacer">
                            <label className="form-label">{text(lang, '画质风格', '畫質風格', 'Visual Tier')}</label>
                            <span>{text(lang, 'classic=过程式线稿回落；default=具象光栅帧。', 'classic=過程式線稿回落；default=具象光柵幀。', 'classic=procedural fallback; default=figurative frames.')}</span>
                        </div>
                        <div className="pet-segmented-control">
                            {(['classic', 'default'] as const).map((variant) => (
                                <button
                                    key={variant}
                                    type="button"
                                    className={selectedVariant === variant ? 'active' : ''}
                                    aria-pressed={selectedVariant === variant}
                                    onClick={() => updatePetConfig({
                                        pet_variant: variant,
                                        pet_figurative_upgrade_prompt_pending: false,
                                    })}
                                >
                                    {variant === 'classic'
                                        ? text(lang, '经典', '經典', 'Classic')
                                        : text(lang, '具象', '具象', 'Figurative')}
                                </button>
                            ))}
                        </div>
                        {upgradePrompt && (
                            <div className="pet-status-chip pet-upgrade-chip" data-tone="warn">
                                <div>
                                    <strong>{text(lang, '可升级具象画质', '可升級具象畫質', 'Figurative upgrade available')}</strong>
                                    <span>{text(lang, '现有安装保持经典，不会静默升级。点击「具象」可切换。', '現有安裝保持經典，不會靜默升級。點擊「具象」可切換。', 'Existing installs stay classic until you opt in.')}</span>
                                </div>
                                <button type="button" onClick={() => updatePetConfig({ pet_variant: 'default', pet_figurative_upgrade_prompt_pending: false })}>
                                    {text(lang, '切换到具象', '切換到具象', 'Switch to figurative')}
                                </button>
                            </div>
                        )}
                        <div className="pet-section-heading pet-section-heading--inline pet-section-spacer">
                            <label className="form-label">{text(lang, '安装宠物包', '安裝寵物包', 'Install pack')}</label>
                            <span>{text(lang, '选择本地 pet pack zip（声明式资源，无 JS）。', '選擇本地 pet pack zip（聲明式資源，無 JS）。', 'Pick a local declarative pet pack zip (no JS).')}</span>
                        </div>
                        <div className="pet-pack-install-row" role="group" aria-label={text(lang, '安装宠物包', '安裝寵物包', 'Install pack')}>
                            <button
                                type="button"
                                className="pet-install-button"
                                disabled={installBusy}
                                aria-busy={installBusy}
                                onClick={() => { void installPetPackFromDialog(); }}
                            >
                                {installBusy
                                    ? text(lang, '安装中…', '安裝中…', 'Installing…')
                                    : text(lang, '选择 Zip 安装', '選擇 Zip 安裝', 'Install Zip')}
                            </button>
                            <button
                                type="button"
                                className="pet-help-button pet-help-button--inline"
                                onClick={() => { void openPetPacksFolder(); }}
                                title={packsDirLabel || text(lang, '打开用户宠物包目录', '開啟使用者寵物包目錄', 'Open user pet packs folder')}
                                aria-label={text(lang, '浏览用户宠物包目录', '瀏覽使用者寵物包目錄', 'Browse user pet packs folder')}
                            >
                                {text(lang, '浏览', '瀏覽', 'Browse')}
                            </button>
                            <button
                                type="button"
                                className="pet-help-button pet-help-button--inline"
                                title={text(lang, '打开宠物包创建指南', '開啟寵物包建立指南', 'Open pet pack creation guide')}
                                aria-label={text(lang, '创建指南：宠物包说明', '建立指南：寵物包說明', 'Guide: pet pack docs')}
                                onClick={openPetPackHelp}
                            >
                                {text(lang, '创建指南', '建立指南', 'Guide')}
                            </button>
                        </div>
                        {packsDirLabel ? (
                            <p className="pet-packs-dir-hint" title={packsDirLabel}>
                                {text(lang, '用户包目录：', '使用者包目錄：', 'User packs: ')}
                                <code>{packsDirLabel}</code>
                            </p>
                        ) : null}
                        {installNotice && (
                            <p className="pet-install-notice" role="status">
                                {installNotice}
                            </p>
                        )}
                        {installError && (
                            <p className="pet-install-error" role="alert">
                                {text(lang, '安装失败：', '安裝失敗：', 'Install failed: ')}{installError}
                            </p>
                        )}
                    </div>

                    <div className="pet-form-section">
                        <div className="pet-section-heading pet-section-heading--inline">
                            <label className="form-label" htmlFor="pet-size-range">{text(lang, '\u5c3a\u5bf8', '\u5c3a\u5bf8', 'Size')}</label>
                            <span>{text(lang, '建议 72-96px，兼顾存在感与遮挡。', '建議 72-96px，兼顧存在感與遮擋。', '72-96px is a comfortable desktop range.')}</span>
                        </div>
                        <div className="pet-range-row">
                            <input
                                id="pet-size-range"
                                type="range"
                                min={56}
                                max={120}
                                step={4}
                                value={petSize}
                                aria-valuetext={`${petSize}px`}
                                onChange={(event) => updatePetConfig({ pet_size: clampPetSize(Number(event.target.value)) }, 'pet-size')}
                            />
                            <span>{petSize}px</span>
                        </div>
                    </div>

                    <div className="pet-form-section">
                        <div className="pet-section-heading pet-section-heading--inline">
                            <label className="form-label">{text(lang, '\u4ea4\u4e92\u98ce\u683c', '\u4e92\u52d5\u98a8\u683c', 'Interaction Style')}</label>
                            <span>{text(lang, '控制动作频率与提示积极性。', '控制動作頻率與提示積極性。', 'Controls motion pace and promptiveness.')}</span>
                        </div>
                        <div className="pet-segmented-control">
                            {modeOptionIds.map((mode) => (
                                <button
                                    key={mode}
                                    type="button"
                                    className={interactionMode === mode ? 'active' : ''}
                                    aria-pressed={interactionMode === mode}
                                    onClick={() => updatePetConfig({ pet_interaction_mode: mode })}
                                >
                                    {interactionModeLabel(lang, mode)}
                                </button>
                            ))}
                        </div>
                    </div>

                    <div className="pet-form-section">
                        <div className="pet-section-heading pet-section-heading--inline">
                            <label className="form-label">{text(lang, '动作音效', '動作音效', 'Motion Sound')}</label>
                            <span>{motionSoundPreviewEnabled ? text(lang, '点击即可试听并保存。', '點擊即可試聽並儲存。', 'Click to preview and save.') : text(lang, '勿扰或音效关闭时仅保存选择。', '勿擾或音效關閉時僅儲存選擇。', 'Quiet or SFX off saves without preview.')}</span>
                        </div>
                        <div className={`pet-sound-grid ${motionSoundPreviewEnabled ? '' : 'pet-sound-grid--muted'}`}>
                            {motionSoundPresetOptionIds.map((preset) => (
                                <button
                                    key={preset}
                                    type="button"
                                    className={`pet-sound-option ${motionSoundPreset === preset ? 'active' : ''}`}
                                    aria-label={optionAriaLabel(motionSoundPresetLabel(lang, preset), motionSoundPresetDescription(lang, preset))}
                                    onClick={() => {
                                        updatePetConfig({ pet_motion_sound_preset: preset });
                                        if (motionSoundPreviewEnabled) {
                                            playMotionSoundPresetPreview(preset);
                                        }
                                    }}
                                    aria-pressed={motionSoundPreset === preset}
                                >
                                    <strong>{motionSoundPresetLabel(lang, preset)}</strong>
                                    <span>{motionSoundPresetDescription(lang, preset)}</span>
                                </button>
                            ))}
                        </div>
                    </div>

                    <details className="pet-advanced-card">
                        <summary>
                            <span>
                                <strong>{text(lang, '高级交互', '進階互動', 'Advanced Interaction')}</strong>
                                <small>{text(lang, '语音模式、播报策略和能力开关', '語音模式、播報策略和能力開關', 'Voice modes, readback, and capability toggles')}</small>
                            </span>
                        </summary>

                    <div className="pet-voice-card">
                        <div className="pet-voice-card-header">
                            <div>
                                <strong>{text(lang, '\u8bed\u97f3\u5bf9\u8bdd', '\u8a9e\u97f3\u5c0d\u8a71', 'Voice Conversation')}</strong>
                                <span>{text(lang, '\u590d\u7528\u5f53\u524d ASR/TTS \u80fd\u529b\uff0c\u8ba9\u5ba0\u7269\u80fd\u542c\u3001\u80fd\u8bf4\u3001\u80fd\u7ee7\u7eed\u8ffd\u95ee\u3002', '\u8907\u7528\u76ee\u524d ASR/TTS \u80fd\u529b\uff0c\u8b93\u5bf5\u7269\u80fd\u807d\u3001\u80fd\u8aaa\u3001\u80fd\u7e7c\u7e8c\u8ffd\u554f\u3002', 'Uses existing ASR/TTS so the pet can listen, speak, and continue a turn.')}</span>
                            </div>
                            <div className="pet-capability-badges">
                                <span className={asrReady ? 'ready' : ''} role="img" aria-label={capabilityLabel(lang, 'ASR', asrReady)} title={capabilityLabel(lang, 'ASR', asrReady)}>ASR</span>
                                <span className={ttsReady ? 'ready' : ''} role="img" aria-label={capabilityLabel(lang, 'TTS', ttsReady)} title={capabilityLabel(lang, 'TTS', ttsReady)}>TTS</span>
                            </div>
                        </div>

                        <div className="pet-form-section">
                            <label className="form-label">{text(lang, '\u5bf9\u8bdd\u6a21\u5f0f', '\u5c0d\u8a71\u6a21\u5f0f', 'Conversation Mode')}</label>
                            <div className="pet-segmented-control pet-segmented-control--voice">
                                {conversationModeOptionIds.map((mode) => (
                                    <button
                                        key={mode}
                                        type="button"
                                        className={conversationMode === mode ? 'active' : ''}
                                        aria-pressed={conversationMode === mode}
                                        onClick={() => updatePetConfig({ pet_conversation_mode: mode })}
                                    >
                                        {conversationModeLabel(lang, mode)}
                                    </button>
                                ))}
                            </div>
                        </div>

                        <div className="pet-form-section">
                            <label className="form-label">{text(lang, '\u64ad\u62a5\u7b56\u7565', '\u64ad\u5831\u7b56\u7565', 'Readback')}</label>
                            <div className="pet-segmented-control pet-segmented-control--readback">
                                {readbackModeOptionIds.map((mode) => (
                                    <button
                                        key={mode}
                                        type="button"
                                        className={readbackMode === mode ? 'active' : ''}
                                        aria-pressed={readbackMode === mode}
                                        onClick={() => updatePetConfig({
                                            pet_readback_mode: mode,
                                            pet_voice_readback_enabled: mode !== 'off',
                                        })}
                                    >
                                        {readbackModeLabel(lang, mode)}
                                    </button>
                                ))}
                            </div>
                        </div>

                        <div className="pet-range-row">
                            <label className="form-label" htmlFor="pet-continuous-timeout">{text(lang, '\u8fde\u7eed\u5bf9\u8bdd\u8d85\u65f6', '\u9023\u7e8c\u5c0d\u8a71\u903e\u6642', 'Continuous Timeout')}</label>
                            <span>{continuousTimeout}s</span>
                            <input
                                id="pet-continuous-timeout"
                                type="range"
                                min={5}
                                max={120}
                                step={5}
                                value={continuousTimeout}
                                aria-valuetext={`${continuousTimeout}s`}
                                onChange={(event) => updatePetConfig({ pet_continuous_timeout_sec: Number(event.target.value) }, 'continuous-timeout')}
                            />
                        </div>

                        <label className="pet-toggle-item">
                            <input
                                type="checkbox"
                                checked={!!config.pet_auto_retry_on_no_hear}
                                onChange={(event) => updatePetConfig({ pet_auto_retry_on_no_hear: event.target.checked })}
                            />
                            <span>{text(lang, '\u6ca1\u542c\u6e05\u65f6\u81ea\u52a8\u8ffd\u95ee', '\u6c92\u807d\u6e05\u6642\u81ea\u52d5\u8ffd\u554f', 'Ask again when speech is unclear')}</span>
                        </label>
                    </div>

                    <div className="pet-toggle-grid">
                        {toggleOptions.map(([key, label]) => (
                            <label key={key} className="pet-toggle-item">
                                <input
                                    type="checkbox"
                                    checked={isToggleChecked(key)}
                                    onChange={(event) => {
                                        if (key === 'pet_voice_readback_enabled') {
                                            updatePetConfig({
                                                pet_voice_readback_enabled: event.target.checked,
                                                pet_readback_mode: event.target.checked ? 'summary' : 'off',
                                            });
                                            return;
                                        }
                                        updatePetConfig({ [key]: event.target.checked });
                                    }}
                                />
                                <span>{label}</span>
                            </label>
                        ))}
                    </div>
                    </details>
                </section>
            </div>
        </div>
    );
}

export default PetSettingsPanel;
