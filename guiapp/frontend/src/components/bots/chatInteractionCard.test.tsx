import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { ChatInteractionCard, interactionDecision, parseChatInteraction, type ChatInteraction } from './chatInteractionCard';

const handoff: ChatInteraction = {
    id: 'card_1',
    task_id: 'task-a',
    run_id: 'run-a',
    bot_id: 'bot-a',
    kind: 'native_handoff',
    question: '请在这台电脑上完成登录',
    choices: [{ id: 'cancel', label: '取消' }, { id: 'completed', label: '我已完成' }],
    operation: { kind: 'verify_native', version: 1, hash: 'abc' },
    target: { origin: 'shop.example', url: 'https://shop.example/pay' },
    handoff: 'full_page',
    spotlight: false,
    shot: { id: 'card_1', captured_at: '2026-11-17T00:00:00Z', lease_id: 'lease-a', desktop_mode: 'isolated', bot_id: 'bot-a', task_id: 'task-a' },
    lease_id: 'lease-a',
    desktop_mode: 'isolated',
    status: 'pending',
};

describe('chat interaction card', () => {
    it('shows the instruction and opens this task computer from the screenshot', () => {
        const onDecide = vi.fn();
        const onOpenDesktop = vi.fn();
        render(
            <ChatInteractionCard
                card={handoff}
                shotData="data:image/png;base64,aaaa"
                lang="zh-Hans"
                onDecide={onDecide}
                onOpenDesktop={onOpenDesktop}
            />,
        );
        expect(screen.getByTestId('chat-interaction-question').textContent).toBe('请在这台电脑上完成登录');
        expect(screen.getByTestId('chat-interaction-origin').textContent).toBe('shop.example');
        expect(screen.getByTestId('chat-interaction-status').textContent).toContain('待你处理');
        expect(screen.queryByTestId('chat-interaction-field-password')).toBeNull();
        fireEvent.click(screen.getByTestId('chat-interaction-shot'));
        expect(onOpenDesktop).toHaveBeenCalledWith({
            botId: 'bot-a',
            requestId: 'card_1',
            runId: 'run-a',
            leaseId: 'lease-a',
            capturedAt: '2026-11-17T00:00:00Z',
            shotId: 'card_1',
        });
        fireEvent.click(screen.getByTestId('chat-interaction-choice-completed'));
        const decision = onDecide.mock.calls[0][0];
        expect(decision.choice_ids).toEqual(['completed']);
        expect(decision.task_id).toBe('task-a');
        expect(decision.payload_hash).toBe('abc');
        expect(decision.done).toBeUndefined();
        expect(decision.user_id).toBeUndefined();
        expect(decision.resume).toBeUndefined();
    });

    it('ignores a forged image and a password field on the card payload', () => {
        const parsed = parseChatInteraction(JSON.stringify({
            ...handoff,
            image: 'data:image/png;base64,forged',
            fields: [{ id: 'password', label: '密码' }, { id: 'note', label: '备注' }],
        }));
        expect(parsed?.fields?.map(field => field.id)).toEqual(['note']);
        expect(parsed?.spotlight).toBe(false);
        const { container } = render(
            <ChatInteractionCard card={parsed!} lang="zh-Hans" onDecide={vi.fn()} onOpenDesktop={vi.fn()} />,
        );
        expect(container.querySelector('img')).toBeNull();
        expect(interactionDecision(parsed!, ['note'], { password: 'secret' })).toBeUndefined();
    });

    it('a confirm choice does not add a purchase grant', () => {
        const card = parseChatInteraction({
            id: 'card_2', task_id: 'task-b', run_id: 'run-b', bot_id: 'bot-a',
            kind: 'confirm', question: '继续这一步？',
            choices: [{ id: 'approve', label: '确认' }, { id: 'decline', label: '取消' }],
            operation: { kind: 'continue', version: 1, hash: 'def' },
            status: 'pending',
        });
        const decision = interactionDecision(card!, ['approve']);
        expect(decision?.choice_ids).toEqual(['approve']);
        expect((decision as { granted?: unknown }).granted).toBeUndefined();
    });
});
