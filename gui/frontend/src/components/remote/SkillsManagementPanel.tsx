import { useState, useEffect, useCallback, useMemo } from "react";
import {
    ListNLSkills,
    CreateNLSkill,
    UpdateNLSkill,
    DeleteNLSkill,
    ImportNLSkillZip,
    SearchSkillHub,
    InstallHubSkill,
    CheckHubSkillUpdates,
    UpdateHubSkill,
    ExportLearnedSkillsZip,
    ImportLearnedSkillsZip,
    UploadNLSkillToMarket,
    DiagnoseSkillFiles,
} from "../../../wailsjs/go/main/App";

interface NLSkillStep {
    action: string;
    params: Record<string, any>;
    on_error: string;
}

interface NLSkillDefinition {
    name: string;
    description: string;
    triggers: string[];
    steps: NLSkillStep[];
    status: string;
    created_at: string;
    source?: string;
    hub_skill_id?: string;
    hub_version?: string;
    trust_level?: string;
    usage_count?: number;
    success_count?: number;
    success_rate?: number;
    last_used_at?: string;
    last_error?: string;
}

interface HubSkillUpdateInfo {
    skill_name: string;
    current_version: string;
    latest_version: string;
    hub_url: string;
}

interface HubSkillMeta {
    id: string;
    name: string;
    description: string;
    tags: string[];
    version: string;
    author: string;
    trust_level: string;
    downloads: number;
    hub_url: string;
    avg_rating: number;
    rating_count: number;
}

type Props = {
    localizeText: (en: string, zhHans: string, zhHant: string) => string;
};

interface SkillDiagEntry {
    dir: string;
    name: string;
    ok: boolean;
    reason?: string;
}

const emptySkill: NLSkillDefinition = {
    name: "",
    description: "",
    triggers: [],
    steps: [],
    status: "active",
    created_at: "",
};

// Localize backend error messages for skill operations.
function makeLocalizeHubError(localizeText: Props["localizeText"]) {
    const patterns: Array<{ re: RegExp; fn: (m: RegExpMatchArray) => string }> = [
        {
            re: /skill "([^"]+)" already exists/,
            fn: (m) => localizeText(
                `skill "${m[1]}" already exists`,
                `技能「${m[1]}」已存在`,
                `技能「${m[1]}」已存在`,
            ),
        },
        {
            re: /skill name is required/,
            fn: () => localizeText("Skill name is required", "技能名称不能为空", "技能名稱不能為空"),
        },
        {
            re: /skill hub client not initialized/,
            fn: () => localizeText("Skill Hub client not initialized", "技能中心客户端未初始化", "技能中心客戶端未初始化"),
        },
        {
            re: /skill executor not initialized/,
            fn: () => localizeText("Skill executor not initialized", "技能执行器未初始化", "技能執行器未初始化"),
        },
        {
            re: /machine not registered/,
            fn: () => localizeText("Machine not registered", "设备未注册", "裝置未註冊"),
        },
    ];
    return (msg: string): string => {
        for (const p of patterns) {
            const m = msg.match(p.re);
            if (m) return p.fn(m);
        }
        return msg;
    };
}

