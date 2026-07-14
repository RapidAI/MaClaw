/**
 * Pure helpers for CodePreviewPanel find / go-to-line / view prefs.
 * Kept free of React so unit tests can import without mounting the panel.
 */

// ── Find ──

export interface FindMatchOptions {
    caseSensitive?: boolean;
    wholeWord?: boolean;
    useRegex?: boolean;
}

export function escapeRegExp(text: string): string {
    return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/**
 * Compile a line matcher for in-file find.
 * Returns an error message when `useRegex` is set and the pattern is invalid.
 */
export function compileFindMatcher(
    query: string,
    options: FindMatchOptions = {},
): { ok: true; test: (line: string) => boolean } | { ok: false; error: string } {
    const caseSensitive = options.caseSensitive === true;
    const wholeWord = options.wholeWord === true;
    const useRegex = options.useRegex === true;
    const q = useRegex ? query : query.trim();
    if (!q) {
        return { ok: true, test: () => false };
    }

    // Fast path: plain substring (case-insensitive or not).
    if (!useRegex && !wholeWord) {
        if (caseSensitive) {
            return { ok: true, test: (line) => line.includes(q) };
        }
        const needle = q.toLowerCase();
        return { ok: true, test: (line) => line.toLowerCase().includes(needle) };
    }

    try {
        // Whole-word wraps the pattern; when useRegex is also on, treat query as
        // a regex and still require word boundaries (VS Code-like combination).
        const body = useRegex ? q : escapeRegExp(q);
        const source = wholeWord ? `\\b(?:${body})\\b` : body;
        const flags = caseSensitive ? '' : 'i';
        const re = new RegExp(source, flags);
        return {
            ok: true,
            // Avoid lastIndex surprises if a future flag adds /g.
            test: (line: string) => {
                re.lastIndex = 0;
                return re.test(line);
            },
        };
    } catch (err) {
        const message = err instanceof Error ? err.message : 'Invalid regular expression';
        return { ok: false, error: message };
    }
}

/**
 * Return 0-based line indexes matching `query` under the given find options.
 * Empty query or invalid regex yields no matches.
 */
export function findMatchLineIndexes(
    content: string,
    query: string,
    options: FindMatchOptions = {},
): number[] {
    const compiled = compileFindMatcher(query, options);
    if (!compiled.ok) return [];
    const lines = content.split('\n');
    const matches: number[] = [];
    for (let i = 0; i < lines.length; i++) {
        if (compiled.test(lines[i])) matches.push(i);
    }
    return matches;
}

/** Cycle match index with wrap-around. Returns -1 when no matches. */
export function cycleMatchIndex(matchCount: number, current: number, delta: number): number {
    if (matchCount <= 0) return -1;
    if (current < 0) return delta >= 0 ? 0 : matchCount - 1;
    const next = current + (delta >= 0 ? 1 : -1);
    if (next < 0) return matchCount - 1;
    if (next >= matchCount) return 0;
    return next;
}

/**
 * Parse a VS Code-style go-to-line input ("12", "12:5").
 * Returns a 1-based line number clamped to [1, maxLines], or null if invalid.
 */
export function parseGoToLineInput(raw: string, maxLines: number): number | null {
    const s = raw.trim();
    if (!s || maxLines <= 0) return null;
    const m = s.match(/^(\d+)/);
    if (!m) return null;
    const n = Number.parseInt(m[1], 10);
    if (!Number.isFinite(n) || n < 1) return null;
    return Math.min(n, maxLines);
}

// ── Font size / view prefs ──

/** Default / bounds for code preview font size (px). */
export const CODE_PREVIEW_FONT_DEFAULT = 13;
export const CODE_PREVIEW_FONT_MIN = 10;
export const CODE_PREVIEW_FONT_MAX = 24;

/** Clamp font size to the supported preview range. */
export function clampCodePreviewFontSize(size: number): number {
    if (!Number.isFinite(size)) return CODE_PREVIEW_FONT_DEFAULT;
    return Math.max(CODE_PREVIEW_FONT_MIN, Math.min(CODE_PREVIEW_FONT_MAX, Math.round(size)));
}

/** Line-height for a given font size (keeps monospace rows readable). */
export function codePreviewLineHeight(fontSize: number): number {
    return Math.round(clampCodePreviewFontSize(fontSize) * 1.55);
}

/** localStorage key for wrap / font zoom preferences. */
export const CODE_PREVIEW_VIEW_PREFS_KEY = 'maclaw.codePreview.viewPrefs';

export interface CodePreviewViewPrefs {
    wordWrap: boolean;
    fontSize: number;
}

export function defaultCodePreviewViewPrefs(): CodePreviewViewPrefs {
    return { wordWrap: false, fontSize: CODE_PREVIEW_FONT_DEFAULT };
}

/** Load wrap/font prefs from localStorage (safe for SSR / restricted storage). */
export function loadCodePreviewViewPrefs(): CodePreviewViewPrefs {
    const fallback = defaultCodePreviewViewPrefs();
    try {
        if (typeof localStorage === 'undefined') return fallback;
        const raw = localStorage.getItem(CODE_PREVIEW_VIEW_PREFS_KEY);
        if (!raw) return fallback;
        const parsed = JSON.parse(raw) as Partial<CodePreviewViewPrefs> | null;
        if (!parsed || typeof parsed !== 'object') return fallback;
        return {
            wordWrap: parsed.wordWrap === true,
            fontSize: clampCodePreviewFontSize(
                typeof parsed.fontSize === 'number' ? parsed.fontSize : CODE_PREVIEW_FONT_DEFAULT,
            ),
        };
    } catch {
        return fallback;
    }
}

/** Persist wrap/font prefs. Failures are ignored (private mode, quota, etc.). */
export function saveCodePreviewViewPrefs(prefs: CodePreviewViewPrefs): void {
    try {
        if (typeof localStorage === 'undefined') return;
        localStorage.setItem(
            CODE_PREVIEW_VIEW_PREFS_KEY,
            JSON.stringify({
                wordWrap: !!prefs.wordWrap,
                fontSize: clampCodePreviewFontSize(prefs.fontSize),
            }),
        );
    } catch {
        // ignore storage failures
    }
}

/** Known language ids → path-bar labels (module-level to avoid per-call allocation). */
const CODE_LANGUAGE_LABELS: Record<string, string> = {
    typescript: 'TypeScript',
    ts: 'TypeScript',
    tsx: 'TSX',
    javascript: 'JavaScript',
    js: 'JavaScript',
    jsx: 'JSX',
    python: 'Python',
    py: 'Python',
    go: 'Go',
    golang: 'Go',
    rust: 'Rust',
    rs: 'Rust',
    java: 'Java',
    c: 'C',
    cpp: 'C++',
    'c++': 'C++',
    csharp: 'C#',
    cs: 'C#',
    json: 'JSON',
    yaml: 'YAML',
    yml: 'YAML',
    markdown: 'Markdown',
    md: 'Markdown',
    html: 'HTML',
    css: 'CSS',
    scss: 'SCSS',
    shell: 'Shell',
    bash: 'Shell',
    sh: 'Shell',
    powershell: 'PowerShell',
    ps1: 'PowerShell',
    sql: 'SQL',
    plaintext: 'Plain Text',
    plain: 'Plain Text',
    text: 'Plain Text',
};

/**
 * Human-readable language label for the path bar (e.g. typescript → TypeScript).
 */
export function formatCodeLanguageLabel(language: string | undefined | null): string {
    const raw = (language || '').trim();
    if (!raw) return 'Plain Text';
    const key = raw.toLowerCase();
    if (CODE_LANGUAGE_LABELS[key]) return CODE_LANGUAGE_LABELS[key];
    // Title-case unknown ids: "objective-c" → "Objective-C"
    return raw
        .split(/([-_\s]+)/)
        .map((part, i) => (i % 2 === 1 ? part : part ? part.charAt(0).toUpperCase() + part.slice(1).toLowerCase() : part))
        .join('');
}
