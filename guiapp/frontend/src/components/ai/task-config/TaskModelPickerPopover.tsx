import { useEffect, useState } from "react";
import { FetchMaclawLLMProfileModels } from "../../../../wailsjs/go/main/App";
import { hubOfficialModelAliases } from "../../../utils/capabilityModelLabel";
import { tailText } from "../../../i18n/mnTail";
import type { Theme } from "../aiAssistantPanelTheme";
import { popoverItemKeyDown, TaskConfigPopoverShell } from "./TaskConfigPopoverShell";
import { popoverItemStyle, popoverListStyle } from "./taskConfigPopoverStyles";
import {
    listedModelsWithCurrent,
    taskModelPairEqual,
    type TaskModelAssignment,
    type TaskModelProvider,
} from "./taskModelChoice";

export function TaskModelPickerPopover({
    anchor,
    theme: t,
    lang,
    providers,
    assignment,
    providerId,
    model,
    pinned,
    onFollow,
    onPick,
    onClose,
}: {
    anchor: HTMLElement | null;
    theme: Theme;
    lang?: string;
    providers: TaskModelProvider[];
    assignment: TaskModelAssignment | null;
    providerId: string;
    model: string;
    pinned: boolean;
    onFollow: () => void;
    onPick: (providerId: string, model: string) => void;
    onClose: () => void;
}) {
    const initial = String(providerId || assignment?.providerId || providers[0]?.id || "").trim();
    const [browseId, setBrowseId] = useState(initial);
    const [models, setModels] = useState<string[]>([]);
    const [loading, setLoading] = useState(false);
    const browse = providers.find((item) => item.id === browseId) || null;
    const followSelected = !pinned;
    const browseKey = browse ? `${browse.id}\0${browse.isHubService ? 1 : 0}\0${browse.model || ""}\0${(browse.models || []).join("\0")}` : "";
    const currentOnBrowse = browse && browse.id === providerId ? model : "";

    useEffect(() => {
        if (!browse) {
            setModels([]);
            setLoading(false);
            return;
        }
        const current = browse.id === providerId ? model : "";
        if (browse.isHubService) {
            setModels(listedModelsWithCurrent(hubOfficialModelAliases(), current));
            setLoading(false);
            return;
        }
        const seeded = listedModelsWithCurrent([browse.model || "", ...(browse.models || [])], current);
        setModels(seeded);
        if (typeof FetchMaclawLLMProfileModels !== "function") return;
        let cancelled = false;
        setLoading(true);
        const providerToFetch = browse.id;
        void Promise.resolve(FetchMaclawLLMProfileModels(providerToFetch)).then((items: any) => {
            if (cancelled) return;
            const fetched = Array.isArray(items)
                ? items.map((item) => String(item?.id ?? item?.ID ?? item?.name ?? item?.Name ?? "").trim()).filter(Boolean)
                : [];
            setModels(listedModelsWithCurrent([...seeded, ...fetched], current));
        }).catch(() => {
            if (!cancelled) setModels(seeded);
        }).finally(() => {
            if (!cancelled) setLoading(false);
        });
        return () => { cancelled = true; };
    }, [browseKey, currentOnBrowse, model, providerId]);

    const pick = (nextProviderId: string, nextModel: string) => {
        if (taskModelPairEqual(nextProviderId, nextModel, assignment?.providerId, assignment?.model)) {
            onFollow();
        } else {
            onPick(nextProviderId, nextModel);
        }
        onClose();
    };

    return (
        <TaskConfigPopoverShell anchor={anchor} theme={t} onClose={onClose} width={320} data-testid="task-model-popover">
            <div style={popoverListStyle()}>
                <div
                    role="button"
                    tabIndex={0}
                    data-testid="task-model-follow"
                    onClick={() => { onFollow(); onClose(); }}
                    onKeyDown={popoverItemKeyDown(() => { onFollow(); onClose(); })}
                    style={popoverItemStyle(t, { selected: followSelected }).item}
                >
                    <span style={{ flex: 1, minWidth: 0 }}>
                        <span style={popoverItemStyle(t, { selected: followSelected }).meta}>
                            {tailText(lang ?? "", "Use model assignment", "使用模型分配")}
                        </span>
                        {(assignment?.providerName || assignment?.model) && (
                            <span style={popoverItemStyle(t, { selected: followSelected }).desc}>
                                {[assignment?.providerName, assignment?.model].filter(Boolean).join(" · ")}
                            </span>
                        )}
                    </span>
                </div>
                {providers.map((provider) => {
                    const selected = provider.id === browseId;
                    const visual = popoverItemStyle(t, { selected });
                    return (
                        <div
                            key={provider.id}
                            role="button"
                            tabIndex={0}
                            data-testid={`task-model-provider-${provider.id}`}
                            onClick={() => setBrowseId(provider.id)}
                            onKeyDown={popoverItemKeyDown(() => setBrowseId(provider.id))}
                            style={visual.item}
                        >
                            <span style={visual.meta}>{provider.name}</span>
                        </div>
                    );
                })}
                {browse && models.map((item) => {
                    const selected = pinned && browse.id === providerId && taskModelPairEqual(browse.id, item, providerId, model);
                    const visual = popoverItemStyle(t, { selected });
                    return (
                        <div
                            key={item}
                            role="button"
                            tabIndex={0}
                            data-testid={`task-model-option-${item}`}
                            onClick={() => pick(browse.id, item)}
                            onKeyDown={popoverItemKeyDown(() => pick(browse.id, item))}
                            style={{ ...visual.item, paddingLeft: 22 }}
                        >
                            <span style={visual.meta}>{item}</span>
                        </div>
                    );
                })}
                {browse && loading && models.length === 0 && (
                    <div style={popoverItemStyle(t, { disabled: true }).item}>
                        {tailText(lang ?? "", "Loading models…", "正在加载模型…")}
                    </div>
                )}
            </div>
        </TaskConfigPopoverShell>
    );
}


