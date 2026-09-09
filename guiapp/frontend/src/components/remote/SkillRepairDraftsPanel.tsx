import { type CSSProperties, useCallback, useEffect, useState } from 'react';
import { useDialog } from '../CustomDialog';
import { useToast } from '../Toast';
import { EventsOn } from '../../../wailsjs/runtime';
import { EVENT_SKILL_REPAIRED, EVENT_SKILL_REPAIR_DRAFT_READY } from '../../constants/events';
import { ApplySkillRepairDraft, ListSkillRepairDrafts, RejectSkillRepairDraft } from '../../../wailsjs/go/main/App';
import { colors, remoteInfoPanelStyle } from './styles';

/** One full step entry in old_steps/new_steps; params is preserved verbatim.
 *  name/label/when/condition only appear when non-empty (omitempty).
 *  capture is a map[string]string on the Go side (JSON object, not a string). */
interface SkillRepairDraftStep {
    action?: string;
    params?: unknown;
    on_error?: string;
    name?: string;
    label?: string;
    when?: string;
    capture?: Record<string, string>;
    condition?: string;
}

/** Pending human-reviewed repair draft for a file-backed skill
 *  (<skill_dir>/.evolution-drafts/*.json), as listed by ListSkillRepairDrafts.
 *  Drafts whose stored JSON cannot be parsed come back with unreadable:true
 *  and no steps; they can only be rejected, never applied. */
interface SkillRepairDraftItem {
    skill?: string;
    draft?: string;
    explanation?: string;
    last_error?: string;
    created_at?: string;
    old_steps?: SkillRepairDraftStep[];
    new_steps?: SkillRepairDraftStep[];
    unreadable?: boolean;
    /** True when the draft proposes disabling the skill entirely — no
     *  old_steps/new_steps; explanation carries the disable reason. */
    disable?: boolean;
}

type Props = {
    localizeText: (en: string, zhHans: string, zhHant: string) => string;
    busy: boolean;
    setBusy: (busy: boolean) => void;
    /** Shared focus skill for repair-draft ↔ audit bidirectional highlight */
    evolutionFocusSkill: string | null;
    /** Focus a skill's related audit rows (parent scrolls the audit panel) */
    onFocusSkill: (skill: string) => void;
    /** Called after a draft is applied/rejected so the parent can refresh audit + skills */
    onDraftsChanged: () => void;
};

function skillNamesEqual(a: string | undefined | null, b: string | undefined | null): boolean {
    return String(a || "").trim().toLowerCase() === String(b || "").trim().toLowerCase();
}

/**
 * "Pending repair drafts" section of the Skills ▸ Evolution tab.
 *
 * File-backed skills never get auto-applied repairs: the evolution pipeline
 * writes a draft under <skill_dir>/.evolution-drafts/ and emits
 * skill:repair_draft_ready. This panel lists those drafts (5s poll + event
 * refresh) and lets the user apply (config + skill.yaml write-back) or
 * reject (delete) each one. Extracted from SkillsManagementPanelView to keep
 * that file under the main-UI guard line budget.
 */
