// guiapp wails-runtime shim (wails3 is the shipped runtime since the v2
// retirement). Keeps the exact 52-export surface the app and vitest mocks
// depend on, routing everything through ./v3bridge.js, which lazily loads
// the v3 runtime module served at /wails/runtime.js.
//
// Known gaps (console.warn): WindowSet*Theme family, ScreenGetAll,
// Environment; Log* go to console instead of the Go-side logger and
// LogFatal no longer terminates the process; CanResolveFilePaths and
// ResolveFilePaths were v2-only utilities.

import {
    eventsOnMultiple,
    eventsOff,
    eventsEmit,
    callWindow,
    browserOpenURL,
    clipboardText,
    clipboardSetText,
    getV3,
} from './v3bridge.js';


export function LogPrint(message) {
    console.log(message);
}

export function LogTrace(message) {
    console.debug('[trace]', message);
}

export function LogDebug(message) {
    console.debug(message);
}

export function LogInfo(message) {
    console.info(message);
}

export function LogWarning(message) {
    console.warn(message);
}

export function LogError(message) {
    console.error(message);
}

export function LogFatal(message) {
    // v2 terminates the process; under wails3 this is only logged (gap).
    console.error('[fatal]', message);
}

export function EventsOnMultiple(eventName, callback, maxCallbacks) {
    // v2 returns the unsubscribe function synchronously. React effect cleanups
    // (and `return EventsOn(...)` itself) call that value. The wails3 subscribe
    // is async because the runtime module loads lazily; handing the Promise
    // back makes the cleanup throw "is not a function". That commit-phase
    // error has no boundary above App, so React unmounts the tree and the
    // window stays white after the boot splash is removed.
    let unsubscribe = null;
    let cancelled = false;
    eventsOnMultiple(eventName, callback, maxCallbacks).then((off) => {
        if (cancelled) {
            if (typeof off === 'function') {
                try { off(); } catch (_) { /* listener already gone */ }
            }
            return;
        }
        unsubscribe = typeof off === 'function' ? off : null;
    }, () => {
        unsubscribe = null;
    });
    return function wails3EventsOff() {
        cancelled = true;
        if (typeof unsubscribe !== 'function') return;
        const off = unsubscribe;
        unsubscribe = null;
        try { off(); } catch (_) { /* listener already gone */ }
    };
}

export function EventsOn(eventName, callback) {
    return EventsOnMultiple(eventName, callback, -1);
}

export function EventsOff(eventName, ...additionalEventNames) {
    return eventsOff(eventName, ...additionalEventNames);
}

export function EventsOnce(eventName, callback) {
    return EventsOnMultiple(eventName, callback, 1);
}

export function EventsEmit(eventName) {
    const args = Array.prototype.slice.call(arguments, 1);
    return eventsEmit(eventName, ...args);
}

export function WindowReload() {
    return callWindow('Reload');
}

export function WindowReloadApp() {
    return callWindow('Reload');
}

export function WindowSetAlwaysOnTop(b) {
    return callWindow('SetAlwaysOnTop', b);
}

export function WindowSetSystemDefaultTheme() {
    return callWindow('SetSystemDefaultTheme');
}

export function WindowSetLightTheme() {
    return callWindow('SetLightTheme');
}

export function WindowSetDarkTheme() {
    return callWindow('SetDarkTheme');
}

export function WindowCenter() {
    return callWindow('Center');
}

export function WindowSetTitle(title) {
    return callWindow('SetTitle', title);
}

export function WindowFullscreen() {
    return callWindow('Fullscreen');
}

export function WindowUnfullscreen() {
    return callWindow('UnFullscreen');
}

export function WindowIsFullscreen() {
    return callWindow('IsFullscreen');
}

export function WindowGetSize() {
    return callWindow('GetSize');
}

export function WindowSetSize(width, height) {
    return callWindow('SetSize', width, height);
}

export function WindowSetMaxSize(width, height) {
    return callWindow('SetMaxSize', width, height);
}

export function WindowSetMinSize(width, height) {
    return callWindow('SetMinSize', width, height);
}

export function WindowSetPosition(x, y) {
    return callWindow('SetPosition', x, y);
}

export function WindowGetPosition() {
    return callWindow('GetPosition');
}

export function WindowHide() {
    return callWindow('Hide');
}

export function WindowShow() {
    return callWindow('Show');
}

export function WindowMaximise() {
    return callWindow('Maximise');
}

export function WindowToggleMaximise() {
    return callWindow('ToggleMaximise');
}

export function WindowUnmaximise() {
    return callWindow('UnMaximise');
}

export function WindowIsMaximised() {
    return callWindow('IsMaximised');
}

export function WindowMinimise() {
    return callWindow('Minimise');
}

export function WindowUnminimise() {
    return callWindow('UnMinimise');
}

export function WindowSetBackgroundColour(R, G, B, A) {
    return callWindow('SetBackgroundColour', R, G, B, A);
}

export function ScreenGetAll() {
    return callWindow('ScreenGetAll');
}

export function WindowIsMinimised() {
    return callWindow('IsMinimised');
}

export function WindowIsNormal() {
    return callWindow('IsNormal');
}

export function BrowserOpenURL(url) {
    return browserOpenURL(url);
}

export async function Environment() {
    console.warn('[wails3-bridge] Environment not available');
    return {};
}

export function Quit() {
    return getV3().then((v3) => {
        if (typeof v3.Quit === 'function') {
            return v3.Quit();
        }
        console.warn('[wails3-bridge] Quit not available');
    });
}

export function Hide() {
    return WindowHide();
}

export function Show() {
    return WindowShow();
}

export function ClipboardGetText() {
    return clipboardText();
}

export function ClipboardSetText(text) {
    return clipboardSetText(text);
}

// v2 delivers resolved absolute paths via callback(x, y, paths).
export function OnFileDrop(callback, useDropTarget) {
    return eventsOnMultiple('common:WindowFilesDropped', (x, y, paths) => callback(x, y, paths), -1);
}

export function OnFileDropOff() {
    return eventsOff('common:WindowFilesDropped');
}

export function CanResolveFilePaths(files) {
    console.warn('[wails3-bridge] CanResolveFilePaths is v2-only');
    return true;
}

export function ResolveFilePaths(files, dir) {
    console.warn('[wails3-bridge] ResolveFilePaths is v2-only');
    return files;
}
