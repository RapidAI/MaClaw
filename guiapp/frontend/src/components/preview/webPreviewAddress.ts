import { fileExtFromName } from './filePreviewKind';

/**
 * Address-bar classification for the assistant web preview.
 * Remote pages stay http(s). Local files stay inside the preview directory.
 */

export type WebPreviewAddress =
    | { kind: 'remote'; url: string }
    | { kind: 'file'; path: string }
    | { kind: 'relative'; path: string }
    | { kind: 'invalid' };

const FILE_EXT = /\.(html?|xhtml|css|js|mjs|json|png|jpe?g|gif|svg|webp|wasm|txt|map|woff2?|ttf)$/i;

/** Sandbox for a framed page. srcdoc must not keep the assistant origin. */
export function webPreviewSandbox(mode: 'srcdoc' | 'framed'): string {
    const parts = ['allow-downloads', 'allow-forms', 'allow-modals', 'allow-popups', 'allow-scripts'];
    if (mode === 'framed') parts.push('allow-same-origin');
    return parts.sort().join(' ');
}

export function classifyWebPreviewAddress(raw: string): WebPreviewAddress {
    const text = String(raw || '').trim();
    if (!text) return { kind: 'invalid' };
    if (/^(javascript|data|file|vbscript|blob):/i.test(text)) return { kind: 'invalid' };
    if (/^https?:\/\//i.test(text)) {
        try {
            const parsed = new URL(text);
            if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return { kind: 'invalid' };
            if (parsed.username || parsed.password) return { kind: 'invalid' };
            return { kind: 'remote', url: parsed.toString() };
        } catch {
            return { kind: 'invalid' };
        }
    }
    if (/^[a-zA-Z]:[\\/]/.test(text) || text.startsWith('\\\\') || text.startsWith('/')) {
        if (text.includes('\0')) return { kind: 'invalid' };
        return { kind: 'file', path: text };
    }
    const normalized = text.replace(/\\/g, '/');
    const slash = normalized.indexOf('/');
    const head = slash >= 0 ? normalized.slice(0, slash) : normalized;
    const hostOnly = head.replace(/:\d+$/, '');
    const hostOK = /^localhost$/i.test(hostOnly) || /^[a-z0-9-]+(\.[a-z0-9-]+)+$/i.test(hostOnly);
    if (head && !/\s/.test(head) && !FILE_EXT.test(head) && hostOK) {
        return classifyWebPreviewAddress(`https://${normalized}`);
    }
    const rel = text.replace(/^\.\//, '').replace(/^[\\/]+/, '');
    if (!rel || rel.split(/[\\/]/).includes('..')) return { kind: 'invalid' };
    return { kind: 'relative', path: rel.replace(/\\/g, '/') };
}

export function localPreviewURL(origin: string, token: string, relPath: string): string {
    const base = String(origin || '').replace(/\/+$/, '');
    const parts = String(relPath || '')
        .split(/[\\/]/)
        .filter((part) => part && part !== '.' && part !== '..')
        .map((part) => encodeURIComponent(part));
    const suffix = parts.length > 0 ? parts.join('/') : 'index.html';
    return `${base}/web-preview/v1/${token}/${suffix}`;
}

export function splitPreviewLocation(relWithQuery: string): { path: string; suffix: string } {
    const value = String(relWithQuery || '');
    const mark = value.search(/[?#]/);
    if (mark < 0) return { path: value, suffix: '' };
    return { path: value.slice(0, mark), suffix: value.slice(mark) };
}

/** A single http(s) address. Bare hosts and page text stay empty. */
export function standaloneHTTPURL(raw: string | undefined | null): string {
    const text = String(raw || '').trim();
    if (!text || /\s/.test(text) || !/^https?:\/\//i.test(text)) return '';
    const classified = classifyWebPreviewAddress(text);
    return classified.kind === 'remote' ? classified.url : '';
}

export type WebPreviewFileTarget =
    | { kind: 'remote'; url: string }
    | { kind: 'file'; format: 'html' | 'svg'; path: string };

function webPreviewFormat(name: string, language?: string): 'html' | 'svg' | null {
    const ext = fileExtFromName(name);
    if (ext === '.html' || ext === '.htm' || ext === '.xhtml') return 'html';
    if (ext === '.svg') return 'svg';
    const lang = String(language || '').trim().toLowerCase();
    if (lang === 'html' || lang === 'xhtml') return 'html';
    if (lang === 'svg') return 'svg';
    return null;
}

function isAbsolutePreviewPath(path: string): boolean {
    return /^[a-zA-Z]:[\\/]/.test(path) || path.startsWith('\\\\') || path.startsWith('/');
}

/**
 * HTML pages, SVG images, and http(s) URLs open in the web preview.
 * Anything else stays a source or image view.
 */
export function webPreviewTargetFromFile(file: {
    fileName?: string;
    filePath?: string;
    absPath?: string;
    content?: string;
    language?: string;
} | null | undefined): WebPreviewFileTarget | null {
    if (!file) return null;
    for (const candidate of [file.absPath, file.filePath, file.fileName]) {
        const remote = standaloneHTTPURL(candidate);
        if (remote) return { kind: 'remote', url: remote };
    }
    const named = [file.absPath, file.filePath, file.fileName].map((value) => String(value || '').trim()).find(Boolean) || '';
    const format = webPreviewFormat(named, file.language);
    if (format) {
        const path = [file.absPath, file.filePath]
            .map((value) => String(value || '').trim())
            .find((value) => value && !standaloneHTTPURL(value) && isAbsolutePreviewPath(value)) || '';
        return { kind: 'file', format, path };
    }
    const contentURL = standaloneHTTPURL(file.content);
    if (contentURL) return { kind: 'remote', url: contentURL };
    return null;
}

/**
 * A new agent write of a page the user should see rendered.
 * Reads stay quiet so looking at source is not yanked back to the browser.
 * The key changes when that write is delivered again.
 */
export function agentWebPreviewRequestKey(file: {
    fileName?: string;
    filePath?: string;
    absPath?: string;
    content?: string;
    language?: string;
    opType?: string;
    forceOpen?: boolean;
    updatedAt?: number;
} | null | undefined): string {
    if (!file || !webPreviewTargetFromFile(file)) return '';
    const requested = file.opType === 'create' || file.opType === 'modify' || file.forceOpen === true;
    if (!requested) return '';
    const id = file.filePath || file.absPath || file.fileName || '';
    return `${id}:${file.updatedAt || 0}:${String(file.content || '').length}`;
}

/** A path reported by the framed page. Invalid escapes stay as sent. */
export function decodePreviewPath(value: string): string {
    const text = String(value || '');
    try {
        return decodeURIComponent(text);
    } catch {
        return text;
    }
}

export function joinPreviewDisplay(root: string, rel: string): string {
    const base = String(root || '').replace(/[\\/]+$/, '');
    const sep = base.includes('\\') ? '\\' : '/';
    const clean = String(rel || '').replace(/^[\\/]+/, '').replace(/[\\/]/g, sep);
    if (!base) return clean;
    if (!clean) return base;
    return base + sep + clean;
}
