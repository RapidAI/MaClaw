// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { VEApprovalCapabilitySection } from "../VEApprovalCapabilitySection";

const GetVEApprovalConfigMock = vi.fn();
const SaveVEApprovalConfigMock = vi.fn();

vi.mock("../../../../wailsjs/go/main/App", () => ({
  GetVEApprovalConfig: (...args: unknown[]) => GetVEApprovalConfigMock(...args),
  SaveVEApprovalConfig: (...args: unknown[]) => SaveVEApprovalConfigMock(...args),
}));

const defaultConfig = {
  enabled: false,
  acl: { mode: "whitelist", departments: [], roles: [], skills: [], entities: [] },
  rules: { auto_reject: [], auto_approve: [], require_human: [] },
  max_queue_size: 50,
  timeout_hours: 24,
  daily_quota: 100,
  fallback_approver: "",
};

beforeEach(() => {
  GetVEApprovalConfigMock.mockResolvedValue({ ...defaultConfig });
  SaveVEApprovalConfigMock.mockResolvedValue(undefined);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("VEApprovalCapabilitySection", () => {
  it("renders the section with toggle defaulting to disabled", async () => {
    render(<VEApprovalCapabilitySection />);
    await waitFor(() => {
      expect(screen.getByTestId("ve-approval-section")).toBeTruthy();
    });
    const toggle = screen.getByTestId("ve-approval-enabled-toggle") as HTMLInputElement;
    expect(toggle.checked).toBe(false);
  });

  it("shows configuration fields when enabled", async () => {
    GetVEApprovalConfigMock.mockResolvedValue({ ...defaultConfig, enabled: true });
    render(<VEApprovalCapabilitySection />);
    await waitFor(() => {
      expect(screen.getByTestId("ve-approval-acl-mode")).toBeTruthy();
    });
    expect(screen.getByTestId("ve-approval-max-queue")).toBeTruthy();
    expect(screen.getByTestId("ve-approval-timeout")).toBeTruthy();
    expect(screen.getByTestId("ve-approval-daily-quota")).toBeTruthy();
    expect(screen.getByTestId("ve-approval-fallback")).toBeTruthy();
  });

  it("hides configuration fields when disabled", async () => {
    render(<VEApprovalCapabilitySection />);
    await waitFor(() => {
      expect(screen.getByTestId("ve-approval-enabled-toggle")).toBeTruthy();
    });
    expect(screen.queryByTestId("ve-approval-acl-mode")).toBeNull();
    expect(screen.queryByTestId("ve-approval-max-queue")).toBeNull();
  });

  it("toggles enabled state and shows/hides config", async () => {
    render(<VEApprovalCapabilitySection />);
    await waitFor(() => {
      expect(screen.getByTestId("ve-approval-enabled-toggle")).toBeTruthy();
    });
    const toggle = screen.getByTestId("ve-approval-enabled-toggle") as HTMLInputElement;
    expect(toggle.checked).toBe(false);
    expect(screen.queryByTestId("ve-approval-acl-mode")).toBeNull();

    fireEvent.click(toggle);
    expect(toggle.checked).toBe(true);
    expect(screen.getByTestId("ve-approval-acl-mode")).toBeTruthy();
  });

  it("displays ACL mode selector with whitelist/blacklist options", async () => {
    GetVEApprovalConfigMock.mockResolvedValue({ ...defaultConfig, enabled: true });
    render(<VEApprovalCapabilitySection />);
    await waitFor(() => {
      expect(screen.getByTestId("ve-approval-acl-mode")).toBeTruthy();
    });
    const select = screen.getByTestId("ve-approval-acl-mode") as HTMLSelectElement;
    expect(select.value).toBe("whitelist");
    expect(select.options.length).toBe(2);
  });

  it("displays ACL text areas for departments, roles, skills, entities", async () => {
    GetVEApprovalConfigMock.mockResolvedValue({
      ...defaultConfig,
      enabled: true,
      acl: {
        mode: "whitelist",
        departments: ["Engineering", "Sales"],
        roles: ["Manager"],
        skills: ["Python"],
        entities: ["user_001"],
      },
    });
    render(<VEApprovalCapabilitySection />);
    await waitFor(() => {
      expect(screen.getByTestId("ve-approval-acl-departments")).toBeTruthy();
    });
    const deptArea = screen.getByTestId("ve-approval-acl-departments") as HTMLTextAreaElement;
    expect(deptArea.value).toBe("Engineering\nSales");
    const rolesArea = screen.getByTestId("ve-approval-acl-roles") as HTMLTextAreaElement;
    expect(rolesArea.value).toBe("Manager");
    const skillsArea = screen.getByTestId("ve-approval-acl-skills") as HTMLTextAreaElement;
    expect(skillsArea.value).toBe("Python");
    const entitiesArea = screen.getByTestId("ve-approval-acl-entities") as HTMLTextAreaElement;
    expect(entitiesArea.value).toBe("user_001");
  });

  it("displays operational limits with correct default values", async () => {
    GetVEApprovalConfigMock.mockResolvedValue({ ...defaultConfig, enabled: true });
    render(<VEApprovalCapabilitySection />);
    await waitFor(() => {
      expect(screen.getByTestId("ve-approval-max-queue")).toBeTruthy();
    });
    const queueInput = screen.getByTestId("ve-approval-max-queue") as HTMLInputElement;
    expect(queueInput.value).toBe("50");
    const timeoutInput = screen.getByTestId("ve-approval-timeout") as HTMLInputElement;
    expect(timeoutInput.value).toBe("24");
    const quotaInput = screen.getByTestId("ve-approval-daily-quota") as HTMLInputElement;
    expect(quotaInput.value).toBe("100");
  });

  it("displays fallback approver input", async () => {
    GetVEApprovalConfigMock.mockResolvedValue({
      ...defaultConfig,
      enabled: true,
      fallback_approver: "ve_backup_001",
    });
    render(<VEApprovalCapabilitySection />);
    await waitFor(() => {
      expect(screen.getByTestId("ve-approval-fallback")).toBeTruthy();
    });
    const fallbackInput = screen.getByTestId("ve-approval-fallback") as HTMLInputElement;
    expect(fallbackInput.value).toBe("ve_backup_001");
  });

  it("saves configuration via SaveVEApprovalConfig", async () => {
    GetVEApprovalConfigMock.mockResolvedValue({ ...defaultConfig, enabled: true });
    render(<VEApprovalCapabilitySection />);
    await waitFor(() => {
      expect(screen.getByTestId("ve-approval-save-btn")).toBeTruthy();
    });

    // Change max queue size
    const queueInput = screen.getByTestId("ve-approval-max-queue") as HTMLInputElement;
    fireEvent.change(queueInput, { target: { value: "100" } });

    // Click save
    fireEvent.click(screen.getByTestId("ve-approval-save-btn"));
    await waitFor(() => {
      expect(SaveVEApprovalConfigMock).toHaveBeenCalledTimes(1);
    });
    const savedConfig = SaveVEApprovalConfigMock.mock.calls[0][0];
    expect(savedConfig.enabled).toBe(true);
    expect(savedConfig.max_queue_size).toBe(100);
  });

  it("shows save error when SaveVEApprovalConfig fails", async () => {
    GetVEApprovalConfigMock.mockResolvedValue({ ...defaultConfig, enabled: true });
    SaveVEApprovalConfigMock.mockRejectedValue(new Error("validation failed: max_queue_size must be between 1 and 1000"));
    render(<VEApprovalCapabilitySection />);
    await waitFor(() => {
      expect(screen.getByTestId("ve-approval-save-btn")).toBeTruthy();
    });
    fireEvent.click(screen.getByTestId("ve-approval-save-btn"));
    await waitFor(() => {
      expect(screen.getByTestId("ve-approval-save-error")).toBeTruthy();
    });
  });

  it("saves disabled state correctly", async () => {
    render(<VEApprovalCapabilitySection />);
    await waitFor(() => {
      expect(screen.getByTestId("ve-approval-save-btn")).toBeTruthy();
    });
    fireEvent.click(screen.getByTestId("ve-approval-save-btn"));
    await waitFor(() => {
      expect(SaveVEApprovalConfigMock).toHaveBeenCalledTimes(1);
    });
    const savedConfig = SaveVEApprovalConfigMock.mock.calls[0][0];
    expect(savedConfig.enabled).toBe(false);
  });
});
