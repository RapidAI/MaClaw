import { useEffect, useRef, useState } from 'react';
import { main } from '../../wailsjs/go/models';
import { getPetSkinOption, petSkinOptions } from './petSkins';

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

const modeOptions = [
    { id: 'quiet', label: 'Quiet' },
    { id: 'balanced', label: 'Balanced' },
    { id: 'active', label: 'Active' },
];

const conversationModeOptions = [
    { id: 'text-first', label: 'Text First' },
    { id: 'voice-turn', label: 'Voice Turn' },
    { id: 'continuous', label: 'Continuous' },
];

const readbackModeOptions = [
    { id: 'off', label: 'Off' },
    { id: 'summary', label: 'Summary' },
    { id: 'full', label: 'Full' },
    { id: 'done-only', label: 'Done Only' },
];

const previewStateOptions: Array<{ id: PetPreviewState; label: string }> = [
    { id: 'idle', label: 'Idle' },
    { id: 'listening', label: 'Listen' },
    { id: 'thinking', label: 'Think' },
    { id: 'speaking', label: 'Speak' },
];

function text(lang: Lang, zhHans: string, zhHant: string, en: string): string {
    if (lang === 'zh-Hans') return zhHans;
    if (lang === 'zh-Hant') return zhHant;
    return en;
}

