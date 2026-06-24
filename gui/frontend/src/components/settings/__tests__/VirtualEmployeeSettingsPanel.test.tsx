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
import { BrowserOpenURL, EventsOn } from "../../../../wailsjs/runtime";

const RegisterVirtualEmployeeMock = vi.fn();
const UpdateVESettingsMock = vi.fn();
const GetVEStatusMock = vi.fn();
const GetDigitalEmployeeSensitiveQueryPolicyMock = vi.fn();
const SaveDigitalEmployeeSensitiveQueryPolicyMock = vi.fn();
const SelectVEAllowedDirectoryMock = vi.fn();
const GetVEAllowedDirectoriesMock = vi.fn();
const SetVEAllowedDirectoriesMock = vi.fn();
const GetVEApprovalConfigMock = vi.fn();
const SaveVEApprovalConfigMock = vi.fn();
const LoadConfigMock = vi.fn();
const SetAuthRequestSoundConfigMock = vi.fn();
const pngSignatureBytes = new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
const avatarInvalidText = "\u8bf7\u4e0a\u4f20 PNG\u3001JPG/JPEG \u6216 WebP \u56fe\u7247\uff0c\u5927\u5c0f\u4e0d\u8d85\u8fc7 5 MB\u3002";

vi.mock("../../../../wailsjs/go/main/App", () => ({
  RegisterVirtualEmployee: (...args: unknown[]) =>
    RegisterVirtualEmployeeMock(...args),
  UpdateVESettings: (...args: unknown[]) => UpdateVESettingsMock(...args),
  GetVEStatus: (...args: unknown[]) => GetVEStatusMock(...args),
  GetDigitalEmployeeSensitiveQueryPolicy: (...args: unknown[]) =>
    GetDigitalEmployeeSensitiveQueryPolicyMock(...args),
  SaveDigitalEmployeeSensitiveQueryPolicy: (...args: unknown[]) =>
    SaveDigitalEmployeeSensitiveQueryPolicyMock(...args),
  SelectVEAllowedDirectory: (...args: unknown[]) =>
    SelectVEAllowedDirectoryMock(...args),
  GetVEAllowedDirectories: (...args: unknown[]) =>
    GetVEAllowedDirectoriesMock(...args),
  SetVEAllowedDirectories: (...args: unknown[]) =>
    SetVEAllowedDirectoriesMock(...args),
  LoadConfig: (...args: unknown[]) => LoadConfigMock(...args),
  SetAuthRequestSoundConfig: (...args: unknown[]) => SetAuthRequestSoundConfigMock(...args),
  GetVEApprovalConfig: (...args: unknown[]) =>
    GetVEApprovalConfigMock(...args),
  SaveVEApprovalConfig: (...args: unknown[]) =>
    SaveVEApprovalConfigMock(...args),
}));

vi.mock("../../../../wailsjs/runtime", () => ({
  EventsOn: vi.fn(() => vi.fn()),
  EventsOff: vi.fn(),
  BrowserOpenURL: vi.fn(),
}));

