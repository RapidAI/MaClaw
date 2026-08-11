/**
 * Small, dependency-free helpers for reviewing an optimized expert before it
 * is saved. Prompt text is compared line-by-line: expert prompts are normally
 * structured in sections, so this makes additions and removals legible without
 * losing the surrounding, unchanged instructions.
 */
export type PromptDiffKind = 'same' | 'added' | 'removed';

export interface PromptDiffLine {
    kind: PromptDiffKind;
    text: string;
}

export interface PromptDiffSummary {
    added: number;
    removed: number;
}

const MAX_LCS_MATRIX_CELLS = 250_000;

export const EXPERT_OPTIMIZATION_REVIEW_FIELDS = [
    'source_name',
    'source_system_prompt',
    'source_tools',
    'source_skills',
    'update_existing',
] as const;

const expertOptimizationReviewFieldSet = new Set<string>(EXPERT_OPTIMIZATION_REVIEW_FIELDS);

function promptLines(value: string): string[] {
    return String(value || '').replace(/\r\n?/g, '\n').split('\n');
}

function normalizePrompt(value: string): string {
    return String(value || '').replace(/\r\n?/g, '\n');
}

function sharedPrefixLength(before: string[], after: string[]): number {
    const limit = Math.min(before.length, after.length);
    let length = 0;
    while (length < limit && before[length] === after[length]) length += 1;
    return length;
}

function sharedSuffixLength(before: string[], after: string[], prefixLength: number): number {
    const limit = Math.min(before.length, after.length) - prefixLength;
    let length = 0;
    while (length < limit && before[before.length - 1 - length] === after[after.length - 1 - length]) length += 1;
    return length;
}

/** Build an LCS-backed line diff, retaining unchanged context for review. */
export function buildPromptLineDiff(source: string, optimized: string): PromptDiffLine[] {
    const normalizedSource = normalizePrompt(source);
    const normalizedOptimized = normalizePrompt(optimized);
    const before = promptLines(normalizedSource);
    if (normalizedSource === normalizedOptimized) {
        return before.map((text) => ({ kind: 'same' as const, text }));
    }
    const after = promptLines(normalizedOptimized);
    // Most expert edits touch a small section of a much longer prompt. Strip
    // matching edges before allocating the LCS matrix so a one-line edit in a
    // 1,000-line prompt remains a tiny comparison rather than a 1M-cell one.
    const prefixLength = sharedPrefixLength(before, after);
    const suffixLength = sharedSuffixLength(before, after, prefixLength);
    const beforeChanged = before.slice(prefixLength, before.length - suffixLength);
    const afterChanged = after.slice(prefixLength, after.length - suffixLength);
    const rows = beforeChanged.length;
    const cols = afterChanged.length;
    const prefix = before.slice(0, prefixLength).map((text) => ({ kind: 'same' as const, text }));
    const suffix = suffixLength === 0
        ? []
        : before.slice(before.length - suffixLength).map((text) => ({ kind: 'same' as const, text }));

    // Avoid allocating a huge matrix for an unexpectedly unstructured prompt.
    // Retaining a matching prefix/suffix still gives the reviewer context
    // without letting an uncommon wholesale rewrite block editing.
    if (rows * cols > MAX_LCS_MATRIX_CELLS) {
        return [
            ...prefix,
            ...beforeChanged.map((text) => ({ kind: 'removed' as const, text })),
            ...afterChanged.map((text) => ({ kind: 'added' as const, text })),
            ...suffix,
        ];
    }

    const lcs = Array.from({ length: rows + 1 }, () => new Uint16Array(cols + 1));
    for (let i = rows - 1; i >= 0; i -= 1) {
        for (let j = cols - 1; j >= 0; j -= 1) {
            lcs[i][j] = beforeChanged[i] === afterChanged[j]
                ? lcs[i + 1][j + 1] + 1
                : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
        }
    }

    const result: PromptDiffLine[] = [];
    let i = 0;
    let j = 0;
    while (i < rows || j < cols) {
        if (i < rows && j < cols && beforeChanged[i] === afterChanged[j]) {
            result.push({ kind: 'same', text: beforeChanged[i] });
            i += 1;
            j += 1;
        } else if (j < cols && (i === rows || lcs[i][j + 1] > lcs[i + 1][j])) {
            result.push({ kind: 'added', text: afterChanged[j] });
            j += 1;
        } else {
            result.push({ kind: 'removed', text: beforeChanged[i] });
            i += 1;
        }
    }
    return [...prefix, ...result, ...suffix];
}

export function changedNames(source: string[], optimized: string[]) {
    const before = new Set(source);
    const after = new Set(optimized);
    return {
        added: optimized.filter((name) => !before.has(name)),
        removed: source.filter((name) => !after.has(name)),
    };
}

/** Remove editor-only optimization review data before crossing a save boundary. */
export function omitOptimizationReviewFields<T extends Record<string, unknown>>(value: T): Omit<T, typeof EXPERT_OPTIMIZATION_REVIEW_FIELDS[number]> {
    return Object.fromEntries(
        Object.entries(value).filter(([key]) => !expertOptimizationReviewFieldSet.has(key)),
    ) as Omit<T, typeof EXPERT_OPTIMIZATION_REVIEW_FIELDS[number]>;
}

export function summarizePromptDiff(lines: PromptDiffLine[]): PromptDiffSummary {
    let added = 0;
    let removed = 0;
    for (const line of lines) {
        if (line.kind === 'added') added += 1;
        if (line.kind === 'removed') removed += 1;
    }
    return { added, removed };
}
