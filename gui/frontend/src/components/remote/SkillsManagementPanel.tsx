import { useState, useEffect, useCallback, useMemo, useRef, type CSSProperties } from "react";
import { useDialog } from "../CustomDialog";
import { useToast } from "../Toast";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { SkillInstallProgressPanel } from "./SkillInstallProgressPanel";
import {
    executionClassBadgeStyle,
    statusDotStyle,
    uploadBtnStyle,
    trustBadgeStyle,
    formatDownloads,
    formatDate,
    renderStars,
} from "./skillsManagementUtils";
import {
    colors,
    remoteCardStyle,
    remoteCodeBlockStyle,
    remoteEmptyStateStyle,
    remoteErrorStateStyle,
    remoteInfoPanelStyle,
    remoteLoadingStateStyle,
    remoteStatusBadgeStyle,
    remoteTableCellStyle,
    remoteTableContainerStyle,
    remoteTableHeaderCellStyle,
    remoteTagStyle,
} from "./styles";
import {
    ListNLSkills,
    CreateNLSkill,
    UpdateNLSkill,
    DeleteNLSkill,
    ImportNLSkillZip,
    SearchMixedSkills,
    InstallMixedSkill,
    CheckHubSkillUpdates,
    UpdateHubSkill,
    ExportLearnedSkillsZip,
    ImportLearnedSkillsZip,
    UploadNLSkillToMarket,
    DiagnoseSkillFiles,
    ListExternalSkillDirsDetailed,
    AddExternalSkillDir,
    RemoveExternalSkillDir,
    SelectProjectDir,
    OpenSystemUrl,
    GetHubRecommendations,
    GetExperienceAuditHealth,
    ListExperienceAudit,
    ResolveCriticalConfirm,
} from "../../../wailsjs/go/main/App";

function localizeSkillInstallRiskLevel(level: string, localizeText: (en: string, zhHans: string, zhHant: string) => string): string {
    const normalized = level.trim().toLowerCase();
    switch (normalized) {
        case "critical":
            return localizeText("critical", "严重", "嚴重");
        case "high":
            return localizeText("high", "高", "高");
        case "medium":
            return localizeText("medium", "中", "中");
        case "low":
            return localizeText("low", "低", "低");
        default:
            return level;
    }
}

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
    source_project?: string;
    execution_class?: string;
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

