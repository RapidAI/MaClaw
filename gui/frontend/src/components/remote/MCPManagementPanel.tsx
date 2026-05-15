import { useState, useEffect, useCallback, useRef } from "react";
import type { CSSProperties, MouseEvent as ReactMouseEvent } from "react";
import { colors, radius } from "./styles";
import { MCPMarketplacePanel } from "./MCPMarketplacePanel";
import { parseRelaxedJson } from "./MCPJsonImportParser";
import { MCPSecretRequirementsNotice } from "./MCPSecretRequirementsNotice";
import { MCPSecretConfigurationEditor } from "./MCPSecretConfigurationEditor";
import { MCPRemoteServerRow } from "./MCPRemoteServerRow";
import type { HubMCPSecretRequirement } from "./MCPSecretRequirementsNotice";
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
    GetHubMCPSecretRequirements,
    GetHubMCPHubSecrets,
    GetHubMCPSecretBindings,
    SaveHubMCPHubSecret,
    SaveHubMCPSecretBinding,
} from "../../../wailsjs/go/main/App";
interface MCPToolView {
    name: string;
    description: string;
    input_schema: Record<string, any>;
}
interface MCPServerCapabilityRef {
    capability_id: string;
    version_key?: string;
    source?: string;
    global_key?: string;
}
interface MCPServerView {
    id: string;
    name: string;
    endpoint_url: string;
    auth_type: "none" | "api_key" | "bearer";
    auth_secret: string;
    headers?: Record<string, string>; // custom HTTP headers
    capability?: MCPServerCapabilityRef;
    tools: MCPToolView[];
    health_status: "healthy" | "slow" | "unavailable" | "unknown" | "checking";
    fail_count: number;
    last_check_at: string;
    created_at: string;
    source?: "manual" | "mdns" | "project" | "marketplace"; managed?: boolean;
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
    source?: "manual" | "marketplace";
    capability?: MCPServerCapabilityRef;
    managed?: boolean;
}
type Props = {
    translate: (key: string) => string;
};
type MCPTab = "local" | "remote" | "marketplace";
type MCPSecretStatus = "configured" | "needs_config" | "optional";
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
export function MCPManagementPanel({ translate }: Props) {
    const [activeTab, setActiveTab] = useState<MCPTab>("remote");
    const [installedCapabilityIDs, setInstalledCapabilityIDs] = useState<string[]>([]);
    const refreshInstalledCapabilities = useCallback(async () => {
        try {
            const [remoteServers, localServers] = await Promise.all([
                ListMCPServers().catch(() => []),
                ListLocalMCPServers().catch(() => []),
            ]);
            const ids: string[] = [];
            if (Array.isArray(remoteServers)) {
                for (const s of remoteServers as MCPServerView[]) {
                    if (s.capability?.capability_id) ids.push(s.capability.capability_id);
                    if (s.capability?.global_key) ids.push(s.capability.global_key);
                    if (s.id) ids.push(s.id);
                }
            }
            if (Array.isArray(localServers)) {
                for (const s of localServers as LocalMCPServer[]) {
                    if (s.capability?.capability_id) ids.push(s.capability.capability_id);
                    if (s.capability?.global_key) ids.push(s.capability.global_key);
                    if (s.id) ids.push(s.id);
                }
            }
            setInstalledCapabilityIDs(ids.filter(Boolean));
        } catch {
            // ignore
        }
    }, []);
    useEffect(() => { void refreshInstalledCapabilities(); }, [refreshInstalledCapabilities]);
    const handleMarketplaceChanged = useCallback(async () => { await refreshInstalledCapabilities(); }, [refreshInstalledCapabilities]);
    return (
        <div style={{ display: "flex", flexDirection: "column", gap: "10px" }}>
            <div style={{ display: "flex", borderBottom: `1px solid ${colors.border}` }}>
                <button style={activeTab === "local" ? tabActiveStyle : tabStyle} onClick={() => setActiveTab("local")}>{translate("mcpTabLocal")}</button>
                <button style={activeTab === "remote" ? tabActiveStyle : tabStyle} onClick={() => setActiveTab("remote")}>{translate("mcpTabRemote")}</button>
                <button style={activeTab === "marketplace" ? tabActiveStyle : tabStyle} onClick={() => setActiveTab("marketplace")}>{translate("mcpTabMarketplace")}</button>
            </div>
            {activeTab === "local" && <LocalMCPPanel translate={translate} />}
            {activeTab === "remote" && <RemoteMCPPanel translate={translate} />}
            {activeTab === "marketplace" && <MCPMarketplacePanel translate={translate} onChanged={handleMarketplaceChanged} installedCapabilities={installedCapabilityIDs} />}
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
                                    {s.managed ? (
                                        <span style={{ display: "inline-flex", alignItems: "center", gap: "2px", padding: "1px 8px", borderRadius: "999px", fontSize: "0.68rem", fontWeight: 600, color: colors.textMuted, border: `1px solid ${colors.border}`, background: colors.surfaceMuted, cursor: "default" }} title={translate("mcpCannotDeleteManaged")}>🔒 {translate("mcpManagedLabel")}</span>
                                    ) : (
                                        <button className="btn-secondary btn-danger" style={smallBtnStyle} onClick={() => setDeleteTarget(s)} disabled={busy}>{translate("mcpDelete")}</button>
                                    )}
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
                            <button className="btn-close" onClick={() => setDeleteTarget(null)}>&times;</button>
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
                            <button className="btn-close" onClick={() => setShowJsonImport(false)}>&times;</button>
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
                            <button className="btn-close" onClick={closeForm}>&times;</button>
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
                                        <button className="btn-secondary btn-danger" style={{ fontSize: "0.68rem", padding: "2px 6px" }} onClick={() => setEnvPairs(envPairs.filter((_, i) => i !== idx))}>&times;</button>
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
    const [headerPairs, setHeaderPairs] = useState<{ key: string; value: string }[]>([]);
    const [secretRequirements, setSecretRequirements] = useState<HubMCPSecretRequirement[]>([]);
    const [secretInputs, setSecretInputs] = useState<Record<string, { storage: "hub" | "local"; value: string; configured?: boolean }>>({});
    const [secretStatusMap, setSecretStatusMap] = useState<Record<string, MCPSecretStatus>>({});
    const [deleteTarget, setDeleteTarget] = useState<MCPServerView | null>(null);
    const [expandedServerID, setExpandedServerID] = useState<string | null>(null);
    const [expandedTools, setExpandedTools] = useState<MCPToolView[]>([]);
    const [toolsLoading, setToolsLoading] = useState(false);
    const [healthDetailID, setHealthDetailID] = useState<string | null>(null);
    const [showJsonImport, setShowJsonImport] = useState(false);
    const [jsonText, setJsonText] = useState("");
    const [jsonError, setJsonError] = useState("");
    const backdropRef = useRef(false);
    const refreshSecretStatuses = useCallback(async (list: MCPServerView[]) => {
        const marketServers = list.filter((s) => s.capability?.capability_id);
        if (marketServers.length === 0) {
            setSecretStatusMap({});
            return;
        }
        const pairs = await Promise.all(marketServers.map(async (server): Promise<[string, MCPSecretStatus | undefined]> => {
            try {
                const requirements = await GetHubMCPSecretRequirements(server.capability!.capability_id, server.capability!.version_key || "");
                const required = Array.isArray(requirements) ? requirements.filter((req: HubMCPSecretRequirement) => req.required) : [];
                if (required.length === 0) return [server.id, "optional"];
                const [hubSecrets, bindings] = await Promise.all([
                    GetHubMCPHubSecrets(server.id).catch(() => []),
                    GetHubMCPSecretBindings(server.id).catch(() => []),
                ]);
                const configured = new Set<string>();
                if (Array.isArray(hubSecrets)) {
                    for (const item of hubSecrets as any[]) {
                        if (item.requirement_name && item.secret_digest) {
                            const req = required.find((candidate) => candidate.name === item.requirement_name);
                            if (secretStorageAllowed("hub", req?.storage_policy)) configured.add(item.requirement_name);
                        }
                    }
                }
                if (Array.isArray(bindings)) {
                    for (const binding of bindings as any[]) {
                        if (!binding.requirement_name) continue;
                        const req = required.find((candidate) => candidate.name === binding.requirement_name);
                        const storage = binding.storage === "local" ? "local" : "hub";
                        if (storage === "local" && secretStorageAllowed("local", req?.storage_policy) && binding.local_secret_ref && server.auth_secret) {
                            configured.add(binding.requirement_name);
                        }
                    }
                }
                return [server.id, required.every((req) => configured.has(req.name)) ? "configured" : "needs_config"];
            } catch {
                return [server.id, undefined];
            }
        }));
        const next: Record<string, MCPSecretStatus> = {};
        for (const [id, status] of pairs) {
            if (status) next[id] = status;
        }
        setSecretStatusMap(next);
    }, []);
    const loadServerList = useCallback(async (): Promise<MCPServerView[]> => {
        const list = await ListMCPServers();
        const normalized = Array.isArray(list) ? list : [];
        setServers(normalized);
        void refreshSecretStatuses(normalized);
        return normalized;
    }, [refreshSecretStatuses]);
    const loadData = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            await loadServerList();
        } catch (err) {
            setError(String(err));
        } finally {
            setLoading(false);
        }
    }, [loadServerList]);
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
            void refreshSecretStatuses(list);
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
                if (!cancelled) {
                    const normalized = Array.isArray(updated) ? updated : [];
                    setServers(normalized);
                    void refreshSecretStatuses(normalized);
                }
            } catch {}
        })();
        return () => { cancelled = true; };
    }, [refreshSecretStatuses]);
    const openCreateForm = () => {
        setEditingServer(null);
        setFormData({ ...emptyServer });
        setHeaderPairs([]);
        setSecretRequirements([]);
        setSecretInputs({});
        setFormError("");
        setShowForm(true);
    };
    const openEditForm = (server: MCPServerView) => {
        setEditingServer(server);
        setFormData({ ...server });
        setHeaderPairs(Object.entries(server.headers || {}).map(([key, value]) => ({ key, value })));
        setSecretRequirements([]);
        setFormError("");
        setShowForm(true);
        if (server.capability?.capability_id) {
            GetHubMCPSecretRequirements(server.capability.capability_id, server.capability.version_key || "")
                .then(async (items) => {
                    const requirements = Array.isArray(items) ? items : [];
                    setSecretRequirements(requirements);
                    const next: Record<string, { storage: "hub" | "local"; value: string; configured?: boolean }> = {};
                    for (const req of requirements) {
                        const policy = req.storage_policy || "hub_or_local";
                        next[req.name] = { storage: policy === "local" ? "local" : "hub", value: "" };
                    }
                    try {
                        const existing = await GetHubMCPHubSecrets(server.id);
                        if (Array.isArray(existing)) {
                            for (const item of existing as any[]) {
                                if (item.requirement_name && item.secret_digest && next[item.requirement_name]) {
                                    const req = requirements.find((candidate) => candidate.name === item.requirement_name);
                                    if (secretStorageAllowed("hub", req?.storage_policy)) {
                                        next[item.requirement_name] = { ...next[item.requirement_name], storage: "hub", configured: true };
                                    }
                                }
                            }
                        }
                    } catch {}
                    try {
                        const bindings = await GetHubMCPSecretBindings(server.id);
                        if (Array.isArray(bindings)) {
                            for (const binding of bindings as any[]) {
                                if (binding.requirement_name && next[binding.requirement_name]) {
                                    const req = requirements.find((candidate) => candidate.name === binding.requirement_name);
                                    const bindingStorage = binding.storage === "local" ? "local" : "hub";
                                    const storage = normalizeSecretStorage(bindingStorage, req?.storage_policy);
                                    const configured = storage === "local" ? bindingStorage === "local" && !!server.auth_secret : false;
                                    next[binding.requirement_name] = { ...next[binding.requirement_name], storage, configured };
                                }
                            }
                        }
                    } catch {}
                    setSecretInputs(next);
                })
                .catch(() => { setSecretRequirements([]); setSecretInputs({}); });
        }
    };
    const closeForm = () => {
        setShowForm(false);
        setEditingServer(null);
        setFormError("");
    };
    const normalizeSecretStorage = (storage: "hub" | "local", policy?: string): "hub" | "local" => {
        if (policy === "local") return "local";
        if (policy === "hub") return "hub";
        return storage;
    };
    const secretStorageAllowed = (storage: "hub" | "local", policy?: string): boolean => {
        return normalizeSecretStorage(storage, policy) === storage;
    };
    const validateMarketplaceSecrets = (): string => {
        if (!editingServer?.capability || secretRequirements.length === 0) return "";
        for (const req of secretRequirements) {
            if (!req.required) continue;
            const input = secretInputs[req.name];
            const policy = req.storage_policy || "hub_or_local";
            const hasNewValue = !!input?.value.trim();
            const alreadyConfigured = !!input?.configured;
            const hasLocalAuthSecret = secretStorageAllowed("local", policy) && !!formData.auth_secret.trim();
            if (!hasNewValue && !alreadyConfigured && !hasLocalAuthSecret) {
                return `${translate("mcpSecretRequired")}: ${req.label || req.name}`;
            }
        }
        return "";
    };
    const applyLocalMarketplaceSecretsToServer = (server: MCPServerView): MCPServerView => {
        if (!editingServer?.capability || secretRequirements.length === 0) return server;
        let next = { ...server };
        for (const req of secretRequirements) {
            const input = secretInputs[req.name];
            if (!input || input.storage !== "local" || !input.value.trim()) continue;
            if (!next.auth_secret.trim()) {
                next.auth_secret = input.value.trim();
                if (next.auth_type === "none") {
                    const name = `${req.name} ${req.label || ""}`.toLowerCase();
                    next.auth_type = name.includes("bearer") || name.includes("token") ? "bearer" : "api_key";
                }
            }
            break;
        }
        return next;
    };
    const saveMarketplaceSecrets = async (serverID: string) => {
        if (!editingServer?.capability || secretRequirements.length === 0) return;
        for (const req of secretRequirements) {
            const input = secretInputs[req.name];
            if (!input) continue;
            if (input.storage === "hub") {
                if (input.value.trim()) {
                    await SaveHubMCPHubSecret({
                        mcp_server_id: serverID,
                        requirement_name: req.name,
                        secret_value: input.value,
                        metadata: { capability_id: editingServer.capability.capability_id, version_key: editingServer.capability.version_key || "" },
                    });
                    continue;
                }
                if (!secretStorageAllowed("local", req.storage_policy) || !formData.auth_secret.trim()) continue;
                await SaveHubMCPSecretBinding({
                    mcp_server_id: serverID,
                    requirement_name: req.name,
                    storage: "local",
                    local_secret_ref: `mcp:${serverID}:${req.name}`,
                    status: "configured",
                });
            } else {
                const hasLocalSecret = !!input.value.trim() || !!formData.auth_secret.trim() || !!input.configured;
                if (!hasLocalSecret && !req.required) continue;
                await SaveHubMCPSecretBinding({
                    mcp_server_id: serverID,
                    requirement_name: req.name,
                    storage: "local",
                    local_secret_ref: `mcp:${serverID}:${req.name}`,
                    status: hasLocalSecret ? "configured" : "needs_config",
                });
            }
        }
    };
    const handleSubmit = async () => {
        if (!formData.name.trim()) { setFormError(translate("mcpNameRequired")); return; }
        if (!formData.endpoint_url.trim()) { setFormError(translate("mcpEndpointRequired")); return; }
        const secretError = validateMarketplaceSecrets();
        if (secretError) { setFormError(secretError); return; }
        setBusy(true);
        setFormError("");
        // Build headers from headerPairs, skipping empty keys
        const headers: Record<string, string> = {};
        for (const p of headerPairs) {
            if (p.key.trim()) headers[p.key.trim()] = p.value;
        }
        const cleanedData = applyLocalMarketplaceSecretsToServer({ ...formData, headers: Object.keys(headers).length > 0 ? headers : undefined });
        try {
            if (editingServer) {
                await UpdateMCPServer(cleanedData);
                await saveMarketplaceSecrets(cleanedData.id);
            } else {
                // Auto-generate id from name for new registrations
                const payload = { ...cleanedData };
                if (!payload.id) {
                    const slug = cleanedData.name.trim().toLowerCase().replace(/[^a-z0-9\u4e00-\u9fff]+/g, "-").replace(/^-|-$/g, "");
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
        // No cache; fetch from backend.
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
            default: return translate("mcpNotChecked"); // "unknown"
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
                                <MCPRemoteServerRow
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
                                    secretStatus={secretStatusMap[s.id]}
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
                            <button className="btn-close" onClick={() => setDeleteTarget(null)}>&times;</button>
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
                            <button className="btn-close" onClick={closeForm}>&times;</button>
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
                            {editingServer?.capability && <MCPSecretRequirementsNotice requirements={secretRequirements} translate={translate} />}
                            {editingServer?.capability && (
                                <MCPSecretConfigurationEditor requirements={secretRequirements} inputs={secretInputs} onChange={setSecretInputs} translate={translate} />
                            )}
                            {formData.auth_type !== "none" && (
                                <div className="form-group" style={{ marginBottom: 0 }}>
                                    <label className="form-label">{formData.auth_type === "api_key" ? translate("mcpAuthApiKey") : translate("mcpAuthBearer")}</label>
                                    <input className="form-input" type="password" value={formData.auth_secret} onChange={(e) => setFormData({ ...formData, auth_secret: e.target.value })} placeholder={formData.auth_type === "api_key" ? translate("mcpEnterApiKey") : translate("mcpEnterBearer")} spellCheck={false} />
                                </div>
                            )}
                            {/* Custom Headers editor */}
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label" style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                                    <span>{translate("mcpCustomHeaders")}</span>
                                    <button
                                        type="button"
                                        className="btn-secondary"
                                        style={{ fontSize: "0.68rem", padding: "1px 8px", lineHeight: "1.6" }}
                                        onClick={() => setHeaderPairs([...headerPairs, { key: "", value: "" }])}
                                    >
                                        + {translate("mcpAddHeader")}
                                    </button>
                                </label>
                                {headerPairs.length > 0 ? (
                                    <div style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
                                        {headerPairs.map((pair, idx) => (
                                            <div key={idx} style={{ display: "flex", gap: "4px", alignItems: "center" }}>
                                                <input
                                                    className="form-input"
                                                    style={{ flex: 1, fontSize: "0.74rem" }}
                                                    value={pair.key}
                                                    onChange={(e) => {
                                                        const next = [...headerPairs];
                                                        next[idx] = { ...next[idx], key: e.target.value };
                                                        setHeaderPairs(next);
                                                    }}
                                                    placeholder="Header-Name"
                                                    spellCheck={false}
                                                />
                                                <input
                                                    className="form-input"
                                                    style={{ flex: 1.5, fontSize: "0.74rem" }}
                                                    value={pair.value}
                                                    onChange={(e) => {
                                                        const next = [...headerPairs];
                                                        next[idx] = { ...next[idx], value: e.target.value };
                                                        setHeaderPairs(next);
                                                    }}
                                                    placeholder="value"
                                                    spellCheck={false}
                                                />
                                                <button
                                                    type="button"
                                                    className="btn-secondary btn-danger"
                                                    style={{ fontSize: "0.68rem", padding: "2px 6px", lineHeight: "1.4", flexShrink: 0 }}
                                                    onClick={() => setHeaderPairs(headerPairs.filter((_, i) => i !== idx))}
                                                >
                                                    &times;
                                                </button>
                                            </div>
                                        ))}
                                    </div>
                                ) : (
                                    <div style={{ fontSize: "0.72rem", color: colors.textMuted, padding: "4px 0" }}>
                                        {translate("mcpNoCustomHeaders")}
                                    </div>
                                )}
                            </div>
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
                            <button className="btn-close" onClick={() => setShowJsonImport(false)}>&times;</button>
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
const thStyle: CSSProperties = {
    padding: "6px 8px",
    textAlign: "left",
    fontWeight: 600,
    fontSize: "0.74rem",
    color: colors.textSecondary,
    borderBottom: `1px solid ${colors.border}`,
};
const smallBtnStyle: CSSProperties = {
    fontSize: "0.72rem",
    padding: "2px 8px",
};

