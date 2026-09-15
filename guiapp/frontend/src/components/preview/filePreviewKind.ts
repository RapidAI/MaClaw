/**
 * Shared file-preview classification. Used by the assistant pane, the mobile
 * document library, and any other surface that needs the same renderer.
 */

export type FilePreviewKind =
    | 'pptx'
    | 'pdf'
    | 'image'
    | 'video'
    | 'audio'
    | 'html'
    | 'markdown'
    | 'office'
    | 'code'
    | 'text';

const IMAGE_EXT = new Set([
    '.png', '.jpg', '.jpeg', '.gif', '.webp', '.bmp', '.svg', '.ico', '.tif', '.tiff', '.heic',
]);
const VIDEO_EXT = new Set(['.mp4', '.webm', '.mov', '.avi', '.mkv', '.m4v']);
const AUDIO_EXT = new Set(['.mp3', '.wav', '.ogg', '.m4a', '.aac', '.flac', '.oga']);
/** Legacy Office containers that are not the PPTX slide renderer. */
const OFFICE_EXT = new Set([
    '.doc', '.docx', '.xls', '.xlsx', '.xlsm', '.ppt', '.odt', '.ods', '.odp', '.rtf',
]);

/** First path/name that has an extension; used so a title without a suffix cannot hide a .pptx original. */
export function previewSourceName(...names: Array<string | undefined | null>): string {
    const list = names.map((n) => String(n || '').trim()).filter(Boolean);
    for (const name of list) {
        if (fileExtFromName(name)) return name;
    }
    return list[0] || '';
}

export function fileExtFromName(name: string): string {
    const base = (String(name || '').split(/[/\\]/).pop() || '').trim().toLowerCase();
    const query = base.search(/[?#]/);
    const cleaned = query >= 0 ? base.slice(0, query) : base;
    const dot = cleaned.lastIndexOf('.');
    if (dot <= 0) return '';
    return cleaned.slice(dot);
}

export function filePreviewKindFromName(name: string, language?: string): FilePreviewKind {
    const ext = fileExtFromName(name);
    const lang = String(language || '').trim().toLowerCase();
    // Filename wins for visual / Office types so a .docx extract labelled
    // "markdown" still uses the document preview, not a generic md view.
    if (ext === '.pptx') return 'pptx';
    if (ext === '.pdf') return 'pdf';
    if (IMAGE_EXT.has(ext)) return 'image';
    if (VIDEO_EXT.has(ext)) return 'video';
    if (AUDIO_EXT.has(ext)) return 'audio';
    if (ext === '.html' || ext === '.htm') return 'html';
    if (OFFICE_EXT.has(ext)) return 'office';
    if (lang === 'pptx') return 'pptx';
    if (lang === 'pdf') return 'pdf';
    if (lang === 'image') return 'image';
    if (lang === 'video') return 'video';
    if (lang === 'audio') return 'audio';
    if (lang === 'html') return 'html';
    if (lang === 'office') return 'office';
    if (lang === 'markdown' || lang === 'md') return 'markdown';
    if (ext === '.md' || ext === '.markdown') return 'markdown';
    if (lang === 'plaintext' || lang === 'text' || ext === '.txt' || ext === '.log') return 'text';
    return 'code';
}

/** Previewers that own scrolling / paging and should hide the code minimap. */
export function isChromeLessPreviewKind(kind: FilePreviewKind): boolean {
    return kind === 'pptx' || kind === 'pdf' || kind === 'image' || kind === 'video' || kind === 'audio' || kind === 'html';
}

/** Native binary/page viewers that need a local original path. */
export function previewNeedsLocalFile(kind: FilePreviewKind): boolean {
    return kind === 'pptx' || kind === 'pdf' || kind === 'image' || kind === 'video' || kind === 'audio';
}

/** Whether the mobile library should download the Hub original before previewing. */
export function previewShouldMaterialize(kind: FilePreviewKind, hasExtractedBody: boolean): boolean {
    if (previewNeedsLocalFile(kind) || kind === 'html' || kind === 'code') return true;
    if ((kind === 'text' || kind === 'office') && !hasExtractedBody) return true;
    return false;
}

const LANGUAGE_BY_EXT: Record<string, string> = {
    '.go': 'go',
    '.ts': 'typescript',
    '.tsx': 'typescript',
    '.js': 'javascript',
    '.jsx': 'javascript',
    '.py': 'python',
    '.rs': 'rust',
    '.java': 'java',
    '.c': 'c',
    '.h': 'c',
    '.cpp': 'cpp',
    '.cc': 'cpp',
    '.hpp': 'cpp',
    '.css': 'css',
    '.json': 'json',
    '.yaml': 'yaml',
    '.yml': 'yaml',
    '.sh': 'shell',
    '.bash': 'shell',
    '.bat': 'batch',
    '.cmd': 'batch',
    '.ps1': 'powershell',
    '.rb': 'ruby',
    '.php': 'php',
    '.swift': 'swift',
    '.kt': 'kotlin',
    '.kts': 'kotlin',
    '.cs': 'csharp',
    '.sql': 'sql',
    '.lua': 'lua',
    '.toml': 'toml',
    '.xml': 'xml',
    '.csv': 'plaintext',
    '.tsv': 'plaintext',
};

/** Syntax / renderer language for a CodeFile. Office extracts render as markdown. */
export function languageFromFileName(name: string, fallbackLanguage?: string): string {
    const kind = filePreviewKindFromName(name, fallbackLanguage);
    if (kind === 'office' || kind === 'markdown') return 'markdown';
    if (kind === 'pptx' || kind === 'pdf' || kind === 'image' || kind === 'video' || kind === 'audio' || kind === 'html') {
        return kind;
    }
    const ext = fileExtFromName(name);
    if (LANGUAGE_BY_EXT[ext]) return LANGUAGE_BY_EXT[ext];
    const lang = String(fallbackLanguage || '').trim();
    return lang || 'plaintext';
}

const MOBILE_DOC_IMAGE_RE =
    /!\[([^\]]*)\]\((\/api\/mobile\/documents\/drafts\/([^/]+)\/images\/([^)\s]+))\)/g;

