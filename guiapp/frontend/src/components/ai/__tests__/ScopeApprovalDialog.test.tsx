import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ScopeApprovalDialog, type ScopeApprovalRequest } from "../ScopeApprovalDialog";

const approval: ScopeApprovalRequest = {
    id: "approval-1",
    tool: "ssh_bash",
    path: "ls -la /home/prj8/build",
    projectPath: "/home/prj8",
    directory: "/home/prj8",
    timeoutSeconds: 7,
    kind: "remote_high_risk_bash",
    message: "",
    autoAllow: false,
    maintenance: false,
};

function renderDialog(overrides: Partial<Parameters<typeof ScopeApprovalDialog>[0]> = {}) {
    const onResolve = vi.fn();
    render(
        <ScopeApprovalDialog
            lang="zh"
            approval={approval}
            isHighRisk
            isRemoteHighRisk
            isRemoteMaintenance={false}
            countdown={7}
            onResolve={onResolve}
            {...overrides}
        />,
    );
    return onResolve;
}

describe("ScopeApprovalDialog", () => {
    it("names the task grant 以后允许 and keeps this-time as the primary action", () => {
        const onResolve = renderDialog();
        expect(screen.getByRole("alertdialog", { name: "远程命令确认" })).toBeTruthy();
        expect(screen.queryByRole("button", { name: "以后放行" })).toBeNull();
        fireEvent.click(screen.getByRole("button", { name: "以后允许" }));
        expect(onResolve).toHaveBeenCalledWith("full_access");
        expect(screen.getByRole("button", { name: "本次放行 (7s)" }).className).toContain("btn-primary");
    });

    it("uses the local command title and denies on Escape", () => {
        const onResolve = renderDialog({ isRemoteHighRisk: false });
        expect(screen.getByRole("alertdialog", { name: "命令确认" })).toBeTruthy();
        fireEvent.keyDown(window, { key: "Escape" });
        fireEvent.keyDown(window, { key: "Escape" });
        expect(onResolve).toHaveBeenCalledTimes(1);
        expect(onResolve).toHaveBeenCalledWith("deny");
    });

    it("keeps directory approval on its own labels", () => {
        const onResolve = renderDialog({ isHighRisk: false, isRemoteHighRisk: false });
        fireEvent.click(screen.getByRole("button", { name: "完全访问" }));
        expect(onResolve).toHaveBeenCalledWith("full_access");
        expect(screen.getByRole("button", { name: "允许该目录 (7s)" })).toBeTruthy();
    });

    it("asks for this call shape without a countdown", () => {
        const onResolve = renderDialog({
            isHighRisk: false,
            isRemoteHighRisk: false,
            countdown: 0,
            approval: {
                ...approval,
                kind: "commit_shape",
                tool: "bash",
                path: "git push origin main",
                timeoutSeconds: 0,
                message: "执行 bash 缺少人的批准，这一步先不执行。要允许这一形状的调用吗？",
            },
        });
        expect(screen.getByRole("alertdialog", { name: "调用确认" })).toBeTruthy();
        expect(screen.getByText("执行 bash 缺少人的批准，这一步先不执行。要允许这一形状的调用吗？")).toBeTruthy();
        expect(screen.getByText("这次调用")).toBeTruthy();
        expect(screen.getByText("git push origin main")).toBeTruthy();
        expect(screen.queryByRole("button", { name: /本次放行|以后允许|完全访问|允许该目录/ })).toBeNull();
        expect(screen.queryByText(/自动拒绝/)).toBeNull();
        fireEvent.click(screen.getByRole("button", { name: "仅这一次" }));
        expect(onResolve).toHaveBeenCalledWith("allow_once");
    });

    it("keeps a task-shaped allow on the task button", () => {
        const onResolve = renderDialog({
            isHighRisk: false,
            countdown: 0,
            approval: { ...approval, kind: "commit_shape", timeoutSeconds: 0, message: "执行 mailbox_send 缺少人的批准，这一步先不执行。要允许这一形状的调用吗？" },
        });
        fireEvent.click(screen.getByRole("button", { name: "本任务内允许" }));
        expect(onResolve).toHaveBeenCalledWith("allow_task");
    });
});
