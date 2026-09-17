import { useState, type CSSProperties, type KeyboardEvent, type ReactNode } from "react";
import type { Theme } from "../aiAssistantPanelTheme";
import { primaryFilledButtonStyle } from "../aiAssistantPanelTheme";
import { TestRemoteSSHConnection } from "../../../../wailsjs/go/main/App";
import { TaskConfigPopoverShell } from "./TaskConfigPopoverShell";
import type { RemoteTarget } from "./taskDraft";

/** 端口校验：空 = 默认 22；非法返回 null（调用方提示错误，不静默改值）。 */
function validatePort(raw: string): number | null {
    const trimmed = raw.trim();
    if (!trimmed) return 22;
    const n = parseInt(trimmed, 10);
    return Number.isFinite(n) && n > 0 && n <= 65535 ? n : null;
}

export interface RemoteServerFormProps {
    theme: Theme;
    lang?: string;
    /** 已保存的远程目标（编辑时回填）。 */
    initial?: RemoteTarget;
    /** 校验通过（host / user / workDir 非空）后回填 workspace.target。 */
    onConfirm: (remote: RemoteTarget) => void;
    /** 追加在底部按钮栏左侧的节点（如工作空间弹层的「返回」）。 */
    footerLeading?: ReactNode;
}

/**
 * 远程服务器（SSH）表单：主机 / 端口 / 用户名 / 密码 / 工作目录 + 测试连接。
 * 独立弹层（RemoteServerPopover）与工作空间弹层的远程子面板共用。
 */
