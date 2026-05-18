import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

vi.mock('../../../../wailsjs/go/main/App', () => ({
    BuildExperienceConflictReconciliationDraft: vi.fn(),
    BuildExperienceEscalationBrief: vi.fn(),
    BuildExperienceMemoryMaintenanceDraft: vi.fn(),
    BuildExperienceRollbackWorkflowDraft: vi.fn(),
    BuildExperienceRoutingAdjustmentDraft: vi.fn(),
    BuildExperienceSkillDraft: vi.fn(),
    BuildExperienceTraceFollowUp: vi.fn(),
    GetExperienceGovernanceSummary: vi.fn(),
    RecordExperienceDraftReview: vi.fn(),
    RecordExperienceTraceFollowUp: vi.fn(),
    ReviewExperienceTrace: vi.fn(),
}));

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
