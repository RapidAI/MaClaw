/**
 * Diff computation module — line-level diff using Myers diff algorithm.
 *
 * Computes the minimal edit script between two text strings, producing
 * a list of DiffLine entries with type (add/delete/unchanged), content,
 * and dual line numbers (oldLineNum for original, newLineNum for modified).
 *
 * Self-contained implementation with no external dependencies.
 */

/** A single line in the diff output. */
export interface DiffLine {
    type: 'add' | 'delete' | 'unchanged';
    content: string;
    /** Line number in the original text (set for unchanged + delete lines). */
    oldLineNum?: number;
    /** Line number in the modified text (set for unchanged + add lines). */
    newLineNum?: number;
}

/**
 * Split text into lines by newline character.
 * Empty string produces an empty array.
 */
function splitLines(text: string): string[] {
    if (text === '') return [];
    return text.split('\n');
}

/**
 * Compute the shortest edit script (SES) between two arrays of lines
 * using the Myers diff algorithm.
 *
 * Returns an array of edit operations:
 *   [0, line]  = unchanged (keep)
 *   [-1, line] = delete from original
 *   [1, line]  = insert from modified
 */
function myersSES(a: string[], b: string[]): Array<[number, string]> {
    const n = a.length;
    const m = b.length;

    if (n === 0 && m === 0) return [];
    if (n === 0) return b.map(line => [1, line]);
    if (m === 0) return a.map(line => [-1, line]);

    const max = n + m;
    const offset = max;
    const size = 2 * max + 1;

    // Each element of trace stores the V array state at the end of step d
    const trace: number[][] = [];
    const v = new Array<number>(size).fill(0);
    v[1 + offset] = 0;

    // Forward pass: find shortest edit distance
    let dFinal = -1;
    for (let d = 0; d <= max; d++) {
        // Save V state at the start of this d
        trace.push([...v]);

        for (let k = -d; k <= d; k += 2) {
            // Decide whether to go down (insert) or right (delete)
            let x: number;
            if (k === -d || (k !== d && v[k - 1 + offset] < v[k + 1 + offset])) {
                x = v[k + 1 + offset]; // down: insert from b
            } else {
                x = v[k - 1 + offset] + 1; // right: delete from a
            }

            let y = x - k;

            // Follow diagonal (matching lines)
            while (x < n && y < m && a[x] === b[y]) {
                x++;
                y++;
            }

            v[k + offset] = x;

            if (x === n && y === m) {
                dFinal = d;
                break;
            }
        }

        if (dFinal >= 0) break;
    }

    if (dFinal < 0) dFinal = max;

    // Backward pass: reconstruct the edit path
    // We walk backwards through the trace to find the sequence of moves
    type Move = { prevX: number; prevY: number; x: number; y: number };
    const moves: Move[] = [];

    let x = n;
    let y = m;

    for (let d = dFinal; d >= 0; d--) {
        const vd = trace[d];
        const k = x - y;

        // Determine which diagonal we came from
        let prevK: number;
        if (d === 0) {
            // At d=0, we only have diagonal moves from (0,0)
            break;
        }

        if (k === -d || (k !== d && vd[k - 1 + offset] < vd[k + 1 + offset])) {
            prevK = k + 1; // came from above (insert)
        } else {
            prevK = k - 1; // came from left (delete)
        }

        const prevX = vd[prevK + offset];
        const prevY = prevX - prevK;

        // Record the move from (prevX, prevY) to (x, y)
        moves.push({ prevX, prevY, x, y });

        x = prevX;
        y = prevY;
    }

    moves.reverse();

    // Build the edit script from the moves
    const edits: Array<[number, string]> = [];

    // Start from (0, 0)
    let cx = 0;
    let cy = 0;

    for (const move of moves) {
        // Diagonal from (cx, cy) to (move.prevX, move.prevY) — these are unchanged lines
        while (cx < move.prevX && cy < move.prevY) {
            edits.push([0, a[cx]]);
            cx++;
            cy++;
        }

        // The non-diagonal step from (move.prevX, move.prevY)
        if (move.prevX === cx && move.prevY === cy) {
            // Determine the type of step
            const nextK = move.x - move.y;
            const prevK2 = move.prevX - move.prevY;

            if (nextK < prevK2 || (move.x === move.prevX && move.y > move.prevY)) {
                // Insert (down move: y increases, x stays)
                edits.push([1, b[cy]]);
                cy++;
            } else {
                // Delete (right move: x increases, y stays)
                edits.push([-1, a[cx]]);
                cx++;
            }

            // Diagonal after the step
            while (cx < move.x && cy < move.y) {
                edits.push([0, a[cx]]);
                cx++;
                cy++;
            }
        }
    }

    // Handle remaining diagonal at d=0 (from (0,0) to wherever we are)
    while (cx < n && cy < m) {
        edits.push([0, a[cx]]);
        cx++;
        cy++;
    }

    return edits;
}

