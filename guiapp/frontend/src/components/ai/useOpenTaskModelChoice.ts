/**
 * The open-task composer chip. A pin is stored on that task only.
 * An empty path leaves the global 模型分配 chip alone.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { FetchMaclawLLMProfileModels, GetTaskLLMChoice, SetTaskLLMChoice } from "../../../wailsjs/go/main/App";
import { EventsOn } from "../../../wailsjs/runtime";
import { hubOfficialModelAliases } from "../../utils/capabilityModelLabel";
import type { SidebarLLMProviderSummary } from "../../types/appShell";
import { localizeText } from "./aiAssistantI18n";
import { defaultModelForQuickProvider } from "./assistantQuickModelMenu";
import { listedModelsWithCurrent } from "./task-config/taskModelChoice";

export interface OpenTaskModelBinding {
    providers: SidebarLLMProviderSummary[];
    currentModel: string;
    modelOptions: string[];
    modelsLoading: boolean;
    label: string;
    title: string;
    followsAssignment: boolean;
    savePending: boolean;
    /** Resolves once an in-flight pin is on disk. Undefined when nothing is writing. */
    whenSaved: () => Promise<void> | undefined;
    onSwitchProvider: (providerID: string) => void;
    onSwitchModel: (modelId: string) => void;
    onOpenModelMenu: () => void;
    onDismissModelMenu: () => void;
    onFollow: () => void;
}

interface Choice {
    providerId: string;
    providerName: string;
    model: string;
    follows: boolean;
}

function choiceFromPayload(raw: any): Choice {
    return {
        providerId: String(raw?.providerId ?? raw?.ProviderID ?? "").trim(),
        providerName: String(raw?.providerName ?? raw?.ProviderName ?? "").trim(),
        model: String(raw?.model ?? raw?.Model ?? "").trim(),
        follows: raw?.followsAssignment !== false && raw?.FollowsAssignment !== false,
    };
}

function modelIdsFromCatalog(items: any): string[] {
    if (!Array.isArray(items)) return [];
    const seen = new Set<string>();
    const out: string[] = [];
    for (const item of items) {
        const id = String(item?.id ?? item?.ID ?? item?.name ?? item?.Name ?? "").trim();
        if (!id || seen.has(id)) continue;
        seen.add(id);
        out.push(id);
    }
    return out;
}

function sortProviderFirst(providers: SidebarLLMProviderSummary[], providerId: string): SidebarLLMProviderSummary[] {
    const id = String(providerId || "").trim();
    if (!id) return providers;
    const index = providers.findIndex((item) => item.id === id);
    if (index <= 0) return providers;
    const next = providers.slice();
    const [picked] = next.splice(index, 1);
    next.unshift(picked);
    return next;
}

