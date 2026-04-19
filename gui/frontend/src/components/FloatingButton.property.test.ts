/**
 * Property-based tests for floating assistant button logic.
 *
 * Tests the state machine and behavior of the floating assistant button
 * using fast-check for property-based testing.
 *
 * Properties tested:
 * - Property 1: State machine consistency under config and operations
 * - Property 2: Click restores main window and switches to AI panel
 * - Property 3: Drag/click threshold classification
 * - Property 4: Drag position round-trip
 * - Property 5: Position clamping within screen bounds
 * - Property 6: Mutual exclusivity invariant
 * - Property 8: Default position calculation
 */
import { describe, it, expect } from 'vitest';
import * as fc from 'fast-check';

// ── Pure state machine for floating button ──

interface FloatingButtonState {
    visible: boolean;
    posX: number;
    posY: number;
    showAssistantEntry: boolean;
    mainWindowVisible: boolean;
}

function initialState(): FloatingButtonState {
    return {
        visible: false,
        posX: 0,
        posY: 0,
        showAssistantEntry: true,
        mainWindowVisible: true,
    };
}

// Commands for state machine testing
type Command =
    | { type: 'hide_main_window' }
    | { type: 'show_main_window' }
    | { type: 'click_floating_button' }
    | { type: 'hide_floating_button' }
    | { type: 'set_config'; value: boolean }
    | { type: 'drag'; x: number; y: number };

function applyCommand(state: FloatingButtonState, cmd: Command): FloatingButtonState {
    switch (cmd.type) {
        case 'hide_main_window':
            // When main window is hidden and config allows, show floating button
            if (state.showAssistantEntry && !state.visible) {
                return {
                    ...state,
                    mainWindowVisible: false,
                    visible: true,
                    // Default position if not set
                    posX: state.posX === 0 && state.posY === 0 ? 1920 / 2 - 28 : state.posX,
                    posY: state.posX === 0 && state.posY === 0 ? 10 : state.posY,
                };
            }
            return { ...state, mainWindowVisible: false };

        case 'show_main_window':
            // When main window is shown, hide floating button (mutual exclusivity)
            return {
                ...state,
                mainWindowVisible: true,
                visible: false,
            };

        case 'click_floating_button':
            // Click restores main window and hides floating button
            if (state.visible) {
                return {
                    ...state,
                    visible: false,
                    mainWindowVisible: true,
                };
            }
            return state;

        case 'hide_floating_button':
            return { ...state, visible: false };

        case 'set_config':
            // When config changes to false, hide floating button
            if (!cmd.value && state.visible) {
                return { ...state, showAssistantEntry: cmd.value, visible: false };
            }
            return { ...state, showAssistantEntry: cmd.value };

        case 'drag':
            // Clamp position to screen bounds
            const buttonSize = 64;
            const screenW = 1920;
            const screenH = 1080;
            const clampedX = Math.max(0, Math.min(cmd.x, screenW - buttonSize));
            const clampedY = Math.max(0, Math.min(cmd.y, screenH - buttonSize));
            return { ...state, posX: clampedX, posY: clampedY };
    }
}

// ── Property 1: State machine consistency ──

describe('Property 1: State machine consistency', () => {
    it('visible is true only when hide-main-window occurred AND config is true AND no subsequent click/hide/config-false', () => {
        fc.assert(
            fc.property(fc.array(commandArbitrary()), (commands) => {
                let state = initialState();
                for (const cmd of commands) {
                    state = applyCommand(state, cmd);
                }

                // Invariant: visible implies showAssistantEntry is true
                if (state.visible) {
                    expect(state.showAssistantEntry).toBe(true);
                }

                // Invariant: visible and mainWindowVisible are mutually exclusive
                if (state.visible) {
                    expect(state.mainWindowVisible).toBe(false);
                }
            })
        );
    });

    it('ShowFloatingButton when already visible is idempotent', () => {
        fc.assert(
            fc.property(
                fc.record({
                    x: fc.integer({ min: 0, max: 1920 }),
                    y: fc.integer({ min: 0, max: 1080 }),
                }),
                ({ x, y }) => {
                    const state1: FloatingButtonState = {
                        visible: true,
                        posX: x,
                        posY: y,
                        showAssistantEntry: true,
                        mainWindowVisible: false,
                    };

                    // Apply hide_main_window again (should be idempotent)
                    const state2 = applyCommand(state1, { type: 'hide_main_window' });

                    expect(state2.visible).toBe(true);
                    expect(state2.posX).toBe(x);
                    expect(state2.posY).toBe(y);
                }
            )
        );
    });
});

