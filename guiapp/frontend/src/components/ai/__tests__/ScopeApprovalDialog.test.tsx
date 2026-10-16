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
});
