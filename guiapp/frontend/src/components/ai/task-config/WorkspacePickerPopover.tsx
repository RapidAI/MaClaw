import { useState } from "react";
import type { Theme } from "../aiAssistantPanelTheme";
import { TaskConfigIcon } from "./taskConfigIcons";
import { TaskConfigPopoverShell, popoverItemKeyDown, popoverSearchInputStyle } from "./TaskConfigPopoverShell";
import { popoverHeadStyle, popoverItemStyle, popoverListStyle, popoverSepStyle } from "./taskConfigPopoverStyles";
import { RemoteServerForm } from "./RemoteServerPopover";
import type { RemoteTarget, TaskType, WorkspaceTarget } from "./taskDraft";

export interface CloudWorkspaceOption {
    id: string;
    name: string;
    spec: string;
    state: string;
}

export interface WorkspacePickerPopoverProps {
    anchor: HTMLElement | null;
    theme: Theme;
    lang?: string;
    workspace: WorkspaceTarget;
    taskType: TaskType;
    recentLocalPaths?: string[];
    cloudWorkspaces?: CloudWorkspaceOption[];
    /** 选择本地目录；path 为空 = 默认工作目录。 */
    onSelectLocal: (path: string) => void;
    onSelectCloud: (id: string, name: string) => void;
    onSelectRemote: (remote: RemoteTarget) => void;
    /** 「浏览目录…」；回调可返回所选路径（则直接选中），无回调则渲染为禁用项。 */
    onBrowseLocal?: () => void | Promise<string | null | undefined>;
    /** 「新建云端工作区…」入口。 */
    onCreateCloud?: () => void;
    /** 已指定专家 → 云端/远程位置置灰（专家任务不携带工作空间，§7）。 */
    expertWorkspaceLocked?: boolean;
    onClose: () => void;
}

type Pane = 'main' | 'local' | 'cloud' | 'remote';

/**
 * 工作空间弹层：位置菜单（本地 / 云端 / 远程，行内 badge 显示当前摘要）
 * + 子面板（本地目录列表 / 云端工作区列表 / SSH 表单）。
 */