export function SkillsManagementPanel({ localizeText }: Props) {
    const [activeTab, setActiveTab] = useState<"local" | "hub" | "learned">("local");
    const [skills, setSkills] = useState<NLSkillDefinition[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);

    // Hub market state
    const [hubSearchQuery, setHubSearchQuery] = useState("");
    const [hubResults, setHubResults] = useState<HubSkillMeta[]>([]);
    const [hubSearching, setHubSearching] = useState(false);
    const [hubError, setHubError] = useState("");
    const [hubSearched, setHubSearched] = useState(false);

    // Localize backend error messages
    const localizeHubError = useMemo(() => makeLocalizeHubError(localizeText), [localizeText]);

    // Install/update state
    const [installingSkills, setInstallingSkills] = useState<string[]>([]);
    const [updatingSkills, setUpdatingSkills] = useState<string[]>([]);
    const [hubUpdates, setHubUpdates] = useState<HubSkillUpdateInfo[]>([]);

    // Form state
    const [showForm, setShowForm] = useState(false);
    const [editingSkill, setEditingSkill] = useState<NLSkillDefinition | null>(null);
    const [formData, setFormData] = useState<NLSkillDefinition>({ ...emptySkill });
    const [triggerInput, setTriggerInput] = useState("");
    const [stepsYaml, setStepsYaml] = useState("");
    const [formError, setFormError] = useState("");

    // Delete confirmation
    const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
    const [importing, setImporting] = useState(false);

    // Toast message state (replaces system alert)
    const [toastMsg, setToastMsg] = useState<{ type: "success" | "error"; text: string } | null>(null);

    // Learned skills tab state
    const [learnedSelected, setLearnedSelected] = useState<Set<string>>(new Set());
    const [learnedExporting, setLearnedExporting] = useState(false);
    const [learnedImporting, setLearnedImporting] = useState(false);
    const [importReport, setImportReport] = useState<{ restored: number; skipped: number; failed: number; details: string[] } | null>(null);
    const [uploadingSkill, setUploadingSkill] = useState<string | null>(null);

    // Diagnose state
    const [diagEntries, setDiagEntries] = useState<SkillDiagEntry[] | null>(null);
    const [diagLoading, setDiagLoading] = useState(false);

    const loadData = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const skillList = await ListNLSkills();
            const raw = Array.isArray(skillList) ? skillList : [];
            // Normalize: ensure triggers/steps are always arrays (Go nil → JSON null)
            const list = raw.map((s: NLSkillDefinition) => ({
                ...s,
                triggers: s.triggers || [],
                steps: s.steps || [],
            }));
            setSkills(list);
            // Clean up learned selection: remove names no longer present
            const learnedNames = new Set(
                list.filter((s: NLSkillDefinition) => s.source === "learned" || s.source === "crafted").map((s: NLSkillDefinition) => s.name)
            );
            setLearnedSelected((prev) => {
                const next = new Set<string>();
                prev.forEach((n) => { if (learnedNames.has(n)) next.add(n); });
                return next.size === prev.size ? prev : next;
            });
        } catch (err) {
            setError(localizeHubError(String(err)));
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        loadData();
    }, [loadData]);

    const handleHubSearch = useCallback(async () => {
        const q = hubSearchQuery.trim();
        if (!q) return;
        setHubSearching(true);
        setHubError("");
        setHubSearched(true);
        try {
            const results = await SearchSkillHub(q);
            setHubResults(Array.isArray(results) ? results : []);
        } catch (err) {
            setHubError(localizeHubError(String(err)));
            setHubResults([]);
        } finally {
            setHubSearching(false);
        }
    }, [hubSearchQuery]);

    // Compute installed Hub Skill IDs from local skills (memoized)
    const installedSkillIds = useMemo(
        () => new Set(
            skills.filter((s) => s.source === "hub" && s.hub_skill_id).map((s) => s.hub_skill_id!)
        ),
        [skills]
    );

    // Map skill_name -> HubSkillUpdateInfo for quick lookup (memoized)
    const updatesMap = useMemo(
        () => new Map(hubUpdates.map((u) => [u.skill_name, u])),
        [hubUpdates]
    );

    // Find the local skill name for a given hub skill ID (for update lookup, memoized)
    const localSkillByHubId = useMemo(() => {
        const map = new Map<string, NLSkillDefinition>();
        for (const s of skills) {
            if (s.source === "hub" && s.hub_skill_id) {
                map.set(s.hub_skill_id, s);
            }
        }
        return map;
    }, [skills]);

    const getLocalSkillForHubId = (hubId: string): NLSkillDefinition | undefined =>
        localSkillByHubId.get(hubId);

    // Check for Hub Skill updates
    const checkUpdates = useCallback(async () => {
        try {
            const updates = await CheckHubSkillUpdates();
            setHubUpdates(Array.isArray(updates) ? updates : []);
        } catch {
            // Silently ignore update check failures
        }
    }, []);

    // When switching to Hub tab, check for updates
    useEffect(() => {
        if (activeTab === "hub") {
            checkUpdates();
        }
    }, [activeTab, checkUpdates]);

    const handleInstall = useCallback(async (skill: HubSkillMeta) => {
        setInstallingSkills((prev) => [...prev, skill.id]);
        try {
            await InstallHubSkill(skill.id, skill.hub_url);
            await loadData();
            await checkUpdates();
        } catch (err) {
            setHubError(localizeHubError(String(err)));
        } finally {
            setInstallingSkills((prev) => prev.filter((id) => id !== skill.id));
        }
    }, [loadData, checkUpdates]);

    const handleUpdate = useCallback(async (skillName: string) => {
        setUpdatingSkills((prev) => [...prev, skillName]);
        try {
            await UpdateHubSkill(skillName);
            await loadData();
            await checkUpdates();
        } catch (err) {
            setHubError(localizeHubError(String(err)));
        } finally {
            setUpdatingSkills((prev) => prev.filter((n) => n !== skillName));
        }
    }, [loadData, checkUpdates]);

    const stepsToYaml = (steps: NLSkillStep[]): string => {
        if (!steps || steps.length === 0) return "";
        return steps
            .map((s) => {
                const lines = [`- action: "${s.action}"`];
                if (s.params && Object.keys(s.params).length > 0) {
                    lines.push("  params:");
                    for (const [k, v] of Object.entries(s.params)) {
                        lines.push(`    ${k}: ${JSON.stringify(v)}`);
                    }
                }
                if (s.on_error) lines.push(`  on_error: "${s.on_error}"`);
                return lines.join("\n");
            })
            .join("\n");
    };

    const yamlToSteps = (yaml: string): NLSkillStep[] => {
        if (!yaml.trim()) return [];
        const steps: NLSkillStep[] = [];
        let current: Partial<NLSkillStep> | null = null;
        let inParams = false;
        const params: Record<string, any> = {};

        for (const line of yaml.split("\n")) {
            const trimmed = line.trim();
            if (trimmed.startsWith("- action:")) {
                if (current) {
                    steps.push({ action: current.action || "", params: { ...params }, on_error: current.on_error || "stop" });
                }
                current = { action: trimmed.replace("- action:", "").trim().replace(/^"|"$/g, "") };
                inParams = false;
                Object.keys(params).forEach((k) => delete params[k]);
            } else if (trimmed === "params:") {
                inParams = true;
            } else if (trimmed.startsWith("on_error:")) {
                if (current) current.on_error = trimmed.replace("on_error:", "").trim().replace(/^"|"$/g, "");
                inParams = false;
            } else if (inParams && trimmed.includes(":")) {
                const idx = trimmed.indexOf(":");
                const key = trimmed.slice(0, idx).trim();
                let val: any = trimmed.slice(idx + 1).trim();
                try { val = JSON.parse(val); } catch { /* keep as string */ }
                params[key] = val;
            }
        }
        if (current) {
            steps.push({ action: current.action || "", params: { ...params }, on_error: current.on_error || "stop" });
        }
        return steps;
    };

    const openCreateForm = () => {
        setEditingSkill(null);
        setFormData({ ...emptySkill });
        setTriggerInput("");
        setStepsYaml("");
        setFormError("");
        setShowForm(true);
    };

    const openEditForm = (skill: NLSkillDefinition) => {
        setEditingSkill(skill);
        setFormData({ ...skill });
        setTriggerInput("");
        setStepsYaml(stepsToYaml(skill.steps));
        setFormError("");
        setShowForm(true);
    };

    const closeForm = () => {
        setShowForm(false);
        setEditingSkill(null);
        setFormError("");
    };

    const addTrigger = () => {
        const t = triggerInput.trim();
        if (t && !formData.triggers.includes(t)) {
            setFormData({ ...formData, triggers: [...formData.triggers, t] });
        }
        setTriggerInput("");
    };

    const removeTrigger = (idx: number) => {
        setFormData({ ...formData, triggers: formData.triggers.filter((_, i) => i !== idx) });
    };

    const handleTriggerKeyDown = (e: React.KeyboardEvent) => {
        if (e.key === "Enter") {
            e.preventDefault();
            addTrigger();
        }
    };

    const handleSubmit = async () => {
        if (!formData.name.trim()) {
            setFormError(localizeText("Name is required", "名称不能为空", "名稱不能為空"));
            return;
        }
        setBusy(true);
        setFormError("");
        try {
            const def = { ...formData, steps: yamlToSteps(stepsYaml) };
            if (editingSkill) {
                await UpdateNLSkill(def);
            } else {
                await CreateNLSkill(def);
            }
            closeForm();
            await loadData();
        } catch (err) {
            setFormError(String(err));
        } finally {
            setBusy(false);
        }
    };

    const handleDelete = async (name: string) => {
        setBusy(true);
        try {
            await DeleteNLSkill(name);
            setDeleteTarget(null);
            await loadData();
        } catch (err) {
            setError(localizeHubError(String(err)));
        } finally {
            setBusy(false);
        }
    };

    const handleImportZip = async () => {
        setImporting(true);
        setError("");
        try {
            const name = await ImportNLSkillZip();
            if (name) {
                await loadData();
            }
        } catch (err) {
            setError(localizeHubError(String(err)));
        } finally {
            setImporting(false);
        }
    };

    // --- Learned skills tab helpers ---

    // Installed skills: exclude auto-generated (learned/crafted) skills
    const installedSkills = useMemo(
        () => skills.filter((s) => s.source !== "learned" && s.source !== "crafted"),
        [skills]
    );

    const learnedSkills = useMemo(
        () => skills.filter((s) => s.source === "learned" || s.source === "crafted"),
        [skills]
    );

    const toggleLearnedSelect = (name: string) => {
        setLearnedSelected((prev) => {
            const next = new Set(prev);
            if (next.has(name)) next.delete(name);
            else next.add(name);
            return next;
        });
    };

    const toggleLearnedSelectAll = () => {
        if (learnedSelected.size === learnedSkills.length) {
            setLearnedSelected(new Set());
        } else {
            setLearnedSelected(new Set(learnedSkills.map((s) => s.name)));
        }
    };

    const handleLearnedExport = async () => {
        if (learnedSelected.size === 0) return;
        setLearnedExporting(true);
        setError("");
        try {
            await ExportLearnedSkillsZip(Array.from(learnedSelected));
        } catch (err) {
            setError(localizeHubError(String(err)));
        } finally {
            setLearnedExporting(false);
        }
    };

    const handleLearnedImport = async () => {
        setLearnedImporting(true);
        setError("");
        setImportReport(null);
        try {
            const report = await ImportLearnedSkillsZip();
            if (report) {
                setImportReport(report);
                await loadData();
            }
        } catch (err) {
            setError(localizeHubError(String(err)));
        } finally {
            setLearnedImporting(false);
        }
    };

    return (
        <div style={{ display: "flex", flexDirection: "column", gap: "10px" }}>
            {/* Tab switcher — sticky so it stays visible while content scrolls */}
            <div style={{ display: "flex", gap: "0", borderBottom: "1px solid #e1e4e8", position: "sticky", top: 0, backgroundColor: "var(--bg-color)", zIndex: 5, paddingTop: "2px" }}>
                <button
                    style={{
                        ...tabBtnStyle,
                        ...(activeTab === "local" ? tabBtnActiveStyle : {}),
                    }}
                    onClick={() => setActiveTab("local")}
                >
                    {localizeText("Installed Skills", "已安装 Skills", "已安裝 Skills")}
                </button>
                <button
                    style={{
                        ...tabBtnStyle,
                        ...(activeTab === "hub" ? tabBtnActiveStyle : {}),
                    }}
                    onClick={() => setActiveTab("hub")}
                >
                    {localizeText("Skill Market", "技能市场", "技能市場")}
                </button>
                <button
                    style={{
                        ...tabBtnStyle,
                        ...(activeTab === "learned" ? tabBtnActiveStyle : {}),
                    }}
                    onClick={() => setActiveTab("learned")}
                >
                    {localizeText("Learned Skills", "自学习技能", "自學習技能")}
                </button>
            </div>

            {/* === Local Skills Tab === */}
            {activeTab === "local" && (
                <>
                    {/* Header with create button */}
                    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                        <span style={{ fontSize: "0.78rem", color: "#5a6577" }}>
                            {installedSkills.length} {localizeText("skill(s) registered", "个已注册 Skill", "個已註冊 Skill")}
                        </span>
                        <div style={{ display: "flex", gap: "6px" }}>
                            <button className="btn-secondary" style={{ fontSize: "0.78rem", padding: "4px 12px" }} onClick={() => { loadData(); setDiagEntries(null); }} disabled={loading}>
                                {loading ? localizeText("Refreshing...", "刷新中...", "重新整理中...") : localizeText("🔄 Refresh", "🔄 刷新", "🔄 重新整理")}
                            </button>
                            <button className="btn-secondary" style={{ fontSize: "0.78rem", padding: "4px 12px" }} onClick={async () => {
                                setDiagLoading(true);
                                try {
                                    const res = await DiagnoseSkillFiles();
                                    setDiagEntries(Array.isArray(res) ? res : []);
                                } catch (err) {
                                    setDiagEntries([{ dir: "error", name: "", ok: false, reason: String(err) }]);
                                } finally {
                                    setDiagLoading(false);
                                }
                            }} disabled={diagLoading}>
                                {diagLoading ? localizeText("Diagnosing...", "诊断中...", "診斷中...") : localizeText("🔍 Diagnose", "🔍 诊断", "🔍 診斷")}
                            </button>
                            <button className="btn-secondary" style={{ fontSize: "0.78rem", padding: "4px 12px" }} onClick={handleImportZip} disabled={busy || importing}>
                                {importing ? localizeText("Importing...", "导入中...", "匯入中...") : localizeText("📦 Upload Skill Pack", "📦 上传 Skill 包", "📦 上傳 Skill 包")}
                            </button>
                            <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 12px" }} onClick={openCreateForm} disabled={busy}>
                                + {localizeText("New Skill", "新建 Skill", "新建 Skill")}
                            </button>
                        </div>
                    </div>

                    {/* Diagnose results */}
                    {diagEntries && diagEntries.length > 0 && (
                        <div style={{ border: "1px solid #e1e4e8", borderRadius: "6px", padding: "10px", background: "#f9fafb", fontSize: "0.76rem" }}>
                            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: "6px" }}>
                                <span style={{ fontWeight: 500, color: "#24292e" }}>📋 {localizeText("Skill Directory Diagnosis", "Skill 目录诊断结果", "Skill 目錄診斷結果")}</span>
                                <button className="btn-secondary" style={{ fontSize: "0.7rem", padding: "2px 8px" }} onClick={() => setDiagEntries(null)}>{localizeText("Close", "关闭", "關閉")}</button>
                            </div>
                            {diagEntries.map((d, i) => (
                                <div key={i} style={{ display: "flex", gap: "6px", alignItems: "baseline", padding: "3px 0", borderTop: i > 0 ? "1px solid #eaecef" : undefined }}>
                                    <span>{d.ok ? "✅" : "❌"}</span>
                                    <span style={{ fontWeight: 500, minWidth: "100px" }}>{d.dir}</span>
                                    {d.ok ? (
                                        <span style={{ color: "#22863a" }}>{localizeText("Loaded", "加载成功", "載入成功")}{d.name ? ` → ${d.name}` : ""}</span>
                                    ) : (
                                        <span style={{ color: "#cb2431" }}>{d.reason}</span>
                                    )}
                                </div>
                            ))}
                        </div>
                    )}

                    {/* Loading */}
                    {loading && (
                        <div style={{ textAlign: "center", padding: "16px", fontSize: "0.78rem", color: "#8b95a5" }}>
                            {localizeText("Loading...", "加载中...", "載入中...")}
                        </div>
                    )}

                    {/* Error */}
                    {error && (
                        <div style={{ fontSize: "0.78rem", color: "#c53030", background: "#fff5f5", padding: "6px 10px", borderRadius: "4px", border: "1px solid #fecdd3" }}>
                            {error}
                        </div>
                    )}

                    {/* Skills table */}
                    {!loading && installedSkills.length > 0 && (
                        <div style={{ border: "1px solid #e1e4e8", borderRadius: "6px", overflow: "hidden" }}>
                            <table style={{ width: "100%", borderCollapse: "collapse", fontSize: "0.76rem" }}>
                                <thead>
                                    <tr style={{ background: "#f4f5f7" }}>
                                        <th style={thStyle}>{localizeText("Name", "名称", "名稱")}</th>
                                        <th style={thStyle}>{localizeText("Description", "描述", "描述")}</th>
                                        <th style={thStyle}>{localizeText("Triggers", "触发短语", "觸發短語")}</th>
                                        <th style={thStyle}>{localizeText("Usage", "使用统计", "使用統計")}</th>
                                        <th style={thStyle}>{localizeText("Status", "状态", "狀態")}</th>
                                        <th style={{ ...thStyle, width: "100px" }}>{localizeText("Actions", "操作", "操作")}</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {installedSkills.map((s) => (
                                        <tr key={s.name} style={{ borderTop: "1px solid #e1e4e8" }}>
                                            <td style={tdStyle}>{s.name}</td>
                                            <td style={tdStyle}>
                                                <div style={descCellStyle} title={s.description || undefined}>{s.description || "—"}</div>
                                            </td>
                                            <td style={tdStyle}>
                                                <div style={{ display: "flex", flexWrap: "wrap", gap: "3px" }}>
                                                    {(s.triggers || []).map((t, i) => (
                                                        <span key={i} style={tagStyle}>{t}</span>
                                                    ))}
                                                </div>
                                            </td>
                                            <td style={tdStyle}>
                                                {(s.usage_count ?? 0) > 0 ? (
                                                    <span style={{ fontSize: "0.72rem", color: "#5a6577" }}>
                                                        {s.usage_count}{localizeText("x", "次", "次")} / {Math.round((s.success_rate ?? 0) * 100)}%
                                                    </span>
                                                ) : (
                                                    <span style={{ fontSize: "0.72rem", color: "#b0b8c4" }}>{localizeText("Unused", "未使用", "未使用")}</span>
                                                )}
                                            </td>
                                            <td style={tdStyle}>
                                                <span style={{ ...statusBadgeStyle, ...(s.status === "active" ? activeBadge : disabledBadge) }}>
                                                    {s.status === "active" ? localizeText("Active", "启用", "啟用") : s.status}
                                                </span>
                                            </td>
                                            <td style={tdStyle}>
                                                <div style={{ display: "flex", gap: "4px" }}>
                                                    <button className="btn-secondary" style={smallBtnStyle} onClick={() => openEditForm(s)} disabled={busy}>{localizeText("Edit", "编辑", "編輯")}</button>
                                                    <button className="btn-secondary btn-danger" style={smallBtnStyle} onClick={() => setDeleteTarget(s.name)} disabled={busy}>{localizeText("Delete", "删除", "刪除")}</button>
                                                </div>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    )}

                    {!loading && installedSkills.length === 0 && !error && (
                        <div style={{ textAlign: "center", padding: "20px", fontSize: "0.78rem", color: "#8b95a5" }}>
                            {localizeText("No registered Skills yet", "暂无已注册的 Skill", "暫無已註冊的 Skill")}
                        </div>
                    )}
                </>
            )}

            {/* === Hub Market Tab === */}
            {activeTab === "hub" && (
                <>
                    {/* Search input */}
                    <div style={{ display: "flex", gap: "6px" }}>
                        <input
                            className="form-input"
                            value={hubSearchQuery}
                            onChange={(e) => setHubSearchQuery(e.target.value)}
                            onKeyDown={(e) => { if (e.key === "Enter") handleHubSearch(); }}
                            placeholder={localizeText("Search Hub Skills...", "搜索 Hub Skill...", "搜尋 Hub Skill...")}
                            spellCheck={false}
                            style={{ flex: 1, fontSize: "0.78rem" }}
                        />
                        <button
                            className="btn-primary"
                            style={{ fontSize: "0.78rem", padding: "4px 12px", flexShrink: 0 }}
                            disabled={!hubSearchQuery.trim() || hubSearching}
                            onClick={handleHubSearch}
                        >
                            {hubSearching ? localizeText("Searching...", "搜索中...", "搜尋中...") : localizeText("Search", "搜索", "搜尋")}
                        </button>
                    </div>

                    {/* Hub error */}
                    {hubError && (
                        <div style={{ fontSize: "0.78rem", color: "#c53030", background: "#fff5f5", padding: "6px 10px", borderRadius: "4px", border: "1px solid #fecdd3" }}>
                            {hubError}
                        </div>
                    )}

                    {/* Loading state */}
                    {hubSearching && (
                        <div style={{
                            border: "1px solid #e1e4e8",
                            borderRadius: "6px",
                            padding: "24px",
                            textAlign: "center",
                            fontSize: "0.78rem",
                            color: "#8b95a5",
                            minHeight: "120px",
                            display: "flex",
                            alignItems: "center",
                            justifyContent: "center",
                        }}>
                            {localizeText("Searching Skill Market...", "正在搜索技能市场...", "正在搜尋技能市場...")}
                        </div>
                    )}

                    {/* Results */}
                    {!hubSearching && hubSearched && hubResults.length === 0 && !hubError && (
                        <div style={{
                            border: "1px solid #e1e4e8",
                            borderRadius: "6px",
                            padding: "24px",
                            textAlign: "center",
                            fontSize: "0.78rem",
                            color: "#8b95a5",
                            minHeight: "120px",
                            display: "flex",
                            alignItems: "center",
                            justifyContent: "center",
                        }}>
                            {localizeText("No results found", "无搜索结果", "無搜尋結果")}
                        </div>
                    )}

                    {!hubSearching && hubResults.length > 0 && (
                        <div style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
                            {hubResults.map((skill) => (
                                <div key={skill.id} style={hubCardStyle}>
                                    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: "8px" }}>
                                        <div style={{ flex: 1, minWidth: 0 }}>
                                            <div style={{ display: "flex", alignItems: "center", gap: "6px", flexWrap: "wrap" }}>
                                                <span style={{ fontWeight: 600, fontSize: "0.82rem", color: "#1a202c" }}>{skill.name}</span>
                                                <span style={trustBadgeStyle(skill.trust_level)}>
                                                    {skill.trust_level === "official" ? localizeText("Official", "官方", "官方") : skill.trust_level === "community" ? localizeText("Community", "社区", "社區") : localizeText("Unknown", "未知", "未知")}
                                                </span>
                                                <span style={{ fontSize: "0.68rem", color: "#8b95a5" }}>v{skill.version}</span>
                                            </div>
                                            <div style={{ fontSize: "0.76rem", color: "#5a6577", marginTop: "4px", lineHeight: 1.4, display: "-webkit-box", WebkitLineClamp: 2, WebkitBoxOrient: "vertical", overflow: "hidden" }} title={skill.description || undefined}>
                                                {skill.description || localizeText("No description", "暂无描述", "暫無描述")}
                                            </div>
                                            <div style={{ display: "flex", alignItems: "center", gap: "6px", marginTop: "6px", flexWrap: "wrap" }}>
                                                {(skill.tags || []).map((tag, i) => (
                                                    <span key={i} style={tagStyle}>{tag}</span>
                                                ))}
                                                <span style={{ fontSize: "0.68rem", color: "#8b95a5", marginLeft: "auto", display: "flex", alignItems: "center", gap: "8px" }}>
                                                    {skill.rating_count > 0 && (
                                                        <span style={{ display: "inline-flex", alignItems: "center", gap: "2px" }}>
                                                            <span style={{ color: "#f59e0b" }}>{renderStars(skill.avg_rating)}</span>
                                                            <span>({skill.rating_count})</span>
                                                        </span>
                                                    )}
                                                    ⬇ {formatDownloads(skill.downloads)}
                                                </span>
                                            </div>
                                        </div>
                                        {(() => {
                                            const isInstalling = installingSkills.includes(skill.id);
                                            const isInstalled = installedSkillIds.has(skill.id);
                                            const localSkill = getLocalSkillForHubId(skill.id);
                                            const updateInfo = localSkill ? updatesMap.get(localSkill.name) : undefined;
                                            const isUpdating = localSkill ? updatingSkills.includes(localSkill.name) : false;

                                            if (isInstalling) {
                                                return (
                                                    <button
                                                        className="btn-primary"
                                                        style={{ fontSize: "0.74rem", padding: "4px 14px", flexShrink: 0, alignSelf: "center", opacity: 0.7 }}
                                                        disabled
                                                    >
                                                        {localizeText("Installing...", "安装中...", "安裝中...")}
                                                    </button>
                                                );
                                            }
                                            if (isInstalled && updateInfo) {
                                                return (
                                                    <button
                                                        className="btn-primary"
                                                        style={{ fontSize: "0.74rem", padding: "4px 14px", flexShrink: 0, alignSelf: "center" }}
                                                        disabled={isUpdating}
                                                        onClick={() => handleUpdate(localSkill!.name)}
                                                    >
                                                        {isUpdating ? localizeText("Updating...", "更新中...", "更新中...") : localizeText("Update", "更新", "更新")}
                                                    </button>
                                                );
                                            }
                                            if (isInstalled) {
                                                return (
                                                    <button
                                                        className="btn-secondary"
                                                        style={{ fontSize: "0.74rem", padding: "4px 14px", flexShrink: 0, alignSelf: "center", opacity: 0.6 }}
                                                        disabled
                                                    >
                                                        {localizeText("Installed", "已安装", "已安裝")}
                                                    </button>
                                                );
                                            }
                                            return (
                                                <button
                                                    className="btn-primary"
                                                    style={{ fontSize: "0.74rem", padding: "4px 14px", flexShrink: 0, alignSelf: "center" }}
                                                    onClick={() => handleInstall(skill)}
                                                >
                                                    {localizeText("Install", "安装", "安裝")}
                                                </button>
                                            );
                                        })()}
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}

                    {/* Initial state — no search performed yet */}
                    {!hubSearching && !hubSearched && !hubError && (
                        <div style={{
                            border: "1px solid #e1e4e8",
                            borderRadius: "6px",
                            padding: "24px",
                            textAlign: "center",
                            fontSize: "0.78rem",
                            color: "#8b95a5",
                            minHeight: "120px",
                            display: "flex",
                            alignItems: "center",
                            justifyContent: "center",
                        }}>
                            {localizeText("Enter keywords to search the Skill Market", "输入关键词搜索技能市场上的 Skill", "輸入關鍵詞搜尋技能市場上的 Skill")}
                        </div>
                    )}
                </>
            )}

            {/* === Learned Skills Tab === */}
            {activeTab === "learned" && (
                <>
                    {/* Header with export/import buttons */}
                    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                        <span style={{ fontSize: "0.78rem", color: "#5a6577" }}>
                            {learnedSkills.length} {localizeText("learned skill(s)", "个自学习技能", "個自學習技能")}
                            {learnedSelected.size > 0 && ` (${localizeText("selected", "已选", "已選")} ${learnedSelected.size})`}
                        </span>
                        <div style={{ display: "flex", gap: "6px" }}>
                            <button className="btn-secondary" style={{ fontSize: "0.78rem", padding: "4px 12px" }} onClick={handleLearnedImport} disabled={learnedImporting}>
                                {learnedImporting ? localizeText("Importing...", "导入中...", "匯入中...") : localizeText("📦 Import", "📦 导入", "📦 匯入")}
                            </button>
                            <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 12px" }} onClick={handleLearnedExport} disabled={learnedExporting || learnedSelected.size === 0}>
                                {learnedExporting ? localizeText("Exporting...", "导出中...", "匯出中...") : `📤 ${localizeText("Export", "导出", "匯出")}${learnedSelected.size > 0 ? ` (${learnedSelected.size})` : ""}`}
                            </button>
                        </div>
                    </div>

                    {/* Import report */}
                    {importReport && (
                        <div style={{ fontSize: "0.76rem", padding: "8px 10px", borderRadius: "4px", border: "1px solid #e1e4e8", background: "#f9fafb" }}>
                            <div style={{ marginBottom: "4px", fontWeight: 600 }}>
                                {localizeText("Import complete:", "导入完成：", "匯入完成：")} {importReport.restored} {localizeText("succeeded", "成功", "成功")}，{importReport.skipped} {localizeText("skipped (duplicate)", "跳过（重名）", "跳過（重名）")}，{importReport.failed} {localizeText("failed", "失败", "失敗")}
                            </div>
                            {importReport.details.length > 0 && (
                                <ul style={{ margin: 0, paddingLeft: "16px", color: "#5a6577" }}>
                                    {importReport.details.map((d, i) => <li key={i}>{d}</li>)}
                                </ul>
                            )}
                            <button className="btn-secondary" style={{ fontSize: "0.72rem", padding: "2px 8px", marginTop: "6px" }} onClick={() => setImportReport(null)}>{localizeText("Close", "关闭", "關閉")}</button>
                        </div>
                    )}

                    {/* Error */}
                    {error && (
                        <div style={{ fontSize: "0.78rem", color: "#c53030", background: "#fff5f5", padding: "6px 10px", borderRadius: "4px", border: "1px solid #fecdd3" }}>
                            {error}
                        </div>
                    )}

                    {/* Loading */}
                    {loading && (
                        <div style={{ textAlign: "center", padding: "16px", fontSize: "0.78rem", color: "#8b95a5" }}>{localizeText("Loading...", "加载中...", "載入中...")}</div>
                    )}

                    {/* Learned skills table */}
                    {!loading && learnedSkills.length > 0 && (
                        <div style={{ border: "1px solid #e1e4e8", borderRadius: "6px", overflow: "hidden" }}>
                            <table style={{ width: "100%", borderCollapse: "collapse", fontSize: "0.76rem" }}>
                                <thead>
                                    <tr style={{ background: "#f4f5f7" }}>
                                        <th style={{ ...thStyle, width: "36px", textAlign: "center" }}>
                                            <input type="checkbox" checked={learnedSkills.length > 0 && learnedSelected.size === learnedSkills.length} onChange={toggleLearnedSelectAll} />
                                        </th>
                                        <th style={thStyle}>{localizeText("Name", "名称", "名稱")}</th>
                                        <th style={thStyle}>{localizeText("Description", "描述", "描述")}</th>
                                        <th style={{ ...thStyle, width: "40px", textAlign: "center" }}>{localizeText("Source", "来源", "來源")}</th>
                                        <th style={thStyle}>{localizeText("Usage", "使用统计", "使用統計")}</th>
                                        <th style={thStyle}>{localizeText("Status", "状态", "狀態")}</th>
                                        <th style={{ ...thStyle, width: "100px" }}>{localizeText("Actions", "操作", "操作")}</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {learnedSkills.map((s) => (
                                        <tr key={s.name} style={{ borderTop: "1px solid #e1e4e8" }}>
                                            <td style={{ ...tdStyle, textAlign: "center" }}>
                                                <input type="checkbox" checked={learnedSelected.has(s.name)} onChange={() => toggleLearnedSelect(s.name)} />
                                            </td>
                                            <td style={tdStyle}>{s.name}</td>
                                            <td style={tdStyle}>
                                                <div style={descCellStyle} title={s.description || undefined}>{s.description || "—"}</div>
                                            </td>
                                            <td style={tdStyle}>
                                                <span
                                                    title={s.source === "learned" ? localizeText("Experience learned", "经验学习", "經驗學習") : s.source === "file" ? localizeText("File import", "文件导入", "檔案匯入") : localizeText("Tool crafted", "工具制作", "工具製作")}
                                                    style={{ cursor: "default" }}
                                                >
                                                    {s.source === "learned" ? "📖" : s.source === "file" ? "📁" : "🔧"}
                                                </span>
                                            </td>
                                            <td style={tdStyle}>
                                                {(s.usage_count ?? 0) > 0 ? (
                                                    <span style={{ fontSize: "0.72rem", color: "#5a6577" }}>
                                                        {s.usage_count}{localizeText("x", "次", "次")} / {Math.round((s.success_rate ?? 0) * 100)}%
                                                    </span>
                                                ) : (
                                                    <span style={{ fontSize: "0.72rem", color: "#b0b8c4" }}>{localizeText("Unused", "未使用", "未使用")}</span>
                                                )}
                                            </td>
                                            <td style={tdStyle}>
                                                <span style={statusDotStyle(s.status === "active")}>
                                                    <span style={{ width: 6, height: 6, borderRadius: "50%", background: s.status === "active" ? "#22c55e" : "#d1d5db", flexShrink: 0 }} />
                                                    {s.status === "active" ? localizeText("Active", "启用", "啟用") : localizeText("Disabled", "停用", "停用")}
                                                </span>
                                            </td>
                                            <td style={tdStyle}>
                                                <div style={{ display: "flex", alignItems: "center", gap: "4px" }}>
                                                    <button
                                                        className="btn-secondary"
                                                        style={uploadBtnStyle}
                                                        disabled={uploadingSkill === s.name}
                                                        onClick={async () => {
                                                            setUploadingSkill(s.name);
                                                            try {
                                                                const sid = await UploadNLSkillToMarket(s.name);
                                                                setToastMsg({ type: "success", text: `${localizeText("Submission ID", "提交ID", "提交ID")}: ${sid}` });
                                                                await loadData();
                                                            } catch (e: any) {
                                                                setToastMsg({ type: "error", text: `${e?.message || e}` });
                                                            } finally {
                                                                setUploadingSkill(null);
                                                            }
                                                        }}
                                                    >
                                                        {uploadingSkill === s.name ? localizeText("Uploading...", "上传中...", "上傳中...") : s.hub_skill_id ? localizeText("⬆ Re-upload", "⬆ 重新上传", "⬆ 重新上傳") : localizeText("⬆ Upload", "⬆ 上传", "⬆ 上傳")}
                                                    </button>
                                                    {s.hub_skill_id && <span title={localizeText("Uploaded to Skill Market", "已上传到技能市场", "已上傳到技能市場")} style={{ fontSize: "0.68rem", color: "#16a34a" }}>✅</span>}
                                                </div>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    )}

                    {!loading && learnedSkills.length === 0 && !error && (
                        <div style={{ textAlign: "center", padding: "20px", fontSize: "0.78rem", color: "#8b95a5" }}>
                            {localizeText("No learned skills yet. MaClaw automatically learns and generates skills during use.", "暂无自学习技能。MaClaw 在使用过程中会自动学习并生成技能。", "暫無自學習技能。MaClaw 在使用過程中會自動學習並生成技能。")}
                        </div>
                    )}
                </>
            )}

            {/* Upload result toast dialog */}
            {toastMsg && (
                <div className="modal-backdrop" onClick={() => setToastMsg(null)}>
                    <div className="modal-content" onClick={(e) => e.stopPropagation()} style={{ width: "320px" }}>
                        <div className="modal-header">
                            <h3 style={{ fontSize: "0.88rem", margin: 0 }}>{toastMsg.type === "success" ? localizeText("Upload Succeeded", "上传成功", "上傳成功") : localizeText("Upload Failed", "上传失败", "上傳失敗")}</h3>
                            <button className="btn-close" onClick={() => setToastMsg(null)}>×</button>
                        </div>
                        <div className="modal-body">
                            <p style={{ fontSize: "0.8rem", color: toastMsg.type === "error" ? "#c53030" : "#5a6577", margin: 0, wordBreak: "break-all" }}>
                                {toastMsg.text}
                            </p>
                        </div>
                        <div className="modal-footer">
                            <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 14px" }} onClick={() => setToastMsg(null)}>{localizeText("OK", "确定", "確定")}</button>
                        </div>
                    </div>
                </div>
            )}

            {/* Delete confirmation dialog */}
            {deleteTarget && (
                <div className="modal-backdrop" onClick={() => setDeleteTarget(null)}>
                    <div className="modal-content" onClick={(e) => e.stopPropagation()} style={{ width: "280px" }}>
                        <div className="modal-header">
                            <h3 style={{ fontSize: "0.88rem", margin: 0 }}>{localizeText("Confirm Delete", "确认删除", "確認刪除")}</h3>
                            <button className="btn-close" onClick={() => setDeleteTarget(null)}>×</button>
                        </div>
                        <div className="modal-body">
                            <p style={{ fontSize: "0.8rem", color: "#5a6577", margin: 0 }}>
                                {localizeText(`Are you sure you want to delete Skill "${deleteTarget}"? This cannot be undone.`, `确定要删除 Skill「${deleteTarget}」吗？此操作不可撤销。`, `確定要刪除 Skill「${deleteTarget}」嗎？此操作不可撤銷。`)}
                            </p>
                        </div>
                        <div className="modal-footer">
                            <button className="btn-secondary" onClick={() => setDeleteTarget(null)} disabled={busy}>{localizeText("Cancel", "取消", "取消")}</button>
                            <button className="btn-secondary btn-danger" onClick={() => handleDelete(deleteTarget)} disabled={busy}>
                                {busy ? localizeText("Deleting...", "删除中...", "刪除中...") : localizeText("Delete", "删除", "刪除")}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* Create/Edit form dialog */}
            {showForm && (
                <div className="modal-backdrop" onClick={closeForm}>
                    <div className="modal-content" onClick={(e) => e.stopPropagation()} style={{ width: "420px", textAlign: "left" }}>
                        <div className="modal-header">
                            <h3 style={{ fontSize: "0.88rem", margin: 0 }}>{editingSkill ? localizeText("Edit Skill", "编辑 Skill", "編輯 Skill") : localizeText("New Skill", "新建 Skill", "新建 Skill")}</h3>
                            <button className="btn-close" onClick={closeForm}>×</button>
                        </div>
                        <div className="modal-body" style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">{localizeText("Name", "名称", "名稱")}</label>
                                <input
                                    className="form-input"
                                    value={formData.name}
                                    onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                                    placeholder="skill-name"
                                    disabled={!!editingSkill}
                                    spellCheck={false}
                                />
                            </div>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">{localizeText("Description", "描述", "描述")}</label>
                                <input
                                    className="form-input"
                                    value={formData.description}
                                    onChange={(e) => setFormData({ ...formData, description: e.target.value })}
                                    placeholder={localizeText("Skill description", "Skill 功能描述", "Skill 功能描述")}
                                    spellCheck={false}
                                />
                            </div>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">{localizeText("Triggers", "触发短语", "觸發短語")}</label>
                                <div style={{ display: "flex", flexWrap: "wrap", gap: "4px", marginBottom: "4px" }}>
                                    {formData.triggers.map((t, i) => (
                                        <span key={i} style={{ ...tagStyle, cursor: "pointer" }} onClick={() => removeTrigger(i)}>
                                            {t} ×
                                        </span>
                                    ))}
                                </div>
                                <div style={{ display: "flex", gap: "4px" }}>
                                    <input
                                        className="form-input"
                                        value={triggerInput}
                                        onChange={(e) => setTriggerInput(e.target.value)}
                                        onKeyDown={handleTriggerKeyDown}
                                        placeholder={localizeText("Type and press Enter to add", "输入后按 Enter 添加", "輸入後按 Enter 添加")}
                                        spellCheck={false}
                                        style={{ flex: 1 }}
                                    />
                                    <button className="btn-secondary" style={{ fontSize: "0.76rem", padding: "3px 8px", flexShrink: 0 }} onClick={addTrigger} type="button">
                                        {localizeText("Add", "添加", "添加")}
                                    </button>
                                </div>
                            </div>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">{localizeText("Steps (YAML)", "操作步骤 (YAML)", "操作步驟 (YAML)")}</label>
                                <textarea
                                    className="form-input"
                                    value={stepsYaml}
                                    onChange={(e) => setStepsYaml(e.target.value)}
                                    placeholder={'- action: "send_input"\n  params:\n    text: "hello"\n  on_error: "stop"'}
                                    spellCheck={false}
                                    style={{ minHeight: "120px", fontFamily: "monospace", fontSize: "0.76rem", resize: "vertical" }}
                                />
                            </div>
                            {formError && (
                                <div style={{ fontSize: "0.76rem", color: "#c53030", background: "#fff5f5", padding: "4px 8px", borderRadius: "4px" }}>
                                    {formError}
                                </div>
                            )}
                        </div>
                        <div className="modal-footer">
                            <button className="btn-secondary" onClick={closeForm} disabled={busy}>{localizeText("Cancel", "取消", "取消")}</button>
                            <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 14px" }} onClick={handleSubmit} disabled={busy}>
                                {busy ? localizeText("Submitting...", "提交中...", "提交中...") : editingSkill ? localizeText("Save", "保存", "儲存") : localizeText("Create", "创建", "建立")}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}

/* Inline style constants */
const thStyle: React.CSSProperties = {
    padding: "6px 8px",
    textAlign: "left",
    fontWeight: 600,
    fontSize: "0.74rem",
    color: "#5a6577",
    borderBottom: "1px solid #e1e4e8",
};

const tdStyle: React.CSSProperties = {
    padding: "6px 8px",
    fontSize: "0.76rem",
    color: "#1a202c",
    verticalAlign: "top",
};

const descCellStyle: React.CSSProperties = {
    maxWidth: "220px",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
};

const tagStyle: React.CSSProperties = {
    display: "inline-block",
    background: "#f4f5f7",
    border: "1px solid #e1e4e8",
    borderRadius: "999px",
    padding: "1px 8px",
    fontSize: "0.7rem",
    color: "#4f5d75",
};

const statusBadgeStyle: React.CSSProperties = {
    display: "inline-block",
    padding: "1px 8px",
    borderRadius: "999px",
    fontSize: "0.68rem",
    fontWeight: 600,
};

const activeBadge: React.CSSProperties = {
    background: "#f0fdf4",
    color: "#2f855a",
    border: "1px solid #86efac",
};

const disabledBadge: React.CSSProperties = {
    background: "#f4f5f7",
    color: "#8b95a5",
    border: "1px solid #e1e4e8",
};

const smallBtnStyle: React.CSSProperties = {
    fontSize: "0.72rem",
    padding: "2px 8px",
};

const tabBtnStyle: React.CSSProperties = {
    background: "none",
    border: "none",
    borderBottom: "2px solid transparent",
    padding: "6px 14px",
    fontSize: "0.78rem",
    color: "#5a6577",
    cursor: "pointer",
    fontWeight: 500,
    transition: "color 0.15s, border-color 0.15s",
};

const tabBtnActiveStyle: React.CSSProperties = {
    color: "#2563eb",
    borderBottomColor: "#2563eb",
    fontWeight: 600,
};

const hubCardStyle: React.CSSProperties = {
    border: "1px solid #e1e4e8",
    borderRadius: "6px",
    padding: "10px 12px",
    background: "#fff",
};

const sourceTextStyle: React.CSSProperties = {
    fontSize: "0.72rem",
    color: "#5a6577",
    whiteSpace: "nowrap",
};

const statusDotStyle = (active: boolean): React.CSSProperties => ({
    display: "inline-flex",
    alignItems: "center",
    gap: "5px",
    fontSize: "0.72rem",
    color: active ? "#16a34a" : "#9ca3af",
    whiteSpace: "nowrap",
});

const uploadBtnStyle: React.CSSProperties = {
    fontSize: "0.7rem",
    padding: "2px 10px",
    whiteSpace: "nowrap",
    minWidth: "60px",
    textAlign: "center",
};

const trustBadgeStyle = (level: string): React.CSSProperties => {
    const colors: Record<string, { bg: string; color: string; border: string }> = {
        official: { bg: "#f0fdf4", color: "#2f855a", border: "#86efac" },
        community: { bg: "#eff6ff", color: "#2563eb", border: "#93c5fd" },
        unknown: { bg: "#f4f5f7", color: "#8b95a5", border: "#e1e4e8" },
    };
    const c = colors[level] || colors.unknown;
    return {
        display: "inline-block",
        padding: "0px 6px",
        borderRadius: "999px",
        fontSize: "0.66rem",
        fontWeight: 600,
        background: c.bg,
        color: c.color,
        border: `1px solid ${c.border}`,
    };
};

function formatDownloads(n: number): string {
    if (n >= 10000) return (n / 10000).toFixed(1).replace(/\.0$/, "") + "w";
    if (n >= 1000) return (n / 1000).toFixed(1).replace(/\.0$/, "") + "k";
    return String(n);
}

function renderStars(avg: number): string {
    const full = Math.floor(avg);
    const half = avg - full >= 0.5 ? 1 : 0;
    const empty = 5 - full - half;
    return "★".repeat(full) + (half ? "½" : "") + "☆".repeat(empty);
}
