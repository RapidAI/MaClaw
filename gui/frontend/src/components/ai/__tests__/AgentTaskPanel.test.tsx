import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AgentTaskPanel } from "../AgentTaskPanel";
import { lightTheme } from "../aiAssistantPanelTheme";
import type { AgentView } from "../agentViewTypes";

const SelectWorkingDirMock = vi.fn();

vi.mock("../../../../wailsjs/go/main/App", () => ({
    SelectWorkingDir: (...args: unknown[]) => SelectWorkingDirMock(...args),
}));

describe("AgentTaskPanel", () => {
    beforeEach(() => {
        SelectWorkingDirMock.mockReset();
    });

    it("keeps the task panel header draggable while close stays clickable", () => {
        const onDismiss = vi.fn();
        const view: AgentView = {
            type: "form",
            id: "drag-header-test",
            title: "Task details",
            fields: [{ name: "goal", label: "Goal", type: "text", value: "ship" }],
        };

        render(<AgentTaskPanel view={view} onDismiss={onDismiss} theme={lightTheme} />);

        expect(screen.getByTestId("agent-task-panel-header").style.getPropertyValue("--wails-draggable")).toBe("no-drag");
        expect(screen.getByRole("button", { name: "Close" }).style.getPropertyValue("--wails-draggable")).toBe("no-drag");
    });

    it("blocks invalid formatted values before submit", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "form",
            id: "format-test",
            title: "Contact",
            fields: [
                {
                    name: "email",
                    label: "Email",
                    type: "text",
                    format: "email",
                    value: "not-an-email",
                },
            ],
            submitLabel: "Save",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.click(screen.getByRole("button", { name: "Save" }));

        expect(onSubmit).not.toHaveBeenCalled();
        expect(screen.getByText(/Email must be a valid email/)).toBeTruthy();
    });

    it("localizes validation messages in Chinese", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "form",
            id: "format-test-zh",
            title: "联系信息",
            fields: [{ name: "email", label: "邮箱", type: "text", format: "email", value: "bad" }],
            submitLabel: "保存",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} lang="zh-Hans" />);

        fireEvent.click(screen.getByRole("button", { name: "保存" }));

        expect(onSubmit).not.toHaveBeenCalled();
        expect(screen.getByText(/邮箱 必须是有效的邮箱地址/)).toBeTruthy();
    });

    it("localizes built-in form controls in Chinese", () => {
        const view: AgentView = {
            type: "form",
            id: "localized-controls-zh",
            title: "\u5de5\u4f5c\u6d41\u53c2\u6570",
            fields: [{ name: "goal", label: "\u76ee\u6807", type: "text", value: "" }],
        };

        render(<AgentTaskPanel view={view} theme={lightTheme} lang="zh-Hans" />);

        expect(screen.getByRole("button", { name: "\u63d0\u4ea4" })).toBeTruthy();
    });

    it("submits valid formatted values", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "form",
            id: "format-test",
            title: "Contact",
            fields: [
                {
                    name: "email",
                    label: "Email",
                    type: "text",
                    format: "email",
                    value: "ops@example.com",
                },
            ],
            submitLabel: "Save",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.click(screen.getByRole("button", { name: "Save" }));

        expect(onSubmit).toHaveBeenCalledWith("format-test", { email: "ops@example.com" });
    });

    it("renders sensitive fields as password inputs while preserving submitted value", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "form",
            id: "sensitive-test",
            title: "Remote",
            fields: [{ name: "ssh_password", label: "Password", type: "text", sensitive: true, value: "secret" }],
            submitLabel: "Connect",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        const input = screen.getByLabelText("Password") as HTMLInputElement;
        expect(input.type).toBe("password");
        expect(input.value).toBe("secret");

        fireEvent.click(screen.getByRole("button", { name: "Connect" }));

        expect(onSubmit).toHaveBeenCalledWith("sensitive-test", { ssh_password: "secret" });
    });

    it("hides coding remote new-connection fields until ssh_profile is __new__", async () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "form",
            id: "coding-remote-visibility",
            title: "项目基本信息",
            fields: [
                { name: "project_name", label: "项目名称", type: "text", required: true, value: "demo" },
                { name: "description", label: "项目描述", type: "textarea", required: true, value: "desc" },
            ],
            variants: [
                {
                    id: "remote",
                    label: "远程编程",
                    fields: [
                        {
                            name: "ssh_profile",
                            label: "SSH 主机",
                            type: "select",
                            required: true,
                            value: "prod",
                            options: [
                                { label: "prod", value: "prod" },
                                { label: "新建连接…", value: "__new__" },
                            ],
                        },
                        {
                            name: "remote_host",
                            label: "主机 IP/域名",
                            type: "text",
                            required: true,
                            visibleWhen: { field: "ssh_profile", equals: "__new__" },
                        },
                        {
                            name: "ssh_password",
                            label: "密码（可选）",
                            type: "text",
                            sensitive: true,
                            required: false,
                            visibleWhen: { field: "ssh_profile", notEmpty: true },
                        },
                        {
                            name: "remote_workdir",
                            label: "远程工作目录",
                            type: "text",
                            required: true,
                            value: "/home/app",
                        },
                    ],
                },
            ],
            submitLabel: "提交",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} lang="zh-Hans" />);

        // Saved profile: host hidden; optional password still shown for session override.
        expect(screen.queryByLabelText(/主机 IP\/域名/)).toBeNull();
        expect(screen.getByLabelText(/密码/)).toBeTruthy();
        expect(screen.getByLabelText(/远程工作目录/)).toBeTruthy();

        // Switch to 新建连接 → host + password appear.
        fireEvent.change(screen.getByLabelText(/SSH 主机/), { target: { value: "__new__" } });
        expect(screen.getByLabelText(/主机 IP\/域名/)).toBeTruthy();
        expect(screen.getByLabelText(/密码/)).toBeTruthy();

        // Optional password must not block submit with a saved profile.
        fireEvent.change(screen.getByLabelText(/SSH 主机/), { target: { value: "prod" } });
        fireEvent.click(screen.getByRole("button", { name: "提交" }));
        await waitFor(() => expect(onSubmit).toHaveBeenCalled());
        const payload = onSubmit.mock.calls[0][1] as Record<string, unknown>;
        expect(payload._agent_view_variant).toBe("remote");
        expect(payload.ssh_profile).toBe("prod");
        expect(payload.remote_workdir).toBe("/home/app");
    });

    it("lets directory fields use the native directory picker", async () => {
        const onSubmit = vi.fn();
        SelectWorkingDirMock.mockResolvedValue("D:\\workprj\\demo");
        const view: AgentView = {
            type: "form",
            id: "directory-test",
            title: "Workspace",
            fields: [{ name: "workdir", label: "Working directory", type: "directory", value: "" }],
            submitLabel: "Run",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.click(screen.getByRole("button", { name: /Browse: Working directory/ }));
        await waitFor(() => expect((screen.getByRole("textbox", { name: /Working directory/ }) as HTMLInputElement).value).toBe("D:\\workprj\\demo"));
        fireEvent.click(screen.getByRole("button", { name: "Run" }));

        expect(SelectWorkingDirMock).toHaveBeenCalledTimes(1);
        expect(onSubmit).toHaveBeenCalledWith("directory-test", { workdir: "D:\\workprj\\demo" });
    });

    it("lets object form directory columns use the native directory picker", async () => {
        const onSubmit = vi.fn();
        SelectWorkingDirMock.mockResolvedValue("D:\\workprj\\nested");
        const view: AgentView = {
            type: "form",
            id: "object-directory-test",
            title: "Workspace",
            fields: [{
                name: "settings",
                label: "Settings",
                type: "object_form",
                value: { workdir: "" },
                columns: [{ name: "workdir", label: "Working directory", type: "directory" }],
            }],
            submitLabel: "Run",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.click(screen.getByRole("button", { name: /Browse: Working directory/ }));
        await waitFor(() => expect((screen.getByRole("textbox", { name: /Working directory/ }) as HTMLInputElement).value).toBe("D:\\workprj\\nested"));
        fireEvent.click(screen.getByRole("button", { name: "Run" }));

        expect(SelectWorkingDirMock).toHaveBeenCalledTimes(1);
        expect(onSubmit).toHaveBeenCalledWith("object-directory-test", { settings: { workdir: "D:\\workprj\\nested" } });
    });

    it("lets array table directory columns use the native directory picker", async () => {
        const onSubmit = vi.fn();
        SelectWorkingDirMock.mockResolvedValue("D:\\workprj\\row");
        const view: AgentView = {
            type: "form",
            id: "table-directory-test",
            title: "Workspace",
            fields: [{
                name: "jobs",
                label: "Jobs",
                type: "array_table",
                value: [{ workdir: "" }],
                columns: [{ name: "workdir", label: "Working directory", type: "directory" }],
            }],
            submitLabel: "Run",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.click(screen.getByRole("button", { name: /Browse: Working directory/ }));
        await waitFor(() => expect((screen.getByRole("textbox", { name: /Working directory 1/ }) as HTMLInputElement).value).toBe("D:\\workprj\\row"));
        fireEvent.click(screen.getByRole("button", { name: "Run" }));

        expect(SelectWorkingDirMock).toHaveBeenCalledTimes(1);
        expect(onSubmit).toHaveBeenCalledWith("table-directory-test", { jobs: [{ workdir: "D:\\workprj\\row" }] });
    });

    it("passes hidden form routing data when dismissed", () => {
        const onDismiss = vi.fn();
        const view: AgentView = {
            type: "form",
            id: "workflow:form:requirements",
            title: "Workflow",
            fields: [
                { name: "goal", label: "Goal", type: "text", value: "build app" },
                { name: "_workflow_user_id", label: "_workflow_user_id", type: "hidden", value: "desktop-user:C:/work" },
                { name: "_workflow_id", label: "_workflow_id", type: "hidden", value: "wf-1" },
            ],
            submitLabel: "Save",
        };

        render(<AgentTaskPanel view={view} onDismiss={onDismiss} theme={lightTheme} />);

        fireEvent.click(screen.getByRole("button", { name: "Close" }));

        expect(onDismiss).toHaveBeenCalledWith("workflow:form:requirements", {
            goal: "build app",
            _workflow_user_id: "desktop-user:C:/work",
            _workflow_id: "wf-1",
        });
    });

    it("locks form submission while submit is in flight", async () => {
        let resolveSubmit: (() => void) | undefined;
        const onSubmit = vi.fn(() => new Promise<void>((resolve) => {
            resolveSubmit = resolve;
        }));
        const view: AgentView = {
            type: "form",
            id: "submit-lock-test",
            title: "Expense",
            fields: [{ name: "amount", label: "Amount", type: "number", value: 86 }],
            submitLabel: "Save",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.click(screen.getByRole("button", { name: "Save" }));
        fireEvent.click(screen.getByRole("button", { name: "Submitting..." }));

        expect(onSubmit).toHaveBeenCalledTimes(1);
        resolveSubmit?.();
        await waitFor(() => expect(screen.getByRole("button", { name: "Save" })).toBeTruthy());
    });

    it("submits result browser item actions with their target view id", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "result_browser",
            id: "mis:transaction-workspace",
            title: "Business transaction workspace",
            results: [{
                id: "txn-1",
                title: "Submit expense",
                status: "awaiting_validation",
                data: { transaction_id: "txn-1" },
                actions: [{
                    label: "Continue",
                    viewId: "mis:resume-transaction",
                    primary: true,
                    data: { transaction_id: "txn-1" },
                }],
            }],
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.click(screen.getByRole("button", { name: "Continue" }));

        expect(onSubmit).toHaveBeenCalledWith("mis:resume-transaction", { transaction_id: "txn-1" });
    });

    it("submits progress view actions with their target view id", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "progress",
            id: "skill:run-status:run-1",
            title: "Running demo",
            steps: [{ title: "bash", status: "running" }],
            actions: [{
                label: "Refresh",
                viewId: "skill:status",
                primary: true,
                data: { run_id: "run-1" },
            }],
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.click(screen.getByRole("button", { name: "Refresh" }));

        expect(onSubmit).toHaveBeenCalledWith("skill:status", { run_id: "run-1" });
    });

    it("renders wizard steps and submits collected structured data", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "wizard",
            id: "mis:intent:expense.submit",
            title: "Expense",
            steps: [
                {
                    id: "basic",
                    title: "Basic",
                    fields: [{ name: "applicant", label: "Applicant", type: "text", required: true, value: "Alice" }],
                },
                {
                    id: "detail",
                    title: "Detail",
                    fields: [{ name: "amount", label: "Amount", type: "number", required: true, value: 86 }],
                },
            ],
            submitLabel: "Save",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        expect(screen.getByText("Step 1 of 2")).toBeTruthy();
        fireEvent.click(screen.getByRole("button", { name: "Next" }));
        expect(screen.getByText("Step 2 of 2")).toBeTruthy();
        fireEvent.change(screen.getByLabelText(/Amount/), { target: { value: "99" } });
        fireEvent.click(screen.getByRole("button", { name: "Save" }));

        expect(onSubmit).toHaveBeenCalledWith("mis:intent:expense.submit", { applicant: "Alice", amount: 99 });
    });

    it("blocks wizard navigation when the current step is incomplete", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "wizard",
            id: "wizard-validation",
            title: "Expense",
            steps: [
                {
                    id: "basic",
                    title: "Basic",
                    fields: [{ name: "applicant", label: "Applicant", type: "text", required: true, value: "" }],
                },
                {
                    id: "detail",
                    title: "Detail",
                    fields: [{ name: "amount", label: "Amount", type: "number", value: 86 }],
                },
            ],
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.click(screen.getByRole("button", { name: "Next" }));

        expect(screen.getByText("Step 1 of 2")).toBeTruthy();
        expect(screen.getByText(/Please fix: Applicant/)).toBeTruthy();
        expect(onSubmit).not.toHaveBeenCalled();
    });

    it("edits and submits table editor rows", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "table_editor",
            id: "mis:bulk-items",
            title: "Items",
            dataKey: "items",
            hiddenData: { transaction_id: "txn-1" },
            columns: [
                { name: "description", label: "Description", required: true },
                { name: "amount", label: "Amount", type: "number", required: true },
            ],
            rows: [{ description: "Taxi", amount: 18 }],
            submitLabel: "Save",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.change(screen.getByDisplayValue("Taxi"), { target: { value: "Train" } });
        fireEvent.click(screen.getByRole("button", { name: "Add row" }));
        fireEvent.change(screen.getAllByRole("textbox")[1], { target: { value: "Meal" } });
        fireEvent.change(screen.getByDisplayValue("0"), { target: { value: "42" } });
        fireEvent.click(screen.getByRole("button", { name: "Save" }));

        expect(onSubmit).toHaveBeenCalledWith("mis:bulk-items", {
            transaction_id: "txn-1",
            items: [
                { description: "Train", amount: 18 },
                { description: "Meal", amount: 42 },
            ],
        });
    });

    it("submits selected resources from a resource picker", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "resource_picker",
            id: "pick-employee",
            title: "Choose approvers",
            resourceType: "employee",
            multiple: true,
            dataKey: "approvers",
            hiddenData: { _mis_transaction_id: "txn-1" },
            options: [
                { label: "Alice", value: "u1", status: "Finance", description: "Finance manager" },
                { label: "Bob", value: "u2", status: "HR" },
            ],
            submitLabel: "Use selected",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        const picker = screen.getByRole("listbox") as HTMLSelectElement;
        Array.from(picker.options).forEach((option) => {
            option.selected = option.value === "u1" || option.value === "u2";
        });
        fireEvent.change(picker);
        fireEvent.click(screen.getByRole("button", { name: "Use selected" }));

        expect(onSubmit).toHaveBeenCalledWith("pick-employee", { _mis_transaction_id: "txn-1", approvers: ["u1", "u2"] });
    });

    it("submits field mapper choices", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "field_mapper",
            id: "map-import",
            title: "Map fields",
            dataKey: "import_mapping",
            hiddenData: { _mis_transaction_id: "txn-2" },
            sourceFields: ["Amount", "Receipt No", "Ignored"],
            targetFields: [
                { name: "amount", label: "Amount", required: true },
                { name: "receipt_no", label: "Receipt No" },
            ],
            submitLabel: "Apply",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.change(screen.getByLabelText(/Amount/), { target: { value: "Amount" } });
        fireEvent.change(screen.getByLabelText(/Receipt No/), { target: { value: "Receipt No" } });
        fireEvent.click(screen.getByRole("button", { name: "Apply" }));

        expect(onSubmit).toHaveBeenCalledWith("map-import", {
            _mis_transaction_id: "txn-2",
            import_mapping: { amount: "Amount", receipt_no: "Receipt No" },
        });
    });

    it("blocks values outside select options", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "form",
            id: "select-test",
            title: "Status",
            fields: [
                {
                    name: "status",
                    label: "Status",
                    type: "select",
                    options: ["open", "closed"],
                    value: "deleted",
                },
            ],
            submitLabel: "Save",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.click(screen.getByRole("button", { name: "Save" }));

        expect(onSubmit).not.toHaveBeenCalled();
        expect(screen.getByText(/Status must be one of: open, closed/)).toBeTruthy();
    });

    it("blocks values outside multiselect options", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "form",
            id: "multiselect-test",
            title: "Scopes",
            fields: [
                {
                    name: "scopes",
                    label: "Scopes",
                    type: "multiselect",
                    options: ["read", "write"],
                    value: ["read", "admin"],
                },
            ],
            submitLabel: "Save",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.click(screen.getByRole("button", { name: "Save" }));

        expect(onSubmit).not.toHaveBeenCalled();
        expect(screen.getByText(/Scopes must be one of: read, write/)).toBeTruthy();
    });

    it("blocks table select cells outside options", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "form",
            id: "table-test",
            title: "Expense",
            fields: [
                {
                    name: "items",
                    label: "Items",
                    type: "array_table",
                    columns: [
                        { name: "category", label: "Category", type: "select", options: ["transport", "meal"] },
                    ],
                    value: [{ category: "hotel" }],
                },
            ],
            submitLabel: "Save",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.click(screen.getByRole("button", { name: "Save" }));

        expect(onSubmit).not.toHaveBeenCalled();
        expect(screen.getByText(/Items\[1\].Category must be one of: transport, meal/)).toBeTruthy();
    });

    it("blocks exclusive numeric boundaries and step violations", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "form",
            id: "number-test",
            title: "Amount",
            fields: [
                {
                    name: "amount",
                    label: "Amount",
                    type: "number",
                    exclusiveMin: 0,
                    exclusiveMax: 10,
                    step: 0.5,
                    value: 10,
                },
            ],
            submitLabel: "Save",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.click(screen.getByRole("button", { name: "Save" }));

        expect(onSubmit).not.toHaveBeenCalled();
        expect(screen.getByText(/Amount must be less than 10/)).toBeTruthy();
    });

    it("blocks values that do not match constValue", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "form",
            id: "const-test",
            title: "Action",
            fields: [
                {
                    name: "action",
                    label: "Action",
                    type: "hidden",
                    constValue: "expense.submit",
                    value: "expense.delete",
                },
            ],
            submitLabel: "Save",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.click(screen.getByRole("button", { name: "Save" }));

        expect(onSubmit).not.toHaveBeenCalled();
        expect(screen.getByText(/Action must be expense.submit/)).toBeTruthy();
    });

    it("blocks duplicate array items when uniqueItems is enabled", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "form",
            id: "unique-test",
            title: "Tags",
            fields: [
                {
                    name: "tags",
                    label: "Tags",
                    type: "multiselect",
                    uniqueItems: true,
                    value: ["finance", "finance"],
                },
            ],
            submitLabel: "Save",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.click(screen.getByRole("button", { name: "Save" }));

        expect(onSubmit).not.toHaveBeenCalled();
        expect(screen.getByText(/Tags must not contain duplicate items/)).toBeTruthy();
    });

    it("keeps read-only fields from being edited", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "form",
            id: "readonly-test",
            title: "System fields",
            fields: [
                {
                    name: "applicant",
                    label: "Applicant",
                    type: "text",
                    readOnly: true,
                    value: "Alice",
                },
            ],
            submitLabel: "Save",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);
        const input = screen.getByDisplayValue("Alice") as HTMLInputElement;

        fireEvent.change(input, { target: { value: "Bob" } });
        fireEvent.click(screen.getByRole("button", { name: "Save" }));

        expect(input.readOnly).toBe(true);
        expect(onSubmit).toHaveBeenCalledWith("readonly-test", { applicant: "Alice" });
    });

    it("renders sensitive fields as password inputs", () => {
        const view: AgentView = {
            type: "form",
            id: "secret-test",
            title: "Secret",
            fields: [
                {
                    name: "token",
                    label: "Token",
                    type: "text",
                    sensitive: true,
                    value: "sk-test",
                },
            ],
            submitLabel: "Save",
        };

        render(<AgentTaskPanel view={view} theme={lightTheme} />);

        const input = screen.getByDisplayValue("sk-test") as HTMLInputElement;
        expect(input.type).toBe("password");
    });

    it("blocks dependent required fields before submit", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "form",
            id: "dependent-test",
            title: "Invoice",
            fields: [
                { name: "invoice_no", label: "Invoice No", type: "text", value: "INV-1" },
                { name: "receipt", label: "Receipt", type: "text", value: "" },
            ],
            dependentRequired: {
                invoice_no: ["receipt"],
            },
            submitLabel: "Save",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.click(screen.getByRole("button", { name: "Save" }));

        expect(onSubmit).not.toHaveBeenCalled();
        expect(screen.getByText(/Receipt is required when Invoice No is provided/)).toBeTruthy();
    });

    it("renders selected variant fields and submits the active variant data", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "form",
            id: "variant-test",
            title: "Expense",
            fields: [
                { name: "applicant", label: "Applicant", type: "text", value: "Alice" },
            ],
            variants: [
                {
                    id: "travel",
                    label: "Travel",
                    fields: [
                        { name: "destination", label: "Destination", type: "text", required: true },
                    ],
                },
                {
                    id: "meal",
                    label: "Meal",
                    fields: [
                        { name: "restaurant", label: "Restaurant", type: "text", required: true },
                    ],
                },
            ],
            submitLabel: "Save",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.change(screen.getByLabelText("Mode"), { target: { value: "meal" } });
        fireEvent.change(screen.getByLabelText(/Restaurant/), { target: { value: "Noodle House" } });
        fireEvent.click(screen.getByRole("button", { name: "Save" }));

        expect(screen.queryByLabelText("Destination")).toBeNull();
        expect(onSubmit).toHaveBeenCalledWith("variant-test", {
            _agent_view_variant: "meal",
            applicant: "Alice",
            restaurant: "Noodle House",
        });
    });

    it("drops inactive variant fields from submitted data", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "form",
            id: "variant-clean-submit-test",
            title: "Expense",
            fields: [
                { name: "_tool_args", label: "_tool_args", type: "hidden", value: { draft_id: "d1" } },
                { name: "applicant", label: "Applicant", type: "text", value: "Alice" },
            ],
            variants: [
                {
                    id: "travel",
                    label: "Travel",
                    fields: [{ name: "destination", label: "Destination", type: "text" }],
                },
                {
                    id: "meal",
                    label: "Meal",
                    fields: [{ name: "restaurant", label: "Restaurant", type: "text" }],
                },
            ],
            submitLabel: "Save",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);

        fireEvent.change(screen.getByLabelText("Destination"), { target: { value: "Shanghai" } });
        fireEvent.change(screen.getByLabelText("Mode"), { target: { value: "meal" } });
        fireEvent.change(screen.getByLabelText("Restaurant"), { target: { value: "Noodle House" } });
        fireEvent.click(screen.getByRole("button", { name: "Save" }));

        expect(onSubmit).toHaveBeenCalledWith("variant-clean-submit-test", {
            _agent_view_variant: "meal",
            _tool_args: { draft_id: "d1" },
            applicant: "Alice",
            restaurant: "Noodle House",
        });
    });

    it("blocks object form dependent required fields before submit", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "form",
            id: "object-dependent-test",
            title: "Filter",
            fields: [
                {
                    name: "filter",
                    label: "Filter",
                    type: "object_form",
                    value: { status: "rejected" },
                    dependentRequired: { status: ["reason"] },
                    columns: [
                        { name: "status", label: "Status" },
                        { name: "reason", label: "Reason" },
                    ],
                },
            ],
            submitLabel: "Save",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);
        fireEvent.click(screen.getByRole("button", { name: "Save" }));

        expect(onSubmit).not.toHaveBeenCalled();
        expect(screen.getByText(/Filter\.Reason is required when Status is provided/)).toBeTruthy();
    });

    it("blocks table row dependent required fields before submit", () => {
        const onSubmit = vi.fn();
        const view: AgentView = {
            type: "form",
            id: "table-dependent-test",
            title: "Cards",
            fields: [
                {
                    name: "items",
                    label: "Items",
                    type: "array_table",
                    value: [{ card_no: "6222", bank: "" }],
                    dependentRequired: { card_no: ["bank"] },
                    columns: [
                        { name: "card_no", label: "Card No" },
                        { name: "bank", label: "Bank" },
                    ],
                },
            ],
            submitLabel: "Save",
        };

        render(<AgentTaskPanel view={view} onSubmit={onSubmit} theme={lightTheme} />);
        fireEvent.click(screen.getByRole("button", { name: "Save" }));

        expect(onSubmit).not.toHaveBeenCalled();
        expect(screen.getByText(/Items\[1\]\.Bank is required when Card No is provided/)).toBeTruthy();
    });
});
