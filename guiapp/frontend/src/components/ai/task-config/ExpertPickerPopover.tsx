import { useMemo, useState, type ReactNode } from "react";
import type { Theme } from "../aiAssistantPanelTheme";
import { DEFAULT_EXPERT_ICON } from "../expertTypes";
import { TaskConfigIcon } from "./taskConfigIcons";
import { TaskConfigPopoverShell, popoverItemKeyDown, popoverSearchInputStyle } from "./TaskConfigPopoverShell";
import { popoverHeadStyle, popoverItemStyle, popoverListStyle, popoverSepStyle } from "./taskConfigPopoverStyles";
import { tailText } from "../../../i18n/mnTail"

export interface ExpertOption {
    id: string;
    name: string;
    description?: string;
    /** Emoji icon (same convention as ExpertDefinition.icon); empty = robot fallback. */
    icon?: string;
}

export interface ExpertPickerPopoverProps {
    anchor: HTMLElement | null;
    theme: Theme;
    lang?: string;
    /** 当前已选专家（null = 通用专家）。 */
    selectedExpertId: string | null;
    experts: ExpertOption[];
    /** 已指定工作流 → 非通用专家全部置灰（互斥，§7）。 */
    workflowLocked: boolean;
    onSelect: (id: string | null, name: string | null) => void;
    /** 「专家市场…」入口；缺省时渲染为禁用地说明项。 */
    onOpenMarket?: () => void;
    onClose: () => void;
}

/** 专家选择弹层：顶部固定「通用专家」+ 专家列表 + 专家市场入口。 */
export function ExpertPickerPopover({
    anchor,
    theme: t,
    lang,
    selectedExpertId,
    experts,
    workflowLocked,
    onSelect,
    onOpenMarket,
    onClose,
}: ExpertPickerPopoverProps) {
    const [query, setQuery] = useState("");
    const q = query.trim().toLowerCase();
    const filtered = useMemo(
        () => (q ? experts.filter((e) => `${e.name} ${e.description || ""}`.toLowerCase().includes(q)) : experts),
        [experts, q],
    );

    const renderItem = (
        key: string,
        icon: ReactNode,
        title: string,
        desc: string,
        opts: { selected?: boolean; disabled?: boolean; onPick?: () => void; testId?: string },
    ) => {
        const v = popoverItemStyle(t, { selected: opts.selected, disabled: opts.disabled });
        return (
            <div
                key={key}
                role={opts.disabled ? undefined : "button"}
                tabIndex={opts.disabled ? undefined : 0}
                data-testid={opts.testId}
                className="mc-taskcfg-item"
                style={v.item}
                onClick={opts.disabled ? undefined : opts.onPick}
                onKeyDown={popoverItemKeyDown(opts.disabled ? undefined : opts.onPick)}
                title={opts.disabled ? (tailText(lang ?? "", "Workflow specified", "已指定工作流，不能再指定专家")) : desc}
            >
                <span style={v.icon}>{icon}</span>
                <span style={v.meta}>
                    {title}
                    <span style={{ ...v.desc, display: "block" }}>{desc}</span>
                </span>
                {opts.selected && <TaskConfigIcon name="check" size={14} />}
            </div>
        );
    };

    return (
        <TaskConfigPopoverShell
            anchor={anchor}
            theme={t}
            onClose={onClose}
            data-testid="task-config-popover-expert"
        >
            <div style={popoverHeadStyle()}>
                <input
                    value={query}
                    onChange={(e) => setQuery(e.currentTarget.value)}
                    placeholder={tailText(lang ?? "", "Search experts", "搜索专家（名称 / 领域）")}
                    aria-label={tailText(lang ?? "", "Search experts", "搜索专家")}
                    style={popoverSearchInputStyle(t)}
                    autoFocus
                />
            </div>
            <div style={popoverListStyle()}>
                {renderItem(
                    "generic",
                    <TaskConfigIcon name="robot" size={15} />,
                    tailText(lang ?? "", "General expert", "通用专家"),
                    tailText(lang ?? "", "Default; general system capability", "默认；由系统通用能力处理，不指定领域专家"),
                    {
                        selected: !selectedExpertId,
                        onPick: () => onSelect(null, null),
                        testId: "expert-item-generic",
                    },
                )}
                {filtered.length > 0 && <div style={popoverSepStyle(t)} />}
                {filtered.map((expert) => renderItem(
                    expert.id,
                    <span aria-hidden="true" style={{ fontSize: 16, lineHeight: 1 }}>{expert.icon?.trim() || DEFAULT_EXPERT_ICON}</span>,
                    expert.name,
                    expert.description || (tailText(lang ?? "", "Expert", "领域专家")),
                    {
                        selected: selectedExpertId === expert.id,
                        disabled: workflowLocked,
                        onPick: () => onSelect(expert.id, expert.name),
                        testId: `expert-item-${expert.id}`,
                    },
                ))}
                {filtered.length === 0 && (
                    <div style={{ padding: "10px", fontSize: 12.5, color: t.textMuted }}>
                        {tailText(lang ?? "", "No matching experts", "没有匹配的专家")}
                    </div>
                )}
                <div style={popoverSepStyle(t)} />
                {onOpenMarket ? (
                    renderItem(
                        "market",
                        <TaskConfigIcon name="grid" size={15} />,
                        tailText(lang ?? "", "Expert market…", "专家市场…"),
                        tailText(lang ?? "", "Browse and install more experts", "浏览并安装更多专家"),
                        { onPick: onOpenMarket, testId: "expert-item-market" },
                    )
                ) : (
                    renderItem(
                        "market-disabled",
                        <TaskConfigIcon name="lock" size={14} />,
                        tailText(lang ?? "", "Expert market…", "专家市场…"),
                        tailText(lang ?? "", "Open Expert Management to browse", "请在「专家管理」中打开专家市场"),
                        { disabled: true },
                    )
                )}
            </div>
        </TaskConfigPopoverShell>
    );
}
