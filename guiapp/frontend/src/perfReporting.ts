/**
 * Renderer-side performance reporting.
 *
 * The backend can measure when it finished its own work, but only the renderer
 * knows when a frame actually reached the screen. These reports complete the
 * startup timeline and add the runtime signals that explain "slow on low-end
 * hardware": long tasks (main-thread stalls), frame rate while idle, and the
 * size of the rendered DOM.
 *
 * Everything goes through LogFrontendDiagnostic, which already strips content
 * keys and truncates strings, so no user text can leak into the log.
 *
 * Reports are one-shot for startup marks and sampled at a low rate afterwards;
 * the goal is a usable baseline, not a profiler.
 */

const DIAG_TAG = 'startup-trace';

type DiagnosticFn = (payload: Record<string, unknown>) => unknown;

function diagnosticFn(): DiagnosticFn | null {
    const fn = (window as any)?.go?.main?.App?.LogFrontendDiagnostic;
    return typeof fn === 'function' ? fn : null;
}

function report(payload: Record<string, unknown>): void {
    const fn = diagnosticFn();
    if (!fn) return;
    try {
        void Promise.resolve(fn(payload)).catch(() => {});
    } catch {
        // Diagnostics must never break the app.
    }
}

function reportPerf(stage: string, extra: Record<string, unknown> = {}): void {
    report({ tag: DIAG_TAG, stage, ts: Date.now(), ...extra });
}

/**
 * Reports the first contentful paint. Called once, as early as the browser
 * exposes the entry. `first-paint` is the moment the user stopped looking at a
 * blank window.
 */
export function reportFirstPaint(): void {
    if (typeof PerformanceObserver === 'undefined') return;
    let done = false;
    const emit = (value: number, source: string) => {
        if (done) return;
        done = true;
        reportPerf('first-paint', { metric: source, value: Math.round(value) });
    };
    try {
        const observer = new PerformanceObserver((list) => {
            for (const entry of list.getEntries()) {
                if (entry.name === 'first-contentful-paint') {
                    emit(entry.startTime, 'fcp');
                    observer.disconnect();
                    return;
                }
            }
        });
        observer.observe({ type: 'paint', buffered: true });
        // Fall back to the legacy timing API when paint entries are unavailable.
        setTimeout(() => {
            const nav = performance.getEntriesByType('navigation')[0] as PerformanceNavigationTiming | undefined;
            if (nav && typeof nav.domContentLoadedEventEnd === 'number') {
                emit(nav.domContentLoadedEventEnd, 'dcl');
            }
        }, 3000);
    } catch {
        // Older WebView2 builds without paint entries: nothing to report.
    }
}

/**
 * Reports the moment the app became usable. A caller passes the measured
 * duration; this is deliberately explicit rather than inferred, because only
 * the app knows when its first interactive surface rendered.
 */
export function reportInteractive(durationMs: number, source: string): void {
    reportPerf('interactive', { metric: source, value: Math.round(durationMs) });
}

/**
 * Samples long tasks (main-thread work over 50ms). On a slow machine these are
 * what make clicks feel ignored. Reported as an aggregate so the log does not
 * grow one line per task.
 */
export function startLongTaskSampling(): void {
    if (typeof PerformanceObserver === 'undefined') return;
    if (typeof (window as any).PerformanceObserver?.supportedEntryTypes?.includes !== 'function') return;
    let supported = false;
    try {
        supported = PerformanceObserver.supportedEntryTypes.includes('longtask');
    } catch {
        supported = false;
    }
    if (!supported) return;

    let count = 0;
    let totalMs = 0;
    let maxMs = 0;
    try {
        const observer = new PerformanceObserver((list) => {
            for (const entry of list.getEntries()) {
                count += 1;
                totalMs += entry.duration;
                if (entry.duration > maxMs) maxMs = entry.duration;
            }
        });
        observer.observe({ type: 'longtask', buffered: false });
    } catch {
        return;
    }
    // One aggregate line per minute keeps the signal without flooding the log.
    setInterval(() => {
        if (count === 0) return;
        reportPerf('longtasks', {
            metric: 'longtask',
            value: Math.round(totalMs),
            count,
            max_ms: Math.round(maxMs),
        });
        count = 0;
        totalMs = 0;
        maxMs = 0;
    }, 60_000);
}

/**
 * Measures idle frame rate. A renderer forced onto the CPU (for example with
 * --disable-gpu) shows up here as a frame rate well below the display refresh
 * even when nothing is animating. Sampling pauses while the document is
 * hidden so a backgrounded window reports nothing rather than a false zero.
 */
export function startFrameRateSampling(): void {
    if (typeof requestAnimationFrame !== 'function') return;
    let frames = 0;
    let startedAt = 0;
    let running = false;

    const tick = () => {
        if (!running) return;
        frames += 1;
        requestAnimationFrame(tick);
    };

    const start = () => {
        if (running) return;
        running = true;
        frames = 0;
        startedAt = performance.now();
        requestAnimationFrame(tick);
    };

    const stop = () => {
        if (!running) return;
        running = false;
        const elapsed = performance.now() - startedAt;
        if (elapsed >= 1000 && frames > 0) {
            reportPerf('fps', {
                metric: 'raf',
                value: Math.round((frames / elapsed) * 1000),
                frames,
                window_ms: Math.round(elapsed),
            });
        }
        frames = 0;
    };

    start();
    if (typeof document !== 'undefined') {
        document.addEventListener('visibilitychange', () => {
            if (document.visibilityState === 'hidden') {
                stop();
            } else {
                start();
            }
        });
    }
    // Report on a slow cadence: frame rate is a trend, not an event.
    setInterval(() => {
        if (document.visibilityState === 'hidden') return;
        const elapsed = performance.now() - startedAt;
        if (elapsed >= 1000 && frames > 0) {
            reportPerf('fps', {
                metric: 'raf',
                value: Math.round((frames / elapsed) * 1000),
                frames,
                window_ms: Math.round(elapsed),
            });
        }
        frames = 0;
        startedAt = performance.now();
    }, 30_000);
}

/**
 * Reports the rendered DOM size. A large node count is the usual reason a
 * low-end machine cannot keep up with scrolling even when nothing is
 * animating.
 */
export function reportDomSize(source: string): void {
    try {
        const nodes = document.getElementsByTagName('*').length;
        reportPerf('dom-size', { metric: source, value: nodes });
    } catch {
        // Non-fatal.
    }
}

/** Installs all renderer-side performance reporting. Idempotent. */
export function installPerfReporting(): void {
    if (typeof window === 'undefined') return;
    if ((window as any).__MACLAW_PERF_INSTALLED) return;
    (window as any).__MACLAW_PERF_INSTALLED = true;
    reportFirstPaint();
    startLongTaskSampling();
    startFrameRateSampling();
}
