import { useCallback, useEffect, useRef, useState, type CSSProperties } from "react";
import type { Theme } from "../aiAssistantPanelTheme";
import { extractErrorMessage } from "../participantAddError";
import { TaskConfigIcon } from "./taskConfigIcons";
import { TaskConfigPopoverShell, popoverItemKeyDown, popoverSearchInputStyle } from "./TaskConfigPopoverShell";
import { popoverBodyColumnStyle, popoverHeadStyle, popoverItemStyle, popoverListStyle, popoverSepStyle, popoverStatusLineStyle } from "./taskConfigPopoverStyles";
import type { MachineTarget } from "./taskDraft";
import { tailText } from "../../../i18n/mnTail";

export interface RemoteDirEntry {
    name: string;
    path: string;
}

/** remote_control list_dirs 的一页结果（被控机器上的目录浏览）。 */
export interface RemoteDirListing {
    path: string;
    parent?: string;
    home?: string;
    default_dir?: string;
    roots?: string[];
    dirs?: RemoteDirEntry[];
    truncated?: boolean;
}

/** Same-folder compare tolerant of separator/case spelling (Windows remotes). */
function sameRemoteDir(a: string, b: string): boolean {
    if (a === b) return true;
    return a.replaceAll("/", "\\").toLowerCase() === b.replaceAll("/", "\\").toLowerCase();
}

export interface RemoteDirPickerPopoverProps {
    anchor: HTMLElement | null;
    theme: Theme;
    lang?: string;
    machine: MachineTarget;
    /** 当前选中的对方电脑工作目录；空 = 对方电脑默认。 */
    current: string;
    /** remote_control list_dirs；path 为空时对方返回其默认工作目录。 */
    onList: (grantId: string, path: string) => Promise<RemoteDirListing>;
    /** workDir 为空 = 对方电脑默认工作目录。 */
    onSelect: (workDir: string) => void;
    onClose: () => void;
}

/**
 * 远控被控机器的目录浏览弹层：与本地目录面板同构——顶部可输入路径，
 * 列表提供默认目录 / 主目录 / 上一级 / 子目录逐级浏览，选中「使用此目录」
 * 写入 draft.machine.workDir。所有行数据来自 onList（remote_control
 * list_dirs），本机不接触对方文件系统。
 */
