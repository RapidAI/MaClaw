export type SuggestOption = {
    value: string;
    label?: string;
    description?: string;
};

export function normalizeSuggestOption(option: string | SuggestOption): SuggestOption {
    return typeof option === "string" ? { value: option } : option;
}

/**
 * typedQuery is null until the user types into the field.
 * Opening the list from the button, focus, or keyboard must pass null so the
 * current value is not treated as a filter.
 */
// Opening the list selects the current text. WebView then emits an input
// event for that same text, sometimes with a trailing space or as
// insertReplacementText. Those are not typing and must not become a filter.
export function shouldIgnoreSuggestInput(next: string, value: string, inputType?: string): boolean {
    return next.trim() === value.trim() || inputType === "insertReplacementText";
}

export function filterSuggestOptions(options: readonly SuggestOption[], typedQuery: string | null): SuggestOption[] {
    if (typedQuery == null) return options.slice();
    const query = typedQuery.trim().toLowerCase();
    if (!query) return options.slice();
    return options.filter((option) => {
        const haystack = `${option.value}\n${option.label || ""}\n${option.description || ""}`.toLowerCase();
        return haystack.includes(query);
    });
}
