import { useEffect, useMemo, useState, type CSSProperties } from "react";
import { BuildExperienceConflictReconciliationDraft, BuildExperienceEscalationBrief, BuildExperienceMemoryMaintenanceDraft, BuildExperienceRollbackWorkflowDraft, BuildExperienceRoutingAdjustmentDraft, BuildExperienceSkillDraft, BuildExperienceTraceFollowUp, GetExperienceGovernanceSummary, RecordExperienceDraftReview, RecordExperienceTraceFollowUp, ReviewExperienceTrace } from "../../../wailsjs/go/main/App";
import { colors, radius } from "./styles";

type Translate = (en: string, zhHans: string, zhHant?: string) => string;

type TraceFocus = { value?: string; seq?: number };

type Props = {
    t: Translate;
    learning: any;
    error: string;
    focusTrace?: TraceFocus;
    onReviewed?: () => void | Promise<void>;
};

type TraceDetail = {
    id?: string;
    kind?: string;
    title?: string;
    summary?: string;
    detail?: string;
    source_type?: string;
    source_url?: string;
    source_trace_id?: string;
    tags?: string[];
    evidence?: number;
    confidence?: number;
    impact?: string;
    review_required?: boolean;
    review_action?: string;
    review_status?: string;
    next_action_kind?: string;
    next_action?: string;
    reviewed_at?: string;
    reviewer?: string;
    review_note?: string;
    review_count?: number;
    follow_up_status?: string;
    follow_up_action_kind?: string;
    follow_up_at?: string;
    follow_up_actor?: string;
    follow_up_note?: string;
    follow_up_count?: number;
    triggered_rollback?: boolean;
    updated_at?: string;
};

type NextActionSummary = {
    kind?: string;
    count?: number;
    latest_trace_id?: string;
    latest_title?: string;
    latest_action?: string;
    latest_updated_at?: string;
};

type ReviewSummary = {
    status?: string;
    count?: number;
    required_count?: number;
    latest_trace_id?: string;
    latest_title?: string;
    latest_kind?: string;
    latest_action?: string;
    latest_reviewer?: string;
    latest_note?: string;
    latest_reviewed_at?: string;
    latest_updated_at?: string;
};

type FollowUpSummary = {
    status?: string;
    count?: number;
    triggered_rollback?: boolean;
    triggered_count?: number;
    latest_trace_id?: string;
    latest_title?: string;
    recommended_trace_id?: string;
    recommended_title?: string;
    recommended_reason?: string;
    latest_action_kind?: string;
    latest_note?: string;
    latest_updated_at?: string;
};

type FollowUpActionSummary = {
    kind?: string;
    count?: number;
    status_counts?: Record<string, number>;
    triggered_rollback?: boolean;
    triggered_count?: number;
    latest_trace_id?: string;
    latest_title?: string;
    recommended_trace_id?: string;
    recommended_title?: string;
    recommended_reason?: string;
    latest_status?: string;
    latest_note?: string;
    latest_updated_at?: string;
};

type GovernanceSummary = {
    recommended_next_action?: string;
    recommended_next_action_reason?: string;
    recommended_focus?: GovernanceFocus;
    recommended_focus_context?: GovernanceFocusContext | null;
    recommended_tool_call?: GovernanceToolCall | null;
    non_executing_boundary?: string;
    memory?: Record<string, any>;
    routing_self_evolution?: Record<string, any>;
    a2a_discussion?: Record<string, any>;
    queues?: Record<string, any>;
};

type GovernanceToolCall = {
    tool?: string;
    args?: Record<string, any>;
    recommended_focus_context?: GovernanceFocusContext | null;
    governance_focus_context?: GovernanceFocusContext | null;
    non_executing?: boolean;
    non_executing_boundary?: string;
};

type GovernanceFocus = {
    trace_filter?: string;
    review_status?: string;
    action_kind?: string;
    follow_up_status?: string;
    follow_up_action_kind?: string;
    triggered_rollback_only?: boolean;
    non_executing?: boolean;
};

type GovernanceFocusContext = {
    priority_trace_id?: string;
    priority_trace_title?: string;
    recommended_trace_id?: string;
    recommended_title?: string;
    reason?: string;
    [key: string]: any;
};

function experienceTriggeredRollbackDetail(detail: TraceDetail | null | undefined): boolean {
    if (!detail) return false;
    return detail.triggered_rollback === true
        || detail.next_action_kind === "review_triggered_rollback_signal"
        || detail.follow_up_action_kind === "review_triggered_rollback_signal"
        || (Array.isArray(detail.tags) && detail.tags.includes("rollback_triggered"))
        || String(detail.detail || "").toLowerCase().includes("matched rollback triggers");
}

type ProtectedCandidate = {
    id?: string;
    title?: string;
    summary?: string;
    source?: string;
    reason?: string;
    tags?: string[];
    strength?: number;
    pinned?: boolean;
};

type FollowUpDraft = {
    draft_title?: string;
    draft?: string;
    action_kind?: string;
    non_executing_boundary?: string;
    recommended_focus_context?: GovernanceFocusContext | null;
    recommended_tool_call?: GovernanceToolCall | null;
};

type SkillDraft = {
    suggested_name?: string;
    draft_markdown?: string;
    non_executing_boundary?: string;
    recommended_focus_context?: GovernanceFocusContext | null;
    recommended_tool_call?: GovernanceToolCall | null;
};

type RollbackDraft = {
    draft_markdown?: string;
    non_executing_boundary?: string;
    rollback_triggers?: string[];
    recommended_focus_context?: GovernanceFocusContext | null;
    recommended_tool_call?: GovernanceToolCall | null;
};

type EscalationBrief = {
    brief_markdown?: string;
    non_executing_boundary?: string;
    target?: string;
    recommended_focus_context?: GovernanceFocusContext | null;
    recommended_tool_call?: GovernanceToolCall | null;
};

type ConflictDraft = {
    draft_markdown?: string;
    non_executing_boundary?: string;
    topic?: string;
    recommended_focus_context?: GovernanceFocusContext | null;
    recommended_tool_call?: GovernanceToolCall | null;
};

type ExperienceLearningDraft = {
    draft_markdown?: string;
    non_executing_boundary?: string;
    recommended_focus_context?: GovernanceFocusContext | null;
    recommended_tool_call?: GovernanceToolCall | null;
};

type DraftReviewRecord = {
    trace_id?: string;
    memory_id?: string;
    kind?: string;
    status?: string;
    source_trace_id?: string;
    recommended_focus_context?: GovernanceFocusContext | null;
    recommended_tool_call?: GovernanceToolCall | null;
    non_executing_boundary?: string;
};

type TraceReviewRecord = {
    trace_id?: string;
    memory_id?: string;
    kind?: string;
    outcome?: string;
    recommended_focus_context?: GovernanceFocusContext | null;
    recommended_tool_call?: GovernanceToolCall | null;
    non_executing_boundary?: string;
};

type FollowUpRecord = {
    trace_id?: string;
    memory_id?: string;
    status?: string;
    action_kind?: string;
    recommended_focus_context?: GovernanceFocusContext | null;
    recommended_tool_call?: GovernanceToolCall | null;
    non_executing_boundary?: string;
};

function draftReviewDraft(markdown?: string, boundary?: string, context?: GovernanceFocusContext | null, toolCall?: GovernanceToolCall | null): ExperienceLearningDraft | null {
    return markdown ? { draft_markdown: markdown, non_executing_boundary: boundary, recommended_focus_context: context || null, recommended_tool_call: toolCall || null } : null;
}

function draftSourceTraceId(context: GovernanceFocusContext | null | undefined, fallback = ""): string {
    return String(context?.priority_trace_id || fallback || "").trim();
}

type DraftReviewControlsProps = {
    t: Translate;
    draft: ExperienceLearningDraft | null;
    note: string;
    recording: DraftReviewStatus | "";
    error: string;
    message: string;
    recommendedToolCall?: GovernanceToolCall | null;
    notePlaceholder?: string;
    onNoteChange: (value: string) => void;
    onRecord: (status: DraftReviewStatus) => void;
};

const traceFilterValues = ["all", "review", "actions", "followups", "reviewed", "a2a", "tools", "sessions"] as const;
type TraceFilter = typeof traceFilterValues[number];
type DraftReviewStatus = "completed" | "blocked" | "deferred";

function isTraceFilter(value: string): value is TraceFilter {
    return (traceFilterValues as readonly string[]).includes(value);
}

