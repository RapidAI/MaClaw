import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';

import { RemoteSessionCard } from '../RemoteSessionCard';
import type { RemoteSessionView } from '../types';

function buildSession(overrides: Partial<RemoteSessionView> = {}): RemoteSessionView {
    return {
        id: 'sess-card-1',
        tool: 'claude',
        title: 'Untitled',
        project_path: 'D:/workprj/aicoder',
        status: 'waiting_input',
        summary: {
            status: 'waiting_input',
            current_task: 'Waiting for your answer',
            last_result: '',
            progress_summary: '',
            suggested_action: 'Choose an option or answer to continue',
            pending_question: {
                question: 'Which auth method should we use?',
            },
        },
        preview: { preview_lines: [] },
        events: [],
        ...overrides,
    };
}

describe('RemoteSessionCard pending question summary', () => {
    it('shows the pending question chip when the session is waiting for input', () => {
        render(
            <RemoteSessionCard
                session={buildSession()}
                remoteInputDrafts={{}}
                setRemoteInputDrafts={vi.fn()}
                sendRemoteInput={vi.fn().mockResolvedValue(true)}
                interruptRemoteSession={vi.fn().mockResolvedValue(undefined)}
                killRemoteSession={vi.fn().mockResolvedValue(undefined)}
                showToastMessage={vi.fn()}
                translate={(key) => key}
                formatText={(key) => key}
            />,
        );

        expect(screen.getByText('❓ Which auth method should we use?')).toBeTruthy();
        expect(screen.getByText('💡 Choose an option or answer to continue')).toBeTruthy();
    });

    it('renders busy badge from backend session status after send succeeds', () => {
        render(
            <RemoteSessionCard
                session={buildSession({
                    status: 'busy',
                    summary: {
                        status: 'busy',
                        current_task: 'Running TodoWrite',
                        last_result: '',
                        progress_summary: '',
                        suggested_action: '',
                    },
                })}
                remoteInputDrafts={{ 'sess-card-1': 'follow up' }}
                setRemoteInputDrafts={vi.fn()}
                sendRemoteInput={vi.fn().mockResolvedValue(true)}
                interruptRemoteSession={vi.fn().mockResolvedValue(undefined)}
                killRemoteSession={vi.fn().mockResolvedValue(undefined)}
                showToastMessage={vi.fn()}
                translate={(key) => key}
                formatText={(key) => key}
            />,
        );

        expect(screen.getByText('busy')).toBeTruthy();
        expect(screen.getByDisplayValue('follow up')).toBeTruthy();
    });
});
