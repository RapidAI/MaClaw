import { describe, expect, it } from "vitest";
import type { AgentView } from "../agentViewTypes";
import {
    isAgentViewFieldVisible,
    matchesVisibleWhen,
    visibleInteractiveFormFields,
} from "../agentViewFieldVisibility";

describe("agentViewFieldVisibility", () => {
    it("matches equals / notEquals / empty rules", () => {
        expect(matchesVisibleWhen({ field: "ssh_profile", equals: "__new__" }, { ssh_profile: "__new__" })).toBe(true);
        expect(matchesVisibleWhen({ field: "ssh_profile", equals: "__new__" }, { ssh_profile: "prod" })).toBe(false);
        expect(matchesVisibleWhen({ field: "ssh_profile", notEquals: "__new__" }, { ssh_profile: "prod" })).toBe(true);
        expect(matchesVisibleWhen({ field: "ssh_profile", empty: true }, { ssh_profile: "" })).toBe(true);
        expect(matchesVisibleWhen({ field: "ssh_profile", notEmpty: true }, { ssh_profile: "x" })).toBe(true);
        expect(matchesVisibleWhen({ field: "ssh_profile", equals: ["a", "b"] }, { ssh_profile: "b" })).toBe(true);
    });

    it("applies coding remote fallback without explicit visibleWhen", () => {
        const remote = { id: "remote", label: "远程编程", fields: [] };
        expect(
            isAgentViewFieldVisible(
                { name: "remote_host", type: "text" },
                { ssh_profile: "prod" },
                remote,
            ),
        ).toBe(false);
        expect(
            isAgentViewFieldVisible(
                { name: "remote_host", type: "text" },
                { ssh_profile: "__new__" },
                remote,
            ),
        ).toBe(true);
        expect(
            isAgentViewFieldVisible(
                { name: "remote_host", type: "text" },
                { ssh_profile: "" },
                remote,
            ),
        ).toBe(false);
        expect(
            isAgentViewFieldVisible(
                { name: "remote_workdir", type: "text" },
                { ssh_profile: "prod" },
                remote,
            ),
        ).toBe(true);
        // Password is optional override for any selected profile (including saved hosts).
        expect(
            isAgentViewFieldVisible(
                { name: "ssh_password", type: "text" },
                { ssh_profile: "prod" },
                remote,
            ),
        ).toBe(true);
        expect(
            isAgentViewFieldVisible(
                { name: "ssh_password", type: "text" },
                { ssh_profile: "" },
                remote,
            ),
        ).toBe(false);
        expect(
            isAgentViewFieldVisible(
                { name: "ssh_password", type: "text" },
                { ssh_profile: "__new__" },
                remote,
            ),
        ).toBe(true);
    });

    it("filters interactive fields for remote coding form", () => {
        const view: AgentView = {
            type: "form",
            title: "项目基本信息",
            fields: [{ name: "project_name", type: "text" }],
            variants: [
                {
                    id: "remote",
                    label: "远程编程",
                    fields: [
                        { name: "ssh_profile", type: "select" },
                        { name: "remote_host", type: "text", visibleWhen: { field: "ssh_profile", equals: "__new__" } },
                        { name: "remote_workdir", type: "text" },
                        { name: "_workflow_id", type: "hidden", value: "wf-1" },
                    ],
                },
            ],
        };
        const remote = view.variants![0];
        const withProfile = visibleInteractiveFormFields(view, remote, { ssh_profile: "prod" });
        expect(withProfile.map((f) => f.name).sort()).toEqual(["project_name", "remote_workdir", "ssh_profile"]);

        const withNew = visibleInteractiveFormFields(view, remote, { ssh_profile: "__new__" });
        expect(withNew.map((f) => f.name).sort()).toEqual([
            "project_name",
            "remote_host",
            "remote_workdir",
            "ssh_profile",
        ]);
    });
});