beforeEach(() => {
  (EventsOn as unknown as ReturnType<typeof vi.fn>).mockImplementation(() =>
    vi.fn(),
  );
  GetVEStatusMock.mockResolvedValue({ registered: false });
  GetDigitalEmployeeSensitiveQueryPolicyMock.mockResolvedValue("confirm");
  SaveDigitalEmployeeSensitiveQueryPolicyMock.mockResolvedValue(undefined);
  GetVEAllowedDirectoriesMock.mockResolvedValue([]);
  SelectVEAllowedDirectoryMock.mockResolvedValue("");
  SetVEAllowedDirectoriesMock.mockResolvedValue(undefined);
  GetVEApprovalConfigMock.mockResolvedValue({
    enabled: false,
    acl: { mode: "whitelist", departments: [], roles: [], skills: [], entities: [] },
    rules: { auto_reject: [], auto_approve: [], require_human: [] },
    max_queue_size: 50,
    timeout_hours: 24,
    daily_quota: 100,
    fallback_approver: "",
  });
  SaveVEApprovalConfigMock.mockResolvedValue(undefined);
  LoadConfigMock.mockResolvedValue({
    remote_hub_url: "http://hub.local",
    remote_machine_id: "machine-123",
    remote_machine_token: "token 123",
    group_discussion: {
      auth_request_sound_preset: "classic",
      auth_request_sound_muted: false,
    },
  });
  SetAuthRequestSoundConfigMock.mockResolvedValue(undefined);
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

  it("exposes the main settings sections as named regions", () => {
    render(<VirtualEmployeeSettingsPanel remoteMachineId="machine-123" />);

    expect(screen.getByRole("region", { name: "\u8eab\u4efd\u4fe1\u606f" })).toBeTruthy();
    expect(screen.getByRole("region", { name: "\u8bbf\u95ee\u4e0e\u8bf7\u6c42" })).toBeTruthy();
    expect(screen.getByRole("region", { name: "\u6587\u4ef6\u8bbf\u95ee" })).toBeTruthy();
  });

  it("places register above approval capability and approval save at the bottom", async () => {
    render(<VirtualEmployeeSettingsPanel remoteMachineId="machine-123" />);

    const submit = screen.getByTestId("ve-submit-btn");
    const approval = await screen.findByTestId("ve-approval-section");
    const workflow = screen.getByTestId("ve-approval-workflow-design-section");
    const save = screen.getByTestId("ve-approval-save-btn");

    expect(Boolean(submit.compareDocumentPosition(approval) & Node.DOCUMENT_POSITION_FOLLOWING)).toBe(true);
    expect(Boolean(approval.compareDocumentPosition(workflow) & Node.DOCUMENT_POSITION_FOLLOWING)).toBe(true);
    expect(Boolean(workflow.compareDocumentPosition(save) & Node.DOCUMENT_POSITION_FOLLOWING)).toBe(true);
  });

  it("opens workflow designer with machine authorization from local config", async () => {
    render(<VirtualEmployeeSettingsPanel remoteMachineId="machine-123" />);

    const button = await screen.findByTestId("ve-approval-workflow-design-btn");
    expect(button.getAttribute("type")).toBe("button");
    fireEvent.click(button);

    await waitFor(() => {
      expect(BrowserOpenURL).toHaveBeenCalledWith(
        "http://hub.local/approval_workflow#machine_id=machine-123&token=token+123",
      );
    });
  });

  it("falls back to panel machine id when config omits it", async () => {
    LoadConfigMock.mockResolvedValue({
      remote_hub_url: "http://hub.local/",
      remote_machine_token: "token-abc",
    });
    render(<VirtualEmployeeSettingsPanel remoteMachineId="prop-machine" />);

    fireEvent.click(await screen.findByTestId("ve-approval-workflow-design-btn"));

    await waitFor(() => {
      expect(BrowserOpenURL).toHaveBeenCalledWith(
        "http://hub.local/approval_workflow#machine_id=prop-machine&token=token-abc",
      );
    });
  });

  it("trims workflow designer auth config before opening", async () => {
    LoadConfigMock.mockResolvedValue({
      remote_hub_url: "  http://hub.local///  ",
      remote_machine_id: " machine-trim ",
      remote_machine_token: " token-trim ",
    });
    render(<VirtualEmployeeSettingsPanel remoteMachineId="prop-machine" />);

    fireEvent.click(await screen.findByTestId("ve-approval-workflow-design-btn"));

    await waitFor(() => {
      expect(BrowserOpenURL).toHaveBeenCalledWith(
        "http://hub.local/approval_workflow#machine_id=machine-trim&token=token-trim",
      );
    });
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
    expect(screen.getByLabelText("\u767d\u540d\u5355")).toBe(screen.getByTestId("list-input"));
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
        "",
      );
    });
  });

  it("trims and deduplicates access list entries case-insensitively", async () => {
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
      target: { value: " User-A " },
    });
    fireEvent.click(screen.getByTestId("list-add-btn"));
    fireEvent.change(screen.getByTestId("list-input"), {
      target: { value: "user-a" },
    });
    fireEvent.click(screen.getByTestId("list-add-btn"));

    expect(screen.getAllByText("User-A")).toHaveLength(1);
    fireEvent.click(screen.getByTestId("ve-submit-btn"));
    await waitFor(() => {
      expect(RegisterVirtualEmployeeMock).toHaveBeenCalledWith(
        "My Digital Employee",
        "AI assistant",
        "whitelist",
        ["User-A"],
        "",
      );
    });
  });

  it("crops uploaded avatar only when saving", async () => {
    const originalImage = globalThis.Image;
    class MockImage {
      width = 640;
      height = 320;
      onload: (() => void) | null = null;
      onerror: (() => void) | null = null;
      set src(_value: string) {
        window.setTimeout(() => this.onload?.(), 0);
      }
    }
    (globalThis as any).Image = MockImage;
    const drawImage = vi.fn();
    const getContextSpy = vi
      .spyOn(HTMLCanvasElement.prototype, "getContext")
      .mockReturnValue({ clearRect: vi.fn(), drawImage } as any);
    const toDataURLSpy = vi
      .spyOn(HTMLCanvasElement.prototype, "toDataURL")
      .mockReturnValue("data:image/jpeg;base64,Q1JPUA==");
    RegisterVirtualEmployeeMock.mockResolvedValue(undefined);

    try {
      const { container } = render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
      await waitFor(() => expect(screen.getByTestId("ve-dirs-empty-hint")).toBeTruthy());
      fireEvent.change(screen.getByLabelText("\u540d\u79f0"), {
        target: { value: "My Digital Employee" },
      });
      fireEvent.change(screen.getByLabelText("\u6280\u80fd\u63cf\u8ff0"), {
        target: { value: "AI assistant" },
      });
      fireEvent.change(screen.getByLabelText("\u8bbf\u95ee\u7b56\u7565"), {
        target: { value: "public" },
      });
      const file = new File([pngSignatureBytes], "avatar.png", { type: "image/png" });
      fireEvent.change(screen.getByTestId("ve-avatar-file-input"), {
        target: { files: [file] },
      });

      await waitFor(() => {
        const preview = container.querySelector(".ve-avatar-editor__image") as HTMLImageElement | null;
        expect(preview?.src).toContain("data:image/png;base64");
      });
      expect(toDataURLSpy).not.toHaveBeenCalled();

      fireEvent.click(screen.getByTestId("ve-submit-btn"));
      await waitFor(() => {
        expect(RegisterVirtualEmployeeMock).toHaveBeenCalledWith(
          "My Digital Employee",
          "AI assistant",
          "public",
          [],
          "data:image/jpeg;base64,Q1JPUA==",
        );
      });
      expect(toDataURLSpy).toHaveBeenCalledWith("image/jpeg", 0.86);
      expect(drawImage).toHaveBeenCalled();
    } finally {
      (globalThis as any).Image = originalImage;
      getContextSpy.mockRestore();
      toDataURLSpy.mockRestore();
    }
  });

  it("keeps saved cropped avatar when status refresh has not returned it yet", async () => {
    const originalImage = globalThis.Image;
    class MockImage {
      width = 640;
      height = 320;
      onload: (() => void) | null = null;
      onerror: (() => void) | null = null;
      set src(_value: string) {
        window.setTimeout(() => this.onload?.(), 0);
      }
    }
    (globalThis as any).Image = MockImage;
    const getContextSpy = vi
      .spyOn(HTMLCanvasElement.prototype, "getContext")
      .mockReturnValue({ clearRect: vi.fn(), drawImage: vi.fn() } as any);
    const toDataURLSpy = vi
      .spyOn(HTMLCanvasElement.prototype, "toDataURL")
      .mockReturnValue("data:image/jpeg;base64,/9j/Q1JPUA==");
    GetVEStatusMock
      .mockResolvedValueOnce({ registered: false })
      .mockResolvedValueOnce({
        registered: true,
        employee: {
          name: "My Digital Employee",
          skill_description: "AI assistant",
          access_policy: "public",
          status: "active",
        },
      });
    RegisterVirtualEmployeeMock.mockResolvedValue(undefined);

    try {
      const { container } = render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
      await waitFor(() => expect(screen.getByTestId("ve-dirs-empty-hint")).toBeTruthy());
      fireEvent.change(screen.getByLabelText("\u540d\u79f0"), {
        target: { value: "My Digital Employee" },
      });
      fireEvent.change(screen.getByLabelText("\u6280\u80fd\u63cf\u8ff0"), {
        target: { value: "AI assistant" },
      });
      fireEvent.change(screen.getByLabelText("\u8bbf\u95ee\u7b56\u7565"), {
        target: { value: "public" },
      });
      fireEvent.change(screen.getByTestId("ve-avatar-file-input"), {
        target: { files: [new File([pngSignatureBytes], "avatar.png", { type: "image/png" })] },
      });

      await waitFor(() => {
        const preview = container.querySelector(".ve-avatar-editor__image") as HTMLImageElement | null;
        expect(preview?.src).toContain("data:image/png;base64");
      });
      fireEvent.click(screen.getByTestId("ve-submit-btn"));

      await waitFor(() => {
        const preview = container.querySelector(".ve-avatar-editor__image") as HTMLImageElement | null;
        expect(preview?.src).toContain("data:image/jpeg;base64,/9j/Q1JPUA==");
      });
      expect(toDataURLSpy).toHaveBeenCalledWith("image/jpeg", 0.86);
    } finally {
      (globalThis as any).Image = originalImage;
      getContextSpy.mockRestore();
      toDataURLSpy.mockRestore();
    }
  });

  it("keeps edited avatar state when saving cropped avatar fails", async () => {
    const originalImage = globalThis.Image;
    class MockImage {
      width = 640;
      height = 320;
      onload: (() => void) | null = null;
      onerror: (() => void) | null = null;
      set src(_value: string) {
        window.setTimeout(() => this.onload?.(), 0);
      }
    }
    (globalThis as any).Image = MockImage;
    const getContextSpy = vi
      .spyOn(HTMLCanvasElement.prototype, "getContext")
      .mockReturnValue({ clearRect: vi.fn(), drawImage: vi.fn() } as any);
    const toDataURLSpy = vi
      .spyOn(HTMLCanvasElement.prototype, "toDataURL")
      .mockReturnValue("data:image/jpeg;base64,Q1JPUA==");
    RegisterVirtualEmployeeMock.mockRejectedValue(new Error("hub unavailable"));

    try {
      const { container } = render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
      await waitFor(() => expect(screen.getByTestId("ve-dirs-empty-hint")).toBeTruthy());
      fireEvent.change(screen.getByLabelText("\u540d\u79f0"), {
        target: { value: "My Digital Employee" },
      });
      fireEvent.change(screen.getByLabelText("\u6280\u80fd\u63cf\u8ff0"), {
        target: { value: "AI assistant" },
      });
      fireEvent.change(screen.getByLabelText("\u8bbf\u95ee\u7b56\u7565"), {
        target: { value: "public" },
      });
      fireEvent.change(screen.getByTestId("ve-avatar-file-input"), {
        target: { files: [new File([pngSignatureBytes], "avatar.png", { type: "image/png" })] },
      });

      const preview = await waitFor(() => {
        const img = container.querySelector(".ve-avatar-editor__image") as HTMLImageElement | null;
        expect(img?.src).toContain("data:image/png;base64");
        return img as HTMLImageElement;
      });
      fireEvent.click(screen.getByTestId("ve-submit-btn"));

      await waitFor(() => expect(RegisterVirtualEmployeeMock).toHaveBeenCalled());
      expect(toDataURLSpy).toHaveBeenCalledWith("image/jpeg", 0.86);
      expect(preview.src).toContain("data:image/png;base64");
      expect(preview.src).not.toContain("Q1JPUA==");
    } finally {
      (globalThis as any).Image = originalImage;
      getContextSpy.mockRestore();
      toDataURLSpy.mockRestore();
    }
  });

  it("rejects unsupported avatar file types before preview", async () => {
    const { container } = render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    await waitFor(() => expect(screen.getByTestId("ve-dirs-empty-hint")).toBeTruthy());
    const input = screen.getByTestId("ve-avatar-file-input") as HTMLInputElement;
    expect(input.accept).toBe("image/png,image/jpeg,image/webp");

    fireEvent.change(input, {
      target: { files: [new File(["<svg />"], "avatar.svg", { type: "image/svg+xml" })] },
    });

    expect(container.querySelector(".ve-avatar-editor__image")).toBeNull();
    expect(screen.getByRole("alert").textContent).toBe(avatarInvalidText);
  });

  it("rejects spoofed avatar file content before preview", async () => {
    const { container } = render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    await waitFor(() => expect(screen.getByTestId("ve-dirs-empty-hint")).toBeTruthy());

    fireEvent.change(screen.getByTestId("ve-avatar-file-input"), {
      target: { files: [new File(["not a png"], "avatar.png", { type: "image/png" })] },
    });

    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toBe(avatarInvalidText);
    });
    expect(container.querySelector(".ve-avatar-editor__image")).toBeNull();
  });

  it("resizes large valid source photos locally before preview", async () => {
    const originalImage = globalThis.Image;
    class MockImage {
      width = 4000;
      height = 2000;
      onload: (() => void) | null = null;
      onerror: (() => void) | null = null;
      set src(_value: string) {
        window.setTimeout(() => this.onload?.(), 0);
      }
    }
    (globalThis as any).Image = MockImage;
    const drawImage = vi.fn();
    const getContextSpy = vi
      .spyOn(HTMLCanvasElement.prototype, "getContext")
      .mockReturnValue({ clearRect: vi.fn(), drawImage } as any);
    const toDataURLSpy = vi
      .spyOn(HTMLCanvasElement.prototype, "toDataURL")
      .mockReturnValue("data:image/jpeg;base64,/9j/U01BTEw=");
    const largeValidPng = new Uint8Array(1100 * 1024);
    largeValidPng.set(pngSignatureBytes, 0);

    try {
      const { container } = render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
      await waitFor(() => expect(screen.getByTestId("ve-dirs-empty-hint")).toBeTruthy());

      fireEvent.change(screen.getByTestId("ve-avatar-file-input"), {
        target: { files: [new File([largeValidPng], "large.png", { type: "image/png" })] },
      });

      await waitFor(() => {
        const preview = container.querySelector(".ve-avatar-editor__image") as HTMLImageElement | null;
        expect(preview?.src).toContain("data:image/jpeg;base64,/9j/U01BTEw=");
      });
      expect(toDataURLSpy).toHaveBeenCalledWith("image/jpeg", 0.9);
      expect(drawImage).toHaveBeenCalledWith(expect.any(Object), 0, 0, 1024, 512);
      expect(screen.queryByRole("alert")).toBeNull();
    } finally {
      (globalThis as any).Image = originalImage;
      getContextSpy.mockRestore();
      toDataURLSpy.mockRestore();
    }
  });

  it("resizes high-resolution source photos even when the file is already small", async () => {
    const originalImage = globalThis.Image;
    class MockImage {
      width = 4096;
      height = 2048;
      onload: (() => void) | null = null;
      onerror: (() => void) | null = null;
      set src(_value: string) {
        window.setTimeout(() => this.onload?.(), 0);
      }
    }
    (globalThis as any).Image = MockImage;
    const drawImage = vi.fn();
    const getContextSpy = vi
      .spyOn(HTMLCanvasElement.prototype, "getContext")
      .mockReturnValue({ clearRect: vi.fn(), drawImage } as any);
    const toDataURLSpy = vi
      .spyOn(HTMLCanvasElement.prototype, "toDataURL")
      .mockReturnValue("data:image/jpeg;base64,/9j/U01BTEw=");

    try {
      const { container } = render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
      await waitFor(() => expect(screen.getByTestId("ve-dirs-empty-hint")).toBeTruthy());

      fireEvent.change(screen.getByTestId("ve-avatar-file-input"), {
        target: { files: [new File([pngSignatureBytes], "huge-dimensions.png", { type: "image/png" })] },
      });

      await waitFor(() => {
        const preview = container.querySelector(".ve-avatar-editor__image") as HTMLImageElement | null;
        expect(preview?.src).toContain("data:image/jpeg;base64,/9j/U01BTEw=");
      });
      expect(toDataURLSpy).toHaveBeenCalledWith("image/jpeg", 0.9);
      expect(drawImage).toHaveBeenCalledWith(expect.any(Object), 0, 0, 1024, 512);
    } finally {
      (globalThis as any).Image = originalImage;
      getContextSpy.mockRestore();
      toDataURLSpy.mockRestore();
    }
  });

  it("ignores stale large-avatar resize after a newer upload", async () => {
    const originalImage = globalThis.Image;
    const images: Array<{ onload: (() => void) | null }> = [];
    class MockImage {
      width: number;
      height: number;
      onload: (() => void) | null = null;
      onerror: (() => void) | null = null;
      constructor() {
        images.push(this);
        this.width = images.length === 1 ? 4000 : 640;
        this.height = images.length === 1 ? 2000 : 320;
      }
      set src(_value: string) {}
    }
    (globalThis as any).Image = MockImage;
    const getContextSpy = vi
      .spyOn(HTMLCanvasElement.prototype, "getContext")
      .mockReturnValue({ clearRect: vi.fn(), drawImage: vi.fn() } as any);
    const toDataURLSpy = vi
      .spyOn(HTMLCanvasElement.prototype, "toDataURL")
      .mockReturnValue("data:image/jpeg;base64,/9j/T0xE");
    const largeValidPng = new Uint8Array(1100 * 1024);
    largeValidPng.set(pngSignatureBytes, 0);

    try {
      const { container } = render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
      await waitFor(() => expect(screen.getByTestId("ve-dirs-empty-hint")).toBeTruthy());
      const input = screen.getByTestId("ve-avatar-file-input");

      fireEvent.change(input, {
        target: { files: [new File([largeValidPng], "large.png", { type: "image/png" })] },
      });
      await waitFor(() => expect(images.length).toBe(1));

      fireEvent.change(input, {
        target: { files: [new File([pngSignatureBytes], "small.png", { type: "image/png" })] },
      });
      await waitFor(() => expect(images.length).toBe(2));
      images[1].onload?.();
      await waitFor(() => {
        const preview = container.querySelector(".ve-avatar-editor__image") as HTMLImageElement | null;
        expect(preview?.src).toContain("data:image/png;base64");
      });

      images[0].onload?.();
      await waitFor(() => expect(toDataURLSpy).toHaveBeenCalledWith("image/jpeg", 0.9));
      const preview = container.querySelector(".ve-avatar-editor__image") as HTMLImageElement | null;
      expect(preview?.src).toContain("data:image/png;base64");
      expect(preview?.src).not.toContain("/9j/T0xE");
    } finally {
      (globalThis as any).Image = originalImage;
      getContextSpy.mockRestore();
      toDataURLSpy.mockRestore();
    }
  });

  it("ignores stale large-avatar resize after status refresh", async () => {
    const handlers = new Map<string, () => void>();
    (EventsOn as unknown as ReturnType<typeof vi.fn>).mockImplementation(
      (event: string, handler: () => void) => {
        handlers.set(event, handler);
        return vi.fn();
      },
    );
    const savedAvatar = "data:image/jpeg;base64,/9j/U0FWRUQ=";
    GetVEStatusMock
      .mockResolvedValueOnce({ registered: false })
      .mockResolvedValueOnce({
        registered: true,
        employee: {
          name: "Saved Name",
          skill_description: "saved skill",
          access_policy: "public",
          avatar_data_url: savedAvatar,
          status: "active",
        },
      });
    const originalImage = globalThis.Image;
    const images: Array<{ onload: (() => void) | null }> = [];
    class MockImage {
      width = 4000;
      height = 2000;
      onload: (() => void) | null = null;
      onerror: (() => void) | null = null;
      constructor() {
        images.push(this);
      }
      set src(_value: string) {}
    }
    (globalThis as any).Image = MockImage;
    const getContextSpy = vi
      .spyOn(HTMLCanvasElement.prototype, "getContext")
      .mockReturnValue({ clearRect: vi.fn(), drawImage: vi.fn() } as any);
    const toDataURLSpy = vi
      .spyOn(HTMLCanvasElement.prototype, "toDataURL")
      .mockReturnValue("data:image/jpeg;base64,/9j/T0xE");
    const largeValidPng = new Uint8Array(1100 * 1024);
    largeValidPng.set(pngSignatureBytes, 0);

    try {
      const { container } = render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
      await waitFor(() => expect(screen.getByTestId("ve-dirs-empty-hint")).toBeTruthy());

      fireEvent.change(screen.getByTestId("ve-avatar-file-input"), {
        target: { files: [new File([largeValidPng], "large.png", { type: "image/png" })] },
      });
      await waitFor(() => expect(images.length).toBe(1));

      handlers.get("ve:disabled")?.();
      await waitFor(() => {
        const preview = container.querySelector(".ve-avatar-editor__image") as HTMLImageElement | null;
        expect(preview?.src).toBe(savedAvatar);
      });

      images[0].onload?.();
      await waitFor(() => expect(toDataURLSpy).toHaveBeenCalledWith("image/jpeg", 0.9));
      const preview = container.querySelector(".ve-avatar-editor__image") as HTMLImageElement | null;
      expect(preview?.src).toBe(savedAvatar);
    } finally {
      (globalThis as any).Image = originalImage;
      getContextSpy.mockRestore();
      toDataURLSpy.mockRestore();
    }
  });

  it("disables submit while a large avatar is being resized", async () => {
    const originalImage = globalThis.Image;
    const images: Array<{ onload: (() => void) | null }> = [];
    class MockImage {
      width = 4000;
      height = 2000;
      onload: (() => void) | null = null;
      onerror: (() => void) | null = null;
      constructor() {
        images.push(this);
      }
      set src(_value: string) {}
    }
    (globalThis as any).Image = MockImage;
    const getContextSpy = vi
      .spyOn(HTMLCanvasElement.prototype, "getContext")
      .mockReturnValue({ clearRect: vi.fn(), drawImage: vi.fn() } as any);
    const toDataURLSpy = vi
      .spyOn(HTMLCanvasElement.prototype, "toDataURL")
      .mockReturnValue("data:image/jpeg;base64,/9j/U01BTEw=");
    const largeValidPng = new Uint8Array(1100 * 1024);
    largeValidPng.set(pngSignatureBytes, 0);

    try {
      render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
      await waitFor(() => expect(screen.getByTestId("ve-dirs-empty-hint")).toBeTruthy());

      fireEvent.change(screen.getByTestId("ve-avatar-file-input"), {
        target: { files: [new File([largeValidPng], "large.png", { type: "image/png" })] },
      });

      const submit = screen.getByTestId("ve-submit-btn") as HTMLButtonElement;
      await waitFor(() => expect(submit.disabled).toBe(true));
      fireEvent.click(submit);
      expect(RegisterVirtualEmployeeMock).not.toHaveBeenCalled();

      await waitFor(() => expect(images.length).toBe(1));
      images[0].onload?.();
      await waitFor(() => expect(submit.disabled).toBe(false));
    } finally {
      (globalThis as any).Image = originalImage;
      getContextSpy.mockRestore();
      toDataURLSpy.mockRestore();
    }
  });

  it("clears uploaded avatar preview when the browser cannot decode it", async () => {
    const originalImage = globalThis.Image;
    class MockImage {
      width = 640;
      height = 320;
      onload: (() => void) | null = null;
      onerror: (() => void) | null = null;
      set src(_value: string) {
        window.setTimeout(() => this.onerror?.(), 0);
      }
    }
    (globalThis as any).Image = MockImage;
    const { container } = render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    await waitFor(() => expect(screen.getByTestId("ve-dirs-empty-hint")).toBeTruthy());

    try {
      fireEvent.change(screen.getByTestId("ve-avatar-file-input"), {
        target: { files: [new File([pngSignatureBytes], "broken.png", { type: "image/png" })] },
      });

      await waitFor(() => {
        expect(container.querySelector(".ve-avatar-editor__image")).toBeNull();
        expect(screen.getByRole("alert").textContent).toBe(avatarInvalidText);
      });
    } finally {
      (globalThis as any).Image = originalImage;
    }
  });

  it("drops unsafe avatar data URLs loaded from status", async () => {
    GetVEStatusMock.mockResolvedValue({
      registered: true,
      employee: {
        name: "Existing Name",
        skill_description: "existing skill",
        access_policy: "public",
        avatar_data_url: "javascript:alert(1)",
        status: "active",
      },
    });
    const { container } = render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

    await waitFor(() =>
      expect(screen.getByTestId("ve-status-badge").textContent).toBe("\u5df2\u6fc0\u6d3b"),
    );
    expect(container.querySelector(".ve-avatar-editor__image")).toBeNull();
  });

  it("clears existing saved avatar if the browser cannot decode it", async () => {
    GetVEStatusMock.mockResolvedValue({
      registered: true,
      employee: {
        name: "Existing Name",
        skill_description: "existing skill",
        access_policy: "public",
        avatar_data_url: "data:image/png;base64,iVBORw0KGgo=",
        status: "active",
      },
    });
    const { container } = render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

    const preview = await waitFor(() => {
      const img = container.querySelector(".ve-avatar-editor__image") as HTMLImageElement | null;
      expect(img).toBeTruthy();
      return img as HTMLImageElement;
    });
    fireEvent.error(preview);

    await waitFor(() => {
      expect(container.querySelector(".ve-avatar-editor__image")).toBeNull();
    });
    fireEvent.click(screen.getByTestId("ve-submit-btn"));
    await waitFor(() => {
      expect(UpdateVESettingsMock).toHaveBeenCalledWith(
        "Existing Name",
        "existing skill",
        "public",
        [],
        "",
      );
    });
  });

  it("keeps existing saved avatar when a replacement image cannot decode", async () => {
    const savedAvatar = "data:image/png;base64,iVBORw0KGgo=";
    const replacementBytes = new Uint8Array([...pngSignatureBytes, 0x01]);
    const originalImage = globalThis.Image;
    class MockImage {
      width = 640;
      height = 320;
      onload: (() => void) | null = null;
      onerror: (() => void) | null = null;
      set src(_value: string) {
        window.setTimeout(() => this.onerror?.(), 0);
      }
    }
    GetVEStatusMock.mockResolvedValue({
      registered: true,
      employee: {
        name: "Existing Name",
        skill_description: "existing skill",
        access_policy: "public",
        avatar_data_url: savedAvatar,
        status: "active",
      },
    });
    UpdateVESettingsMock.mockResolvedValue(undefined);
    const { container } = render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

    await waitFor(() => expect(container.querySelector(".ve-avatar-editor__image")).toBeTruthy());
    (globalThis as any).Image = MockImage;
    try {
      fireEvent.change(screen.getByTestId("ve-avatar-file-input"), {
        target: { files: [new File([replacementBytes], "replacement.png", { type: "image/png" })] },
      });

      await waitFor(() => {
        const img = container.querySelector(".ve-avatar-editor__image") as HTMLImageElement | null;
        expect(img?.src).toContain(savedAvatar);
        expect(screen.getByRole("alert").textContent).toBe(avatarInvalidText);
      });
      fireEvent.click(screen.getByTestId("ve-submit-btn"));
      await waitFor(() => {
        expect(UpdateVESettingsMock).toHaveBeenCalledWith(
          "Existing Name",
          "existing skill",
          "public",
          [],
          savedAvatar,
        );
      });
    } finally {
      (globalThis as any).Image = originalImage;
    }
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
        "",
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
        "",
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

  it("loads and saves access request ringtone preset", async () => {
    LoadConfigMock.mockResolvedValue({
      remote_hub_url: "http://hub.local",
      remote_machine_id: "machine-123",
      remote_machine_token: "token 123",
      group_discussion: {
        auth_request_sound_preset: "soft",
        auth_request_sound_muted: false,
      },
    });
    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

    const select = (await screen.findByLabelText("访问请求铃声")) as HTMLSelectElement;
    await waitFor(() => expect(select.value).toBe("soft"));

    fireEvent.change(select, { target: { value: "urgent" } });

    await waitFor(() => {
      expect(SetAuthRequestSoundConfigMock).toHaveBeenCalledWith("urgent", false);
    });
  });

  it("saves access request ringtone muted state", async () => {
    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    const checkbox = (await screen.findByLabelText("收到访问请求时播放铃声")) as HTMLInputElement;
    await waitFor(() => expect(checkbox.checked).toBe(true));

    fireEvent.click(checkbox);

    await waitFor(() => {
      expect(SetAuthRequestSoundConfigMock).toHaveBeenCalledWith("classic", true);
    });
  });

  it("does not roll back newer ringtone changes when an older save fails", async () => {
    let rejectFirstSave: ((err: Error) => void) | undefined;
    SetAuthRequestSoundConfigMock
      .mockImplementationOnce(
        () =>
          new Promise<void>((_resolve, reject) => {
            rejectFirstSave = reject;
          }),
      )
      .mockResolvedValueOnce(undefined);

    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);

    const select = document.querySelector("#ve-auth-sound-preset") as HTMLSelectElement;
    await waitFor(() => expect(select.value).toBe("classic"));

    fireEvent.change(select, { target: { value: "soft" } });
    fireEvent.change(select, { target: { value: "urgent" } });
    rejectFirstSave?.(new Error("stale save failed"));

    await waitFor(() => expect(select.value).toBe("urgent"));
    expect(SetAuthRequestSoundConfigMock).toHaveBeenNthCalledWith(1, "soft", false);
    expect(SetAuthRequestSoundConfigMock).toHaveBeenNthCalledWith(2, "urgent", false);
  });
});