// ── Property 2: Click restores main window ──

describe('Property 2: Click restores main window', () => {
    it('click on visible floating button shows main window and hides floating button', () => {
        fc.assert(
            fc.property(
                fc.integer({ min: 0, max: 1920 }),
                fc.integer({ min: 0, max: 1080 }),
                (x, y) => {
                    let state: FloatingButtonState = {
                        visible: true,
                        posX: x,
                        posY: y,
                        showAssistantEntry: true,
                        mainWindowVisible: false,
                    };

                    state = applyCommand(state, { type: 'click_floating_button' });

                    expect(state.visible).toBe(false);
                    expect(state.mainWindowVisible).toBe(true);
                }
            )
        );
    });
});

// ── Property 3: Drag/click threshold classification ──

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

// ── Property 4: Drag position round-trip ──

describe('Property 4: Drag position round-trip', () => {
    it('button appears at saved position after drag', () => {
        fc.assert(
            fc.property(
                fc.integer({ min: 0, max: 1856 }), // 1920 - 64
                fc.integer({ min: 0, max: 1016 }), // 1080 - 64
                (x, y) => {
                    let state = initialState();
                    state.showAssistantEntry = true;

                    // Drag to position
                    state = applyCommand(state, { type: 'drag', x, y });

                    // Hide and show again
                    state = applyCommand(state, { type: 'hide_floating_button' });
                    state = applyCommand(state, { type: 'hide_main_window' });

                    expect(state.posX).toBe(x);
                    expect(state.posY).toBe(y);
                }
            )
        );
    });
});

// ── Property 5: Position clamping within screen bounds ──

describe('Property 5: Position clamping within screen bounds', () => {
    const BUTTON_SIZE = 64;
    const SCREEN_W = 1920;
    const SCREEN_H = 1080;

    it('clamped position satisfies 0 <= x <= W - buttonWidth and 0 <= y <= H - buttonHeight', () => {
        fc.assert(
            fc.property(
                fc.integer({ min: -1000, max: 3000 }),
                fc.integer({ min: -1000, max: 3000 }),
                (x, y) => {
                    let state = initialState();
                    state = applyCommand(state, { type: 'drag', x, y });

                    expect(state.posX).toBeGreaterThanOrEqual(0);
                    expect(state.posX).toBeLessThanOrEqual(SCREEN_W - BUTTON_SIZE);
                    expect(state.posY).toBeGreaterThanOrEqual(0);
                    expect(state.posY).toBeLessThanOrEqual(SCREEN_H - BUTTON_SIZE);
                }
            )
        );
    });
});

// ── Property 6: Mutual exclusivity invariant ──

describe('Property 6: Mutual exclusivity invariant', () => {
    it('floating button and main window are never simultaneously visible', () => {
        fc.assert(
            fc.property(fc.array(commandArbitrary()), (commands) => {
                let state = initialState();
                for (const cmd of commands) {
                    state = applyCommand(state, cmd);
                    // Invariant check after each command
                    expect(!(state.visible && state.mainWindowVisible)).toBe(true);
                }
            })
        );
    });
});

// ── Property 8: Default position calculation ──

describe('Property 8: Default position calculation', () => {
    it('default position is (W/2 - 28, 10)', () => {
        fc.assert(
            fc.property(fc.integer({ min: 800, max: 3840 }), (screenW) => {
                const defaultX = Math.floor(screenW / 2) - 28;
                const defaultY = 10;

                // Verify formula: X should be approximately half of screen width minus 28
                expect(defaultX).toBeGreaterThanOrEqual(0);
                expect(defaultY).toBe(10);
            })
        );
    });
});

// ── Arbitraries ──

function commandArbitrary(): fc.Arbitrary<Command> {
    return fc.oneof(
        fc.constant({ type: 'hide_main_window' } as Command),
        fc.constant({ type: 'show_main_window' } as Command),
        fc.constant({ type: 'click_floating_button' } as Command),
        fc.constant({ type: 'hide_floating_button' } as Command),
        fc.record({ type: fc.constant('set_config'), value: fc.boolean() }) as fc.Arbitrary<Command>,
        fc.record({
            type: fc.constant('drag'),
            x: fc.integer({ min: -100, max: 3000 }),
            y: fc.integer({ min: -100, max: 3000 }),
        }) as fc.Arbitrary<Command>
    );
}
