import { describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { BugReportButton, bugReportDescriptionDraft, bugReportSubmitDescription, clampBugReportDescription, GUI_BUG_REPORT_DESCRIPTION_BYTES } from "../BugReportButton";

const { capture, discard, submit } = vi.hoisted(() => ({
    capture: vi.fn(),
    discard: vi.fn(),
    submit: vi.fn(),
}));

vi.mock("../../../../wailsjs/go/main/App", () => ({
    CaptureGUIBugReportScreenshot: () => capture(),
    DiscardGUIBugReportScreenshot: (token: string) => discard(token),
    SubmitGUIBugReport: (description: string) => submit(description),
}));

vi.mock("../../CustomDialog", () => ({
    useDialog: () => ({ showAlert: vi.fn(async () => undefined) }),
}));

describe("clampBugReportDescription", () => {
    it("keeps 32 KB of ASCII and cuts Chinese text at the same byte limit", () => {
        const ascii = "a".repeat(GUI_BUG_REPORT_DESCRIPTION_BYTES);
        expect(clampBugReportDescription(ascii)).toBe(ascii);
        expect(clampBugReportDescription(ascii + "b").length).toBe(GUI_BUG_REPORT_DESCRIPTION_BYTES);

        const han = "界".repeat(12_000);
        const clamped = clampBugReportDescription(han);
        const bytes = new TextEncoder().encode(clamped).byteLength;
        expect(bytes).toBeLessThanOrEqual(GUI_BUG_REPORT_DESCRIPTION_BYTES);
        expect(bytes).toBeGreaterThan(GUI_BUG_REPORT_DESCRIPTION_BYTES - 3);
        expect(clamped.length).toBeLessThan(han.length);
        expect(bugReportDescriptionDraft(han, true)).toBe(han);
        expect(bugReportDescriptionDraft(han, false)).toBe(clamped);
        expect(bugReportSubmitDescription(han)).toBeNull();
        expect(bugReportSubmitDescription(clamped)).toBe(clamped);
        expect(bugReportSubmitDescription("  ")).toBeNull();
    });
});

describe("BugReportButton", () => {
    it("captures once when the button is clicked twice before the shot returns", async () => {
        let resolveCapture: (token: string) => void = () => {};
        capture.mockReset();
        capture.mockImplementation(() => new Promise<string>((resolve) => {
            resolveCapture = resolve;
        }));
        render(<BugReportButton lang="zh" />);
        const button = screen.getByTestId("task-bug-report-btn");
        fireEvent.click(button);
        fireEvent.click(button);
        expect(capture).toHaveBeenCalledTimes(1);

        await act(async () => {
            resolveCapture("shot-a");
        });
        expect(screen.getByRole("dialog")).toBeTruthy();
        expect((button as HTMLButtonElement).disabled).toBe(true);
        fireEvent.click(button);
        expect(capture).toHaveBeenCalledTimes(1);

        const area = screen.getByLabelText("故障描述") as HTMLTextAreaElement;
        fireEvent.change(area, { target: { value: "界".repeat(12_000) } });
        expect(new TextEncoder().encode(area.value).byteLength).toBeLessThanOrEqual(GUI_BUG_REPORT_DESCRIPTION_BYTES);
        expect(screen.getByRole("alert").textContent).toContain("32 KB");

        discard.mockClear();
        (document.activeElement as HTMLElement | null)?.blur();
        expect(document.activeElement).toBe(document.body);
        fireEvent.keyDown(document.body, { key: "Escape" });
        expect(screen.queryByRole("dialog")).toBeNull();
        expect(discard).toHaveBeenCalledTimes(1);
        expect(discard).toHaveBeenCalledWith("shot-a");
        expect((button as HTMLButtonElement).disabled).toBe(false);
    });

    it("discards the capture that belonged to the dialog that closed", async () => {
        capture.mockReset();
        discard.mockReset();
        capture.mockResolvedValueOnce("shot-a").mockResolvedValueOnce("shot-b");
        render(<BugReportButton lang="zh" />);
        const button = screen.getByTestId("task-bug-report-btn");
        fireEvent.click(button);
        await act(async () => {});
        fireEvent.click(screen.getByRole("button", { name: "取消" }));
        expect(discard).toHaveBeenCalledTimes(1);
        expect(discard).toHaveBeenCalledWith("shot-a");

        fireEvent.click(button);
        await act(async () => {});
        fireEvent.click(screen.getByRole("button", { name: "取消" }));
        expect(discard).toHaveBeenCalledTimes(2);
        expect(discard.mock.calls[1][0]).toBe("shot-b");
    });

    it("does not upload a description that still exceeds 32 KB", async () => {
        capture.mockReset();
        capture.mockResolvedValue(undefined);
        submit.mockReset();
        submit.mockResolvedValue({ id: "BR-1" });
        render(<BugReportButton lang="zh" />);
        fireEvent.click(screen.getByTestId("task-bug-report-btn"));
        await act(async () => {});
        const area = screen.getByLabelText("故障描述") as HTMLTextAreaElement;
        const han = "界".repeat(12_000);
        fireEvent.input(area, { target: { value: han }, isComposing: true });
        expect(area.value).toBe(han);

        fireEvent.click(screen.getByRole("button", { name: "发送报告" }));
        expect(submit).not.toHaveBeenCalled();
        expect(screen.getByRole("alert").textContent).toContain("32 KB");
        expect(new TextEncoder().encode((screen.getByLabelText("故障描述") as HTMLTextAreaElement).value).byteLength)
            .toBeLessThanOrEqual(GUI_BUG_REPORT_DESCRIPTION_BYTES);

        await act(async () => {
            fireEvent.click(screen.getByRole("button", { name: "发送报告" }));
        });
        expect(submit).toHaveBeenCalledTimes(1);
        const sent = submit.mock.calls[0][0] as string;
        expect(new TextEncoder().encode(sent).byteLength).toBeLessThanOrEqual(GUI_BUG_REPORT_DESCRIPTION_BYTES);
        expect(sent.length).toBeGreaterThan(0);
    });

    it("does not upload when composition ends on the send click", async () => {
        capture.mockReset();
        capture.mockResolvedValue(undefined);
        submit.mockReset();
        submit.mockResolvedValue({ id: "BR-1" });
        render(<BugReportButton lang="zh" />);
        fireEvent.click(screen.getByTestId("task-bug-report-btn"));
        await act(async () => {});
        const area = screen.getByLabelText("故障描述") as HTMLTextAreaElement;
        const han = "界".repeat(12_000);
        fireEvent.input(area, { target: { value: han }, isComposing: true });
        const send = screen.getByRole("button", { name: "发送报告" });
        // The browser ends IME composition after pointer-down and before click.
        fireEvent.pointerDown(send);
        fireEvent.compositionEnd(area);
        fireEvent.click(send);
        expect(submit).not.toHaveBeenCalled();
        expect(screen.getByRole("alert").textContent).toContain("32 KB");
        expect(new TextEncoder().encode((screen.getByLabelText("故障描述") as HTMLTextAreaElement).value).byteLength)
            .toBeLessThanOrEqual(GUI_BUG_REPORT_DESCRIPTION_BYTES);

        await act(async () => {
            fireEvent.click(screen.getByRole("button", { name: "发送报告" }));
        });
        expect(submit).toHaveBeenCalledTimes(1);
    });

    it("sends on the next press after a send gesture is cancelled", async () => {
        capture.mockReset();
        capture.mockResolvedValue(undefined);
        submit.mockReset();
        submit.mockResolvedValue({ id: "BR-1" });
        render(<BugReportButton lang="zh" />);
        fireEvent.click(screen.getByTestId("task-bug-report-btn"));
        await act(async () => {});
        const area = screen.getByLabelText("故障描述") as HTMLTextAreaElement;
        fireEvent.input(area, { target: { value: "界".repeat(12_000) }, isComposing: true });
        const send = screen.getByRole("button", { name: "发送报告" });
        fireEvent.pointerDown(send);
        fireEvent.compositionEnd(area);
        fireEvent.pointerDown(send);
        await act(async () => {
            fireEvent.click(send);
        });
        expect(submit).toHaveBeenCalledTimes(1);
        const sent = submit.mock.calls[0][0] as string;
        expect(new TextEncoder().encode(sent).byteLength).toBeLessThanOrEqual(GUI_BUG_REPORT_DESCRIPTION_BYTES);
    });

    it("sends when a later composition ends after the send press was released", async () => {
        capture.mockReset();
        capture.mockResolvedValue("shot");
        submit.mockReset();
        submit.mockResolvedValue({ id: "BR-1" });
        render(<BugReportButton lang="zh" />);
        fireEvent.click(screen.getByTestId("task-bug-report-btn"));
        await act(async () => {});
        const area = screen.getByLabelText("故障描述") as HTMLTextAreaElement;
        fireEvent.input(area, { target: { value: "界".repeat(12_000) }, isComposing: true });
        const send = screen.getByRole("button", { name: "发送报告" });
        fireEvent.pointerDown(send);
        fireEvent.pointerUp(send);
        fireEvent.compositionEnd(area);
        await act(async () => {});
        await act(async () => {
            fireEvent.click(send);
        });
        expect(submit).toHaveBeenCalledTimes(1);
    });

    it("keeps the send gesture armed when the pointer leaves before composition ends", async () => {
        capture.mockReset();
        capture.mockResolvedValue("shot");
        submit.mockReset();
        submit.mockResolvedValue({ id: "BR-1" });
        render(<BugReportButton lang="zh" />);
        fireEvent.click(screen.getByTestId("task-bug-report-btn"));
        await act(async () => {});
        const area = screen.getByLabelText("故障描述") as HTMLTextAreaElement;
        fireEvent.input(area, { target: { value: "界".repeat(12_000) }, isComposing: true });
        const send = screen.getByRole("button", { name: "发送报告" });
        fireEvent.pointerDown(send);
        fireEvent.pointerLeave(send);
        fireEvent.compositionEnd(area);
        await act(async () => {});
        await act(async () => {
            fireEvent.click(send);
        });
        expect(submit).not.toHaveBeenCalled();
    });

    it("discards the capture when the button unmounts before send", async () => {
        capture.mockReset();
        discard.mockReset();
        capture.mockResolvedValue("shot-a");
        const view = render(<BugReportButton lang="zh" />);
        fireEvent.click(screen.getByTestId("task-bug-report-btn"));
        await act(async () => {});
        view.unmount();
        expect(discard).toHaveBeenCalledTimes(1);
        expect(discard).toHaveBeenCalledWith("shot-a");
    });

    it("discards a capture that finishes after the button is gone", async () => {
        let resolveCapture: (token: string) => void = () => {};
        capture.mockReset();
        discard.mockReset();
        capture.mockImplementation(() => new Promise<string>((resolve) => {
            resolveCapture = resolve;
        }));
        const view = render(<BugReportButton lang="zh" />);
        fireEvent.click(screen.getByTestId("task-bug-report-btn"));
        view.unmount();
        await act(async () => {
            resolveCapture("shot-a");
        });
        expect(discard).toHaveBeenCalledTimes(1);
        expect(discard).toHaveBeenCalledWith("shot-a");
    });

    it("keeps the capture while send is still in flight after unmount", async () => {
        let rejectSubmit: (err: Error) => void = () => {};
        capture.mockReset();
        discard.mockReset();
        submit.mockReset();
        capture.mockResolvedValue("shot-a");
        submit.mockImplementation(() => new Promise((_, reject) => {
            rejectSubmit = reject;
        }));
        const view = render(<BugReportButton lang="zh" />);
        fireEvent.click(screen.getByTestId("task-bug-report-btn"));
        await act(async () => {});
        fireEvent.change(screen.getByLabelText("故障描述"), { target: { value: "窗口卡住了" } });
        fireEvent.click(screen.getByRole("button", { name: "发送报告" }));
        await act(async () => {});
        expect(submit).toHaveBeenCalledTimes(1);
        view.unmount();
        expect(discard).not.toHaveBeenCalled();
        await act(async () => {
            rejectSubmit(new Error("hub down"));
        });
        expect(discard).toHaveBeenCalledTimes(1);
        expect(discard).toHaveBeenCalledWith("shot-a");
    });
});
