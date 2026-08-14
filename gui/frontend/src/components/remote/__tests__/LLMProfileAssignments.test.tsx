// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";

const getState = vi.fn();
const saveProfiles = vi.fn();
const testProfile = vi.fn();
const eventsOn = vi.fn();
const eventsOff = vi.fn();

vi.mock("../../../../wailsjs/go/main/App", () => ({
    GetMaclawLLMProfilePanelState: (...args: unknown[]) => getState(...args),
    SaveMaclawLLMProfiles: (...args: unknown[]) => saveProfiles(...args),
    TestMaclawLLMProfile: (...args: unknown[]) => testProfile(...args),
}));

vi.mock("../../../../wailsjs/runtime", () => ({
    EventsOn: (...args: unknown[]) => eventsOn(...args),
    EventsOff: (...args: unknown[]) => eventsOff(...args),
}));

import { LLMProfileAssignments } from "../LLMProfileAssignments";

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

describe("LLMProfileAssignments", () => {
    beforeEach(() => {
        getState.mockReset();
        saveProfiles.mockReset();
        testProfile.mockReset();
        eventsOn.mockReset();
        eventsOff.mockReset();
    });

    it("does not render a duplicate provider-management action", async () => {
        getState.mockResolvedValue(state);
        render(<LLMProfileAssignments lang="en" />);

        await screen.findByRole("heading", { name: "Model assignments" });
        expect(screen.queryByRole("button", { name: "Manage providers" })).toBeNull();
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
        expect(screen.getByText("Only providers with a passed connection test are available here. Connections and credentials are managed separately.")).toBeTruthy();
    });

    it("explains how to make providers available when none have passed a test", async () => {
        getState.mockResolvedValue({ ...state, providers: [] });
        render(<LLMProfileAssignments lang="en" />);

        expect((await screen.findByRole("status")).textContent).toContain("No tested providers yet. Test and save a provider in Provider management first.");
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
});
