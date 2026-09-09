import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { PassthroughCommandsPanel } from '../PassthroughCommandsPanel';
import { DeletePassthroughCommand, ExportPassthroughCommand, ListPassthroughCommands, RunPassthroughCommand, SavePassthroughCommand, SavePassthroughSettings, SetPassthroughCommandEnabled } from '../../../../wailsjs/go/main/App';
import { main } from '../../../../wailsjs/go/models';

const dialogConfirm = vi.fn<(message: string) => Promise<boolean>>();
const dialogPrompt = vi.fn<(message: string, defaultValue: string) => Promise<string | null>>();

vi.mock("../../../../wailsjs/go/main/App", () => ({
    DeletePassthroughCommand: vi.fn(),
    ExportPassthroughCommand: vi.fn().mockResolvedValue("/runctl save git-status --cmd \"git -C ${target} status --short\" --params-json '[{\"name\":\"target\",\"type\":\"path\",\"required\":true,\"example\":\"D:\\\\workprj\\\\aicoder\"}]' --confirm"),
    GetPassthroughSettings: vi.fn().mockResolvedValue({ allow_exec: false }),
    ListPassthroughAudit: vi.fn().mockResolvedValue([]),
    ListPassthroughCommands: vi.fn().mockResolvedValue([]),
    PassthroughRegistryPath: vi.fn().mockResolvedValue("C:\\Users\\tester\\.maclaw\\passthrough\\commands.json"),
    PreviewPassthroughCommand: vi.fn().mockResolvedValue(["git", "-C", "D:\\workprj\\aicoder", "status"]),
    PreviewPassthroughDraftCommand: vi.fn().mockResolvedValue(["git", "-C", "D:\\workprj\\aicoder", "status", "--short"]),
    RunPassthroughCommand: vi.fn().mockResolvedValue({ command_name: "repair-env", status: "success", exit_code: 0, duration_ms: 10, output: "ok" }),
    SavePassthroughCommand: vi.fn().mockImplementation(async (cmd) => cmd),
    SavePassthroughSettings: vi.fn().mockImplementation(async (settings) => settings),
    SetPassthroughCommandEnabled: vi.fn(),
}));

vi.mock("../../CustomDialog", () => ({
    useDialog: () => ({
        showAlert: vi.fn(),
        showConfirm: (message: string) => dialogConfirm(message),
        showPrompt: (message: string, _title?: string, options?: { defaultValue?: string }) =>
            dialogPrompt(message, options?.defaultValue ?? ''),
    }),
}));

async function renderPanelWithForm() {
    const result = render(<PassthroughCommandsPanel lang="zh-Hans" />);
    fireEvent.click(await screen.findByText("+ 新建"));
    return result;
}

