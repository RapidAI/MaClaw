import React from "react";
import { localizeText } from "./aiAssistantI18n";
import { relativeLuminance, type Theme } from "./aiAssistantPanelTheme";
import { chatCodeWellStyle, codeFenceInnerStyle, codeFenceScrollUnderLangStyle } from "./assistantCodeFence";
import { copyTextToClipboard } from "./AssistantReplyCopyButton";
import { typesetMermaidLabelMath } from "./mermaidLabelMath";

let mermaidModule: any = null;
let mermaidModulePromise: Promise<any> | null = null;
// Mermaid stores its configuration globally. Serialising initialise + render
// keeps diagrams in different chat messages from borrowing each other's theme.
let renderQueue: Promise<void> = Promise.resolve();
// Mermaid keeps its configuration in module scope and already serialises its own
// work behind an internal execution queue (verified in the browser: racing two
// initialise+render pairs does not let either borrow the other's theme). This
// chain is therefore belt-and-braces, not the thing standing between us and
// cross-contamination — which matters, because a queue nobody can escape costs
// more than it saves: every diagram in the session shares it, so one render that
// never settles would otherwise park every later diagram behind it forever.
const MERMAID_QUEUE_STALL_MS = 10_000;

/**
 * Run `work` once the previous diagram has finished, or after the stall cap,
 * whichever comes first. Whichever route fires first owns the work — waiting on
 * it twice would render one diagram twice for nothing.
 */
function runWhenQueued(work: () => Promise<void>): void {
    const previous = renderQueue;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let claimed = false;
    const claim = () => {
        if (claimed) return;
        claimed = true;
        if (timer) clearTimeout(timer);
        void work();
    };
    renderQueue = previous.catch(() => undefined).then(() => { claim(); }).catch(() => undefined);
    timer = setTimeout(claim, MERMAID_QUEUE_STALL_MS);
}
const MAX_MERMAID_SOURCE_LENGTH = 50_000;
const MAX_MERMAID_NODE_COUNT = 750;
const MAX_MERMAID_EDGE_COUNT = 1_500;

// 100% is the reading column, not Mermaid's user-unit width. A fixed multiple
// of that intrinsic size (the old 125% default) left a narrow graph as a small
// island: the bubble shrink-wrapped to the drawing and the rest of the column
// stayed empty. Zoom is a multiple of the column; the viewBox supplies the
// height, so the box cannot grow past the drawing. The bounds keep the
// controls from shrinking a diagram into noise or growing it until the scroll
// container becomes useless.
export const DEFAULT_MERMAID_ZOOM = 1;
export const MIN_MERMAID_ZOOM = 0.5;
export const MAX_MERMAID_ZOOM = 3;
const MERMAID_ZOOM_STEP = 0.25;

function round2(value: number): number {
    return Math.round(value * 100) / 100;
}

export function clampMermaidZoom(zoom: number): number {
    if (!Number.isFinite(zoom)) return DEFAULT_MERMAID_ZOOM;
    return Math.min(MAX_MERMAID_ZOOM, Math.max(MIN_MERMAID_ZOOM, zoom));
}

/** One press of +/-. Rounding to two decimals stops 0.25 steps from drifting. */
export function stepMermaidZoom(current: number, direction: 1 | -1): number {
    return clampMermaidZoom(round2(clampMermaidZoom(current) + direction * MERMAID_ZOOM_STEP));
}

/** viewBox width/height, or 0 when the root has no usable viewBox. */
function mermaidViewBoxSize(root: string): { width: number; height: number } {
    const viewBox = root.match(/\sviewBox="([^"]+)"/);
    const parts = viewBox ? viewBox[1].trim().split(/[\s,]+/).map(Number) : [];
    if (parts.length !== 4 || !(parts[2] > 0) || !(parts[3] > 0)) return { width: 0, height: 0 };
    return { width: parts[2], height: parts[3] };
}

/**
 * Rewrite the root <svg> so zoom resizes the viewport, not Mermaid's coordinates.
 *
 * Mermaid's `useMaxWidth` default emits `width="100%"` plus
 * `max-width: <user-units>px` and no height. That cap leaves a narrow graph
 * at its user-unit width, with empty column beside it. The viewport width is
 * a percentage of the reading column, and the height is the viewBox ratio of
 * that width, so the box and the drawing are the same rectangle. Zoom is that
 * percentage. Rewriting the markup keeps zoom a string re-render instead of a
 * second Mermaid pass. The viewBox itself has to be the drawing: a stylesheet
 * transition on foreignObject width/height makes getBBox keep Mermaid's
 * scratch size, and this ratio would then lock that blank square in.
 */
export function scaleMermaidSvg(svg: string, zoom: number): string {
    const factor = clampMermaidZoom(zoom);
    // Not anchored: a wrapper may emit an XML declaration, comment, or leading
    // whitespace before the root tag. Failing to find it would otherwise return
    // the SVG completely unscaled — a silent zoom failure. Keep any prefix.
    const open = svg.match(/<svg\b[^>]*>/);
    if (!open || open.index === undefined) return svg;
    const root = open[0];
    const offset = open.index;
    const tail = svg.slice(offset + root.length);

    const styleText = root.match(/style="([^"]*)"/)?.[1] ?? "";
    const styleMaxWidth = Number.parseFloat(styleText.match(/max-width:\s*([0-9.]+)px/i)?.[1] ?? "");
    const styleHeight = Number.parseFloat(styleText.match(/(?:^|;)\s*height:\s*([0-9.]+)px/i)?.[1] ?? "");
    const attrWidthRaw = root.match(/\swidth="([^"]*)"/)?.[1] ?? "";
    // A percentage width is the column, not a pixel size. Only a unitless
    // number or px value can stand in for the viewBox when Mermaid omitted one.
    const attrWidth = /%$/.test(attrWidthRaw) ? 0 : Number.parseFloat(attrWidthRaw);
    const attrHeight = Number.parseFloat(root.match(/\sheight="([0-9.]+)"/)?.[1] ?? "");
    const naturalWidth = Number.isFinite(styleMaxWidth) && styleMaxWidth > 0 ? styleMaxWidth
        : Number.isFinite(attrWidth) && attrWidth > 0 ? attrWidth : 0;
    const naturalHeight = Number.isFinite(attrHeight) && attrHeight > 0 ? attrHeight
        : Number.isFinite(styleHeight) && styleHeight > 0 ? styleHeight : 0;
    const viewBox = mermaidViewBoxSize(root);
    const aspectWidth = viewBox.width || naturalWidth;
    const aspectHeight = viewBox.height || naturalHeight;

    // Percentage of the reading column. A pixel width would re-introduce the
    // intrinsic-size cap this function exists to remove.
    const width = `${round2(factor * 100)}%`;
    let next = root.replace(/\swidth="[^"]*"/, ` width="${width}"`);
    if (next === root) next = next.replace(/<svg\b/, `<svg width="${width}"`);
    // A pixel height on the root locks the box to user units and fights the
    // percentage width. The foreignObject scratch size is a different element;
    // it never belongs on this attribute.
    next = next.replace(/\sheight="[^"]*"/, "");

    // Mermaid may pin width/height in the inline style (max-width cap, or an
    // explicit height). Those would fight the viewport we are about to write.
    const keptStyle = styleText
        .split(";")
        .map((part) => part.trim())
        .filter((part) => part && !/^(?:max-)?(?:width|height)\s*:/i.test(part) && !/^aspect-ratio\s*:/i.test(part) && !/^display\s*:/i.test(part));
    const viewportStyle = aspectWidth > 0 && aspectHeight > 0
        ? [`aspect-ratio: ${round2(aspectWidth)} / ${round2(aspectHeight)}`, "height: auto", "display: block"]
        : [];
    const styleValue = [...keptStyle, ...viewportStyle].join("; ");
    if (/style="/.test(next)) {
        next = styleValue
            ? next.replace(/style="[^"]*"/, `style="${styleValue}"`)
            : next.replace(/\sstyle="[^"]*"/, "");
    } else if (styleValue) {
        next = next.replace(/<svg\b/, `<svg style="${styleValue}"`);
    }

    return svg.slice(0, offset) + next + tail;
}

function mermaidZoomBarStyle(t: Theme): React.CSSProperties {
    return {
        display: "flex",
        alignItems: "center",
        justifyContent: "flex-end",
        gap: 4,
        color: t.textMuted,
    };
}

function mermaidZoomButtonStyle(t: Theme, hovered: boolean, disabled: boolean): React.CSSProperties {
    return {
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        minWidth: 22,
        height: 22,
        padding: "0 6px",
        boxSizing: "border-box",
        borderRadius: 6,
        border: `1px solid ${t.codeBlockBorder}`,
        background: hovered && !disabled ? t.codeBlockBg : "transparent",
        color: t.text,
        fontFamily: "inherit",
        fontSize: 12,
        fontWeight: 600,
        lineHeight: 1,
        cursor: disabled ? "default" : "pointer",
        opacity: disabled ? 0.4 : hovered ? 1 : 0.75,
        transition: "opacity 120ms ease, background 120ms ease",
    };
}