export function WorkspacePickerPopover({
    anchor,
    theme: t,
    lang,
    workspace,
    taskType,
    recentLocalPaths = [],
    cloudWorkspaces = [],
    onSelectLocal,
    onSelectCloud,
    onSelectRemote,
    onBrowseLocal,
    onCreateCloud,
    expertWorkspaceLocked = false,
    onClose,
}: WorkspacePickerPopoverProps) {
    const isZh = !lang?.startsWith("en");
    const [pane, setPane] = useState<Pane>('main');
    const [browsing, setBrowsing] = useState(false);
    const [manualPath, setManualPath] = useState("");

    const cloudDisabled = taskType === 'coding';
    const cloudHint = isZh ? "编程任务不支持云端工作区" : "Coding tasks cannot use cloud workspaces";
    // 指定专家：云端与远程整行置灰（专家任务不携带工作空间）。
    const expertWorkspaceHint = isZh ? "专家任务不携带工作空间" : "Expert tasks do not carry a workspace";
    const cloudRowDisabled = cloudDisabled || expertWorkspaceLocked;
    const cloudRowHint = expertWorkspaceLocked ? expertWorkspaceHint : cloudHint;

    const localBadge = workspace.kind === 'local'
        ? (workspace.localPath || (isZh ? "默认" : "Default"))
        : "";
    const cloudBadge = workspace.kind === 'cloud' ? (workspace.cloudName || "") : "";
    const remoteBadge = workspace.kind === 'remote' ? (workspace.remote?.host || "") : "";

    const browse = async () => {
        if (!onBrowseLocal || browsing) return;
        setBrowsing(true);
        try {
            const picked = await onBrowseLocal();
            if (picked && String(picked).trim()) onSelectLocal(String(picked).trim());
        } finally {
            setBrowsing(false);
        }
    };

    const renderRow = (
        key: string,
        icon: Parameters<typeof TaskConfigIcon>[0]["name"],
        title: string,
        desc: string,
        opts: { selected?: boolean; disabled?: boolean; disabledHint?: string; badge?: string; onPick?: () => void; testId?: string },
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
                title={opts.disabled ? opts.disabledHint : desc}
            >
                <span style={v.icon}><TaskConfigIcon name={icon} size={15} /></span>
                <span style={v.meta}>
                    {title}
                    <span style={{ ...v.desc, display: "block" }}>{desc}</span>
                </span>
                {opts.badge && <span style={v.badge}>{opts.badge}</span>}
                {opts.selected && <TaskConfigIcon name="check" size={14} />}
            </div>
        );
    };

    const backRow = (to: Pane, testId: string) => (
        <>
            <div style={popoverSepStyle(t)} />
            {renderRow("back", "back", isZh ? "返回" : "Back", "", {
                onPick: () => setPane(to),
                testId,
            })}
        </>
    );

    const head = (placeholder: string, testId: string) => (
        <div style={popoverHeadStyle()}>
            <input
                placeholder={placeholder}
                aria-label={placeholder}
                data-testid={testId}
                style={popoverSearchInputStyle(t)}
                readOnly
                tabIndex={-1}
            />
        </div>
    );

    const sep = popoverSepStyle(t);
    const localSelected = workspace.kind === 'local' && !workspace.localPath;

    return (
        <TaskConfigPopoverShell
            anchor={anchor}
            theme={t}
            onClose={onClose}
            data-testid="task-config-popover-workspace"
        >
            <div
                key={pane}
                className="mc-taskcfg-pane"
                style={{ display: "flex", flexDirection: "column", flex: 1, minHeight: 0 }}
            >
                {pane === 'main' && (
                    <>
                        {head(isZh ? "搜索工作空间" : "Search workspace", "workspace-search")}
                        <div style={popoverListStyle()}>
                            {renderRow("local", "folder", isZh ? "本地目录" : "Local folder", isZh ? "默认目录 / 最近目录 / 浏览" : "Default / recent / browse", {
                                selected: workspace.kind === 'local',
                                badge: localBadge,
                                onPick: () => setPane('local'),
                                testId: "workspace-row-local",
                            })}
                            {renderRow("cloud", "cloud", isZh ? "云端工作区" : "Cloud workspace", isZh ? "选择或新建云端工作区" : "Pick or create a cloud workspace", {
                                selected: workspace.kind === 'cloud',
                                disabled: cloudRowDisabled,
                                disabledHint: cloudRowHint,
                                badge: cloudBadge,
                                onPick: () => setPane('cloud'),
                                testId: "workspace-row-cloud",
                            })}
                            {renderRow("remote", "server", isZh ? "远程服务器" : "Remote server", isZh ? "SSH 连接，指定主机与工作目录" : "SSH connection with host and work directory", {
                                selected: workspace.kind === 'remote',
                                badge: remoteBadge,
                                disabled: expertWorkspaceLocked,
                                disabledHint: expertWorkspaceHint,
                                onPick: () => setPane('remote'),
                                testId: "workspace-row-remote",
                            })}
                        </div>
                    </>
                )}
                {pane === 'local' && (
                    <>
                        <div style={popoverHeadStyle()}>
                            <input
                                placeholder={isZh ? "搜索或输入路径" : "Search or type a path"}
                                aria-label={isZh ? "搜索或输入路径" : "Search or type a path"}
                                data-testid="workspace-local-search"
                                style={popoverSearchInputStyle(t)}
                                value={manualPath}
                                onChange={(e) => setManualPath(e.target.value)}
                                onKeyDown={(e) => {
                                    // Enter submits a typed/pasted path directly —
                                    // a deterministic fallback when the native
                                    // directory dialog is unavailable or buried.
                                    if (e.key !== "Enter") return;
                                    e.preventDefault();
                                    const path = manualPath.trim();
                                    if (path) onSelectLocal(path);
                                }}
                            />
                        </div>
                        <div style={popoverListStyle()}>
                            {manualPath.trim() && renderRow("manual", "folder", manualPath.trim(), isZh ? "按 Enter 或点击使用此路径" : "Press Enter or click to use this path", {
                                onPick: () => onSelectLocal(manualPath.trim()),
                                testId: "workspace-local-manual",
                            })}
                            {renderRow("default", "home", isZh ? "默认工作目录" : "Default working directory", isZh ? "使用当前会话默认目录" : "Use the session default directory", {
                                selected: localSelected,
                                onPick: () => onSelectLocal(""),
                                testId: "workspace-local-default",
                            })}
                            {recentLocalPaths.length > 0 && <div style={sep} />}
                            {recentLocalPaths.map((path) => renderRow(`recent-${path}`, "folder", path, isZh ? "最近使用" : "Recent", {
                                selected: workspace.kind === 'local' && workspace.localPath === path,
                                onPick: () => onSelectLocal(path),
                                testId: `workspace-local-recent-${path}`,
                            }))}
                            <div style={sep} />
                            {onBrowseLocal ? (
                                renderRow("browse", "folder", isZh ? "浏览本地目录…" : "Browse local folder…", "", {
                                    onPick: () => void browse(),
                                    testId: "workspace-local-browse",
                                })
                            ) : (
                                renderRow("browse-disabled", "folder", isZh ? "浏览本地目录…" : "Browse local folder…", isZh ? "浏览不可用" : "Browse unavailable", {
                                    disabled: true,
                                    testId: "workspace-local-browse-disabled",
                                })
                            )}
                            {backRow('main', "workspace-back-main")}
                        </div>
                    </>
                )}
                {pane === 'cloud' && (
                    <>
                        {head(isZh ? "搜索云端工作区" : "Search cloud workspaces", "workspace-cloud-search")}
                        <div style={popoverListStyle()}>
                            {cloudWorkspaces.map((cw) => renderRow(`cloud-${cw.id}`, "cloud", cw.name, `${cw.spec} · ${cw.state}`, {
                                selected: workspace.kind === 'cloud' && workspace.cloudWorkspaceId === cw.id,
                                onPick: () => onSelectCloud(cw.id, cw.name),
                                testId: `workspace-cloud-${cw.id}`,
                            }))}
                            {cloudWorkspaces.length === 0 && (
                                <div style={{ padding: "10px", fontSize: 12.5, color: t.textMuted }}>
                                    {isZh ? "暂无云端工作区" : "No cloud workspaces"}
                                </div>
                            )}
                            <div style={sep} />
                            {onCreateCloud ? (
                                renderRow("create", "plus", isZh ? "新建云端工作区…" : "New cloud workspace…", "", {
                                    onPick: onCreateCloud,
                                    testId: "workspace-cloud-create",
                                })
                            ) : (
                                renderRow("create-disabled", "plus", isZh ? "新建云端工作区…" : "New cloud workspace…", isZh ? "请在任务管理中创建" : "Create from Task Management", {
                                    disabled: true,
                                })
                            )}
                            {backRow('main', "workspace-back-cloud")}
                        </div>
                    </>
                )}
                {pane === 'remote' && (
                    <RemoteServerForm
                        theme={t}
                        lang={lang}
                        initial={workspace.remote}
                        onConfirm={onSelectRemote}
                        footerLeading={(
                            <button
                                type="button"
                                data-testid="workspace-remote-back"
                                onClick={() => setPane('main')}
                                style={{
                                    height: 30,
                                    padding: "0 12px",
                                    borderRadius: 8,
                                    border: `1px solid ${t.fieldBorder}`,
                                    background: "transparent",
                                    color: t.textMuted,
                                    fontSize: 12.5,
                                    cursor: "pointer",
                                    fontFamily: "system-ui, -apple-system, sans-serif",
                                    marginRight: "auto",
                                }}
                            >
                                ‹ {isZh ? "返回" : "Back"}
                            </button>
                        )}
                    />
                )}
            </div>
        </TaskConfigPopoverShell>
    );
}
