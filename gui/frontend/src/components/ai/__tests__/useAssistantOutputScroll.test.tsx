import { act, fireEvent, render, renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ChatMessage } from '../useAIAssistant';
import { useAssistantOutputScroll } from '../useAssistantOutputScroll';

const scrollIntoViewMock = vi.fn();

function makeMsg(overrides: Partial<ChatMessage> & { role: ChatMessage['role'] }): ChatMessage {
    return {
        id: overrides.id ?? `msg-${Math.random()}`,
        content: overrides.content ?? '',
        timestamp: overrides.timestamp ?? 1,
        ...overrides,
    };
}

function ScrollHarness({
    activityKey = '',
    hasConversation = true,
    messages,
    ready = true,
}: {
    activityKey?: string;
    hasConversation?: boolean;
    messages: ChatMessage[];
    ready?: boolean;
}) {
    const { handleScroll, handleUserScrollIntent, outputContainerRef, outputEndRef } = useAssistantOutputScroll({
        activityKey,
        hasConversation,
        messages,
        ready,
    });
    return (
        <div
            ref={outputContainerRef}
            data-testid="scroll-box"
            onScroll={handleScroll}
            onWheel={handleUserScrollIntent}
            onTouchMove={handleUserScrollIntent}
            onPointerDown={handleUserScrollIntent}
            style={{ height: 120, overflow: 'auto' }}
        >
            <div data-testid="scroll-content" style={{ height: 400 }}>
                {messages.map((message) => message.content || message.reasoning || '')}
            </div>
            <div data-nested-scroll="" data-testid="nested-scroll" style={{ height: 80, overflow: 'auto' }}>
                nested
            </div>
            <div
                ref={(node) => {
                    outputEndRef.current = node;
                    if (node) node.scrollIntoView = scrollIntoViewMock;
                }}
            />
        </div>
    );
}

describe('useAssistantOutputScroll', () => {
    afterEach(() => {
        scrollIntoViewMock.mockReset();
        vi.unstubAllGlobals();
        vi.useRealTimers();
    });

    it('follows same-count thinking updates without waiting for a quiet window', () => {
        const user = makeMsg({ role: 'user', content: 'Ningbo weather' });
        const first = makeMsg({ id: 'a1', role: 'assistant', content: '', reasoning: 'Task received' });
        const { rerender } = render(<ScrollHarness messages={[user, first]} />);

        scrollIntoViewMock.mockClear();
        rerender(
            <ScrollHarness
                messages={[user, { ...first, reasoning: 'Task received\nPreparing the execution path' }]}
            />,
        );

        expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'auto', block: 'end' });
    });

    it('does not unpin follow when a delayed scroll arrives without user intent', () => {
        const user = makeMsg({ role: 'user', content: 'Ningbo weather' });
        const first = makeMsg({ id: 'a1', role: 'assistant', content: 'Ning' });
        const { getByTestId, rerender } = render(<ScrollHarness messages={[user, first]} />);
        const box = getByTestId('scroll-box');
        Object.defineProperties(box, {
            clientHeight: { configurable: true, value: 120 },
            scrollHeight: { configurable: true, value: 400 },
            scrollTop: { configurable: true, value: 200, writable: true },
        });

        scrollIntoViewMock.mockClear();
        rerender(<ScrollHarness messages={[user, { ...first, content: 'Ningbo weather looks' }]} />);
        fireEvent.scroll(box);

        scrollIntoViewMock.mockClear();
        rerender(<ScrollHarness messages={[user, { ...first, content: 'Ningbo weather looks sunny' }]} />);
        expect(scrollIntoViewMock).toHaveBeenCalled();
    });

    it('does not unpin follow after a downward wheel at the tail', () => {
        const user = makeMsg({ role: 'user', content: 'Ningbo weather' });
        const first = makeMsg({ id: 'a1', role: 'assistant', content: 'Ning' });
        const { getByTestId, rerender } = render(<ScrollHarness messages={[user, first]} />);
        const box = getByTestId('scroll-box');
        Object.defineProperties(box, {
            clientHeight: { configurable: true, value: 120 },
            scrollHeight: { configurable: true, value: 400 },
            scrollTop: { configurable: true, value: 280, writable: true },
        });
        fireEvent.wheel(box, { deltaY: 40 });

        scrollIntoViewMock.mockClear();
        rerender(<ScrollHarness messages={[user, { ...first, content: 'Ningbo weather looks sunny' }]} />);
        expect(scrollIntoViewMock).toHaveBeenCalled();
    });

    it('does not unpin follow when the user scrolls a nested thinking pane', () => {
        const user = makeMsg({ role: 'user', content: 'Ningbo weather' });
        const first = makeMsg({ id: 'a1', role: 'assistant', content: '', reasoning: 'Task received' });
        const { getByTestId, rerender } = render(<ScrollHarness messages={[user, first]} />);
        fireEvent.wheel(getByTestId('nested-scroll'), { deltaY: -40 });

        scrollIntoViewMock.mockClear();
        rerender(
            <ScrollHarness
                messages={[user, { ...first, reasoning: 'Task received\nPreparing the execution path' }]}
            />,
        );
        expect(scrollIntoViewMock).toHaveBeenCalled();
    });

    it('does not yank back when a token arrives after an upward wheel', () => {
        const user = makeMsg({ role: 'user', content: 'Earlier question' });
        const assistant = makeMsg({ id: 'a1', role: 'assistant', content: 'Earlier answer' });
        const { getByTestId, rerender } = render(<ScrollHarness messages={[user, assistant]} />);
        const box = getByTestId('scroll-box');
        Object.defineProperties(box, {
            clientHeight: { configurable: true, value: 100 },
            scrollHeight: { configurable: true, value: 400 },
            scrollTop: { configurable: true, value: 40, writable: true },
        });
        fireEvent.wheel(box, { deltaY: -40 });

        scrollIntoViewMock.mockClear();
        rerender(<ScrollHarness messages={[user, { ...assistant, content: 'Earlier answer plus more' }]} />);
        expect(scrollIntoViewMock).not.toHaveBeenCalled();
    });

    it('stops following after the user scrolls up', () => {
        const user = makeMsg({ role: 'user', content: 'Earlier question' });
        const assistant = makeMsg({ id: 'a1', role: 'assistant', content: 'Earlier answer' });
        const { getByTestId, rerender } = render(<ScrollHarness messages={[user, assistant]} />);
        const box = getByTestId('scroll-box');
        Object.defineProperties(box, {
            clientHeight: { configurable: true, value: 100 },
            scrollHeight: { configurable: true, value: 400 },
            scrollTop: { configurable: true, value: 0, writable: true },
        });
        fireEvent.wheel(box, { deltaY: -40 });
        fireEvent.scroll(box);

        scrollIntoViewMock.mockClear();
        rerender(<ScrollHarness messages={[user, { ...assistant, content: 'Earlier answer plus more' }]} />);
        expect(scrollIntoViewMock).not.toHaveBeenCalled();
    });

    it('exposes scrollToBottom for forced pins', () => {
        const { result } = renderHook(() => useAssistantOutputScroll({
            hasConversation: true,
            messages: [makeMsg({ role: 'assistant', content: 'hi' })],
            ready: true,
        }));
        act(() => {
            result.current.scrollToBottom('auto', true);
        });
        expect(result.current.userScrolledUpRef.current).toBe(false);
    });
});