export function ExperienceLearningPanel({ t, learning, error, focusTrace, onReviewed }: Props) {
    const hints = Array.isArray(learning?.routing_hints) ? learning.routing_hints : [];
    const nudges = Array.isArray(learning?.skill_nudge_candidates) ? learning.skill_nudge_candidates : [];
    const patterns = Array.isArray(learning?.usage_patterns) ? learning.usage_patterns : [];
    const recoveryPatterns = Array.isArray(learning?.recovery_patterns) ? learning.recovery_patterns : [];
    const traceDetails: TraceDetail[] = Array.isArray(learning?.trace_details) ? learning.trace_details : [];
    const reviewSummaries: ReviewSummary[] = Array.isArray(learning?.review_summaries) ? learning.review_summaries : [];
    const nextActionSummaries: NextActionSummary[] = Array.isArray(learning?.next_action_summaries) ? learning.next_action_summaries : [];
    const followUpSummaries: FollowUpSummary[] = Array.isArray(learning?.follow_up_summaries) ? learning.follow_up_summaries : [];
    const followUpActionSummaries: FollowUpActionSummary[] = Array.isArray(learning?.follow_up_action_summaries) ? learning.follow_up_action_summaries : [];
    const protectedCandidates: ProtectedCandidate[] = Array.isArray(learning?.memory_experience?.protected_samples) ? learning.memory_experience.protected_samples : [];
    const layeredMemoryRecommended = Boolean(learning?.layered_memory_recommended || learning?.memory_experience?.layered_recommended);
    const layeredMemoryReason = String(learning?.layered_memory_reason || learning?.memory_experience?.reason || "").trim();
    const memoryMaintenanceRecommendation = String(learning?.memory_maintenance_recommendation || "").trim();
    const memoryMaintenanceBoundary = String(learning?.memory_maintenance_boundary || "").trim();
    const traceKindCounts = toCountMap(learning?.trace_kind_counts);
    const reviewStatusCounts = toCountMap(learning?.review_status_counts);
    const nextActionKindCounts = toCountMap(learning?.next_action_kind_counts);
    const followUpStatusCounts = toCountMap(learning?.follow_up_status_counts);
    const traceDetailCount = toSafeCount(learning?.trace_detail_count, traceDetails.length);
    const governanceSummary: GovernanceSummary | null = learning?.governance_summary && typeof learning.governance_summary === "object" ? learning.governance_summary : null;
    const triggeredRollbackFollowUpCount = followUpSummaries.reduce((total, item) => total + toSafeCount(item.triggered_count, item.triggered_rollback ? toSafeCount(item.count, 0) : 0), 0);
    const triggeredRollbackFollowUpReason = String(followUpSummaries.find((item) => item.triggered_rollback && String(item.recommended_reason || item.latest_note || "").trim())?.recommended_reason || followUpSummaries.find((item) => item.triggered_rollback && String(item.latest_note || "").trim())?.latest_note || "").trim();
    const [governancePreview, setGovernancePreview] = useState<GovernanceSummary | null>(null);
    const [governancePreviewTaskType, setGovernancePreviewTaskType] = useState("");
    const [governancePreviewQuery, setGovernancePreviewQuery] = useState("");
    const [governancePreviewTool, setGovernancePreviewTool] = useState("");
    const [governancePreviewLoading, setGovernancePreviewLoading] = useState(false);
    const [governancePreviewError, setGovernancePreviewError] = useState("");
    const [selectedTraceId, setSelectedTraceId] = useState("");
    const [traceFilter, setTraceFilter] = useState<TraceFilter>("all");
    const [reviewFocusStatus, setReviewFocusStatus] = useState("");
    const [actionFocusKind, setActionFocusKind] = useState("");
    const [followUpFocusStatus, setFollowUpFocusStatus] = useState("");
    const [followUpFocusActionKind, setFollowUpFocusActionKind] = useState("");
    const [followUpTriggeredOnly, setFollowUpTriggeredOnly] = useState(false);
    const [followUpFocusReason, setFollowUpFocusReason] = useState("");
    const [followUpRecommendedTraceId, setFollowUpRecommendedTraceId] = useState("");
    const [priorityTraceId, setPriorityTraceId] = useState("");
    const [priorityTraceReason, setPriorityTraceReason] = useState("");
    const [maintenanceDraft, setMaintenanceDraft] = useState<ExperienceLearningDraft | null>(null);
    const [maintenanceDraftQuery, setMaintenanceDraftQuery] = useState("");
    const [maintenanceDraftLoading, setMaintenanceDraftLoading] = useState(false);
    const [maintenanceDraftError, setMaintenanceDraftError] = useState("");
    const [maintenanceReviewNote, setMaintenanceReviewNote] = useState("");
    const [maintenanceReviewing, setMaintenanceReviewing] = useState<DraftReviewStatus | "">("");
    const [maintenanceReviewError, setMaintenanceReviewError] = useState("");
    const [maintenanceReviewMessage, setMaintenanceReviewMessage] = useState("");
    const [maintenanceReviewRecord, setMaintenanceReviewRecord] = useState<DraftReviewRecord | null>(null);
    const [routingDraft, setRoutingDraft] = useState<ExperienceLearningDraft | null>(null);
    const [routingDraftQuery, setRoutingDraftQuery] = useState("");
    const [routingDraftLoading, setRoutingDraftLoading] = useState(false);
    const [routingDraftError, setRoutingDraftError] = useState("");
    const [routingReviewNote, setRoutingReviewNote] = useState("");
    const [routingReviewing, setRoutingReviewing] = useState<DraftReviewStatus | "">("");
    const [routingReviewError, setRoutingReviewError] = useState("");
    const [routingReviewMessage, setRoutingReviewMessage] = useState("");
    const [routingReviewRecord, setRoutingReviewRecord] = useState<DraftReviewRecord | null>(null);
    const displayedGovernanceSummary = governancePreview || governanceSummary;
    const governancePriorityTrace = useMemo(() => {
        if (!displayedGovernanceSummary) return { id: "", title: "", reason: "" };
        const backendContext = asRecord(displayedGovernanceSummary.recommended_focus_context);
        const contextTraceID = String(backendContext.priority_trace_id || backendContext.recommended_trace_id || "").trim();
        const contextTraceTitle = String(backendContext.priority_trace_title || backendContext.recommended_title || "").trim();
        const contextReason = String(backendContext.reason || "").trim();
        if (contextTraceID || contextTraceTitle || contextReason) {
            return { id: contextTraceID, title: contextTraceTitle || contextTraceID, reason: contextReason };
        }
        const focus = displayedGovernanceSummary.recommended_focus;
        const action = String(displayedGovernanceSummary.recommended_next_action || "").trim();
        const reason = String(displayedGovernanceSummary.recommended_next_action_reason || "").trim();
        const focusFilter = String(focus?.trace_filter || "").trim();
        const focusReviewStatus = String(focus?.review_status || "").trim();
        const focusActionKind = String(focus?.action_kind || "").trim();
        const focusFollowUpStatus = String(focus?.follow_up_status || "").trim();
        const focusFollowUpActionKind = String(focus?.follow_up_action_kind || "").trim();
        const focusTriggeredRollback = Boolean(focus?.triggered_rollback_only);
        const traceId = findPriorityTraceForGovernanceFocus(
            traceDetails,
            isTraceFilter(focusFilter) ? focusFilter : fallbackGovernanceTraceFilter(action),
            focusReviewStatus,
            focusActionKind || fallbackGovernanceActionKind(action),
            focusFollowUpStatus,
            focusFollowUpActionKind || fallbackGovernanceFollowUpActionKind(action),
            focusTriggeredRollback || fallbackGovernanceTriggeredRollbackOnly(action),
        );
        const target = traceDetails.find((item) => item.id === traceId) || null;
        return {
            id: traceId,
            title: String(target?.title || target?.id || "").trim(),
            reason,
        };
    }, [displayedGovernanceSummary, traceDetails]);
    const visibleTraceDetails = useMemo(() => {
        const filtered = filterTraceDetails(traceDetails, traceFilter, reviewFocusStatus, actionFocusKind, followUpFocusStatus, followUpTriggeredOnly, followUpFocusActionKind);
        return sortTraceDetailsForFocus(filtered, priorityTraceId, followUpTriggeredOnly || actionFocusKind === "review_triggered_rollback_signal" || traceFilter === "reviewed");
    }, [traceDetails, traceFilter, reviewFocusStatus, actionFocusKind, followUpFocusStatus, followUpTriggeredOnly, followUpFocusActionKind, priorityTraceId]);
    const selectedTrace = useMemo(() => visibleTraceDetails.find((item) => item.id === selectedTraceId) || visibleTraceDetails[0] || null, [selectedTraceId, visibleTraceDetails]);
    useEffect(() => {
        if (!traceDetails.length || !focusTrace?.seq) return;
        const focused = findFocusedTrace(traceDetails, focusTrace.value || "") || traceDetails[0];
        setTraceFilter(traceFilterForDetail(focused));
        setReviewFocusStatus(reviewStatusOfDetail(focused));
        setActionFocusKind("");
        setFollowUpFocusStatus(focused?.follow_up_status || "");
        setFollowUpFocusActionKind(focused?.follow_up_action_kind || "");
        setFollowUpTriggeredOnly(experienceTriggeredRollbackDetail(focused));
        setFollowUpFocusReason("");
        setFollowUpRecommendedTraceId("");
        setPriorityTraceId(focused?.id || "");
        setPriorityTraceReason("");
        setSelectedTraceId(focused?.id || "");
    }, [traceDetails, focusTrace?.seq, focusTrace?.value]);
    useEffect(() => {
        if (!visibleTraceDetails.length) {
            setSelectedTraceId("");
            return;
        }
        if (!visibleTraceDetails.some((item) => item.id === selectedTraceId)) {
            setSelectedTraceId(visibleTraceDetails[0]?.id || "");
        }
    }, [selectedTraceId, visibleTraceDetails]);
    const routingHintCount = toSafeCount(learning?.routing_hint_count, hints.length);
    const skillNudgeCount = toSafeCount(learning?.skill_nudge_count, nudges.length);
    const usagePatternCount = toSafeCount(learning?.usage_pattern_count, patterns.length);
    const recoveryPatternCount = toSafeCount(learning?.recovery_pattern_count, recoveryPatterns.length);
    const protectedMemoryCount = toSafeCount(learning?.protected_memory_count, learning?.memory_experience?.protected_candidates || 0);
    const reviewRequiredTraceCount = toSafeCount(learning?.review_required_trace_count, traceDetails.filter((detail) => detail.review_required).length);
    const nextActionTraceCount = toSafeCount(learning?.next_action_trace_count, traceDetails.filter((detail) => detail.next_action || detail.next_action_kind).length);
    const followUpTraceCount = toSafeCount(learning?.follow_up_trace_count, traceDetails.filter((detail) => detail.follow_up_status).length);
    const hasRoutingDraftEvidence = routingHintCount > 0 || skillNudgeCount > 0 || usagePatternCount > 0 || recoveryPatternCount > 0;
    const hasSignals = hasRoutingDraftEvidence || protectedMemoryCount > 0 || reviewRequiredTraceCount > 0 || nextActionTraceCount > 0 || followUpTraceCount > 0;
    const loadMaintenanceDraft = async () => {
        if (maintenanceDraftLoading) return;
        setMaintenanceDraftLoading(true);
        setMaintenanceDraftError("");
        setMaintenanceReviewMessage("");
        setMaintenanceReviewRecord(null);
        try {
            const result = await BuildExperienceMemoryMaintenanceDraft({ limit: 12, query: maintenanceDraftQuery.trim() });
            setMaintenanceDraft(result || null);
        } catch (err) {
            setMaintenanceDraftError(String(err));
        } finally {
            setMaintenanceDraftLoading(false);
        }
    };
    const loadMaintenanceDraftFromGovernance = async () => {
        if (maintenanceDraftLoading) return;
        const query = governancePreviewQuery.trim();
        setMaintenanceDraftQuery(query);
        setMaintenanceDraftLoading(true);
        setMaintenanceDraftError("");
        setMaintenanceReviewMessage("");
        try {
            const result = await BuildExperienceMemoryMaintenanceDraft({ limit: 12, query });
            setMaintenanceDraft(result || null);
        } catch (err) {
            setMaintenanceDraftError(String(err));
        } finally {
            setMaintenanceDraftLoading(false);
        }
    };
    const loadRoutingDraft = async () => {
        if (routingDraftLoading) return;
        setRoutingDraftLoading(true);
        setRoutingDraftError("");
        setRoutingReviewMessage("");
        setRoutingReviewRecord(null);
        try {
            const result = await BuildExperienceRoutingAdjustmentDraft({ limit: 12, query: routingDraftQuery.trim() });
            setRoutingDraft(result || null);
        } catch (err) {
            setRoutingDraftError(String(err));
        } finally {
            setRoutingDraftLoading(false);
        }
    };
    const loadRoutingDraftFromGovernancePreview = async () => {
        if (routingDraftLoading) return;
        const source = governancePreview || governanceSummary;
        const routing = asRecord(source?.routing_self_evolution);
        const routingQuery = asRecord(routing.query);
        const taskType = String(routingQuery.task_type || governancePreviewTaskType).trim();
        const query = String(routingQuery.query || governancePreviewQuery).trim();
        const tool = String(routingQuery.tool || governancePreviewTool).trim();
        setRoutingDraftLoading(true);
        setRoutingDraftError("");
        setRoutingReviewMessage("");
        try {
            const result = await BuildExperienceRoutingAdjustmentDraft({ task_type: taskType, query, tool, limit: 12 });
            setRoutingDraft(result || null);
            setRoutingDraftQuery(query || [taskType, tool].filter(Boolean).join(" "));
        } catch (err) {
            setRoutingDraftError(String(err));
        } finally {
            setRoutingDraftLoading(false);
        }
    };
    const recordMaintenanceDraftReview = async (status: DraftReviewStatus) => {
        if (!maintenanceDraft?.draft_markdown || maintenanceReviewing) return;
        setMaintenanceReviewing(status);
        setMaintenanceReviewError("");
        setMaintenanceReviewMessage("");
        setMaintenanceReviewRecord(null);
        try {
            const record = await RecordExperienceDraftReview({
                kind: "memory_maintenance_draft",
                status,
                source_trace_id: draftSourceTraceId(maintenanceDraft.recommended_focus_context),
                query: maintenanceDraftQuery.trim(),
                note: maintenanceReviewNote.trim(),
                draft_markdown: maintenanceDraft.draft_markdown,
                non_executing_boundary: maintenanceDraft.non_executing_boundary || "",
            });
            setMaintenanceReviewRecord(record || null);
            setMaintenanceReviewMessage(t("Draft review recorded.", "\u8349\u6848\u5ba1\u9605\u5df2\u8bb0\u5f55\u3002", "\u8349\u6848\u5be9\u95b1\u5df2\u8a18\u9304\u3002"));
            setMaintenanceReviewNote("");
            await onReviewed?.();
        } catch (err) {
            setMaintenanceReviewError(String(err));
        } finally {
            setMaintenanceReviewing("");
        }
    };
    const recordRoutingDraftReview = async (status: DraftReviewStatus) => {
        if (!routingDraft?.draft_markdown || routingReviewing) return;
        setRoutingReviewing(status);
        setRoutingReviewError("");
        setRoutingReviewMessage("");
        setRoutingReviewRecord(null);
        try {
            const record = await RecordExperienceDraftReview({
                kind: "routing_adjustment_draft",
                status,
                source_trace_id: draftSourceTraceId(routingDraft.recommended_focus_context),
                query: routingDraftQuery.trim(),
                note: routingReviewNote.trim(),
                draft_markdown: routingDraft.draft_markdown,
                non_executing_boundary: routingDraft.non_executing_boundary || "",
            });
            setRoutingReviewRecord(record || null);
            setRoutingReviewMessage(t("Draft review recorded.", "\u8349\u6848\u5ba1\u9605\u5df2\u8bb0\u5f55\u3002", "\u8349\u6848\u5be9\u95b1\u5df2\u8a18\u9304\u3002"));
            setRoutingReviewNote("");
            await onReviewed?.();
        } catch (err) {
            setRoutingReviewError(String(err));
        } finally {
            setRoutingReviewing("");
        }
    };
    const loadGovernancePreview = async () => {
        if (governancePreviewLoading) return;
        setGovernancePreviewLoading(true);
        setGovernancePreviewError("");
        try {
            const result = await GetExperienceGovernanceSummary({
                task_type: governancePreviewTaskType.trim(),
                query: governancePreviewQuery.trim(),
                tool: governancePreviewTool.trim(),
                limit: 8,
            });
            setGovernancePreview(result || null);
        } catch (err) {
            setGovernancePreviewError(String(err));
        } finally {
            setGovernancePreviewLoading(false);
        }
    };
    const clearGovernancePreview = () => {
        setGovernancePreview(null);
        setGovernancePreviewTaskType("");
        setGovernancePreviewQuery("");
        setGovernancePreviewTool("");
        setGovernancePreviewError("");
    };
    const focusGovernanceAction = (action: string, focus?: GovernanceFocus, reason?: string, focusContext?: GovernanceFocusContext | null) => {
        const normalized = action.trim();
        const backendContext = asRecord(focusContext);
        const backendTraceId = String(backendContext.priority_trace_id || backendContext.recommended_trace_id || "").trim();
        const backendReason = String(backendContext.reason || "").trim();
        const focusFilter = String(focus?.trace_filter || "").trim();
        const focusReviewStatus = String(focus?.review_status || "").trim();
        const focusActionKind = String(focus?.action_kind || "").trim();
        const focusFollowUpStatus = String(focus?.follow_up_status || "").trim();
        const focusFollowUpActionKind = String(focus?.follow_up_action_kind || "").trim();
        const focusTriggeredRollback = Boolean(focus?.triggered_rollback_only);
        const effectiveFocusFilter = isTraceFilter(focusFilter) ? focusFilter : fallbackGovernanceTraceFilter(normalized);
        const effectiveActionKind = focusActionKind || fallbackGovernanceActionKind(normalized);
        const effectiveFollowUpActionKind = focusFollowUpActionKind || fallbackGovernanceFollowUpActionKind(normalized);
        const effectiveTriggeredRollback = focusTriggeredRollback || fallbackGovernanceTriggeredRollbackOnly(normalized);
        const governancePriorityTraceId = backendTraceId || findPriorityTraceForGovernanceFocus(traceDetails, effectiveFocusFilter, focusReviewStatus, effectiveActionKind, focusFollowUpStatus, effectiveFollowUpActionKind, effectiveTriggeredRollback);
        const governancePriorityReason = backendReason || String(reason || "").trim();
        setReviewFocusStatus("");
        setActionFocusKind("");
        setFollowUpFocusStatus("");
        setFollowUpFocusActionKind("");
        setFollowUpTriggeredOnly(false);
        setFollowUpFocusReason("");
        setFollowUpRecommendedTraceId("");
        setPriorityTraceId("");
        setPriorityTraceReason("");
        if (isTraceFilter(focusFilter)) {
            setReviewFocusStatus(focusReviewStatus);
            setActionFocusKind(effectiveActionKind);
            setFollowUpFocusStatus(focusFollowUpStatus);
            setFollowUpFocusActionKind(effectiveFollowUpActionKind);
            setFollowUpTriggeredOnly(effectiveTriggeredRollback);
            if (effectiveTriggeredRollback) {
                const focusReason = governancePriorityReason;
                setFollowUpFocusReason(focusReason);
                setPriorityTraceReason(focusReason);
            }
            if (governancePriorityTraceId) {
                setPriorityTraceId(governancePriorityTraceId);
                setSelectedTraceId(governancePriorityTraceId);
            }
            setTraceFilter(focusFilter);
            return;
        }
        switch (normalized) {
            case "review_required_traces":
            case "review_signal":
                if (governancePriorityTraceId) {
                    setPriorityTraceId(governancePriorityTraceId);
                    setSelectedTraceId(governancePriorityTraceId);
                }
                setTraceFilter("review");
                return;
            case "review_triggered_rollback_signal":
                setPriorityTraceReason(governancePriorityReason);
                if (governancePriorityTraceId) {
                    setPriorityTraceId(governancePriorityTraceId);
                    setSelectedTraceId(governancePriorityTraceId);
                }
                setTraceFilter("actions");
                setActionFocusKind("review_triggered_rollback_signal");
                return;
            case "inspect_follow_up_actions":
            case "inspect_triggered_rollback_followups":
                if (normalized === "inspect_triggered_rollback_followups") {
                    const focusReason = governancePriorityReason;
                    setFollowUpTriggeredOnly(true);
                    setFollowUpFocusReason(focusReason);
                    setPriorityTraceReason(focusReason);
                    setFollowUpFocusActionKind(effectiveFollowUpActionKind);
                }
                if (governancePriorityTraceId) {
                    setPriorityTraceId(governancePriorityTraceId);
                    setSelectedTraceId(governancePriorityTraceId);
                }
                setTraceFilter("followups");
                return;
            case "review_routing_candidates":
            case "inspect_routing_signals":
            case "inspect_skill_nudge_candidates":
            case "inspect_tool_recovery_governance":
                if (governancePriorityTraceId) {
                    setPriorityTraceId(governancePriorityTraceId);
                    setSelectedTraceId(governancePriorityTraceId);
                }
                setPriorityTraceReason(governancePriorityReason);
                setTraceFilter("tools");
                return;
            case "build_memory_maintenance_draft":
            case "inspect_memory_candidates":
            case "normal_operation":
                setTraceFilter("all");
                return;
            default:
                if (normalized) {
                    setActionFocusKind(normalized);
                    setTraceFilter("actions");
                }
        }
    };

    return (
        <div className="memory-learning-panel" style={{ border: "1px solid " + colors.border, borderRadius: radius.lg, padding: "14px 16px", marginBottom: 14, background: colors.surface }}>
            <div className="memory-learning-panel__header">
                <div className="memory-learning-panel__title">
                    {t("Experience Learning", "\u7ecf\u9a8c\u5b66\u4e60", "\u7d93\u9a57\u5b78\u7fd2")}
                </div>
                <span className="memory-learning-panel__badge" style={{ border: "1px solid " + colors.borderLight, borderRadius: radius.sm }}>
                    {t("review-gated", "\u5ba1\u9605\u95e8\u63a7", "\u5be9\u95b1\u95dc\u5361")}
                </span>
            </div>
            <div className="memory-learning-stat-grid" style={{ marginBottom: hasSignals || error ? 10 : 0 }}>
                <LearningStat label={t("Routing Hints", "\u8def\u7531\u63d0\u793a", "\u8def\u7531\u63d0\u793a")} value={routingHintCount} />
                <LearningStat label={t("Skill Nudges", "\u6280\u80fd\u5019\u9009", "\u6280\u80fd\u5019\u9078")} value={skillNudgeCount} />
                <LearningStat label={t("Usage Patterns", "\u5de5\u5177\u6a21\u5f0f", "\u5de5\u5177\u6a21\u5f0f")} value={usagePatternCount} />
                <LearningStat label={t("Recoveries", "\u6062\u590d\u6a21\u5f0f", "\u6062\u5fa9\u6a21\u5f0f")} value={recoveryPatternCount} />
                <LearningStat label={t("Protected", "\u4fdd\u62a4\u8bb0\u5fc6", "\u4fdd\u8b77\u8a18\u61b6")} value={protectedMemoryCount} />
                <LearningStat label={t("Needs Review", "\u9700\u8bc4\u5ba1", "\u9700\u8a55\u5be9")} value={reviewRequiredTraceCount} />
                <LearningStat label={t("Next Actions", "\u540e\u7eed\u52a8\u4f5c", "\u5f8c\u7e8c\u52d5\u4f5c")} value={nextActionTraceCount} />
                <LearningStat label={t("Follow-ups", "\u540e\u7eed\u8bb0\u5f55", "\u5f8c\u7e8c\u8a18\u9304")} value={followUpTraceCount} />
                <LearningStat label={t("Rollback Audit", "\u56de\u6eda\u5ba1\u8ba1", "\u56de\u6efe\u5be9\u8a08")} value={triggeredRollbackFollowUpCount} />
            </div>
            {error && <div role="alert" style={{ color: colors.danger, fontSize: "0.72rem", marginBottom: 8 }}>{error}</div>}
            {displayedGovernanceSummary && (
                <GovernanceSummaryNotice
                    t={t}
                    summary={displayedGovernanceSummary}
                    taskType={governancePreviewTaskType}
                    query={governancePreviewQuery}
                    tool={governancePreviewTool}
                    previewActive={Boolean(governancePreview)}
                    loading={governancePreviewLoading}
                    error={governancePreviewError}
                    triggeredRollbackFollowUpCount={triggeredRollbackFollowUpCount}
                    triggeredRollbackFollowUpReason={triggeredRollbackFollowUpReason}
                    recommendedTraceId={governancePriorityTrace.id}
                    recommendedTraceTitle={governancePriorityTrace.title}
                    recommendedTraceReason={governancePriorityTrace.reason}
                    onTaskTypeChange={setGovernancePreviewTaskType}
                    onQueryChange={setGovernancePreviewQuery}
                    onToolChange={setGovernancePreviewTool}
                    onPreview={loadGovernancePreview}
                    onClearPreview={clearGovernancePreview}
                    onFocusAction={focusGovernanceAction}
                    maintenanceDrafting={maintenanceDraftLoading}
                    routingDrafting={routingDraftLoading}
                    onDraftMaintenance={loadMaintenanceDraftFromGovernance}
                    onDraftRoutingPreview={loadRoutingDraftFromGovernancePreview}
                />
            )}
            {(memoryMaintenanceRecommendation || layeredMemoryReason || memoryMaintenanceBoundary) && (
                <MemoryMaintenanceNotice
                    t={t}
                    layered={layeredMemoryRecommended}
                    recommendation={memoryMaintenanceRecommendation}
                    reason={layeredMemoryReason}
                    boundary={memoryMaintenanceBoundary}
                    draft={maintenanceDraft}
                    draftQuery={maintenanceDraftQuery}
                    draftLoading={maintenanceDraftLoading}
                    draftError={maintenanceDraftError}
                    reviewNote={maintenanceReviewNote}
                    reviewRecording={maintenanceReviewing}
                    reviewError={maintenanceReviewError}
                    reviewMessage={maintenanceReviewMessage}
                    reviewToolCall={maintenanceReviewRecord?.recommended_tool_call || null}
                    onDraftQueryChange={setMaintenanceDraftQuery}
                    onDraft={loadMaintenanceDraft}
                    onReviewNoteChange={setMaintenanceReviewNote}
                    onRecordReview={recordMaintenanceDraftReview}
                />
            )}
            {hasRoutingDraftEvidence && (
                <RoutingDraftNotice
                    t={t}
                    draft={routingDraft}
                    query={routingDraftQuery}
                    loading={routingDraftLoading}
                    error={routingDraftError}
                    reviewNote={routingReviewNote}
                    reviewRecording={routingReviewing}
                    reviewError={routingReviewError}
                    reviewMessage={routingReviewMessage}
                    reviewToolCall={routingReviewRecord?.recommended_tool_call || null}
                    onQueryChange={setRoutingDraftQuery}
                    onDraft={loadRoutingDraft}
                    onReviewNoteChange={setRoutingReviewNote}
                    onRecordReview={recordRoutingDraftReview}
                />
            )}
            {!hasSignals && !error && (
                <div style={{ fontSize: "0.72rem", color: colors.textMuted }}>
                    {t("No active distilled signals yet.", "\u6682\u65e0\u6d3b\u8dc3\u7684\u84b8\u998f\u4fe1\u53f7\u3002", "\u66ab\u7121\u6d3b\u8e8d\u7684\u84b8\u993e\u8a0a\u865f\u3002")}
                </div>
            )}
            {protectedCandidates.length > 0 && <ProtectedMemoryRows t={t} items={protectedCandidates} />}
            {nextActionSummaries.length > 0 && (
                <NextActionSummaryRows
                    t={t}
                    items={nextActionSummaries}
                    onOpen={(item) => {
                        setReviewFocusStatus("");
                        setActionFocusKind(item.kind || "");
                        setFollowUpFocusStatus("");
                        setFollowUpFocusActionKind("");
                        setFollowUpTriggeredOnly(false);
                        setFollowUpFocusReason("");
                        setFollowUpRecommendedTraceId("");
                        setPriorityTraceId(item.kind === "review_triggered_rollback_signal" ? String(item.latest_trace_id || "").trim() : "");
                        setPriorityTraceReason(item.kind === "review_triggered_rollback_signal" ? t("This action is waiting on a rollback audit review before any rollback workflow can move forward.", "\u8be5\u52a8\u4f5c\u6b63\u5728\u7b49\u5f85 rollback \u5ba1\u8ba1\u590d\u6838\uff0c\u5728\u4efb\u4f55 rollback \u6d41\u7a0b\u7ee7\u7eed\u524d\u9700\u5148\u5b8c\u6210\u8fd9\u4e00\u6b65\u3002", "\u8a72\u52d5\u4f5c\u6b63\u5728\u7b49\u5f85 rollback \u5be9\u8a08\u8907\u6838\uff0c\u5728\u4efb\u4f55 rollback \u6d41\u7a0b\u7e7c\u7e8c\u524d\u9700\u5148\u5b8c\u6210\u9019\u4e00\u6b65\u3002") : "");
                        setTraceFilter("actions");
                        if (item.latest_trace_id) setSelectedTraceId(item.latest_trace_id);
                    }}
                />
            )}
            {reviewSummaries.length > 0 && (
                <ReviewSummaryRows
                    t={t}
                    items={reviewSummaries}
                    onOpen={(item) => {
                        const status = item.status || "";
                        setReviewFocusStatus(status);
                        setActionFocusKind("");
                        setFollowUpFocusStatus("");
                        setFollowUpFocusActionKind("");
                        setFollowUpTriggeredOnly(false);
                        setFollowUpFocusReason("");
                        setFollowUpRecommendedTraceId("");
                        setPriorityTraceId(reviewSummaryIsTriggeredRollback(item) ? String(item.latest_trace_id || "").trim() : "");
                        setPriorityTraceReason(reviewSummaryIsTriggeredRollback(item) ? String(item.latest_note || item.latest_action || "").trim() || t("This review queue item is tracking a rollback audit outcome.", "\u8fd9\u4e2a\u5ba1\u9605\u961f\u5217\u9879\u6b63\u5728\u8ddf\u8e2a rollback \u5ba1\u8ba1\u7ed3\u679c\u3002", "\u9019\u500b\u5be9\u95b1\u4f47\u5217\u9805\u6b63\u5728\u8ffd\u8e64 rollback \u5be9\u8a08\u7d50\u679c\u3002") : "");
                        setTraceFilter(status === "approved" || status === "rejected" ? "reviewed" : "review");
                        if (item.latest_trace_id) setSelectedTraceId(item.latest_trace_id);
                    }}
                />
            )}
            {followUpSummaries.length > 0 && (
                <FollowUpSummaryRows
                    t={t}
                    items={followUpSummaries}
                    onOpen={(item) => {
                        setReviewFocusStatus("");
                        setActionFocusKind("");
                        setFollowUpFocusStatus(item.status || "");
                        setFollowUpFocusActionKind("");
                        setFollowUpTriggeredOnly(Boolean(item.triggered_rollback));
                        setFollowUpFocusReason(item.triggered_rollback ? String(item.recommended_reason || "").trim() : "");
                        setFollowUpRecommendedTraceId(item.triggered_rollback ? String(item.recommended_trace_id || item.latest_trace_id || "").trim() : "");
                        setPriorityTraceId(item.triggered_rollback ? String(item.recommended_trace_id || item.latest_trace_id || "").trim() : "");
                        setPriorityTraceReason(item.triggered_rollback ? String(item.recommended_reason || "").trim() : "");
                        setTraceFilter("followups");
                        if (item.recommended_trace_id || item.latest_trace_id) setSelectedTraceId(item.recommended_trace_id || item.latest_trace_id || "");
                    }}
                />
            )}
            {followUpActionSummaries.length > 0 && (
                <FollowUpActionSummaryRows
                    t={t}
                    items={followUpActionSummaries}
                    onOpen={(item) => {
                        setReviewFocusStatus("");
                        setActionFocusKind("");
                        setFollowUpFocusStatus("");
                        setFollowUpFocusActionKind(item.kind || "");
                        setFollowUpTriggeredOnly(Boolean(item.triggered_rollback));
                        setFollowUpFocusReason(item.triggered_rollback ? String(item.recommended_reason || "").trim() : "");
                        setFollowUpRecommendedTraceId(item.triggered_rollback ? String(item.recommended_trace_id || item.latest_trace_id || "").trim() : "");
                        setPriorityTraceId(item.triggered_rollback ? String(item.recommended_trace_id || item.latest_trace_id || "").trim() : "");
                        setPriorityTraceReason(item.triggered_rollback ? String(item.recommended_reason || "").trim() : "");
                        setTraceFilter("followups");
                        if (item.recommended_trace_id || item.latest_trace_id) setSelectedTraceId(item.recommended_trace_id || item.latest_trace_id || "");
                    }}
                />
            )}
            {hasSignals && (
                <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))", gap: 10 }}>
                    {hints.slice(0, 3).map((hint: any, idx: number) => (
                        <LearningSignalRow
                            key={"hint-" + (hint.context_key || idx)}
                            title={hint.context_key || t("Routing hint", "\u8def\u7531\u63d0\u793a", "\u8def\u7531\u63d0\u793a")}
                            meta={t("Prefer", "\u504f\u597d", "\u504f\u597d") + ": " + joinSignalList(hint.prefer_tools) + " | " + t("Avoid", "\u907f\u5f00", "\u907f\u958b") + ": " + joinSignalList(hint.avoid_tools) + " | " + t("Confidence", "\u7f6e\u4fe1\u5ea6", "\u4fe1\u5fc3") + ": " + formatConfidence(hint.confidence)}
                            detail={hint.description}
                        />
                    ))}
                    {nudges.slice(0, 3).map((nudge: any, idx: number) => (
                        <LearningSignalRow
                            key={"nudge-" + (nudge.context_key || idx)}
                            title={nudge.suggested_name || t("Skill candidate", "\u6280\u80fd\u5019\u9009", "\u6280\u80fd\u5019\u9078")}
                            meta={joinSignalList(nudge.tool_sequence, " -> ") + " | " + t("Success", "\u6210\u529f\u7387", "\u6210\u529f\u7387") + ": " + formatConfidence(nudge.success_rate) + " | " + t("Evidence", "\u8bc1\u636e", "\u8b49\u64da") + ": " + (nudge.evidence || 0)}
                            detail={nudge.description}
                        />
                    ))}
                    {patterns.slice(0, 2).map((pattern: any, idx: number) => (
                        <LearningSignalRow
                            key={"pattern-" + (pattern.tool_name || idx)}
                            title={pattern.tool_name || t("Usage pattern", "\u5de5\u5177\u6a21\u5f0f", "\u5de5\u5177\u6a21\u5f0f")}
                            meta={joinSignalList(pattern.top_tokens) + " | " + t("Success", "\u6210\u529f\u7387", "\u6210\u529f\u7387") + ": " + formatConfidence(pattern.success_rate) + " | " + t("Calls", "\u8c03\u7528", "\u547c\u53eb") + ": " + (pattern.count || 0)}
                            detail={pattern.description}
                        />
                    ))}
                    {recoveryPatterns.slice(0, 3).map((pattern: any, idx: number) => (
                        <LearningSignalRow
                            key={"recovery-" + (pattern.context_key || idx) + "-" + (pattern.failed_tool || "tool")}
                            title={(pattern.failed_tool || t("Tool", "\u5de5\u5177", "\u5de5\u5177")) + " -> " + (pattern.recovery_tool || t("Recovery", "\u6062\u590d", "\u6062\u5fa9"))}
                            meta={(pattern.error_class || "-") + " | " + t("Recovered", "\u6062\u590d\u7387", "\u6062\u5fa9\u7387") + ": " + formatConfidence(pattern.success_rate) + " | " + t("Evidence", "\u8bc1\u636e", "\u8b49\u64da") + ": " + (pattern.evidence || 0)}
                            detail={pattern.description}
                        />
                    ))}
                </div>
            )}
            {traceDetails.length > 0 && (
                <LearningTraceDetails
                    t={t}
                    details={visibleTraceDetails}
                    total={traceDetailCount}
                    counts={traceFilterCounts(traceDetails, traceKindCounts, reviewStatusCounts, nextActionKindCounts, followUpStatusCounts, traceDetailCount, reviewRequiredTraceCount, nextActionTraceCount, followUpTraceCount)}
                    filter={traceFilter}
                    reviewFocusStatus={reviewFocusStatus}
                    actionFocusKind={actionFocusKind}
                    followUpFocusStatus={followUpFocusStatus}
                    followUpTriggeredOnly={followUpTriggeredOnly}
                    followUpFocusReason={followUpFocusReason}
                    followUpRecommendedTraceId={followUpRecommendedTraceId}
                    safetyBoundary={String(governanceSummary?.non_executing_boundary || learning?.non_executing_boundary || "").trim()}
                    selected={selectedTrace}
                    selectedId={selectedTrace?.id || ""}
                    onFilterChange={(filter) => {
                        setReviewFocusStatus("");
                        setActionFocusKind("");
                        setFollowUpFocusStatus("");
                        setFollowUpFocusActionKind("");
                        setFollowUpTriggeredOnly(false);
                        setFollowUpFocusReason("");
                        setFollowUpRecommendedTraceId("");
                        setPriorityTraceId("");
                        setPriorityTraceReason("");
                        setTraceFilter(filter);
                    }}
                    onClearReviewFocus={() => { setReviewFocusStatus(""); setPriorityTraceId(""); setPriorityTraceReason(""); }}
                    onClearActionFocus={() => { setActionFocusKind(""); setPriorityTraceId(""); setPriorityTraceReason(""); }}
                    onClearFollowUpFocus={() => { setFollowUpFocusStatus(""); setFollowUpFocusReason(""); setFollowUpRecommendedTraceId(""); setPriorityTraceId(""); setPriorityTraceReason(""); }}
                    followUpFocusActionKind={followUpFocusActionKind}
                    priorityTraceId={priorityTraceId}
                    priorityTraceReason={priorityTraceReason}
                    onClearFollowUpActionFocus={() => { setFollowUpFocusActionKind(""); setFollowUpTriggeredOnly(false); setFollowUpFocusReason(""); setFollowUpRecommendedTraceId(""); setPriorityTraceId(""); setPriorityTraceReason(""); }}
                    onSelect={setSelectedTraceId}
                    onReviewed={onReviewed}
                />
            )}
        </div>
    );
}

