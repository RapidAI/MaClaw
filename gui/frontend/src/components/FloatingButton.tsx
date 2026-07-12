/**
 * FloatingButton.tsx - MaClaw desktop pet component rendered in the companion window.
 *
 * Supports click-to-open, drag positioning, animated pet states, motion SFX,
 * and a minimal context menu for settings and quitting the app.
 */

import { useState, useRef, useCallback, useEffect } from 'react';
import type { AnimationEvent as ReactAnimationEvent, MouseEvent as ReactMouseEvent } from 'react';
import { EventsOff, EventsOn } from '../../wailsjs/runtime';
import './FloatingButton.css';
import logoSrc from '../assets/images/maclaw-agent-mark.svg';
import { defaultPetSize, defaultPetSkinId, getPetSkinOption, normalizePetSkinId, type PetSkinId } from './petSkins';
import { normalizeMotionSoundPreset, type MotionSoundPreset } from './petMotionSounds';

// Wails Go binding bridge
// The floating window's WebView will have Wails bindings injected.
// Safe fallbacks keep the component renderable before the Go backend is ready.

declare global {
    interface Window {
        go?: {
            main?: {
                App?: {
                    OnFloatingButtonClicked(): Promise<void>;
                    OnFloatingButtonDragged(x: number, y: number): Promise<void>;
                    OpenPetSettingsFromMenu(): Promise<void>;
                    QuitApp(): Promise<void>;
                    LoadConfig(): Promise<Record<string, unknown>>;
                    PatchConfigFields(patch: Record<string, unknown>): Promise<Record<string, unknown>>;
                };
            };
        };
    }
}

function callGoBinding(method: 'OnFloatingButtonClicked'): void;
function callGoBinding(method: 'OnFloatingButtonDragged', x: number, y: number): void;
function callGoBinding(method: 'OpenPetSettingsFromMenu'): void;
function callGoBinding(method: 'QuitApp'): void;
function callGoBinding(method: string, ...args: unknown[]): void {
    try {
        const app = window.go?.main?.App;
        if (app && typeof (app as Record<string, unknown>)[method] === 'function') {
            (app as Record<string, (...a: unknown[]) => unknown>)[method](...args);
        } else {
            console.warn(`[FloatingButton] Go binding not available: ${method}`);
        }
    } catch (err) {
        console.error(`[FloatingButton] Error calling ${method}:`, err);
    }
}

// Drag threshold (pixels)
const DRAG_THRESHOLD = 5;
const PET_CONFIG_FALLBACK_POLL_MS = 60000;

type PetState = 'idle' | 'listening' | 'thinking' | 'speaking';
type PetInteractionMode = 'quiet' | 'balanced' | 'active';
type MotionSoundVariant = 'step' | 'accent' | 'flutter';
type PetGesture = 'none' | 'wake' | 'drag' | 'menu';

type PetStateEvent = {
    state?: PetState;
    source?: string;
    ttlMs?: number;
};

function subscribeRuntimeEvent(eventName: string, handler: (...args: any[]) => void): (() => void) | undefined {
    try {
        const unsubscribe = EventsOn(eventName, handler);
        return typeof unsubscribe === 'function' ? unsubscribe : () => EventsOff(eventName);
    } catch (err) {
        console.warn(`[FloatingButton] Runtime event unavailable: ${eventName}`, err);
        return undefined;
    }
}

function isAsrSource(source: unknown): boolean {
    return typeof source === 'string' && source.startsWith('asr:');
}

function clampContextMenuPosition(x: number, y: number): { x: number; y: number } {
    const menuWidth = 152;
    const menuHeight = 116;
    const maxX = Math.max(0, window.innerWidth - menuWidth - 4);
    const maxY = Math.max(0, window.innerHeight - menuHeight - 4);
    return {
        x: Math.max(4, Math.min(x, maxX)),
        y: Math.max(4, Math.min(y, maxY)),
    };
}

interface FloatingPetConfig {
    petEnabled: boolean;
    petSkin: PetSkinId;
    petSize: number;
    motionEnabled: boolean;
    motionSoundEnabled: boolean;
    motionSoundPreset: MotionSoundPreset;
    quietMode: boolean;
    interactionMode: PetInteractionMode;
}

const defaultPetConfig: FloatingPetConfig = {
    petEnabled: false,
    petSkin: defaultPetSkinId,
    petSize: defaultPetSize,
    motionEnabled: true,
    motionSoundEnabled: true,
    motionSoundPreset: 'classic',
    quietMode: false,
    interactionMode: 'balanced',
};

function clampPetSize(value: unknown): number {
    const n = Number(value);
    if (!Number.isFinite(n)) return defaultPetSize;
    return Math.min(120, Math.max(56, Math.round(n)));
}

function normalizeInteractionMode(value: unknown): PetInteractionMode {
    return value === 'quiet' || value === 'active' || value === 'balanced'
        ? value
        : 'balanced';
}

function isPetState(value: unknown): value is PetState {
    return value === 'idle' || value === 'listening' || value === 'thinking' || value === 'speaking';
}

