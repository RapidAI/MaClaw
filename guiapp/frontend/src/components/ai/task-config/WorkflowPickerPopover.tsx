import { useMemo, useState } from "react";
import type { Theme } from "../aiAssistantPanelTheme";
import { TaskConfigIcon } from "./taskConfigIcons";
import { TaskConfigPopoverShell, popoverItemKeyDown, popoverSearchInputStyle } from "./TaskConfigPopoverShell";
import { popoverHeadStyle, popoverItemStyle, popoverListStyle, popoverSepStyle } from "./taskConfigPopoverStyles";
import { WORKFLOW_AUTO } from "./taskDraft";

export interface WorkflowOption {
    id: string;
    title: string;
    category: string;
    phaseCount: number;
    requiresWorkingDir: boolean;
    hasParamSlots: boolean;
}

export interface WorkflowPickerPopoverProps {
    anchor: HTMLElement | null;
    theme: Theme;
    lang?: string;
    workflows: WorkflowOption[];
    /** 当前已选（null = 无；'auto' = 自动判断；其余 = 模板 id）。 */
    selectedWorkflowId: string | null;
    /** 已指定专家 → 全部模板置灰（互斥，§7）。 */
    expertLocked: boolean;
    /** 「全部 N 个工作流…」入口（仅当还有更多模板时展示）。 */
    hasMoreWorkflows?: boolean;
    onShowAllWorkflows?: () => void;
    onSelect: (id: string | null) => void;
    /** 选中带参数槽的模板时触发（父级打开参数弹窗，本组件不管参数 UI）。 */
    onPickTemplateParams?: (workflow: WorkflowOption) => void;
    onClose: () => void;
}