function LearningStat({ label, value }: { label: string; value: number }) {
    return (
        <div className="memory-learning-stat" style={{ borderTop: "1px solid " + colors.borderLight }}>
            <div className="memory-learning-stat__value">{value}</div>
            <div className="memory-learning-stat__label">{label}</div>
        </div>
    );
}

function LearningSignalRow({ title, meta, detail }: { title: string; meta: string; detail?: string }) {
    const cleanDetail = typeof detail === "string" ? detail.trim() : "";
    return (
        <div style={{ borderTop: "1px solid " + colors.borderLight, paddingTop: 7, minWidth: 0 }}>
            <div style={{ fontSize: "0.72rem", fontWeight: 600, color: colors.text, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{title}</div>
            <div style={{ fontSize: "0.68rem", color: colors.textSecondary, marginTop: 2, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{meta}</div>
            {cleanDetail && (
                <details style={{ marginTop: 4, fontSize: "0.66rem", color: colors.textMuted }}>
                    <summary style={{ cursor: "pointer" }}>Details</summary>
                    <div style={{ marginTop: 3, lineHeight: 1.45, whiteSpace: "normal", overflowWrap: "anywhere" }}>{cleanDetail}</div>
                </details>
            )}
        </div>
    );
}

function GovernanceSummaryNotice({ t, summary, taskType, query, tool, previewActive, loading, error, triggeredRollbackFollowUpCount, triggeredRollbackFollowUpReason, recommendedTraceId, recommendedTraceTitle, recommendedTraceReason, maintenanceDrafting, routingDrafting, onTaskTypeChange, onQueryChange, onToolChange, onPreview, onClearPreview, onFocusAction, onDraftMaintenance, onDraftRoutingPreview }: { t: Translate; summary: GovernanceSummary; taskType: string; query: string; tool: string; previewActive: boolean; loading: boolean; error: string; triggeredRollbackFollowUpCount: number; triggeredRollbackFollowUpReason: string; recommendedTraceId: string; recommendedTraceTitle: string; recommendedTraceReason: string; maintenanceDrafting: boolean; routingDrafting: boolean; onTaskTypeChange: (value: string) => void; onQueryChange: (value: string) => void; onToolChange: (value: string) => void; onPreview: () => void; onClearPreview: () => void; onFocusAction?: (action: string, focus?: GovernanceFocus, reason?: string, focusContext?: GovernanceFocusContext | null) => void; onDraftMaintenance?: () => void; onDraftRoutingPreview?: () => void }) {
    const memory = asRecord(summary.memory);
    const routing = asRecord(summary.routing_self_evolution);
    const a2a = asRecord(summary.a2a_discussion);
    const queues = asRecord(summary.queues);
    const recommendedAction = String(summary.recommended_next_action || "").trim();
    const reason = String(summary.recommended_next_action_reason || "").trim();
    const boundary = String(summary.non_executing_boundary || "").trim();
    const recommendedToolCall = formatGovernanceRecommendedToolCall(asRecord(summary.recommended_tool_call), recommendedTraceId, recommendedTraceTitle, recommendedTraceReason);
    const memoryBoundary = String(memory.non_executing_boundary || "").trim();
    const memoryFocusContext = formatGovernanceFocusContext(asRecord(memory.recommended_focus_context));
    const memoryRecommendedToolCall = formatGovernanceRecommendedToolCall(asRecord(memory.recommended_tool_call));
    const a2aBoundary = String(a2a.non_executing_boundary || "").trim();
    const a2aFocusContext = formatGovernanceFocusContext(asRecord(a2a.recommended_focus_context));
    const a2aRecommendedToolCall = formatGovernanceRecommendedToolCall(asRecord(a2a.recommended_tool_call));
    const routingQuery = asRecord(routing.query);
    const toolRecoveryGovernance = asRecord(routing.tool_recovery_governance);
    const toolRecoveryCount = toSafeCount(toolRecoveryGovernance.count, 0);
    const toolRecoveryReviewCount = toSafeCount(toolRecoveryGovernance.review_required_count, 0);
    const toolRecoveryDisabledCount = toSafeCount(toolRecoveryGovernance.disabled_count, 0);
    const routingCandidates = Array.isArray(routing.tool_candidates) ? routing.tool_candidates : [];
    const routingRecommendation = String(routing.recommendation || routing.routing_recommendation || "").trim();
    const routingBoundary = String(routing.non_executing_boundary || "").trim();
    const routingFocusContext = formatGovernanceFocusContext(asRecord(routing.recommended_focus_context));
    const routingRecommendedToolCall = formatGovernanceRecommendedToolCall(asRecord(routing.recommended_tool_call));
    const hasRoutingPreview = previewActive || routingCandidates.length > 0 || routingRecommendation || routingQuery.query || routingQuery.tool || routingQuery.task_type;
    const canDraftMaintenance = recommendedAction === "build_memory_maintenance_draft" || Boolean(memory.layered_memory_recommended);
    const triggeredRollbackReview = recommendedAction === "review_triggered_rollback_signal" || recommendedAction === "inspect_triggered_rollback_followups";
    const tracks = [
        {
            title: t("Memory", "\u8bb0\u5fc6", "\u8a18\u61b6"),
            meta: governanceTrackLine([
                { label: t("Protected", "\u4fdd\u62a4", "\u4fdd\u8b77"), value: memory.protected_memory_count },
                { label: t("Active", "\u6d3b\u8dc3", "\u6d3b\u8e8d"), value: memory.active_entries },
            ]),
            detail: Boolean(memory.layered_memory_recommended) ? t("Layered maintenance recommended", "\u5efa\u8bae\u5206\u5c42\u7ef4\u62a4", "\u5efa\u8b70\u5206\u5c64\u7dad\u8b77") : t("Normal maintenance posture", "\u5e38\u89c4\u7ef4\u62a4\u59ff\u6001", "\u5e38\u898f\u7dad\u8b77\u72c0\u614b"),
        },
        {
            title: t("Routing", "\u8def\u7531", "\u8def\u7531"),
            meta: governanceTrackLine([
                { label: t("Hints", "\u63d0\u793a", "\u63d0\u793a"), value: routing.routing_hint_count },
                { label: t("Skills", "\u6280\u80fd", "\u6280\u80fd"), value: routing.skill_nudge_count },
                { label: t("Recoveries", "\u6062\u590d", "\u6062\u5fa9"), value: routing.recovery_pattern_count },
                { label: t("Usage", "\u7528\u6cd5", "\u7528\u6cd5"), value: routing.usage_pattern_count },
                { label: t("Failures", "\u5931\u8d25", "\u5931\u6557"), value: toolRecoveryCount },
                { label: t("Review", "\u8bc4\u5ba1", "\u8a55\u5be9"), value: toolRecoveryReviewCount },
            ]),
            detail: toolRecoveryCount > 0
                ? t("Tool recovery windows are visible without changing routing or retry policy.", "\u5de5\u5177\u6062\u590d\u7a97\u53e3\u53ef\u89c1\uff0c\u4e0d\u4f1a\u6539\u53d8\u8def\u7531\u6216\u91cd\u8bd5\u7b56\u7565\u3002", "\u5de5\u5177\u6062\u5fa9\u7a97\u53e3\u53ef\u898b\uff0c\u4e0d\u6703\u6539\u8b8a\u8def\u7531\u6216\u91cd\u8a66\u7b56\u7565\u3002")
                : t("Self-evolution evidence remains review-gated.", "\u81ea\u8fdb\u5316\u8bc1\u636e\u4ecd\u7531\u8bc4\u5ba1\u95e8\u63a7\u3002", "\u81ea\u9032\u5316\u8b49\u64da\u4ecd\u7531\u8a55\u5be9\u95dc\u5361\u3002"),
        },
        {
            title: "A2A",
            meta: governanceTrackLine([
                { label: t("Results", "\u7ed3\u679c", "\u7d50\u679c"), value: a2a.discussion_results },
                { label: t("Conflicts", "\u51b2\u7a81", "\u885d\u7a81"), value: a2a.conflict_reviews },
                { label: t("Rollback", "\u56de\u6eda", "\u56de\u6efe"), value: a2a.rollback_reviews },
                { label: t("Escalations", "\u5347\u7ea7", "\u5347\u7d1a"), value: a2a.escalation_evidence },
            ]),
            detail: t("Discussion decisions, rollback, and escalation stay auditable.", "\u8ba8\u8bba\u51b3\u7b56\u3001\u56de\u6eda\u548c\u5347\u7ea7\u4fdd\u6301\u53ef\u5ba1\u8ba1\u3002", "\u8a0e\u8ad6\u6c7a\u7b56\u3001\u56de\u6efe\u548c\u5347\u7d1a\u4fdd\u6301\u53ef\u5be9\u8a08\u3002"),
        },
        {
            title: t("Queues", "\u961f\u5217", "\u961f\u5217"),
            meta: governanceTrackLine([
                { label: t("Review", "\u8bc4\u5ba1", "\u8a55\u5be9"), value: a2a.review_required_trace_count },
                { label: t("Actions", "\u52a8\u4f5c", "\u52d5\u4f5c"), value: queues.next_action_trace_count },
                { label: t("Follow-ups", "\u540e\u7eed", "\u5f8c\u7e8c"), value: queues.follow_up_trace_count },
                { label: t("Rollback audit", "\u56de\u6eda\u5ba1\u8ba1", "\u56de\u6efe\u5be9\u8a08"), value: triggeredRollbackFollowUpCount },
                { label: t("Traces", "\u8f68\u8ff9", "\u8ecc\u8de1"), value: queues.trace_detail_count },
            ]),
            detail: triggeredRollbackFollowUpCount > 0
                ? t("Rollback audit follow-ups are active in the queue.", "\u961f\u5217\u4e2d\u5b58\u5728\u6d3b\u8dc3\u7684 rollback \u5ba1\u8ba1\u540e\u7eed\u3002", "\u4f47\u5217\u4e2d\u5b58\u5728\u6d3b\u8e8d\u7684 rollback \u5be9\u8a08\u5f8c\u7e8c\u3002") + (triggeredRollbackFollowUpReason ? " " + triggeredRollbackFollowUpReason : "")
                : t("Queues are inspectable without approving or executing work.", "\u961f\u5217\u53ef\u68c0\u89c6\uff0c\u4e0d\u4f1a\u6279\u51c6\u6216\u6267\u884c\u5de5\u4f5c\u3002", "\u961f\u5217\u53ef\u6aa2\u8996\uff0c\u4e0d\u6703\u6279\u51c6\u6216\u57f7\u884c\u5de5\u4f5c\u3002"),
        },
    ];
    return (
        <div className="memory-governance-panel" style={triggeredRollbackReview ? governanceNoticeAlertStyle : governanceNoticeStyle}>
            <div className="memory-governance-panel__header">
                <span className="memory-governance-panel__title">{t("Governance Summary", "\u6cbb\u7406\u6458\u8981", "\u6cbb\u7406\u6458\u8981")}</span>
                {recommendedAction && (
                    <span className="memory-governance-panel__actions">
                        {previewActive && <span style={neutralBadgeStyle}>{t("Preview", "\u9884\u89c8", "\u9810\u89bd")}</span>}
                        {triggeredRollbackReview && <span style={warningBadgeStyle}>{t("Triggered", "\u5df2\u89e6\u53d1", "\u5df2\u89f8\u767c")}</span>}
                        {canDraftMaintenance && (
                            <button type="button" disabled={maintenanceDrafting} onClick={onDraftMaintenance} style={followUpButtonStyle(maintenanceDrafting)}>
                                {maintenanceDrafting ? t("Drafting...", "\u751f\u6210\u4e2d...", "\u751f\u6210\u4e2d...") : t("Draft Plan", "\u751f\u6210\u8349\u6848", "\u751f\u6210\u8349\u6848")}
                            </button>
                        )}
                        <button type="button" title={recommendedTraceTitle || recommendedTraceId || undefined} onClick={() => onFocusAction?.(recommendedAction, summary.recommended_focus, reason, summary.recommended_focus_context)} style={governanceFocusButtonStyle}>{t("Focus", "\u805a\u7126", "\u805a\u7126")}</button>
                        <span style={triggeredRollbackReview ? warningBadgeStyle : kindBadgeStyle}>{formatNextActionKind(t, recommendedAction)}</span>
                    </span>
                )}
            </div>
            {reason && <div style={maintenanceTextStyle}>{reason}</div>}
            {recommendedTraceId && (
                <div style={governancePriorityTraceStyle}>
                    <div style={{ fontSize: "0.66rem", fontWeight: 700, color: colors.text }}>
                        {t("Priority Trace", "\u4f18\u5148\u8f68\u8ff9", "\u512a\u5148\u8ecc\u8de1")}
                    </div>
                    <div style={{ fontSize: "0.66rem", color: colors.textSecondary, marginTop: 2, lineHeight: 1.45, overflowWrap: "anywhere" }}>
                        {recommendedTraceTitle || recommendedTraceId}
                        {recommendedTraceTitle && recommendedTraceId ? " (" + recommendedTraceId + ")" : ""}
                    </div>
                    {recommendedTraceReason && (
                        <div style={{ fontSize: "0.64rem", color: colors.textMuted, marginTop: 2, lineHeight: 1.4, overflowWrap: "anywhere" }}>
                            <span style={{ fontWeight: 700 }}>{t("Why this trace", "\u4e3a\u4ec0\u4e48\u662f\u8fd9\u6761\u8f68\u8ff9", "\u70ba\u4ec0\u9ebc\u662f\u9019\u689d\u8ecc\u8de1")}: </span>
                            <span>{recommendedTraceReason}</span>
                        </div>
                    )}
                </div>
            )}
            {toolRecoveryCount > 0 && (
                <div style={maintenanceMetaStyle}>
                    {t("Tool recovery governance", "\u5de5\u5177\u6062\u590d\u6cbb\u7406", "\u5de5\u5177\u6062\u5fa9\u6cbb\u7406")}: {governanceTrackLine([
                        { label: t("Windows", "\u7a97\u53e3", "\u7a97\u53e3"), value: toolRecoveryCount },
                        { label: t("Review", "\u8bc4\u5ba1", "\u8a55\u5be9"), value: toolRecoveryReviewCount },
                        { label: t("Disabled", "\u7981\u7528", "\u505c\u7528"), value: toolRecoveryDisabledCount },
                    ])}
                </div>
            )}
            {triggeredRollbackReview && (
                <div style={maintenanceMetaStyle}>
                    {t("Current A2A evidence already matches rollback conditions. Review the signal before drafting or reusing any rollback workflow.", "\u5f53\u524d A2A \u8bc1\u636e\u5df2\u547d\u4e2d rollback \u6761\u4ef6\uff0c\u8bf7\u5148\u590d\u6838\u8be5\u4fe1\u53f7\uff0c\u518d\u5904\u7406 rollback \u8349\u6848\u6216\u590d\u7528\u6d41\u7a0b\u3002", "\u7576\u524d A2A \u8b49\u64da\u5df2\u547d\u4e2d rollback \u689d\u4ef6\uff0c\u8acb\u5148\u8907\u6838\u6b64\u8a0a\u865f\uff0c\u518d\u8655\u7406 rollback \u8349\u6848\u6216\u8907\u7528\u6d41\u7a0b\u3002")}
                </div>
            )}
            <div style={governanceQueryRowStyle}>
                <input
                    value={taskType}
                    onChange={(event) => onTaskTypeChange(event.target.value)}
                    placeholder={t("Task type", "\u4efb\u52a1\u7c7b\u578b", "\u4efb\u52d9\u985e\u578b")}
                    disabled={loading}
                    style={governanceToolInputStyle}
                    aria-label={t("Governance task type filter", "\u6cbb\u7406\u4efb\u52a1\u7c7b\u578b\u8fc7\u6ee4", "\u6cbb\u7406\u4efb\u52d9\u985e\u578b\u904e\u6ffe")}
                />
                <input
                    value={query}
                    onChange={(event) => onQueryChange(event.target.value)}
                    placeholder={t("Task or question for routing evidence", "\u7528\u4e8e\u8def\u7531\u8bc1\u636e\u7684\u4efb\u52a1\u6216\u95ee\u9898", "\u7528\u65bc\u8def\u7531\u8b49\u64da\u7684\u4efb\u52d9\u6216\u554f\u984c")}
                    disabled={loading}
                    style={governanceQueryInputStyle}
                    aria-label={t("Governance query", "\u6cbb\u7406\u67e5\u8be2", "\u6cbb\u7406\u67e5\u8a62")}
                />
                <input
                    value={tool}
                    onChange={(event) => onToolChange(event.target.value)}
                    placeholder={t("Optional tool", "\u53ef\u9009\u5de5\u5177", "\u53ef\u9078\u5de5\u5177")}
                    disabled={loading}
                    style={governanceToolInputStyle}
                    aria-label={t("Governance tool filter", "\u6cbb\u7406\u5de5\u5177\u8fc7\u6ee4", "\u6cbb\u7406\u5de5\u5177\u904e\u6ffe")}
                />
                <button type="button" disabled={loading} onClick={onPreview} style={followUpButtonStyle(loading)}>{loading ? t("Previewing...", "\u9884\u89c8\u4e2d...", "\u9810\u89bd\u4e2d...") : t("Preview", "\u9884\u89c8", "\u9810\u89bd")}</button>
                {previewActive && <button type="button" disabled={loading} onClick={onClearPreview} style={governanceFocusButtonStyle}>{t("Clear", "\u6e05\u9664", "\u6e05\u9664")}</button>}
            </div>
            {error && <div role="alert" style={{ color: colors.danger, fontSize: "0.66rem", marginTop: 5 }}>{error}</div>}
            <div style={governanceTrackGridStyle}>
                {tracks.map((track) => (
                    <div key={track.title} style={governanceTrackStyle}>
                        <div style={{ fontSize: "0.68rem", fontWeight: 700, color: colors.text }}>{track.title}</div>
                        <div style={{ fontSize: "0.66rem", color: colors.textSecondary, marginTop: 2, lineHeight: 1.45 }}>{track.meta}</div>
                        <div style={{ fontSize: "0.64rem", color: colors.textMuted, marginTop: 2, lineHeight: 1.4 }}>{track.detail}</div>
                    </div>
                ))}
            </div>
            <GovernanceHandoffBlock t={t} title={t("Memory Handoff", "\u8bb0\u5fc6\u4ea4\u63a5", "\u8a18\u61b6\u4ea4\u63a5")} focusContext={memoryFocusContext} recommendedToolCall={memoryRecommendedToolCall} boundary={memoryBoundary} />
            <GovernanceHandoffBlock t={t} title={t("A2A Handoff", "A2A \u4ea4\u63a5", "A2A \u4ea4\u63a5")} focusContext={a2aFocusContext} recommendedToolCall={a2aRecommendedToolCall} boundary={a2aBoundary} />
            {hasRoutingPreview && (
                <div style={governanceRoutingPreviewStyle}>
                    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8, marginBottom: 4 }}>
                        <div style={{ fontSize: "0.66rem", color: colors.textMuted, fontWeight: 700 }}>{t("Routing Evidence Preview", "\u8def\u7531\u8bc1\u636e\u9884\u89c8", "\u8def\u7531\u8b49\u64da\u9810\u89bd")}</div>
                        {routingCandidates.length > 0 && (
                            <button type="button" disabled={routingDrafting} onClick={onDraftRoutingPreview} style={followUpButtonStyle(routingDrafting)}>
                                {routingDrafting ? t("Drafting...", "\u751f\u6210\u4e2d...", "\u751f\u6210\u4e2d...") : t("Draft Review", "\u751f\u6210\u5ba1\u67e5\u8349\u6848", "\u751f\u6210\u5be9\u67e5\u8349\u6848")}
                            </button>
                        )}
                    </div>
                    {(routingQuery.query || routingQuery.tool || routingQuery.task_type) && (
                        <div style={maintenanceMetaStyle}>
                            {governanceRoutingQueryLine(t, routingQuery)}
                        </div>
                    )}
                    {routingRecommendation && <div style={maintenanceTextStyle}>{routingRecommendation}</div>}
                    <GovernanceHandoffBlock t={t} title="" focusContext={routingFocusContext} recommendedToolCall={routingRecommendedToolCall} boundary={routingBoundary} compact />
                    {routingCandidates.length > 0 ? (
                        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))", gap: 6, marginTop: 6 }}>
                            {routingCandidates.slice(0, 4).map((candidate: any, index: number) => (
                                <div key={(candidate.tool_name || "tool") + "-" + index} style={protectedMemoryCardStyle}>
                                    <span style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 6 }}>
                                        <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{candidate.tool_name || "-"}</span>
                                        <span style={{ color: colors.textMuted }}>{formatRoutingAdjustment(candidate.adjustment)}</span>
                                    </span>
                                    <span style={{ display: "block", marginTop: 2, color: colors.textMuted, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{candidate.direction || "-"}</span>
                                    {Array.isArray(candidate.reasons) && candidate.reasons.length > 0 && <span style={{ display: "block", marginTop: 1, color: colors.textMuted, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{candidate.reasons.join(", ")}</span>}
                                </div>
                            ))}
                        </div>
                    ) : (
                        <div style={maintenanceMetaStyle}>{t("No bounded tool candidate crossed the reporting threshold.", "\u6ca1\u6709\u5de5\u5177\u5019\u9009\u8fbe\u5230\u6709\u754c\u62a5\u544a\u9608\u503c\u3002", "\u6c92\u6709\u5de5\u5177\u5019\u9078\u9054\u5230\u6709\u754c\u5831\u544a\u95be\u503c\u3002")}</div>
                    )}
                </div>
            )}
            {recommendedToolCall && <DetailBlock label={t("Recommended Tool Call", "\u5efa\u8bae\u5de5\u5177\u8c03\u7528", "\u5efa\u8b70\u5de5\u5177\u8abf\u7528")} value={recommendedToolCall} copyValueText={recommendedToolCall} pre {...draftCopyProps(t)} />}
            {boundary && <div style={governanceBoundaryStyle}>{boundary}</div>}
        </div>
    );
}

function MemoryMaintenanceNotice({ t, layered, recommendation, reason, boundary, draft, draftQuery, draftLoading, draftError, reviewNote, reviewRecording, reviewError, reviewMessage, reviewToolCall, onDraftQueryChange, onDraft, onReviewNoteChange, onRecordReview }: { t: Translate; layered: boolean; recommendation: string; reason: string; boundary: string; draft: ExperienceLearningDraft | null; draftQuery: string; draftLoading: boolean; draftError: string; reviewNote: string; reviewRecording: DraftReviewStatus | ""; reviewError: string; reviewMessage: string; reviewToolCall?: GovernanceToolCall | null; onDraftQueryChange: (value: string) => void; onDraft: () => void; onReviewNoteChange: (value: string) => void; onRecordReview: (status: DraftReviewStatus) => void }) {
    return (
        <div style={memoryMaintenanceNoticeStyle}>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8, marginBottom: 4 }}>
                <span style={{ fontSize: "0.68rem", color: colors.textMuted, fontWeight: 700 }}>{t("Memory Maintenance", "\u8bb0\u5fc6\u7ef4\u62a4", "\u8a18\u61b6\u7dad\u8b77")}</span>
                <span style={{ display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap", justifyContent: "flex-end" }}>
                    <button type="button" disabled={draftLoading} onClick={onDraft} style={followUpButtonStyle(draftLoading)}>{draftLoading ? t("Drafting...", "\u751f\u6210\u4e2d...", "\u751f\u6210\u4e2d...") : t("Draft Plan", "\u751f\u6210\u8349\u6848", "\u751f\u6210\u8349\u6848")}</button>
                    <span style={layered ? warningBadgeStyle : neutralBadgeStyle}>{layered ? t("Layered", "\u5206\u5c42", "\u5206\u5c64") : t("Normal", "\u5e38\u89c4", "\u5e38\u898f")}</span>
                </span>
            </div>
            {recommendation && <div style={maintenanceTextStyle}>{recommendation}</div>}
            {reason && <div style={maintenanceMetaStyle}>{t("Reason", "\u539f\u56e0", "\u539f\u56e0")}: {reason}</div>}
            {boundary && <div style={maintenanceMetaStyle}>{boundary}</div>}
            <input value={draftQuery} onChange={(event) => onDraftQueryChange(event.target.value)} placeholder={t("Optional anchor filter", "\u53ef\u9009\u4fdd\u7559\u951a\u70b9\u8fc7\u6ee4", "\u53ef\u9078\u4fdd\u7559\u9328\u9ede\u904e\u6ffe")} disabled={draftLoading} style={draftQueryInputStyle} aria-label={t("Memory draft query", "\u8bb0\u5fc6\u8349\u6848\u67e5\u8be2", "\u8a18\u61b6\u8349\u6848\u67e5\u8a62")} />
            <DraftOutput t={t} label={t("Maintenance Draft", "\u7ef4\u62a4\u8349\u6848", "\u7dad\u8b77\u8349\u6848")} draft={draft} error={draftError} />
            <DraftReviewControls t={t} draft={draft} note={reviewNote} recording={reviewRecording} error={reviewError} message={reviewMessage} recommendedToolCall={reviewToolCall} onNoteChange={onReviewNoteChange} onRecord={onRecordReview} />
        </div>
    );
}

function RoutingDraftNotice({ t, draft, query, loading, error, reviewNote, reviewRecording, reviewError, reviewMessage, reviewToolCall, onQueryChange, onDraft, onReviewNoteChange, onRecordReview }: { t: Translate; draft: ExperienceLearningDraft | null; query: string; loading: boolean; error: string; reviewNote: string; reviewRecording: DraftReviewStatus | ""; reviewError: string; reviewMessage: string; reviewToolCall?: GovernanceToolCall | null; onQueryChange: (value: string) => void; onDraft: () => void; onReviewNoteChange: (value: string) => void; onRecordReview: (status: DraftReviewStatus) => void }) {
    return (
        <div style={memoryMaintenanceNoticeStyle}>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8, marginBottom: 4 }}>
                <span style={{ fontSize: "0.68rem", color: colors.textMuted, fontWeight: 700 }}>{t("Routing Adjustment Draft", "\u8def\u7531\u8c03\u6574\u8349\u6848", "\u8def\u7531\u8abf\u6574\u8349\u6848")}</span>
                <button type="button" disabled={loading} onClick={onDraft} style={followUpButtonStyle(loading)}>{loading ? t("Drafting...", "\u751f\u6210\u4e2d...", "\u751f\u6210\u4e2d...") : t("Draft Plan", "\u751f\u6210\u8349\u6848", "\u751f\u6210\u8349\u6848")}</button>
            </div>
            <div style={maintenanceTextStyle}>{t("Generate a review-only routing adjustment from distilled hints, recoveries, skills, and usage evidence.", "\u4ece\u8def\u7531\u63d0\u793a\u3001\u6062\u590d\u3001\u6280\u80fd\u548c\u7528\u6cd5\u8bc1\u636e\u751f\u6210\u53ea\u8bfb\u8c03\u6574\u8349\u6848\u3002", "\u5f9e\u8def\u7531\u63d0\u793a\u3001\u6062\u5fa9\u3001\u6280\u80fd\u548c\u7528\u6cd5\u8b49\u64da\u751f\u6210\u552f\u8b80\u8abf\u6574\u8349\u6848\u3002")}</div>
            <input value={query} onChange={(event) => onQueryChange(event.target.value)} placeholder={t("Optional task or tool filter", "\u53ef\u9009\u4efb\u52a1\u6216\u5de5\u5177\u8fc7\u6ee4", "\u53ef\u9078\u4efb\u52d9\u6216\u5de5\u5177\u904e\u6ffe")} disabled={loading} style={draftQueryInputStyle} aria-label={t("Routing draft query", "\u8def\u7531\u8349\u6848\u67e5\u8be2", "\u8def\u7531\u8349\u6848\u67e5\u8a62")} />
            <DraftOutput t={t} label={t("Routing Draft", "\u8def\u7531\u8349\u6848", "\u8def\u7531\u8349\u6848")} draft={draft} error={error} />
            <DraftReviewControls t={t} draft={draft} note={reviewNote} recording={reviewRecording} error={reviewError} message={reviewMessage} recommendedToolCall={reviewToolCall} onNoteChange={onReviewNoteChange} onRecord={onRecordReview} />
        </div>
    );
}

