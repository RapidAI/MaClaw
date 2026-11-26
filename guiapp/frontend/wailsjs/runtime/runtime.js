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
    return eventsOnMultiple(eventName, callback, maxCallbacks);
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