function MermaidZoomButton({ testId, label, disabled, onClick, theme, children }: {
    testId: string;
    label: string;
    disabled?: boolean;
    onClick: () => void;
    theme: Theme;
    children: React.ReactNode;
}) {
    const [hovered, setHovered] = React.useState(false);
    return (
        <button
            type="button"
            data-testid={testId}
            aria-label={label}
            title={label}
            disabled={disabled}
            onMouseEnter={() => setHovered(true)}
            onMouseLeave={() => setHovered(false)}
            onFocus={() => setHovered(true)}
            onBlur={() => setHovered(false)}
            onClick={onClick}
            style={mermaidZoomButtonStyle(theme, hovered, disabled === true)}
        >
            {children}
        </button>
    );
}

function getMermaid(): Promise<any> {
    if (mermaidModule) return Promise.resolve(mermaidModule);
    if (mermaidModulePromise) return mermaidModulePromise;
    mermaidModulePromise = import("mermaid")
        .then((module) => {
            mermaidModule = module.default || module;
            return mermaidModule;
        })
        .catch((error) => {
            // A transient chunk/network failure should not permanently disable
            // diagrams for the rest of the assistant session.
            mermaidModulePromise = null;
            throw error;
        });
    return mermaidModulePromise;
}

type Rgb = { r: number; g: number; b: number };