function GovernanceHandoffBlock({ t, title, focusContext, recommendedToolCall, boundary, compact }: { t: Translate; title: string; focusContext: string; recommendedToolCall: string; boundary: string; compact?: boolean }) {
    if (!focusContext && !recommendedToolCall && !boundary) return null;
    return (
        <div style={compact ? undefined : governanceRoutingPreviewStyle}>
            {title && <div style={{ fontSize: "0.66rem", color: colors.textMuted, fontWeight: 700, marginBottom: 4 }}>{title}</div>}
            {focusContext && <DetailBlock label={t("Focus Context", "\u805a\u7126\u4e0a\u4e0b\u6587", "\u805a\u7126\u4e0a\u4e0b\u6587")} value={focusContext} copyValueText={focusContext} pre {...draftCopyProps(t)} />}
            {recommendedToolCall && <DetailBlock label={t("Recommended Tool Call", "\u5efa\u8bae\u5de5\u5177\u8c03\u7528", "\u5efa\u8b70\u5de5\u5177\u8abf\u7528")} value={recommendedToolCall} copyValueText={recommendedToolCall} pre {...draftCopyProps(t)} />}
            {boundary && <DetailBlock label={t("Safety Boundary", "\u5b89\u5168\u8fb9\u754c", "\u5b89\u5168\u908a\u754c")} value={boundary} />}
        </div>
    );
}

function DraftOutput({ t, label, draft, error }: { t: Translate; label: string; draft: ExperienceLearningDraft | null; error: string }) {
    const context = draft?.recommended_focus_context || null;
    const contextTrace = draftSourceTraceId(context);
    const contextReason = String(context?.reason || "").trim();
    const contextTitle = String(context?.priority_trace_title || "").trim();
    const contextLine = [contextTitle || contextTrace, contextTitle && contextTrace ? contextTrace : "", contextReason].filter(Boolean).join(" | ");
    return (
        <>
            {error && <div role="alert" style={{ fontSize: "0.66rem", color: colors.danger, marginTop: 5 }}>{error}</div>}
            {contextLine && <DetailBlock label={t("Priority Trace", "\u4f18\u5148\u8f68\u8ff9", "\u512a\u5148\u8ecc\u8de1")} value={contextLine} />}
            {draft?.draft_markdown && <DetailBlock label={label} value={draft.draft_markdown} pre monospace {...draftCopyProps(t)} />}
            {draft?.non_executing_boundary && <DetailBlock label={t("Safety Boundary", "\u5b89\u5168\u8fb9\u754c", "\u5b89\u5168\u908a\u754c")} value={draft.non_executing_boundary} />}
            {draft?.recommended_tool_call && <RecommendedToolCallBlock t={t} call={draft.recommended_tool_call} />}
        </>
    );
}

function RecommendedToolCallBlock({ t, call }: { t: Translate; call?: GovernanceToolCall | null }) {
    const text = formatGovernanceRecommendedToolCall(asRecord(call));
    return text ? <DetailBlock label={t("Recommended Tool Call", "\u5efa\u8bae\u5de5\u5177\u8c03\u7528", "\u5efa\u8b70\u5de5\u5177\u8abf\u7528")} value={text} copyValueText={text} pre {...draftCopyProps(t)} /> : null;
}

function DraftReviewControls({ t, draft, note, recording, error, message, recommendedToolCall, notePlaceholder, onNoteChange, onRecord }: DraftReviewControlsProps) {
    if (!draft?.draft_markdown) return null;
    const busy = Boolean(recording);
    const buttonLabel = (status: DraftReviewStatus, label: string) => recording === status ? t("Recording...", "\u8bb0\u5f55\u4e2d...", "\u8a18\u9304\u4e2d...") : label;
    return (
        <div style={reviewPanelStyle}>
            <div style={{ fontSize: "0.66rem", color: colors.textMuted, fontWeight: 700, marginBottom: 5 }}>{t("Draft review note", "\u8349\u6848\u5ba1\u9605\u5907\u6ce8", "\u8349\u6848\u5be9\u95b1\u5099\u8a3b")}</div>
            <textarea
                value={note}
                onChange={(event) => onNoteChange(event.target.value)}
                placeholder={notePlaceholder || t("Record manual decision or blocker", "\u8bb0\u5f55\u4eba\u5de5\u51b3\u5b9a\u6216\u963b\u585e\u70b9", "\u8a18\u9304\u4eba\u5de5\u6c7a\u5b9a\u6216\u963b\u585e\u9ede")}
                disabled={busy}
                style={reviewTextareaStyle}
            />
            <div style={reviewButtonRowStyle}>
                <button type="button" disabled={busy} onClick={() => onRecord("completed")} style={followUpOutcomeButtonStyle("completed", recording === "completed")}>{buttonLabel("completed", t("Done", "\u5b8c\u6210", "\u5b8c\u6210"))}</button>
                <button type="button" disabled={busy} onClick={() => onRecord("blocked")} style={followUpOutcomeButtonStyle("blocked", recording === "blocked")}>{buttonLabel("blocked", t("Blocked", "\u963b\u585e", "\u963b\u585e"))}</button>
                <button type="button" disabled={busy} onClick={() => onRecord("deferred")} style={followUpOutcomeButtonStyle("deferred", recording === "deferred")}>{buttonLabel("deferred", t("Defer", "\u5ef6\u540e", "\u5ef6\u5f8c"))}</button>
            </div>
            {error && <div role="alert" style={{ color: colors.danger, fontSize: "0.66rem", marginTop: 5 }}>{error}</div>}
            {message && <div style={{ color: colors.success, fontSize: "0.66rem", marginTop: 5 }}>{message}</div>}
            {recommendedToolCall && <RecommendedToolCallBlock t={t} call={recommendedToolCall} />}
        </div>
    );
}

function ReviewSummaryRows({ t, items, onOpen }: { t: Translate; items: ReviewSummary[]; onOpen: (item: ReviewSummary) => void }) {
    const rankedItems = rankReviewSummaries(items).slice(0, 4);
    return (
        <div style={{ borderTop: "1px solid " + colors.borderLight, paddingTop: 8, marginBottom: 10 }}>
            <div style={{ fontSize: "0.68rem", color: colors.textMuted, fontWeight: 700, marginBottom: 6 }}>{t("Review Queue", "\u5ba1\u9605\u961f\u5217", "\u5be9\u95b1\u961f\u5217")}</div>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(190px, 1fr))", gap: 6 }}>
                {rankedItems.map((item) => (
                    <button key={item.status || item.latest_trace_id || "review"} type="button" onClick={() => onOpen(item)} style={reviewSummaryCardStyle(item)} title={item.latest_action || item.latest_note || item.latest_title || item.status}>
                        <span style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 6 }}>
                            <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{formatReviewStatus(t, item.status) || item.status || "-"}</span>
                            <span style={{ color: colors.textMuted, fontVariantNumeric: "tabular-nums" }}>{toSafeCount(item.count, 0)}</span>
                        </span>
                        <span style={{ display: "block", marginTop: 2, color: colors.textMuted, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{item.latest_title || item.latest_action || "-"}</span>
                        {item.latest_kind && <span style={{ display: "block", marginTop: 1, color: colors.textMuted, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontWeight: 600 }}>{formatKind(t, item.latest_kind)}</span>}
                        {reviewSummaryIsTriggeredRollback(item) && <span style={summaryAuditLineStyle}>{t("Rollback audit review", "\u56de\u6eda\u5ba1\u8ba1\u590d\u6838", "\u56de\u6efe\u5be9\u8a08\u8907\u6838")}</span>}
                    </button>
                ))}
            </div>
        </div>
    );
}

function ProtectedMemoryRows({ t, items }: { t: Translate; items: ProtectedCandidate[] }) {
    return (
        <div style={{ borderTop: "1px solid " + colors.borderLight, paddingTop: 8, marginBottom: 10 }}>
            <div style={{ fontSize: "0.68rem", color: colors.textMuted, fontWeight: 700, marginBottom: 6 }}>{t("Protected Memory", "\u4fdd\u62a4\u8bb0\u5fc6", "\u4fdd\u8b77\u8a18\u61b6")}</div>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(190px, 1fr))", gap: 6 }}>
                {items.slice(0, 4).map((item) => (
                    <div key={item.id || item.title || item.reason || "protected"} style={protectedMemoryCardStyle} title={item.summary || item.title || item.reason}>
                        <span style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 6 }}>
                            <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{item.title || item.id || "-"}</span>
                            <span style={{ color: colors.textMuted, fontVariantNumeric: "tabular-nums" }}>{formatProtectedReason(t, item.reason)}</span>
                        </span>
                        <span style={{ display: "block", marginTop: 2, color: colors.textMuted, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{item.summary || item.source || "-"}</span>
                        <span style={{ display: "block", marginTop: 1, color: colors.textMuted, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontWeight: 600 }}>
                            {formatProtectedSource(t, item.source)}{item.pinned ? " / " + t("pinned", "\u5df2\u56fa\u5b9a", "\u5df2\u56fa\u5b9a") : ""}
                        </span>
                    </div>
                ))}
            </div>
        </div>
    );
}

function NextActionSummaryRows({ t, items, onOpen }: { t: Translate; items: NextActionSummary[]; onOpen: (item: NextActionSummary) => void }) {
    const rankedItems = rankNextActionSummaries(items).slice(0, 4);
    return (
        <div style={{ borderTop: "1px solid " + colors.borderLight, paddingTop: 8, marginBottom: 10 }}>
            <div style={{ fontSize: "0.68rem", color: colors.textMuted, fontWeight: 700, marginBottom: 6 }}>{t("Action Queue", "\u52a8\u4f5c\u961f\u5217", "\u52d5\u4f5c\u961f\u5217")}</div>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(190px, 1fr))", gap: 6 }}>
                {rankedItems.map((item) => (
                    <button key={item.kind || item.latest_trace_id || "action"} type="button" onClick={() => onOpen(item)} style={nextActionSummaryCardStyle(item.kind)} title={item.latest_action || item.latest_title || item.kind}>
                        <span style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 6 }}>
                            <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{formatNextActionKind(t, item.kind)}</span>
                            <span style={{ color: colors.textMuted, fontVariantNumeric: "tabular-nums" }}>{toSafeCount(item.count, 0)}</span>
                        </span>
                        <span style={{ display: "block", marginTop: 2, color: colors.textMuted, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{item.latest_title || item.latest_action || "-"}</span>
                        {item.kind === "review_triggered_rollback_signal" && <span style={summaryAuditLineStyle}>{t("Rollback audit review", "\u56de\u6eda\u5ba1\u8ba1\u590d\u6838", "\u56de\u6efe\u5be9\u8a08\u8907\u6838")}</span>}
                    </button>
                ))}
            </div>
        </div>
    );
}

function FollowUpSummaryRows({ t, items, onOpen }: { t: Translate; items: FollowUpSummary[]; onOpen: (item: FollowUpSummary) => void }) {
    const rankedItems = rankFollowUpSummaries(items).slice(0, 4);
    return (
        <div style={{ borderTop: "1px solid " + colors.borderLight, paddingTop: 8, marginBottom: 10 }}>
            <div style={{ fontSize: "0.68rem", color: colors.textMuted, fontWeight: 700, marginBottom: 6 }}>{t("Follow-up Log", "\u540e\u7eed\u8bb0\u5f55", "\u5f8c\u7e8c\u8a18\u9304")}</div>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(190px, 1fr))", gap: 6 }}>
                {rankedItems.map((item) => (
                    <button key={item.status || item.latest_trace_id || "followup"} type="button" onClick={() => onOpen(item)} style={item.triggered_rollback ? warningSummaryCardStyle : nextActionSummaryStyle} title={item.recommended_reason || item.latest_note || item.latest_title || item.status}>
                        <span style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 6 }}>
                            <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{formatFollowUpStatus(t, item.status) || item.status || "-"}</span>
                            <span style={{ color: colors.textMuted, fontVariantNumeric: "tabular-nums" }}>{toSafeCount(item.count, 0)}</span>
                        </span>
                        <span style={{ display: "block", marginTop: 2, color: colors.textMuted, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{item.latest_title || item.latest_note || "-"}</span>
                        {item.latest_action_kind && <span style={{ display: "block", marginTop: 1, color: colors.textMuted, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontWeight: 600 }}>{formatNextActionKind(t, item.latest_action_kind)}</span>}
                        {item.triggered_rollback && <span style={summaryAuditLineStyle}>{formatTriggeredRollbackSummaryLine(t, item.triggered_count, item.latest_note || item.recommended_reason)}</span>}
                        {item.recommended_trace_id && <span style={{ display: "block", marginTop: 1, color: colors.primaryDark, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontWeight: 700 }}>{t("Recommended trace", "\u5efa\u8bae\u8f68\u8ff9", "\u5efa\u8b70\u8ecc\u8de1")}</span>}
                        {item.triggered_rollback && <span style={{ display: "block", marginTop: 1, color: colors.warning, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontWeight: 700 }}>{t("Triggered rollback", "\u5df2\u89e6\u53d1\u56de\u6eda", "\u5df2\u89f8\u767c\u56de\u6efe")}</span>}
                    </button>
                ))}
            </div>
        </div>
    );
}

function FollowUpActionSummaryRows({ t, items, onOpen }: { t: Translate; items: FollowUpActionSummary[]; onOpen: (item: FollowUpActionSummary) => void }) {
    const rankedItems = rankFollowUpActionSummaries(items).slice(0, 4);
    return (
        <div style={{ borderTop: "1px solid " + colors.borderLight, paddingTop: 8, marginBottom: 10 }}>
            <div style={{ fontSize: "0.68rem", color: colors.textMuted, fontWeight: 700, marginBottom: 6 }}>{t("Follow-up Action Log", "\u540e\u7eed\u52a8\u4f5c\u8bb0\u5f55", "\u5f8c\u7e8c\u52d5\u4f5c\u8a18\u9304")}</div>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(190px, 1fr))", gap: 6 }}>
                {rankedItems.map((item) => (
                    <button key={item.kind || item.latest_trace_id || "followup-action"} type="button" onClick={() => onOpen(item)} style={item.triggered_rollback ? warningSummaryCardStyle : nextActionSummaryStyle} title={item.recommended_reason || item.latest_note || item.latest_title || item.kind}>
                        <span style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 6 }}>
                            <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{formatNextActionKind(t, item.kind)}</span>
                            <span style={{ color: colors.textMuted, fontVariantNumeric: "tabular-nums" }}>{toSafeCount(item.count, 0)}</span>
                        </span>
                        <span style={{ display: "block", marginTop: 2, color: colors.textMuted, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{item.latest_title || item.latest_note || "-"}</span>
                        {item.latest_status && <span style={{ display: "block", marginTop: 1, color: colors.textMuted, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontWeight: 600 }}>{formatFollowUpStatus(t, item.latest_status) || item.latest_status}</span>}
                        {item.triggered_rollback && <span style={summaryAuditLineStyle}>{formatTriggeredRollbackSummaryLine(t, item.triggered_count, item.latest_note || item.recommended_reason)}</span>}
                        {item.recommended_trace_id && <span style={{ display: "block", marginTop: 1, color: colors.primaryDark, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontWeight: 700 }}>{t("Recommended trace", "\u5efa\u8bae\u8f68\u8ff9", "\u5efa\u8b70\u8ecc\u8de1")}</span>}
                        {item.triggered_rollback && <span style={{ display: "block", marginTop: 1, color: colors.warning, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontWeight: 700 }}>{t("Triggered rollback", "\u5df2\u89e6\u53d1\u56de\u6eda", "\u5df2\u89f8\u767c\u56de\u6efe")}</span>}
                    </button>
                ))}
            </div>
        </div>
    );
}

