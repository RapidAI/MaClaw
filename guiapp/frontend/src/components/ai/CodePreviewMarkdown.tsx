/**
 * Markdown preview renderer for the code preview panel.
 * Supports find/go-to-line via data-line-start / data-line-end on blocks.
 */
import React, { useMemo } from 'react';
import ReactMarkdown, { type Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import remarkMath from 'remark-math';
import rehypeKatex from 'rehype-katex';
import type { Plugin } from 'unified';
import 'katex/dist/katex.min.css';
import { documentMarkdownImageSrc, MarkdownExternalLink, MarkdownImagePreview, MarkdownLinkedImage, safeHttpURL } from './markdownMedia';
import type { CodePreviewTheme } from './FileTabBar';
import { markdownPreviewInkFromBg, type MarkdownPreviewInk } from './markdownPreviewInk';

/**
 * `==mark==` is not GFM, but the previous preview painter highlighted it and
 * Obsidian-style notes use it. Rewrite `==x==` text spans to an emphasis node
 * tagged as <mark> so mdast-util-to-hast emits it. Code spans, fenced code and
 * math are different node types and are never visited.
 */
function highlightMarkSyntax(parent: any) {
    const children = parent?.children;
    if (!Array.isArray(children)) return;
    for (let i = 0; i < children.length; i++) {
        const child = children[i];
        if (child?.type === 'text' && typeof child.value === 'string' && child.value.includes('==')) {
            const parts = child.value.split(/(==[^=\n]+==)/g);
            if (parts.length > 1) {
                const replacement = parts.filter(Boolean).map((part: string) => {
                    const m = part.match(/^==([^=\n]+)==$/);
                    return m
                        ? { type: 'emphasis', children: [{ type: 'text', value: m[1] }], data: { hName: 'mark' } }
                        : { type: 'text', value: part };
                });
                children.splice(i, 1, ...replacement);
                i += replacement.length - 1;
                continue;
            }
        }
        highlightMarkSyntax(child);
    }
}

const remarkMarkHighlight: Plugin = () => (tree) => {
    highlightMarkSyntax(tree);
};

/**
 * Two fixups on remark-math output, using source offsets on node positions.
 *
 * - A paragraph that is only `$$...$$` parses as inline math. Promote it to a
 *   display `math` node so the formula gets a block instead of a text run.
 * - `$5 and $1,200` pairs two prices into "math". When the formula delimiters
 *   both sit next to digits it is currency; revert it to the source text.
 */
const remarkDocMathFixups: Plugin = () => (tree, file) => {
    const src = String(file.value || '');
    if (!src.includes('$')) return;
    const fixupParent = (parent: any) => {
        const kids = parent?.children;
        if (!Array.isArray(kids)) return;
        for (let i = 0; i < kids.length; i++) {
            const node = kids[i];
            if (node?.type === 'paragraph') {
                const inner = (node.children || []).filter((c: any) => !(c?.type === 'text' && !String(c?.value || '').trim()));
                if (inner.length === 1 && inner[0].type === 'inlineMath') {
                    const s = inner[0].position?.start?.offset;
                    if (typeof s === 'number' && src.startsWith('$$', s)) {
                        // mdast-util-math maps the node through data.hName.
                        kids[i] = {
                            type: 'math',
                            value: inner[0].value,
                            data: { ...(inner[0].data || {}), hName: 'div', hProperties: { className: ['math', 'math-display'] } },
                            position: node.position,
                        };
                        continue;
                    }
                }
            }
            if (node?.type === 'inlineMath') {
                const s = node.position?.start?.offset;
                const e = node.position?.end?.offset;
                if (
                    typeof s === 'number' && typeof e === 'number'
                    && src[s] === '$' && src[s + 1] !== '$'
                    && /\d/.test(src[s + 1] || '')
                    && /\d/.test(src[e] || '')
                ) {
                    kids[i] = { type: 'text', value: src.slice(s, e) };
                    continue;
                }
            }
            fixupParent(node);
        }
    };
    fixupParent(tree);
};

/**
 * `skipHtml` drops every raw node, which also erases the `<br>` line breaks
 * document markdown puts inside table cells. A raw node that is only a `br`
 * tag is inert, so promote it to a real element; other raw HTML stays dropped.
 */
const rehypeBareBr: Plugin = () => (tree) => {
    const walk = (parent: any) => {
        const kids = parent?.children;
        if (!Array.isArray(kids)) return;
        for (let i = 0; i < kids.length; i++) {
            const node = kids[i];
            if (node?.type === 'raw' && /^\s*<br\s*\/?>\s*$/i.test(String(node.value || ''))) {
                kids[i] = { type: 'element', tagName: 'br', properties: {}, children: [] };
                continue;
            }
            walk(node);
        }
    };
    walk(tree);
};

const EMPTY_MD_MATCH_LINE_INDEXES: number[] = [];

type MdBlockPosition = { start?: { line?: number | null } | null; end?: { line?: number | null } | null };

interface MdPreviewBlockCtx {
    ink: MarkdownPreviewInk;
    matchSet: Set<number>;
    activeMatchLine: number;
}

/** Wrap a markdown block with line-range attrs for find / go-to-line. */
function wrapMdFindBlock(ctx: MdPreviewBlockCtx, node: MdBlockPosition | undefined | null, child: React.ReactNode, className?: string): React.ReactNode {
    const start = Math.max(0, node?.start?.line ?? 0);
    const end = Math.max(start, node?.end?.line ?? start);
    // Fast path when find is closed / no matches — skip the per-line scan.
    // matchSet / activeMatchLine are 0-based; hast positions are 1-based.
    let isMatch = false;
    let isActive = false;
    if (ctx.matchSet.size > 0 || ctx.activeMatchLine >= 0) {
        for (let L = start; L <= end; L++) {
            if (ctx.matchSet.has(L - 1)) isMatch = true;
            if (L - 1 === ctx.activeMatchLine) isActive = true;
            if (isMatch && isActive) break;
        }
    }
    const bg = isActive
        ? 'rgba(234, 179, 8, 0.28)'
        : isMatch
            ? 'rgba(234, 179, 8, 0.12)'
            : undefined;
    return (
        <div
            className={className}
            data-testid="code-preview-md-block"
            data-line-start={start}
            data-line-end={end}
            data-find-match={isMatch ? 'true' : undefined}
            data-find-active={isActive ? 'true' : undefined}
            style={{
                backgroundColor: bg,
                borderRadius: bg ? 4 : undefined,
                margin: bg ? '2px 0' : undefined,
            }}
        >
            {child}
        </div>
    );
}

/** Element children of a hast node, ignoring whitespace-only text. */
function meaningfulChildren(node: any): any[] {
    const children = node?.children;
    if (!Array.isArray(children)) return [];
    return children.filter((c: any) => c.type === 'element' || (c.type === 'text' && String(c.value || '').trim() !== ''));
}

function hastProp(node: any, name: string): string {
    const value = node?.properties?.[name];
    return value == null ? '' : String(value);
}

/** Language tag on a fenced block: the code child's `language-x` class. */
function fencedCodeLanguage(node: any): string {
    const code = (node?.children || []).find((c: any) => c?.type === 'element' && c?.tagName === 'code');
    const className = code?.properties?.className;
    const first = Array.isArray(className) ? String(className[0] || '') : String(className || '');
    return first.replace(/^language-/, '');
}

function buildMarkdownComponents(ctx: MdPreviewBlockCtx): Components {
    const { ink } = ctx;
    const wrap = (node: any, child: React.ReactNode, className?: string) => wrapMdFindBlock(ctx, node?.position, child, className);

    const headingSizes: Record<number, number> = { 1: 22, 2: 18, 3: 15, 4: 14, 5: 13, 6: 12 };
    const headingMargins: Record<number, string> = { 1: '16px 0 8px', 2: '14px 0 6px', 3: '12px 0 4px', 4: '10px 0 4px', 5: '8px 0 3px', 6: '8px 0 3px' };
    const heading = ({ level, node, children }: any) => wrap(node, (
        <div style={{ fontSize: headingSizes[level] || 14, fontWeight: level <= 2 ? 700 : 600, margin: headingMargins[level] || '8px 0 3px', color: ink.emphasis, borderBottom: level <= 2 ? `1px solid ${ink.rule}` : undefined, paddingBottom: level <= 2 ? 4 : undefined }}>
            {children}
        </div>
    ));

    const cellStyle: React.CSSProperties = {
        border: `1px solid ${ink.rule}`,
        padding: '6px 10px',
        fontSize: 13,
        lineHeight: 1.5,
        verticalAlign: 'top',
    };

    return {
        p: ({ node, children }: any) => {
            // A paragraph that is only an image (optionally wrapped in a link)
            // renders as a block figure, not an inline run.
            const only = meaningfulChildren(node);
            if (only.length === 1 && only[0].type === 'element') {
                const el = only[0];
                if (el.tagName === 'img') {
                    const src = documentMarkdownImageSrc(hastProp(el, 'src'));
                    return wrap(node, (
                        <div className="cpm-img-wrap">
                            {src
                                ? <MarkdownImagePreview src={src} alt={hastProp(el, 'alt')} block document />
                                : hastProp(el, 'alt') || null}
                        </div>
                    ));
                }
                if (el.tagName === 'a') {
                    const inner = meaningfulChildren(el);
                    if (inner.length === 1 && inner[0].type === 'element' && inner[0].tagName === 'img') {
                        return wrap(node, (
                            <div className="cpm-img-wrap">
                                <MarkdownLinkedImage alt={hastProp(inner[0], 'alt')} src={hastProp(inner[0], 'src')} pageURL={hastProp(el, 'href')} block document linkColor={ink.link} />
                            </div>
                        ));
                    }
                }
            }
            return wrap(node, <p style={{ margin: '4px 0', lineHeight: 1.6, color: ink.body }}>{children}</p>);
        },
        h1: heading,
        h2: heading,
        h3: heading,
        h4: heading,
        h5: heading,
        h6: heading,
        blockquote: ({ node, children }: any) => wrap(node, (
            <blockquote style={{ borderLeft: `1px solid ${ink.rule}`, paddingLeft: 12, margin: '6px 0', color: ink.muted, fontStyle: 'italic', lineHeight: 1.6 }}>
                {children}
            </blockquote>
        )),
        pre: ({ node, children }: any) => {
            const lang = fencedCodeLanguage(node);
            return wrap(node, (
                <pre style={{ background: ink.wash, border: `1px solid ${ink.rule}`, borderRadius: 6, padding: '10px 14px', margin: '8px 0', overflow: 'auto', fontSize: 13, lineHeight: 1.5 }}>
                    {lang && <span style={{ color: ink.muted, fontSize: 11, display: 'block', marginBottom: 4 }}>{lang}</span>}
                    {children}
                </pre>
            ));
        },
        code: ({ inline, className, children }: any) => inline
            ? <code style={{ background: ink.wash, padding: '1px 4px', borderRadius: 3, fontSize: '0.9em', color: ink.body }}>{children}</code>
            : <code className={className} style={{ color: ink.body, fontFamily: "'Cascadia Code', 'Consolas', monospace" }}>{children}</code>,
        ul: ({ node, children }: any) => wrap(node, <ul className="cpm-ul" style={{ color: ink.body }}>{children}</ul>),
        ol: ({ node, children, start }: any) => wrap(node, <ol className="cpm-ol" start={start} style={{ color: ink.body }}>{children}</ol>),
        li: ({ children, checked }: any) => (
            <li className={checked != null ? 'task-list-item' : undefined} style={{ marginBottom: 2, listStyleType: checked != null ? 'none' : undefined, marginLeft: checked != null ? -16 : undefined }}>
                {children}
            </li>
        ),
        input: ({ checked }: any) => (
            <input type="checkbox" disabled checked={checked === true} readOnly style={{ marginRight: 6, verticalAlign: 'middle' }} />
        ),
        table: ({ node, children }: any) => wrap(node, (
            <div className="cpm-table-scroll">
                <table style={{ borderCollapse: 'collapse', width: '100%', color: ink.body }}>{children}</table>
            </div>
        )),
        th: ({ children, style }: any) => (
            <th style={{ ...cellStyle, ...style, fontWeight: 600, background: ink.wash, color: ink.emphasis }}>{children}</th>
        ),
        td: ({ children, style }: any) => <td style={{ ...cellStyle, ...style }}>{children}</td>,
        tr: ({ children, isHeader, index }: any) => (
            <tr style={{ background: !isHeader && typeof index === 'number' && index % 2 === 1 ? ink.wash : undefined }}>{children}</tr>
        ),
        hr: ({ node }: any) => wrap(node, <hr style={{ border: 'none', borderTop: `1px solid ${ink.rule}`, margin: '12px 0' }} />),
        a: ({ href, children }: any) => {
            const safe = safeHttpURL(String(href || ''));
            if (safe) return <MarkdownExternalLink href={safe} color={ink.link}>{children}</MarkdownExternalLink>;
            // In-page anchors and relative document paths are not navigable here.
            return <span style={{ color: ink.link, textDecoration: 'underline' }}>{children}</span>;
        },
        img: ({ src, alt }: any) => (
            <MarkdownImagePreview className="cpm-inline-img" src={String(src || '')} alt={String(alt || '')} document />
        ),
        strong: ({ children }: any) => <strong style={{ color: ink.emphasis }}>{children}</strong>,
        del: ({ children }: any) => <del className="cpm-del">{children}</del>,
        mark: ({ children }: any) => <mark style={{ background: ink.wash, color: ink.emphasis, padding: '0 2px', borderRadius: 2 }}>{children}</mark>,
        // Display math arrives as div.math-display; every other div comes from
        // a plugin, so it is safe to wrap them all for find/go-to-line.
        div: ({ node, className, children }: any) => {
            const isMathDisplay = String(className || '').includes('math-display');
            return wrap(node, (
                <div className={className} style={isMathDisplay ? { margin: '8px 0', overflowX: 'auto', overflowY: 'hidden' } : undefined}>
                    {children}
                </div>
            ));
        },
        section: ({ node, className, children }: any) => wrap(node, <section className={className} style={{ fontSize: 13 }}>{children}</section>),
    };
}

export const MarkdownPreview = React.memo(function MarkdownPreview({
    content,
    theme,
    matchLineIndexes = EMPTY_MD_MATCH_LINE_INDEXES,
    activeMatchLine = -1,
}: {
    content: string;
    theme: CodePreviewTheme;
    matchLineIndexes?: number[];
    activeMatchLine?: number;
}) {
    const matchSet = useMemo(() => new Set(matchLineIndexes), [matchLineIndexes]);
    const ink = useMemo(() => markdownPreviewInkFromBg(theme.bg), [theme.bg]);

    const components = useMemo(
        () => buildMarkdownComponents({ ink, matchSet, activeMatchLine }),
        [activeMatchLine, ink, matchSet],
    );

    const rendered = useMemo(() => (
        <ReactMarkdown
            remarkPlugins={[remarkGfm, remarkMath, remarkDocMathFixups, remarkMarkHighlight]}
            rehypePlugins={[rehypeBareBr, rehypeKatex]}
            skipHtml
            includeElementIndex
            components={components}
        >
            {content}
        </ReactMarkdown>
    ), [components, content]);

    return (
        <div
            data-testid="code-preview-markdown-view"
            style={{ padding: '16px 20px', fontSize: 14, lineHeight: 1.6, color: ink.body, fontFamily: 'inherit', wordBreak: 'break-word' }}
        >
            {rendered}
        </div>
    );
});
