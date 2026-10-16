import { localizeText } from '../../i18n';

export type PPTStyleChoiceOption = {
    id: string;
    label: string;
    label_en?: string;
    label_hant?: string;
    summary?: string;
    summary_en?: string;
    summary_hant?: string;
    keywords?: string[];
    accent?: string;
    builtin?: boolean;
};

export function localizedStyleLabel(style: PPTStyleChoiceOption, lang: string): string {
    return localizeText(lang, style.label_en || style.label, style.label, style.label_hant || style.label);
}

export function localizedStyleSummary(style: PPTStyleChoiceOption, lang: string): string {
    return localizeText(lang, style.summary_en || style.summary || '', style.summary || '', style.summary_hant || style.summary || '');
}

export type PPTStyleChoice = {
    id: string;
    label: string;
};

const PPT_EXPERT_ID = "builtin-pptx-maker";

export function isPPTMakerExpert(expertId?: string | null): boolean {
    return String(expertId || "").trim() === PPT_EXPERT_ID;
}

/** Longest matching keyword wins, then total length, then catalog order. */
export function recommendPPTStyle(styles: PPTStyleChoiceOption[], text: string): string {
    const hint = String(text || "").toLowerCase();
    let best = styles[0]?.id || "business";
    let bestLen = 0;
    let bestScore = 0;
    styles.forEach((style) => {
        let score = 0;
        let longest = 0;
        for (const keyword of style.keywords || []) {
            const kw = String(keyword || "").trim().toLowerCase();
            if (Array.from(kw).length < 2 || !hint.includes(kw)) continue;
            const n = Array.from(kw).length;
            score += n;
            if (n > longest) longest = n;
        }
        if (score === 0) return;
        if (longest > bestLen || (longest === bestLen && score > bestScore)) {
            best = style.id;
            bestLen = longest;
            bestScore = score;
        }
    });
    return best;
}

export function withConfirmedPPTStyle(text: string, choice: PPTStyleChoice | null): string {
    const body = String(text || "");
    const trimmed = body.trim();
    if (!choice?.id || !trimmed || trimmed.startsWith("/")) return body;
    if (body.includes(`theme=${choice.id}`)) return body;
    const label = choice.label.replace(/[\r\n\t`」]/g, "").trim() || choice.id;
    return `若本条是在制作或修改演示文稿，请使用风格「${label}」（theme=${choice.id}）。若只是在询问风格或其它问题，不要生成文件。\n${body}`;
}
