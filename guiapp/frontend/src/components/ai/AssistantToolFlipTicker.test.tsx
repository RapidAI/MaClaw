// @vitest-environment jsdom
import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { lightTheme } from "./aiAssistantPanelTheme";
import { AssistantToolFlipTicker } from "./AssistantToolFlipTicker";

describe("AssistantToolFlipTicker", () => {
    it("hovers the extracted argument instead of the raw JSON blob", () => {
        render(
            <AssistantToolFlipTicker
                call={{ id: "1", name: "web_search", action: "搜索网页", detail: '{"query":"北京天气","limit":5}' }}
                theme={lightTheme}
            />,
        );
        const strip = screen.getByTestId("assistant-tool-flip");
        expect(strip.getAttribute("aria-label")).toBe("web_search (北京天气)");
        expect(strip.getAttribute("title")).toBe("web_search (北京天气)");
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it("drops the next call in on a split flap and settles when the flap lands", () => {
        vi.useFakeTimers();
        const { rerender } = render(
            <AssistantToolFlipTicker
                call={{ id: "1", name: "web_search", action: "搜索网页", detail: "北京天气" }}
                theme={lightTheme}
            />,
        );
        const strip = screen.getByTestId("assistant-tool-flip");
        expect(strip.getAttribute("aria-label")).toBe("web_search (北京天气)");
        expect(strip.getAttribute("data-flipping")).toBe("false");
        expect(strip.querySelector(".assistant-tool-flip-face--enter")).toBeTruthy();

        rerender(
            <AssistantToolFlipTicker
                call={{ id: "1", name: "web_search", action: "搜索网页", detail: "北京今天天气" }}
                theme={lightTheme}
            />,
        );
        expect(strip.getAttribute("data-flipping")).toBe("false");
        expect(strip.getAttribute("aria-label")).toBe("web_search (北京今天天气)");

        rerender(
            <AssistantToolFlipTicker
                call={{ id: "2", name: "generate_pdf", action: "生成 PDF", detail: "北京天气.pdf" }}
                theme={lightTheme}
            />,
        );
        expect(strip.getAttribute("data-flipping")).toBe("true");
        expect(screen.getByTestId("assistant-tool-flip-in").textContent).toContain("generate_pdf");
        expect(screen.getByTestId("assistant-tool-flip-in").textContent).toContain("北京天气.pdf");
        expect(screen.getByTestId("assistant-tool-flip-out").textContent).toContain("web_search");
        expect(screen.getByTestId("assistant-tool-flip-out").className).toContain("assistant-tool-flip-face--out");
        expect(screen.getByTestId("assistant-tool-flip-in").className).toContain("assistant-tool-flip-face--in");

        act(() => {
            vi.advanceTimersByTime(1280);
        });
        expect(strip.getAttribute("data-flipping")).toBe("false");
        expect(screen.queryByTestId("assistant-tool-flip-out")).toBeNull();
        expect(strip.getAttribute("aria-label")).toBe("generate_pdf (北京天气.pdf)");
    });
});
