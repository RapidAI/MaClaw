import { type CSSProperties, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useDialog } from '../CustomDialog';
import { useToast } from '../Toast';
import { EventsOn } from '../../../wailsjs/runtime';
import { EVENT_CONFIG_CHANGED, EVENT_CONFIG_UPDATED, EVENT_MACLAW_CONFIG_CHANGED, EVENT_SKILL_AUTO_DISCOVERED, EVENT_SKILL_EVOLUTION_CANCELLED, EVENT_SKILL_EVOLUTION_ROLLED_BACK, EVENT_SKILL_EVOLUTION_TIMED_OUT, EVENT_SKILL_EXECUTION_FAILED, EVENT_SKILL_INDEX_REFRESHED, EVENT_SKILL_OPTIMIZED, EVENT_SKILL_REPAIRED, EVENT_SKILL_USAGE_UPDATED } from '../../constants/events';
import { SkillInstallProgressPanel } from './SkillInstallProgressPanel';
import { SkillRepairDraftsPanel } from './SkillRepairDraftsPanel';
import { MaclawAppMarketPreview } from './MaclawAppMarketPreview';
import { SkillProductBadge, isMaclawAppSearchResult } from './SkillProductBadge';
import { SkillSourceBadge } from './SkillSourceBadge';
import { formatInstalledOpenPanelMessage, localizeMiniAppPack, miniAppLabels } from '../../i18n/maclawMiniAppLabels';
import { StatusGlyph } from '../ai/WorkbenchIcons';
import { displayHubVersion, executionClassBadgeStyle, formatDate, formatDownloads, renderStars, shouldShowTrustBadge, statusDotStyle, trustBadgeStyle, trustLevelLabel, uploadBtnStyle } from './skillsManagementUtils';
import { colors, remoteCardStyle, remoteCodeBlockStyle, remoteEmptyStateStyle, remoteErrorStateStyle, remoteInfoPanelStyle, remoteLoadingStateStyle, remoteStatusBadgeStyle, remoteTableCellStyle, remoteTableContainerStyle, remoteTableHeaderCellStyle, remoteTagStyle } from './styles';
import { AddExternalSkillDir, ApplySkillMaintenanceAction, BatchSetNLSkillStatus, CancelSkillEvolution, CheckHubSkillUpdates, CreateNLSkill, DeleteNLSkill, DiagnoseSkillFiles, ExportLearnedSkillsZip, ExportTextFile, GetExperienceAuditHealth, GetHubRecommendations, GetSkillEvolutionStatus, ImportLearnedSkillsZip, ImportNLSkillZip, InstallMixedSkill, ListExperienceAudit, ListExternalSkillDirsDetailed, ListNLSkills, ListSkillEvolutionAudit, ListSkillEvolutionCompensations, ListSkillMaintenanceDrafts, ListSkillYAMLBackups, LoadConfig, OpenFileOrShowInFolder, OpenSystemUrl, PatchConfigFields, RemoveExternalSkillDir, RenameNLSkill, ResolveCriticalConfirm, RestoreSkillYAMLBackup, SearchMixedSkills, SelectProjectDir, SetNLSkillStatus, TriggerSkillOptimize, TriggerSkillSelfRepair, UpdateHubSkill, UpdateNLSkill, UploadNLSkillToMarket, VerifyAndActivateNLSkillWithArgs } from '../../../wailsjs/go/main/App';
import { corelib } from '../../../wailsjs/go/models';
import { openSettingsTab } from '../../utils/settingsNavigation';

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

interface SkillRepairRecord {
    timestamp?: string;
    error_class?: string;
    explanation?: string;
    success?: boolean;
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
    review_reason?: string;
    repair_attempt_count?: number;
    last_repair_at?: string;
    repair_history?: SkillRepairRecord[];
    optimization_count?: number;
    last_optimized_at?: string;
    verified_at?: string;
    verification_run_id?: string;
    verification_digest?: string;
    verification_gate_status?: string;
    is_maclaw_app?: boolean;
    maclaw_app_count?: number;
    maclaw_app_entry?: string;
    params?: Array<{ name: string; description?: string; required?: boolean }>;
    required_args?: string[];
    has_documentation?: boolean;
    mode?: string;
}

function isAgentGuidedWorkflow(skill: NLSkillDefinition): boolean {
    return skill.execution_class === "agent_guided_workflow";
}

// Builds before the workflow classification fix used this runner error as the
// whole reason to demote an imported multi-agent workflow to needs_review.
// It is not a safety finding, and the backend reconciles it on load. Keep a
// stale client-side copy out of the review queue while leaving genuine review
// decisions (security, repair, governance) visible and actionable.
function isLegacyAgentGuidedWorkflowRunnerReview(skill: NLSkillDefinition): boolean {
    if (!isAgentGuidedWorkflow(skill) || String(skill.status || "").toLowerCase() !== "needs_review") return false;
    const reason = String(skill.last_error || "").trim().toLowerCase();
    return reason.startsWith("runner incompatible:")
        && reason.includes("interactive agent orchestration")
        && reason.includes("gui skill runner");
}

interface SkillEvolutionStatus {
    pipeline_started?: boolean;
    pending_skills?: number;
    coalesced_notifications?: number;
    dropped_notifications?: number;
    processed_requests?: number;
    enable_repair?: boolean;
    enable_optimizer?: boolean;
    enable_promoter?: boolean;
    repair_cooldown?: string;
    has_repair_hook?: boolean;
    has_optimizer?: boolean;
    has_promoter?: boolean;
    repair_cooldown_hours?: number;
    env_disabled?: boolean;
    config_enabled?: boolean;
    config_disabled?: boolean;
    disabled?: boolean;
    audit_available?: boolean;
    last_audit_error?: string;
    audit_failure_count?: number;
    last_audit_success_at?: string;
    oldest_pending_at?: string;
    queue_wait_seconds?: number;
    max_concurrent_workers?: number;
    max_concurrent_workers_configured?: number;
    observation_enabled?: boolean;
    worker_timeout_seconds?: number;
    active_skills?: number;
    cancelled_requests?: number;
    timed_out_requests?: number;
    pending_compensations?: number;
    compensation_queue_healthy?: boolean;
    compensation_queue_error?: string;
    requests?: Array<{ request_id?: string; skill?: string; state?: string; enqueued_at?: string; started_at?: string }>;
    failure_summaries?: Array<{ skill?: string; failure_count?: number; last_error?: string; last_error_class?: string; last_args_digest?: string; last_failure_at?: string }>;
}

interface SkillEvolutionCompensationSummary {
    request_id?: string;
    skill?: string;
    action?: string;
    attempts?: number;
    status?: string;
    failure_reason?: string;
    last_error?: string;
    created_at?: string;
    next_retry_at?: string;
}

type EvolutionActivityKind = "repaired" | "optimized" | "discovered" | "failed";

interface EvolutionActivityItem {
    id: string;
    kind: EvolutionActivityKind;
    skill: string;
    explanation: string;
    at: number; // epoch ms
}

interface EvolutionAuditRow {
    timestamp?: string;
    kind?: string;
    skill?: string;
    explanation?: string;
    source?: string;
    /** Optional outcome marker; kind=repair_draft + status="rejected" means the
     *  draft was rejected rather than left pending review. */
    status?: string;
    via?: string;
    action?: string;
    decision?: string;
    reason?: string;
    risk?: string;
    gate_status?: string;
    evidence_digest?: string;
    backup_version?: string;
    operator?: string;
    trigger?: string;
    schema_version?: string;
    request_id?: string;
    attempt?: string;
    config_revision?: string;
    evidence_mode?: string;
    failure_reason?: string;
    termination?: string;
}

interface MaintenancePatchDraft {
    kind?: string;
    skill?: string;
    skill_dir?: string;
    target_file?: string;
    required_args?: string[];
    suggested_yaml?: string;
    recommended_action?: string;
    /** Present on kind=attempt_repair review packets */
    error_class?: string;
    last_error?: string;
    action_hint?: string;
    evidence?: string[];
}

function isRepairPatchDraft(d: MaintenancePatchDraft | null | undefined): boolean {
    const kind = String(d?.kind || "").trim().toLowerCase();
    return kind === "attempt_repair" || kind === "repair";
}

function skillNamesEqual(a: string | undefined | null, b: string | undefined | null): boolean {
    return String(a || "").trim().toLowerCase() === String(b || "").trim().toLowerCase();
}

