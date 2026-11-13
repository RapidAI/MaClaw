import { useCallback, useEffect, useRef, useState, type CSSProperties, type ReactNode, type RefObject } from "react";
import type { Theme } from "../aiAssistantPanelTheme";
import { DEFAULT_EXPERT_ICON } from "../expertTypes";
import { TaskConfigIcon, type TaskConfigIconName } from "./taskConfigIcons";
import { popoverItemKeyDown, TaskConfigPopoverShell } from "./TaskConfigPopoverShell";
import {
    canSpecifyExpert,
    canSpecifyWorkflow,
    expertNeedsSetup,
    isDraftDefault,
    isExpertSpecified,
    isWorkflowChosen,
    needsLocalPath,
    withTaskType,
    withRunWindow,
    withExpert,
    withLocalWorkspace,
    withCloudWorkspace,
    withMachine,
    withRemoteWorkspace,
    withLatexTemplate,
    withWorkflow,
    workspaceKindsForExpert,
    workspaceSummaryLabel,
    WORKFLOW_AUTO,
    type MachineTarget,
    type RemoteTarget,
    type RunWindow,
    type TaskDraft,
    type TaskType,
} from "./taskDraft";
import { isRunWindowScheduled, nextHourLocalValue, runWindowChipLabel, toDatetimeLocalValue } from "./runWindow";
import { LATEX_BLANK_TEMPLATE_ID, latexBlankTemplateName, type LatexTemplate } from "../../../utils/latexTemplates";
import { LatexTemplatePickerPopover } from "./LatexTemplatePickerPopover";
import type { ExpertOption } from "./ExpertPickerPopover";
import { ExpertPickerPopover } from "./ExpertPickerPopover";
import type { WorkflowOption } from "./WorkflowPickerPopover";
import { WorkflowPickerPopover } from "./WorkflowPickerPopover";
import type { CloudWorkspaceOption } from "./WorkspacePickerPopover";
import { WorkspacePickerPopover } from "./WorkspacePickerPopover";
import { WorkspaceTypeBadge } from "./WorkspaceTypeBadge";
import { tailText } from "../../../i18n/mnTail"

export type { ExpertOption } from "./ExpertPickerPopover";
export type { WorkflowOption } from "./WorkflowPickerPopover";
export type { CloudWorkspaceOption } from "./WorkspacePickerPopover";

type ConfigMenu = 'machine' | 'type' | 'expert' | 'workflow' | 'workspace' | 'latex' | 'window';

/**
 * 提示态（编程任务缺工作目录等）值文本的柔和砖红：仍然醒目引起注意，
 * 但不像 errorText 那样刺眼（用户反馈「太扎眼」）。真正的错误仍用 errorText。
 */
const WARN_VALUE_COLOR_LIGHT = "#d05656";
const WARN_VALUE_COLOR_DARK = "#e58f8f";

/** 远控被控机器候选（远控 grant；operate=false 的机器只能查看）。 */
export interface RemoteMachineOption {
    grantId: string;
    machineName: string;
    online: boolean;
    operate: boolean;
}

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
    /** 「新建云端工作区…」确认名称后创建。 */
    onCreateCloud?: (name: string) => void | Promise<void>;
    /** 云端工作区行右侧「改名」。 */
    onRenameCloud?: (id: string, name: string) => void | Promise<void>;
    /** 「专家市场…」入口。 */
    onOpenMarket?: () => void;
    /** 选中带参数槽的工作流模板时触发（父级打开参数弹窗）。 */
    onPickTemplateParams?: (workflow: WorkflowOption) => void;
    /** 「全部工作流…」入口（仅当还有更多模板时展示）。 */
    hasMoreWorkflows?: boolean;
    onShowAllWorkflows?: () => void;
    /** Installed LaTeX templates. Blank is always offered even when this list is empty. */
    latexTemplates?: LatexTemplate[];
    /** Reload the catalogue when the template menu opens. */
    onPrepareLatexTemplates?: () => void;
    /**
     * 远控被控机器候选。非空即显示「位置」chip（本机/远程·机器名）；
     * 空或缺省 = 无远控主机，位置恒为本机且不显示该 chip。
     */
    remoteMachines?: RemoteMachineOption[];
    /**
     * 展示形态：chip（默认，圆角描边按钮）| bare（无边框轻量文字，用于
     * 输入卡片下方的配置条，视觉退居次位）。
     */
    variant?: "chip" | "bare";
}

