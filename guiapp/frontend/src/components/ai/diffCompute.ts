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
 *
 * @param original - The original text content
 * @param modified - The modified text content
 * @returns Array of DiffLine entries with type, content, and line numbers,
 *          or null if the input is too large for diff computation
 */
export function computeDiff(original: string, modified: string): DiffLine[] | null {
    const oldLines = splitLines(original);
    const newLines = splitLines(modified);

    // Size guard: skip diff for very large files to avoid O((n+m)²) memory
    if (oldLines.length + newLines.length > MAX_DIFF_LINES) {
        return null;
    }

    // Edge case: both empty
    if (oldLines.length === 0 && newLines.length === 0) {
        return [];
    }

    // Edge case: empty original → all adds
    if (oldLines.length === 0) {
        return newLines.map((line, i) => ({
            type: 'add' as const,
            content: line,
            newLineNum: i + 1,
        }));
    }

    // Edge case: empty modified → all deletes
    if (newLines.length === 0) {
        return oldLines.map((line, i) => ({
            type: 'delete' as const,
            content: line,
            oldLineNum: i + 1,
        }));
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
