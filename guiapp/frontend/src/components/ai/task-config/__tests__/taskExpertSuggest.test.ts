import { describe, expect, it } from "vitest";
import { suggestedExpertForDraft } from "../taskExpertSuggest";
import { defaultTaskDraft, withCloudWorkspace, withExpert, withLocalWorkspace, withRemoteWorkspace, withWorkflow, WORKFLOW_AUTO } from "../taskDraft";

const translate = { expertId: "builtin-paper-translate", expertName: "论文翻译专家", confident: true, source: "local" };

describe("suggestedExpertForDraft", () => {
    it("assigns a confident expert on the default draft", () => {
        expect(suggestedExpertForDraft(defaultTaskDraft(), translate)).toEqual({
            id: "builtin-paper-translate",
            name: "论文翻译专家",
        });
    });

    it("keeps a chosen local directory", () => {
        const draft = withLocalWorkspace(defaultTaskDraft(), "D:/papers");
        expect(suggestedExpertForDraft(draft, translate)?.id).toBe("builtin-paper-translate");
    });

    it("uses the id when the suggestion has no name", () => {
        expect(suggestedExpertForDraft(defaultTaskDraft(), { expertId: "expert-notes", confident: true })).toEqual({
            id: "expert-notes",
            name: "expert-notes",
        });
    });

    it("stays general when the reading is unsure, general, or empty", () => {
        expect(suggestedExpertForDraft(defaultTaskDraft(), { expertId: "", confident: false })).toBeNull();
        expect(suggestedExpertForDraft(defaultTaskDraft(), { expertId: "general", confident: true })).toBeNull();
        expect(suggestedExpertForDraft(defaultTaskDraft(), { confident: true })).toBeNull();
        expect(suggestedExpertForDraft(defaultTaskDraft(), null)).toBeNull();
    });

    it("leaves an explicit expert, a workflow, and a remote or cloud workspace alone", () => {
        expect(suggestedExpertForDraft(withExpert(defaultTaskDraft(), "expert-1", "笔记"), translate)).toBeNull();
        expect(suggestedExpertForDraft(withWorkflow(defaultTaskDraft(), WORKFLOW_AUTO), translate)).toBeNull();
        expect(suggestedExpertForDraft(withWorkflow(defaultTaskDraft(), "wf-1"), translate)).toBeNull();
        expect(suggestedExpertForDraft(withCloudWorkspace(defaultTaskDraft(), "cws-1", "讲义"), translate)).toBeNull();
        expect(suggestedExpertForDraft(withRemoteWorkspace(defaultTaskDraft(), {
            host: "dev.example",
            port: 22,
            user: "me",
            password: "secret",
            workDir: "/src",
        }), translate)).toBeNull();
    });
});