function parseHexColor(color: string): Rgb | null {
    const match = color.trim().match(/^#([\da-f]{3}|[\da-f]{6})$/i);
    if (!match) return null;
    const hex = match[1].length === 3 ? match[1].split("").map((channel) => channel + channel).join("") : match[1];
    return {
        r: Number.parseInt(hex.slice(0, 2), 16),
        g: Number.parseInt(hex.slice(2, 4), 16),
        b: Number.parseInt(hex.slice(4, 6), 16),
    };
}

function hexColor(rgb: Rgb): string {
    const channel = (value: number) => Math.round(Math.min(255, Math.max(0, value))).toString(16).padStart(2, "0");
    return `#${channel(rgb.r)}${channel(rgb.g)}${channel(rgb.b)}`;
}

function mixRgb(base: Rgb, accent: Rgb, accentShare: number): Rgb {
    const keep = 1 - accentShare;
    return {
        r: base.r * keep + accent.r * accentShare,
        g: base.g * keep + accent.g * accentShare,
        b: base.b * keep + accent.b * accentShare,
    };
}

function colorSpread(rgb: Rgb): number {
    return Math.max(rgb.r, rgb.g, rgb.b) - Math.min(rgb.r, rgb.g, rgb.b);
}

/** A blue seed for themes whose own accent is gray. Hue rotation of gray stays gray. */
const MERMAID_COLOR_SEED = "#3b82f6";

function mermaidAccent(theme: Theme): Rgb {
    for (const candidate of [theme.linkColor, theme.btnColor, theme.sendBtnBg]) {
        const rgb = parseHexColor(candidate);
        if (rgb && colorSpread(rgb) >= 28) return rgb;
    }
    return parseHexColor(MERMAID_COLOR_SEED) as Rgb;
}

function mermaidIsDark(theme: Theme): boolean {
    if (typeof theme.isDark === "boolean") return theme.isDark;
    const bg = parseHexColor(theme.fieldBg) || parseHexColor(theme.bg);
    if (!bg) return false;
    return (0.2126 * bg.r + 0.7152 * bg.g + 0.0722 * bg.b) / 255 < 0.45;
}

function contrastRatio(foreground: string, background: string): number {
    const fg = relativeLuminance(foreground);
    const bg = relativeLuminance(background);
    if (fg == null || bg == null) return 1;
    const lighter = Math.max(fg, bg);
    const darker = Math.min(fg, bg);
    return (lighter + 0.05) / (darker + 0.05);
}

/** Bubble text is tuned for the chat surface. On a node or note fill it often
 *  falls under 4.5:1, so keep it only when it still clears that bar. */
function readableInk(fill: string, preferred: string): string {
    const background = relativeLuminance(fill);
    const foreground = relativeLuminance(preferred);
    if (background != null && foreground != null) {
        const lighter = Math.max(background, foreground);
        const darker = Math.min(background, foreground);
        if ((lighter + 0.05) / (darker + 0.05) >= 4.5) return preferred;
    }
    const darkInk = relativeLuminance("#111111") ?? 0;
    if (background == null) return "#111111";
    const onDark = (background + 0.05) / (darkInk + 0.05);
    const onLight = 1.05 / (background + 0.05);
    return onDark >= onLight ? "#111111" : "#ffffff";
}

/** Node strokes share the fill's hue, so the raw accent can disappear into it.
 *  Nudge toward black or white only until the stroke clears `minimum` on the fill. */
function strokeColor(accent: Rgb, fill: string, dark: boolean, minimum = 3): string {
    const toward: Rgb = dark ? { r: 255, g: 255, b: 255 } : { r: 0, g: 0, b: 0 };
    const accentHex = hexColor(accent);
    if (contrastRatio(accentHex, fill) >= minimum) return accentHex;
    let lo = 0;
    let hi = 1;
    let best = hexColor(toward);
    for (let step = 0; step < 14; step++) {
        const mid = (lo + hi) / 2;
        const candidate = hexColor(mixRgb(accent, toward, mid));
        if (contrastRatio(candidate, fill) >= minimum) {
            best = candidate;
            hi = mid;
        } else {
            lo = mid;
        }
    }
    return best;
}

/** Arrows cross the page and the group fill. A line in the node hue merges
 *  with the box border, so this is a gray of the page that still clears 4.5:1
 *  on every surface it crosses. */
function neutralLine(surfaces: string[], dark: boolean): string {
    const toward: Rgb = dark ? { r: 255, g: 255, b: 255 } : { r: 0, g: 0, b: 0 };
    const anchor = parseHexColor(surfaces[0]) ?? (dark ? { r: 20, g: 20, b: 20 } : { r: 255, g: 255, b: 255 });
    const clears = (candidate: string) => surfaces.every((surface) => contrastRatio(candidate, surface) >= 4.5);
    let lo = 0;
    let hi = 1;
    let best = hexColor(toward);
    for (let step = 0; step < 14; step++) {
        const mid = (lo + hi) / 2;
        const candidate = hexColor(mixRgb(anchor, toward, mid));
        if (clears(candidate)) {
            best = candidate;
            hi = mid;
        } else {
            lo = mid;
        }
    }
    return best;
}

/** The quietest neutral that still clears `minimum` against the bubble. */
function visibleOn(surface: string, dark: boolean, minimum: number): string {
    const parsed = parseHexColor(surface);
    const base = parsed ?? (dark ? { r: 15, g: 23, b: 42 } : { r: 255, g: 255, b: 255 });
    const field = parsed ? surface : hexColor(base);
    const toward: Rgb = dark ? { r: 255, g: 255, b: 255 } : { r: 0, g: 0, b: 0 };
    let lo = 0;
    let hi = 1;
    let best = hexColor(toward);
    for (let step = 0; step < 14; step++) {
        const mid = (lo + hi) / 2;
        const candidate = hexColor(mixRgb(base, toward, mid));
        if (contrastRatio(candidate, field) >= minimum) {
            best = candidate;
            hi = mid;
        } else {
            lo = mid;
        }
    }
    return best;
}

function rotateHue(rgb: Rgb, degrees: number): Rgb {
    const r = rgb.r / 255;
    const g = rgb.g / 255;
    const b = rgb.b / 255;
    const max = Math.max(r, g, b);
    const min = Math.min(r, g, b);
    const lightness = (max + min) / 2;
    const delta = max - min;
    if (delta === 0) return rgb;
    const saturation = lightness > 0.5 ? delta / (2 - max - min) : delta / (max + min);
    let hue = 0;
    if (max === r) hue = ((g - b) / delta) % 6;
    else if (max === g) hue = (b - r) / delta + 2;
    else hue = (r - g) / delta + 4;
    hue = ((hue * 60 + degrees) % 360 + 360) % 360;
    const chroma = (1 - Math.abs(2 * lightness - 1)) * saturation;
    const x = chroma * (1 - Math.abs((hue / 60) % 2 - 1));
    const match = lightness - chroma / 2;
    let red = 0;
    let green = 0;
    let blue = 0;
    if (hue < 60) { red = chroma; green = x; }
    else if (hue < 120) { red = x; green = chroma; }
    else if (hue < 180) { green = chroma; blue = x; }
    else if (hue < 240) { green = x; blue = chroma; }
    else if (hue < 300) { red = x; blue = chroma; }
    else { red = chroma; blue = x; }
    return { r: (red + match) * 255, g: (green + match) * 255, b: (blue + match) * 255 };
}

/** Pull a hue-shifted fill toward the page only until the node ink still reads. */
function holdInk(fill: Rgb, ink: string, paper: Rgb): string {
    const plain = hexColor(fill);
    if (contrastRatio(ink, plain) >= 4.5) return plain;
    let lo = 0;
    let hi = 1;
    let best = hexColor(paper);
    for (let step = 0; step < 14; step++) {
        const mid = (lo + hi) / 2;
        const candidate = hexColor(mixRgb(fill, paper, mid));
        if (contrastRatio(ink, candidate) >= 4.5) {
            best = candidate;
            hi = mid;
        } else {
            lo = mid;
        }
    }
    return best;
}

/** A hue can land where neither black nor white quite reaches 4.5:1.
 *  Nudge that fill toward the ink's opposite until the chosen ink clears. */
function liftContrast(fill: Rgb, ink: string): string {
    const plain = hexColor(fill);
    if (contrastRatio(ink, plain) >= 4.5) return plain;
    const toward: Rgb = (relativeLuminance(ink) ?? 0) > 0.5 ? { r: 0, g: 0, b: 0 } : { r: 255, g: 255, b: 255 };
    let lo = 0;
    let hi = 1;
    let best = hexColor(toward);
    for (let step = 0; step < 14; step++) {
        const mid = (lo + hi) / 2;
        const candidate = hexColor(mixRgb(fill, toward, mid));
        if (contrastRatio(ink, candidate) >= 4.5) {
            best = candidate;
            hi = mid;
        } else {
            lo = mid;
        }
    }
    return best;
}

export function getMermaidTheme(theme: Theme) {
    const dark = mermaidIsDark(theme);
    const accent = mermaidAccent(theme);
    const paper = dark ? { r: 15, g: 23, b: 42 } : { r: 255, g: 255, b: 255 };
    // A light wash, then a hairline in the same hue. A heavier mix plus a
    // near-black rim is what makes the boxes look coarse.
    const primary = hexColor(mixRgb(paper, accent, dark ? 0.38 : 0.22));
    const note = hexColor(mixRgb(paper, { r: 234, g: 179, b: 8 }, dark ? 0.42 : 0.55));
    // Groups and edge chips stay in this hue. A full complement turns an
    // architecture diagram into orange panels and neon green labels.
    // The active bar is the strongest surface this ink is painted on.
    const active = hexColor(mixRgb(paper, accent, dark ? 0.72 : 0.62));
    const ink = readableInk(active, theme.text);
    const secondary = holdInk(mixRgb(paper, rotateHue(accent, -28), dark ? 0.3 : 0.2), ink, paper);
    const tertiary = holdInk(mixRgb(paper, accent, dark ? 0.22 : 0.16), ink, paper);
    const border = strokeColor(accent, primary, dark);
    const line = neutralLine([theme.fieldBg, tertiary, primary], dark);
    const done = hexColor(mixRgb(paper, accent, dark ? 0.34 : 0.28));
    const critAccent: Rgb = { r: 220, g: 38, b: 38 };
    const crit = hexColor(mixRgb(paper, critAccent, dark ? 0.62 : 0.5));
    const grid = visibleOn(theme.fieldBg, dark, 3);
    const themeVariables: Record<string, string | boolean> = {
            darkMode: dark,
            background: theme.fieldBg,
            primaryColor: primary,
            secondaryColor: secondary,
            tertiaryColor: tertiary,
            // The HTML label plate is this field. Left unset, a dark chat
            // darkens it by 30% after the contrast check.
            edgeLabelBackground: secondary,
            secondaryBorderColor: strokeColor(accent, secondary, dark),
            tertiaryBorderColor: strokeColor(accent, tertiary, dark),
            fontFamily: MERMAID_FONT,
            fontSize: "14px",
            primaryTextColor: ink,
            classText: ink,
            primaryBorderColor: border,
            textColor: theme.text,
            titleColor: ink,
            lineColor: line,
            arrowheadColor: line,
            // Architecture edges and group boxes default to gray and black.
            // Black dashes disappear on a dark bubble.
            archEdgeColor: line,
            archEdgeArrowColor: line,
            archGroupBorderColor: line,
            // Sequence messages read signalColor, which otherwise copies textColor
            // and stays the bubble gray while flowchart arrows follow lineColor.
            signalColor: line,
            sequenceNumberColor: readableInk(line, theme.text),
            noteBkgColor: note,
            noteTextColor: readableInk(note, theme.text),
            // State transition labels otherwise copy textColor and sit on the
            // hue-shifted chip. Alternate composite regions otherwise stay #efefef.
            transitionLabelColor: ink,
            altBackground: theme.fieldBg,
            // Slice labels use one ink on every wedge. Later wedges are held
            // to that ink; the default stroke is black and vanishes on a dark wedge.
            pieSectionTextColor: ink,
            pieStrokeColor: line,
            pieOuterStrokeColor: line,
            pieOpacity: "1",
            // Task labels sit on the bar. Mermaid's done-bar default is lightgrey,
            // so in a dark chat the light bubble text disappears on it.
            taskTextColor: ink,
            taskTextDarkColor: ink,
            taskTextClickableColor: ink,
            activeTaskBkgColor: active,
            activeTaskBorderColor: strokeColor(accent, active, dark),
            doneTaskBkgColor: done,
            doneTaskBorderColor: strokeColor(accent, done, dark),
            critBkgColor: crit,
            critBorderColor: strokeColor(critAccent, crit, dark),
            excludeBkgColor: hexColor(mixRgb(paper, accent, dark ? 0.16 : 0.1)),
            gridColor: grid,
            vertLineColor: line,
    };
    // Mindmap sections use cScale. Mermaid then darkens that scale by 75% in
    // dark mode, which turns an already-dark fill black. These overrides are
    // copied again after that pass. Hue shifts change luminance, so each
    // section gets its own label color.
    for (let i = 0; i < 12; i++) {
        const rotated = mixRgb(paper, rotateHue(accent, i * 30), dark ? 0.4 : 0.22);
        const label = readableInk(hexColor(rotated), ink);
        const fill = liftContrast(rotated, label);
        themeVariables[`cScale${i}`] = fill;
        themeVariables[`cScaleLabel${i}`] = label;
        // One label color is painted on every pie wedge.
        themeVariables[`pie${i + 1}`] = holdInk(rotated, ink, paper);
        if (i < 8) {
            themeVariables[`git${i}`] = fill;
            themeVariables[`gitBranchLabel${i}`] = themeVariables[`cScaleLabel${i}`];
            // Journey task labels are white, and a section title shares the
            // bar class, so the stylesheet paints that title with the bar fill.
            // Hold every bar until the one node ink still reads on it.
            themeVariables[`fillType${i}`] = holdInk(rotated, ink, paper);
        }
    }
    return {
        startOnLoad: false,
        theme: "base",
        // Chat responses are untrusted model output. Strict mode asks Mermaid to
        // sanitise diagram labels and disables executable click handlers.
        securityLevel: "strict",
        // WebView2 reports MathML support, so Mermaid would emit bare MathML.
        // Inside a label's foreignObject that MathML does not take KaTeX's
        // layout. HTML output uses the KaTeX stylesheet already on the page.
        legacyMathML: true,
        forceLegacyMathML: true,
        suppressErrorRendering: true,
        fontFamily: MERMAID_FONT,
        themeVariables,
    };
}

/**
 * Canonical diagram-type keywords, keyed by lower-case alias.
 *
 * Mermaid's diagram-type detectors are case-SENSITIVE regular expressions
 * (mermaid 11.14.0 registers `/^\s*sequenceDiagram/`, `/^\s*erDiagram/`,
 * `/^\s*gitGraph/`, `/^\s*quadrantChart/`, … — verified in
 * node_modules/mermaid/dist/mermaid.core.mjs) even though several diagram
 * grammars themselves accept lower-case keywords. Blindly lower-casing the
 * declaration therefore passes every parse-level check yet fails at detection
 * time with "No diagram type detected matching given configuration for text:
 * sequencediagram …". Each alias maps to the exact keyword its detector
 * expects.
 */
const MERMAID_DIAGRAM_TYPE_KEYWORDS: Record<string, string> = {
    graph: "graph",
    flowchart: "flowchart",
    sequencediagram: "sequenceDiagram",
    statediagram: "stateDiagram",
    "statediagram-v2": "stateDiagram-v2",
    classdiagram: "classDiagram",
    erdiagram: "erDiagram",
    gantt: "gantt",
    pie: "pie",
    gitgraph: "gitGraph",
    journey: "journey",
    mindmap: "mindmap",
    timeline: "timeline",
    quadrantchart: "quadrantChart",
    "sankey-beta": "sankey-beta",
    "xychart-beta": "xychart-beta",
};

/** Map any accepted casing of a Mermaid diagram type to its canonical keyword. */
export function canonicalMermaidDiagramKeyword(alias: string): string {
    return MERMAID_DIAGRAM_TYPE_KEYWORDS[alias.toLowerCase()] ?? alias;
}

/** Repair harmless casing errors commonly emitted by language models. */
export function sanitizeMermaidCode(raw: string): string {
    const diagramType = /^(\s*)(Graph|Flowchart|SequenceDiagram|StateDiagram|StateDiagram-v2|ClassDiagram|ErDiagram|Gantt|Pie|Gitgraph|Journey|Mindmap|Timeline|Quadrantchart|Sankey-beta|Xychart-beta)\b/i;
    const keyword = /^(\s*)(Subgraph|ClassDef|Style|Click|Note|Loop|Alt|Else|Opt|Par|Critical|Break|Rect|Activate|Deactivate|Direction)\b/i;
    return raw.split("\n").map((line) => {
        const diagramMatch = line.match(diagramType);
        if (diagramMatch) return `${diagramMatch[1]}${canonicalMermaidDiagramKeyword(diagramMatch[2])}${line.slice(diagramMatch[0].length)}`;
        if (/^(\s*)End\s*$/.test(line)) return line.replace(/End/i, "end");
        const keywordMatch = line.match(keyword);
        if (!keywordMatch) return line;
        const normalized = keywordMatch[2].toLowerCase() === "classdef" ? "classDef" : keywordMatch[2].toLowerCase();
        return `${keywordMatch[1]}${normalized}${line.slice(keywordMatch[0].length)}`;
    }).join("\n");
}

/**
 * Keep chat-supplied diagrams within a predictable rendering budget. This is
 * intentionally a simple source-level estimate: it runs before Mermaid parses
 * the diagram and protects the UI from accidentally generated giant graphs.
 */
export function exceedsMermaidRenderBudget(source: string): boolean {
    const nodeIds = new Set<string>();
    for (const match of source.matchAll(/(?:^|[\s;])([A-Za-z][\w-]*)(?:\s*(?:\[|\(|\{|-->|---|==>))/gm)) {
        nodeIds.add(match[1]);
        if (nodeIds.size > MAX_MERMAID_NODE_COUNT) return true;
    }
    const edgeCount = (source.match(/(?:-->|---|==>|-.->|==>)/g) || []).length;
    return edgeCount > MAX_MERMAID_EDGE_COUNT;
}

/** Why a diagram was refused before Mermaid ever saw it. Exported for copy tests. */
export function mermaidBudgetMessage(lang?: string): string {
    return localizeText(
        lang,
        `The Mermaid diagram exceeds the ${MAX_MERMAID_NODE_COUNT} node or ${MAX_MERMAID_EDGE_COUNT.toLocaleString()} edge rendering limit.`,
        `Mermaid 图表超过 ${MAX_MERMAID_NODE_COUNT} 节点或 ${MAX_MERMAID_EDGE_COUNT.toLocaleString()} 连线的渲染上限。`,
        `Mermaid 圖表超過 ${MAX_MERMAID_NODE_COUNT} 節點或 ${MAX_MERMAID_EDGE_COUNT.toLocaleString()} 連線的渲染上限。`,
    );
}

export function getMermaidFallbackMessage(error: string, lang?: string): string {
    const unable = localizeText(lang, "Unable to render this Mermaid diagram.", "无法渲染此 Mermaid 图表。", "無法渲染此 Mermaid 圖表。");
    // Refusals this component already wrote (empty source, length, budget) are
    // complete sentences in every language. Prefixing them again doubles the
    // lead. Mermaid's own parser text does not start this way, so it still
    // gets the localised lead.
    if (
        error.startsWith("The Mermaid")
        || error.startsWith("Unable to render this Mermaid diagram.")
        || error.startsWith(unable)
        || error.startsWith("Mermaid ")
    ) return error;
    return `${unable} ${error}`;
}

function errorMessage(reason: unknown, lang?: string): string {
    return reason instanceof Error && reason.message
        ? reason.message
        : localizeText(lang, "Unable to render this Mermaid diagram.", "无法渲染此 Mermaid 图表。", "無法渲染此 Mermaid 圖表。");
}

const MERMAID_FONT = 'system-ui, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif';
const MERMAID_STYLE_COLOR = /^#[0-9a-fA-F]{6}$/;

/** The bitmap behind a diagram. Arrows are a gray of this surface. */
export function mermaidRasterPaper(fieldBg: string | undefined): string {
    return fieldBg && MERMAID_STYLE_COLOR.test(fieldBg) ? fieldBg : "#ffffff";
}

/** Open upward when the card fits above and the space below is shorter than the card. */
export function mermaidMenuOpensUpward(spaceAbove: number, spaceBelow: number, height: number): boolean {
    if (!(height > 0)) return false;
    const room = height + 8;
    if (spaceBelow >= room) return false;
    if (spaceAbove >= room) return true;
    return spaceAbove > spaceBelow;
}

/** A 1-unit border grows with the column. This is that border in the drawing's own units, so one screen pixel at the measured width stays one pixel in the saved image. */
export function mermaidHairlineStroke(viewBoxWidth: number, displayWidth: number): number {
    if (!(viewBoxWidth > 0) || !(displayWidth > viewBoxWidth)) return 1;
    const unit = viewBoxWidth / displayWidth;
    const rounded = Math.round(unit * 100) / 100;
    return rounded > 0 ? rounded : unit;
}

function nodeRim(nodeStroke: number): number {
    if (!Number.isFinite(nodeStroke) || !(nodeStroke > 0) || nodeStroke > 1) return 1;
    return nodeStroke;
}

function mermaidKatexInkCss(scope: string, ink: string): string {
    // Mermaid paints every .node path, including KaTeX glyph strokes, and then
    // forces those strokes black at 1px. The formula follows the node ink.
    return `${scope} .katex{color:inherit}${scope} .node .katex path{fill:${ink};stroke:${ink};stroke-width:0.04em}`;
}

/** Same KaTeX ink override for a diagram that does not go through reinforceMermaidSvg. */
export function paintMermaidKatexInk(svg: string, ink: string): string {
    if (!MERMAID_STYLE_COLOR.test(ink)) return svg;
    const id = svg.match(/<svg\b[^>]*\sid="([^"]+)"/)?.[1];
    const close = svg.lastIndexOf("</svg>");
    if (!id || close < 0 || !/^[A-Za-z_][\w-]*$/.test(id)) return svg;
    const css = mermaidKatexInkCss(`#${id}`, ink);
    return `${svg.slice(0, close)}<style>${css}</style>${svg.slice(close)}`;
}

/** Mermaid prefixes every diagram rule with the svg id. One id beats any
 *  number of classes on the page, so the overrides have to live in the svg. */
export function reinforceMermaidSvg(svg: string, theme: Theme, nodeStroke = 1): string {
    const id = svg.match(/<svg\b[^>]*\sid="([^"]+)"/)?.[1];
    const close = svg.lastIndexOf("</svg>");
    if (!id || close < 0 || !/^[A-Za-z_][\w-]*$/.test(id)) return svg;
    const variables = getMermaidTheme(theme).themeVariables;
    const ink = String(variables.primaryTextColor);
    const chip = String(variables.secondaryColor);
    const line = String(variables.lineColor);
    const paper = theme.fieldBg;
    if (![ink, chip, line, paper].every((color) => MERMAID_STYLE_COLOR.test(color))) return svg;
    const scope = `#${id}`;
    const css = [
        `${scope} .statediagram .edgeLabel{color:${ink}}`,
        `${scope} .stateGroup .alt-composit{fill:${paper}}`,
        `${scope} [class*="section-edge-"]{stroke:${line}}`,
        `${scope} .edgeLabel .label rect,${scope} .edgeLabel .labelBkg,${scope} .labelBkg>.edgeLabel{opacity:1;background-color:${chip};fill:${chip};color:${ink}}`,
        `${scope} .arrowMarkerPath:not([fill]){fill:${line}}`,
        `${scope} .arrowMarkerPath:not([stroke]){stroke:${line}}`,
        `${scope} text.journey-section,${scope} [class*="task-type-"]~text.task{fill:${ink}}`,
        // Mindmap depth connectors are 17px, 14px, 11px. Section lines are 3px.
        // Only those. A blanket .edge rule would also thin architecture and timeline.
        `${scope} [class*="edge-depth-"],${scope} .mindmap-node line{stroke-width:1.25px}`,
        `${scope} .edgePath .path{stroke-width:1px}`,
        // Node rims use the column's pixel in drawing units. Connector lines
        // stay at 1 unit so they keep the same scale as their arrowheads.
        `${scope} .node rect,${scope} .node circle,${scope} .node ellipse,${scope} .node polygon,${scope} .node path,${scope} .cluster rect,${scope} .stateGroup rect,${scope} .entityBox,${scope} .transition{stroke-width:${nodeRim(nodeStroke)}px}`,
        mermaidKatexInkCss(scope, ink),
    ].join("");
    return `${svg.slice(0, close)}<style>${css}</style>${svg.slice(close)}`;
}

async function renderDiagram(
    mermaid: any,
    id: string,
    code: string,
    workspace: HTMLDivElement | null,
    theme: Theme,
    cancelled: () => boolean,
): Promise<string | null> {
    let resolveResult!: (svg: string | null) => void;
    let rejectResult!: (reason: unknown) => void;
    const result = new Promise<string | null>((resolve, reject) => {
        resolveResult = resolve;
        rejectResult = reject;
    });

    runWhenQueued(async () => {
        if (cancelled()) {
            resolveResult(null);
            return;
        }
        try {
            mermaid.initialize(getMermaidTheme(theme));
            const { svg } = await mermaid.render(id, typesetMermaidLabelMath(code), workspace ?? undefined);
            const root = svg.match(/<svg\b[^>]*>/)?.[0] ?? "";
            const display = workspace?.clientWidth || workspace?.parentElement?.clientWidth || 0;
            const stroke = mermaidHairlineStroke(mermaidViewBoxSize(root).width, display);
            resolveResult(reinforceMermaidSvg(svg, theme, stroke));
        } catch (error) {
            rejectResult(error);
        }
    });
    return result;
}

export function isMermaidCodeFence(language: string): boolean {
    return language.trim().split(/\s+/, 1)[0]?.toLowerCase() === "mermaid";
}

/** The diagram card asks the composer to quote this picture for the next turn. */
export const CITE_MERMAID_DIAGRAM_EVENT = "maclaw:cite-mermaid-diagram";

function mermaidFence(body: string): string {
    const marker = body.includes("```") ? "````" : "```";
    return `${marker}mermaid\n${body}\n${marker}`;
}

export function mermaidDiagramCitation(source: string, lang?: string): string {
    const lead = localizeText(lang, "Please continue from this diagram:", "请基于这张图继续：", "請基於這張圖繼續：");
    return `${lead}\n${mermaidFence(source.trim())}\n`;
}

/** Append a diagram quote without wiping text the reader already typed. */
export function applyMermaidDiagramCitation(source: string, lang: string | undefined, existing: string): string {
    const body = source.trim();
    if (!body) return existing;
    const current = String(existing || "");
    const fence = mermaidFence(body);
    if (current.replace(/\r\n/g, "\n").includes(fence)) return current;
    const draft = mermaidDiagramCitation(body, lang);
    if (!current.trim()) return draft;
    return current.replace(/\s+$/u, "") + "\n\n" + draft;
}

export function citeMermaidDiagramSourceFromEvent(event: Event): string {
    const detail = (event as CustomEvent<{ source?: string }>).detail;
    return String(detail?.source || "").trim();
}

export function dispatchCiteMermaidDiagram(source: string): void {
    const cleaned = source.trim();
    if (!cleaned || typeof window === "undefined") return;
    window.dispatchEvent(new CustomEvent(CITE_MERMAID_DIAGRAM_EVENT, { detail: { source: cleaned } }));
}

function saveDiagramPngViaDesktop(dataUrl: string): Promise<string> | null {
    const bridge = (globalThis as { go?: { main?: { App?: { SaveDiagramPNG?: (data: string) => Promise<string> } } } }).go?.main?.App?.SaveDiagramPNG;
    if (typeof bridge !== "function") return null;
    return bridge(dataUrl);
}

function downloadPngViaAnchor(href: string): boolean {
    const anchor = document.createElement("a");
    anchor.href = href;
    anchor.download = "diagram.png";
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    return true;
}

async function downloadMermaidSvg(svg: string, paper: string): Promise<boolean> {
    if (!svg || typeof document === "undefined") return false;
    let objectUrl = "";
    try {
        const canvas = await rasterizeMermaidSvg(svg, paper);
        if (!canvas) return false;
        let href = "";
        try {
            href = canvas.toDataURL("image/png");
        } catch {
            const blob = await canvasToPngBlob(canvas);
            if (!blob || blob.size <= 0) return false;
            objectUrl = URL.createObjectURL(blob);
            href = objectUrl;
        }
        if (!href) return false;
        const desktop = href.startsWith("data:image/png") ? saveDiagramPngViaDesktop(href) : null;
        if (desktop) {
            const saved = await desktop;
            return typeof saved === "string";
        }
        return downloadPngViaAnchor(href);
    } catch {
        return false;
    } finally {
        if (objectUrl) {
            const revoke = objectUrl;
            setTimeout(() => URL.revokeObjectURL(revoke), 10000);
        }
    }
}

function canvasToPngBlob(canvas: HTMLCanvasElement): Promise<Blob | null> {
    if (typeof canvas.toBlob !== "function") return Promise.resolve(null);
    return new Promise((resolve) => {
        const timer = setTimeout(() => resolve(null), 8000);
        canvas.toBlob((blob) => {
            clearTimeout(timer);
            resolve(blob);
        }, "image/png");
    });
}

/** Render a Mermaid fenced block while keeping an inspectable source fallback. */
export function AssistantMermaidDiagram({ code, theme, lang }: { code: string; theme: Theme; lang?: string }) {
    const [svg, setSvg] = React.useState("");
    const [error, setError] = React.useState("");
    const [zoom, setZoom] = React.useState(DEFAULT_MERMAID_ZOOM);
    const [menuOpen, setMenuOpen] = React.useState(false);
    const [menuUp, setMenuUp] = React.useState(false);
    const [sourceOpen, setSourceOpen] = React.useState(false);
    const [note, setNote] = React.useState("");
    const workspaceRef = React.useRef<HTMLDivElement>(null);
    const moreRef = React.useRef<HTMLDivElement>(null);
    const menuPanelRef = React.useRef<HTMLDivElement>(null);
    const savingRef = React.useRef(false);
    const noteTimerRef = React.useRef<ReturnType<typeof setTimeout> | null>(null);
    const mountedRef = React.useRef(true);
    const idRef = React.useRef(`assistant-mermaid-${Math.random().toString(36).slice(2, 10)}`);
    const zoomedSvg = React.useMemo(() => (svg ? scaleMermaidSvg(svg, zoom) : ""), [svg, zoom]);
    const zoomGroupLabel = localizeText(lang, "Diagram zoom", "图表缩放", "圖表縮放");
    const zoomOutLabel = localizeText(lang, "Zoom out", "缩小图表", "縮小圖表");
    const zoomInLabel = localizeText(lang, "Zoom in", "放大图表", "放大圖表");
    const zoomResetLabel = localizeText(lang, "Reset diagram size", "重置图表大小", "重設圖表大小");
    const renderingLabel = localizeText(lang, "Rendering diagram…", "正在渲染图表…", "正在渲染圖表…");
    const diagramLabel = localizeText(lang, "Mermaid diagram", "Mermaid 图表", "Mermaid 圖表");
    const citeLabel = localizeText(lang, "Add to chat", "添加到对话", "新增到對話");
    const downloadLabel = localizeText(lang, "Download", "下载到本地", "下載到本機");
    const moreLabel = localizeText(lang, "More diagram actions", "更多操作", "更多操作");
    const saveImageLabel = localizeText(lang, "Save as image", "保存为图片", "儲存為圖片");
    const copyCodeLabel = localizeText(lang, "Copy code", "复制代码", "複製程式碼");
    const viewCodeLabel = sourceOpen
        ? localizeText(lang, "Hide code", "隐藏代码", "隱藏程式碼")
        : localizeText(lang, "View code", "查看代码", "查看程式碼");
    const copiedLabel = localizeText(lang, "Copied", "已复制", "已複製");
    const copyFailedLabel = localizeText(lang, "Copy failed", "复制失败", "複製失敗");
    const saveFailedLabel = localizeText(lang, "Could not save the image", "无法保存图片", "無法儲存圖片");
    const source = sanitizeMermaidCode(code).trim();
    const menuDark = mermaidIsDark(theme);
    const menuStyle = {
        "--amd-menu-bg": menuDark ? "#1c1c1e" : "#ffffff",
        "--amd-menu-text": theme.text,
        "--amd-menu-border": menuDark ? "rgba(255,255,255,0.08)" : "rgba(15, 23, 42, 0.08)",
        "--amd-menu-hover": menuDark ? "rgba(255,255,255,0.08)" : "#f4f5f7",
        "--amd-menu-shadow": menuDark ? "0 8px 24px rgba(0, 0, 0, 0.45)" : "0 8px 28px rgba(15, 23, 42, 0.12)",
    } as React.CSSProperties;

    const flash = (text: string) => {
        if (!mountedRef.current) return;
        setNote(text);
        if (noteTimerRef.current) clearTimeout(noteTimerRef.current);
        noteTimerRef.current = setTimeout(() => {
            if (mountedRef.current) setNote("");
        }, 1600);
    };
    const saveDiagram = () => {
        if (savingRef.current || !svg) return;
        savingRef.current = true;
        void downloadMermaidSvg(svg, mermaidRasterPaper(theme.fieldBg)).then((ok) => {
            if (!mountedRef.current || ok) return;
            flash(saveFailedLabel);
        }).finally(() => {
            savingRef.current = false;
        });
    };
    const copyDiagram = () => {
        void copyTextToClipboard(source).then((ok) => {
            if (mountedRef.current) flash(ok ? copiedLabel : copyFailedLabel);
        });
    };

    React.useEffect(() => () => {
        mountedRef.current = false;
        if (noteTimerRef.current) clearTimeout(noteTimerRef.current);
    }, []);

    React.useEffect(() => {
        if (!menuOpen) return;
        const onPointer = (event: PointerEvent) => {
            if (!moreRef.current?.contains(event.target as Node)) {
                setMenuOpen(false);
                setMenuUp(false);
            }
        };
        const onKey = (event: KeyboardEvent) => {
            if (event.key !== "Escape") return;
            event.preventDefault();
            event.stopPropagation();
            setMenuOpen(false);
            setMenuUp(false);
        };
        document.addEventListener("pointerdown", onPointer);
        document.addEventListener("keydown", onKey, true);
        return () => {
            document.removeEventListener("pointerdown", onPointer);
            document.removeEventListener("keydown", onKey, true);
        };
    }, [menuOpen]);

    React.useLayoutEffect(() => {
        if (!menuOpen || menuUp) return;
        const panel = menuPanelRef.current;
        if (!panel) return;
        const menu = panel.getBoundingClientRect();
        const button = moreRef.current?.getBoundingClientRect();
        if (!button || !(menu.height > 0) || !(window.innerHeight > 0)) return;
        let clipTop = 0;
        let clipBottom = window.innerHeight;
        let node: HTMLElement | null = moreRef.current?.parentElement ?? null;
        while (node && node !== document.documentElement) {
            const overflowY = window.getComputedStyle(node).overflowY;
            if (overflowY === "auto" || overflowY === "scroll" || overflowY === "hidden" || overflowY === "clip") {
                const host = node.getBoundingClientRect();
                if (host.height > 0) {
                    clipTop = Math.max(clipTop, host.top);
                    clipBottom = Math.min(clipBottom, host.bottom);
                }
            }
            node = node.parentElement;
        }
        if (mermaidMenuOpensUpward(button.top - clipTop, clipBottom - button.bottom, menu.height)) setMenuUp(true);
    }, [menuOpen, menuUp]);

    React.useEffect(() => {
        let cancelled = false;
        setSvg("");
        setError("");
        setMenuOpen(false);
        setMenuUp(false);
        setSourceOpen(false);
        setNote("");
        // A new diagram replaces the old one; keep the previous reader's zoom
        // from silently carrying onto a different picture.
        setZoom(DEFAULT_MERMAID_ZOOM);
        const source = sanitizeMermaidCode(code).trim();
        if (!source) {
            setError(localizeText(lang, "The Mermaid code block is empty.", "Mermaid 代码块为空。", "Mermaid 程式碼區塊為空。"));
            return () => { cancelled = true; };
        }
        if (source.length > MAX_MERMAID_SOURCE_LENGTH) {
            setError(localizeText(
                lang,
                `The Mermaid source exceeds the ${MAX_MERMAID_SOURCE_LENGTH.toLocaleString()} character rendering limit.`,
                `Mermaid 源码超过 ${MAX_MERMAID_SOURCE_LENGTH.toLocaleString()} 字符渲染上限。`,
                `Mermaid 原始碼超過 ${MAX_MERMAID_SOURCE_LENGTH.toLocaleString()} 字元渲染上限。`,
            ));
            return () => { cancelled = true; };
        }
        if (exceedsMermaidRenderBudget(source)) {
            setError(mermaidBudgetMessage(lang));
            return () => { cancelled = true; };
        }
        void getMermaid().then(async (mermaid) => {
            const rendered = await renderDiagram(
                mermaid,
                idRef.current,
                source,
                workspaceRef.current,
                theme,
                () => cancelled,
            );
            if (!cancelled && rendered) {
                setSvg(rendered);
                // The render host is only ever a scratch pad Mermaid draws into;
                // the visible markup is the string we just captured. Leaving the
                // node behind would let it accumulate across re-renders (the
                // effect re-runs on every code/theme change), so drop it the
                // moment we have the result. The error path already does this.
                if (workspaceRef.current) workspaceRef.current.innerHTML = "";
            }
        }).catch((reason: unknown) => {
            if (!cancelled) {
                setError(errorMessage(reason, lang));
                if (workspaceRef.current) workspaceRef.current.innerHTML = "";
            }
        });
        return () => { cancelled = true; };
        // lang participates because the refusals above are localised strings built
        // inside this effect: a language switch must re-run it, or the reader keeps
        // reading a rejection in a language they already left.
    }, [code, theme, lang]);

    const diagramTheme = getMermaidTheme(theme).themeVariables;
    return (
        <div className="amd-figure">
            <div ref={workspaceRef} aria-hidden="true" className="amd-measure-host" />
            {svg ? (
                <div data-testid="assistant-mermaid-figure" className="amd-figure-body">
                    <div className="amd-toolbar" style={menuStyle}>
                    <div className="amd-actions">
                        <button type="button" className="amd-action" data-testid="assistant-mermaid-cite" onClick={() => dispatchCiteMermaidDiagram(source)}>
                            {citeLabel}
                        </button>
                        <button type="button" className="amd-action" data-testid="assistant-mermaid-download" onClick={saveDiagram}>
                            {downloadLabel}
                        </button>
                        <div className="amd-more" ref={moreRef}>
                            <button
                                type="button"
                                className="amd-action"
                                data-testid="assistant-mermaid-more"
                                aria-haspopup="menu"
                                aria-expanded={menuOpen}
                                aria-label={moreLabel}
                                onClick={() => {
                                    setMenuUp(false);
                                    setMenuOpen((open) => !open);
                                }}
                            >
                                {"···"}
                            </button>
                            {menuOpen ? (
                                <div role="menu" className={menuUp ? "amd-more-menu amd-more-menu-up" : "amd-more-menu"} data-testid="assistant-mermaid-menu" ref={menuPanelRef}>
                                    <button type="button" role="menuitem" className="amd-action" onClick={() => { setMenuOpen(false); setMenuUp(false); saveDiagram(); }}>
                                        {saveImageLabel}
                                    </button>
                                    <button type="button" role="menuitem" className="amd-action" data-testid="assistant-mermaid-copy" onClick={() => { setMenuOpen(false); setMenuUp(false); copyDiagram(); }}>
                                        {copyCodeLabel}
                                    </button>
                                    <button type="button" role="menuitem" className="amd-action" data-testid="assistant-mermaid-view-code" onClick={() => { setMenuOpen(false); setMenuUp(false); setSourceOpen((open) => !open); }}>
                                        {viewCodeLabel}
                                    </button>
                                </div>
                            ) : null}
                        </div>
                    </div>
                    <div role="group" aria-label={zoomGroupLabel} style={mermaidZoomBarStyle(theme)}>
                        <MermaidZoomButton
                            testId="assistant-mermaid-zoom-out"
                            label={zoomOutLabel}
                            disabled={zoom <= MIN_MERMAID_ZOOM}
                            onClick={() => setZoom((current) => stepMermaidZoom(current, -1))}
                            theme={theme}
                        >
                            {"−"}
                        </MermaidZoomButton>
                        <MermaidZoomButton
                            testId="assistant-mermaid-zoom-reset"
                            label={zoomResetLabel}
                            onClick={() => setZoom(DEFAULT_MERMAID_ZOOM)}
                            theme={theme}
                        >
                            {`${Math.round(zoom * 100)}%`}
                        </MermaidZoomButton>
                        <MermaidZoomButton
                            testId="assistant-mermaid-zoom-in"
                            label={zoomInLabel}
                            disabled={zoom >= MAX_MERMAID_ZOOM}
                            onClick={() => setZoom((current) => stepMermaidZoom(current, 1))}
                            theme={theme}
                        >
                            {"+"}
                        </MermaidZoomButton>
                    </div>
                    {note ? <span role="status" className="amd-note">{note}</span> : null}
                    </div>
                    <div
                        data-testid="assistant-mermaid-diagram"
                        role="img"
                        aria-label={diagramLabel}
                        className="amd-diagram"
                        style={Object.assign({ color: theme.text }, {
                            "--amd-ink": diagramTheme.primaryTextColor,
                            "--amd-line": diagramTheme.lineColor,
                            "--amd-paper": theme.fieldBg,
                            "--amd-chip": diagramTheme.secondaryColor,
                        })}
                        dangerouslySetInnerHTML={{ __html: zoomedSvg }}
                    />
                    {sourceOpen ? (
                        <pre data-testid="assistant-mermaid-source" className="amd-source" style={codeFenceScrollUnderLangStyle}>
                            <code style={codeFenceInnerStyle}>{source}</code>
                        </pre>
                    ) : null}
                </div>
            ) : error ? (
                <div data-testid="assistant-mermaid-fallback" style={chatCodeWellStyle(theme)}>
                    <div role="status" style={{ margin: 0, padding: "8px 12px 0", color: theme.text, fontSize: "12px", lineHeight: 1.4, whiteSpace: "normal", overflowWrap: "anywhere" }}>{getMermaidFallbackMessage(error, lang)}</div>
                    <pre style={codeFenceScrollUnderLangStyle}>
                        <code style={codeFenceInnerStyle}>{code}</code>
                    </pre>
                </div>
            ) : (
                <div data-testid="assistant-mermaid-loading" style={{ margin: "8px 0", padding: "10px 12px", color: theme.textMuted, fontSize: "0.9em" }}>{renderingLabel}</div>
            )}
        </div>
    );
}

/**
 * Render one Mermaid fence to a PNG data URL.
 *
 * A PDF built by the Go renderer cannot run JavaScript, so a diagram has to
 * arrive as pixels. Rendering it here — where it already renders for the
 * screen — keeps one answer to "what does this diagram look like" instead of
 * teaching the document layer a second one.
 *
 * Returns null when the diagram cannot be drawn; the caller then keeps the
 * source text, which is what the reader would have seen anyway.
 */
/**
 * Screen-equivalent source preparation for PDF export, or null when the export
 * should keep the fence's source text instead of a picture.
 *
 * The on-screen pipeline sanitises model casing and enforces the render budget
 * before Mermaid sees the source. Export must answer for the same fence the
 * reader saw on screen; skipping either step would silently diverge — a
 * lower-cased "sequencediagram" renders on screen yet fails detection in the
 * export path, degrading the PDF to a source-code block for a diagram the user
 * already saw drawn.
 */
export function prepareMermaidExportSource(code: string): string | null {
    // Sanitised, not just trimmed: the on-screen path repairs model casing
    // before Mermaid sees the source, and a diagram the reader watched render
    // must not come back as a source-code block in the PDF.
    const source = sanitizeMermaidCode(code).trim();
    if (!source || source.length > MAX_MERMAID_SOURCE_LENGTH) return null;
    if (exceedsMermaidRenderBudget(source)) return null;
    return source;
}

export async function renderMermaidFenceToPNGDataURL(code: string, theme: Theme): Promise<string | null> {
    const source = prepareMermaidExportSource(code);
    if (!source) return null;
    try {
        const mermaid = await getMermaid();
        exportDiagramSeq += 1;
        const svg = await renderDiagram(
            mermaid,
            `mermaid-export-${exportDiagramSeq}`,
            source,
            null,
            theme,
            () => false,
        );
        if (!svg) return null;
        return await mermaidSvgToPNGDataURL(svg, mermaidRasterPaper(theme.fieldBg));
    } catch {
        return null;
    }
}

let exportDiagramSeq = 0;

// A printed page is white, and the document layer stretches every picture to
// the width of the text column. Sizing only for the screen therefore leaves a
// small diagram soft once it spans the page: mermaid reports a viewBox in its
// own natural units, so a compact flowchart doubled is still well under what
// the page asks for. The canvas aims at print resolution instead — never below
// twice the diagram, never past the ceiling. Enlarging costs nothing in quality
// because the source is vector.
const MERMAID_EXPORT_SCALE = 2;
const MERMAID_EXPORT_TARGET_EDGE = 1800;
const MERMAID_EXPORT_MAX_EDGE = 2600;

/**
 * The pixel size to draw a diagram at for export.
 *
 * Exported so the rule can be checked without a canvas, which jsdom does not
 * provide.
 */
export function mermaidExportCanvasSize(width: number, height: number): { width: number; height: number } | null {
    if (!(width > 0) || !(height > 0)) return null;
    const longest = Math.max(width, height);
    const target = Math.min(
        MERMAID_EXPORT_MAX_EDGE,
        Math.max(longest * MERMAID_EXPORT_SCALE, MERMAID_EXPORT_TARGET_EDGE),
    );
    const scale = target / longest;
    return {
        width: Math.max(1, Math.round(width * scale)),
        height: Math.max(1, Math.round(height * scale)),
    };
}

async function mermaidSvgToPNGDataURL(svg: string, paper: string): Promise<string | null> {
    const canvas = await rasterizeMermaidSvg(svg, paper);
    if (!canvas) return null;
    try {
        return canvas.toDataURL("image/png");
    } catch {
        return null;
    }
}

async function rasterizeMermaidSvg(svg: string, paper: string): Promise<HTMLCanvasElement | null> {
    const sized = svgReadyForRaster(svg);
    const size = mermaidSvgSize(sized);
    if (!size) return null;
    const canvasSize = mermaidExportCanvasSize(size.width, size.height);
    if (!canvasSize) return null;
    const canvas = document.createElement("canvas");
    canvas.width = canvasSize.width;
    canvas.height = canvasSize.height;
    const context = canvas.getContext("2d");
    if (!context) return null;
    const image = await loadSvgAsImage(sized);
    // The SVG is transparent. Arrow gray is chosen against the chat surface,
    // so the bitmap uses that same surface. A white sheet hides those arrows
    // when the chat is dark.
    context.fillStyle = paper;
    context.fillRect(0, 0, canvas.width, canvas.height);
    context.drawImage(image, 0, 0, canvas.width, canvas.height);
    return canvas;
}

// The rendered SVG states its size in width/height attributes, or — when the
// width is a percentage, which is mermaid's `useMaxWidth` default — in viewBox.
// Both are read so a diagram is never scaled to zero.
export function mermaidSvgSize(svg: string): { width: number; height: number } | null {
    // Not anchored: a wrapper may emit an XML declaration, comment, or leading
    // whitespace before the root tag. Failing to find it would return null and
    // silently drop the diagram to a source-code block in the exported PDF — the
    // same silent-failure class scaleMermaidSvg already guards against on screen.
    const root = svg.match(/<svg\b[^>]*>/);
    if (!root) return null;
    const attr = (name: string): number => {
        const match = root[0].match(new RegExp(`${name}="([^"]+)"`));
        // A bare number or a px value is a pixel size. Mermaid writes
        // `width="100%"` and parseFloat("100%") is 100 — accepting that would read
        // a 100px diagram where none exists and short-circuit the viewBox fallback
        // below, which is what actually carries the true aspect ratio. A px suffix
        // is tolerated so screen and export sizing agree on px-suffixed output.
        const valueMatch = match?.[1].trim().match(/^([0-9.]+)(?:px)?$/i);
        if (!match || !valueMatch) return 0;
        const value = parseFloat(valueMatch[1]);
        return Number.isFinite(value) && value > 0 ? value : 0;
    };
    let width = attr("width");
    let height = attr("height");
    if (!width || !height) {
        const viewBox = root[0].match(/viewBox="([^"]+)"/i);
        const parts = viewBox ? viewBox[1].trim().split(/[\s,]+/).map(Number) : [];
        if (parts.length === 4 && parts[2] > 0 && parts[3] > 0) {
            width = width || parts[2];
            height = height || parts[3];
        }
    }
    if (!width || !height) return null;
    return { width, height };
}

/** HTML labels inside foreignObject disappear when an SVG is painted as an image.
 *  The box stays; the words are rewritten as SVG text in that same box. */
export function svgWithRasterLabels(svg: string): string {
    if (!/<foreignobject\b/i.test(svg)) return svg;
    const ink = rasterLabelInk(svg);
    const fontFamily = MERMAID_FONT.replace(/"/g, "'");
    return svg.replace(/<foreignobject\b([^>]*)>([\s\S]*?)<\/foreignobject>/gi, (_whole, attrs: string, inner: string) => {
        const num = (name: string) => {
            const match = attrs.match(new RegExp(`\\b${name}="([^"]+)"`));
            const value = match ? Number.parseFloat(match[1]) : Number.NaN;
            return Number.isFinite(value) ? value : 0;
        };
        const x = num("x");
        const y = num("y");
        const boxWidth = num("width");
        const height = num("height");
        const laid = layoutRasterLabel(foreignObjectLines(inner), boxWidth, height);
        const lines = laid.lines;
        if (!lines.length) return "";
        const fontSize = laid.fontSize;
        const cx = x + boxWidth / 2;
        const transform = attrs.match(/\btransform="([^"]*)"/)?.[1];
        const body = lines.length === 1
            ? `<text fill="${ink}" font-size="${fontSize}" font-family="${fontFamily}" text-anchor="middle" dominant-baseline="central" x="${cx}" y="${y + height / 2}">${encodeRasterText(lines[0])}</text>`
            : (() => {
                const lineHeight = fontSize * 1.25;
                const baseline = y + (height - lines.length * lineHeight) / 2 + fontSize * 0.8;
                const tspans = lines.map((line, index) => `<tspan x="${cx}" dy="${index === 0 ? 0 : lineHeight}">${encodeRasterText(line)}</tspan>`).join("");
                return `<text fill="${ink}" font-size="${fontSize}" font-family="${fontFamily}" text-anchor="middle" x="${cx}" y="${baseline}">${tspans}</text>`;
            })();
        return transform ? `<g transform="${transform}">${body}</g>` : body;
    });
}

function rasterLabelInk(svg: string): string {
    const block = svg.match(/\.nodeLabel[^{]*\{([^}]*)\}/i)?.[1] ?? "";
    return cssInk(block) ?? cssInk(svg) ?? "#111111";
}

function cssInk(text: string): string | undefined {
    return text.match(/(?:^|[;{\s])color:\s*(#[0-9a-fA-F]{6})/i)?.[1];
}

function wrapRasterLines(lines: string[], width: number, height: number): string[] {
    const fontSize = rasterFontSize(height, Math.max(lines.length, 1));
    const wrapped = lines.flatMap((line) => wrapRasterLine(line, width, fontSize));
    if (wrapped.length === lines.length) return wrapped;
    const fitted = rasterFontSize(height, wrapped.length);
    return lines.flatMap((line) => wrapRasterLine(line, width, fitted));
}

function layoutRasterLabel(source: string[], boxWidth: number, height: number): { fontSize: number; lines: string[] } {
    if (!source.length) return { fontSize: 14, lines: source };
    const wrapped = wrapRasterLines(source, boxWidth, height);
    const fontSize = fittedRasterFont(wrapped, boxWidth, height);
    const again = source.flatMap((line) => wrapRasterLine(line, boxWidth, fontSize));
    if (!again.length || again.join("\n") === wrapped.join("\n")) return { fontSize, lines: wrapped };
    const againFont = fittedRasterFont(again, boxWidth, height);
    if (again.length <= wrapped.length && againFont + 0.05 >= fontSize) return { fontSize: againFont, lines: again };
    return { fontSize, lines: wrapped };
}

function charAdvance(ch: string, fontSize: number): number {
    return ch.charCodeAt(0) < 128 ? fontSize * 0.56 : fontSize;
}

function textAdvance(text: string, fontSize: number): number {
    let used = 0;
    for (const ch of text) used += charAdvance(ch, fontSize);
    return used;
}

function wrapRasterLine(line: string, width: number, fontSize: number): string[] {
    if (!(width > 8) || !(fontSize > 0) || line.length < 2) return [line];
    const limit = width * 0.92;
    if (textAdvance(line, fontSize) <= limit) return [line];
    const parts: string[] = [];
    let current = "";
    let used = 0;
    const pushCurrent = () => {
        const trimmed = current.trim();
        if (trimmed) parts.push(trimmed);
        current = "";
        used = 0;
    };
    for (const token of line.split(/(\s+)/)) {
        if (!token) continue;
        if (/^\s+$/.test(token)) {
            if (!current) continue;
            const gap = textAdvance(token, fontSize);
            if (used + gap > limit) pushCurrent();
            else {
                current += token;
                used += gap;
            }
            continue;
        }
        const tokenWidth = textAdvance(token, fontSize);
        if (tokenWidth > limit) {
            if (current) pushCurrent();
            for (const ch of token) {
                const advance = charAdvance(ch, fontSize);
                if (current && used + advance > limit) pushCurrent();
                current += ch;
                used += advance;
            }
            continue;
        }
        if (current && used + tokenWidth > limit) pushCurrent();
        current += token;
        used += tokenWidth;
    }
    pushCurrent();
    return parts.length ? parts : [line];
}

function longestRasterAdvance(lines: string[], fontSize: number): number {
    let max = 0;
    for (const line of lines) {
        const used = textAdvance(line, fontSize);
        if (used > max) max = used;
    }
    return max;
}

function rasterFontSize(height: number, count: number): number {
    if (!(height > 0) || !(count > 0)) return 14;
    return Math.max(10, Math.min(16, Math.round(height / count * 0.72)));
}

/** The wrap font is a starting size. A floor there lets a long title spill
 *  out of the measured box, so the last step only shrinks. */
function fittedRasterFont(lines: string[], boxWidth: number, height: number): number {
    let fontSize = rasterFontSize(height, lines.length);
    if (boxWidth > 8) {
        const limit = boxWidth * 0.92;
        const used = longestRasterAdvance(lines, fontSize);
        if (used > limit && used > 0) fontSize = fontSize * limit / used;
    }
    if (height > 0 && lines.length > 0) {
        const stack = height / (lines.length * 1.25);
        if (fontSize > stack) fontSize = stack;
    }
    if (!(fontSize > 0)) return 1;
    return Math.max(1, Math.floor(fontSize * 10) / 10);
}

function isKatexRoot(openTag: string): boolean {
    const cls = openTag.match(/\bclass\s*=\s*['"]([^'"]*)['"]/i)?.[1] ?? "";
    return cls.split(/\s+/).includes("katex");
}

function matchingSpanEnd(html: string, from: number): number {
    let depth = 1;
    const tags = /<\/?span\b[^>]*>/gi;
    tags.lastIndex = from;
    let match: RegExpExecArray | null;
    while ((match = tags.exec(html))) {
        depth += match[0].startsWith("</") ? -1 : 1;
        if (depth === 0) return match.index + match[0].length;
    }
    return -1;
}

/** A typeset label carries the original formula on `data-plain`. The bitmap
 *  path drops the KaTeX tree and prints that text once. */
function katexLabelsAsPlain(html: string): string {
    let out = "";
    let index = 0;
    const lower = html.toLowerCase();
    while (index < html.length) {
        const start = lower.indexOf("<span", index);
        if (start < 0) {
            out += html.slice(index);
            break;
        }
        out += html.slice(index, start);
        const openEnd = html.indexOf(">", start);
        if (openEnd < 0) {
            out += html.slice(start);
            break;
        }
        const open = html.slice(start, openEnd + 1);
        if (!isKatexRoot(open)) {
            out += open;
            index = openEnd + 1;
            continue;
        }
        const close = matchingSpanEnd(html, openEnd + 1);
        if (close < 0) {
            out += html.slice(start);
            break;
        }
        const block = html.slice(start, close);
        const plain = open.match(/\bdata-plain\s*=\s*(['"])([\s\S]*?)\1/i);
        if (plain) {
            out += plain[2];
        } else {
            const annotation = block.match(/<annotation\b[^>]*>([\s\S]*?)<\/annotation>/i);
            out += annotation ? annotation[1] : block.replace(/<[^>]+>/g, "");
        }
        index = close;
    }
    return out;
}

function foreignObjectLines(inner: string): string[] {
    const broken = katexLabelsAsPlain(inner).replace(/<br\s*\/?>/gi, "\n").replace(/<\/(?:p|div|li|h[1-6])>/gi, "\n");
    const plain = decodeRasterText(broken.replace(/<[^>]+>/g, "").replace(/&nbsp;|&#160;/gi, " "));
    return plain.split("\n").map((line) => line.replace(/\s+/g, " ").trim()).filter(Boolean);
}

function decodeRasterText(value: string): string {
    const named = value.replace(/&([a-zA-Z][a-zA-Z0-9]+);/g, (whole, name: string) => {
        if (name === "amp" || name === "lt" || name === "gt" || name === "quot" || name === "apos") return whole;
        return decodeHtmlNamed(whole);
    });
    return named
        .replace(/&lt;/g, "<")
        .replace(/&gt;/g, ">")
        .replace(/&quot;/g, "\"")
        .replace(/&apos;/g, "'")
        .replace(/&#39;/g, "'")
        .replace(/&amp;/g, "&");
}

function decodeHtmlNamed(entity: string): string {
    if (typeof document === "undefined") return " ";
    const el = document.createElement("textarea");
    el.innerHTML = entity;
    const decoded = el.value;
    return decoded && decoded !== entity ? decoded : " ";
}

function encodeRasterText(value: string): string {
    return value.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

/** Mermaid returns the diagram as HTML (`innerHTML`), then DOMPurify serializes it
 *  again. The bitmap path parses that string as XML, so a lowercase foreignObject,
 *  a raw `<br>`, or an HTML-only entity fails the whole image. */
export function svgReadyForRaster(svg: string): string {
    const labeled = svgWithRasterLabels(svg);
    const stripped = labeled
        .replace(/<foreignobject\b[^>]*\/>/gi, "")
        .replace(/<foreignobject\b[^>]*>[\s\S]*?<\/foreignobject>/gi, "")
        .replace(/<(?:img|hr|meta|link|input|wbr)\b[^>]*\/?>/gi, "")
        .replace(/<br\s*\/?>/gi, "");
    return svgForRaster(ensureSvgNamespaces(xmlSafeMarkup(stripped)));
}

function xmlSafeMarkup(svg: string): string {
    const named = svg.replace(/&([a-zA-Z][a-zA-Z0-9]+);/g, (whole, name: string) => {
        if (name === "amp" || name === "lt" || name === "gt" || name === "quot" || name === "apos") return whole;
        return encodeRasterText(decodeHtmlNamed(whole));
    });
    return named.replace(/&(?!(?:amp|lt|gt|quot|apos|#\d+|#x[0-9a-fA-F]+);)/g, "&amp;");
}

function ensureSvgNamespaces(svg: string): string {
    const open = svg.match(/<svg\b[^>]*>/);
    if (!open || open.index === undefined) return svg;
    // HTML serialization lowercases viewBox. XML then ignores it, and a
    // width="100%" image has no viewport, so the bitmap comes out blank.
    let root = open[0].replace(/\bviewbox=/gi, "viewBox=");
    const extras: string[] = [];
    if (!/\sxmlns=/.test(root)) extras.push('xmlns="http://www.w3.org/2000/svg"');
    if (/\sxlink:/.test(svg) && !/xmlns:xlink=/.test(root)) extras.push('xmlns:xlink="http://www.w3.org/1999/xlink"');
    if (!extras.length && root === open[0]) return svg;
    if (extras.length) root = root.replace(/<svg\b/, `<svg ${extras.join(" ")}`);
    return svg.slice(0, open.index) + root + svg.slice(open.index + open[0].length);
}

/** Give a percentage-sized diagram a real pixel size before it is drawn to a bitmap.
 *  An SVG image with width="100%" otherwise decodes at the browser's default box. */
export function svgForRaster(svg: string): string {
    const size = mermaidSvgSize(svg);
    const open = svg.match(/<svg\b[^>]*>/);
    if (!size || !open || open.index === undefined) return svg;
    let root = open[0];
    const width = String(size.width);
    const height = String(size.height);
    root = /\swidth="/.test(root)
        ? root.replace(/\swidth="[^"]*"/, ` width="${width}"`)
        : root.replace(/<svg\b/, `<svg width="${width}"`);
    root = /\sheight="/.test(root)
        ? root.replace(/\sheight="[^"]*"/, ` height="${height}"`)
        : root.replace(/<svg\b/, `<svg height="${height}"`);
    const styleText = root.match(/style="([^"]*)"/)?.[1] ?? "";
    const kept = styleText.split(";").map((part) => part.trim()).filter((part) => part && !/^(?:max-)?(?:width|height)\s*:/i.test(part));
    if (/style="/.test(root)) {
        root = kept.length ? root.replace(/style="[^"]*"/, `style="${kept.join("; ")}"`) : root.replace(/\sstyle="[^"]*"/, "");
    }
    return svg.slice(0, open.index) + root + svg.slice(open.index + open[0].length);
}

function loadSvgAsImage(svg: string): Promise<HTMLImageElement> {
    return new Promise((resolve, reject) => {
        // A charset parameter makes Chromium treat the blob as a non-image,
        // so the decode fails and the save reports that it could not write.
        const blob = new Blob([svg], { type: "image/svg+xml" });
        const url = URL.createObjectURL(blob);
        const image = new Image();
        image.onload = () => {
            URL.revokeObjectURL(url);
            resolve(image);
        };
        image.onerror = () => {
            URL.revokeObjectURL(url);
            reject(new Error("svg could not be decoded"));
        };
        image.src = url;
    });
}
