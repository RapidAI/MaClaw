// guiapp wails-runtime shim (Phase 2 of the v2→v3 migration).
//
// The original file was wails-generated ("DO NOT EDIT") and delegated every
// call to `window.runtime`, the global the v2 runtime injects. Under wails3
// there is no window.runtime; this file keeps the exact same 52-export
// surface (803 call sites / 174 test mocks depend on the paths and names)
// and routes everything through ./v3bridge.js, which lazily loads the v3
// runtime module served at /wails/runtime.js.
//
// Known gaps (console.warn under wails3, to be closed after the smoke run):
//   - WindowSetSystemDefaultTheme/SetLightTheme/SetDarkTheme
//   - ScreenGetAll, Environment
//   - Log* go to console instead of the Go-side logger; LogFatal no longer
//     terminates the process
//   - CanResolveFilePaths/ResolveFilePaths are v2-only utilities
// The v2 build never loads this implementation's wails3 paths: index.html
// only wires the bridge when window.runtime is absent.

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

// v2 fast path: when the v2 runtime global is present (every v2 build), all
// calls delegate to it byte-for-byte as before the migration. The wails3
// bridge is used only when window.runtime is absent (wails3 builds). Without
// this guard a v2 binary embedding this shim loses its event channel
// entirely (the v3 runtime URL does not exist there) and boots to a white
// screen.
function hasV2Runtime() {
    return typeof window !== 'undefined' && !!window.runtime;
}

export function LogPrint(message) {
    if (hasV2Runtime()) return window.runtime.LogPrint(message);
    console.log(message);
}

export function LogTrace(message) {
    console.debug('[trace]', message);
}

export function LogDebug(message) {
    if (hasV2Runtime()) return window.runtime.LogDebug(message);
    console.debug(message);
}

export function LogInfo(message) {
    if (hasV2Runtime()) return window.runtime.LogInfo(message);
    console.info(message);
}

export function LogWarning(message) {
    if (hasV2Runtime()) return window.runtime.LogWarning(message);
    console.warn(message);
}

export function LogError(message) {
    if (hasV2Runtime()) return window.runtime.LogError(message);
    console.error(message);
}

export function LogFatal(message) {
    // v2 terminates the process; under wails3 this is only logged (gap).
    console.error('[fatal]', message);
}

export function EventsOnMultiple(eventName, callback, maxCallbacks) {
    if (hasV2Runtime()) return window.runtime.EventsOnMultiple(eventName, callback, maxCallbacks);
    return eventsOnMultiple(eventName, callback, maxCallbacks);
}

export function EventsOn(eventName, callback) {
    return EventsOnMultiple(eventName, callback, -1);
}

export function EventsOff(eventName, ...additionalEventNames) {
    if (hasV2Runtime()) return window.runtime.EventsOff(eventName, ...additionalEventNames);
    return eventsOff(eventName, ...additionalEventNames);
}

export function EventsOnce(eventName, callback) {
    return EventsOnMultiple(eventName, callback, 1);
}

export function EventsEmit(eventName) {
    const args = Array.prototype.slice.call(arguments, 1);
    if (hasV2Runtime()) return window.runtime.EventsEmit(eventName, ...args);
    return eventsEmit(eventName, ...args);
}

export function WindowReload() {
    if (hasV2Runtime()) return window.runtime.WindowReload();
    return callWindow('Reload');
}

export function WindowReloadApp() {
    if (hasV2Runtime()) return window.runtime.WindowReloadApp();
    return callWindow('Reload');
}

export function WindowSetAlwaysOnTop(b) {
    if (hasV2Runtime()) return window.runtime.WindowSetAlwaysOnTop(b);
    return callWindow('SetAlwaysOnTop', b);
}

export function WindowSetSystemDefaultTheme() {
    if (hasV2Runtime()) return window.runtime.WindowSetSystemDefaultTheme();
    return callWindow('SetSystemDefaultTheme');
}

export function WindowSetLightTheme() {
    if (hasV2Runtime()) return window.runtime.WindowSetLightTheme();
    return callWindow('SetLightTheme');
}

export function WindowSetDarkTheme() {
    if (hasV2Runtime()) return window.runtime.WindowSetDarkTheme();
    return callWindow('SetDarkTheme');
}

export function WindowCenter() {
    if (hasV2Runtime()) return window.runtime.WindowCenter();
    return callWindow('Center');
}

export function WindowSetTitle(title) {
    if (hasV2Runtime()) return window.runtime.WindowSetTitle(title);
    return callWindow('SetTitle', title);
}

