import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

vi.mock('../AgentNetTaskBoard', () => ({
    AgentNetTaskBoard: () => <div>task panel</div>,
}));

vi.mock('../AgentNetKnowledgePanel', () => ({
    AgentNetKnowledgePanel: () => <div>knowledge panel</div>,
}));

vi.mock('../AgentNetChatPanel', () => ({
    AgentNetChatPanel: () => <div>chat panel</div>,
}));

vi.mock('../AgentNetBundlePanel', () => ({
    AgentNetBundlePanel: () => <div>bundle panel</div>,
}));

vi.mock('../AgentNetNetworkPanel', () => ({
    AgentNetNetworkPanel: () => <div>network panel</div>,
}));

vi.mock('../AgentNetPoIPanel', () => ({
    AgentNetPoIPanel: () => <div>poi panel</div>,
}));

vi.mock('../AgentNetServicesPanel', () => ({
    AgentNetServicesPanel: () => <div>services panel</div>,
}));

vi.mock('../AgentNetToolsPanel', () => ({
    AgentNetToolsPanel: () => <div>tools panel</div>,
}));

import { AgentNetTabContainer } from '../AgentNetTabContainer';

describe('AgentNetTabContainer labels', () => {
    it('localizes sub-tab labels for Simplified Chinese', () => {
        render(<AgentNetTabContainer lang="zh-Hans" agentNetRunning />);

        [
            ['任务', '任任务'],
            ['知识', '知知识'],
            ['聊天', '聊聊天'],
            ['网络', '网网络'],
            ['智能证明', '证智能证明'],
            ['服务', '服服务'],
            ['工具', '工工具'],
            ['包', '包包'],
        ].forEach(([name, visibleText]) => {
            const button = screen.getByRole('button', { name });
            expect(button.textContent).toBe(visibleText);
        });
    });

    it('localizes sub-tab labels for Traditional Chinese', () => {
        render(<AgentNetTabContainer lang="zh-Hant" agentNetRunning />);

        [
            ['任務', '任任務'],
            ['知識', '知知識'],
            ['聊天', '聊聊天'],
            ['網路', '網網路'],
            ['智能證明', '證智能證明'],
            ['服務', '服服務'],
            ['工具', '工工具'],
            ['包', '包包'],
        ].forEach(([name, visibleText]) => {
            const button = screen.getByRole('button', { name });
            expect(button.textContent).toBe(visibleText);
        });
    });

    it('keeps English labels in English locale', () => {
        render(<AgentNetTabContainer lang="en" agentNetRunning />);

        [
            ['Tasks', 'TASKTasks'],
            ['Knowledge', 'KNOWKnowledge'],
            ['Chat', 'CHATChat'],
            ['Network', 'NETNetwork'],
            ['PoI', 'POIPoI'],
            ['Services', 'SVCServices'],
            ['Tools', 'TOOLTools'],
            ['Bundle', 'NUTBundle'],
        ].forEach(([name, visibleText]) => {
            const button = screen.getByRole('button', { name });
            expect(button.textContent).toBe(visibleText);
        });
    });
});
