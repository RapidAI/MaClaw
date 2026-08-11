import { BeginWindowDrag } from '../../wailsjs/go/main/App';

// Custom window-drag handling for the frameless window on Windows.
//
// Wails' built-in --wails-draggable path is disabled on Windows (see
// cssDragPropertyOverride): its async "drag" message can arrive after mouse-up
// and wedge the window in the native modal move loop.
//
// Instead, drag regions carry `data-window-drag`. We arm on mousedown and only
// start a native move after the pointer travels past a threshold while the
// button is still held — so quick clicks never enter the move loop.
//
// Starting the move must use window.WailsInvoke("drag"), the same WebView
// host-thread path Wails uses internally. Calling a Go binding (BeginWindowDrag)
// runs ReleaseCapture/PostMessage off the UI thread; WebView2 keeps mouse
// capture and the window never moves. BeginWindowDrag remains a fallback only.
//
// On macOS/Linux the built-in CSS drag stays enabled; this handler still runs
// but WailsInvoke/BeginWindowDrag are effectively no-ops for move there.
//
// Listeners attach to `window` in the bubble phase so stopPropagation() on
// inner controls (tool select, title-bar buttons, ...) still blocks drag arming.

export const DRAG_THRESHOLD_PX = 4;

let armX = 0;
let armY = 0;
let armed = false;
let installed = false;

/** Test helper: reset module state between cases. */
export function resetWindowDragHandlerForTests(): void {
    armX = 0;
    armY = 0;
    armed = false;
}

type WailsInvokeFn = (message: string) => void;

/** Start native window move via Wails host path, with Go binding fallback. */
export function startNativeWindowDrag(): void {
    try {
        const wailsInvoke = (window as unknown as { WailsInvoke?: WailsInvokeFn }).WailsInvoke;
        if (typeof wailsInvoke === 'function') {
            wailsInvoke('drag');
            return;
        }
    } catch (err) {
        console.warn('[window-drag] WailsInvoke(drag) failed:', err);
    }
    void BeginWindowDrag().catch((err) => console.warn('[window-drag] BeginWindowDrag failed:', err));
}

export function installWindowDragHandler(): void {
    if (installed) return;
    installed = true;

    window.addEventListener('mousedown', (event: MouseEvent) => {
        // e.detail !== 1 skips double-clicks so double-click-to-maximize on the
        // header keeps working.
        if (event.button !== 0 || event.detail !== 1) {
            armed = false;
            return;
        }
        const target = event.target as Element | null;
        if (!target || !target.closest('[data-window-drag]')) {
            armed = false;
            return;
        }
        armX = event.screenX;
        armY = event.screenY;
        armed = true;
    });

    window.addEventListener('mousemove', (event: MouseEvent) => {
        if (!armed) return;
        // Button released (or never held) — cancel without starting a drag.
        if ((event.buttons & 1) === 0) {
            armed = false;
            return;
        }
        const dx = Math.abs(event.screenX - armX);
        const dy = Math.abs(event.screenY - armY);
        // Stay armed until the pointer actually travels past the threshold.
        // Early 0–2px jitter must not consume the one-shot arm.
        if (dx + dy < DRAG_THRESHOLD_PX) return;
        armed = false; // one successful start per mousedown
        startNativeWindowDrag();
    });

    window.addEventListener('mouseup', () => {
        armed = false;
    });
}
