import type { SidebarLLMProviderSummary } from "../../types/appShell";

/**
 * Pure helpers for the bottom quick-settings model picker.
 * Kept free of React so section visibility can be unit-tested in isolation.
 */

export function resolveQuickModelList(
    modelOptions: string[] | undefined,
    currentModel: string | undefined,
): string[] {
    const seen = new Set<string>();
    const out: string[] = [];
    const add = (raw: string | undefined | null) => {
        const m = String(raw || "").trim();
        if (!m || seen.has(m)) return;
        seen.add(m);
        out.push(m);
    };

    // Configured model first (matches buildSidebarModelOptions), then catalog entries.
    add(currentModel);
    for (const m of modelOptions || []) add(m);
    return out;
}

export type QuickModelMenuSections = {
    currentProvider: SidebarLLMProviderSummary | null;
    switchableProviders: SidebarLLMProviderSummary[];
    showProviders: boolean;
    showModels: boolean;
};

/**
 * Decide which sections the bottom model menu shows.
 * - Prefer a clean model list when models are available.
 * - Only surface the provider section when there are other providers, or when
 *   there is nothing else to show (avoids an empty popover).
 */
export function resolveQuickModelMenuSections(input: {
    providers: SidebarLLMProviderSummary[];
    modelList: string[];
    currentModel?: string;
    modelsLoading?: boolean;
    hasSwitchModel?: boolean;
}): QuickModelMenuSections {
    const providers = input.providers || [];
    const currentProvider = providers[0] || null;
    const switchableProviders = providers.slice(1);
    const configured = String(input.currentModel || "").trim();
    const showModels = !!(
        input.hasSwitchModel
        && (input.modelList.length > 0 || input.modelsLoading || !!configured)
    );
    const showProviders = !!currentProvider && (switchableProviders.length > 0 || !showModels);
    return { currentProvider, switchableProviders, showProviders, showModels };
}

export function modelIdsEqual(a: string | undefined, b: string | undefined): boolean {
    return String(a || "").trim() === String(b || "").trim();
}