/** 工作流选择弹层：顶部固定「自动判断」+ 模板列表（阶段数 badge）。 */
export function WorkflowPickerPopover({
    anchor,
    theme: t,
    lang,
    workflows,
    selectedWorkflowId,
    expertLocked,
    hasMoreWorkflows,
    onShowAllWorkflows,
    onSelect,
    onPickTemplateParams,
    onClose,
}: WorkflowPickerPopoverProps) {
    const isZh = !lang?.startsWith("en");
    const [query, setQuery] = useState("");
    const q = query.trim().toLowerCase();
    const filtered = useMemo(
        () => (q ? workflows.filter((w) => `${w.title} ${w.category}`.toLowerCase().includes(q)) : workflows),
        [workflows, q],
    );

    const lockHint = isZh ? "已指定专家，工作流由专家决定" : "Expert specified; workflow follows the expert";

    const pick = (workflow: WorkflowOption) => {
        onSelect(workflow.id);
        if (workflow.hasParamSlots) onPickTemplateParams?.(workflow);
    };

    const noneVisual = popoverItemStyle(t, { selected: selectedWorkflowId === null, disabled: expertLocked });
    const autoVisual = popoverItemStyle(t, { selected: selectedWorkflowId === WORKFLOW_AUTO, disabled: expertLocked });
    const itemVisual = (selected: boolean) => popoverItemStyle(t, { selected, disabled: expertLocked });
    const marketVisual = popoverItemStyle(t, {});
    const sep = popoverSepStyle(t);

    return (
        <TaskConfigPopoverShell
            anchor={anchor}
            theme={t}
            onClose={onClose}
            width={492}
            data-testid="task-config-popover-workflow"
        >
            <div style={popoverHeadStyle()}>
                <input
                    value={query}
                    onChange={(e) => setQuery(e.currentTarget.value)}
                    placeholder={isZh ? "搜索工作流模板" : "Search workflow templates"}
                    aria-label={isZh ? "搜索工作流模板" : "Search workflow templates"}
                    style={popoverSearchInputStyle(t)}
                    autoFocus
                />
            </div>
            <div style={popoverListStyle()}>
                <div
                    role={expertLocked ? undefined : "button"}
                    tabIndex={expertLocked ? undefined : 0}
                    data-testid="workflow-item-none"
                    className="mc-taskcfg-item"
                    style={noneVisual.item}
                    onClick={expertLocked ? undefined : () => onSelect(null)}
                    onKeyDown={popoverItemKeyDown(expertLocked ? undefined : () => onSelect(null))}
                    title={expertLocked ? lockHint : undefined}
                >
                    <span style={noneVisual.icon}><TaskConfigIcon name="ban" size={15} /></span>
                    <span style={noneVisual.meta}>
                        {isZh ? "无" : "None"}
                        <span style={{ ...noneVisual.desc, display: "block" }}>
                            {isZh ? "默认，不执行工作流（普通会话）" : "Default; no workflow runs (plain chat)"}
                        </span>
                    </span>
                    {selectedWorkflowId === null && <TaskConfigIcon name="check" size={14} />}
                </div>
                <div
                    role={expertLocked ? undefined : "button"}
                    tabIndex={expertLocked ? undefined : 0}
                    data-testid="workflow-item-auto"
                    className="mc-taskcfg-item"
                    style={autoVisual.item}
                    onClick={expertLocked ? undefined : () => onSelect(WORKFLOW_AUTO)}
                    onKeyDown={popoverItemKeyDown(expertLocked ? undefined : () => onSelect(WORKFLOW_AUTO))}
                    title={expertLocked ? lockHint : undefined}
                >
                    <span style={autoVisual.icon}><TaskConfigIcon name="spark" size={15} /></span>
                    <span style={autoVisual.meta}>
                        {isZh ? "自动判断" : "Auto"}
                        <span style={{ ...autoVisual.desc, display: "block" }}>
                            {isZh ? "发送后由系统识别意图并匹配工作流" : "System detects intent after send"}
                        </span>
                    </span>
                    {selectedWorkflowId === WORKFLOW_AUTO && <TaskConfigIcon name="check" size={14} />}
                </div>
                {filtered.length > 0 && <div style={sep} />}
                {filtered.map((workflow) => {
                    const v = itemVisual(selectedWorkflowId === workflow.id);
                    const desc = workflow.category
                        + (workflow.requiresWorkingDir
                            ? (isZh ? " · 需要工作目录" : " · needs working dir")
                            : "");
                    return (
                        <div
                            key={workflow.id}
                            role={expertLocked ? undefined : "button"}
                            tabIndex={expertLocked ? undefined : 0}
                            data-testid={`workflow-item-${workflow.id}`}
                            className="mc-taskcfg-item"
                            style={v.item}
                            onClick={expertLocked ? undefined : () => pick(workflow)}
                            onKeyDown={popoverItemKeyDown(expertLocked ? undefined : () => pick(workflow))}
                            title={expertLocked ? lockHint : desc}
                        >
                            <span style={v.icon}><TaskConfigIcon name="spark" size={15} /></span>
                            <span style={v.meta}>
                                {workflow.title}
                                <span style={{ ...v.desc, display: "block" }}>{desc}</span>
                            </span>
                            <span style={v.badge}>
                                {workflow.phaseCount}{isZh ? " 个阶段" : " phases"}
                            </span>
                        </div>
                    );
                })}
                {filtered.length === 0 && (
                    <div style={{ padding: "10px", fontSize: 12.5, color: t.textMuted }}>
                        {isZh ? "没有匹配的工作流" : "No matching workflows"}
                    </div>
                )}
                {hasMoreWorkflows && (
                    <>
                        <div style={sep} />
                        {onShowAllWorkflows ? (
                            <div
                                role="button"
                                tabIndex={0}
                                data-testid="workflow-item-show-all"
                                className="mc-taskcfg-item"
                                style={marketVisual.item}
                                onClick={onShowAllWorkflows}
                                onKeyDown={popoverItemKeyDown(onShowAllWorkflows)}
                            >
                                <span style={marketVisual.icon}><TaskConfigIcon name="grid" size={15} /></span>
                                <span style={marketVisual.meta}>{isZh ? "全部工作流…" : "All workflows…"}</span>
                            </div>
                        ) : (
                            <div
                                className="mc-taskcfg-item"
                                style={{ ...marketVisual.item, opacity: 0.45, cursor: "default" }}
                            >
                                <span style={marketVisual.icon}><TaskConfigIcon name="grid" size={15} /></span>
                                <span style={marketVisual.meta}>{isZh ? "全部工作流…" : "All workflows…"}</span>
                            </div>
                        )}
                    </>
                )}
            </div>
        </TaskConfigPopoverShell>
    );
}