const CHIP_HEIGHT = 30;
const CHIP_RADIUS = 15;

/** 维度名前面的图标。类型、工作流随当前值变化；工作空间用显示器，避免和种类徽标里的文件夹/云/服务器重复。 */
function configLeadIcon(key: ConfigMenu, draft: TaskDraft, workflowLocked: boolean): TaskConfigIconName {
    switch (key) {
        case 'machine':
            return 'monitor';
        case 'type':
            return draft.taskType === 'coding' ? 'code' : 'chat';
        case 'expert':
            return 'robot';
        case 'workflow':
            if (workflowLocked) return 'lock';
            return draft.workflowTemplateId === null ? 'ban' : 'spark';
        case 'latex':
            return 'file';
        case 'workspace':
            return 'monitor';
        case 'window':
            return 'clock';
    }
}

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
    onRenameCloud,
    onOpenMarket,
    onPickTemplateParams,
    hasMoreWorkflows,
    onShowAllWorkflows,
    latexTemplates,
    onPrepareLatexTemplates,
    remoteMachines,
    variant = "chip",
}: TaskConfigBarProps) {
    const bare = variant === "bare";
    const isZh = !!lang?.startsWith("zh");
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
        machine: useRef<HTMLButtonElement | null>(null),
        type: useRef<HTMLButtonElement | null>(null),
        expert: useRef<HTMLButtonElement | null>(null),
        workflow: useRef<HTMLButtonElement | null>(null),
        workspace: useRef<HTMLButtonElement | null>(null),
        latex: useRef<HTMLButtonElement | null>(null),
        window: useRef<HTMLButtonElement | null>(null),
    } satisfies Record<ConfigMenu, RefObject<HTMLButtonElement | null>>;

    const scheduledWindow = isRunWindowScheduled(draft);
    const expanded = userExpanded || !isDraftDefault(draft) || scheduledWindow;
    const expertSpecified = isExpertSpecified(draft);
    const workflowLocked = !canSpecifyWorkflow(draft);
    const expertLocked = !canSpecifyExpert(draft);
    const localPathMissing = needsLocalPath(draft);
    // 远控机器目标：专家/工作流/工作空间/定时在对方电脑上不可配——锁定。
    const remoteMachineLocked = draft.machine != null;
    const showMachineChip = remoteMachines != null && remoteMachines.length > 0;
    // 专家任务不携带工作空间（§7）：指定专家时云端/远程位置锁定。
    const expertWorkspaceLocked = !workspaceKindsForExpert(draft).includes('cloud');
    const showLatexTemplate = expertNeedsSetup(draft);
    const latexTemplateId = draft.latexTemplateId || LATEX_BLANK_TEMPLATE_ID;
    const latexTemplateLabel = (() => {
        if (!showLatexTemplate || latexTemplateId === LATEX_BLANK_TEMPLATE_ID) return latexBlankTemplateName(lang || "zh-Hans");
        return draft.latexTemplateName
            || latexTemplates?.find((template) => template.id === latexTemplateId)?.name
            || latexTemplateId;
    })();

    const openMenu = useCallback((next: ConfigMenu) => {
        setMenu((prev) => (prev === next ? null : next));
    }, []);
    // Reload once each time the menu opens. The loader's identity changes
    // when the catalogue arrives; depending on it would fetch again forever.
    const prepareLatexRef = useRef(onPrepareLatexTemplates);
    prepareLatexRef.current = onPrepareLatexTemplates;
    useEffect(() => {
        if (menu === 'latex') prepareLatexRef.current?.();
    }, [menu]);

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
    const warnValueColor = t.isDark ? WARN_VALUE_COLOR_DARK : WARN_VALUE_COLOR_LIGHT;
    const REMOTE_LOCKED_TITLE = tailText(lang ?? "", "Not configurable for a task on that computer", "远程机器上的任务不可配置此项");
    const leadIconSize = bare ? 13 : 14;
    const leadIconStyle: CSSProperties = { color: t.textMuted };

    const chipBase: CSSProperties = {
        display: "inline-flex",
        alignItems: "center",
        gap: bare ? 5 : 6,
        height: bare ? 28 : CHIP_HEIGHT,
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
        options: { active?: boolean; locked?: boolean; warning?: boolean; title?: string; noMenu?: boolean } = {},
    ) => {
        const valueColor = options.warning ? warnValueColor : options.active ? accent : t.text;
        const lead = configLeadIcon(key, draft, workflowLocked);
        return (
            <button
                ref={chipRefs[key]}
                type="button"
                data-testid={testId}
                disabled={disabled}
                aria-expanded={menu === key}
                title={options.title}
                onClick={() => { if (!options.noMenu) openMenu(key) }}
                style={{
                    ...chipBase,
                    ...(options.active ? chipActive : {}),
                    ...(options.locked ? { opacity: 0.55 } : {}),
                }}
            >
                <span data-testid={`${testId}-label`} style={{ display: "inline-flex", alignItems: "center", gap: bare ? 4 : 5 }}>
                    <TaskConfigIcon name={lead} size={leadIconSize} style={leadIconStyle} />
                    {chipKey(keyLabel)}
                </span>
                <span
                    data-testid={`${testId}-value`}
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

    const workspaceLabel = workspaceSummaryLabel(draft, (lang || (isZh ? "zh-Hans" : "en")));

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
                    title={tailText(lang ?? "", "Default — Chat · General expert · No workflow · Local", "默认 — 会话 · 通用专家 · 无工作流 · 本地")}
                    onClick={() => setUserExpanded(true)}
                    style={collapsedStyle}
                >
                    <TaskConfigIcon name="gear" size={13} />
                    {tailText(lang ?? "", "Default", "默认")}
                    <TaskConfigIcon name="chevronDown" size={10} style={{ color: t.textMuted }} />
                </button>
            ) : (
                <>
                    {showMachineChip && renderChip('machine', "task-config-chip-machine", tailText(lang ?? "", "Location", "位置"), (
                        (draft.machine?.machineName?.trim() ? draft.machine.machineName : tailText(lang ?? "", "This computer", "本机"))
                    ), {
                        active: draft.machine != null,
                        title: tailText(lang ?? "", "Which computer runs this task", "任务在哪台电脑上执行"),
                    })}
                    {renderChip('type', "task-config-chip-type", tailText(lang ?? "", "Type", "类型"), draft.taskType === 'chat' ? (tailText(lang ?? "", "Chat", "会话")) : (tailText(lang ?? "", "Coding", "编程")), {
                        active: draft.taskType === 'coding',
                        title: tailText(lang ?? "", "Chat / Coding", "会话 / 编程"),
                    })}
                    {renderChip('expert', "task-config-chip-expert", tailText(lang ?? "", "Expert", "专家"), (
                        <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                            {expertSpecified && (
                                <span aria-hidden="true" style={{ fontSize: 13, lineHeight: 1 }}>
                                    {experts.find(e => e.id === draft.expertId)?.icon?.trim() || DEFAULT_EXPERT_ICON}
                                </span>
                            )}
                            {draft.expertName || (tailText(lang ?? "", "General", "通用专家"))}
                        </span>
                    ), {
                        active: expertSpecified,
                        locked: expertLocked || remoteMachineLocked,
                        noMenu: remoteMachineLocked,
                        title: remoteMachineLocked ? (REMOTE_LOCKED_TITLE) : expertLocked ? (tailText(lang ?? "", "Workflow chosen", "已选择工作流，专家保持通用")) : undefined,
                    })}
                    {showLatexTemplate ? renderChip('latex', "task-config-chip-latex-template", tailText(lang ?? "", "Template", "模板"), latexTemplateLabel, {
                        active: latexTemplateId !== LATEX_BLANK_TEMPLATE_ID,
                        title: tailText(lang ?? "", "Blank template by default. Pick an installed LaTeX template.", "默认空白模板，可改选已安装的 LaTeX 模板"),
                    }) : renderChip('workflow', "task-config-chip-workflow", tailText(lang ?? "", "Workflow", "工作流"), workflowLocked ? (tailText(lang ?? "", "By expert", "由专家决定")) : (draft.workflowTemplateId === null ? (tailText(lang ?? "", "None", "无")) : draft.workflowTemplateId === WORKFLOW_AUTO ? (tailText(lang ?? "", "Auto", "自动判断")) : (workflows.find((w) => w.id === draft.workflowTemplateId)?.title || draft.workflowTemplateId)), {
                        active: isWorkflowChosen(draft),
                        locked: workflowLocked || remoteMachineLocked,
                        noMenu: remoteMachineLocked,
                        title: remoteMachineLocked ? (REMOTE_LOCKED_TITLE) : workflowLocked ? (tailText(lang ?? "", "Expert specified", "已指定专家，工作流由专家决定")) : undefined,
                    })}
                    {renderChip('workspace', "task-config-chip-workspace", tailText(lang ?? "", "Workspace", "工作空间"), remoteMachineLocked ? (
                        tailText(lang ?? "", "Default on that computer", "对方电脑默认")
                    ) : (
                        <span style={{ display: "inline-flex", alignItems: "center", gap: 5, minWidth: 0, maxWidth: "100%" }}>
                            <WorkspaceTypeBadge
                                kind={draft.workspace.kind}
                                label={draft.workspace.kind === 'local'
                                    ? (tailText(lang ?? "", "Local", "本地"))
                                    : draft.workspace.kind === 'cloud'
                                        ? (tailText(lang ?? "", "Cloud", "云端"))
                                        : (tailText(lang ?? "", "Remote", "远程"))}
                            />
                            {draft.workspace.kind === 'local' && !draft.workspace.localPath && !localPathMissing && (
                                <TaskConfigIcon name="home" size={leadIconSize} style={leadIconStyle} />
                            )}
                            <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{workspaceLabel}</span>
                        </span>
                    ), {
                        active: !remoteMachineLocked && (draft.workspace.kind !== 'local' || localPathMissing),
                        warning: !remoteMachineLocked && localPathMissing,
                        locked: remoteMachineLocked,
                        noMenu: remoteMachineLocked,
                        title: remoteMachineLocked
                            ? (REMOTE_LOCKED_TITLE)
                            : localPathMissing
                                ? (tailText(lang ?? "", "Coding tasks need a local directory", "编程任务需要选择本地目录"))
                                : expertSpecified
                                    ? (tailText(lang ?? "", "Choose a local working directory", "可选择本地工作目录"))
                                    : undefined,
                    })}
                    {renderChip('window', "task-config-chip-window", tailText(lang ?? "", "Window", "运行时段"), runWindowChipLabel(draft.runWindow, (lang || (isZh ? "zh-Hans" : "en"))), {
                        active: scheduledWindow,
                        locked: remoteMachineLocked,
                        noMenu: remoteMachineLocked,
                        title: remoteMachineLocked ? (REMOTE_LOCKED_TITLE) : tailText(lang ?? "", "Run now, or set a start and optional end", "立即执行，或设定开始和结束时间"),
                    })}
                </>
            )}

            {menu === 'machine' && remoteMachines != null && (
                <MachinePickerPopover
                    anchor={chipRefs.machine.current}
                    theme={t}
                    lang={lang}
                    machines={remoteMachines}
                    current={draft.machine}
                    onSelect={(target) => { onChange(withMachine(draft, target)); closeMenu('machine'); }}
                    onClose={() => closeMenu('machine')}
                />
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
            {menu === 'latex' && showLatexTemplate && (
                <LatexTemplatePickerPopover
                    anchor={chipRefs.latex.current}
                    theme={t}
                    lang={lang}
                    templates={latexTemplates || []}
                    selectedTemplateId={latexTemplateId}
                    onSelect={(id, name) => { onChange(withLatexTemplate(draft, id, name)); closeMenu('latex'); }}
                    onClose={() => closeMenu('latex')}
                />
            )}
            {menu === 'workflow' && !showLatexTemplate && (
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
                    onRenameCloud={onRenameCloud}
                    expertWorkspaceLocked={expertWorkspaceLocked}
                    onClose={() => closeMenu('workspace')}
                />
            )}
            {menu === 'window' && (
                <RunWindowPopover
                    anchor={chipRefs.window.current}
                    theme={t}
                    lang={lang}
                    runWindow={draft.runWindow}
                    onChange={(next) => onChange(withRunWindow(draft, next))}
                    onClose={() => closeMenu('window')}
                />
            )}
        </div>
    );
}