export function useOpenTaskModelChoice(args: {
    projectPath?: string | null;
    providers: SidebarLLMProviderSummary[];
    lang: string;
    showToast?: (message: string) => void;
}): OpenTaskModelBinding | null {
    const path = String(args.projectPath || "").trim();
    const providers = args.providers;
    const lang = args.lang;
    const showToast = args.showToast;
    const [choice, setChoice] = useState<Choice | null>(null);
    const [override, setOverride] = useState<{ providerId: string; model: string } | null>(null);
    const [fetched, setFetched] = useState<Record<string, string[]>>({});
    const [loadingId, setLoadingId] = useState("");
    const [savePending, setSavePending] = useState(false);
    const seqRef = useRef(0);
    const overrideStampRef = useRef(0);
    const pinTokenRef = useRef(0);
    const pendingPinRef = useRef<{ providerId: string; token: number } | null>(null);
    const pathRef = useRef(path);
    pathRef.current = path;

    const pinOverride = useCallback((next: { providerId: string; model: string } | null) => {
        overrideStampRef.current += 1;
        setOverride(next);
    }, []);

    const reload = useCallback(async (target: string): Promise<Choice | null> => {
        if (!target || typeof GetTaskLLMChoice !== "function") return null;
        const token = ++seqRef.current;
        const stamp = overrideStampRef.current;
        try {
            const raw = await GetTaskLLMChoice(target);
            if (token !== seqRef.current || pathRef.current !== target) return null;
            const next = choiceFromPayload(raw);
            if (stamp !== overrideStampRef.current) return next;
            setChoice(next);
            setOverride(null);
            return next;
        } catch {
            if (token === seqRef.current && stamp === overrideStampRef.current) setChoice(null);
            return null;
        }
    }, []);

    useEffect(() => {
        overrideStampRef.current += 1;
        setChoice(null);
        setOverride(null);
        pendingPinRef.current = null;
        if (!path) return;
        void reload(path);
        let off: unknown;
        try {
            off = EventsOn("llm-profiles-changed", () => { void reload(path); });
        } catch {
            off = undefined;
        }
        return () => {
            if (typeof off === "function") (off as () => void)();
        };
    }, [path, reload]);

    const providerId = override?.providerId || choice?.providerId || "";
    const model = override ? override.model : (choice?.model || "");
    const follows = override ? false : (choice ? choice.follows : true);
    const sorted = useMemo(() => sortProviderFirst(providers, providerId), [providers, providerId]);
    const currentProvider = sorted.find((item) => item.id === providerId) || null;

    const modelOptions = useMemo(() => {
        if (currentProvider?.isHubService) return listedModelsWithCurrent(hubOfficialModelAliases(), model);
        return listedModelsWithCurrent(fetched[providerId] || currentProvider?.models || [], model);
    }, [currentProvider, fetched, model, providerId]);

    const writeRef = useRef<{ path: string; providerId: string; model: string } | null>(null);
    const writingRef = useRef(false);
    const writeWaitRef = useRef<{ promise: Promise<void>; resolve: () => void } | null>(null);
    const releaseWriteWaiters = useCallback(() => {
        if (writeRef.current) return;
        const pending = writeWaitRef.current;
        writeWaitRef.current = null;
        pending?.resolve();
    }, []);
    const whenSaved = useCallback((): Promise<void> | undefined => {
        if (!writingRef.current && !writeRef.current) return undefined;
        if (writeWaitRef.current) return writeWaitRef.current.promise;
        let resolve = () => {};
        const promise = new Promise<void>((done) => { resolve = done; });
        writeWaitRef.current = { promise, resolve };
        return promise;
    }, []);

    const remember = useCallback(async (target: string, nextProviderId: string, nextModel: string) => {
        if (!target || typeof SetTaskLLMChoice !== "function") return;
        writeRef.current = { path: target, providerId: nextProviderId, model: nextModel };
        if (writingRef.current) return;
        writingRef.current = true;
        setSavePending(true);
        try {
            let failed: unknown = null;
            let wrotePath = "";
            while (writeRef.current) {
                const job = writeRef.current;
                writeRef.current = null;
                try {
                    await SetTaskLLMChoice(job.path, job.providerId, job.model);
                    wrotePath = job.path;
                    failed = null;
                } catch (err) {
                    wrotePath = job.path;
                    failed = err;
                    if (writeRef.current) {
                        failed = null;
                        continue;
                    }
                    break;
                }
            }
            // The pin is on disk. A send that started after the chip changed
            // can read it now; the read-back and toast stay off that path.
            releaseWriteWaiters();
            const stamp = overrideStampRef.current;
            if (!pathRef.current || wrotePath !== pathRef.current) return;
            const saved = await reload(pathRef.current);
            if (wrotePath !== pathRef.current || writeRef.current || stamp !== overrideStampRef.current) return;
            if (failed) {
                showToast?.(localizeText(
                    lang,
                    `Could not save this task's model: ${failed}`,
                    `没能记住这个任务的模型：${failed}`,
                    `沒能記住這個任務的模型：${failed}`,
                ));
                return;
            }
            if (!saved) return;
            showToast?.(saved.follows
                ? localizeText(
                    lang,
                    "This task follows model assignment again. The next send uses it.",
                    "这个任务已改回跟随模型分配，下次发送时生效。",
                    "這個任務已改回跟隨模型分配，下次發送時生效。",
                )
                : localizeText(
                    lang,
                    "Saved for this task. The next send uses it.",
                    "已记住这个任务的模型，下次发送时生效。",
                    "已記住這個任務的模型，下次發送時生效。",
                ));
        } finally {
            writingRef.current = false;
            setSavePending(false);
            const pending = writeRef.current;
            if (pending) {
                void remember(pending.path, pending.providerId, pending.model);
            }
            if (!writingRef.current && !writeRef.current) releaseWriteWaiters();
        }
    }, [lang, releaseWriteWaiters, reload, showToast]);

    const fetchModels = useCallback(async (nextProviderId: string) => {
        const id = String(nextProviderId || "").trim();
        if (!id || typeof FetchMaclawLLMProfileModels !== "function") return [] as string[];
        const provider = providers.find((item) => item.id === id);
        if (provider?.isHubService) return hubOfficialModelAliases();
        setLoadingId(id);
        try {
            const items = await FetchMaclawLLMProfileModels(id);
            const ids = modelIdsFromCatalog(items);
            if (ids.length > 0) {
                setFetched((previous) => ({ ...previous, [id]: ids }));
            }
            return ids;
        } catch {
            return [] as string[];
        } finally {
            setLoadingId((current) => (current === id ? "" : current));
        }
    }, [providers]);

    const onSwitchProvider = useCallback((nextProviderId: string) => {
        if (!path) return;
        const provider = providers.find((item) => item.id === nextProviderId)
            ?? providers.find((item) => item.name === nextProviderId);
        if (!provider?.id) return;
        const providerId = provider.id;
        const nextModel = defaultModelForQuickProvider(provider);
        pinOverride({ providerId, model: nextModel });
        if (nextModel) {
            pendingPinRef.current = null;
            void remember(path, providerId, nextModel);
            return;
        }
        const token = ++pinTokenRef.current;
        pendingPinRef.current = { providerId, token };
        void fetchModels(providerId).then((ids) => {
            if (pendingPinRef.current?.token !== token) return;
            pendingPinRef.current = null;
            const first = ids[0] || "";
            if (!first) return;
            pinOverride({ providerId, model: first });
            void remember(path, providerId, first);
        });
    }, [fetchModels, path, pinOverride, providers, remember]);

    const onSwitchModel = useCallback((modelId: string) => {
        if (!path) return;
        const next = String(modelId || "").trim();
        const id = override?.providerId || choice?.providerId || sorted[0]?.id || "";
        if (!next || !id) return;
        pendingPinRef.current = null;
        pinOverride({ providerId: id, model: next });
        void remember(path, id, next);
    }, [choice?.providerId, override?.providerId, path, pinOverride, remember, sorted]);

    const onFollow = useCallback(() => {
        if (!path) return;
        pendingPinRef.current = null;
        pinOverride(null);
        void remember(path, "", "");
    }, [path, pinOverride, remember]);

    const onOpenModelMenu = useCallback(() => {
        const id = override?.providerId || choice?.providerId || "";
        if (id) void fetchModels(id);
    }, [choice?.providerId, fetchModels, override?.providerId]);

    const onDismissModelMenu = useCallback(() => {
        pendingPinRef.current = null;
    }, []);

    if (!path) return null;

    const providerName = currentProvider?.name || choice?.providerName || providerId;
    const pair = [providerName, model].filter(Boolean).join(" · ");
    const label = !choice && !override
        ? localizeText(lang, "Model assignment", "模型分配", "模型分配")
        : follows
            ? (pair || localizeText(lang, "Model assignment", "模型分配", "模型分配"))
            : `${localizeText(lang, "This task", "本任务", "本任務")}${pair ? ` · ${pair}` : ""}`;

    return {
        providers: sorted,
        currentModel: model,
        modelOptions,
        modelsLoading: !!providerId && loadingId === providerId,
        label,
        title: localizeText(
            lang,
            "Starts from model assignment. A change here affects only this task.",
            "默认来自模型分配。在这里改只影响这个任务。",
            "預設來自模型分配。在這裡改只影響這個任務。",
        ),
        followsAssignment: follows,
        savePending,
        whenSaved,
        onSwitchProvider,
        onSwitchModel,
        onOpenModelMenu,
        onDismissModelMenu,
        onFollow,
    };
}
