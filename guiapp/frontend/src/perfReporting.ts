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

/**
 * Reports that arrive before the Wails bindings are injected are queued rather
 * than dropped. The paint observer fires within the first frames, which is
 * typically before window.go exists, so dropping here would lose exactly the
 * startup marks this module exists to capture.
 */
const pendingReports: Array<Record<string, unknown>> = [];
let flushTimer: ReturnType<typeof setTimeout> | null = null;

function flushPending(): void {
    flushTimer = null;
    const fn = diagnosticFn();
    if (!fn) {
        // Bindings still absent: keep waiting rather than discarding.
        if (pendingReports.length > 0) scheduleFlush();
        return;
    }
    while (pendingReports.length > 0) {
        const payload = pendingReports.shift()!;
        try {
            void Promise.resolve(fn(payload)).catch(() => {});
        } catch {
            // Diagnostics must never break the app.
        }
    }
}

function scheduleFlush(): void {
    if (flushTimer !== null) return;
    flushTimer = setTimeout(flushPending, 100);
}

function report(payload: Record<string, unknown>): void {
    if (!diagnosticFn()) {
        // Bounded queue: a broken binding must not grow without limit.
        if (pendingReports.length < 50) pendingReports.push(payload);
        scheduleFlush();
        return;
    }
    try {
        void Promise.resolve(diagnosticFn()!(payload)).catch(() => {});
    } catch {
        // Diagnostics must never break the app.
    }
}

function reportPerf(stage: string, extra: Record<string, unknown> = {}): void {
    report({ tag: DIAG_TAG, stage, ts: Date.now(), ...extra });
}

/**
 * Reports that the document reached the renderer. This duplicates the
 * index.html boot-ping on purpose: that ping has been observed not to arrive on
 * some launches, which silently left the backend timeline without html/react
 * marks. The renderer reporting through the Wails binding is a second,
 * independent path to the same fact.
 */
export function reportHtmlReady(): void {
    reportPerf('html-ready', { metric: 'module-eval' });
}

/**
 * Reports the first contentful paint. Called once, as early as the browser
 * exposes the entry. `first-paint` is the moment the user stopped looking at a
 * blank window.
 *
 * WebView2 does not always emit paint entries to a `buffered: true` observer
 * created after the entries were recorded, so the navigation timing fallback
 * runs unconditionally rather than only on observer failure.
 */
export function reportFirstPaint(): void {
    let done = false;
    const emit = (value: number, source: string) => {
        if (done || !Number.isFinite(value) || value <= 0) return;
        done = true;
        reportPerf('first-paint', { metric: source, value: Math.round(value) });
    };

    try {
        if (typeof PerformanceObserver !== 'undefined') {
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
        }
    } catch {
        // Older WebView2 builds without paint entries: the fallback covers it.
    }

    // Fallback: derive a paint estimate from navigation timing. This fires
    // shortly after load even when no paint entry ever arrives.
    const fallback = () => {
        if (done) return;
        try {
            const nav = performance.getEntriesByType('navigation')[0] as PerformanceNavigationTiming | undefined;
            if (nav) {
                // domContentLoadedEventEnd is the most reliable "the page has
                // content" signal available across WebView2 versions. Use
                // responseEnd when the DCL timing is missing (0), which happens
                // on some embedded Chromium builds.
                const candidate = nav.domContentLoadedEventEnd || nav.responseEnd || nav.loadEventEnd;
                emit(candidate, 'nav-timing');
                return;
            }
            const legacy = performance.timing;
            if (legacy && legacy.domContentLoadedEventEnd > 0 && legacy.navigationStart > 0) {
                emit(legacy.domContentLoadedEventEnd - legacy.navigationStart, 'dcl-legacy');
                return;
            }
        } catch {
            // Nothing measurable on this build.
        }
        // Last resort: report the moment the module ran, so the timeline still
        // records that first paint happened rather than silently omitting it.
        emit(performance.now(), 'module-eval');
    };
    // Give the paint observer a moment first; the fallback then covers builds
    // where it never fires.
    setTimeout(fallback, 1500);
}

/**
 * Reports the moment the app became usable.
 *
 * `durationMs` is measured on the renderer clock (performance.now()), which
 * starts when the document begins loading, not when the process started. The
 * backend keeps its own process-relative mark; this value is the renderer-side
 * view and is logged alongside it under `metric`.
 */
export function reportInteractive(durationMs: number, source: string): void {
    reportPerf('interactive', { metric: source, value: Math.round(durationMs) });
}

/**
 * Samples long tasks (main-thread work over 50ms). On a slow machine these are
 * what make clicks feel ignored. Reported as an aggregate plus the worst
 * offender's start offset, because "a 7s stall exists" is only actionable if we
 * know when it happened (chunk parse, first render, state hydration, ...).
 */
export function startLongTaskSampling(): void {
    if (typeof PerformanceObserver === 'undefined') return;
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
    let maxAt = -1;
    try {
        const observer = new PerformanceObserver((list) => {
            for (const entry of list.getEntries()) {
                count += 1;
                totalMs += entry.duration;
                if (entry.duration > maxMs) {
                    maxMs = entry.duration;
                    maxAt = Math.round(entry.startTime);
                }
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
            max_at: maxAt,
        });
        count = 0;
        totalMs = 0;
        maxMs = 0;
        maxAt = -1;
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
    reportHtmlReady();
    reportFirstPaint();
    startLongTaskSampling();
    startFrameRateSampling();
}
