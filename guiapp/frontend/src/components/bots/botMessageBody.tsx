import { memo, type ReactNode } from 'react';
import { parseMarkdownTableCells } from '../ai/aiAssistantMarkdownTable';

// Bot replies often arrive as raw Markdown. The transcript stores that text
// unchanged and only formats assistant bubbles here.

type Block =
    | { kind: 'p'; text: string; heading: boolean }
    | { kind: 'h'; level: number; text: string }
    | { kind: 'pre'; text: string }
    | { kind: 'quote'; blocks: Block[] }
    | { kind: 'rule' }
    | { kind: 'table'; header: string[]; rows: string[][] }
    | { kind: 'list'; ordered: boolean; start?: number; items: Item[] };

type Item = { text: string; blocks: Block[]; n?: number };

const LIST_LINE = /^(\s*)([-*]|\d+\.)\s+(.*)$/;
const TOP_LIST_LINE = /^([-*]|\d+\.)\s+/;
const ATX_HEADING = /^(#{1,6})[ \t]+(.*)$/;
const RULE_LINE = /^\s*([-*_])\1{2,}\s*$/;

function listKind(line: string): 'ol' | 'ul' | null {
    const marker = LIST_LINE.exec(line);
    if (!marker || marker[1].length !== 0) return null;
    return /^\d+\.$/.test(marker[2]) ? 'ol' : 'ul';
}

export function botMessageIsStructured(text: string): boolean {
    return text.includes('```')
        || /\*\*[^*\n]+\*\*/.test(text)
        || /`[^`\n]+`/.test(text)
        || /(^|\n)\s*(?:[-*]|\d+\.)\s+\S/.test(text)
        || /(^|\n)\s*#{1,6}[ \t]+\S/.test(text)
        || /(^|\n)\s*>\s*\S/.test(text)
        || /(^|\n)\s*([-*_])\2{2,}\s*(?:\n|$)/.test(text)
        || /(^|\n)\s*[\|\uFF5C\u2502]?\s*:?[\-\uFF0D\u2014\u2013\u2500]{3,}:?\s*[\|\uFF5C\u2502]/.test(text)
        || /(^|\n)\s*[\|\uFF5C\u2502][^\n]*\n[^\n]*[\|\uFF5C\u2502]/.test(text);
}

// Models sometimes draw a table with a fullwidth bar or a box-drawing bar.
// Those are still column separators. A sentence that only mentions one stays
// ordinary text, because a row still needs two cells.
function pipeGlyphs(line: string): string {
    return line.replace(/[\uFF5C\u2502]/g, '|');
}

function headingOf(line: string): { level: number; text: string } | null {
    const match = ATX_HEADING.exec(line.trim());
    if (!match) return null;
    const text = match[2].trim().replace(/\s+#+\s*$/, '').trim();
    if (!text) return null;
    return { level: match[1].length, text };
}

// Digital-employee replies prefix a pipe row with a list marker:
// "- | 操作 | 状态 |". Only a row that itself starts with "|" is a table row.
// A bullet whose words merely contain a pipe stays a list.
const TABLE_LIST_PREFIX = /^(?:[-*+]|\u2022|\u00b7|\d+[.)])\s+(\|.*)$/;

function leadingPipeLine(line: string): string | null {
    const trimmed = pipeGlyphs(line).trim();
    const marked = TABLE_LIST_PREFIX.exec(trimmed);
    const candidate = (marked ? marked[1] : trimmed).trim();
    if (!candidate.startsWith('|')) return null;
    return candidate;
}

function isDashRow(cells: string[]): boolean {
    return cells.length >= 2 && cells.every(cell => /^:?[\-\uFF0D\u2014\u2013\u2500]{3,}:?$/.test(cell));
}

// A pipe row may omit the outer "|". A list item, heading, rule, or quote is
// never a row, so "- 看 a | b" stays a list.
function cellsOf(line: string): string[] | null {
    const led = leadingPipeLine(line);
    const candidate = led ?? pipeGlyphs(line).trim();
    if (!candidate.includes('|')) return null;
    if (!led && (listKind(candidate) || headingOf(candidate) || RULE_LINE.test(candidate) || candidate.startsWith('>') || candidate.startsWith('```'))) {
        return null;
    }
    const cells = parseMarkdownTableCells(candidate);
    if (cells.length < 2) return null;
    return cells;
}