function isMotionSoundAllowed(config: FloatingPetConfig): boolean {
    return config.petEnabled && !config.quietMode && config.motionEnabled && config.motionSoundEnabled;
}

function readPetConfig(cfg: Record<string, unknown>): FloatingPetConfig {
    return {
        petEnabled: !!cfg.pet_enabled,
        petSkin: normalizePetSkinId(cfg.pet_skin),
        petSize: clampPetSize(cfg.pet_size),
        motionEnabled: cfg.pet_motion_enabled !== false,
        motionSoundEnabled: cfg.pet_motion_sound_enabled !== false,
        motionSoundPreset: normalizeMotionSoundPreset(cfg.pet_motion_sound_preset),
        quietMode: !!cfg.pet_quiet_mode,
        interactionMode: normalizeInteractionMode(cfg.pet_interaction_mode),
    };
}

function arePetConfigsEqual(a: FloatingPetConfig, b: FloatingPetConfig): boolean {
    return a.petEnabled === b.petEnabled
        && a.petSkin === b.petSkin
        && a.petSize === b.petSize
        && a.motionEnabled === b.motionEnabled
        && a.motionSoundEnabled === b.motionSoundEnabled
        && a.motionSoundPreset === b.motionSoundPreset
        && a.quietMode === b.quietMode
        && a.interactionMode === b.interactionMode;
}

async function savePetMotionSoundEnabled(enabled: boolean): Promise<FloatingPetConfig> {
    const app = window.go?.main?.App;
    if (!app?.LoadConfig || !app?.PatchConfigFields) {
        throw new Error('Go config bindings not available');
    }
    const cfg = await app.LoadConfig();
    const nextConfig = { ...cfg, pet_motion_sound_enabled: enabled };
    const savedConfig = await app.PatchConfigFields({ pet_motion_sound_enabled: enabled });
    return readPetConfig(savedConfig ?? nextConfig);
}

type MotionSoundProfile = {
    firstType: OscillatorType;
    secondType: OscillatorType;
    baseHz: number;
    glideHz: number;
    accentHz: number;
    peakGain: number;
    tailSeconds: number;
    filterHz: number;
    delaySeconds: number;
    delayGain: number;
    noiseGain: number;
    pitchDrift: number;
    filterDrift: number;
    accentDelay: number;
    compressorRatio: number;
};

function disconnectAudioNode(node: AudioNode | undefined): void {
    try {
        node?.disconnect();
    } catch {
        // The node may already have been disconnected by a previous cleanup path.
    }
}

function motionSoundGapMs(config: FloatingPetConfig, state: PetState): number {
    if (config.interactionMode === 'active') {
        return state === 'speaking' ? 720 : 900;
    }
    if (state === 'speaking' || state === 'listening') {
        return 1200;
    }
    if (config.petSkin === 'focus-claw') {
        return 1900;
    }
    return 1500;
}

function motionCycleMs(config: FloatingPetConfig, state: PetState): number {
    const base = state === 'speaking' ? 950 : state === 'thinking' ? 1800 : state === 'listening' ? 1200 : 3800;
    let duration = base;
    if (config.petSkin === 'mini-claw') {
        if (state === 'idle') duration = 2600;
        if (state === 'speaking') duration = 720;
    } else if (config.petSkin === 'dev-claw') {
        if (state === 'listening') duration = 1000;
        if (state === 'thinking') duration = 1100;
    } else if (config.petSkin === 'focus-claw') {
        if (state === 'idle') duration = 5200;
        if (state === 'thinking' || state === 'speaking') duration = 1800;
    }
    if (config.interactionMode === 'active') {
        return state === 'speaking' ? 720 : state === 'thinking' ? 1050 : state === 'listening' ? 850 : 2600;
    }
    if (config.interactionMode === 'quiet') {
        return state === 'speaking' ? 1600 : state === 'thinking' ? 2800 : state === 'listening' ? 2000 : 6000;
    }
    return duration;
}