export function SkillRepairDraftsPanel({
    localizeText,
    busy,
    setBusy,
    evolutionFocusSkill,
    onFocusSkill,
    onDraftsChanged,
}: Props) {
    const { showToast } = useToast();
    const { showConfirm } = useDialog();
    const [repairDrafts, setRepairDrafts] = useState<SkillRepairDraftItem[]>([]);
    const [repairDraftsLoading, setRepairDraftsLoading] = useState(false);
    const [expandedRepairDraftKey, setExpandedRepairDraftKey] = useState<string | null>(null);

    // ListSkillRepairDrafts returns a JSON string: { ok, count, drafts: [...] }.
    const loadRepairDrafts = useCallback(async () => {
        setRepairDraftsLoading(true);
        try {
            const raw = await ListSkillRepairDrafts();
            let parsed: unknown = null;
            try {
                parsed = typeof raw === "string" ? JSON.parse(raw) : raw;
            } catch {
                parsed = null;
            }
            const list = Array.isArray(parsed)
                ? parsed
                : (Array.isArray((parsed as { drafts?: unknown[] } | null)?.drafts)
                    ? (parsed as { drafts: unknown[] }).drafts
                    : []);
            setRepairDrafts(list as SkillRepairDraftItem[]);
        } catch (err) {
            // Keep the last good list on failure instead of flashing an empty one.
            console.warn("loadRepairDrafts failed", err);
        } finally {
            setRepairDraftsLoading(false);
        }
    }, []);

    // Poll alongside the Evolution tab's pipeline-status refresh (same 5s cadence).
    // The panel is only mounted while the Evolution tab is active.
    useEffect(() => {
        void loadRepairDrafts();
        const id = window.setInterval(() => { void loadRepairDrafts(); }, 5000);
        return () => window.clearInterval(id);
    }, [loadRepairDrafts]);

    // Refresh on repair-draft / repair-applied events; toast on a brand-new draft.
    useEffect(() => {
        const onRepairDraftReady = (payload?: unknown) => {
            void loadRepairDrafts();
            const data = (payload && typeof payload === "object" ? payload : {}) as Record<string, unknown>;
            // status:"rejected" is emitted after a draft is rejected — silent refresh only.
            if (typeof data.status === "string" && data.status.trim() !== "") {
                return;
            }
            const skillName = typeof data.skill === "string" && data.skill.trim() !== ""
                ? data.skill
                : localizeText("a skill", "某技能", "某技能");
            showToast(
                localizeText(
                    `Skill “${skillName}” has a new repair draft pending review`,
                    `技能「${skillName}」有新的待评审修复`,
                    `技能「${skillName}」有新的待評審修復`,
                ),
                "info",
                5000,
            );
        };
        const onRepaired = () => {
            void loadRepairDrafts();
        };
        const unsubs = [
            EventsOn(EVENT_SKILL_REPAIR_DRAFT_READY, onRepairDraftReady),
            EventsOn(EVENT_SKILL_REPAIRED, onRepaired),
        ];
        // Unsubscribe via the EventsOn callbacks only — EventsOff(name) would
        // also drop the parent panel's listeners for the same event names.
        return () => {
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
    }, [loadRepairDrafts, localizeText, showToast]);

    // ApplySkillRepairDraft / RejectSkillRepairDraft return JSON strings with { ok, error?, warning? }.
    // ok:false (e.g. skill.yaml write-back failed) keeps the draft in the list;
    // ok:true + warning means the apply succeeded but the draft file could not be deleted.
    const handleApplyRepairDraft = useCallback(async (d: SkillRepairDraftItem) => {
        const name = String(d.skill || "").trim();
        const draft = String(d.draft || "").trim();
        if (!name || !draft || d.unreadable) return;
        if (d.disable) {
            const confirmed = await showConfirm(
                localizeText(
                    `Disable skill "${name}"?\n\nThe skill will be turned off as the draft suggests; you can re-enable it later.`,
                    `禁用技能「${name}」？\n\n将按草案建议关闭该技能，之后可重新启用。`,
                    `停用技能「${name}」？\n\n將按草案建議關閉該技能，之後可重新啟用。`,
                ),
                localizeText("Disable skill", "禁用技能", "停用技能"),
                {
                    confirmText: localizeText("Disable", "禁用", "停用"),
                    cancelText: localizeText("Cancel", "取消", "取消"),
                    confirmVariant: 'danger',
                },
            );
            if (!confirmed) return;
        }
        setBusy(true);
        try {
            const raw = await ApplySkillRepairDraft(name, draft);
            let res: { ok?: boolean; error?: string; message?: string; warning?: string } = {};
            try {
                res = (typeof raw === "string" ? JSON.parse(raw) : raw) || {};
            } catch {
                res = { ok: false, error: String(raw) };
            }
            if (res?.ok) {
                const warning = res.warning || "";
                if (warning) {
                    showToast(
                        localizeText(
                            `Repair draft applied to "${name}", but: ${warning}`,
                            `修复草案已应用到「${name}」，但：${warning}`,
                            `修復草案已套用到「${name}」，但：${warning}`,
                        ),
                        "warning",
                        7000,
                    );
                }
                // No success toast here — the parent panel already toasts on the
                // backend-emitted skill:repaired (via=reviewed_draft) event.
                await loadRepairDrafts();
                onDraftsChanged();
            } else {
                // Draft stays in the list — show the backend error only.
                showToast(String(res?.error || res?.message || "apply failed"), "error", 7000);
            }
        } catch (err) {
            showToast(String(err), "error");
        } finally {
            setBusy(false);
        }
    }, [showConfirm, showToast, localizeText, setBusy, loadRepairDrafts, onDraftsChanged]);

    const handleRejectRepairDraft = useCallback(async (d: SkillRepairDraftItem) => {
        const name = String(d.skill || "").trim();
        const draft = String(d.draft || "").trim();
        if (!name || !draft) return;
        const confirmed = await showConfirm(
            localizeText(
                `Reject repair draft "${draft}" for "${name}"?\n\nThe draft file is deleted and the skill keeps its current steps.`,
                `拒绝「${name}」的修复草案 ${draft}？\n\n草案文件将被删除，技能保持现有步骤不变。`,
                `拒絕「${name}」的修復草案 ${draft}？\n\n草案檔案將被刪除，技能保持現有步驟不變。`,
            ),
            localizeText("Reject repair draft", "拒绝修复草案", "拒絕修復草案"),
            {
                confirmText: localizeText("Reject", "拒绝", "拒絕"),
                cancelText: localizeText("Cancel", "取消", "取消"),
                confirmVariant: 'danger',
            },
        );
        if (!confirmed) return;
        setBusy(true);
        try {
            const raw = await RejectSkillRepairDraft(name, draft);
            let res: { ok?: boolean; error?: string; message?: string } = {};
            try {
                res = (typeof raw === "string" ? JSON.parse(raw) : raw) || {};
            } catch {
                res = { ok: false, error: String(raw) };
            }
            if (res?.ok) {
                showToast(
                    localizeText(
                        `Repair draft rejected for "${name}"`,
                        `已拒绝「${name}」的修复草案`,
                        `已拒絕「${name}」的修復草案`,
                    ),
                    "success",
                    4000,
                );
                await loadRepairDrafts();
                onDraftsChanged();
            } else {
                showToast(String(res?.error || res?.message || "reject failed"), "error", 7000);
            }
        } catch (err) {
            showToast(String(err), "error");
        } finally {
            setBusy(false);
        }
    }, [showConfirm, showToast, localizeText, setBusy, loadRepairDrafts, onDraftsChanged]);

    return (
        <div style={{ ...remoteInfoPanelStyle, marginBottom: "12px", padding: "10px 12px" }}>
            <div style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: "6px", flexWrap: "wrap" }}>
                <div style={{ fontSize: "0.8rem", fontWeight: 600 }}>
                    {localizeText("Pending repair drafts", "待评审修复", "待評審修復")}
                    {" "}({repairDrafts.length})
                </div>
                <button
                    type="button"
                    className="btn-secondary"
                    style={{ fontSize: "0.7rem", padding: "2px 8px", marginLeft: "auto" }}
                    disabled={repairDraftsLoading}
                    onClick={() => { void loadRepairDrafts(); }}
                >
                    {repairDraftsLoading ? "..." : localizeText("Refresh", "刷新", "重新整理")}
                </button>
            </div>
            <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginBottom: "8px" }}>
                {localizeText(
                    "Generated after a file-backed skill fails. Nothing is auto-applied — Apply writes the new steps to config and skill.yaml; Reject deletes the draft.",
                    "文件型技能执行失败后自动生成。不会自动应用——「应用」会把新步骤写入配置和 skill.yaml；「拒绝」会删除草案。",
                    "檔案型技能執行失敗後自動生成。不會自動套用——「套用」會把新步驟寫入設定和 skill.yaml；「拒絕」會刪除草案。",
                )}
            </div>
            {repairDraftsLoading && repairDrafts.length === 0 ? (
                <div style={{ fontSize: "0.74rem", color: colors.textMuted }}>
                    {localizeText("Loading...", "加载中...", "載入中...")}
                </div>
            ) : repairDrafts.length === 0 ? (
                <div style={{ fontSize: "0.74rem", color: colors.textMuted }}>
                    {localizeText("No repair drafts pending review.", "暂无待评审的修复。", "暫無待評審的修復。")}
                </div>
            ) : (
                <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
                    {repairDrafts.map((d, i) => {
                        const key = `${d.skill || "unknown"}-${d.draft || i}`;
                        const open = expandedRepairDraftKey === key;
                        const focused = skillNamesEqual(d.skill, evolutionFocusSkill);
                        const unreadable = d.unreadable === true;
                        const disable = d.disable === true;
                        const oldSteps = Array.isArray(d.old_steps) ? d.old_steps : [];
                        const newSteps = Array.isArray(d.new_steps) ? d.new_steps : [];
                        return (
                            <div
                                key={key}
                                data-evolution-skill={d.skill || ""}
                                style={{
                                    border: focused
                                        ? `1px solid ${colors.primary}`
                                        : `1px solid ${colors.border}`,
                                    borderLeft: `3px solid ${focused ? colors.primary : unreadable || disable ? colors.danger : (colors.warning || "#d97706")}`,
                                    borderRadius: 6,
                                    padding: "6px 8px",
                                    background: focused ? "rgba(59, 130, 246, 0.12)" : undefined,
                                    boxShadow: focused ? `0 0 0 1px ${colors.primary}` : undefined,
                                }}
                            >
                                <div style={{ display: "flex", gap: "8px", alignItems: "center", flexWrap: "wrap" }}>
                                    <strong
                                        style={{ ...skillNameLinkStyle, fontSize: "0.76rem" }}
                                        title={localizeText(
                                            "Highlight related audit rows",
                                            "高亮相关审计记录",
                                            "突顯相關審計記錄",
                                        )}
                                        onClick={() => onFocusSkill(d.skill || "")}
                                    >
                                        {d.skill || "—"}
                                    </strong>
                                    {unreadable && (
                                        <span style={{ fontSize: "0.66rem", color: colors.danger, fontWeight: 600 }}>
                                            {localizeText("corrupted / unreadable", "文件损坏/不可读", "檔案損毀/不可讀")}
                                        </span>
                                    )}
                                    {disable && !unreadable && (
                                        <span style={{ fontSize: "0.66rem", color: colors.danger, fontWeight: 600 }}>
                                            {localizeText("suggested: disable skill", "建议禁用", "建議停用")}
                                        </span>
                                    )}
                                    <span style={{ fontSize: "0.68rem", color: colors.textMuted }}>
                                        {d.created_at ? new Date(d.created_at).toLocaleString() : ""}
                                    </span>
                                    <span style={{ fontSize: "0.68rem", color: colors.textMuted }}>
                                        {d.draft || ""}
                                    </span>
                                    <span style={{ marginLeft: "auto", display: "flex", gap: "6px" }}>
                                        {!disable && (
                                            <button
                                                type="button"
                                                className="btn-secondary"
                                                style={{ fontSize: "0.7rem", padding: "2px 8px" }}
                                                disabled={unreadable}
                                                title={unreadable
                                                    ? localizeText(
                                                        "Draft file is corrupted/unreadable; steps cannot be shown",
                                                        "草案文件损坏/不可读，无法展示步骤",
                                                        "草案檔案損毀/不可讀，無法顯示步驟",
                                                    )
                                                    : undefined}
                                                onClick={() => setExpandedRepairDraftKey(open ? null : key)}
                                            >
                                                {open
                                                    ? localizeText("Hide steps", "收起步骤", "收起步驟")
                                                    : unreadable
                                                        ? localizeText("Steps (unreadable)", "步骤（不可读）", "步驟（不可讀）")
                                                        : localizeText(
                                                            `Steps (${oldSteps.length} → ${newSteps.length})`,
                                                            `步骤 (${oldSteps.length} → ${newSteps.length})`,
                                                            `步驟 (${oldSteps.length} → ${newSteps.length})`,
                                                        )}
                                            </button>
                                        )}
                                        <button
                                            type="button"
                                            className="btn-primary"
                                            style={{ fontSize: "0.7rem", padding: "2px 10px" }}
                                            disabled={busy || unreadable}
                                            title={unreadable
                                                ? localizeText(
                                                    "Draft file is corrupted/unreadable; it can only be rejected",
                                                    "草案文件损坏/不可读，只能拒绝",
                                                    "草案檔案損毀/不可讀，只能拒絕",
                                                )
                                                : undefined}
                                            onClick={() => { void handleApplyRepairDraft(d); }}
                                        >
                                            {disable
                                                ? localizeText("Disable this skill", "禁用该技能", "停用該技能")
                                                : localizeText("Apply", "应用", "套用")}
                                        </button>
                                        <button
                                            type="button"
                                            className="btn-secondary"
                                            style={{ fontSize: "0.7rem", padding: "2px 10px" }}
                                            disabled={busy}
                                            onClick={() => { void handleRejectRepairDraft(d); }}
                                        >
                                            {localizeText("Reject", "拒绝", "拒絕")}
                                        </button>
                                    </span>
                                </div>
                                {d.explanation && (
                                    <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginTop: "4px" }}>
                                        {d.explanation}
                                    </div>
                                )}
                                {d.last_error && (
                                    <div style={{ fontSize: "0.7rem", color: colors.textMuted, marginTop: "2px" }}>
                                        {localizeText("Last error", "最近错误", "最近錯誤")}: {d.last_error}
                                    </div>
                                )}
                                {unreadable && (
                                    <div style={{ fontSize: "0.7rem", color: colors.danger, marginTop: "4px" }}>
                                        {localizeText(
                                            "The draft file is corrupted or unreadable, so its steps cannot be shown. It cannot be applied — reject it to discard.",
                                            "草案文件损坏或不可读，无法展示步骤对比。不可应用——请拒绝以丢弃。",
                                            "草案檔案損毀或不可讀，無法顯示步驟對比。不可套用——請拒絕以丟棄。",
                                        )}
                                    </div>
                                )}
                                {open && !unreadable && (
                                    <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "8px", marginTop: "6px", fontSize: "0.7rem" }}>
                                        <StepsColumn
                                            title={localizeText("Old steps", "旧步骤", "舊步驟")}
                                            steps={oldSteps}
                                            localizeText={localizeText}
                                        />
                                        <StepsColumn
                                            title={localizeText("New steps", "新步骤", "新步驟")}
                                            steps={newSteps}
                                            localizeText={localizeText}
                                        />
                                    </div>
                                )}
                            </div>
                        );
                    })}
                </div>
            )}
        </div>
    );
}