describe("VirtualEmployeeSettingsPanel - Directory Configuration UI", () => {
  it("renders empty state when no directories configured", async () => {
    GetVEAllowedDirectoriesMock.mockResolvedValue([]);
    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    await waitFor(() =>
      expect(screen.getByTestId("ve-dirs-empty-hint")).toBeTruthy(),
    );
    expect(screen.getByTestId("ve-dirs-empty-hint").textContent).toContain(
      "未配置目录",
    );
    expect(screen.queryByTestId("ve-allowed-dirs-list")).toBeNull();
  });

  it("loads and displays directories on mount", async () => {
    GetVEAllowedDirectoriesMock.mockResolvedValue([
      "D:\\Documents\\Templates",
      "C:\\Users\\Owner\\Downloads",
    ]);
    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    await waitFor(() =>
      expect(screen.getByTestId("ve-allowed-dirs-list")).toBeTruthy(),
    );
    expect(screen.getByText("D:\\Documents\\Templates")).toBeTruthy();
    expect(screen.getByText("C:\\Users\\Owner\\Downloads")).toBeTruthy();
    expect(screen.queryByTestId("ve-dirs-empty-hint")).toBeNull();
  });

  it("adds a directory when SelectVEAllowedDirectory returns a path", async () => {
    GetVEAllowedDirectoriesMock.mockResolvedValue([]);
    SelectVEAllowedDirectoryMock.mockResolvedValue("D:\\NewFolder\\Shared");
    SetVEAllowedDirectoriesMock.mockResolvedValue(undefined);

    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    await waitFor(() =>
      expect(screen.getByTestId("ve-add-dir-btn")).toBeTruthy(),
    );

    fireEvent.click(screen.getByTestId("ve-add-dir-btn"));

    await waitFor(() =>
      expect(SetVEAllowedDirectoriesMock).toHaveBeenCalledWith([
        "D:\\NewFolder\\Shared",
      ]),
    );
    await waitFor(() =>
      expect(screen.getByText("D:\\NewFolder\\Shared")).toBeTruthy(),
    );
  });

  it("removes a directory and calls SetVEAllowedDirectories with updated list", async () => {
    GetVEAllowedDirectoriesMock.mockResolvedValue([
      "D:\\Dir1",
      "D:\\Dir2",
      "D:\\Dir3",
    ]);
    SetVEAllowedDirectoriesMock.mockResolvedValue(undefined);

    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    await waitFor(() => expect(screen.getByText("D:\\Dir2")).toBeTruthy());

    fireEvent.click(screen.getByTestId("ve-remove-dir-D:\\Dir2"));

    await waitFor(() =>
      expect(SetVEAllowedDirectoriesMock).toHaveBeenCalledWith([
        "D:\\Dir1",
        "D:\\Dir3",
      ]),
    );
    await waitFor(() => expect(screen.queryByText("D:\\Dir2")).toBeNull());
  });

  it("does not change list when user cancels directory picker (empty string returned)", async () => {
    GetVEAllowedDirectoriesMock.mockResolvedValue(["D:\\Existing"]);
    SelectVEAllowedDirectoryMock.mockResolvedValue("");

    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    await waitFor(() => expect(screen.getByText("D:\\Existing")).toBeTruthy());

    fireEvent.click(screen.getByTestId("ve-add-dir-btn"));

    // Wait a tick to ensure async handler completes
    await waitFor(() =>
      expect(SelectVEAllowedDirectoryMock).toHaveBeenCalled(),
    );
    expect(SetVEAllowedDirectoriesMock).not.toHaveBeenCalled();
    expect(screen.getByText("D:\\Existing")).toBeTruthy();
    expect(screen.queryByTestId("ve-dir-duplicate-warning")).toBeNull();
  });

  it("shows duplicate warning when adding same directory with different casing", async () => {
    GetVEAllowedDirectoriesMock.mockResolvedValue(["D:\\Documents\\Templates"]);
    SelectVEAllowedDirectoryMock.mockResolvedValue("d:\\documents\\templates");

    render(<VirtualEmployeeSettingsPanel remoteMachineId="m1" />);
    await waitFor(() =>
      expect(screen.getByText("D:\\Documents\\Templates")).toBeTruthy(),
    );

    fireEvent.click(screen.getByTestId("ve-add-dir-btn"));

    await waitFor(() =>
      expect(screen.getByTestId("ve-dir-duplicate-warning")).toBeTruthy(),
    );
    expect(
      screen.getByTestId("ve-dir-duplicate-warning").textContent,
    ).toContain("已在列表中");
    expect(SetVEAllowedDirectoriesMock).not.toHaveBeenCalled();
  });
});
