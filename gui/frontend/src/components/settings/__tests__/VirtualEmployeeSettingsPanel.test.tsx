// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { VirtualEmployeeSettingsPanel } from "../VirtualEmployeeSettingsPanel";
import { EventsOn } from "../../../../wailsjs/runtime";

const RegisterVirtualEmployeeMock = vi.fn();
const UpdateVESettingsMock = vi.fn();
const GetVEStatusMock = vi.fn();
const GetDigitalEmployeeSensitiveQueryPolicyMock = vi.fn();
const SaveDigitalEmployeeSensitiveQueryPolicyMock = vi.fn();

vi.mock("../../../../wailsjs/go/main/App", () => ({
  RegisterVirtualEmployee: (...args: unknown[]) =>
    RegisterVirtualEmployeeMock(...args),
  UpdateVESettings: (...args: unknown[]) => UpdateVESettingsMock(...args),
  GetVEStatus: (...args: unknown[]) => GetVEStatusMock(...args),
  GetDigitalEmployeeSensitiveQueryPolicy: (...args: unknown[]) =>
    GetDigitalEmployeeSensitiveQueryPolicyMock(...args),
  SaveDigitalEmployeeSensitiveQueryPolicy: (...args: unknown[]) =>
    SaveDigitalEmployeeSensitiveQueryPolicyMock(...args),
}));

vi.mock("../../../../wailsjs/runtime", () => ({
  EventsOn: vi.fn(() => vi.fn()),
  EventsOff: vi.fn(),
}));