function getMotionSoundProfile(config: FloatingPetConfig, state: PetState, variant: MotionSoundVariant): MotionSoundProfile {
    const skinPitch = config.petSkin === 'mini-claw' ? 1.2 : config.petSkin === 'dev-claw' ? 0.92 : config.petSkin === 'focus-claw' ? 0.78 : 1;
    const statePitch = state === 'listening' ? 1.08 : state === 'thinking' ? 0.94 : state === 'speaking' ? 1.16 : 1;
    const modePitch = config.interactionMode === 'active' ? 1.18 : 1;
    const pitch = skinPitch * statePitch * modePitch;
    const variantPitch = variant === 'accent' ? 1.18 : variant === 'flutter' ? 1.32 : 1;
    const variantGain = variant === 'accent' ? 0.82 : variant === 'flutter' ? 0.58 : 1;
    const variantTail = variant === 'flutter' ? 0.72 : 1;
    const base = (state === 'thinking' ? 660 : state === 'listening' ? 820 : state === 'speaking' ? 920 : 760) * variantPitch;

    let profile: MotionSoundProfile;
    if (config.petSkin === 'dev-claw') {
        profile = {
            firstType: 'square',
            secondType: 'sawtooth',
            baseHz: base * pitch,
            glideHz: (base + 260) * pitch,
            accentHz: (base + 540) * pitch,
            peakGain: (config.interactionMode === 'active' ? 0.018 : 0.013) * variantGain,
            tailSeconds: (state === 'thinking' ? 0.18 : 0.15) * variantTail,
            filterHz: 2100,
            delaySeconds: 0.035,
            delayGain: 0.07,
            noiseGain: 0.0025 * variantGain,
            pitchDrift: 0.018,
            filterDrift: 0.045,
            accentDelay: 0.04,
            compressorRatio: 5,
        };
    } else if (config.petSkin === 'mini-claw') {
        profile = {
            firstType: 'triangle',
            secondType: 'sine',
            baseHz: base * pitch,
            glideHz: (base + 360) * pitch,
            accentHz: (base + 690) * pitch,
            peakGain: (config.interactionMode === 'active' ? 0.02 : 0.015) * variantGain,
            tailSeconds: (state === 'speaking' ? 0.15 : 0.13) * variantTail,
            filterHz: 3400,
            delaySeconds: 0.028,
            delayGain: 0.065,
            noiseGain: 0.0014 * variantGain,
            pitchDrift: 0.018,
            filterDrift: 0.05,
            accentDelay: 0.034,
            compressorRatio: 4,
        };
    } else if (config.petSkin === 'focus-claw') {
        profile = {
            firstType: 'sine',
            secondType: 'triangle',
            baseHz: base * pitch,
            glideHz: (base + 140) * pitch,
            accentHz: (base + 300) * pitch,
            peakGain: 0.010 * variantGain,
            tailSeconds: 0.22 * variantTail,
            filterHz: 1300,
            delaySeconds: 0.045,
            delayGain: 0.045,
            noiseGain: 0.0007 * variantGain,
            pitchDrift: 0.012,
            filterDrift: 0.03,
            accentDelay: 0.045,
            compressorRatio: 3,
        };
    } else {
        profile = {
            firstType: 'sine',
            secondType: 'triangle',
            baseHz: base * pitch,
            glideHz: (base + 220) * pitch,
            accentHz: (base + 460) * pitch,
            peakGain: (config.interactionMode === 'active' ? 0.019 : 0.014) * variantGain,
            tailSeconds: (state === 'speaking' ? 0.17 : 0.14) * variantTail,
            filterHz: 2600,
            delaySeconds: 0.032,
            delayGain: 0.055,
            noiseGain: 0.0012 * variantGain,
            pitchDrift: 0.018,
            filterDrift: 0.045,
            accentDelay: 0.034,
            compressorRatio: 4,
        };
    }

    switch (config.motionSoundPreset) {
        case 'bubble':
            return {
                ...profile,
                firstType: 'sine',
                secondType: 'triangle',
                baseHz: profile.baseHz * 0.82,
                glideHz: profile.glideHz * 1.18,
                accentHz: profile.accentHz * 1.12,
                peakGain: profile.peakGain * 0.78,
                tailSeconds: profile.tailSeconds * 1.25,
                filterHz: Math.min(4200, profile.filterHz * 1.25),
                delaySeconds: 0.052,
                delayGain: profile.delayGain * 1.25,
                noiseGain: profile.noiseGain * 0.35,
                pitchDrift: 0.026,
                filterDrift: 0.07,
                accentDelay: 0.052,
            };
        case 'chime':
            return {
                ...profile,
                firstType: 'sine',
                secondType: 'sine',
                baseHz: profile.baseHz * 1.26,
                glideHz: profile.glideHz * 1.42,
                accentHz: profile.accentHz * 1.72,
                peakGain: profile.peakGain * 0.62,
                tailSeconds: profile.tailSeconds * 1.7,
                filterHz: Math.min(5200, profile.filterHz * 1.42),
                delaySeconds: 0.07,
                delayGain: profile.delayGain * 1.55,
                noiseGain: profile.noiseGain * 0.18,
                pitchDrift: 0.01,
                filterDrift: 0.03,
                accentDelay: 0.065,
            };
        case 'synth':
            return {
                ...profile,
                firstType: 'square',
                secondType: 'triangle',
                baseHz: profile.baseHz * 0.92,
                glideHz: profile.glideHz * 1.06,
                accentHz: profile.accentHz * 0.98,
                peakGain: profile.peakGain * 0.76,
                tailSeconds: profile.tailSeconds * 1.02,
                filterHz: Math.max(1500, profile.filterHz * 0.7),
                delaySeconds: 0.026,
                delayGain: profile.delayGain * 0.65,
                noiseGain: profile.noiseGain * 0.9,
                pitchDrift: 0.014,
                filterDrift: 0.05,
                accentDelay: 0.026,
            };
        case 'soft':
            return {
                ...profile,
                firstType: 'sine',
                secondType: 'triangle',
                baseHz: profile.baseHz * 0.72,
                glideHz: profile.glideHz * 0.78,
                accentHz: profile.accentHz * 0.82,
                peakGain: profile.peakGain * 0.48,
                tailSeconds: profile.tailSeconds * 1.8,
                filterHz: Math.max(1100, profile.filterHz * 0.62),
                delaySeconds: 0.06,
                delayGain: profile.delayGain * 0.95,
                noiseGain: profile.noiseGain * 0.12,
                pitchDrift: 0.008,
                filterDrift: 0.02,
                accentDelay: 0.052,
            };
        case 'classic':
        default:
            return profile;
    }
}