function isSeparator(line: string): boolean {
    const cells = cellsOf(line);
    return !!cells && isDashRow(cells);
}

function bodyCells(line: string): string[] | null {
    const cells = cellsOf(line);
    if (!cells || isDashRow(cells)) return null;
    return cells;
}

function parseInline(text: string, keyPrefix: string): ReactNode[] {
    const nodes: ReactNode[] = [];
    let start = 0;
    let index = 0;
    let serial = 0;
    const flush = (end: number) => {
        if (end > start) nodes.push(text.slice(start, end));
        start = end;
    };
    while (index < text.length) {
        if (text[index] === '`') {
            const end = text.indexOf('`', index + 1);
            if (end > index + 1) {
                flush(index);
                nodes.push(<code key={`${keyPrefix}-c${serial}`}>{text.slice(index + 1, end)}</code>);
                serial += 1;
                index = end + 1;
                start = index;
                continue;
            }
        }
        if (text.startsWith('**', index)) {
            let cursor = index + 2;
            let end = -1;
            while (cursor < text.length) {
                if (text[cursor] === '`') {
                    const codeEnd = text.indexOf('`', cursor + 1);
                    if (codeEnd > cursor + 1) {
                        cursor = codeEnd + 1;
                        continue;
                    }
                }
                if (text.startsWith('**', cursor)) {
                    end = cursor;
                    break;
                }
                cursor += 1;
            }
            if (end > index + 2) {
                flush(index);
                const innerKey = `${keyPrefix}-b${serial}`;
                nodes.push(<strong key={innerKey}>{parseInline(text.slice(index + 2, end), innerKey)}</strong>);
                serial += 1;
                index = end + 2;
                start = index;
                continue;
            }
        }
        index += 1;
    }
    flush(text.length);
    return nodes;
}

function flushParagraph(paragraph: string[], blocks: Block[]) {
    const text = paragraph.join('\n').trim();
    paragraph.length = 0;
    if (!text) return;
    blocks.push({ kind: 'p', text, heading: /^\*\*[^*\n]+\*\*$/.test(text) });
}

function sameListMarker(line: string, ordered: boolean): boolean {
    const kind = listKind(line);
    return ordered ? kind === 'ol' : kind === 'ul';
}

// An opening fence with no closer must not swallow the rest of the reply.
function takeFence(lines: string[], index: number): { text: string; next: number } | null {
    if (!lines[index].trimStart().startsWith('```')) return null;
    const body: string[] = [];
    let cursor = index + 1;
    while (cursor < lines.length && !lines[cursor].trimStart().startsWith('```')) {
        body.push(lines[cursor]);
        cursor += 1;
    }
    if (cursor >= lines.length) return null;
    return { text: body.join('\n'), next: cursor + 1 };
}

function nextContent(lines: string[], index: number): number {
    let look = index;
    while (look < lines.length && !lines[look].trim()) look += 1;
    return look;
}

// Drop only the shared continuation indent. Extra spaces stay, so a nested
// fence keeps its own indentation. Blank lines stay in the item: a fence can
// contain them, and the next top-level marker still starts the next item.
function dedent(lines: string[]): string {
    let indent = Infinity;
    for (const line of lines) {
        if (!line.trim()) continue;
        const width = /^\s*/.exec(line)?.[0].length ?? 0;
        if (width < indent) indent = width;
    }
    if (indent === Infinity || indent === 0) return lines.join('\n');
    return lines.map(line => (line.trim() ? line.slice(indent) : '')).join('\n');
}

