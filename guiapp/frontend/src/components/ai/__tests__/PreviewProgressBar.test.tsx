import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PreviewLoadingStatus, PreviewProgressBar, previewProgressLabel } from "../PreviewProgressBar";

const phrases = {
    wait: "正在加载表格预览…",
    read: "正在读取表格…",
    parse: "正在解析表格…",
};

describe("previewProgressLabel", () => {
    it("appends a percent while bytes arrive and leaves parsing unlabeled", () => {
        expect(previewProgressLabel("wait", null, phrases)).toBe("正在加载表格预览…");
        expect(previewProgressLabel("read", null, phrases)).toBe("正在读取表格…");
        expect(previewProgressLabel("read", 0.4, phrases)).toBe("正在读取表格… 40%");
        expect(previewProgressLabel("read", 2, phrases)).toBe("正在读取表格… 100%");
        expect(previewProgressLabel("parse", 0.4, phrases)).toBe("正在解析表格…");
    });
});

describe("PreviewProgressBar", () => {
    it("draws a determinate bar from the byte ratio", () => {
        const { getByTestId } = render(<PreviewProgressBar label="正在读取表格… 40%" ratio={0.4} />);
        const bar = getByTestId("preview-progress-bar");
        expect(bar.getAttribute("role")).toBe("progressbar");
        expect(bar.getAttribute("aria-label")).toBe("正在读取表格… 40%");
        expect(bar.getAttribute("aria-valuenow")).toBe("40");
        expect(bar.hasAttribute("aria-valuetext")).toBe(false);
        expect(bar.getAttribute("data-mode")).toBe("determinate");
        expect((bar.firstElementChild as HTMLElement).style.width).toBe("40%");
    });

    it("draws an indeterminate bar while a document is parsing", () => {
        const { getByTestId } = render(<PreviewProgressBar label="正在解析表格…" ratio={null} />);
        const bar = getByTestId("preview-progress-bar");
        expect(bar.hasAttribute("aria-valuenow")).toBe(false);
        expect(bar.getAttribute("data-mode")).toBe("indeterminate");
        expect(bar.firstElementChild?.className).toContain("maclaw-preview-progress-slide");
        expect(bar.getAttribute("aria-label")).toBe("正在解析表格…");
    });
});

describe("PreviewLoadingStatus", () => {
    it("keeps the byte percent out of the status live region", () => {
        const { getByRole, getByTestId } = render(
            <PreviewLoadingStatus
                testId="preview-loading"
                phase="read"
                ratio={0.4}
                showBar
                phrases={phrases}
            />,
        );
        const status = getByRole("status");
        const bar = getByTestId("preview-progress-bar");
        expect(status.textContent).toBe("正在读取表格…");
        expect(status.contains(bar)).toBe(false);
        expect(getByTestId("preview-loading").textContent).toContain("40%");
        expect(bar.getAttribute("aria-label")).toBe("正在读取表格…");
        expect(bar.getAttribute("aria-valuenow")).toBe("40");
    });

    it("does not invent a percent while parsing", () => {
        const { getByRole, getByTestId } = render(
            <PreviewLoadingStatus
                testId="preview-loading"
                phase="parse"
                ratio={0.4}
                showBar
                phrases={phrases}
            />,
        );
        expect(getByRole("status").textContent).toBe("正在解析表格…");
        expect(getByTestId("preview-loading").textContent).not.toContain("%");
        expect(getByTestId("preview-progress-bar").getAttribute("data-mode")).toBe("indeterminate");
    });
});
