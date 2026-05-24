import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

vi.mock('../../../../wailsjs/go/main/App', () => ({
    BuildExperienceBlockedSkillDraft: vi.fn(),
    BuildExperienceConflictReconciliationDraft: vi.fn(),
    BuildExperienceEscalationBrief: vi.fn(),
    BuildExperienceMemoryMaintenanceDraft: vi.fn(),
    BuildExperienceRollbackWorkflowDraft: vi.fn(),
    BuildExperienceRoutingAdjustmentDraft: vi.fn(),
    BuildExperienceSkillDraft: vi.fn(),
    BuildExperienceTraceFollowUp: vi.fn(),
    ConfirmPreviewedSkillDraftReview: vi.fn(),
    GetExperienceGovernanceSummary: vi.fn(),
    RecordBlockedSkillDraftReview: vi.fn(),
    RecordExperienceDraftReview: vi.fn(),
    RecordExperienceTraceFollowUp: vi.fn(),
    ReviewExperienceTrace: vi.fn(),
}));

import { BuildExperienceBlockedSkillDraft, ConfirmPreviewedSkillDraftReview, RecordBlockedSkillDraftReview } from '../../../../wailsjs/go/main/App';
import { ExperienceLearningPanel } from '../MemoryExperienceLearningPanel';

const t = (en: string) => en;

function recoveryLearning() {
    return {
        routing_hints: [],
        skill_nudge_candidates: [],
        usage_patterns: [],
        recovery_patterns: [],
        trace_kind_counts: { tool_recovery_pattern: 1 },
        trace_source_counts: { tool_usage: 1 },
        review_status_counts: {},
        next_action_kind_counts: {},
        follow_up_status_counts: {},
        follow_up_action_kind_counts: {},
        review_summaries: [],
        next_action_summaries: [],
        follow_up_summaries: [],
        follow_up_action_summaries: [],
        trace_detail_count: 1,
        trace_details: [{
            id: 'memory:adaptive-retry-browser_open-network',
            kind: 'tool_recovery_pattern',
            title: 'Browser retry recovery',
            source_type: 'tool_usage',
            review_required: false,
            detail: 'Provider: ChatFire\nModel: gpt-5.1-codex-mini\nWire API: responses',
        }],
        governance_summary: {
            recommended_next_action: 'inspect_tool_recovery_governance',
            recommended_next_action_reason: 'tool recovery evidence exists and can be inspected without changing routing or retry policy',
            recommended_focus: { trace_filter: 'tools', non_executing: true },
            recommended_focus_context: {
                recommended_trace_id: 'memory:adaptive-retry-browser_open-network',
                recommended_title: 'Browser retry recovery',
                reason: 'inspect repeated tool failure recovery windows',
                provider_counts: { ChatFire: 1 },
                model_counts: { 'gpt-5.1-codex-mini': 1 },
                wire_api_counts: { responses: 1 },
            },
            recommended_tool_call: {
                tool: 'experience_learning',
                args: { action: 'tool_recovery', limit: 20 },
                non_executing: true,
                recommended_focus_context: {
                    recommended_trace_id: 'memory:adaptive-retry-browser_open-network',
                    recommended_title: 'Browser retry recovery',
                },
            },
            routing_self_evolution: {
                routing_hint_count: 0,
                skill_nudge_count: 0,
                recovery_pattern_count: 0,
                usage_pattern_count: 0,
                tool_recovery_governance: {
                    count: 1,
                    review_required_count: 1,
                    disabled_count: 0,
                    provider_counts: { ChatFire: 1 },
                    model_counts: { 'gpt-5.1-codex-mini': 1 },
                    wire_api_counts: { responses: 1 },
                    category_counts: { network: 1 },
                    tool_counts: { browser_open: 1 },
                },
            },
            memory: {},
            a2a_discussion: {},
            queues: { trace_detail_count: 1 },
        },
    };
}

describe('ExperienceLearningPanel tool recovery governance', () => {
    it('renders recovery governance counts and recommended trace context', () => {
        render(<ExperienceLearningPanel t={t} learning={recoveryLearning()} error="" />);

        expect(screen.getByText('Governance Summary')).toBeTruthy();
        expect(screen.getAllByText('Browser retry recovery').length).toBeGreaterThan(0);
        expect(screen.getByText((text) => text.includes('Tool recovery governance') && text.includes('Windows: 1') && text.includes('Review: 1'))).toBeTruthy();
        expect(screen.getByText((text) => text.includes('Provider: ChatFire'))).toBeTruthy();
    });

    it('focuses tool recovery governance on tools without losing recommended trace', () => {
        render(<ExperienceLearningPanel t={t} learning={recoveryLearning()} error="" />);

        fireEvent.click(screen.getByRole('button', { name: 'Focus' }));

        expect(screen.getAllByText('Browser retry recovery').length).toBeGreaterThan(0);
        expect(screen.getAllByText((text) => text.includes('inspect repeated tool failure recovery windows')).length).toBeGreaterThan(0);
    });
});

