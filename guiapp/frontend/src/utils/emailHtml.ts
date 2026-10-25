// Sanitizer for untrusted email HTML. Mail bodies come from outside, so the
// reading pane renders them only after every script carrier, event handler,
// and javascript: URL is gone. Remote pictures stay blocked until the user
// asks for them, which keeps tracking pixels dark by default.

export type SanitizedEmailHtml = {
    html: string;
    /** True when the mail referenced at least one remote picture that was blocked. */
    hasBlockedImages: boolean;
};

const MAX_HTML_BYTES = 512 * 1024;

const REMOVED_TAGS = new Set([
    'script', 'iframe', 'frame', 'frameset', 'object', 'embed', 'applet',
    'link', 'meta', 'base', 'form', 'input', 'button', 'select', 'textarea',
    'option', 'noscript', 'template', 'title', 'head',
]);

// Attributes naming a URL. Each is checked against the scheme rules; an
// entry with a forbidden scheme loses just the attribute, not the element.
const URL_ATTRS = new Set(['href', 'src', 'action', 'formaction', 'background', 'poster', 'xlink:href', 'data']);

// Dropped outright: they embed URLs the element attributes above cannot see.
const REMOVED_ATTRS = new Set(['srcset', 'srcdoc', 'formaction', 'ping']);

const FORBIDDEN_URL_PREFIXES = /^(?:javascript|vbscript|blob|file):/i;
// data: URLs pass the forbidden list only as embedded images; other payloads
// (data:text/html, …) are script carriers and must not survive.
const FORBIDDEN_DATA_URL = /^data:(?!image\/(?:png|gif|jpeg|jpg|webp|bmp|svg\+xml)\s*[;,])/i;
const SAFE_HREF_PREFIX = /^(?:https?:|mailto:|tel:)/i;
const REMOTE_SRC_PREFIX = /^https?:/i;
const INLINE_SRC_PREFIX = /^data:image\//i;

function attrValue(element: Element, name: string): string {
    return element.getAttribute(name) || '';
}

/**
 * Drop dangerous URLs from one attribute. A relative URL is a mail leftover
 * that would resolve against the app shell, so it is removed as well. Links
 * keep their text when the target is dropped. Returns true when a remote
 * picture URL was stripped.
 */
function cleanUrlAttr(element: Element, name: string, allowRemoteImages: boolean): boolean {
    if (!element.hasAttribute(name)) return false;
    const value = attrValue(element, name).trim();
    const drop = () => { element.removeAttribute(name); return false; };
    if (!value || FORBIDDEN_URL_PREFIXES.test(value) || FORBIDDEN_DATA_URL.test(value)) return drop();
    if (name === 'href') return SAFE_HREF_PREFIX.test(value) ? false : drop();
    if (name === 'src' || name === 'poster' || name === 'background') {
        if (INLINE_SRC_PREFIX.test(value)) return false;
        if (REMOTE_SRC_PREFIX.test(value)) {
            if (!allowRemoteImages) {
                element.removeAttribute(name);
                return true;
            }
            return false;
        }
        return drop();
    }
    return false;
}