/**
 * Maximum number of lines for diff computation.
 * Files larger than this fall back to showing modified content only,
 * avoiding O((n+m)²) memory usage from the Myers trace array.
 */
const MAX_DIFF_LINES = 5000;

/**
 * Compute a line-level diff between original and modified text.
 *
 * For files exceeding MAX_DIFF_LINES total lines, returns null to signal
 * the caller should fall back to plain view (avoids excessive memory usage).
 * The guard only guards the Myers pass: a file created from nothing (or
 * emptied) has a trivial edit script, so it is diffed at any size.
 *
 * @param original - The original text content
 * @param modified - The modified text content
 * @returns Array of DiffLine entries with type, content, and line numbers,
 *          or null if the input is too large for diff computation
 */
export function computeDiff(original: string, modified: string): DiffLine[] | null {
    const oldLines = splitLines(original);
    const newLines = splitLines(modified);

    // Edge case: both empty
    if (oldLines.length === 0 && newLines.length === 0) {
        return [];
    }

    // Edge case: empty original → all adds (no Myers pass needed)
    if (oldLines.length === 0) {
        return newLines.map((line, i) => ({
            type: 'add' as const,
            content: line,
            newLineNum: i + 1,
        }));
    }

    // Edge case: empty modified → all deletes (no Myers pass needed)
    if (newLines.length === 0) {
        return oldLines.map((line, i) => ({
            type: 'delete' as const,
            content: line,
            oldLineNum: i + 1,
        }));
    }

    // Size guard: skip diff for very large files to avoid O((n+m)²) memory
    if (oldLines.length + newLines.length > MAX_DIFF_LINES) {
        return null;
    }

    const edits = myersSES(oldLines, newLines);

    // Assign line numbers
    let oldNum = 1;
    let newNum = 1;

    return edits.map(([op, content]) => {
        if (op === 0) {
            const line: DiffLine = {
                type: 'unchanged',
                content,
                oldLineNum: oldNum,
                newLineNum: newNum,
            };
            oldNum++;
            newNum++;
            return line;
        } else if (op === -1) {
            const line: DiffLine = {
                type: 'delete',
                content,
                oldLineNum: oldNum,
            };
            oldNum++;
            return line;
        } else {
            const line: DiffLine = {
                type: 'add',
                content,
                newLineNum: newNum,
            };
            newNum++;
            return line;
        }
    });
}

// ── Change-focused row model ──

/** A highlighted run inside a changed line (word-level diff output). */
export interface WordSegment {
    text: string;
    /** true when this run exists on only one side of the change. */
    changed: boolean;
}

/**
 * A visual diff row. `modify` pairs a deleted line with its rewritten
 * counterpart so the UI can show "改了什么" instead of two unrelated rows.
 */
export interface DiffRow {
    kind: 'unchanged' | 'add' | 'delete' | 'modify';
    oldLineNum?: number;
    newLineNum?: number;
    /** Left/original text (unchanged, delete, modify). */
    oldText: string;
    /** Right/modified text (unchanged, add, modify). */
    newText: string;
    /** Word-level marks for the original side of a `modify` row. */
    oldSegments?: WordSegment[];
    /** Word-level marks for the modified side of a `modify` row. */
    newSegments?: WordSegment[];
}

/** Add / remove / rewrite counts derived from paired rows. */
export interface DiffRowStats {
    added: number;
    removed: number;
    modified: number;
}

/** A contiguous block of changed rows plus its surrounding context. */
export interface DiffHunk {
    /** Start index (inclusive) into the row array. */
    start: number;
    /** End index (exclusive) into the row array. */
    end: number;
    oldStart: number;
    oldCount: number;
    newStart: number;
    newCount: number;
}

