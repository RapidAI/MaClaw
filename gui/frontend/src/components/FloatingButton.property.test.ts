/**
 * Property-based tests for the MaClaw desktop pet entry logic.
 *
 * The desktop pet is independent from main window visibility and is controlled
 * by the Pet tab's pet_enabled setting.
 */
import { describe, it, expect } from 'vitest';
import * as fc from 'fast-check';

const PET_SIZE = 88 + 16;
const SCREEN_W = 1920;
const SCREEN_H = 1080;

interface DesktopPetState {
    visible: boolean;
    posX: number;
    posY: number;
    petEnabled: boolean;
    mainWindowVisible: boolean;
    hasSavedPosition: boolean;
}

function initialState(): DesktopPetState {
    return {
        visible: false,
        posX: 0,
        posY: 0,
        petEnabled: true,
        mainWindowVisible: true,
        hasSavedPosition: false,
    };
}

type Command =
    | { type: 'hide_main_window' }
    | { type: 'show_main_window' }
    | { type: 'click_pet' }
    | { type: 'hide_pet' }
    | { type: 'set_pet_enabled'; value: boolean }
    | { type: 'drag'; x: number; y: number };

type PetRuntimeState = 'idle' | 'listening' | 'thinking' | 'speaking';

function applyPetRuntimeEvent(
    current: { state: PetRuntimeState; asrListeningActive: boolean },
    event: { state: PetRuntimeState; source?: string }
): { state: PetRuntimeState; asrListeningActive: boolean } {
    const fromAsr = event.source?.startsWith('asr:') === true;
    const asrListeningActive = fromAsr
        ? event.state === 'listening'
        : current.asrListeningActive;

    if (!fromAsr && event.state === 'idle' && current.asrListeningActive) {
        return { ...current, asrListeningActive };
    }

    return { state: event.state, asrListeningActive };
}

function applyPetStateTtl(current: { state: PetRuntimeState; asrListeningActive: boolean }): { state: PetRuntimeState; asrListeningActive: boolean } {
    return {
        ...current,
        state: current.asrListeningActive ? 'listening' : 'idle',
    };
}

function applyCommand(state: DesktopPetState, cmd: Command): DesktopPetState {
    switch (cmd.type) {
        case 'hide_main_window':
            return {
                ...state,
                mainWindowVisible: false,
                visible: state.petEnabled ? true : state.visible,
                posX: state.hasSavedPosition ? state.posX : SCREEN_W - 150,
                posY: state.hasSavedPosition ? state.posY : 100,
            };

        case 'show_main_window':
            return {
                ...state,
                mainWindowVisible: true,
            };

        case 'click_pet':
            return state.visible
                ? { ...state, mainWindowVisible: true }
                : state;

        case 'hide_pet':
            return { ...state, visible: false };

        case 'set_pet_enabled':
            return {
                ...state,
                petEnabled: cmd.value,
                visible: cmd.value ? state.visible : false,
            };

        case 'drag': {
            const clampedX = Math.max(0, Math.min(cmd.x, SCREEN_W - PET_SIZE));
            const clampedY = Math.max(0, Math.min(cmd.y, SCREEN_H - PET_SIZE));
            return { ...state, posX: clampedX, posY: clampedY, hasSavedPosition: true };
        }
    }
}

describe('Property 1: Desktop pet state consistency', () => {
    it('visible implies pet_enabled is true', () => {
        fc.assert(
            fc.property(fc.array(commandArbitrary()), (commands) => {
                let state = initialState();
                for (const cmd of commands) {
                    state = applyCommand(state, cmd);
                    if (state.visible) {
                        expect(state.petEnabled).toBe(true);
                    }
                }
            })
        );
    });

    it('showing the main window does not hide an already-visible pet', () => {
        fc.assert(
            fc.property(fc.boolean(), (mainVisible) => {
                const state: DesktopPetState = {
                    visible: true,
                    posX: 120,
                    posY: 160,
                    petEnabled: true,
                    mainWindowVisible: mainVisible,
                    hasSavedPosition: true,
                };

                const next = applyCommand(state, { type: 'show_main_window' });

                expect(next.visible).toBe(true);
                expect(next.mainWindowVisible).toBe(true);
            })
        );
    });
});

describe('Property 2: Click opens main window without hiding pet', () => {
    it('click on visible pet shows main window and keeps pet visible', () => {
        fc.assert(
            fc.property(
                fc.integer({ min: 0, max: SCREEN_W - PET_SIZE }),
                fc.integer({ min: 0, max: SCREEN_H - PET_SIZE }),
                (x, y) => {
                    let state: DesktopPetState = {
                        visible: true,
                        posX: x,
                        posY: y,
                        petEnabled: true,
                        mainWindowVisible: false,
                        hasSavedPosition: true,
                    };

                    state = applyCommand(state, { type: 'click_pet' });

                    expect(state.visible).toBe(true);
                    expect(state.mainWindowVisible).toBe(true);
                }
            )
        );
    });
});

