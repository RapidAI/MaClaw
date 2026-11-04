/*
 _	   __	  _ __
| |	 / /___ _(_) /____
| | /| / / __ `/ / / ___/
| |/ |/ / /_/ / / (__  )
|__/|__/\__,_/_/_/____/
The electron alternative for Go
(c) Lea Anthony 2019-present
*/
/* jshint esversion: 9 */
import * as Log from './log';
import {
  eventListeners,
  EventsEmit,
  EventsNotify,
  EventsOff,
  EventsOffAll,
  EventsOn,
  EventsOnce,
  EventsOnMultiple,
} from "./events";
import { Call, Callback, callbacks } from './calls';
import { SetBindings } from "./bindings";
import * as Window from "./window";
import * as Screen from "./screen";
import * as Browser from "./browser";
import * as Clipboard from "./clipboard";
import * as DragAndDrop from "./draganddrop";
import * as ContextMenu from "./contextmenu";
import { isResizeCursor, resizeCursorForPointer } from "./resize_cursor";

export function Quit() {
    window.WailsInvoke('Q');
}

export function Show() {
    window.WailsInvoke('S');
}

export function Hide() {
    window.WailsInvoke('H');
}

export function Environment() {
    return Call(":wails:Environment");
}

// The JS runtime
window.runtime = {
    ...Log,
    ...Window,
    ...Browser,
    ...Screen,
    ...Clipboard,
    ...DragAndDrop,
    EventsOn,
    EventsOnce,
    EventsOnMultiple,
    EventsEmit,
    EventsOff,
    EventsOffAll,
    Environment,
    Show,
    Hide,
    Quit
};

// Internal wails endpoints
window.wails = {
    Callback,
    EventsNotify,
    SetBindings,
    eventListeners,
    callbacks,
    flags: {
        disableScrollbarDrag: false,
        disableDefaultContextMenu: false,
        enableResize: false,
        defaultCursor: null,
        borderThickness: 6,
        shouldDrag: false,
        deferDragToMouseMove: true,
        cssDragProperty: "--wails-draggable",
        cssDragValue: "drag",
        cssDropProperty: "--wails-drop-target",
        cssDropValue: "drop",
        enableWailsDragAndDrop: false,
    }
};

// Set the bindings
if (window.wailsbindings) {
    window.wails.SetBindings(window.wailsbindings);
    delete window.wails.SetBindings;
}

// (bool) This is evaluated at build time in package.json
if (!DEBUG) {
    delete window.wailsbindings;
}

let dragTest = function(e) {
    var val = window.getComputedStyle(e.target).getPropertyValue(window.wails.flags.cssDragProperty);
    if (val) {
        val = val.trim();
    }

    if (val !== window.wails.flags.cssDragValue) {
        return false;
    }

    if (e.buttons !== 1) {
        // Do not start dragging if not the primary button has been clicked.
        return false;
    }

    if (e.detail !== 1) {
        // Do not start dragging if more than once has been clicked, e.g. when double clicking
        return false;
    }

    return true;
};

window.wails.setCSSDragProperties = function(property, value) {
    window.wails.flags.cssDragProperty = property;
    window.wails.flags.cssDragValue = value;
}

window.wails.setCSSDropProperties = function(property, value) {
    window.wails.flags.cssDropProperty = property;
    window.wails.flags.cssDropValue = value;
}

window.addEventListener('mousedown', (e) => {
    // Check for resizing
    if (window.wails.flags.resizeEdge) {
        window.WailsInvoke("resize:" + window.wails.flags.resizeEdge);
        e.preventDefault();
        return;
    }

    if (dragTest(e)) {
        if (window.wails.flags.disableScrollbarDrag) {
            // This checks for clicks on the scroll bar
            if (e.offsetX > e.target.clientWidth || e.offsetY > e.target.clientHeight) {
                return;
            }
        }
        if (window.wails.flags.deferDragToMouseMove) {
            window.wails.flags.shouldDrag = true;
        } else {
            e.preventDefault()
            window.WailsInvoke("drag");
        }
        return;
    } else {
        window.wails.flags.shouldDrag = false;
    }
});

window.addEventListener('mouseup', () => {
    window.wails.flags.shouldDrag = false;
});

function restoredCursor() {
    const fallback = window.wails.flags.defaultCursor;
    return fallback && !isResizeCursor(fallback) ? fallback : "";
}

function setResize(cursor) {
    if (window.wails.flags.resizeEdge === cursor) return;
    document.documentElement.style.cursor = cursor || restoredCursor();
    window.wails.flags.resizeEdge = cursor;
}

// clearResizeCursor drops a sizing cursor left on the root element. Maximise
// and fullscreen do not draw that cursor, so leaving it there hides the pointer.
function clearResizeCursor() {
    const current = document.documentElement.style.cursor || "";
    if (!window.wails.flags.resizeEdge && !isResizeCursor(current)) return;
    document.documentElement.style.cursor = restoredCursor();
    window.wails.flags.resizeEdge = undefined;
}

// Called after the fullscreen size change. Edge tracking stays off until
// unfullscreen turns it back on, and a sizing cursor painted during the
// resize is removed here instead of waiting for the next mousemove.
window.wails.suspendResizeCursor = function() {
    window.wails.flags.enableResize = false;
    clearResizeCursor();
};

window.addEventListener('mousemove', function(e) {
    if (window.wails.flags.shouldDrag) {
        window.wails.flags.shouldDrag = false;
        let mousePressed = e.buttons !== undefined ? e.buttons : e.which;
        if (mousePressed > 0) {
            window.WailsInvoke("drag");
            return;
        }
    }
    if (!window.wails.flags.enableResize) {
        // Fullscreen leaves edge tracking off. Only a cursor already latched
        // on the root needs clearing; reading style on every move does not.
        if (window.wails.flags.resizeEdge !== undefined) clearResizeCursor();
        return;
    }
    if (window.wails.flags.defaultCursor == null) {
        const current = document.documentElement.style.cursor || "";
        window.wails.flags.defaultCursor = isResizeCursor(current) ? "" : current;
    }
    const cursor = resizeCursorForPointer(
        window.outerWidth,
        window.outerHeight,
        window.innerWidth,
        window.innerHeight,
        e.clientX,
        e.clientY,
        window.wails.flags.borderThickness,
    );
    if (!cursor) {
        if (window.wails.flags.resizeEdge !== undefined) clearResizeCursor();
        return;
    }
    setResize(cursor);
});

// Setup context menu hook
window.addEventListener('contextmenu', function(e) {
    // always show the contextmenu in debug & dev
    if (DEBUG) return;

    if (window.wails.flags.disableDefaultContextMenu) {
        e.preventDefault();
    } else {
        ContextMenu.processDefaultContextMenu(e);
    }
});

window.WailsInvoke("runtime:ready");