function openDatetimePicker(input: HTMLInputElement | null) {
    if (!input) return;
    try {
        if (typeof input.showPicker === "function") {
            input.showPicker();
            return;
        }
    } catch {
        // WebView2 rejects showPicker outside a direct gesture.
    }
    input.focus({ preventScroll: true });
}

function RunWindowDatetimeField({
    label,
    testId,
    pickerLabel,
    value,
    min,
    theme: t,
    onValue,
}: {
    label: string;
    testId: string;
    pickerLabel: string;
    value: string;
    min?: string;
    theme: Theme;
    onValue: (value: string) => void;
}) {
    const inputRef = useRef<HTMLInputElement | null>(null);
    const field: CSSProperties = {
        width: "100%",
        boxSizing: "border-box",
        height: 32,
        borderRadius: 8,
        border: `1px solid ${t.fieldBorder}`,
        background: t.fieldBg,
        color: t.text,
        padding: "0 36px 0 8px",
        fontSize: 13,
        fontFamily: "system-ui, -apple-system, sans-serif",
        colorScheme: t.isDark ? "dark" : "light",
    };
    return (
        <div>
            <label htmlFor={testId} style={{ display: "block", fontSize: 12, color: t.textMuted, marginBottom: 4 }}>{label}</label>
            <span className="mc-taskcfg-datetime-wrap">
                <input
                    ref={inputRef}
                    id={testId}
                    className="mc-taskcfg-datetime"
                    data-testid={testId}
                    type="datetime-local"
                    value={value}
                    min={min}
                    onChange={(event) => onValue(event.target.value)}
                    style={field}
                />
                <button
                    type="button"
                    className="mc-taskcfg-datetime-btn"
                    data-testid={`${testId}-picker`}
                    aria-label={pickerLabel}
                    style={{ color: t.text }}
                    onMouseDown={(event) => event.preventDefault()}
                    onClick={() => openDatetimePicker(inputRef.current)}
                >
                    <TaskConfigIcon name="calendar" size={15} />
                </button>
            </span>
        </div>
    );
}

