// Polyfill DOM methods not implemented in jsdom.
Element.prototype.scrollIntoView = Element.prototype.scrollIntoView || function () {};

// Stub the Wails runtime bridge so components that subscribe to backend
// events via wailsjs/runtime (EventsOn/EventsOff/...) do not crash under
// jsdom where window.runtime is undefined.
const wailsRuntimeStub = new Proxy({} as Record<string, unknown>, {
    get: (_target, prop) => {
        if (prop === 'EventsOn' || prop === 'EventsOnMultiple' || prop === 'EventsOnce') {
            return () => () => {};
        }
        return () => Promise.resolve(undefined);
    },
});
(window as any).runtime = wailsRuntimeStub;
