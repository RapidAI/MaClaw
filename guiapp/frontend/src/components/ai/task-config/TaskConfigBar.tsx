import { useCallback, useEffect, useRef, useState, type CSSProperties, type ReactNode, type RefObject } from "react";
import type { Theme } from "../aiAssistantPanelTheme";
import { DEFAULT_EXPERT_ICON } from "../expertTypes";
import { TaskConfigIcon } from "./taskConfigIcons";
import { popoverItemKeyDown, TaskConfigPopoverShell } from "./TaskConfigPopoverShell";
import {
    canSpecifyExpert,
    canSpecifyWorkflow,
    isDraftDefault,
    isExpertSpecified,
    isWorkflowChosen,
    needsLocalPath,
    withTaskType,
    withExpert,
    withLocalWorkspace,
    withCloudWorkspace,
    withRemoteWorkspace,
    withWorkflow,
    workspaceKindsForExpert,
    workspaceSummaryLabel,
    WORKFLOW_AUTO,
    type RemoteTarget,
    type TaskDraft,
    type TaskType,
} from "./taskDraft";
import type { ExpertOption } from "./ExpertPickerPopover";
import { ExpertPickerPopover } from "./ExpertPickerPopover";
import type { WorkflowOption } from "./WorkflowPickerPopover";
import { WorkflowPickerPopover } from "./WorkflowPickerPopover";
import type { CloudWorkspaceOption } from "./WorkspacePickerPopover";
import { WorkspacePickerPopover } from "./WorkspacePickerPopover";
import { WorkspaceTypeBadge } from "./WorkspaceTypeBadge";

export type { ExpertOption } from "./ExpertPickerPopover";
export type { WorkflowOption } from "./WorkflowPickerPopover";
export type { CloudWorkspaceOption } from "./WorkspacePickerPopover";

type ConfigMenu = 'type' | 'expert' | 'workflow' | 'workspace';

export interface TaskConfigBarProps {
    draft: TaskDraft;
    onChange: (draft: TaskDraft) => void;
    experts: ExpertOption[];
    workflows: WorkflowOption[];
    theme: Theme;
    lang?: string;
    disabled?: boolean;
    /** 展开信号：true 时强制展开四枚 chip（如新建任务向导页签）；只扩不折。 */
    defaultExpanded?: boolean;
    /** 本地子面板「最近目录」。 */
    recentLocalPaths?: string[];
    cloudWorkspaces?: CloudWorkspaceOption[];
    /** 「浏览目录…」；回调可返回所选路径（则直接选中）。 */
    onBrowseLocal?: () => void | Promise<string | null | undefined>;
    /** 「新建云端工作区…」入口。 */
    onCreateCloud?: () => void;
    /** 「专家市场…」入口。 */
    onOpenMarket?: () => void;
    /** 选中带参数槽的工作流模板时触发（父级打开参数弹窗）。 */
    onPickTemplateParams?: (workflow: WorkflowOption) => void;
    /** 「全部工作流…」入口（仅当还有更多模板时展示）。 */
    hasMoreWorkflows?: boolean;
    onShowAllWorkflows?: () => void;
    /**
     * 展示形态：chip（默认，圆角描边按钮）| bare（无边框轻量文字，用于
     * 输入卡片下方的配置条，视觉退居次位）。
     */
    variant?: "chip" | "bare";
}

const CHIP_HEIGHT = 30;
const CHIP_RADIUS = 15;

/**
 * 新任务引导页配置条：折叠态一枚「⚙ 默认」chip；展开态四枚 chip
 * （类型 · 专家 · 工作流 · 工作空间），点击打开对应弹层。
 * 互斥 / 类型联动全部由 taskDraft.ts 的纯函数保证，UI 只反映状态。
 */