function blockedSkillLearning() {
    return {
        routing_hints: [],
        skill_nudge_candidates: [],
        usage_patterns: [],
        recovery_patterns: [],
        trace_kind_counts: { skill_draft_review: 1 },
        trace_source_counts: { experience_learning: 1 },
        review_status_counts: {},
        next_action_kind_counts: {},
        follow_up_status_counts: {},
        follow_up_action_kind_counts: {},
        review_summaries: [],
        next_action_summaries: [],
        follow_up_summaries: [],
        follow_up_action_summaries: [],
        trace_detail_count: 1,
        trace_details: [{
            id: 'memory:blocked-skill-review',
            kind: 'skill_draft_review',
            title: 'Blocked skill review',
            source_type: 'experience_learning',
            review_required: false,
            draft_id: 'skill_draft:mark_needs_review:broken:',
            draft_execution_status: 'blocked',
            draft_execution_note: 'current plan no longer contains reviewed draft',
        }, {
            id: 'memory:previewed-skill-review',
            kind: 'skill_draft_review',
            title: 'Previewed skill review',
            source_type: 'experience_learning',
            review_required: false,
            draft_id: 'skill_draft:mark_needs_review:previewed:',
            draft_execution_status: 'previewed',
        }],
        governance_summary: {
            queues: {
                blocked_skill_draft_review_count: 1,
                skill_draft_review_queues: {
                    previewed_waiting_confirm: [{
                        trace_id: 'memory:previewed-skill-review',
                        title: 'Previewed skill review',
                        draft_id: 'skill_draft:mark_needs_review:previewed:',
                        execution_status: 'previewed',
                        execution_affordances: [{ id: 'confirm_previewed_skill_draft', label: 'Confirm previewed draft' }],
                    }],
                    blocked: [{
                        trace_id: 'memory:blocked-skill-review',
                        title: 'Blocked skill review',
                        draft_id: 'skill_draft:mark_needs_review:broken:',
                        execution_status: 'blocked',
                        stale: true,
                        stale_recommendation: 'blocked skill draft is stale',
                    }],
                },
            },
        },
    };
}

describe('ExperienceLearningPanel blocked skill draft affordances', () => {
    it('renders close and reopen actions from backend affordance metadata', async () => {
        vi.mocked(BuildExperienceBlockedSkillDraft).mockResolvedValueOnce({
            draft_id: 'skill_draft:mark_needs_review:broken:',
            execution_status: 'blocked',
            draft_markdown: '# Blocked Skill Draft Repair/Evidence Draft',
            non_executing_boundary: 'read-only blocked skill draft repair/evidence draft',
            review_affordances: [
                {
                    id: 'close',
                    label: 'Close blocked draft',
                    intent: 'close_blocked_skill_draft',
                    tool_call: { tool: 'experience_learning', args: { action: 'record_blocked_skill_draft_review', resolution: 'close' } },
                },
                {
                    id: 'reopen',
                    label: 'Reopen with replacement draft',
                    intent: 'reopen_blocked_skill_draft',
                    required_inputs: [{ name: 'replacement_draft_id', required: true, placeholder: 'skill_draft:...' }],
                    tool_call: { tool: 'experience_learning', args: { action: 'record_blocked_skill_draft_review', resolution: 'reopen' } },
                },
            ],
        });
        vi.mocked(RecordBlockedSkillDraftReview).mockResolvedValueOnce({
            kind: 'skill_draft_review',
            status: 'completed',
            recommended_tool_call: { tool: 'manage_skill', args: { action: 'execute_maintenance_plan', dry_run: true } },
        });
        vi.mocked(ConfirmPreviewedSkillDraftReview).mockResolvedValueOnce({ ok: true, draft_execution_queue: 'applied', result: { executed_count: 1 } });

        render(<ExperienceLearningPanel t={t} learning={blockedSkillLearning()} error="" />);

        expect(screen.getByText('Skill draft review queues')).toBeTruthy();
        expect(screen.getByText('blocked skill draft is stale')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Confirm previewed draft' }));
        await waitFor(() => expect(ConfirmPreviewedSkillDraftReview).toHaveBeenCalledWith('memory:previewed-skill-review'));
        await screen.findByText((text) => text.includes('Previewed draft confirmed') && text.includes('Executed: 1') && text.includes('Queue: applied'));
        fireEvent.click(screen.getByRole('button', { name: 'Repair Draft' }));
        await waitFor(() => expect(BuildExperienceBlockedSkillDraft).toHaveBeenCalledWith('memory:blocked-skill-review'));
        await screen.findByText('Blocked draft decision');

        fireEvent.change(screen.getByPlaceholderText('skill_draft:...'), { target: { value: 'skill_draft:mark_needs_review:fixed:' } });
        fireEvent.click(screen.getByRole('button', { name: 'Reopen with replacement draft' }));

        await waitFor(() => expect(RecordBlockedSkillDraftReview).toHaveBeenCalledWith('memory:blocked-skill-review', 'reopen', 'skill_draft:mark_needs_review:fixed:', '', 'operator'));
    });
});
