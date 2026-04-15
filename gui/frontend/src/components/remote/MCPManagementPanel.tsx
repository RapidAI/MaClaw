import { useState, useEffect, useCallback, useRef } from "react";
import type { CSSProperties, MouseEvent as ReactMouseEvent } from "react";
import JSON5 from "json5";
import { colors, radius } from "./styles";
import {
    ListMCPServers,
    RegisterMCPServer,
    UpdateMCPServer,
    UnregisterMCPServer,
    GetMCPServerTools,
    CheckMCPServerHealth,
    ProbeMCPServers,
    ListLocalMCPServers,
    RegisterLocalMCPServer,
    UpdateLocalMCPServer,
    UnregisterLocalMCPServer,
    SyncLocalMCPServers,
    SetLocalMCPAutoStart,
    GetLocalMCPServerStatuses,
} from "../../../wailsjs/go/main/App";

interface MCPToolView {
    name: string;
    description: string;
    input_schema: Record<string, any>;
}

interface MCPServerView {
    id: string;
    name: string;
    endpoint_url: string;
    auth_type: "none" | "api_key" | "bearer";
    auth_secret: string;
    headers?: Record<string, string>; // custom HTTP headers
    tools: MCPToolView[];
    health_status: "healthy" | "slow" | "unavailable" | "unknown" | "checking";
    fail_count: number;
    last_check_at: string;
    created_at: string;
}

interface LocalMCPServer {
    id: string;
    name: string;
    command: string;
    args: string[];
    env: Record<string, string>;
    disabled: boolean;
    auto_start?: boolean;
    created_at: string;
}

type Props = {
    translate: (key: string) => string;
};

type MCPTab = "local" | "remote";

const emptyServer: MCPServerView = {
    id: "",
    name: "",
    endpoint_url: "",
    auth_type: "none",
    auth_secret: "",
    tools: [],
    health_status: "healthy",
    fail_count: 0,
    last_check_at: "",
    created_at: "",
};

const emptyLocalServer: LocalMCPServer = {
    id: "",
    name: "",
    command: "npx",
    args: [],
    env: {},
    disabled: false,
    auto_start: false,
    created_at: "",
};

const tabStyle: CSSProperties = {
    flex: 1,
    padding: "6px 0",
    fontSize: "0.78rem",
    fontWeight: 600,
    cursor: "pointer",
    textAlign: "center",
    borderBottom: "2px solid transparent",
    color: colors.textMuted,
    background: "none",
    border: "none",
    borderRadius: 0,
    transition: "color 0.15s, border-color 0.15s",
};

const tabActiveStyle: CSSProperties = {
    ...tabStyle,
    color: "var(--theme-primary)",
    borderBottom: "2px solid var(--theme-primary)",
};

/**
 * Returns onMouseDown + onClick props for a modal backdrop.
 * Only fires `onClose` when the mousedown *started* on the backdrop itself,
 * preventing Ctrl+A (select-all) or drag-selections from accidentally
 * dismissing the dialog.
 */
function makeBackdropProps(onClose: () => void, ref: React.MutableRefObject<boolean>) {
    return {
        onMouseDown: (e: ReactMouseEvent<HTMLDivElement>) => {
            ref.current = e.target === e.currentTarget;
        },
        onClick: (e: ReactMouseEvent<HTMLDivElement>) => {
            if (e.target === e.currentTarget && ref.current) {
                onClose();
            }
            ref.current = false;
        },
    };
}

/**
 * Strip BOM and zero-width invisible characters that can sneak in from
 * copy-paste, then replace fullwidth CJK punctuation with ASCII equivalents.
 * This pre-processing runs before JSON5.parse() to handle characters that
 * even JSON5 doesn't accept.
 */
function preCleanJsonText(raw: string): string {
    return raw
        .replace(/[\uFEFF\u200B\u200C\u200D\u2060]/g, "")
        .replace(/\uff0c/g, ",")   // ，→ ,
        .replace(/\uff1a/g, ":")   // ：→ :
        .replace(/\uff1b/g, ";")   // ；→ ;
        .replace(/\u201c/g, '"')   // " → "
        .replace(/\u201d/g, '"')   // " → "
        .replace(/\uff5b/g, "{")   // ｛→ {
        .replace(/\uff5d/g, "}")   // ｝→ }
        .replace(/\uff3b/g, "[")   // ［→ [
        .replace(/\uff3d/g, "]");  // ］→ ]
}

/**
 * Parse a relaxed JSON/JSONC/JSON5 string into an object.
 * Handles comments, trailing commas, single quotes, unquoted keys,
 * fullwidth CJK punctuation, and BOM — all common when users paste
 * config snippets from editors or Chinese IME.
 */
function parseRelaxedJson(raw: string): any {
    return JSON5.parse(preCleanJsonText(raw));
}

export function MCPManagementPanel({ translate }: Props) {
    const [activeTab, setActiveTab] = useState<MCPTab>("remote");

    return (
        <div style={{ display: "flex", flexDirection: "column", gap: "10px" }}>
            <div style={{ display: "flex", borderBottom: `1px solid ${colors.border}` }}>
                <button
                    style={activeTab === "local" ? tabActiveStyle : tabStyle}
                    onClick={() => setActiveTab("local")}
                >
                    {translate("mcpTabLocal")}
                </button>
                <button
                    style={activeTab === "remote" ? tabActiveStyle : tabStyle}
                    onClick={() => setActiveTab("remote")}
                >
                    {translate("mcpTabRemote")}
                </button>
            </div>

            {activeTab === "local" && <LocalMCPPanel translate={translate} />}
            {activeTab === "remote" && <RemoteMCPPanel translate={translate} />}
        </div>
    );
}

