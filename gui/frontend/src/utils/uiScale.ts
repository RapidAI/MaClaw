/**
 * DPI / resolution-aware UI scale helpers.
 *
 * The shell applies `--ui-scale` on `.app-scale-layer` (CSS `zoom` + inverse box
 * so the layer still fills the viewport). Wails is DPI-aware: `screen.width/height`
 * are logical CSS pixels; `devicePixelRatio` is the OS display scale
 * (100% ≈ 1, 150% ≈ 1.5, …).
 *
 * Saved config:
 *   - `ui_zoom_factor <= 0` → Auto (recommend from display metrics)
 *   - `ui_zoom_factor` in [0.5, 2] → Manual absolute scale
 */

export type DisplayMetrics = {
    screenWidth: number;
    screenHeight: number;
    devicePixelRatio: number;
};

export const UI_SCALE_MIN = 0.5;
export const UI_SCALE_MAX = 2.0;
/** Sentinel written to config for Auto mode. */
export const UI_SCALE_AUTO = 0;
/** Slider / config step (5%). */
export const UI_SCALE_STEP = 0.05;

const DEFAULT_METRICS: DisplayMetrics = {
    screenWidth: 1920,
    screenHeight: 1080,
    devicePixelRatio: 1,
};

export function readDisplayMetrics(): DisplayMetrics {
    if (typeof window === 'undefined') {
        return { ...DEFAULT_METRICS };
    }
    const screenWidth = window.screen?.width || window.innerWidth || DEFAULT_METRICS.screenWidth;
    const screenHeight = window.screen?.height || window.innerHeight || DEFAULT_METRICS.screenHeight;
    const devicePixelRatio = window.devicePixelRatio || DEFAULT_METRICS.devicePixelRatio;
    return {
        screenWidth: Math.max(1, screenWidth),
        screenHeight: Math.max(1, screenHeight),
        devicePixelRatio: Math.max(0.5, devicePixelRatio),
    };
}

/** Snap to UI_SCALE_STEP so Auto values align with the settings slider. */
export function quantizeUIScale(factor: number): number {
    if (!Number.isFinite(factor)) {
        return 1;
    }
    const steps = Math.round(factor / UI_SCALE_STEP);
    return Math.round(steps * UI_SCALE_STEP * 100) / 100;
}

/** Clamp a manual/effective scale into the supported range (never returns Auto). */
export function clampUIScale(factor: number): number {
    if (!Number.isFinite(factor)) {
        return 1;
    }
    const clamped = Math.min(UI_SCALE_MAX, Math.max(UI_SCALE_MIN, factor));
    return quantizeUIScale(clamped);
}

export function isUIScaleAuto(savedFactor: number | null | undefined): boolean {
    return savedFactor == null || !Number.isFinite(savedFactor) || savedFactor <= 0;
}

/**
 * Recommend a UI scale so compact rem chrome stays readable on low-DPI panels
 * without double-scaling on HiDPI (where the OS already enlarges logical pixels).
 *
 * Baseline: ~1920×1080 logical @ 100% → 1.05 (small bump for dense rem UI).
 */
export function recommendUIScale(metrics?: Partial<DisplayMetrics>): number {
    const m: DisplayMetrics = {
        ...readDisplayMetrics(),
        ...metrics,
    };
    const longSide = Math.max(m.screenWidth, m.screenHeight);
    const shortSide = Math.min(m.screenWidth, m.screenHeight);
    const dpr = m.devicePixelRatio;

    let scale = 1;

    if (dpr <= 1.05) {
        // True low-DPI / 100% OS scale: tiny rem labels (≈0.54–0.64rem) look broken.
        if (longSide <= 1366 || shortSide <= 768) {
            scale = 1.15;
        } else if (longSide <= 1600 || shortSide <= 900) {
            scale = 1.1;
        } else if (longSide <= 1920) {
            scale = 1.05;
        } else {
            scale = 1.0;
        }
    } else if (dpr < 1.4) {
        // ~125%: OS already enlarged; ease off when logical space is tight.
        scale = longSide <= 1440 || shortSide <= 900 ? 0.95 : 1.0;
    } else if (dpr < 1.8) {
        // ~150%
        if (longSide <= 1600 || shortSide <= 900) {
            scale = 0.95;
        } else if (longSide >= 2560) {
            scale = 1.05;
        } else {
            scale = 1.0;
        }
    } else {
        // 200%+ Retina / HiDPI — OS scaling is strong.
        if (longSide <= 1440 || shortSide <= 900) {
            scale = 0.95;
        } else if (longSide >= 2560) {
            scale = 1.05;
        } else {
            scale = 1.0;
        }
    }

    return clampUIScale(scale);
}

/** Resolve saved config (0 = Auto) to the effective scale applied to the UI. */
export function resolveUIScale(savedFactor: number | null | undefined, metrics?: Partial<DisplayMetrics>): number {
    if (isUIScaleAuto(savedFactor)) {
        return recommendUIScale(metrics);
    }
    return clampUIScale(savedFactor as number);
}

/** True when two scales are equal within config precision (2 decimals). */
export function uiScaleEquals(a: number, b: number): boolean {
    if (!Number.isFinite(a) || !Number.isFinite(b)) {
        return false;
    }
    return Math.round(a * 100) === Math.round(b * 100);
}

/** Integer percent for UI labels (e.g. 1.05 → 105). */
export function uiScaleToPercent(factor: number): number {
    return Math.round(clampUIScale(factor) * 100);
}

/**
 * Apply a saved config value (0 = Auto) to React zoom state setters.
 * Skips setState when the effective scale / mode is unchanged.
 */
export function applySavedUIZoomFactor(
    savedFactor: number | null | undefined,
    setUiZoomAuto: (auto: boolean | ((prev: boolean) => boolean)) => void,
    setUiZoom: (updater: number | ((prev: number) => number)) => void,
    metrics?: Partial<DisplayMetrics>,
): { auto: boolean; scale: number } {
    const auto = isUIScaleAuto(savedFactor);
    const scale = resolveUIScale(savedFactor, metrics);
    setUiZoomAuto((prev) => (prev === auto ? prev : auto));
    setUiZoom((prev) => (uiScaleEquals(prev, scale) ? prev : scale));
    return { auto, scale };
}

/**
 * Subscribe to display metrics changes that affect Auto UI scale.
 * Returns an unsubscribe function. Debounces resize; rebinds DPR media query.
 */
export function subscribeDisplayScaleChanges(
    onChange: () => void,
    options?: { resizeDebounceMs?: number },
): () => void {
    if (typeof window === 'undefined') {
        return () => {};
    }
    const debounceMs = options?.resizeDebounceMs ?? 150;
    let resizeTimer: ReturnType<typeof setTimeout> | null = null;
    let dprQuery: MediaQueryList | null = null;

    const onResize = () => {
        if (resizeTimer != null) {
            clearTimeout(resizeTimer);
        }
        resizeTimer = setTimeout(onChange, debounceMs);
    };

    const onDprChange = () => {
        onChange();
        attachDprListener();
    };

    const attachDprListener = () => {
        if (dprQuery) {
            dprQuery.removeEventListener('change', onDprChange);
        }
        dprQuery = window.matchMedia(`(resolution: ${window.devicePixelRatio}dppx)`);
        dprQuery.addEventListener('change', onDprChange);
    };

    window.addEventListener('resize', onResize);
    attachDprListener();

    return () => {
        if (resizeTimer != null) {
            clearTimeout(resizeTimer);
        }
        window.removeEventListener('resize', onResize);
        dprQuery?.removeEventListener('change', onDprChange);
    };
}
