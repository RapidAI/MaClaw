/** Parse and fill welcome-scenario prompt templates that use [placeholder] slots. */

export type WelcomeTemplateField = {
    /** Stable key for form state (f0, f1, …). */
    id: string;
    /** Human label taken from the same line before "：" / ":". */
    label: string;
    /** Hint text inside the brackets. */
    hint: string;
    /** Inclusive start index of `[hint]` in the template. */
    start: number;
    /** Exclusive end index of `[hint]` in the template. */
    end: number;
    /** Prefer a multi-line control for paste-heavy fields. */
    multiline: boolean;
    /**
     * Quick-pick options derived from the hint when it looks like
     * "正式 / 简洁 / 强硬" or "测试/预发/生产".
     */
    chips: string[];
};

const MULTILINE_HINT_RE =
    /粘贴|拖入|文件|路径|日志|材料|正文|内容|要点|意见|记录|转写|数据|文档|简历|指南|paste|attach|drag|file|path|log|note|material|content|transcript|document|csv|excel|pdf|json/i;

/**
 * Split a placeholder hint into chip options when it enumerates short choices.
 * Returns [] when the hint is free-form prose rather than a choice list.
 */
export function extractHintChips(hint: string): string[] {
    const raw = (hint || "").trim();
    if (!raw || raw.length > 80) return [];
    // Prefer explicit separators: / · 、 |  or spaced slash " / "
    const parts = raw
        .split(/\s*[|·、]\s*|(?:\s*\/\s*)/)
        .map((p) => p.trim())
        .filter(Boolean);
    if (parts.length < 2 || parts.length > 8) return [];
    if (parts.some((p) => p.length === 0 || p.length > 20)) return [];
    // Avoid splitting narrative phrases with sentence punctuation or too many spaces.
    if (parts.some((p) => /[。；;!?？]/.test(p) || (p.match(/\s/g) || []).length >= 3)) {
        return [];
    }
    // Skip example prose like "e.g. SaaS" / "例如 SaaS 协作".
    if (parts.some((p) => /^(e\.g\.|eg\.|例如|比如|如)/i.test(p))) {
        return [];
    }
    // Dedup while preserving order.
    const seen = new Set<string>();
    const chips: string[] = [];
    for (const p of parts) {
        if (seen.has(p)) continue;
        seen.add(p);
        chips.push(p);
    }
    return chips.length >= 2 ? chips : [];
}

/**
 * Extract ordered [placeholder] fields from a welcome prompt template.
 * Labels come from the same line when written as `标签：[提示]`.
 */
export function extractWelcomeTemplateFields(template: string): WelcomeTemplateField[] {
    if (!template) return [];
    const fields: WelcomeTemplateField[] = [];
    const re = /\[([^\[\]]+)\]/g;
    let match: RegExpExecArray | null;
    let index = 0;

    while ((match = re.exec(template)) !== null) {
        const hint = match[1].trim();
        if (!hint) continue;

        const lineStart = template.lastIndexOf("\n", match.index - 1) + 1;
        const nextNl = template.indexOf("\n", match.index);
        const lineEnd = nextNl === -1 ? template.length : nextNl;
        const line = template.slice(lineStart, lineEnd);
        const localIdx = match.index - lineStart;
        const before = line.slice(0, localIdx);

        // Only treat "标签：" / "Label:" as a field label. Bare prose before [hint]
        // (e.g. "Please cover [scope]") must not become the form label.
        let label = "";
        const labeled = before.match(/^(.*?)\s*[：:]\s*$/);
        if (labeled) {
            label = labeled[1].trim();
        }
        if (!label) {
            // Prefer the hint as the visible label; fall back to a language-neutral Field N.
            label = hint.length > 0 && hint.length <= 28 ? hint : `Field ${index + 1}`;
        }

        const multiline = MULTILINE_HINT_RE.test(hint) || MULTILINE_HINT_RE.test(label) || hint.length > 24;
        fields.push({
            id: `f${index}`,
            label,
            hint,
            start: match.index,
            end: match.index + match[0].length,
            multiline,
            // Multiline paste fields rarely need chips; keep UI quiet.
            chips: multiline ? [] : extractHintChips(hint),
        });
        index += 1;
    }

    return fields;
}

/**
 * Replace each field's [placeholder] with the user value.
 * Empty values become emptyToken (default empty string).
 * Replaces from the end so earlier offsets stay valid.
 */
export function fillWelcomeTemplate(
    template: string,
    fields: WelcomeTemplateField[],
    values: Record<string, string>,
    emptyToken = "",
): string {
    if (!template || fields.length === 0) return template;
    let result = template;
    for (let i = fields.length - 1; i >= 0; i--) {
        const field = fields[i];
        const raw = (values[field.id] ?? "").trim();
        const replacement = raw.length > 0 ? raw : emptyToken;
        result = result.slice(0, field.start) + replacement + result.slice(field.end);
    }
    return result.replace(/[ \t]+\n/g, "\n").replace(/\n{3,}/g, "\n\n").trim();
}

/** True when the template has at least one fillable [slot]. */
export function welcomeTemplateNeedsParams(template: string): boolean {
    return extractWelcomeTemplateFields(template).length > 0;
}
