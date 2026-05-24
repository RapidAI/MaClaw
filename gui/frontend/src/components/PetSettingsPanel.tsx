import { useEffect, useRef, useState } from 'react';
import { main } from '../../wailsjs/go/models';
import { defaultPetSize, getPetSkinOption, petSkinOptions } from './petSkins';
import { motionSoundPresetOptionIds, normalizeMotionSoundPreset, type MotionSoundPreset } from './petMotionSounds';

type Lang = 'en' | 'zh-Hans' | 'zh-Hant' | string;
type PetPreviewState = 'idle' | 'listening' | 'thinking' | 'speaking';
type DebouncedFieldKey = 'pet-size' | 'continuous-timeout';
type SaveState = 'idle' | 'pending' | 'saving' | 'saved' | 'error';

interface PetSettingsPanelProps {
    config: main.AppConfig;
    lang: Lang;
    setConfig: (config: main.AppConfig) => void;
    saveConfig: (config: main.AppConfig) => Promise<void>;
}

const modeOptionIds = ['quiet', 'balanced', 'active'] as const;
const conversationModeOptionIds = ['text-first', 'voice-turn', 'continuous'] as const;
const readbackModeOptionIds = ['off', 'summary', 'full', 'done-only'] as const;
const previewStateOptionIds: PetPreviewState[] = ['idle', 'listening', 'thinking', 'speaking'];
const defaultEnabledToggleKeys = new Set(['pet_motion_enabled', 'pet_motion_sound_enabled', 'pet_text_interaction_enabled', 'pet_file_drop_enabled']);

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
            return text(lang, '轻快弹跳，适合 mini 形象。', '輕快彈跳，適合 mini 形象。', 'Bouncy and playful.');
        case 'chime':
            return text(lang, '清脆、有尾音，像提示铃。', '清脆、有尾音，像提示鈴。', 'Bright with a small ring tail.');
        case 'synth':
            return text(lang, '更电子、更利落，适合开发者风格。', '更電子、更俐落，適合開發者風格。', 'Sharper electronic texture.');
        case 'soft':
            return text(lang, '低存在感，适合专注或夜间。', '低存在感，適合專注或夜間。', 'Gentle and low-distraction.');
        case 'classic':
        default:
            return text(lang, '当前默认漫画动作音效。', '目前預設漫畫動作音效。', 'The current comic motion sound.');
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
        const output = ctx.createGain();
        output.gain.setValueAtTime(0.0001, now);
        output.gain.exponentialRampToValueAtTime(preset === 'soft' ? 0.016 : 0.024, now + 0.012);
        output.gain.exponentialRampToValueAtTime(0.0001, now + 0.18);
        output.connect(ctx.destination);

        const filter = ctx.createBiquadFilter();
        filter.type = preset === 'soft' ? 'lowpass' : 'bandpass';
        filter.frequency.setValueAtTime(preset === 'chime' ? 4200 : preset === 'synth' ? 1800 : preset === 'soft' ? 1400 : 3200, now);
        filter.Q.setValueAtTime(preset === 'bubble' ? 0.55 : 0.9, now);
        filter.connect(output);

        const tones: Array<[number, number, OscillatorType]> = preset === 'bubble'
            ? [[620, 0, 'sine'], [980, 0.045, 'triangle']]
            : preset === 'chime'
                ? [[1047, 0, 'sine'], [1568, 0.065, 'sine']]
                : preset === 'synth'
                    ? [[740, 0, 'square'], [520, 0.04, 'sawtooth']]
                    : preset === 'soft'
                        ? [[440, 0, 'sine'], [660, 0.07, 'triangle']]
                        : [[760, 0, 'sine'], [1120, 0.045, 'triangle']];

        tones.forEach(([hz, delay, type]) => {
            const osc = ctx.createOscillator();
            osc.type = type;
            osc.frequency.setValueAtTime(hz, now + delay);
            osc.connect(filter);
            osc.start(now + delay);
            osc.stop(now + delay + (preset === 'chime' || preset === 'soft' ? 0.18 : 0.11));
        });

        window.setTimeout(() => {
            output.disconnect();
            filter.disconnect();
            void ctx.close().catch(() => undefined);
        }, 320);
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
            return text(lang, '桌面宠物的轻量小伙伴形象', '桌面寵物的輕量小夥伴形象', 'Minimal desktop pet companion');
        case 'dev-claw':
            return text(lang, '面向开发场景的伙伴形象', '面向開發場景的夥伴形象', 'Developer-focused companion style');
        case 'focus-claw':
            return text(lang, '低打扰的安静桌面陪伴', '低打擾的安靜桌面陪伴', 'Quiet low-distraction desktop presence');
        case 'clawmate':
        default:
            return text(lang, '默认 MaClaw 爪爪伙伴', '預設 MaClaw 爪爪夥伴', 'Default MaClaw claw companion');
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

