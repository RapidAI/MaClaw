/**
 * Language selection helpers shared by key-based and inline i18n.
 * Kept free of product-label imports to avoid circular module graphs.
 */

import { mnInline } from './mnInline';
import { mnTailPackA } from './mnTailPackA';
import { mnTailPackB } from './mnTailPackB';
import { mnTailPackC } from './mnTailPackC';
import { mnTailPackD } from './mnTailPackD';
import { mnTailPackE } from './mnTailPackE';
import { mnTailPackF } from './mnTailPackF';

const mnPacks: Array<Record<string, string>> = [mnTailPackA, mnTailPackB, mnTailPackC, mnTailPackD, mnTailPackE, mnTailPackF];

type MnTemplate = {
    re: RegExp;
    literals: number;
    mnParts: string[];
};

let mnTemplates: MnTemplate[] | null = null;

function splitMnHoles(input: string): string[] | null {
    const parts: string[] = [];
    let literal = '';
    let holes = 0;
    for (let i = 0; i < input.length; i++) {
        if (input.startsWith('${', i)) {
            let depth = 1;
            let j = i + 2;
            while (j < input.length && depth > 0) {
                if (input.startsWith('${', j)) {
                    depth += 1;
                    j += 2;
                    continue;
                }
                if (input[j] === '}') depth -= 1;
                if (depth === 0) break;
                j += 1;
            }
            if (depth !== 0) return null;
            parts.push(literal);
            literal = '';
            holes += 1;
            i = j;
            continue;
        }
        literal += input[i];
    }
    parts.push(literal);
    return holes > 0 ? parts : null;
}

function escapeMnLiteral(value: string): string {
    return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

function compiledMnTemplates(): MnTemplate[] {
    if (mnTemplates) return mnTemplates;
    const compiled: MnTemplate[] = [];
    const sources: Array<Record<string, string>> = [mnInline, ...mnPacks];
    for (const source of sources) {
        for (const key of Object.keys(source)) {
            const parts = splitMnHoles(key);
            if (!parts) continue;
            const mnParts = splitMnHoles(source[key]);
            if (!mnParts || mnParts.length !== parts.length) continue;
            const body = parts.map(escapeMnLiteral).join('([\\s\\S]*?)');
            compiled.push({
                re: new RegExp(`^${body}$`),
                literals: parts.reduce((sum, part) => sum + part.length, 0),
                mnParts,
            });
        }
    }
    compiled.sort((a, b) => b.literals - a.literals);
    mnTemplates = compiled;
    return compiled;
}

function packExact(en: string): string | undefined {
    for (const pack of mnPacks) {
        const hit = pack[en];
        if (hit) return hit;
    }
    return undefined;
}

/** Exact Mongolian catalog hit. ${} templates are not applied. */
export function mnExactSentence(en: string): string | undefined {
    const key = en.trim();
    if (!key) return undefined;
    return mnInline[key] || packExact(key);
}

function mnFromInline(en: string): string {
    const exact = mnInline[en] || packExact(en);
    if (exact) return exact;
    let best: { score: number; out: string } | undefined;
    for (const template of compiledMnTemplates()) {
        if (best && template.literals < best.score) break;
        const match = template.re.exec(en);
        if (!match) continue;
        let out = template.mnParts[0];
        for (let i = 1; i < template.mnParts.length; i++) out += (match[i] ?? '') + template.mnParts[i];
        best = { score: template.literals, out };
        break;
    }
    return best?.out || en;
}

/**
 * Normalize a language tag to one of: 'en' | 'zh-Hans' | 'zh-Hant' | 'mn'.
 * Anything that is not Simplified Chinese, Traditional Chinese, or Cyrillic
 * Mongolian — including empty and unknown tags — is English.
 */
export function normalizeLang(lang: string | undefined | null): string {
    const raw = (lang || '').trim().toLowerCase().replace(/_/g, '-');
    if (raw === 'zh-hant' || raw === 'zh-tw' || raw === 'zh-hk' || raw === 'zh-mo' || raw.startsWith('zh-hant-') || raw.startsWith('zh-tw-') || raw.startsWith('zh-hk-') || raw.startsWith('zh-mo-')) {
        return 'zh-Hant';
    }
    if (raw === 'zh' || raw === 'zh-cn' || raw === 'zh-hans' || raw === 'zh-sg' || raw.startsWith('zh-hans-') || raw.startsWith('zh-cn-') || raw.startsWith('zh-sg-') || raw === 'chinese' || raw === 'cn') {
        return 'zh-Hans';
    }
    if (isCyrillicMongolian(raw)) return 'mn';
    return 'en';
}

function isCyrillicMongolian(raw: string): boolean {
    if (raw === 'mn' || raw === 'mn-mn' || raw === 'mn-cyrl' || raw === 'mn-cyrl-mn') return true;
    if (raw.startsWith('mn-mong')) return false;
    return raw.startsWith('mn-cyrl-') || raw.startsWith('mn-mn-');
}

/** True when the tag resolves to Simplified or Traditional Chinese. */
export function isChineseLang(lang: string | undefined | null): boolean {
    const normalized = normalizeLang(lang);
    return normalized === 'zh-Hans' || normalized === 'zh-Hant';
}

/** True when the tag resolves to Traditional Chinese. */
export function isTraditionalLang(lang: string | undefined | null): boolean {
    return normalizeLang(lang) === 'zh-Hant';
}

/** BCP-47 tag for documentElement.lang. */
export function htmlLang(lang: string | undefined | null): string {
    switch (normalizeLang(lang)) {
        case 'zh-Hans': return 'zh-CN';
        case 'zh-Hant': return 'zh-TW';
        case 'mn': return 'mn';
        default: return 'en';
    }
}

/** Locale for Intl date and number formatting. */
export function intlLocale(lang: string | undefined | null): string {
    switch (normalizeLang(lang)) {
        case 'zh-Hans': return 'zh-CN';
        case 'zh-Hant': return 'zh-TW';
        case 'mn': return 'mn-MN';
        default: return 'en-US';
    }
}

/**
 * Inline tri-lingual text selection.
 * Cyrillic Mongolian uses the English sentence as a key into mnInline.
 * Missing Mongolian copy stays English, never Chinese.
 */
export function localizeText(lang: string | undefined | null, en: string, zhHans: string, zhHant?: string): string {
    const normalized = normalizeLang(lang);
    if (normalized === 'en') return en;
    if (normalized === 'mn') return mnFromInline(en);
    if (normalized === 'zh-Hant') return zhHant || zhHans;
    return zhHans;
}
