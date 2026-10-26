import { useEffect } from 'react';
import { reportDomSize, reportInteractive } from '../../perfReporting';

/**
 * Reports the workbench DOM node count once the first render has settled.
 * Kept out of App.tsx so the startup instrumentation does not grow that file.
 */
export function useStartupPerfProbe(): void {
    useEffect(() => {
        // The workbench DOM is what a slow machine struggles to lay out, so
        // record its node count after the first render has had time to settle.
        const timer = window.setTimeout(() => reportDomSize('app-mounted'), 5000);
        return () => window.clearTimeout(timer);
    }, []);
    useInteractiveFallback();
}

/**
 * Reports the startup "interactive" mark two frames after the loading shell is
 * removed, which is when the workbench has actually painted.
 */
export function reportInteractiveAfterPaint(reason: string): void {
    const interactiveAt = performance.now();
    window.requestAnimationFrame(() => {
        window.requestAnimationFrame(() => {
            reportInteractive(interactiveAt, `loading-shell:${reason}`);
        });
    });
}

/**
 * Fallback "interactive" mark for the paths where the loading shell is never
 * removed (env-check skipped, or the event never reaches this listener). Without
 * it the timeline silently loses its most important number, which is exactly
 * what happened before: `finish-loading-shell` never fired and `interactive`
 * stayed unrecorded.
 *
 * The workbench is considered usable once the App subtree has rendered and two
 * frames have passed, capped so a stalled renderer still reports something.
 */
export function useInteractiveFallback(): void {
    useEffect(() => {
        const startedAt = performance.now();
        let cancelled = false;
        const timer = window.setTimeout(() => {
            if (cancelled) return;
            reportInteractive(startedAt, 'app-mounted-fallback');
        }, 8000);
        return () => {
            cancelled = true;
            window.clearTimeout(timer);
        };
    }, []);
}
