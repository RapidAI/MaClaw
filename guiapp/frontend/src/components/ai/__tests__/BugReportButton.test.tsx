import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { BugReportButton, bugReportDescriptionDraft, bugReportSubmitDescription, clampBugReportDescription, GUI_BUG_REPORT_DESCRIPTION_BYTES } from "../BugReportButton";

const TINY_PNG = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";

const { capture, discard, submit, shotURL } = vi.hoisted(() => ({
    capture: vi.fn(),
    discard: vi.fn(),
    submit: vi.fn(),
    shotURL: vi.fn(),
}));

vi.mock("../../../../wailsjs/go/main/App", () => ({
    CaptureGUIBugReportScreenshot: () => capture(),
    GUIBugReportScreenshotDataURL: () => shotURL(),
    DiscardGUIBugReportScreenshot: (token: string) => discard(token),
    SubmitGUIBugReport: (description: string, annotated?: string) => submit(description, annotated ?? ""),
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
    beforeEach(() => {
        shotURL.mockReset();
        shotURL.mockResolvedValue(TINY_PNG);
    });

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
        expect(submit.mock.calls[0][1]).toBe("");
        expect(new TextEncoder().encode(sent).byteLength).toBeLessThanOrEqual(GUI_BUG_REPORT_DESCRIPTION_BYTES);
        expect(sent.length).toBeGreaterThan(0);
    });

    it("shows the screenshot and sends the marked image", async () => {
        capture.mockReset();
        capture.mockResolvedValue("shot-a");
        submit.mockReset();
        submit.mockResolvedValue({ id: "BR-marked" });
        render(<BugReportButton lang="zh" />);
        fireEvent.click(screen.getByTestId("task-bug-report-btn"));
        const img = await screen.findByRole("img", { name: "当前界面截图" }) as HTMLImageElement;
        expect(img.getAttribute("src")).toBe(TINY_PNG);
        expect(screen.getByRole("button", { name: "荧光笔" }).getAttribute("aria-pressed")).toBe("true");
        fireEvent.click(screen.getByRole("button", { name: "自由画笔" }));
        expect(screen.getByRole("button", { name: "自由画笔" }).getAttribute("aria-pressed")).toBe("true");
        expect(screen.getByRole("button", { name: "荧光笔" }).getAttribute("aria-pressed")).toBe("false");
        fireEvent.click(screen.getByRole("button", { name: "红色" }));
        expect(screen.getByRole("button", { name: "红色" }).getAttribute("aria-pressed")).toBe("true");

        const canvas = screen.getByLabelText("在截图上标注");
        const box = { x: 0, y: 0, left: 0, top: 0, right: 200, bottom: 100, width: 200, height: 100, toJSON() { return {}; } };
        canvas.getBoundingClientRect = () => box;
        img.getBoundingClientRect = () => box;
        Object.defineProperty(img, "naturalWidth", { configurable: true, value: 20 });
        Object.defineProperty(img, "naturalHeight", { configurable: true, value: 10 });
        const originalGetContext = HTMLCanvasElement.prototype.getContext;
        const originalToDataURL = HTMLCanvasElement.prototype.toDataURL;
        const ctx = {
            save() {},
            restore() {},
            beginPath() {},
            moveTo() {},
            lineTo() {},
            quadraticCurveTo() {},
            stroke() {},
            fill() {},
            arc() {},
            strokeRect() {},
            clearRect() {},
            setTransform() {},
            drawImage() {},
            strokeStyle: "",
            fillStyle: "",
            lineWidth: 1,
            lineCap: "butt" as CanvasLineCap,
            lineJoin: "miter" as CanvasLineJoin,
            globalAlpha: 1,
        };
        HTMLCanvasElement.prototype.getContext = (() => ctx) as unknown as typeof HTMLCanvasElement.prototype.getContext;
        HTMLCanvasElement.prototype.toDataURL = (() => "data:image/png;base64,EDITED") as typeof HTMLCanvasElement.prototype.toDataURL;
        try {
            await act(async () => {
                fireEvent.load(img);
            });
            expect((img.parentElement as HTMLElement).style.width).toBe("200px");
            fireEvent.pointerDown(canvas, { clientX: 20, clientY: 20, button: 0, pointerId: 1 });
            expect(document.activeElement).toBe(canvas);
            fireEvent.pointerUp(canvas, { clientX: 160, clientY: 70, button: 0, pointerId: 1 });
            expect((screen.getByRole("button", { name: "撤销" }) as HTMLButtonElement).disabled).toBe(false);
            fireEvent.keyDown(canvas, { key: "z", ctrlKey: true });
            expect((screen.getByRole("button", { name: "撤销" }) as HTMLButtonElement).disabled).toBe(true);
            fireEvent.pointerDown(canvas, { clientX: 20, clientY: 20, button: 0, pointerId: 1 });
            fireEvent.pointerUp(canvas, { clientX: 20, clientY: 20, button: 0, pointerId: 2 });
            expect((screen.getByRole("button", { name: "撤销" }) as HTMLButtonElement).disabled).toBe(true);
            fireEvent.pointerMove(canvas, { clientX: 120, clientY: 60, button: 0, pointerId: 1 });
            fireEvent.pointerUp(canvas, { clientX: 120, clientY: 60, button: 0, pointerId: 1 });
            expect((screen.getByRole("button", { name: "撤销" }) as HTMLButtonElement).disabled).toBe(false);
            const area = screen.getByLabelText("故障描述");
            area.focus();
            fireEvent.keyDown(area, { key: "z", ctrlKey: true });
            expect((screen.getByRole("button", { name: "撤销" }) as HTMLButtonElement).disabled).toBe(false);
            canvas.focus();
            fireEvent.keyDown(canvas, { key: "z", ctrlKey: true });
            expect((screen.getByRole("button", { name: "撤销" }) as HTMLButtonElement).disabled).toBe(true);
            fireEvent.pointerDown(canvas, { clientX: 20, clientY: 20, button: 0, pointerId: 1 });
            fireEvent.pointerMove(canvas, { clientX: 120, clientY: 60, button: 0, pointerId: 1 });
            fireEvent.pointerUp(canvas, { clientX: 120, clientY: 60, button: 0, pointerId: 1 });
            expect((screen.getByRole("button", { name: "撤销" }) as HTMLButtonElement).disabled).toBe(false);
            fireEvent.click(screen.getByRole("button", { name: "清除标注" }));
            expect((screen.getByRole("button", { name: "撤销" }) as HTMLButtonElement).disabled).toBe(false);
            expect((screen.getByRole("button", { name: "清除标注" }) as HTMLButtonElement).disabled).toBe(true);
            fireEvent.click(screen.getByRole("button", { name: "撤销" }));
            expect((screen.getByRole("button", { name: "清除标注" }) as HTMLButtonElement).disabled).toBe(false);
            fireEvent.change(screen.getByLabelText("故障描述"), { target: { value: "窗口上标出了卡住的按钮" } });
            await act(async () => {
                fireEvent.click(screen.getByRole("button", { name: "发送报告" }));
            });
        } finally {
            HTMLCanvasElement.prototype.getContext = originalGetContext;
            HTMLCanvasElement.prototype.toDataURL = originalToDataURL;
        }
        expect(submit).toHaveBeenCalledTimes(1);
        expect(submit.mock.calls[0][0]).toBe("窗口上标出了卡住的按钮");
        expect(submit.mock.calls[0][1]).toBe("data:image/png;base64,EDITED");
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