export function PetSettingsPanel({ config, lang, setConfig, saveConfig }: PetSettingsPanelProps) {
    const [previewState, setPreviewState] = useState<PetPreviewState>('idle');
    const [saveState, setSaveState] = useState<SaveState>('idle');
    const latestConfigRef = useRef<main.AppConfig>(config);
    const saveTimersRef = useRef<Partial<Record<DebouncedFieldKey, number>>>({});
    const savedTimerRef = useRef<number | undefined>(undefined);
    const mountedRef = useRef(true);
    const saveSeqRef = useRef(0);
    const petSize = clampPetSize((config as any).pet_size || defaultPetSize);
    const selectedSkin = (config as any).pet_skin || 'clawmate';
    const interactionMode = (config as any).pet_interaction_mode || 'balanced';
    const motionSoundPreset = normalizeMotionSoundPreset((config as any).pet_motion_sound_preset);
    const conversationMode = (config as any).pet_conversation_mode || 'text-first';
    const readbackMode = (config as any).pet_readback_mode || ((config as any).pet_voice_readback_enabled ? 'summary' : 'off');
    const continuousTimeout = Math.min(120, Math.max(5, Number((config as any).pet_continuous_timeout_sec || 30)));
    const asrReady = !!(config as any).asr_enabled;
    const ttsReady = !!(config as any).tts_enabled;
    const petEnabled = !!(config as any).pet_enabled;
    const quietMode = !!(config as any).pet_quiet_mode;
    const voiceReady = asrReady && ttsReady;
    const selectedSkinOption = getPetSkinOption(selectedSkin);
    const motionEnabled = (config as any).pet_motion_enabled !== false;
    const motionSoundPreviewEnabled = (config as any).pet_motion_sound_enabled !== false && !(config as any).pet_quiet_mode;
    const toggleOptions = [
        ['pet_motion_enabled', text(lang, '\u52a8\u4f5c\u52a8\u753b', '\u52d5\u4f5c\u52d5\u756b', 'Motion')],
        ['pet_motion_sound_enabled', text(lang, '\u52a8\u4f5c\u97f3\u6548', '\u52d5\u4f5c\u97f3\u6548', 'Motion SFX')],
        ['pet_text_interaction_enabled', text(lang, '\u6587\u5b57\u4ea4\u6d41', '\u6587\u5b57\u4ea4\u6d41', 'Text Chat')],
        ['pet_voice_input_enabled', text(lang, '\u8bed\u97f3\u8f93\u5165', '\u8a9e\u97f3\u8f38\u5165', 'Voice Input')],
        ['pet_voice_readback_enabled', text(lang, '\u8bed\u97f3\u64ad\u62a5', '\u8a9e\u97f3\u64ad\u5831', 'Voice Readback')],
        ['pet_file_drop_enabled', text(lang, '\u6587\u4ef6\u62d6\u62fd', '\u6587\u4ef6\u62d6\u66f3', 'File Drop')],
        ['pet_quiet_mode', text(lang, '\u52ff\u6270\u6a21\u5f0f', '\u52ff\u64fe\u6a21\u5f0f', 'Do Not Disturb')],
    ] as const;
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
            if (hadPendingSave) void saveConfig(latestConfigRef.current);
        };
    }, [saveConfig]);

    const persistPetConfig = (next: main.AppConfig) => {
        if (savedTimerRef.current) window.clearTimeout(savedTimerRef.current);
        const saveSeq = ++saveSeqRef.current;
        setSaveState('saving');
        void saveConfig(next)
            .then(() => {
                if (!mountedRef.current || saveSeq !== saveSeqRef.current) return;
                setSaveState('saved');
                savedTimerRef.current = window.setTimeout(() => {
                    if (mountedRef.current && saveSeq === saveSeqRef.current) setSaveState('idle');
                }, 1400);
            })
            .catch((err) => {
                console.error('[PetSettingsPanel] SaveConfig failed:', err);
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
        if (debounceKey) {
            clearPendingSaveTimers();
            setSaveState('pending');
            saveTimersRef.current[debounceKey] = window.setTimeout(() => {
                saveTimersRef.current[debounceKey] = undefined;
                persistPetConfig(latestConfigRef.current);
            }, 350);
            return;
        }
        clearPendingSaveTimers();
        persistPetConfig(next);
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
                            '\u5728 MaClaw \u684c\u9762\u5ba0\u7269\u4e2d\u7edf\u4e00\u7ba1\u7406\u5165\u53e3\u3001\u5f62\u8c61\u3001\u52a8\u4f5c\u3001\u6587\u5b57/\u8bed\u97f3\u4ea4\u6d41\u548c\u97f3\u6548\u3002',
                            '\u5728 MaClaw \u684c\u9762\u5bf5\u7269\u4e2d\u7d71\u4e00\u7ba1\u7406\u5165\u53e3\u3001\u5f62\u8c61\u3001\u52d5\u4f5c\u3001\u6587\u5b57/\u8a9e\u97f3\u4ea4\u6d41\u548c\u97f3\u6548\u3002',
                            'Manage the MaClaw desktop pet entry, skins, motion, chat, voice, and SFX in one place.'
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
                        <span>{text(lang, '切换状态可检查动作节奏与尺寸。', '切換狀態可檢查動作節奏與尺寸。', 'Switch states to check motion and size.')}</span>
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
                            <span>{text(lang, '选择宠物外观与默认行为气质。', '選擇寵物外觀與預設行為氣質。', 'Choose the pet look and baseline behavior.')}</span>
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
                                checked={!!(config as any).pet_auto_retry_on_no_hear}
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
                                    checked={defaultEnabledToggleKeys.has(key)
                                        ? (config as any)[key] !== false
                                        : !!(config as any)[key]}
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
                </section>
            </div>
        </div>
    );
}

export default PetSettingsPanel;