// Component

export function FloatingButton() {
    const [showMenu, setShowMenu] = useState(false);
    const [menuPos, setMenuPos] = useState({ x: 0, y: 0 });
    const [petConfig, setPetConfig] = useState<FloatingPetConfig>(defaultPetConfig);
    const [petState, setPetState] = useState<PetState>('idle');
    const [petBurstId, setPetBurstId] = useState(0);
    const [petBurstActive, setPetBurstActive] = useState(false);
    const [petGesture, setPetGesture] = useState<PetGesture>('none');
    const petSkin = getPetSkinOption(petConfig.petSkin);
    const petConfigRef = useRef(petConfig);
    const petStateRef = useRef(petState);

    // Drag state tracked via ref to avoid re-renders during drag.
    const dragRef = useRef({
        startScreenX: 0,
        startScreenY: 0,
        isDragging: false,
        rafId: 0,
    });
    const audioCtxRef = useRef<AudioContext | null>(null);
    const activeMotionSoundCleanupsRef = useRef<Array<() => void>>([]);
    const activeMotionSoundTimersRef = useRef<number[]>([]);
    const lastMotionSoundAtRef = useRef(0);
    const petBurstTimerRef = useRef<number | undefined>(undefined);
    const petGestureTimerRef = useRef<number | undefined>(undefined);
    const lastPetHoverAtRef = useRef(0);
    const asrListeningActiveRef = useRef(false);
    const resumeSoundRetryRef = useRef(false);
    const restoreIdleState = useCallback(() => {
        setPetState(asrListeningActiveRef.current ? 'listening' : 'idle');
    }, []);


    useEffect(() => {
        petConfigRef.current = petConfig;
        petStateRef.current = petState;
    }, [petConfig, petState]);

    useEffect(() => {
        return () => {
            const ctx = audioCtxRef.current;
            audioCtxRef.current = null;
            activeMotionSoundTimersRef.current.splice(0).forEach((timer) => window.clearTimeout(timer));
            if (petBurstTimerRef.current) {
                window.clearTimeout(petBurstTimerRef.current);
                petBurstTimerRef.current = undefined;
            }
            if (petGestureTimerRef.current) {
                window.clearTimeout(petGestureTimerRef.current);
                petGestureTimerRef.current = undefined;
            }
            activeMotionSoundCleanupsRef.current.splice(0).forEach((cleanup) => cleanup());
            if (ctx && ctx.state !== 'closed') {
                void ctx.close().catch((err) => console.warn('[FloatingButton] Close pet audio failed:', err));
            }
        };
    }, []);

    useEffect(() => {
        let cancelled = false;
        const applyPetConfig = (cfg: Record<string, unknown>) => {
            const nextConfig = readPetConfig(cfg);
            setPetConfig((current) => arePetConfigsEqual(current, nextConfig) ? current : nextConfig);
        };
        const load = async () => {
            try {
                const cfg = await window.go?.main?.App?.LoadConfig?.();
                if (!cfg || cancelled) return;
                applyPetConfig(cfg);
            } catch (err) {
                console.warn('[FloatingButton] LoadConfig failed:', err);
            }
        };
        void load();
        const handleConfigChange = (cfg?: Record<string, unknown>) => {
            if (!cfg || cancelled) {
                void load();
                return;
            }
            applyPetConfig(cfg);
        };
        const offConfigChanged = subscribeRuntimeEvent('config-changed', handleConfigChange);
        const offConfigUpdated = subscribeRuntimeEvent('config-updated', handleConfigChange);
        const timer = offConfigChanged || offConfigUpdated
            ? undefined
            : window.setInterval(load, PET_CONFIG_FALLBACK_POLL_MS);
        return () => {
            cancelled = true;
            offConfigChanged?.();
            offConfigUpdated?.();
            if (timer !== undefined) window.clearInterval(timer);
        };
    }, []);

    useEffect(() => {
        if (petConfig.petEnabled && !petConfig.quietMode && petConfig.motionEnabled) return;
        if (petBurstTimerRef.current) {
            window.clearTimeout(petBurstTimerRef.current);
            petBurstTimerRef.current = undefined;
        }
        if (petGestureTimerRef.current) {
            window.clearTimeout(petGestureTimerRef.current);
            petGestureTimerRef.current = undefined;
        }
        setPetBurstActive(false);
        setPetGesture('none');
    }, [petConfig.petEnabled, petConfig.quietMode, petConfig.motionEnabled]);

    useEffect(() => {
        if (!petConfig.petEnabled || petConfig.quietMode) {
            asrListeningActiveRef.current = false;
            setPetState('idle');
            return;
        }

        let idleTimer: number | undefined;
        const clearIdleTimer = () => {
            if (idleTimer) {
                window.clearTimeout(idleTimer);
                idleTimer = undefined;
            }
        };
        const scheduleIdle = (ttlMs?: number) => {
            clearIdleTimer();
            if (!ttlMs || ttlMs <= 0) return;
            idleTimer = window.setTimeout(restoreIdleState, ttlMs);
        };
        const handler = (payload?: PetStateEvent | PetState) => {
            const nextState = typeof payload === 'string' ? payload : payload?.state;
            if (!isPetState(nextState)) return;
            const source = typeof payload === 'object' ? payload.source : undefined;
            const fromAsr = isAsrSource(source);

            if (fromAsr) {
                asrListeningActiveRef.current = nextState === 'listening';
            } else if (nextState === 'idle' && asrListeningActiveRef.current) {
                return;
            }

            setPetState(nextState);
            if (nextState !== 'idle') {
                setPetBurstId((value) => value + 1);
                setPetBurstActive(true);
                if (petBurstTimerRef.current) {
                    window.clearTimeout(petBurstTimerRef.current);
                }
                petBurstTimerRef.current = window.setTimeout(() => {
                    setPetBurstActive(false);
                    petBurstTimerRef.current = undefined;
                }, nextState === 'speaking' ? 460 : 560);
            } else {
                setPetBurstActive(false);
            }
            scheduleIdle(typeof payload === 'object' ? payload.ttlMs : undefined);
        };

        const offPetState = subscribeRuntimeEvent('pet:state', handler);
        return () => {
            clearIdleTimer();
            offPetState?.();
        };
    }, [petConfig.petEnabled, petConfig.quietMode, restoreIdleState]);

    const playPetMotionSound = useCallback((variant: MotionSoundVariant = 'step') => {
        if (!isMotionSoundAllowed(petConfigRef.current)) return;
        try {
            const AudioContextCtor = window.AudioContext || (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
            if (!AudioContextCtor) return;
            const ctx = audioCtxRef.current ?? new AudioContextCtor();
            audioCtxRef.current = ctx;
            if (ctx.state === 'suspended') {
                if (!resumeSoundRetryRef.current) {
                    resumeSoundRetryRef.current = true;
                    void ctx.resume()
                        .then(() => {
                            if (isMotionSoundAllowed(petConfigRef.current)) playPetMotionSound(variant);
                        })
                        .catch((err) => console.warn('[FloatingButton] Resume pet audio failed:', err))
                        .finally(() => {
                            resumeSoundRetryRef.current = false;
                        });
                } else {
                    void ctx.resume().catch((err) => console.warn('[FloatingButton] Resume pending pet audio failed:', err));
                }
                return;
            }

            const soundConfig = petConfigRef.current;
            const soundState = petStateRef.current;
            const profile = getMotionSoundProfile(soundConfig, soundState, variant);
            const pitchDrift = 1 - profile.pitchDrift / 2 + Math.random() * profile.pitchDrift;
            const filterDrift = 1 - profile.filterDrift / 2 + Math.random() * profile.filterDrift;
            const accentDelay = profile.accentDelay + Math.random() * 0.012;
            const now = ctx.currentTime;
            const endAt = now + profile.tailSeconds + profile.delaySeconds + 0.08;
            const output = ctx.createDynamicsCompressor();
            output.threshold.setValueAtTime(-24, now);
            output.knee.setValueAtTime(18, now);
            output.ratio.setValueAtTime(profile.compressorRatio, now);
            output.attack.setValueAtTime(0.003, now);
            output.release.setValueAtTime(0.12, now);
            output.connect(ctx.destination);

            const filter = ctx.createBiquadFilter();
            filter.type = 'lowpass';
            filter.frequency.setValueAtTime(profile.filterHz * filterDrift, now);
            filter.Q.setValueAtTime(0.8, now);
            filter.connect(output);

            const delay = ctx.createDelay(0.12);
            const delayGain = ctx.createGain();
            delay.delayTime.setValueAtTime(profile.delaySeconds, now);
            delayGain.gain.setValueAtTime(profile.delayGain, now);
            filter.connect(delay);
            delay.connect(delayGain);
            delayGain.connect(output);

            const gain = ctx.createGain();
            gain.gain.setValueAtTime(0.0001, now);
            gain.gain.exponentialRampToValueAtTime(profile.peakGain, now + 0.012);
            gain.gain.exponentialRampToValueAtTime(0.0001, now + profile.tailSeconds);
            gain.connect(filter);

            const first = ctx.createOscillator();
            const second = ctx.createOscillator();
            first.type = profile.firstType;
            second.type = profile.secondType;
            first.frequency.setValueAtTime(profile.baseHz * pitchDrift, now);
            first.frequency.exponentialRampToValueAtTime(profile.glideHz * pitchDrift, now + 0.052);
            second.frequency.setValueAtTime(profile.accentHz * pitchDrift, now + accentDelay);
            second.frequency.exponentialRampToValueAtTime(profile.accentHz * pitchDrift * 0.84, now + profile.tailSeconds);
            first.connect(gain);
            second.connect(gain);
            first.start(now);
            first.stop(now + Math.min(0.08, profile.tailSeconds));
            second.start(now + accentDelay);
            second.stop(now + profile.tailSeconds);

            const noiseBuffer = ctx.createBuffer(1, Math.max(1, Math.floor(ctx.sampleRate * 0.035)), ctx.sampleRate);
            const samples = noiseBuffer.getChannelData(0);
            for (let i = 0; i < samples.length; i += 1) {
                samples[i] = (Math.random() * 2 - 1) * (1 - i / samples.length);
            }
            const noise = ctx.createBufferSource();
            const noiseFilter = ctx.createBiquadFilter();
            const noiseGain = ctx.createGain();
            noise.buffer = noiseBuffer;
            noiseFilter.type = 'highpass';
            noiseFilter.frequency.setValueAtTime(1800, now);
            noiseGain.gain.setValueAtTime(0.0001, now);
            noiseGain.gain.exponentialRampToValueAtTime(profile.noiseGain, now + 0.006);
            noiseGain.gain.exponentialRampToValueAtTime(0.0001, now + 0.04);
            noise.connect(noiseFilter);
            noiseFilter.connect(noiseGain);
            noiseGain.connect(filter);
            noise.start(now);
            noise.stop(now + 0.045);

            let cleaned = false;
            const cleanup = () => {
                if (cleaned) return;
                cleaned = true;
                disconnectAudioNode(first);
                disconnectAudioNode(second);
                disconnectAudioNode(gain);
                disconnectAudioNode(noise);
                disconnectAudioNode(noiseFilter);
                disconnectAudioNode(noiseGain);
                disconnectAudioNode(delay);
                disconnectAudioNode(delayGain);
                disconnectAudioNode(filter);
                disconnectAudioNode(output);
                activeMotionSoundCleanupsRef.current = activeMotionSoundCleanupsRef.current.filter((fn) => fn !== cleanup);
            };
            activeMotionSoundCleanupsRef.current.push(cleanup);
            while (activeMotionSoundCleanupsRef.current.length > 3) {
                activeMotionSoundCleanupsRef.current.shift()?.();
            }
            second.onended = cleanup;
            window.setTimeout(cleanup, Math.ceil((endAt - now) * 1000));
        } catch (err) {
            console.warn('[FloatingButton] Pet motion sound failed:', err);
        }
    }, []);

    const motionSoundReady = isMotionSoundAllowed(petConfig);

    const triggerPetGesture = useCallback((gesture: PetGesture, durationMs = 520) => {
        if (!petConfig.petEnabled || petConfig.quietMode || !petConfig.motionEnabled) return;
        setPetGesture(gesture);
        if (petGestureTimerRef.current) {
            window.clearTimeout(petGestureTimerRef.current);
        }
        petGestureTimerRef.current = window.setTimeout(() => {
            setPetGesture('none');
            petGestureTimerRef.current = undefined;
        }, durationMs);
    }, [petConfig.petEnabled, petConfig.quietMode, petConfig.motionEnabled]);

    useEffect(() => {
        if (motionSoundReady) return;
        lastMotionSoundAtRef.current = 0;
        resumeSoundRetryRef.current = false;
        activeMotionSoundTimersRef.current.splice(0).forEach((timer) => window.clearTimeout(timer));
        activeMotionSoundCleanupsRef.current.splice(0).forEach((cleanup) => cleanup());
        const ctx = audioCtxRef.current;
        audioCtxRef.current = null;
        if (ctx && ctx.state !== 'closed') {
            void ctx.close().catch((err) => console.warn('[FloatingButton] Close disabled pet audio failed:', err));
        }
    }, [motionSoundReady]);

    const schedulePetMotionSounds = useCallback(() => {
        const cycleMs = motionCycleMs(petConfig, petState);
        const at = (ratio: number) => Math.round(cycleMs * ratio);
        const pattern: Array<[number, MotionSoundVariant]> = [[0, 'step']];
        if (petState === 'speaking') {
            pattern.push([at(0.13), 'accent'], [at(0.3), 'step'], [at(0.46), 'flutter']);
        } else if (petState === 'listening') {
            pattern.push([at(0.22), 'flutter'], [at(0.48), petConfig.petSkin === 'dev-claw' ? 'accent' : 'step']);
        } else if (petState === 'thinking') {
            pattern.push([at(0.2), petConfig.petSkin === 'dev-claw' ? 'flutter' : 'accent']);
            if (petConfig.petSkin === 'dev-claw' || petConfig.interactionMode === 'active') {
                pattern.push([at(0.42), 'flutter']);
            }
        } else if (petConfig.petSkin === 'mini-claw' || petConfig.interactionMode === 'active') {
            pattern.push([at(0.2), 'accent']);
        }

        activeMotionSoundTimersRef.current.splice(0).forEach((timer) => window.clearTimeout(timer));
        pattern.forEach(([delayMs, variant]) => {
            if (delayMs <= 0) {
                playPetMotionSound(variant);
                return;
            }
            const timer = window.setTimeout(() => {
                activeMotionSoundTimersRef.current = activeMotionSoundTimersRef.current.filter((id) => id !== timer);
                playPetMotionSound(variant);
            }, delayMs);
            activeMotionSoundTimersRef.current.push(timer);
        });
    }, [petConfig.interactionMode, petConfig.petSkin, petState, playPetMotionSound]);

    useEffect(() => {
        if (!petBurstId || !motionSoundReady || petState === 'idle') return;
        const now = Date.now();
        const minGapMs = Math.min(360, motionSoundGapMs(petConfig, petState));
        if (now - lastMotionSoundAtRef.current < minGapMs) return;
        lastMotionSoundAtRef.current = now;
        schedulePetMotionSounds();
    }, [petBurstId, motionSoundReady, petConfig, petState, schedulePetMotionSounds]);

    const handlePetAnimationStart = useCallback((e: ReactAnimationEvent<HTMLImageElement>) => {
        if (e.currentTarget !== e.target || !motionSoundReady || petState === 'idle') return;
        const now = Date.now();
        const minGapMs = Math.min(480, motionSoundGapMs(petConfig, petState));
        if (now - lastMotionSoundAtRef.current < minGapMs) return;
        lastMotionSoundAtRef.current = now;
        schedulePetMotionSounds();
    }, [motionSoundReady, petConfig, petState, schedulePetMotionSounds]);

    const handlePetAnimationIteration = useCallback((e: ReactAnimationEvent<HTMLImageElement>) => {
        if (e.currentTarget !== e.target || !motionSoundReady) return;
        const now = Date.now();
        const minGapMs = motionSoundGapMs(petConfig, petState);
        if (now - lastMotionSoundAtRef.current < minGapMs) return;
        lastMotionSoundAtRef.current = now;
        schedulePetMotionSounds();
    }, [motionSoundReady, petConfig, petState, schedulePetMotionSounds]);

    // Left-click / drag handling

    const handleMouseDown = useCallback((e: ReactMouseEvent) => {
        if (e.button !== 0) return; // only left button
        e.preventDefault();

        // Close context menu if open.
        setShowMenu(false);
        triggerPetGesture('wake', 480);
        if (motionSoundReady) playPetMotionSound('accent');

        const drag = dragRef.current;
        drag.startScreenX = e.screenX;
        drag.startScreenY = e.screenY;
        drag.isDragging = false;

        const onMouseMove = (ev: MouseEvent) => {
            const dx = ev.screenX - drag.startScreenX;
            const dy = ev.screenY - drag.startScreenY;

            if (!drag.isDragging) {
                // Check if displacement exceeds threshold.
                if (Math.abs(dx) > DRAG_THRESHOLD || Math.abs(dy) > DRAG_THRESHOLD) {
                    drag.isDragging = true;
                    triggerPetGesture('drag', 900);
                } else {
                    return;
                }
            }

            // Throttle window move calls with requestAnimationFrame.
            if (drag.rafId) return;
            drag.rafId = requestAnimationFrame(() => {
                drag.rafId = 0;
                // Compute new window position: current window position + delta.
                // window.screenX/screenY give the window's position on screen.
                const newX = window.screenX + (ev.screenX - drag.startScreenX);
                const newY = window.screenY + (ev.screenY - drag.startScreenY);
                drag.startScreenX = ev.screenX;
                drag.startScreenY = ev.screenY;
                callGoBinding('OnFloatingButtonDragged', newX, newY);
            });
        };

        const onMouseUp = () => {
            document.removeEventListener('mousemove', onMouseMove);
            document.removeEventListener('mouseup', onMouseUp);

            if (drag.rafId) {
                cancelAnimationFrame(drag.rafId);
                drag.rafId = 0;
            }

            if (!drag.isDragging) {
                // Displacement <= threshold: treat as left-click.
                callGoBinding('OnFloatingButtonClicked');
            } else {
                triggerPetGesture('wake', 360);
            }
        };

        document.addEventListener('mousemove', onMouseMove);
        document.addEventListener('mouseup', onMouseUp);
    }, [motionSoundReady, playPetMotionSound, triggerPetGesture]);

    const handleMouseEnter = useCallback(() => {
        const now = Date.now();
        if (now - lastPetHoverAtRef.current < 1800) return;
        lastPetHoverAtRef.current = now;
        triggerPetGesture('wake', 420);
    }, [triggerPetGesture]);

    // Right-click context menu

    const handleContextMenu = useCallback((e: ReactMouseEvent) => {
        e.preventDefault();
        e.stopPropagation();
        triggerPetGesture('menu', 760);
        if (motionSoundReady) playPetMotionSound('flutter');
        setMenuPos(clampContextMenuPosition(e.clientX, e.clientY));
        setShowMenu(true);
    }, [motionSoundReady, playPetMotionSound, triggerPetGesture]);

    const handleQuit = useCallback(() => {
        setShowMenu(false);
        callGoBinding('QuitApp');
    }, []);

    const handleOpenSettings = useCallback(() => {
        setShowMenu(false);
        callGoBinding('OpenPetSettingsFromMenu');
    }, []);

    const handleToggleSoundOff = useCallback(() => {
        const nextSoundEnabled = !petConfig.motionSoundEnabled;
        setShowMenu(false);
        setPetConfig((current) => ({ ...current, motionSoundEnabled: nextSoundEnabled }));
        void savePetMotionSoundEnabled(nextSoundEnabled)
            .then((savedConfig) => {
                setPetConfig(savedConfig);
            })
            .catch((err) => {
                console.warn('[FloatingButton] Save pet sound-off setting failed:', err);
                void window.go?.main?.App?.LoadConfig?.()
                    .then((cfg) => {
                        setPetConfig(cfg ? readPetConfig(cfg) : petConfigRef.current);
                    })
                    .catch((loadErr) => {
                        console.warn('[FloatingButton] Reload pet config failed:', loadErr);
                        setPetConfig(petConfigRef.current);
                    });
            });
    }, [petConfig.motionSoundEnabled]);

    // Close context menu when clicking outside.
    useEffect(() => {
        if (!showMenu) return;

        const handleClickOutside = (e: MouseEvent) => {
            // If the click is not on a menu item, close the menu.
            const target = e.target as HTMLElement;
            if (!target.closest('.floating-context-menu')) {
                setShowMenu(false);
            }
        };
        const handleKeyDown = (e: KeyboardEvent) => {
            if (e.key === 'Escape') setShowMenu(false);
        };

        // Use a short delay so the current right-click event doesn't
        // immediately close the menu.
        const timer = setTimeout(() => {
            document.addEventListener('mousedown', handleClickOutside);
            document.addEventListener('keydown', handleKeyDown);
        }, 0);

        return () => {
            clearTimeout(timer);
            document.removeEventListener('mousedown', handleClickOutside);
            document.removeEventListener('keydown', handleKeyDown);
        };
    }, [showMenu]);

    const soundOff = !petConfig.motionSoundEnabled;

    return (
        <div
            className={`floating-assistant-container ${petConfig.petEnabled ? 'floating-assistant-container--pet' : ''}`}
            onMouseDown={handleMouseDown}
            onMouseEnter={handleMouseEnter}
            onContextMenu={handleContextMenu}
            data-pet-state={petState}
            data-pet-skin={petConfig.petSkin}
            data-interaction-mode={petConfig.interactionMode}
            data-motion={petConfig.motionEnabled ? 'on' : 'off'}
            data-quiet={petConfig.quietMode ? 'on' : 'off'}
            data-burst={petBurstActive ? 'on' : 'off'}
            data-gesture={petGesture}
            style={petConfig.petEnabled ? { width: petConfig.petSize, height: petConfig.petSize } : undefined}
        >
            <div className="floating-assistant-halo" />
            {petConfig.petEnabled ? (
                <>
                    <img
                        key={petConfig.petSkin + '-' + petState + '-' + petBurstId}
                        src={petSkin.image}
                        className="floating-assistant-pet"
                        alt={petSkin.alt}
                        draggable={false}
                        onAnimationStart={handlePetAnimationStart}
                        onAnimationIteration={handlePetAnimationIteration}
                    />
                    <div className="floating-assistant-pet-status" />
                    {petBurstActive && petConfig.motionEnabled && !petConfig.quietMode && (
                        <span key={petBurstId} className="floating-assistant-motion-burst" aria-hidden="true" />
                    )}
                    <span className="floating-assistant-face floating-assistant-face--eye-left" aria-hidden="true" />
                    <span className="floating-assistant-face floating-assistant-face--eye-right" aria-hidden="true" />
                    <span className="floating-assistant-face floating-assistant-face--mouth" aria-hidden="true" />
                    <span className="floating-assistant-motion-mark floating-assistant-motion-mark--a" aria-hidden="true" />
                    <span className="floating-assistant-motion-mark floating-assistant-motion-mark--b" aria-hidden="true" />
                    <span className="floating-assistant-motion-mark floating-assistant-motion-mark--c" aria-hidden="true" />
                </>
            ) : (
                <img
                    src={logoSrc}
                    className="floating-assistant-logo"
                    alt="MaClaw Assistant"
                    draggable={false}
                />
            )}
            {showMenu && (
                <div
                    className="floating-context-menu"
                    role="menu"
                    style={{ top: menuPos.y, left: menuPos.x }}
                    onMouseDown={(e) => e.stopPropagation()}
                >
                    <button
                        type="button"
                        className="floating-context-menu-item floating-context-menu-item--check"
                        role="menuitemcheckbox"
                        aria-checked={soundOff}
                        onClick={handleToggleSoundOff}
                    >
                        <span className="floating-context-menu-check" aria-hidden="true">
                            {soundOff ? "OK" : ""}
                        </span>
                        <span>{"\u97f3\u6548\u5173\u95ed"}</span>
                    </button>
                    <div className="floating-context-menu-separator" />
                    <button
                        type="button"
                        className="floating-context-menu-item"
                        role="menuitem"
                        onClick={handleOpenSettings}
                    >{"\u8bbe\u7f6e"}</button>
                    <button
                        type="button"
                        className="floating-context-menu-item"
                        role="menuitem"
                        onClick={handleQuit}
                    >{"\u9000\u51fa"}</button>
                </div>
            )}
        </div>
    );
}

export default FloatingButton;
