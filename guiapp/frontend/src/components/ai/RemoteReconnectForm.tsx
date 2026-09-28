import { formFieldInputStyle, formFieldLabelColor, primaryFilledButtonStyle, type Theme } from "./aiAssistantPanelTheme";
import { localizeText } from "./aiAssistantI18n";

/**
 * Extracted from AIAssistantPanel (kept under the main-UI line guard).
 * Shape mirrors the panel's remote-reconnect useState exactly.
 */
export interface RemoteReconnectState {
    needsReconnect: boolean;
    safety?: "diagnosis";
    host: string;
    user: string;
    port: number;
    workDir: string;
    password: string;
    connecting: boolean;
    error: string;
    success: string;
    sessionPlan: string;
}

export interface RemoteReconnectFormProps {
    lang?: string;
    theme: Theme;
    reconnect: RemoteReconnectState;
    onFieldChange: (updater: (prev: RemoteReconnectState) => RemoteReconnectState) => void;
    onBlurIdentity: () => void;
    onSubmit: () => void;
}

/** SSH reconnect form shown when a remote coding tab needs re-connection. */
export function RemoteReconnectForm(props: RemoteReconnectFormProps) {
    const {
        lang,
        theme: t,
        reconnect: remoteReconnect,
        onFieldChange: setRemoteReconnect,
        onBlurIdentity: hydrateRemoteReconnectIdentity,
        onSubmit,
    } = props;
    return (
        <div className="aap-reconnect-form" data-testid="remote-coding-reconnect-form">
                                <div style={{ fontSize: 12, fontWeight: 600, color: t.headingColor || t.text }}>
                                    {localizeText(lang, "Reconnect remote SSH", "重新连接远程 SSH", "重新連線遠端 SSH")}
                                </div>
                                <div className="aap-reconnect-grid">
                                    <label style={{ display: "flex", flexDirection: "column", gap: 3, fontSize: 11, fontWeight: 600, color: formFieldLabelColor(t) }}>
                                        {localizeText(lang, "Host", "主机", "主機")}
                                        <input
                                            data-testid="remote-reconnect-host"
                                            disabled={remoteReconnect.connecting}
                                            value={remoteReconnect.host}
                                            onChange={(e) => setRemoteReconnect(prev => ({ ...prev, host: e.target.value, error: "" }))}
                                            onBlur={hydrateRemoteReconnectIdentity}
                                            style={{ height: 28, padding: "0 8px", borderRadius: 4, fontSize: 12, ...formFieldInputStyle(t) }}
                                        />
                                    </label>
                                    <label style={{ display: "flex", flexDirection: "column", gap: 3, fontSize: 11, fontWeight: 600, color: formFieldLabelColor(t) }}>
                                        {localizeText(lang, "User", "用户名", "使用者")}
                                        <input
                                            data-testid="remote-reconnect-user"
                                            disabled={remoteReconnect.connecting}
                                            value={remoteReconnect.user}
                                            onChange={(e) => setRemoteReconnect(prev => ({ ...prev, user: e.target.value, error: "" }))}
                                            onBlur={hydrateRemoteReconnectIdentity}
                                            style={{ height: 28, padding: "0 8px", borderRadius: 4, fontSize: 12, ...formFieldInputStyle(t) }}
                                        />
                                    </label>
                                    <label style={{ display: "flex", flexDirection: "column", gap: 3, fontSize: 11, fontWeight: 600, color: formFieldLabelColor(t) }}>
                                        {localizeText(lang, "Port", "端口", "連接埠")}
                                        <input
                                            data-testid="remote-reconnect-port"
                                            type="number"
                                            disabled={remoteReconnect.connecting}
                                            value={remoteReconnect.port || 22}
                                            onChange={(e) => setRemoteReconnect(prev => ({ ...prev, port: Number(e.target.value) || 22, error: "" }))}
                                            onBlur={hydrateRemoteReconnectIdentity}
                                            style={{ height: 28, padding: "0 8px", borderRadius: 4, fontSize: 12, ...formFieldInputStyle(t) }}
                                        />
                                    </label>
                                    <label style={{ display: "flex", flexDirection: "column", gap: 3, fontSize: 11, fontWeight: 600, color: formFieldLabelColor(t) }}>
                                        {localizeText(lang, "Password", "密码", "密碼")}
                                        <input
                                            data-testid="remote-reconnect-password"
                                            type="password"
                                            disabled={remoteReconnect.connecting}
                                            autoComplete="current-password"
                                            value={remoteReconnect.password}
                                            onChange={(e) => setRemoteReconnect(prev => ({ ...prev, password: e.target.value }))}
                                            onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); onSubmit(); } }}
                                            placeholder={localizeText(lang, "Remembered on this device", "本机记忆，下次自动填充", "本機記憶，下次自動填入")}
                                            style={{ height: 28, padding: "0 8px", borderRadius: 4, fontSize: 12, ...formFieldInputStyle(t) }}
                                        />
                                    </label>
                                </div>
                                <label style={{ display: "flex", flexDirection: "column", gap: 3, fontSize: 11, fontWeight: 600, color: formFieldLabelColor(t) }}>
                                    {localizeText(lang, "Remote work directory", "远程工作目录", "遠端工作目錄")}
                                    <input
                                        data-testid="remote-reconnect-workdir"
                                        disabled={remoteReconnect.connecting}
                                        value={remoteReconnect.workDir}
                                        onChange={(e) => setRemoteReconnect(prev => ({ ...prev, workDir: e.target.value }))}
                                        style={{ height: 28, padding: "0 8px", borderRadius: 4, fontSize: 12, ...formFieldInputStyle(t) }}
                                    />
                                </label>
                                {remoteReconnect.sessionPlan && (
                                    <div style={{ fontSize: 11, color: formFieldLabelColor(t), lineHeight: 1.45 }}>
                                        {localizeText(lang, "Continuing session plan: ", "将延续会话目标：", "將延續工作階段目標：")}
                                        {remoteReconnect.sessionPlan.length > 160 ? `${remoteReconnect.sessionPlan.slice(0, 160)}…` : remoteReconnect.sessionPlan}
                                    </div>
                                )}
                                {remoteReconnect.error && (
                                    <div data-testid="remote-reconnect-error" style={{ fontSize: 11, color: t.errorText || "#c43d34" }}>{remoteReconnect.error}</div>
                                )}
                                {remoteReconnect.connecting && remoteReconnect.success && (
                                    <div
                                        data-testid="remote-reconnect-progress"
                                        role="status"
                                        aria-live="polite"
                                        style={{ fontSize: 11, color: formFieldLabelColor(t) }}
                                    >
                                        {remoteReconnect.success}
                                    </div>
                                )}
                                <div className="aap-actions-end">
                                    <button
                                        type="button"
                                        data-testid="remote-reconnect-submit"
                                        disabled={remoteReconnect.connecting}
                                        onClick={() => { onSubmit(); }}
                                        style={primaryFilledButtonStyle(t, {
                                            height: 28,
                                            padding: "0 14px",
                                            borderRadius: 4,
                                            fontSize: 12,
                                            fontWeight: 600,
                                            cursor: remoteReconnect.connecting ? "wait" : "pointer",
                                            opacity: remoteReconnect.connecting ? 0.75 : 1,
                                        })}
                                    >
                                        {remoteReconnect.connecting
                                            ? localizeText(lang, "Connecting…", "连接中…", "連線中…")
                                            : localizeText(lang, "Reconnect", "重新连接", "重新連線")}
                                    </button>
                                </div>
                            </div>
    );
}

