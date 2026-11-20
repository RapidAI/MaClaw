/**
 * Per-task provider and model. An empty pair follows 模型分配 live.
 * Picking that same pair again stays a follow, so a later assignment change
 * still reaches this task.
 */
import { capabilityBandName } from "../../../utils/capabilityModelLabel";
import type { TaskDraft, TaskType } from "./taskDraft";
import { taskModelPinned } from "./taskDraft";

export interface TaskModelProvider {
    id: string;
    name: string;
    model?: string;
    models?: string[];
    isHubService?: boolean;
}

export interface TaskModelAssignment {
    providerId: string;
    providerName: string;
    model: string;
}

export interface TaskModelCatalog {
    providers: TaskModelProvider[];
    chat: TaskModelAssignment | null;
    coding: TaskModelAssignment | null;
}

export function assignmentForTaskType(catalog: TaskModelCatalog | null | undefined, taskType: TaskType): TaskModelAssignment | null {
    if (!catalog) return null;
    return taskType === "coding" ? catalog.coding : catalog.chat;
}

/** Keep the current id visible. A capability band already in the list is not repeated. */
export function listedModelsWithCurrent(models: string[] | undefined, current: string | undefined): string[] {
    const out: string[] = [];
    const add = (raw: string) => {
        const model = String(raw || "").trim();
        if (!model || out.some((item) => taskModelPairEqual("_", item, "_", model))) return;
        out.push(model);
    };
    for (const item of models || []) add(item);
    const extra = String(current || "").trim();
    if (extra && !out.some((item) => taskModelPairEqual("_", item, "_", extra))) out.unshift(extra);
    return out;
}

export function taskModelPairEqual(
    providerId: string | undefined,
    model: string | undefined,
    otherProviderId: string | undefined,
    otherModel: string | undefined,
): boolean {
    if (String(providerId || "").trim() !== String(otherProviderId || "").trim()) return false;
    const left = String(model || "").trim();
    const right = String(otherModel || "").trim();
    if (!left || !right) return false;
    if (left === right) return true;
    const leftBand = capabilityBandName(left);
    const rightBand = capabilityBandName(right);
    return !!leftBand && leftBand === rightBand;
}

export function providerNameFor(providers: TaskModelProvider[] | undefined, providerId: string | undefined): string {
    const id = String(providerId || "").trim();
    if (!id) return "";
    const found = (providers || []).find((item) => item.id === id);
    return String(found?.name || "").trim();
}

/** Chip value while the new-task bar is expanded. Remote machines stay locked. */
export function newTaskModelChipValue(
    draft: TaskDraft,
    providers: TaskModelProvider[] | undefined,
    assignment: TaskModelAssignment | null | undefined,
): { text: string; pinned: boolean; remote: boolean } {
    if (draft.machine) return { text: "", pinned: false, remote: true };
    if (taskModelPinned(draft)) {
        const name = providerNameFor(providers, draft.llmProviderId) || String(draft.llmProviderId || "").trim();
        const model = String(draft.llmModel || "").trim();
        return { text: [name, model].filter(Boolean).join(" · "), pinned: true, remote: false };
    }
    const name = String(assignment?.providerName || "").trim();
    const model = String(assignment?.model || "").trim();
    if (!name && !model) return { text: "", pinned: false, remote: false };
    return { text: [name, model].filter(Boolean).join(" · "), pinned: false, remote: false };
}

function textField(source: any, ...keys: string[]): string {
    if (!source || typeof source !== "object") return "";
    for (const key of keys) {
        const value = String(source[key] ?? "").trim();
        if (value) return value;
    }
    return "";
}

function assignmentFromSummary(source: any): TaskModelAssignment | null {
    const providerId = textField(source, "provider_id", "providerId", "ProviderID");
    const model = textField(source, "model", "Model");
    const providerName = textField(source, "provider_name", "providerName", "ProviderName");
    if (!providerId && !model && !providerName) return null;
    return { providerId, providerName, model };
}

export function taskModelCatalogFromPanel(state: any): TaskModelCatalog {
    const rows = Array.isArray(state?.providers) ? state.providers : Array.isArray(state?.Providers) ? state.Providers : [];
    const providers: TaskModelProvider[] = rows.map((provider: any) => ({
        id: textField(provider, "id", "ID"),
        name: textField(provider, "name", "Name"),
        model: textField(provider, "model", "Model") || undefined,
        models: Array.isArray(provider?.models ?? provider?.Models)
            ? (provider.models ?? provider.Models).map((item: any) => String(item || "").trim()).filter(Boolean)
            : [],
        isHubService: !!(provider?.is_hub_service ?? provider?.isHubService ?? provider?.IsHubService),
    })).filter((provider: TaskModelProvider) => !!provider.id && !!provider.name);
    return {
        providers,
        chat: assignmentFromSummary(state?.assistant ?? state?.Assistant),
        coding: assignmentFromSummary(state?.coding ?? state?.Coding),
    };
}
