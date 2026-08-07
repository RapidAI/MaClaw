import { BeginWindowDrag } from '../../wailsjs/go/main/App';

// Custom window-drag handling for the frameless window.
//
// Wails' built-in --wails-draggable handling on Windows fires an async
// "drag" invoke on the first mousemove after mousedown; if the button has
// already been released by the time the Go side posts
// WM_NCLBUTTONDOWN/HTCAPTION, the window wedges in the native modal move loop
// (UI renders but no clicks/drag work until the process is killed).
//
// Instead, drag regions carry a `data-window-drag` attribute; this handler
// arms on mousedown and calls the guarded BeginWindowDrag binding only after
// the pointer has travelled past a threshold while the button is still held.
// The Go side re-checks the physical button state before posting, closing the
// race. On macOS/Linux the built-in CSS drag stays enabled and BeginWindowDrag
// is a no-op.
//
// Listeners attach to `window` in the bubble phase (same as the Wails
// runtime) so existing stopPropagation() calls on inner controls (tool
// select, title-bar buttons, ...) keep working as before.

const DRAG_THRESHOLD_PX = 4;

let armX = 0;
let armY = 0;
let armed = false;

export function installWindowDragHandler(): void {
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
        armed = false; // one shot per mousedown, like the Wails runtime
        if ((event.buttons & 1) === 0) return;
        const dx = Math.abs(event.screenX - armX);
        const dy = Math.abs(event.screenY - armY);
        if (dx + dy < DRAG_THRESHOLD_PX) return;
        void BeginWindowDrag().catch((err) => console.warn('[window-drag] BeginWindowDrag failed:', err));
    });

    window.addEventListener('mouseup', () => {
        armed = false;
    });
}