describe("PassthroughCommandsPanel", () => {
    beforeEach(() => {
        vi.clearAllMocks();
        dialogConfirm.mockResolvedValue(true);
        dialogPrompt.mockResolvedValue(null);
    });

    it("keeps pristine new task form quiet while required actions stay disabled", async () => {
        await renderPanelWithForm();

        expect(await screen.findByText("直通任务")).toBeTruthy();
        expect(screen.queryByText(/任务名不能为空/)).toBeNull();
        expect(screen.queryByText(/命令模板不能为空/)).toBeNull();
        expect(screen.getByText("保存")).toHaveProperty("disabled", true);
        expect(screen.getByText("预览 argv")).toHaveProperty("disabled", true);
    });

    it("edits command templates with structured parameters", async () => {
        await renderPanelWithForm();

        expect(await screen.findByText("直通任务")).toBeTruthy();
        expect(screen.getAllByText("命令模板").length).toBeGreaterThan(0);

        fireEvent.change(screen.getByPlaceholderText("repair-env"), { target: { value: "git-status" } });
        fireEvent.change(screen.getByPlaceholderText("例如：git -C ${target} status --short"), {
            target: { value: "git -C ${target} status --short" },
        });
        fireEvent.click(screen.getByText("添加形参"));
        fireEvent.change(screen.getByPlaceholderText("target"), { target: { value: "target" } });

        fireEvent.click(screen.getByText("保存"));

        await waitFor(() => {
            expect(SavePassthroughCommand).toHaveBeenCalledWith(expect.objectContaining({
                name: "git-status",
                script_path: "git",
                template_args: ["-C", "${target}", "status", "--short"],
                params: [expect.objectContaining({ name: "target", type: "text" })],
            }));
        });
    });

    it("keeps Windows backslashes in command templates", async () => {
        await renderPanelWithForm();

        fireEvent.change(await screen.findByPlaceholderText("repair-env"), { target: { value: "repair-env" } });
        fireEvent.change(screen.getByPlaceholderText("例如：git -C ${target} status --short"), {
            target: { value: "D:\\ops\\repair.ps1 ${target}" },
        });
        fireEvent.click(screen.getByText("添加形参"));
        fireEvent.change(screen.getByPlaceholderText("target"), { target: { value: "target" } });
        fireEvent.click(screen.getByText("保存"));

        await waitFor(() => {
            expect(SavePassthroughCommand).toHaveBeenLastCalledWith(expect.objectContaining({
                script_path: "D:\\ops\\repair.ps1",
                template_args: ["${target}"],
            }));
        });
    });

    it("keeps escaped quotes inside command template arguments", async () => {
        await renderPanelWithForm();

        fireEvent.change(await screen.findByPlaceholderText("repair-env"), { target: { value: "quoted-args" } });
        fireEvent.change(screen.getByPlaceholderText("例如：git -C ${target} status --short"), {
            target: { value: "tool --message \"say \\\"hello\\\"\"" },
        });
        fireEvent.click(screen.getByText("保存"));

        await waitFor(() => {
            expect(SavePassthroughCommand).toHaveBeenLastCalledWith(expect.objectContaining({
                script_path: "tool",
                template_args: ["--message", "say \"hello\""],
            }));
        });
    });

    it("preserves empty quoted command template arguments", async () => {
        await renderPanelWithForm();

        fireEvent.change(await screen.findByPlaceholderText("repair-env"), { target: { value: "empty-arg" } });
        fireEvent.change(screen.getByPlaceholderText("例如：git -C ${target} status --short"), {
            target: { value: "tool \"\" '' tail" },
        });
        fireEvent.click(screen.getByText("保存"));

        await waitFor(() => {
            expect(SavePassthroughCommand).toHaveBeenLastCalledWith(expect.objectContaining({
                script_path: "tool",
                template_args: ["", "", "tail"],
            }));
        });
    });

    it("round-trips single quotes and trailing backslashes in displayed command templates", async () => {
        vi.mocked(ListPassthroughCommands).mockResolvedValueOnce([{
            name: "quoted-args",
            script_path: "tool",
            template_args: ["--message", "Bob's", "C:\\ops dir\\", ""],
            runtime: "direct",
            timeout_seconds: 120,
            confirm_required: true,
            enabled: true,
            params: [],
        }] as unknown as main.PassthroughCommand[]);
        await renderPanelWithForm();

        expect((await screen.findAllByText("quoted-args")).length).toBeGreaterThan(0);
        fireEvent.click(screen.getByText("编辑"));
        expect(screen.getByDisplayValue("tool --message \"Bob's\" \"C:\\\\ops dir\\\\\" \"\"")).toBeTruthy();
        fireEvent.click(screen.getByText("保存"));

        await waitFor(() => {
            expect(SavePassthroughCommand).toHaveBeenLastCalledWith(expect.objectContaining({
                script_path: "tool",
                template_args: ["--message", "Bob's", "C:\\ops dir\\", ""],
            }));
        });
    });

    it("shows the simple /exec emergency example when enabled", async () => {
        dialogConfirm.mockResolvedValueOnce(true);
        await renderPanelWithForm();

        fireEvent.click(await screen.findByText("允许 /exec 一次性系统命令（需 --confirm，不经过 shell）"));

        await waitFor(() => {
            expect(screen.getByText(/\/exec git status --short --confirm/)).toBeTruthy();
        });
    });

    it("requires confirmation before enabling /exec from the monitor panel", async () => {
        dialogConfirm.mockResolvedValueOnce(false).mockResolvedValueOnce(true);
        await renderPanelWithForm();

        const toggle = await screen.findByText("允许 /exec 一次性系统命令（需 --confirm，不经过 shell）");
        fireEvent.click(toggle);
        expect(SavePassthroughSettings).not.toHaveBeenCalled();

        fireEvent.click(toggle);
        await waitFor(() => {
            expect(SavePassthroughSettings).toHaveBeenCalledWith(expect.objectContaining({ allow_exec: true }));
        });
    });

    it("shows registry path and remote /exec toggle commands", async () => {
        await renderPanelWithForm();

        expect(await screen.findByText(/commands\.json/)).toBeTruthy();
        expect(screen.getByText(/\/runctl exec enable/)).toBeTruthy();
        expect(screen.getByText(/\/runctl exec disable/)).toBeTruthy();
        expect(screen.getByText(/--cmd 'git -C \$\{target\} status --short'/)).toBeTruthy();
        expect(screen.getByText(/--params-json/)).toBeTruthy();
        expect(screen.getByText(/敏感参数值会脱敏/)).toBeTruthy();
    });

    it("copies separate run and registration command snippets", async () => {
        vi.mocked(ListPassthroughCommands).mockResolvedValueOnce([{
            name: "git-status",
            script_path: "git",
            template_args: ["-C", "${target}", "status", "--short"],
            runtime: "direct",
            timeout_seconds: 120,
            confirm_required: true,
            enabled: true,
            params: [{ name: "target", type: "path", required: true, example: "D:\\workprj\\aicoder" }],
        }] as unknown as main.PassthroughCommand[]);
        Object.defineProperty(navigator, "clipboard", {
            value: { writeText: vi.fn().mockRejectedValue(new Error("clipboard unavailable")) },
            configurable: true,
        });
        await renderPanelWithForm();

        expect(await screen.findByText("复制运行")).toBeTruthy();
        fireEvent.click(screen.getByText("复制注册"));

        await waitFor(() => {
            expect(ExportPassthroughCommand).toHaveBeenCalledWith("git-status");
        });
        await screen.findAllByText(/\/runctl save git-status/);
        expect(screen.getAllByText(/\/runctl save git-status/).length).toBeGreaterThan(0);
        expect(screen.getAllByText(/--params-json/).length).toBeGreaterThan(0);
    });

    it("warns before saving an unterminated quoted command template", async () => {
        await renderPanelWithForm();

        fireEvent.change(await screen.findByPlaceholderText("repair-env"), { target: { value: "bad-template" } });
        fireEvent.change(screen.getByPlaceholderText("例如：git -C ${target} status --short"), {
            target: { value: "\"D:\\ops\\repair.ps1 ${target}" },
        });

        expect(await screen.findByText(/引号未闭合/)).toBeTruthy();
        expect(screen.getByText("保存")).toHaveProperty("disabled", true);
        expect(screen.getByText("保存并测试")).toHaveProperty("disabled", true);
    });

    it("warns when a command template references an undefined parameter", async () => {
        await renderPanelWithForm();

        fireEvent.change(await screen.findByPlaceholderText("repair-env"), { target: { value: "bad-template" } });
        fireEvent.change(screen.getByPlaceholderText("例如：git -C ${target} status --short"), {
            target: { value: "git -C ${target} status" },
        });

        expect(await screen.findByText(/未定义/)).toBeTruthy();
        expect(screen.getByText("保存")).toHaveProperty("disabled", true);
    });

    it("warns before saving an empty command template", async () => {
        await renderPanelWithForm();

        fireEvent.change(await screen.findByPlaceholderText("repair-env"), { target: { value: "empty-template" } });

        expect(await screen.findByText(/命令模板不能为空/)).toBeTruthy();
        expect(screen.getByText("保存")).toHaveProperty("disabled", true);
        expect(screen.getByText("预览 argv")).toHaveProperty("disabled", true);
    });

    it("warns before saving an invalid task name", async () => {
        await renderPanelWithForm();

        fireEvent.change(await screen.findByPlaceholderText("repair-env"), { target: { value: "bad name" } });
        fireEvent.change(screen.getByPlaceholderText("例如：git -C ${target} status --short"), {
            target: { value: "git status" },
        });

        expect(await screen.findByText(/任务名需以字母或数字开头/)).toBeTruthy();
        expect(screen.getByText("保存")).toHaveProperty("disabled", true);
    });

    it("warns before saving a timeout outside the backend limit", async () => {
        await renderPanelWithForm();

        fireEvent.change(await screen.findByPlaceholderText("repair-env"), { target: { value: "slow-task" } });
        fireEvent.change(screen.getByPlaceholderText("例如：git -C ${target} status --short"), {
            target: { value: "git status" },
        });
        fireEvent.change(screen.getByRole("spinbutton"), { target: { value: "3601" } });

        expect(await screen.findByText(/超时秒数必须在 1 到 3600 之间/)).toBeTruthy();
        expect(screen.getByText("保存")).toHaveProperty("disabled", true);
    });

    it("warns before saving an invalid numeric test value", async () => {
        await renderPanelWithForm();

        fireEvent.change(await screen.findByPlaceholderText("repair-env"), { target: { value: "bad-number" } });
        fireEvent.change(screen.getByPlaceholderText("例如：git -C ${target} status --short"), {
            target: { value: "tool --count ${count}" },
        });
        fireEvent.click(screen.getByText("添加形参"));
        fireEvent.change(screen.getByPlaceholderText("target"), { target: { value: "count" } });
        const selects = screen.getAllByRole("combobox");
        fireEvent.change(selects[selects.length - 1], { target: { value: "number" } });
        const textboxes = screen.getAllByRole("textbox");
        fireEvent.change(textboxes[textboxes.length - 1], { target: { value: "many" } });

        expect(await screen.findByText(/必须是数字/)).toBeTruthy();
        expect(screen.getByText("保存")).toHaveProperty("disabled", true);
    });

    it("warns before saving an invalid boolean default value", async () => {
        await renderPanelWithForm();

        fireEvent.change(await screen.findByPlaceholderText("repair-env"), { target: { value: "bad-boolean" } });
        fireEvent.change(screen.getByPlaceholderText("例如：git -C ${target} status --short"), {
            target: { value: "tool --deep ${deep}" },
        });
        fireEvent.click(screen.getByText("添加形参"));
        fireEvent.change(screen.getByPlaceholderText("target"), { target: { value: "deep" } });
        const selects = screen.getAllByRole("combobox");
        fireEvent.change(selects[selects.length - 1], { target: { value: "boolean" } });
        const textboxes = screen.getAllByRole("textbox");
        fireEvent.change(textboxes[textboxes.length - 2], { target: { value: "maybe" } });

        expect(await screen.findByText(/必须是布尔值/)).toBeTruthy();
        expect(screen.getByText("保存")).toHaveProperty("disabled", true);
    });

    it("previews draft argv with test parameter values before saving", async () => {
        await renderPanelWithForm();

        fireEvent.change(await screen.findByPlaceholderText("repair-env"), { target: { value: "git-status" } });
        fireEvent.change(screen.getByPlaceholderText("例如：git -C ${target} status --short"), {
            target: { value: "git -C ${target} status --short" },
        });
        fireEvent.click(screen.getByText("添加形参"));
        fireEvent.change(screen.getByPlaceholderText("target"), { target: { value: "target" } });
        const textboxes = screen.getAllByRole("textbox");
        fireEvent.change(textboxes[textboxes.length - 1], { target: { value: "D:\\workprj\\aicoder" } });

        fireEvent.click(screen.getByText("预览 argv"));

        expect(await screen.findByText("git -C D:\\workprj\\aicoder status --short")).toBeTruthy();
    });

    it("saves test values as parameter examples for remote run snippets", async () => {
        await renderPanelWithForm();

        fireEvent.change(await screen.findByPlaceholderText("repair-env"), { target: { value: "git-status" } });
        fireEvent.change(screen.getByPlaceholderText("例如：git -C ${target} status --short"), {
            target: { value: "git -C ${target} status --short" },
        });
        fireEvent.click(screen.getByText("添加形参"));
        fireEvent.change(screen.getByPlaceholderText("target"), { target: { value: "target" } });
        const textboxes = screen.getAllByRole("textbox");
        fireEvent.change(textboxes[textboxes.length - 1], { target: { value: "D:\\workprj\\aicoder" } });

        fireEvent.click(screen.getByText("保存"));

        await waitFor(() => {
            expect(SavePassthroughCommand).toHaveBeenLastCalledWith(expect.objectContaining({
                params: [expect.objectContaining({ name: "target", example: "D:\\workprj\\aicoder" })],
            }));
        });
    });

    it("preserves explicit empty and padded parameter examples", async () => {
        await renderPanelWithForm();

        fireEvent.change(await screen.findByPlaceholderText("repair-env"), { target: { value: "precise-values" } });
        fireEvent.change(screen.getByPlaceholderText("例如：git -C ${target} status --short"), {
            target: { value: "tool --message ${message} --empty ${empty}" },
        });
        fireEvent.click(screen.getByText("添加形参"));
        fireEvent.change(screen.getByPlaceholderText("target"), { target: { value: "message" } });
        let textboxes = screen.getAllByRole("textbox");
        fireEvent.change(textboxes[textboxes.length - 1], { target: { value: "  hello  " } });

        fireEvent.click(screen.getByText("添加形参"));
        fireEvent.change(screen.getAllByPlaceholderText("target")[1], { target: { value: "empty" } });
        textboxes = screen.getAllByRole("textbox");
        fireEvent.change(textboxes[textboxes.length - 1], { target: { value: "" } });

        await waitFor(() => {
            expect(document.body.textContent).toContain('--message "  hello  "');
            expect(document.body.textContent).toContain('--empty ""');
        });

        fireEvent.click(screen.getByText("保存"));

        await waitFor(() => {
            expect(SavePassthroughCommand).toHaveBeenLastCalledWith(expect.objectContaining({
                params: [
                    expect.objectContaining({ name: "message", example: "  hello  " }),
                    expect.objectContaining({ name: "empty", example: "" }),
                ],
            }));
        });
    });

    it("uses equals syntax in run snippets when a parameter value starts with dashes", async () => {
        await renderPanelWithForm();

        fireEvent.change(await screen.findByPlaceholderText("repair-env"), { target: { value: "flag-value" } });
        fireEvent.change(screen.getByPlaceholderText("例如：git -C ${target} status --short"), {
            target: { value: "tool --message ${message}" },
        });
        fireEvent.click(screen.getByText("添加形参"));
        fireEvent.change(screen.getByPlaceholderText("target"), { target: { value: "message" } });
        const textboxes = screen.getAllByRole("textbox");
        fireEvent.change(textboxes[textboxes.length - 1], { target: { value: "--force" } });

        expect(await screen.findByText(/--message=--force/)).toBeTruthy();
    });

    it("allows clearing an existing parameter example", async () => {
        vi.mocked(ListPassthroughCommands).mockResolvedValueOnce([{
            name: "git-status",
            script_path: "git",
            template_args: ["-C", "${target}", "status"],
            runtime: "direct",
            timeout_seconds: 120,
            confirm_required: true,
            enabled: true,
            params: [{ name: "target", type: "path", required: true, example: "D:\\workprj\\aicoder" }],
        }] as unknown as main.PassthroughCommand[]);
        render(<PassthroughCommandsPanel lang="zh-Hans" />);

        expect((await screen.findAllByText("git-status")).length).toBeGreaterThan(0);
        fireEvent.click(screen.getByText("编辑"));
        fireEvent.change(screen.getByDisplayValue("D:\\workprj\\aicoder"), { target: { value: "" } });
        fireEvent.click(screen.getByText("保存"));

        await waitFor(() => {
            expect(SavePassthroughCommand).toHaveBeenLastCalledWith(expect.objectContaining({
                params: [expect.objectContaining({ name: "target", example: "" })],
            }));
        });
    });

    it("shows raw output when a test run returns a failed result", async () => {
        vi.mocked(RunPassthroughCommand).mockResolvedValueOnce({
            command_name: "fail-test",
            status: "failed",
            exit_code: 7,
            duration_ms: 12,
            output: "failed-output",
            started_at: "2025-01-01T00:00:00Z",
            finished_at: "2025-01-01T00:00:01Z",
        });
        dialogConfirm.mockResolvedValueOnce(true);
        await renderPanelWithForm();

        fireEvent.change(await screen.findByPlaceholderText("repair-env"), { target: { value: "fail-test" } });
        fireEvent.change(screen.getByPlaceholderText("例如：git -C ${target} status --short"), {
            target: { value: "git status" },
        });
        fireEvent.click(screen.getByText("保存并测试"));

        expect(await screen.findByText("failed-output")).toBeTruthy();
    });

    it("requires confirmation before test-running a passthrough task", async () => {
        vi.mocked(ListPassthroughCommands).mockResolvedValueOnce([{
            name: "git-status",
            script_path: "git",
            template_args: ["status"],
            runtime: "direct",
            timeout_seconds: 120,
            confirm_required: true,
            enabled: true,
            params: [],
        }] as unknown as main.PassthroughCommand[]);
        dialogConfirm.mockResolvedValueOnce(false).mockResolvedValueOnce(true);
        render(<PassthroughCommandsPanel lang="zh-Hans" />);

        expect((await screen.findAllByText("git-status")).length).toBeGreaterThan(0);
        fireEvent.click(screen.getByText("测试"));
        expect(RunPassthroughCommand).not.toHaveBeenCalled();

        fireEvent.click(screen.getByText("测试"));
        await waitFor(() => {
            expect(RunPassthroughCommand).toHaveBeenCalledWith("git-status", {}, true);
        });
    });

    it("does not save when save-and-test confirmation is cancelled", async () => {
        dialogConfirm.mockResolvedValueOnce(false);
        await renderPanelWithForm();

        fireEvent.change(await screen.findByPlaceholderText("repair-env"), { target: { value: "cancel-test" } });
        fireEvent.change(screen.getByPlaceholderText("例如：git -C ${target} status --short"), {
            target: { value: "git status" },
        });
        fireEvent.click(screen.getByText("保存并测试"));

        expect(SavePassthroughCommand).not.toHaveBeenCalled();
        expect(RunPassthroughCommand).not.toHaveBeenCalled();
    });

    it("requires confirmation before enabling a disabled passthrough task from the monitor panel", async () => {
        vi.mocked(ListPassthroughCommands).mockResolvedValueOnce([{
            name: "git-status",
            script_path: "git",
            template_args: ["status"],
            runtime: "direct",
            timeout_seconds: 120,
            confirm_required: true,
            enabled: false,
            params: [],
        }] as unknown as main.PassthroughCommand[]);
        dialogConfirm.mockResolvedValueOnce(false).mockResolvedValueOnce(true);
        render(<PassthroughCommandsPanel lang="zh-Hans" />);

        expect((await screen.findAllByText("git-status")).length).toBeGreaterThan(0);
        const enableButton = () => screen.getAllByText("启用").find((el) => el.tagName === "BUTTON") as HTMLElement;
        fireEvent.click(enableButton());
        expect(SetPassthroughCommandEnabled).not.toHaveBeenCalled();
        await waitFor(() => expect(dialogConfirm).toHaveBeenCalledTimes(1));

        fireEvent.click(enableButton());
        await waitFor(() => {
            expect(SetPassthroughCommandEnabled).toHaveBeenCalledWith("git-status", true);
        });
    });

    it("requires confirmation before deleting a passthrough task", async () => {
        vi.mocked(ListPassthroughCommands).mockResolvedValueOnce([{
            name: "git-status",
            script_path: "git",
            template_args: ["status"],
            runtime: "direct",
            timeout_seconds: 120,
            confirm_required: true,
            enabled: true,
            params: [],
        }] as unknown as main.PassthroughCommand[]);
        dialogConfirm.mockResolvedValueOnce(false).mockResolvedValueOnce(true);
        render(<PassthroughCommandsPanel lang="zh-Hans" />);

        expect((await screen.findAllByText("git-status")).length).toBeGreaterThan(0);
        fireEvent.click(screen.getByText("删除"));
        expect(DeletePassthroughCommand).not.toHaveBeenCalled();

        fireEvent.click(screen.getByText("删除"));
        await waitFor(() => {
            expect(DeletePassthroughCommand).toHaveBeenCalledWith("git-status");
        });
    });
});
