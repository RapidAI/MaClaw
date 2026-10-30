// After Provider management detects and saves a provider, the agent assignment
// is a separate write. These helpers decide whether to offer that switch and
// how the confirm copy is shaped so the provider name becomes the dialog subject.

export type AgentProviderSwitchTarget = {
    providerName: string;
    // Model that just passed Test & Save. The assignment directory can still
    // be showing the previous id when the user agrees, so the handoff must
    // carry this value instead of trusting that stale record.
    model?: string;
    token: number;
};

// "confirm" asks before changing the assistant provider. "focus" means the
// assistant already uses that provider but not the model that was just saved,
// so the model field should take focus without another question. "none"
// means the saved provider and model are already the assignment.
export type AgentProviderSwitchPlan = "confirm" | "focus" | "none";

type ProviderLike = { id?: string; name?: string; ID?: string; Name?: string };
type AssistantLike = {
    provider_id?: string;
    provider_name?: string;
    providerID?: string;
    providerName?: string;
    model?: string;
    Model?: string;
};
type StoredAssistant = {
    provider_id?: string;
    providerID?: string;
    ProviderID?: string;
    model?: string;
    Model?: string;
};

export type AgentAssignmentSnapshot = {
    providers?: ProviderLike[];
    assistant?: AssistantLike;
    // Persisted assignment. The effective assistant summary can already name
    // the provider that was just saved while this draft still points elsewhere.
    profiles?: { assistant?: StoredAssistant };
} | null | undefined;

type Translate = (en: string, zhHans: string, zhHant?: string) => string;

function folded(value?: string): string {
    return String(value || "").trim().toLowerCase();
}

function providerMatches(provider: ProviderLike, name: string, id: string): boolean {
    return folded(provider.name || provider.Name) === name && folded(provider.id || provider.ID) === id;
}

// The form saves profiles.assistant. Prefer that over the effective summary,
// which can follow the provider that Test & Save just made current.
function storedAssistantID(state: AgentAssignmentSnapshot): string {
    const assistant = state?.profiles?.assistant;
    return folded(assistant?.provider_id || assistant?.providerID || assistant?.ProviderID);
}

export function assistantAlreadyUsesProvider(state: AgentAssignmentSnapshot, providerName: string): boolean {
    const want = folded(providerName);
    if (!want || !state) return false;
    const storedID = storedAssistantID(state);
    if (storedID) {
        return (state.providers || []).some(provider => providerMatches(provider, want, storedID));
    }
    const assistant = state.assistant;
    const assistantName = folded(assistant?.provider_name || assistant?.providerName);
    if (assistantName && assistantName === want) return true;
    const assistantID = folded(assistant?.provider_id || assistant?.providerID);
    if (!assistantID) return false;
    return (state.providers || []).some(provider => providerMatches(provider, want, assistantID));
}

function assignedAssistantModel(state: AgentAssignmentSnapshot): string {
    const stored = state?.profiles?.assistant;
    const storedModel = String(stored?.model || stored?.Model || "").trim();
    if (storedModel) return storedModel;
    return String(state?.assistant?.model || state?.assistant?.Model || "").trim();
}

export function agentProviderSwitchPlan(state: AgentAssignmentSnapshot, providerName: string, testedModel?: string): AgentProviderSwitchPlan {
    const tested = String(testedModel || "").trim();
    if (!String(providerName || "").trim()) return "none";
    if (!state || !assistantAlreadyUsesProvider(state, providerName)) return "confirm";
    const assigned = assignedAssistantModel(state);
    if (tested && folded(assigned) !== folded(tested)) return "focus";
    return "none";
}

// The quote is the last token of the question so the confirm dialog lifts the
// provider name into its subject card. The following paragraph stays the
// instruction to pick a model and save.
export function agentProviderSwitchConfirmCopy(t: Translate, providerName: string): { title: string; message: string } {
    const name = String(providerName || "").trim();
    return {
        title: t("Switch provider", "切换服务商", "切換服務商"),
        message: t(
            `Switch the agent provider to "${name}"?\nChoose a model and save the assignment for it to take effect.`,
            `是否将 Agent 的服务商切换为「${name}」？\n请选择模型并保存配置，即可生效。`,
            `是否將 Agent 的服務商切換為「${name}」？\n請選擇模型並保存配置，即可生效。`,
        ),
    };
}