beforeEach(() => {
  (EventsOn as unknown as ReturnType<typeof vi.fn>).mockImplementation(() =>
    vi.fn(),
  );
  GetVEStatusMock.mockResolvedValue({ registered: false });
  GetDigitalEmployeeSensitiveQueryPolicyMock.mockResolvedValue("confirm");
  SaveDigitalEmployeeSensitiveQueryPolicyMock.mockResolvedValue(undefined);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("VirtualEmployeeSettingsPanel", () => {
  it("does not render when remoteMachineId is empty", () => {
    const { container } = render(
      <VirtualEmployeeSettingsPanel remoteMachineId="" />,
    );
    expect(
      container.querySelector('[data-testid="ve-settings-panel"]'),
    ).toBeNull();
  });

  it("renders digital employee settings text when remoteMachineId is present", () => {
    render(<VirtualEmployeeSettingsPanel remoteMachineId="machine-123" />);
    expect(screen.getByTestId("ve-settings-panel")).toBeTruthy();
    expect(
      screen.getByText("\u6570\u5b57\u5458\u5de5\u8bbe\u7f6e"),
    ).toBeTruthy();
    expect(screen.queryByText(/\u865a\u62df\u5458\u5de5/)).toBeNull();
  });

  it("validates required fields with readable Chinese copy", () => {
    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    fireEvent.click(screen.getByTestId("ve-submit-btn"));
    expect(screen.getByTestId("name-error").textContent).toBe(
      "\u540d\u79f0\u4e0d\u80fd\u4e3a\u7a7a",
    );
    expect(screen.getByTestId("skill-error").textContent).toBe(
      "\u6280\u80fd\u63cf\u8ff0\u4e0d\u80fd\u4e3a\u7a7a",
    );
    expect(screen.getByTestId("policy-error").textContent).toBe(
      "\u8bf7\u9009\u62e9\u8bbf\u95ee\u7b56\u7565",
    );
    expect(RegisterVirtualEmployeeMock).not.toHaveBeenCalled();
  });

  it("shows whitelist and blacklist editors for matching access policies", () => {
    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    fireEvent.change(screen.getByLabelText("\u8bbf\u95ee\u7b56\u7565"), {
      target: { value: "whitelist" },
    });
    expect(screen.getByText("\u767d\u540d\u5355")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("\u8bbf\u95ee\u7b56\u7565"), {
      target: { value: "blacklist" },
    });
    expect(screen.getByText("\u9ed1\u540d\u5355")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("\u8bbf\u95ee\u7b56\u7565"), {
      target: { value: "public" },
    });
    expect(screen.queryByTestId("list-editor")).toBeNull();
  });

  it("registers a digital employee with selected policy and list values", async () => {
    RegisterVirtualEmployeeMock.mockResolvedValue(undefined);
    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    fireEvent.change(screen.getByLabelText("\u540d\u79f0"), {
      target: { value: "My Digital Employee" },
    });
    fireEvent.change(screen.getByLabelText("\u6280\u80fd\u63cf\u8ff0"), {
      target: { value: "AI assistant" },
    });
    fireEvent.change(screen.getByLabelText("\u8bbf\u95ee\u7b56\u7565"), {
      target: { value: "whitelist" },
    });
    fireEvent.change(screen.getByTestId("list-input"), {
      target: { value: "user-a" },
    });
    fireEvent.click(screen.getByTestId("list-add-btn"));
    fireEvent.click(screen.getByTestId("ve-submit-btn"));
    await waitFor(() => {
      expect(RegisterVirtualEmployeeMock).toHaveBeenCalledWith(
        "My Digital Employee",
        "AI assistant",
        "whitelist",
        ["user-a"],
      );
    });
  });

  it("loads existing whitelist and preserves it when updating", async () => {
    GetVEStatusMock.mockResolvedValue({
      registered: true,
      employee: {
        name: "Existing Name",
        skill_description: "existing skill",
        access_policy: "whitelist",
        whitelist: ["user-a", "user-b"],
        blacklist: ["blocked-user"],
        status: "active",
      },
    });
    UpdateVESettingsMock.mockResolvedValue(undefined);
    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

    await waitFor(() => expect(screen.getByText("user-a")).toBeTruthy());
    expect(screen.getByText("user-b")).toBeTruthy();

    fireEvent.click(screen.getByTestId("ve-submit-btn"));
    await waitFor(() => {
      expect(UpdateVESettingsMock).toHaveBeenCalledWith(
        "Existing Name",
        "existing skill",
        "whitelist",
        ["user-a", "user-b"],
      );
    });
  });

  it("updates settings when already registered", async () => {
    GetVEStatusMock.mockResolvedValue({
      registered: true,
      employee: {
        name: "Old Name",
        skill_description: "old skill",
        access_policy: "public",
        status: "active",
      },
    });
    UpdateVESettingsMock.mockResolvedValue(undefined);
    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    await waitFor(() =>
      expect(screen.getByTestId("ve-status-badge").textContent).toBe(
        "\u5df2\u6fc0\u6d3b",
      ),
    );
    fireEvent.change(screen.getByLabelText("\u540d\u79f0"), {
      target: { value: "New Name" },
    });
    fireEvent.click(screen.getByTestId("ve-submit-btn"));
    await waitFor(() => {
      expect(UpdateVESettingsMock).toHaveBeenCalledWith(
        "New Name",
        "old skill",
        "public",
        [],
      );
    });
  });

  it("clears stale registration fields when backend reports unregistered", async () => {
    GetVEStatusMock.mockResolvedValueOnce({
      registered: true,
      employee: {
        name: "Old Name",
        skill_description: "old skill",
        access_policy: "public",
        status: "active",
      },
    }).mockResolvedValueOnce({ registered: false });

    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    await waitFor(() =>
      expect((screen.getByLabelText("\u540d\u79f0") as HTMLInputElement).value).toBe("Old Name"),
    );

    fireEvent.change(screen.getByLabelText("\u540d\u79f0"), {
      target: { value: "Edited stale name" },
    });
    fireEvent.click(screen.getByTestId("ve-submit-btn"));

    await waitFor(() =>
      expect((screen.getByLabelText("\u540d\u79f0") as HTMLInputElement).value).toBe(""),
    );
    expect((screen.getByLabelText("\u8bbf\u95ee\u7b56\u7565") as HTMLSelectElement).value).toBe("");
  });

  it("clears stale registration fields when status refresh fails", async () => {
    GetVEStatusMock.mockResolvedValueOnce({
      registered: true,
      employee: {
        name: "Old Name",
        skill_description: "old skill",
        access_policy: "public",
        status: "active",
      },
    }).mockRejectedValueOnce(new Error("hub unavailable"));

    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    await waitFor(() =>
      expect((screen.getByLabelText("\u540d\u79f0") as HTMLInputElement).value).toBe("Old Name"),
    );

    fireEvent.click(screen.getByTestId("ve-submit-btn"));

    await waitFor(() =>
      expect((screen.getByLabelText("\u540d\u79f0") as HTMLInputElement).value).toBe(""),
    );
    expect(screen.queryByTestId("ve-status-badge")).toBeNull();
  });

  it("refreshes status when disabled event arrives", async () => {
    const handlers = new Map<string, () => void>();
    (EventsOn as unknown as ReturnType<typeof vi.fn>).mockImplementation(
      (event: string, handler: () => void) => {
        handlers.set(event, handler);
        return vi.fn();
      },
    );
    GetVEStatusMock.mockResolvedValueOnce({
      registered: true,
      employee: {
        name: "Old Name",
        skill_description: "old skill",
        access_policy: "public",
        status: "active",
      },
    }).mockResolvedValueOnce({
      registered: true,
      employee: {
        name: "Old Name",
        skill_description: "old skill",
        access_policy: "public",
        status: "disabled",
      },
    });
    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    await waitFor(() =>
      expect(screen.getByTestId("ve-status-badge").textContent).toBe(
        "\u5df2\u6fc0\u6d3b",
      ),
    );
    handlers.get("ve:disabled")?.();
    await waitFor(() =>
      expect(screen.getByTestId("ve-status-badge").textContent).toBe(
        "\u5df2\u7981\u7528",
      ),
    );
  });
  it("keeps sensitive query policy editable while registration is pending", async () => {
    GetVEStatusMock.mockResolvedValue({
      registered: true,
      employee: {
        name: "Pending Name",
        skill_description: "pending skill",
        access_policy: "public",
        status: "pending",
      },
    });
    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    await waitFor(() =>
      expect(screen.getByTestId("ve-status-badge").textContent).toBe(
        "\u5ba1\u6838\u4e2d",
      ),
    );
    const sensitiveSelect = screen.getByLabelText(
      "\u5bc6\u7801\u6216\u654f\u611f\u4fe1\u606f\u67e5\u8be2",
    ) as HTMLSelectElement;
    expect(sensitiveSelect.disabled).toBe(false);
  });

  it("loads and saves sensitive query policy", async () => {
    GetDigitalEmployeeSensitiveQueryPolicyMock.mockResolvedValue("deny");
    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    const select = (await screen.findByLabelText(
      "\u5bc6\u7801\u6216\u654f\u611f\u4fe1\u606f\u67e5\u8be2",
    )) as HTMLSelectElement;
    await waitFor(() => expect(select.value).toBe("deny"));
    fireEvent.change(select, { target: { value: "allow" } });
    await waitFor(() =>
      expect(SaveDigitalEmployeeSensitiveQueryPolicyMock).toHaveBeenCalledWith(
        "allow",
      ),
    );
  });

  it("normalizes sensitive query policy loaded from backend", async () => {
    GetDigitalEmployeeSensitiveQueryPolicyMock.mockResolvedValue(" ALLOW ");
    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    const select = (await screen.findByLabelText(
      "\u5bc6\u7801\u6216\u654f\u611f\u4fe1\u606f\u67e5\u8be2",
    )) as HTMLSelectElement;
    await waitFor(() => expect(select.value).toBe("allow"));
  });
});
