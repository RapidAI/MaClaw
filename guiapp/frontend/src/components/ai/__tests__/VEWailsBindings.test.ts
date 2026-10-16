import { describe, expect, it } from "vitest";
import * as AppBindings from "../../../../wailsjs/go/main/App";

describe("VE Wails bindings", () => {
    it("exports the local executor registration binding used by group chat", () => {
        expect(typeof AppBindings.RegisterLocalExecutorInGroup).toBe("function");
    });
});
