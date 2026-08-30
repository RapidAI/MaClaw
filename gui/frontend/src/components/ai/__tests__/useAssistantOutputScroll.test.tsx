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

    it('re-pins the tail when a new user message arrives after scrolling up', () => {
        const user1 = makeMsg({ role: 'user', content: 'Earlier question' });
        const assistant = makeMsg({ id: 'a1', role: 'assistant', content: 'Earlier answer' });
        const { getByTestId, rerender } = render(<ScrollHarness messages={[user1, assistant]} />);
        const box = getByTestId('scroll-box');
        Object.defineProperties(box, {
            clientHeight: { configurable: true, value: 100 },
            scrollHeight: { configurable: true, value: 400 },
            scrollTop: { configurable: true, value: 0, writable: true },
        });
        fireEvent.wheel(box, { deltaY: -40 });
        fireEvent.scroll(box);

        scrollIntoViewMock.mockClear();
        // Mirror the real send flow: the user message and the assistant
        // placeholder are appended together, so the tail role is assistant.
        const user2 = makeMsg({ id: 'u2', role: 'user', content: 'New question' });
        const placeholder = makeMsg({ id: 'a2', role: 'assistant', content: '' });
        rerender(<ScrollHarness messages={[user1, assistant, user2, placeholder]} />);
        expect(scrollIntoViewMock).toHaveBeenCalled();
    });

    it('resumes following streamed tokens after a new user message re-pins the tail', () => {
        const user1 = makeMsg({ role: 'user', content: 'Earlier question' });
        const assistant1 = makeMsg({ id: 'a1', role: 'assistant', content: 'Earlier answer' });
        const user2 = makeMsg({ id: 'u2', role: 'user', content: 'New question' });
        const assistant2 = makeMsg({ id: 'a2', role: 'assistant', content: '' });
        const { getByTestId, rerender } = render(<ScrollHarness messages={[user1, assistant1]} />);
        const box = getByTestId('scroll-box');
        Object.defineProperties(box, {
            clientHeight: { configurable: true, value: 100 },
            scrollHeight: { configurable: true, value: 400 },
            scrollTop: { configurable: true, value: 0, writable: true },
        });
        fireEvent.wheel(box, { deltaY: -40 });
        fireEvent.scroll(box);

        rerender(<ScrollHarness messages={[user1, assistant1, user2, assistant2]} />);
        scrollIntoViewMock.mockClear();
        rerender(<ScrollHarness messages={[user1, assistant1, user2, { ...assistant2, content: 'Reply token' }]} />);
        rerender(<ScrollHarness messages={[user1, assistant1, user2, { ...assistant2, content: 'Reply token plus more' }]} />);
        expect(scrollIntoViewMock).toHaveBeenCalled();
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

    it('follows content growth reported by ResizeObserver while pinned', () => {
        const callbacks: Array<() => void> = [];
        class MockResizeObserver {
            constructor(cb: () => void) { callbacks.push(cb); }
            observe() {}
            unobserve() {}
            disconnect() {}
        }
        vi.stubGlobal('ResizeObserver', MockResizeObserver);
        // The observer coalesces notifications through requestAnimationFrame;
        // run the frame synchronously for the assertion.
        vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => { cb(0); return 1; });
        const user = makeMsg({ role: 'user', content: 'question' });
        const assistant = makeMsg({ id: 'a1', role: 'assistant', content: 'answer' });
        render(<ScrollHarness messages={[user, assistant]} />);
        expect(callbacks.length).toBeGreaterThan(0);

        scrollIntoViewMock.mockClear();
        act(() => { callbacks.forEach((cb) => cb()); });
        expect(scrollIntoViewMock).toHaveBeenCalled();
    });

    it('ignores ResizeObserver growth when the user scrolled up', () => {
        const callbacks: Array<() => void> = [];
        class MockResizeObserver {
            constructor(cb: () => void) { callbacks.push(cb); }
            observe() {}
            unobserve() {}
            disconnect() {}
        }
        vi.stubGlobal('ResizeObserver', MockResizeObserver);
        vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => { cb(0); return 1; });
        const user = makeMsg({ role: 'user', content: 'question' });
        const assistant = makeMsg({ id: 'a1', role: 'assistant', content: 'answer' });
        const { getByTestId } = render(<ScrollHarness messages={[user, assistant]} />);
        const box = getByTestId('scroll-box');
        Object.defineProperties(box, {
            clientHeight: { configurable: true, value: 100 },
            scrollHeight: { configurable: true, value: 400 },
            scrollTop: { configurable: true, value: 0, writable: true },
        });
        fireEvent.wheel(box, { deltaY: -40 });
        fireEvent.scroll(box);

        scrollIntoViewMock.mockClear();
        act(() => { callbacks.forEach((cb) => cb()); });
        expect(scrollIntoViewMock).not.toHaveBeenCalled();
    });
});