/** Similarity floor for pairing a deleted line with an added one. */
const PAIR_THRESHOLD = 0.42;
/** Skip pairing on huge rewrite blocks (quadratic similarity scans). */
const MAX_PAIR_BLOCK = 120;
/** Hard cap on similarity probes per block so a pathological file cannot stall the UI. */
const MAX_PAIR_PROBES = 600;
/** Similarity / word-diff guard: cap token counts per line. */
const MAX_COMPARE_TOKENS = 64;
/** Character-level fallback guard for single-token / very long lines. */
const MAX_CHAR_COMPARE = 240;
/** Word-level marks are skipped above this token count (LCS cost). */
const MAX_WORD_DIFF_TOKENS = 150;
/** Long lines skip word-level marks (whole line is marked instead). */
const MAX_WORD_DIFF_LENGTH = 600;

function tokenizeForCompare(text: string): string[] {
    return text.split(/\s+/).filter(Boolean);
}

function tokenizeForWordDiff(text: string): string[] {
    return text.match(/\s+|[A-Za-z0-9_$]+|[\u4e00-\u9fff]|[^\sA-Za-z0-9_$\u4e00-\u9fff]/g) || [];
}

/** LCS length ratio in [0,1] — shared by the word and character fallbacks. */
function lcsRatio(a: string[], b: string[]): number {
    const n = a.length;
    const m = b.length;
    if (n === 0 || m === 0) return 0;
    let prev = new Array<number>(m + 1).fill(0);
    let cur = new Array<number>(m + 1).fill(0);
    for (let i = n - 1; i >= 0; i--) {
        cur[m] = 0;
        for (let j = m - 1; j >= 0; j--) {
            cur[j] = a[i] === b[j] ? prev[j + 1] + 1 : Math.max(prev[j], cur[j + 1]);
        }
        // Rows are consumed right-to-left, so the just-built row becomes the
        // "previous" one; swapping beats copying the row back.
        const spare = prev;
        prev = cur;
        cur = spare;
    }
    return (2 * prev[0]) / (n + m);
}

/**
 * Lower bound on the similarity of two strings, from their lengths alone.
 * Any common subsequence is at most `min(la, lb)` long, so the LCS ratio can
 * never exceed `1 - |la - lb| / (la + lb)`; using `max` keeps it conservative.
 */
function lengthRatio(a: string, b: string): number {
    const max = Math.max(a.length, b.length);
    if (max === 0) return 1;
    return 1 - Math.abs(a.length - b.length) / max;
}

/** Cheap prefix/suffix ratio for lines too long to diff exactly. */
function edgeRatio(x: string, y: string): number {
    let head = 0;
    const max = Math.min(x.length, y.length);
    while (head < max && x[head] === y[head]) head++;
    let tail = 0;
    while (tail < max - head && x[x.length - 1 - tail] === y[y.length - 1 - tail]) tail++;
    return (head + tail) / Math.max(x.length, y.length);
}

/** Similarity in [0,1] — word-level LCS, character-level for single tokens. */
export function lineSimilarity(a: string, b: string): number {
    const x = a.trim();
    const y = b.trim();
    if (x === y) return 1;
    if (!x || !y) return 0;
    const wa = tokenizeForCompare(x);
    const wb = tokenizeForCompare(y);
    // One-token lines (identifiers, long paths) carry no word signal, and very
    // long lines are too costly: fall back to characters.
    if (wa.length <= 1 || wb.length <= 1 || wa.length > MAX_COMPARE_TOKENS || wb.length > MAX_COMPARE_TOKENS) {
        if (x.length > MAX_CHAR_COMPARE || y.length > MAX_CHAR_COMPARE) return edgeRatio(x, y);
        return lcsRatio(x.split(''), y.split(''));
    }
    return lcsRatio(wa, wb);
}

/** Longest common subsequence index pairs over two token arrays. */
function lcsPairs(a: string[], b: string[]): { ai: Set<number>; bi: Set<number> } {
    const n = a.length;
    const m = b.length;
    const ai = new Set<number>();
    const bi = new Set<number>();
    if (n === 0 || m === 0) return { ai, bi };
    const width = m + 1;
    const dp = new Int32Array((n + 1) * width);
    for (let i = n - 1; i >= 0; i--) {
        for (let j = m - 1; j >= 0; j--) {
            dp[i * width + j] = a[i] === b[j]
                ? dp[(i + 1) * width + j + 1] + 1
                : Math.max(dp[(i + 1) * width + j], dp[i * width + j + 1]);
        }
    }
    let i = 0;
    let j = 0;
    while (i < n && j < m) {
        if (a[i] === b[j]) {
            ai.add(i);
            bi.add(j);
            i++;
            j++;
        } else if (dp[(i + 1) * width + j] >= dp[i * width + j + 1]) {
            i++;
        } else {
            j++;
        }
    }
    return { ai, bi };
}