export function TaskConfigBar({
    draft,
    onChange,
    experts,
    workflows,
    theme: t,
    lang,
    disabled,
    defaultExpanded,
    recentLocalPaths,
    cloudWorkspaces,
    onBrowseLocal,
    onCreateCloud,
    onOpenMarket,
    onPickTemplateParams,
    hasMoreWorkflows,
    onShowAllWorkflows,
    variant = "chip",
}: TaskConfigBarProps) {
    const bare = variant === "bare";
    const isZh = !lang?.startsWith("en");
    const [userExpanded, setUserExpanded] = useState(!!defaultExpanded);
    // defaultExpanded is a live signal (wizard tab marker): expand when it
    // turns true even if the bar was already mounted collapsed. Never collapses.
    useEffect(() => {
        if (defaultExpanded) setUserExpanded(true);
    }, [defaultExpanded]);
    const [menu, setMenu] = useState<ConfigMenu | null>(null);
    const focusTimer = useRef<number | null>(null);
    useEffect(() => () => {
        if (focusTimer.current !== null) window.clearTimeout(focusTimer.current);
    }, []);
    const chipRefs = {
        type: useRef<HTMLButtonElement | null>(null),
        expert: useRef<HTMLButtonElement | null>(null),
        workflow: useRef<HTMLButtonElement | null>(null),
        workspace: useRef<HTMLButtonElement | null>(null),
    } satisfies Record<ConfigMenu, RefObject<HTMLButtonElement | null>>;

    const expanded = userExpanded || !isDraftDefault(draft);
    const expertSpecified = isExpertSpecified(draft);
    const workflowLocked = !canSpecifyWorkflow(draft);
    const expertLocked = !canSpecifyExpert(draft);
    const localPathMissing = needsLocalPath(draft);
    // 专家任务不携带工作空间（§7）：指定专家时云端/远程位置锁定。
    const expertWorkspaceLocked = !workspaceKindsForExpert(draft).includes('cloud');

    const openMenu = useCallback((next: ConfigMenu) => {
        setMenu((prev) => (prev === next ? null : next));
    }, []);

    const closeMenu = useCallback((focus?: ConfigMenu) => {
        setMenu(null);
        if (focus) {
            // 选中后关闭并聚焦回配置条对应 chip。
            focusTimer.current = window.setTimeout(() => chipRefs[focus].current?.focus(), 0);
        }
    }, [chipRefs, focusTimer]);

    const selectType = (taskType: TaskType) => {
        onChange(withTaskType(draft, taskType));
        closeMenu('type');
    };

    const selectExpert = (id: string | null, name: string | null) => {
        onChange(withExpert(draft, id, name));
        closeMenu('expert');
    };

    const selectWorkflow = (id: string | null) => {
        onChange(withWorkflow(draft, id));
        closeMenu('workflow');
    };

    const handlePickTemplateParams = (workflow: WorkflowOption) => {
        closeMenu('workflow');
        onPickTemplateParams?.(workflow);
    };

    const accent = t.btnColor;

    const chipBase: CSSProperties = {
        display: "inline-flex",
        alignItems: "center",
        gap: bare ? 5 : 6,
        height: bare ? 26 : CHIP_HEIGHT,
        padding: bare ? "0 6px" : "0 12px",
        borderRadius: bare ? 8 : CHIP_RADIUS,
        border: bare ? "none" : `1px solid ${t.fieldBorder}`,
        background: bare ? "transparent" : t.bg,
        fontSize: bare ? 12.5 : 13,
        color: t.text,
        cursor: disabled ? "not-allowed" : "pointer",
        userSelect: "none",
        fontFamily: "system-ui, -apple-system, sans-serif",
        opacity: disabled ? 0.5 : 1,
        flexShrink: 0,
        transition: "border-color 120ms ease, background 120ms ease",
    };

    const chipActive: CSSProperties = bare
        ? {
            color: accent,
            fontWeight: 600,
            background: `color-mix(in srgb, ${accent} 8%, transparent)`,
        }
        : {
            border: `1px solid ${accent}`,
            background: `color-mix(in srgb, ${accent} 9%, ${t.bg})`,
            color: accent,
            fontWeight: 600,
        };

    const chipKey = (label: string): ReactNode => (
        <span style={{ color: t.textMuted, fontWeight: 400 }}>{label}</span>
    );

    const renderChip = (
        key: ConfigMenu,
        testId: string,
        keyLabel: string,
        value: ReactNode,
        options: { active?: boolean; locked?: boolean; warning?: boolean; title?: string } = {},
    ) => {
        const valueColor = options.warning ? (t.errorText || "#ef4444") : options.active ? accent : t.text;
        return (
            <button
                ref={chipRefs[key]}
                type="button"
                data-testid={testId}
                disabled={disabled}
                aria-expanded={menu === key}
                title={options.title}
                onClick={() => openMenu(key)}
                style={{
                    ...chipBase,
                    ...(options.active ? chipActive : {}),
                    ...(options.locked ? { opacity: 0.55 } : {}),
                }}
            >
                {chipKey(keyLabel)}
                <span
                    style={{
                        color: valueColor,
                        fontWeight: options.active || options.warning ? 600 : 400,
                        maxWidth: 200,
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                    }}
                >
                    {value}
                </span>
                <TaskConfigIcon name="chevronDown" size={10} style={{ color: options.active ? accent : t.textMuted }} />
            </button>
        );
    };

    const collapsedStyle: CSSProperties = bare
        ? {
            ...chipBase,
            color: t.btnColor,
        }
        : {
            ...chipBase,
            border: `1px dashed ${t.fieldBorder}`,
            background: `color-mix(in srgb, ${accent} 4%, ${t.bg})`,
            color: t.btnColor,
        };

    const workspaceLabel = workspaceSummaryLabel(draft, isZh);

    return (
        <div
            data-testid="task-config-bar"
            style={{ display: "inline-flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}
        >
            {!expanded ? (
                <button
                    ref={chipRefs.type}
                    type="button"
                    data-testid="task-config-collapsed"
                    disabled={disabled}
                    title={isZh ? "默认 — 会话 · 通用专家 · 无工作流 · 本地" : "Default — Chat · General expert · No workflow · Local"}
                    onClick={() => setUserExpanded(true)}
                    style={collapsedStyle}
                >
                    <TaskConfigIcon name="gear" size={13} />
                    {isZh ? "默认" : "Default"}
                    <TaskConfigIcon name="chevronDown" size={10} style={{ color: t.textMuted }} />
                </button>
            ) : (
                <>
                    {renderChip('type', "task-config-chip-type", isZh ? "类型" : "Type", draft.taskType === 'chat' ? (isZh ? "会话" : "Chat") : (isZh ? "编程" : "Coding"), {
                        active: draft.taskType === 'coding',
                        title: isZh ? "会话 / 编程" : "Chat / Coding",
                    })}
                    {renderChip('expert', "task-config-chip-expert", isZh ? "专家" : "Expert", (
                        <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                            {expertSpecified && (
                                <span aria-hidden="true" style={{ fontSize: 13, lineHeight: 1 }}>
                                    {experts.find(e => e.id === draft.expertId)?.icon?.trim() || DEFAULT_EXPERT_ICON}
                                </span>
                            )}
                            {draft.expertName || (isZh ? "通用专家" : "General")}
                        </span>
                    ), {
                        active: expertSpecified,
                        locked: expertLocked,
                        title: expertLocked ? (isZh ? "已选择工作流，专家保持通用" : "Workflow chosen") : undefined,
                    })}
                    {renderChip('workflow', "task-config-chip-workflow", isZh ? "工作流" : "Workflow", workflowLocked ? (isZh ? "由专家决定" : "By expert") : (draft.workflowTemplateId === null ? (isZh ? "无" : "None") : draft.workflowTemplateId === WORKFLOW_AUTO ? (isZh ? "自动判断" : "Auto") : (workflows.find((w) => w.id === draft.workflowTemplateId)?.title || draft.workflowTemplateId)), {
                        active: isWorkflowChosen(draft),
                        locked: workflowLocked,
                        title: workflowLocked ? (isZh ? "已指定专家，工作流由专家决定" : "Expert specified") : undefined,
                    })}
                    {renderChip('workspace', "task-config-chip-workspace", isZh ? "工作空间" : "Workspace", (
                        <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                            <WorkspaceTypeBadge
                                kind={draft.workspace.kind}
                                label={draft.workspace.kind === 'local'
                                    ? (isZh ? "本地" : "Local")
                                    : draft.workspace.kind === 'cloud'
                                        ? (isZh ? "云端" : "Cloud")
                                        : (isZh ? "远程" : "Remote")}
                            />
                            {workspaceLabel}
                        </span>
                    ), {
                        active: draft.workspace.kind !== 'local' || localPathMissing,
                        warning: localPathMissing,
                        title: localPathMissing
                            ? (isZh ? "编程任务需要选择本地目录" : "Coding tasks need a local directory")
                            : expertSpecified
                                ? (isZh ? "专家任务不携带工作空间" : "Expert tasks do not carry a workspace")
                                : undefined,
                    })}
                </>
            )}

            {menu === 'type' && (
                <TypePickerPopover
                    anchor={chipRefs.type.current}
                    theme={t}
                    lang={lang}
                    taskType={draft.taskType}
                    onSelect={selectType}
                    onClose={() => closeMenu('type')}
                />
            )}
            {menu === 'expert' && (
                <ExpertPickerPopover
                    anchor={chipRefs.expert.current}
                    theme={t}
                    lang={lang}
                    selectedExpertId={draft.expertId}
                    experts={experts}
                    workflowLocked={expertLocked}
                    onSelect={selectExpert}
                    onOpenMarket={onOpenMarket}
                    onClose={() => closeMenu('expert')}
                />
            )}
            {menu === 'workflow' && (
                <WorkflowPickerPopover
                    anchor={chipRefs.workflow.current}
                    theme={t}
                    lang={lang}
                    workflows={workflows}
                    selectedWorkflowId={draft.workflowTemplateId}
                    expertLocked={workflowLocked}
                    hasMoreWorkflows={hasMoreWorkflows}
                    onShowAllWorkflows={onShowAllWorkflows}
                    onSelect={selectWorkflow}
                    onPickTemplateParams={handlePickTemplateParams}
                    onClose={() => closeMenu('workflow')}
                />
            )}
            {menu === 'workspace' && (
                <WorkspacePickerPopover
                    anchor={chipRefs.workspace.current}
                    theme={t}
                    lang={lang}
                    workspace={draft.workspace}
                    taskType={draft.taskType}
                    recentLocalPaths={recentLocalPaths}
                    cloudWorkspaces={cloudWorkspaces}
                    onSelectLocal={(path) => { onChange(withLocalWorkspace(draft, path)); closeMenu('workspace'); }}
                    onSelectCloud={(id, name) => { onChange(withCloudWorkspace(draft, id, name)); closeMenu('workspace'); }}
                    onSelectRemote={(remote: RemoteTarget) => { onChange(withRemoteWorkspace(draft, remote)); closeMenu('workspace'); }}
                    onBrowseLocal={onBrowseLocal}
                    onCreateCloud={onCreateCloud}
                    expertWorkspaceLocked={expertWorkspaceLocked}
                    onClose={() => closeMenu('workspace')}
                />
            )}
        </div>
    );
}

/** 任务类型弹层（会话 / 编程）。 */
function TypePickerPopover({
    anchor,
    theme: t,
    lang,
    taskType,
    onSelect,
    onClose,
}: {
    anchor: HTMLElement | null;
    theme: Theme;
    lang?: string;
    taskType: TaskType;
    onSelect: (taskType: TaskType) => void;
    onClose: () => void;
}) {
    const isZh = !lang?.startsWith("en");
    const options: { value: TaskType; icon: "chat" | "code"; title: string; desc: string }[] = [
        { value: 'chat', icon: "chat", title: isZh ? "会话" : "Chat", desc: isZh ? "直接对话完成任务；工作空间可选本地 / 云端 / 远程" : "Conversational task; workspace can be local / cloud / remote" },
        { value: 'coding', icon: "code", title: isZh ? "编程" : "Coding", desc: isZh ? "在代码仓库中开发；工作空间可选本地 / 远程" : "Develop in a repo; workspace can be local / remote" },
    ];
    return (
        <TaskConfigPopoverShell anchor={anchor} theme={t} onClose={onClose} width={400} data-testid="task-config-popover-type">
            <div style={{ padding: "6px 8px 10px", overflowY: "auto" }}>
                {options.map((opt) => {
                const selected = taskType === opt.value;
                const item: CSSProperties = {
                    display: "flex",
                    alignItems: "center",
                    gap: 10,
                    padding: "9px 10px",
                    borderRadius: 10,
                    cursor: "pointer",
                    fontSize: 13.5,
                    color: selected ? t.btnColor : t.text,
                    fontWeight: selected ? 600 : 400,
                    background: selected ? `color-mix(in srgb, ${t.btnColor} 9%, ${t.bg})` : "transparent",
                    fontFamily: "system-ui, -apple-system, sans-serif",
                    userSelect: "none",
                };
                const iconBox: CSSProperties = {
                    width: 28,
                    height: 28,
                    borderRadius: 8,
                    background: `color-mix(in srgb, ${t.btnColor} 7%, ${t.fieldBg})`,
                    color: t.btnColor,
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    flex: "0 0 28px",
                };
                return (
                    <div
                        key={opt.value}
                        role="button"
                        tabIndex={0}
                        data-testid={`type-item-${opt.value}`}
                        className="mc-taskcfg-item"
                        style={item}
                        onClick={() => onSelect(opt.value)}
                        onKeyDown={popoverItemKeyDown(() => onSelect(opt.value))}
                    >
                        <span style={iconBox}><TaskConfigIcon name={opt.icon} size={15} /></span>
                        <span style={{ flex: 1, minWidth: 0 }}>
                            {opt.title}
                            <span style={{ display: "block", fontSize: 12, fontWeight: 400, color: selected ? t.btnColor : t.textMuted, marginTop: 1 }}>
                                {opt.desc}
                            </span>
                        </span>
                        {selected && <TaskConfigIcon name="check" size={14} />}
                    </div>
                );
            })}
            </div>
        </TaskConfigPopoverShell>
    );
}
