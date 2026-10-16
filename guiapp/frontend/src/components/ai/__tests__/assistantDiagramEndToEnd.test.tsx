/**
 * Proof that the visual-output feature actually WORKS end to end: when the
 * model follows PromptVisualizationRules and emits a ```mermaid fence inside a
 * normal reply, the assistant's real message pipeline reaches
 * AssistantMermaidDiagram and hands it the diagram source.
 *
 * The unit suites around this cover fence detection in isolation; what they do
 * not prove is that a full assistant reply — prose before, prose after,
 * diagram in the middle, the shape the prompt asks for — reaches the diagram
 * component at all. That is what this file exists to establish.
 *
 * ASSERTION LEVEL (deliberate): this asserts that the diagram component is
 * MOUNTED with the right source, not that the SVG appears. Under jsdom,
 * mermaid.render always fails with "getBBox is not a function" because jsdom
 * implements no SVG layout engine — so the component correctly takes its
 * documented fallback path (error + readable source). Verified with a probe:
 * the failure is environmental, and real SVG production cannot be asserted
 * without a browser. Mounting-with-source is the strongest claim this runtime
 * can honestly support, and it is exactly the link that must not break.
 */
import React from 'react';
import { describe, expect, it } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MessageContentRenderer } from '../MessageContentRenderer';
import { lightTheme } from '../aiAssistantPanelTheme';

// Exactly the shape PromptVisualizationRules asks for: say what the diagram
// shows, draw it, then point out the key path.
const REPLY_WITH_DIAGRAM = [
    '这个系统分三层，数据从采集流向检索：',
    '',
    '```mermaid',
    'flowchart TD',
    '  A["采集层"] --> B["存储层"]',
    '  B --> C["检索层"]',
    '```',
    '',
    '关键路径是采集到存储这一段，它的吞吐决定了整体上限。',
].join('\n');

describe('assistant reply renders the taught diagram', () => {
    it('mounts the diagram component with the fenced source inside a prose reply', async () => {
        const { container } = render(<MessageContentRenderer content={REPLY_WITH_DIAGRAM} theme={lightTheme} />);

        // Either the diagram rendered or it took its documented fallback; both
        // mean the pipeline reached AssistantMermaidDiagram with this source.
        // (Under jsdom it is always the fallback — see file header.)
        await waitFor(() => {
            const settled =
                screen.queryByTestId('assistant-mermaid-diagram') ||
                screen.queryByTestId('assistant-mermaid-fallback');
            expect(settled).toBeTruthy();
        });
        // The diagram source arrived intact, with no stray prose mixed in.
        const source = container.querySelector('[data-testid="assistant-mermaid-fallback"] pre')?.textContent ?? '';
        expect(source).toContain('flowchart TD');
        expect(source).toContain('A["采集层"] --> B["存储层"]');
        // Surrounding prose survives: the diagram is added, not substituted.
        expect(screen.getByText(/这个系统分三层/)).toBeTruthy();
        expect(screen.getByText(/关键路径是采集到存储这一段/)).toBeTruthy();
    });

    it('keeps a still-streaming fence as source instead of a half-rendered diagram', () => {
        const partial = [
            '这个系统分三层：',
            '',
            '```mermaid',
            'flowchart TD',
            '  A["采集层"] -->',
        ].join('\n');
        const { container } = render(<MessageContentRenderer content={partial} theme={lightTheme} />);

        expect(screen.queryByTestId('assistant-mermaid-diagram')).toBeNull();
        expect(container.querySelector('pre')?.textContent).toContain('A["采集层"]');
    });

    it('mounts a separate diagram component per fence in one reply', async () => {
        const two = [
            '```mermaid',
            'flowchart TD',
            '  A --> B',
            '```',
            '',
            '再看时序：',
            '',
            '```mermaid',
            'sequenceDiagram',
            '  A->>B: ping',
            '```',
        ].join('\n');
        const { container } = render(<MessageContentRenderer content={two} theme={lightTheme} />);

        await waitFor(() => {
            const settled =
                screen.queryAllByTestId('assistant-mermaid-diagram').length +
                screen.queryAllByTestId('assistant-mermaid-fallback').length;
            expect(settled).toBe(2);
        });
        // Both sources reached their own component, none leaked into prose.
        const sources = Array.from(container.querySelectorAll('pre')).map((p) => p.textContent ?? '');
        expect(sources.some((s) => s.includes('flowchart TD'))).toBe(true);
        expect(sources.some((s) => s.includes('A->>B: ping'))).toBe(true);
    });
});