export type MarkdownImageResolver = (draftId: string, imageId: string) => Promise<string>;

/** Replace Hub draft image URLs with data URLs so MarkdownPreview can render them. */
export async function rewriteMarkdownImageUrls(
    markdown: string,
    resolve: MarkdownImageResolver,
): Promise<string> {
    const src = String(markdown || '');
    const matches = [...src.matchAll(new RegExp(MOBILE_DOC_IMAGE_RE.source, 'g'))];
    if (matches.length === 0) return src;
    const unique = new Map<string, { draftId: string; imageId: string }>();
    for (const match of matches) {
        const draftId = match[3];
        const imageId = match[4];
        const key = `${draftId}\0${imageId}`;
        if (!unique.has(key)) unique.set(key, { draftId, imageId });
    }
    const urls = new Map<string, string>();
    await Promise.all([...unique.entries()].map(async ([key, ids]) => {
        try {
            const dataUrl = String(await resolve(ids.draftId, ids.imageId) || '').trim();
            if (isRewrittenMarkdownImageUrl(dataUrl)) urls.set(key, dataUrl);
        } catch {
            // Keep the original Hub URL; MarkdownPreview will show a broken image.
        }
    }));
    if (urls.size === 0) return src;
    return src.replace(new RegExp(MOBILE_DOC_IMAGE_RE.source, 'g'), (full, alt, _url, draftId, imageId) => {
        const dataUrl = urls.get(`${draftId}\0${imageId}`);
        if (!dataUrl) return full;
        return `![${alt || imageId}](${dataUrl})`;
    });
}

function isRewrittenMarkdownImageUrl(url: string): boolean {
    if (url.startsWith('blob:')) return !/[)\s]/.test(url);
    // Raster data URLs only. SVG-as-data can carry script; markdown destinations
    // also break on the first ')' / whitespace.
    if (!url.startsWith('data:image/') || /^data:image\/svg\+xml/i.test(url)) return false;
    return !/[)\s]/.test(url);
}
