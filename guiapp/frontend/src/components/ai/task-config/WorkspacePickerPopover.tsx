import { useCallback, useEffect, useRef, useState, type CSSProperties, type KeyboardEvent as ReactKeyboardEvent } from "react";
import type { Theme } from "../aiAssistantPanelTheme";
import { extractErrorMessage } from "../participantAddError";
import { TaskConfigIcon } from "./taskConfigIcons";
import { TaskConfigPopoverShell, popoverItemKeyDown, popoverSearchInputStyle } from "./TaskConfigPopoverShell";
import { popoverHeadStyle, popoverItemStyle, popoverListStyle, popoverSepStyle } from "./taskConfigPopoverStyles";
import { RemoteServerForm } from "./RemoteServerPopover";
import type { RemoteTarget, TaskType, WorkspaceTarget } from "./taskDraft";

/** Enter while an IME candidate is open must not create or rename. */
function imeComposing(event: ReactKeyboardEvent): boolean {
    return event.nativeEvent.isComposing || event.keyCode === 229;
}

/**
 * Hub stores the series as "工作区 1". Also treat "工作区1" as that number so
 * the suggestion does not sit next to a near-duplicate.
 */
const defaultCloudWorkspaceName = /^工作区\s*([1-9][0-9]*)$/;

export function nextDefaultCloudWorkspaceName(names: string[]): string {
    const used = new Set<number>();
    for (const name of names) {
        const match = defaultCloudWorkspaceName.exec(name.trim());
        if (!match) continue;
        const n = Number(match[1]);
        if (Number.isInteger(n) && n >= 1) used.add(n);
    }
    let i = 1;
    while (used.has(i)) i += 1;
    return `工作区 ${i}`;
}