interface MixedSkillSearchResult {
    id: string;
    name: string;
    description: string;
    tags: string[];
    source: string;
    source_label: string;
    install_ref?: string;
    file_path?: string;
    version?: string;
    author?: string;
    created_at?: string;
    trust_level?: string;
    avg_rating: number;
    rating_count: number;
    downloads: number;
    score: number;
    price: number;
    repo_url?: string;
    installed: boolean;
    installed_name?: string;
    can_update: boolean;
    has_update: boolean;
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

interface ExternalSkillDirInfo {
    path: string;
    skill_count: number;
    error?: string;
}

interface ExperienceAuditHealth {
    runs: number;
    completed: number;
    no_candidates: number;
    failed: number;
    total_candidates: number;
    registered: number;
    updated: number;
    skipped: number;
    avg_duration_ms?: number;
    latest_timestamp?: string;
    status?: string;
    issue_code?: string;
    primary_issue?: string;
    suggested_action?: string;
    skip_reasons?: Record<string, number>;
    unsupported_steps?: Record<string, number>;
}

interface ExperienceAuditEntry {
    timestamp: string;
    session_id?: string;
    tool?: string;
    title?: string;
    project_path?: string;
    status?: string;
    duration_ms?: number;
    error?: string;
    summary?: {
        total_candidates: number;
        registered: number;
        updated: number;
        skipped: number;
        skip_reasons?: Record<string, number>;
        unsupported_steps?: Record<string, number>;
    };
    decisions?: Array<{
        pattern_name: string;
        action: string;
        reason?: string;
        matched_skill_name?: string;
        quality?: {
            score?: number;
            reasons?: string[];
        };
        evidence?: {
            score?: number;
            reasons?: string[];
            unsupported_steps?: string[];
        };
    }>;
    upserted?: string[];
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
            re: /hubcenter URL not configured/,
            fn: () => localizeText("HubCenter URL not configured", "HubCenter 地址未配置", "HubCenter 位址未配置"),
        },
        {
            re: /missing github install ref/,
            fn: () => localizeText("Missing GitHub install reference", "缺少 GitHub 安装引用", "缺少 GitHub 安裝引用"),
        },
        {
            re: /invalid github install ref/,
            fn: () => localizeText("Invalid GitHub install reference", "GitHub 安装引用无效", "GitHub 安裝引用無效"),
        },
        {
            re: /当前企业策略不允许从该能力市场安装此 Skill|Your organization policy does not allow installing this skill from this capability marketplace|skill source .*allowed sources|skill source .*not allowed by policy/,
            fn: () => localizeText(
                "Your organization policy does not allow installing this Skill from this capability marketplace.",
                "当前企业策略不允许从该能力市场安装此 Skill。",
                "目前企業策略不允許從此能力市場安裝此 Skill。",
            ),
        },
        {
            re: /unsupported skill source/,
            fn: () => localizeText("Unsupported skill source", "不支持的技能来源", "不支援的技能來源"),
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

const LEARNED_SOURCES = new Set(["learned", "crafted"]);
function isLearnedSource(source: string): boolean {
    return LEARNED_SOURCES.has(source);
}

function getStatusBadgeVariant(status: string): CSSProperties {
    switch (status) {
        case "active":
            return { background: colors.successBg, color: colors.success, border: `1px solid ${colors.success}` };
        case "disabled":
            return { background: colors.surfaceMuted, color: colors.textMuted, border: `1px solid ${colors.border}` };
        case "needs_setup":
            return { background: "#2d2000", color: "#f59e0b", border: "1px solid #f59e0b" };
        case "needs_review":
            return { background: "#1a1a2e", color: "#818cf8", border: "1px solid #818cf8" };
        default:
            return { background: colors.surfaceMuted, color: colors.textMuted, border: `1px solid ${colors.border}` };
    }
}

function learnedSourceIcon(source: string): string {
    if (source === "learned") return "📖";
    if (source === "crafted") return "🔧";
    return "📁";
}

const LEARNED_DESCRIPTION_PREVIEW_CHARS = 20;
export function getLearnedSkillDescriptionPreview(description: string, maxChars = LEARNED_DESCRIPTION_PREVIEW_CHARS): string {
    const normalized = description.trim().replace(/\s+/g, " ");
    if (!normalized) return "-"; const chars = Array.from(normalized);
    return chars.length <= maxChars ? normalized : chars.slice(0, maxChars).join("") + "...";
}

export function SkillsManagementPanel({ localizeText }: Props) {
    const { showConfirm } = useDialog();
    const { showToast } = useToast();

    const localizeSkillStatus = (status: string): string => {
        switch (status) {
            case "active": return localizeText("Active", "启用", "啟用");
            case "disabled": return localizeText("Disabled", "已禁用", "已停用");
            case "needs_setup": return localizeText("Needs Setup", "待配置", "待配置");
            case "needs_review": return localizeText("Needs Review", "待审核", "待審核");
            default: return status || "—";
        }
    };
    const backdropMouseDownRef = useRef(false);
    const [activeTab, setActiveTab] = useState<"local" | "hub" | "learned" | "extdirs">("local");
    const [skills, setSkills] = useState<NLSkillDefinition[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);

    // Hub market state
    const [hubSearchQuery, setHubSearchQuery] = useState("");
    const [hubResults, setHubResults] = useState<MixedSkillSearchResult[]>([]);
    const [hubSearching, setHubSearching] = useState(false);
    const [hubError, setHubError] = useState("");
    const [hubSearched, setHubSearched] = useState(false);
    const [hubRecommendations, setHubRecommendations] = useState<MixedSkillSearchResult[]>([]);
    const [hubRecsLoading, setHubRecsLoading] = useState(false);

    // Localize backend error messages
    const localizeHubError = useMemo(() => makeLocalizeHubError(localizeText), [localizeText]);

    const learnedSourceTooltip = (source: string): string => {
        if (source === "learned") return localizeText("Experience learned", "经验学习", "經驗學習");
        if (source === "crafted") return localizeText("Tool crafted", "工具制作", "工具製作");
        return source;
    };
    const localizeAuditStatus = (value?: string): string => {
        const normalized = String(value || "empty").toLowerCase();
        const labels: Record<string, string> = {
            empty: localizeText("empty", "\u7a7a", "\u7a7a"),
            healthy: localizeText("healthy", "\u5065\u5eb7", "\u5065\u5eb7"),
            ok: localizeText("ok", "\u6b63\u5e38", "\u6b63\u5e38"),
            warning: localizeText("warning", "\u8b66\u544a", "\u8b66\u544a"),
            failed: localizeText("failed", "\u5931\u8d25", "\u5931\u6557"),
            completed: localizeText("completed", "\u5df2\u5b8c\u6210", "\u5df2\u5b8c\u6210"),
            no_candidates: localizeText("no candidates", "\u65e0\u5019\u9009", "\u7121\u5019\u9078"),
        };
        return labels[normalized] || value || labels.empty;
    };

    const localizeAuditText = (value?: string): string => {
        const auditText = String(value || "").trim();
        if (!auditText) return "";
        const normalized = auditText.toLowerCase();
        if (normalized.includes("run an eligible successful session")) {
            return localizeText(
                "run an eligible successful session before expecting learned skills",
                "\u8bf7\u5148\u5b8c\u6210\u4e00\u6b21\u7b26\u5408\u6761\u4ef6\u7684\u6210\u529f\u4f1a\u8bdd\uff0c\u518d\u67e5\u770b\u81ea\u5b66\u4e60\u6280\u80fd\u7ed3\u679c",
                "\u8acb\u5148\u5b8c\u6210\u4e00\u6b21\u7b26\u5408\u689d\u4ef6\u7684\u6210\u529f\u6703\u8a71\uff0c\u518d\u67e5\u770b\u81ea\u5b78\u7fd2\u6280\u80fd\u7d50\u679c",
            );
        }
        return auditText;
    };

    // Install/update state
    const [installingSkills, setInstallingSkills] = useState<Set<string>>(new Set());
    const [updatingSkills, setUpdatingSkills] = useState<Set<string>>(new Set());
    const [hubUpdates, setHubUpdates] = useState<HubSkillUpdateInfo[]>([]);

    // Form state
    const [showForm, setShowForm] = useState(false);
    const [editingSkill, setEditingSkill] = useState<NLSkillDefinition | null>(null);
    const [formData, setFormData] = useState<NLSkillDefinition>({ ...emptySkill });
    const [triggerInput, setTriggerInput] = useState("");
    const [stepsYaml, setStepsYaml] = useState("");
    const [formError, setFormError] = useState("");

    const [importing, setImporting] = useState(false);

    // Learned skill detail state
    const [detailSkill, setDetailSkill] = useState<NLSkillDefinition | null>(null);

    // Learned skills tab state
    const [learnedSelected, setLearnedSelected] = useState<Set<string>>(new Set());
    const [learnedExporting, setLearnedExporting] = useState(false);
    const [learnedImporting, setLearnedImporting] = useState(false);
    const [importReport, setImportReport] = useState<{ restored: number; skipped: number; failed: number; details: string[] } | null>(null);
    const [experienceAudit, setExperienceAudit] = useState<ExperienceAuditEntry[]>([]);
    const [experienceAuditHealth, setExperienceAuditHealth] = useState<ExperienceAuditHealth | null>(null);
    const [experienceAuditLoading, setExperienceAuditLoading] = useState(false);
    const [experienceAuditError, setExperienceAuditError] = useState("");
    const [uploadingSkill, setUploadingSkill] = useState<string | null>(null);

    // Diagnose state
    const [diagEntries, setDiagEntries] = useState<SkillDiagEntry[] | null>(null);
    const [diagLoading, setDiagLoading] = useState(false);

    // External skill directories state
    const [extDirs, setExtDirs] = useState<ExternalSkillDirInfo[]>([]);
    const [extDirsLoading, setExtDirsLoading] = useState(false);
    const [extDirInput, setExtDirInput] = useState("");
    const [extDirAdding, setExtDirAdding] = useState(false);
    const [extDirError, setExtDirError] = useState("");
    const [extDirRemoving, setExtDirRemoving] = useState(false);

    const loadData = useCallback(async (): Promise<NLSkillDefinition[]> => {
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
                list.filter((s: NLSkillDefinition) => isLearnedSource(s.source ?? "")).map((s: NLSkillDefinition) => s.name)
            );
            setLearnedSelected((prev) => {
                const next = new Set<string>();
                prev.forEach((n) => { if (learnedNames.has(n)) next.add(n); });
                return next.size === prev.size ? prev : next;
            });
            return list;
        } catch (err) {
            setError(localizeHubError(String(err)));
            return [];
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        loadData();
    }, [loadData]);

    // Refresh skill list when usage stats are updated from the agent.
    useEffect(() => {
        EventsOn("skill:usage_updated", () => {
            loadData();
        });
        return () => {
            EventsOff("skill:usage_updated");
        };
    }, [loadData]);

    // Listen for skill-install-risk-confirm events from the backend (manual install path).
    // Shows a localized confirmation dialog when a skill has security risks.
    useEffect(() => {
        const cleanup = EventsOn("skill-install-risk-confirm", async (payload: unknown) => {
            if (!payload || typeof payload !== "object") return;
            const data = payload as Record<string, unknown>;
            const confirmID = typeof data.confirm_id === "string" ? data.confirm_id : "";
            const skillName = typeof data.skill_name === "string" ? data.skill_name : "";
            const level = typeof data.level === "string" ? data.level : "";
            const factors = Array.isArray(data.factors) ? (data.factors as string[]) : [];
            if (!confirmID) return;

            const eventLang = typeof data.lang === "string" ? data.lang : "";
            const normalizedEventLang = eventLang.trim().toLowerCase();
            const labels = data.labels && typeof data.labels === "object" ? data.labels as Record<string, unknown> : {};
            const eventLocalizeText = normalizedEventLang === "en"
                ? (en: string, _zhHans: string, _zhHant: string) => en
                : normalizedEventLang.startsWith("zh-hant") || normalizedEventLang.startsWith("zh-tw") || normalizedEventLang.startsWith("zh-hk")
                    ? (_en: string, _zhHans: string, zhHant: string) => zhHant
                    : normalizedEventLang.startsWith("zh")
                        ? (_en: string, zhHans: string, _zhHant: string) => zhHans
                        : localizeText;

            const localizedLevel = localizeSkillInstallRiskLevel(level, eventLocalizeText);
            let message = eventLocalizeText(
                `Security warning: Skill "${skillName}" was assessed as ${localizedLevel} risk.`,
                `安全警告：技能「${skillName}」被评估为${localizedLevel}风险。`,
                `安全警告：技能「${skillName}」被評估為${localizedLevel}風險。`,
            );
            if (factors.length > 0) {
                message += "\n\n" + eventLocalizeText("Risk factors:", "风险因素：", "風險因素：");
                for (const f of factors) {
                    message += `\n  - ${f}`;
                }
            }
            message += "\n\n" + eventLocalizeText(
                "Do you want to allow this installation?",
                "是否允许安装此技能？",
                "是否允許安裝此技能？",
            );

            const confirmTitle = eventLocalizeText("Security Risk", "安全风险", "安全風險");
            const confirmButton = typeof labels.confirm === "string" ? labels.confirm : eventLocalizeText("Confirm install", "确认安装", "確認安裝");
            const rejectButton = typeof labels.reject === "string" ? labels.reject : eventLocalizeText("Reject install", "拒绝安装", "拒絕安裝");
            const confirmed = await showConfirm(message, confirmTitle, { confirmText: confirmButton, cancelText: rejectButton });
            try {
                await ResolveCriticalConfirm(confirmID, confirmed);
            } catch {
                // Confirmation expired or already handled — ignore.
            }
        });
        return cleanup;
    }, [showConfirm, localizeText]);

    const handleHubSearch = useCallback(async () => {
        const q = hubSearchQuery.trim();
        if (!q) return;
        setHubSearching(true);
        setHubError("");
        setHubSearched(true);
        try {
            const results = await SearchMixedSkills(q);
            setHubResults(Array.isArray(results) ? results : []);
        } catch (err) {
            setHubError(localizeHubError(String(err)));
            setHubResults([]);
        } finally {
            setHubSearching(false);
        }
    }, [hubSearchQuery, localizeHubError]);

    // Check for Hub Skill updates
    const checkUpdates = useCallback(async () => {
        try {
            const updates = await CheckHubSkillUpdates();
            setHubUpdates(Array.isArray(updates) ? updates : []);
        } catch {
            // Silently ignore update check failures
        }
    }, []);

    // When switching to Hub tab, check for updates and load recommendations
    useEffect(() => {
        if (activeTab === "hub") {
            checkUpdates();
            // Load recommendations for initial state (no search performed yet)
            if (!hubSearched && hubRecommendations.length === 0 && !hubRecsLoading) {
                setHubRecsLoading(true);
                GetHubRecommendations()
                    .then((recs) => {
                        setHubRecommendations(Array.isArray(recs) ? recs : []);
                    })
                    .catch(() => {})
                    .finally(() => setHubRecsLoading(false));
            }
        }
    }, [activeTab, checkUpdates]);

    // Load external dirs when switching to extdirs tab
    const loadExtDirs = useCallback(async () => {
        setExtDirsLoading(true);
        setExtDirError("");
        try {
            const dirs = await ListExternalSkillDirsDetailed();
            setExtDirs(Array.isArray(dirs) ? dirs : []);
        } catch (err) {
            setExtDirError(String(err));
        } finally {
            setExtDirsLoading(false);
        }
    }, []);

    useEffect(() => {
        if (activeTab === "extdirs") {
            loadExtDirs();
        }
    }, [activeTab, loadExtDirs]);

    const handleAddExtDir = useCallback(async () => {
        const dir = extDirInput.trim();
        if (!dir) return;
        setExtDirAdding(true);
        setExtDirError("");
        try {
            await AddExternalSkillDir(dir);
            setExtDirInput("");
            await loadExtDirs();
            await loadData();
        } catch (err) {
            setExtDirError(localizeHubError(String(err)));
        } finally {
            setExtDirAdding(false);
        }
    }, [extDirInput, loadExtDirs, loadData]);

    const handleRemoveExtDir = useCallback(async (path: string) => {
        const confirmed = await showConfirm(
            localizeText(
                "Remove external skill directory? Skills from this directory will no longer be scanned.",
                "确定要移除此外部技能目录吗？该目录下的技能将不再被扫描。",
                "確定要移除此外部技能目錄嗎？該目錄下的技能將不再被掃描。",
            ) + `\n\n${path}`,
            localizeText("Confirm Remove", "确认移除", "確認移除"),
        );
        if (!confirmed) return;
        setExtDirRemoving(true);
        try {
            await RemoveExternalSkillDir(path);
            await loadExtDirs();
            await loadData();
        } catch (err) {
            setExtDirError(localizeHubError(String(err)));
        } finally {
            setExtDirRemoving(false);
        }
    }, [loadData, loadExtDirs, localizeText, localizeHubError, showConfirm]);

    const handleInstall = useCallback(async (skill: MixedSkillSearchResult) => {
        setInstallingSkills((prev) => new Set(prev).add(skill.id));
        try {
            await InstallMixedSkill(skill.source, skill.id, skill.install_ref || "");
            await loadData();
            await checkUpdates();
            showToast(localizeText(
                `Skill "${skill.name}" installed successfully`,
                `技能「${skill.name}」安装成功`,
                `技能「${skill.name}」安裝成功`,
            ));
            if (hubSearchQuery.trim()) {
                const refreshed = await SearchMixedSkills(hubSearchQuery.trim());
                setHubResults(Array.isArray(refreshed) ? refreshed : []);
            }
            // Refresh recommendations so the installed state updates
            if (!hubSearched) {
                const recs = await GetHubRecommendations();
                setHubRecommendations(Array.isArray(recs) ? recs : []);
            }
        } catch (err) {
            setHubError(localizeHubError(String(err)));
        } finally {
            setInstallingSkills((prev) => {
                const next = new Set(prev);
                next.delete(skill.id);
                return next;
            });
        }
    }, [loadData, checkUpdates, hubSearchQuery, hubSearched, localizeHubError, showToast, localizeText]);

    const handleUpdate = useCallback(async (skillName: string) => {
        setUpdatingSkills((prev) => new Set(prev).add(skillName));
        try {
            await UpdateHubSkill(skillName);
            await loadData();
            await checkUpdates();
            showToast(localizeText(
                `Skill "${skillName}" updated successfully`,
                `技能「${skillName}」更新成功`,
                `技能「${skillName}」更新成功`,
            ));
        } catch (err) {
            setHubError(localizeHubError(String(err)));
        } finally {
            setUpdatingSkills((prev) => {
                const next = new Set(prev);
                next.delete(skillName);
                return next;
            });
        }
    }, [loadData, checkUpdates, localizeHubError, showToast, localizeText]);

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
                try { val = JSON.parse(val); } catch { }
                params[key] = val;
            }
        }
        if (current) {
            steps.push({ action: current.action || "", params: { ...params }, on_error: current.on_error || "stop" });
        }
        return steps;
    };

    const renderHubActionButton = (skill: MixedSkillSearchResult) => {
        const isInstalling = installingSkills.has(skill.id);
        const canUpdate = !!skill.installed && !!skill.can_update && !!skill.installed_name;
        const isUpdating = !!skill.installed_name && updatingSkills.has(skill.installed_name);

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
        if (canUpdate && skill.has_update) {
            return (
                <button
                    className="btn-primary"
                    style={{ fontSize: "0.74rem", padding: "4px 14px", flexShrink: 0, alignSelf: "center" }}
                    disabled={isUpdating}
                    onClick={() => handleUpdate(skill.installed_name!)}
                >
                    {isUpdating ? localizeText("Updating...", "更新中...", "更新中...") : localizeText("Update", "更新", "更新")}
                </button>
            );
        }
        if (skill.installed) {
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
    };

    const openCreateForm = () => {
        setEditingSkill(null);
        setFormData({ ...emptySkill });
        setTriggerInput("");
        setStepsYaml("");
        setFormError("");
        setShowForm(true);
    };

    const openEditForm = async (skill: NLSkillDefinition) => {
        // Re-fetch skills from backend to pick up any on-disk changes
        setBusy(true);
        try {
            const list = await loadData();
            const fresh = list.find((s) => s.name === skill.name);
            const target = fresh || skill;
            setEditingSkill(target);
            setFormData({ ...target });
            setTriggerInput("");
            setStepsYaml(stepsToYaml(target.steps));
        } catch {
            // Fallback to stale state if refresh fails
            setEditingSkill(skill);
            setFormData({ ...skill });
            setTriggerInput("");
            setStepsYaml(stepsToYaml(skill.steps));
        } finally {
            setBusy(false);
        }
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
        const confirmed = await showConfirm(
            localizeText(
                `Are you sure you want to delete Skill "${name}"? This cannot be undone.`,
                `确定要删除 Skill「${name}」吗？此操作不可撤销。`,
                `確定要刪除 Skill「${name}」嗎？此操作不可撤銷。`,
            ),
            localizeText("Confirm Delete", "确认删除", "確認刪除"),
        );
        if (!confirmed) return;
        setBusy(true);
        try {
            await DeleteNLSkill(name);
            await loadData();
        } catch (err) {
            setError(localizeHubError(String(err)));
        } finally {
            setBusy(false);
        }
    };

    const handleLearnedDelete = async (name: string) => {
        const confirmed = await showConfirm(
            localizeText(
                `Are you sure you want to delete Skill "${name}"? This cannot be undone.`,
                `确定要删除 Skill「${name}」吗？此操作不可撤销。`,
                `確定要刪除 Skill「${name}」嗎？此操作不可撤銷。`,
            ),
            localizeText("Confirm Delete", "确认删除", "確認刪除"),
        );
        if (!confirmed) return;
        setBusy(true);
        try {
            await DeleteNLSkill(name);
            setDetailSkill((prev) => (prev?.name === name ? null : prev));
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

    // Installed skills: exclude auto-generated (learned/crafted/auto-installed) skills
    const installedSkills = useMemo(
        () => skills.filter((s) => !isLearnedSource(s.source ?? "")),
        [skills]
    );

    const learnedSkills = useMemo(
        () => skills.filter((s) => isLearnedSource(s.source ?? "")),
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

    const loadExperienceAudit = useCallback(async () => {
        setExperienceAuditLoading(true);
        setExperienceAuditError("");
        try {
            const [records, health] = await Promise.all([ListExperienceAudit(), GetExperienceAuditHealth()]);
            setExperienceAudit(Array.isArray(records) ? records : []);
            setExperienceAuditHealth(health && typeof health === "object" ? health as ExperienceAuditHealth : null);
        } catch (err) {
            setExperienceAuditError(String(err));
            setExperienceAudit([]);
            setExperienceAuditHealth(null);
        } finally {
            setExperienceAuditLoading(false);
        }
    }, []);

    useEffect(() => {
        if (activeTab === "learned") {
            loadExperienceAudit();
        }
    }, [activeTab, loadExperienceAudit]);

    const getExecutionClassLabel = (executionClass?: string) => {
        switch (executionClass) {
            case "agent_markdown_skill":
                return localizeText("Agent Skill", "代理 Skill", "代理 Skill");
            case "native_skill":
                return localizeText("Native Skill", "原生 Skill", "原生 Skill");
            default:
                return "";
        }
    };

    const getExecutionClassTitle = (skill: NLSkillDefinition) => {
        switch (skill.execution_class) {
            case "agent_markdown_skill":
                return localizeText(
                    "Imported markdown-style skill executed through the agent skill pipeline.",
                    "导入的 Markdown 类 Skill，通过 agent skill 流程执行。",
                    "導入的 Markdown 類 Skill，透過 agent skill 流程執行。",
                );
            case "native_skill":
                return localizeText(
                    "Regular skill executed directly by the native skill runner.",
                    "常规 Skill，直接由原生 skill runner 执行。",
                    "常規 Skill，直接由原生 skill runner 執行。",
                );
            default:
                return skill.source_project || "";
        }
    };

    const formatStepText = (steps: NLSkillStep[]) => {
        if (!steps || steps.length === 0) {
            return localizeText("No steps", "无步骤", "無步驟");
        }
        return steps
            .map((step, index) => {
                const lines = [`${index + 1}. action: ${step.action || "-"}`];
                const params = step.params && Object.keys(step.params).length > 0
                    ? JSON.stringify(step.params, null, 2)
                    : null;
                if (params) lines.push(`   params: ${params.replace(/\n/g, "\n   ")}`);
                if (step.on_error) lines.push(`   on_error: ${step.on_error}`);
                return lines.join("\n");
            })
            .join("\n\n");
    };

    return (
        <div style={skillsPanelShellStyle}>
            {/* Keep tabs outside the scroll container so the vertical scrollbar starts below them. */}
            <div style={skillsTabBarStyle}>
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
                    {localizeText("Capability Market", "能力市场", "能力市場")}
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
                <button
                    style={{
                        ...tabBtnStyle,
                        ...(activeTab === "extdirs" ? tabBtnActiveStyle : {}),
                    }}
                    onClick={() => setActiveTab("extdirs")}
                >
                    {localizeText("External Dirs", "外部技能目录", "外部技能目錄")}
                </button>
            </div>

            <div style={skillsTabContentStyle}>
            {/* === Local Skills Tab === */}
            {activeTab === "local" && (
                <>
                    {/* Header with create button */}
                    <div style={localSkillsToolbarStyle}>
                        <span style={{ fontSize: "0.78rem", color: colors.textSecondary }}>
                            {installedSkills.length} {localizeText("skill(s) registered", "个已注册 Skill", "個已註冊 Skill")}
                        </span>
                        <div style={localSkillsActionBarStyle}>
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
                                {importing ? localizeText("Importing...", "导入中...", "匯入中...") : localizeText("📦 Import Skill Pack", "📦 导入 Skill 包", "📦 匯入 Skill 包")}
                            </button>
                            <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 12px" }} onClick={openCreateForm} disabled={busy}>
                                + {localizeText("New Skill", "新建 Skill", "新建 Skill")}
                            </button>
                        </div>
                    </div>
                    <div style={localSkillsHintStyle}>
                        {localizeText(
                            "OpenClaw skill zips usually contain SKILL.md or skill.md; skill.yaml / skill.yml are also supported.",
                            "标准 OpenClaw Skill ZIP 通常包含 SKILL.md 或 skill.md；也兼容 skill.yaml / skill.yml。",
                            "標準 OpenClaw Skill ZIP 通常包含 SKILL.md 或 skill.md；也兼容 skill.yaml / skill.yml。",
                        )}
                    </div>

                    <SkillInstallProgressPanel active={importing || installingSkills.size > 0 || (busy && showForm)} localizeText={localizeText} />
                    {/* Diagnose results */}
                    {diagEntries && diagEntries.length > 0 && (
                        <div style={{ ...remoteInfoPanelStyle, fontSize: "0.76rem" }}>
                            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: "6px" }}>
                                <span style={{ fontWeight: 500, color: colors.text }}>📋 {localizeText("Skill Directory Diagnosis", "Skill 目录诊断结果", "Skill 目錄診斷結果")}</span>
                                <button className="btn-secondary" style={{ fontSize: "0.7rem", padding: "2px 8px" }} onClick={() => setDiagEntries(null)}>{localizeText("Close", "关闭", "關閉")}</button>
                            </div>
                            {diagEntries.map((d, i) => (
                                <div key={i} style={{ display: "flex", gap: "6px", alignItems: "baseline", padding: "3px 0", borderTop: i > 0 ? `1px solid ${colors.borderLight}` : undefined }}>
                                    <span>{d.ok ? "✅" : "❌"}</span>
                                    <span style={{ fontWeight: 500, minWidth: "100px" }}>{d.dir}</span>
                                    {d.ok ? (
                                        <span style={{ color: colors.success }}>{localizeText("Loaded", "加载成功", "載入成功")}{d.name ? ` → ${d.name}` : ""}</span>
                                    ) : (
                                        <span style={{ color: colors.danger }}>{d.reason}</span>
                                    )}
                                </div>
                            ))}
                        </div>
                    )}

                    {/* Loading */}
                    {loading && (
                        <div style={remoteLoadingStateStyle}>
                            {localizeText("Loading...", "加载中...", "載入中...")}
                        </div>
                    )}

                    {/* Error */}
                    {error && (
                        <div style={remoteErrorStateStyle}>
                            {error}
                        </div>
                    )}

                    {/* Skills table */}
                    {!loading && installedSkills.length > 0 && (
                        <div style={localSkillsTableContainerStyle}>
                            <table style={localSkillsTableStyle}>
                                <thead>
                                    <tr style={{ background: colors.surfaceMuted }}>
                                        <th style={{ ...thStyle, width: "140px", textAlign: "left" }}>{localizeText("Name", "名称", "名稱")}</th>
                                        <th style={{ ...thStyle, textAlign: "left" }}>{localizeText("Description", "描述", "描述")}</th>
                                        <th style={{ ...thStyle, width: "72px", textAlign: "left", paddingRight: 4 }}>{localizeText("Type", "类型", "類型")}</th>
                                        <th style={{ ...thStyle, width: "40px", textAlign: "center", paddingLeft: 2, paddingRight: 2 }}>{localizeText("Version", "版本", "版本")}</th>
                                        <th style={{ ...thStyle, width: "80px", textAlign: "left", paddingRight: 4 }}>{localizeText("Usage", "使用统计", "使用統計")}</th>
                                        <th style={{ ...thStyle, width: "60px", whiteSpace: "nowrap", textAlign: "center", paddingRight: 2 }}>{localizeText("Status", "状态", "狀態")}</th>
                                        <th style={{ ...thStyle, width: "56px", textAlign: "center", paddingLeft: 2 }}>{localizeText("Actions", "操作", "操作")}</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {installedSkills.map((s) => (
                                        <tr key={s.name} style={{ borderTop: `1px solid ${colors.border}` }}>
                                            <td style={{ ...tdStyle, textAlign: "left" }}>{s.name}</td>
                                            <td style={tdStyle}>
                                                <div style={descCellStyle} title={s.description || undefined}>{s.description || "—"}</div>
                                            </td>
                                            <td style={{ ...tdStyle, textAlign: "left", paddingRight: 4 }}>
                                                {s.execution_class ? (
                                                    <span style={executionClassBadgeStyle} title={getExecutionClassTitle(s)}>
                                                        {getExecutionClassLabel(s.execution_class)}
                                                    </span>
                                                ) : (
                                                    <span style={{ fontSize: "0.72rem", color: colors.textMuted }}>—</span>
                                                )}
                                            </td>
                                            <td style={{ ...tdStyle, textAlign: "center", paddingLeft: 2, paddingRight: 2 }}>
                                                <span style={{ fontSize: "0.72rem", color: colors.textSecondary }}>{s.hub_version || "—"}</span>
                                            </td>
                                            <td style={{ ...tdStyle, textAlign: "left", paddingRight: 4 }}>
                                                {(s.usage_count ?? 0) > 0 ? (
                                                    <span style={{ fontSize: "0.72rem", color: colors.textSecondary }}>
                                                        {s.usage_count}{localizeText("x", "次", "次")} / {Math.round((s.success_rate ?? 0) * 100)}%
                                                    </span>
                                                ) : (
                                                    <span style={{ fontSize: "0.72rem", color: colors.textMuted }}>{localizeText("Unused", "未使用", "未使用")}</span>
                                                )}
                                            </td>
                                            <td style={{ ...tdStyle, textAlign: "center", whiteSpace: "nowrap", paddingRight: 2 }}>
                                                <span style={{ ...statusBadgeStyle, ...getStatusBadgeVariant(s.status) }}>
                                                    {localizeSkillStatus(s.status)}
                                                </span>
                                            </td>
                                            <td style={{ ...tdStyle, textAlign: "center", paddingLeft: 2 }}>
                                                <div style={localSkillsRowActionsStyle}>
                                                    <button
                                                        className="btn-secondary"
                                                        style={iconBtnStyle}
                                                        onClick={() => openEditForm(s)}
                                                        disabled={busy}
                                                        title={localizeText("Edit", "编辑", "編輯")}
                                                        aria-label={localizeText("Edit", "编辑", "編輯")}
                                                    >
                                                        {"\u270E"}
                                                    </button>
                                                    <button
                                                        className="btn-secondary"
                                                        style={deleteIconBtnStyle}
                                                        onClick={() => handleDelete(s.name)}
                                                        disabled={busy}
                                                        title={localizeText("Delete", "删除", "刪除")}
                                                        aria-label={localizeText("Delete", "删除", "刪除")}
                                                    >
                                                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                                                            <polyline points="3 6 5 6 21 6" />
                                                            <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
                                                            <line x1="10" y1="11" x2="10" y2="17" />
                                                            <line x1="14" y1="11" x2="14" y2="17" />
                                                        </svg>
                                                    </button>
                                                </div>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    )}

                    {!loading && installedSkills.length === 0 && !error && (
                        <div style={skillsEmptyStateStyle}>
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
                        {hubSearched && (
                            <button
                                className="btn-secondary"
                                style={{ fontSize: "0.78rem", padding: "4px 12px", flexShrink: 0 }}
                                onClick={async () => {
                                    setHubSearchQuery("");
                                    setHubResults([]);
                                    setHubSearched(false);
                                    setHubError("");
                                    // Always refresh recommendations to pick up installed state changes
                                    setHubRecsLoading(true);
                                    try {
                                        const recs = await GetHubRecommendations();
                                        setHubRecommendations(Array.isArray(recs) ? recs : []);
                                    } catch { /* ignore */ }
                                    finally { setHubRecsLoading(false); }
                                }}
                            >
                                {localizeText("Recommended", "推荐", "推薦")}
                            </button>
                        )}
                    </div>

                    {/* Hub error */}
                    {hubError && (
                        <div style={remoteErrorStateStyle}>
                            {hubError}
                        </div>
                    )}

                    {/* Loading state */}
                    {hubSearching && (
                        <div style={{
                            ...remoteLoadingStateStyle,
                            minHeight: "120px",
                            display: "flex",
                            alignItems: "center",
                            justifyContent: "center",
                        }}>
                            {localizeText("Searching Capability Market...", "正在搜索能力市场...", "正在搜尋能力市場...")}
                        </div>
                    )}

                    {/* Results */}
                    {!hubSearching && hubSearched && hubResults.length === 0 && !hubError && (
                        <div style={skillsEmptyStateStyle}>
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
                                                <span style={{ fontWeight: 600, fontSize: "0.82rem", color: colors.text }}>{skill.name}</span>
                                                <span style={{
                                                    display: "inline-flex",
                                                    alignItems: "center",
                                                    gap: "4px",
                                                    fontSize: "0.66rem",
                                                    padding: "2px 6px",
                                                    borderRadius: "999px",
                                                    background: colors.surfaceMuted,
                                                    color: colors.textSecondary,
                                                    border: `1px solid ${colors.borderLight}`,
                                                }}>
                                                    {skill.source_label}
                                                </span>
                                                {skill.trust_level && (
                                                    <span style={trustBadgeStyle(skill.trust_level)}>
                                                        {skill.trust_level === "official" ? localizeText("Official", "官方", "官方") : skill.trust_level === "community" ? localizeText("Community", "社区", "社區") : localizeText("Unknown", "未知", "未知")}
                                                    </span>
                                                )}
                                                {skill.version && (
                                                    <span style={{ fontSize: "0.68rem", color: colors.textMuted }}>v{skill.version}</span>
                                                )}
                                            </div>
                                            {skill.source === "github" && (skill.repo_url || skill.file_path) && (
                                                <div style={{ fontSize: "0.68rem", color: colors.textMuted, marginTop: "4px", display: "flex", gap: "8px", flexWrap: "wrap" }}>
                                                    {skill.repo_url && (
                                                        <button
                                                            type="button"
                                                            onClick={() => OpenSystemUrl(skill.repo_url!)}
                                                            style={{
                                                                padding: 0,
                                                                border: "none",
                                                                background: "transparent",
                                                                color: colors.link,
                                                                cursor: "pointer",
                                                                fontSize: "0.68rem",
                                                                textDecoration: "underline",
                                                            }}
                                                            title={skill.repo_url}
                                                        >
                                                            {skill.repo_url}
                                                        </button>
                                                    )}
                                                    {skill.file_path && <span>{skill.file_path}</span>}
                                                </div>
                                            )}
                                            <div style={hubSkillDescriptionStyle} title={skill.description || undefined}>
                                                {skill.description || localizeText("No description", "暂无描述", "暫無描述")}
                                            </div>
                                            <div style={{ display: "flex", alignItems: "center", gap: "6px", marginTop: "6px", flexWrap: "wrap" }}>
                                                {(skill.tags || []).map((tag, i) => (
                                                    <span key={i} style={tagStyle}>{tag}</span>
                                                ))}
                                                <span style={{ fontSize: "0.68rem", color: colors.textMuted, marginLeft: "auto", display: "flex", alignItems: "center", gap: "8px", flexWrap: "wrap" }}>
                                                    {skill.author && (
                                                        <span>{skill.author}</span>
                                                    )}
                                                    {skill.rating_count > 0 && (
                                                        <span style={{ display: "inline-flex", alignItems: "center", gap: "2px" }}>
                                                            <span style={{ color: colors.warning }}>{renderStars(skill.avg_rating)}</span>
                                                            <span>({skill.rating_count})</span>
                                                        </span>
                                                    )}
                                                    {skill.downloads > 0 && (
                                                        <span>⬇ {formatDownloads(skill.downloads)}</span>
                                                    )}
                                                    {skill.price > 0 && (
                                                        <span>{localizeText(`Price ${skill.price}`, `价格 ${skill.price}`, `價格 ${skill.price}`)}</span>
                                                    )}
                                                    {skill.created_at && (
                                                        <span>{formatDate(skill.created_at)}</span>
                                                    )}
                                                </span>
                                            </div>
                                        </div>
                                        {renderHubActionButton(skill)}
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}

                    {/* Initial state — no search performed yet: show recommendations */}
                    {!hubSearching && !hubSearched && !hubError && (
                        <>
                            {hubRecsLoading && (
                                <div style={{
                                    ...remoteLoadingStateStyle,
                                    minHeight: "80px",
                                    display: "flex",
                                    alignItems: "center",
                                    justifyContent: "center",
                                }}>
                                    {localizeText("Loading recommendations...", "加载推荐中...", "載入推薦中...")}
                                </div>
                            )}
                            {!hubRecsLoading && hubRecommendations.length > 0 && (
                                <div style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
                                    <div style={{ fontSize: "0.78rem", color: colors.textSecondary, fontWeight: 500 }}>
                                        🔥 {localizeText("Popular Skills", "热门 Skill", "熱門 Skill")}
                                    </div>
                                    {hubRecommendations.map((skill) => (
                                        <div key={skill.id} style={hubCardStyle}>
                                            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: "8px" }}>
                                                <div style={{ flex: 1, minWidth: 0 }}>
                                                    <div style={{ display: "flex", alignItems: "center", gap: "6px", flexWrap: "wrap" }}>
                                                        <span style={{ fontWeight: 600, fontSize: "0.82rem", color: colors.text }}>{skill.name}</span>
                                                        {skill.version && (
                                                            <span style={{ fontSize: "0.68rem", color: colors.textMuted }}>v{skill.version}</span>
                                                        )}
                                                    </div>
                                                    <div style={hubSkillDescriptionStyle} title={skill.description || undefined}>
                                                        {skill.description || localizeText("No description", "暂无描述", "暫無描述")}
                                                    </div>
                                                    <div style={{ display: "flex", alignItems: "center", gap: "8px", marginTop: "6px", fontSize: "0.68rem", color: colors.textMuted }}>
                                                        {skill.author && <span>{skill.author}</span>}
                                                        {skill.downloads > 0 && <span>⬇ {formatDownloads(skill.downloads)}</span>}
                                                        {skill.rating_count > 0 && (
                                                            <span style={{ display: "inline-flex", alignItems: "center", gap: "2px" }}>
                                                                <span style={{ color: colors.warning }}>{renderStars(skill.avg_rating)}</span>
                                                                <span>({skill.rating_count})</span>
                                                            </span>
                                                        )}
                                                    </div>
                                                </div>
                                                {renderHubActionButton(skill)}
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            )}
                            {!hubRecsLoading && hubRecommendations.length === 0 && (
                                <div style={skillsEmptyStateStyle}>
                                    {localizeText("Enter keywords to search the Capability Market", "输入关键词搜索能力市场上的 Skill", "輸入關鍵詞搜尋能力市場上的 Skill")}
                                </div>
                            )}
                        </>
                    )}
                </>
            )}

            {/* === Learned Skills Tab === */}
            {activeTab === "learned" && (
                <>
                    {/* Header with export/import buttons */}
                    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                        <span style={{ fontSize: "0.78rem", color: colors.textSecondary }}>
                            {learnedSkills.length} {localizeText("learned skill(s)", "个自学习技能", "個自學習技能")}
                            {learnedSelected.size > 0 && ` (${localizeText("selected", "已选", "已選")} ${learnedSelected.size})`}
                        </span>
                        <div style={{ display: "flex", gap: "6px" }}>
                            <button className="btn-secondary" style={{ fontSize: "0.78rem", padding: "4px 12px" }} onClick={loadExperienceAudit} disabled={experienceAuditLoading}>
                                {experienceAuditLoading ? localizeText("Refreshing...", "\u5237\u65b0\u4e2d...", "\u91cd\u65b0\u6574\u7406\u4e2d...") : localizeText("Audit", "\u5ba1\u8ba1", "\u7a3d\u6838")}
                            </button>
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
                        <div style={{ ...remoteInfoPanelStyle, padding: "8px 10px", borderRadius: "4px" }}>
                            <div style={{ marginBottom: "4px", fontWeight: 600 }}>
                                {localizeText("Import complete:", "导入完成：", "匯入完成：")} {importReport.restored} {localizeText("succeeded", "成功", "成功")}，{importReport.skipped} {localizeText("skipped (duplicate)", "跳过（重名）", "跳過（重名）")}，{importReport.failed} {localizeText("failed", "失败", "失敗")}
                            </div>
                            {importReport.details.length > 0 && (
                                <ul style={{ margin: 0, paddingLeft: "16px", color: colors.textSecondary }}>
                                    {importReport.details.map((d, i) => <li key={i}>{d}</li>)}
                                </ul>
                            )}
                            <button className="btn-secondary" style={{ fontSize: "0.72rem", padding: "2px 8px", marginTop: "6px" }} onClick={() => setImportReport(null)}>{localizeText("Close", "关闭", "關閉")}</button>
                        </div>
                    )}

                    {/* Experience extraction audit */}
                    {(experienceAuditError || experienceAuditHealth || experienceAudit.length > 0) && (
                        <div style={{ ...remoteInfoPanelStyle, padding: "8px 10px", borderRadius: "4px" }}>
                            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: "6px" }}>
                                <span style={{ fontWeight: 600, color: colors.text }}>{localizeText("Experience Audit", "\u7ecf\u9a8c\u5ba1\u8ba1", "\u7d93\u9a57\u7a3d\u6838")}</span>
                                <button className="btn-secondary" style={{ fontSize: "0.72rem", padding: "2px 8px" }} onClick={() => { setExperienceAudit([]); setExperienceAuditHealth(null); }}>{localizeText("Hide", "\u9690\u85cf", "\u96b1\u85cf")}</button>
                            </div>
                            {experienceAuditError && <div style={{ color: colors.danger, fontSize: "0.74rem" }}>{experienceAuditError}</div>}
                            {!experienceAuditError && experienceAuditHealth && (
                                <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(120px, 1fr))", gap: "6px", marginBottom: "8px" }}>
                                    {[
                                        [localizeText("Health", "\u5065\u5eb7\u72b6\u6001", "\u5065\u5eb7\u72c0\u614b"), localizeAuditStatus(experienceAuditHealth.status)],
                                        [localizeText("Runs", "\u8fd0\u884c\u6b21\u6570", "\u57f7\u884c\u6b21\u6578"), experienceAuditHealth.runs],
                                        [localizeText("Completed", "\u5df2\u5b8c\u6210", "\u5df2\u5b8c\u6210"), experienceAuditHealth.completed],
                                        [localizeText("Failed", "\u5931\u8d25", "\u5931\u6557"), experienceAuditHealth.failed],
                                        [localizeText("Registered", "\u5df2\u6ce8\u518c", "\u5df2\u8a3b\u518a"), experienceAuditHealth.registered],
                                        [localizeText("Updated", "\u5df2\u66f4\u65b0", "\u5df2\u66f4\u65b0"), experienceAuditHealth.updated],
                                        [localizeText("Skipped", "\u5df2\u8df3\u8fc7", "\u5df2\u8df3\u904e"), experienceAuditHealth.skipped],
                                    ].map(([label, value]) => (
                                        <div key={String(label)} style={{ border: "1px solid " + colors.borderLight, borderRadius: "4px", padding: "5px 7px", background: colors.surface }}>
                                            <div style={{ fontSize: "0.66rem", color: colors.textMuted }}>{label}</div>
                                            <div style={{ fontSize: "0.84rem", fontWeight: 700, color: colors.text }}>{value}</div>
                                        </div>
                                    ))}
                                </div>
                            )}
                            {!experienceAuditError && experienceAuditHealth?.primary_issue && (
                                <div style={{ fontSize: "0.72rem", color: colors.warning, marginBottom: "6px" }}>
                                    {localizeText("Primary issue", "\u4e3b\u8981\u95ee\u9898", "\u4e3b\u8981\u554f\u984c")}: {localizeAuditText(experienceAuditHealth.primary_issue)}
                                </div>
                            )}
                            {!experienceAuditError && experienceAuditHealth?.suggested_action && (
                                <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginBottom: "6px" }}>
                                    {localizeText("Suggested action", "\u5efa\u8bae\u64cd\u4f5c", "\u5efa\u8b70\u64cd\u4f5c")}: {localizeAuditText(experienceAuditHealth.suggested_action)}
                                </div>
                            )}
                            {!experienceAuditError && experienceAuditHealth && (Object.keys(experienceAuditHealth.skip_reasons || {}).length > 0 || Object.keys(experienceAuditHealth.unsupported_steps || {}).length > 0) && (
                                <div style={{ fontSize: "0.72rem", color: colors.textMuted, marginBottom: "8px" }}>
                                    {Object.entries(experienceAuditHealth.skip_reasons || {}).slice(0, 3).map(([reason, count]) => String(reason) + " x" + count).join("; ")}
                                    {Object.keys(experienceAuditHealth.skip_reasons || {}).length > 0 && Object.keys(experienceAuditHealth.unsupported_steps || {}).length > 0 ? " / " : ""}
                                    {Object.entries(experienceAuditHealth.unsupported_steps || {}).slice(0, 3).map(([step, count]) => String(step) + " x" + count).join("; ")}
                                </div>
                            )}
                            {!experienceAuditError && experienceAudit.slice(0, 5).map((record, index) => {
                                const summary = record.summary || { total_candidates: 0, registered: 0, updated: 0, skipped: 0 };
                                const skipReasons = Object.entries(summary.skip_reasons || {});
                                const unsupported = Object.entries(summary.unsupported_steps || {});
                                const status = record.status || (record.error ? "failed" : summary.total_candidates > 0 ? "completed" : "no_candidates");
                                const statusColor = status === "failed" ? colors.danger : status === "no_candidates" ? colors.textMuted : colors.success;
                                const durationLabel = typeof record.duration_ms === "number" && record.duration_ms > 0 ? `${record.duration_ms}ms` : "";
                                return (
                                    <div key={`${record.timestamp}-${index}`} style={{ padding: "6px 0", borderTop: index > 0 ? `1px solid ${colors.borderLight}` : undefined }}>
                                        <div style={{ display: "flex", justifyContent: "space-between", gap: "8px", alignItems: "baseline" }}>
                                            <span style={{ fontWeight: 600, fontSize: "0.76rem", color: colors.text }}>{record.title || record.tool || localizeText("Untitled session", "Untitled session", "Untitled session")}</span>
                                            <span style={{ fontSize: "0.68rem", color: statusColor, marginLeft: "auto" }}>{localizeText("Status", "Status", "Status")}: {status}{durationLabel ? ` / ${durationLabel}` : ""}</span>
                                            <span style={{ fontSize: "0.68rem", color: colors.textMuted }}>{record.timestamp ? new Date(record.timestamp).toLocaleString() : ""}</span>
                                        </div>
                                        <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginTop: "3px" }}>
                                            {localizeText("Candidates", "\u5019\u9009", "\u5019\u9078")}: {summary.total_candidates} / {localizeText("Registered", "\u5df2\u6ce8\u518c", "\u5df2\u8a3b\u518a")}: {summary.registered} / {localizeText("Updated", "\u5df2\u66f4\u65b0", "\u5df2\u66f4\u65b0")}: {summary.updated} / {localizeText("Skipped", "\u5df2\u8df3\u8fc7", "\u5df2\u8df3\u904e")}: {summary.skipped}
                                        </div>
                                        {record.error && (
                                            <div style={{ ...auditWrapLineStyle, color: colors.danger }} title={record.error}>
                                                {localizeText("Extraction error", "Extraction error", "Extraction error")}: {record.error}
                                            </div>
                                        )}
                                        {record.upserted && record.upserted.length > 0 && (
                                            <div style={{ fontSize: "0.72rem", color: colors.success, marginTop: "3px" }}>{localizeText("Upserted", "Upserted", "Upserted")}: {record.upserted.join(", ")}</div>
                                        )}
                                        {skipReasons.length > 0 && (
                                            <div style={{ fontSize: "0.72rem", color: colors.textMuted, marginTop: "3px" }}>
                                                {localizeText("Skip reasons", "Skip reasons", "Skip reasons")}: {skipReasons.map(([reason, count]) => `${reason} x${count}`).join("; ")}
                                            </div>
                                        )}
                                        {unsupported.length > 0 && (
                                            <div style={{ fontSize: "0.72rem", color: colors.warning, marginTop: "3px" }}>
                                                {localizeText("Unsupported evidence", "Unsupported evidence", "Unsupported evidence")}: {unsupported.map(([step, count]) => `${step} x${count}`).join("; ")}
                                            </div>
                                        )}
                                        {record.decisions && record.decisions.length > 0 && (
                                            <div style={{ display: "flex", flexDirection: "column", gap: "3px", marginTop: "5px" }}>
                                                {record.decisions.slice(0, 3).map((decision, decisionIndex) => {
                                                    const qualityScore = decision.quality?.score;
                                                    const evidenceScore = decision.evidence?.score;
                                                    const detail = [
                                                        decision.reason,
                                                        decision.matched_skill_name ? `${localizeText("matched", "matched", "matched")}: ${decision.matched_skill_name}` : "",
                                                        typeof qualityScore === "number" ? `Q:${qualityScore}` : "",
                                                        typeof evidenceScore === "number" ? `E:${evidenceScore}` : "",
                                                    ].filter(Boolean).join(" / ");
                                                    return (
                                                        <div key={`${record.timestamp}-${decisionIndex}-${decision.pattern_name}`} style={{ ...auditWrapLineStyle, fontSize: "0.7rem", color: decision.action === "skipped" ? colors.textMuted : colors.success }} title={detail || undefined}>
                                                            {decision.action}: {decision.pattern_name || localizeText("unnamed", "unnamed", "unnamed")}{detail ? ` - ${detail}` : ""}
                                                        </div>
                                                    );
                                                })}
                                                {record.decisions.length > 3 && (
                                                    <div style={{ fontSize: "0.68rem", color: colors.textMuted }}>{localizeText("More decisions", "More decisions", "More decisions")}: {record.decisions.length - 3}</div>
                                                )}
                                            </div>
                                        )}
                                    </div>
                                );
                            })}
                        </div>
                    )}
                    {/* Error */}
                    {error && (
                        <div style={remoteErrorStateStyle}>
                            {error}
                        </div>
                    )}

                    {/* Loading */}
                    {loading && (
                        <div style={remoteLoadingStateStyle}>{localizeText("Loading...", "加载中...", "載入中...")}</div>
                    )}

                    {/* Learned skills table */}
                    {!loading && learnedSkills.length > 0 && (
                        <div style={remoteTableContainerStyle}>
                            <table style={learnedSkillsTableStyle}>
                                <colgroup><col style={{ width: "36px" }} /><col style={{ width: "18%" }} /><col style={{ width: "22%" }} /><col style={{ width: "40px" }} /><col style={{ width: "56px" }} /><col style={{ width: "58px" }} /><col style={{ width: "170px" }} /></colgroup>
                                <thead>
                                    <tr style={{ background: colors.surfaceMuted }}>
                                        <th style={{ ...thStyle, width: "36px", textAlign: "center" }}>
                                            <input type="checkbox" checked={learnedSkills.length > 0 && learnedSelected.size === learnedSkills.length} onChange={toggleLearnedSelectAll} />
                                        </th>
                                        <th style={{ ...thStyle, textAlign: "left" }}>{localizeText("Name", "\u540d\u79f0", "\u540d\u7a31")}</th>
                                        <th style={{ ...thStyle, textAlign: "left" }}>{localizeText("Description", "\u63cf\u8ff0", "\u63cf\u8ff0")}</th>
                                        <th style={{ ...thStyle, width: "40px", textAlign: "center", paddingLeft: 4, paddingRight: 4 }}>{localizeText("Source", "\u6765\u6e90", "\u4f86\u6e90")}</th>
                                        <th style={{ ...thStyle, width: "56px", textAlign: "center", paddingLeft: 4, paddingRight: 4 }}>{localizeText("Usage", "\u4f7f\u7528\u7edf\u8ba1", "\u4f7f\u7528\u7d71\u8a08")}</th>
                                        <th style={{ ...thStyle, width: "58px", textAlign: "center", paddingLeft: 4, paddingRight: 4 }}>{localizeText("Status", "\u72b6\u6001", "\u72c0\u614b")}</th>
                                        <th style={{ ...thStyle, width: "170px", textAlign: "left" }}>{localizeText("Actions", "\u64cd\u4f5c", "\u64cd\u4f5c")}</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {learnedSkills.map((s) => (
                                        <tr key={s.name} style={{ borderTop: `1px solid ${colors.border}` }}>
                                            <td style={{ ...tdStyle, textAlign: "center" }}>
                                                <input type="checkbox" checked={learnedSelected.has(s.name)} onChange={() => toggleLearnedSelect(s.name)} />
                                            </td>
                                            <td style={{ ...tdStyle, textAlign: "left" }}>{s.name}</td>
                                            <td style={{ ...tdStyle, textAlign: "left" }}>
                                                <div style={learnedDescriptionPreviewStyle} title={s.description || undefined}>{getLearnedSkillDescriptionPreview(s.description || "")}</div>
                                            </td>
                                            <td style={{ ...tdStyle, textAlign: "center", paddingLeft: 4, paddingRight: 4 }}>
                                                <span
                                                    title={learnedSourceTooltip(s.source ?? "")}
                                                    style={{ cursor: "default" }}
                                                >
                                                    {learnedSourceIcon(s.source ?? "")}
                                                </span>
                                            </td>
                                            <td style={{ ...tdStyle, textAlign: "center", paddingLeft: 4, paddingRight: 4 }}>
                                                {(s.usage_count ?? 0) > 0 ? (
                                                    <span style={{ fontSize: "0.72rem", color: colors.textSecondary }}>
                                                        {s.usage_count}{localizeText("x", "次", "次")} / {Math.round((s.success_rate ?? 0) * 100)}%
                                                    </span>
                                                ) : (
                                                    <span style={{ fontSize: "0.72rem", color: colors.textMuted }}>{localizeText("Unused", "未使用", "未使用")}</span>
                                                )}
                                            </td>
                                            <td style={{ ...tdStyle, textAlign: "center", paddingLeft: 4, paddingRight: 4 }}>
                                                <span style={statusDotStyle(s.status === "active")}>
                                                    <span style={{ width: 6, height: 6, borderRadius: "50%", background: s.status === "active" ? colors.success : colors.border, flexShrink: 0 }} />
                                                    {s.status === "active" ? localizeText("Active", "启用", "啟用") : localizeText("Disabled", "停用", "停用")}
                                                </span>
                                            </td>
                                            <td style={{ ...tdStyle, textAlign: "left", paddingLeft: 4 }}>
                                                <div style={{ display: "flex", alignItems: "center", justifyContent: "flex-start", gap: "4px", flexWrap: "wrap" }}>
                                                    <button
                                                        className="btn-secondary"
                                                        style={smallBtnStyle}
                                                        onClick={() => setDetailSkill(s)}
                                                        disabled={busy}
                                                    >
                                                        {localizeText("View", "查看", "查看")}
                                                    </button>
                                                    <button
                                                        className="btn-secondary btn-danger"
                                                        style={smallBtnStyle}
                                                        onClick={() => handleLearnedDelete(s.name)}
                                                        disabled={busy}
                                                    >
                                                        {busy ? localizeText("Deleting...", "删除中...", "刪除中...") : localizeText("Delete", "删除", "刪除")}
                                                    </button>
                                                    <button
                                                        className="btn-secondary"
                                                        style={uploadBtnStyle}
                                                        disabled={uploadingSkill === s.name || busy}
                                                        onClick={async () => {
                                                            setUploadingSkill(s.name);
                                                            try {
                                                                const sid = await UploadNLSkillToMarket(s.name);
                                                                showToast(`${localizeText("Submission ID", "提交ID", "提交ID")}: ${sid}`, "success");
                                                                await loadData();
                                                            } catch (e: any) {
                                                                showToast(`${e?.message || e}`, "error");
                                                            } finally {
                                                                setUploadingSkill(null);
                                                            }
                                                        }}
                                                    >
                                                        {uploadingSkill === s.name ? localizeText("Uploading...", "上传中...", "上傳中...") : s.hub_skill_id ? localizeText("⬆ Re-upload", "⬆ 重新上传", "⬆ 重新上傳") : localizeText("⬆ Upload", "⬆ 上传", "⬆ 上傳")}
                                                    </button>
                                                    {s.hub_skill_id && <span title={localizeText("Uploaded to Capability Market", "已上传到能力市场", "已上傳到能力市場")} style={{ fontSize: "0.68rem", color: colors.success }}>✅</span>}
                                                </div>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    )}

                    {!loading && learnedSkills.length === 0 && !error && (
                        <div style={skillsEmptyStateStyle}>
                            {localizeText("No learned skills yet. MaClaw automatically learns and generates skills during use.", "暂无自学习技能。MaClaw 在使用过程中会自动学习并生成技能。", "暫無自學習技能。MaClaw 在使用過程中會自動學習並生成技能。")}
                        </div>
                    )}
                </>
            )}

            {/* === External Skill Directories Tab === */}
            {activeTab === "extdirs" && (
                <>
                    <div style={{ fontSize: "0.76rem", color: colors.textSecondary, marginBottom: "4px" }}>
                        {localizeText(
                            "Add external directories that contain skill subdirectories (each with skill.md or skill.yaml).",
                            "添加包含技能子目录的外部目录（每个子目录需包含 skill.md 或 skill.yaml）。",
                            "添加包含技能子目錄的外部目錄（每個子目錄需包含 skill.md 或 skill.yaml）。"
                        )}
                    </div>

                    {/* Add directory input */}
                    <div style={{ display: "flex", gap: "6px" }}>
                        <input
                            className="form-input"
                            value={extDirInput}
                            onChange={(e) => setExtDirInput(e.target.value)}
                            onKeyDown={(e) => {
                                if (e.key === "Enter" && extDirInput.trim()) {
                                    e.preventDefault();
                                    handleAddExtDir();
                                }
                            }}
                            placeholder={localizeText("Enter directory path...", "输入目录路径...", "輸入目錄路徑...")}
                            spellCheck={false}
                            style={{ flex: 1, fontSize: "0.78rem" }}
                            disabled={extDirAdding}
                        />
                        <button
                            className="btn-secondary"
                            style={{ fontSize: "0.78rem", padding: "4px 10px", flexShrink: 0 }}
                            disabled={extDirAdding}
                            onClick={async () => {
                                setExtDirError("");
                                try {
                                    const selected = await SelectProjectDir();
                                    if (selected) setExtDirInput(selected);
                                } catch (err) {
                                    setExtDirError(localizeHubError(String(err)));
                                }
                            }}
                            title={localizeText("Browse for directory", "浏览目录", "瀏覽目錄")}
                        >
                            ...
                        </button>
                        <button
                            className="btn-primary"
                            style={{ fontSize: "0.78rem", padding: "4px 12px", flexShrink: 0 }}
                            disabled={!extDirInput.trim() || extDirAdding}
                            onClick={handleAddExtDir}
                        >
                            {extDirAdding ? localizeText("Adding...", "添加中...", "添加中...") : localizeText("+ Add", "+ 添加", "+ 添加")}
                        </button>
                        <button
                            className="btn-secondary"
                            style={{ fontSize: "0.78rem", padding: "4px 12px", flexShrink: 0 }}
                            onClick={loadExtDirs}
                            disabled={extDirsLoading}
                        >
                            {extDirsLoading ? localizeText("Refreshing...", "刷新中...", "重新整理中...") : localizeText("🔄 Refresh", "🔄 刷新", "🔄 重新整理")}
                        </button>
                    </div>

                    {/* Error */}
                    {extDirError && (
                        <div style={remoteErrorStateStyle}>
                            {extDirError}
                        </div>
                    )}

                    {/* Loading */}
                    {extDirsLoading && (
                        <div style={remoteLoadingStateStyle}>
                            {localizeText("Loading...", "加载中...", "載入中...")}
                        </div>
                    )}

                    {/* Directory list */}
                    {!extDirsLoading && extDirs.length > 0 && (
                        <div style={remoteTableContainerStyle}>
                            <table style={{ width: "100%", borderCollapse: "collapse", fontSize: "0.76rem" }}>
                                <thead>
                                    <tr style={{ background: colors.surfaceMuted }}>
                                        <th style={thStyle}>{localizeText("Directory Path", "目录路径", "目錄路徑")}</th>
                                        <th style={{ ...thStyle, width: "100px" }}>{localizeText("Skills Found", "技能数量", "技能數量")}</th>
                                        <th style={{ ...thStyle, width: "120px" }}>{localizeText("Status", "状态", "狀態")}</th>
                                        <th style={{ ...thStyle, width: "80px" }}>{localizeText("Actions", "操作", "操作")}</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {extDirs.map((d) => (
                                        <tr key={d.path} style={{ borderTop: `1px solid ${colors.border}` }}>
                                            <td style={tdStyle}>
                                                <span style={{ fontFamily: "monospace", fontSize: "0.74rem", wordBreak: "break-all" }}>{d.path}</span>
                                            </td>
                                            <td style={{ ...tdStyle, textAlign: "center" }}>{d.skill_count}</td>
                                            <td style={tdStyle}>
                                                {d.error ? (
                                                    <span style={{ color: colors.danger, fontSize: "0.72rem" }}>⚠ {d.error}</span>
                                                ) : (
                                                    <span style={{ color: colors.success, fontSize: "0.72rem" }}>✅ {localizeText("OK", "正常", "正常")}</span>
                                                )}
                                            </td>
                                            <td style={tdStyle}>
                                                <button
                                                    className="btn-secondary btn-danger"
                                                    style={smallBtnStyle}
                                                    onClick={() => handleRemoveExtDir(d.path)}
                                                    disabled={extDirRemoving}
                                                >
                                                    {localizeText("Remove", "移除", "移除")}
                                                </button>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    )}

                    {!extDirsLoading && extDirs.length === 0 && !extDirError && (
                        <div style={skillsEmptyStateStyle}>
                            {localizeText(
                                "No external skill directories configured. Add a directory above to scan for skills.",
                                "暂无外部技能目录。在上方添加目录以扫描技能。",
                                "暫無外部技能目錄。在上方添加目錄以掃描技能。"
                            )}
                        </div>
                    )}
                </>
            )}
            </div>

            {detailSkill && (
                <div className="modal-backdrop" onMouseDown={(e) => {
                    backdropMouseDownRef.current = e.target === e.currentTarget;
                }} onClick={(e) => {
                    if (backdropMouseDownRef.current && e.target === e.currentTarget) {
                        setDetailSkill(null);
                    }
                    backdropMouseDownRef.current = false;
                }}>
                    <div className="modal-content" onClick={(e) => e.stopPropagation()} style={{ width: "min(560px, 92vw)", textAlign: "left" }}>
                        <div className="modal-header">
                            <h3 style={{ fontSize: "0.88rem", margin: 0 }}>{localizeText("Skill Details", "技能详情", "技能詳情")}</h3>
                            <button className="btn-close" onClick={() => setDetailSkill(null)}>×</button>
                        </div>
                        <div className="modal-body" style={{ display: "flex", flexDirection: "column", gap: "10px", maxHeight: "70vh", overflowY: "auto" }}>
                            <div style={detailGridStyle}>
                                <div><strong>{localizeText("Name", "名称", "名稱")}</strong><div>{detailSkill.name}</div></div>
                                <div><strong>{localizeText("Source", "来源", "來源")}</strong><div>{detailSkill.source || "—"}</div></div>
                                <div><strong>{localizeText("Status", "状态", "狀態")}</strong><div>{localizeSkillStatus(detailSkill.status)}</div></div>
                                <div><strong>{localizeText("Execution", "执行类型", "執行類型")}</strong><div>{getExecutionClassLabel(detailSkill.execution_class) || "—"}</div></div>
                                <div><strong>{localizeText("Created", "创建时间", "建立時間")}</strong><div>{detailSkill.created_at ? new Date(detailSkill.created_at).toLocaleString() : "—"}</div></div>
                                <div><strong>{localizeText("Usage", "使用统计", "使用統計")}</strong><div>{(detailSkill.usage_count ?? 0) > 0 ? `${detailSkill.usage_count}${localizeText("x", "次", "次")} / ${Math.round((detailSkill.success_rate ?? 0) * 100)}%` : localizeText("Unused", "未使用", "未使用")}</div></div>
                            </div>
                            <div>
                                <strong>{localizeText("Description", "描述", "描述")}</strong>
                                <div style={detailTextBlockStyle}>{detailSkill.description || "—"}</div>
                            </div>
                            <div>
                                <strong>{localizeText("Triggers", "触发短语", "觸發短語")}</strong>
                                <div style={{ display: "flex", flexWrap: "wrap", gap: "4px", marginTop: "4px" }}>
                                    {(detailSkill.triggers || []).length > 0
                                        ? detailSkill.triggers.map((trigger, index) => <span key={index} style={tagStyle}>{trigger}</span>)
                                        : <span style={{ fontSize: "0.74rem", color: colors.textMuted }}>—</span>}
                                </div>
                            </div>
                            <div>
                                <strong>{localizeText("Steps", "操作步骤", "操作步驟")}</strong>
                                <pre style={detailPreStyle}>{formatStepText(detailSkill.steps || [])}</pre>
                            </div>
                            <div style={detailGridStyle}>
                                <div><strong>{localizeText("Source Project", "来源项目", "來源項目")}</strong><div>{detailSkill.source_project || "—"}</div></div>
                                <div><strong>{localizeText("Hub Skill ID", "市场技能ID", "市場技能ID")}</strong><div>{detailSkill.hub_skill_id || "—"}</div></div>
                                <div><strong>{localizeText("Last Used", "最近使用", "最近使用")}</strong><div>{detailSkill.last_used_at ? new Date(detailSkill.last_used_at).toLocaleString() : "—"}</div></div>
                                <div><strong>{localizeText("Last Error", "最近错误", "最近錯誤")}</strong><div>{detailSkill.last_error || "—"}</div></div>
                            </div>
                        </div>
                        <div className="modal-footer">
                            <button className="btn-secondary" onClick={() => setDetailSkill(null)}>{localizeText("Close", "关闭", "關閉")}</button>
                        </div>
                    </div>
                </div>
            )}

            {/* Create/Edit form dialog */}
            {showForm && (
                <div className="modal-backdrop" onMouseDown={(e) => {
                    backdropMouseDownRef.current = e.target === e.currentTarget;
                }} onClick={(e) => {
                    if (backdropMouseDownRef.current && e.target === e.currentTarget) {
                        closeForm();
                    }
                    backdropMouseDownRef.current = false;
                }}>
                    <div className="modal-content" onClick={(e) => e.stopPropagation()} style={{ width: "min(420px, 95vw)", textAlign: "left" }}>
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
                                    placeholder={'- action: "bash"\n  params:\n    command: "echo hello"\n  on_error: "stop"'}
                                    spellCheck={false}
                                    style={{ minHeight: "120px", fontFamily: "monospace", fontSize: "0.76rem", resize: "vertical" }}
                                />
                            </div>
                            {formError && (
                                <div style={{ ...remoteErrorStateStyle, fontSize: "0.76rem", padding: "4px 8px" }}>
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
const thStyle: CSSProperties = {
    ...remoteTableHeaderCellStyle,
};

const tdStyle: CSSProperties = {
    ...remoteTableCellStyle,
};

const descCellStyle: CSSProperties = {
    maxWidth: "100%",
    overflow: "hidden",
    display: "-webkit-box",
    WebkitLineClamp: 2,
    WebkitBoxOrient: "vertical",
    textAlign: "left",
    lineHeight: 1.45,
    overflowWrap: "anywhere",
};

const tagStyle: CSSProperties = {
    ...remoteTagStyle,
};

const statusBadgeStyle: CSSProperties = {
    ...remoteStatusBadgeStyle,
};

const smallBtnStyle: CSSProperties = {
    fontSize: "0.72rem",
    padding: "2px 8px",
};

const iconBtnStyle: CSSProperties = {
    width: "28px",
    height: "28px",
    padding: 0,
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    fontSize: "0.78rem",
    lineHeight: 1,
};

const deleteIconBtnStyle: CSSProperties = {
    ...iconBtnStyle,
    color: colors.textMuted,
};

const detailGridStyle: CSSProperties = {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit, minmax(min(180px, 100%), 1fr))",
    gap: "10px 14px",
    fontSize: "0.76rem",
    color: colors.text,
    overflowWrap: "anywhere",
};

const detailTextBlockStyle: CSSProperties = {
    marginTop: "4px",
    fontSize: "0.76rem",
    color: colors.text,
    whiteSpace: "pre-wrap",
    wordBreak: "break-word",
    lineHeight: 1.5,
    textAlign: "left",
};

const detailPreStyle: CSSProperties = {
    ...remoteCodeBlockStyle,
};

const learnedSkillsTableStyle: CSSProperties = { width: "100%", minWidth: 674, tableLayout: "fixed", borderCollapse: "collapse", fontSize: "0.76rem" };

const learnedDescriptionPreviewStyle: CSSProperties = { overflow: "hidden", display: "-webkit-box", WebkitLineClamp: 2, WebkitBoxOrient: "vertical", cursor: "help", textAlign: "left", lineHeight: 1.45, overflowWrap: "anywhere" };

const auditWrapLineStyle: CSSProperties = { fontSize: "0.72rem", marginTop: "3px", lineHeight: 1.45, textAlign: "left", overflowWrap: "anywhere" };

const hubSkillDescriptionStyle: CSSProperties = {
    fontSize: "0.76rem",
    color: colors.textSecondary,
    marginTop: "4px",
    lineHeight: 1.45,
    display: "-webkit-box",
    WebkitLineClamp: 2,
    WebkitBoxOrient: "vertical",
    overflow: "hidden",
    textAlign: "left",
    overflowWrap: "anywhere",
};

const skillsPanelShellStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "8px",
    height: "100%",
    minHeight: 0,
    minWidth: 0,
    textAlign: "left",
};

const skillsTabBarStyle: CSSProperties = {
    display: "flex",
    gap: "2px",
    borderBottom: `1px solid ${colors.border}`,
    backgroundColor: colors.bg,
    flexShrink: 0,
    padding: "2px 0 0 0",
    overflowX: "auto",
    overflowY: "hidden",
};

const skillsTabContentStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "8px",
    flex: "1 1 auto",
    minHeight: 0,
    minWidth: 0,
    overflowY: "auto",
    overflowX: "hidden",
    padding: "0 0 4px 0",
    textAlign: "left",
};

const localSkillsToolbarStyle: CSSProperties = {
    display: "grid",
    gridTemplateColumns: "minmax(140px, 1fr) auto",
    gap: "12px",
    alignItems: "center",
};
const localSkillsActionBarStyle: CSSProperties = {
    display: "flex",
    gap: "8px",
    justifyContent: "flex-end",
    alignItems: "center",
    flexWrap: "wrap",
};
const localSkillsHintStyle: CSSProperties = {
    fontSize: "0.74rem",
    color: colors.textMuted,
    textAlign: "left",
    lineHeight: 1.5,
    margin: "2px 0 4px",
    maxWidth: "72ch",
};
const skillsEmptyStateStyle: CSSProperties = {
    ...remoteEmptyStateStyle,
    textAlign: "left",
    lineHeight: 1.5,
};
const localSkillsTableContainerStyle: CSSProperties = {
    ...remoteTableContainerStyle,
    flex: "1 1 auto",
    minHeight: 0,
    width: "100%",
    overflow: "auto",
};
const localSkillsTableStyle: CSSProperties = {
    width: "100%",
    minWidth: "880px",
    tableLayout: "fixed",
    borderCollapse: "collapse",
    fontSize: "0.76rem",
};

const localSkillsRowActionsStyle: CSSProperties = {
    display: "inline-flex",
    justifyContent: "center",
    alignItems: "center",
    gap: "6px",
    flexWrap: "nowrap",
};

const tabBtnStyle: CSSProperties = {
    background: "none",
    border: "none",
    borderBottom: "2px solid transparent",
    padding: "6px 14px",
    fontSize: "0.78rem",
    color: colors.textSecondary,
    cursor: "pointer",
    fontWeight: 500,
    transition: "color 0.15s, border-color 0.15s",
};

const tabBtnActiveStyle: CSSProperties = {
    color: colors.primary,
    borderBottomColor: colors.primary,
    fontWeight: 600,
};

const hubCardStyle: CSSProperties = {
    ...remoteCardStyle,
};

const sourceTextStyle: CSSProperties = {
    fontSize: "0.72rem",
    color: colors.textSecondary,
    whiteSpace: "nowrap",
};

// Style utilities and formatters extracted to skillsManagementUtils.ts