function parseQuote(lines: string[], start: number): { block: Block; next: number } {
    const inner: string[] = [];
    let index = start;
    while (index < lines.length && /^\s*>/.test(lines[index])) {
        inner.push(lines[index].replace(/^\s*> ?/, ''));
        index += 1;
    }
    return { block: { kind: 'quote', blocks: parseBlocks(inner.join('\n')) }, next: index };
}

function parseTable(lines: string[], start: number): { block: Block; next: number } | null {
    if (isSeparator(lines[start]) || start + 1 >= lines.length) return null;
    const header = bodyCells(lines[start]);
    if (!header) return null;
    // A row without a leading "|" is a header only when the next line is a delimiter.
    if (!leadingPipeLine(lines[start]) && !isSeparator(lines[start + 1])) return null;
    const separated = isSeparator(lines[start + 1]);
    if (!separated && !bodyCells(lines[start + 1])) return null;
    const rawRows: string[][] = [];
    let width = header.length;
    let index = separated ? start + 2 : start + 1;
    while (index < lines.length && lines[index].trim()) {
        if (isSeparator(lines[index])) {
            index += 1;
            continue;
        }
        const cells = bodyCells(lines[index]);
        if (!cells) break;
        rawRows.push(cells);
        if (cells.length > width) width = cells.length;
        index += 1;
    }
    const headerCells = width > header.length
        ? [...header, ...Array.from({ length: width - header.length }, () => '')]
        : header;
    const rows = rawRows.map(cells => headerCells.map((_, cell) => cells[cell] ?? ''));
    return { block: { kind: 'table', header: headerCells, rows }, next: index };
}

function parseList(lines: string[], start: number): { block: Block; next: number } {
    const first = LIST_LINE.exec(lines[start]);
    const ordered = !!first && /^\d+\.$/.test(first[2]);
    const startNum = ordered && first ? Number(first[2].slice(0, -1)) : undefined;
    const items: Item[] = [];
    let index = start;
    while (index < lines.length) {
        if (!lines[index].trim()) {
            const look = nextContent(lines, index + 1);
            const next = lines[look];
            if (look >= lines.length || (!sameListMarker(next, ordered) && !/^\s+\S/.test(next))) break;
            if (sameListMarker(next, ordered)) {
                index = look;
                continue;
            }
        }
        const marker = lines[index].trim() ? LIST_LINE.exec(lines[index]) : null;
        if (marker && marker[1].length === 0) {
            if (/^\d+\.$/.test(marker[2]) !== ordered) break;
            const n = ordered ? Number(marker[2].slice(0, -1)) : undefined;
            items.push({ text: marker[3], blocks: [], n });
            index += 1;
            continue;
        }
        if (!items.length) break;
        const chunk: string[] = [];
        while (index < lines.length) {
            if (!lines[index].trim()) {
                const look = nextContent(lines, index + 1);
                if (look >= lines.length || TOP_LIST_LINE.test(lines[look])) break;
                chunk.push(lines[index]);
                index += 1;
                continue;
            }
            if (TOP_LIST_LINE.test(lines[index])) break;
            chunk.push(lines[index]);
            index += 1;
        }
        if (chunk.length) items[items.length - 1].blocks.push(...parseBlocks(dedent(chunk)));
    }
    return { block: { kind: 'list', ordered, start: startNum, items }, next: index };
}

