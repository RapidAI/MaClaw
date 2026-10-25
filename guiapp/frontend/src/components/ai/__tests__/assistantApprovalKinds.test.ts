import { describe, expect, it } from "vitest";
import { scopeApprovalShouldAutoDismiss } from "../assistantApprovalKinds";

describe("scopeApprovalShouldAutoDismiss", () => {
    it("leaves a commit-shape dialog open when there is no countdown", () => {
        expect(scopeApprovalShouldAutoDismiss({ autoAllow: false, timeoutSeconds: 0 }, 0)).toBe(false);
    });

    it("closes a high-risk dialog when its countdown reaches zero", () => {
        expect(scopeApprovalShouldAutoDismiss({ autoAllow: false, timeoutSeconds: 10 }, 0)).toBe(true);
        expect(scopeApprovalShouldAutoDismiss({ autoAllow: true, timeoutSeconds: 10 }, 1)).toBe(true);
        expect(scopeApprovalShouldAutoDismiss({ autoAllow: false, timeoutSeconds: 10 }, 4)).toBe(false);
        expect(scopeApprovalShouldAutoDismiss(null, 0)).toBe(false);
    });
});
