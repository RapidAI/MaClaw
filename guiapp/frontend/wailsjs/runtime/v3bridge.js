// wails3 migration bridge (Phase 2). Loads the v3 runtime module lazily and
// exposes the v2-shaped surface the guiapp frontend expects:
//   - window.runtime equivalent (events, window ops, clipboard, browser)
//   - window.go.main.App equivalent (Proxy → Call.ByName)
// Under the v2 build this file is never loaded: index.html installs it only
// when window.runtime is absent (v2 injects window.runtime natively).
//
// V3_SERVICE_PREFIX must match the wails3 service registration: package
// guiapp, service type App → "guiapp.App". Verify via the wails3 smoke run
// (dist/gate_wails3.exe) before cutting over.
export const V3_SERVICE_PREFIX = 'guiapp.App.';

let v3Promise = null;

// getV3 resolves the wails3 runtime module once. The dynamic import target
// is the module the v3 asset server serves to pages (see the Phase 0 spike).
export function getV3() {
    if (!v3Promise) {
        v3Promise = import(/* @vite-ignore */ '/wails' + '/runtime.js').then((m) => {
            // Some builds expose the API spread on the module root; pick the
            // known namespaces with fallbacks so either shape works.
            return {
                Events: m.Events || (m.default && m.default.Events),
                Call: m.Call || (m.default && m.default.Call),
                Window: m.Window || (m.default && m.default.Window),
                Clipboard: m.Clipboard || (m.default && m.default.Clipboard),
                Browser: m.Browser || (m.default && m.default.Browser),
                Quit: m.Quit || (m.default && m.default.Quit),
            };
        });
    }
    return v3Promise;
}

// windowGoProxy installs the window.go.main.App Proxy: every method access
// yields an async function that routes to Call.ByName. This keeps the
// generated wailsjs/go/main/App.js (window['go']['main']['App']['X'] calls)
// working unchanged under wails3.
export async function installWindowGoProxy() {
    const v3 = await getV3();
    if (!v3.Call) {
        console.warn('[wails3-bridge] Call namespace missing; window.go shim disabled');
        return;
    }
    window.go = window.go || {};
    window.go.main = window.go.main || {};
    window.go.main.App = new Proxy(window.go.main.App || {}, {
        get(_target, methodName) {
            if (typeof methodName !== 'string' || methodName === 'then') {
                return undefined;
            }
            return (...args) => v3.Call.ByName(V3_SERVICE_PREFIX + methodName, ...args);
        },
    });
}

// v2 events semantics: a single payload is delivered as ONE callback arg
// (array payloads included — never spread); multiple emit args are spread.
// Bridge convention: JS-side multi-arg emits are wrapped in a __maclawMulti
// marker (see eventsEmit); plain arrays from Go passes through as one arg,
// matching v2.
function adaptEventCallback(callback) {
    return (event) => {
        const data = event && typeof event === 'object' && 'data' in event ? event.data : event;
        if (data && typeof data === 'object' && Array.isArray(data.__maclawMulti)) {
            callback(...data.__maclawMulti);
            return;
        }
        callback(data);
    };
}

export async function eventsOnMultiple(eventName, callback, maxCallbacks) {
    const v3 = await getV3();
    const off = v3.Events.OnMultiple(eventName, adaptEventCallback(callback), maxCallbacks);
    // v2 returns a function that unsubscribes when called.
    return typeof off === 'function' ? off : () => { try { v3.Events.Off(eventName); } catch (_) {} };
}

export async function eventsOff(eventName, ...additionalEventNames) {
    const v3 = await getV3();
    for (const name of [eventName, ...additionalEventNames]) {
        try { v3.Events.Off(name); } catch (_) {}
    }
}

export async function eventsEmit(eventName, ...args) {
    const v3 = await getV3();
    // Mirror v2: one payload → single arg; several → array (matched by the
    // adaptEventCallback spread on the receiving side).
    if (args.length === 1) return v3.Events.Emit(eventName, args[0]);
    return v3.Events.Emit(eventName, { __maclawMulti: args });
}

// callWindow routes a v2 Window* call to the v3 Window namespace. Unmapped
// methods degrade to a console warning instead of crashing the caller — the
// smoke run's console is the checklist for closing remaining mappings.
export async function callWindow(v3Method, ...args) {
    const v3 = await getV3();
    const fn = v3.Window && v3.Window[v3Method];
    if (typeof fn !== 'function') {
        console.warn('[wails3-bridge] Window.' + v3Method + ' not available');
        return undefined;
    }
    return fn.apply(v3.Window, args);
}

export async function browserOpenURL(url) {
    const v3 = await getV3();
    return v3.Browser.OpenURL(url);
}

export async function clipboardText() {
    const v3 = await getV3();
    return v3.Clipboard.Text();
}

export async function clipboardSetText(text) {
    const v3 = await getV3();
    return v3.Clipboard.SetText(text);
}