/** Strip style values that can escape the reading pane or execute. */
function cleanStyleAttr(value: string): string {
    if (!value) return '';
    return value
        .replace(/expression\s*\(/gi, '(')
        .replace(/javascript\s*:/gi, '')
        .replace(/position\s*:\s*fixed/gi, '')
        .replace(/url\s*\(\s*['"]?\s*(?:javascript|vbscript)\s*:?[^)]*\)/gi, 'none')
        .replace(/url\s*\(\s*['"]?\s*data:(?!image\/)[^)]*\)/gi, 'none');
}

function cleanAttributes(element: Element, allowRemoteImages: boolean): boolean {
    let blockedRemote = false;
    for (const name of Array.from(element.getAttributeNames())) {
        const lower = name.toLowerCase();
        if (lower.startsWith('on')) {
            element.removeAttribute(name);
            continue;
        }
        if (URL_ATTRS.has(lower)) {
            if (cleanUrlAttr(element, name, allowRemoteImages)) blockedRemote = true;
            continue;
        }
        if (lower === 'style') {
            element.setAttribute('style', cleanStyleAttr(attrValue(element, name)));
            continue;
        }
        if (REMOVED_ATTRS.has(lower)) {
            element.removeAttribute(name);
        }
    }
    return blockedRemote;
}

const BLOCKED_IMAGE_CLASS = 'email-html-blocked-image';

function replaceBlockedImage(element: Element): void {
    const holder = element.ownerDocument.createElement('span');
    holder.className = BLOCKED_IMAGE_CLASS;
    holder.setAttribute('data-blocked-image', '1');
    holder.textContent = element.getAttribute('alt')?.trim() || '🖼';
    if (element.parentNode) {
        element.replaceWith(holder);
    }
}

/**
 * Scope a stylesheet to the reading pane: every selector is prefixed so mail
 * CSS cannot restyle the application around the pane. At-rule preludes
 * (@media, @font-face) and declaration blocks are left untouched. Unbalanced
 * CSS is dropped rather than half-applied, @keyframes blocks go away because
 * their percentage selectors are not element selectors, and top-level
 * @import/@namespace statements come out — an imported sheet applies
 * document-wide, outside every scope prefix. With remote content blocked,
 * url(...) references to http(s) targets are removed too, so a stylesheet
 * cannot double as a tracking pixel.
 */
function scopeCss(text: string, scopeSelector: string, allowRemoteImages: boolean): string {
    text = text.replace(/@keyframes[^{]*\{(?:[^{}]*\{[^{}]*\})*[^{}]*\}/gi, '');
    let out = '';
    let depth = 0;
    let start = 0;
    for (let i = 0; i < text.length; i++) {
        const ch = text[i];
        if (ch === '{') {
            const selector = text.slice(start, i).trim();
            depth += 1;
            out += (selector.startsWith('@') ? selector : `${scopeSelector} ${selector}`) + ' {';
            start = i + 1;
            continue;
        }
        if (ch === '}') {
            out += text.slice(start, i) + '}';
            depth -= 1;
            start = i + 1;
            continue;
        }
        if (ch === ';' && depth === 0) {
            const stmt = text.slice(start, i + 1).trim();
            if (!/^@(?:import|namespace)\b/i.test(stmt)) out += stmt + ' ';
            start = i + 1;
        }
    }
    if (depth !== 0) return '';
    out = out.replace(/position\s*:\s*fixed/gi, '');
    if (!allowRemoteImages) {
        out = out.replace(/url\(\s*(['"]?)\s*(?:https?:)?\/\/[^)'"]+\1\s*\)/gi, 'none');
    }
    return out;
}

/**
 * Rewrite <style> blocks so mail CSS cannot restyle the whole application.
 * Scoped sheets are moved into the body root: the parser puts them in <head>,
 * and the reading pane renders the body's HTML only.
 */
function scopeStyleTags(doc: Document, root: Element, scopeSelector: string, allowRemoteImages: boolean): void {
    for (const tag of Array.from(doc.querySelectorAll('style'))) {
        const scoped = scopeCss(tag.textContent || '', scopeSelector, allowRemoteImages);
        if (scoped.trim()) {
            tag.textContent = scoped;
            root.appendChild(tag);
        } else {
            tag.remove();
        }
    }
}

export function sanitizeEmailHtml(raw: string, allowRemoteImages: boolean): SanitizedEmailHtml {
    const clipped = raw.length > MAX_HTML_BYTES ? raw.slice(0, MAX_HTML_BYTES) : raw;
    if (!clipped.trim()) return { html: '', hasBlockedImages: false };
    let doc: Document;
    try {
        doc = new DOMParser().parseFromString(clipped, 'text/html');
    } catch {
        return { html: '', hasBlockedImages: false };
    }
    const root = doc.body || doc.documentElement;
    if (!root) return { html: '', hasBlockedImages: false };
    let hasBlockedImages = false;
    const walker = doc.createTreeWalker(root, 1 /* element nodes */);
    const elements: Element[] = [];
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        elements.push(node as Element);
    }
    for (const element of elements) {
        if (REMOVED_TAGS.has(element.tagName.toLowerCase())) {
            element.remove();
            continue;
        }
        if (!allowRemoteImages && element.tagName.toLowerCase() === 'img'
            && (REMOTE_SRC_PREFIX.test(attrValue(element, 'src').trim()) || element.hasAttribute('srcset'))) {
            hasBlockedImages = true;
            replaceBlockedImage(element);
            continue;
        }
        if (cleanAttributes(element, allowRemoteImages)) hasBlockedImages = true;
    }
    scopeStyleTags(doc, root, '.email-html-body', allowRemoteImages);
    return { html: root.innerHTML, hasBlockedImages };
}
