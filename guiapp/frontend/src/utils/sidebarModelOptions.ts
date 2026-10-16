/**
 * Build the quick-switch model list for the sidebar provider menu.
 *
 * Preference order:
 * 1. Live-fetched catalog (when non-empty)
 * 2. Cached `models` from provider config
 * 3. Always include the configured model from LLM settings (even when fetch fails)
 */
export function buildSidebarModelOptions(input: {
    configuredModel?: string | null;
    cachedModels?: string[] | null;
    fetchedModels?: string[] | null;
}): string[] {
    const configured = String(input.configuredModel || '').trim();
    const fetched = (input.fetchedModels || []).map((m) => String(m || '').trim()).filter(Boolean);
    const cached = (input.cachedModels || []).map((m) => String(m || '').trim()).filter(Boolean);
    const primary = fetched.length > 0 ? fetched : cached;

    const seen = new Set<string>();
    const out: string[] = [];
    const add = (raw: string) => {
        const m = String(raw || '').trim();
        if (!m || seen.has(m)) return;
        seen.add(m);
        out.push(m);
    };

    // Configured model first so the active setting is always visible / selectable.
    add(configured);
    for (const m of primary) add(m);
    return out;
}