const skillNameLinkStyle: CSSProperties = {
    fontWeight: 600,
    fontSize: "0.8rem",
    color: colors.primary,
    cursor: "pointer",
};

/** Cap for rendered params JSON; longer payloads are truncated with a notice. */
const STEP_PARAMS_PREVIEW_LIMIT = 2000;

/** Optional per-step meta fields shown as small key-value rows under params. */
const STEP_META_FIELDS = ["name", "label", "when", "condition"] as const;

function formatStepParams(
    params: unknown,
    localizeText: (en: string, zhHans: string, zhHant: string) => string,
): string {
    let text: string;
    try {
        text = JSON.stringify(params ?? {}, null, 2);
    } catch {
        text = String(params);
    }
    if (text.length > STEP_PARAMS_PREVIEW_LIMIT) {
        return text.slice(0, STEP_PARAMS_PREVIEW_LIMIT)
            + localizeText("\n… (truncated)", "\n…（已截断）", "\n…（已截斷）");
    }
    return text;
}

/** One column of the old/new steps comparison: numbered steps with action,
 *  full params JSON (scrollable, truncated past the preview limit) and
 *  on_error when set. */
function StepsColumn({
    title,
    steps,
    localizeText,
}: {
    title: string;
    steps: SkillRepairDraftStep[];
    localizeText: (en: string, zhHans: string, zhHant: string) => string;
}) {
    return (
        <div>
            <strong>{title} ({steps.length})</strong>
            {steps.length === 0 ? (
                <div style={{ color: colors.textMuted }}>—</div>
            ) : (
                <ol style={{ margin: "2px 0 0 1.1rem", padding: 0, color: colors.textSecondary }}>
                    {steps.map((s, j) => (
                        <li key={j} style={{ marginBottom: 4 }}>
                            <div style={{ fontWeight: 600 }}>{s.action || "—"}</div>
                            <pre style={stepParamsPreStyle}>
                                {formatStepParams(s.params, localizeText)}
                            </pre>
                            {s.on_error ? (
                                <div style={{ fontSize: "0.66rem", color: colors.textMuted }}>
                                    on_error: {s.on_error}
                                </div>
                            ) : null}
                            {STEP_META_FIELDS.map((f) =>
                                s[f] ? (
                                    <div key={f} style={{ fontSize: "0.66rem", color: colors.textMuted }}>
                                        {f}: {s[f]}
                                    </div>
                                ) : null,
                            )}
                            {s.capture && Object.keys(s.capture).length > 0 ? (
                                <div style={{ fontSize: "0.66rem", color: colors.textMuted }}>
                                    capture:
                                    {Object.entries(s.capture).map(([varName, pattern]) => (
                                        <div key={varName}>{varName} → {pattern}</div>
                                    ))}
                                </div>
                            ) : null}
                        </li>
                    ))}
                </ol>
            )}
        </div>
    );
}

const stepParamsPreStyle: CSSProperties = {
    margin: "2px 0 0",
    padding: "4px 6px",
    maxHeight: 160,
    overflow: "auto",
    fontSize: "0.64rem",
    lineHeight: 1.35,
    whiteSpace: "pre",
    background: "rgba(127, 127, 127, 0.08)",
    border: `1px solid ${colors.border}`,
    borderRadius: 4,
};
