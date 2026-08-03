import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { WelcomePromptParamDialog } from "../WelcomePromptParamDialog";
import { lightTheme } from "../aiAssistantPanelTheme";
import {
    WELCOME_CODING_ENV_KEY,
    WELCOME_FIELD_VALUES_KEY,
    welcomePromptKey,
} from "../welcomeTaskMemory";

describe("WelcomePromptParamDialog", () => {
    beforeEach(() => {
        localStorage.clear();
    });

    it("renders labeled fields and inserts filled prompt", () => {
        const onSubmit = vi.fn();
        const onClose = vi.fn();
        render(
            <WelcomePromptParamDialog
                open
                onClose={onClose}
                lang="zh"
                theme={lightTheme}
                title="做一份可汇报的竞品分析"
                description="结论、对比表"
                template={"帮我做竞品分析\n行业/赛道：[例如 SaaS]\n我方产品：[名称]\n请输出结论。"}
                submitMode="chat"
                canSend
                onSubmit={onSubmit}
            />,
        );

        expect(screen.getByRole("dialog")).toBeTruthy();
        expect(screen.getByText("做一份可汇报的竞品分析")).toBeTruthy();
        expect(screen.getByLabelText("行业/赛道")).toBeTruthy();
        expect(screen.getByLabelText("我方产品")).toBeTruthy();

        fireEvent.change(screen.getByLabelText("行业/赛道"), { target: { value: "协作 SaaS" } });
        fireEvent.change(screen.getByLabelText("我方产品"), { target: { value: "MaClaw" } });
        fireEvent.click(screen.getByTestId("welcome-prompt-param-insert"));

        expect(onSubmit).toHaveBeenCalledTimes(1);
        const [filled, mode, action] = onSubmit.mock.calls[0];
        expect(mode).toBe("chat");
        expect(action).toBe("insert");
        expect(filled).toContain("行业/赛道：协作 SaaS");
        expect(filled).toContain("我方产品：MaClaw");
        expect(filled).not.toContain("[例如 SaaS]");
    });

    it("primary submit sends for chat mode", () => {
        const onSubmit = vi.fn();
        render(
            <WelcomePromptParamDialog
                open
                onClose={() => {}}
                lang="zh"
                theme={lightTheme}
                title="周报"
                template={"项目名称：[名称]\n本周进展：[要点]"}
                submitMode="chat"
                canSend
                onSubmit={onSubmit}
            />,
        );
        expect(screen.getByTestId("welcome-prompt-param-submit").textContent).toContain("直接发送");
        fireEvent.change(screen.getByLabelText("项目名称"), { target: { value: "Alpha" } });
        fireEvent.click(screen.getByTestId("welcome-prompt-param-submit"));
        expect(onSubmit.mock.calls[0][2]).toBe("send");
        expect(onSubmit.mock.calls[0][0]).toContain("Alpha");
    });

    it("requires remote env before creating coding task", () => {
        const onSubmit = vi.fn();
        render(
            <WelcomePromptParamDialog
                open
                onClose={() => {}}
                lang="zh"
                theme={lightTheme}
                title="排查修复线上故障"
                template={"现象：[用户看到什么]\n期望：[修复后]"}
                submitMode="remote_coding_dev"
                onSubmit={onSubmit}
            />,
        );
        expect(screen.getByTestId("welcome-prompt-param-submit").textContent).toContain("远程任务");
        expect(screen.getByTestId("welcome-coding-env")).toBeTruthy();
        expect(screen.queryByTestId("welcome-prompt-param-insert")).toBeNull();
        fireEvent.click(screen.getByTestId("welcome-prompt-param-submit"));
        expect(onSubmit).not.toHaveBeenCalled();
        expect(screen.getByTestId("welcome-coding-env-error").textContent).toMatch(/主机|密码/);
    });

    it("submits remote coding env with autoCreate when complete", () => {
        const onSubmit = vi.fn();
        render(
            <WelcomePromptParamDialog
                open
                onClose={() => {}}
                lang="zh"
                theme={lightTheme}
                title="排查修复线上故障"
                template={"现象：[用户看到什么]"}
                submitMode="remote_coding_dev"
                onSubmit={onSubmit}
            />,
        );
        fireEvent.change(screen.getByTestId("welcome-remote-host"), { target: { value: "10.0.0.8" } });
        fireEvent.change(screen.getByTestId("welcome-remote-user"), { target: { value: "ubuntu" } });
        fireEvent.change(screen.getByTestId("welcome-remote-password"), { target: { value: "secret" } });
        fireEvent.change(screen.getByTestId("welcome-remote-workdir"), { target: { value: "/app" } });
        fireEvent.change(screen.getByLabelText("现象"), { target: { value: "5xx" } });
        fireEvent.click(screen.getByTestId("welcome-prompt-param-submit"));
        expect(onSubmit).toHaveBeenCalledTimes(1);
        const [filled, mode, action, codingEnv] = onSubmit.mock.calls[0];
        expect(mode).toBe("remote_coding_dev");
        expect(action).toBe("insert");
        expect(filled).toContain("5xx");
        expect(codingEnv).toMatchObject({
            autoCreate: true,
            remote: { host: "10.0.0.8", user: "ubuntu", workDir: "/app", password: "secret" },
        });
    });

    it("labels remote ops as a read-only SSH diagnosis and forwards its safety posture", () => {
        const onSubmit = vi.fn();
        render(
            <WelcomePromptParamDialog
                open
                onClose={() => {}}
                lang="zh"
                theme={lightTheme}
                title="排查服务器磁盘占满"
                template={"症状：[磁盘告警]"}
                submitMode="remote_coding_dev"
                remoteSafety="diagnosis"
                onSubmit={onSubmit}
            />,
        );
        expect(screen.getByTestId("welcome-prompt-param-submit").textContent).toContain("只读诊断");
        expect(screen.getByText(/首轮只读：不会改文件、重启服务或创建目录/)).toBeTruthy();

        fireEvent.change(screen.getByTestId("welcome-remote-host"), { target: { value: "10.0.0.8" } });
        fireEvent.change(screen.getByTestId("welcome-remote-user"), { target: { value: "ubuntu" } });
        fireEvent.change(screen.getByTestId("welcome-remote-password"), { target: { value: "secret" } });
        fireEvent.change(screen.getByTestId("welcome-remote-workdir"), { target: { value: "/srv/app" } });
        fireEvent.change(screen.getByLabelText("症状"), { target: { value: "磁盘 100%" } });
        fireEvent.click(screen.getByTestId("welcome-prompt-param-submit"));

        expect(onSubmit.mock.calls[0][3]).toMatchObject({
            autoCreate: true,
            remoteSafety: "diagnosis",
            remote: { host: "10.0.0.8", user: "ubuntu", workDir: "/srv/app", password: "secret" },
        });
        expect(localStorage.getItem(WELCOME_CODING_ENV_KEY)).toBeNull();
    });

    it("applies suggestion chips to the field value", () => {
        const onSubmit = vi.fn();
        render(
            <WelcomePromptParamDialog
                open
                onClose={() => {}}
                lang="zh"
                theme={lightTheme}
                title="邮件"
                template={"语气：[正式 / 简洁 / 强硬]"}
                submitMode="chat"
                canSend
                onSubmit={onSubmit}
            />,
        );
        fireEvent.click(screen.getByTestId("welcome-param-chip-f0-简洁"));
        fireEvent.click(screen.getByTestId("welcome-prompt-param-insert"));
        expect(onSubmit.mock.calls[0][0]).toContain("语气：简洁");
    });

    it("restores remembered field values and shows preview", () => {
        const taskKey = welcomePromptKey("writing", "Write a project weekly update");
        localStorage.setItem(
            WELCOME_FIELD_VALUES_KEY,
            JSON.stringify({ [taskKey]: { "项目名称": "Beta" } }),
        );
        render(
            <WelcomePromptParamDialog
                open
                onClose={() => {}}
                lang="zh"
                theme={lightTheme}
                title="写一份项目周报"
                template={"项目名称：[名称]\n本周进展：[要点]"}
                taskKey={taskKey}
                submitMode="chat"
                canSend
                onSubmit={() => {}}
            />,
        );
        expect((screen.getByLabelText("项目名称") as HTMLInputElement).value).toBe("Beta");

        fireEvent.click(screen.getByTestId("welcome-prompt-preview-toggle"));
        expect(screen.getByTestId("welcome-prompt-preview-body").textContent).toContain("Beta");
    });

    it("saves template without submitting", () => {
        const onSubmit = vi.fn();
        const onSaveTemplate = vi.fn<(filledPrompt: string, title: string, codingEnv?: unknown) => boolean>(() => true);
        render(
            <WelcomePromptParamDialog
                open
                onClose={() => {}}
                lang="zh"
                theme={lightTheme}
                title="周报"
                template={"项目名称：[名称]"}
                submitMode="chat"
                canSend
                onSubmit={onSubmit}
                onSaveTemplate={onSaveTemplate}
            />,
        );
        fireEvent.change(screen.getByLabelText("项目名称"), { target: { value: "Alpha" } });
        fireEvent.click(screen.getByTestId("welcome-prompt-save-template"));
        expect(onSaveTemplate).toHaveBeenCalledTimes(1);
        expect(onSaveTemplate.mock.calls[0][0]).toContain("Alpha");
        expect(onSaveTemplate.mock.calls[0][1]).toBe("周报");
        expect(onSaveTemplate.mock.calls[0][2]).toBeUndefined();
        expect(onSubmit).not.toHaveBeenCalled();
        expect(screen.getByTestId("welcome-prompt-save-note").textContent).toMatch(/已保存|Saved/);
    });

    it("saves remote coding env with password and restores it from initialCodingEnv", () => {
        const onSaveTemplate = vi.fn<(filledPrompt: string, title: string, codingEnv?: unknown) => boolean>(() => true);
        const { rerender } = render(
            <WelcomePromptParamDialog
                open
                onClose={() => {}}
                lang="zh"
                theme={lightTheme}
                title="驱网状态"
                template={"现象：[用户看到什么]"}
                submitMode="remote_coding_dev"
                onSubmit={() => {}}
                onSaveTemplate={onSaveTemplate}
            />,
        );
        fireEvent.change(screen.getByTestId("welcome-remote-host"), { target: { value: "192.168.1.10" } });
        fireEvent.change(screen.getByTestId("welcome-remote-user"), { target: { value: "ubuntu" } });
        fireEvent.change(screen.getByTestId("welcome-remote-password"), { target: { value: "secret" } });
        fireEvent.change(screen.getByTestId("welcome-remote-workdir"), { target: { value: "/home/ubuntu/app" } });
        fireEvent.change(screen.getByLabelText("现象"), { target: { value: "查看服务器上的运行状态" } });
        fireEvent.click(screen.getByTestId("welcome-prompt-save-template"));
        expect(onSaveTemplate).toHaveBeenCalledTimes(1);
        expect(onSaveTemplate.mock.calls[0][2]).toEqual({
            remote: {
                host: "192.168.1.10",
                port: 22,
                user: "ubuntu",
                workDir: "/home/ubuntu/app",
                password: "secret",
            },
        });
        expect(screen.getByTestId("welcome-prompt-save-note").textContent).toMatch(/密码|password/i);

        // Re-open with the saved coding env prefills host/user/workdir/password.
        rerender(
            <WelcomePromptParamDialog
                open
                onClose={() => {}}
                lang="zh"
                theme={lightTheme}
                title="驱网状态"
                template={"在远程环境排查并修复线上故障\n现象：查看服务器上的运行状态"}
                submitMode="remote_coding_dev"
                initialCodingEnv={{
                    remote: {
                        host: "192.168.1.10",
                        port: 22,
                        user: "ubuntu",
                        workDir: "/home/ubuntu/app",
                        password: "secret",
                    },
                }}
                onSubmit={() => {}}
            />,
        );
        expect((screen.getByTestId("welcome-remote-host") as HTMLInputElement).value).toBe("192.168.1.10");
        expect((screen.getByTestId("welcome-remote-user") as HTMLInputElement).value).toBe("ubuntu");
        expect((screen.getByTestId("welcome-remote-workdir") as HTMLInputElement).value).toBe("/home/ubuntu/app");
        expect((screen.getByTestId("welcome-remote-password") as HTMLInputElement).value).toBe("secret");
    });

    it("prefills local workdir from remembered coding env", () => {
        localStorage.setItem(
            "maclaw:welcome-coding-env",
            JSON.stringify({ workingDir: "D:/work/remembered" }),
        );
        render(
            <WelcomePromptParamDialog
                open
                onClose={() => {}}
                lang="zh"
                theme={lightTheme}
                title="实现功能"
                template={"需求描述：[一句话]"}
                submitMode="coding_dev"
                onSubmit={() => {}}
            />,
        );
        expect((screen.getByTestId("welcome-local-workdir") as HTMLInputElement).value).toBe("D:/work/remembered");
    });

    it("left-aligns dialog titles and field labels (overrides global html text-align:center)", () => {
        render(
            <WelcomePromptParamDialog
                open
                onClose={() => {}}
                lang="zh"
                theme={lightTheme}
                title="按需求实现功能"
                template={"需求描述：[一句话目标 + 关键点]\n验收标准：[怎样算完成]\n约束：[兼容接口 / 不大重构 / 其他]"}
                submitMode="coding_dev"
                onSubmit={() => {}}
            />,
        );

        const dialog = screen.getByTestId("welcome-prompt-param-dialog");
        expect(dialog.style.textAlign).toBe("left");

        // Labels render (optional suffix is nested); inputs are labeled for a11y.
        expect(screen.getByLabelText("需求描述")).toBeTruthy();
        expect(screen.getByLabelText("验收标准")).toBeTruthy();
        expect(screen.getByLabelText("约束")).toBeTruthy();
        expect(screen.getByText("本地工作目录")).toBeTruthy();
    });
});
