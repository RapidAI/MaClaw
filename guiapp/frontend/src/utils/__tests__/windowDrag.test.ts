import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const beginWindowDrag = vi.fn(() => Promise.resolve());
const wailsInvoke = vi.fn();

vi.mock('../../../wailsjs/go/main/App', () => ({
    BeginWindowDrag: () => beginWindowDrag(),
}));

import {
    DRAG_THRESHOLD_PX,
    installWindowDragHandler,
    resetWindowDragHandlerForTests,
    startNativeWindowDrag,
} from '../windowDrag';

function dispatchMouse(
    type: 'mousedown' | 'mousemove' | 'mouseup',
    init: MouseEventInit & { target?: EventTarget | null },
): void {
    const { target, ...rest } = init;
    const event = new MouseEvent(type, {
        bubbles: true,
        cancelable: true,
        ...rest,
    });
    if (target) {
        (target as EventTarget).dispatchEvent(event);
    } else {
        window.dispatchEvent(event);
    }
}

describe('startNativeWindowDrag', () => {
    afterEach(() => {
        delete (window as unknown as { WailsInvoke?: unknown }).WailsInvoke;
        beginWindowDrag.mockClear();
        wailsInvoke.mockClear();
    });

    it('prefers WailsInvoke("drag") on the host path', () => {
        (window as unknown as { WailsInvoke: typeof wailsInvoke }).WailsInvoke = wailsInvoke;
        startNativeWindowDrag();
        expect(wailsInvoke).toHaveBeenCalledWith('drag');
        expect(beginWindowDrag).not.toHaveBeenCalled();
    });

    it('falls back to BeginWindowDrag when WailsInvoke is missing', () => {
        startNativeWindowDrag();
        expect(beginWindowDrag).toHaveBeenCalledTimes(1);
    });
});

describe('installWindowDragHandler', () => {
    let dragRegion: HTMLDivElement;

    beforeEach(() => {
        beginWindowDrag.mockClear();
        wailsInvoke.mockClear();
        (window as unknown as { WailsInvoke: typeof wailsInvoke }).WailsInvoke = wailsInvoke;
        resetWindowDragHandlerForTests();
        installWindowDragHandler();
        dragRegion = document.createElement('div');
        dragRegion.setAttribute('data-window-drag', '');
        document.body.appendChild(dragRegion);
    });

    afterEach(() => {
        dragRegion.remove();
        resetWindowDragHandlerForTests();
        delete (window as unknown as { WailsInvoke?: unknown }).WailsInvoke;
    });

    it('does not start drag on sub-threshold jitter, then starts after threshold', () => {
        dispatchMouse('mousedown', {
            button: 0,
            detail: 1,
            buttons: 1,
            screenX: 100,
            screenY: 100,
            target: dragRegion,
        });

        // First mousemove often has tiny movement; must stay armed.
        dispatchMouse('mousemove', {
            buttons: 1,
            screenX: 100 + 1,
            screenY: 100 + 1,
        });
        expect(wailsInvoke).not.toHaveBeenCalled();

        // Cross threshold on a later move.
        dispatchMouse('mousemove', {
            buttons: 1,
            screenX: 100 + DRAG_THRESHOLD_PX,
            screenY: 100 + 1,
        });
        expect(wailsInvoke).toHaveBeenCalledWith('drag');
        expect(wailsInvoke).toHaveBeenCalledTimes(1);
        expect(beginWindowDrag).not.toHaveBeenCalled();
    });

    it('starts drag only once per mousedown after threshold', () => {
        dispatchMouse('mousedown', {
            button: 0,
            detail: 1,
            buttons: 1,
            screenX: 50,
            screenY: 50,
            target: dragRegion,
        });
        dispatchMouse('mousemove', {
            buttons: 1,
            screenX: 50 + DRAG_THRESHOLD_PX + 2,
            screenY: 50,
        });
        dispatchMouse('mousemove', {
            buttons: 1,
            screenX: 50 + DRAG_THRESHOLD_PX + 20,
            screenY: 50,
        });
        expect(wailsInvoke).toHaveBeenCalledTimes(1);
    });

    it('ignores mousedown outside data-window-drag regions', () => {
        const outside = document.createElement('div');
        document.body.appendChild(outside);
        try {
            dispatchMouse('mousedown', {
                button: 0,
                detail: 1,
                buttons: 1,
                screenX: 10,
                screenY: 10,
                target: outside,
            });
            dispatchMouse('mousemove', {
                buttons: 1,
                screenX: 10 + DRAG_THRESHOLD_PX + 5,
                screenY: 10,
            });
            expect(wailsInvoke).not.toHaveBeenCalled();
            expect(beginWindowDrag).not.toHaveBeenCalled();
        } finally {
            outside.remove();
        }
    });

    it('cancels arm on mouseup before threshold', () => {
        dispatchMouse('mousedown', {
            button: 0,
            detail: 1,
            buttons: 1,
            screenX: 0,
            screenY: 0,
            target: dragRegion,
        });
        dispatchMouse('mouseup', { button: 0, buttons: 0 });
        dispatchMouse('mousemove', {
            buttons: 1,
            screenX: DRAG_THRESHOLD_PX + 10,
            screenY: 0,
        });
        expect(wailsInvoke).not.toHaveBeenCalled();
    });
});
