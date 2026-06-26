import { useEffect, useRef, useState } from 'react';
import { main } from '../../wailsjs/go/models';
import { defaultPetSize, getPetSkinOption, petSkinOptions } from './petSkins';
import { motionSoundPresetOptionIds, normalizeMotionSoundPreset, type MotionSoundPreset } from './petMotionSounds';

type Lang = 'en' | 'zh-Hans' | 'zh-Hant' | string;
type PetPreviewState = 'idle' | 'listening' | 'thinking' | 'speaking';
type DebouncedFieldKey = 'pet-size' | 'continuous-timeout';
type SaveState = 'idle' | 'pending' | 'saving' | 'saved' | 'error';
type PetToggleKey =
    | 'pet_motion_enabled'
    | 'pet_motion_sound_enabled'
    | 'pet_text_interaction_enabled'
    | 'pet_voice_input_enabled'
    | 'pet_voice_readback_enabled'
    | 'pet_file_drop_enabled'
    | 'pet_quiet_mode';

interface PetSettingsPanelProps {
    config: main.AppConfig;
    lang: Lang;
    setConfig: (config: main.AppConfig) => void;
    patchConfig: (patch: Record<string, unknown>) => Promise<main.AppConfig | void>;
}

const modeOptionIds = ['quiet', 'balanced', 'active'] as const;
const conversationModeOptionIds = ['text-first', 'voice-turn', 'continuous'] as const;
const readbackModeOptionIds = ['off', 'summary', 'full', 'done-only'] as const;
const previewStateOptionIds: PetPreviewState[] = ['idle', 'listening', 'thinking', 'speaking'];
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
    const latestConfigRef = useRef<main.AppConfig>(config);
    const saveTimersRef = useRef<Partial<Record<DebouncedFieldKey, number>>>({});
    const savedTimerRef = useRef<number | undefined>(undefined);
    const mountedRef = useRef(true);
    const saveSeqRef = useRef(0);
    const pendingPatchRef = useRef<Record<string, unknown>>({});
    const petSize = clampPetSize(config.pet_size || defaultPetSize);
    const selectedSkin = config.pet_skin || 'clawmate';
    const interactionMode = config.pet_interaction_mode || 'balanced';
    const motionSoundPreset = normalizeMotionSoundPreset(config.pet_motion_sound_preset);
    const conversationMode = config.pet_conversation_mode || 'text-first';
    const readbackMode = config.pet_readback_mode || (config.pet_voice_readback_enabled ? 'summary' : 'off');
    const continuousTimeout = Math.min(120, Math.max(5, Number(config.pet_continuous_timeout_sec || 30)));
    const asrReady = !!config.asr_enabled;
    const ttsReady = !!config.tts_enabled;
    const petEnabled = !!config.pet_enabled;
    const quietMode = !!config.pet_quiet_mode;
    const voiceReady = asrReady && ttsReady;
    const selectedSkinOption = getPetSkinOption(selectedSkin);
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
            if (hadPendingSave && Object.keys(pendingPatchRef.current).length > 0) {
                void patchConfig(pendingPatchRef.current);
                pendingPatchRef.current = {};
            }
        };
    }, [patchConfig]);

    const persistPetConfig = (patch: Record<string, unknown>, next: main.AppConfig) => {
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
    };

    const clearPendingSaveTimers = () => {
        Object.entries(saveTimersRef.current).forEach(([key, timer]) => {
            if (timer) {
                window.clearTimeout(timer);
                saveTimersRef.current[key as DebouncedFieldKey] = undefined;
            }
        });
    };

    const updatePetConfig = (patch: Record<string, unknown>, debounceKey?: DebouncedFieldKey) => {
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
    };

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
                        <div className="pet-preview-avatar" style={{ width: petSize * 1.45, height: petSize * 1.45 }}>
                            <img src={selectedSkinOption.image} alt={selectedSkinOption.label} className="pet-preview-image" />
                            <span className="pet-preview-face pet-preview-face--eye-left" aria-hidden="true" />
                            <span className="pet-preview-face pet-preview-face--eye-right" aria-hidden="true" />
                            <span className="pet-preview-face pet-preview-face--mouth" aria-hidden="true" />
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
                        <span>{skinPreviewLine(lang, selectedSkinOption.id)}</span>
                    </div>
                </section>

                <section className="pet-config-card">
                    <div className="pet-form-section">
                        <div className="pet-section-heading">
                            <strong>{text(lang, '\u5f62\u8c61', '\u5f62\u8c61', 'Skin')}</strong>
                            <span>{text(lang, '每个形象都有明确身份，不只是抽象图标。', '每個形象都有明確身份，不只是抽象圖示。', 'Each skin has a clear role, not just an abstract icon.')}</span>
                        </div>
                        <div className="pet-skin-grid">
                            {petSkinOptions.map((skin) => (
                                <button
                                    key={skin.id}
                                    type="button"
                                    className={`pet-skin-option ${selectedSkin === skin.id ? 'active' : ''}`}
                                    aria-pressed={selectedSkin === skin.id}
                                    onClick={() => updatePetConfig({ pet_skin: skin.id })}
                                >
                                    <img src={skin.image} alt="" className="pet-skin-thumb" aria-hidden="true" />
                                    <span className="pet-skin-title-row">
                                        <span>{skin.label}</span>
                                        <em>{skinToneLabel(lang, skin.tone)}</em>
                                    </span>
                                    <small>{skinDescription(lang, skin.id)}</small>
                                </button>
                            ))}
                        </div>
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
