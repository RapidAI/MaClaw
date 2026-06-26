import { describe, expect, it } from 'vitest';
import React from 'react';
import { cleanup, render, screen, within } from '@testing-library/react';
import { WorkflowDocPreview, deriveProgressPhases } from '../WorkflowDocPreview';
import type { PhaseInfo } from '../useWorkflowState';

const testTheme = {
    bg: '#fff',
    text: '#111',
    textMuted: '#666',
    border: '#ddd',
    headerBg: '#f8f8f8',
    accentColor: '#4f46e5',
    accentBg: '#eef2ff',
    codeBg: '#f1f5f9',
    codeText: '#111827',
    codeBlockBg: '#0f172a',
    codeBlockBorder: '#334155',
    headingColor: '#111827',
    linkColor: '#2563eb',
    quoteBorder: '#c7d2fe',
    quoteText: '#374151',
    quoteBg: '#f8fafc',
};

// ── Task 8.5: deriveProgressPhases (Requirements 3.2, 3.3, 3.4) ──
//
// deriveProgressPhases is the single frontend reducer. These tests exercise the
// exported function directly (no rendering) so we assert the full ProgressPhase
// shape — id, label, AND the single document-expectation value — not just ids.
describe('deriveProgressPhases', () => {
    // Requirement 3.2: when metadata is present, order/labels/doc-flags come from
    // the emitted metadata, not the fallback maps.
    it('derives order from index, labels from name, and doc-flags from metadata', () => {
        const phases: PhaseInfo[] = [
            { id: 'beta', name: 'Beta phase', index: 1, expectsDocument: false },
            { id: 'alpha', name: 'Alpha phase', index: 0, expectsDocument: true },
        ];

        // Input is intentionally out of index order; result must be sorted by index.
        expect(deriveProgressPhases('coding', phases, new Map(), '')).toEqual([
            { id: 'alpha', label: 'Alpha phase', expectsDocument: true },
            { id: 'beta', label: 'Beta phase', expectsDocument: false },
        ]);
    });

    // Requirement 3.3: when metadata is absent or empty, fall back to the hardcoded
    // workflowPhaseOrders / phaseLabels / fallbackNonDocumentPhaseIDs maps.
    it('falls back to the hardcoded maps when phases is undefined', () => {
        const result = deriveProgressPhases('coding', undefined, new Map(), '');

        expect(result.map(p => p.id)).toEqual([
            'requirements',
            'design',
            'tasks',
            'implementation',
            'verification',
        ]);
        // Labels resolve from the hardcoded phaseLabels map, which is reconciled
        // character-for-character with the generated artifact (the single source of truth).
        expect(result.find(p => p.id === 'requirements')!.label).toBe('需求文档');
        expect(result.find(p => p.id === 'design')!.label).toBe('技术设计');
        expect(result.find(p => p.id === 'implementation')!.label).toBe('编码执行');
        // implementation is in fallbackNonDocumentPhaseIDs -> execution phase.
        expect(result.find(p => p.id === 'implementation')!.expectsDocument).toBe(false);
        // Document phases default to producing a document.
        expect(result.find(p => p.id === 'requirements')!.expectsDocument).toBe(true);
        expect(result.find(p => p.id === 'verification')!.expectsDocument).toBe(false);
    });

    it('falls back to the hardcoded maps when phases is an empty array', () => {
        expect(deriveProgressPhases('coding', [], new Map(), '').map(p => p.id)).toEqual([
            'requirements',
            'design',
            'tasks',
            'implementation',
            'verification',
        ]);
    });

    it('returns an empty list when no metadata and no fallback order exist', () => {
        expect(deriveProgressPhases('unknown_type', [], new Map(), '')).toEqual([]);
        expect(deriveProgressPhases(undefined, undefined, new Map(), '')).toEqual([]);
    });

    // Requirement 3.2: metadata is authoritative — the fallback maps are NOT read for
    // order, labels, or doc-expectation when metadata is present.
    it('does not read fallback maps for an id absent from them when metadata is present', () => {
        const phases: PhaseInfo[] = [
            { id: 'custom_phase', name: 'Custom Phase', index: 0, expectsDocument: false },
        ];

        // custom_phase is in neither phaseLabels nor fallbackNonDocumentPhaseIDs, yet it
        // resolves entirely from metadata (label = name, expectsDocument = metadata value).
        expect(deriveProgressPhases('coding', phases, new Map(), '')).toEqual([
            { id: 'custom_phase', label: 'Custom Phase', expectsDocument: false },
        ]);
    });

    it('lets metadata override the fallback maps and uses metadata order even when it differs', () => {
        // Both ids exist in the coding fallback, but in the opposite order, with opposite
        // doc-expectations. Metadata wins on order, labels, AND doc-flags.
        const phases: PhaseInfo[] = [
            { id: 'implementation', name: 'Impl Stage', index: 0, expectsDocument: true },
            { id: 'requirements', name: 'Req Stage', index: 1, expectsDocument: false },
        ];

        expect(deriveProgressPhases('coding', phases, new Map(), '')).toEqual([
            // metadata order: implementation before requirements (fallback order is reversed)
            { id: 'implementation', label: 'Impl Stage', expectsDocument: true }, // overrides fallback false
            { id: 'requirements', label: 'Req Stage', expectsDocument: false },    // overrides fallback true
        ]);
    });

    it('falls back per-field only when the metadata omits that field', () => {
        const phases: PhaseInfo[] = [
            { id: 'implementation', name: '', index: 0 },   // no name, no expectsDocument
            { id: 'verification', name: 'Verification!', index: 1 },     // name present, no expectsDocument
        ];

        const result = deriveProgressPhases('coding', phases, new Map(), '');
        // implementation: empty name -> fallback label '编码执行'; missing flag -> fallback false.
        expect(result[0]).toEqual({ id: 'implementation', label: '编码执行', expectsDocument: false });
        // verification: metadata label kept; missing flag -> fallback false (execution/review phase).
        expect(result[1]).toEqual({ id: 'verification', label: 'Verification!', expectsDocument: false });
    });

    // Requirement 3.4: ids seen only in phaseDocuments or as currentPhaseID are appended
    // with a non-empty label resolved metadata -> fallback map -> id-derived, duplicate-free.
    it('appends document-only and current-phase ids with resolved non-empty labels', () => {
        const docs = new Map<string, string>([
            ['design', '# Design'],       // resolvable via fallback phaseLabels -> '技术设计'
            ['custom_doc', '# Custom'],   // unknown -> id-derived label
        ]);
        const phases: PhaseInfo[] = [
            { id: 'requirements', name: 'Requirements', index: 0, expectsDocument: true },
        ];

        const result = deriveProgressPhases('coding', phases, docs, 'mystery_phase');

        // metadata first, then doc-only ids (map insertion order), then the current id.
        expect(result.map(p => p.id)).toEqual([
            'requirements',
            'design',
            'custom_doc',
            'mystery_phase',
        ]);
        expect(result.find(p => p.id === 'design')!.label).toBe('技术设计');     // fallback map
        expect(result.find(p => p.id === 'custom_doc')!.label).toBe('custom_doc'); // id-derived
        expect(result.find(p => p.id === 'mystery_phase')!.label).toBe('mystery_phase'); // id-derived
        // Every rendered phase resolves to a non-empty label.
        for (const phase of result) {
            expect(phase.label.trim().length).toBeGreaterThan(0);
        }
    });

    it('does not duplicate an id that is both the current phase and a collected document', () => {
        const docs = new Map<string, string>([['design', '# Design']]);
        const phases: PhaseInfo[] = [
            { id: 'requirements', name: 'R', index: 0, expectsDocument: true },
        ];

        expect(deriveProgressPhases('coding', phases, docs, 'design').map(p => p.id)).toEqual([
            'requirements',
            'design',
        ]);
    });

    it('does not append a current phase that is already in the metadata list', () => {
        const phases: PhaseInfo[] = [
            { id: 'requirements', name: 'R', index: 0, expectsDocument: true },
        ];

        expect(deriveProgressPhases('coding', phases, new Map(), 'requirements').map(p => p.id)).toEqual([
            'requirements',
        ]);
    });
});

