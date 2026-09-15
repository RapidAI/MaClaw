/** Run `tick` on an interval, pausing while the document is hidden. */
export function startVisibleInterval(tick: () => void, ms: number): () => void {
    let timer: ReturnType<typeof setInterval> | null = null;
    const start = () => {
        if (!timer) timer = setInterval(tick, ms);
    };
    const stopTimer = () => {
        if (timer) {
            clearInterval(timer);
            timer = null;
        }
    };
    const hidden = () => typeof document !== 'undefined' && document.visibilityState === 'hidden';
    const onVisibility = () => {
        if (hidden()) {
            stopTimer();
            return;
        }
        start();
        tick();
    };
    if (!hidden()) start();
    if (typeof document !== 'undefined') {
        document.addEventListener('visibilitychange', onVisibility);
    }
    return () => {
        stopTimer();
        if (typeof document !== 'undefined') {
            document.removeEventListener('visibilitychange', onVisibility);
        }
    };
}
