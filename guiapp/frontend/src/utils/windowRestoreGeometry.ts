export type WindowRestoreGeometry = {
    width: number;
    height: number;
    x: number;
    y: number;
};

export type WindowGeometryAPI = {
    getSize: () => Promise<{ w?: number; width?: number; h?: number; height?: number }>;
    getPosition: () => Promise<{ x?: number; y?: number }>;
    setSize: (width: number, height: number) => void;
    setPosition: (x: number, y: number) => void;
};

const MIN_RESTORE_WIDTH = 160;
const MIN_RESTORE_HEIGHT = 120;
/** Re-apply after OS Unmaximise, which can stomp SetSize with the corrupted restore rect. */
export const WINDOW_RESTORE_REASSERT_MS = 80;
/** Second write after resize-debounce/OS restore-rect settle. */
export const WINDOW_RESTORE_REASSERT_LATE_MS = 250;
/** Cover resize debounce (150ms) + clamp delay (80ms) without blocking a later OS maximize. */
export const WINDOW_RESTORE_CLAMP_SUPPRESS_MS = 400;
const WINDOW_RESTORE_REASSERT_DELAYS_MS = [WINDOW_RESTORE_REASSERT_MS, WINDOW_RESTORE_REASSERT_LATE_MS] as const;

export function parseWindowRestoreGeometry(
    size: { w?: number; width?: number; h?: number; height?: number } | null | undefined,
    position: { x?: number; y?: number } | null | undefined,
): WindowRestoreGeometry | null {
    const width = Number(size?.w ?? size?.width);
    const height = Number(size?.h ?? size?.height);
    const x = Number(position?.x);
    const y = Number(position?.y);
    if (!Number.isFinite(width) || !Number.isFinite(height) || width < MIN_RESTORE_WIDTH || height < MIN_RESTORE_HEIGHT) {
        return null;
    }
    return {
        width: Math.round(width),
        height: Math.round(height),
        x: Number.isFinite(x) ? Math.round(x) : 0,
        y: Number.isFinite(y) ? Math.round(y) : 0,
    };
}

export async function captureWindowRestoreGeometry(api: WindowGeometryAPI): Promise<WindowRestoreGeometry | null> {
    try {
        const [size, position] = await Promise.all([api.getSize(), api.getPosition()]);
        return parseWindowRestoreGeometry(size, position);
    } catch {
        return null;
    }
}

export function applyWindowRestoreGeometry(
    api: Pick<WindowGeometryAPI, "setSize" | "setPosition">,
    geometry: WindowRestoreGeometry | null,
): boolean {
    if (!geometry) return false;
    try {
        api.setSize(geometry.width, geometry.height);
        api.setPosition(geometry.x, geometry.y);
        return true;
    } catch {
        return false;
    }
}

export function createWindowMaximizeRestoreSession(
    api: WindowGeometryAPI,
    schedule: (fn: () => void, ms: number) => void = (fn, ms) => { setTimeout(fn, ms); },
    now: () => number = () => Date.now(),
) {
    let opGen = 0;
    let snapshot: WindowRestoreGeometry | null = null;
    let suppressClampUntil = 0;

    const applySnapshot = (geometry: WindowRestoreGeometry | null) => applyWindowRestoreGeometry(api, geometry);

    return {
        beginMaximize(): number {
            return ++opGen;
        },
        isCurrent(op: number): boolean {
            return op === opGen;
        },
        async rememberNormal(op?: number): Promise<void> {
            const geo = await captureWindowRestoreGeometry(api);
            if (op !== undefined && op !== opGen) return;
            if (geo) snapshot = geo;
        },
        restoreNormal(): boolean {
            opGen += 1;
            suppressClampUntil = now() + WINDOW_RESTORE_CLAMP_SUPPRESS_MS;
            const geo = snapshot;
            snapshot = null;
            const applied = applySnapshot(geo);
            if (geo) {
                const op = opGen;
                for (const ms of WINDOW_RESTORE_REASSERT_DELAYS_MS) {
                    schedule(() => {
                        if (op !== opGen) return;
                        applySnapshot(geo);
                    }, ms);
                }
            }
            return applied;
        },
        shouldSkipClamp(): boolean {
            return now() < suppressClampUntil;
        },
    };
}