export function RemoteDirPickerPopover({
    anchor,
    theme: t,
    lang,
    machine,
    current,
    onList,
    onSelect,
    onClose,
}: RemoteDirPickerPopoverProps) {
    const [listing, setListing] = useState<RemoteDirListing | null>(null);
    const [manualPath, setManualPath] = useState("");
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    // Drive-root view (Windows): reached by pressing 上一级 at a drive root.
    const [atRoots, setAtRoots] = useState(false);
    const seqRef = useRef(0);
    const mountedRef = useRef(true);
    useEffect(() => () => { mountedRef.current = false; }, []);

    const navigate = useCallback(async (path: string) => {
        const seq = ++seqRef.current;
        setLoading(true);
        setError("");
        try {
            const next = await onList(machine.grantId, path);
            if (!mountedRef.current || seq !== seqRef.current) return;
            setListing(next);
            setManualPath(next.path || "");
            setAtRoots(false);
        } catch (err) {
            if (!mountedRef.current || seq !== seqRef.current) return;
            setError(extractErrorMessage(err) || tailText(lang ?? "", "Failed to read that folder", "目录读取失败"));
        } finally {
            if (mountedRef.current && seq === seqRef.current) setLoading(false);
        }
    }, [machine.grantId, onList, lang]);

    // 打开时已选目录直接进入该目录，否则落在对方默认工作目录；
    // 换一台机器后重新加载（对方的已选目录不跨机器迁移）。
    const initialRef = useRef({ grantId: machine.grantId, path: current });
    useEffect(() => {
        const initial = initialRef.current;
        const path = initial.grantId === machine.grantId ? initial.path : "";
        initialRef.current = { grantId: machine.grantId, path };
        void navigate(path);
        // eslint-disable-next-line react-hooks/exhaustive-deps -- reload only on machine switch
    }, [machine.grantId]);

    const roots = listing?.roots || [];
    const goUp = () => {
        if (!listing) return;
        if (listing.parent) void navigate(listing.parent);
        else if (roots.length > 0) setAtRoots(true);
    };
    const pickCurrent = () => {
        if (listing?.path) onSelect(listing.path);
    };

    const renderRow = (
        key: string,
        icon: Parameters<typeof TaskConfigIcon>[0]["name"],
        title: string,
        desc: string,
        opts: { selected?: boolean; disabled?: boolean; onPick?: () => void; testId?: string },
    ) => {
        const v = popoverItemStyle(t, { selected: opts.selected, disabled: opts.disabled });
        const descStyle: CSSProperties = { ...v.desc, display: "block" };
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
                title={opts.disabled ? undefined : desc || title}
            >
                <span style={v.icon}><TaskConfigIcon name={icon} size={15} /></span>
                <span style={v.meta}>
                    {title}
                    {desc ? <span style={descStyle}>{desc}</span> : null}
                </span>
                {opts.selected && <TaskConfigIcon name="check" size={14} />}
            </div>
        );
    };

    const sep = popoverSepStyle(t);

    return (
        <TaskConfigPopoverShell
            anchor={anchor}
            theme={t}
            onClose={onClose}
            width={400}
            data-testid="task-config-popover-remote-dir"
        >
            <div style={popoverBodyColumnStyle()}>
                <div style={popoverHeadStyle()}>
                    <input
                        placeholder={tailText(lang ?? "", "Type a path on that computer", "输入对方电脑上的路径")}
                        aria-label={tailText(lang ?? "", "Type a path on that computer", "输入对方电脑上的路径")}
                        data-testid="remote-dir-path-input"
                        style={popoverSearchInputStyle(t)}
                        value={manualPath}
                        onChange={(e) => setManualPath(e.target.value)}
                        onKeyDown={(e) => {
                            if (e.key !== "Enter") return;
                            e.preventDefault();
                            const path = manualPath.trim();
                            if (path) void navigate(path);
                        }}
                    />
                </div>
                <div style={popoverListStyle()}>
                    {renderRow("default", "home", tailText(lang ?? "", "Default working directory", "默认工作目录"), listing?.default_dir || tailText(lang ?? "", "Use that computer's default directory", "使用对方电脑的默认目录"), {
                        selected: !current,
                        onPick: () => onSelect(""),
                        testId: "remote-dir-default",
                    })}
                    {listing?.home && listing.home !== listing.path ? renderRow("home", "folder", tailText(lang ?? "", "Home folder", "主目录"), listing.home, {
                        onPick: () => void navigate(listing.home || ""),
                        testId: "remote-dir-home",
                    }) : null}
                    {!atRoots && listing?.path ? renderRow("use-current", "check", tailText(lang ?? "", "Use this folder", "使用此目录"), listing.path, {
                        selected: !!current && sameRemoteDir(current, listing.path),
                        onPick: pickCurrent,
                        testId: "remote-dir-use-current",
                    }) : null}
                    {manualPath.trim() && (!listing || !sameRemoteDir(manualPath.trim(), listing.path)) ? renderRow("manual", "folder", tailText(lang ?? "", "Use this path", "使用此路径"), manualPath.trim(), {
                        // A typed path may not exist yet — create_task allows a
                        // fresh project root, same as the local typed path.
                        onPick: () => onSelect(manualPath.trim()),
                        testId: "remote-dir-manual",
                    }) : null}
                    {error ? (
                        <div data-testid="remote-dir-error" style={popoverStatusLineStyle(t, { error: true })}>{error}</div>
                    ) : null}
                    <div style={sep} />
                    {!atRoots && listing ? renderRow("up", "back", tailText(lang ?? "", "Up one level", "上一级"), listing.parent || (roots.length > 0 ? tailText(lang ?? "", "This PC", "此电脑") : ""), {
                        disabled: !listing.parent && roots.length === 0,
                        onPick: goUp,
                        testId: "remote-dir-up",
                    }) : null}
                    {loading ? (
                        <div data-testid="remote-dir-loading" style={popoverStatusLineStyle(t)}>
                            {tailText(lang ?? "", "Loading…", "正在读取…")}
                        </div>
                    ) : atRoots ? (
                        roots.map((root) => renderRow(`root-${root}`, "monitor", root, "", {
                            onPick: () => void navigate(root),
                            testId: `remote-dir-root-${root}`,
                        }))
                    ) : listing ? (
                        <>
                            {(listing.dirs || []).map((dir) => renderRow(`dir-${dir.path}`, "folder", dir.name, dir.path, {
                                onPick: () => void navigate(dir.path),
                                testId: `remote-dir-${dir.name}`,
                            }))}
                            {(listing.dirs || []).length === 0 ? (
                                <div data-testid="remote-dir-empty" style={popoverStatusLineStyle(t)}>
                                    {tailText(lang ?? "", "No subfolders", "没有子目录")}
                                </div>
                            ) : null}
                            {listing.truncated ? (
                                <div data-testid="remote-dir-truncated" style={popoverStatusLineStyle(t, { small: true })}>
                                    {tailText(lang ?? "", "Showing the first entries only", "仅显示前部分目录")}
                                </div>
                            ) : null}
                        </>
                    ) : null}
                </div>
            </div>
        </TaskConfigPopoverShell>
    );
}