function parseBlocks(src: string): Block[] {
    const lines = src.replace(/\r\n/g, '\n').split('\n');
    const blocks: Block[] = [];
    const paragraph: string[] = [];
    let index = 0;

    while (index < lines.length) {
        const line = lines[index];
        if (!line.trim()) {
            flushParagraph(paragraph, blocks);
            index += 1;
            continue;
        }
        const fence = takeFence(lines, index);
        if (line.trimStart().startsWith('```') && fence) {
            flushParagraph(paragraph, blocks);
            blocks.push({ kind: 'pre', text: fence.text });
            index = fence.next;
            continue;
        }
        const table = parseTable(lines, index);
        if (table) {
            flushParagraph(paragraph, blocks);
            blocks.push(table.block);
            index = table.next;
            continue;
        }
        if (RULE_LINE.test(line)) {
            flushParagraph(paragraph, blocks);
            blocks.push({ kind: 'rule' });
            index += 1;
            continue;
        }
        const heading = headingOf(line);
        if (heading) {
            flushParagraph(paragraph, blocks);
            blocks.push({ kind: 'h', level: heading.level, text: heading.text });
            index += 1;
            continue;
        }
        if (/^\s*>/.test(line)) {
            flushParagraph(paragraph, blocks);
            const quote = parseQuote(lines, index);
            if (quote.block.kind === 'quote' && quote.block.blocks.length) blocks.push(quote.block);
            index = quote.next;
            continue;
        }
        if (listKind(line)) {
            flushParagraph(paragraph, blocks);
            const parsed = parseList(lines, index);
            blocks.push(parsed.block);
            index = parsed.next;
            continue;
        }
        paragraph.push(line.trim());
        index += 1;
    }
    flushParagraph(paragraph, blocks);
    return blocks;
}

function renderBlocks(blocks: Block[], keyPrefix: string): ReactNode[] {
    return blocks.map((block, index) => {
        const key = `${keyPrefix}-${index}`;
        if (block.kind === 'pre') {
            return <pre key={key} className="desktop-bot-chat__pre"><code>{block.text}</code></pre>;
        }
        if (block.kind === 'h') {
            const Tag = `h${block.level}` as 'h1' | 'h2' | 'h3' | 'h4' | 'h5' | 'h6';
            return <Tag key={key} className="desktop-bot-chat__heading">{parseInline(block.text, key)}</Tag>;
        }
        if (block.kind === 'rule') {
            return <hr key={key} className="desktop-bot-chat__rule" />;
        }
        if (block.kind === 'quote') {
            return <blockquote key={key} className="desktop-bot-chat__quote">{renderBlocks(block.blocks, key)}</blockquote>;
        }
        if (block.kind === 'table') {
            return (
                <div key={key} className="desktop-bot-chat__table-wrap">
                    <table className="desktop-bot-chat__table">
                        <thead>
                            <tr>
                                {block.header.map((cell, cellIndex) => (
                                    <th key={`${key}-h${cellIndex}`} scope="col">{parseInline(cell, `${key}-h${cellIndex}`)}</th>
                                ))}
                            </tr>
                        </thead>
                        <tbody>
                            {block.rows.map((row, rowIndex) => (
                                <tr key={`${key}-r${rowIndex}`}>
                                    {row.map((cell, cellIndex) => (
                                        <td key={`${key}-r${rowIndex}-c${cellIndex}`}>{parseInline(cell, `${key}-r${rowIndex}-c${cellIndex}`)}</td>
                                    ))}
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            );
        }
        if (block.kind === 'p') {
            return (
                <p key={key} className={block.heading ? 'desktop-bot-chat__heading' : undefined}>
                    {parseInline(block.text, key)}
                </p>
            );
        }
        const items = block.items.map((item, itemIndex) => (
            <li key={`${key}-${itemIndex}`} value={item.n}>
                {parseInline(item.text, `${key}-${itemIndex}`)}
                {item.blocks.length ? renderBlocks(item.blocks, `${key}-${itemIndex}-n`) : null}
            </li>
        ));
        return block.ordered
            ? <ol key={key} start={block.start}>{items}</ol>
            : <ul key={key}>{items}</ul>;
    });
}

export const BotMessageBody = memo(function BotMessageBody({ text }: { text: string }) {
    if (!botMessageIsStructured(text)) return <>{text}</>;
    return <div className="desktop-bot-chat__body">{renderBlocks(parseBlocks(text), 'b')}</div>;
});
