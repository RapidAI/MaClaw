import { afterEach, describe, expect, it } from "vitest";
import { lightTheme } from "../aiAssistantPanelTheme";
import { isSearchDismissExemptTarget, searchSurfaceRootStyle } from "../projectSearchSurface";

describe("projectSearchSurface", () => {
    afterEach(() => {
        document.body.innerHTML = "";
    });

    it("fills the assistant pane as a column overlay", () => {
        const style = searchSurfaceRootStyle(lightTheme);
        expect(style.position).toBe("absolute");
        expect(style.inset).toBe(0);
        expect(style.display).toBe("flex");
        expect(style.flexDirection).toBe("column");
        expect(style.minHeight).toBe(0);
        expect(style.overflow).toBe("hidden");
        expect(style.background).toBe(lightTheme.bg);
    });

    it("exempts title-bar chrome from outside-dismiss", () => {
        document.body.innerHTML = `
            <div data-testid="ai-title-bar"><button id="max">restore</button></div>
            <span class="mc-header-search-wrap"><input id="q" /></span>
            <div id="side">sidebar</div>
        `;
        expect(isSearchDismissExemptTarget(document.getElementById("max"))).toBe(true);
        expect(isSearchDismissExemptTarget(document.getElementById("q"))).toBe(true);
        expect(isSearchDismissExemptTarget(document.getElementById("side"))).toBe(false);
        expect(isSearchDismissExemptTarget(null)).toBe(false);
    });
});
