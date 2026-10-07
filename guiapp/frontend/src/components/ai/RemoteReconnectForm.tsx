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
                                            name="maclaw-remote-ssh-host"
                                            autoComplete="off"
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
                                            name="maclaw-remote-ssh-user"
                                            autoComplete="off"
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
                                            name="maclaw-remote-ssh-secret"
                                            type="password"
                                            disabled={remoteReconnect.connecting}
                                            autoComplete="new-password"
                                            autoCapitalize="off"
                                            spellCheck={false}
                                            data-1p-ignore="true"
                                            data-lpignore="true"
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

/** Status row after a remote SSH reconnect succeeds. Matches the main task surface, not a green alert. */
export function RemoteReconnectSuccessToast(props: RemoteReconnectSuccessToastProps) {
    const { lang, theme: t, message, onDismiss } = props;
    return (
                    <div
                        className="aap-reconnect-success"
                        data-testid="remote-coding-reconnect-success"
                        data-surface="theme"
                        data-coding-float-ignore-outside=""
                        role="status"
                        style={{
                            borderColor: t.titleBarBorder || "var(--theme-border)",
                            background: t.bg || "var(--theme-surface, #fff)",
                            color: t.text,
                        }}
                    >
                        <span className="aap-reconnect-success__mark" aria-hidden="true">✓</span>
                        <span className="aap-reconnect-success__text">{message}</span>
                        <button
                            type="button"
                            className="aap-reconnect-success__dismiss"
                            data-testid="remote-coding-reconnect-success-dismiss"
                            onClick={onDismiss}
                            style={{ color: t.textMuted || "var(--theme-text-secondary)" }}
                        >
                            {localizeText(lang, "Dismiss", "关闭", "關閉")}
                        </button>
                    </div>
    );
}
