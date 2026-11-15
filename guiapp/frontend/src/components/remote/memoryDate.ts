import { intlLocale } from "../../i18n";

export function formatMemoryDate(value: string, lang: string): string {
    if (!value) return "-";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    try {
        return date.toLocaleString(intlLocale(lang));
    } catch {
        return value;
    }
}