export function WindowFullscreen() {
    if (hasV2Runtime()) return window.runtime.WindowFullscreen();
    return callWindow('Fullscreen');
}

export function WindowUnfullscreen() {
    if (hasV2Runtime()) return window.runtime.WindowUnfullscreen();
    return callWindow('UnFullscreen');
}

export function WindowIsFullscreen() {
    if (hasV2Runtime()) return window.runtime.WindowIsFullscreen();
    return callWindow('IsFullscreen');
}

export function WindowGetSize() {
    if (hasV2Runtime()) return window.runtime.WindowGetSize();
    return callWindow('GetSize');
}

export function WindowSetSize(width, height) {
    if (hasV2Runtime()) return window.runtime.WindowSetSize(width, height);
    return callWindow('SetSize', width, height);
}

export function WindowSetMaxSize(width, height) {
    if (hasV2Runtime()) return window.runtime.WindowSetMaxSize(width, height);
    return callWindow('SetMaxSize', width, height);
}

export function WindowSetMinSize(width, height) {
    if (hasV2Runtime()) return window.runtime.WindowSetMinSize(width, height);
    return callWindow('SetMinSize', width, height);
}

export function WindowSetPosition(x, y) {
    if (hasV2Runtime()) return window.runtime.WindowSetPosition(x, y);
    return callWindow('SetPosition', x, y);
}

export function WindowGetPosition() {
    if (hasV2Runtime()) return window.runtime.WindowGetPosition();
    return callWindow('GetPosition');
}

export function WindowHide() {
    if (hasV2Runtime()) return window.runtime.WindowHide();
    return callWindow('Hide');
}

export function WindowShow() {
    if (hasV2Runtime()) return window.runtime.WindowShow();
    return callWindow('Show');
}

export function WindowMaximise() {
    if (hasV2Runtime()) return window.runtime.WindowMaximise();
    return callWindow('Maximise');
}

export function WindowToggleMaximise() {
    if (hasV2Runtime()) return window.runtime.WindowToggleMaximise();
    return callWindow('ToggleMaximise');
}

export function WindowUnmaximise() {
    if (hasV2Runtime()) return window.runtime.WindowUnmaximise();
    return callWindow('UnMaximise');
}

export function WindowIsMaximised() {
    if (hasV2Runtime()) return window.runtime.WindowIsMaximised();
    return callWindow('IsMaximised');
}

export function WindowMinimise() {
    if (hasV2Runtime()) return window.runtime.WindowMinimise();
    return callWindow('Minimise');
}

export function WindowUnminimise() {
    if (hasV2Runtime()) return window.runtime.WindowUnminimise();
    return callWindow('UnMinimise');
}

export function WindowSetBackgroundColour(R, G, B, A) {
    if (hasV2Runtime()) return window.runtime.WindowSetBackgroundColour(R, G, B, A);
    return callWindow('SetBackgroundColour', R, G, B, A);
}

export function ScreenGetAll() {
    if (hasV2Runtime()) return window.runtime.ScreenGetAll();
    return callWindow('ScreenGetAll');
}

export function WindowIsMinimised() {
    if (hasV2Runtime()) return window.runtime.WindowIsMinimised();
    return callWindow('IsMinimised');
}

export function WindowIsNormal() {
    if (hasV2Runtime()) return window.runtime.WindowIsNormal();
    return callWindow('IsNormal');
}

export function BrowserOpenURL(url) {
    if (hasV2Runtime()) return window.runtime.BrowserOpenURL(url);
    return browserOpenURL(url);
}

export async function Environment() {
    console.warn('[wails3-bridge] Environment not available');
    return {};
}

export function Quit() {
    if (hasV2Runtime()) return window.runtime.Quit();
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
    if (hasV2Runtime()) return window.runtime.ClipboardGetText();
    return clipboardText();
}

export function ClipboardSetText(text) {
    if (hasV2Runtime()) return window.runtime.ClipboardSetText(text);
    return clipboardSetText(text);
}

// v2 delivers resolved absolute paths via callback(x, y, paths).
export function OnFileDrop(callback, useDropTarget) {
    if (hasV2Runtime()) return window.runtime.OnFileDrop(callback, useDropTarget);
    return eventsOnMultiple('common:WindowFilesDropped', (x, y, paths) => callback(x, y, paths), -1);
}

export function OnFileDropOff() {
    if (hasV2Runtime()) return window.runtime.OnFileDropOff();
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
