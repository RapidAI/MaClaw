/**
 * FloatingButton.tsx - Floating assistant button component (rendered in the second window).
 *
 * Renders a 56x56px circular button with the maclaw logo and a pulsing halo animation.
 * Supports:
 *   - Left-click calls OnFloatingButtonClicked() via Wails binding
 *   - Drag (displacement > 5px) calls OnFloatingButtonDragged(x, y) with rAF throttling
 *   - Right-click opens a custom context menu
 *
 * Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 3.1, 3.2, 4.1, 4.2, 4.3, 4.4, 11.3
 */

import { useState, useRef, useCallback, useEffect } from 'react';
import type { MouseEvent as ReactMouseEvent } from 'react';
import { EventsOn } from '../../wailsjs/runtime';
import './FloatingButton.css';
import logoSrc from '../assets/images/maclaw2.png';
import { defaultPetSize, defaultPetSkinId, getPetSkinOption, normalizePetSkinId, type PetSkinId } from './petSkins';

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
                    HideFloatingButton(): Promise<void>;
                    QuitApp(): Promise<void>;
                    LoadConfig(): Promise<Record<string, unknown>>;
                };
            };
        };
    }
}

function callGoBinding(method: 'OnFloatingButtonClicked'): void;
function callGoBinding(method: 'OnFloatingButtonDragged', x: number, y: number): void;
function callGoBinding(method: 'HideFloatingButton'): void;
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

type PetState = 'idle' | 'listening' | 'thinking' | 'speaking';
type PetInteractionMode = 'quiet' | 'balanced' | 'active';

type PetStateEvent = {
    state?: PetState;
    source?: string;
    ttlMs?: number;
};

interface FloatingPetConfig {
    petEnabled: boolean;
    petSkin: PetSkinId;
    petSize: number;
    motionEnabled: boolean;
    quietMode: boolean;
    interactionMode: PetInteractionMode;
}

const defaultPetConfig: FloatingPetConfig = {
    petEnabled: false,
    petSkin: defaultPetSkinId,
    petSize: defaultPetSize,
    motionEnabled: true,
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

function readPetConfig(cfg: Record<string, unknown>): FloatingPetConfig {
    return {
        petEnabled: !!cfg.pet_enabled,
        petSkin: normalizePetSkinId(cfg.pet_skin),
        petSize: clampPetSize(cfg.pet_size),
        motionEnabled: cfg.pet_motion_enabled !== false,
        quietMode: !!cfg.pet_quiet_mode,
        interactionMode: normalizeInteractionMode(cfg.pet_interaction_mode),
    };
}

// Component

export function FloatingButton() {
    const [showMenu, setShowMenu] = useState(false);
    const [menuPos, setMenuPos] = useState({ x: 0, y: 0 });
    const [petConfig, setPetConfig] = useState<FloatingPetConfig>(defaultPetConfig);
    const [petState, setPetState] = useState<PetState>('idle');
    const petSkin = getPetSkinOption(petConfig.petSkin);

    // Drag state tracked via ref to avoid re-renders during drag.
    const dragRef = useRef({
        startScreenX: 0,
        startScreenY: 0,
        isDragging: false,
        rafId: 0,
    });

    useEffect(() => {
        let cancelled = false;
        const load = async () => {
            try {
                const cfg = await window.go?.main?.App?.LoadConfig?.();
                if (!cfg || cancelled) return;
                setPetConfig(readPetConfig(cfg));
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
            setPetConfig(readPetConfig(cfg));
        };
        const offConfigChanged = EventsOn('config-changed', handleConfigChange);
        const offConfigUpdated = EventsOn('config-updated', handleConfigChange);
        const timer = window.setInterval(load, 5000);
        return () => {
            cancelled = true;
            offConfigChanged();
            offConfigUpdated();
            window.clearInterval(timer);
        };
    }, []);

    useEffect(() => {
        if (!petConfig.petEnabled || petConfig.quietMode) {
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
            idleTimer = window.setTimeout(() => setPetState('idle'), ttlMs);
        };
        const handler = (payload?: PetStateEvent | PetState) => {
            const nextState = typeof payload === 'string' ? payload : payload?.state;
            if (!isPetState(nextState)) return;
            setPetState(nextState);
            scheduleIdle(typeof payload === 'object' ? payload.ttlMs : undefined);
        };

        const offPetState = EventsOn('pet:state', handler);
        return () => {
            clearIdleTimer();
            offPetState();
        };
    }, [petConfig.petEnabled, petConfig.quietMode]);

    useEffect(() => {
        if (!petConfig.petEnabled || petConfig.quietMode || !petConfig.motionEnabled || petState !== 'idle') return;
        const timer = window.setInterval(() => setPetState('idle'), 4200);
        return () => window.clearInterval(timer);
    }, [petConfig.petEnabled, petConfig.quietMode, petConfig.motionEnabled, petState]);

    // Left-click / drag handling

    const handleMouseDown = useCallback((e: ReactMouseEvent) => {
        if (e.button !== 0) return; // only left button
        e.preventDefault();

        // Close context menu if open.
        setShowMenu(false);

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
            }
        };

        document.addEventListener('mousemove', onMouseMove);
        document.addEventListener('mouseup', onMouseUp);
    }, []);

    // Right-click context menu

    const handleContextMenu = useCallback((e: ReactMouseEvent) => {
        e.preventDefault();
        e.stopPropagation();
        setMenuPos({ x: e.clientX, y: e.clientY });
        setShowMenu(true);
    }, []);

    const handleHide = useCallback(() => {
        setShowMenu(false);
        callGoBinding('HideFloatingButton');
    }, []);

    const handleQuit = useCallback(() => {
        setShowMenu(false);
        callGoBinding('QuitApp');
    }, []);

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

        // Use a short delay so the current right-click event doesn't
        // immediately close the menu.
        const timer = setTimeout(() => {
            document.addEventListener('mousedown', handleClickOutside);
        }, 0);

        return () => {
            clearTimeout(timer);
            document.removeEventListener('mousedown', handleClickOutside);
        };
    }, [showMenu]);

    return (
        <div
            className={`floating-assistant-container ${petConfig.petEnabled ? 'floating-assistant-container--pet' : ''}`}
            onMouseDown={handleMouseDown}
            onContextMenu={handleContextMenu}
            data-pet-state={petState}
            data-pet-skin={petConfig.petSkin}
            data-interaction-mode={petConfig.interactionMode}
            data-motion={petConfig.motionEnabled ? 'on' : 'off'}
            data-quiet={petConfig.quietMode ? 'on' : 'off'}
            style={petConfig.petEnabled ? { width: petConfig.petSize, height: petConfig.petSize } : undefined}
        >
            <div className="floating-assistant-halo" />
            {petConfig.petEnabled ? (
                <>
                    <img
                        src={petSkin.image}
                        className="floating-assistant-pet"
                        alt={petSkin.alt}
                        draggable={false}
                    />
                    <div className="floating-assistant-pet-status" />
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
                    style={{ top: menuPos.y, left: menuPos.x }}
                >
                    <div
                        className="floating-context-menu-item"
                        onClick={handleHide}
                    >{"\u9690\u85cf"}</div>
                    <div className="floating-context-menu-separator" />
                    <div
                        className="floating-context-menu-item"
                        onClick={handleQuit}
                    >{"\u9000\u51fa"}</div>
                </div>
            )}
        </div>
    );
}

export default FloatingButton;