function mergeSegments(segments: WordSegment[]): WordSegment[] {
    const out: WordSegment[] = [];
    for (const seg of segments) {
        if (!seg.text) continue;
        const last = out[out.length - 1];
        if (last && last.changed === seg.changed) {
            last.text += seg.text;
        } else {
            out.push({ ...seg });
        }
    }
    return out;
}

/** Drop leading/trailing whitespace out of a changed run so marks hug the text. */
function trimWhitespaceEdges(segments: WordSegment[]): WordSegment[] {
    const out: WordSegment[] = [];
    for (const seg of segments) {
        if (!seg.changed || seg.text.trim() === '') {
            out.push(seg);
            continue;
        }
        const lead = /^\s+/.exec(seg.text)?.[0] ?? '';
        const trail = /\s+$/.exec(seg.text)?.[0] ?? '';
        const core = seg.text.slice(lead.length, seg.text.length - trail.length);
        if (lead) out.push({ text: lead, changed: false });
        if (core) out.push({ text: core, changed: seg.changed });
        if (trail) out.push({ text: trail, changed: false });
    }
    return out;
}

/**
 * Word-level diff for one rewritten line.
 * Returns the marked runs for both sides; the whole line is marked when the
 * line is too long to compare cheaply.
 */
export function computeWordSegments(oldText: string, newText: string): { old: WordSegment[]; new: WordSegment[] } {
    if (oldText.length > MAX_WORD_DIFF_LENGTH || newText.length > MAX_WORD_DIFF_LENGTH) {
        return {
            old: [{ text: oldText, changed: true }],
            new: [{ text: newText, changed: true }],
        };
    }
    const a = tokenizeForWordDiff(oldText);
    const b = tokenizeForWordDiff(newText);
    if (a.length > MAX_WORD_DIFF_TOKENS || b.length > MAX_WORD_DIFF_TOKENS) {
        return {
            old: [{ text: oldText, changed: true }],
            new: [{ text: newText, changed: true }],
        };
    }
    const { ai, bi } = lcsPairs(a, b);
    const build = (tokens: string[], matched: Set<number>): WordSegment[] => {
        const raw: WordSegment[] = tokens.map((t, i) => ({ text: t, changed: !matched.has(i) }));
        return mergeSegments(trimWhitespaceEdges(mergeSegments(raw)));
    };
    return { old: build(a, ai), new: build(b, bi) };
}

/**
 * Pair adjacent delete/add runs into `modify` rows so the preview can point at
 * the exact rewrite instead of showing a bare removal plus a bare insertion.
 */
export function buildDiffRows(lines: DiffLine[]): DiffRow[] {
    const rows: DiffRow[] = [];
    let i = 0;
    while (i < lines.length) {
        const line = lines[i];
        if (line.type === 'unchanged') {
            rows.push({
                kind: 'unchanged',
                oldLineNum: line.oldLineNum,
                newLineNum: line.newLineNum,
                oldText: line.content,
                newText: line.content,
            });
            i++;
            continue;
        }
        let j = i;
        while (j < lines.length && lines[j].type !== 'unchanged') j++;
        appendChangeBlock(lines, i, j, rows);
        i = j;
    }
    // `splitLines` turns a trailing newline into one extra empty line, so a file
    // that merely gained its final "\n" would report a phantom "+1 blank line".
    // That artifact is always the last row and empty on both sides; drop it.
    const last = rows[rows.length - 1];
    if (last && (last.kind === 'add' || last.kind === 'delete') && last.oldText === '' && last.newText === '') {
        rows.pop();
    }
    return rows;
}