function RunWindowPopover({
    anchor,
    theme: t,
    lang,
    runWindow,
    onChange,
    onClose,
}: {
    anchor: HTMLElement | null;
    theme: Theme;
    lang?: string;
    runWindow?: RunWindow;
    onChange: (next: RunWindow) => void;
    onClose: () => void;
}) {
    const scheduled = runWindow?.mode === "scheduled";
    const startValue = runWindow?.startAt || "";
    const endValue = runWindow?.endAt || "";
    const chooseImmediate = () => {
        onChange({ mode: "immediate", startAt: "", endAt: "" });
        onClose();
    };
    const chooseScheduled = () => {
        onChange({
            mode: "scheduled",
            startAt: startValue || nextHourLocalValue(),
            endAt: endValue,
        });
    };
    return (
        <TaskConfigPopoverShell anchor={anchor} theme={t} onClose={onClose} width={360} data-testid="task-config-popover-window">
            <div style={{ padding: "8px 10px 12px" }}>
                <div
                    role="button"
                    tabIndex={0}
                    data-testid="run-window-immediate"
                    className="mc-taskcfg-item"
                    onClick={chooseImmediate}
                    onKeyDown={popoverItemKeyDown(chooseImmediate)}
                    style={{
                        padding: "8px 10px",
                        borderRadius: 10,
                        cursor: "pointer",
                        fontSize: 13.5,
                        fontWeight: scheduled ? 400 : 600,
                        color: scheduled ? t.text : t.btnColor,
                        background: scheduled ? "transparent" : `color-mix(in srgb, ${t.btnColor} 9%, ${t.bg})`,
                    }}
                >
                    {tailText(lang ?? "", "Now", "立即")}
                    <span style={{ display: "block", fontSize: 12, fontWeight: 400, color: t.textMuted, marginTop: 2 }}>
                        {tailText(lang ?? "", "Start as soon as the task is created", "创建后马上执行")}
                    </span>
                </div>
                <div
                    role="button"
                    tabIndex={0}
                    data-testid="run-window-scheduled"
                    className="mc-taskcfg-item"
                    onClick={chooseScheduled}
                    onKeyDown={popoverItemKeyDown(chooseScheduled)}
                    style={{
                        padding: "8px 10px",
                        borderRadius: 10,
                        cursor: "pointer",
                        fontSize: 13.5,
                        fontWeight: scheduled ? 600 : 400,
                        color: scheduled ? t.btnColor : t.text,
                        background: scheduled ? `color-mix(in srgb, ${t.btnColor} 9%, ${t.bg})` : "transparent",
                    }}
                >
                    {tailText(lang ?? "", "Scheduled", "定时")}
                    <span style={{ display: "block", fontSize: 12, fontWeight: 400, color: t.textMuted, marginTop: 2 }}>
                        {tailText(lang ?? "", "Wait until the start time. Leave the end empty to run until the work finishes", "到开始时间再执行。结束时间可不填，不填则任务完成即结束")}
                    </span>
                </div>
                {scheduled && (
                    <div style={{ display: "grid", gap: 10, padding: "8px 10px 0" }}>
                        <RunWindowDatetimeField
                            label={tailText(lang ?? "", "Start", "开始")}
                            testId="run-window-start"
                            pickerLabel={tailText(lang ?? "", "Choose start time", "选择开始时间")}
                            value={startValue}
                            min={toDatetimeLocalValue(new Date())}
                            theme={t}
                            onValue={(startAt) => onChange({ mode: "scheduled", startAt, endAt: endValue })}
                        />
                        <RunWindowDatetimeField
                            label={tailText(lang ?? "", "End (optional)", "结束（可选）")}
                            testId="run-window-end"
                            pickerLabel={tailText(lang ?? "", "Choose end time", "选择结束时间")}
                            value={endValue}
                            min={startValue || undefined}
                            theme={t}
                            onValue={(endAt) => onChange({ mode: "scheduled", startAt: startValue, endAt })}
                        />
                    </div>
                )}
            </div>
        </TaskConfigPopoverShell>
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
    const options: { value: TaskType; icon: "chat" | "code"; title: string; desc: string }[] = [
        { value: 'chat', icon: "chat", title: tailText(lang ?? "", "Chat", "会话"), desc: tailText(lang ?? "", "Conversational task; workspace can be local / cloud / remote", "直接对话完成任务；工作空间可选本地 / 云端 / 远程") },
        { value: 'coding', icon: "code", title: tailText(lang ?? "", "Coding", "编程"), desc: tailText(lang ?? "", "Develop in a repo; workspace can be local / remote", "在代码仓库中开发；工作空间可选本地 / 远程") },
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


/** 位置弹层：本机 + 各远控被控机器（离线/仅查看不可选）。 */
function MachinePickerPopover({
    anchor,
    theme: t,
    lang,
    machines,
    current,
    onSelect,
    onClose,
}: {
    anchor: HTMLElement | null;
    theme: Theme;
    lang?: string;
    machines: RemoteMachineOption[];
    current?: MachineTarget | null;
    onSelect: (target: MachineTarget | null) => void;
    onClose: () => void;
}) {
    const isZh = !!lang?.startsWith("zh");
    const localSelected = current == null;
    const row = (
        testId: string,
        icon: TaskConfigIconName,
        title: string,
        desc: string,
        selected: boolean,
        enabled: boolean,
        onPick: () => void,
    ) => {
        const item: CSSProperties = {
            display: "flex",
            alignItems: "center",
            gap: 10,
            padding: "9px 10px",
            borderRadius: 10,
            cursor: enabled ? "pointer" : "not-allowed",
            opacity: enabled ? 1 : 0.5,
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
                key={testId}
                role="button"
                tabIndex={enabled ? 0 : -1}
                data-testid={testId}
                className="mc-taskcfg-item"
                style={item}
                onClick={() => { if (enabled) onPick(); }}
                onKeyDown={enabled ? popoverItemKeyDown(onPick) : undefined}
            >
                <span style={iconBox}><TaskConfigIcon name={icon} size={15} /></span>
                <span style={{ flex: 1, minWidth: 0 }}>
                    {title}
                    <span style={{ display: "block", fontSize: 12, fontWeight: 400, color: selected ? t.btnColor : t.textMuted, marginTop: 1 }}>
                        {desc}
                    </span>
                </span>
                {selected && <TaskConfigIcon name="check" size={14} />}
            </div>
        );
    };
    return (
        <TaskConfigPopoverShell anchor={anchor} theme={t} onClose={onClose} width={400} data-testid="task-config-popover-machine">
            <div style={{ padding: "6px 8px 10px", overflowY: "auto" }}>
                {row(
                    "machine-item-local",
                    "home",
                    tailText(lang ?? "", "This computer", "本机"),
                    tailText(lang ?? "", "Create and run the task here", "在本机创建并执行任务"),
                    localSelected,
                    true,
                    () => onSelect(null),
                )}
                {machines.map((m) => {
                    const enabled = m.online && m.operate;
                    const desc = !m.online
                        ? tailText(lang ?? "", "Offline", "离线")
                        : !m.operate
                            ? tailText(lang ?? "", "View only", "仅查看")
                            : tailText(lang ?? "", "Create and run on this computer", "在对方电脑上创建并执行");
                    return row(
                        `machine-item-${m.grantId}`,
                        "monitor",
                        m.machineName || (isZh ? "远程电脑" : "Remote computer"),
                        desc,
                        current?.grantId === m.grantId,
                        enabled,
                        () => onSelect({ grantId: m.grantId, machineName: m.machineName }),
                    );
                })}
            </div>
        </TaskConfigPopoverShell>
    );
}
