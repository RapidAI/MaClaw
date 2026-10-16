import {
    assistantDarkSchemes,
    getAssistantDarkScheme,
    ASSISTANT_DARK_SCHEME_STORAGE_KEY,
} from "./assistantDarkSchemes";
import {
    assistantLightSchemes,
    getAssistantLightScheme,
    ASSISTANT_LIGHT_SCHEME_STORAGE_KEY,
    DEFAULT_ASSISTANT_LIGHT_SCHEME_ID,
} from "./assistantLightSchemes";

/**
 * Boot-time theme palette injected into index.html before any CSS/JS loads,
 * so the first WebView frame paints the correct scheme background instead of
 * native black. This module is the single render-time source of the palette:
 * the values are derived from the assistant scheme modules, and both the
 * inline boot script (via the vite bootThemePlugin) and the guard tests
 * consume it. Do not hardcode page backgrounds elsewhere.
 *
 * The module is imported from vite.config.ts (a Node context), so it must
 * stay free of React/DOM imports; the scheme modules it pulls in only carry
 * type-only imports and plain data.
 */

/** Keep in sync with AI_THEME_MODE_STORAGE_KEY in aiAssistantPanelTheme.tsx
 * (a vitest guard asserts equality). Duplicated here because that module is
 * a React component file and must not be loaded by the vite config bundle. */
export const BOOT_THEME_MODE_STORAGE_KEY = "ai_assistant_theme_mode";

export type BootThemePalette = {
    /** scheme storage value -> page background, always including the default. */
    dark: Record<string, string>;
    light: Record<string, string>;
    defaultDark: string;
    defaultLight: string;
};

export function buildBootThemePalette(): BootThemePalette {
    const dark: Record<string, string> = {};
    for (const scheme of assistantDarkSchemes) dark[scheme.storageValue] = scheme.cssVars.pageBg;
    const light: Record<string, string> = {};
    for (const scheme of assistantLightSchemes) light[scheme.storageValue] = scheme.cssVars.pageBg;
    return {
        dark,
        light,
        defaultDark: getAssistantDarkScheme(undefined).cssVars.pageBg,
        defaultLight: getAssistantLightScheme(DEFAULT_ASSISTANT_LIGHT_SCHEME_ID).cssVars.pageBg,
    };
}

/**
 * Render the self-contained inline boot script. Mirrors the original
 * hand-written snippet in index.html: reads the stored mode + scheme, picks
 * the page background (falling back to the scheme defaults), and paints
 * <html> before first paint. Unknown/legacy stored values fall through to
 * the default, exactly like the original ternary chains.
 */
export function renderBootThemeScript(palette: BootThemePalette): string {
    const mapLiteral = (map: Record<string, string>, fallback: string): string => {
        const entries = Object.entries(map)
            .map(([k, v]) => `${JSON.stringify(k)}:${JSON.stringify(v)}`)
            .join(",");
        return `{${entries},"":${JSON.stringify(fallback)},__default:${JSON.stringify(fallback)}}`;
    };
    return [
        "try {",
        `  var d = localStorage.getItem(${JSON.stringify(BOOT_THEME_MODE_STORAGE_KEY)}) === 'dark';`,
        `  var D = ${mapLiteral(palette.dark, palette.defaultDark)};`,
        `  var L = ${mapLiteral(palette.light, palette.defaultLight)};`,
        "  var c;",
        "  if (d) {",
        `    c = D[localStorage.getItem(${JSON.stringify(ASSISTANT_DARK_SCHEME_STORAGE_KEY)})] || D.__default;`,
        "  } else {",
        `    c = L[localStorage.getItem(${JSON.stringify(ASSISTANT_LIGHT_SCHEME_STORAGE_KEY)})] || L.__default;`,
        "  }",
        "  document.documentElement.style.backgroundColor = c;",
        "  document.documentElement.style.colorScheme = d ? 'dark' : 'light';",
        "  document.documentElement.style.setProperty('--theme-page-bg', c);",
        "  document.documentElement.setAttribute('data-boot-theme', d ? 'dark' : 'light');",
        "} catch(e) {}",
    ].join("\n      ");
}