function csvEscapeCell(value: unknown): string {
    const s = String(value ?? "");
    if (/[",\n\r]/.test(s)) {
        return `"${s.replace(/"/g, '""')}"`;
    }
    return s;
}

function evolutionAuditToCSV(rows: EvolutionAuditRow[]): string {
    const header = ["timestamp", "kind", "skill", "source", "request_id", "attempt", "status", "via", "action", "decision", "reason", "risk", "gate_status", "evidence_mode", "failure_reason", "termination", "config_revision", "evidence_digest", "backup_version", "operator", "trigger", "schema_version", "explanation"];
    const lines = [header.join(",")];
    for (const row of rows) {
        lines.push([
            csvEscapeCell(row.timestamp),
            csvEscapeCell(row.kind),
            csvEscapeCell(row.skill),
            csvEscapeCell(row.source),
            csvEscapeCell(row.request_id),
            csvEscapeCell(row.attempt),
            csvEscapeCell(row.status),
            csvEscapeCell(row.via),
            csvEscapeCell(row.action),
            csvEscapeCell(row.decision),
            csvEscapeCell(row.reason),
            csvEscapeCell(row.risk),
            csvEscapeCell(row.gate_status),
            csvEscapeCell(row.evidence_mode),
            csvEscapeCell(row.failure_reason),
            csvEscapeCell(row.termination),
            csvEscapeCell(row.config_revision),
            csvEscapeCell(row.evidence_digest),
            csvEscapeCell(row.backup_version),
            csvEscapeCell(row.operator),
            csvEscapeCell(row.trigger),
            csvEscapeCell(row.schema_version),
            csvEscapeCell(row.explanation),
        ].join(","));
    }
    return lines.join("\n") + "\n";
}

function downloadTextFile(filename: string, content: string, mime: string) {
    const blob = new Blob([content], { type: mime });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    a.rel = "noopener";
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
}

/** all | 1h | 24h | 7d | 30d */
type EvolutionAuditTimeRange = "all" | "1h" | "24h" | "7d" | "30d";

function evolutionAuditTimeRangeCutoffMs(range: EvolutionAuditTimeRange, nowMs: number = Date.now()): number | null {
    switch (range) {
        case "1h":
            return nowMs - 60 * 60 * 1000;
        case "24h":
            return nowMs - 24 * 60 * 60 * 1000;
        case "7d":
            return nowMs - 7 * 24 * 60 * 60 * 1000;
        case "30d":
            return nowMs - 30 * 24 * 60 * 60 * 1000;
        default:
            return null;
    }
}

function parseEvolutionAuditTimestampMs(value: string | undefined): number | null {
    const raw = String(value || "").trim();
    if (!raw) return null;
    const ms = Date.parse(raw);
    return Number.isFinite(ms) ? ms : null;
}

function filterEvolutionAuditRows(
    rows: EvolutionAuditRow[],
    filterRaw: string,
    skillQueryRaw: string = "",
    timeRange: EvolutionAuditTimeRange = "all",
): EvolutionAuditRow[] {
    const filter = String(filterRaw || "all").trim().toLowerCase();
    const skillQuery = String(skillQueryRaw || "").trim().toLowerCase();
    const cutoff = evolutionAuditTimeRangeCutoffMs(timeRange);
    const known = new Set([
        "repaired",
        "failed",
        "yaml_restore",
        "maintenance_apply",
        "mark_needs_review",
        "optimized",
        "discovered",
        "queue_full",
        "repair_draft",
    ]);
    return rows.filter((row) => {
        const kind = String(row.kind || "other").trim().toLowerCase() || "other";
        if (filter && filter !== "all") {
            if (filter === "other") {
                if (known.has(kind)) return false;
            } else if (kind !== filter) {
                return false;
            }
        }
        if (cutoff != null) {
            const ts = parseEvolutionAuditTimestampMs(row.timestamp);
            // Keep undated rows when a time filter is active so operators don't lose context.
            if (ts != null && ts < cutoff) {
                return false;
            }
        }
        if (skillQuery) {
            const hay = [
                row.skill,
                row.kind,
                row.explanation,
                row.source,
            ].map((v) => String(v || "").toLowerCase()).join(" ");
            if (!hay.includes(skillQuery)) {
                return false;
            }
        }
        return true;
    });
}

function sanitizeFilenamePart(value: string): string {
    return String(value || "")
        .trim()
        .replace(/[<>:"/\\|?*\u0000-\u001f]+/g, "_")
        .replace(/\s+/g, "-")
        .slice(0, 48);
}

interface MaintenanceMergeDraft {
    kind?: string;
    primary_skill?: string;
    duplicate_skill?: string;
    recommended_keep?: string;
    recommended_retire?: string;
    reasons?: string[];
    recommended_action?: string;
}

interface MaintenanceDraftsSnapshot {
    ok?: boolean;
    plan_summary?: string;
    plan_actions?: number;
    patch_drafts?: MaintenancePatchDraft[];
    merge_drafts?: MaintenanceMergeDraft[];
    queued_repair?: Array<{ action?: string; skill?: string; reason?: string }>;
    patch_count?: number;
    merge_count?: number;
    error?: string;
}

const EVOLUTION_ACTIVITY_MAX = 20;

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
    product_kind?: string;
    is_maclaw_app?: boolean;
    maclaw_app_id?: string;
    maclaw_app_name?: string;
    maclaw_app_description?: string;
    maclaw_app_category?: string;
    maclaw_app_icon?: string;
    maclaw_app_input_mode?: string;
    maclaw_app_output_modes?: string[];
    maclaw_app_definition_sha256?: string;
    maclaw_app_test_evidence?: {
        run_id?: string;
        verified_at?: string;
        definition_fingerprint?: string;
        artifact_present?: boolean;
        artifact_name?: string;
        output_count?: number;
        primary_result?: string;
        result_payload?: Record<string, unknown>;
    };
    artifact_contract_required?: boolean;
    artifact_contract_output_modes?: string[];
    artifact_contract_presentation?: string;
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
            re: /hubcenter client not initialized/,
            fn: () => localizeText("HubCenter client not initialized", "HubCenter 客户端未初始化", "HubCenter 客戶端未初始化"),
        },
        {
            re: /hub marketplace client is not configured/,
            fn: () => localizeText("Hub marketplace client is not configured. Check your Hub URL and viewer token.", "Hub 能力市场客户端未配置，请检查 Hub 地址和查看令牌。", "Hub 能力市場客戶端未配置，請檢查 Hub 位址和查看令牌。"),
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
            re: /enterprise policy only allows installing skills from enterprise Hub/,
            fn: () => localizeText(
                "Enterprise policy only allows installing skills from enterprise Hub.",
                "企业策略仅允许从企业 Hub 安装技能。",
                "企業策略僅允許從企業 Hub 安裝技能。",
            ),
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
            re: /enterprise capability id is required/,
            fn: () => localizeText("Enterprise capability ID is required", "企业能力 ID 不能为空", "企業能力 ID 不能為空"),
        },
        {
            re: /skill (.+) has no steps and no SKILL\.md/,
            fn: (m) => localizeText(
                `Skill "${m[1]}" has no executable steps and no SKILL.md definition file.`,
                `技能「${m[1]}」没有可执行步骤，也没有 SKILL.md 定义文件。`,
                `技能「${m[1]}」沒有可執行步驟，也沒有 SKILL.md 定義檔案。`,
            ),
        },
        {
            re: /skill search blocked by security policy/,
            fn: () => localizeText(
                "Skill search blocked by security policy.",
                "技能搜索被安全策略阻止。",
                "技能搜尋被安全策略阻止。",
            ),
        },
        {
            re: /skill security scan rejected installation/,
            fn: () => localizeText(
                "Installation rejected by security scan.",
                "安全扫描拒绝了此次安装。",
                "安全掃描拒絕了此次安裝。",
            ),
        },
        {
            re: /skill security scan requires user approval and installation was rejected/,
            fn: () => localizeText(
                "Installation was rejected after security review.",
                "安全审查后安装被拒绝。",
                "安全審查後安裝被拒絕。",
            ),
        },
        {
            re: /network access is disabled by Hub security policy/,
            fn: () => localizeText(
                "Network access is disabled by Hub security policy.",
                "网络访问已被 Hub 安全策略禁用。",
                "網路存取已被 Hub 安全策略停用。",
            ),
        },
        {
            re: /network access is restricted by Hub security policy/,
            fn: () => localizeText(
                "Network access is restricted by Hub security policy.",
                "网络访问被 Hub 安全策略限制。",
                "網路存取被 Hub 安全策略限制。",
            ),
        },
        {
            re: /machine not registered/,
            fn: () => localizeText("Machine not registered. Please activate remote connection first.", "设备未注册，请先激活远程连接。", "裝置未註冊，請先啟用遠端連線。"),
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
    return LEARNED_SOURCES.has(String(source || "").trim().toLowerCase());
}

type HubSourceFilter = "all" | "hubcenter" | "clawhub" | "github";

// `SkillHub` and `SkillMarket` are legacy API names for HubCenter-backed
// search. They are implementation details, not separate user-facing markets.
export function hubSourceFilterMatches(source: string, filter: HubSourceFilter): boolean {
    if (filter === "all") return true;
    const normalized = source.trim().toLowerCase();
    switch (filter) {
        case "hubcenter":
            return ["enterprise_hub", "hub", "hubcenter", "skillmarket", "skillhub"].includes(normalized);
        default:
            return normalized === filter;
    }
}

function getStatusBadgeVariant(status: string): CSSProperties {
    switch (status) {
        case "active":
            return { background: colors.successBg, color: colors.success, border: `1px solid ${colors.success}` };
        case "disabled":
            return { background: colors.surfaceMuted, color: colors.textMuted, border: `1px solid ${colors.border}` };
        case "needs_setup":
            return { background: colors.infoBg, color: colors.primaryDark, border: `1px solid ${colors.primary}` };
        case "needs_review":
            return { background: colors.infoBg, color: colors.primaryDark, border: `1px solid ${colors.primary}` };
        case "staged":
            return { background: colors.warningBg, color: colors.warning, border: `1px solid ${colors.warning}` };
        default:
            return { background: colors.surfaceMuted, color: colors.textMuted, border: `1px solid ${colors.border}` };
    }
}

function learnedSourceIcon(source: string): string {
    if (source === "learned") return "REF";
    if (source === "crafted") return "TOOL";
    return "DIR";
}

const LEARNED_DESCRIPTION_PREVIEW_CHARS = 20;
const LOCAL_SKILLS_COL_PX = { name: 190, description: 160, type: 110, usage: 100, status: 72, actionsMin: 148 } as const;
export const LOCAL_SKILLS_DESCRIPTION_COL_PX = LOCAL_SKILLS_COL_PX.description;
export const LOCAL_SKILLS_TABLE_MIN_WIDTH_PX = LOCAL_SKILLS_COL_PX.name + LOCAL_SKILLS_COL_PX.description + LOCAL_SKILLS_COL_PX.type + LOCAL_SKILLS_COL_PX.usage + LOCAL_SKILLS_COL_PX.status + LOCAL_SKILLS_COL_PX.actionsMin;

function previewSkillDescription(description: string, maxChars = LEARNED_DESCRIPTION_PREVIEW_CHARS, emptyText = "-"): { preview: string; tooltip?: string } {
    const normalized = description.trim().replace(/\s+/g, " ");
    if (!normalized) return { preview: emptyText };
    const chars = Array.from(normalized);
    return chars.length <= maxChars ? { preview: normalized } : { preview: chars.slice(0, maxChars).join("") + "...", tooltip: normalized };
}

export function getLearnedSkillDescriptionPreview(description: string, maxChars = LEARNED_DESCRIPTION_PREVIEW_CHARS, emptyText = "-"): string {
    return previewSkillDescription(description, maxChars, emptyText).preview;
}

export function skillDescriptionTooltip(description: string, maxChars = LEARNED_DESCRIPTION_PREVIEW_CHARS): string | undefined {
    return previewSkillDescription(description, maxChars, "").tooltip;
}

function renderSkillDescriptionPreview(description: string, extraStyle?: CSSProperties) {
    const { preview, tooltip } = previewSkillDescription(description, LEARNED_DESCRIPTION_PREVIEW_CHARS, "—");
    return <div style={{ ...localSkillsDescriptionPreviewStyle, ...(tooltip ? { cursor: "help" as const } : {}), ...extraStyle }} title={tooltip}>{preview}</div>;
}

function skillReviewReason(skill: NLSkillDefinition, localizeText: Props["localizeText"]): string {
	const reason = String(skill.review_reason || skill.last_error || "").trim();
    if (reason) return reason;
    if (skill.status === "needs_setup") {
        return localizeText("This skill needs configuration before use.", "\u8be5 Skill \u9700\u8981\u5b8c\u6210\u914d\u7f6e\u540e\u624d\u80fd\u4f7f\u7528\u3002", "\u8a72 Skill \u9700\u8981\u5b8c\u6210\u8a2d\u5b9a\u5f8c\u624d\u80fd\u4f7f\u7528\u3002");
    }
    if (skill.status === "needs_review") {
        return localizeText("This skill was marked for local review by safety, repair, or governance checks.", "\u8be5 Skill \u88ab\u672c\u5730\u5b89\u5168\u3001\u4fee\u590d\u6216\u6cbb\u7406\u68c0\u67e5\u6807\u8bb0\u4e3a\u9700\u8981\u4eba\u5de5\u5ba1\u6838\u3002", "\u8a72 Skill \u88ab\u672c\u5730\u5b89\u5168\u3001\u4fee\u5fa9\u6216\u6cbb\u7406\u6aa2\u67e5\u6a19\u8a18\u70ba\u9700\u8981\u4eba\u5de5\u5be9\u6838\u3002");
    }
	if (isAgentGuidedWorkflow(skill)) {
		return localizeText(
			"This imported workflow is ready for an AI-agent project task. It cannot run as one GUI Skill Runner step.",
			"该导入工作流应在 AI 助手任务中使用，不能作为单步 GUI Skill Runner 运行。",
			"此匯入工作流程應在 AI 助手任務中使用，不能作為單一步驟 GUI Skill Runner 執行。",
		);
	}
    return "";
}

function skillReviewReasonPreview(skill: NLSkillDefinition, localizeText: Props["localizeText"]): string {
    return getLearnedSkillDescriptionPreview(skillReviewReason(skill, localizeText), 96);
}

export function SkillsManagementPanel({ localizeText }: Props) {
    const { showConfirm, showPrompt } = useDialog();
    const { showToast } = useToast();

    const localizeSkillStatus = (status: string): string => {
        switch (status) {
            case "active": return localizeText("Active", "启用", "啟用");
            case "disabled": return localizeText("Disabled", "已禁用", "已停用");
            case "needs_setup": return localizeText("Needs Setup", "待配置", "待配置");
            case "needs_review": return localizeText("Needs Review", "待审核", "待審核");
            case "staged": return localizeText("Staged · verify", "待验证", "待驗證");
            default: return status || "—";
        }
    };
    const backdropMouseDownRef = useRef(false);
    const [activeTab, setActiveTab] = useState<"local" | "maclaw_app" | "hub" | "learned" | "extdirs" | "evolution">("local");
    // Derived: is the "My Skills" top-level tab active? (covers all sub-filters)
    const isMySkillsTabActive = activeTab === "local" || activeTab === "maclaw_app" || activeTab === "learned";
    const isSettingsTabActive = activeTab === "extdirs" || activeTab === "evolution";
    // Panel width tracking for responsive layout (table vs card)
    const panelRef = useRef<HTMLDivElement>(null);
    const [panelWidth, setPanelWidth] = useState(900);
    useEffect(() => {
        const el = panelRef.current;
        if (!el) return;
        const obs = new ResizeObserver((entries) => {
            for (const entry of entries) {
                const w = entry.contentRect.width;
                if (w > 0) setPanelWidth(w); // Ignore 0-width (hidden/unmounting)
            }
        });
        obs.observe(el);
        if (el.clientWidth > 0) setPanelWidth(el.clientWidth);
        return () => obs.disconnect();
    }, []);
    const [skills, setSkills] = useState<NLSkillDefinition[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);
    const [isConfiguringSkill, setIsConfiguringSkill] = useState(false);

    // Hub market state
    const [hubSearchQuery, setHubSearchQuery] = useState("");
    const [hubResults, setHubResults] = useState<MixedSkillSearchResult[]>([]);
    const [hubSearching, setHubSearching] = useState(false);
    const [hubError, setHubError] = useState("");
    const [hubSearched, setHubSearched] = useState(false);
    const [hubRecommendations, setHubRecommendations] = useState<MixedSkillSearchResult[]>([]);
    const [hubRecsLoading, setHubRecsLoading] = useState(false);
    // Hub filter/sort state
    const [hubFilterSource, setHubFilterSource] = useState<HubSourceFilter>("all");
    const [hubFilterTrust, setHubFilterTrust] = useState<string>("all");
    const [hubSortBy, setHubSortBy] = useState<string>("relevance");

    // Localize backend error messages
    const localizeHubError = useMemo(() => makeLocalizeHubError(localizeText), [localizeText]);

    // Filtered and sorted hub results
    const filteredHubResults = useMemo(() => {
        let results = hubResults;
        if (hubFilterSource !== "all") {
            results = results.filter((s) => hubSourceFilterMatches(s.source || "", hubFilterSource));
        }
        if (hubFilterTrust !== "all") {
            results = results.filter((s) => (s.trust_level || "") === hubFilterTrust);
        }
        if (hubSortBy === "downloads") {
            results = [...results].sort((a, b) => (b.downloads || 0) - (a.downloads || 0));
        } else if (hubSortBy === "rating") {
            results = [...results].sort((a, b) => (b.avg_rating || 0) - (a.avg_rating || 0));
        } else if (hubSortBy === "newest") {
            results = [...results].sort((a, b) => (b.created_at || "").localeCompare(a.created_at || ""));
        }
        return results;
    }, [hubResults, hubFilterSource, hubFilterTrust, hubSortBy]);

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
    const [detailActionBusy, setDetailActionBusy] = useState<"repair" | "optimize" | null>(null);

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

    // Skill evolution settings (self-repair cooldown + independent switches)
    const [repairCooldownHours, setRepairCooldownHours] = useState<number>(1);
    const [repairCooldownSaving, setRepairCooldownSaving] = useState(false);
    const [repairCooldownMsg, setRepairCooldownMsg] = useState("");
    const [evolutionEnabled, setEvolutionEnabled] = useState(true);
    const [evolutionEnabledSaving, setEvolutionEnabledSaving] = useState(false);
    const [observationEnabled, setObservationEnabled] = useState(true);
    const [observationSaving, setObservationSaving] = useState(false);
    const [maxConcurrentWorkers, setMaxConcurrentWorkers] = useState(2);
    const [workersSaving, setWorkersSaving] = useState(false);
    const [workerTimeoutSeconds, setWorkerTimeoutSeconds] = useState(180);
    const [workerTimeoutSaving, setWorkerTimeoutSaving] = useState(false);
    const [evolutionStatus, setEvolutionStatus] = useState<SkillEvolutionStatus | null>(null);
    const [evolutionStatusLoading, setEvolutionStatusLoading] = useState(false);
    const [evolutionActivity, setEvolutionActivity] = useState<EvolutionActivityItem[]>([]);
    const [evolutionAudit, setEvolutionAudit] = useState<EvolutionAuditRow[]>([]);
    const [evolutionAuditLoading, setEvolutionAuditLoading] = useState(false);
    /** all | repaired | failed | yaml_restore | maintenance_apply | mark_needs_review | optimized | discovered | other */
    const [evolutionAuditFilter, setEvolutionAuditFilter] = useState<string>("all");
    /** Free-text search over skill / kind / explanation / source */
    const [evolutionAuditSkillQuery, setEvolutionAuditSkillQuery] = useState("");
    /** all | 1h | 24h | 7d | 30d */
    const [evolutionAuditTimeRange, setEvolutionAuditTimeRange] = useState<EvolutionAuditTimeRange>("all");
    /** Shared focus for repair-draft ↔ audit bidirectional highlight */
    const [evolutionFocusSkill, setEvolutionFocusSkill] = useState<string | null>(null);
    /** When true and a focus skill is set, audit list only shows that skill */
    const [evolutionAuditFocusOnly, setEvolutionAuditFocusOnly] = useState(false);
    /** Live progress for sequential batch repair/optimize */
    const [batchProgress, setBatchProgress] = useState<{
        kind: "repair" | "optimize";
        total: number;
        current: number;
        currentName: string;
        succeeded: string[];
        failed: Array<{ name: string; error: string }>;
        cancelled?: boolean;
        done?: boolean;
    } | null>(null);
    const batchCancelRef = useRef(false);
    const [maintenanceDrafts, setMaintenanceDrafts] = useState<MaintenanceDraftsSnapshot | null>(null);
    const [maintenanceDraftsLoading, setMaintenanceDraftsLoading] = useState(false);
    const [evolutionCompensations, setEvolutionCompensations] = useState<SkillEvolutionCompensationSummary[]>([]);
    const [evolutionCompensationsLoading, setEvolutionCompensationsLoading] = useState(false);
    const [evolutionCompensationsError, setEvolutionCompensationsError] = useState("");
    const [expandedDraftKey, setExpandedDraftKey] = useState<string | null>(null);
    /** skillName -> { versions desc, latest, selected } for YAML rollback UI */
    const [yamlBackupInfo, setYamlBackupInfo] = useState<Record<string, { versions: number[]; latest: number; selected: number }>>({});
    /** How many filtered audit rows to render (pagination for long lists) */
    const [auditVisibleCount, setAuditVisibleCount] = useState(30);
    const [evolutionHelpOpen, setEvolutionHelpOpen] = useState(false);
    const evolutionAuditPanelRef = useRef<HTMLDivElement | null>(null);
    const evolutionDraftsPanelRef = useRef<HTMLDivElement | null>(null);

    const loadEvolutionAudit = useCallback(async () => {
        setEvolutionAuditLoading(true);
        try {
            const rows = await ListSkillEvolutionAudit(200);
            setEvolutionAudit(Array.isArray(rows) ? rows : []);
        } catch {
            setEvolutionAudit([]);
        } finally {
            setEvolutionAuditLoading(false);
        }
    }, []);

    const loadEvolutionCompensations = useCallback(async () => {
        setEvolutionCompensationsLoading(true);
        setEvolutionCompensationsError("");
        try {
            const result = await ListSkillEvolutionCompensations();
            if (!result || result.ok === false || !Array.isArray(result.items)) {
                setEvolutionCompensations([]);
                setEvolutionCompensationsError(String(result?.error || "recovery queue unavailable"));
                return;
            }
            setEvolutionCompensations(result.items as SkillEvolutionCompensationSummary[]);
        } catch {
            setEvolutionCompensations([]);
            setEvolutionCompensationsError("recovery queue unavailable");
        } finally {
            setEvolutionCompensationsLoading(false);
        }
    }, []);

    const loadMaintenanceDrafts = useCallback(async () => {
        setMaintenanceDraftsLoading(true);
        try {
            const snap = await ListSkillMaintenanceDrafts();
            const next = snap && typeof snap === "object" ? snap as MaintenanceDraftsSnapshot : null;
            setMaintenanceDrafts(next);
            // Prefetch YAML backup lists for file-backed contract patch drafts only.
            const names = (next?.patch_drafts || [])
                .filter((d) => !isRepairPatchDraft(d) && !!d.skill_dir)
                .map((d) => String(d.skill || "").trim())
                .filter(Boolean);
            const unique = Array.from(new Set(names));
            if (unique.length > 0) {
                const entries = await Promise.all(unique.map(async (name) => {
                    try {
                        const info = await ListSkillYAMLBackups(name);
                        if (!info?.ok) return null;
                        const versions: number[] = Array.isArray(info.versions)
                            ? [...(info.versions as number[])].sort((a, b) => b - a)
                            : [];
                        const latest = Number(info.latest || (versions[0] || 0));
                        return [name, { versions, latest, selected: latest }] as const;
                    } catch {
                        return null;
                    }
                }));
                setYamlBackupInfo((prev) => {
                    const merged = { ...prev };
                    for (const e of entries) {
                        if (e) merged[e[0]] = e[1];
                    }
                    return merged;
                });
            }
        } catch {
            setMaintenanceDrafts(null);
        } finally {
            setMaintenanceDraftsLoading(false);
        }
    }, []);

    const copyText = useCallback(async (text: string, okMsg: string) => {
        try {
            await navigator.clipboard?.writeText(text);
            showToast(okMsg, "success");
        } catch {
            showToast(localizeText("Copy failed", "复制失败", "複製失敗"), "error");
        }
    }, [localizeText, showToast]);

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

    const handleApplyMergeDraft = useCallback(async (d: MaintenanceMergeDraft) => {
        const keep = d.recommended_keep || d.primary_skill || "";
        const retire = d.recommended_retire || d.duplicate_skill || "";
        if (!keep || !retire) {
            showToast(localizeText("Incomplete merge draft", "合并草案不完整", "合併草案不完整"), "error");
            return;
        }
        const step1 = await showConfirm(
            localizeText(
                `Retire skill "${retire}" and keep "${keep}"?\n\nThis disables the retire candidate (metadata only; files are not deleted).`,
                `确定退役「${retire}」、保留「${keep}」吗？\n\n仅禁用退役技能元数据，不会删除文件。`,
                `確定退役「${retire}」、保留「${keep}」嗎？\n\n僅停用退役技能元資料，不會刪除檔案。`,
            ),
            localizeText("Confirm merge retire", "确认合并退役", "確認合併退役"),
            {
                confirmText: localizeText("Continue", "继续", "繼續"),
                cancelText: localizeText("Cancel", "取消", "取消"),
            },
        );
        if (!step1) return;
        const step2 = await showConfirm(
            localizeText(
                `Final confirmation: disable "${retire}" now?`,
                `最终确认：现在禁用「${retire}」？`,
                `最終確認：現在停用「${retire}」？`,
            ),
            localizeText("Final confirmation", "最终确认", "最終確認"),
            {
                confirmText: localizeText("Retire now", "立即退役", "立即退役"),
                cancelText: localizeText("Cancel", "取消", "取消"),
            },
        );
        if (!step2) return;
        setBusy(true);
        try {
            const res = await ApplySkillMaintenanceAction("merge_duplicate", keep, retire, true, true);
            if (res?.ok) {
                showToast(String(res.message || localizeText("Duplicate retired", "已退役重复技能", "已退役重複技能")), "success");
                await loadData();
                await loadMaintenanceDrafts();
            } else {
                showToast(String(res?.error || res?.message || "apply failed"), "error", 6000);
            }
        } catch (err) {
            showToast(String(err), "error");
        } finally {
            setBusy(false);
        }
    }, [showConfirm, showToast, localizeText, loadData, loadMaintenanceDrafts]);

    const handleRollbackYAML = useCallback(async (skillName: string, version?: number) => {
        if (!skillName) return;
        try {
            const cached = yamlBackupInfo[skillName];
            let versions = cached?.versions || [];
            let latest = cached?.latest || 0;
            let selected = version && version > 0 ? version : (cached?.selected || latest);
            if (versions.length === 0 || latest <= 0) {
                const info = await ListSkillYAMLBackups(skillName);
                if (!info?.ok) {
                    showToast(String(info?.error || "no backups"), "error");
                    return;
                }
                versions = Array.isArray(info.versions)
                    ? [...(info.versions as number[])].sort((a, b) => b - a)
                    : [];
                latest = Number(info.latest || (versions[0] || 0));
                selected = version && version > 0 ? version : latest;
            }
            if (latest <= 0 || versions.length === 0) {
                showToast(localizeText("No YAML backups found", "没有 YAML 备份", "沒有 YAML 備份"), "error");
                return;
            }
            if (!selected || selected <= 0) selected = latest;
            const confirmed = await showConfirm(
                localizeText(
                    `Restore skill.yaml for "${skillName}" from backup v${selected}?\n\nAvailable: ${versions.join(", ")}\nCurrent file will be saved as a new backup first.`,
                    `从备份 v${selected} 恢复「${skillName}」的 skill.yaml？\n\n可用版本：${versions.join(", ")}\n当前文件会先另存为新备份。`,
                    `從備份 v${selected} 還原「${skillName}」的 skill.yaml？\n\n可用版本：${versions.join(", ")}\n目前檔案會先另存為新備份。`,
                ),
                localizeText("Restore YAML backup", "恢复 YAML 备份", "還原 YAML 備份"),
                {
                    confirmText: localizeText("Restore", "恢复", "還原"),
                    cancelText: localizeText("Cancel", "取消", "取消"),
                },
            );
            if (!confirmed) return;
            setBusy(true);
            const res = await RestoreSkillYAMLBackup(skillName, selected, true);
            if (res?.ok) {
                showToast(String(res.message || localizeText("Restored", "已恢复", "已還原")), "success", 5000);
                await loadData();
                await loadMaintenanceDrafts();
            } else {
                showToast(String(res?.error || "restore failed"), "error", 6000);
            }
        } catch (err) {
            showToast(String(err), "error");
        } finally {
            setBusy(false);
        }
    }, [showConfirm, showToast, localizeText, loadData, loadMaintenanceDrafts, yamlBackupInfo]);

    const handleUnretireSkill = useCallback(async (skill: NLSkillDefinition) => {
        if (!skill?.name) return;
        const confirmed = await showConfirm(
            localizeText(
                `Re-enable retired skill "${skill.name}"?\n\nStatus becomes active and maintenance markers on last_error are cleared (files were never deleted).`,
                `重新启用已退役技能「${skill.name}」？\n\n状态将设为 active，并清理 last_error 上的维护标记（文件从未删除）。`,
                `重新啟用已退役技能「${skill.name}」？\n\n狀態將設為 active，並清理 last_error 上的維護標記（檔案從未刪除）。`,
            ),
            localizeText("Unretire skill", "恢复退役技能", "恢復退役技能"),
            {
                confirmText: localizeText("Re-enable", "重新启用", "重新啟用"),
                cancelText: localizeText("Cancel", "取消", "取消"),
            },
        );
        if (!confirmed) return;
        setBusy(true);
        try {
            await SetNLSkillStatus(skill.name, "active");
            showToast(localizeText("Skill re-enabled", "技能已重新启用", "技能已重新啟用"), "success");
            await loadData();
            await loadMaintenanceDrafts();
        } catch (err) {
            showToast(String(err), "error");
        } finally {
            setBusy(false);
        }
    }, [showConfirm, showToast, localizeText, loadData, loadMaintenanceDrafts]);

    const openSkillFromAudit = useCallback((skillName: string) => {
        const name = String(skillName || "").trim();
        if (!name) return;
        const found = skills.find((s) => s.name === name || s.name.toLowerCase() === name.toLowerCase());
        if (found) {
            setDetailSkill(found);
            return;
        }
        showToast(
            localizeText(
                `Skill "${name}" is not in the current list.`,
                `当前列表中找不到技能「${name}」。`,
                `目前列表中找不到技能「${name}」。`,
            ),
            "info",
            4000,
        );
    }, [skills, showToast, localizeText]);

    const handleApplyContractDraft = useCallback(async (d: MaintenancePatchDraft) => {
        const name = d.skill || "";
        if (!name) return;
        const isFile = !!d.skill_dir;
        const confirmed = await showConfirm(
            isFile
                ? localizeText(
                    `Write contract into skill.yaml for "${name}"?\n\nA Versioner backup (skill.yaml.vN) is created first. Other YAML fields are preserved.`,
                    `将契约写入「${name}」的 skill.yaml？\n\n会先创建 Versioner 备份（skill.yaml.vN），并保留其它 YAML 字段。`,
                    `將契約寫入「${name}」的 skill.yaml？\n\n會先建立 Versioner 備份（skill.yaml.vN），並保留其它 YAML 欄位。`,
                )
                : localizeText(
                    `Apply contract draft for "${name}" to config-backed params?`,
                    `对「${name}」应用契约草案到配置参数？`,
                    `對「${name}」套用契約草案到設定參數？`,
                ),
            localizeText("Apply contract draft", "应用契约草案", "套用契約草案"),
            {
                confirmText: localizeText("Apply", "应用", "套用"),
                cancelText: localizeText("Cancel", "取消", "取消"),
            },
        );
        if (!confirmed) return;
        setBusy(true);
        try {
            const res = await ApplySkillMaintenanceAction("improve_contract", name, "", true, false);
            if (res?.ok) {
                const extra = res.backup_version
                    ? localizeText(
                        ` (backup v${res.backup_version})`,
                        `（备份 v${res.backup_version}）`,
                        `（備份 v${res.backup_version}）`,
                    )
                    : "";
                showToast(String(res.message || localizeText("Contract applied", "契约已应用", "契約已套用")) + extra, "success", 5000);
                await loadData();
                await loadMaintenanceDrafts();
            } else {
                const msg = String(res?.error || res?.message || "apply failed");
                showToast(msg, "error", 7000);
                const yaml = (res as any)?.patch_draft?.suggested_yaml || d.suggested_yaml;
                if (yaml) {
                    await copyText(yaml, localizeText("YAML copied for manual apply", "已复制 YAML 供手工应用", "已複製 YAML 供手動套用"));
                }
            }
        } catch (err) {
            showToast(String(err), "error");
        } finally {
            setBusy(false);
        }
    }, [showConfirm, showToast, localizeText, loadData, loadMaintenanceDrafts, copyText]);

    const pushEvolutionActivity = useCallback((kind: EvolutionActivityKind, skillName: string, explanation: string) => {
        const item: EvolutionActivityItem = {
            id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
            kind,
            skill: skillName || "—",
            explanation: String(explanation || "").trim(),
            at: Date.now(),
        };
        setEvolutionActivity((prev) => [item, ...prev].slice(0, EVOLUTION_ACTIVITY_MAX));
    }, []);

    const handleMarkNeedsReview = useCallback(async (skillName: string, reason?: string) => {
        const name = String(skillName || "").trim();
        if (!name) return;
        const confirmed = await showConfirm(
            localizeText(
                `Mark skill "${name}" as needs_review?\n\nIt will leave automatic routing until you re-activate it. Last error evidence is kept.`,
                `将技能「${name}」标记为 needs_review？\n\n在你重新启用前，自动路由会避开它；最近错误证据会保留。`,
                `將技能「${name}」標記為 needs_review？\n\n在你重新啟用前，自動路由會避開它；最近錯誤證據會保留。`,
            ),
            localizeText("Mark needs review", "标记待审", "標記待審"),
            {
                confirmText: localizeText("Mark review", "标记待审", "標記待審"),
                cancelText: localizeText("Cancel", "取消", "取消"),
            },
        );
        if (!confirmed) return;
        setBusy(true);
        try {
            await SetNLSkillStatus(name, "needs_review");
            showToast(
                localizeText(
                    `Marked "${name}" as needs_review${reason ? `: ${reason}` : ""}`,
                    `已将「${name}」标记为待审${reason ? `：${reason}` : ""}`,
                    `已將「${name}」標記為待審${reason ? `：${reason}` : ""}`,
                ),
                "success",
                5000,
            );
            pushEvolutionActivity("failed", name, reason || "marked needs_review");
            setEvolutionFocusSkill(name);
            await loadData();
            await loadMaintenanceDrafts();
            await loadEvolutionAudit();
        } catch (err) {
            showToast(String(err), "error");
        } finally {
            setBusy(false);
        }
    }, [showConfirm, showToast, localizeText, loadData, loadMaintenanceDrafts, loadEvolutionAudit, pushEvolutionActivity]);

    const focusEvolutionSkill = useCallback((skillName: string, source: "draft" | "audit" | "queue" = "draft") => {
        const name = String(skillName || "").trim();
        if (!name) return;
        setEvolutionFocusSkill(name);
        // Jumping from draft/queue into audit: narrow the list to that skill for faster triage.
        if (source === "draft" || source === "queue") {
            setEvolutionAuditFocusOnly(true);
            requestAnimationFrame(() => {
                evolutionAuditPanelRef.current?.scrollIntoView({ behavior: "smooth", block: "nearest" });
            });
        } else {
            setExpandedDraftKey(`repair-${name}`);
            requestAnimationFrame(() => {
                evolutionDraftsPanelRef.current?.scrollIntoView({ behavior: "smooth", block: "nearest" });
            });
        }
    }, []);

    const clearEvolutionFocus = useCallback(() => {
        setEvolutionFocusSkill(null);
        setEvolutionAuditFocusOnly(false);
    }, []);

    const exportEvolutionAudit = useCallback(async (format: "json" | "csv") => {
        try {
            const rowsRaw = await ListSkillEvolutionAudit(200);
            let rows = filterEvolutionAuditRows(
                Array.isArray(rowsRaw) ? rowsRaw as EvolutionAuditRow[] : [],
                evolutionAuditFilter,
                evolutionAuditSkillQuery,
                evolutionAuditTimeRange,
            );
            if (evolutionAuditFocusOnly && evolutionFocusSkill) {
                rows = rows.filter((row) => skillNamesEqual(row.skill, evolutionFocusSkill));
            }
            if (rows.length === 0) {
                showToast(localizeText("Nothing to export for this filter.", "当前筛选没有可导出的记录。", "目前篩選沒有可匯出的記錄。"), "info");
                return;
            }
            const filterSummary = [
                evolutionAuditFilter === "all" ? "kind=all" : `kind=${evolutionAuditFilter}`,
                evolutionAuditTimeRange === "all" ? "time=all" : `time=${evolutionAuditTimeRange}`,
                evolutionAuditSkillQuery.trim() ? `q=${evolutionAuditSkillQuery.trim()}` : "",
                evolutionAuditFocusOnly && evolutionFocusSkill ? `focus=${evolutionFocusSkill}` : "",
            ].filter(Boolean).join(", ");
            const confirmed = await showConfirm(
                localizeText(
                    `Export ${rows.length} audit row(s) as ${format.toUpperCase()}?\n\nFilters: ${filterSummary}\n\nA save dialog will open next.`,
                    `导出 ${rows.length} 条审计记录为 ${format.toUpperCase()}？\n\n筛选：${filterSummary}\n\n接下来会打开系统另存为对话框。`,
                    `匯出 ${rows.length} 條審計記錄為 ${format.toUpperCase()}？\n\n篩選：${filterSummary}\n\n接下來會開啟系統另存新檔對話框。`,
                ),
                localizeText("Confirm export", "确认导出", "確認匯出"),
                {
                    confirmText: localizeText("Export", "导出", "匯出"),
                    cancelText: localizeText("Cancel", "取消", "取消"),
                },
            );
            if (!confirmed) return;

            const stamp = new Date().toISOString().replace(/[:.]/g, "-").slice(0, 19);
            const filterTag = evolutionAuditFilter === "all" ? "all" : evolutionAuditFilter;
            const timeTag = evolutionAuditTimeRange === "all" ? "" : `-${evolutionAuditTimeRange}`;
            const focusTag = evolutionFocusSkill ? `-focus-${sanitizeFilenamePart(evolutionFocusSkill)}` : "";
            const queryTag = evolutionAuditSkillQuery.trim()
                ? `-q-${sanitizeFilenamePart(evolutionAuditSkillQuery)}`
                : "";
            const defaultName = `skill-evolution-audit-${filterTag}${timeTag}${focusTag}${queryTag}-${stamp}.${format}`;
            const content = format === "json"
                ? JSON.stringify(rows, null, 2) + "\n"
                : evolutionAuditToCSV(rows);
            // Prefer native save dialog; fall back to browser download if cancelled/unavailable.
            let savedPath = "";
            try {
                savedPath = await ExportTextFile(content, defaultName);
            } catch {
                savedPath = "";
            }
            if (savedPath) {
                showToast(
                    localizeText(
                        `Saved ${rows.length} audit row(s) to ${savedPath}`,
                        `已将 ${rows.length} 条审计记录保存到 ${savedPath}`,
                        `已將 ${rows.length} 條審計記錄儲存到 ${savedPath}`,
                    ),
                    "success",
                    6000,
                );
                return;
            }
            // User cancelled dialog or dialog unavailable (e.g. headless) → browser download fallback.
            downloadTextFile(
                defaultName,
                content,
                format === "json" ? "application/json;charset=utf-8" : "text/csv;charset=utf-8",
            );
            showToast(
                localizeText(
                    `Exported ${rows.length} audit row(s) as ${format.toUpperCase()}`,
                    `已导出 ${rows.length} 条审计记录（${format.toUpperCase()}）`,
                    `已匯出 ${rows.length} 條審計記錄（${format.toUpperCase()}）`,
                ),
                "success",
            );
        } catch (err) {
            showToast(String(err), "error");
        }
    }, [
        evolutionAuditFilter,
        evolutionAuditSkillQuery,
        evolutionAuditTimeRange,
        evolutionFocusSkill,
        evolutionAuditFocusOnly,
        showConfirm,
        showToast,
        localizeText,
    ]);

    const handleBatchSetStatus = useCallback(async (
        names: string[],
        status: "needs_review" | "active",
        label: string,
    ) => {
        const unique = Array.from(new Set(names.map((n) => String(n || "").trim()).filter(Boolean)));
        if (unique.length === 0) {
            showToast(localizeText("No skills selected.", "没有可选技能。", "沒有可選技能。"), "info");
            return;
        }
        const confirmed = await showConfirm(
            status === "needs_review"
                ? localizeText(
                    `Mark ${unique.length} skill(s) as needs_review?\n\n${unique.slice(0, 8).join(", ")}${unique.length > 8 ? "…" : ""}\n\nAutomatic routing will avoid them until re-enabled.`,
                    `将 ${unique.length} 个技能标记为 needs_review？\n\n${unique.slice(0, 8).join("、")}${unique.length > 8 ? "…" : ""}\n\n重新启用前自动路由会避开它们。`,
                    `將 ${unique.length} 個技能標記為 needs_review？\n\n${unique.slice(0, 8).join("、")}${unique.length > 8 ? "…" : ""}\n\n重新啟用前自動路由會避開它們。`,
                )
                : localizeText(
                    `Re-enable ${unique.length} skill(s) as active?\n\n${unique.slice(0, 8).join(", ")}${unique.length > 8 ? "…" : ""}`,
                    `将 ${unique.length} 个技能重新启用为 active？\n\n${unique.slice(0, 8).join("、")}${unique.length > 8 ? "…" : ""}`,
                    `將 ${unique.length} 個技能重新啟用為 active？\n\n${unique.slice(0, 8).join("、")}${unique.length > 8 ? "…" : ""}`,
                ),
            label,
            {
                confirmText: localizeText("Confirm", "确认", "確認"),
                cancelText: localizeText("Cancel", "取消", "取消"),
            },
        );
        if (!confirmed) return;
        setBusy(true);
        try {
            const res = await BatchSetNLSkillStatus(unique, status);
            const count = Number(res?.count || 0);
            const errors = Array.isArray(res?.errors) ? res.errors as string[] : [];
            if (count > 0) {
                showToast(
                    localizeText(
                        `Updated ${count} skill(s)${errors.length ? ` (${errors.length} failed)` : ""}`,
                        `已更新 ${count} 个技能${errors.length ? `（${errors.length} 个失败）` : ""}`,
                        `已更新 ${count} 個技能${errors.length ? `（${errors.length} 個失敗）` : ""}`,
                    ),
                    errors.length ? "info" : "success",
                    6000,
                );
                if (status === "needs_review") {
                    pushEvolutionActivity("failed", unique[0], `batch mark needs_review x${count}`);
                }
            } else {
                showToast(String(res?.error || errors[0] || "batch update failed"), "error", 7000);
            }
            await loadData();
            await loadMaintenanceDrafts();
            await loadEvolutionAudit();
        } catch (err) {
            showToast(String(err), "error");
        } finally {
            setBusy(false);
        }
    }, [showConfirm, showToast, localizeText, loadData, loadMaintenanceDrafts, loadEvolutionAudit, pushEvolutionActivity]);

    // Refresh skill list when usage/evolution/self-repair events fire from the agent.
    useEffect(() => {
        const refresh = () => {
            loadData();
        };
        const parseSkillPayload = (payload?: unknown) => {
            if (!payload || typeof payload !== "object") {
                return { skillName: "", explanation: "", via: "" };
            }
            const data = payload as Record<string, unknown>;
            return {
                skillName: typeof data.skill === "string" ? data.skill : "",
                explanation: typeof data.explanation === "string" ? data.explanation : "",
                via: typeof data.via === "string" ? data.via : "",
            };
        };
        const onRepaired = (payload?: unknown) => {
            refresh();
            void loadEvolutionAudit();
            const { skillName, explanation, via } = parseSkillPayload(payload);
            const name = skillName || localizeText("a skill", "某技能", "某技能");
            pushEvolutionActivity("repaired", skillName || name, explanation);
            if (via === "reviewed_draft") {
                showToast(
                    localizeText(
                        `Repair draft applied to “${name}”`,
                        `修复草稿已应用到「${name}」`,
                        `修復草稿已套用到「${name}」`,
                    ),
                    "success",
                    4500,
                );
                return;
            }
            if (via === "reviewed_draft_disable") {
                showToast(
                    localizeText(
                        `Disabled “${name}” as the reviewed draft suggested`,
                        `已按评审建议禁用「${name}」`,
                        `已按評審建議停用「${name}」`,
                    ),
                    "success",
                    4500,
                );
                return;
            }
            showToast(
                explanation
                    ? localizeText(
                        `Self-repaired “${name}”: ${explanation}`,
                        `已自修复「${name}」：${explanation}`,
                        `已自修復「${name}」：${explanation}`,
                    )
                    : localizeText(
                        `Self-repaired “${name}”`,
                        `已自修复「${name}」`,
                        `已自修復「${name}」`,
                    ),
                "success",
                4500,
            );
        };
        const onOptimized = (payload?: unknown) => {
            refresh();
            void loadEvolutionAudit();
            const { skillName, explanation } = parseSkillPayload(payload);
            const name = skillName || localizeText("a skill", "某技能", "某技能");
            pushEvolutionActivity("optimized", skillName || name, explanation);
            showToast(
                explanation
                    ? localizeText(
                        `Optimized “${name}”: ${explanation}`,
                        `已优化「${name}」：${explanation}`,
                        `已優化「${name}」：${explanation}`,
                    )
                    : localizeText(
                        `Optimized “${name}”`,
                        `已优化「${name}」`,
                        `已優化「${name}」`,
                    ),
                "info",
                4500,
            );
        };
        const onDiscovered = (payload?: unknown) => {
            refresh();
            void loadEvolutionAudit();
            const { skillName, explanation } = parseSkillPayload(payload);
            const name = skillName || localizeText("new skill", "新技能", "新技能");
            pushEvolutionActivity("discovered", skillName || name, explanation);
            showToast(
                explanation
                    ? localizeText(
                        `Auto-discovered “${name}”: ${explanation}`,
                        `自动发现「${name}」：${explanation}`,
                        `自動發現「${name}」：${explanation}`,
                    )
                    : localizeText(
                        `Auto-discovered “${name}”`,
                        `自动发现「${name}」`,
                        `自動發現「${name}」`,
                    ),
                "success",
                5000,
            );
        };
        const onFailed = (payload?: unknown) => {
            refresh();
            void loadEvolutionAudit();
            const { skillName, explanation } = parseSkillPayload(payload);
            pushEvolutionActivity("failed", skillName || "—", explanation);
        };
        const onCancelled = (payload?: unknown) => {
            refresh();
            void loadEvolutionAudit();
            const { skillName, explanation } = parseSkillPayload(payload);
            const reason = explanation || localizeText("operator requested cancellation", "操作员请求取消", "操作員要求取消");
            pushEvolutionActivity("failed", skillName || "—", `cancelled: ${reason}`);
            showToast(
                localizeText(`Evolution cancelled${skillName ? ` for “${skillName}”` : ""}: ${reason}`, `已取消${skillName ? `「${skillName}」` : ""}的进化任务：${reason}`, `已取消${skillName ? `「${skillName}」` : ""}的進化任務：${reason}`),
                "info",
                4500,
            );
        };
        const onTimedOut = (payload?: unknown) => {
            refresh();
            void loadEvolutionAudit();
            const { skillName, explanation } = parseSkillPayload(payload);
            const reason = explanation || localizeText("worker deadline exceeded", "超过工作任务超时限制", "超過工作任務逾時限制");
            pushEvolutionActivity("failed", skillName || "—", `timed_out: ${reason}`);
            showToast(
                localizeText(`Evolution timed out${skillName ? ` for “${skillName}”` : ""}: ${reason}`, `进化任务${skillName ? `「${skillName}」` : ""}超时：${reason}`, `進化任務${skillName ? `「${skillName}」` : ""}逾時：${reason}`),
                "warning",
                6000,
            );
        };
        const onRolledBack = (payload?: unknown) => {
            refresh();
            void loadEvolutionAudit();
            const { skillName, explanation } = parseSkillPayload(payload);
            const reason = explanation || localizeText("definition commit was rolled back", "技能变更提交已回滚", "技能變更提交已回滾");
            pushEvolutionActivity("failed", skillName || "—", `rolled_back: ${reason}`);
            showToast(
                localizeText(
                    `Evolution rolled back${skillName ? ` for “${skillName}”` : ""}: ${reason}`,
                    `进化任务${skillName ? `「${skillName}」` : ""}已回滚：${reason}`,
                    `進化任務${skillName ? `「${skillName}」` : ""}已回滾：${reason}`,
                ),
                "warning",
                6000,
            );
        };
        const unsubs = [
            EventsOn(EVENT_SKILL_USAGE_UPDATED, refresh),
            EventsOn(EVENT_SKILL_INDEX_REFRESHED, refresh),
            EventsOn(EVENT_SKILL_EXECUTION_FAILED, onFailed),
            EventsOn(EVENT_SKILL_REPAIRED, onRepaired),
            EventsOn(EVENT_SKILL_OPTIMIZED, onOptimized),
            EventsOn(EVENT_SKILL_AUTO_DISCOVERED, onDiscovered),
            EventsOn(EVENT_SKILL_EVOLUTION_CANCELLED, onCancelled),
            EventsOn(EVENT_SKILL_EVOLUTION_TIMED_OUT, onTimedOut),
            EventsOn(EVENT_SKILL_EVOLUTION_ROLLED_BACK, onRolledBack),
        ];
        return () => {
            // Prefer EventsOn unsubscribe callbacks only — EventsOff(name) would
            // drop App-level listeners for the same event names on unmount.
            for (const u of unsubs) {
                if (typeof u === "function") {
                    try {
                        u();
                    } catch {
                        /* ignore */
                    }
                }
            }
        };
    }, [loadData, localizeText, showToast, pushEvolutionActivity, loadEvolutionAudit]);

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
        setHubFilterSource("all");
        setHubFilterTrust("all");
        setHubSortBy("relevance");
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

    const loadEvolutionSettings = useCallback(async () => {
        setEvolutionStatusLoading(true);
        try {
            const cfg = await LoadConfig();
            const hours = Number((cfg as any)?.skill_evolution_repair_cooldown_hours ?? 0);
            setRepairCooldownHours(hours > 0 ? hours : 1);
            setRepairCooldownMsg("");
            // nil/undefined means default enabled
            const en = (cfg as any)?.skill_evolution_enabled;
            setEvolutionEnabled(en === undefined || en === null ? true : Boolean(en));
            const obs = (cfg as any)?.skill_maintenance_observation_enabled;
            setObservationEnabled(obs === undefined || obs === null ? true : Boolean(obs));
            const workers = Number((cfg as any)?.skill_evolution_max_concurrent_workers ?? 2);
            setMaxConcurrentWorkers(Number.isFinite(workers) && workers > 0 ? Math.min(16, Math.floor(workers)) : 2);
            const timeout = Number((cfg as any)?.skill_evolution_worker_timeout_seconds ?? 180);
            setWorkerTimeoutSeconds(Number.isFinite(timeout) && timeout >= 30 && timeout <= 1800 ? Math.floor(timeout) : 180);
            try {
                const st = await GetSkillEvolutionStatus();
                setEvolutionStatus(st && typeof st === "object" ? st : null);
            } catch {
                setEvolutionStatus(null);
            }
        } catch {
            /* ignore — keep default */
        } finally {
            setEvolutionStatusLoading(false);
        }
    }, []);

    const saveRepairCooldown = useCallback(async () => {
        let hours = Math.floor(Number(repairCooldownHours) || 0);
        if (hours < 0) hours = 0;
        if (hours > 24 * 30) hours = 24 * 30;
        setRepairCooldownSaving(true);
        setRepairCooldownMsg("");
        try {
            await PatchConfigFields({ skill_evolution_repair_cooldown_hours: hours });
            setRepairCooldownHours(hours > 0 ? hours : 1);
            setRepairCooldownMsg(
                localizeText("Saved.", "已保存。", "已儲存。"),
            );
        } catch (err) {
            setRepairCooldownMsg(localizeHubError(String(err)));
        } finally {
            setRepairCooldownSaving(false);
        }
    }, [repairCooldownHours, localizeText, localizeHubError]);

    const saveEvolutionEnabled = useCallback(async (enabled: boolean) => {
        setEvolutionEnabledSaving(true);
        setRepairCooldownMsg("");
        try {
            await PatchConfigFields({ skill_evolution_enabled: enabled });
            setEvolutionEnabled(enabled);
            setRepairCooldownMsg(
                enabled
                    ? localizeText("Automatic evolution enabled.", "已开启自动自进化。", "已開啟自動自進化。")
                    : localizeText("Automatic evolution disabled.", "已关闭自动自进化。", "已關閉自動自進化。"),
            );
            try {
                const st = await GetSkillEvolutionStatus();
                setEvolutionStatus(st && typeof st === "object" ? st : null);
            } catch {
                /* keep prior */
            }
        } catch (err) {
            setRepairCooldownMsg(localizeHubError(String(err)));
        } finally {
            setEvolutionEnabledSaving(false);
        }
    }, [localizeText, localizeHubError]);

    const saveObservationEnabled = useCallback(async (enabled: boolean) => {
        setObservationSaving(true);
        try {
            await PatchConfigFields({ skill_maintenance_observation_enabled: enabled });
            setObservationEnabled(enabled);
        } catch (err) {
            setRepairCooldownMsg(localizeHubError(String(err)));
        } finally {
            setObservationSaving(false);
        }
    }, [localizeHubError]);

    const saveMaxConcurrentWorkers = useCallback(async () => {
        const value = Math.max(1, Math.min(16, Math.floor(Number(maxConcurrentWorkers) || 2)));
        setWorkersSaving(true);
        try {
            await PatchConfigFields({ skill_evolution_max_concurrent_workers: value });
            setMaxConcurrentWorkers(value);
        } catch (err) {
            setRepairCooldownMsg(localizeHubError(String(err)));
        } finally {
            setWorkersSaving(false);
        }
    }, [maxConcurrentWorkers, localizeHubError]);

    const saveWorkerTimeout = useCallback(async () => {
        const value = Math.max(30, Math.min(1800, Math.floor(Number(workerTimeoutSeconds) || 180)));
        setWorkerTimeoutSaving(true);
        try {
            await PatchConfigFields({ skill_evolution_worker_timeout_seconds: value });
            setWorkerTimeoutSeconds(value);
            setRepairCooldownMsg(localizeText("Worker timeout saved.", "Worker 超时已保存。", "Worker 逾時已儲存。"));
            const st = await GetSkillEvolutionStatus();
            setEvolutionStatus(st && typeof st === "object" ? st : null);
        } catch (err) {
            setRepairCooldownMsg(localizeHubError(String(err)));
        } finally {
            setWorkerTimeoutSaving(false);
        }
    }, [workerTimeoutSeconds, localizeText, localizeHubError]);

    useEffect(() => {
        if (activeTab === "extdirs") {
            loadExtDirs();
        }
        if (activeTab === "evolution") {
            // Refresh skill list so Attention candidates use current last_error / rates.
            void loadData();
            loadEvolutionSettings();
            void loadEvolutionAudit();
            void loadMaintenanceDrafts();
            void loadEvolutionCompensations();
        } else if (activeTab === "extdirs") {
            loadEvolutionSettings();
        }
    }, [activeTab, loadExtDirs, loadEvolutionSettings, loadData, loadEvolutionAudit, loadMaintenanceDrafts, loadEvolutionCompensations]);

    // Auto-refresh evolution pipeline status while the Evolution settings tab is open.
    useEffect(() => {
        if (activeTab !== "evolution") {
            return;
        }
        const tick = () => {
            void GetSkillEvolutionStatus()
                .then((st) => {
                    setEvolutionStatus(st && typeof st === "object" ? st : null);
                })
                .catch(() => {
                    /* keep last snapshot */
                });
            void loadEvolutionCompensations();
        };
        tick();
        const id = window.setInterval(tick, 5000);
        return () => window.clearInterval(id);
    }, [activeTab, loadEvolutionCompensations]);

    // Keep Evolution UI in sync when General Settings / manage_skill / PatchConfigFields
    // changes skill_evolution_enabled or repair cooldown (any tab; cheap).
    useEffect(() => {
        const applyConfigSnapshot = (cfg: unknown) => {
            if (!cfg || typeof cfg !== "object") return;
            const rec = cfg as Record<string, unknown>;
            if ("skill_evolution_enabled" in rec) {
                const en = rec.skill_evolution_enabled;
                setEvolutionEnabled(en === undefined || en === null ? true : Boolean(en));
            }
            if ("skill_maintenance_observation_enabled" in rec) {
                const obs = rec.skill_maintenance_observation_enabled;
                setObservationEnabled(obs === undefined || obs === null ? true : Boolean(obs));
            }
            if ("skill_evolution_max_concurrent_workers" in rec) {
                const workers = Number(rec.skill_evolution_max_concurrent_workers ?? 2);
                if (Number.isFinite(workers) && workers > 0) setMaxConcurrentWorkers(Math.min(16, Math.floor(workers)));
            }
            if ("skill_evolution_worker_timeout_seconds" in rec) {
                const timeout = Number(rec.skill_evolution_worker_timeout_seconds ?? 180);
                if (Number.isFinite(timeout) && timeout >= 30 && timeout <= 1800) {
                    setWorkerTimeoutSeconds(Math.floor(timeout));
                }
            }
            if ("skill_evolution_repair_cooldown_hours" in rec) {
                const hours = Number(rec.skill_evolution_repair_cooldown_hours ?? 0);
                if (Number.isFinite(hours) && hours >= 0) {
                    setRepairCooldownHours(hours > 0 ? hours : 1);
                }
            }
        };
        const refreshStatus = () => {
            void GetSkillEvolutionStatus()
                .then((st) => {
                    setEvolutionStatus(st && typeof st === "object" ? st : null);
                    if (st && typeof st === "object") {
                        const s = st as SkillEvolutionStatus;
                        if (typeof s.config_enabled === "boolean") {
                            setEvolutionEnabled(s.config_enabled);
                        }
                        if (typeof s.repair_cooldown_hours === "number" && s.repair_cooldown_hours > 0) {
                            setRepairCooldownHours(s.repair_cooldown_hours);
                        }
                    }
                })
                .catch(() => { /* keep last */ });
        };
        const onBackendConfig = (cfg?: unknown) => {
            applyConfigSnapshot(cfg);
            refreshStatus();
        };
        const onLocalConfig = (e: Event) => {
            applyConfigSnapshot((e as CustomEvent).detail);
            refreshStatus();
        };
        // Prefer EventsOn unsubscribe callbacks only — EventsOff(name) would
        // drop App-level config listeners for the same event names.
        const unsubs = [
            EventsOn(EVENT_CONFIG_CHANGED, onBackendConfig),
            EventsOn(EVENT_CONFIG_UPDATED, onBackendConfig),
        ];
        window.addEventListener(EVENT_MACLAW_CONFIG_CHANGED, onLocalConfig);
        return () => {
            for (const u of unsubs) {
                if (typeof u === "function") {
                    try { u(); } catch { /* ignore */ }
                }
            }
            window.removeEventListener(EVENT_MACLAW_CONFIG_CHANGED, onLocalConfig);
        };
    }, []);

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
            showToast(
                isMaclawAppSearchResult(skill)
                    ? formatInstalledOpenPanelMessage(skill.name, localizeText)
                    : localizeText(
                        `"${skill.name}" installed! Click Run in My Skills to try it.`,
                        `「${skill.name}」安装成功！在"我的技能"中点击运行试用。`,
                        `「${skill.name}」安裝成功！在「我的技能」中點擊執行試用。`,
                    )
            );
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
            showToast(localizeHubError(String(err)), "error");
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
            showToast(localizeHubError(String(err)), "error");
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
        setIsConfiguringSkill(false);
        setFormData({ ...emptySkill });
        setTriggerInput("");
        setStepsYaml("");
        setFormError("");
        setShowForm(true);
    };

    const openEditForm = async (skill: NLSkillDefinition, configure = false) => {
        // Re-fetch skills from backend to pick up any on-disk changes
        setBusy(true);
        try {
            const list = await loadData();
            const fresh = list.find((s) => s.name === skill.name);
            const target = fresh || skill;
            const enableAfterSave = configure && target.status === "needs_setup";
            setEditingSkill(target);
            setFormData({ ...target, status: enableAfterSave ? "active" : target.status });
            setTriggerInput("");
            setStepsYaml(stepsToYaml(target.steps));
            setIsConfiguringSkill(enableAfterSave);
        } catch {
            // Fallback to stale state if refresh fails
            const enableAfterSave = configure && skill.status === "needs_setup";
            setEditingSkill(skill);
            setFormData({ ...skill, status: enableAfterSave ? "active" : skill.status });
            setTriggerInput("");
            setStepsYaml(stepsToYaml(skill.steps));
            setIsConfiguringSkill(enableAfterSave);
        } finally {
            setBusy(false);
        }
        setFormError("");
        setShowForm(true);
    };

    const closeForm = () => {
        setShowForm(false);
        setEditingSkill(null);
        setIsConfiguringSkill(false);
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
                await UpdateNLSkill(def as unknown as corelib.NLSkillEntry);
            } else {
                await CreateNLSkill(def as unknown as corelib.NLSkillEntry);
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

    const handleApproveSkillReview = async (skill: NLSkillDefinition) => {
        const reason = skillReviewReason(skill, localizeText);
        const confirmed = await showConfirm(
            [
                localizeText(
                    `Approve and enable Skill "${skill.name}"?`,
                    `\u786e\u8ba4\u5ba1\u6838\u901a\u8fc7\u5e76\u542f\u7528 Skill\u300c${skill.name}\u300d\uff1f`,
                    `\u78ba\u8a8d\u5be9\u6838\u901a\u904e\u4e26\u555f\u7528 Skill\u300c${skill.name}\u300d\uff1f`,
                ),
                reason ? localizeText(`Review reason: ${reason}`, `\u5ba1\u6838\u539f\u56e0\uff1a${reason}`, `\u5be9\u6838\u539f\u56e0\uff1a${reason}`) : "",
            ].filter(Boolean).join("\n\n"),
            localizeText("Approve Skill Review", "\u5ba1\u6838 Skill", "\u5be9\u6838 Skill"),
            {
                confirmText: localizeText("Approve and enable", "\u5ba1\u6838\u901a\u8fc7\u5e76\u542f\u7528", "\u5be9\u6838\u901a\u904e\u4e26\u555f\u7528"),
                cancelText: localizeText("Cancel", "\u53d6\u6d88", "\u53d6\u6d88"),
            },
        );
        if (!confirmed) return;
        setBusy(true);
        try {
            await SetNLSkillStatus(skill.name, "active");
            if (detailSkill?.name === skill.name) {
                setDetailSkill({ ...detailSkill, status: "active" });
            }
            await loadData();
            showToast(localizeText("Skill enabled after review.", "Skill \u5df2\u5ba1\u6838\u901a\u8fc7\u5e76\u542f\u7528\u3002", "Skill \u5df2\u5be9\u6838\u901a\u904e\u4e26\u555f\u7528\u3002"), "success");
        } catch (err) {
            setError(localizeHubError(String(err)));
        } finally {
            setBusy(false);
        }
    };

    // Staged auto-discovered skills require an explicit constrained replay.
    // Collect arguments as JSON so required_args can be satisfied without
    // ever offering the ordinary status API as an activation shortcut.
    const handleVerifyAndActivate = async (skill: NLSkillDefinition) => {
        const required = Array.from(new Set([
            ...(skill.required_args || []),
            ...((skill.params || []).filter((p) => p.required).map((p) => p.name)),
        ].map((v) => String(v || '').trim()).filter(Boolean)));
        const prompt = localizeText(
            `Enter replay arguments as a JSON object for "${skill.name}"${required.length ? ` (required: ${required.join(', ')})` : ''}. Values are used once for verification and are not saved in plaintext.`,
            `请为「${skill.name}」输入一次重放参数 JSON 对象${required.length ? `（必填：${required.join('、')}）` : ''}。参数仅用于验证，不会以明文保存。`,
            `請為「${skill.name}」輸入一次重放參數 JSON 物件${required.length ? `（必填：${required.join('、')}）` : ''}。參數僅用於驗證，不會以明文保存。`,
        );
        const raw = await showPrompt(prompt, localizeText('Verify staged skill', '验证待审核技能', '驗證待審核技能'), {
            placeholder: '{"key":"value"}',
            confirmText: localizeText('Verify & activate', '验证并激活', '驗證並啟用'),
            cancelText: localizeText('Cancel', '取消', '取消'),
        });
        if (raw === null) return;
        let args: Record<string, unknown>;
        try {
            const parsed: unknown = JSON.parse(raw.trim());
            if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('JSON must be an object');
            args = parsed as Record<string, unknown>;
        } catch (err) {
            showToast(localizeText(`Invalid JSON: ${String(err)}`, `JSON 格式无效：${String(err)}`, `JSON 格式無效：${String(err)}`), 'error');
            return;
        }
        setBusy(true);
        try {
            await VerifyAndActivateNLSkillWithArgs(skill.name, args);
            await loadData();
            if (detailSkill?.name === skill.name) setDetailSkill((prev) => prev ? { ...prev, status: 'active', verification_gate_status: 'passed' } : prev);
            showToast(localizeText('Skill verified and activated.', '技能已验证并激活。', '技能已驗證並啟用。'), 'success');
        } catch (err) {
            showToast(localizeHubError(String(err)), 'error');
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

    const handleLearnedRename = async (oldName: string) => {
        const newName = await showPrompt(
            localizeText(
                `Enter a new name for "${oldName}":`,
                `为「${oldName}」输入新名称：`,
                `為「${oldName}」輸入新名稱：`,
            ),
            undefined,
            { defaultValue: oldName },
        );
        if (!newName || newName.trim() === "" || newName.trim() === oldName) return;
        setBusy(true);
        try {
            await RenameNLSkill(oldName, newName.trim());
            setDetailSkill((prev) => (prev?.name === oldName ? { ...prev, name: newName.trim() } : prev));
            await loadData();
            showToast(localizeText("Skill renamed successfully", "技能已改名", "技能已改名"), "success");
        } catch (err) {
            showToast(`${localizeText("Rename failed", "改名失败", "改名失敗")}: ${err}`, "error");
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
                // Backend emits skill:index_refreshed; also reload here so the list
                // updates even if the event is delayed or missed.
                await loadData();
                showToast(
                    localizeText(
                        `Imported skill “${name}”`,
                        `已导入技能「${name}」`,
                        `已匯入技能「${name}」`,
                    ),
                    "success",
                );
            }
        } catch (err) {
            setError(localizeHubError(String(err)));
        } finally {
            setImporting(false);
        }
    };

    // --- Learned skills tab helpers ---

    const maclawAppSkills = useMemo(
        () => skills.filter((s) => !isLearnedSource(s.source ?? "") && !!s.is_maclaw_app),
        [skills]
    );
    const miniAppShort = localizeMiniAppPack(localizeText, miniAppLabels.short);

    const learnedSkills = useMemo(
        () => skills.filter((s) => isLearnedSource(s.source ?? "")),
        [skills]
    );

    // Filtered list for "My Skills" tab based on active sub-filter
    const filteredSkillsForMyTab = useMemo(() => {
        if (activeTab === "maclaw_app") return maclawAppSkills;
        if (activeTab === "learned") return learnedSkills;
        return skills; // "local" shows all
    }, [activeTab, skills, maclawAppSkills, learnedSkills]);

    // Evolution attention queues (shown on Evolution settings tab).
    const reviewQueue = useMemo(() => {
        return skills
            // Exclude only the obsolete runner-incompatibility demotion. A
            // genuine security, repair, or governance review for an agent
            // workflow must remain visible here and be explicitly approved.
            .filter((s) => String(s.status || "").toLowerCase() === "needs_review" && !isLegacyAgentGuidedWorkflowRunnerReview(s))
            .slice(0, 20);
    }, [skills]);

    // Skills disabled by maintenance merge retire (metadata-only).
    const retiredSkills = useMemo(() => {
        return skills
            .filter((s) => {
                if (String(s.status || "").toLowerCase() !== "disabled") return false;
                const err = String(s.last_error || "").toLowerCase();
                return err.includes("retired_by_maintenance") || err.includes("archived_by_maintenance");
            })
            .slice(0, 20);
    }, [skills]);

    const filteredEvolutionAudit = useMemo(() => {
        let rows = filterEvolutionAuditRows(
            evolutionAudit,
            evolutionAuditFilter,
            evolutionAuditSkillQuery,
            evolutionAuditTimeRange,
        );
        if (evolutionAuditFocusOnly && evolutionFocusSkill) {
            rows = rows.filter((row) => skillNamesEqual(row.skill, evolutionFocusSkill));
        }
        return rows;
    }, [
        evolutionAudit,
        evolutionAuditFilter,
        evolutionAuditSkillQuery,
        evolutionAuditTimeRange,
        evolutionAuditFocusOnly,
        evolutionFocusSkill,
    ]);

    // Reset pagination when filters change so operators always start from the top match.
    useEffect(() => {
        setAuditVisibleCount(30);
    }, [
        evolutionAuditFilter,
        evolutionAuditSkillQuery,
        evolutionAuditTimeRange,
        evolutionAuditFocusOnly,
        evolutionFocusSkill,
    ]);

    const visibleEvolutionAudit = useMemo(
        () => filteredEvolutionAudit.slice(0, Math.max(1, auditVisibleCount)),
        [filteredEvolutionAudit, auditVisibleCount],
    );

    const repairDraftSkills = useMemo(() => {
        const names = (maintenanceDrafts?.patch_drafts || [])
            .filter(isRepairPatchDraft)
            .map((d) => String(d.skill || "").trim())
            .filter(Boolean);
        return Array.from(new Set(names));
    }, [maintenanceDrafts]);

    const repairCandidates = useMemo(() => {
        return skills
            .filter((s) => {
                if (!String(s.last_error || "").trim()) return false;
                if (isAgentGuidedWorkflow(s)) return false;
                const st = String(s.status || "active").toLowerCase();
                return st === "active" || st === "needs_review" || st === "";
            })
            .slice(0, 12);
    }, [skills]);

    const handleBatchRepairCandidates = useCallback(async () => {
        const names = repairCandidates.map((s) => s.name).filter(Boolean);
        if (names.length === 0) {
            showToast(localizeText("No repair candidates.", "没有修复候选。", "沒有修復候選。"), "info");
            return;
        }
        const confirmed = await showConfirm(
            localizeText(
                `Force self-repair on ${names.length} skill(s)?\n\n${names.slice(0, 8).join(", ")}${names.length > 8 ? "…" : ""}\n\nRuns sequentially with live progress; file-backed skills still require patch review.`,
                `对 ${names.length} 个技能强制自修复？\n\n${names.slice(0, 8).join("、")}${names.length > 8 ? "…" : ""}\n\n顺序执行并显示进度；文件型技能仍需草案人审。`,
                `對 ${names.length} 個技能強制自修復？\n\n${names.slice(0, 8).join("、")}${names.length > 8 ? "…" : ""}\n\n順序執行並顯示進度；檔案型技能仍需草案人審。`,
            ),
            localizeText("Batch repair now", "批量立即修复", "批量立即修復"),
            {
                confirmText: localizeText("Repair all", "全部修复", "全部修復"),
                cancelText: localizeText("Cancel", "取消", "取消"),
            },
        );
        if (!confirmed) return;
        setBusy(true);
        setDetailActionBusy("repair");
        batchCancelRef.current = false;
        const succeeded: string[] = [];
        const failed: Array<{ name: string; error: string }> = [];
        let cancelled = false;
        setBatchProgress({
            kind: "repair",
            total: names.length,
            current: 0,
            currentName: names[0] || "",
            succeeded: [],
            failed: [],
            cancelled: false,
            done: false,
        });
        try {
            for (let i = 0; i < names.length; i++) {
                if (batchCancelRef.current) {
                    cancelled = true;
                    break;
                }
                const name = names[i];
                setBatchProgress({
                    kind: "repair",
                    total: names.length,
                    current: i + 1,
                    currentName: name,
                    succeeded: [...succeeded],
                    failed: [...failed],
                    cancelled: false,
                    done: false,
                });
                try {
                    const raw = await TriggerSkillSelfRepair(name, true);
                    let parsed: { ok?: boolean; error?: string; message?: string } = {};
                    try {
                        parsed = typeof raw === "string" ? JSON.parse(raw) : (raw as typeof parsed) || {};
                    } catch {
                        parsed = { ok: false, error: String(raw) };
                    }
                    if (parsed.ok) {
                        succeeded.push(name);
                        pushEvolutionActivity("repaired", name, parsed.message || "batch repair");
                    } else {
                        failed.push({ name, error: String(parsed.error || parsed.message || "repair failed") });
                    }
                } catch (err) {
                    failed.push({ name, error: String(err) });
                }
            }
            setBatchProgress({
                kind: "repair",
                total: names.length,
                current: cancelled ? succeeded.length + failed.length : names.length,
                currentName: "",
                succeeded: [...succeeded],
                failed: [...failed],
                cancelled,
                done: true,
            });
            showToast(
                cancelled
                    ? localizeText(
                        `Repair cancelled: ${succeeded.length} ok, ${failed.length} failed, remaining skipped`,
                        `修复已取消：成功 ${succeeded.length}，失败 ${failed.length}，其余已跳过`,
                        `修復已取消：成功 ${succeeded.length}，失敗 ${failed.length}，其餘已跳過`,
                    )
                    : localizeText(
                        `Repair finished: ${succeeded.length} ok, ${failed.length} failed`,
                        `修复完成：成功 ${succeeded.length}，失败 ${failed.length}`,
                        `修復完成：成功 ${succeeded.length}，失敗 ${failed.length}`,
                    ),
                failed.length && succeeded.length === 0 ? "error" : failed.length || cancelled ? "info" : "success",
                7000,
            );
            if (failed.length > 0) {
                showToast(`${failed[0].name}: ${failed[0].error}`, "info", 8000);
            }
            await loadData();
            await loadMaintenanceDrafts();
            await loadEvolutionAudit();
        } catch (err) {
            showToast(String(err), "error");
        } finally {
            setBusy(false);
            setDetailActionBusy(null);
            batchCancelRef.current = false;
            // Keep final progress visible briefly for operators.
            window.setTimeout(() => setBatchProgress(null), 4000);
        }
    }, [repairCandidates, showConfirm, showToast, localizeText, pushEvolutionActivity, loadData, loadMaintenanceDrafts, loadEvolutionAudit]);

    const optimizeCandidates = useMemo(() => {
        return skills
            .filter((s) => {
                const usage = s.usage_count ?? 0;
                if (usage < 3) return false;
                if (String(s.last_error || "").trim()) return false; // prefer repair first
                const st = String(s.status || "active").toLowerCase();
                if (st !== "active" && st !== "needs_review" && st !== "") return false;
                const rate = typeof s.success_rate === "number"
                    ? s.success_rate
                    : (usage > 0 ? (s.success_count ?? 0) / usage : 0);
                return rate >= 0.5 && rate <= 0.85;
            })
            .slice(0, 12);
    }, [skills]);

    const handleBatchOptimizeCandidates = useCallback(async () => {
        const names = optimizeCandidates.map((s) => s.name).filter(Boolean);
        if (names.length === 0) {
            showToast(localizeText("No optimize candidates.", "没有优化候选。", "沒有優化候選。"), "info");
            return;
        }
        const confirmed = await showConfirm(
            localizeText(
                `Force optimize on ${names.length} skill(s)?\n\n${names.slice(0, 8).join(", ")}${names.length > 8 ? "…" : ""}\n\nRuns sequentially with live progress; force mode skips auto thresholds / 24h throttle.`,
                `对 ${names.length} 个技能强制优化？\n\n${names.slice(0, 8).join("、")}${names.length > 8 ? "…" : ""}\n\n顺序执行并显示进度；强制模式跳过自动门槛与 24h 节流。`,
                `對 ${names.length} 個技能強制優化？\n\n${names.slice(0, 8).join("、")}${names.length > 8 ? "…" : ""}\n\n順序執行並顯示進度；強制模式跳過自動門檻與 24h 節流。`,
            ),
            localizeText("Batch optimize now", "批量立即优化", "批量立即優化"),
            {
                confirmText: localizeText("Optimize all", "全部优化", "全部優化"),
                cancelText: localizeText("Cancel", "取消", "取消"),
            },
        );
        if (!confirmed) return;
        setBusy(true);
        setDetailActionBusy("optimize");
        batchCancelRef.current = false;
        const succeeded: string[] = [];
        const failed: Array<{ name: string; error: string }> = [];
        let cancelled = false;
        setBatchProgress({
            kind: "optimize",
            total: names.length,
            current: 0,
            currentName: names[0] || "",
            succeeded: [],
            failed: [],
            cancelled: false,
            done: false,
        });
        try {
            for (let i = 0; i < names.length; i++) {
                if (batchCancelRef.current) {
                    cancelled = true;
                    break;
                }
                const name = names[i];
                setBatchProgress({
                    kind: "optimize",
                    total: names.length,
                    current: i + 1,
                    currentName: name,
                    succeeded: [...succeeded],
                    failed: [...failed],
                    cancelled: false,
                    done: false,
                });
                try {
                    const raw = await TriggerSkillOptimize(name, true);
                    let parsed: {
                        ok?: boolean;
                        error?: string;
                        message?: string;
                        explanation?: string;
                        skip_reason?: string;
                    } = {};
                    try {
                        parsed = typeof raw === "string" ? JSON.parse(raw) : (raw as typeof parsed) || {};
                    } catch {
                        parsed = { ok: false, error: String(raw) };
                    }
                    if (parsed.ok) {
                        succeeded.push(name);
                        pushEvolutionActivity("optimized", name, parsed.message || parsed.explanation || "batch optimize");
                    } else {
                        failed.push({
                            name,
                            error: String(parsed.error || parsed.skip_reason || parsed.message || "optimize failed"),
                        });
                    }
                } catch (err) {
                    failed.push({ name, error: String(err) });
                }
            }
            setBatchProgress({
                kind: "optimize",
                total: names.length,
                current: cancelled ? succeeded.length + failed.length : names.length,
                currentName: "",
                succeeded: [...succeeded],
                failed: [...failed],
                cancelled,
                done: true,
            });
            showToast(
                cancelled
                    ? localizeText(
                        `Optimize cancelled: ${succeeded.length} ok, ${failed.length} failed, remaining skipped`,
                        `优化已取消：成功 ${succeeded.length}，失败 ${failed.length}，其余已跳过`,
                        `優化已取消：成功 ${succeeded.length}，失敗 ${failed.length}，其餘已跳過`,
                    )
                    : localizeText(
                        `Optimize finished: ${succeeded.length} ok, ${failed.length} failed`,
                        `优化完成：成功 ${succeeded.length}，失败 ${failed.length}`,
                        `優化完成：成功 ${succeeded.length}，失敗 ${failed.length}`,
                    ),
                failed.length && succeeded.length === 0 ? "error" : failed.length || cancelled ? "info" : "success",
                7000,
            );
            if (failed.length > 0) {
                showToast(`${failed[0].name}: ${failed[0].error}`, "info", 8000);
            }
            await loadData();
            await loadMaintenanceDrafts();
            await loadEvolutionAudit();
        } catch (err) {
            showToast(String(err), "error");
        } finally {
            setBusy(false);
            setDetailActionBusy(null);
            batchCancelRef.current = false;
            window.setTimeout(() => setBatchProgress(null), 4000);
        }
    }, [optimizeCandidates, showConfirm, showToast, localizeText, pushEvolutionActivity, loadData, loadMaintenanceDrafts, loadEvolutionAudit]);

    // Run a skill by dispatching an event to the AI assistant
	const handleRunSkill = useCallback((skillName: string) => {
		window.dispatchEvent(new CustomEvent("maclaw:run-skill", { detail: { name: skillName } }));
	}, []);

	// Agent-guided Markdown workflows are intentionally not GUI-runner skills.
	// Launching a dedicated assistant task gives the agent the conversational,
	// multi-stage orchestration context required by the imported workflow.
	const handleStartAgentGuidedWorkflow = useCallback((skillName: string) => {
		window.dispatchEvent(new CustomEvent("maclaw:start-agent-guided-workflow", { detail: { name: skillName } }));
	}, []);

    // Manual self-repair from skill detail modal (force=true skips usage-rate threshold).
    const handleTriggerSelfRepair = useCallback(async (skill: NLSkillDefinition) => {
        if (!skill?.name) return;
        setBusy(true);
        setDetailActionBusy("repair");
        try {
            const raw = await TriggerSkillSelfRepair(skill.name, true);
            let parsed: { ok?: boolean; error?: string; message?: string; draft_created?: boolean; requires_review?: boolean } = {};
            try {
                parsed = typeof raw === "string" ? JSON.parse(raw) : (raw as typeof parsed) || {};
            } catch {
                parsed = { ok: false, error: String(raw) };
            }
            if (parsed.ok) {
                if (parsed.draft_created || parsed.requires_review) {
                    // Disk-backed skills are never overwritten by the repair
                    // action. Take the reviewer directly to the generated
                    // draft, where applying it remains an explicit choice.
                    setActiveTab("evolution");
                    setEvolutionFocusSkill(skill.name);
                    pushEvolutionActivity("repaired", skill.name, "repair draft pending review");
                } else {
                    pushEvolutionActivity("repaired", skill.name, parsed.message || "");
                }
                showToast(
                    parsed.message
                        || localizeText("Self-repair finished", "自修复已完成", "自修復已完成"),
                    "success",
                );
                const list = await loadData();
                const updated = list.find((s) => s.name === skill.name);
                if (updated) {
                    setDetailSkill(updated);
                }
            } else {
                showToast(
                    parsed.error
                        || localizeText("Self-repair failed", "自修复失败", "自修復失敗"),
                    "error",
                    6000,
                );
            }
        } catch (err) {
            showToast(
                `${localizeText("Self-repair failed", "自修复失败", "自修復失敗")}: ${err}`,
                "error",
            );
        } finally {
            setBusy(false);
            setDetailActionBusy(null);
        }
    }, [loadData, localizeText, showToast, pushEvolutionActivity, setActiveTab]);

    // Manual one-shot LLM optimization (force=true skips auto thresholds + 24h throttle).
    const handleTriggerOptimize = useCallback(async (skill: NLSkillDefinition) => {
        if (!skill?.name) return;
        setBusy(true);
        setDetailActionBusy("optimize");
        try {
            const raw = await TriggerSkillOptimize(skill.name, true);
            let parsed: {
                ok?: boolean;
                error?: string;
                message?: string;
                explanation?: string;
                skip_reason?: string;
                optimized?: boolean;
            } = {};
            try {
                parsed = typeof raw === "string" ? JSON.parse(raw) : (raw as typeof parsed) || {};
            } catch {
                parsed = { ok: false, error: String(raw) };
            }
            if (parsed.ok) {
                const msg = parsed.message
                    || parsed.explanation
                    || localizeText("Optimization finished", "优化已完成", "優化已完成");
                pushEvolutionActivity("optimized", skill.name, msg);
                showToast(msg, parsed.optimized === false ? "info" : "success", 5000);
                const list = await loadData();
                const updated = list.find((s) => s.name === skill.name);
                if (updated) {
                    setDetailSkill(updated);
                }
            } else {
                showToast(
                    parsed.error
                        || parsed.skip_reason
                        || localizeText("Optimization failed", "优化失败", "優化失敗"),
                    "error",
                    6000,
                );
            }
        } catch (err) {
            showToast(
                `${localizeText("Optimization failed", "优化失败", "優化失敗")}: ${err}`,
                "error",
            );
        } finally {
            setBusy(false);
            setDetailActionBusy(null);
        }
    }, [loadData, localizeText, showToast, pushEvolutionActivity]);

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
            case "agent_guided_workflow":
                return localizeText("Agent-guided workflow", "需 Agent 编排", "需 Agent 編排");
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
            case "agent_guided_workflow":
                return localizeText(
                    "This imported Markdown workflow needs interactive multi-step agent orchestration; it cannot run or self-repair as one GUI skill step.",
                    "该导入 Markdown 工作流需要 Agent 进行交互式多步骤编排，不能作为单个 GUI Skill 步骤运行或自修复。",
                    "此匯入 Markdown 工作流程需要 Agent 進行互動式多步驟編排，不能作為單一 GUI Skill 步驟執行或自我修復。",
                );
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
        <div style={skillsPanelShellStyle} ref={panelRef}>
            {/* Keep tabs outside the scroll container so the vertical scrollbar starts below them. */}
            <div style={skillsTabBarStyle}>
                <button
                    style={{
                        ...tabBtnStyle,
                        ...(isMySkillsTabActive ? tabBtnActiveStyle : {}),
                    }}
                    onClick={() => setActiveTab("local")}
                >
                    {localizeText("My Skills", "我的技能", "我的技能")}
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
                        ...(isSettingsTabActive ? tabBtnActiveStyle : {}),
                    }}
                    onClick={() => setActiveTab("evolution")}
                >
                    {localizeText("Settings", "设置", "設定")}
                </button>
            </div>

            <div style={skillsTabContentStyle}>
            {/* === My Skills Tab (merged: installed + app + learned) === */}
            {isMySkillsTabActive && (
                <>
                    {/* Sub-filter chips */}
                    <div style={{ display: "flex", gap: "6px", alignItems: "center", flexWrap: "wrap" }}>
                        <button style={{ ...chipStyle, ...(activeTab === "local" ? chipActiveStyle : {}) }} onClick={() => setActiveTab("local")}>
                            {localizeText("All", "全部", "全部")} ({skills.length})
                        </button>
                        <button style={{ ...chipStyle, ...(activeTab === "maclaw_app" ? chipActiveStyle : {}) }} onClick={() => setActiveTab("maclaw_app")}>
                            {miniAppShort} ({maclawAppSkills.length})
                        </button>
                        <button style={{ ...chipStyle, ...(activeTab === "learned" ? chipActiveStyle : {}) }} onClick={() => setActiveTab("learned")}>
                            {localizeText("Learned", "自学习", "自學習")} ({learnedSkills.length})
                        </button>
                        <div style={{ marginLeft: "auto", display: "flex", gap: "6px", flexWrap: "wrap" }}>
                            <button className="btn-secondary" style={{ fontSize: "0.74rem", padding: "3px 10px" }} onClick={() => { loadData(); setDiagEntries(null); }} disabled={loading}>
                                {loading ? "..." : localizeText("Refresh", "刷新", "重新整理")}
                            </button>
                            <button className="btn-secondary" style={{ fontSize: "0.74rem", padding: "3px 10px" }} onClick={handleImportZip} disabled={busy || importing}>
                                {localizeText("Import", "导入", "匯入")}
                            </button>
                            <button className="btn-primary" style={{ fontSize: "0.74rem", padding: "3px 10px" }} onClick={openCreateForm} disabled={busy}>
                                + {localizeText("New", "新建", "新建")}
                            </button>
                        </div>
                    </div>

                    <SkillInstallProgressPanel active={importing || installingSkills.size > 0 || (busy && showForm)} localizeText={localizeText} />
                    {/* Diagnose results */}
                    {diagEntries && diagEntries.length > 0 && (
                        <div style={{ ...remoteInfoPanelStyle, fontSize: "0.76rem" }}>
                            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: "6px" }}>
                                <span style={{ fontWeight: 500, color: colors.text }}>{localizeText("Skill Directory Diagnosis", "Skill 目录诊断结果", "Skill 目錄診斷結果")}</span>
                                <button className="btn-secondary" style={{ fontSize: "0.7rem", padding: "2px 8px" }} onClick={() => setDiagEntries(null)}>{localizeText("Close", "关闭", "關閉")}</button>
                            </div>
                            {diagEntries.map((d, i) => (
                                <div key={i} style={{ display: "flex", gap: "6px", alignItems: "baseline", padding: "3px 0", borderTop: i > 0 ? `1px solid ${colors.borderLight}` : undefined }}>
                                    <span>{d.ok ? "OK" : "ERR"}</span>
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

                    {/* Skills list — responsive: cards when narrow, table when wide */}
                    {!loading && filteredSkillsForMyTab.length > 0 && (
                        panelWidth < LOCAL_SKILLS_TABLE_MIN_WIDTH_PX ? (
                            /* Card layout for narrow panels */
                            <div style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
                                {filteredSkillsForMyTab.map((s) => (
                                    <div key={s.name} style={skillCardStyle}>
                                        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: "8px" }}>
                                            <div style={{ flex: 1, minWidth: 0 }}>
                                                <div style={{ display: "flex", alignItems: "center", gap: "6px", flexWrap: "wrap" }}>
                                                    <span style={skillNameLinkStyle} onClick={() => setDetailSkill(s)}>{s.name}</span>
                                                    {s.is_maclaw_app && <span style={appBadgeStyle}>{miniAppShort}</span>}
                                                    {isLearnedSource(s.source ?? "") && <span style={learnedBadgeStyle}>{localizeText("Learned", "自学习", "自學習")}</span>}
                                                    <span style={{ ...statusBadgeStyle, ...getStatusBadgeVariant(s.status) }} title={s.status === "needs_review" ? skillReviewReason(s, localizeText) : undefined}>{localizeSkillStatus(s.status)}</span>
                                                </div>
                                                {renderSkillDescriptionPreview(s.description || "", { marginTop: "4px" })}
                                                {(s.status === "needs_setup" || s.status === "needs_review") && (
                                                    <div style={{ fontSize: "0.7rem", color: colors.warning, marginTop: "3px" }}>
                                                        {s.status === "needs_setup"
                                                            ? localizeText("Needs configuration before use", "需要配置后才能使用", "需要配置後才能使用")
                                                            : localizeText("Needs review before use", "需要审核后才能使用", "需要審核後才能使用")}
                                                    </div>
                                                )}
                                                {s.status === "needs_review" && skillReviewReason(s, localizeText) && (
                                                    <div style={{ fontSize: "0.7rem", color: colors.textSecondary, marginTop: "3px" }} title={skillReviewReason(s, localizeText)}>
                                                        {localizeText("Review reason", "\u5ba1\u6838\u539f\u56e0", "\u5be9\u6838\u539f\u56e0")}: {skillReviewReasonPreview(s, localizeText)}
                                                    </div>
                                                )}
                                                <div style={{ display: "flex", gap: "8px", marginTop: "6px", fontSize: "0.7rem", color: colors.textMuted }}>
                                                    {s.execution_class && <span>{getExecutionClassLabel(s.execution_class)}</span>}
                                                    {displayHubVersion(s.hub_version) && <span>v{displayHubVersion(s.hub_version)}</span>}
                                                    {(s.usage_count ?? 0) > 0 && <span>{s.usage_count}{localizeText("x", "次", "次")} / {Math.round((s.success_rate ?? 0) * 100)}%</span>}
                                                </div>
                                            </div>
                                            <div style={{ display: "flex", gap: "4px", flexShrink: 0, alignItems: "center" }}>
                                                {s.status === "staged" && (
                                                    <button className="btn-primary" style={{ ...iconBtnStyle, width: "auto", padding: "0 8px" }} onClick={() => { void handleVerifyAndActivate(s); }} disabled={busy} title={localizeText("Replay with arguments, verify, and activate", "使用参数重放、验证并激活", "使用參數重放、驗證並啟用")} aria-label={localizeText("Verify and activate", "验证并激活", "驗證並啟用")}>
                                                        {localizeText("Verify", "验证", "驗證")}
                                                    </button>
                                                )}
                                                {s.status === "needs_setup" && (
                                                    <button className="btn-primary" style={{ ...iconBtnStyle, width: "auto", padding: "0 8px" }} onClick={() => openEditForm(s, true)} disabled={busy} title={localizeText("Configure and enable", "配置并启用", "設定並啟用")} aria-label={localizeText("Configure and enable", "配置并启用", "設定並啟用")}>
                                                        {localizeText("Configure", "配置", "設定")}
                                                    </button>
                                                )}
                                                {s.status === "needs_review" && (
                                                    <button className="btn-secondary" style={iconBtnStyle} onClick={() => handleApproveSkillReview(s)} disabled={busy} title={localizeText("Review and enable", "\u5ba1\u6838\u5e76\u542f\u7528", "\u5be9\u6838\u4e26\u555f\u7528")} aria-label={localizeText("Review and enable", "\u5ba1\u6838\u5e76\u542f\u7528", "\u5be9\u6838\u4e26\u555f\u7528")}>{localizeText("OK", "通过", "通過")}</button>
                                                )}
                                                {!!s.last_error && !isAgentGuidedWorkflow(s) && (
                                                    <button
                                                        className="btn-secondary"
                                                        style={{ ...iconBtnStyle, color: "var(--theme-warning, #b45309)" }}
                                                        onClick={() => { void handleTriggerSelfRepair(s); }}
                                                        disabled={busy || detailActionBusy !== null}
                                                        title={localizeText("Repair now", "立即修复", "立即修復")}
                                                        aria-label={localizeText("Repair now", "立即修复", "立即修復")}
                                                    >
                                                        {localizeText("Fix", "修复", "修復")}
                                                    </button>
                                                )}
                                                {!isAgentGuidedWorkflow(s) && !s.last_error && (s.usage_count ?? 0) >= 3 && (() => {
                                                    const rate = typeof s.success_rate === "number" ? s.success_rate : 0;
                                                    return rate >= 0.5 && rate <= 0.85;
                                                })() && (
                                                    <button
                                                        className="btn-secondary"
                                                        style={iconBtnStyle}
                                                        onClick={() => { void handleTriggerOptimize(s); }}
                                                        disabled={busy || detailActionBusy !== null}
                                                        title={localizeText("Optimize now", "立即优化", "立即優化")}
                                                        aria-label={localizeText("Optimize now", "立即优化", "立即優化")}
                                                    >
                                                        {localizeText("Opt", "优化", "優化")}
                                                    </button>
                                                )}
                                                {isAgentGuidedWorkflow(s) ? (
                                                    <button className="btn-primary" style={{ ...runBtnStyle, width: "auto", padding: "0 8px" }} onClick={() => handleStartAgentGuidedWorkflow(s.name)} disabled={busy || s.status !== "active"} title={localizeText("Open an AI-agent project task for this workflow", "在 AI 助手中启动此工作流任务", "在 AI 助手中啟動此工作流程任務")} aria-label={localizeText("Start with AI Agent", "用 AI 助手启动", "用 AI 助手啟動")}>{localizeText("Start", "启动", "啟動")}</button>
                                                ) : (
                                                    <button className="btn-primary" style={runBtnStyle} onClick={() => handleRunSkill(s.name)} disabled={busy || s.status !== "active"} title={localizeText("Run", "运行", "執行")} aria-label={localizeText("Run", "运行", "執行")}>{localizeText("Run", "运行", "執行")}</button>
                                                )}
                                                <button className="btn-secondary" style={iconBtnStyle} onClick={() => openEditForm(s)} disabled={busy} title={localizeText("Edit", "编辑", "編輯")} aria-label={localizeText("Edit", "编辑", "編輯")}>{localizeText("Edit", "编辑", "編輯")}</button>
                                                <button className="btn-secondary" style={deleteIconBtnStyle} onClick={() => handleDelete(s.name)} disabled={busy} title={localizeText("Delete", "删除", "刪除")} aria-label={localizeText("Delete", "删除", "刪除")}>
                                                    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polyline points="3 6 5 6 21 6" /><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" /></svg>
                                                </button>
                                            </div>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        ) : (
                            /* Table layout for wide panels */
                            <div style={localSkillsTableContainerStyle}>
                                <table style={localSkillsTableStyle}>
                                    <colgroup>
                                        <col style={{ width: LOCAL_SKILLS_COL_PX.name }} /><col style={{ width: LOCAL_SKILLS_COL_PX.description }} /><col style={{ width: LOCAL_SKILLS_COL_PX.type }} /><col style={{ width: LOCAL_SKILLS_COL_PX.usage }} /><col style={{ width: LOCAL_SKILLS_COL_PX.status }} /><col style={{ width: "auto" }} />
                                    </colgroup>
                                    <thead>
                                        <tr style={{ background: colors.surfaceMuted }}>
                                            <th style={{ ...thStyle, textAlign: "left" }}>{localizeText("Name", "名称", "名稱")}</th>
                                            <th style={{ ...thStyle, ...localSkillsDescriptionColStyle, textAlign: "left" }}>{localizeText("Description", "描述", "描述")}</th>
                                            <th style={{ ...thStyle, textAlign: "left", paddingRight: 4 }}>{localizeText("Type", "类型", "類型")}</th>
                                            <th style={{ ...thStyle, textAlign: "left", paddingRight: 4 }}>{localizeText("Usage", "使用统计", "使用統計")}</th>
                                            <th style={{ ...thStyle, whiteSpace: "nowrap", textAlign: "center", paddingRight: 4 }}>{localizeText("Status", "状态", "狀態")}</th>
                                            <th style={{ ...thStyle, textAlign: "center", paddingLeft: 4 }}>{localizeText("Actions", "操作", "操作")}</th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        {filteredSkillsForMyTab.map((s) => (
                                            <tr key={s.name} style={{ borderTop: `1px solid ${colors.border}` }}>
                                                <td style={{ ...tdStyle, ...localSkillsClipCellStyle, textAlign: "left" }}>
                                                    <div style={localSkillsNameCellStyle}>
                                                        {s.is_maclaw_app && <span title={miniAppShort} style={appBadgeStyle}>{miniAppShort}</span>}
                                                        {isLearnedSource(s.source ?? "") && <span title={localizeText("Learned", "自学习", "自學習")} style={learnedBadgeStyle}>{localizeText("Learned", "自学习", "自學習")}</span>}
                                                        <span style={localSkillsNameLinkStyle} onClick={() => setDetailSkill(s)} title={s.name}>{s.name}</span>
                                                    </div>
                                                </td>
                                                <td style={{ ...tdStyle, ...localSkillsDescriptionColStyle }}>
                                                    {renderSkillDescriptionPreview(s.description || "")}
                                                    {s.status === "needs_review" && skillReviewReason(s, localizeText) && (
                                                        <div style={{ fontSize: "0.7rem", color: colors.textSecondary, marginTop: "3px", lineHeight: 1.35, overflowWrap: "anywhere" }} title={skillReviewReason(s, localizeText)}>
                                                            {localizeText("Review reason", "\u5ba1\u6838\u539f\u56e0", "\u5be9\u6838\u539f\u56e0")}: {skillReviewReasonPreview(s, localizeText)}
                                                        </div>
                                                    )}
                                                </td>
                                                <td style={{ ...tdStyle, ...localSkillsClipCellStyle, textAlign: "left", paddingRight: 4 }}>
                                                    {s.execution_class ? (<span style={localSkillsTypeBadgeStyle} title={getExecutionClassTitle(s)}>{getExecutionClassLabel(s.execution_class)}</span>) : (<span style={{ fontSize: "0.72rem", color: colors.textMuted }}>—</span>)}
                                                </td>
                                                <td style={{ ...tdStyle, ...localSkillsClipCellStyle, textAlign: "left", paddingRight: 4 }}>
                                                    {(s.usage_count ?? 0) > 0 ? (<span style={localSkillsMetaTextStyle}>{s.usage_count}{localizeText("x", "次", "次")} / {Math.round((s.success_rate ?? 0) * 100)}%</span>) : (<span style={{ ...localSkillsMetaTextStyle, color: colors.textMuted }}>{localizeText("Unused", "未使用", "未使用")}</span>)}
                                                </td>
                                                <td style={{ ...tdStyle, ...localSkillsClipCellStyle, textAlign: "center", whiteSpace: "nowrap", paddingRight: 4 }}>
                                                    <span style={{ ...statusBadgeStyle, ...getStatusBadgeVariant(s.status) }} title={s.status === "needs_review" && skillReviewReason(s, localizeText) ? skillReviewReason(s, localizeText) : localizeSkillStatus(s.status)}>{localizeSkillStatus(s.status)}</span>
                                                </td>
                                                <td style={{ ...tdStyle, textAlign: "center", paddingLeft: 4, minWidth: 0 }}>
                                                    <div style={localSkillsRowActionsStyle}>
                                                        {s.status === "staged" && (
                                                            <button className="btn-primary" style={{ ...iconBtnStyle, width: "auto", padding: "0 8px" }} onClick={() => { void handleVerifyAndActivate(s); }} disabled={busy} title={localizeText("Replay with arguments, verify, and activate", "使用参数重放、验证并激活", "使用參數重放、驗證並啟用")} aria-label={localizeText("Verify and activate", "验证并激活", "驗證並啟用")}>
                                                                {localizeText("Verify", "验证", "驗證")}
                                                            </button>
                                                        )}
                                                        {s.status === "needs_setup" && (
                                                            <button className="btn-primary" style={{ ...iconBtnStyle, width: "auto", padding: "0 8px" }} onClick={() => openEditForm(s, true)} disabled={busy} title={localizeText("Configure and enable", "配置并启用", "設定並啟用")} aria-label={localizeText("Configure and enable", "配置并启用", "設定並啟用")}>
                                                                {localizeText("Configure", "配置", "設定")}
                                                            </button>
                                                        )}
                                                        {s.status === "needs_review" && (
                                                            <button className="btn-secondary" style={iconBtnStyle} onClick={() => handleApproveSkillReview(s)} disabled={busy} title={localizeText("Review and enable", "\u5ba1\u6838\u5e76\u542f\u7528", "\u5be9\u6838\u4e26\u555f\u7528")} aria-label={localizeText("Review and enable", "\u5ba1\u6838\u5e76\u542f\u7528", "\u5be9\u6838\u4e26\u555f\u7528")}>{localizeText("OK", "通过", "通過")}</button>
                                                        )}
                                                        {!!s.last_error && !isAgentGuidedWorkflow(s) && (
                                                            <button
                                                                className="btn-secondary"
                                                                style={{ ...iconBtnStyle, color: "var(--theme-warning, #b45309)" }}
                                                                onClick={() => { void handleTriggerSelfRepair(s); }}
                                                                disabled={busy || detailActionBusy !== null}
                                                                title={localizeText("Repair now", "立即修复", "立即修復")}
                                                                aria-label={localizeText("Repair now", "立即修复", "立即修復")}
                                                            >
                                                                {localizeText("Fix", "修复", "修復")}
                                                            </button>
                                                        )}
                                                        {!isAgentGuidedWorkflow(s) && !s.last_error && (s.usage_count ?? 0) >= 3 && (() => {
                                                            const rate = typeof s.success_rate === "number" ? s.success_rate : 0;
                                                            return rate >= 0.5 && rate <= 0.85;
                                                        })() && (
                                                            <button
                                                                className="btn-secondary"
                                                                style={iconBtnStyle}
                                                                onClick={() => { void handleTriggerOptimize(s); }}
                                                                disabled={busy || detailActionBusy !== null}
                                                                title={localizeText("Optimize now", "立即优化", "立即優化")}
                                                                aria-label={localizeText("Optimize now", "立即优化", "立即優化")}
                                                            >
                                                                {localizeText("Opt", "优化", "優化")}
                                                            </button>
                                                        )}
                                                        {isAgentGuidedWorkflow(s) ? (
                                                            <button className="btn-primary" style={{ ...runBtnStyle, width: "auto", padding: "0 8px" }} onClick={() => handleStartAgentGuidedWorkflow(s.name)} disabled={busy || s.status !== "active"} title={localizeText("Open an AI-agent project task for this workflow", "在 AI 助手中启动此工作流任务", "在 AI 助手中啟動此工作流程任務")} aria-label={localizeText("Start with AI Agent", "用 AI 助手启动", "用 AI 助手啟動")}>{localizeText("Start", "启动", "啟動")}</button>
                                                        ) : (
                                                            <button className="btn-primary" style={runBtnStyle} onClick={() => handleRunSkill(s.name)} disabled={busy || s.status !== "active"} title={localizeText("Run", "运行", "執行")} aria-label={localizeText("Run", "运行", "執行")}>{localizeText("Run", "运行", "執行")}</button>
                                                        )}
                                                        <button className="btn-secondary" style={iconBtnStyle} onClick={() => openEditForm(s)} disabled={busy} title={localizeText("Edit", "编辑", "編輯")} aria-label={localizeText("Edit", "编辑", "編輯")}>{localizeText("Edit", "编辑", "編輯")}</button>
                                                        <button className="btn-secondary" style={deleteIconBtnStyle} onClick={() => handleDelete(s.name)} disabled={busy} title={localizeText("Delete", "删除", "刪除")} aria-label={localizeText("Delete", "删除", "刪除")}>
                                                            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polyline points="3 6 5 6 21 6" /><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" /><line x1="10" y1="11" x2="10" y2="17" /><line x1="14" y1="11" x2="14" y2="17" /></svg>
                                                        </button>
                                                    </div>
                                                </td>
                                            </tr>
                                        ))}
                                    </tbody>
                                </table>
                            </div>
                        )
                    )}

                    {!loading && filteredSkillsForMyTab.length === 0 && !error && (
                        <div style={skillsEmptyStateStyle}>
                            {activeTab === "learned"
                                ? localizeText("No learned skills yet. MaClaw automatically learns and generates skills during use.", "暂无自学习技能。MaClaw 在使用过程中会自动学习并生成技能。", "暫無自學習技能。MaClaw 在使用過程中會自動學習並生成技能。")
                                : activeTab === "maclaw_app"
                                    ? localizeMiniAppPack(localizeText, miniAppLabels.emptyMarketBrowse)
                                    : localizeText("No registered Skills yet", "暂无已注册的 Skill", "暫無已註冊的 Skill")}
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

                    {/* Filter & Sort (shown when results exist) */}
                    {hubSearched && hubResults.length > 0 && (
                        <div style={{ display: "flex", gap: "6px", alignItems: "center", flexWrap: "wrap", fontSize: "0.72rem" }}>
                            <select aria-label="Market source" className="form-input" style={{ fontSize: "0.72rem", padding: "2px 6px", width: "auto", minWidth: "112px" }} value={hubFilterSource} onChange={(e) => setHubFilterSource(e.target.value as HubSourceFilter)}>
                                <option value="all">{localizeText("All Sources", "全部来源", "全部來源")}</option>
                                <option value="hubcenter">Hub / HubCenter</option>
                                <option value="clawhub">ClawHub</option>
                                <option value="github">GitHub</option>
                            </select>
                            <select className="form-input" style={{ fontSize: "0.72rem", padding: "2px 6px", width: "auto", minWidth: "80px" }} value={hubFilterTrust} onChange={(e) => setHubFilterTrust(e.target.value)}>
                                <option value="all">{localizeText("All Trust", "全部信任", "全部信任")}</option>
                                <option value="official">{localizeText("Official", "官方", "官方")}</option>
                                <option value="trusted">{localizeText("Trusted", "可信", "可信")}</option>
                                <option value="community">{localizeText("Community", "社区", "社區")}</option>
                            </select>
                            <select className="form-input" style={{ fontSize: "0.72rem", padding: "2px 6px", width: "auto", minWidth: "80px" }} value={hubSortBy} onChange={(e) => setHubSortBy(e.target.value)}>
                                <option value="relevance">{localizeText("Relevance", "相关度", "相關度")}</option>
                                <option value="downloads">{localizeText("Downloads", "下载量", "下載量")}</option>
                                <option value="rating">{localizeText("Rating", "评分", "評分")}</option>
                                <option value="newest">{localizeText("Newest", "最新", "最新")}</option>
                            </select>
                            <span style={{ color: colors.textMuted, marginLeft: "auto" }}>
                                {filteredHubResults.length}/{hubResults.length} {localizeText("results", "个结果", "個結果")}
                            </span>
                        </div>
                    )}

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
                    {!hubSearching && hubSearched && filteredHubResults.length === 0 && !hubError && (
                        <div style={skillsEmptyStateStyle}>
                            {hubResults.length === 0
                                ? localizeText("No results found", "无搜索结果", "無搜尋結果")
                                : localizeText("No results match current filters", "当前筛选条件下无结果", "目前篩選條件下無結果")}
                        </div>
                    )}

                    {!hubSearching && filteredHubResults.length > 0 && (
                        <div style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
                            {filteredHubResults.map((skill) => (
                                <div key={skill.id} style={hubCardStyle}>
                                    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: "8px" }}>
                                        <div style={{ flex: 1, minWidth: 0 }}>
                                            <div style={{ display: "flex", alignItems: "center", gap: "6px", flexWrap: "wrap" }}>
                                                <span style={{ fontWeight: 600, fontSize: "0.82rem", color: colors.text }}>{skill.name}</span>
                                                <SkillSourceBadge skill={skill} localizeText={localizeText} />
                                                <SkillProductBadge skill={skill} localizeText={localizeText} />
                                                {shouldShowTrustBadge(skill.trust_level) && (
                                                    <span style={trustBadgeStyle(skill.trust_level!)}>
                                                        {trustLevelLabel(skill.trust_level!, localizeText)}
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
                                            <MaclawAppMarketPreview skill={skill} localizeText={localizeText} />
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
                                                            <span style={{ color: colors.primary }}>{renderStars(skill.avg_rating)}</span>
                                                            <span>({skill.rating_count})</span>
                                                        </span>
                                                    )}
                                                    {skill.downloads > 0 && (
                                                        <span>{formatDownloads(skill.downloads)}</span>
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
                                        {localizeText("Popular Skills", "热门 Skill", "熱門 Skill")}
                                    </div>
                                    {hubRecommendations.map((skill) => (
                                        <div key={skill.id} style={hubCardStyle}>
                                            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: "8px" }}>
                                                <div style={{ flex: 1, minWidth: 0 }}>
                                                    <div style={{ display: "flex", alignItems: "center", gap: "6px", flexWrap: "wrap" }}>
                                                        <span style={{ fontWeight: 600, fontSize: "0.82rem", color: colors.text }}>{skill.name}</span>
                                                        <SkillSourceBadge skill={skill} localizeText={localizeText} />
                                                        <SkillProductBadge skill={skill} localizeText={localizeText} />
                                                        {shouldShowTrustBadge(skill.trust_level) && (
                                                            <span style={trustBadgeStyle(skill.trust_level!)}>
                                                                {trustLevelLabel(skill.trust_level!, localizeText)}
                                                            </span>
                                                        )}
                                                        {skill.version && (
                                                            <span style={{ fontSize: "0.68rem", color: colors.textMuted }}>v{skill.version}</span>
                                                        )}
                                                    </div>
                                                    <div style={hubSkillDescriptionStyle} title={skill.description || undefined}>
                                                        {skill.description || localizeText("No description", "暂无描述", "暫無描述")}
                                                    </div>
                                                    <MaclawAppMarketPreview skill={skill} localizeText={localizeText} />
                                                    <div style={{ display: "flex", alignItems: "center", gap: "8px", marginTop: "6px", fontSize: "0.68rem", color: colors.textMuted }}>
                                                        {skill.author && <span>{skill.author}</span>}
                                                        {skill.downloads > 0 && <span>{formatDownloads(skill.downloads)}</span>}
                                                        {skill.rating_count > 0 && (
                                                            <span style={{ display: "inline-flex", alignItems: "center", gap: "2px" }}>
                                                                <span style={{ color: colors.primary }}>{renderStars(skill.avg_rating)}</span>
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

            {/* === Learned Skills detail section (shows within My Skills when learned sub-filter active) === */}
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
                                {learnedImporting ? localizeText("Importing...", "导入中...", "匯入中...") : localizeText("PACK Import", "PACK 导入", "PACK 匯入")}
                            </button>
                            <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 12px" }} onClick={handleLearnedExport} disabled={learnedExporting || learnedSelected.size === 0}>
                                {learnedExporting ? localizeText("Exporting...", "导出中...", "匯出中...") : `PACK ${localizeText("Export", "导出", "匯出")}${learnedSelected.size > 0 ? ` (${learnedSelected.size})` : ""}`}
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
                                <div style={{ fontSize: "0.72rem", color: colors.primaryDark, marginBottom: "6px" }}>
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
                                            <div style={{ fontSize: "0.72rem", color: colors.primaryDark, marginTop: "3px" }}>
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
                </>
            )}

            {/* === Settings Tab === */}
            {isSettingsTabActive && (
                <>
                    <div style={{ display: "flex", gap: "6px", alignItems: "center", flexWrap: "wrap", marginBottom: "10px" }}>
                        <button
                            style={{ ...chipStyle, ...(activeTab === "evolution" ? chipActiveStyle : {}) }}
                            onClick={() => setActiveTab("evolution")}
                        >
                            {localizeText("Evolution", "自进化", "自進化")}
                        </button>
                        <button
                            style={{ ...chipStyle, ...(activeTab === "extdirs" ? chipActiveStyle : {}) }}
                            onClick={() => setActiveTab("extdirs")}
                        >
                            {localizeText("External dirs", "外部目录", "外部目錄")}
                        </button>
                    </div>
                </>
            )}

            {activeTab === "evolution" && (
                <>
                    <div style={{ ...remoteInfoPanelStyle, marginBottom: "12px", padding: "10px 12px" }}>
                        <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: evolutionHelpOpen ? 8 : 0 }}>
                            <div style={{ fontSize: "0.8rem", fontWeight: 600 }}>
                                {localizeText("Operator quick guide", "运维速查", "運維速查")}
                            </div>
                            <button
                                type="button"
                                className="btn-secondary"
                                style={{ fontSize: "0.7rem", padding: "2px 8px", marginLeft: "auto" }}
                                onClick={() => setEvolutionHelpOpen((v) => !v)}
                            >
                                {evolutionHelpOpen
                                    ? localizeText("Hide", "收起", "收起")
                                    : localizeText("Show tips", "显示说明", "顯示說明")}
                            </button>
                        </div>
                        {evolutionHelpOpen && (
                            <div style={{ fontSize: "0.72rem", color: colors.textSecondary, lineHeight: 1.55 }}>
                                <div style={{ marginBottom: 6 }}>
                                    {localizeText(
                                        "Read-only can run automatically; disk writes always need confirmation; bad YAML can be rolled back.",
                                        "只读可自动；写盘必须确认；错误 YAML 可回滚。",
                                        "唯讀可自動；寫盤必須確認；錯誤 YAML 可回滾。",
                                    )}
                                </div>
                                <ul style={{ margin: "0 0 6px 1.1rem", padding: 0 }}>
                                    <li>
                                        {localizeText(
                                            "Draft review: repair packets (open folder / mark needs_review) and contract patches (apply with backup).",
                                            "草案人审：修复审阅包（打开目录/标记待审）；契约补丁（应用含备份）。",
                                            "草案人審：修復審閱包（開啟目錄/標記待審）；契約修補（套用含備份）。",
                                        )}
                                    </li>
                                    <li>
                                        {localizeText(
                                            "Audit: filter by kind/time/search; Focus only; export JSON/CSV via save dialog; load more for long lists.",
                                            "审计：按类型/时间/搜索筛选；仅聚焦技能；导出 JSON/CSV 走另存为；长列表可加载更多。",
                                            "審計：依類型/時間/搜尋篩選；僅聚焦技能；匯出 JSON/CSV 走另存新檔；長列表可載入更多。",
                                        )}
                                    </li>
                                    <li>
                                        {localizeText(
                                            "Attention: batch repair/optimize with live progress and Cancel batch (skips remaining).",
                                            "待处理：批量修复/优化带进度，可「取消批量」（跳过剩余）。",
                                            "待處理：批量修復/優化帶進度，可「取消批量」（跳過剩餘）。",
                                        )}
                                    </li>
                                    <li>
                                        {localizeText(
                                            "Queues: batch re-enable retired skills; batch approve needs_review.",
                                            "队列：退役技能可批量恢复；待审核可批量通过。",
                                            "佇列：退役技能可批量恢復；待審核可批量通過。",
                                        )}
                                    </li>
                                    <li>
                                        {localizeText(
                                            "Data: ~/.maclaw/skill_evolution/audit.jsonl · skill.yaml.vN backups under skill_dir.",
                                            "数据：~/.maclaw/skill_evolution/audit.jsonl · skill_dir 下 skill.yaml.vN 备份。",
                                            "資料：~/.maclaw/skill_evolution/audit.jsonl · skill_dir 下 skill.yaml.vN 備份。",
                                        )}
                                    </li>
                                </ul>
                                <div style={{ color: colors.textMuted }}>
                                    {localizeText(
                                        "Full ops handbook: docs/skill-self-evolution-technical-design-zh.md §操作手册.",
                                        "完整操作手册见 docs/skill-self-evolution-technical-design-zh.md「操作手册」节。",
                                        "完整操作手冊見 docs/skill-self-evolution-technical-design-zh.md「操作手冊」節。",
                                    )}
                                </div>
                            </div>
                        )}
                    </div>

                    {(evolutionStatus?.env_disabled) ? (
                        <div style={{
                            ...remoteInfoPanelStyle,
                            marginBottom: "12px",
                            padding: "10px 12px",
                            borderLeft: "3px solid var(--theme-warning, #b45309)",
                        }}>
                            <div style={{ fontSize: "0.8rem", fontWeight: 600, marginBottom: "4px" }}>
                                {localizeText("Evolution disabled by environment", "自进化已被环境变量关闭", "自進化已被環境變數關閉")}
                            </div>
                            <div style={{ fontSize: "0.76rem", color: colors.textSecondary }}>
                                {localizeText(
                                    "MACLAW_DISABLE_SKILL_EVOLUTION is set. Automatic repair/optimize/promote after skill runs is suppressed. Manual Repair now / Optimize now still work.",
                                    "已设置环境变量 MACLAW_DISABLE_SKILL_EVOLUTION。技能执行后的自动修复/优化/发现会被跳过；详情页「立即修复/立即优化」仍可手动触发。",
                                    "已設定環境變數 MACLAW_DISABLE_SKILL_EVOLUTION。技能執行後的自動修復/優化/發現會被跳過；詳情頁「立即修復/立即優化」仍可手動觸發。",
                                )}
                            </div>
                        </div>
                    ) : null}

                    <div style={{ ...remoteInfoPanelStyle, marginBottom: "12px", padding: "10px 12px" }}>
                        <div style={{ fontSize: "0.8rem", fontWeight: 600, marginBottom: "6px" }}>
                            {localizeText("Skill self-repair & evolution", "技能自修复与进化", "技能自修復與進化")}
                        </div>
                        <div style={{ fontSize: "0.76rem", color: colors.textSecondary, marginBottom: "8px" }}>
                            {localizeText(
                                "After runs, Maclaw may auto-repair, optimize, or discover skills in the background. Configure the minimum hours between self-repair attempts for the same skill (0 = default 1 hour).",
                                "技能执行后，系统可能在后台自动修复、优化或发现技能。可配置同一技能两次自动自修复的最短间隔（小时）。0 表示默认 1 小时。",
                                "技能執行後，系統可能在背景自動修復、優化或發現技能。可設定同一技能兩次自動自修復的最短間隔（小時）。0 表示預設 1 小時。",
                            )}
                        </div>
                        <div style={{ display: "flex", gap: "8px", alignItems: "center", flexWrap: "wrap", marginBottom: "10px" }}>
                            <label style={{ fontSize: "0.78rem", display: "flex", alignItems: "center", gap: "6px", cursor: evolutionEnabledSaving || !!evolutionStatus?.env_disabled ? "default" : "pointer" }}>
                                <input
                                    type="checkbox"
                                    checked={evolutionEnabled && !evolutionStatus?.env_disabled}
                                    disabled={evolutionEnabledSaving || !!evolutionStatus?.env_disabled}
                                    onChange={(e) => { void saveEvolutionEnabled(e.target.checked); }}
                                    aria-label={localizeText("Enable automatic evolution", "启用自动自进化", "啟用自動自進化")}
                                />
                                {localizeText("Enable automatic evolution", "启用自动自进化", "啟用自動自進化")}
                            </label>
                            {evolutionEnabledSaving && (
                                <span style={{ fontSize: "0.72rem", color: colors.textSecondary }}>...</span>
                            )}
                        </div>
                        <div style={{ display: "flex", gap: "14px", alignItems: "center", flexWrap: "wrap", marginBottom: "10px" }}>
                            <label style={{ fontSize: "0.78rem", display: "flex", alignItems: "center", gap: "6px", cursor: observationSaving ? "default" : "pointer" }}>
                                <input
                                    type="checkbox"
                                    checked={observationEnabled}
                                    disabled={observationSaving}
                                    onChange={(e) => { void saveObservationEnabled(e.target.checked); }}
                                    aria-label={localizeText("Record maintenance observations", "记录维护观察", "記錄維護觀察")}
                                />
                                {localizeText("Record maintenance observations", "记录维护观察", "記錄維護觀察")}
                            </label>
                            <label style={{ fontSize: "0.78rem", display: "flex", alignItems: "center", gap: "6px" }}>
                                {localizeText("Concurrent workers", "并发工作数", "並發工作數")}
                                <input
                                    className="form-input"
                                    type="number"
                                    min={1}
                                    max={16}
                                    value={maxConcurrentWorkers}
                                    onChange={(e) => setMaxConcurrentWorkers(Number(e.target.value))}
                                    style={{ width: "64px", fontSize: "0.78rem" }}
                                    disabled={workersSaving}
                                />
                                <button
                                    className="btn-secondary"
                                    style={{ fontSize: "0.72rem", padding: "3px 9px" }}
                                    disabled={workersSaving}
                                    onClick={() => { void saveMaxConcurrentWorkers(); }}
                                >
                                    {workersSaving ? "..." : localizeText("Save", "保存", "儲存")}
                                </button>
                            </label>
                            <label style={{ fontSize: "0.78rem", display: "flex", alignItems: "center", gap: "6px" }}>
                                {localizeText("Worker timeout (sec)", "Worker 超时（秒）", "Worker 逾時（秒）")}
                                <input
                                    className="form-input"
                                    type="number"
                                    min={30}
                                    max={1800}
                                    value={workerTimeoutSeconds}
                                    onChange={(e) => setWorkerTimeoutSeconds(Number(e.target.value))}
                                    style={{ width: "78px", fontSize: "0.78rem" }}
                                    disabled={workerTimeoutSaving}
                                    title={localizeText("Background evolution worker timeout. Range: 30–1800 seconds.", "后台自进化 Worker 超时。范围：30–1800 秒。", "背景自進化 Worker 逾時。範圍：30–1800 秒。")}
                                />
                                <button
                                    className="btn-secondary"
                                    style={{ fontSize: "0.72rem", padding: "3px 9px" }}
                                    disabled={workerTimeoutSaving}
                                    onClick={() => { void saveWorkerTimeout(); }}
                                >
                                    {workerTimeoutSaving ? "..." : localizeText("Save", "保存", "儲存")}
                                </button>
                            </label>
                        </div>
                        <div style={{ fontSize: "0.7rem", color: colors.textSecondary, marginBottom: "8px" }}>
                            {localizeText(
                                "Observation is read-only evidence collection and can remain enabled while automatic evolution is paused. Worker count is limited to 1–16, timeout to 30–1800 seconds; the same skill is always serialized.",
                                "观察只采集只读证据；即使暂停自动自进化也可以保留。并发工作数限制为 1–16，Worker 超时限制为 30–1800 秒；同一技能始终串行。",
                                "觀察只收集唯讀證據；即使暫停自動自進化也可以保留。並發工作數限制為 1–16，Worker 逾時限制為 30–1800 秒；同一技能始終串行。",
                            )}
                        </div>
                        <div style={{ display: "flex", gap: "8px", alignItems: "center", flexWrap: "wrap" }}>
                            <label style={{ fontSize: "0.78rem" }}>
                                {localizeText("Repair cooldown (hours)", "自修复冷却（小时）", "自修復冷卻（小時）")}
                            </label>
                            <input
                                className="form-input"
                                type="number"
                                min={0}
                                max={720}
                                value={repairCooldownHours}
                                onChange={(e) => setRepairCooldownHours(Number(e.target.value))}
                                style={{ width: "88px", fontSize: "0.78rem" }}
                                disabled={repairCooldownSaving}
                            />
                            <button
                                className="btn-primary"
                                style={{ fontSize: "0.78rem", padding: "4px 12px" }}
                                disabled={repairCooldownSaving}
                                onClick={saveRepairCooldown}
                            >
                                {repairCooldownSaving
                                    ? localizeText("Saving...", "保存中...", "儲存中...")
                                    : localizeText("Save", "保存", "儲存")}
                            </button>
                            {repairCooldownMsg && (
                                <span style={{ fontSize: "0.76rem", color: colors.textSecondary }}>{repairCooldownMsg}</span>
                            )}
                        </div>
                        <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginTop: "10px", lineHeight: 1.45 }}>
                            {localizeText(
                                "Tip: when a skill is repaired, optimized, or auto-discovered, a toast appears and this list refreshes automatically. Manual Repair/Optimize still work when automatic evolution is off.",
                                "提示：技能被修复、优化或自动发现时，会弹出提示并自动刷新列表。关闭自动自进化后，手动「立即修复/立即优化」仍可用。",
                                "提示：技能被修復、優化或自動發現時，會彈出提示並自動重新整理列表。關閉自動自進化後，手動「立即修復/立即優化」仍可用。",
                            )}
                        </div>
                        <div style={{ marginTop: "8px" }}>
                            <button
                                type="button"
                                className="btn-secondary"
                                style={{ fontSize: "0.72rem", padding: "2px 10px" }}
                                onClick={() => {
                                    openSettingsTab('general');
                                }}
                            >
                                {localizeText(
                                    "Open General Settings",
                                    "打开通用设置",
                                    "開啟通用設定",
                                )}
                            </button>
                        </div>
                    </div>

                    <div style={{ ...remoteInfoPanelStyle, marginBottom: "12px", padding: "10px 12px" }}>
                        <div style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: "8px" }}>
                            <div style={{ fontSize: "0.8rem", fontWeight: 600 }}>
                                {localizeText("Pipeline status", "管道状态", "管道狀態")}
                            </div>
                            <button
                                className="btn-secondary"
                                style={{ fontSize: "0.72rem", padding: "2px 8px", marginLeft: "auto" }}
                                onClick={loadEvolutionSettings}
                                disabled={evolutionStatusLoading}
                            >
                                {evolutionStatusLoading ? "..." : localizeText("Refresh", "刷新", "重新整理")}
                            </button>
                        </div>
                        {evolutionStatus ? (
                            <>
                            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "6px 12px", fontSize: "0.76rem" }}>
                                <div>
                                    <strong>{localizeText("Started", "已启动", "已啟動")}</strong>
                                    <div>{evolutionStatus.pipeline_started ? "OK" : "—"}</div>
                                </div>
                                <div>
                                    <strong>{localizeText("Pending", "排队", "排隊")}</strong>
                                    <div>{evolutionStatus.pending_skills ?? 0}</div>
                                </div>
                                <div>
                                    <strong>{localizeText("Queue wait", "等待时间", "等待時間")}</strong>
                                    <div>{Number(evolutionStatus.queue_wait_seconds ?? 0)}s</div>
                                </div>
                                <div>
                                    <strong>{localizeText("Processed", "已处理", "已處理")}</strong>
                                    <div>{evolutionStatus.processed_requests ?? 0}</div>
                                </div>
                                <div>
                                    <strong>{localizeText("Coalesced", "合并通知", "合併通知")}</strong>
                                    <div>{Number(evolutionStatus.coalesced_notifications ?? 0)}</div>
                                </div>
                                <div>
                                    <strong>{localizeText("Repair", "自修复", "自修復")}</strong>
                                    <div>{evolutionStatus.enable_repair ? (evolutionStatus.has_repair_hook ? "on" : "on*") : "off"}</div>
                                </div>
                                <div>
                                    <strong>{localizeText("Optimize", "优化", "優化")}</strong>
                                    <div>{evolutionStatus.enable_optimizer && evolutionStatus.has_optimizer ? "on" : "off"}</div>
                                </div>
                                <div>
                                    <strong>{localizeText("Promote", "自动发现", "自動發現")}</strong>
                                    <div>{evolutionStatus.enable_promoter && evolutionStatus.has_promoter ? "on" : "off"}</div>
                                </div>
                                <div>
                                    <strong>{localizeText("Cooldown", "冷却", "冷卻")}</strong>
                                    <div>{evolutionStatus.repair_cooldown || "—"}</div>
                                </div>
                                <div>
                                    <strong>{localizeText("Workers", "并发工作数", "並發工作數")}</strong>
                                    <div>{evolutionStatus.max_concurrent_workers ?? maxConcurrentWorkers}</div>
                                </div>
                                <div>
                                    <strong>{localizeText("Active jobs", "运行中任务", "執行中任務")}</strong>
                                    <div>{evolutionStatus.active_skills ?? 0}</div>
                                </div>
                                <div>
                                    <strong>{localizeText("Cancelled", "已取消", "已取消")}</strong>
                                    <div>{evolutionStatus.cancelled_requests ?? 0}</div>
                                </div>
                                <div>
                                    <strong>{localizeText("Timed out", "已超时", "已逾時")}</strong>
                                    <div>{evolutionStatus.timed_out_requests ?? 0}</div>
                                </div>
                                <div>
                                    <strong>{localizeText("Recovery queue", "待恢复补偿", "待恢復補償")}</strong>
                                    <div style={{ color: (evolutionStatus.pending_compensations ?? 0) > 0 ? colors.danger : colors.success }}>
                                        {evolutionStatus.pending_compensations ?? 0}
                                    </div>
                                </div>
                                <div>
                                    <strong>{localizeText("Observation", "观察", "觀察")}</strong>
                                    <div>{evolutionStatus.observation_enabled === false ? "off" : "on"}</div>
                                </div>
                                <div>
                                    <strong>{localizeText("Timeout", "超时", "逾時")}</strong>
                                    <div>{Number(evolutionStatus.worker_timeout_seconds ?? 180)}s</div>
                                </div>
                                <div>
                                    <strong>{localizeText("Audit sink", "审计写入", "審計寫入")}</strong>
                                    <div style={{ color: evolutionStatus.audit_available === false ? colors.danger : colors.success }}>
                                        {evolutionStatus.audit_available === false
                                            ? localizeText("unavailable", "不可用", "不可用")
                                            : localizeText("healthy", "正常", "正常")}
                                    </div>
                                </div>
                                <div>
                                    <strong>{localizeText("Audit failures", "审计失败", "審計失敗")}</strong>
                                    <div>{Number(evolutionStatus.audit_failure_count ?? 0)}</div>
                                </div>
                            </div>
                            {evolutionStatus.last_audit_error && (
                                <div style={{ color: colors.danger, fontSize: "0.72rem", marginTop: "8px", overflowWrap: "anywhere" }}>
                                    {localizeText("Last audit error", "最近审计错误", "最近審計錯誤")}: {evolutionStatus.last_audit_error}
                                </div>
                            )}
                            {evolutionStatus.compensation_queue_healthy === false && (
                                <div style={{ color: colors.danger, fontSize: "0.72rem", marginTop: "8px", overflowWrap: "anywhere" }}>
                                    {localizeText("Recovery queue unreadable", "补偿队列不可读，已按安全策略阻断", "補償佇列不可讀，已按安全策略阻斷")}
                                    {evolutionStatus.compensation_queue_error ? `: ${evolutionStatus.compensation_queue_error}` : ""}
                                </div>
                            )}
                            {evolutionCompensationsError && evolutionStatus.compensation_queue_healthy !== false && (
                                <div style={{ color: colors.danger, fontSize: "0.72rem", marginTop: "8px", overflowWrap: "anywhere" }}>
                                    {localizeText("Recovery queue unavailable", "补偿队列不可用，已按安全策略阻断", "補償佇列不可用，已按安全策略阻斷")}: {evolutionCompensationsError}
                                </div>
                            )}
                            {(evolutionCompensationsLoading || evolutionCompensations.length > 0) && (
                                <div style={{ marginTop: "10px", fontSize: "0.72rem" }}>
                                    <strong>{localizeText("Pending compensation details", "待恢复补偿详情", "待恢復補償詳情")}</strong>
                                    {evolutionCompensationsLoading ? (
                                        <div style={{ color: colors.textSecondary, marginTop: "4px" }}>...</div>
                                    ) : (
                                        <div style={{ display: "flex", flexDirection: "column", gap: "4px", marginTop: "4px" }}>
                                            {evolutionCompensations.slice(0, 10).map((item, i) => {
                                                const needsReview = String(item.status || "").toLowerCase() === "needs_review";
                                                return (
                                                    <div key={`${item.request_id || item.skill || "compensation"}-${i}`} style={{ color: needsReview ? colors.danger : colors.textSecondary, overflowWrap: "anywhere" }}>
                                                        {item.skill || "—"} · {item.action || "rollback"} · {item.status || "pending"} · {Number(item.attempts || 0)}x
                                                        {item.request_id ? ` · ${item.request_id}` : ""}
                                                        {item.failure_reason ? ` · ${item.failure_reason}` : ""}
                                                        {item.last_error ? ` · ${item.last_error}` : ""}
                                                    </div>
                                                );
                                            })}
                                        </div>
                                    )}
                                </div>
                            )}
                            {(evolutionStatus.failure_summaries || []).length > 0 && (
                                <div style={{ marginTop: "8px", fontSize: "0.72rem" }}>
                                    <strong>{localizeText("Recent failures", "近期失败", "近期失敗")}</strong>
                                    <div style={{ display: "flex", flexDirection: "column", gap: "3px", marginTop: "4px" }}>
                                        {(evolutionStatus.failure_summaries || []).slice(0, 5).map((f, i) => (
                                            <div key={`${f.skill || "failure"}-${i}`} style={{ color: colors.textSecondary, overflowWrap: "anywhere" }}>
                                                {f.skill || "—"} · {f.last_error_class || "unknown"} · {f.failure_count || 0}x{f.last_error ? ` · ${f.last_error}` : ""}
                                                {f.skill ? (
                                                    <button
                                                        type="button"
                                                        className="btn-secondary"
                                                        style={{ fontSize: "0.65rem", padding: "1px 6px", marginLeft: "6px" }}
                                                        onClick={() => { void CancelSkillEvolution(f.skill!).then(() => { void GetSkillEvolutionStatus().then((st) => setEvolutionStatus(st)); }); }}
                                                    >
                                                        {localizeText("Cancel", "取消", "取消")}
                                                    </button>
                                                ) : null}
                                            </div>
                                        ))}
                                    </div>
                                </div>
                            )}
                            {(evolutionStatus.requests || []).length > 0 && (
                                <div style={{ marginTop: "8px", fontSize: "0.72rem" }}>
                                    <strong>{localizeText("Evolution tasks", "进化任务", "進化任務")}</strong>
                                    <div style={{ display: "flex", flexDirection: "column", gap: "3px", marginTop: "4px" }}>
                                        {(evolutionStatus.requests || []).map((request, i) => (
                                            <div key={`${request.request_id || request.skill || "request"}-${i}`} style={{ display: "flex", alignItems: "center", gap: "6px", color: colors.textSecondary, overflowWrap: "anywhere" }}>
                                                <span>{request.skill || "—"} · {request.state || "unknown"}</span>
                                                <span style={{ fontSize: "0.64rem" }}>{request.request_id || "—"}</span>
                                                {(request.skill && (request.state === "pending" || request.state === "running")) ? (
                                                    <button
                                                        type="button"
                                                        className="btn-secondary"
                                                        style={{ fontSize: "0.65rem", padding: "1px 6px", marginLeft: "auto" }}
                                                        onClick={() => {
                                                            void CancelSkillEvolution(request.skill!).then((cancelled) => {
                                                                if (cancelled) showToast(localizeText("Evolution task cancelled", "已取消进化任务", "已取消進化任務"), "success");
                                                                return GetSkillEvolutionStatus();
                                                            }).then((st) => setEvolutionStatus(st));
                                                        }}
                                                    >
                                                        {localizeText("Cancel", "取消", "取消")}
                                                    </button>
                                                ) : null}
                                            </div>
                                        ))}
                                    </div>
                                </div>
                            )}
                            </>
                        ) : (
                            <div style={{ fontSize: "0.76rem", color: colors.textSecondary }}>
                                {evolutionStatusLoading
                                    ? localizeText("Loading...", "加载中...", "載入中...")
                                    : localizeText("Status unavailable", "状态不可用", "狀態不可用")}
                            </div>
                        )}
                    </div>

                    {/* Pending human-reviewed repair drafts for file-backed skills (.evolution-drafts) */}
                    <SkillRepairDraftsPanel
                        localizeText={localizeText}
                        busy={busy}
                        setBusy={setBusy}
                        evolutionFocusSkill={evolutionFocusSkill}
                        onFocusSkill={(skill) => {
                            setEvolutionFocusSkill(skill || null);
                            evolutionAuditPanelRef.current?.scrollIntoView({ behavior: "smooth", block: "nearest" });
                        }}
                        onDraftsChanged={() => {
                            void loadEvolutionAudit();
                            void loadData();
                        }}
                    />

                    {/* Patch / merge review drafts (from maintenance dry-run) */}
                    <div ref={evolutionDraftsPanelRef} style={{ ...remoteInfoPanelStyle, marginBottom: "12px", padding: "10px 12px" }}>
                        <div style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: "6px", flexWrap: "wrap" }}>
                            <div style={{ fontSize: "0.8rem", fontWeight: 600 }}>
                                {localizeText("Draft review", "草案人审", "草案人審")}
                            </div>
                            {repairDraftSkills.length > 0 && (
                                <button
                                    type="button"
                                    className="btn-secondary"
                                    style={{ fontSize: "0.7rem", padding: "2px 8px" }}
                                    disabled={busy}
                                    onClick={() => {
                                        void handleBatchSetStatus(
                                            repairDraftSkills,
                                            "needs_review",
                                            localizeText("Batch mark needs_review", "批量标记待审", "批量標記待審"),
                                        );
                                    }}
                                >
                                    {localizeText(
                                        `Mark all repair drafts (${repairDraftSkills.length})`,
                                        `修复草案全部待审 (${repairDraftSkills.length})`,
                                        `修復草案全部待審 (${repairDraftSkills.length})`,
                                    )}
                                </button>
                            )}
                            <button
                                type="button"
                                className="btn-secondary"
                                style={{ fontSize: "0.7rem", padding: "2px 8px", marginLeft: "auto" }}
                                disabled={maintenanceDraftsLoading}
                                onClick={() => { void loadMaintenanceDrafts(); }}
                            >
                                {maintenanceDraftsLoading ? "..." : localizeText("Refresh", "刷新", "重新整理")}
                            </button>
                        </div>
                        <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginBottom: "8px" }}>
                            {localizeText(
                                "Repair review packets, contract patches, and duplicate merges. File-backed YAML is never auto-written for repair; contract apply creates a Versioner backup; merge retire needs two confirmations.",
                                "修复审阅包、契约补丁与重复合并。文件型修复不会自动写 YAML；契约应用会做 Versioner 备份；合并退役需两次确认。",
                                "修復審閱包、契約修補與重複合併。檔案型修復不會自動寫 YAML；契約套用會建立 Versioner 備份；合併退役需兩次確認。",
                            )}
                        </div>
                        {maintenanceDrafts?.plan_summary && (
                            <div style={{ fontSize: "0.72rem", color: colors.textMuted, marginBottom: "8px" }}>
                                {maintenanceDrafts.plan_summary}
                            </div>
                        )}
                        {maintenanceDraftsLoading && !maintenanceDrafts ? (
                            <div style={{ fontSize: "0.74rem", color: colors.textMuted }}>
                                {localizeText("Loading...", "加载中...", "載入中...")}
                            </div>
                        ) : (
                            <>
                                {(() => {
                                    const allPatches = maintenanceDrafts?.patch_drafts || [];
                                    const repairDrafts = allPatches.filter(isRepairPatchDraft);
                                    const contractDrafts = allPatches.filter((d) => !isRepairPatchDraft(d));
                                    return (
                                        <>
                                <div style={{ fontSize: "0.76rem", fontWeight: 600, marginBottom: "4px" }}>
                                    {localizeText("Repair drafts", "修复草案", "修復草案")}
                                    {" "}({repairDrafts.length})
                                </div>
                                {repairDrafts.length === 0 ? (
                                    <div style={{ fontSize: "0.74rem", color: colors.textMuted, marginBottom: "10px" }}>
                                        {localizeText("No file-backed repair review packets.", "暂无文件型修复审阅包。", "暫無檔案型修復審閱包。")}
                                    </div>
                                ) : (
                                    <div style={{ display: "flex", flexDirection: "column", gap: "6px", marginBottom: "12px" }}>
                                        {repairDrafts.map((d, i) => {
                                            const key = `repair-${d.skill || i}`;
                                            const open = expandedDraftKey === key;
                                            const errClass = String(d.error_class || "").trim();
                                            const focused = skillNamesEqual(d.skill, evolutionFocusSkill);
                                            return (
                                                <div
                                                    key={key}
                                                    data-evolution-skill={d.skill || ""}
                                                    style={{
                                                        border: focused
                                                            ? `1px solid ${colors.primary}`
                                                            : `1px solid ${colors.warning || colors.primary || colors.border}`,
                                                        borderLeft: `3px solid ${focused ? colors.primary : (colors.warning || colors.primary || "#d97706")}`,
                                                        borderRadius: 6,
                                                        padding: "6px 8px",
                                                        background: focused
                                                            ? "rgba(59, 130, 246, 0.12)"
                                                            : "rgba(217, 119, 6, 0.06)",
                                                        boxShadow: focused ? `0 0 0 1px ${colors.primary}` : undefined,
                                                    }}
                                                >
                                                    <div style={{ display: "flex", gap: "8px", alignItems: "center", flexWrap: "wrap" }}>
                                                        <strong
                                                            style={{ ...skillNameLinkStyle, fontSize: "0.76rem" }}
                                                            title={localizeText(
                                                                "Highlight related audit rows",
                                                                "高亮关联审计记录",
                                                                "高亮關聯審計記錄",
                                                            )}
                                                            onClick={() => focusEvolutionSkill(d.skill || "", "draft")}
                                                        >
                                                            {d.skill || "—"}
                                                        </strong>
                                                        <span style={{
                                                            fontSize: "0.65rem",
                                                            padding: "1px 6px",
                                                            borderRadius: 999,
                                                            background: "rgba(217, 119, 6, 0.15)",
                                                            color: colors.textSecondary,
                                                            fontWeight: 600,
                                                        }}>
                                                            {localizeText("repair review", "修复审阅", "修復審閱")}
                                                        </span>
                                                        {errClass && (
                                                            <span style={{ fontSize: "0.68rem", color: colors.danger || colors.textMuted, fontFamily: "ui-monospace, monospace" }}>
                                                                {errClass}
                                                            </span>
                                                        )}
                                                        <button
                                                            type="button"
                                                            className="btn-secondary"
                                                            style={{ fontSize: "0.7rem", padding: "2px 8px", marginLeft: "auto" }}
                                                            onClick={() => setExpandedDraftKey(open ? null : key)}
                                                        >
                                                            {open
                                                                ? localizeText("Hide", "收起", "收起")
                                                                : localizeText("Show packet", "查看审阅包", "查看審閱包")}
                                                        </button>
                                                        {d.suggested_yaml && (
                                                            <button
                                                                type="button"
                                                                className="btn-secondary"
                                                                style={{ fontSize: "0.7rem", padding: "2px 8px" }}
                                                                onClick={() => { void copyText(d.suggested_yaml || "", localizeText("Copied review YAML", "已复制审阅 YAML", "已複製審閱 YAML")); }}
                                                            >
                                                                {localizeText("Copy YAML", "复制 YAML", "複製 YAML")}
                                                            </button>
                                                        )}
                                                        <button
                                                            type="button"
                                                            className="btn-secondary"
                                                            style={{ fontSize: "0.7rem", padding: "2px 8px" }}
                                                            onClick={() => {
                                                                const payload = {
                                                                    action: "attempt_repair",
                                                                    skill: d.skill,
                                                                    skill_dir: d.skill_dir,
                                                                    error_class: d.error_class,
                                                                    last_error: d.last_error,
                                                                    action_hint: d.action_hint,
                                                                    evidence: d.evidence,
                                                                    note: "file-backed: edit skill.yaml/scripts under skill_dir after review; never auto-applied",
                                                                    suggested_yaml: d.suggested_yaml,
                                                                    recommended_action: d.recommended_action,
                                                                };
                                                                void copyText(JSON.stringify(payload, null, 2), localizeText("Copied review packet", "已复制审阅包", "已複製審閱包"));
                                                            }}
                                                        >
                                                            {localizeText("Copy packet", "复制审阅包", "複製審閱包")}
                                                        </button>
                                                        {d.skill_dir && (
                                                            <button
                                                                type="button"
                                                                className="btn-primary"
                                                                style={{ fontSize: "0.7rem", padding: "2px 8px" }}
                                                                onClick={() => {
                                                                    void OpenFileOrShowInFolder(d.skill_dir || "").catch((err) => {
                                                                        showToast(String(err), "error");
                                                                    });
                                                                }}
                                                            >
                                                                {localizeText("Open folder", "打开目录", "開啟目錄")}
                                                            </button>
                                                        )}
                                                        {d.skill && (
                                                            <button
                                                                type="button"
                                                                className="btn-secondary"
                                                                style={{ fontSize: "0.7rem", padding: "2px 8px" }}
                                                                onClick={() => openSkillFromAudit(d.skill || "")}
                                                            >
                                                                {localizeText("Open skill", "打开技能", "開啟技能")}
                                                            </button>
                                                        )}
                                                        {d.skill && (
                                                            <button
                                                                type="button"
                                                                className="btn-secondary"
                                                                style={{ fontSize: "0.7rem", padding: "2px 8px" }}
                                                                disabled={busy}
                                                                title={localizeText(
                                                                    "Park skill as needs_review until you re-activate it",
                                                                    "将技能挂起为 needs_review，直到你重新启用",
                                                                    "將技能掛起為 needs_review，直到你重新啟用",
                                                                )}
                                                                onClick={() => {
                                                                    void handleMarkNeedsReview(
                                                                        d.skill || "",
                                                                        d.error_class || d.action_hint || d.recommended_action,
                                                                    );
                                                                }}
                                                            >
                                                                {localizeText("Mark needs_review", "标记待审", "標記待審")}
                                                            </button>
                                                        )}
                                                    </div>
                                                    {d.action_hint && (
                                                        <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginTop: 4 }}>
                                                            {d.action_hint}
                                                        </div>
                                                    )}
                                                    {d.recommended_action && (
                                                        <div style={{ fontSize: "0.72rem", color: colors.textMuted, marginTop: 2 }}>{d.recommended_action}</div>
                                                    )}
                                                    {d.last_error && !open && (
                                                        <div style={{
                                                            fontSize: "0.7rem",
                                                            color: colors.textMuted,
                                                            marginTop: 4,
                                                            overflow: "hidden",
                                                            textOverflow: "ellipsis",
                                                            whiteSpace: "nowrap",
                                                        }}>
                                                            {d.last_error}
                                                        </div>
                                                    )}
                                                    {open && (
                                                        <div style={{ marginTop: 6 }}>
                                                            {d.last_error && (
                                                                <pre style={{ ...remoteCodeBlockStyle, fontSize: "0.7rem", maxHeight: 120, overflow: "auto", marginBottom: 6 }}>
                                                                    {d.last_error}
                                                                </pre>
                                                            )}
                                                            {d.suggested_yaml && (
                                                                <pre style={{ ...remoteCodeBlockStyle, fontSize: "0.7rem", maxHeight: 200, overflow: "auto" }}>
                                                                    {d.suggested_yaml}
                                                                </pre>
                                                            )}
                                                            {(d.evidence || []).length > 0 && (
                                                                <div style={{ fontSize: "0.68rem", color: colors.textMuted, marginTop: 4 }}>
                                                                    {(d.evidence || []).join(" · ")}
                                                                </div>
                                                            )}
                                                        </div>
                                                    )}
                                                </div>
                                            );
                                        })}
                                    </div>
                                )}

                                <div style={{ fontSize: "0.76rem", fontWeight: 600, marginBottom: "4px" }}>
                                    {localizeText("Contract patches", "契约补丁", "契約修補")}
                                    {" "}({contractDrafts.length})
                                </div>
                                {contractDrafts.length === 0 ? (
                                    <div style={{ fontSize: "0.74rem", color: colors.textMuted, marginBottom: "10px" }}>
                                        {localizeText("No contract patch drafts.", "暂无契约补丁草案。", "暫無契約修補草案。")}
                                    </div>
                                ) : (
                                    <div style={{ display: "flex", flexDirection: "column", gap: "6px", marginBottom: "12px" }}>
                                        {contractDrafts.map((d, i) => {
                                            const key = `patch-${d.skill || i}`;
                                            const open = expandedDraftKey === key;
                                            return (
                                                <div key={key} style={{ border: `1px solid ${colors.border}`, borderRadius: 6, padding: "6px 8px" }}>
                                                    <div style={{ display: "flex", gap: "8px", alignItems: "center", flexWrap: "wrap" }}>
                                                        <strong style={{ fontSize: "0.76rem" }}>{d.skill || "—"}</strong>
                                                        <span style={{ fontSize: "0.7rem", color: colors.textMuted }}>{d.target_file || "skill.yaml"}</span>
                                                        <button
                                                            type="button"
                                                            className="btn-secondary"
                                                            style={{ fontSize: "0.7rem", padding: "2px 8px", marginLeft: "auto" }}
                                                            onClick={() => setExpandedDraftKey(open ? null : key)}
                                                        >
                                                            {open ? localizeText("Hide", "收起", "收起") : localizeText("Show YAML", "查看 YAML", "查看 YAML")}
                                                        </button>
                                                        {d.suggested_yaml && (
                                                            <button
                                                                type="button"
                                                                className="btn-secondary"
                                                                style={{ fontSize: "0.7rem", padding: "2px 8px" }}
                                                                onClick={() => { void copyText(d.suggested_yaml || "", localizeText("Copied YAML", "已复制 YAML", "已複製 YAML")); }}
                                                            >
                                                                {localizeText("Copy YAML", "复制 YAML", "複製 YAML")}
                                                            </button>
                                                        )}
                                                        <button
                                                            type="button"
                                                            className="btn-secondary"
                                                            style={{ fontSize: "0.7rem", padding: "2px 8px" }}
                                                            onClick={() => {
                                                                const payload = {
                                                                    action: "improve_contract",
                                                                    skill: d.skill,
                                                                    note: "file-backed: paste suggested_yaml into skill.yaml after review; config-backed: use Apply",
                                                                    suggested_yaml: d.suggested_yaml,
                                                                    recommended_action: d.recommended_action,
                                                                };
                                                                void copyText(JSON.stringify(payload, null, 2), localizeText("Copied review packet", "已复制审阅包", "已複製審閱包"));
                                                            }}
                                                        >
                                                            {localizeText("Copy packet", "复制审阅包", "複製審閱包")}
                                                        </button>
                                                        {d.skill_dir && (
                                                            <button
                                                                type="button"
                                                                className="btn-secondary"
                                                                style={{ fontSize: "0.7rem", padding: "2px 8px" }}
                                                                onClick={() => {
                                                                    void OpenFileOrShowInFolder(d.skill_dir || "").catch((err) => {
                                                                        showToast(String(err), "error");
                                                                    });
                                                                }}
                                                            >
                                                                {localizeText("Open folder", "打开目录", "開啟目錄")}
                                                            </button>
                                                        )}
                                                        {d.skill_dir && d.skill && (() => {
                                                            const info = yamlBackupInfo[d.skill || ""];
                                                            const versions = info?.versions || [];
                                                            if (versions.length === 0) {
                                                                return (
                                                                    <button
                                                                        type="button"
                                                                        className="btn-secondary"
                                                                        style={{ fontSize: "0.7rem", padding: "2px 8px" }}
                                                                        disabled={busy}
                                                                        title={localizeText("No backups yet — apply first to create one", "尚无备份 — 先应用可生成", "尚無備份 — 先套用可產生")}
                                                                        onClick={() => { void handleRollbackYAML(d.skill || ""); }}
                                                                    >
                                                                        {localizeText("Rollback", "回滚", "回滾")}
                                                                    </button>
                                                                );
                                                            }
                                                            return (
                                                                <span style={{ display: "inline-flex", gap: 4, alignItems: "center" }}>
                                                                    <select
                                                                        className="form-input"
                                                                        style={{ fontSize: "0.7rem", padding: "2px 6px", width: "auto", minWidth: 72 }}
                                                                        value={info?.selected || info?.latest || versions[0]}
                                                                        disabled={busy}
                                                                        aria-label={localizeText("Backup version", "备份版本", "備份版本")}
                                                                        onChange={(e) => {
                                                                            const v = Number(e.target.value);
                                                                            setYamlBackupInfo((prev) => ({
                                                                                ...prev,
                                                                                [d.skill || ""]: {
                                                                                    versions,
                                                                                    latest: info?.latest || versions[0],
                                                                                    selected: v,
                                                                                },
                                                                            }));
                                                                        }}
                                                                    >
                                                                        {versions.map((ver) => (
                                                                            <option key={ver} value={ver}>v{ver}</option>
                                                                        ))}
                                                                    </select>
                                                                    <button
                                                                        type="button"
                                                                        className="btn-secondary"
                                                                        style={{ fontSize: "0.7rem", padding: "2px 8px" }}
                                                                        disabled={busy}
                                                                        title={localizeText(
                                                                            "Restore skill.yaml from selected Versioner backup",
                                                                            "从所选 Versioner 备份恢复 skill.yaml",
                                                                            "從所選 Versioner 備份還原 skill.yaml",
                                                                        )}
                                                                        onClick={() => {
                                                                            const sel = yamlBackupInfo[d.skill || ""]?.selected || info?.latest;
                                                                            void handleRollbackYAML(d.skill || "", sel);
                                                                        }}
                                                                    >
                                                                        {localizeText("Rollback", "回滚", "回滾")}
                                                                    </button>
                                                                </span>
                                                            );
                                                        })()}
                                                        <button
                                                            type="button"
                                                            className="btn-primary"
                                                            style={{ fontSize: "0.7rem", padding: "2px 8px" }}
                                                            disabled={busy}
                                                            onClick={() => { void handleApplyContractDraft(d); }}
                                                        >
                                                            {localizeText("Apply (with backup)", "应用（含备份）", "套用（含備份）")}
                                                        </button>
                                                    </div>
                                                    {d.recommended_action && (
                                                        <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginTop: 4 }}>{d.recommended_action}</div>
                                                    )}
                                                    {open && d.suggested_yaml && (
                                                        <pre style={{ ...remoteCodeBlockStyle, marginTop: 6, fontSize: "0.7rem", maxHeight: 200, overflow: "auto" }}>
                                                            {d.suggested_yaml}
                                                        </pre>
                                                    )}
                                                </div>
                                            );
                                        })}
                                    </div>
                                )}
                                        </>
                                    );
                                })()}

                                <div style={{ fontSize: "0.76rem", fontWeight: 600, marginBottom: "4px" }}>
                                    {localizeText("Merge drafts", "合并草案", "合併草案")}
                                    {" "}({maintenanceDrafts?.merge_drafts?.length ?? 0})
                                </div>
                                {(maintenanceDrafts?.merge_drafts?.length ?? 0) === 0 ? (
                                    <div style={{ fontSize: "0.74rem", color: colors.textMuted, marginBottom: "10px" }}>
                                        {localizeText("No duplicate merge drafts.", "暂无重复合并草案。", "暫無重複合併草案。")}
                                    </div>
                                ) : (
                                    <div style={{ display: "flex", flexDirection: "column", gap: "6px", marginBottom: "12px" }}>
                                        {(maintenanceDrafts?.merge_drafts || []).map((d, i) => (
                                            <div key={`merge-${d.primary_skill}-${d.duplicate_skill}-${i}`} style={{ border: `1px solid ${colors.border}`, borderRadius: 6, padding: "6px 8px", fontSize: "0.74rem" }}>
                                                <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
                                                    <div style={{ flex: "1 1 200px" }}>
                                                        <strong>{d.recommended_keep || d.primary_skill}</strong>
                                                        <span style={{ color: colors.textMuted }}> {localizeText("keep", "保留", "保留")} </span>
                                                        <span style={{ color: colors.textSecondary }}>/ </span>
                                                        <strong style={{ color: colors.danger }}>{d.recommended_retire || d.duplicate_skill}</strong>
                                                        <span style={{ color: colors.textMuted }}> {localizeText("retire", "退役", "退役")}</span>
                                                    </div>
                                                    <button
                                                        type="button"
                                                        className="btn-primary"
                                                        style={{ fontSize: "0.7rem", padding: "2px 8px" }}
                                                        disabled={busy}
                                                        onClick={() => { void handleApplyMergeDraft(d); }}
                                                    >
                                                        {localizeText("Retire (2-step confirm)", "退役（两次确认）", "退役（兩次確認）")}
                                                    </button>
                                                </div>
                                                {(d.reasons || []).length > 0 && (
                                                    <div style={{ color: colors.textSecondary, marginTop: 4 }}>{(d.reasons || []).join(" · ")}</div>
                                                )}
                                                {d.recommended_action && (
                                                    <div style={{ color: colors.textMuted, marginTop: 4 }}>{d.recommended_action}</div>
                                                )}
                                            </div>
                                        ))}
                                    </div>
                                )}

                                {(maintenanceDrafts?.queued_repair?.length ?? 0) > 0 && (
                                    <>
                                        <div style={{ fontSize: "0.76rem", fontWeight: 600, marginBottom: "4px" }}>
                                            {localizeText("Queued self-repair", "排队自修复", "排隊自修復")}
                                            {" "}({maintenanceDrafts?.queued_repair?.length ?? 0})
                                        </div>
                                        <div style={{ display: "flex", flexDirection: "column", gap: "4px", marginBottom: "4px" }}>
                                            {(maintenanceDrafts?.queued_repair || []).map((q, i) => (
                                                <div key={`qr-${q.skill}-${i}`} style={{ fontSize: "0.74rem", display: "flex", gap: 8, alignItems: "center" }}>
                                                    <strong>{q.skill}</strong>
                                                    <span style={{ color: colors.textSecondary, flex: 1 }}>{q.reason}</span>
                                                    {q.skill && (
                                                        <button
                                                            type="button"
                                                            className="btn-secondary"
                                                            style={{ fontSize: "0.7rem", padding: "2px 8px" }}
                                                            disabled={busy || detailActionBusy !== null}
                                                            onClick={() => {
                                                                const s = skills.find((x) => x.name === q.skill);
                                                                if (s) void handleTriggerSelfRepair(s);
                                                            }}
                                                        >
                                                            {localizeText("Repair now", "立即修复", "立即修復")}
                                                        </button>
                                                    )}
                                                </div>
                                            ))}
                                        </div>
                                    </>
                                )}
                            </>
                        )}
                    </div>

                    {/* Retired / archived by maintenance — one-click re-enable */}
                    <div style={{ ...remoteInfoPanelStyle, marginBottom: "12px", padding: "10px 12px" }}>
                        <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: "6px", flexWrap: "wrap" }}>
                            <div style={{ fontSize: "0.8rem", fontWeight: 600 }}>
                                {localizeText("Retired / archived", "已退役 / 已归档", "已退役 / 已封存")}
                                {" "}({retiredSkills.length})
                            </div>
                            {retiredSkills.length > 1 && (
                                <button
                                    type="button"
                                    className="btn-secondary"
                                    style={{ fontSize: "0.7rem", padding: "2px 8px", marginLeft: "auto" }}
                                    disabled={busy}
                                    onClick={() => {
                                        void handleBatchSetStatus(
                                            retiredSkills.map((s) => s.name),
                                            "active",
                                            localizeText("Batch re-enable retired", "批量恢复退役", "批量恢復退役"),
                                        );
                                    }}
                                >
                                    {localizeText("Re-enable all", "全部重新启用", "全部重新啟用")}
                                </button>
                            )}
                        </div>
                        <div style={{ fontSize: "0.74rem", color: colors.textSecondary, marginBottom: "8px" }}>
                            {localizeText(
                                "Disabled by merge retire or stale archive (metadata only). Re-enable restores active status.",
                                "因合并退役或过期归档而禁用（仅元数据）。重新启用会恢复 active。",
                                "因合併退役或過期封存而停用（僅元資料）。重新啟用會恢復 active。",
                            )}
                        </div>
                        {retiredSkills.length === 0 ? (
                            <div style={{ fontSize: "0.74rem", color: colors.textMuted }}>
                                {localizeText("No maintenance-retired skills.", "没有维护退役的技能。", "沒有維護退役的技能。")}
                            </div>
                        ) : (
                            <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
                                {retiredSkills.map((s) => (
                                    <div
                                        key={`retired-${s.name}`}
                                        style={{
                                            display: "flex",
                                            gap: "8px",
                                            alignItems: "center",
                                            flexWrap: "wrap",
                                            padding: "6px 8px",
                                            borderRadius: 6,
                                            border: `1px solid ${colors.border}`,
                                        }}
                                    >
                                        <span
                                            style={{ ...skillNameLinkStyle, flex: "1 1 120px", minWidth: 0 }}
                                            onClick={() => setDetailSkill(s)}
                                        >
                                            {s.name}
                                        </span>
                                        <span
                                            style={{ fontSize: "0.7rem", color: colors.textSecondary, flex: "2 1 160px", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}
                                            title={s.last_error}
                                        >
                                            {s.last_error || "—"}
                                        </span>
                                        <button
                                            className="btn-primary"
                                            style={{ fontSize: "0.72rem", padding: "2px 8px", flexShrink: 0 }}
                                            disabled={busy}
                                            onClick={() => { void handleUnretireSkill(s); }}
                                        >
                                            {localizeText("Re-enable", "重新启用", "重新啟用")}
                                        </button>
                                    </div>
                                ))}
                            </div>
                        )}
                    </div>

                    {/* Review queue: needs_review skills waiting for human approval */}
                    <div style={{ ...remoteInfoPanelStyle, marginBottom: "12px", padding: "10px 12px" }}>
                        <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: "6px", flexWrap: "wrap" }}>
                            <div style={{ fontSize: "0.8rem", fontWeight: 600 }}>
                                {localizeText("Review queue", "待审核", "待審核")}
                                {" "}({reviewQueue.length})
                            </div>
                            {reviewQueue.length > 1 && (
                                <button
                                    type="button"
                                    className="btn-secondary"
                                    style={{ fontSize: "0.7rem", padding: "2px 8px", marginLeft: "auto" }}
                                    disabled={busy}
                                    onClick={() => {
                                        void handleBatchSetStatus(
                                            reviewQueue.map((s) => s.name),
                                            "active",
                                            localizeText("Batch approve review queue", "批量通过待审核", "批量通過待審核"),
                                        );
                                    }}
                                >
                                    {localizeText("Approve all", "全部通过", "全部通過")}
                                </button>
                            )}
                        </div>
                        <div style={{ fontSize: "0.74rem", color: colors.textSecondary, marginBottom: "8px" }}>
                            {localizeText(
                                "Skills marked needs_review (e.g. after self-repair flags or security). Approve to re-enable.",
                                "状态为 needs_review 的技能（如自修复标记或安全审查）。通过后重新启用。",
                                "狀態為 needs_review 的技能（如自修復標記或安全審查）。通過後重新啟用。",
                            )}
                        </div>
                        {reviewQueue.length === 0 ? (
                            <div style={{ fontSize: "0.74rem", color: colors.textMuted }}>
                                {localizeText("No skills waiting for review.", "当前没有待审核技能。", "目前沒有待審核技能。")}
                            </div>
                        ) : (
                            <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
                                {reviewQueue.map((s) => (
                                    <div
                                        key={`review-${s.name}`}
                                        style={{
                                            display: "flex",
                                            gap: "8px",
                                            alignItems: "center",
                                            flexWrap: "wrap",
                                            padding: "6px 8px",
                                            borderRadius: 6,
                                            border: `1px solid ${colors.border}`,
                                        }}
                                    >
                                        <span
                                            style={{ ...skillNameLinkStyle, flex: "1 1 120px", minWidth: 0 }}
                                            onClick={() => setDetailSkill(s)}
                                            title={skillReviewReason(s, localizeText) || s.name}
                                        >
                                            {s.name}
                                        </span>
                                        <span
                                            style={{ fontSize: "0.7rem", color: colors.textSecondary, flex: "2 1 160px", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}
                                            title={skillReviewReason(s, localizeText)}
                                        >
                                            {skillReviewReasonPreview(s, localizeText) || "—"}
                                        </span>
                                        <button
                                            className="btn-primary"
                                            style={{ fontSize: "0.72rem", padding: "2px 8px", flexShrink: 0 }}
                                            disabled={busy}
                                            onClick={() => { void handleApproveSkillReview(s); }}
                                        >
                                            {localizeText("Approve", "通过", "通過")}
                                        </button>
                                    </div>
                                ))}
                            </div>
                        )}
                    </div>

                    {/* Session activity feed (in-memory; from evolution events) */}
                    <div style={{ ...remoteInfoPanelStyle, marginBottom: "12px", padding: "10px 12px" }}>
                        <div style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: "6px" }}>
                            <div style={{ fontSize: "0.8rem", fontWeight: 600 }}>
                                {localizeText("Recent activity", "最近活动", "最近活動")}
                                {" "}({evolutionActivity.length})
                            </div>
                            {evolutionActivity.length > 0 && (
                                <button
                                    type="button"
                                    className="btn-secondary"
                                    style={{ fontSize: "0.7rem", padding: "2px 8px", marginLeft: "auto" }}
                                    onClick={() => setEvolutionActivity([])}
                                >
                                    {localizeText("Clear", "清空", "清空")}
                                </button>
                            )}
                        </div>
                        <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginBottom: "8px" }}>
                            {localizeText(
                                "In-session log of repair / optimize / discover / failure events (not persisted).",
                                "本会话内的修复/优化/发现/失败事件（不落盘）。",
                                "本工作階段內的修復/優化/發現/失敗事件（不落盤）。",
                            )}
                        </div>
                        {evolutionActivity.length === 0 ? (
                            <div style={{ fontSize: "0.74rem", color: colors.textMuted }}>
                                {localizeText("No evolution events yet this session.", "本会话尚无进化事件。", "本工作階段尚無進化事件。")}
                            </div>
                        ) : (
                            <div style={{ display: "flex", flexDirection: "column", gap: "4px", maxHeight: 220, overflowY: "auto" }}>
                                {evolutionActivity.map((item) => {
                                    const kindLabel = (() => {
                                        switch (item.kind) {
                                            case "repaired": return localizeText("repaired", "已修复", "已修復");
                                            case "optimized": return localizeText("optimized", "已优化", "已優化");
                                            case "discovered": return localizeText("discovered", "已发现", "已發現");
                                            case "failed": return localizeText("failed", "执行失败", "執行失敗");
                                            default: return item.kind;
                                        }
                                    })();
                                    const tone = item.kind === "failed"
                                        ? colors.danger
                                        : item.kind === "optimized"
                                            ? colors.textSecondary
                                            : colors.success;
                                    return (
                                        <div
                                            key={item.id}
                                            style={{
                                                fontSize: "0.72rem",
                                                padding: "4px 6px",
                                                borderLeft: `3px solid ${tone}`,
                                                background: "var(--theme-surface-2, transparent)",
                                            }}
                                        >
                                            <span style={{ color: colors.textMuted, marginRight: 6 }}>
                                                {new Date(item.at).toLocaleTimeString()}
                                            </span>
                                            <strong style={{ color: tone, marginRight: 6 }}>{kindLabel}</strong>
                                            <span style={{ fontWeight: 500 }}>{item.skill}</span>
                                            {item.explanation ? (
                                                <div style={{ color: colors.textSecondary, marginTop: 2, whiteSpace: "pre-wrap" }}>
                                                    {item.explanation.length > 160 ? `${item.explanation.slice(0, 160)}…` : item.explanation}
                                                </div>
                                            ) : null}
                                        </div>
                                    );
                                })}
                            </div>
                        )}
                    </div>

                    {/* Durable audit log (JSONL on disk) */}
                    <div ref={evolutionAuditPanelRef} style={{ ...remoteInfoPanelStyle, marginBottom: "12px", padding: "10px 12px" }}>
                        <div style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: "6px", flexWrap: "wrap" }}>
                            <div style={{ fontSize: "0.8rem", fontWeight: 600 }}>
                                {localizeText("Audit history", "审计历史", "審計歷史")}
                                {" "}(
                                {(
                                    evolutionAuditFilter !== "all"
                                    || evolutionAuditTimeRange !== "all"
                                    || !!evolutionAuditSkillQuery.trim()
                                    || (evolutionAuditFocusOnly && !!evolutionFocusSkill)
                                )
                                    ? `${filteredEvolutionAudit.length}/${evolutionAudit.length}`
                                    : evolutionAudit.length}
                                )
                            </div>
                            {evolutionFocusSkill && (
                                <>
                                    <label
                                        style={{
                                            display: "inline-flex",
                                            alignItems: "center",
                                            gap: 4,
                                            fontSize: "0.68rem",
                                            color: colors.textSecondary,
                                            cursor: "pointer",
                                            userSelect: "none",
                                        }}
                                        title={localizeText(
                                            "Only show audit rows for the focused skill",
                                            "审计列表仅显示聚焦技能",
                                            "審計列表僅顯示聚焦技能",
                                        )}
                                    >
                                        <input
                                            type="checkbox"
                                            checked={evolutionAuditFocusOnly}
                                            onChange={(e) => setEvolutionAuditFocusOnly(e.target.checked)}
                                        />
                                        {localizeText("Focus only", "仅聚焦技能", "僅聚焦技能")}
                                    </label>
                                    <button
                                        type="button"
                                        className="btn-secondary"
                                        style={{ fontSize: "0.66rem", padding: "2px 8px" }}
                                        onClick={() => clearEvolutionFocus()}
                                    >
                                        {localizeText(
                                            `Clear focus (${evolutionFocusSkill})`,
                                            `清除聚焦（${evolutionFocusSkill}）`,
                                            `清除聚焦（${evolutionFocusSkill}）`,
                                        )}
                                    </button>
                                </>
                            )}
                            <button
                                type="button"
                                className="btn-secondary"
                                style={{ fontSize: "0.7rem", padding: "2px 8px" }}
                                disabled={evolutionAuditLoading}
                                onClick={() => { void exportEvolutionAudit("json"); }}
                            >
                                {localizeText("Export JSON", "导出 JSON", "匯出 JSON")}
                            </button>
                            <button
                                type="button"
                                className="btn-secondary"
                                style={{ fontSize: "0.7rem", padding: "2px 8px" }}
                                disabled={evolutionAuditLoading}
                                onClick={() => { void exportEvolutionAudit("csv"); }}
                            >
                                {localizeText("Export CSV", "导出 CSV", "匯出 CSV")}
                            </button>
                            <button
                                type="button"
                                className="btn-secondary"
                                style={{ fontSize: "0.7rem", padding: "2px 8px", marginLeft: "auto" }}
                                disabled={evolutionAuditLoading}
                                onClick={() => { void loadEvolutionAudit(); }}
                            >
                                {evolutionAuditLoading ? "..." : localizeText("Refresh", "刷新", "重新整理")}
                            </button>
                        </div>
                        <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginBottom: "8px" }}>
                            {localizeText(
                                "Persisted under ~/.maclaw/skill_evolution/audit.jsonl. Filter by kind or search text; export opens a save dialog; click a skill name to open details; YAML restore rows offer rollback.",
                                "持久化：~/.maclaw/skill_evolution/audit.jsonl。可按类型筛选或搜索；导出走系统另存为；点击技能名打开详情；YAML 恢复类记录可回滚。",
                                "持久化：~/.maclaw/skill_evolution/audit.jsonl。可依類型篩選或搜尋；匯出走系統另存新檔；點擊技能名開啟詳情；YAML 還原類記錄可回滾。",
                            )}
                        </div>
                        <div style={{ display: "flex", flexWrap: "wrap", gap: 4, marginBottom: 8, alignItems: "center" }}>
                            {([
                                { id: "all", en: "All", zh: "全部", zhHant: "全部" },
                                { id: "repaired", en: "Repaired", zh: "已修复", zhHant: "已修復" },
                                { id: "repair_draft", en: "Repair drafts", zh: "修复草稿", zhHant: "修復草稿" },
                                { id: "failed", en: "Failed", zh: "失败", zhHant: "失敗" },
                                { id: "yaml_restore", en: "YAML restore", zh: "YAML 恢复", zhHant: "YAML 還原" },
                                { id: "maintenance_apply", en: "Maintenance", zh: "维护应用", zhHant: "維護套用" },
                                { id: "mark_needs_review", en: "Needs review", zh: "标记待审", zhHant: "標記待審" },
                                { id: "optimized", en: "Optimized", zh: "已优化", zhHant: "已優化" },
                                { id: "discovered", en: "Discovered", zh: "已发现", zhHant: "已發現" },
                                { id: "other", en: "Other", zh: "其他", zhHant: "其他" },
                            ] as const).map((opt) => {
                                const active = evolutionAuditFilter === opt.id;
                                return (
                                    <button
                                        key={opt.id}
                                        type="button"
                                        className={active ? "btn-primary" : "btn-secondary"}
                                        style={{ fontSize: "0.66rem", padding: "2px 8px", borderRadius: 999 }}
                                        onClick={() => setEvolutionAuditFilter(opt.id)}
                                    >
                                        {localizeText(opt.en, opt.zh, opt.zhHant)}
                                    </button>
                                );
                            })}
                            <input
                                type="search"
                                className="form-input"
                                value={evolutionAuditSkillQuery}
                                onChange={(e) => setEvolutionAuditSkillQuery(e.target.value)}
                                placeholder={localizeText("Search skill / text…", "搜索技能 / 文本…", "搜尋技能 / 文本…")}
                                style={{
                                    fontSize: "0.7rem",
                                    padding: "3px 8px",
                                    minWidth: 160,
                                    flex: "1 1 160px",
                                    maxWidth: 280,
                                }}
                                aria-label={localizeText("Search audit", "搜索审计", "搜尋審計")}
                            />
                            {evolutionAuditSkillQuery && (
                                <button
                                    type="button"
                                    className="btn-secondary"
                                    style={{ fontSize: "0.66rem", padding: "2px 8px" }}
                                    onClick={() => setEvolutionAuditSkillQuery("")}
                                >
                                    {localizeText("Clear search", "清除搜索", "清除搜尋")}
                                </button>
                            )}
                        </div>
                        <div style={{ display: "flex", flexWrap: "wrap", gap: 4, marginBottom: 8, alignItems: "center" }}>
                            <span style={{ fontSize: "0.68rem", color: colors.textMuted, marginRight: 2 }}>
                                {localizeText("Time", "时间", "時間")}
                            </span>
                            {([
                                { id: "all" as const, en: "All time", zh: "全部时间", zhHant: "全部時間" },
                                { id: "1h" as const, en: "1h", zh: "1 小时", zhHant: "1 小時" },
                                { id: "24h" as const, en: "24h", zh: "24 小时", zhHant: "24 小時" },
                                { id: "7d" as const, en: "7d", zh: "7 天", zhHant: "7 天" },
                                { id: "30d" as const, en: "30d", zh: "30 天", zhHant: "30 天" },
                            ]).map((opt) => {
                                const active = evolutionAuditTimeRange === opt.id;
                                return (
                                    <button
                                        key={opt.id}
                                        type="button"
                                        className={active ? "btn-primary" : "btn-secondary"}
                                        style={{ fontSize: "0.66rem", padding: "2px 8px", borderRadius: 999 }}
                                        onClick={() => setEvolutionAuditTimeRange(opt.id)}
                                    >
                                        {localizeText(opt.en, opt.zh, opt.zhHant)}
                                    </button>
                                );
                            })}
                        </div>
                        {evolutionAuditLoading && evolutionAudit.length === 0 ? (
                            <div style={{ fontSize: "0.74rem", color: colors.textMuted }}>
                                {localizeText("Loading...", "加载中...", "載入中...")}
                            </div>
                        ) : evolutionAudit.length === 0 ? (
                            <div style={{ fontSize: "0.74rem", color: colors.textMuted }}>
                                {localizeText("No audit entries yet.", "暂无审计记录。", "暫無審計記錄。")}
                            </div>
                        ) : filteredEvolutionAudit.length === 0 ? (
                            <div style={{ fontSize: "0.74rem", color: colors.textMuted }}>
                                {localizeText("No entries for this filter.", "当前筛选无匹配记录。", "目前篩選無符合記錄。")}
                            </div>
                        ) : (
                            <div style={{ display: "flex", flexDirection: "column", gap: "4px", maxHeight: 320, overflowY: "auto" }}>
                                {visibleEvolutionAudit.map((row, i) => {
                                    const kind = String(row.kind || "other");
                                    const repairDraftRejected = kind === "repair_draft" && String(row.status || "") === "rejected";
                                    const kindLabel = (() => {
                                        switch (kind) {
                                            case "repaired": return localizeText("repaired", "已修复", "已修復");
                                            case "optimized": return localizeText("optimized", "已优化", "已優化");
                                            case "discovered": return localizeText("discovered", "已发现", "已發現");
                                            case "failed": return localizeText("failed", "执行失败", "執行失敗");
                                            case "queue_full": return localizeText("queue full", "队列满", "佇列滿");
                                            case "yaml_restore": return localizeText("yaml restore", "YAML 恢复", "YAML 還原");
                                            case "maintenance_apply": return localizeText("maintenance apply", "维护应用", "維護套用");
                                            case "mark_needs_review": return localizeText("needs review", "标记待审", "標記待審");
                                            case "repair_draft": return repairDraftRejected
                                                ? localizeText("repair draft rejected", "修复草稿已拒绝", "修復草稿已拒絕")
                                                : localizeText("repair draft", "修复草稿待审", "修復草稿待審");
                                            default: return kind;
                                        }
                                    })();
                                    const tone = kind === "failed" || kind === "queue_full" || kind === "mark_needs_review"
                                        ? colors.danger
                                        : repairDraftRejected
                                            ? colors.textSecondary
                                            : kind === "repair_draft"
                                                ? colors.primary
                                                : kind === "optimized" || kind === "yaml_restore"
                                                    ? colors.textSecondary
                                                    : colors.success;
                                    let when = row.timestamp || "";
                                    try {
                                        if (row.timestamp) when = new Date(row.timestamp).toLocaleString();
                                    } catch { /* keep raw */ }
                                    const skillName = String(row.skill || "").trim();
                                    const canRollback = !!skillName && (kind === "yaml_restore" || kind === "maintenance_apply" || kind === "repaired");
                                    const focused = skillNamesEqual(skillName, evolutionFocusSkill);
                                    return (
                                        <div
                                            key={`audit-${row.timestamp || i}-${row.skill || ""}-${kind}`}
                                            data-evolution-skill={skillName}
                                            style={{
                                                fontSize: "0.72rem",
                                                padding: "4px 6px",
                                                borderLeft: `3px solid ${focused ? colors.primary : tone}`,
                                                background: focused ? "rgba(59, 130, 246, 0.12)" : undefined,
                                                boxShadow: focused ? `inset 0 0 0 1px ${colors.primary}` : undefined,
                                                borderRadius: 4,
                                            }}
                                        >
                                            <div style={{ display: "flex", gap: 6, alignItems: "center", flexWrap: "wrap" }}>
                                                <span style={{ color: colors.textMuted }}>{when}</span>
                                                <strong style={{ color: focused ? colors.primary : tone }}>{kindLabel}</strong>
                                                {skillName ? (
                                                    <span
                                                        style={{ ...skillNameLinkStyle, fontWeight: 600 }}
                                                        title={localizeText(
                                                            "Open skill details and highlight related repair draft",
                                                            "打开技能详情并高亮关联修复草案",
                                                            "開啟技能詳情並高亮關聯修復草案",
                                                        )}
                                                        onClick={() => {
                                                            focusEvolutionSkill(skillName, "audit");
                                                            openSkillFromAudit(skillName);
                                                        }}
                                                    >
                                                        {skillName}
                                                    </span>
                                                ) : (
                                                    <span style={{ fontWeight: 500 }}>—</span>
                                                )}
                                                {row.source ? (
                                                    <span style={{ color: colors.textMuted }}>[{row.source}]</span>
                                                ) : null}
                                                {canRollback && (
                                                    <button
                                                        type="button"
                                                        className="btn-secondary"
                                                        style={{ fontSize: "0.68rem", padding: "1px 6px", marginLeft: "auto" }}
                                                        disabled={busy}
                                                        onClick={() => { void handleRollbackYAML(skillName); }}
                                                    >
                                                        {localizeText("Rollback", "回滚", "回滾")}
                                                    </button>
                                                )}
                                            </div>
                                            {row.explanation ? (
                                                <div style={{ color: colors.textSecondary, marginTop: 2, whiteSpace: "pre-wrap" }}>
                                                    {row.explanation.length > 180 ? `${row.explanation.slice(0, 180)}…` : row.explanation}
                                                </div>
                                            ) : null}
                                        </div>
                                    );
                                })}
                                {visibleEvolutionAudit.length < filteredEvolutionAudit.length && (
                                    <div style={{ display: "flex", alignItems: "center", gap: 8, marginTop: 4 }}>
                                        <button
                                            type="button"
                                            className="btn-secondary"
                                            style={{ fontSize: "0.7rem", padding: "3px 10px" }}
                                            onClick={() => setAuditVisibleCount((n) => n + 30)}
                                        >
                                            {localizeText(
                                                `Show more (${filteredEvolutionAudit.length - visibleEvolutionAudit.length} left)`,
                                                `加载更多（剩余 ${filteredEvolutionAudit.length - visibleEvolutionAudit.length}）`,
                                                `載入更多（剩餘 ${filteredEvolutionAudit.length - visibleEvolutionAudit.length}）`,
                                            )}
                                        </button>
                                        <button
                                            type="button"
                                            className="btn-secondary"
                                            style={{ fontSize: "0.7rem", padding: "3px 10px" }}
                                            onClick={() => setAuditVisibleCount(filteredEvolutionAudit.length)}
                                        >
                                            {localizeText("Show all", "显示全部", "顯示全部")}
                                        </button>
                                    </div>
                                )}
                            </div>
                        )}
                    </div>

                    {/* Live batch progress for sequential repair/optimize */}
                    {batchProgress && (
                        <div style={{ ...remoteInfoPanelStyle, marginBottom: "12px", padding: "10px 12px", borderColor: batchProgress.cancelled ? colors.warning : colors.primary }}>
                            <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 6, flexWrap: "wrap" }}>
                                <div style={{ fontSize: "0.8rem", fontWeight: 600 }}>
                                    {batchProgress.kind === "repair"
                                        ? localizeText("Batch repair progress", "批量修复进度", "批量修復進度")
                                        : localizeText("Batch optimize progress", "批量优化进度", "批量優化進度")}
                                    {batchProgress.cancelled
                                        ? ` · ${localizeText("cancelled", "已取消", "已取消")}`
                                        : batchProgress.done
                                            ? ` · ${localizeText("done", "已完成", "已完成")}`
                                            : ""}
                                </div>
                                <span style={{ fontSize: "0.72rem", color: colors.textMuted }}>
                                    {batchProgress.current}/{batchProgress.total}
                                </span>
                                {!batchProgress.done && !batchProgress.cancelled && (
                                    <button
                                        type="button"
                                        className="btn-secondary"
                                        style={{ fontSize: "0.7rem", padding: "2px 8px", marginLeft: "auto" }}
                                        onClick={() => {
                                            batchCancelRef.current = true;
                                            setBatchProgress((prev) => prev ? { ...prev, cancelled: true } : prev);
                                        }}
                                    >
                                        {localizeText("Cancel batch", "取消批量", "取消批量")}
                                    </button>
                                )}
                            </div>
                            <div style={{
                                height: 8,
                                borderRadius: 999,
                                background: colors.border,
                                overflow: "hidden",
                                marginBottom: 6,
                            }}>
                                <div style={{
                                    height: "100%",
                                    width: `${batchProgress.total > 0 ? Math.round((batchProgress.current / batchProgress.total) * 100) : 0}%`,
                                    background: batchProgress.cancelled ? (colors.warning || colors.primary) : colors.primary,
                                    transition: "width 0.2s ease",
                                }} />
                            </div>
                            <div style={{ fontSize: "0.72rem", color: colors.textSecondary }}>
                                {batchProgress.done
                                    ? (batchProgress.cancelled
                                        ? localizeText("Stopped after cancel request.", "已按取消请求停止。", "已依取消請求停止。")
                                        : localizeText("All items processed.", "全部项已处理。", "全部項目已處理。"))
                                    : batchProgress.currentName
                                        ? localizeText(
                                            `Working on: ${batchProgress.currentName}`,
                                            `正在处理：${batchProgress.currentName}`,
                                            `正在處理：${batchProgress.currentName}`,
                                        )
                                        : localizeText("Starting…", "开始中…", "開始中…")}
                            </div>
                            <div style={{ fontSize: "0.7rem", color: colors.textMuted, marginTop: 4 }}>
                                {localizeText(
                                    `ok ${batchProgress.succeeded.length} · failed ${batchProgress.failed.length}`,
                                    `成功 ${batchProgress.succeeded.length} · 失败 ${batchProgress.failed.length}`,
                                    `成功 ${batchProgress.succeeded.length} · 失敗 ${batchProgress.failed.length}`,
                                )}
                                {batchProgress.failed.length > 0 && batchProgress.failed[batchProgress.failed.length - 1]
                                    ? ` · ${batchProgress.failed[batchProgress.failed.length - 1].name}`
                                    : ""}
                            </div>
                        </div>
                    )}

                    {/* Attention queues: skills that look repairable / optimizable */}
                    <div style={{ ...remoteInfoPanelStyle, marginBottom: "12px", padding: "10px 12px" }}>
                        <div style={{ fontSize: "0.8rem", fontWeight: 600, marginBottom: "6px" }}>
                            {localizeText("Attention", "待处理", "待處理")}
                        </div>
                        <div style={{ fontSize: "0.74rem", color: colors.textSecondary, marginBottom: "8px" }}>
                            {localizeText(
                                "Skills with recent errors (repair) or middling success rates (optimize). Actions use force mode.",
                                "有最近错误的技能可修复；成功率中等的技能可优化。操作使用强制模式。",
                                "有最近錯誤的技能可修復；成功率中等的技能可優化。操作使用強制模式。",
                            )}
                        </div>

                        <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: "4px", flexWrap: "wrap" }}>
                            <div style={{ fontSize: "0.76rem", fontWeight: 600 }}>
                                {localizeText("Repair candidates", "修复候选", "修復候選")}
                                {" "}({repairCandidates.length})
                            </div>
                            {repairCandidates.length > 0 && (
                                <button
                                    type="button"
                                    className="btn-secondary"
                                    style={{ fontSize: "0.7rem", padding: "2px 8px", marginLeft: "auto" }}
                                    disabled={busy || detailActionBusy !== null}
                                    onClick={() => { void handleBatchRepairCandidates(); }}
                                >
                                    {detailActionBusy === "repair"
                                        ? localizeText("Repairing…", "修复中…", "修復中…")
                                        : localizeText(
                                            `Repair all (${repairCandidates.length})`,
                                            `全部立即修复 (${repairCandidates.length})`,
                                            `全部立即修復 (${repairCandidates.length})`,
                                        )}
                                </button>
                            )}
                        </div>
                        {repairCandidates.length === 0 ? (
                            <div style={{ fontSize: "0.74rem", color: colors.textMuted, marginBottom: "10px" }}>
                                {localizeText("No skills with last_error right now.", "当前没有记录 last_error 的技能。", "目前沒有記錄 last_error 的技能。")}
                            </div>
                        ) : (
                            <div style={{ display: "flex", flexDirection: "column", gap: "6px", marginBottom: "12px" }}>
                                {repairCandidates.map((s) => (
                                    <div
                                        key={`repair-${s.name}`}
                                        style={{
                                            display: "flex",
                                            gap: "8px",
                                            alignItems: "center",
                                            flexWrap: "wrap",
                                            padding: "6px 8px",
                                            borderRadius: 6,
                                            border: `1px solid ${colors.border}`,
                                        }}
                                    >
                                        <span
                                            style={{ ...skillNameLinkStyle, flex: "1 1 120px", minWidth: 0 }}
                                            onClick={() => setDetailSkill(s)}
                                            title={s.last_error || s.name}
                                        >
                                            {s.name}
                                        </span>
                                        <span style={{ fontSize: "0.7rem", color: colors.danger, flex: "2 1 160px", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={s.last_error}>
                                            {s.last_error}
                                        </span>
                                        <button
                                            className="btn-secondary"
                                            style={{ fontSize: "0.72rem", padding: "2px 8px", flexShrink: 0 }}
                                            disabled={busy || detailActionBusy !== null}
                                            onClick={() => { void handleTriggerSelfRepair(s); }}
                                        >
                                            {detailActionBusy === "repair"
                                                ? localizeText("Repairing…", "修复中…", "修復中…")
                                                : localizeText("Repair now", "立即修复", "立即修復")}
                                        </button>
                                    </div>
                                ))}
                            </div>
                        )}

                        <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: "4px", flexWrap: "wrap" }}>
                            <div style={{ fontSize: "0.76rem", fontWeight: 600 }}>
                                {localizeText("Optimize candidates", "优化候选", "優化候選")}
                                {" "}({optimizeCandidates.length})
                            </div>
                            {optimizeCandidates.length > 0 && (
                                <button
                                    type="button"
                                    className="btn-secondary"
                                    style={{ fontSize: "0.7rem", padding: "2px 8px", marginLeft: "auto" }}
                                    disabled={busy || detailActionBusy !== null}
                                    onClick={() => { void handleBatchOptimizeCandidates(); }}
                                >
                                    {detailActionBusy === "optimize"
                                        ? localizeText("Optimizing…", "优化中…", "優化中…")
                                        : localizeText(
                                            `Optimize all (${optimizeCandidates.length})`,
                                            `全部立即优化 (${optimizeCandidates.length})`,
                                            `全部立即優化 (${optimizeCandidates.length})`,
                                        )}
                                </button>
                            )}
                        </div>
                        {optimizeCandidates.length === 0 ? (
                            <div style={{ fontSize: "0.74rem", color: colors.textMuted }}>
                                {localizeText("No skills in the 50–85% success band with enough runs.", "没有同时满足使用次数与 50–85% 成功率的技能。", "沒有同時滿足使用次數與 50–85% 成功率的技能。")}
                            </div>
                        ) : (
                            <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
                                {optimizeCandidates.map((s) => (
                                    <div
                                        key={`opt-${s.name}`}
                                        style={{
                                            display: "flex",
                                            gap: "8px",
                                            alignItems: "center",
                                            flexWrap: "wrap",
                                            padding: "6px 8px",
                                            borderRadius: 6,
                                            border: `1px solid ${colors.border}`,
                                        }}
                                    >
                                        <span
                                            style={{ ...skillNameLinkStyle, flex: "1 1 120px", minWidth: 0 }}
                                            onClick={() => setDetailSkill(s)}
                                        >
                                            {s.name}
                                        </span>
                                        <span style={{ fontSize: "0.72rem", color: colors.textSecondary, flexShrink: 0 }}>
                                            {(s.usage_count ?? 0)}{localizeText("x", "次", "次")} / {Math.round((s.success_rate ?? 0) * 100)}%
                                        </span>
                                        <button
                                            className="btn-secondary"
                                            style={{ fontSize: "0.72rem", padding: "2px 8px", flexShrink: 0 }}
                                            disabled={busy || detailActionBusy !== null}
                                            onClick={() => { void handleTriggerOptimize(s); }}
                                        >
                                            {detailActionBusy === "optimize"
                                                ? localizeText("Optimizing…", "优化中…", "優化中…")
                                                : localizeText("Optimize now", "立即优化", "立即優化")}
                                        </button>
                                    </div>
                                ))}
                            </div>
                        )}
                    </div>
                </>
            )}

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
                            {extDirsLoading ? localizeText("Refreshing...", "刷新中...", "重新整理中...") : localizeText("Refresh", "刷新", "重新整理")}
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
                                                    <span style={{ color: colors.danger, fontSize: "0.72rem", display: "inline-flex", alignItems: "center", gap: 4 }}>
                                                        <StatusGlyph kind="error" size={12} />
                                                        {d.error}
                                                    </span>
                                                ) : (
                                                    <span style={{ color: colors.success, fontSize: "0.72rem", display: "inline-flex", alignItems: "center", gap: 4 }}>
                                                        <StatusGlyph kind="ok" size={12} />
                                                        {localizeText("OK", "正常", "正常")}
                                                    </span>
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
                            <button className="btn-close" onClick={() => setDetailSkill(null)}>X</button>
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
                            {/* Parameters */}
                            {((detailSkill.params && detailSkill.params.length > 0) || (detailSkill.required_args && detailSkill.required_args.length > 0)) && (
                                <div>
                                    <strong>{localizeText("Parameters", "参数", "參數")}</strong>
                                    <div style={{ marginTop: "4px", fontSize: "0.76rem" }}>
                                        {detailSkill.params && detailSkill.params.length > 0 ? (
                                            <div style={{ display: "flex", flexDirection: "column", gap: "3px" }}>
                                                {detailSkill.params.map((p, i) => (
                                                    <div key={i} style={{ display: "flex", gap: "6px", alignItems: "baseline" }}>
                                                        <code style={{ fontSize: "0.74rem", color: colors.primary }}>{p.name}</code>
                                                        {p.required && <span style={{ fontSize: "0.66rem", color: colors.danger }}>*</span>}
                                                        {p.description && <span style={{ color: colors.textSecondary }}>{p.description}</span>}
                                                    </div>
                                                ))}
                                            </div>
                                        ) : (
                                            <div style={{ display: "flex", flexWrap: "wrap", gap: "4px" }}>
                                                {(detailSkill.required_args || []).map((arg, i) => (
                                                    <span key={i} style={{ ...tagStyle, borderColor: colors.primary, color: colors.primary }}>{arg} *</span>
                                                ))}
                                            </div>
                                        )}
                                    </div>
                                </div>
                            )}
                            <div>
                                <strong>{localizeText("Steps", "操作步骤", "操作步驟")}</strong>
                                <pre style={detailPreStyle}>{formatStepText(detailSkill.steps || [])}</pre>
                            </div>
                            <div style={detailGridStyle}>
                                <div><strong>{localizeText("Source Project", "来源项目", "來源項目")}</strong><div>{detailSkill.source_project || "—"}</div></div>
                                <div><strong>{localizeText("Hub Skill ID", "市场技能ID", "市場技能ID")}</strong><div>{detailSkill.hub_skill_id || "—"}</div></div>
                                <div><strong>{localizeText("Last Used", "最近使用", "最近使用")}</strong><div>{detailSkill.last_used_at ? new Date(detailSkill.last_used_at).toLocaleString() : "—"}</div></div>
                                <div><strong>{localizeText("Last Error", "最近错误", "最近錯誤")}</strong><div>{detailSkill.last_error || "—"}</div></div>
                                <div><strong>{localizeText("Repair attempts", "修复次数", "修復次數")}</strong><div>{detailSkill.repair_attempt_count ?? 0}</div></div>
                                <div><strong>{localizeText("Last repair", "最近修复", "最近修復")}</strong><div>{detailSkill.last_repair_at ? new Date(detailSkill.last_repair_at).toLocaleString() : "—"}</div></div>
                                <div><strong>{localizeText("Optimizations", "优化次数", "優化次數")}</strong><div>{detailSkill.optimization_count ?? 0}</div></div>
                                <div><strong>{localizeText("Last optimized", "最近优化", "最近優化")}</strong><div>{detailSkill.last_optimized_at ? new Date(detailSkill.last_optimized_at).toLocaleString() : "—"}</div></div>
                                {detailSkill.status === "staged" && (
                                    <>
                                        <div><strong>{localizeText("Verification", "验证状态", "驗證狀態")}</strong><div>{detailSkill.verification_gate_status || localizeText("Pending", "待验证", "待驗證")}</div></div>
                                        <div><strong>{localizeText("Verification run", "验证运行 ID", "驗證執行 ID")}</strong><div style={{ overflowWrap: "anywhere" }}>{detailSkill.verification_run_id || "—"}</div></div>
                                    </>
                                )}
                            </div>
                            {(detailSkill.repair_history && detailSkill.repair_history.length > 0) && (
                                <div>
                                    <strong>{localizeText("Repair history", "修复历史", "修復歷史")}</strong>
                                    <div style={{ marginTop: "6px", display: "flex", flexDirection: "column", gap: "6px" }}>
                                        {detailSkill.repair_history.map((rec, i) => (
                                            <div
                                                key={`${rec.timestamp || "r"}-${i}`}
                                                style={{
                                                    ...remoteInfoPanelStyle,
                                                    padding: "8px 10px",
                                                    fontSize: "0.74rem",
                                                    borderLeft: rec.success
                                                        ? "3px solid var(--theme-success, #4f7f6f)"
                                                        : "3px solid var(--theme-warning, #64748b)",
                                                }}
                                            >
                                                <div style={{ display: "flex", gap: "8px", flexWrap: "wrap", marginBottom: "4px" }}>
                                                    <span>{rec.timestamp ? new Date(rec.timestamp).toLocaleString() : "—"}</span>
                                                    {rec.error_class && (
                                                        <code style={{ color: colors.primary }}>{rec.error_class}</code>
                                                    )}
                                                    <span style={{ marginLeft: "auto", color: colors.textSecondary }}>
                                                        {rec.success
                                                            ? localizeText("verified", "已验证", "已驗證")
                                                            : localizeText("attempted", "已尝试", "已嘗試")}
                                                    </span>
                                                </div>
                                                <div style={{ color: colors.textSecondary, whiteSpace: "pre-wrap" }}>
                                                    {rec.explanation || "—"}
                                                </div>
                                            </div>
                                        ))}
                                    </div>
                                </div>
                            )}
                        </div>
                        {detailSkill.status === "needs_review" && (
                            <div style={{ ...remoteInfoPanelStyle, fontSize: "0.78rem" }}>
                                <strong>{localizeText("Review reason", "\u5ba1\u6838\u539f\u56e0", "\u5be9\u6838\u539f\u56e0")}</strong>
                                <div style={{ marginTop: 4, whiteSpace: "pre-wrap" }}>{skillReviewReason(detailSkill, localizeText)}</div>
                            </div>
                        )}
                        <div className="modal-footer">
                            {detailSkill.status === "staged" && (
                                <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 14px" }} onClick={() => { void handleVerifyAndActivate(detailSkill); }} disabled={busy}>
                                    {localizeText("Verify and activate", "验证并激活", "驗證並啟用")}
                                </button>
                            )}
                            {detailSkill.status === "needs_setup" && (
                                <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 14px" }} onClick={() => { openEditForm(detailSkill, true); setDetailSkill(null); }}>
                                    {localizeText("Configure and enable", "配置并启用", "設定並啟用")}
                                </button>
                            )}
                            {detailSkill.status === "needs_review" && (
                                <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 14px" }} onClick={() => handleApproveSkillReview(detailSkill)}>
                                    {localizeText("Approve and Enable", "\u5ba1\u6838\u901a\u8fc7\u5e76\u542f\u7528", "\u5be9\u6838\u901a\u904e\u4e26\u555f\u7528")}
                                </button>
                            )}
                            {!!detailSkill.last_error && !isAgentGuidedWorkflow(detailSkill) && (
                                <button
                                    className="btn-secondary"
                                    style={{ fontSize: "0.78rem", padding: "4px 14px" }}
                                    onClick={() => { void handleTriggerSelfRepair(detailSkill); }}
                                    disabled={busy || detailActionBusy !== null}
                                    title={localizeText(
                                        "Run LLM self-repair now (force, skips usage-rate threshold)",
                                        "立即用 LLM 尝试自修复（强制，跳过使用率门槛）",
                                        "立即用 LLM 嘗試自修復（強制，跳過使用率門檻）",
                                    )}
                                >
                                    {detailActionBusy === "repair"
                                        ? localizeText("Repairing…", "修复中…", "修復中…")
                                        : localizeText("Repair now", "立即修复", "立即修復")}
                                </button>
                            )}
                            {(detailSkill.status === "active" || detailSkill.status === "needs_review") && !isAgentGuidedWorkflow(detailSkill) && (
                                <button
                                    className="btn-secondary"
                                    style={{ fontSize: "0.78rem", padding: "4px 14px" }}
                                    onClick={() => { void handleTriggerOptimize(detailSkill); }}
                                    disabled={busy || detailActionBusy !== null}
                                    title={localizeText(
                                        "Run LLM optimization now (force, skips auto thresholds and 24h throttle)",
                                        "立即用 LLM 尝试优化（强制，跳过自动门槛与 24h 节流）",
                                        "立即用 LLM 嘗試優化（強制，跳過自動門檻與 24h 節流）",
                                    )}
                                >
                                    {detailActionBusy === "optimize"
                                        ? localizeText("Optimizing…", "优化中…", "優化中…")
                                        : localizeText("Optimize now", "立即优化", "立即優化")}
                                </button>
                            )}
                            {isAgentGuidedWorkflow(detailSkill) ? (
                                <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 14px" }} onClick={() => { handleStartAgentGuidedWorkflow(detailSkill.name); setDetailSkill(null); }} disabled={detailSkill.status !== "active"}>
                                    {localizeText("Start with AI Agent", "用 AI 助手启动", "用 AI 助手啟動")}
                                </button>
                            ) : (
                                <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 14px" }} onClick={() => { handleRunSkill(detailSkill.name); setDetailSkill(null); }} disabled={detailSkill.status !== "active"}>
                                    {localizeText("Run", "运行", "執行")}
                                </button>
                            )}
                            <button className="btn-secondary" onClick={() => { openEditForm(detailSkill); setDetailSkill(null); }}>
                                {localizeText("Edit", "编辑", "編輯")}
                            </button>
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
                            <h3 style={{ fontSize: "0.88rem", margin: 0 }}>{isConfiguringSkill ? localizeText("Configure Skill", "配置 Skill", "設定 Skill") : editingSkill ? localizeText("Edit Skill", "编辑 Skill", "編輯 Skill") : localizeText("New Skill", "新建 Skill", "新建 Skill")}</h3>
                            <button className="btn-close" onClick={closeForm}>X</button>
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
                                            {t} DEL
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
                                {busy ? localizeText("Submitting...", "提交中...", "提交中...") : isConfiguringSkill ? localizeText("Save and enable", "保存并启用", "儲存並啟用") : editingSkill ? localizeText("Save", "保存", "儲存") : localizeText("Create", "创建", "建立")}
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

const localSkillsDescriptionPreviewStyle: CSSProperties = {
    ...descCellStyle,
    fontSize: "0.74rem",
    color: colors.textSecondary,
};

const localSkillsDescriptionColStyle: CSSProperties = { width: LOCAL_SKILLS_DESCRIPTION_COL_PX, maxWidth: LOCAL_SKILLS_DESCRIPTION_COL_PX, overflow: "hidden" };
const localSkillsClipCellStyle: CSSProperties = { overflow: "hidden" };
const localSkillsTypeBadgeStyle: CSSProperties = { ...executionClassBadgeStyle, maxWidth: "100%", overflow: "hidden", textOverflow: "ellipsis" };

const localSkillsNameCellStyle: CSSProperties = {
    display: "flex",
    alignItems: "center",
    gap: "4px",
    minWidth: 0,
};

const localSkillsNameLinkStyle: CSSProperties = {
    cursor: "pointer",
    color: colors.primary,
    display: "block",
    minWidth: 0,
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
};

const localSkillsMetaTextStyle: CSSProperties = {
    display: "block",
    maxWidth: "100%",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
    fontSize: "0.72rem",
    color: colors.textSecondary,
    lineHeight: 1.4,
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
    boxSizing: "border-box",
    flex: "1 1 auto",
    minHeight: 0,
    width: "100%",
    overflowX: "hidden",
    overflowY: "auto",
};
const localSkillsTableStyle: CSSProperties = {
    width: "100%",
    tableLayout: "fixed",
    borderCollapse: "collapse",
    fontSize: "0.76rem",
};

const localSkillsRowActionsStyle: CSSProperties = {
    display: "flex",
    justifyContent: "center",
    alignItems: "center",
    gap: "6px",
    flexWrap: "wrap",
    width: "100%",
    minWidth: 0,
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

const chipStyle: CSSProperties = {
    background: colors.surface,
    border: `1px solid ${colors.border}`,
    borderRadius: "12px",
    padding: "3px 10px",
    fontSize: "0.72rem",
    color: colors.textSecondary,
    cursor: "pointer",
    fontWeight: 500,
    transition: "all 0.15s",
};

const chipActiveStyle: CSSProperties = {
    background: colors.infoBg,
    border: `1px solid ${colors.primary}`,
    color: colors.primary,
    fontWeight: 600,
};

const skillCardStyle: CSSProperties = {
    ...remoteCardStyle,
    padding: "10px 12px",
};

const appBadgeStyle: CSSProperties = {
    fontSize: "0.66rem",
    padding: "1px 6px",
    borderRadius: "8px",
    background: colors.infoBg,
    color: colors.primaryDark,
    fontWeight: 500,
};

const learnedBadgeStyle: CSSProperties = {
    fontSize: "0.72rem",
};

const runBtnStyle: CSSProperties = {
    width: "28px",
    height: "28px",
    padding: 0,
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    fontSize: "0.72rem",
    lineHeight: 1,
    borderRadius: "50%",
};

const skillNameLinkStyle: CSSProperties = {
    fontWeight: 600,
    fontSize: "0.8rem",
    color: colors.primary,
    cursor: "pointer",
};