export interface RemoteReconnectSuccessToastProps {
    lang?: string;
    theme: Theme;
    message: string;
    onDismiss: () => void;
}

/** Floating success toast after a remote SSH reconnect succeeds. */
export function RemoteReconnectSuccessToast(props: RemoteReconnectSuccessToastProps) {
    const { lang, theme: t, message, onDismiss } = props;
    return (
                    <div
                        data-testid="remote-coding-reconnect-success"
                        data-coding-float-ignore-outside=""
                        role="status"
                        style={{
                            position: "absolute",
                            // Below the coding chip's rest position (defaultTop 80 + chip height).
                            top: 124,
                            right: 10,
                            // Above coding float root (zIndex 40) so dismiss stays clickable.
                            zIndex: 45,
                            maxWidth: "min(320px, calc(100% - 20px))",
                            padding: "8px 12px",
                            borderRadius: 8,
                            border: `1px solid ${t.titleBarBorder}`,
                            background: `color-mix(in srgb, var(--theme-success, #4f7f6f) 12%, ${t.bg || "var(--theme-surface, #fff)"})`,
                            color: t.text,
                            fontSize: 12,
                            display: "flex",
                            alignItems: "center",
                            justifyContent: "space-between",
                            gap: 8,
                            boxShadow: "0 8px 20px rgba(15,23,42,0.12)",
                            pointerEvents: "auto",
                        }}
                    >
                        <span>{message}</span>
                        <button
                            type="button"
                            data-testid="remote-coding-reconnect-success-dismiss"
                            onClick={onDismiss}
                            style={{ border: "none", background: "transparent", color: t.textMuted, cursor: "pointer", fontSize: 11 }}
                        >
                            {localizeText(lang, "Dismiss", "关闭", "關閉")}
                        </button>
                    </div>
    );
}