function appendChangeBlock(lines: DiffLine[], from: number, to: number, out: DiffRow[]): void {
    const dels: number[] = [];
    const adds: number[] = [];
    for (let k = from; k < to; k++) {
        if (lines[k].type === 'delete') dels.push(k);
        else adds.push(k);
    }
    // delete index -> add index
    const pairs = new Map<number, number>();
    if (dels.length > 0 && adds.length > 0 && (dels.length + adds.length) <= MAX_PAIR_BLOCK) {
        const taken = new Set<number>();
        let probes = MAX_PAIR_PROBES;
        for (const di of dels) {
            if (probes <= 0) break;
            const oldText = lines[di].content;
            let best = -1;
            let bestScore = 0;
            for (const ai of adds) {
                if (taken.has(ai)) continue;
                if (probes <= 0) break;
                const newText = lines[ai].content;
                // Cheap reject first: the LCS ratio is bounded by the length
                // ratio, so very different lengths cannot reach the threshold.
                if (lengthRatio(oldText, newText) < PAIR_THRESHOLD) continue;
                probes--;
                const score = lineSimilarity(oldText, newText);
                if (score > bestScore) {
                    bestScore = score;
                    best = ai;
                }
            }
            if (best >= 0 && bestScore >= PAIR_THRESHOLD) {
                pairs.set(di, best);
                taken.add(best);
            }
        }
    }
    const pairedAdds = new Set(pairs.values());
    for (let k = from; k < to; k++) {
        const line = lines[k];
        if (line.type === 'delete') {
            const addIdx = pairs.get(k);
            if (addIdx != null) {
                const added = lines[addIdx];
                const marks = computeWordSegments(line.content, added.content);
                out.push({
                    kind: 'modify',
                    oldLineNum: line.oldLineNum,
                    newLineNum: added.newLineNum,
                    oldText: line.content,
                    newText: added.content,
                    oldSegments: marks.old,
                    newSegments: marks.new,
                });
            } else {
                out.push({ kind: 'delete', oldLineNum: line.oldLineNum, oldText: line.content, newText: '' });
            }
        } else if (!pairedAdds.has(k)) {
            out.push({ kind: 'add', newLineNum: line.newLineNum, oldText: '', newText: line.content });
        }
    }
}

/** `+added / -removed / ~modified` for the paired row model. */
export function computeDiffRowStats(rows: DiffRow[]): DiffRowStats {
    let added = 0;
    let removed = 0;
    let modified = 0;
    for (const row of rows) {
        if (row.kind === 'add' || row.kind === 'modify') added++;
        if (row.kind === 'delete' || row.kind === 'modify') removed++;
        if (row.kind === 'modify') modified++;
    }
    return { added, removed, modified };
}

/**
 * Group changed rows into hunks with `context` unchanged rows around them.
 * Hunks let the view collapse untouched regions so every change is on screen.
 */
export function buildDiffHunks(rows: DiffRow[], context = 3): DiffHunk[] {
    const changed: number[] = [];
    for (let i = 0; i < rows.length; i++) {
        if (rows[i].kind !== 'unchanged') changed.push(i);
    }
    if (changed.length === 0) return [];

    const hunks: DiffHunk[] = [];
    let start = Math.max(0, changed[0] - context);
    let end = Math.min(rows.length, changed[0] + 1 + context);
    for (let c = 1; c < changed.length; c++) {
        const nextStart = Math.max(0, changed[c] - context);
        const nextEnd = Math.min(rows.length, changed[c] + 1 + context);
        if (nextStart <= end) {
            end = nextEnd;
        } else {
            hunks.push(toHunk(rows, start, end));
            start = nextStart;
            end = nextEnd;
        }
    }
    hunks.push(toHunk(rows, start, end));
    return hunks;
}

function toHunk(rows: DiffRow[], start: number, end: number): DiffHunk {
    let oldStart = 0;
    let newStart = 0;
    let oldCount = 0;
    let newCount = 0;
    for (let i = start; i < end; i++) {
        const row = rows[i];
        if (row.oldLineNum != null) {
            if (oldStart === 0) oldStart = row.oldLineNum;
            oldCount++;
        }
        if (row.newLineNum != null) {
            if (newStart === 0) newStart = row.newLineNum;
            newCount++;
        }
    }
    // Pure insert/delete hunks have no line number on one side: report the
    // position they sit at (git's `@@ -l,0 @@` form) instead of a bare 0.
    if (oldStart === 0) oldStart = lastLineNumBefore(rows, start, 'old') ?? 0;
    if (newStart === 0) newStart = lastLineNumBefore(rows, start, 'new') ?? 0;
    return { start, end, oldStart, oldCount, newStart, newCount };
}

function lastLineNumBefore(rows: DiffRow[], start: number, side: 'old' | 'new'): number | undefined {
    for (let i = start - 1; i >= 0; i--) {
        const num = side === 'old' ? rows[i].oldLineNum : rows[i].newLineNum;
        if (num != null) return num;
    }
    return undefined;
}