function LearningTraceDetails({ t, details, total, counts, filter, reviewFocusStatus, actionFocusKind, followUpFocusStatus, followUpFocusActionKind, followUpTriggeredOnly, followUpFocusReason, followUpRecommendedTraceId, safetyBoundary, priorityTraceId, priorityTraceReason, selected, selectedId, onFilterChange, onClearReviewFocus, onClearActionFocus, onClearFollowUpFocus, onClearFollowUpActionFocus, onSelect, onReviewed }: { t: Translate; details: TraceDetail[]; total: number; counts: Record<TraceFilter, number>; filter: TraceFilter; reviewFocusStatus: string; actionFocusKind: string; followUpFocusStatus: string; followUpFocusActionKind: string; followUpTriggeredOnly: boolean; followUpFocusReason: string; followUpRecommendedTraceId: string; safetyBoundary: string; priorityTraceId: string; priorityTraceReason: string; selected: TraceDetail | null; selectedId: string; onFilterChange: (filter: TraceFilter) => void; onClearReviewFocus: () => void; onClearActionFocus: () => void; onClearFollowUpFocus: () => void; onClearFollowUpActionFocus: () => void; onSelect: (id: string) => void; onReviewed?: () => void | Promise<void> }) {
    const filters: { value: TraceFilter; label: string; count: number }[] = [
        { value: "all", label: t("All", "\u5168\u90e8", "\u5168\u90e8"), count: counts.all },
        { value: "review", label: t("Needs Review", "\u9700\u8bc4\u5ba1", "\u9700\u8a55\u5be9"), count: counts.review },
        { value: "actions", label: t("Actions", "\u52a8\u4f5c", "\u52d5\u4f5c"), count: counts.actions },
        { value: "followups", label: t("Follow-ups", "\u540e\u7eed", "\u5f8c\u7e8c"), count: counts.followups },
        { value: "reviewed", label: t("Reviewed", "\u5df2\u5ba1\u9605", "\u5df2\u5be9\u95b1"), count: counts.reviewed },
        { value: "a2a", label: "A2A", count: counts.a2a },
        { value: "tools", label: t("Tools", "\u5de5\u5177", "\u5de5\u5177"), count: counts.tools },
        { value: "sessions", label: t("Sessions", "\u4f1a\u8bdd", "\u6703\u8a71"), count: counts.sessions },
    ];
    const rollbackPriorityFocus = Boolean(priorityTraceId && priorityTraceReason && (followUpTriggeredOnly || actionFocusKind === "review_triggered_rollback_signal" || reviewFocusStatus !== ""));
    return (
        <div style={{ borderTop: "1px solid " + colors.border, marginTop: 12, paddingTop: 12 }}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 8, marginBottom: 8 }}>
                <div style={{ fontSize: "0.74rem", fontWeight: 700, color: colors.text }}>{t("Trace Details", "\u8f68\u8ff9\u8be6\u60c5", "\u8ecc\u8de1\u8a73\u60c5")}</div>
                <span style={{ fontSize: "0.66rem", color: colors.textMuted }}>{details.length}/{total}</span>
            </div>
            <div style={traceFilterBarStyle} role="tablist" aria-label={t("Trace filter", "\u8f68\u8ff9\u8fc7\u6ee4", "\u8ecc\u8de1\u904e\u6ffe")}>
                {filters.map((item) => <button key={item.value} type="button" role="tab" aria-selected={filter === item.value} onClick={() => onFilterChange(item.value)} style={traceFilterButtonStyle(filter === item.value)}>{item.label} <span style={traceFilterCountStyle}>{item.count}</span></button>)}
            </div>
            {safetyBoundary && (
                <div style={traceSafetyBoundaryStyle}>
                    <span style={{ fontWeight: 700 }}>{t("Safety Boundary", "\u5b89\u5168\u8fb9\u754c", "\u5b89\u5168\u908a\u754c")}: </span>
                    <span>{safetyBoundary}</span>
                </div>
            )}
            {(filter === "review" || filter === "reviewed") && reviewFocusStatus && (
                <div style={actionFocusStyle}>
                    <span>{t("Focused review", "\u805a\u7126\u5ba1\u9605", "\u805a\u7126\u5be9\u95b1")}: {formatReviewStatus(t, reviewFocusStatus) || reviewFocusStatus}</span>
                    <button type="button" onClick={onClearReviewFocus} style={actionFocusClearStyle}>{t("Clear", "\u6e05\u9664", "\u6e05\u9664")}</button>
                </div>
            )}
            {filter === "actions" && actionFocusKind && (
                <div style={actionFocusStyle}>
                    <span>{t("Focused action", "\u805a\u7126\u52a8\u4f5c", "\u805a\u7126\u52d5\u4f5c")}: {formatNextActionKind(t, actionFocusKind)}</span>
                    <button type="button" onClick={onClearActionFocus} style={actionFocusClearStyle}>{t("Clear", "\u6e05\u9664", "\u6e05\u9664")}</button>
                </div>
            )}
            {filter === "followups" && followUpFocusStatus && (
                <div style={actionFocusStyle}>
                    <span>{t("Focused follow-up", "\u805a\u7126\u540e\u7eed", "\u805a\u7126\u5f8c\u7e8c")}: {formatFollowUpStatus(t, followUpFocusStatus) || followUpFocusStatus}</span>
                    <button type="button" onClick={onClearFollowUpFocus} style={actionFocusClearStyle}>{t("Clear", "\u6e05\u9664", "\u6e05\u9664")}</button>
                </div>
            )}
            {filter === "followups" && followUpFocusActionKind && (
                <div style={actionFocusStyle}>
                    <span>{t("Focused follow-up action", "\u805a\u7126\u540e\u7eed\u52a8\u4f5c", "\u805a\u7126\u5f8c\u7e8c\u52d5\u4f5c")}: {formatNextActionKind(t, followUpFocusActionKind)}</span>
                    <button type="button" onClick={onClearFollowUpActionFocus} style={actionFocusClearStyle}>{t("Clear", "\u6e05\u9664", "\u6e05\u9664")}</button>
                </div>
            )}
            {filter === "followups" && followUpTriggeredOnly && (
                <div style={actionFocusStyle}>
                    <span>{t("Focused rollback risk", "\u805a\u7126\u56de\u6eda\u98ce\u9669", "\u805a\u7126\u56de\u6efe\u98a8\u96aa")}: {t("Triggered rollback only", "\u4ec5\u770b\u5df2\u89e6\u53d1 rollback", "\u50c5\u770b\u5df2\u89f8\u767c rollback")}</span>
                    <button type="button" onClick={onClearFollowUpActionFocus} style={actionFocusClearStyle}>{t("Clear", "\u6e05\u9664", "\u6e05\u9664")}</button>
                </div>
            )}
            {filter === "followups" && followUpTriggeredOnly && followUpFocusReason && (
                <div style={actionFocusHintStyle}>
                    <span style={{ fontWeight: 700 }}>{t("Why this focus", "\u805a\u7126\u539f\u56e0", "\u805a\u7126\u539f\u56e0")}: </span>
                    <span>{followUpFocusReason}</span>
                </div>
            )}
            {(filter === "actions" || filter === "review" || filter === "reviewed") && rollbackPriorityFocus && (
                <div style={actionFocusHintStyle}>
                    <span style={{ fontWeight: 700 }}>{t("Why this focus", "\u805a\u7126\u539f\u56e0", "\u805a\u7126\u539f\u56e0")}: </span>
                    <span>{priorityTraceReason}</span>
                </div>
            )}
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(240px, 1fr))", gap: 10, alignItems: "stretch" }}>
                <div style={{ borderRight: "1px solid " + colors.borderLight, paddingRight: 8, maxHeight: 260, overflowY: "auto" }}>
                    {details.length === 0 && <div style={{ fontSize: "0.7rem", color: colors.textMuted, padding: "8px 4px" }}>{t("No trace details match this filter.", "\u6ca1\u6709\u5339\u914d\u6b64\u8fc7\u6ee4\u7684\u8f68\u8ff9\u8be6\u60c5\u3002", "\u6c92\u6709\u5339\u914d\u6b64\u904e\u6ffe\u7684\u8ecc\u8de1\u8a73\u60c5\u3002")}</div>}
                    {details.slice(0, 20).map((detail, idx) => {
                        const id = detail.id || "trace-" + idx;
                        const active = id === selectedId;
                        const recommended = id === priorityTraceId && Boolean(priorityTraceReason) && experienceTriggeredRollbackDetail(detail);
                        return (
                            <button key={id} type="button" onClick={() => onSelect(id)} style={traceButtonStyle(active)} title={detail.title || id}>
                                <span style={{ fontSize: "0.68rem", color: active ? colors.primaryDark : colors.textMuted, fontWeight: 700 }}>{formatKind(t, detail.kind)}</span>
                                <span style={{ display: "block", marginTop: 2, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{detail.title || id}</span>
                                {recommended && (
                                    <span style={traceRecommendationBadgeStyle}>
                                        {t("Recommended rollback trace", "\u5efa\u8bae rollback \u8f68\u8ff9", "\u5efa\u8b70 rollback \u8ecc\u8de1")}
                                    </span>
                                )}
                                {recommended && followUpFocusReason && (
                                    <span style={traceRecommendationReasonStyle}>
                                        {followUpFocusReason}
                                    </span>
                                )}
                                {(detail.review_required || detail.review_status) && <span style={reviewStatusBadgeStyle(detail.review_status || (detail.review_required ? "required" : ""))}>{formatReviewStatus(t, detail.review_status || (detail.review_required ? "required" : ""))}</span>}
                            </button>
                        );
                    })}
                </div>
                <TraceDetailPanel
                    t={t}
                    detail={selected}
                    recommendedRollbackTrace={Boolean(selected?.id) && selected?.id === priorityTraceId && Boolean(priorityTraceReason) && experienceTriggeredRollbackDetail(selected)}
                    recommendedRollbackReason={Boolean(selected?.id) && selected?.id === priorityTraceId ? priorityTraceReason : ""}
                    onReviewed={onReviewed}
                />
            </div>
        </div>
    );
}

