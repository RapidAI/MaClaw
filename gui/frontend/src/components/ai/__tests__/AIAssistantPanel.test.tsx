/**
 * Property-based tests for AIAssistantPanel component.
 *
 * Feature: ai-assistant-sidebar-icon
 * Property 8: 响应渲染完整性
 *
 * Uses fast-check for property-based testing with ≥100 iterations.
 */
import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, cleanup } from '@testing-library/react';
import * as fc from 'fast-check';
import { AIAssistantPanel } from '../AIAssistantPanel';
import type { ChatMessage } from '../useAIAssistant';

// ── Mock Wails runtime (not used by panel but imported transitively) ──
vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(),
    EventsOff: vi.fn(),
}));

// Helper: build a ChatMessage for testing.
function makeMsg(overrides: Partial<ChatMessage> & { role: ChatMessage['role'] }): ChatMessage {
    return {
        id: `test-${Math.random()}`,
        content: overrides.content ?? '',
        timestamp: Date.now(),
        ...overrides,
    };
}

describe('AIAssistantPanel property tests', () => {
    afterEach(() => {
        cleanup();
    });

    // ─────────────────────────────────────────────────────────────────
    // Feature: ai-assistant-sidebar-icon, Property 8: 响应渲染完整性
    //
    // Validates: Requirements 4.2, 4.3, 4.4
    // For any IMAgentResponse with fields, actions, and/or error:
    //   - Every field label/value is rendered
    //   - Action button count matches actions array length
    //   - Error text is rendered with error styling
    // ─────────────────────────────────────────────────────────────────
    it('Property 8: fields, actions, and errors are fully rendered', () => {
        const fieldArb = fc.record({
            label: fc.string({ minLength: 1, maxLength: 20 }).filter(s => s.trim().length > 0),
            value: fc.string({ minLength: 1, maxLength: 40 }).filter(s => s.trim().length > 0),
        });

        const actionArb = fc.record({
            label: fc.string({ minLength: 1, maxLength: 20 }).filter(s => s.trim().length > 0),
            command: fc.string({ minLength: 1, maxLength: 30 }),
            style: fc.constantFrom('default', 'danger'),
        });

        fc.assert(
            fc.property(
                fc.array(fieldArb, { minLength: 0, maxLength: 5 }),
                fc.array(actionArb, { minLength: 0, maxLength: 4 }),
                fc.option(fc.string({ minLength: 1, maxLength: 60 }).filter(s => s.trim().length > 0)),
                (fields, actions, errorOpt) => {
                    const messages: ChatMessage[] = [];

                    // Add an assistant message with fields and actions.
                    if (fields.length > 0 || actions.length > 0) {
                        messages.push(makeMsg({
                            role: 'assistant',
                            content: 'Response text',
                            fields: fields.length > 0 ? fields : undefined,
                            actions: actions.length > 0 ? actions : undefined,
                        }));
                    }

                    // Add an error message if generated.
                    if (errorOpt !== null) {
                        messages.push(makeMsg({
                            role: 'error',
                            content: errorOpt,
                        }));
                    }

                    if (messages.length === 0) return; // skip trivial case

                    // Clean up previous render to avoid DOM leaks across iterations.
                    cleanup();

                    const { container } = render(
                        <AIAssistantPanel
                            onClose={() => {}}
                            lang="en"
                            messages={messages}
                            sending={false}
                            streaming={false}
                            ready={true}
                            sendMessage={async () => {}}
                            clearHistory={async () => {}}
                            executeAction={async () => {}}
                        />
                    );

                    // Verify every field label and value is rendered.
                    for (const f of fields) {
                        const fieldCards = container.querySelectorAll('[data-testid="field-card"]');
                        const texts = Array.from(fieldCards).map(el => el.textContent || '');
                        const found = texts.some(t => t.includes(f.label) && t.includes(f.value));
                        expect(found).toBe(true);
                    }

                    // Verify action button count.
                    const actionButtons = container.querySelectorAll('[data-testid="action-button"]');
                    expect(actionButtons.length).toBe(actions.length);

                    // Verify error text is rendered.
                    if (errorOpt !== null) {
                        expect(container.textContent).toContain(errorOpt);
                    }
                },
            ),
            { numRuns: 100 },
        );
    });
});