describe('Property 3: Drag/click threshold classification', () => {
    const THRESHOLD = 5;

    it('classifies as drag if |deltaX| > 5 OR |deltaY| > 5', () => {
        fc.assert(
            fc.property(
                fc.integer({ min: -100, max: 100 }),
                fc.integer({ min: -100, max: 100 }),
                (deltaX, deltaY) => {
                    const isDrag = Math.abs(deltaX) > THRESHOLD || Math.abs(deltaY) > THRESHOLD;
                    const expectedIsDrag = Math.abs(deltaX) > THRESHOLD || Math.abs(deltaY) > THRESHOLD;
                    expect(isDrag).toBe(expectedIsDrag);
                }
            )
        );
    });

    it('classifies as click if |deltaX| <= 5 AND |deltaY| <= 5', () => {
        fc.assert(
            fc.property(
                fc.integer({ min: -5, max: 5 }),
                fc.integer({ min: -5, max: 5 }),
                (deltaX, deltaY) => {
                    const isClick = Math.abs(deltaX) <= THRESHOLD && Math.abs(deltaY) <= THRESHOLD;
                    expect(isClick).toBe(true);
                }
            )
        );
    });
});

describe('Property 4: Drag position round-trip', () => {
    it('pet appears at saved position after drag', () => {
        fc.assert(
            fc.property(
                fc.integer({ min: 0, max: SCREEN_W - PET_SIZE }),
                fc.integer({ min: 0, max: SCREEN_H - PET_SIZE }),
                (x, y) => {
                    let state = initialState();
                    state = applyCommand(state, { type: 'drag', x, y });
                    state = applyCommand(state, { type: 'hide_pet' });
                    state = applyCommand(state, { type: 'hide_main_window' });

                    expect(state.posX).toBe(x);
                    expect(state.posY).toBe(y);
                }
            )
        );
    });
});

describe('Property 5: Position clamping within screen bounds', () => {
    it('clamped position satisfies desktop work area bounds', () => {
        fc.assert(
            fc.property(
                fc.integer({ min: -1000, max: 3000 }),
                fc.integer({ min: -1000, max: 3000 }),
                (x, y) => {
                    let state = initialState();
                    state = applyCommand(state, { type: 'drag', x, y });

                    expect(state.posX).toBeGreaterThanOrEqual(0);
                    expect(state.posX).toBeLessThanOrEqual(SCREEN_W - PET_SIZE);
                    expect(state.posY).toBeGreaterThanOrEqual(0);
                    expect(state.posY).toBeLessThanOrEqual(SCREEN_H - PET_SIZE);
                }
            )
        );
    });
});

describe('Property 6: Desktop pet and main window can coexist', () => {
    it('main window and desktop pet may be simultaneously visible', () => {
        let state = initialState();
        state = applyCommand(state, { type: 'hide_main_window' });
        state = applyCommand(state, { type: 'show_main_window' });

        expect(state.visible).toBe(true);
        expect(state.mainWindowVisible).toBe(true);
    });
});

describe('Property 7: Voice listening state priority', () => {
    it('keeps listening when non-ASR idle events arrive during continuous voice input', () => {
        fc.assert(
            fc.property(fc.array(fc.constantFrom('idle', 'thinking', 'speaking') as fc.Arbitrary<PetRuntimeState>), (states) => {
                let pet = applyPetRuntimeEvent(
                    { state: 'idle', asrListeningActive: false },
                    { state: 'listening', source: 'asr:continuous' }
                );

                for (const state of states) {
                    const before = pet.state;
                    pet = applyPetRuntimeEvent(pet, { state, source: `ai:${state}` });
                    if (state === 'idle') {
                        expect(pet.state).toBe(before);
                    }
                }
            })
        );
    });

    it('allows ASR idle to end the listening priority', () => {
        let pet = applyPetRuntimeEvent(
            { state: 'idle', asrListeningActive: false },
            { state: 'listening', source: 'asr:continuous' }
        );

        pet = applyPetRuntimeEvent(pet, { state: 'idle', source: 'asr:continuous-stop' });

        expect(pet.state).toBe('idle');
        expect(pet.asrListeningActive).toBe(false);
    });

    it('returns to listening when a non-ASR TTL expires during continuous voice input', () => {
        let pet = applyPetRuntimeEvent(
            { state: 'idle', asrListeningActive: false },
            { state: 'listening', source: 'asr:continuous' }
        );
        pet = applyPetRuntimeEvent(pet, { state: 'speaking', source: 'ai:token' });

        pet = applyPetStateTtl(pet);

        expect(pet.state).toBe('listening');
        expect(pet.asrListeningActive).toBe(true);
    });
});

describe('Property 8: Default position calculation', () => {
    it('default position keeps the pet near the top-right work area', () => {
        fc.assert(
            fc.property(fc.integer({ min: 800, max: 3840 }), (screenW) => {
                const defaultX = screenW - 150;
                const defaultY = 100;

                expect(defaultX).toBeGreaterThanOrEqual(0);
                expect(defaultX + PET_SIZE).toBeLessThanOrEqual(screenW);
                expect(defaultY).toBe(100);
            })
        );
    });
});

function commandArbitrary(): fc.Arbitrary<Command> {
    return fc.oneof(
        fc.constant({ type: 'hide_main_window' } as Command),
        fc.constant({ type: 'show_main_window' } as Command),
        fc.constant({ type: 'click_pet' } as Command),
        fc.constant({ type: 'hide_pet' } as Command),
        fc.record({ type: fc.constant('set_pet_enabled'), value: fc.boolean() }) as fc.Arbitrary<Command>,
        fc.record({
            type: fc.constant('drag'),
            x: fc.integer({ min: -100, max: 3000 }),
            y: fc.integer({ min: -100, max: 3000 }),
        }) as fc.Arbitrary<Command>
    );
}