function cloudWorkspaceNameTaken(err: unknown): boolean {
    const text = extractErrorMessage(err);
    return text.includes("名称已存在") || /name taken/i.test(text);
}

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
    /** 「新建云端工作区…」确认名称后创建。名称为空时不调用。 */
    onCreateCloud?: (name: string) => void | Promise<void>;
    /** 云端工作区行右侧「改名」。 */
    onRenameCloud?: (id: string, name: string) => void | Promise<void>;
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
    onRenameCloud,
    expertWorkspaceLocked = false,
    onClose,
}: WorkspacePickerPopoverProps) {
    const isZh = !lang?.startsWith("en");
    const [pane, setPane] = useState<Pane>('main');
    const [browsing, setBrowsing] = useState(false);
    const [manualPath, setManualPath] = useState("");
    /** null = the create row is the button; a string is the unnamed draft being edited. */
    const [creatingName, setCreatingName] = useState<string | null>(null);
    const [renaming, setRenaming] = useState<{ id: string; value: string } | null>(null);
    const [cloudBusy, setCloudBusy] = useState(false);
    const [cloudError, setCloudError] = useState("");
    const cloudBusyRef = useRef(false);
    const createNameRef = useRef<HTMLInputElement | null>(null);
    /** Default names already rejected in this editor, so a stale list cannot offer them again. */
    const rejectedDefaultNamesRef = useRef<string[]>([]);
    const selectCreateNameRef = useRef(false);
    /** The suggested name while the user has not typed over it. Null once they edit. */
    const untouchedSuggestionRef = useRef<string | null>(null);
    const cloudNamesRef = useRef<string[]>([]);
    cloudNamesRef.current = cloudWorkspaces.map((cw) => cw.name);
    const editorRef = useRef({ creating: false, renaming: false });
    editorRef.current.creating = creatingName !== null;
    editorRef.current.renaming = renaming !== null;

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

    const nameInputStyle: CSSProperties = {
        width: "100%",
        boxSizing: "border-box",
        height: 28,
        borderRadius: 7,
        border: `1px solid ${t.btnColor}`,
        background: t.fieldBg,
        color: t.inputText || t.text,
        minWidth: 0,
        padding: "0 8px",
        fontSize: 13.5,
        fontWeight: 500,
        outline: "none",
        fontFamily: "system-ui, -apple-system, sans-serif",
        lineHeight: "26px",
        userSelect: "text",
    };
    const renameButtonStyle: CSSProperties = {
        flexShrink: 0,
        height: 24,
        padding: "0 8px",
        boxSizing: "border-box",
        borderRadius: 6,
        border: `1px solid ${t.fieldBorder}`,
        background: t.fieldBg,
        color: t.textMuted,
        fontSize: 12,
        fontWeight: 400,
        lineHeight: "22px",
        cursor: cloudBusy ? "default" : "pointer",
        fontFamily: "system-ui, -apple-system, sans-serif",
    };
    const namePlaceholder = isZh ? "工作区名称" : "Workspace name";
    const cloudMetaStyle: CSSProperties = { flex: 1, minWidth: 0, display: "flex", flexDirection: "column", gap: 1 };
    const cloudNameStyle: CSSProperties = { minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" };
    /** Input and 改名 share one row so the button is not centered against the hint below. */
    const nameLineStyle: CSSProperties = { display: "flex", alignItems: "center", gap: 10, minWidth: 0, width: "100%", height: 28 };
    const nameInputFlexStyle: CSSProperties = { ...nameInputStyle, flex: "1 1 0", width: "auto" };
    const renameBesideInputStyle: CSSProperties = { ...renameButtonStyle, height: 28, lineHeight: "26px" };
    const draftVisual = popoverItemStyle(t);

    const suggestDefaultName = (extra: string[] = []) => nextDefaultCloudWorkspaceName([
        ...cloudNamesRef.current,
        ...rejectedDefaultNamesRef.current,
        ...extra,
    ]);

    const beginCreate = () => {
        if (cloudBusyRef.current || !onCreateCloud) return;
        setCloudError("");
        setRenaming(null);
        rejectedDefaultNamesRef.current = [];
        const next = suggestDefaultName();
        untouchedSuggestionRef.current = next;
        selectCreateNameRef.current = true;
        setCreatingName(next);
    };

    // Select first. The list-sync effect below may set the flag and a new name
    // in this same flush; consuming the flag here would select the old value.
    useEffect(() => {
        if (!selectCreateNameRef.current) return;
        selectCreateNameRef.current = false;
        if (creatingName === null) return;
        const input = createNameRef.current;
        if (!input) return;
        // readOnly during the request must not leave the field blurred.
        input.focus();
        input.select();
    }, [creatingName]);

    useEffect(() => {
        if (creatingName === null || untouchedSuggestionRef.current === null) return;
        if (creatingName !== untouchedSuggestionRef.current) return;
        const next = suggestDefaultName();
        if (next === creatingName) return;
        untouchedSuggestionRef.current = next;
        selectCreateNameRef.current = true;
        setCreatingName(next);
    }, [cloudWorkspaces, creatingName]);

    const submitCreate = async () => {
        const name = (creatingName ?? "").trim();
        if (!name || !onCreateCloud || cloudBusyRef.current) return;
        cloudBusyRef.current = true;
        setCloudBusy(true);
        setCloudError("");
        try {
            await onCreateCloud(name);
            setCreatingName(null);
        } catch (err) {
            // Keep the editor so the typed name can be retried. The page-level
            // error sits under the popover, so repeat it inside the pane.
            setCloudError(extractErrorMessage(err) || (isZh ? "新建云端工作区失败" : "Failed to create cloud workspace"));
            const taken = name.trim();
            if (cloudWorkspaceNameTaken(err) && defaultCloudWorkspaceName.test(taken)) {
                rejectedDefaultNamesRef.current = [...rejectedDefaultNamesRef.current, taken];
                const next = suggestDefaultName();
                if (next !== taken) {
                    untouchedSuggestionRef.current = next;
                    selectCreateNameRef.current = true;
                    setCreatingName(next);
                }
            }
        } finally {
            cloudBusyRef.current = false;
            setCloudBusy(false);
        }
    };

    const beginRename = (cw: CloudWorkspaceOption) => {
        if (cloudBusyRef.current || !onRenameCloud) return;
        setCloudError("");
        setCreatingName(null);
        setRenaming({ id: cw.id, value: cw.name });
    };

    const submitRename = async () => {
        if (!renaming || !onRenameCloud || cloudBusyRef.current) return;
        const name = renaming.value.trim();
        if (!name) {
            setRenaming(null);
            setCloudError("");
            return;
        }
        const current = cloudWorkspaces.find((cw) => cw.id === renaming.id);
        if (current && current.name === name) {
            setRenaming(null);
            setCloudError("");
            return;
        }
        cloudBusyRef.current = true;
        setCloudBusy(true);
        setCloudError("");
        try {
            await onRenameCloud(renaming.id, name);
            setRenaming(null);
        } catch (err) {
            setCloudError(extractErrorMessage(err) || (isZh ? "云端工作区改名失败" : "Failed to rename cloud workspace"));
        } finally {
            cloudBusyRef.current = false;
            setCloudBusy(false);
        }
    };

    // First Escape leaves the name editor; the next one closes the popover.
    const consumeEscape = useCallback(() => {
        if (!editorRef.current.creating && !editorRef.current.renaming) return false;
        editorRef.current.creating = false;
        editorRef.current.renaming = false;
        setCreatingName(null);
        setRenaming(null);
        setCloudError("");
        return true;
    }, []);

    const onNameKeyDown = (event: ReactKeyboardEvent<HTMLInputElement>, submit: () => void) => {
        event.stopPropagation();
        if (imeComposing(event) || event.key !== "Enter") return;
        event.preventDefault();
        submit();
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
            onEscape={consumeEscape}
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
                            {cloudWorkspaces.map((cw) => {
                                const selected = workspace.kind === 'cloud' && workspace.cloudWorkspaceId === cw.id;
                                const editing = renaming?.id === cw.id;
                                const v = popoverItemStyle(t, { selected });
                                const subtitle = `${cw.spec} · ${cw.state}`;
                                return (
                                    <div
                                        key={cw.id}
                                        data-testid={`workspace-cloud-${cw.id}`}
                                        className="mc-taskcfg-item"
                                        style={{ ...v.item, minWidth: 0, alignItems: "flex-start", cursor: editing || cloudBusy ? "default" : "pointer", userSelect: editing ? "text" : "none" }}
                                        onClick={editing ? undefined : (event) => {
                                            if (cloudBusyRef.current) return;
                                            const target = event.target as HTMLElement;
                                            if (target.closest("button, input")) return;
                                            onSelectCloud(cw.id, cw.name);
                                        }}
                                        onKeyDown={editing ? undefined : popoverItemKeyDown(() => {
                                            if (cloudBusyRef.current) return;
                                            onSelectCloud(cw.id, cw.name);
                                        })}
                                        tabIndex={editing ? undefined : 0}
                                    >
                                        <span style={v.icon}><TaskConfigIcon name="cloud" size={15} /></span>
                                        <span style={cloudMetaStyle}>
                                            <span style={nameLineStyle}>
                                                {editing ? (
                                                    <input
                                                        autoFocus
                                                        className="mc-taskcfg-name-input"
                                                        data-testid={`workspace-cloud-rename-input-${cw.id}`}
                                                        aria-label={namePlaceholder}
                                                        placeholder={namePlaceholder}
                                                        value={renaming.value}
                                                        disabled={cloudBusy}
                                                        onChange={(event) => setRenaming({ id: cw.id, value: event.target.value })}
                                                        onClick={(event) => event.stopPropagation()}
                                                        onKeyDown={(event) => onNameKeyDown(event, () => { void submitRename(); })}
                                                        onFocus={(event) => event.currentTarget.select()}
                                                        style={nameInputFlexStyle}
                                                    />
                                                ) : (
                                                    <span style={{ ...cloudNameStyle, flex: "1 1 0" }}>{cw.name}</span>
                                                )}
                                                {selected && !editing && <TaskConfigIcon name="check" size={14} />}
                                                {onRenameCloud && (
                                                    <button
                                                        type="button"
                                                        data-testid={`workspace-cloud-rename-${cw.id}`}
                                                        aria-label={isZh ? `改名 ${cw.name}` : `Rename ${cw.name}`}
                                                        disabled={cloudBusy}
                                                        onClick={(event) => {
                                                            event.stopPropagation();
                                                            if (editing) void submitRename();
                                                            else beginRename(cw);
                                                        }}
                                                        onKeyDown={(event) => event.stopPropagation()}
                                                        style={renameBesideInputStyle}
                                                    >
                                                        {isZh ? "改名" : "Rename"}
                                                    </button>
                                                )}
                                            </span>
                                            <span style={{ ...v.desc, display: "block", marginTop: 0 }}>{subtitle}</span>
                                            {editing && cloudError && (
                                                <span data-testid="workspace-cloud-error" role="alert" style={{ fontSize: 12, lineHeight: 1.4, color: t.errorText }}>{cloudError}</span>
                                            )}
                                        </span>
                                    </div>
                                );
                            })}
                            {cloudWorkspaces.length === 0 && creatingName === null && (
                                <div style={{ padding: "10px", fontSize: 12.5, color: t.textMuted }}>
                                    {isZh ? "暂无云端工作区" : "No cloud workspaces"}
                                </div>
                            )}
                            <div style={sep} />
                            {creatingName !== null ? (
                                <div
                                    data-testid="workspace-cloud-create-editor"
                                    className="mc-taskcfg-item"
                                    style={{ ...draftVisual.item, minWidth: 0, alignItems: "flex-start", cursor: "default", userSelect: "text" }}
                                >
                                    <span style={draftVisual.icon}><TaskConfigIcon name="cloud" size={15} /></span>
                                    <span style={cloudMetaStyle}>
                                        <span style={nameLineStyle}>
                                            <input
                                                ref={createNameRef}
                                                autoFocus
                                                className="mc-taskcfg-name-input"
                                                data-testid="workspace-cloud-create-name"
                                                aria-label={namePlaceholder}
                                                placeholder={namePlaceholder}
                                                value={creatingName}
                                                readOnly={cloudBusy}
                                                onChange={(event) => {
                                                    const value = event.target.value;
                                                    if (value !== untouchedSuggestionRef.current) untouchedSuggestionRef.current = null;
                                                    setCreatingName(value);
                                                }}
                                                onFocus={(event) => event.currentTarget.select()}
                                                onKeyDown={(event) => onNameKeyDown(event, () => { void submitCreate(); })}
                                                style={nameInputFlexStyle}
                                            />
                                            <button
                                                type="button"
                                                data-testid="workspace-cloud-create-rename"
                                                aria-label={isZh ? "改名" : "Rename"}
                                                disabled={cloudBusy}
                                                onClick={() => {
                                                    if ((creatingName ?? "").trim()) void submitCreate();
                                                    else createNameRef.current?.focus();
                                                }}
                                                style={renameBesideInputStyle}
                                            >
                                                {isZh ? "改名" : "Rename"}
                                            </button>
                                        </span>
                                        <span style={{ ...draftVisual.desc, display: "block", marginTop: 0 }}>
                                            {isZh ? "输入名称，Enter 或点改名创建" : "Type a name, then press Enter or Rename"}
                                        </span>
                                        {cloudError && (
                                            <span data-testid="workspace-cloud-error" role="alert" style={{ fontSize: 12, lineHeight: 1.4, color: t.errorText }}>{cloudError}</span>
                                        )}
                                    </span>
                                </div>
                            ) : onCreateCloud ? (
                                renderRow("create", "plus", isZh ? "新建云端工作区…" : "New cloud workspace…", "", {
                                    onPick: beginCreate,
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