// ── Task 8.6: highlighting and progress (Requirements 4.1, 4.2) ──
//
// The highlighting/progress logic lives inside the WorkflowProgressBoard render
// path, so these tests drive the real component (WorkflowDocPreview renders the
// board) and assert against the rendered DOM — preferring the real exported code
// path over re-implementing the index/progress formula.

/**
 * Reads the rendered progress-track fill width as a percentage. The board renders
 * two absolutely-positioned line divs: the track (no width; uses left/right) and the
 * fill (explicit width: "0px" or "calc(P% - Npx)"). Only the fill carries a width.
 */
function readProgressPercent(container: HTMLElement): number {
    const fill = Array.from(container.querySelectorAll('div')).find(
        (d) => (d as HTMLElement).style.position === 'absolute' && (d as HTMLElement).style.width,
    ) as HTMLElement | undefined;
    if (!fill) return -1;
    const w = fill.style.width;
    if (w === '0px' || w === '') return 0;
    const m = w.match(/calc\(([\d.]+)%/);
    return m ? parseFloat(m[1]) : -1;
}

describe('WorkflowProgressBoard highlighting and progress', () => {
    const codingPhases: PhaseInfo[] = [
        { id: 'requirements', name: '需求', index: 0, expectsDocument: true },
        { id: 'design', name: '设计', index: 1, expectsDocument: true },
        { id: 'tasks', name: '任务', index: 2, expectsDocument: true },
    ];

    // Requirement 4.1: a non-canonical alias current phase id highlights exactly one
    // node — the canonical node it aliases to (tech_design -> design).
    it('highlights exactly one node for a non-canonical alias current phase id', () => {
        render(React.createElement(WorkflowDocPreview, {
            phaseDocuments: new Map(),
            currentPhaseID: 'tech_design', // alias of 'design'
            latestDocumentPhaseID: '',
            phases: codingPhases,
            workflowType: 'coding',
            gateResults: new Map(),
            theme: testTheme,
        }));

        // Exactly one node is in the "current" state (生成中). Document phases without a
        // doc that are NOT current render 缺文档 (past) or 待开始 (future).
        const current = screen.getAllByLabelText(/，生成中$/);
        expect(current).toHaveLength(1);
        // The single highlighted node is the canonical 'design' node, resolved from the alias.
        expect(screen.getByLabelText('设计，生成中')).toBeTruthy();
        // requirements precedes the current node -> past; tasks follows -> future.
        expect(screen.getByLabelText('需求，缺文档')).toBeTruthy();
        expect(screen.getByLabelText('任务，待开始')).toBeTruthy();
    });

    // Requirement 4.1: when no current phase resolves within the list, zero nodes are
    // highlighted (currentIndex = -1). A non-empty current id is always surfaced and
    // therefore matched, so the no-highlight case is an absent/unresolvable current phase.
    it('highlights zero nodes when there is no resolvable current phase', () => {
        const { container } = render(React.createElement(WorkflowDocPreview, {
            phaseDocuments: new Map(),
            currentPhaseID: '',
            latestDocumentPhaseID: '',
            phases: codingPhases,
            workflowType: 'coding',
            gateResults: new Map(),
            theme: testTheme,
        }));

        // No node is current; every document phase without a doc shows 待开始.
        expect(screen.queryAllByLabelText(/，生成中$/)).toHaveLength(0);
        expect(screen.getAllByLabelText(/，待开始$/)).toHaveLength(3);
        // Progress stays at zero when there is no active node.
        expect(readProgressPercent(container)).toBe(0);
    });

    // Requirement 4.2: progress is a monotonic function of the active node's zero-based
    // index, reaching its maximum (100%) only at the final phase.
    it('produces monotonic progress that reaches 100% only at the final phase', () => {
        // Use the coding fallback order (5 phases) so each current id maps to a known index.
        const order = ['requirements', 'design', 'tasks', 'implementation', 'verification'];
        const percents: number[] = [];

        for (const phaseID of order) {
            const { container, unmount } = render(React.createElement(WorkflowDocPreview, {
                phaseDocuments: new Map(),
                currentPhaseID: phaseID,
                latestDocumentPhaseID: '',
                workflowType: 'coding',
                gateResults: new Map(),
                    theme: testTheme,
            }));
            percents.push(readProgressPercent(container));
            unmount();
        }

        // Strictly increasing with index.
        for (let i = 1; i < percents.length; i++) {
            expect(percents[i]).toBeGreaterThan(percents[i - 1]);
        }
        // Minimum at the first phase, maximum (100%) only at the final phase.
        expect(percents[0]).toBe(0);
        expect(percents[percents.length - 1]).toBe(100);
        for (let i = 0; i < percents.length - 1; i++) {
            expect(percents[i]).toBeLessThan(100);
        }
    });

    // Requirement 4.2: the maximum is driven by the active node's index, not by a
    // later-collected document — a document collected ahead of the active node must not
    // push progress to 100% before the final phase.
    it('does not reach 100% from a document collected ahead of the active node', () => {
        const { container } = render(React.createElement(WorkflowDocPreview, {
            // A document exists for the final phase, but the active node is earlier.
            phaseDocuments: new Map([['verification', '# Verification']]),
            currentPhaseID: 'design', // index 1 of 5
            latestDocumentPhaseID: '',
            workflowType: 'coding',
            gateResults: new Map(),
            theme: testTheme,
        }));

        const percent = readProgressPercent(container);
        expect(percent).toBeGreaterThan(0);
        expect(percent).toBeLessThan(100);
    });

    // afterEach cleanup is auto-registered by @testing-library/react under vitest globals;
    // the explicit unmount in the progress loop guards against cross-render query bleed.
    it('cleans up rendered output between cases', () => {
        cleanup();
        expect(within(document.body).queryByText('文档预览')).toBeNull();
    });
});
