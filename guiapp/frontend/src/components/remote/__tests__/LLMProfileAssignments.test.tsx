// @vitest-environment jsdom
import { StrictMode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";

const getState = vi.fn();
const saveProfiles = vi.fn();
const testProfile = vi.fn();
const fetchProfileModels = vi.fn();
const eventsOn = vi.fn();
const eventsOff = vi.fn();

vi.mock("../../../../wailsjs/go/main/App", () => ({
    GetMaclawLLMProfilePanelState: (...args: unknown[]) => getState(...args),
    SaveMaclawLLMProfiles: (...args: unknown[]) => saveProfiles(...args),
    TestMaclawLLMProfile: (...args: unknown[]) => testProfile(...args),
    FetchMaclawLLMProfileModels: (...args: unknown[]) => fetchProfileModels(...args),
}));

vi.mock("../../../../wailsjs/runtime", () => ({
    EventsOn: (...args: unknown[]) => eventsOn(...args),
    EventsOff: (...args: unknown[]) => eventsOff(...args),
}));

import { LLMProfileAssignments, captionModelMissingVision, modelVisionStatus } from "../LLMProfileAssignments";
import type { AgentProviderSwitchTarget } from "../agentProviderSwitchOffer";

const state = {
    providers: [
        { id: "assistant", name: "OpenAI", model: "gpt-5", models: ["gpt-5", "gpt-5-mini"], supports_vision: true, connection_test_passed: true },
        { id: "coding", name: "DeepSeek", model: "deepseek-coder", models: ["deepseek-coder"], supports_vision: false, connection_test_passed: true },
    ],
    profiles: {
        version: 1,
        assistant: { provider_id: "assistant", model: "gpt-5" },
        coding: { provider_id: "coding", model: "deepseek-coder", inherit_assistant: false },
    },
    assistant: { profile: "assistant", provider_id: "assistant", provider_name: "OpenAI", model: "gpt-5", health: "configured" },
    coding: { profile: "coding", provider_id: "coding", provider_name: "DeepSeek", model: "deepseek-coder", health: "configured" },
    revision: "rev-1",
};

const followingState = {
    ...state,
    profiles: {
        ...state.profiles,
        coding: { inherit_assistant: true },
    },
    coding: { ...state.assistant, profile: "coding", inherit_assistant: true },
};

function footerSaveButton(): HTMLButtonElement {
    const button = document.querySelector<HTMLButtonElement>(".llm-profile-footer .llm-config-btn--primary");
    if (!button) throw new Error("assignment save button missing");
    return button;
}

describe("LLMProfileAssignments", () => {
    beforeEach(() => {
        getState.mockReset();
        saveProfiles.mockReset();
        testProfile.mockReset();
        fetchProfileModels.mockReset();
        fetchProfileModels.mockResolvedValue([]);
        eventsOn.mockReset();
        eventsOff.mockReset();
    });

    it("does not render a duplicate provider-management action", async () => {
        getState.mockResolvedValue(state);
        render(<LLMProfileAssignments lang="en" />);

        await screen.findByRole("heading", { name: "Model assignments" });
        expect(screen.queryByRole("button", { name: "Manage providers" })).toBeNull();
    });

    it("renders a description action after the assignment help text", async () => {
        getState.mockResolvedValue(state);
        render(<LLMProfileAssignments lang="en" descriptionAction={<button type="button">Import other agents</button>} />);

        const help = await screen.findByText("Providers that passed a connection test, plus the provider currently in use. Connections and credentials are managed separately.");
        const action = screen.getByRole("button", { name: "Import other agents" });
        expect(help.contains(action)).toBe(false);
        expect(help.compareDocumentPosition(action) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    });

    it("keeps the description action available while assignments are loading", () => {
        getState.mockReturnValue(new Promise(() => {}));
        render(<LLMProfileAssignments lang="en" descriptionAction={<button type="button">Import other agents</button>} />);

        const section = screen.getByRole("region", { name: "Model assignments" });
        expect(screen.getByRole("button", { name: "Import other agents" })).toBeTruthy();
        expect(screen.getByRole("status").textContent).toBe("Loading model assignments…");
        expect(section.getAttribute("aria-busy")).toBeNull();
    });

    it("keeps the description action available when assignments fail to load", async () => {
        getState.mockRejectedValue(new Error("unavailable"));
        render(<LLMProfileAssignments lang="en" descriptionAction={<button type="button">Import other agents</button>} />);

        expect((await screen.findByRole("alert")).textContent).toBe("Error: unavailable");
        expect(screen.getByRole("button", { name: "Import other agents" })).toBeTruthy();
    });

    it("lists persisted and live provider models in the assignment picker", async () => {
        fetchProfileModels.mockImplementation(async (providerID: string) => {
            if (providerID === "assistant") return [{ id: "gpt-5-nano" }];
            if (providerID === "coding") return [{ id: "deepseek-chat" }];
            return [];
        });
        getState.mockResolvedValue(state);
        render(<LLMProfileAssignments lang="en" />);

        await screen.findByRole("heading", { name: "Model assignments" });
        await waitFor(() => expect(fetchProfileModels).toHaveBeenCalledWith("assistant"));
        const optionValues = (id: string) => Array.from(document.getElementById(id)?.querySelectorAll("[data-value]") || []).map((node) => node.getAttribute("data-value"));
        const openList = (id: string) => {
            const toggle = document.querySelector<HTMLButtonElement>(`button[aria-controls="${id}"]`);
            expect(toggle).toBeTruthy();
            if (toggle?.getAttribute("aria-expanded") !== "true") fireEvent.click(toggle!);
        };
        openList("assistant-profile-models");
        await waitFor(() => expect(optionValues("assistant-profile-models")).toEqual(expect.arrayContaining(["gpt-5", "gpt-5-mini", "gpt-5-nano"])));
        openList("coding-profile-models");
        await waitFor(() => expect(optionValues("coding-profile-models")).toEqual(expect.arrayContaining(["deepseek-coder", "deepseek-chat"])));
    });

    it("switches the assistant to the saved provider, focuses the model, and saves that draft", async () => {
        const scrollIntoView = vi.spyOn(HTMLElement.prototype, "scrollIntoView").mockImplementation(() => {});
        getState.mockResolvedValue(state);
        saveProfiles.mockResolvedValue(undefined);
        const request: AgentProviderSwitchTarget = { providerName: "DeepSeek", token: 1 };
        try {
            render(<LLMProfileAssignments lang="en" agentProviderSwitch={request} />);

            const provider = await screen.findByLabelText("Assistant provider") as HTMLSelectElement;
            await waitFor(() => expect(provider.value).toBe("coding"));
            const model = screen.getByLabelText("Assistant model") as HTMLInputElement;
            expect(model.value).toBe("deepseek-coder");
            expect(screen.getByTestId("assistant-provider-switch-hint").textContent).toContain("DeepSeek");
            expect(screen.getByTestId("assistant-provider-switch-hint").textContent).toContain("Choose a model and save");
            await waitFor(() => expect(document.activeElement).toBe(model));
            expect(scrollIntoView.mock.instances).toContain(model);
            expect(scrollIntoView).toHaveBeenCalledWith({ block: "center" });
            expect(scrollIntoView.mock.instances.some(node => node instanceof HTMLElement && node.id === "llm-assistant-provider-switch")).toBe(false);
        } finally {
            scrollIntoView.mockRestore();
        }

        fireEvent.click(footerSaveButton());
        await waitFor(() => expect(saveProfiles).toHaveBeenCalledWith(expect.objectContaining({
            assistant: expect.objectContaining({ provider_id: "coding", model: "deepseek-coder" }),
        }), "rev-1"));
        await waitFor(() => expect(screen.queryByTestId("assistant-provider-switch-hint")).toBeNull());
    });

    it("focuses the model again after StrictMode clears the first timer", async () => {
        getState.mockResolvedValue(state);
        render(
            <StrictMode>
                <LLMProfileAssignments lang="en" agentProviderSwitch={{ providerName: "DeepSeek", model: "deepseek-v4-flash", token: 3 }} />
            </StrictMode>,
        );

        const model = await screen.findByLabelText("Assistant model") as HTMLInputElement;
        await waitFor(() => expect(model.value).toBe("deepseek-v4-flash"));
        await waitFor(() => expect(document.activeElement).toBe(model));
        expect((screen.getByLabelText("Assistant provider") as HTMLSelectElement).value).toBe("coding");
    });

    it("does not pull focus back to the model when a later directory refresh arrives", async () => {
        getState.mockResolvedValueOnce(state).mockResolvedValueOnce({
            ...state,
            providers: [...state.providers, { id: "extra", name: "Extra", model: "x", models: ["x"], connection_test_passed: true }],
        });
        const request: AgentProviderSwitchTarget = { providerName: "DeepSeek", model: "deepseek-v4-flash", token: 8 };
        const view = render(<LLMProfileAssignments lang="en" providerListRevision={0} agentProviderSwitch={request} />);
        const model = await screen.findByLabelText("Assistant model") as HTMLInputElement;
        await waitFor(() => expect(document.activeElement).toBe(model));
        const save = footerSaveButton();
        await act(async () => { save.focus(); });
        expect(document.activeElement).toBe(save);

        view.rerender(<LLMProfileAssignments lang="en" providerListRevision={1} agentProviderSwitch={request} />);
        await waitFor(() => expect(getState).toHaveBeenCalledTimes(2));
        await act(async () => { await new Promise(resolve => setTimeout(resolve, 80)); });
        expect(document.activeElement).toBe(save);
        expect(model.value).toBe("deepseek-v4-flash");
    });

    it("applies the model that just passed Test and Save instead of the stale directory model", async () => {
        getState.mockResolvedValue(state);
        saveProfiles.mockResolvedValue(undefined);
        const request: AgentProviderSwitchTarget = { providerName: "DeepSeek", model: "deepseek-v4-flash", token: 7 };
        render(<LLMProfileAssignments lang="en" agentProviderSwitch={request} />);

        const provider = await screen.findByLabelText("Assistant provider") as HTMLSelectElement;
        await waitFor(() => expect(provider.value).toBe("coding"));
        const model = screen.getByLabelText("Assistant model") as HTMLInputElement;
        expect(model.value).toBe("deepseek-v4-flash");
        expect((screen.getByLabelText("Coding model") as HTMLInputElement).value).toBe("deepseek-coder");
        const toggle = document.querySelector<HTMLButtonElement>('button[aria-controls="assistant-profile-models"]');
        if (toggle?.getAttribute("aria-expanded") !== "true") fireEvent.click(toggle!);
        const optionValues = () => Array.from(document.getElementById("assistant-profile-models")?.querySelectorAll("[data-value]") || []).map(node => node.getAttribute("data-value"));
        await waitFor(() => expect(optionValues()[0]).toBe("deepseek-v4-flash"));
        expect(optionValues()).toEqual(expect.arrayContaining(["deepseek-v4-flash", "deepseek-coder"]));

        fireEvent.click(footerSaveButton());
        await waitFor(() => expect(saveProfiles).toHaveBeenCalledWith(expect.objectContaining({
            assistant: expect.objectContaining({ provider_id: "coding", model: "deepseek-v4-flash" }),
            coding: expect.objectContaining({ provider_id: "coding", model: "deepseek-coder" }),
        }), "rev-1"));
    });

    it("follows the model the user picks and saves from the hint next to that field", async () => {
        getState.mockResolvedValue(state);
        saveProfiles.mockResolvedValue(undefined);
        render(<LLMProfileAssignments lang="en" agentProviderSwitch={{ providerName: "DeepSeek", model: "deepseek-v4-flash", token: 12 }} />);

        const hint = await screen.findByTestId("assistant-provider-switch-hint");
        await waitFor(() => expect(hint.textContent).toContain("deepseek-v4-flash"));
        const statusText = () => hint.querySelector("[role='status']")?.textContent || "";
        const announced = statusText();
        expect(announced).toContain("not saved yet");
        expect(announced).not.toContain("deepseek-v4-flash");
        fireEvent.change(screen.getByLabelText("Assistant model"), { target: { value: "deepseek-coder" } });
        expect(statusText()).toBe(announced);
        expect(hint.textContent).toContain("deepseek-coder");
        expect(hint.textContent).not.toContain("deepseek-v4-flash");

        const hintSave = hint.querySelector("button");
        expect(hintSave?.textContent).toBe("Save changes");
        fireEvent.click(hintSave!);
        await waitFor(() => expect(saveProfiles).toHaveBeenCalledWith(expect.objectContaining({
            assistant: expect.objectContaining({ provider_id: "coding", model: "deepseek-coder" }),
        }), "rev-1"));
    });

    it("hides the switch hint after the assistant draft matches the saved assignment again", async () => {
        getState.mockResolvedValue(state);
        render(<LLMProfileAssignments lang="en" agentProviderSwitch={{ providerName: "DeepSeek", model: "deepseek-v4-flash", token: 14 }} />);
        await screen.findByTestId("assistant-provider-switch-hint");

        fireEvent.change(screen.getByLabelText("Assistant provider"), { target: { value: "assistant" } });
        expect(screen.queryByTestId("assistant-provider-switch-hint")).toBeNull();

        fireEvent.change(screen.getByLabelText("Caption provider"), { target: { value: "coding" } });
        expect(screen.queryByTestId("assistant-provider-switch-hint")).toBeNull();
        expect(footerSaveButton().disabled).toBe(false);
    });

    it("previews the foreground follow from the unsaved assistant draft", async () => {
        getState.mockResolvedValue({
            ...state,
            foreground: { model: "gpt-5", inherit_hub: true, provider_id: "assistant", provider_name: "OpenAI", configured: true },
        });
        render(<LLMProfileAssignments lang="en" agentProviderSwitch={{ providerName: "DeepSeek", model: "deepseek-v4-flash", token: 15 }} />);

        await waitFor(() => expect((screen.getByLabelText("Assistant model") as HTMLInputElement).value).toBe("deepseek-v4-flash"));
        expect(screen.getByText("Effective after save: DeepSeek · deepseek-v4-flash")).toBeTruthy();
        expect(screen.queryByText("Effective now: OpenAI · gpt-5")).toBeNull();
    });

    it("lets a later directory refresh replace a handoff that did not change the draft", async () => {
        const already = {
            ...state,
            profiles: {
                ...state.profiles,
                assistant: { provider_id: "coding", model: "deepseek-coder" },
            },
            assistant: { profile: "assistant", provider_id: "coding", provider_name: "DeepSeek", model: "deepseek-coder", health: "configured" },
        };
        const refreshed = {
            ...already,
            profiles: {
                ...already.profiles,
                assistant: { provider_id: "coding", model: "deepseek-chat" },
            },
            assistant: { ...already.assistant, model: "deepseek-chat" },
        };
        getState.mockResolvedValueOnce(already).mockResolvedValue(refreshed);
        const view = render(<LLMProfileAssignments lang="en" providerListRevision={0} agentProviderSwitch={{ providerName: "DeepSeek", model: "deepseek-coder", token: 9 }} />);

        await waitFor(() => expect((screen.getByLabelText("Assistant model") as HTMLInputElement).value).toBe("deepseek-coder"));
        expect(screen.queryByTestId("assistant-provider-switch-hint")).toBeNull();
        expect(footerSaveButton().disabled).toBe(true);

        view.rerender(<LLMProfileAssignments lang="en" providerListRevision={1} agentProviderSwitch={{ providerName: "DeepSeek", model: "deepseek-coder", token: 9 }} />);
        await waitFor(() => expect((screen.getByLabelText("Assistant model") as HTMLInputElement).value).toBe("deepseek-chat"));
    });

    it("keeps the handoff draft when a profile reload started before the switch returns", async () => {
        let resolveReload: (value: unknown) => void = () => {};
        getState.mockResolvedValueOnce(state).mockImplementationOnce(() => new Promise(resolve => {
            resolveReload = resolve;
        }));
        let onProfiles: ((payload?: { changed?: string }) => void) | undefined;
        eventsOn.mockImplementation((name: string, handler: (payload?: { changed?: string }) => void) => {
            if (name === "llm-profiles-changed") onProfiles = handler;
            return () => {};
        });
        const view = render(<LLMProfileAssignments lang="en" />);
        await screen.findByLabelText("Assistant provider");

        act(() => { onProfiles?.({ changed: "profiles" }); });
        expect(await screen.findByText("Loading model assignments…")).toBeTruthy();
        view.rerender(<LLMProfileAssignments lang="en" agentProviderSwitch={{ providerName: "DeepSeek", model: "deepseek-v4-flash", token: 11 }} />);
        await act(async () => { resolveReload(state); });

        const model = await screen.findByLabelText("Assistant model") as HTMLInputElement;
        await waitFor(() => expect(model.value).toBe("deepseek-v4-flash"));
        expect((screen.getByLabelText("Assistant provider") as HTMLSelectElement).value).toBe("coding");
    });

    it("waits until the saved provider is in the assignment list before switching", async () => {
        getState
            .mockResolvedValueOnce({ ...state, providers: [state.providers[0]] })
            .mockResolvedValueOnce(state);
        const view = render(<LLMProfileAssignments lang="en" providerListRevision={0} agentProviderSwitch={{ providerName: "DeepSeek", token: 4 }} />);

        const provider = await screen.findByLabelText("Assistant provider") as HTMLSelectElement;
        expect(provider.value).toBe("assistant");
        expect(screen.queryByTestId("assistant-provider-switch-hint")).toBeNull();

        view.rerender(<LLMProfileAssignments lang="en" providerListRevision={1} agentProviderSwitch={{ providerName: "DeepSeek", token: 4 }} />);
        await waitFor(() => expect(provider.value).toBe("coding"));
        expect(screen.getByTestId("assistant-provider-switch-hint").textContent).toContain("DeepSeek");
    });

    it("keeps the provider's saved model when its live catalog starts with default-model", async () => {
        fetchProfileModels.mockImplementation(async (providerID: string) => {
            if (providerID === "workbuddy") {
                return [{ id: "default-model" }, { id: "deepseek-v4.1-flash" }, { id: "gpt-5.4" }];
            }
            return [];
        });
        getState.mockResolvedValue({
            ...state,
            providers: [
                ...state.providers,
                {
                    id: "workbuddy",
                    name: "WorkBuddy",
                    model: "deepseek-v4.1-flash",
                    models: ["deepseek-v4.1-flash", "gpt-5.4"],
                    connection_test_passed: true,
                },
            ],
            profiles: {
                ...state.profiles,
                assistant: { provider_id: "workbuddy", model: "deepseek-v4.1-flash" },
            },
        });
        render(<LLMProfileAssignments lang="en" />);
        const provider = await screen.findByLabelText("Assistant provider") as HTMLSelectElement;
        await waitFor(() => expect(fetchProfileModels).toHaveBeenCalledWith("workbuddy"));
        fireEvent.change(provider, { target: { value: "assistant" } });
        fireEvent.change(provider, { target: { value: "workbuddy" } });
        expect((screen.getByLabelText("Assistant model") as HTMLInputElement).value).toBe("deepseek-v4.1-flash");
    });

    it("lists every model from the dropdown button and filters only after typing", async () => {
        getState.mockResolvedValue({
            ...state,
            providers: [{
                id: "assistant",
                name: "WorkBuddy",
                model: "default-model",
                models: ["default-model", "gpt-5.4", "glm-5.3"],
                supports_vision: true,
                connection_test_passed: true,
            }],
            profiles: {
                ...state.profiles,
                assistant: { provider_id: "assistant", model: "default-model" },
            },
        });
        render(<LLMProfileAssignments lang="en" />);

        const input = await screen.findByLabelText("Assistant model") as HTMLInputElement;
        expect(input.value).toBe("default-model");
        const optionValues = () => Array.from(document.getElementById("assistant-profile-models")?.querySelectorAll("[data-value]") || []).map((node) => node.getAttribute("data-value"));
        const toggle = () => document.querySelector<HTMLButtonElement>('button[aria-controls="assistant-profile-models"]')!;
        fireEvent.click(toggle());
        fireEvent.change(input, { target: { value: "default-model" } });
        expect(optionValues()).toEqual(["default-model", "gpt-5.4", "glm-5.3"]);

        fireEvent.change(input, { target: { value: "glm" } });
        expect(optionValues()).toEqual(["glm-5.3"]);

        fireEvent.click(screen.getByRole("option", { name: "glm-5.3" }));
        expect(input.value).toBe("glm-5.3");
        expect(document.getElementById("assistant-profile-models")).toBeNull();

        fireEvent.click(toggle());
        expect(optionValues()).toEqual(["default-model", "gpt-5.4", "glm-5.3"]);
    });

    it("renders only the connection-tested providers returned by the assignment API", async () => {
        getState.mockResolvedValue({
            ...state,
            providers: [state.providers[0]],
        });
        render(<LLMProfileAssignments lang="en" />);

        await screen.findByRole("heading", { name: "Model assignments" });
        const options = Array.from((screen.getByLabelText("Assistant provider") as HTMLSelectElement).options).map(option => option.text);
        expect(options).toEqual(["Select provider", "OpenAI"]);
        expect(screen.getByText("Providers that passed a connection test, plus the provider currently in use. Connections and credentials are managed separately.")).toBeTruthy();
    });

    it("explains how to make providers available when none have passed a test", async () => {
        getState.mockResolvedValue({ ...state, providers: [] });
        render(<LLMProfileAssignments lang="en" />);

        expect((await screen.findByText("No eligible providers yet. Test and save a provider in Provider management, or keep using the current assistant provider.")).textContent).toBeTruthy();
    });

    it("refreshes the eligible provider directory after Provider management confirms a test without discarding a draft", async () => {
        getState
            .mockResolvedValueOnce({ ...state, providers: [state.providers[0]] })
            .mockResolvedValueOnce({ ...state, revision: "rev-after-test" })
            // Save reloads the authoritative snapshot once more; retain the
            // new revision in this response so the test exercises a complete
            // successful save rather than falling through to an empty mock.
            .mockResolvedValueOnce({ ...state, revision: "rev-after-test" });
        saveProfiles.mockResolvedValue(undefined);
        const view = render(<LLMProfileAssignments lang="en" providerListRevision={0} />);

        fireEvent.change(await screen.findByLabelText("Coding provider"), { target: { value: "assistant" } });
        view.rerender(<LLMProfileAssignments lang="en" providerListRevision={1} />);

        await waitFor(() => expect(getState).toHaveBeenCalledTimes(2));
        expect(Array.from((screen.getByLabelText("Assistant provider") as HTMLSelectElement).options).map(option => option.text))
            .toEqual(["Select provider", "OpenAI", "DeepSeek"]);
        expect((screen.getByLabelText("Coding provider") as HTMLSelectElement).value).toBe("assistant");
        expect(screen.getByText("Unsaved changes")).toBeTruthy();
        fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
        await waitFor(() => expect(saveProfiles).toHaveBeenCalledWith(expect.anything(), "rev-after-test"));
    });

    it("keeps coding independent and sends the profile revision on save", async () => {
        getState.mockResolvedValue(state);
        saveProfiles.mockResolvedValue(undefined);
        render(<LLMProfileAssignments lang="en" />);

        fireEvent.change(await screen.findByLabelText("Coding provider"), { target: { value: "assistant" } });
        fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

        await waitFor(() => expect(saveProfiles).toHaveBeenCalledWith(expect.objectContaining({
            coding: expect.objectContaining({ provider_id: "assistant", model: "gpt-5", inherit_assistant: false }),
        }), "rev-1"));
    });

    it("hides coding selectors while following the assistant without erasing its saved draft", async () => {
        getState.mockResolvedValue(state);
        render(<LLMProfileAssignments lang="en" />);
        const follow = await screen.findByRole("checkbox", { name: "Follow AI assistant" });
        fireEvent.click(follow);
        expect(screen.queryByLabelText("Coding provider")).toBeNull();
        expect(screen.getAllByRole("button", { name: "Test connection" })).toHaveLength(2);
        expect(screen.getByText("Effective after save: OpenAI · gpt-5")).toBeTruthy();
        fireEvent.change(screen.getByLabelText("Assistant model"), { target: { value: "gpt-5-mini" } });
        expect(screen.getByText("Effective after save: OpenAI · gpt-5-mini")).toBeTruthy();
        fireEvent.click(follow);
        expect((screen.getByLabelText("Coding provider") as HTMLSelectElement).value).toBe("coding");
    });

    it("distinguishes the current inherited choice from an unsaved follow preview", async () => {
        getState.mockResolvedValue(followingState);
        render(<LLMProfileAssignments lang="en" />);

        expect(await screen.findByText("Effective now: OpenAI · gpt-5")).toBeTruthy();
        fireEvent.change(screen.getByLabelText("Assistant model"), { target: { value: "gpt-5-mini" } });
        expect(screen.getByText("Effective after save: OpenAI · gpt-5-mini")).toBeTruthy();
    });

    it("tests the unsaved profile draft without persisting it", async () => {
        getState.mockResolvedValue(state);
        testProfile.mockResolvedValue({ profile: "coding", health: "unavailable", reason_code: "authentication_failed" });
        render(<LLMProfileAssignments lang="en" />);

        fireEvent.change(await screen.findByLabelText("Coding provider"), { target: { value: "assistant" } });
        fireEvent.click(screen.getAllByRole("button", { name: "Test connection" })[1]);

        await waitFor(() => expect(testProfile).toHaveBeenCalledWith("coding", "assistant", "gpt-5"));
        expect(saveProfiles).not.toHaveBeenCalled();
        expect(screen.getByText("Unavailable")).toBeTruthy();
    });

    it("clears a stale connection result when its draft selection changes", async () => {
        getState.mockResolvedValue(state);
        testProfile.mockResolvedValue({ profile: "assistant", health: "configured" });
        render(<LLMProfileAssignments lang="en" />);

        fireEvent.click((await screen.findAllByRole("button", { name: "Test connection" }))[0]);
        expect(await screen.findByText("Connected")).toBeTruthy();

        fireEvent.change(screen.getByLabelText("Assistant model"), { target: { value: "gpt-5-mini" } });
        expect(screen.queryByText("Connected")).toBeNull();
    });

    it("does not apply a late probe result after its draft has changed", async () => {
        let resolveProbe: ((value: unknown) => void) | undefined;
        getState.mockResolvedValue(state);
        testProfile.mockImplementation(() => new Promise(resolve => { resolveProbe = resolve; }));
        render(<LLMProfileAssignments lang="en" />);

        fireEvent.click((await screen.findAllByRole("button", { name: "Test connection" }))[0]);
        fireEvent.change(screen.getByLabelText("Assistant model"), { target: { value: "gpt-5-mini" } });
        await act(async () => { resolveProbe?.({ profile: "assistant", health: "configured" }); });

        expect(screen.queryByText("Connected")).toBeNull();
    });

    it("leaves the loading state when the panel read itself refreshes providers", async () => {
        let resolveInitial: ((value: typeof state) => void) | undefined;
        let onProfilesChanged: ((payload?: { changed?: string }) => void) | undefined;
        eventsOn.mockImplementation((name: string, handler: (payload?: { changed?: string }) => void) => {
            if (name === "llm-profiles-changed") onProfilesChanged = handler;
            return vi.fn();
        });
        getState
            .mockImplementationOnce(() => new Promise(resolve => { resolveInitial = resolve; }))
            .mockResolvedValueOnce(state);
        render(<LLMProfileAssignments lang="en" />);

        expect(screen.getByText("Loading model assignments…")).toBeTruthy();
        act(() => { onProfilesChanged?.({ changed: "hub-provider" }); });
        await act(async () => { await Promise.resolve(); });
        await act(async () => { resolveInitial?.(state); });

        expect(await screen.findByRole("heading", { name: "Model assignments" })).toBeTruthy();
        expect(screen.queryByText("Loading model assignments…")).toBeNull();
    });

    it("keeps the newest profile snapshot when overlapping reloads finish out of order", async () => {
        let resolveInitial: ((value: typeof state) => void) | undefined;
        let resolveRefresh: ((value: typeof state) => void) | undefined;
        let onProfilesChanged: (() => void) | undefined;
        eventsOn.mockImplementation((name: string, handler: () => void) => {
            if (name === "llm-profiles-changed") onProfilesChanged = handler;
            return vi.fn();
        });
        getState
            .mockImplementationOnce(() => new Promise(resolve => { resolveInitial = resolve; }))
            .mockImplementationOnce(() => new Promise(resolve => { resolveRefresh = resolve; }));
        render(<LLMProfileAssignments lang="en" />);

        act(() => { onProfilesChanged?.(); });
        await waitFor(() => expect(getState).toHaveBeenCalledTimes(2));
        await act(async () => { resolveRefresh?.({ ...state, revision: "rev-2" }); });
        expect(await screen.findByRole("heading", { name: "Model assignments" })).toBeTruthy();
        await act(async () => { resolveInitial?.({ ...state, revision: "rev-1", profiles: { ...state.profiles, assistant: { provider_id: "assistant", model: "stale-model" } } }); });

        fireEvent.change(screen.getByLabelText("Coding provider"), { target: { value: "assistant" } });
        fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
        await waitFor(() => expect(saveProfiles).toHaveBeenCalledWith(expect.anything(), "rev-2"));
    });

    it("keeps an unsaved draft when another entry changes profiles and offers a refresh", async () => {
        let onProfilesChanged: (() => void) | undefined;
        eventsOn.mockImplementation((name: string, handler: () => void) => {
            if (name === "llm-profiles-changed") onProfilesChanged = handler;
            return vi.fn();
        });
        getState.mockResolvedValue(state);
        render(<LLMProfileAssignments lang="en" />);

        fireEvent.change(await screen.findByLabelText("Coding provider"), { target: { value: "assistant" } });
        act(() => { onProfilesChanged?.(); });

        expect(screen.getByText("Model assignments changed elsewhere. Refresh before saving.")).toBeTruthy();
        expect((screen.getByLabelText("Coding provider") as HTMLSelectElement).value).toBe("assistant");
        fireEvent.click(screen.getByRole("button", { name: "Refresh draft" }));
        await waitFor(() => expect(getState).toHaveBeenCalledTimes(2));
        expect((screen.getByLabelText("Coding provider") as HTMLSelectElement).value).toBe("coding");
    });

    it("updates provider maintenance events without treating them as assignment conflicts", async () => {
        let onProfilesChanged: ((payload?: { changed?: string }) => void) | undefined;
        eventsOn.mockImplementation((name: string, handler: (payload?: { changed?: string }) => void) => {
            if (name === "llm-profiles-changed") onProfilesChanged = handler;
            return vi.fn();
        });
        getState
            .mockResolvedValueOnce({ ...state, providers: [state.providers[0]] })
            .mockResolvedValueOnce(state);
        render(<LLMProfileAssignments lang="en" />);

        fireEvent.change(await screen.findByLabelText("Coding provider"), { target: { value: "assistant" } });
        act(() => { onProfilesChanged?.({ changed: "providers" }); });

        await waitFor(() => expect(getState).toHaveBeenCalledTimes(2));
        expect(screen.queryByText("Model assignments changed elsewhere. Refresh before saving.")).toBeNull();
        expect((screen.getByLabelText("Coding provider") as HTMLSelectElement).value).toBe("assistant");
        expect(Array.from((screen.getByLabelText("Assistant provider") as HTMLSelectElement).options).map(option => option.text))
            .toEqual(["Select provider", "OpenAI", "DeepSeek"]);
    });

    it("coalesces the Test & Save callback and provider event into one refresh", async () => {
        let onProfilesChanged: ((payload?: { changed?: string }) => void) | undefined;
        eventsOn.mockImplementation((name: string, handler: (payload?: { changed?: string }) => void) => {
            if (name === "llm-profiles-changed") onProfilesChanged = handler;
            return vi.fn();
        });
        getState
            .mockResolvedValueOnce({ ...state, providers: [state.providers[0]] })
            .mockResolvedValueOnce(state);
        const view = render(<LLMProfileAssignments lang="en" providerListRevision={0} />);

        await screen.findByRole("heading", { name: "Model assignments" });
        act(() => {
            view.rerender(<LLMProfileAssignments lang="en" providerListRevision={1} />);
            onProfilesChanged?.({ changed: "providers" });
        });

        await waitFor(() => expect(getState).toHaveBeenCalledTimes(2));
    });

    it("re-reads eligibility when another provider update arrives during a refresh", async () => {
        let onProfilesChanged: ((payload?: { changed?: string }) => void) | undefined;
        let resolveFirstRefresh: ((value: typeof state) => void) | undefined;
        let resolveSecondRefresh: ((value: typeof state) => void) | undefined;
        eventsOn.mockImplementation((name: string, handler: (payload?: { changed?: string }) => void) => {
            if (name === "llm-profiles-changed") onProfilesChanged = handler;
            return vi.fn();
        });
        getState
            .mockResolvedValueOnce(state)
            .mockImplementationOnce(() => new Promise(resolve => { resolveFirstRefresh = resolve; }))
            .mockImplementationOnce(() => new Promise(resolve => { resolveSecondRefresh = resolve; }));
        saveProfiles.mockResolvedValue(undefined);
        render(<LLMProfileAssignments lang="en" />);

        await screen.findByRole("heading", { name: "Model assignments" });
        act(() => { onProfilesChanged?.({ changed: "providers" }); });
        await waitFor(() => expect(getState).toHaveBeenCalledTimes(2));
        act(() => { onProfilesChanged?.({ changed: "providers" }); });
        expect(getState).toHaveBeenCalledTimes(2);

        await act(async () => { resolveFirstRefresh?.({ ...state, revision: "rev-2" }); });
        await waitFor(() => expect(getState).toHaveBeenCalledTimes(3));
        await act(async () => { resolveSecondRefresh?.({ ...state, revision: "rev-3" }); });

        fireEvent.change(screen.getByLabelText("Coding provider"), { target: { value: "assistant" } });
        fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
        await waitFor(() => expect(saveProfiles).toHaveBeenCalledWith(expect.anything(), "rev-3"));
    });

    it("keeps a provider maintenance refresh from replacing a later profile change", async () => {
        let onProfilesChanged: ((payload?: { changed?: string }) => void) | undefined;
        let resolveProfileRefresh: ((value: typeof state) => void) | undefined;
        eventsOn.mockImplementation((name: string, handler: (payload?: { changed?: string }) => void) => {
            if (name === "llm-profiles-changed") onProfilesChanged = handler;
            return vi.fn();
        });
        getState
            .mockResolvedValueOnce(state)
            .mockImplementationOnce(() => new Promise(resolve => { resolveProfileRefresh = resolve; }));
        render(<LLMProfileAssignments lang="en" />);

        await screen.findByRole("heading", { name: "Model assignments" });
        act(() => { onProfilesChanged?.({ changed: "providers" }); });
        act(() => { onProfilesChanged?.({ changed: "profiles" }); });
        await waitFor(() => expect(getState).toHaveBeenCalledTimes(2));

        await act(async () => { resolveProfileRefresh?.({ ...state, revision: "rev-2" }); });

        fireEvent.change(screen.getByLabelText("Coding provider"), { target: { value: "assistant" } });
        fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
        await waitFor(() => expect(saveProfiles).toHaveBeenCalledWith(expect.anything(), "rev-2"));
    });

    it("saves an optional caption model independently of assistant and coding", async () => {
        getState.mockResolvedValue(state);
        saveProfiles.mockResolvedValue(undefined);
        render(<LLMProfileAssignments lang="en" />);

        fireEvent.change(await screen.findByLabelText("Caption provider"), { target: { value: "assistant" } });
        fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

        await waitFor(() => expect(saveProfiles).toHaveBeenCalledWith(expect.objectContaining({
            caption: expect.objectContaining({ provider_id: "assistant", model: "gpt-5" }),
            coding: expect.objectContaining({ provider_id: "coding" }),
        }), "rev-1"));
    });

    it("warns when the caption provider is not marked vision-capable", async () => {
        getState.mockResolvedValue(state);
        render(<LLMProfileAssignments lang="en" />);

        fireEvent.change(await screen.findByLabelText("Caption provider"), { target: { value: "coding" } });
        expect(screen.getByText("This model was not marked vision-capable. Captioning unlabeled boxes needs a vision model.")).toBeTruthy();
    });

    it("warns when the caption model is not in the provider vision list", async () => {
        getState.mockResolvedValue({
            ...state,
            providers: [
                { id: "assistant", name: "OpenAI", model: "gpt-5", models: ["gpt-5", "gpt-5-mini"], supports_vision: true, vision_models: ["gpt-5"], connection_test_passed: true },
                { id: "coding", name: "DeepSeek", model: "deepseek-coder", models: ["deepseek-coder"], supports_vision: false, connection_test_passed: true },
            ],
        });
        render(<LLMProfileAssignments lang="en" />);

        fireEvent.change(await screen.findByLabelText("Caption provider"), { target: { value: "assistant" } });
        fireEvent.change(screen.getByLabelText("Caption model"), { target: { value: "gpt-5-mini" } });
        expect(screen.getByText("This model's image support has not been tested. Captioning unlabeled boxes needs a vision model.")).toBeTruthy();
        expect(screen.getAllByRole("button", { name: "Test connection and image support" }).length).toBeGreaterThan(0);
    });

    it("tests image support for an untested assigned model without saving the assignment", async () => {
        getState.mockResolvedValue({
            ...state,
            providers: [
                { id: "assistant", name: "OpenAI", model: "gpt-5", models: ["gpt-5", "gpt-5-mini"], supports_vision: true, vision_models: ["gpt-5"], vision_tested_models: ["gpt-5"], connection_test_passed: true },
                state.providers[1],
            ],
        });
        testProfile.mockResolvedValue({ profile: "assistant", health: "configured", supports_vision: true, vision_probe_status: "supported" });
        render(<LLMProfileAssignments lang="en" />);

        fireEvent.change(await screen.findByLabelText("Assistant model"), { target: { value: "gpt-5-mini" } });
        expect(screen.getByText("Vision support: not tested")).toBeTruthy();
        fireEvent.click(screen.getByRole("button", { name: "Test connection and image support" }));

        await waitFor(() => expect(testProfile).toHaveBeenCalledWith("assistant", "assistant", "gpt-5-mini"));
        expect(saveProfiles).not.toHaveBeenCalled();
        expect(screen.getByText("Connected")).toBeTruthy();
        expect(screen.getByText("Vision support: enabled")).toBeTruthy();
        expect(screen.queryByText("Vision support: not tested")).toBeNull();
        expect(screen.queryByRole("button", { name: "Test connection and image support" })).toBeNull();
    });

    it("accepts Wails PascalCase vision fields from an assignment probe", async () => {
        getState.mockResolvedValue({
            ...state,
            providers: [
                { id: "assistant", name: "OpenAI", model: "gpt-5", models: ["gpt-5", "gpt-5-mini"], supports_vision: true, vision_models: ["gpt-5"], vision_tested_models: ["gpt-5"], connection_test_passed: true },
                state.providers[1],
            ],
        });
        testProfile.mockResolvedValue({ profile: "assistant", health: "configured", SupportsVision: true, VisionProbeStatus: "supported" });
        render(<LLMProfileAssignments lang="en" />);

        fireEvent.change(await screen.findByLabelText("Assistant model"), { target: { value: "gpt-5-mini" } });
        fireEvent.click(screen.getByRole("button", { name: "Test connection and image support" }));

        expect(await screen.findByText("Vision support: enabled")).toBeTruthy();
        expect(screen.queryByRole("button", { name: "Test connection and image support" })).toBeNull();
    });

    it("keeps image testing available when a vision result cannot be saved", async () => {
        getState.mockResolvedValue({
            ...state,
            providers: [
                { id: "assistant", name: "OpenAI", model: "gpt-5", models: ["gpt-5", "gpt-5-mini"], supports_vision: true, vision_models: ["gpt-5"], vision_tested_models: ["gpt-5"], connection_test_passed: true },
                state.providers[1],
            ],
        });
        testProfile.mockResolvedValue({
            profile: "assistant",
            health: "configured",
            supports_vision: true,
            vision_probe_status: "supported",
            vision_persist_failed: true,
        });
        render(<LLMProfileAssignments lang="en" />);

        fireEvent.change(await screen.findByLabelText("Assistant model"), { target: { value: "gpt-5-mini" } });
        fireEvent.click(screen.getByRole("button", { name: "Test connection and image support" }));

        expect(await screen.findByText("The image-support result could not be saved. Test it again.")).toBeTruthy();
        expect(screen.getByRole("button", { name: "Test connection and image support" })).toBeTruthy();
        expect(screen.getByText("Vision support: not confirmed; please retry")).toBeTruthy();
        expect(screen.queryByText("Vision support: enabled")).toBeNull();
    });

    it("rewrites the assignment draft to the probed model ID", async () => {
        getState.mockResolvedValue({
            ...state,
            providers: [
                { id: "assistant", name: "OpenAI", model: "gpt-5", models: ["gpt-5", "gpt-5-mini"], supports_vision: true, vision_models: ["gpt-5"], vision_tested_models: ["gpt-5"], connection_test_passed: true },
                state.providers[1],
            ],
        });
        testProfile.mockResolvedValue({
            profile: "assistant",
            health: "configured",
            supports_vision: true,
            vision_probe_status: "supported",
            provider_id: "assistant",
            model: "canonical-mini",
        });
        render(<LLMProfileAssignments lang="en" />);

        fireEvent.change(await screen.findByLabelText("Assistant model"), { target: { value: "gpt-5-mini" } });
        fireEvent.click(screen.getByRole("button", { name: "Test connection and image support" }));

        await waitFor(() => expect((screen.getByLabelText("Assistant model") as HTMLInputElement).value).toBe("canonical-mini"));
        expect(screen.getByText("Vision support: enabled")).toBeTruthy();
    });

    it("records an unsupported assignment vision result without treating it as capable", async () => {
        getState.mockResolvedValue({
            ...state,
            providers: [
                { id: "assistant", name: "OpenAI", model: "gpt-5", models: ["gpt-5", "gpt-5-mini"], supports_vision: true, vision_models: ["gpt-5"], vision_tested_models: ["gpt-5"], connection_test_passed: true },
                state.providers[1],
            ],
        });
        testProfile.mockResolvedValue({ profile: "assistant", health: "configured", supports_vision: false, vision_probe_status: "unsupported" });
        render(<LLMProfileAssignments lang="en" />);

        fireEvent.change(await screen.findByLabelText("Assistant model"), { target: { value: "gpt-5-mini" } });
        fireEvent.click(screen.getByRole("button", { name: "Test connection and image support" }));

        await waitFor(() => expect(screen.queryByRole("button", { name: "Test connection and image support" })).toBeNull());
        expect(screen.getAllByText("Vision support: disabled").length).toBeGreaterThan(0);
        fireEvent.change(screen.getByLabelText("Caption provider"), { target: { value: "assistant" } });
        fireEvent.change(screen.getByLabelText("Caption model"), { target: { value: "gpt-5-mini" } });
        expect(screen.getByText("This model was not marked vision-capable. Captioning unlabeled boxes needs a vision model.")).toBeTruthy();
    });

    it("does not keep a stale inconclusive vision result after the same model is confirmed", async () => {
        getState.mockResolvedValue({
            ...state,
            providers: [
                { id: "assistant", name: "OpenAI", model: "gpt-5", models: ["gpt-5", "gpt-5-mini"], supports_vision: true, vision_models: ["gpt-5"], vision_tested_models: ["gpt-5"], connection_test_passed: true },
                state.providers[1],
            ],
        });
        testProfile
            .mockResolvedValueOnce({ profile: "caption", health: "configured", vision_probe_status: "inconclusive" })
            .mockResolvedValueOnce({ profile: "assistant", health: "configured", supports_vision: true, vision_probe_status: "supported", model: "gpt-5-mini" });
        render(<LLMProfileAssignments lang="en" />);

        fireEvent.change(await screen.findByLabelText("Assistant model"), { target: { value: "gpt-5-mini" } });
        fireEvent.change(screen.getByLabelText("Caption provider"), { target: { value: "assistant" } });
        fireEvent.change(screen.getByLabelText("Caption model"), { target: { value: "gpt-5-mini" } });
        const visionButtons = screen.getAllByRole("button", { name: "Test connection and image support" });
        fireEvent.click(visionButtons[visionButtons.length - 1]);
        expect(await screen.findByText("Vision support: not confirmed; please retry")).toBeTruthy();

        fireEvent.click(screen.getAllByRole("button", { name: "Test connection and image support" })[0]);
        await waitFor(() => expect(screen.queryByText("Vision support: not confirmed; please retry")).toBeNull());
        expect(screen.getAllByText("Vision support: enabled").length).toBeGreaterThan(0);
    });

    it("keeps MaClaw Official model names on auto/low/mid/high when the live catalog changes", async () => {
        fetchProfileModels.mockResolvedValue([
            { id: "DeepSeek-V4-Flash" },
            { id: "official-mid" },
            { id: "gpt-4o" },
        ]);
        getState.mockResolvedValue({
            ...state,
            providers: [
                { id: "hub", name: "MaClaw Official", model: "official-mid", models: ["DeepSeek-V4-Flash"], is_hub_service: true, connection_test_passed: true },
            ],
            profiles: {
                version: 1,
                assistant: { provider_id: "hub", model: "official-mid" },
                coding: { inherit_assistant: true },
            },
            assistant: { profile: "assistant", provider_id: "hub", provider_name: "MaClaw Official", model: "official-mid", health: "configured" },
            coding: { profile: "coding", inherit_assistant: true, provider_id: "hub", model: "official-mid", health: "configured" },
        });
        render(<LLMProfileAssignments lang="zh-Hans" />);

        const input = await screen.findByLabelText("普通 AI 助手模型") as HTMLInputElement;
        expect(input.value).toBe("mid");
        await waitFor(() => expect(fetchProfileModels).toHaveBeenCalledWith("hub"));
        const toggle = document.querySelector<HTMLButtonElement>('button[aria-controls="assistant-profile-models"]');
        expect(toggle).toBeTruthy();
        fireEvent.click(toggle!);
        const optionValues = () => Array.from(document.getElementById("assistant-profile-models")?.querySelectorAll("[data-value]") || []).map((node) => node.getAttribute("data-value"));
        await waitFor(() => expect(optionValues()).toEqual(["auto", "low", "mid", "high"]));
        expect(screen.getByText(/MaClaw Official · mid/)).toBeTruthy();
    });

    it("does not offer an image probe for Hub-managed models", async () => {
        getState.mockResolvedValue({
            ...state,
            providers: [
                { id: "hub", name: "MaClaw Official", model: "hub-model", models: ["hub-model"], is_hub_service: true, connection_test_passed: true },
            ],
            profiles: {
                version: 1,
                assistant: { provider_id: "hub", model: "hub-model" },
                coding: { inherit_assistant: true },
            },
            assistant: { profile: "assistant", provider_id: "hub", provider_name: "MaClaw Official", model: "hub-model", health: "configured" },
            coding: { profile: "coding", inherit_assistant: true, provider_id: "hub", model: "hub-model", health: "configured" },
        });
        render(<LLMProfileAssignments lang="en" />);

        await screen.findByRole("heading", { name: "Model assignments" });
        expect(screen.queryByRole("button", { name: "Test connection and image support" })).toBeNull();
        expect(screen.getAllByRole("button", { name: "Test connection" }).length).toBeGreaterThan(0);
        expect(screen.getAllByText("Vision support: not verified locally").length).toBeGreaterThan(0);
        expect(screen.queryByText("Vision support: not tested")).toBeNull();

        fireEvent.change(screen.getByLabelText("Caption provider"), { target: { value: "hub" } });
        expect(screen.getByText("Hub models are not image-tested here. Captioning unlabeled boxes needs a vision model.")).toBeTruthy();
        expect(screen.queryByRole("button", { name: "Test connection and image support" })).toBeNull();
    });

    it("defaults the three loop roles to follow Hub and can pin one", async () => {
        getState.mockResolvedValue({
            ...state,
            profiles: {
                ...state.profiles,
                horizon: { manager: { model: "keep-horizon" } },
            },
            hub_role_models: { foreground: "auto", background: "worker", computer_use: "auto" },
            foreground: { model: "auto", inherit_hub: true, provider_name: "Hub", configured: true },
            background: { model: "worker", inherit_hub: true, provider_name: "Hub", configured: true },
            computer_use: { model: "auto", inherit_hub: true, provider_name: "Hub", configured: true },
        });
        saveProfiles.mockResolvedValue(undefined);
        render(<LLMProfileAssignments lang="en" />);

        const foregroundFollow = await screen.findByRole("checkbox", { name: "Foreground follows Hub" }) as HTMLInputElement;
        const backgroundFollow = screen.getByRole("checkbox", { name: "Background follows Hub" }) as HTMLInputElement;
        const computerUseFollow = screen.getByRole("checkbox", { name: "Computer use follows Hub" }) as HTMLInputElement;
        expect(foregroundFollow.checked).toBe(true);
        expect(backgroundFollow.checked).toBe(true);
        expect(computerUseFollow.checked).toBe(true);
        expect(screen.getByText(/Hub · worker/)).toBeTruthy();

        fireEvent.click(screen.getByRole("checkbox", { name: "Foreground follows Hub" }));
        fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
        await waitFor(() => expect(saveProfiles).toHaveBeenCalled());
        const draft = saveProfiles.mock.calls[0][0] as {
            foreground: { inherit_hub?: boolean; model?: string };
            horizon: { manager: { model: string } };
        };
        expect(draft.foreground.inherit_hub).toBe(false);
        expect(draft.foreground.model).toBe("auto");
        expect(draft.horizon.manager.model).toBe("keep-horizon");
    });

    it("matches backend vision-model rules for caption warnings", () => {
        expect(captionModelMissingVision({ id: "p", name: "P", supports_vision: true, model: "gpt-5" }, "gpt-5")).toBe(false);
        expect(captionModelMissingVision({ id: "p", name: "P", supports_vision: true, model: "" }, "llava")).toBe(true);
        expect(captionModelMissingVision({ id: "p", name: "P", supports_vision: true, vision_models: ["llava"] }, "LLAVA")).toBe(false);
        expect(captionModelMissingVision({ id: "p", name: "P", supports_vision: false }, "deepseek-coder")).toBe(true);
        expect(modelVisionStatus({ id: "p", name: "P", supports_vision: true, vision_models: ["gpt-5"], vision_tested_models: ["gpt-5"], connection_test_passed: true, model: "gpt-5" }, "gpt-5-mini")).toBe("untested");
        expect(modelVisionStatus({ id: "p", name: "P", supports_vision: false, vision_tested_models: ["text-only"], connection_test_passed: true, model: "gpt-5" }, "text-only")).toBe("unsupported");
        expect(modelVisionStatus({ id: "hub", name: "Hub", is_hub_service: true, connection_test_passed: true, model: "hub-model" }, "hub-model")).toBe("untested");
    });
});
