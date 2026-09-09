export function isWailsAppReady(): boolean {
    const w = window as unknown as {
        go?: { main?: { App?: unknown }; guiapp?: { App?: unknown } };
        runtime?: unknown;
    };
    if (w.go?.guiapp && !w.go.main) {
        w.go.main = w.go.guiapp;
    }
    const runtime = w.runtime;
    return !!(w.go?.main?.App && runtime);
}

export function waitForWailsApp(timeoutMs = 8000): Promise<boolean> {
    if (isWailsAppReady()) return Promise.resolve(true);
    return new Promise((resolve) => {
        const started = Date.now();
        const timer = window.setInterval(() => {
            const ready = isWailsAppReady();
            if (ready || Date.now() - started >= timeoutMs) {
                window.clearInterval(timer);
                resolve(ready);
            }
        }, 50);
    });
}