function clampPetSize(value: number): number {
    if (!Number.isFinite(value)) return 72;
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
    const petSize = clampPetSize((config as any).pet_size || 72);
    const selectedSkin = (config as any).pet_skin || 'clawmate';
    const interactionMode = (config as any).pet_interaction_mode || 'balanced';
    const conversationMode = (config as any).pet_conversation_mode || 'text-first';
    const readbackMode = (config as any).pet_readback_mode || ((config as any).pet_voice_readback_enabled ? 'summary' : 'off');
    const continuousTimeout = Math.min(120, Math.max(5, Number((config as any).pet_continuous_timeout_sec || 30)));
    const asrReady = !!(config as any).asr_enabled;
    const ttsReady = !!(config as any).tts_enabled;
    const selectedSkinOption = getPetSkinOption(selectedSkin);
    const motionEnabled = (config as any).pet_motion_enabled !== false;
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
                            '\u628a MaClaw \u6d6e\u52a8\u6309\u94ae\u5347\u7ea7\u4e3a\u53ef\u6362\u5f62\u8c61\u3001\u53ef\u52a8\u4f5c\u3001\u53ef\u6587\u5b57/\u8bed\u97f3\u4ea4\u6d41\u7684\u684c\u9762\u4f19\u4f34\u3002',
                            '\u628a MaClaw \u6d6e\u52d5\u6309\u9215\u5347\u7d1a\u70ba\u53ef\u63db\u5f62\u8c61\u3001\u53ef\u52d5\u4f5c\u3001\u53ef\u6587\u5b57/\u8a9e\u97f3\u4ea4\u6d41\u7684\u684c\u9762\u5925\u4f34\u3002',
                            'Turn the MaClaw floating entry into a skinnable desktop companion with motion, chat, and voice.'
                        )}
                    </p>
                </div>
                <div className="pet-header-actions">
                    {saveStateLabel && (
                        <span className="pet-save-state" data-state={saveState}>{saveStateLabel}</span>
                    )}
                    <label className="pet-switch-row">
                        <input
                            type="checkbox"
                            checked={!!(config as any).pet_enabled}
                            onChange={(event) => updatePetConfig({ pet_enabled: event.target.checked })}
                        />
                        <span>{text(lang, '\u542f\u7528\u5ba0\u7269\u5165\u53e3', '\u555f\u7528\u5bf5\u7269\u5165\u53e3', 'Enable Pet Entry')}</span>
                    </label>
                </div>
            </div>

            <div className="pet-settings-grid">
                <section className="pet-preview-card" aria-label="MaClaw pet preview">
                    <div
                        className="pet-preview-stage"
                        data-pet-skin={selectedSkinOption.id}
                        data-pet-state={previewState}
                        data-interaction-mode={interactionMode}
                        data-motion={motionEnabled ? 'on' : 'off'}
                    >
                        <img src={selectedSkinOption.image} alt={selectedSkinOption.label} className="pet-preview-image" style={{ width: petSize * 1.45, height: petSize * 1.45 }} />
                    </div>
                    <div className="pet-preview-state-row" aria-label="Pet animation preview state">
                        {previewStateOptions.map((state) => (
                            <button
                                key={state.id}
                                type="button"
                                className={previewState === state.id ? 'active' : ''}
                                onClick={() => setPreviewState(state.id)}
                            >
                                {state.label}
                            </button>
                        ))}
                    </div>
                    <div className="pet-preview-meta">
                        <strong>{selectedSkinOption.label}</strong>
                        <span>{selectedSkinOption.previewLine}</span>
                    </div>
                </section>

                <section className="pet-config-card">
                    <div className="pet-form-section">
                        <label className="form-label">{text(lang, '\u5f62\u8c61', '\u5f62\u8c61', 'Skin')}</label>
                        <div className="pet-skin-grid">
                            {petSkinOptions.map((skin) => (
                                <button
                                    key={skin.id}
                                    type="button"
                                    className={`pet-skin-option ${selectedSkin === skin.id ? 'active' : ''}`}
                                    onClick={() => updatePetConfig({ pet_skin: skin.id })}
                                >
                                    <img src={skin.image} alt="" className="pet-skin-thumb" aria-hidden="true" />
                                    <span className="pet-skin-title-row">
                                        <span>{skin.label}</span>
                                        <em>{skin.tone}</em>
                                    </span>
                                    <small>{skin.desc}</small>
                                </button>
                            ))}
                        </div>
                    </div>

                    <div className="pet-form-section">
                        <label className="form-label" htmlFor="pet-size-range">{text(lang, '\u5c3a\u5bf8', '\u5c3a\u5bf8', 'Size')}</label>
                        <div className="pet-range-row">
                            <input
                                id="pet-size-range"
                                type="range"
                                min={56}
                                max={120}
                                step={4}
                                value={petSize}
                                onChange={(event) => updatePetConfig({ pet_size: clampPetSize(Number(event.target.value)) }, 'pet-size')}
                            />
                            <span>{petSize}px</span>
                        </div>
                    </div>

                    <div className="pet-form-section">
                        <label className="form-label">{text(lang, '\u4ea4\u4e92\u98ce\u683c', '\u4e92\u52d5\u98a8\u683c', 'Interaction Style')}</label>
                        <div className="pet-segmented-control">
                            {modeOptions.map((mode) => (
                                <button
                                    key={mode.id}
                                    type="button"
                                    className={interactionMode === mode.id ? 'active' : ''}
                                    onClick={() => updatePetConfig({ pet_interaction_mode: mode.id })}
                                >
                                    {mode.label}
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
                                <span className={asrReady ? 'ready' : ''}>ASR</span>
                                <span className={ttsReady ? 'ready' : ''}>TTS</span>
                            </div>
                        </div>

                        <div className="pet-form-section">
                            <label className="form-label">{text(lang, '\u5bf9\u8bdd\u6a21\u5f0f', '\u5c0d\u8a71\u6a21\u5f0f', 'Conversation Mode')}</label>
                            <div className="pet-segmented-control pet-segmented-control--voice">
                                {conversationModeOptions.map((mode) => (
                                    <button
                                        key={mode.id}
                                        type="button"
                                        className={conversationMode === mode.id ? 'active' : ''}
                                        onClick={() => updatePetConfig({ pet_conversation_mode: mode.id })}
                                    >
                                        {mode.label}
                                    </button>
                                ))}
                            </div>
                        </div>

                        <div className="pet-form-section">
                            <label className="form-label">{text(lang, '\u64ad\u62a5\u7b56\u7565', '\u64ad\u5831\u7b56\u7565', 'Readback')}</label>
                            <div className="pet-segmented-control pet-segmented-control--readback">
                                {readbackModeOptions.map((mode) => (
                                    <button
                                        key={mode.id}
                                        type="button"
                                        className={readbackMode === mode.id ? 'active' : ''}
                                        onClick={() => updatePetConfig({
                                            pet_readback_mode: mode.id,
                                            pet_voice_readback_enabled: mode.id !== 'off',
                                        })}
                                    >
                                        {mode.label}
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
                        {[
                            ['pet_motion_enabled', text(lang, '\u52a8\u4f5c\u52a8\u753b', '\u52d5\u4f5c\u52d5\u756b', 'Motion')],
                            ['pet_text_interaction_enabled', text(lang, '\u6587\u5b57\u4ea4\u6d41', '\u6587\u5b57\u4ea4\u6d41', 'Text Chat')],
                            ['pet_voice_input_enabled', text(lang, '\u8bed\u97f3\u8f93\u5165', '\u8a9e\u97f3\u8f38\u5165', 'Voice Input')],
                            ['pet_voice_readback_enabled', text(lang, '\u8bed\u97f3\u64ad\u62a5', '\u8a9e\u97f3\u64ad\u5831', 'Voice Readback')],
                            ['pet_file_drop_enabled', text(lang, '\u6587\u4ef6\u62d6\u62fd', '\u6587\u4ef6\u62d6\u66f3', 'File Drop')],
                            ['pet_quiet_mode', text(lang, '\u52ff\u6270\u6a21\u5f0f', '\u52ff\u64fe\u6a21\u5f0f', 'Do Not Disturb')],
                        ].map(([key, label]) => (
                            <label key={key} className="pet-toggle-item">
                                <input
                                    type="checkbox"
                                    checked={key === 'pet_motion_enabled' || key === 'pet_text_interaction_enabled' || key === 'pet_file_drop_enabled'
                                        ? (config as any)[key] !== false
                                        : !!(config as any)[key]}
                                    onChange={(event) => updatePetConfig({ [key]: event.target.checked })}
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
