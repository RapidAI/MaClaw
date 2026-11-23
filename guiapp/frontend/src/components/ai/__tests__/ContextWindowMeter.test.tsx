import React from "react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ContextWindowMeter, formatContextWindowTokens } from "../ContextWindowMeter";

describe("ContextWindowMeter", () => {
    it("shows latest input, usable window, and fill", () => {
        render(<ContextWindowMeter lang="zh-Hans" usedTokens={24000} maxTokens={204800} mutedColor="#667085" />);
        expect(screen.getByTestId("context-window-meter-value").textContent).toBe("24.0k / 205k · 12%");
        expect(screen.getByTestId("context-window-meter-fill").style.width).toBe("12%");
    });

    it("marks a pre-request estimate without hiding the current value", () => {
        render(<ContextWindowMeter lang="zh-Hans" usedTokens={24000} maxTokens={204800} mutedColor="#667085" estimated />);
        expect(screen.getByTestId("context-window-meter-value").textContent).toBe("≈24.0k / 205k · 12%");
        expect(screen.getByTestId("context-window-meter").getAttribute("title")).toContain("估算");
    });

    it("caps only the bar when provider usage exceeds the usable window", () => {
        render(<ContextWindowMeter lang="en" usedTokens={220000} maxTokens={204800} mutedColor="#667085" />);
        expect(screen.getByTestId("context-window-meter-value").textContent).toContain("107%");
        expect(screen.getByTestId("context-window-meter-fill").style.width).toBe("100%");
    });

    it("stays hidden without a resolved model window", () => {
        const { container } = render(<ContextWindowMeter lang="en" usedTokens={100} maxTokens={0} mutedColor="#667085" />);
        expect(container.firstChild).toBeNull();
    });

    it("formats token counts compactly", () => {
        expect(formatContextWindowTokens(999)).toBe("999");
        expect(formatContextWindowTokens(1200)).toBe("1.2k");
        expect(formatContextWindowTokens(1200000)).toBe("1.2M");
    });
});