function LocalMCPPanel({ translate }: Props) {
    const [servers, setServers] = useState<LocalMCPServer[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);
    const [statusMap, setStatusMap] = useState<Record<string, boolean>>({});
    const syncTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    const [showForm, setShowForm] = useState(false);
    const [editingServer, setEditingServer] = useState<LocalMCPServer | null>(null);
    const [formData, setFormData] = useState<LocalMCPServer>({ ...emptyLocalServer });
    const [formError, setFormError] = useState("");
    const [argsText, setArgsText] = useState("");
    const [envPairs, setEnvPairs] = useState<{ key: string; value: string }[]>([]);

    const [deleteTarget, setDeleteTarget] = useState<LocalMCPServer | null>(null);
    const [showJsonImport, setShowJsonImport] = useState(false);
    const [jsonText, setJsonText] = useState("");
    const [jsonError, setJsonError] = useState("");
    const backdropRef = useRef(false);

    const fetchStatuses = useCallback(async () => {
        try {
            const statuses = await GetLocalMCPServerStatuses();
            if (Array.isArray(statuses)) {
                const map: Record<string, boolean> = {};
                for (const s of statuses) map[s.id] = s.running;
                setStatusMap(map);
            }
        } catch {}
    }, []);

    const loadData = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const list = await ListLocalMCPServers();
            setServers(Array.isArray(list) ? list : []);
        } catch (err) {
            setError(String(err));
        } finally {
            setLoading(false);
        }
        await fetchStatuses();
    }, [fetchStatuses]);

    const reloadAndSync = useCallback(async () => {
        await loadData();
        try { await SyncLocalMCPServers(); } catch {}
        if (syncTimerRef.current) clearTimeout(syncTimerRef.current);
        syncTimerRef.current = setTimeout(() => { fetchStatuses(); }, 1500);
    }, [loadData, fetchStatuses]);

    useEffect(() => {
        loadData();
        if (syncTimerRef.current) clearTimeout(syncTimerRef.current);
        syncTimerRef.current = setTimeout(() => { fetchStatuses(); }, 1500);
    }, [loadData, fetchStatuses]);

    useEffect(() => {
        return () => {
            if (syncTimerRef.current) clearTimeout(syncTimerRef.current);
        };
    }, []);

    const openCreateForm = () => {
        setEditingServer(null);
        setFormData({ ...emptyLocalServer });
        setArgsText("");
        setEnvPairs([]);
        setFormError("");
        setShowForm(true);
    };

    const openEditForm = (server: LocalMCPServer) => {
        setEditingServer(server);
        setFormData({ ...server });
        setArgsText((server.args || []).join("\n"));
        setEnvPairs(Object.entries(server.env || {}).map(([key, value]) => ({ key, value })));
        setFormError("");
        setShowForm(true);
    };

    const closeForm = () => {
        setShowForm(false);
        setEditingServer(null);
        setFormError("");
    };

    const handleSubmit = async () => {
        if (!formData.name.trim()) { setFormError(translate("mcpNameRequired")); return; }
        if (!formData.command.trim()) { setFormError(translate("mcpCommandRequired")); return; }
        const args = argsText.split("\n").map(s => s.trim()).filter(Boolean);
        const env: Record<string, string> = {};
        for (const p of envPairs) {
            if (p.key.trim()) env[p.key.trim()] = p.value;
        }
        const entry: LocalMCPServer = { ...formData, args, env };
        setBusy(true);
        setFormError("");
        try {
            if (editingServer) {
                await UpdateLocalMCPServer(entry);
            } else {
                await RegisterLocalMCPServer(entry);
            }
            closeForm();
            await reloadAndSync();
        } catch (err) {
            setFormError(String(err));
        } finally {
            setBusy(false);
        }
    };

    const handleDelete = async (server: LocalMCPServer) => {
        setBusy(true);
        try {
            await UnregisterLocalMCPServer(server.id);
            setDeleteTarget(null);
            await reloadAndSync();
        } catch (err) {
            setError(String(err));
        } finally {
            setBusy(false);
        }
    };

    const handleToggleDisabled = async (server: LocalMCPServer) => {
        setBusy(true);
        try {
            await UpdateLocalMCPServer({ ...server, disabled: !server.disabled });
            await reloadAndSync();
        } catch (err) {
            setError(String(err));
        } finally {
            setBusy(false);
        }
    };

    const handleToggleAutoStart = async (server: LocalMCPServer) => {
        setBusy(true);
        try {
            await SetLocalMCPAutoStart(server.id, !server.auto_start);
            await loadData();
            if (syncTimerRef.current) clearTimeout(syncTimerRef.current);
            syncTimerRef.current = setTimeout(() => { fetchStatuses(); }, 1500);
        } catch (err) {
            setError(String(err));
        } finally {
            setBusy(false);
        }
    };

    const handleJsonImport = async () => {
        setJsonError("");
        let parsed: any;
        try {
            parsed = parseRelaxedJson(jsonText);
        } catch (e: any) {
            const detail = e?.message ? `: ${e.message}` : "";
            setJsonError(translate("mcpJsonFormatError") + detail); return;
        }
        const mcpServers = parsed.mcpServers || parsed;
        if (typeof mcpServers !== "object" || Array.isArray(mcpServers)) {
            setJsonError(translate("mcpJsonStructureError"));
            return;
        }
        setBusy(true);
        try {
            for (const [name, cfg] of Object.entries(mcpServers) as [string, any][]) {
                const entry: LocalMCPServer = {
                    ...emptyLocalServer,
                    name,
                    command: cfg.command || "npx",
                    args: Array.isArray(cfg.args) ? cfg.args : [],
                    env: typeof cfg.env === "object" && cfg.env ? cfg.env : {},
                    disabled: cfg.disabled === true,
                    auto_start: cfg.auto_start === true,
                };
                await RegisterLocalMCPServer(entry);
            }
            setShowJsonImport(false);
            setJsonText("");
            await reloadAndSync();
        } catch (err) {
            setJsonError(String(err));
        } finally {
            setBusy(false);
        }
    };

    return (
        <div style={{ display: "flex", flexDirection: "column", gap: "10px" }}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                <span style={{ fontSize: "0.78rem", color: colors.textSecondary }}>
                    {servers.length} {translate("mcpLocalCount")}
                </span>
                <div style={{ display: "flex", gap: "6px" }}>
                    <button className="btn-secondary" style={{ fontSize: "0.72rem", padding: "3px 10px" }} onClick={() => { setShowJsonImport(true); setJsonText(""); setJsonError(""); }} disabled={busy}>
                        {translate("mcpImportJson")}
                    </button>
                    <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 12px" }} onClick={openCreateForm} disabled={busy}>
                        {translate("mcpAdd")}
                    </button>
                </div>
            </div>

            {loading && <div style={{ textAlign: "center", padding: "16px", fontSize: "0.78rem", color: colors.textMuted }}>{translate("mcpLoading")}</div>}
            {error && <div style={{ fontSize: "0.78rem", color: colors.danger, background: "var(--theme-danger-bg)", padding: "6px 10px", borderRadius: "4px", border: `1px solid ${colors.danger}` }}>{error}</div>}

            {!loading && servers.length > 0 && (
                <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
                    {servers.map((s) => (
                        <div key={s.id} style={{
                            border: `1px solid ${colors.border}`,
                            borderRadius: "6px",
                            padding: "8px 10px",
                            background: s.disabled ? colors.surfaceMuted : colors.surface,
                            opacity: s.disabled ? 0.6 : 1,
                        }}>
                            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                                <div style={{ display: "flex", alignItems: "center", gap: "6px" }}>
                                    <span
                                        style={{
                                            display: "inline-block",
                                            width: "8px",
                                            height: "8px",
                                            borderRadius: "50%",
                                            background: s.disabled ? colors.textMuted : statusMap[s.id] ? colors.success : colors.danger,
                                            flexShrink: 0,
                                        }}
                                        title={s.disabled ? translate("mcpDisabled") : statusMap[s.id] ? translate("mcpRunning") : translate("mcpNotRunning")}
                                    />
                                    <span style={{ fontSize: "0.78rem", fontWeight: 600, color: colors.text }}>{s.name}</span>
                                    {s.disabled && <span style={{ fontSize: "0.66rem", color: colors.textMuted }}>({translate("mcpDisabled")})</span>}
                                    {s.auto_start && !s.disabled && <span style={{ fontSize: "0.66rem", color: colors.primary }}>({translate("mcpAutoStartOn")})</span>}
                                </div>
                                <div style={{ display: "flex", gap: "4px" }}>
                                    <button className="btn-secondary" style={smallBtnStyle} onClick={() => handleToggleDisabled(s)} disabled={busy}>
                                        {s.disabled ? translate("mcpEnable") : translate("mcpDisable")}
                                    </button>
                                    <button className="btn-secondary" style={smallBtnStyle} onClick={() => handleToggleAutoStart(s)} disabled={busy || s.disabled} title={s.disabled ? translate("mcpAutoStartDisabledHint") : undefined}>
                                        {s.auto_start ? translate("mcpAutoStartOff") : translate("mcpAutoStartOn")}
                                    </button>
                                    <button className="btn-secondary" style={smallBtnStyle} onClick={() => openEditForm(s)} disabled={busy}>{translate("mcpEdit")}</button>
                                    <button className="btn-secondary btn-danger" style={smallBtnStyle} onClick={() => setDeleteTarget(s)} disabled={busy}>{translate("mcpDelete")}</button>
                                </div>
                            </div>
                            <div style={{ fontSize: "0.72rem", color: colors.textSecondary, fontFamily: "monospace", marginTop: "4px", wordBreak: "break-all" }}>
                                {s.command} {(s.args || []).join(" ")}
                            </div>
                            {s.env && Object.keys(s.env).length > 0 && (
                                <div style={{ fontSize: "0.68rem", color: colors.textMuted, marginTop: "2px" }}>
                                    {translate("mcpEnvVars")}: {Object.keys(s.env).join(", ")}
                                </div>
                            )}
                            <div style={{ fontSize: "0.68rem", color: colors.textMuted, marginTop: "2px" }}>
                                {translate("mcpAutoStartStatus")}: {s.auto_start ? translate("mcpAutoStartEnabled") : translate("mcpAutoStartDisabled")}
                            </div>
                        </div>
                    ))}
                </div>
            )}

            {!loading && servers.length === 0 && !error && (
                <div style={{ textAlign: "center", padding: "20px", fontSize: "0.78rem", color: colors.textMuted }}>
                    {translate("mcpNoLocalServers")}
                </div>
            )}

            {deleteTarget && (
                <div className="modal-backdrop" {...makeBackdropProps(() => setDeleteTarget(null), backdropRef)}>
                    <div className="modal-content" onClick={(e) => e.stopPropagation()} style={{ width: "280px" }}>
                        <div className="modal-header">
                            <h3 style={{ fontSize: "0.88rem", margin: 0 }}>{translate("mcpConfirmDelete")}</h3>
                            <button className="btn-close" onClick={() => setDeleteTarget(null)}>×</button>
                        </div>
                        <div className="modal-body">
                            <p style={{ fontSize: "0.8rem", color: colors.textSecondary, margin: 0 }}>
                                {translate("mcpConfirmDeleteLocal").replace("{name}", deleteTarget.name)}
                            </p>
                        </div>
                        <div className="modal-footer">
                            <button className="btn-secondary" onClick={() => setDeleteTarget(null)} disabled={busy}>{translate("cancel")}</button>
                            <button className="btn-secondary btn-danger" onClick={() => handleDelete(deleteTarget)} disabled={busy}>
                                {busy ? translate("mcpDeleting") : translate("mcpDelete")}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {showJsonImport && (
                <div className="modal-backdrop" {...makeBackdropProps(() => setShowJsonImport(false), backdropRef)}>
                    <div className="modal-content" onClick={(e) => e.stopPropagation()} style={{ width: "480px", textAlign: "left" }}>
                        <div className="modal-header">
                            <h3 style={{ fontSize: "0.88rem", margin: 0 }}>{translate("mcpImportJsonTitle")}</h3>
                            <button className="btn-close" onClick={() => setShowJsonImport(false)}>×</button>
                        </div>
                        <div className="modal-body" style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
                            <div style={{ fontSize: "0.72rem", color: colors.textSecondary }}>
                                {translate("mcpImportJsonDesc")}
                            </div>
                            <pre style={{ fontSize: "0.68rem", background: colors.surfaceMuted, padding: "6px 8px", borderRadius: "4px", margin: 0, whiteSpace: "pre-wrap", color: colors.textSecondary }}>
{`{"mcpServers": {"server-name": {
  "command": "npx",
  "args": ["-y", "@scope/package"],
  "env": {"KEY": "value"},
  "auto_start": true
}}}`}
                            </pre>
                            <textarea
                                className="form-input"
                                rows={8}
                                value={jsonText}
                                onChange={(e) => setJsonText(e.target.value)}
                                placeholder={translate("mcpImportJsonPlaceholder")}
                                spellCheck={false}
                                style={{ fontFamily: "monospace", fontSize: "0.74rem", resize: "vertical" }}
                            />
                            {jsonError && (
                                <div style={{ fontSize: "0.76rem", color: colors.danger, background: "var(--theme-danger-bg)", padding: "4px 8px", borderRadius: "4px" }}>
                                    {jsonError}
                                </div>
                            )}
                        </div>
                        <div className="modal-footer">
                            <button className="btn-secondary" onClick={() => setShowJsonImport(false)} disabled={busy}>{translate("cancel")}</button>
                            <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 14px" }} onClick={handleJsonImport} disabled={busy || !jsonText.trim()}>
                                {busy ? translate("mcpImporting") : translate("mcpImport")}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {showForm && (
                <div className="modal-backdrop" {...makeBackdropProps(closeForm, backdropRef)}>
                    <div className="modal-content" onClick={(e) => e.stopPropagation()} style={{ width: "440px", textAlign: "left" }}>
                        <div className="modal-header">
                            <h3 style={{ fontSize: "0.88rem", margin: 0 }}>{editingServer ? translate("mcpEditLocalServer") : translate("mcpAddLocalServer")}</h3>
                            <button className="btn-close" onClick={closeForm}>×</button>
                        </div>
                        <div className="modal-body" style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">{translate("mcpNameLabel")}</label>
                                <input className="form-input" value={formData.name} onChange={(e) => setFormData({ ...formData, name: e.target.value })} placeholder="brave-search" spellCheck={false} />
                            </div>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">{translate("mcpCommandLabel")}</label>
                                <input className="form-input" value={formData.command} onChange={(e) => setFormData({ ...formData, command: e.target.value })} placeholder="npx" spellCheck={false} />
                            </div>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">{translate("mcpArgsLabel")}</label>
                                <textarea
                                    className="form-input"
                                    rows={3}
                                    value={argsText}
                                    onChange={(e) => setArgsText(e.target.value)}
                                    placeholder={"-y\n@modelcontextprotocol/server-brave-search"}
                                    spellCheck={false}
                                    style={{ fontFamily: "monospace", fontSize: "0.74rem", resize: "vertical" }}
                                />
                            </div>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">{translate("mcpEnvLabel")}</label>
                                {envPairs.map((pair, idx) => (
                                    <div key={idx} style={{ display: "flex", gap: "4px", marginBottom: "4px", alignItems: "center" }}>
                                        <input
                                            className="form-input"
                                            style={{ flex: 1, fontSize: "0.74rem" }}
                                            value={pair.key}
                                            onChange={(e) => {
                                                const next = [...envPairs];
                                                next[idx] = { ...next[idx], key: e.target.value };
                                                setEnvPairs(next);
                                            }}
                                            placeholder="KEY"
                                            spellCheck={false}
                                        />
                                        <span style={{ color: colors.textMuted }}>=</span>
                                        <input
                                            className="form-input"
                                            style={{ flex: 2, fontSize: "0.74rem" }}
                                            value={pair.value}
                                            onChange={(e) => {
                                                const next = [...envPairs];
                                                next[idx] = { ...next[idx], value: e.target.value };
                                                setEnvPairs(next);
                                            }}
                                            placeholder="value"
                                            spellCheck={false}
                                        />
                                        <button className="btn-secondary btn-danger" style={{ fontSize: "0.68rem", padding: "2px 6px" }} onClick={() => setEnvPairs(envPairs.filter((_, i) => i !== idx))}>×</button>
                                    </div>
                                ))}
                                <button className="btn-secondary" style={{ fontSize: "0.72rem", padding: "2px 8px" }} onClick={() => setEnvPairs([...envPairs, { key: "", value: "" }])}>
                                    {translate("mcpAddEnvVar")}
                                </button>
                            </div>
                            <label style={{ display: "flex", alignItems: "center", gap: "8px", fontSize: "0.76rem", color: colors.text }}>
                                <input
                                    type="checkbox"
                                    checked={!!formData.auto_start}
                                    onChange={(e) => setFormData({ ...formData, auto_start: e.target.checked })}
                                />
                                <span>{translate("mcpAutoStartCheckbox")}</span>
                            </label>
                            {formError && (
                                <div style={{ fontSize: "0.76rem", color: colors.danger, background: "var(--theme-danger-bg)", padding: "4px 8px", borderRadius: "4px" }}>
                                    {formError}
                                </div>
                            )}
                        </div>
                        <div className="modal-footer">
                            <button className="btn-secondary" onClick={closeForm} disabled={busy}>{translate("cancel")}</button>
                            <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 14px" }} onClick={handleSubmit} disabled={busy}>
                                {busy ? translate("mcpSubmitting") : editingServer ? translate("mcpSave") : translate("mcpAdd")}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}

function RemoteMCPPanel({ translate }: Props) {
    const [servers, setServers] = useState<MCPServerView[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);

    const [showForm, setShowForm] = useState(false);
    const [editingServer, setEditingServer] = useState<MCPServerView | null>(null);
    const [formData, setFormData] = useState<MCPServerView>({ ...emptyServer });
    const [formError, setFormError] = useState("");

    const [deleteTarget, setDeleteTarget] = useState<MCPServerView | null>(null);
    const [expandedServerID, setExpandedServerID] = useState<string | null>(null);
    const [expandedTools, setExpandedTools] = useState<MCPToolView[]>([]);
    const [toolsLoading, setToolsLoading] = useState(false);
    const [healthDetailID, setHealthDetailID] = useState<string | null>(null);
    const [showJsonImport, setShowJsonImport] = useState(false);
    const [jsonText, setJsonText] = useState("");
    const [jsonError, setJsonError] = useState("");
    const backdropRef = useRef(false);

    const loadData = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const list = await ListMCPServers();
            setServers(Array.isArray(list) ? list : []);
        } catch (err) {
            setError(String(err));
        } finally {
            setLoading(false);
        }
    }, []);

    // On mount: load servers, then auto-probe any with "unknown" status.
    useEffect(() => {
        let cancelled = false;
        (async () => {
            // Step 1: Load server list.
            let list: MCPServerView[] = [];
            try {
                const raw = await ListMCPServers();
                list = Array.isArray(raw) ? raw : [];
            } catch (err) {
                if (!cancelled) setError(String(err));
                return;
            }
            if (cancelled) return;
            setServers(list);
            setLoading(false);

            // Step 2: Find servers that need probing.
            const unknowns = list.filter((s) => !s.health_status || s.health_status === "unknown");
            if (unknowns.length === 0) return;

            // Mark them as "checking" in the UI.
            setServers((prev) =>
                prev.map((s) =>
                    !s.health_status || s.health_status === "unknown"
                        ? { ...s, health_status: "checking" as MCPServerView["health_status"] }
                        : s
                )
            );

            // Step 3: Check each server (same call as manual "Check Now").
            for (const s of unknowns) {
                if (cancelled) return;
                try {
                    await CheckMCPServerHealth(s.id);
                } catch {
                    // Backend records the failure; we'll pick it up below.
                }
            }

            // Step 4: Reload the full list with updated statuses + tools.
            if (cancelled) return;
            try {
                const updated = await ListMCPServers();
                if (!cancelled) setServers(Array.isArray(updated) ? updated : []);
            } catch {}
        })();
        return () => { cancelled = true; };
    }, []); // eslint-disable-line react-hooks/exhaustive-deps

    const openCreateForm = () => {
        setEditingServer(null);
        setFormData({ ...emptyServer });
        setFormError("");
        setShowForm(true);
    };

    const openEditForm = (server: MCPServerView) => {
        setEditingServer(server);
        setFormData({ ...server });
        setFormError("");
        setShowForm(true);
    };

    const closeForm = () => {
        setShowForm(false);
        setEditingServer(null);
        setFormError("");
    };

    const handleSubmit = async () => {
        if (!formData.name.trim()) { setFormError(translate("mcpNameRequired")); return; }
        if (!formData.endpoint_url.trim()) { setFormError(translate("mcpEndpointRequired")); return; }
        setBusy(true);
        setFormError("");
        try {
            if (editingServer) {
                await UpdateMCPServer(formData);
            } else {
                // Auto-generate id from name for new registrations
                const payload = { ...formData };
                if (!payload.id) {
                    const slug = formData.name.trim().toLowerCase().replace(/[^a-z0-9\u4e00-\u9fff]+/g, "-").replace(/^-|-$/g, "");
                    const suffix = Date.now().toString(36);
                    payload.id = slug ? `${slug}-${suffix}` : `mcp-${suffix}`;
                }
                await RegisterMCPServer(payload);
            }
            closeForm();
            await loadData();
        } catch (err) {
            setFormError(String(err));
        } finally {
            setBusy(false);
        }
    };

    const handleDelete = async (server: MCPServerView) => {
        setBusy(true);
        try {
            await UnregisterMCPServer(server.id);
            setDeleteTarget(null);
            await loadData();
        } catch (err) {
            setError(String(err));
        } finally {
            setBusy(false);
        }
    };

    const toggleTools = async (serverID: string) => {
        if (expandedServerID === serverID) {
            setExpandedServerID(null);
            setExpandedTools([]);
            return;
        }
        setExpandedServerID(serverID);
        // Use cached tools from the server view if available (populated by probe).
        const cached = servers.find((s) => s.id === serverID);
        if (cached?.tools && cached.tools.length > 0) {
            setExpandedTools(cached.tools);
            return;
        }
        // No cache — fetch from backend.
        setToolsLoading(true);
        try {
            const tools = await GetMCPServerTools(serverID);
            setExpandedTools(Array.isArray(tools) ? tools : []);
        } catch (err) {
            setExpandedTools([]);
            setError(String(err));
        } finally {
            setToolsLoading(false);
        }
    };

    const handleHealthCheck = async (serverID: string) => {
        setBusy(true);
        try {
            await CheckMCPServerHealth(serverID);
            await loadData();
            // If this server's tools are currently expanded, refresh them
            // since the health check also refreshes the backend tools cache.
            if (expandedServerID === serverID) {
                const tools = await GetMCPServerTools(serverID);
                setExpandedTools(Array.isArray(tools) ? tools : []);
            }
        } catch (err) {
            setError(String(err));
        } finally {
            setBusy(false);
        }
    };

    const toggleHealthDetail = (serverID: string) => {
        setHealthDetailID(healthDetailID === serverID ? null : serverID);
    };

    const handleJsonImport = async () => {
        setJsonError("");
        let parsed: any;
        try {
            parsed = parseRelaxedJson(jsonText);
        } catch (e: any) {
            const detail = e?.message ? `: ${e.message}` : "";
            setJsonError(translate("mcpJsonFormatError") + detail); return;
        }
        const mcpServers = parsed.mcpServers || parsed;
        if (typeof mcpServers !== "object" || Array.isArray(mcpServers)) {
            setJsonError(translate("mcpRemoteJsonStructureError"));
            return;
        }
        const entries = Object.entries(mcpServers) as [string, any][];
        if (entries.length === 0) {
            setJsonError(translate("mcpRemoteJsonStructureError"));
            return;
        }
        setBusy(true);
        try {
            const baseTime = Date.now().toString(36);
            for (let i = 0; i < entries.length; i++) {
                const [name, cfg] = entries[i];
                // Detect auth from headers or explicit fields
                let authType: MCPServerView["auth_type"] = "none";
                let authSecret = "";
                const headers = typeof cfg.headers === "object" && cfg.headers ? cfg.headers : {};
                const authHeader: string = headers["Authorization"] || headers["authorization"] || "";
                if (cfg.auth_type && cfg.auth_secret) {
                    // Explicit auth fields (MaCLaw native format)
                    authType = cfg.auth_type;
                    authSecret = cfg.auth_secret;
                } else if (authHeader) {
                    // Extract from Authorization header (Kiro / Cursor / Claude Desktop format)
                    if (authHeader.toLowerCase().startsWith("bearer ")) {
                        authType = "bearer";
                        authSecret = authHeader.slice(7).trim();
                    } else {
                        authType = "api_key";
                        authSecret = authHeader;
                    }
                }
                // Support both "url" and "endpoint_url"
                const endpointUrl = cfg.endpoint_url || cfg.url || "";
                if (!endpointUrl) {
                    throw new Error(translate("mcpRemoteJsonMissingUrl").replace("{name}", name));
                }
                // Pass through custom headers (exclude Authorization if already extracted into auth fields).
                const customHeaders: Record<string, string> = {};
                for (const [hk, hv] of Object.entries(headers)) {
                    if (typeof hv === "string" && hv) {
                        if (hk.toLowerCase() === "authorization" && authSecret) continue;
                        customHeaders[hk] = hv;
                    }
                }
                const slug = name.trim().toLowerCase().replace(/[^a-z0-9\u4e00-\u9fff]+/g, "-").replace(/^-|-$/g, "");
                const suffix = i === 0 ? baseTime : `${baseTime}${i}`;
                const payload: MCPServerView = {
                    ...emptyServer,
                    id: slug ? `${slug}-${suffix}` : `mcp-${suffix}`,
                    name,
                    endpoint_url: endpointUrl,
                    auth_type: authType,
                    auth_secret: authSecret,
                    ...(Object.keys(customHeaders).length > 0 ? { headers: customHeaders } : {}),
                };
                await RegisterMCPServer(payload);
            }
            setShowJsonImport(false);
            setJsonText("");
            await loadData();
        } catch (err) {
            setJsonError(String(err));
        } finally {
            setBusy(false);
        }
    };

    const healthColor = (status: string): string => {
        switch (status) {
            case "healthy": return "var(--theme-success)";
            case "slow": return "var(--theme-warning)";
            case "unavailable": return "var(--theme-danger)";
            case "checking": return colors.textMuted;
            default: return colors.textMuted; // "unknown"
        }
    };

    const healthBg = (status: string): string => {
        switch (status) {
            case "healthy": return "var(--theme-success-bg)";
            case "slow": return "var(--theme-warning-bg)";
            case "unavailable": return "var(--theme-danger-bg)";
            case "checking": return colors.surfaceMuted;
            default: return colors.surfaceMuted;
        }
    };

    const healthBorder = (status: string): string => {
        switch (status) {
            case "healthy": return "var(--theme-success)";
            case "slow": return "var(--theme-warning)";
            case "unavailable": return "var(--theme-danger)";
            case "checking": return colors.border;
            default: return colors.border;
        }
    };

    const healthLabel = (status: string): string => {
        switch (status) {
            case "healthy": return translate("mcpHealthy");
            case "slow": return translate("mcpSlow");
            case "unavailable": return translate("mcpUnavailable");
            case "checking": return translate("mcpChecking");
            default: return translate("mcpNotChecked"); // "unknown" → 未检测
        }
    };

    return (
        <div style={{ display: "flex", flexDirection: "column", gap: "10px" }}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                <span style={{ fontSize: "0.78rem", color: colors.textSecondary }}>
                    {servers.length} {translate("mcpServersRegistered")}
                </span>
                <div style={{ display: "flex", gap: "6px" }}>
                    <button className="btn-secondary" style={{ fontSize: "0.72rem", padding: "3px 10px" }} onClick={() => { setShowJsonImport(true); setJsonText(""); setJsonError(""); }} disabled={busy}>
                        {translate("mcpImportJson")}
                    </button>
                    <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 12px" }} onClick={openCreateForm} disabled={busy}>
                        {translate("mcpRegisterServer")}
                    </button>
                </div>
            </div>

            {loading && <div style={{ textAlign: "center", padding: "16px", fontSize: "0.78rem", color: colors.textMuted }}>{translate("mcpLoading")}</div>}
            {error && <div style={{ fontSize: "0.78rem", color: colors.danger, background: "var(--theme-danger-bg)", padding: "6px 10px", borderRadius: "4px", border: `1px solid ${colors.danger}` }}>{error}</div>}

            {!loading && servers.length > 0 && (
                <div style={{ border: `1px solid ${colors.border}`, borderRadius: "6px", overflow: "hidden" }}>
                    <table style={{ width: "100%", borderCollapse: "collapse", fontSize: "0.76rem" }}>
                        <thead>
                            <tr style={{ background: colors.surfaceMuted }}>
                                <th style={thStyle}>{translate("mcpColName")}</th>
                                <th style={{ ...thStyle, textAlign: "right" }}>{translate("mcpColHealth")}</th>
                                <th style={{ ...thStyle, textAlign: "center" }}>{translate("mcpColTools")}</th>
                                <th style={{ ...thStyle, width: "140px" }}>{translate("mcpColActions")}</th>
                            </tr>
                        </thead>
                        <tbody>
                            {servers.map((s) => (
                                <ServerRow
                                    key={s.id}
                                    server={s}
                                    busy={busy}
                                    expandedServerID={expandedServerID}
                                    expandedTools={expandedTools}
                                    toolsLoading={toolsLoading}
                                    healthDetailID={healthDetailID}
                                    onEdit={() => openEditForm(s)}
                                    onDelete={() => setDeleteTarget(s)}
                                    onToggleTools={() => toggleTools(s.id)}
                                    onHealthCheck={() => handleHealthCheck(s.id)}
                                    onToggleHealthDetail={() => toggleHealthDetail(s.id)}
                                    healthColor={healthColor}
                                    healthBg={healthBg}
                                    healthBorder={healthBorder}
                                    healthLabel={healthLabel}
                                    translate={translate}
                                />
                            ))}
                        </tbody>
                    </table>
                </div>
            )}

            {!loading && servers.length === 0 && !error && (
                <div style={{ textAlign: "center", padding: "20px", fontSize: "0.78rem", color: colors.textMuted }}>
                    {translate("mcpNoRemoteServers")}
                </div>
            )}

            {deleteTarget && (
                <div className="modal-backdrop" {...makeBackdropProps(() => setDeleteTarget(null), backdropRef)}>
                    <div className="modal-content" onClick={(e) => e.stopPropagation()} style={{ width: "280px" }}>
                        <div className="modal-header">
                            <h3 style={{ fontSize: "0.88rem", margin: 0 }}>{translate("mcpConfirmDelete")}</h3>
                            <button className="btn-close" onClick={() => setDeleteTarget(null)}>×</button>
                        </div>
                        <div className="modal-body">
                            <p style={{ fontSize: "0.8rem", color: colors.textSecondary, margin: 0 }}>
                                {translate("mcpConfirmDeleteRemote").replace("{name}", deleteTarget.name)}
                            </p>
                        </div>
                        <div className="modal-footer">
                            <button className="btn-secondary" onClick={() => setDeleteTarget(null)} disabled={busy}>{translate("cancel")}</button>
                            <button className="btn-secondary btn-danger" onClick={() => handleDelete(deleteTarget)} disabled={busy}>
                                {busy ? translate("mcpDeleting") : translate("mcpDelete")}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {showForm && (
                <div className="modal-backdrop" {...makeBackdropProps(closeForm, backdropRef)}>
                    <div className="modal-content" onClick={(e) => e.stopPropagation()} style={{ width: "420px", textAlign: "left" }}>
                        <div className="modal-header">
                            <h3 style={{ fontSize: "0.88rem", margin: 0 }}>{editingServer ? translate("mcpEditServer") : translate("mcpRegisterServerTitle")}</h3>
                            <button className="btn-close" onClick={closeForm}>×</button>
                        </div>
                        <div className="modal-body" style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">{translate("mcpNameLabel")}</label>
                                <input className="form-input" value={formData.name} onChange={(e) => setFormData({ ...formData, name: e.target.value })} placeholder="my-mcp-server" spellCheck={false} />
                            </div>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">{translate("mcpEndpointLabel")}</label>
                                <input className="form-input" value={formData.endpoint_url} onChange={(e) => setFormData({ ...formData, endpoint_url: e.target.value })} placeholder="https://mcp.example.com/v1" spellCheck={false} />
                            </div>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">{translate("mcpAuthType")}</label>
                                <select className="form-input" value={formData.auth_type} onChange={(e) => setFormData({ ...formData, auth_type: e.target.value as MCPServerView["auth_type"] })}>
                                    <option value="none">{translate("mcpAuthNone")}</option>
                                    <option value="api_key">{translate("mcpAuthApiKey")}</option>
                                    <option value="bearer">{translate("mcpAuthBearer")}</option>
                                </select>
                            </div>
                            {formData.auth_type !== "none" && (
                                <div className="form-group" style={{ marginBottom: 0 }}>
                                    <label className="form-label">{formData.auth_type === "api_key" ? translate("mcpAuthApiKey") : translate("mcpAuthBearer")}</label>
                                    <input className="form-input" type="password" value={formData.auth_secret} onChange={(e) => setFormData({ ...formData, auth_secret: e.target.value })} placeholder={formData.auth_type === "api_key" ? translate("mcpEnterApiKey") : translate("mcpEnterBearer")} spellCheck={false} />
                                </div>
                            )}
                            {formError && (
                                <div style={{ fontSize: "0.76rem", color: colors.danger, background: "var(--theme-danger-bg)", padding: "4px 8px", borderRadius: "4px" }}>{formError}</div>
                            )}
                        </div>
                        <div className="modal-footer">
                            <button className="btn-secondary" onClick={closeForm} disabled={busy}>{translate("cancel")}</button>
                            <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 14px" }} onClick={handleSubmit} disabled={busy}>
                                {busy ? translate("mcpSubmitting") : editingServer ? translate("mcpSave") : translate("mcpRegister")}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {showJsonImport && (
                <div className="modal-backdrop" {...makeBackdropProps(() => setShowJsonImport(false), backdropRef)}>
                    <div className="modal-content" onClick={(e) => e.stopPropagation()} style={{ width: "500px", textAlign: "left" }}>
                        <div className="modal-header">
                            <h3 style={{ fontSize: "0.88rem", margin: 0 }}>{translate("mcpRemoteImportJsonTitle")}</h3>
                            <button className="btn-close" onClick={() => setShowJsonImport(false)}>×</button>
                        </div>
                        <div className="modal-body" style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
                            <div style={{ fontSize: "0.72rem", color: colors.textSecondary }}>
                                {translate("mcpRemoteImportJsonDesc")}
                            </div>
                            <pre style={{ fontSize: "0.68rem", background: colors.surfaceMuted, padding: "6px 8px", borderRadius: "4px", margin: 0, whiteSpace: "pre-wrap", color: colors.textSecondary }}>
{`{
  "mcpServers": {
    "server-name": {
      "type": "streamableHttp",
      "url": "https://api.example.com/mcp",
      "headers": {
        "Authorization": "Bearer sk-xxx"
      }
    }
  }
}`}
                            </pre>
                            <textarea
                                className="form-input"
                                rows={10}
                                value={jsonText}
                                onChange={(e) => setJsonText(e.target.value)}
                                placeholder={translate("mcpImportJsonPlaceholder")}
                                spellCheck={false}
                                style={{ fontFamily: "monospace", fontSize: "0.74rem", resize: "vertical" }}
                            />
                            {jsonError && (
                                <div style={{ fontSize: "0.76rem", color: colors.danger, background: "var(--theme-danger-bg)", padding: "4px 8px", borderRadius: "4px" }}>{jsonError}</div>
                            )}
                        </div>
                        <div className="modal-footer">
                            <button className="btn-secondary" onClick={() => setShowJsonImport(false)} disabled={busy}>{translate("cancel")}</button>
                            <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 14px" }} onClick={handleJsonImport} disabled={busy || !jsonText.trim()}>
                                {busy ? translate("mcpImporting") : translate("mcpImport")}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}

function ServerRow({
    server,
    busy,
    expandedServerID,
    expandedTools,
    toolsLoading,
    healthDetailID,
    onEdit,
    onDelete,
    onToggleTools,
    onHealthCheck,
    onToggleHealthDetail,
    healthColor,
    healthBg,
    healthBorder,
    healthLabel,
    translate,
}: {
    server: MCPServerView;
    busy: boolean;
    expandedServerID: string | null;
    expandedTools: MCPToolView[];
    toolsLoading: boolean;
    healthDetailID: string | null;
    onEdit: () => void;
    onDelete: () => void;
    onToggleTools: () => void;
    onHealthCheck: () => void;
    onToggleHealthDetail: () => void;
    healthColor: (s: string) => string;
    healthBg: (s: string) => string;
    healthBorder: (s: string) => string;
    healthLabel: (s: string) => string;
    translate: (key: string) => string;
}) {
    const isExpanded = expandedServerID === server.id;
    const showHealthDetail = healthDetailID === server.id;
    const toolCount = server.tools ? server.tools.length : 0;
    const toolCountDisplay = server.health_status === "checking" ? "…" : String(toolCount);

    return (
        <>
            <tr style={{ borderTop: `1px solid ${colors.border}` }}>
                <td style={tdStyle} title={server.endpoint_url}>
                    <span style={{ cursor: "default", borderBottom: `1px dashed ${colors.textMuted}`, paddingBottom: "1px" }}>{server.name}</span>
                </td>
                <td style={{ ...tdStyle, textAlign: "right" }}>
                    <span
                        style={{
                            ...statusBadgeStyle,
                            background: healthBg(server.health_status),
                            color: healthColor(server.health_status),
                            border: `1px solid ${healthBorder(server.health_status)}`,
                            cursor: "pointer",
                        }}
                        onClick={onToggleHealthDetail}
                        title={translate("mcpHealthRecord")}
                    >
                        {server.health_status === "checking" ? "◌" : "●"} {healthLabel(server.health_status)}
                    </span>
                </td>
                <td style={{ ...tdStyle, textAlign: "center" }}>{toolCountDisplay}</td>
                <td style={tdStyle}>
                    <div style={{ display: "flex", gap: "4px", flexWrap: "wrap" }}>
                        <button className="btn-secondary" style={smallBtnStyle} onClick={onToggleTools} disabled={busy}>
                            {isExpanded ? translate("mcpCollapse") : translate("mcpTools")}
                        </button>
                        <button className="btn-secondary" style={smallBtnStyle} onClick={onEdit} disabled={busy}>{translate("mcpEdit")}</button>
                        <button className="btn-secondary btn-danger" style={smallBtnStyle} onClick={onDelete} disabled={busy}>{translate("mcpDelete")}</button>
                    </div>
                </td>
            </tr>

            {showHealthDetail && (
                <tr>
                    <td colSpan={4} style={{ padding: "6px 8px", background: colors.surfaceMuted, borderTop: `1px solid ${colors.border}` }}>
                        <div style={{ fontSize: "0.72rem", color: colors.textSecondary }}>
                            <div style={{ fontWeight: 600, marginBottom: "4px" }}>{translate("mcpHealthRecord")}</div>
                            <div style={{ display: "flex", gap: "6px", alignItems: "center", flexWrap: "wrap" }}>
                                <span>{translate("mcpHealthStatus")}: <span style={{ color: healthColor(server.health_status), fontWeight: 600 }}>{healthLabel(server.health_status)}</span></span>
                                <span>·</span>
                                <span>{translate("mcpFailCount")}: {server.fail_count}</span>
                                <span>·</span>
                                <span>{translate("mcpLastCheck")}: {server.last_check_at ? new Date(server.last_check_at).toLocaleString() : "—"}</span>
                                <button className="btn-secondary" style={{ ...smallBtnStyle, marginLeft: "8px" }} onClick={onHealthCheck} disabled={busy}>
                                    {translate("mcpCheckNow")}
                                </button>
                            </div>
                        </div>
                    </td>
                </tr>
            )}

            {isExpanded && (
                <tr>
                    <td colSpan={4} style={{ padding: "6px 8px", background: colors.surfaceMuted, borderTop: `1px solid ${colors.border}` }}>
                        {toolsLoading ? (
                            <div style={{ fontSize: "0.74rem", color: colors.textMuted, padding: "4px 0" }}>{translate("mcpLoadingTools")}</div>
                        ) : expandedTools.length > 0 ? (
                            <div style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
                                <div style={{ fontSize: "0.72rem", fontWeight: 600, color: colors.textSecondary, marginBottom: "2px" }}>
                                    {translate("mcpToolList")} ({expandedTools.length})
                                </div>
                                {expandedTools.map((tool) => (
                                    <div key={tool.name} style={{ background: colors.surface, border: `1px solid ${colors.border}`, borderRadius: "4px", padding: "4px 8px" }}>
                                        <div style={{ fontSize: "0.74rem", fontWeight: 600, color: colors.text }}>{tool.name}</div>
                                        <div style={{ fontSize: "0.7rem", color: colors.textSecondary }}>{tool.description || translate("mcpNoDescription")}</div>
                                    </div>
                                ))}
                            </div>
                        ) : (
                            <div style={{ fontSize: "0.74rem", color: colors.textMuted, padding: "4px 0" }}>{translate("mcpNoTools")}</div>
                        )}
                    </td>
                </tr>
            )}
        </>
    );
}

const thStyle: CSSProperties = {
    padding: "6px 8px",
    textAlign: "left",
    fontWeight: 600,
    fontSize: "0.74rem",
    color: colors.textSecondary,
    borderBottom: `1px solid ${colors.border}`,
};

const tdStyle: CSSProperties = {
    padding: "6px 8px",
    fontSize: "0.76rem",
    color: colors.text,
    verticalAlign: "top",
};

const statusBadgeStyle: CSSProperties = {
    display: "inline-block",
    padding: "1px 8px",
    borderRadius: "999px",
    fontSize: "0.68rem",
    fontWeight: 600,
};

const smallBtnStyle: CSSProperties = {
    fontSize: "0.72rem",
    padding: "2px 8px",
};
