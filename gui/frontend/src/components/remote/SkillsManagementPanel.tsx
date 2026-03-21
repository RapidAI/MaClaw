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
    translate: (key: string) => string;
};

const emptySkill: NLSkillDefinition = {
    name: "",
    description: "",
    triggers: [],
    steps: [],
    status: "active",
    created_at: "",
};

export function SkillsManagementPanel({ translate }: Props) {
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

    const loadData = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const skillList = await ListNLSkills();
            const list = Array.isArray(skillList) ? skillList : [];
            setSkills(list);
            // Clean up learned selection: remove names no longer present
            const learnedNames = new Set(
                list.filter((s: NLSkillDefinition) => s.source === "learned" || s.source === "crafted" || s.source === "file").map((s: NLSkillDefinition) => s.name)
            );
            setLearnedSelected((prev) => {
                const next = new Set<string>();
                prev.forEach((n) => { if (learnedNames.has(n)) next.add(n); });
                return next.size === prev.size ? prev : next;
            });
        } catch (err) {
            setError(String(err));
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
            setHubError(String(err));
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
            setHubError(String(err));
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
            setHubError(String(err));
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
            setFormError("名称不能为空");
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
            setError(String(err));
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
            setError(String(err));
        } finally {
            setImporting(false);
        }
    };

    // --- Learned skills tab helpers ---

    const learnedSkills = useMemo(
        () => skills.filter((s) => s.source === "learned" || s.source === "crafted" || s.source === "file"),
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
            setError(String(err));
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
            setError(String(err));
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
                    本地 Skills
                </button>
                <button
                    style={{
                        ...tabBtnStyle,
                        ...(activeTab === "hub" ? tabBtnActiveStyle : {}),
                    }}
                    onClick={() => setActiveTab("hub")}
                >
                    技能市场
                </button>
                <button
                    style={{
                        ...tabBtnStyle,
                        ...(activeTab === "learned" ? tabBtnActiveStyle : {}),
                    }}
                    onClick={() => setActiveTab("learned")}
                >
                    自学习技能
                </button>
            </div>

            {/* === Local Skills Tab === */}
            {activeTab === "local" && (
                <>
                    {/* Header with create button */}
                    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                        <span style={{ fontSize: "0.78rem", color: "#5a6577" }}>
                            {skills.length} {translate("skillsRegistered") || "个已注册 Skill"}
                        </span>
                        <div style={{ display: "flex", gap: "6px" }}>
                            <button className="btn-secondary" style={{ fontSize: "0.78rem", padding: "4px 12px" }} onClick={handleImportZip} disabled={busy || importing}>
                                {importing ? "导入中..." : "📦 上传 Skill 包"}
                            </button>
                            <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 12px" }} onClick={openCreateForm} disabled={busy}>
                                + 新建 Skill
                            </button>
                        </div>
                    </div>

                    {/* Loading */}
                    {loading && (
                        <div style={{ textAlign: "center", padding: "16px", fontSize: "0.78rem", color: "#8b95a5" }}>
                            加载中...
                        </div>
                    )}

                    {/* Error */}
                    {error && (
                        <div style={{ fontSize: "0.78rem", color: "#c53030", background: "#fff5f5", padding: "6px 10px", borderRadius: "4px", border: "1px solid #fecdd3" }}>
                            {error}
                        </div>
                    )}

                    {/* Skills table */}
                    {!loading && skills.length > 0 && (
                        <div style={{ border: "1px solid #e1e4e8", borderRadius: "6px", overflow: "hidden" }}>
                            <table style={{ width: "100%", borderCollapse: "collapse", fontSize: "0.76rem" }}>
                                <thead>
                                    <tr style={{ background: "#f4f5f7" }}>
                                        <th style={thStyle}>名称</th>
                                        <th style={thStyle}>描述</th>
                                        <th style={thStyle}>触发短语</th>
                                        <th style={thStyle}>使用统计</th>
                                        <th style={thStyle}>状态</th>
                                        <th style={{ ...thStyle, width: "100px" }}>操作</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {skills.map((s) => (
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
                                                        {s.usage_count}次 / {Math.round((s.success_rate ?? 0) * 100)}%
                                                    </span>
                                                ) : (
                                                    <span style={{ fontSize: "0.72rem", color: "#b0b8c4" }}>未使用</span>
                                                )}
                                            </td>
                                            <td style={tdStyle}>
                                                <span style={{ ...statusBadgeStyle, ...(s.status === "active" ? activeBadge : disabledBadge) }}>
                                                    {s.status === "active" ? "启用" : s.status}
                                                </span>
                                            </td>
                                            <td style={tdStyle}>
                                                <div style={{ display: "flex", gap: "4px" }}>
                                                    <button className="btn-secondary" style={smallBtnStyle} onClick={() => openEditForm(s)} disabled={busy}>编辑</button>
                                                    <button className="btn-secondary btn-danger" style={smallBtnStyle} onClick={() => setDeleteTarget(s.name)} disabled={busy}>删除</button>
                                                </div>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    )}

                    {!loading && skills.length === 0 && !error && (
                        <div style={{ textAlign: "center", padding: "20px", fontSize: "0.78rem", color: "#8b95a5" }}>
                            暂无已注册的 Skill
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
                            placeholder="搜索 Hub Skill..."
                            spellCheck={false}
                            style={{ flex: 1, fontSize: "0.78rem" }}
                        />
                        <button
                            className="btn-primary"
                            style={{ fontSize: "0.78rem", padding: "4px 12px", flexShrink: 0 }}
                            disabled={!hubSearchQuery.trim() || hubSearching}
                            onClick={handleHubSearch}
                        >
                            {hubSearching ? "搜索中..." : "搜索"}
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
                            正在搜索技能市场...
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
                            无搜索结果
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
                                                    {skill.trust_level === "official" ? "官方" : skill.trust_level === "community" ? "社区" : "未知"}
                                                </span>
                                                <span style={{ fontSize: "0.68rem", color: "#8b95a5" }}>v{skill.version}</span>
                                            </div>
                                            <div style={{ fontSize: "0.76rem", color: "#5a6577", marginTop: "4px", lineHeight: 1.4, display: "-webkit-box", WebkitLineClamp: 2, WebkitBoxOrient: "vertical", overflow: "hidden" }} title={skill.description || undefined}>
                                                {skill.description || "暂无描述"}
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
                                                        安装中...
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
                                                        {isUpdating ? "更新中..." : "更新"}
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
                                                        已安装
                                                    </button>
                                                );
                                            }
                                            return (
                                                <button
                                                    className="btn-primary"
                                                    style={{ fontSize: "0.74rem", padding: "4px 14px", flexShrink: 0, alignSelf: "center" }}
                                                    onClick={() => handleInstall(skill)}
                                                >
                                                    安装
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
                            输入关键词搜索技能市场上的 Skill
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
                            {learnedSkills.length} 个自学习技能
                            {learnedSelected.size > 0 && ` (已选 ${learnedSelected.size})`}
                        </span>
                        <div style={{ display: "flex", gap: "6px" }}>
                            <button className="btn-secondary" style={{ fontSize: "0.78rem", padding: "4px 12px" }} onClick={handleLearnedImport} disabled={learnedImporting}>
                                {learnedImporting ? "导入中..." : "📦 导入"}
                            </button>
                            <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 12px" }} onClick={handleLearnedExport} disabled={learnedExporting || learnedSelected.size === 0}>
                                {learnedExporting ? "导出中..." : `📤 导出${learnedSelected.size > 0 ? ` (${learnedSelected.size})` : ""}`}
                            </button>
                        </div>
                    </div>

                    {/* Import report */}
                    {importReport && (
                        <div style={{ fontSize: "0.76rem", padding: "8px 10px", borderRadius: "4px", border: "1px solid #e1e4e8", background: "#f9fafb" }}>
                            <div style={{ marginBottom: "4px", fontWeight: 600 }}>
                                导入完成：{importReport.restored} 成功，{importReport.skipped} 跳过（重名），{importReport.failed} 失败
                            </div>
                            {importReport.details.length > 0 && (
                                <ul style={{ margin: 0, paddingLeft: "16px", color: "#5a6577" }}>
                                    {importReport.details.map((d, i) => <li key={i}>{d}</li>)}
                                </ul>
                            )}
                            <button className="btn-secondary" style={{ fontSize: "0.72rem", padding: "2px 8px", marginTop: "6px" }} onClick={() => setImportReport(null)}>关闭</button>
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
                        <div style={{ textAlign: "center", padding: "16px", fontSize: "0.78rem", color: "#8b95a5" }}>加载中...</div>
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
                                        <th style={thStyle}>名称</th>
                                        <th style={thStyle}>描述</th>
                                        <th style={thStyle}>来源</th>
                                        <th style={thStyle}>使用统计</th>
                                        <th style={thStyle}>状态</th>
                                        <th style={thStyle}>操作</th>
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
                                                <span style={sourceTextStyle}>
                                                    {s.source === "learned" ? "📖 经验学习" : s.source === "file" ? "📁 文件导入" : "🔧 工具制作"}
                                                </span>
                                            </td>
                                            <td style={tdStyle}>
                                                {(s.usage_count ?? 0) > 0 ? (
                                                    <span style={{ fontSize: "0.72rem", color: "#5a6577" }}>
                                                        {s.usage_count}次 / {Math.round((s.success_rate ?? 0) * 100)}%
                                                    </span>
                                                ) : (
                                                    <span style={{ fontSize: "0.72rem", color: "#b0b8c4" }}>未使用</span>
                                                )}
                                            </td>
                                            <td style={tdStyle}>
                                                <span style={statusDotStyle(s.status === "active")}>
                                                    <span style={{ width: 6, height: 6, borderRadius: "50%", background: s.status === "active" ? "#22c55e" : "#d1d5db", flexShrink: 0 }} />
                                                    {s.status === "active" ? "启用" : "停用"}
                                                </span>
                                            </td>
                                            <td style={tdStyle}>
                                                <button
                                                    className="btn-secondary"
                                                    style={uploadBtnStyle}
                                                    disabled={uploadingSkill === s.name}
                                                    onClick={async () => {
                                                        setUploadingSkill(s.name);
                                                        try {
                                                            const sid = await UploadNLSkillToMarket(s.name);
                                                            setToastMsg({ type: "success", text: `提交ID: ${sid}` });
                                                        } catch (e: any) {
                                                            setToastMsg({ type: "error", text: `${e?.message || e}` });
                                                        } finally {
                                                            setUploadingSkill(null);
                                                        }
                                                    }}
                                                >
                                                    {uploadingSkill === s.name ? "上传中..." : "⬆ 上传"}
                                                </button>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    )}

                    {!loading && learnedSkills.length === 0 && !error && (
                        <div style={{ textAlign: "center", padding: "20px", fontSize: "0.78rem", color: "#8b95a5" }}>
                            暂无自学习技能。MaClaw 在使用过程中会自动学习并生成技能。
                        </div>
                    )}
                </>
            )}

            {/* Upload result toast dialog */}
            {toastMsg && (
                <div className="modal-backdrop" onClick={() => setToastMsg(null)}>
                    <div className="modal-content" onClick={(e) => e.stopPropagation()} style={{ width: "320px" }}>
                        <div className="modal-header">
                            <h3 style={{ fontSize: "0.88rem", margin: 0 }}>{toastMsg.type === "success" ? "上传成功" : "上传失败"}</h3>
                            <button className="btn-close" onClick={() => setToastMsg(null)}>×</button>
                        </div>
                        <div className="modal-body">
                            <p style={{ fontSize: "0.8rem", color: toastMsg.type === "error" ? "#c53030" : "#5a6577", margin: 0, wordBreak: "break-all" }}>
                                {toastMsg.text}
                            </p>
                        </div>
                        <div className="modal-footer">
                            <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 14px" }} onClick={() => setToastMsg(null)}>确定</button>
                        </div>
                    </div>
                </div>
            )}

            {/* Delete confirmation dialog */}
            {deleteTarget && (
                <div className="modal-backdrop" onClick={() => setDeleteTarget(null)}>
                    <div className="modal-content" onClick={(e) => e.stopPropagation()} style={{ width: "280px" }}>
                        <div className="modal-header">
                            <h3 style={{ fontSize: "0.88rem", margin: 0 }}>确认删除</h3>
                            <button className="btn-close" onClick={() => setDeleteTarget(null)}>×</button>
                        </div>
                        <div className="modal-body">
                            <p style={{ fontSize: "0.8rem", color: "#5a6577", margin: 0 }}>
                                确定要删除 Skill「{deleteTarget}」吗？此操作不可撤销。
                            </p>
                        </div>
                        <div className="modal-footer">
                            <button className="btn-secondary" onClick={() => setDeleteTarget(null)} disabled={busy}>取消</button>
                            <button className="btn-secondary btn-danger" onClick={() => handleDelete(deleteTarget)} disabled={busy}>
                                {busy ? "删除中..." : "删除"}
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
                            <h3 style={{ fontSize: "0.88rem", margin: 0 }}>{editingSkill ? "编辑 Skill" : "新建 Skill"}</h3>
                            <button className="btn-close" onClick={closeForm}>×</button>
                        </div>
                        <div className="modal-body" style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">名称</label>
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
                                <label className="form-label">描述</label>
                                <input
                                    className="form-input"
                                    value={formData.description}
                                    onChange={(e) => setFormData({ ...formData, description: e.target.value })}
                                    placeholder="Skill 功能描述"
                                    spellCheck={false}
                                />
                            </div>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">触发短语</label>
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
                                        placeholder="输入后按 Enter 添加"
                                        spellCheck={false}
                                        style={{ flex: 1 }}
                                    />
                                    <button className="btn-secondary" style={{ fontSize: "0.76rem", padding: "3px 8px", flexShrink: 0 }} onClick={addTrigger} type="button">
                                        添加
                                    </button>
                                </div>
                            </div>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">操作步骤 (YAML)</label>
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
                            <button className="btn-secondary" onClick={closeForm} disabled={busy}>取消</button>
                            <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 14px" }} onClick={handleSubmit} disabled={busy}>
                                {busy ? "提交中..." : editingSkill ? "保存" : "创建"}
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