export function RemoteServerForm({
    theme: t,
    lang,
    initial,
    onConfirm,
    footerLeading,
}: RemoteServerFormProps) {
    const isZh = !lang?.startsWith("en");
    const [host, setHost] = useState(initial?.host || "");
    const [port, setPort] = useState(String(initial?.port || 22));
    const [user, setUser] = useState(initial?.user || "");
    const [password, setPassword] = useState(initial?.password || "");
    const [workDir, setWorkDir] = useState(initial?.workDir || "");
    const [error, setError] = useState("");
    const [info, setInfo] = useState("");
    const [testing, setTesting] = useState(false);

    const fieldStyle = (extra: CSSProperties = {}): CSSProperties => ({
        border: `1px solid ${t.fieldBorder}`,
        background: t.fieldBg,
        color: t.inputText || t.text,
        borderRadius: 8,
        padding: "0 10px",
        fontSize: 13,
        height: 30,
        outline: "none",
        fontFamily: "system-ui, -apple-system, sans-serif",
        ...extra,
    });

    const rowLabelStyle: CSSProperties = {
        width: 60,
        flex: "0 0 60px",
        fontSize: 12.5,
        color: t.textMuted,
        textAlign: "right",
        fontFamily: "system-ui, -apple-system, sans-serif",
    };

    const portError = isZh ? "端口需为 1–65535 的整数。" : "Port must be an integer between 1 and 65535.";

    const testConnection = async () => {
        if (testing) return;
        const h = host.trim();
        const u = user.trim();
        const w = workDir.trim();
        if (!h || !u || !w) {
            setError(isZh ? "测试连接需填写主机、用户名和工作目录。" : "Host, username and work directory are required to test.");
            return;
        }
        const p = validatePort(port);
        if (p === null) {
            setError(portError);
            return;
        }
        setTesting(true);
        setError("");
        setInfo("");
        try {
            const msg = await TestRemoteSSHConnection(h, u, password, w, p);
            setInfo(msg || (isZh ? "SSH 连接成功" : "SSH connection OK"));
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err) || (isZh ? "SSH 连接失败" : "SSH connection failed"));
        } finally {
            setTesting(false);
        }
    };

    const confirm = () => {
        const h = host.trim();
        const u = user.trim();
        const w = workDir.trim();
        if (!h || !u || !w) {
            setError(isZh ? "请填写主机、用户名和远程工作目录。" : "Please fill host, username and remote work directory.");
            return;
        }
        const p = validatePort(port);
        if (p === null) {
            setError(portError);
            return;
        }
        onConfirm({ host: h, port: p, user: u, password, workDir: w });
    };

    const onFieldKeyDown = (event: KeyboardEvent<HTMLInputElement>, isLast: boolean) => {
        if (event.key !== "Enter" || event.shiftKey) return;
        event.preventDefault();
        if (isLast) {
            confirm();
        } else {
            const form = event.currentTarget.closest("form");
            const controls = form?.querySelectorAll<HTMLElement>("input");
            if (!controls) return;
            const list = Array.from(controls);
            list[list.indexOf(event.currentTarget) + 1]?.focus();
        }
    };

    const row = (label: string, control: ReactNode, key: string) => (
        <div key={key} style={{ display: "flex", alignItems: "center", gap: 8, padding: "5px 4px" }}>
            <label style={rowLabelStyle}>{label}</label>
            {control}
        </div>
    );

    return (
        <>
            <div style={{ padding: "12px 14px 4px", flexShrink: 0 }}>
                <div style={{ fontSize: 13.5, fontWeight: 600, color: t.titleText || t.text, fontFamily: "system-ui, -apple-system, sans-serif" }}>
                    {isZh ? "远程服务器（SSH）" : "Remote server (SSH)"}
                </div>
                <div style={{ fontSize: 12, color: t.textMuted, marginTop: 2, fontFamily: "system-ui, -apple-system, sans-serif" }}>
                    {isZh ? "通过「工作空间 › 远程服务器」进入，在此指定 SSH 服务器信息" : "Entered from Workspace › Remote server"}
                </div>
            </div>
            <form
                onSubmit={(e) => { e.preventDefault(); confirm(); }}
                style={{ padding: "4px 10px 8px", flexShrink: 0 }}
            >
                {row(isZh ? "主机" : "Host", (
                    <input
                        data-testid="remote-host"
                        value={host}
                        onChange={(e) => setHost(e.currentTarget.value)}
                        onKeyDown={(e) => onFieldKeyDown(e, false)}
                        placeholder={isZh ? "例如 192.168.1.10" : "e.g. 192.168.1.10"}
                        autoComplete="off"
                        style={fieldStyle({ flex: 1 })}
                    />
                ), "host")}
                {row(isZh ? "端口" : "Port", (
                    <input
                        data-testid="remote-port"
                        value={port}
                        onChange={(e) => setPort(e.currentTarget.value)}
                        onKeyDown={(e) => onFieldKeyDown(e, false)}
                        placeholder="22"
                        autoComplete="off"
                        style={fieldStyle({ width: 90 })}
                    />
                ), "port")}
                {row(isZh ? "用户名" : "User", (
                    <input
                        data-testid="remote-user"
                        value={user}
                        onChange={(e) => setUser(e.currentTarget.value)}
                        onKeyDown={(e) => onFieldKeyDown(e, false)}
                        placeholder="root"
                        autoComplete="off"
                        style={fieldStyle({ flex: 1 })}
                    />
                ), "user")}
                {row(isZh ? "密码" : "Password", (
                    <input
                        data-testid="remote-password"
                        type="password"
                        value={password}
                        onChange={(e) => setPassword(e.currentTarget.value)}
                        onKeyDown={(e) => onFieldKeyDown(e, false)}
                        placeholder="••••••••"
                        autoComplete="new-password"
                        style={fieldStyle({ flex: 1 })}
                    />
                ), "password")}
                {row(isZh ? "工作目录" : "Work dir", (
                    <input
                        data-testid="remote-workdir"
                        value={workDir}
                        onChange={(e) => setWorkDir(e.currentTarget.value)}
                        onKeyDown={(e) => onFieldKeyDown(e, true)}
                        placeholder="/opt/project"
                        autoComplete="off"
                        style={fieldStyle({ flex: 1 })}
                    />
                ), "workdir")}
            </form>
            {(error || info) && (
                <div
                    data-testid="remote-form-status"
                    style={{
                        padding: "0 14px 6px",
                        fontSize: 12,
                        color: error ? (t.errorText || "#ef4444") : t.btnColor,
                        fontFamily: "system-ui, -apple-system, sans-serif",
                        flexShrink: 0,
                    }}
                >
                    {error || info}
                </div>
            )}
            <div style={{
                padding: "10px 14px",
                borderTop: `1px solid ${t.divider || t.fieldBorder}`,
                display: "flex",
                justifyContent: "flex-end",
                alignItems: "center",
                gap: 8,
                flexShrink: 0,
            }}>
                {footerLeading}
                <button
                    type="button"
                    data-testid="remote-test-connection"
                    onClick={() => void testConnection()}
                    disabled={testing}
                    style={{
                        height: 30,
                        padding: "0 14px",
                        borderRadius: 8,
                        border: `1px solid ${t.fieldBorder}`,
                        background: t.fieldBg,
                        color: t.text,
                        fontSize: 12.5,
                        cursor: testing ? "wait" : "pointer",
                        fontFamily: "system-ui, -apple-system, sans-serif",
                    }}
                >
                    {testing ? (isZh ? "测试中…" : "Testing…") : (isZh ? "测试连接" : "Test connection")}
                </button>
                <button
                    type="button"
                    data-testid="remote-confirm"
                    onClick={confirm}
                    style={{
                        ...primaryFilledButtonStyle(t),
                        padding: "0 14px",
                        height: 30,
                        fontSize: 12.5,
                        borderRadius: 8,
                    }}
                >
                    {isZh ? "确定" : "OK"}
                </button>
            </div>
        </>
    );
}

export interface RemoteServerPopoverProps extends RemoteServerFormProps {
    anchor: HTMLElement | null;
    onClose: () => void;
}

/** 独立远程服务器弹层（供「工作空间 › 远程服务器」等入口使用）。 */
export function RemoteServerPopover({ anchor, onClose, ...formProps }: RemoteServerPopoverProps) {
    return (
        <TaskConfigPopoverShell
            anchor={anchor}
            theme={formProps.theme}
            onClose={onClose}
            width={420}
            data-testid="task-config-popover-remote"
        >
            <RemoteServerForm {...formProps} />
        </TaskConfigPopoverShell>
    );
}
