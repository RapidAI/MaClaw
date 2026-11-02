import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { DemoSkillPanel } from './DemoSkillPanel';
import {
    DistillDesktopDemoRecording,
    GetDesktopDemoShot,
    NarrateDesktopDemoRecording,
    RedistillDesktopDemoSkill,
    StartDesktopDemoRecording,
    StopDesktopDemoRecording,
    UpdateDesktopDemoSkill,
} from '../../../wailsjs/go/main/App';

vi.mock('../../../wailsjs/go/main/App', () => ({
    StartDesktopDemoRecording: vi.fn(),
    StopDesktopDemoRecording: vi.fn(),
    NarrateDesktopDemoRecording: vi.fn(),
    DistillDesktopDemoRecording: vi.fn(),
    GetDesktopDemoShot: vi.fn(),
    UpdateDesktopDemoSkill: vi.fn(),
    RedistillDesktopDemoSkill: vi.fn(),
}));

const labels = {
    demoSkill: '演示录成技能',
    start: '开始录制',
    stop: '结束录制',
    narrate: '旁白',
    pushToTalk: '按住说话',
    distill: '提炼技能',
    edit: '保存修改',
    redistill: '重新提炼',
    titlePlaceholder: '演示标题（可选）',
    narratePlaceholder: '说明这一步在做什么',
    notesPlaceholder: '备注（可选）',
    busy: '处理中…',
};

describe('DemoSkillPanel', () => {
    beforeEach(() => vi.clearAllMocks());

    it('starts, narrates, stops and distills a demo recording', async () => {
        vi.mocked(StartDesktopDemoRecording).mockResolvedValue({ asr_available: false });
        vi.mocked(StopDesktopDemoRecording).mockResolvedValue({
            timeline: { id: 't1', events: [{ kind: 'screenshot', ref: 's1' }] },
        });
        vi.mocked(DistillDesktopDemoRecording).mockResolvedValue({ item: { name: 'skill-a', content: 'steps' } });
        vi.mocked(GetDesktopDemoShot).mockResolvedValue({ data: 'x', mime: 'image/png' });

        render(<DemoSkillPanel botId="b1" labels={labels} />);

        fireEvent.change(screen.getByPlaceholderText('演示标题（可选）'), { target: { value: '发文章' } });
        fireEvent.click(screen.getByRole('button', { name: '开始录制' }));
        await waitFor(() => expect(StartDesktopDemoRecording).toHaveBeenCalledWith('b1', '发文章'));

        // 录制中：标题输入锁定，出现旁白行与结束按钮。
        // mock 在点击时同步调用，录制态要等 React 刷新，必须一起 waitFor。
        await waitFor(() => expect(screen.getByPlaceholderText('演示标题（可选）')).toHaveProperty('disabled', true));
        fireEvent.change(screen.getByPlaceholderText('说明这一步在做什么'), { target: { value: '打开表单' } });
        fireEvent.click(screen.getByRole('button', { name: '旁白' }));
        await waitFor(() => expect(NarrateDesktopDemoRecording).toHaveBeenCalledWith('b1', '打开表单', '', ''));

        fireEvent.click(screen.getByRole('button', { name: '结束录制' }));
        await waitFor(() => expect(StopDesktopDemoRecording).toHaveBeenCalledWith('b1'));
        expect(await screen.findByText(/1\. 画面 s1/)).toBeTruthy();

        fireEvent.change(screen.getByPlaceholderText('备注（可选）'), { target: { value: '重点' } });
        fireEvent.click(screen.getByRole('button', { name: '提炼技能' }));
        await waitFor(() => expect(DistillDesktopDemoRecording).toHaveBeenCalledWith('b1', expect.anything(), '重点'));
        expect(await screen.findByText('skill-a')).toBeTruthy();
        await waitFor(() => expect(GetDesktopDemoShot).toHaveBeenCalledWith('b1', 't1', 's1'));
    });

    it('surfaces a hub error without leaving recording state broken', async () => {
        vi.mocked(StartDesktopDemoRecording).mockResolvedValue({ error: 'hub offline' });

        render(<DemoSkillPanel botId="b1" labels={labels} />);
        fireEvent.click(screen.getByRole('button', { name: '开始录制' }));

        expect(await screen.findByText('hub offline')).toBeTruthy();
        // 出错后仍在待机态：还能再点开始
        expect(screen.getByRole('button', { name: '开始录制' })).toBeTruthy();
        expect(RedistillDesktopDemoSkill).not.toHaveBeenCalled();
        expect(UpdateDesktopDemoSkill).not.toHaveBeenCalled();
    });
});
