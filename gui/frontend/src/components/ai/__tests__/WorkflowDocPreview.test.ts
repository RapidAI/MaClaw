import { describe, expect, it } from 'vitest';
import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { WorkflowDocPreview, workflowProgressPhaseCardState, workflowProgressPhaseIDs } from '../WorkflowDocPreview';

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

describe('workflowProgressPhaseIDs', () => {
    it('marks only the workflow preview header as a window drag region', () => {
        render(React.createElement(WorkflowDocPreview, {
            phaseDocuments: new Map([['requirements', '# Requirements']]),
            currentPhaseID: 'requirements',
            latestDocumentPhaseID: 'requirements',
            workflowType: 'coding',
            gateResults: new Map(),
            onClose: () => undefined,
            theme: testTheme,
        }));

        expect(screen.getByTestId('workflow-doc-preview-header').style.getPropertyValue('--wails-draggable')).toBe('drag');
        expect(screen.getByTitle('关闭文档预览').style.getPropertyValue('--wails-draggable')).toBe('no-drag');
    });

    it('uses the coding workflow order and preserves generated document phases', () => {
        const docs = new Map<string, string>([
            ['requirements', '# Requirements'],
            ['tasks', '# Tasks'],
        ]);

        expect(workflowProgressPhaseIDs('coding', docs, 'design')).toEqual([
            'requirements',
            'design',
            'tasks',
            'implementation',
            'review',
        ]);
    });

    it('appends unknown generated and current phases without duplicates', () => {
        const docs = new Map<string, string>([
            ['custom_doc', '# Custom'],
            ['requirements', '# Requirements'],
        ]);

        expect(workflowProgressPhaseIDs(undefined, docs, 'custom_doc')).toEqual([
            'custom_doc',
            'requirements',
        ]);
    });

    it('prefers backend-provided phase order and labels over the fallback map', () => {
        const docs = new Map<string, string>([
            ['alpha', '# Alpha'],
        ]);

        expect(workflowProgressPhaseIDs('coding', docs, 'beta', [
            { id: 'alpha', name: 'Alpha phase', index: 0 },
            { id: 'beta', name: 'Beta phase', index: 1 },
        ])).toEqual(['alpha', 'beta']);

        render(React.createElement(WorkflowDocPreview, {
            phaseDocuments: docs,
            currentPhaseID: 'beta',
            latestDocumentPhaseID: '',
            phases: [
                { id: 'alpha', name: 'Alpha phase', index: 0 },
                { id: 'beta', name: 'Beta phase', index: 1 },
            ],
            workflowType: 'coding',
            gateResults: new Map(),
            onClose: () => undefined,
            theme: testTheme,
        }));

        expect(screen.getByTitle('Alpha phase · 已完成')).toBeTruthy();
        expect(screen.getByTitle('Beta phase · 生成中')).toBeTruthy();
    });

    it('localizes fallback workflow labels without overriding backend phase metadata', () => {
        render(React.createElement(WorkflowDocPreview, {
            phaseDocuments: new Map([['requirements', '# Requirements']]),
            currentPhaseID: 'tasks',
            latestDocumentPhaseID: 'requirements',
            phases: [
                { id: 'requirements', name: 'Requirements analysis', index: 0, expectsDocument: true },
                { id: 'design', name: 'Technical design', index: 1, expectsDocument: true },
                { id: 'tasks', name: 'Task breakdown', index: 2, expectsDocument: true },
            ],
            workflowType: 'coding',
            gateResults: new Map(),
            lang: 'en',
            onClose: () => undefined,
            theme: testTheme,
        }));

        expect(screen.getByText('Workflow progress')).toBeTruthy();
        expect(screen.getByText('3 phases')).toBeTruthy();
        expect(screen.getByText('1/3 docs')).toBeTruthy();
        expect(screen.getByLabelText('Requirements analysis, Completed')).toBeTruthy();
        expect(screen.getByLabelText('Technical design, Missing doc')).toBeTruthy();
        expect(screen.getByLabelText('Task breakdown, Generating')).toBeTruthy();
    });

    it('uses localized fallback labels when phase metadata is unavailable', () => {
        render(React.createElement(WorkflowDocPreview, {
            phaseDocuments: new Map([['requirements', '# Requirements']]),
            currentPhaseID: 'tasks',
            latestDocumentPhaseID: 'requirements',
            workflowType: 'coding',
            gateResults: new Map(),
            lang: 'en',
            onClose: () => undefined,
            theme: testTheme,
        }));

        expect(screen.getByLabelText('Requirements, Completed')).toBeTruthy();
        expect(screen.getByLabelText('Design, Missing doc')).toBeTruthy();
        expect(screen.getByLabelText('Tasks, Generating')).toBeTruthy();
    });

    it('sorts and deduplicates backend-provided phase metadata defensively', () => {
        expect(workflowProgressPhaseIDs('coding', new Map(), 'gamma', [
            { id: 'beta', name: 'Beta phase', index: 1 },
            { id: 'alpha', name: 'Alpha phase', index: 0 },
            { id: 'beta', name: 'Beta duplicate', index: 2 },
        ])).toEqual(['alpha', 'beta', 'gamma']);
    });

    it('marks past phases without collected documents as missing instead of completed', () => {
        expect(workflowProgressPhaseCardState({
            hasDoc: false,
            isCurrent: false,
            isPast: true,
        })).toEqual({
            status: '缺文档',
            tone: 'attention',
            emphasized: true,
        });

        expect(workflowProgressPhaseCardState({
            hasDoc: true,
            isCurrent: false,
            isPast: true,
        }).status).toBe('已完成');

        expect(workflowProgressPhaseCardState({
            expectsDocument: false,
            hasDoc: false,
            isCurrent: false,
            isPast: true,
        })).toEqual({
            status: '已执行',
            tone: 'done',
            emphasized: true,
        });
    });

    it('does not let gate results hide missing documents for document phases', () => {
        expect(workflowProgressPhaseCardState({
            expectsDocument: true,
            gatePassed: true,
            hasDoc: false,
            isCurrent: false,
            isPast: true,
        })).toEqual({
            status: '缺文档',
            tone: 'attention',
            emphasized: true,
        });

        expect(workflowProgressPhaseCardState({
            expectsDocument: true,
            gatePassed: false,
            hasDoc: false,
            isCurrent: true,
            isPast: false,
        })).toEqual({
            status: '生成中',
            tone: 'current',
            emphasized: true,
        });

        expect(workflowProgressPhaseCardState({
            expectsDocument: true,
            gatePassed: true,
            hasDoc: true,
            isCurrent: false,
            isPast: true,
        }).status).toBe('质检通过');
    });

    it('shows current generated document phases as waiting for confirmation even when the quality gate needs review', () => {
        expect(workflowProgressPhaseCardState({
            expectsDocument: true,
            gatePassed: false,
            hasDoc: true,
            isCurrent: true,
            isPast: false,
        })).toEqual({
            status: '待确认',
            tone: 'current',
            emphasized: true,
        });

        expect(workflowProgressPhaseCardState({
            expectsDocument: true,
            gatePassed: false,
            hasDoc: true,
            isCurrent: false,
            isPast: true,
        })).toEqual({
            status: '需调整',
            tone: 'attention',
            emphasized: true,
        });
    });

    it('does not count non-document execution phases as missing documents', () => {
        render(React.createElement(WorkflowDocPreview, {
            phaseDocuments: new Map([
                ['requirements', '# Requirements'],
                ['design', '# Design'],
                ['tasks', '# Tasks'],
            ]),
            currentPhaseID: 'review',
            latestDocumentPhaseID: 'tasks',
            phases: [
                { id: 'requirements', name: '需求分析', index: 0, expectsDocument: true },
                { id: 'design', name: '技术设计', index: 1, expectsDocument: true },
                { id: 'tasks', name: '任务拆分', index: 2, expectsDocument: true },
                { id: 'implementation', name: '编码实现', index: 3, expectsDocument: false },
                { id: 'review', name: '代码审查', index: 4, expectsDocument: true },
            ],
            workflowType: 'coding',
            gateResults: new Map(),
            onClose: () => undefined,
            theme: testTheme,
        }));

        expect(screen.getByText('3/4 个文档')).toBeTruthy();
        expect(screen.getByText('已执行')).toBeTruthy();
        expect(screen.queryByText('缺文档')).toBeNull();
        expect(screen.getByLabelText('任务拆分，已完成').getAttribute('aria-pressed')).toBe('true');
        expect(screen.getByLabelText('编码实现，已执行').getAttribute('aria-pressed')).toBe('false');
    });

    it('uses an execution summary instead of a zero document ratio for non-document-only phases', () => {
        render(React.createElement(WorkflowDocPreview, {
            phaseDocuments: new Map(),
            currentPhaseID: 'implementation',
            latestDocumentPhaseID: '',
            phases: [
                { id: 'implementation', name: '编码实现', index: 0, expectsDocument: false },
            ],
            workflowType: 'coding',
            gateResults: new Map(),
            onClose: () => undefined,
            theme: testTheme,
        }));

        expect(screen.getByText('执行阶段')).toBeTruthy();
        expect(screen.queryByText('0/0 个文档')).toBeNull();
        expect(screen.getByText('编码实现暂无预览文档')).toBeTruthy();
    });

    it('renders a gate banner even when the gate item list is missing', () => {
        render(React.createElement(WorkflowDocPreview, {
            phaseDocuments: new Map([['requirements', '# Requirements']]),
            currentPhaseID: 'requirements',
            latestDocumentPhaseID: 'requirements',
            workflowType: 'coding',
            gateResults: new Map([
                ['requirements', {
                    phase_id: 'requirements',
                    passed: true,
                    items: undefined as any,
                    checked_at: '',
                }],
            ]),
            onClose: () => undefined,
            theme: testTheme,
        }));

        expect(screen.getByText(/质量门禁/)).toBeTruthy();
        expect(screen.getByText('暂无检查项')).toBeTruthy();
    });

    it('auto-previews an existing document when the current phase has no document yet', () => {
        const { rerender } = render(React.createElement(WorkflowDocPreview, {
            phaseDocuments: new Map([['requirements', '# Requirements']]),
            currentPhaseID: 'design',
            latestDocumentPhaseID: '',
            workflowType: 'coding',
            gateResults: new Map(),
            onClose: () => undefined,
            theme: testTheme,
        }));

        expect(screen.getByText('Requirements')).toBeTruthy();

        fireEvent.click(screen.getByTitle('技术设计 · 生成中'));

        expect(screen.getByText('技术设计文档尚未生成')).toBeTruthy();

        rerender(React.createElement(WorkflowDocPreview, {
            phaseDocuments: new Map([
                ['requirements', '# Requirements'],
                ['tasks', '# Tasks'],
            ]),
            currentPhaseID: 'tasks',
            latestDocumentPhaseID: 'tasks',
            workflowType: 'coding',
            gateResults: new Map(),
            onClose: () => undefined,
            theme: testTheme,
        }));

        expect(screen.getByText('Tasks')).toBeTruthy();
    });

    it('keeps mixed document collection visible across completed, missing, and current phases', () => {
        render(React.createElement(WorkflowDocPreview, {
            phaseDocuments: new Map([['requirements', '# Requirements']]),
            currentPhaseID: 'tasks',
            latestDocumentPhaseID: 'requirements',
            phases: [
                { id: 'requirements', name: '需求分析', index: 0, expectsDocument: true },
                { id: 'design', name: '技术设计', index: 1, expectsDocument: true },
                { id: 'tasks', name: '任务拆分', index: 2, expectsDocument: true },
            ],
            workflowType: 'coding',
            gateResults: new Map(),
            onClose: () => undefined,
            theme: testTheme,
        }));

        expect(screen.getByText('1/3 个文档')).toBeTruthy();
        expect(screen.getByLabelText('需求分析，已完成').getAttribute('aria-pressed')).toBe('true');
        expect(screen.getByLabelText('技术设计，缺文档')).toBeTruthy();
        expect(screen.getByLabelText('任务拆分，生成中')).toBeTruthy();

        fireEvent.click(screen.getByTitle('任务拆分 · 生成中'));

        expect(screen.getByText('任务拆分文档尚未生成')).toBeTruthy();
    });

    it('clears a stale manual phase selection after workflow documents reset', () => {
        const { rerender } = render(React.createElement(WorkflowDocPreview, {
            phaseDocuments: new Map([['requirements', '# Old Requirements']]),
            currentPhaseID: 'design',
            latestDocumentPhaseID: 'requirements',
            workflowType: 'coding',
            gateResults: new Map(),
            onClose: () => undefined,
            theme: testTheme,
        }));

        fireEvent.click(screen.getByTitle('技术设计 · 生成中'));
        expect(screen.getByText('技术设计文档尚未生成')).toBeTruthy();

        rerender(React.createElement(WorkflowDocPreview, {
            phaseDocuments: new Map(),
            currentPhaseID: '',
            latestDocumentPhaseID: '',
            workflowType: 'coding',
            gateResults: new Map(),
            onClose: () => undefined,
            theme: testTheme,
        }));

        rerender(React.createElement(WorkflowDocPreview, {
            phaseDocuments: new Map([['requirements', '# New Requirements']]),
            currentPhaseID: 'design',
            latestDocumentPhaseID: 'requirements',
            workflowType: 'coding',
            gateResults: new Map(),
            onClose: () => undefined,
            theme: testTheme,
        }));

        expect(screen.getByText('New Requirements')).toBeTruthy();
    });
});