function TraceDetailPanel({ t, detail, recommendedRollbackTrace, recommendedRollbackReason, onReviewed }: { t: Translate; detail: TraceDetail | null; recommendedRollbackTrace?: boolean; recommendedRollbackReason?: string; onReviewed?: () => void | Promise<void> }) {
    const [reviewNote, setReviewNote] = useState("");
    const [reviewing, setReviewing] = useState("");
    const [reviewError, setReviewError] = useState("");
    const [reviewMessage, setReviewMessage] = useState("");
    const [reviewRecord, setReviewRecord] = useState<TraceReviewRecord | null>(null);
    const [draft, setDraft] = useState<FollowUpDraft | null>(null);
    const [draftLoading, setDraftLoading] = useState(false);
    const [draftError, setDraftError] = useState("");
    const [skillDraft, setSkillDraft] = useState<SkillDraft | null>(null);
    const [skillDraftLoading, setSkillDraftLoading] = useState(false);
    const [skillDraftError, setSkillDraftError] = useState("");
    const [rollbackDraft, setRollbackDraft] = useState<RollbackDraft | null>(null);
    const [rollbackDraftLoading, setRollbackDraftLoading] = useState(false);
    const [rollbackDraftError, setRollbackDraftError] = useState("");
    const [escalationBrief, setEscalationBrief] = useState<EscalationBrief | null>(null);
    const [escalationBriefLoading, setEscalationBriefLoading] = useState(false);
    const [escalationBriefError, setEscalationBriefError] = useState("");
    const [conflictDraft, setConflictDraft] = useState<ConflictDraft | null>(null);
    const [conflictDraftLoading, setConflictDraftLoading] = useState(false);
    const [conflictDraftError, setConflictDraftError] = useState("");
    const [followUpNote, setFollowUpNote] = useState("");
    const [recordingFollowUp, setRecordingFollowUp] = useState("");
    const [followUpError, setFollowUpError] = useState("");
    const [followUpMessage, setFollowUpMessage] = useState("");
    const [followUpRecord, setFollowUpRecord] = useState<FollowUpRecord | null>(null);
    const [traceDraftReviewKind, setTraceDraftReviewKind] = useState("");
    const [traceDraftReviewNote, setTraceDraftReviewNote] = useState("");
    const [traceDraftReviewing, setTraceDraftReviewing] = useState<DraftReviewStatus | "">("");
    const [traceDraftReviewError, setTraceDraftReviewError] = useState("");
    const [traceDraftReviewMessage, setTraceDraftReviewMessage] = useState("");
    const [traceDraftReviewRecord, setTraceDraftReviewRecord] = useState<DraftReviewRecord | null>(null);
    useEffect(() => {
        setReviewNote("");
        setReviewing("");
        setReviewError("");
        setReviewMessage("");
        setReviewRecord(null);
        setDraft(null);
        setDraftLoading(false);
        setDraftError("");
        setSkillDraft(null);
        setSkillDraftLoading(false);
        setSkillDraftError("");
        setRollbackDraft(null);
        setRollbackDraftLoading(false);
        setRollbackDraftError("");
        setEscalationBrief(null);
        setEscalationBriefLoading(false);
        setEscalationBriefError("");
        setConflictDraft(null);
        setConflictDraftLoading(false);
        setConflictDraftError("");
        setFollowUpNote("");
        setRecordingFollowUp("");
        setFollowUpError("");
        setFollowUpMessage("");
        setFollowUpRecord(null);
        setTraceDraftReviewKind("");
        setTraceDraftReviewNote("");
        setTraceDraftReviewing("");
        setTraceDraftReviewError("");
        setTraceDraftReviewMessage("");
        setTraceDraftReviewRecord(null);
    }, [detail?.id]);
    if (!detail) return null;
    const canReview = Boolean(detail.review_required && detail.id?.startsWith("memory:"));
    const canDraftFollowUp = Boolean(detail.id?.startsWith("memory:") && detail.next_action_kind && detail.next_action_kind !== "review_signal" && !detail.review_required);
    const canDraftSkill = Boolean(canDraftFollowUp && detail.next_action_kind === "draft_skill_manually");
    const canDraftRollback = Boolean(canDraftFollowUp && detail.next_action_kind === "draft_rollback_workflow");
    const canDraftEscalation = Boolean(canDraftFollowUp && detail.next_action_kind === "prepare_escalation_brief");
    const canDraftConflict = Boolean(canDraftFollowUp && detail.next_action_kind === "resolve_a2a_conflict_manually");
    const triggeredRollbackReview = experienceTriggeredRollbackDetail(detail);
    const recommendedReason = String(recommendedRollbackReason || "").trim();
    const draftTitlePrefix = recommendedRollbackTrace
        ? t("Recommended rollback", "\u63a8\u8350 rollback", "\u63a8\u85a6 rollback")
        : "";
    const followUpDraftLabel = triggeredRollbackReview
        ? t("Draft Review Handoff", "\u751f\u6210\u590d\u6838\u79fb\u4ea4\u8349\u7a3f", "\u751f\u6210\u8907\u6838\u79fb\u4ea4\u8349\u7a3f")
        : t("Draft Follow-up", "\u751f\u6210\u540e\u7eed\u8349\u7a3f", "\u751f\u6210\u5f8c\u7e8c\u8349\u7a3f");
    const followUpDraftDoneMessage = triggeredRollbackReview
        ? t("Triggered rollback review recorded.", "\u5df2\u8bb0\u5f55\u89e6\u53d1 rollback \u590d\u6838\u7ed3\u679c\u3002", "\u5df2\u8a18\u9304\u89f8\u767c rollback \u8907\u6838\u7d50\u679c\u3002")
        : t("Follow-up recorded.", "\u5df2\u8bb0\u5f55\u540e\u7eed\u72b6\u6001\u3002", "\u5df2\u8a18\u9304\u5f8c\u7e8c\u72c0\u614b\u3002");
    const followUpNotePlaceholder = triggeredRollbackReview
        ? t("Owner decision, rejected trigger, or next review checkpoint", "\u8d1f\u8d23\u4eba\u51b3\u5b9a\u3001\u88ab\u62d2\u7684 trigger\uff0c\u6216\u4e0b\u4e00\u4e2a\u590d\u6838\u68c0\u67e5\u70b9", "\u8ca0\u8cac\u4eba\u6c7a\u5b9a\u3001\u88ab\u62d2\u7684 trigger\uff0c\u6216\u4e0b\u4e00\u500b\u8907\u6838\u6aa2\u67e5\u9ede")
        : t("Manual outcome or blocker", "\u624b\u5de5\u7ed3\u679c\u6216\u963b\u585e\u70b9", "\u624b\u52d5\u7d50\u679c\u6216\u963b\u585e\u9ede");
    const contextualFollowUpNotePlaceholder = recommendedRollbackTrace && recommendedReason
        ? followUpNotePlaceholder + " | " + t("Keep the note tied to the recommended rollback trace context", "\u8bf7\u56f4\u7ed5\u63a8\u8350 rollback \u8f68\u8ff9\u8bb0\u5f55", "\u8acb\u570d\u7e5e\u63a8\u85a6 rollback \u8ecc\u8de1\u8a18\u9304")
        : followUpNotePlaceholder;
    const draftReviewNotePlaceholder = recommendedRollbackTrace && recommendedReason
        ? t("Record the manual decision, blocker, or owner checkpoint for this recommended rollback trace", "\u8bf7\u8bb0\u5f55\u8be5\u63a8\u8350 rollback \u8f68\u8ff9\u7684\u4eba\u5de5\u51b3\u5b9a\u3001\u963b\u585e\u6216 owner \u68c0\u67e5\u70b9", "\u8acb\u8a18\u9304\u9019\u689d\u63a8\u85a6 rollback \u8ecc\u8de1\u7684\u4eba\u5de5\u6c7a\u5b9a\u3001\u963b\u585e\u6216 owner \u6aa2\u67e5\u9ede")
        : t("Record manual decision or blocker", "\u8bb0\u5f55\u4eba\u5de5\u51b3\u5b9a\u6216\u963b\u585e\u70b9", "\u8a18\u9304\u4eba\u5de5\u6c7a\u5b9a\u6216\u963b\u585e\u9ede");
    const reviewNotePlaceholder = recommendedRollbackTrace && recommendedReason
        ? t("Review against the recommended rollback trace, then capture the decision or next verification point", "\u8bf7\u5148\u56f4\u7ed5\u63a8\u8350 rollback \u8f68\u8ff9\u590d\u6838\uff0c\u518d\u8bb0\u5f55\u51b3\u5b9a\u6216\u4e0b\u4e00\u4e2a\u9a8c\u8bc1\u70b9", "\u8acb\u5148\u570d\u7e5e\u63a8\u85a6 rollback \u8ecc\u8de1\u8907\u6838\uff0c\u518d\u8a18\u9304\u6c7a\u5b9a\u6216\u4e0b\u4e00\u500b\u9a57\u8b49\u9ede")
        : t("Reason or follow-up check", "\u7406\u7531\u6216\u540e\u7eed\u9a8c\u8bc1\u70b9", "\u7406\u7531\u6216\u5f8c\u7e8c\u9a57\u8b49\u9ede");
    const reviewRecordedMessage = recommendedRollbackTrace && recommendedReason
        ? t("Recommended rollback trace review recorded.", "\u5df2\u8bb0\u5f55\u63a8\u8350 rollback \u8f68\u8ff9\u7684\u590d\u6838\u7ed3\u679c\u3002", "\u5df2\u8a18\u9304\u63a8\u85a6 rollback \u8ecc\u8de1\u7684\u8907\u6838\u7d50\u679c\u3002")
        : t("Review recorded.", "\u5df2\u8bb0\u5f55\u5ba1\u9605\u7ed3\u679c\u3002", "\u5df2\u8a18\u9304\u5be9\u95b1\u7d50\u679c\u3002");
    const draftReviewRecordedMessage = recommendedRollbackTrace && recommendedReason
        ? t("Recommended rollback trace draft review recorded.", "\u5df2\u8bb0\u5f55\u63a8\u8350 rollback \u8f68\u8ff9\u7684\u8349\u6848\u590d\u6838\u7ed3\u679c\u3002", "\u5df2\u8a18\u9304\u63a8\u85a6 rollback \u8ecc\u8de1\u7684\u8349\u6848\u8907\u6838\u7d50\u679c\u3002")
        : t("Draft review recorded.", "\u5df2\u8bb0\u5f55\u8349\u6848\u5ba1\u9605\u7ed3\u679c\u3002", "\u5df2\u8a18\u9304\u8349\u6848\u5be9\u95b1\u7d50\u679c\u3002");
    const contextualFollowUpDoneMessage = recommendedRollbackTrace && recommendedReason
        ? t("Recommended rollback trace follow-up recorded.", "\u5df2\u8bb0\u5f55\u63a8\u8350 rollback \u8f68\u8ff9\u7684\u540e\u7eed\u7ed3\u679c\u3002", "\u5df2\u8a18\u9304\u63a8\u85a6 rollback \u8ecc\u8de1\u7684\u5f8c\u7e8c\u7d50\u679c\u3002")
        : followUpDraftDoneMessage;
    const contextualDetailLabel = (normalLabel: string, contextualLabel: string): string => recommendedRollbackTrace ? contextualLabel : normalLabel;
    const traceDraftRecording = (kind: string): DraftReviewStatus | "" => traceDraftReviewKind === kind ? traceDraftReviewing : "";
    const traceDraftError = (kind: string): string => traceDraftReviewKind === kind ? traceDraftReviewError : "";
    const traceDraftMessage = (kind: string): string => traceDraftReviewKind === kind ? traceDraftReviewMessage : "";
    const traceDraftToolCall = (kind: string): GovernanceToolCall | null => traceDraftReviewKind === kind ? traceDraftReviewRecord?.recommended_tool_call || null : null;
    const recommendedCopyText = (label: string, value: string): string => {
        if (!recommendedRollbackTrace || !recommendedReason) return value;
        return [
            label,
            t("Recommendation Context", "\u63a8\u8350\u4e0a\u4e0b\u6587", "\u63a8\u85a6\u4e0a\u4e0b\u6587") + ": " + recommendedReason,
            "",
            value,
        ].join("\n");
    };
    const recommendedDraftLabel = (label: string): string => draftTitlePrefix ? draftTitlePrefix + " " + label : label;
    const submitReview = async (outcome: "approved" | "rejected" | "deferred") => {
        if (!detail.id || reviewing) return;
        setReviewing(outcome);
        setReviewError("");
        setReviewMessage("");
        setReviewRecord(null);
        try {
            const record = await (ReviewExperienceTrace(detail.id, { outcome, note: reviewNote }) as Promise<TraceReviewRecord | void>);
            setReviewRecord(record || null);
            setReviewNote("");
            setReviewMessage(reviewRecordedMessage);
            await onReviewed?.();
        } catch (err) {
            setReviewError(String(err));
        } finally {
            setReviewing("");
        }
    };
    const loadFollowUpDraft = async () => {
        if (!detail.id || draftLoading) return;
        setDraftLoading(true);
        setDraftError("");
        try {
            const result = await BuildExperienceTraceFollowUp(detail.id);
            setDraft(result || null);
        } catch (err) {
            setDraftError(String(err));
        } finally {
            setDraftLoading(false);
        }
    };
    const loadSkillDraft = async () => {
        if (!detail.id || skillDraftLoading) return;
        setSkillDraftLoading(true);
        setSkillDraftError("");
        try {
            const result = await BuildExperienceSkillDraft(detail.id);
            setSkillDraft(result || null);
        } catch (err) {
            setSkillDraftError(String(err));
        } finally {
            setSkillDraftLoading(false);
        }
    };
    const loadRollbackDraft = async () => {
        if (!detail.id || rollbackDraftLoading) return;
        setRollbackDraftLoading(true);
        setRollbackDraftError("");
        try {
            const result = await BuildExperienceRollbackWorkflowDraft(detail.id);
            setRollbackDraft(result || null);
        } catch (err) {
            setRollbackDraftError(String(err));
        } finally {
            setRollbackDraftLoading(false);
        }
    };
    const loadEscalationBrief = async () => {
        if (!detail.id || escalationBriefLoading) return;
        setEscalationBriefLoading(true);
        setEscalationBriefError("");
        try {
            const result = await BuildExperienceEscalationBrief(detail.id);
            setEscalationBrief(result || null);
        } catch (err) {
            setEscalationBriefError(String(err));
        } finally {
            setEscalationBriefLoading(false);
        }
    };
    const loadConflictDraft = async () => {
        if (!detail.id || conflictDraftLoading) return;
        setConflictDraftLoading(true);
        setConflictDraftError("");
        try {
            const result = await BuildExperienceConflictReconciliationDraft(detail.id);
            setConflictDraft(result || null);
        } catch (err) {
            setConflictDraftError(String(err));
        } finally {
            setConflictDraftLoading(false);
        }
    };
    const recordFollowUp = async (status: "completed" | "blocked" | "deferred") => {
        if (!detail.id || recordingFollowUp) return;
        setRecordingFollowUp(status);
        setFollowUpError("");
        setFollowUpMessage("");
        setFollowUpRecord(null);
        try {
            const record = await (RecordExperienceTraceFollowUp(detail.id, { status, note: followUpNote }) as Promise<FollowUpRecord | void>);
            setFollowUpRecord(record || null);
            setFollowUpNote("");
            setFollowUpMessage(contextualFollowUpDoneMessage);
            await onReviewed?.();
        } catch (err) {
            setFollowUpError(String(err));
        } finally {
            setRecordingFollowUp("");
        }
    };
    const recordTraceDraftReview = async (kind: string, markdown: string | undefined, boundary: string | undefined, status: DraftReviewStatus, context?: GovernanceFocusContext | null) => {
        if (!markdown || traceDraftReviewing) return;
        setTraceDraftReviewKind(kind);
        setTraceDraftReviewing(status);
        setTraceDraftReviewError("");
        setTraceDraftReviewMessage("");
        setTraceDraftReviewRecord(null);
        try {
            const record = await RecordExperienceDraftReview({
                kind,
                status,
                source_trace_id: draftSourceTraceId(context, detail.id || ""),
                query: detail.title || detail.id || "",
                note: traceDraftReviewNote.trim(),
                draft_markdown: markdown,
                non_executing_boundary: boundary || "",
            });
            setTraceDraftReviewRecord(record || null);
            setTraceDraftReviewNote("");
            setTraceDraftReviewMessage(draftReviewRecordedMessage);
            await onReviewed?.();
        } catch (err) {
            setTraceDraftReviewError(String(err));
        } finally {
            setTraceDraftReviewing("");
        }
    };
    return (
        <div style={{ minWidth: 0, maxHeight: 260, overflowY: "auto", paddingRight: 4 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap", marginBottom: 4 }}>
                <span style={{ ...kindBadgeStyle, color: detail.review_required ? colors.warning : colors.primaryDark, borderColor: detail.review_required ? colors.warning : colors.primary }}>{formatKind(t, detail.kind)}</span>
                {(detail.review_required || detail.review_status) && <span style={reviewStatusBadgeStyle(detail.review_status || (detail.review_required ? "required" : ""))}>{formatReviewStatus(t, detail.review_status || (detail.review_required ? "required" : ""))}</span>}
                {triggeredRollbackReview && <span style={warningBadgeStyle}>{t("Triggered rollback", "\u5df2\u89e6\u53d1\u56de\u6eda", "\u5df2\u89f8\u767c\u56de\u6efe")}</span>}
                {recommendedRollbackTrace && <span style={traceRecommendationBadgeStyle}>{t("Recommended rollback trace", "\u5efa\u8bae rollback \u8f68\u8ff9", "\u5efa\u8b70 rollback \u8ecc\u8de1")}</span>}
                {detail.updated_at && <span style={{ fontSize: "0.66rem", color: colors.textMuted }}>{formatDate(detail.updated_at)}</span>}
            </div>
            <div style={{ fontSize: "0.78rem", fontWeight: 700, color: colors.text, marginBottom: 4, overflowWrap: "anywhere" }}>{detail.title || "-"}</div>
            {detail.summary && <div style={detailTextStyle}>{detail.summary}</div>}
            {recommendedRollbackTrace && recommendedReason && (
                <div style={actionFocusHintStyle}>
                    <span style={{ fontWeight: 700 }}>{t("Why this trace", "\u63a8\u8350\u539f\u56e0", "\u63a8\u85a6\u539f\u56e0")}: </span>
                    <span>{recommendedReason}</span>
                </div>
            )}
            {triggeredRollbackReview && (
                <div style={triggeredRollbackNoticeStyle}>
                    {t("This trace already carries A2A evidence that matches rollback conditions. Keep the next step in read-only review mode until an owner explicitly approves any rollback workflow.", "\u8be5\u8f68\u8ff9\u5df2\u643a\u5e26\u547d\u4e2d rollback \u6761\u4ef6\u7684 A2A \u8bc1\u636e\uff0c\u4e0b\u4e00\u6b65\u8bf7\u4fdd\u6301\u5728\u53ea\u8bfb\u590d\u6838\u6a21\u5f0f\uff0c\u76f4\u5230\u6709 owner \u660e\u786e\u6279\u51c6 rollback \u6d41\u7a0b\u3002", "\u8a72\u8ecc\u8de1\u5df2\u643a\u5e36\u547d\u4e2d rollback \u689d\u4ef6\u7684 A2A \u8b49\u64da\uff0c\u4e0b\u4e00\u6b65\u8acb\u4fdd\u6301\u5728\u53ea\u8b80\u8907\u6838\u6a21\u5f0f\uff0c\u76f4\u5230\u6709 owner \u660e\u78ba\u6279\u51c6 rollback \u6d41\u7a0b\u3002")}
                </div>
            )}
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(110px, 1fr))", gap: 6, marginTop: 8 }}>
                <DetailMetric label={t("Evidence", "\u8bc1\u636e", "\u8b49\u64da")} value={detail.evidence ? String(detail.evidence) : "-"} />
                <DetailMetric label={t("Confidence", "\u7f6e\u4fe1\u5ea6", "\u4fe1\u5fc3")} value={formatConfidence(detail.confidence)} />
                <DetailMetric label={t("Source", "\u6765\u6e90", "\u4f86\u6e90")} value={detail.source_type || "-"} />
                <DetailMetric label={t("Review", "\u5ba1\u9605", "\u5be9\u95b1")} value={formatReviewStatus(t, detail.review_status || (detail.review_required ? "required" : "")) || "-"} />
                <DetailMetric label={t("Reviewed At", "\u5ba1\u9605\u65f6\u95f4", "\u5be9\u95b1\u6642\u9593")} value={detail.reviewed_at || "-"} />
                <DetailMetric label={t("Reviews", "\u5ba1\u9605\u6b21\u6570", "\u5be9\u95b1\u6b21\u6578")} value={detail.review_count ? String(detail.review_count) : "-"} />
                <DetailMetric label={t("Follow-up", "\u540e\u7eed", "\u5f8c\u7e8c")} value={formatFollowUpStatus(t, detail.follow_up_status) || "-"} />
            </div>
            {detail.impact && <DetailBlock label={t("Impact", "\u5f71\u54cd", "\u5f71\u97ff")} value={detail.impact} />}
            {detail.review_action && <DetailBlock label={t("Review Action", "\u8bc4\u5ba1\u52a8\u4f5c", "\u8a55\u5be9\u52d5\u4f5c")} value={detail.review_action} />}
            {detail.next_action && <DetailBlock label={t("Next Action", "\u540e\u7eed\u52a8\u4f5c", "\u5f8c\u7e8c\u52d5\u4f5c")} value={(detail.next_action_kind ? formatNextActionKind(t, detail.next_action_kind) + ": " : "") + detail.next_action} />}
            {canDraftFollowUp && (
                <div style={followUpPanelStyle}>
                    {recommendedRollbackTrace && recommendedReason && (
                        <div style={maintenanceMetaStyle}>
                            {t("Recommended rollback trace is active for this follow-up flow.", "\u5f53\u524d\u540e\u7eed\u6d41\u7a0b\u6b63\u5728\u4f7f\u7528\u63a8\u8350\u7684 rollback \u8f68\u8ff9\u3002", "\u7576\u524d\u5f8c\u7e8c\u6d41\u7a0b\u6b63\u5728\u4f7f\u7528\u63a8\u85a6\u7684 rollback \u8ecc\u8de1\u3002")}
                            {" "}
                            {recommendedReason}
                        </div>
                    )}
                    <button type="button" disabled={draftLoading} onClick={loadFollowUpDraft} style={followUpButtonStyle(draftLoading)}>{draftLoading ? t("Drafting...", "\u751f\u6210\u4e2d...", "\u751f\u6210\u4e2d...") : followUpDraftLabel}</button>
                    {canDraftSkill && <button type="button" disabled={skillDraftLoading} onClick={loadSkillDraft} style={{ ...followUpButtonStyle(skillDraftLoading), marginLeft: 6 }}>{skillDraftLoading ? t("Drafting...", "\u751f\u6210\u4e2d...", "\u751f\u6210\u4e2d...") : t("Skill Draft", "\u6280\u80fd\u8349\u7a3f", "\u6280\u80fd\u8349\u7a3f")}</button>}
                    {canDraftRollback && <button type="button" disabled={rollbackDraftLoading} onClick={loadRollbackDraft} style={{ ...followUpButtonStyle(rollbackDraftLoading), marginLeft: 6 }}>{rollbackDraftLoading ? t("Drafting...", "\u751f\u6210\u4e2d...", "\u751f\u6210\u4e2d...") : t("Rollback Draft", "\u56de\u6eda\u8349\u7a3f", "\u56de\u6efe\u8349\u7a3f")}</button>}
                    {canDraftEscalation && <button type="button" disabled={escalationBriefLoading} onClick={loadEscalationBrief} style={{ ...followUpButtonStyle(escalationBriefLoading), marginLeft: 6 }}>{escalationBriefLoading ? t("Drafting...", "\u751f\u6210\u4e2d...", "\u751f\u6210\u4e2d...") : t("Escalation Brief", "\u5347\u7ea7\u7b80\u62a5", "\u5347\u7d1a\u7c21\u5831")}</button>}
                    {canDraftConflict && <button type="button" disabled={conflictDraftLoading} onClick={loadConflictDraft} style={{ ...followUpButtonStyle(conflictDraftLoading), marginLeft: 6 }}>{conflictDraftLoading ? t("Drafting...", "\u751f\u6210\u4e2d...", "\u751f\u6210\u4e2d...") : t("Conflict Draft", "\u51b2\u7a81\u8349\u7a3f", "\u885d\u7a81\u8349\u7a3f")}</button>}
                    {draftError && <div role="alert" style={{ fontSize: "0.66rem", color: colors.danger, marginTop: 5 }}>{draftError}</div>}
                    {draft?.draft && <DetailBlock label={recommendedDraftLabel(draft.draft_title || t("Follow-up Draft", "\u540e\u7eed\u8349\u7a3f", "\u5f8c\u7e8c\u8349\u7a3f"))} value={draft.draft} copyValueText={recommendedCopyText(draft.draft_title || t("Follow-up Draft", "\u540e\u7eed\u8349\u7a3f", "\u5f8c\u7e8c\u8349\u7a3f"), draft.draft)} pre monospace {...draftCopyProps(t)} />}
                    {draft?.non_executing_boundary && <DetailBlock label={t("Safety Boundary", "\u5b89\u5168\u8fb9\u754c", "\u5b89\u5168\u908a\u754c")} value={draft.non_executing_boundary} />}
                    <RecommendedToolCallBlock t={t} call={draft?.recommended_tool_call} />
                    {skillDraftError && <div role="alert" style={{ fontSize: "0.66rem", color: colors.danger, marginTop: 5 }}>{skillDraftError}</div>}
                    {skillDraft?.draft_markdown && <DetailBlock label={recommendedDraftLabel((skillDraft.suggested_name ? skillDraft.suggested_name + " " : "") + t("Skill Draft", "\u6280\u80fd\u8349\u7a3f", "\u6280\u80fd\u8349\u7a3f"))} value={skillDraft.draft_markdown} copyValueText={recommendedCopyText((skillDraft.suggested_name ? skillDraft.suggested_name + " " : "") + t("Skill Draft", "\u6280\u80fd\u8349\u7a3f", "\u6280\u80fd\u8349\u7a3f"), skillDraft.draft_markdown)} pre monospace {...draftCopyProps(t)} />}
                    {skillDraft?.non_executing_boundary && <DetailBlock label={t("Safety Boundary", "\u5b89\u5168\u8fb9\u754c", "\u5b89\u5168\u908a\u754c")} value={skillDraft.non_executing_boundary} />}
                    <RecommendedToolCallBlock t={t} call={skillDraft?.recommended_tool_call} />
                    <DraftReviewControls t={t} draft={draftReviewDraft(skillDraft?.draft_markdown, skillDraft?.non_executing_boundary, skillDraft?.recommended_focus_context, skillDraft?.recommended_tool_call)} note={traceDraftReviewNote} recording={traceDraftRecording("skill_draft")} error={traceDraftError("skill_draft")} message={traceDraftMessage("skill_draft")} recommendedToolCall={traceDraftToolCall("skill_draft")} notePlaceholder={draftReviewNotePlaceholder} onNoteChange={setTraceDraftReviewNote} onRecord={(status) => recordTraceDraftReview("skill_draft", skillDraft?.draft_markdown, skillDraft?.non_executing_boundary, status, skillDraft?.recommended_focus_context)} />
                    {rollbackDraftError && <div role="alert" style={{ fontSize: "0.66rem", color: colors.danger, marginTop: 5 }}>{rollbackDraftError}</div>}
                    {rollbackDraft?.draft_markdown && <DetailBlock label={recommendedDraftLabel(t("Rollback Workflow Draft", "\u56de\u6eda\u6d41\u7a0b\u8349\u7a3f", "\u56de\u6efe\u6d41\u7a0b\u8349\u7a3f"))} value={rollbackDraft.draft_markdown} copyValueText={recommendedCopyText(t("Rollback Workflow Draft", "\u56de\u6eda\u6d41\u7a0b\u8349\u7a3f", "\u56de\u6efe\u6d41\u7a0b\u8349\u7a3f"), rollbackDraft.draft_markdown)} pre monospace {...draftCopyProps(t)} />}
                    {rollbackDraft?.non_executing_boundary && <DetailBlock label={t("Safety Boundary", "\u5b89\u5168\u8fb9\u754c", "\u5b89\u5168\u908a\u754c")} value={rollbackDraft.non_executing_boundary} />}
                    <RecommendedToolCallBlock t={t} call={rollbackDraft?.recommended_tool_call} />
                    <DraftReviewControls t={t} draft={draftReviewDraft(rollbackDraft?.draft_markdown, rollbackDraft?.non_executing_boundary, rollbackDraft?.recommended_focus_context, rollbackDraft?.recommended_tool_call)} note={traceDraftReviewNote} recording={traceDraftRecording("rollback_workflow_draft")} error={traceDraftError("rollback_workflow_draft")} message={traceDraftMessage("rollback_workflow_draft")} recommendedToolCall={traceDraftToolCall("rollback_workflow_draft")} notePlaceholder={draftReviewNotePlaceholder} onNoteChange={setTraceDraftReviewNote} onRecord={(status) => recordTraceDraftReview("rollback_workflow_draft", rollbackDraft?.draft_markdown, rollbackDraft?.non_executing_boundary, status, rollbackDraft?.recommended_focus_context)} />
                    {escalationBriefError && <div role="alert" style={{ fontSize: "0.66rem", color: colors.danger, marginTop: 5 }}>{escalationBriefError}</div>}
                    {escalationBrief?.brief_markdown && <DetailBlock label={recommendedDraftLabel((escalationBrief.target ? escalationBrief.target + " " : "") + t("Escalation Brief", "\u5347\u7ea7\u7b80\u62a5", "\u5347\u7d1a\u7c21\u5831"))} value={escalationBrief.brief_markdown} copyValueText={recommendedCopyText((escalationBrief.target ? escalationBrief.target + " " : "") + t("Escalation Brief", "\u5347\u7ea7\u7b80\u62a5", "\u5347\u7d1a\u7c21\u5831"), escalationBrief.brief_markdown)} pre monospace {...draftCopyProps(t)} />}
                    {escalationBrief?.non_executing_boundary && <DetailBlock label={t("Safety Boundary", "\u5b89\u5168\u8fb9\u754c", "\u5b89\u5168\u908a\u754c")} value={escalationBrief.non_executing_boundary} />}
                    <RecommendedToolCallBlock t={t} call={escalationBrief?.recommended_tool_call} />
                    <DraftReviewControls t={t} draft={draftReviewDraft(escalationBrief?.brief_markdown, escalationBrief?.non_executing_boundary, escalationBrief?.recommended_focus_context, escalationBrief?.recommended_tool_call)} note={traceDraftReviewNote} recording={traceDraftRecording("escalation_brief")} error={traceDraftError("escalation_brief")} message={traceDraftMessage("escalation_brief")} recommendedToolCall={traceDraftToolCall("escalation_brief")} notePlaceholder={draftReviewNotePlaceholder} onNoteChange={setTraceDraftReviewNote} onRecord={(status) => recordTraceDraftReview("escalation_brief", escalationBrief?.brief_markdown, escalationBrief?.non_executing_boundary, status, escalationBrief?.recommended_focus_context)} />
                    {conflictDraftError && <div role="alert" style={{ fontSize: "0.66rem", color: colors.danger, marginTop: 5 }}>{conflictDraftError}</div>}
                    {conflictDraft?.draft_markdown && <DetailBlock label={recommendedDraftLabel((conflictDraft.topic ? conflictDraft.topic + " " : "") + t("Conflict Reconciliation Draft", "\u51b2\u7a81\u8c03\u548c\u8349\u7a3f", "\u885d\u7a81\u8abf\u548c\u8349\u7a3f"))} value={conflictDraft.draft_markdown} copyValueText={recommendedCopyText((conflictDraft.topic ? conflictDraft.topic + " " : "") + t("Conflict Reconciliation Draft", "\u51b2\u7a81\u8c03\u548c\u8349\u7a3f", "\u885d\u7a81\u8abf\u548c\u8349\u7a3f"), conflictDraft.draft_markdown)} pre monospace {...draftCopyProps(t)} />}
                    {conflictDraft?.non_executing_boundary && <DetailBlock label={t("Safety Boundary", "\u5b89\u5168\u8fb9\u754c", "\u5b89\u5168\u908a\u754c")} value={conflictDraft.non_executing_boundary} />}
                    <RecommendedToolCallBlock t={t} call={conflictDraft?.recommended_tool_call} />
                    <DraftReviewControls t={t} draft={draftReviewDraft(conflictDraft?.draft_markdown, conflictDraft?.non_executing_boundary, conflictDraft?.recommended_focus_context, conflictDraft?.recommended_tool_call)} note={traceDraftReviewNote} recording={traceDraftRecording("conflict_reconciliation_draft")} error={traceDraftError("conflict_reconciliation_draft")} message={traceDraftMessage("conflict_reconciliation_draft")} recommendedToolCall={traceDraftToolCall("conflict_reconciliation_draft")} notePlaceholder={draftReviewNotePlaceholder} onNoteChange={setTraceDraftReviewNote} onRecord={(status) => recordTraceDraftReview("conflict_reconciliation_draft", conflictDraft?.draft_markdown, conflictDraft?.non_executing_boundary, status, conflictDraft?.recommended_focus_context)} />
                    <label style={{ display: "block", fontSize: "0.66rem", color: colors.textMuted, fontWeight: 700, marginTop: 8, marginBottom: 4 }}>
                        {t("Follow-up note", "\u540e\u7eed\u5907\u6ce8", "\u5f8c\u7e8c\u5099\u8a3b")}
                    </label>
                    <textarea
                        value={followUpNote}
                        onChange={(event) => setFollowUpNote(event.target.value)}
                        placeholder={contextualFollowUpNotePlaceholder}
                        style={reviewTextareaStyle}
                        rows={2}
                    />
                    <div style={reviewButtonRowStyle}>
                        <button type="button" disabled={Boolean(recordingFollowUp)} onClick={() => recordFollowUp("completed")} style={followUpOutcomeButtonStyle("completed", recordingFollowUp === "completed")}>{recordingFollowUp === "completed" ? t("Recording...", "\u8bb0\u5f55\u4e2d...", "\u8a18\u9304\u4e2d...") : t("Done", "\u5b8c\u6210", "\u5b8c\u6210")}</button>
                        <button type="button" disabled={Boolean(recordingFollowUp)} onClick={() => recordFollowUp("blocked")} style={followUpOutcomeButtonStyle("blocked", recordingFollowUp === "blocked")}>{recordingFollowUp === "blocked" ? t("Recording...", "\u8bb0\u5f55\u4e2d...", "\u8a18\u9304\u4e2d...") : t("Blocked", "\u963b\u585e", "\u963b\u585e")}</button>
                        <button type="button" disabled={Boolean(recordingFollowUp)} onClick={() => recordFollowUp("deferred")} style={followUpOutcomeButtonStyle("deferred", recordingFollowUp === "deferred")}>{recordingFollowUp === "deferred" ? t("Recording...", "\u8bb0\u5f55\u4e2d...", "\u8a18\u9304\u4e2d...") : t("Defer", "\u5ef6\u540e", "\u5ef6\u5f8c")}</button>
                    </div>
                    {followUpError && <div role="alert" style={{ fontSize: "0.66rem", color: colors.danger, marginTop: 5 }}>{followUpError}</div>}
                    {followUpMessage && <div style={{ fontSize: "0.66rem", color: colors.success, marginTop: 5 }}>{followUpMessage}</div>}
                    {followUpRecord?.non_executing_boundary && <DetailBlock label={t("Safety Boundary", "\u5b89\u5168\u8fb9\u754c", "\u5b89\u5168\u908a\u754c")} value={followUpRecord.non_executing_boundary} />}
                    <RecommendedToolCallBlock t={t} call={followUpRecord?.recommended_tool_call} />
                </div>
            )}
            {detail.reviewer && <DetailBlock label={contextualDetailLabel(t("Reviewer", "\u5ba1\u9605\u4eba", "\u5be9\u95b1\u4eba"), t("Rollback Reviewer", "\u56de\u6eda\u590d\u6838\u4eba", "\u56de\u6efe\u8907\u6838\u4eba"))} value={detail.reviewer} />}
            {detail.review_note && <DetailBlock label={contextualDetailLabel(t("Review Note", "\u5ba1\u9605\u5907\u6ce8", "\u5be9\u95b1\u5099\u8a3b"), t("Rollback Review Note", "\u56de\u6eda\u590d\u6838\u5907\u6ce8", "\u56de\u6efe\u8907\u6838\u5099\u8a3b"))} value={detail.review_note} pre />}
            {detail.follow_up_action_kind && <DetailBlock label={contextualDetailLabel(t("Follow-up Action", "\u540e\u7eed\u52a8\u4f5c", "\u5f8c\u7e8c\u52d5\u4f5c"), t("Rollback Follow-up Action", "\u56de\u6eda\u540e\u7eed\u52a8\u4f5c", "\u56de\u6efe\u5f8c\u7e8c\u52d5\u4f5c"))} value={formatNextActionKind(t, detail.follow_up_action_kind)} />}
            {detail.follow_up_actor && <DetailBlock label={contextualDetailLabel(t("Follow-up Actor", "\u540e\u7eed\u5904\u7406\u4eba", "\u5f8c\u7e8c\u8655\u7406\u4eba"), t("Rollback Follow-up Actor", "\u56de\u6eda\u540e\u7eed\u5904\u7406\u4eba", "\u56de\u6efe\u5f8c\u7e8c\u8655\u7406\u4eba"))} value={detail.follow_up_actor} />}
            {detail.follow_up_at && <DetailBlock label={contextualDetailLabel(t("Follow-up At", "\u540e\u7eed\u65f6\u95f4", "\u5f8c\u7e8c\u6642\u9593"), t("Rollback Follow-up At", "\u56de\u6eda\u540e\u7eed\u65f6\u95f4", "\u56de\u6efe\u5f8c\u7e8c\u6642\u9593"))} value={detail.follow_up_at} />}
            {detail.follow_up_note && <DetailBlock label={contextualDetailLabel(t("Follow-up Note", "\u540e\u7eed\u5907\u6ce8", "\u5f8c\u7e8c\u5099\u8a3b"), t("Rollback Follow-up Note", "\u56de\u6eda\u540e\u7eed\u5907\u6ce8", "\u56de\u6efe\u5f8c\u7e8c\u5099\u8a3b"))} value={detail.follow_up_note} pre />}
            {canReview && (
                <div style={reviewPanelStyle}>
                    <label style={{ display: "block", fontSize: "0.66rem", color: colors.textMuted, fontWeight: 700, marginBottom: 4 }}>
                        {t("Review note", "\u5ba1\u9605\u5907\u6ce8", "\u5be9\u95b1\u5099\u8a3b")}
                    </label>
                    <textarea
                        value={reviewNote}
                        onChange={(event) => setReviewNote(event.target.value)}
                        placeholder={reviewNotePlaceholder}
                        style={reviewTextareaStyle}
                        rows={2}
                    />
                    <div style={reviewButtonRowStyle}>
                        <button type="button" disabled={Boolean(reviewing)} onClick={() => submitReview("approved")} style={reviewButtonStyle("approved", reviewing === "approved")}>{reviewing === "approved" ? t("Recording...", "\u8bb0\u5f55\u4e2d...", "\u8a18\u9304\u4e2d...") : t("Approve", "\u6279\u51c6", "\u6279\u51c6")}</button>
                        <button type="button" disabled={Boolean(reviewing)} onClick={() => submitReview("rejected")} style={reviewButtonStyle("rejected", reviewing === "rejected")}>{reviewing === "rejected" ? t("Recording...", "\u8bb0\u5f55\u4e2d...", "\u8a18\u9304\u4e2d...") : t("Reject", "\u62d2\u7edd", "\u62d2\u7d55")}</button>
                        <button type="button" disabled={Boolean(reviewing)} onClick={() => submitReview("deferred")} style={reviewButtonStyle("deferred", reviewing === "deferred")}>{reviewing === "deferred" ? t("Recording...", "\u8bb0\u5f55\u4e2d...", "\u8a18\u9304\u4e2d...") : t("Defer", "\u5ef6\u540e", "\u5ef6\u5f8c")}</button>
                    </div>
                    {reviewError && <div role="alert" style={{ fontSize: "0.66rem", color: colors.danger, marginTop: 5 }}>{reviewError}</div>}
                    {reviewMessage && <div style={{ fontSize: "0.66rem", color: colors.success, marginTop: 5 }}>{reviewMessage}</div>}
                    {reviewRecord?.non_executing_boundary && <DetailBlock label={t("Safety Boundary", "\u5b89\u5168\u8fb9\u754c", "\u5b89\u5168\u908a\u754c")} value={reviewRecord.non_executing_boundary} />}
                    <RecommendedToolCallBlock t={t} call={reviewRecord?.recommended_tool_call} />
                </div>
            )}
            {detail.source_url && <DetailBlock label={t("Source URL", "\u6765\u6e90 URL", "\u4f86\u6e90 URL")} value={detail.source_url} monospace />}
            {detail.source_trace_id && <DetailBlock label={t("Source Trace", "\u6765\u6e90\u8f68\u8ff9", "\u4f86\u6e90\u8ecc\u8de1")} value={detail.source_trace_id} monospace />}
            {Array.isArray(detail.tags) && detail.tags.length > 0 && <TraceTags tags={detail.tags} />}
            {detail.detail && <DetailBlock label={t("Detail", "\u8be6\u60c5", "\u8a73\u60c5")} value={detail.detail} pre />}
        </div>
    );
}

function DetailMetric({ label, value }: { label: string; value: string }) {
    return (
        <div style={{ borderTop: "1px solid " + colors.borderLight, paddingTop: 5, minWidth: 0 }}>
            <div style={{ fontSize: "0.66rem", color: colors.textMuted }}>{label}</div>
            <div style={{ fontSize: "0.72rem", color: colors.text, fontWeight: 700, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{value}</div>
        </div>
    );
}

function DetailBlock({ label, value, copyValueText, pre, monospace, copyLabel, copiedLabel }: { label: string; value: string; copyValueText?: string; pre?: boolean; monospace?: boolean; copyLabel?: string; copiedLabel?: string }) {
    const [copied, setCopied] = useState(false);
    const copyValue = async () => {
        if (!copyLabel || !navigator.clipboard?.writeText) return;
        try {
            await navigator.clipboard.writeText(copyValueText || value);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1400);
        } catch {
            setCopied(false);
        }
    };
    return (
        <div style={{ marginTop: 8 }}>
            <div style={detailBlockHeaderStyle}>
                <div style={{ fontSize: "0.66rem", color: colors.textMuted, fontWeight: 700 }}>{label}</div>
                {copyLabel && <button type="button" onClick={copyValue} style={draftCopyButtonStyle}>{copied ? copiedLabel || copyLabel : copyLabel}</button>}
            </div>
            <div style={{ ...detailTextStyle, whiteSpace: pre ? "pre-wrap" : "normal", fontFamily: monospace ? "monospace" : undefined }}>{value}</div>
        </div>
    );
}

function draftCopyProps(t: Translate): { copyLabel: string; copiedLabel: string } {
    return { copyLabel: t("Copy", "\u590d\u5236", "\u8907\u88fd"), copiedLabel: t("Copied", "\u5df2\u590d\u5236", "\u5df2\u8907\u88fd") };
}

function findFocusedTrace(details: TraceDetail[], focus: string): TraceDetail | null {
    const token = focus.trim();
    if (!token) return details[0] || null;
    return details.find((detail) => traceMatchesFocus(detail, token)) || null;
}

function filterTraceDetails(details: TraceDetail[], filter: TraceFilter, reviewStatus = "", actionKind = "", followUpStatus = "", followUpTriggeredOnly = false, followUpActionKind = ""): TraceDetail[] {
    switch (filter) {
        case "review": return details.filter((detail) => detail.review_required).filter((detail) => !reviewStatus || reviewStatusOfDetail(detail) === reviewStatus);
        case "actions": return details.filter((detail) => Boolean(detail.next_action || detail.next_action_kind)).filter((detail) => !actionKind || actionKindOfDetail(detail) === actionKind);
        case "followups": return details.filter((detail) => Boolean(detail.follow_up_status)).filter((detail) => !followUpStatus || detail.follow_up_status === followUpStatus).filter((detail) => !followUpTriggeredOnly || experienceTriggeredRollbackDetail(detail)).filter((detail) => !followUpActionKind || detail.follow_up_action_kind === followUpActionKind);
        case "reviewed": return details.filter((detail) => isResolvedReviewStatus(detail.review_status)).filter((detail) => !reviewStatus || reviewStatusOfDetail(detail) === reviewStatus);
        case "a2a": return details.filter((detail) => String(detail.kind || "").startsWith("a2a_") || detail.source_type === "group_discussion");
        case "tools": return details.filter((detail) => String(detail.kind || "").includes("tool") || detail.kind === "routing_hint" || detail.kind === "usage_pattern" || detail.kind === "skill_nudge_candidate" || detail.source_type === "tool_usage");
        case "sessions": return details.filter((detail) => detail.kind === "session_history" || detail.source_type === "session_history");
        default: return details;
    }
}

function sortTraceDetailsForFocus(details: TraceDetail[], priorityTraceId: string, preferRollback: boolean): TraceDetail[] {
    if (!priorityTraceId && !preferRollback) return details;
    return [...details].sort((a, b) => {
        const aPriority = a.id === priorityTraceId ? 1 : 0;
        const bPriority = b.id === priorityTraceId ? 1 : 0;
        if (aPriority !== bPriority) return bPriority - aPriority;
        if (preferRollback) {
            const aRollback = experienceTriggeredRollbackDetail(a) ? 1 : 0;
            const bRollback = experienceTriggeredRollbackDetail(b) ? 1 : 0;
            if (aRollback !== bRollback) return bRollback - aRollback;
        }
        return String(b.updated_at || "").localeCompare(String(a.updated_at || ""));
    });
}

function findPriorityTraceForGovernanceFocus(details: TraceDetail[], filter?: TraceFilter, reviewStatus = "", actionKind = "", followUpStatus = "", followUpActionKind = "", triggeredRollbackOnly = false): string {
    if (!details.length || !filter) return "";
    const filtered = filterTraceDetails(details, filter, reviewStatus, actionKind, followUpStatus, triggeredRollbackOnly, followUpActionKind);
    const sorted = sortTraceDetailsForFocus(filtered, "", triggeredRollbackOnly || actionKind === "review_triggered_rollback_signal" || filter === "reviewed");
    return String(sorted[0]?.id || "");
}

function reviewStatusOfDetail(detail: TraceDetail | null): string {
    if (!detail) return "";
    return detail.review_status || (detail.review_required ? "required" : "");
}

function actionKindOfDetail(detail: TraceDetail): string {
    return detail.next_action_kind || (detail.next_action ? "manual_follow_up" : "");
}

function traceFilterForDetail(detail: TraceDetail | null): TraceFilter {
    if (!detail) return "all";
    if (detail.review_required) return "review";
    if (detail.next_action || detail.next_action_kind) return "actions";
    if (detail.follow_up_status) return "followups";
    if (isResolvedReviewStatus(detail.review_status)) return "reviewed";
    const kind = String(detail.kind || "");
    if (kind.startsWith("a2a_") || detail.source_type === "group_discussion") return "a2a";
    if (kind === "session_history" || detail.source_type === "session_history") return "sessions";
    if (kind.includes("tool") || kind === "routing_hint" || kind === "usage_pattern" || kind === "skill_nudge_candidate" || detail.source_type === "tool_usage") return "tools";
    return "all";
}

function traceFilterCounts(details: TraceDetail[], kindCounts: Record<string, number>, statusCounts: Record<string, number>, actionCounts: Record<string, number>, followUpCounts: Record<string, number>, total: number, reviewRequired: number, nextActions: number, followUps: number): Record<TraceFilter, number> {
    const fallback = {
        all: details.length,
        review: details.filter((detail) => detail.review_required).length,
        actions: details.filter((detail) => detail.next_action || detail.next_action_kind).length,
        followups: details.filter((detail) => detail.follow_up_status).length,
        reviewed: details.filter((detail) => isResolvedReviewStatus(detail.review_status)).length,
        a2a: filterTraceDetails(details, "a2a").length,
        tools: filterTraceDetails(details, "tools").length,
        sessions: filterTraceDetails(details, "sessions").length,
    };
    const tools = sumCountMap(kindCounts, ["routing_hint", "skill_nudge_candidate", "skill_nudge_review", "tool_recovery_pattern", "usage_pattern", "tool_memory"]);
    const a2a = sumCountMap(kindCounts, ["a2a_discussion_result", "a2a_conflict_review", "a2a_rollback_review"]);
    const actionTotal = sumCountMap(actionCounts, Object.keys(actionCounts));
    const followUpTotal = sumCountMap(followUpCounts, Object.keys(followUpCounts));
    return {
        all: total || fallback.all,
        review: reviewRequired || fallback.review,
        actions: nextActions || actionTotal || fallback.actions,
        followups: followUps || followUpTotal || fallback.followups,
        reviewed: (statusCounts.approved || 0) + (statusCounts.rejected || 0) || fallback.reviewed,
        a2a: a2a || fallback.a2a,
        tools: tools || fallback.tools,
        sessions: kindCounts.session_history || fallback.sessions,
    };
}

function isResolvedReviewStatus(status?: string): boolean {
    return status === "approved" || status === "rejected";
}

function toCountMap(value: any): Record<string, number> {
    if (!value || typeof value !== "object") return {};
    const out: Record<string, number> = {};
    Object.keys(value).forEach((key) => { out[key] = toSafeCount(value[key], 0); });
    return out;
}

function asRecord(value: any): Record<string, any> {
    return value && typeof value === "object" ? value : {};
}

function governanceTrackLine(parts: { label: string; value: any }[]): string {
    return parts.map((part) => part.label + ": " + toSafeCount(part.value, 0)).join(" | ");
}

function governanceRoutingQueryLine(t: Translate, query: Record<string, any>): string {
    const parts = [
        [t("Task", "\u4efb\u52a1", "\u4efb\u52d9"), query.task_type],
        [t("Tool", "\u5de5\u5177", "\u5de5\u5177"), query.tool],
        [t("Query", "\u67e5\u8be2", "\u67e5\u8a62"), query.query],
    ].filter((part) => String(part[1] || "").trim());
    return parts.map((part) => part[0] + ": " + String(part[1]).trim()).join(" | ");
}

function formatGovernanceRecommendedToolCall(call: Record<string, any>, traceId = "", traceTitle = "", traceReason = ""): string {
    if (!call.tool && !call.args) return "";
    try {
        const lines: string[] = [];
        const toolName = String(call.tool || "").trim();
        if (toolName) lines.push("工具: " + toolName);
        const args = call.args;
        if (args && typeof args === "object") {
            const argEntries = Object.entries(args).filter(([, v]) => v !== undefined && v !== null && v !== "");
            if (argEntries.length > 0) {
                lines.push("参数:");
                for (const [key, value] of argEntries) {
                    const displayValue = typeof value === "object" ? JSON.stringify(value) : String(value);
                    const truncated = displayValue.length > 120 ? displayValue.slice(0, 117) + "..." : displayValue;
                    lines.push("  " + key + ": " + truncated);
                }
            }
        }
        const targetID = String(traceId || "").trim();
        const targetTitle = String(traceTitle || "").trim();
        const reason = String(traceReason || "").trim();
        if (targetTitle || targetID) lines.push("关联轨迹: " + (targetTitle || targetID));
        if (reason) lines.push("原因: " + reason);
        const boundary = String(call.non_executing_boundary || "").trim();
        if (call.non_executing) lines.push("⚠️ 仅供参考，不会自动执行");
        if (boundary) lines.push("安全边界: " + boundary);
        return lines.join("\n");
    } catch {
        return "";
    }
}

function formatGovernanceFocusContext(context: Record<string, any>): string {
    if (Object.keys(context).length === 0) return "";
    try {
        const lines: string[] = [];
        const traceId = String(context.priority_trace_id || context.recommended_trace_id || "").trim();
        const traceTitle = String(context.priority_trace_title || context.recommended_title || "").trim();
        const reason = String(context.reason || "").trim();
        if (traceTitle) lines.push("优先轨迹: " + traceTitle);
        else if (traceId) lines.push("优先轨迹: " + traceId);
        if (traceTitle && traceId) lines.push("轨迹 ID: " + traceId);
        if (reason) lines.push("原因: " + reason);
        // Fallback for any other keys not covered above
        for (const [key, value] of Object.entries(context)) {
            if (["priority_trace_id", "priority_trace_title", "recommended_trace_id", "recommended_title", "reason"].includes(key)) continue;
            if (value === undefined || value === null || value === "") continue;
            if (typeof value === "object" && !Array.isArray(value)) {
                // Flatten one level of nested objects
                for (const [subKey, subValue] of Object.entries(value as Record<string, any>)) {
                    if (subValue === undefined || subValue === null || subValue === "") continue;
                    const sv = typeof subValue === "object" ? JSON.stringify(subValue) : String(subValue);
                    lines.push(key + "." + subKey + ": " + sv);
                }
            } else {
                const displayValue = Array.isArray(value) ? value.join(", ") : String(value);
                lines.push(key + ": " + displayValue);
            }
        }
        return lines.join("\n") || "";
    } catch {
        return "";
    }
}

function fallbackGovernanceTraceFilter(action: string): TraceFilter | undefined {
    switch (action) {
        case "review_required_traces":
        case "review_signal":
            return "review";
        case "review_triggered_rollback_signal":
            return "actions";
        case "inspect_follow_up_actions":
        case "inspect_triggered_rollback_followups":
            return "followups";
        case "review_routing_candidates":
        case "inspect_routing_signals":
        case "inspect_skill_nudge_candidates":
        case "inspect_tool_recovery_governance":
            return "tools";
        case "inspect_memory_candidates":
        case "build_memory_maintenance_draft":
        case "normal_operation":
            return "all";
        default:
            return undefined;
    }
}

function fallbackGovernanceActionKind(action: string): string {
    return action === "review_triggered_rollback_signal" ? "review_triggered_rollback_signal" : "";
}

function fallbackGovernanceFollowUpActionKind(action: string): string {
    return action === "inspect_triggered_rollback_followups" ? "draft_rollback_workflow" : "";
}

function fallbackGovernanceTriggeredRollbackOnly(action: string): boolean {
    return action === "inspect_triggered_rollback_followups";
}

function formatRoutingAdjustment(value: any): string {
    const n = Number(value);
    if (!Number.isFinite(n) || n === 0) return "0";
    return (n > 0 ? "+" : "") + n.toFixed(3);
}

function formatTriggeredRollbackSummaryLine(t: Translate, triggeredCount?: number, note?: string): string {
    const count = toSafeCount(triggeredCount, 0);
    const label = t("Rollback audit", "\u56de\u6eda\u5ba1\u8ba1", "\u56de\u6efe\u5be9\u8a08");
    const meta = count > 0 ? label + " x" + count : label;
    const suffix = String(note || "").trim();
    if (!suffix) return meta;
    return meta + " | " + suffix;
}

function sortByPriorityAndRecency<T>(items: T[], score: (item: T) => number, count: (item: T) => number, updatedAt: (item: T) => string): T[] {
    return [...items].sort((a, b) => {
        const scoreDiff = score(b) - score(a);
        if (scoreDiff !== 0) return scoreDiff;
        const countDiff = count(b) - count(a);
        if (countDiff !== 0) return countDiff;
        return String(updatedAt(b) || "").localeCompare(String(updatedAt(a) || ""));
    });
}

function reviewSummaryIsTriggeredRollback(item: ReviewSummary): boolean {
    return item.latest_kind === "a2a_rollback_review"
        || String(item.latest_action || "").includes("review_triggered_rollback_signal")
        || String(item.latest_note || "").toLowerCase().includes("rollback");
}

function rankReviewSummaries(items: ReviewSummary[]): ReviewSummary[] {
    return sortByPriorityAndRecency(items, (item) => reviewSummaryIsTriggeredRollback(item) ? 1 : 0, (item) => toSafeCount(item.count, 0), (item) => String(item.latest_updated_at || item.latest_reviewed_at || ""));
}

function rankNextActionSummaries(items: NextActionSummary[]): NextActionSummary[] {
    return sortByPriorityAndRecency(items, (item) => item.kind === "review_triggered_rollback_signal" ? 1 : 0, (item) => toSafeCount(item.count, 0), (item) => String(item.latest_updated_at || ""));
}

function rankFollowUpSummaries(items: FollowUpSummary[]): FollowUpSummary[] {
    return sortByPriorityAndRecency(items, (item) => item.triggered_rollback ? 1 : 0, (item) => toSafeCount(item.triggered_count, toSafeCount(item.count, 0)), (item) => String(item.latest_updated_at || ""));
}

function rankFollowUpActionSummaries(items: FollowUpActionSummary[]): FollowUpActionSummary[] {
    return sortByPriorityAndRecency(items, (item) => item.triggered_rollback ? 1 : 0, (item) => toSafeCount(item.triggered_count, toSafeCount(item.count, 0)), (item) => String(item.latest_updated_at || ""));
}

function sumCountMap(counts: Record<string, number>, keys: string[]): number {
    return keys.reduce((total, key) => total + (counts[key] || 0), 0);
}

function traceMatchesFocus(detail: TraceDetail, token: string): boolean {
    if (detail.id === token || detail.source_url === token || detail.kind === token) return true;
    const tags = Array.isArray(detail.tags) ? detail.tags : [];
    if (tags.includes(token)) return true;
    const discussionID = normalizeDiscussionFocus(token);
    if (!discussionID) return false;
    return tags.includes("discussion:" + discussionID) || detail.source_url === "a2a://current_hub/" + discussionID;
}

function normalizeDiscussionFocus(token: string): string {
    if (token.startsWith("discussion:")) return token.slice("discussion:".length).trim();
    if (token.startsWith("a2a://current_hub/")) return token.slice("a2a://current_hub/".length).trim();
    return token.trim();
}

function TraceTags({ tags }: { tags: string[] }) {
    return (
        <div style={{ display: "flex", flexWrap: "wrap", gap: 4, marginTop: 8 }}>
            {tags.slice(0, 10).map((tag) => <span key={tag} style={tagStyle}>{tag}</span>)}
        </div>
    );
}

const detailTextStyle: CSSProperties = { fontSize: "0.7rem", color: colors.textSecondary, lineHeight: 1.5, overflowWrap: "anywhere" };
const tagStyle: CSSProperties = { border: "1px solid " + colors.borderLight, borderRadius: radius.pill, padding: "1px 6px", fontSize: "0.64rem", color: colors.textSecondary, background: colors.bg };
const kindBadgeStyle: CSSProperties = { border: "1px solid " + colors.primary, borderRadius: radius.pill, padding: "1px 7px", fontSize: "0.64rem", fontWeight: 700, background: colors.bg };
const memoryMaintenanceNoticeStyle: CSSProperties = { borderTop: "1px solid " + colors.borderLight, paddingTop: 8, marginBottom: 10 };
const governanceNoticeStyle: CSSProperties = { ...memoryMaintenanceNoticeStyle, borderTopColor: colors.border };
const governanceNoticeAlertStyle: CSSProperties = { ...governanceNoticeStyle, borderTopColor: colors.warning };
const governanceTrackGridStyle: CSSProperties = { display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(160px, 1fr))", gap: 8, marginTop: 8 };
const governanceTrackStyle: CSSProperties = { borderTop: "1px solid " + colors.borderLight, paddingTop: 6, minWidth: 0 };
const governancePriorityTraceStyle: CSSProperties = { marginTop: 6, padding: "7px 9px", borderRadius: radius.sm, border: "1px solid " + colors.borderLight, background: colors.surfaceMuted };
const governanceBoundaryStyle: CSSProperties = { marginTop: 7, fontSize: "0.62rem", color: colors.textMuted, lineHeight: 1.4, overflowWrap: "anywhere" };
const governanceFocusButtonStyle: CSSProperties = { border: "1px solid " + colors.border, borderRadius: radius.pill, background: colors.bg, color: colors.textSecondary, padding: "1px 7px", fontSize: "0.62rem", fontWeight: 700, cursor: "pointer", lineHeight: 1.4 };
const governanceQueryRowStyle: CSSProperties = { display: "flex", gap: 6, alignItems: "center", flexWrap: "wrap", marginTop: 8 };
const governanceQueryInputStyle: CSSProperties = { flex: "1 1 220px", minWidth: 160, boxSizing: "border-box", border: "1px solid " + colors.borderLight, borderRadius: radius.sm, background: colors.bg, color: colors.text, padding: "5px 7px", fontSize: "0.68rem", lineHeight: 1.4 };
const governanceToolInputStyle: CSSProperties = { ...governanceQueryInputStyle, flex: "0 1 130px" };
const governanceRoutingPreviewStyle: CSSProperties = { borderTop: "1px solid " + colors.borderLight, marginTop: 8, paddingTop: 7 };
const maintenanceTextStyle: CSSProperties = { fontSize: "0.7rem", color: colors.textSecondary, lineHeight: 1.45, overflowWrap: "anywhere" };
const maintenanceMetaStyle: CSSProperties = { ...maintenanceTextStyle, color: colors.textMuted, marginTop: 3 };
const warningBadgeStyle: CSSProperties = { border: "1px solid " + colors.warning, borderRadius: radius.pill, padding: "1px 7px", fontSize: "0.64rem", fontWeight: 700, color: colors.warning, background: colors.warningBg };
const neutralBadgeStyle: CSSProperties = { border: "1px solid " + colors.borderLight, borderRadius: radius.pill, padding: "1px 7px", fontSize: "0.64rem", fontWeight: 700, color: colors.textMuted, background: colors.bg };
const followUpPanelStyle: CSSProperties = { marginTop: 8, borderTop: "1px solid " + colors.borderLight, paddingTop: 8 };
const triggeredRollbackNoticeStyle: CSSProperties = { marginTop: 8, border: "1px solid " + colors.warning, borderRadius: radius.md, background: colors.warningBg, color: colors.textSecondary, padding: "7px 8px", fontSize: "0.68rem", lineHeight: 1.45, overflowWrap: "anywhere" };
const detailBlockHeaderStyle: CSSProperties = { display: "flex", alignItems: "center", justifyContent: "space-between", gap: 6, marginBottom: 3 };
const draftCopyButtonStyle: CSSProperties = { border: "1px solid " + colors.border, borderRadius: radius.pill, background: colors.bg, color: colors.textSecondary, padding: "1px 7px", fontSize: "0.62rem", fontWeight: 700, cursor: "pointer", lineHeight: 1.4 };
const draftQueryInputStyle: CSSProperties = { width: "100%", boxSizing: "border-box", marginTop: 6, border: "1px solid " + colors.borderLight, borderRadius: radius.sm, background: colors.bg, color: colors.text, padding: "5px 7px", fontSize: "0.68rem", lineHeight: 1.4 };
const reviewPanelStyle: CSSProperties = { marginTop: 8, border: "1px solid " + colors.borderLight, borderRadius: radius.md, padding: 8, background: colors.bg };
const reviewTextareaStyle: CSSProperties = { width: "100%", resize: "vertical", minHeight: 48, boxSizing: "border-box", border: "1px solid " + colors.border, borderRadius: radius.sm, background: colors.surface, color: colors.text, padding: "6px 8px", fontSize: "0.7rem", lineHeight: 1.45 };
const reviewButtonRowStyle: CSSProperties = { display: "flex", flexWrap: "wrap", gap: 6, marginTop: 6 };
const traceFilterBarStyle: CSSProperties = { display: "flex", gap: 6, flexWrap: "wrap", marginBottom: 8 };
const traceFilterCountStyle: CSSProperties = { marginLeft: 4, color: colors.textMuted, fontVariantNumeric: "tabular-nums" };
const traceSafetyBoundaryStyle: CSSProperties = { marginTop: -2, marginBottom: 8, border: "1px solid " + colors.borderLight, borderRadius: radius.sm, background: colors.bg, color: colors.textMuted, padding: "6px 8px", fontSize: "0.65rem", lineHeight: 1.45, overflowWrap: "anywhere" };
const actionFocusStyle: CSSProperties = { display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap", marginBottom: 8, fontSize: "0.66rem", color: colors.textSecondary };
const actionFocusHintStyle: CSSProperties = { marginTop: -3, marginBottom: 8, fontSize: "0.65rem", color: colors.textMuted, lineHeight: 1.45, overflowWrap: "anywhere" };
const actionFocusClearStyle: CSSProperties = { border: "1px solid " + colors.border, borderRadius: radius.pill, background: colors.bg, color: colors.textSecondary, padding: "1px 7px", fontSize: "0.64rem", fontWeight: 700, cursor: "pointer" };
const summaryAuditLineStyle: CSSProperties = { display: "block", marginTop: 1, color: colors.warning, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontSize: "0.64rem", fontWeight: 600 };
const traceRecommendationBadgeStyle: CSSProperties = { display: "inline-block", marginTop: 3, border: "1px solid " + colors.warning, borderRadius: radius.pill, background: colors.warningBg, color: colors.warning, padding: "1px 6px", fontSize: "0.62rem", fontWeight: 700, maxWidth: "100%", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" };
const traceRecommendationReasonStyle: CSSProperties = { display: "block", marginTop: 2, fontSize: "0.64rem", color: colors.textMuted, lineHeight: 1.4, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" };
const nextActionSummaryStyle: CSSProperties = { border: "1px solid " + colors.borderLight, borderRadius: radius.md, background: colors.bg, color: colors.text, padding: "6px 8px", textAlign: "left", cursor: "pointer", minWidth: 0, fontSize: "0.68rem", fontWeight: 700 };
const warningSummaryCardStyle: CSSProperties = { ...nextActionSummaryStyle, border: "1px solid " + colors.warning, background: colors.warningBg };
const protectedMemoryCardStyle: CSSProperties = { ...nextActionSummaryStyle, cursor: "default" };

function nextActionSummaryCardStyle(kind?: string): CSSProperties {
    if (kind === "review_triggered_rollback_signal") {
        return { ...nextActionSummaryStyle, border: "1px solid " + colors.warning, background: colors.warningBg };
    }
    return nextActionSummaryStyle;
}

function reviewSummaryCardStyle(item: ReviewSummary): CSSProperties {
    if (reviewSummaryIsTriggeredRollback(item)) {
        return warningSummaryCardStyle;
    }
    return nextActionSummaryStyle;
}

function traceButtonStyle(active: boolean): CSSProperties {
    return {
        width: "100%",
        textAlign: "left",
        border: active ? "1px solid " + colors.primary : "1px solid transparent",
        borderRadius: radius.md,
        background: active ? colors.primaryLight : "transparent",
        color: colors.text,
        padding: "6px 8px",
        cursor: "pointer",
        marginBottom: 4,
        fontSize: "0.72rem",
    };
}

function traceFilterButtonStyle(active: boolean): CSSProperties {
    return {
        border: "1px solid " + (active ? colors.primary : colors.border),
        borderRadius: radius.pill,
        background: active ? colors.primaryLight : colors.bg,
        color: active ? colors.primaryDark : colors.textSecondary,
        padding: "3px 9px",
        fontSize: "0.66rem",
        fontWeight: 700,
        cursor: "pointer",
        lineHeight: 1.5,
    };
}

function followUpButtonStyle(active: boolean): CSSProperties {
    return {
        border: "1px solid " + colors.primary,
        borderRadius: radius.md,
        background: active ? colors.primaryLight : colors.bg,
        color: colors.primaryDark,
        padding: "4px 9px",
        fontSize: "0.66rem",
        fontWeight: 700,
        cursor: active ? "wait" : "pointer",
        lineHeight: 1.4,
    };
}

function reviewButtonStyle(kind: "approved" | "rejected" | "deferred", active: boolean): CSSProperties {
    const tone = kind === "approved" ? colors.success : kind === "rejected" ? colors.danger : colors.warning;
    const bg = kind === "approved" ? colors.successBg : kind === "rejected" ? colors.dangerBg : colors.warningBg;
    return {
        border: "1px solid " + tone,
        borderRadius: radius.md,
        background: active ? bg : colors.surface,
        color: tone,
        padding: "4px 9px",
        fontSize: "0.66rem",
        fontWeight: 700,
        cursor: active ? "wait" : "pointer",
        lineHeight: 1.4,
    };
}

function followUpOutcomeButtonStyle(kind: "completed" | "blocked" | "deferred", active: boolean): CSSProperties {
    const tone = kind === "completed" ? colors.success : kind === "blocked" ? colors.danger : colors.warning;
    const bg = kind === "completed" ? colors.successBg : kind === "blocked" ? colors.dangerBg : colors.warningBg;
    return {
        border: "1px solid " + tone,
        borderRadius: radius.md,
        background: active ? bg : colors.surface,
        color: tone,
        padding: "4px 9px",
        fontSize: "0.66rem",
        fontWeight: 700,
        cursor: active ? "wait" : "pointer",
        lineHeight: 1.4,
    };
}

function reviewStatusBadgeStyle(status: string): CSSProperties {
    const tone = status === "approved" ? colors.success : status === "rejected" ? colors.danger : colors.warning;
    const bg = status === "approved" ? colors.successBg : status === "rejected" ? colors.dangerBg : colors.warningBg;
    return {
        display: "inline-block",
        marginTop: 3,
        borderRadius: radius.pill,
        border: "1px solid " + tone,
        color: tone,
        background: bg,
        padding: "0 6px",
        fontSize: "0.62rem",
        fontWeight: 700,
    };
}

function formatReviewStatus(t: Translate, status?: string): string {
    switch (status) {
        case "approved": return t("approved", "\u5df2\u6279\u51c6", "\u5df2\u6279\u51c6");
        case "rejected": return t("rejected", "\u5df2\u62d2\u7edd", "\u5df2\u62d2\u7d55");
        case "deferred": return t("deferred", "\u5df2\u5ef6\u540e", "\u5df2\u5ef6\u5f8c");
        case "required": return t("review", "\u5f85\u8bc4\u5ba1", "\u5f85\u8a55\u5be9");
        default: return "";
    }
}

function formatFollowUpStatus(t: Translate, status?: string): string {
    switch (status) {
        case "completed": return t("completed", "\u5df2\u5b8c\u6210", "\u5df2\u5b8c\u6210");
        case "blocked": return t("blocked", "\u5df2\u963b\u585e", "\u5df2\u963b\u585e");
        case "deferred": return t("deferred", "\u5df2\u5ef6\u540e", "\u5df2\u5ef6\u5f8c");
        default: return "";
    }
}

function formatNextActionKind(t: Translate, kind?: string): string {
    switch (kind) {
        case "review_required_traces": return t("Review queue", "\u8bc4\u5ba1\u961f\u5217", "\u8a55\u5be9\u961f\u5217");
        case "review_routing_candidates": return t("Routing candidates", "\u8def\u7531\u5019\u9009", "\u8def\u7531\u5019\u9078");
        case "build_memory_maintenance_draft": return t("Maintenance draft", "\u7ef4\u62a4\u8349\u6848", "\u7dad\u8b77\u8349\u6848");
        case "inspect_follow_up_actions": return t("Follow-up actions", "\u540e\u7eed\u52a8\u4f5c", "\u5f8c\u7e8c\u52d5\u4f5c");
        case "inspect_triggered_rollback_followups": return t("Triggered rollback follow-ups", "\u89e6\u53d1\u56de\u6eda\u540e\u7eed", "\u89f8\u767c\u56de\u6efe\u5f8c\u7e8c");
        case "inspect_skill_nudge_candidates": return t("Skill candidates", "\u6280\u80fd\u5019\u9009", "\u6280\u80fd\u5019\u9078");
        case "inspect_routing_signals": return t("Routing signals", "\u8def\u7531\u4fe1\u53f7", "\u8def\u7531\u8a0a\u865f");
        case "inspect_memory_candidates": return t("Memory candidates", "\u8bb0\u5fc6\u5019\u9009", "\u8a18\u61b6\u5019\u9078");
        case "normal_operation": return t("Normal operation", "\u5e38\u89c4\u8fd0\u884c", "\u5e38\u898f\u904b\u884c");
        case "review_signal": return t("Review signal", "\u5ba1\u9605\u4fe1\u53f7", "\u5be9\u95b1\u8a0a\u865f");
        case "review_triggered_rollback_signal": return t("Triggered rollback review", "\u89e6\u53d1\u56de\u6eda\u590d\u6838", "\u89f8\u767c\u56de\u6efe\u8907\u6838");
        case "draft_rollback_workflow": return t("Rollback workflow", "\u56de\u6eda\u6d41\u7a0b", "\u56de\u6efe\u6d41\u7a0b");
        case "draft_skill_manually": return t("Skill draft", "\u6280\u80fd\u8349\u7a3f", "\u6280\u80fd\u8349\u7a3f");
        case "prepare_escalation_brief": return t("Escalation brief", "\u5347\u7ea7\u7b80\u62a5", "\u5347\u7d1a\u7c21\u5831");
        case "collect_a2a_conflict_evidence": return t("Conflict evidence", "\u51b2\u7a81\u8865\u8bc1", "\u885d\u7a81\u88dc\u8b49");
        case "resolve_a2a_conflict_manually": return t("Conflict reconcile", "\u51b2\u7a81\u8c03\u548c", "\u885d\u7a81\u8abf\u548c");
        case "collect_rollback_evidence": return t("Rollback evidence", "\u56de\u6eda\u8865\u8bc1", "\u56de\u6efe\u88dc\u8b49");
        case "block_rollback_use": return t("Block rollback", "\u963b\u6b62\u56de\u6eda", "\u963b\u6b62\u56de\u6efe");
        case "suppress_skill_candidate": return t("Suppress skill", "\u6291\u5236\u6280\u80fd", "\u6291\u5236\u6280\u80fd");
        case "memory_maintenance_draft": return t("Maintenance Draft", "\u7ef4\u62a4\u8349\u6848", "\u7dad\u8b77\u8349\u6848");
        case "routing_adjustment_draft": return t("Routing Draft", "\u8def\u7531\u8349\u6848", "\u8def\u7531\u8349\u6848");
        default: return kind || t("Manual follow-up", "\u4eba\u5de5\u8ddf\u8fdb", "\u4eba\u5de5\u8ddf\u9032");
    }
}

function formatProtectedReason(t: Translate, reason?: string): string {
    switch (reason) {
        case "pinned": return t("pinned", "\u56fa\u5b9a", "\u56fa\u5b9a");
        case "instruction": return t("instruction", "\u6307\u4ee4", "\u6307\u4ee4");
        case "self_identity": return t("identity", "\u8eab\u4efd", "\u8eab\u4efd");
        case "high_strength": return t("strong", "\u9ad8\u5f3a\u5ea6", "\u9ad8\u5f37\u5ea6");
        case "a2a_discussion": return "A2A";
        case "tool_usage": return t("tool", "\u5de5\u5177", "\u5de5\u5177");
        case "swarm_trace": return t("swarm", "\u7fa4\u4f53", "\u7fa4\u9ad4");
        default: return reason || "-";
    }
}

function formatProtectedSource(t: Translate, source?: string): string {
    switch (source) {
        case "conversation": return t("Conversation", "\u4f1a\u8bdd", "\u6703\u8a71");
        case "workflow": return t("Workflow", "\u5de5\u4f5c\u6d41", "\u5de5\u4f5c\u6d41");
        case "swarm": return t("Swarm", "\u7fa4\u4f53", "\u7fa4\u9ad4");
        case "a2a_discussion": return "A2A";
        case "tool_usage": return t("Tool Usage", "\u5de5\u5177\u4f7f\u7528", "\u5de5\u5177\u4f7f\u7528");
        case "manual": return t("Manual", "\u624b\u52a8", "\u624b\u52d5");
        default: return source || "-";
    }
}

function formatKind(t: Translate, kind?: string): string {
    switch (kind) {
        case "routing_hint": return t("Routing", "\u8def\u7531", "\u8def\u7531");
        case "skill_nudge_candidate": return t("Skill", "\u6280\u80fd", "\u6280\u80fd");
        case "skill_nudge_review": return t("Skill Review", "\u6280\u80fd\u5ba1\u9605", "\u6280\u80fd\u5be9\u95b1");
        case "tool_recovery_pattern": return t("Recovery", "\u6062\u590d", "\u6062\u5fa9");
        case "usage_pattern": return t("Usage", "\u7528\u6cd5", "\u7528\u6cd5");
        case "a2a_discussion_result": return "A2A";
        case "a2a_conflict_review": return t("A2A Review", "A2A \u8bc4\u5ba1", "A2A \u8a55\u5be9");
        case "a2a_rollback_review": return t("Rollback", "\u56de\u6eda", "\u56de\u6efe");
        case "a2a_escalation_evidence": return t("Escalation", "\u5347\u7ea7", "\u5347\u7d1a");
        case "memory_maintenance_draft_review": return t("Maintenance Draft", "\u7ef4\u62a4\u8349\u6848", "\u7dad\u8b77\u8349\u6848");
        case "routing_adjustment_draft_review": return t("Routing Draft", "\u8def\u7531\u8349\u6848", "\u8def\u7531\u8349\u6848");
        case "skill_draft_review": return t("Skill Draft", "\u6280\u80fd\u8349\u7a3f", "\u6280\u80fd\u8349\u7a3f");
        case "rollback_workflow_draft_review": return t("Rollback Draft", "\u56de\u6eda\u8349\u7a3f", "\u56de\u6efe\u8349\u7a3f");
        case "escalation_brief_review": return t("Escalation Brief", "\u5347\u7ea7\u7b80\u62a5", "\u5347\u7d1a\u7c21\u5831");
        case "conflict_reconciliation_draft_review": return t("Conflict Draft", "\u51b2\u7a81\u8349\u7a3f", "\u885d\u7a81\u8349\u7a3f");
        case "experience_draft_review": return t("Draft Review", "\u8349\u6848\u5ba1\u9605", "\u8349\u6848\u5be9\u95b1");
        case "tool_memory": return t("Tool Memory", "\u5de5\u5177\u8bb0\u5fc6", "\u5de5\u5177\u8a18\u61b6");
        case "session_history": return t("Session", "\u4f1a\u8bdd", "\u6703\u8a71");
        default: return t("Signal", "\u4fe1\u53f7", "\u8a0a\u865f");
    }
}

function toSafeCount(value: any, fallback: number): number {
    const n = Number(value ?? fallback);
    return Number.isFinite(n) && n > 0 ? Math.round(n) : 0;
}

function joinSignalList(value: any, sep = ", "): string {
    return Array.isArray(value) && value.length > 0 ? value.join(sep) : "-";
}

function formatConfidence(value: any): string {
    const n = Number(value);
    if (!Number.isFinite(n) || n <= 0) return "-";
    return Math.round(n * 100) + "%";
}

function formatDate(value: string): string {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString();
}
