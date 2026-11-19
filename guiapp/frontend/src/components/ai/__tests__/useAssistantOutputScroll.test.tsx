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

    it('does not measure the outer transcript when a capped thought pane absorbs the growth', () => {
        const user = makeMsg({ role: 'user', content: 'Ningbo weather' });
        const first = makeMsg({ id: 'a1', role: 'assistant', content: 'answer', reasoning: 'Task received' });
        const { getByTestId, rerender } = render(<ScrollHarness messages={[user, first]} />);
        const nested = getByTestId('nested-scroll');
        Object.defineProperties(nested, {
            clientHeight: { configurable: true, value: 80 },
            scrollHeight: { configurable: true, value: 640 },
        });

        scrollIntoViewMock.mockClear();
        rerender(
            <ScrollHarness
                messages={[user, { ...first, reasoning: 'Task received\nPreparing the execution path' }]}
            />,
        );
        // The flush that first fills the pane still grows the outer box.
        expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'auto', block: 'end' });

        scrollIntoViewMock.mockClear();
        rerender(
            <ScrollHarness
                messages={[user, { ...first, reasoning: 'Task received\nPreparing the execution path\nStill thinking' }]}
            />,
        );
        expect(scrollIntoViewMock).not.toHaveBeenCalled();

        rerender(
            <ScrollHarness
                activityKey="progress-1"
                messages={[user, { ...first, reasoning: 'Task received\nPreparing the execution path' }]}
            />,
        );
        expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'auto', block: 'end' });

        scrollIntoViewMock.mockClear();
        rerender(
            <ScrollHarness
                activityKey="progress-1"
                messages={[user, { ...first, content: 'answer grows', reasoning: 'Task received\nPreparing the execution path' }]}
            />,
        );
        expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'auto', block: 'end' });
    });

    it('keeps following when an earlier-messages overlay reports its own overflow', () => {
        const user = makeMsg({ role: 'user', content: 'Ningbo weather' });
        const first = makeMsg({ id: 'a1', role: 'assistant', content: 'answer', reasoning: 'Task received' });
        function Harness({ messages }: { messages: ChatMessage[] }) {
            const { outputContainerRef, outputEndRef } = useAssistantOutputScroll({
                hasConversation: true,
                messages,
                ready: true,
            });
            return (
                <div ref={outputContainerRef} data-testid="scroll-box" style={{ height: 120, overflow: 'auto' }}>
                    <div data-scroll-intent-boundary="" data-testid="archive-overlay" />
                    <div>{messages.map((message) => message.content || message.reasoning || '')}</div>
                    <div
                        ref={(node) => {
                            outputEndRef.current = node;
                            if (node) node.scrollIntoView = scrollIntoViewMock;
                        }}
                    />
                </div>
            );
        }
        const { getByTestId, rerender } = render(<Harness messages={[user, first]} />);
        Object.defineProperties(getByTestId('archive-overlay'), {
            clientHeight: { configurable: true, value: 100 },
            scrollHeight: { configurable: true, value: 4000 },
        });

        scrollIntoViewMock.mockClear();
        rerender(
            <Harness messages={[user, { ...first, reasoning: 'Task received\nPreparing the execution path' }]} />,
        );
        expect(scrollIntoViewMock).toHaveBeenCalled();

        scrollIntoViewMock.mockClear();
        rerender(
            <Harness
                messages={[user, { ...first, reasoning: 'Task received\nPreparing the execution path\nStill thinking' }]}
            />,
        );
        expect(scrollIntoViewMock).toHaveBeenCalled();
    });

    it('does not follow while the transcript is covered and pins once it is shown', () => {
        const user = makeMsg({ role: 'user', content: 'Ningbo weather' });
        const first = makeMsg({ id: 'a1', role: 'assistant', content: 'Ning' });
        const suspendedRef = { current: true };
        function Harness({ suspended, messages }: { suspended: boolean; messages: ChatMessage[] }) {
            suspendedRef.current = suspended;
            const { outputContainerRef, outputEndRef } = useAssistantOutputScroll({
                hasConversation: true,
                messages,
                ready: true,
                suspendedRef,
            });
            return (
                <div ref={outputContainerRef} data-testid="scroll-box" hidden={suspended || undefined}>
                    <div>{messages.map((message) => message.content)}</div>
                    <div data-scroll-intent-boundary="" data-testid="archive-overlay" />
                    <div data-nested-scroll="" data-testid="nested-scroll" />
                    <div
                        ref={(node) => {
                            outputEndRef.current = node;
                            if (node) node.scrollIntoView = scrollIntoViewMock;
                        }}
                    />
                </div>
            );
        }
        const grown = [user, { ...first, content: 'Ningbo weather looks' }];
        const { getByTestId, rerender } = render(<Harness suspended messages={[user, first]} />);
        const nested = getByTestId('nested-scroll');
        const overlay = getByTestId('archive-overlay');
        Object.defineProperty(nested, 'scrollHeight', { configurable: true, value: 640 });
        Object.defineProperty(overlay, 'scrollHeight', { configurable: true, value: 900 });
        scrollIntoViewMock.mockClear();
        rerender(<Harness suspended messages={grown} />);
        expect(scrollIntoViewMock).not.toHaveBeenCalled();
        rerender(<Harness suspended={false} messages={grown} />);
        expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'auto', block: 'end' });
        expect(nested.scrollTop).toBe(640);
        expect(overlay.scrollTop).toBe(0);
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

    it('clears the follow latch on a forced pin while covered so the uncover pins', () => {
        const user = makeMsg({ role: 'user', content: 'Earlier question' });
        const assistant = makeMsg({ id: 'a1', role: 'assistant', content: 'Earlier answer' });
        const suspendedRef = { current: false };
        let forced: ((behavior?: ScrollBehavior, force?: boolean, settleFrames?: number) => void) | null = null;
        let latched = () => false;
        function Harness({ suspended }: { suspended: boolean }) {
            const { handleScroll, handleUserScrollIntent, outputContainerRef, outputEndRef, scrollToBottom, userScrolledUpRef } = useAssistantOutputScroll({
                hasConversation: true,
                messages: [user, assistant],
                ready: true,
                suspendedRef,
            });
            forced = scrollToBottom;
            latched = () => userScrolledUpRef.current;
            return (
                <div
                    ref={outputContainerRef}
                    data-testid="scroll-box"
                    hidden={suspended || undefined}
                    onScroll={handleScroll}
                    onWheel={handleUserScrollIntent}
                    onTouchMove={handleUserScrollIntent}
                    onPointerDown={handleUserScrollIntent}
                >
                    <div>{assistant.content}</div>
                    <div
                        ref={(node) => {
                            outputEndRef.current = node;
                            if (node) node.scrollIntoView = scrollIntoViewMock;
                        }}
                    />
                </div>
            );
        }
        const { getByTestId, rerender } = render(<Harness suspended={false} />);
        const box = getByTestId('scroll-box');
        Object.defineProperties(box, {
            clientHeight: { configurable: true, value: 100 },
            scrollHeight: { configurable: true, value: 400 },
            scrollTop: { configurable: true, value: 0, writable: true },
        });
        fireEvent.wheel(box, { deltaY: -40 });
        fireEvent.scroll(box);
        expect(latched()).toBe(true);

        // The new-task cover is up: the pin itself is suspended, but the
        // forced request still has to drop the latch the user left behind.
        rerender(<Harness suspended />);
        scrollIntoViewMock.mockClear();
        act(() => { forced?.('auto', true); });
        expect(scrollIntoViewMock).not.toHaveBeenCalled();
        expect(latched()).toBe(false);

        rerender(<Harness suspended={false} />);
        expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'auto', block: 'end' });
        expect(box.scrollTop).toBe(400);
    });

    it('does not rescan the transcript for token-only updates', () => {
        let roleReads = 0;
        const messages = Array.from({ length: 120 }, (_, index) => {
            const role = index === 119 ? 'assistant' : (index % 2 === 0 ? 'user' : 'assistant');
            const message = makeMsg({ id: `m${index}`, role, content: index === 119 ? 'answer' : `message ${index}` });
            Object.defineProperty(message, 'role', {
                configurable: true,
                get: () => {
                    roleReads += 1;
                    return role;
                },
            });
            return message;
        });
        const { rerender } = render(<ScrollHarness messages={messages} />);
        const readsAfterMount = roleReads;
        const tail = messages[messages.length - 1];

        rerender(<ScrollHarness messages={[
            ...messages.slice(0, -1),
            makeMsg({ id: tail.id, role: 'assistant', content: 'answer grows' }),
        ]} />);

        expect(roleReads).toBe(readsAfterMount);
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

    it('latches an upward wheel that chains through an exhausted nested pane', () => {
        const user = makeMsg({ role: 'user', content: 'Earlier question' });
        const assistant = makeMsg({ id: 'a1', role: 'assistant', content: 'Earlier answer' });
        const { getByTestId, rerender } = render(<ScrollHarness messages={[user, assistant]} />);
        const box = getByTestId('scroll-box');
        Object.defineProperties(box, {
            clientHeight: { configurable: true, value: 100 },
            scrollHeight: { configurable: true, value: 400 },
            scrollTop: { configurable: true, value: 0, writable: true },
        });
        // The nested thought pane is at its top: the wheel-up cannot move it,
        // so the gesture chains to the transcript and must count as intent.
        // Previously it was swallowed, the scroll stayed intentless, and the
        // next streaming pin yanked the view back to the bottom.
        fireEvent.wheel(getByTestId('nested-scroll'), { deltaY: -40 });
        fireEvent.scroll(box);

        scrollIntoViewMock.mockClear();
        rerender(<ScrollHarness messages={[user, { ...assistant, content: 'Earlier answer plus more' }]} />);
        expect(scrollIntoViewMock).not.toHaveBeenCalled();
    });

    it('does not re-pin when a host-initiated round appends a user row while scrolled up', () => {
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
        // A backend-initiated round (goal continuation, remote control, ACP)
        // appends a user row the reader did not type. It must not yank the
        // scrolled-up view back to the tail.
        const hostRow = makeMsg({ id: 'u2', role: 'user', content: '〔远控〕 continue', hostInitiated: true });
        const placeholder = makeMsg({ id: 'a2', role: 'assistant', content: '' });
        rerender(<ScrollHarness messages={[user1, assistant, hostRow, placeholder]} />);
        expect(scrollIntoViewMock).not.toHaveBeenCalled();
    });

    it('exposes scrolledUp and unreadTail for a jump-to-latest affordance', () => {
        const user = makeMsg({ role: 'user', content: 'Earlier question' });
        const assistant = makeMsg({ id: 'a1', role: 'assistant', content: 'Earlier answer' });
        const apiBox: { current: ReturnType<typeof useAssistantOutputScroll> | null } = { current: null };
        function Harness({ messages }: { messages: ChatMessage[] }) {
            const api = useAssistantOutputScroll({ hasConversation: true, messages, ready: true });
            apiBox.current = api;
            return (
                <div
                    ref={api.outputContainerRef}
                    data-testid="scroll-box"
                    onScroll={api.handleScroll}
                    onWheel={api.handleUserScrollIntent}
                    onTouchMove={api.handleUserScrollIntent}
                    onPointerDown={api.handleUserScrollIntent}
                    style={{ height: 100, overflow: 'auto' }}
                >
                    <div>{messages.map((message) => message.content)}</div>
                    <div
                        ref={(node) => {
                            api.outputEndRef.current = node;
                            if (node) node.scrollIntoView = scrollIntoViewMock;
                        }}
                    />
                </div>
            );
        }
        const { getByTestId, rerender } = render(<Harness messages={[user, assistant]} />);
        const box = getByTestId('scroll-box');
        Object.defineProperties(box, {
            clientHeight: { configurable: true, value: 100 },
            scrollHeight: { configurable: true, value: 400 },
            scrollTop: { configurable: true, value: 0, writable: true },
        });
        fireEvent.wheel(box, { deltaY: -40 });
        fireEvent.scroll(box);
        expect(apiBox.current?.scrolledUp).toBe(true);
        expect(apiBox.current?.unreadTail).toBe(false);

        // Content growing below the fold while latched flags the unread tail.
        rerender(<Harness messages={[user, { ...assistant, content: 'Earlier answer plus more' }]} />);
        expect(apiBox.current?.unreadTail).toBe(true);

        act(() => { apiBox.current?.scrollToBottom('auto', true); });
        expect(apiBox.current?.scrolledUp).toBe(false);
        expect(apiBox.current?.unreadTail).toBe(false);
    });

    function makeRect(top: number, bottom: number): DOMRect {
        return { top, bottom, left: 0, right: 0, width: 0, height: bottom - top, x: 0, y: top, toJSON: () => ({}) } as DOMRect;
    }
    function stubRect(el: Element, top: number, bottom: number) {
        (el as HTMLElement).getBoundingClientRect = () => makeRect(top, bottom);
    }

    it('re-anchors a latched viewport when the transcript reflows above it', () => {
        const user = makeMsg({ role: 'user', content: 'Earlier question' });
        const assistant = makeMsg({ id: 'a1', role: 'assistant', content: 'Earlier answer' });
        const { getByTestId, rerender } = render(<ScrollHarness messages={[user, assistant]} />);
        const box = getByTestId('scroll-box');
        const content = getByTestId('scroll-content');
        stubRect(box, 0, 100);
        // The reader's anchor row: top edge 40px above the scrollport.
        stubRect(content, -40, 60);
        Object.defineProperties(box, {
            clientHeight: { configurable: true, value: 100 },
            scrollHeight: { configurable: true, value: 400 },
            scrollTop: { configurable: true, value: 0, writable: true },
        });
        fireEvent.wheel(box, { deltaY: -40 });
        fireEvent.scroll(box);

        // The transcript re-projects and 500px lands above the anchor row.
        // Without anchoring the browser drops the reader at scrollTop ~0 —
        // the jump behind "scrolled up and lost the goal closeout".
        stubRect(content, 460, 560);
        rerender(<ScrollHarness messages={[user, assistant]} />);
        expect(box.scrollTop).toBe(500);
    });

    it('re-anchors by row index when the anchored node was remounted with a new identity', () => {
        const assistant = makeMsg({ id: 'a1', role: 'assistant', content: 'answer' });
        function RemountHarness({ tag, messages }: { tag: 'div' | 'section'; messages: ChatMessage[] }) {
            const api = useAssistantOutputScroll({ hasConversation: true, messages, ready: true });
            const Tag = tag;
            return (
                <div
                    ref={api.outputContainerRef}
                    data-testid="scroll-box"
                    onScroll={api.handleScroll}
                    onWheel={api.handleUserScrollIntent}
                    onTouchMove={api.handleUserScrollIntent}
                    onPointerDown={api.handleUserScrollIntent}
                >
                    {/* A remount that also loses the testid can only be
                        recovered by the stored child index. */}
                    <Tag data-testid={`anchor-row-${tag}`}>row</Tag>
                    <div
                        ref={(node) => {
                            api.outputEndRef.current = node;
                            if (node) node.scrollIntoView = scrollIntoViewMock;
                        }}
                    />
                </div>
            );
        }
        const { getByTestId, rerender } = render(<RemountHarness tag="div" messages={[assistant]} />);
        const box = getByTestId('scroll-box');
        stubRect(box, 0, 100);
        stubRect(getByTestId('anchor-row-div'), -40, 60);
        Object.defineProperties(box, {
            clientHeight: { configurable: true, value: 100 },
            scrollHeight: { configurable: true, value: 400 },
            scrollTop: { configurable: true, value: 0, writable: true },
        });
        fireEvent.wheel(box, { deltaY: -40 });
        fireEvent.scroll(box);

        // A different element type remounts the row: the old node detaches,
        // the index fallback finds the replacement and restores toward its
        // (unstubbed, zero-rect) position — drift of 40px to reach relTop -40.
        rerender(<RemountHarness tag="section" messages={[assistant]} />);
        expect(box.scrollTop).toBe(40);
    });

    it('re-anchors by testid before falling back to the stored index', () => {
        const assistant = makeMsg({ id: 'a1', role: 'assistant', content: 'answer' });
        function ReorderHarness({ flipped, messages }: { flipped: boolean; messages: ChatMessage[] }) {
            const api = useAssistantOutputScroll({ hasConversation: true, messages, ready: true });
            const anchorTag: 'div' | 'section' = flipped ? 'section' : 'div';
            const AnchorTag = anchorTag;
            return (
                <div
                    ref={api.outputContainerRef}
                    data-testid="scroll-box"
                    onScroll={api.handleScroll}
                    onWheel={api.handleUserScrollIntent}
                    onTouchMove={api.handleUserScrollIntent}
                    onPointerDown={api.handleUserScrollIntent}
                >
                    {flipped ? <div key="x" data-testid="row-x" /> : null}
                    <div key="a" data-testid="row-a" />
                    {/* The type change remounts the anchored row while its
                        testid (message id) survives — the stored index now
                        points at a different row and must not win. */}
                    <AnchorTag key="t" data-testid="anchor-me" />
                    <div
                        ref={(node) => {
                            api.outputEndRef.current = node;
                            if (node) node.scrollIntoView = scrollIntoViewMock;
                        }}
                    />
                </div>
            );
        }
        const { getByTestId, rerender } = render(<ReorderHarness flipped={false} messages={[assistant]} />);
        const box = getByTestId('scroll-box');
        stubRect(box, 0, 100);
        stubRect(getByTestId('row-a'), -200, -100);
        stubRect(getByTestId('anchor-me'), -40, 60);
        Object.defineProperties(box, {
            clientHeight: { configurable: true, value: 100 },
            scrollHeight: { configurable: true, value: 400 },
            scrollTop: { configurable: true, value: 200, writable: true },
        });
        fireEvent.wheel(box, { deltaY: -40 });
        fireEvent.scroll(box);

        // The remount pushes the anchored row to index 2 while the stored
        // index 1 now holds row-a. Its stubbed top would give drift -160;
        // the testid match on the new (zero-rect) node gives +40.
        rerender(<ReorderHarness flipped messages={[assistant]} />);
        expect(box.scrollTop).toBe(240);
    });

    function NestedHarness({ messages }: { messages: ChatMessage[] }) {
        const api = useAssistantOutputScroll({ hasConversation: true, messages, ready: true });
        apiBoxRef.current = api;
        return (
            <div
                ref={api.outputContainerRef}
                data-testid="scroll-box"
                onScroll={api.handleScroll}
                onWheel={api.handleUserScrollIntent}
                onTouchMove={api.handleUserScrollIntent}
                onPointerDown={api.handleUserScrollIntent}
            >
                <div>row</div>
                <div data-nested-scroll="" data-testid="nested-pane">pane</div>
                <div
                    ref={(node) => {
                        api.outputEndRef.current = node;
                        if (node) node.scrollIntoView = scrollIntoViewMock;
                    }}
                />
            </div>
        );
    }
    const apiBoxRef: { current: ReturnType<typeof useAssistantOutputScroll> | null } = { current: null };

    function latchableBox(box: HTMLElement) {
        Object.defineProperties(box, {
            clientHeight: { configurable: true, value: 100 },
            scrollHeight: { configurable: true, value: 400 },
            scrollTop: { configurable: true, value: 200, writable: true },
        });
    }

    it('latches when a downward finger pull tops out a nested pane', () => {
        const assistant = makeMsg({ id: 'a1', role: 'assistant', content: 'answer' });
        const { getByTestId } = render(<NestedHarness messages={[assistant]} />);
        const box = getByTestId('scroll-box');
        const pane = getByTestId('nested-pane');
        Object.defineProperty(pane, 'scrollTop', { configurable: true, value: 0 });
        latchableBox(box);

        // Finger moves down (clientY grows): the topped pane cannot consume
        // the drag, it chains to the transcript — leave-the-tail intent.
        fireEvent.touchMove(pane, { touches: [{ clientY: 300 }] });
        fireEvent.touchMove(pane, { touches: [{ clientY: 360 }] });
        fireEvent.scroll(box);
        expect(apiBoxRef.current?.scrolledUp).toBe(true);
    });

    it('ignores a finger-up drag inside a topped nested pane', () => {
        const assistant = makeMsg({ id: 'a1', role: 'assistant', content: 'answer' });
        const { getByTestId } = render(<NestedHarness messages={[assistant]} />);
        const box = getByTestId('scroll-box');
        const pane = getByTestId('nested-pane');
        Object.defineProperty(pane, 'scrollTop', { configurable: true, value: 0 });
        latchableBox(box);

        // Finger moves up (clientY shrinks): the pane has room to scroll.
        fireEvent.touchMove(pane, { touches: [{ clientY: 300 }] });
        fireEvent.touchMove(pane, { touches: [{ clientY: 240 }] });
        fireEvent.scroll(box);
        expect(apiBoxRef.current?.scrolledUp).toBe(false);
    });

    it('does not measure transcript rows while the view follows the tail', () => {
        const rectSpy = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect');
        try {
            const user = makeMsg({ role: 'user', content: 'q' });
            const assistant = makeMsg({ id: 'a1', role: 'assistant', content: 'a' });
            const { rerender } = render(<ScrollHarness messages={[user, assistant]} />);
            rectSpy.mockClear();
            rerender(<ScrollHarness messages={[user, { ...assistant, content: 'a2' }]} />);
            rerender(<ScrollHarness messages={[user, { ...assistant, content: 'a3' }]} />);
            expect(rectSpy).not.toHaveBeenCalled();
        } finally {
            rectSpy.mockRestore();
        }
    });

    it('flags the tail as unread when the latch predates the transcript', () => {
        const apiBox: { current: ReturnType<typeof useAssistantOutputScroll> | null } = { current: null };
        function Harness({ messages, hasConversation, seq }: { messages: ChatMessage[]; hasConversation: boolean; seq: number }) {
            const api = useAssistantOutputScroll({ hasConversation, messages, ready: true, scrollToTopSeq: seq });
            apiBox.current = api;
            return (
                <div
                    ref={api.outputContainerRef}
                    data-testid="scroll-box"
                    onScroll={api.handleScroll}
                    style={{ height: 100, overflow: 'auto' }}
                >
                    <div>{messages.map((message) => message.content)}</div>
                    <div
                        ref={(node) => {
                            api.outputEndRef.current = node;
                            if (node) node.scrollIntoView = scrollIntoViewMock;
                        }}
                    />
                </div>
            );
        }
        const { getByTestId, rerender } = render(<Harness messages={[]} hasConversation={false} seq={0} />);
        const box = getByTestId('scroll-box');
        (box as HTMLElement).scrollTo = vi.fn();
        // The wizard's scroll-to-top latches an empty view. Its unread baseline
        // is the empty tail stamp, which must not suppress the flag.
        rerender(<Harness messages={[]} hasConversation={false} seq={1} />);
        expect(apiBox.current?.scrolledUp).toBe(true);
        rerender(<Harness messages={[makeMsg({ id: 'a1', role: 'assistant', content: 'started' })]} hasConversation={true} seq={1} />);
        expect(apiBox.current?.unreadTail).toBe(true);
    });

    it('parks the transcript on a user turn', () => {
        const user = makeMsg({ id: 'u1', role: 'user', content: 'hello there' });
        const assistant = makeMsg({ id: 'a1', role: 'assistant', content: 'reply' });
        function Harness() {
            const api = useAssistantOutputScroll({ hasConversation: true, messages: [user, assistant], ready: true });
            return (
                <div ref={api.outputContainerRef} data-testid="jump-box" onScroll={api.handleScroll} style={{ height: 100, overflow: 'auto' }}>
                    <div className="mc-conversation-body">
                        <div data-testid="assistant-chat-user-u1">hello there</div>
                        <div style={{ height: 800 }}>reply</div>
                    </div>
                    <div ref={api.outputEndRef} />
                    <button type="button" data-testid="jump" onClick={() => { api.jumpToUserTurn('missing'); api.jumpToUserTurn('u1'); }}>jump</button>
                    <div data-testid="latched">{api.scrolledUp ? 'up' : 'follow'}</div>
                </div>
            );
        }
        const view = render(<Harness />);
        const box = view.getByTestId('jump-box');
        const node = view.getByTestId('assistant-chat-user-u1');
        const rect = (top: number, height: number): DOMRect => ({ top, bottom: top + height, left: 0, right: 10, width: 10, height, x: 0, y: top, toJSON() { return {}; } });
        node.getBoundingClientRect = () => rect(500, 40);
        box.getBoundingClientRect = () => rect(0, 100);
        Object.defineProperty(box, 'scrollHeight', { configurable: true, value: 2000 });
        Object.defineProperty(box, 'clientHeight', { configurable: true, value: 100 });
        fireEvent.click(view.getByTestId('jump'));
        expect(box.scrollTop).toBe(488);
        expect(view.getByTestId('latched').textContent).toBe('up');
    });
});