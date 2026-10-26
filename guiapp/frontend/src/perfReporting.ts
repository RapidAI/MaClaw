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
// Bindings appear within the first second of a healthy launch. If they still
// are not there after this window, the page is not running against the app
// shell (a browser preview, a failed bind) and the retry loop must stop rather
// than wake the renderer ten times a second forever.
const FLUSH_GIVE_UP_MS = 30_000;
const flushStartedAt = Date.now();

function flushPending(): void {
    flushTimer = null;
    const fn = diagnosticFn();
    if (!fn) {
        if (pendingReports.length > 0 && Date.now()-flushStartedAt < FLUSH_GIVE_UP_MS) {
            scheduleFlush();
            return;
        }
        pendingReports.length = 0;
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
 * what make clicks feel ignored.
 *
 * Each of the first few tasks is reported individually with its start offset, so
 * a multi-second stall can be lined up against the backend timeline in
 * maclaw.log (was it chunk parse, first render, history hydration?). After that
 * a single aggregate keeps watching without flooding the log.
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

    // Individual reports for the startup window, where a stall is most likely
    // and most useful to attribute. Beyond this the traffic is not worth it.
    const INDIVIDUAL_LIMIT = 12;
    let reported = 0;
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
                if (reported < INDIVIDUAL_LIMIT && entry.duration >= 250) {
                    reported += 1;
                    reportPerf('longtask', {
                        metric: 'single',
                        value: Math.round(entry.duration),
                        at: Math.round(entry.startTime),
                    });
                }
            }
        });
        observer.observe({ type: 'longtask', buffered: false });
    } catch {
        return;
    }
    // One aggregate line per minute keeps the trend without more noise.
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
 * Measures frame rate in short bursts.
 *
 * A permanently running requestAnimationFrame loop would itself keep the
 * compositor busy and prevent the renderer from ever going idle — the opposite
 * of what a slow machine needs. Each measurement therefore lasts only
 * BURST_MS and runs every INTERVAL_MS, and nothing at all is scheduled while
 * the document is hidden.
 *
 * The first burst starts a few seconds after launch, once startup work has
 * settled, so the number describes steady-state rendering rather than the
 * initial paint storm.
 */
export function startFrameRateSampling(): void {
    if (typeof requestAnimationFrame !== 'function') return;

    const BURST_MS = 2000;
    const INTERVAL_MS = 60_000;
    let frames = 0;
    let burstStart = 0;
    let burstTimer: ReturnType<typeof setTimeout> | null = null;
    let running = false;

    const tick = () => {
        if (!running) return;
        frames += 1;
        requestAnimationFrame(tick);
    };

    const stopBurst = () => {
        if (!running) return;
        running = false;
        if (burstTimer !== null) {
            clearTimeout(burstTimer);
            burstTimer = null;
        }
        const elapsed = performance.now() - burstStart;
        if (elapsed >= 500 && frames > 0) {
            reportPerf('fps', {
                metric: 'raf',
                value: Math.round((frames / elapsed) * 1000),
                frames,
                window_ms: Math.round(elapsed),
            });
        }
        frames = 0;
    };

    const startBurst = () => {
        if (running) return;
        if (typeof document !== 'undefined' && document.visibilityState === 'hidden') return;
        running = true;
        frames = 0;
        burstStart = performance.now();
        requestAnimationFrame(tick);
        burstTimer = setTimeout(stopBurst, BURST_MS);
    };

    if (typeof document !== 'undefined') {
        document.addEventListener('visibilitychange', () => {
            if (document.visibilityState === 'hidden') stopBurst();
        });
    }

    // First burst after the startup settle window, then on a slow cadence.
    setTimeout(startBurst, 8000);
    setInterval(startBurst, INTERVAL_MS);
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
