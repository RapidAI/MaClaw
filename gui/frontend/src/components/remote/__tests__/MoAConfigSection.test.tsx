// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor, cleanup } from "@testing-library/react";

const GetMoAConfigMock = vi.fn();
const SaveMoAConfigMock = vi.fn();
const GetMoASessionStateMock = vi.fn();
const SetMoAStickyMock = vi.fn();
const GetMoAStatsMock = vi.fn();

vi.mock("../../../../wailsjs/go/main/App", () => ({
    GetMoAConfig: (...args: unknown[]) => GetMoAConfigMock(...args),
    SaveMoAConfig: (...args: unknown[]) => SaveMoAConfigMock(...args),
    GetMoASessionState: (...args: unknown[]) => GetMoASessionStateMock(...args),
    SetMoASticky: (...args: unknown[]) => SetMoAStickyMock(...args),
    GetMoAStats: (...args: unknown[]) => GetMoAStatsMock(...args),
}));

vi.mock("../../../../wailsjs/runtime", () => ({
    EventsOn: vi.fn(() => vi.fn()),
    EventsOff: vi.fn(),
}));

import { MoAConfigSection } from "../MoAConfigSection";

const providers = [
    {
        name: "OpenAI",
        url: "",
        key: "",
        model: "gpt",
        protocol: "openai",
        is_custom: false,
        supports_vision: false,
    },
    {
        name: "DeepSeek",
        url: "",
        key: "",
        model: "v3",
        protocol: "openai",
        is_custom: true,
        supports_vision: false,
    },
] as any[];

describe("MoAConfigSection (simplified)", () => {
    beforeEach(() => {
        vi.clearAllMocks();
        GetMoAConfigMock.mockResolvedValue({
            enabled: false,
            default_preset: "review",
            presets: {
                review: {
                    enabled: true,
                    display_name: "方案评审",
                    aggregator: { use_primary: true },
                    reference_models: [{ provider: "OpenAI" }],
                    reference_max_tokens: 600,
                },
            },
        });
        SaveMoAConfigMock.mockResolvedValue(undefined);
        GetMoASessionStateMock.mockResolvedValue({ sticky: false });
        SetMoAStickyMock.mockResolvedValue(undefined);
        GetMoAStatsMock.mockResolvedValue({ fanouts: 0 });
    });

    afterEach(() => {
        cleanup();
    });

    it("stays minimal when disabled, expands advisors when enabled, and saves", async () => {
        render(<MoAConfigSection lang="zh-Hans" providers={providers} />);

        await waitFor(() => expect(screen.getByTestId("moa-config-section")).toBeTruthy());
        expect(screen.queryByTestId("moa-advisor-0")).toBeNull();

        fireEvent.click(screen.getByTestId("moa-config-enabled"));
        expect(screen.getByTestId("moa-advisor-0")).toBeTruthy();
        expect(screen.getByTestId("moa-advisor-1")).toBeTruthy();

        // Advanced hidden by default
        expect(screen.queryByTestId("moa-advanced")).toBeNull();

        fireEvent.change(screen.getByTestId("moa-advisor-0"), { target: { value: "OpenAI" } });
        fireEvent.change(screen.getByTestId("moa-advisor-1"), { target: { value: "DeepSeek" } });
        fireEvent.click(screen.getByTestId("moa-config-save"));

        await waitFor(() => expect(SaveMoAConfigMock).toHaveBeenCalled());
        const arg = SaveMoAConfigMock.mock.calls[0][0] as {
            enabled: boolean;
            presets: { review: { aggregator: { use_primary?: boolean }; reference_models: { provider: string }[] } };
        };
        expect(arg.enabled).toBe(true);
        expect(arg.presets.review.aggregator.use_primary).toBe(true);
        expect(arg.presets.review.reference_models.map((r) => r.provider)).toEqual(["OpenAI", "DeepSeek"]);
        await waitFor(() => expect(screen.getByTestId("moa-config-saved")).toBeTruthy());
    });

    it("requires at least one advisor when enabling", async () => {
        GetMoAConfigMock.mockResolvedValue({ enabled: false, presets: {} });
        render(<MoAConfigSection lang="en" providers={providers} />);
        await waitFor(() => expect(screen.getByTestId("moa-config-section")).toBeTruthy());
        fireEvent.click(screen.getByTestId("moa-config-enabled"));
        fireEvent.click(screen.getByTestId("moa-config-save"));
        await waitFor(() => expect(screen.getByTestId("moa-config-error")).toBeTruthy());
        expect(SaveMoAConfigMock).not.toHaveBeenCalled();
    });

    it("toggles advanced options", async () => {
        render(<MoAConfigSection lang="en" providers={providers} />);
        await waitFor(() => expect(screen.getByTestId("moa-config-section")).toBeTruthy());
        fireEvent.click(screen.getByTestId("moa-config-enabled"));
        fireEvent.click(screen.getByTestId("moa-advanced-toggle"));
        expect(screen.getByTestId("moa-advanced")).toBeTruthy();
    });

    it("preserves extra presets when saving simple UI", async () => {
        GetMoAConfigMock.mockResolvedValue({
            enabled: true,
            default_preset: "review",
            max_references: 4,
            presets: {
                review: {
                    enabled: true,
                    display_name: "方案评审",
                    aggregator: { use_primary: true },
                    reference_models: [{ provider: "OpenAI" }],
                },
                deep: {
                    enabled: true,
                    display_name: "Deep",
                    aggregator: { use_primary: true },
                    reference_models: [{ provider: "DeepSeek" }],
                },
            },
        });
        render(<MoAConfigSection lang="en" providers={providers} />);
        await waitFor(() => expect(screen.getByTestId("moa-config-section")).toBeTruthy());
        fireEvent.change(screen.getByTestId("moa-advisor-0"), { target: { value: "DeepSeek" } });
        fireEvent.click(screen.getByTestId("moa-config-save"));
        await waitFor(() => expect(SaveMoAConfigMock).toHaveBeenCalled());
        const arg = SaveMoAConfigMock.mock.calls[0][0] as {
            max_references?: number;
            presets: Record<string, { reference_models?: { provider: string }[] }>;
        };
        expect(arg.presets.deep).toBeTruthy();
        expect(arg.presets.deep.reference_models?.[0]?.provider).toBe("DeepSeek");
        expect(arg.max_references).toBe(4);
        expect(arg.presets.review.reference_models?.map((r) => r.provider)).toEqual(["DeepSeek"]);
    });

    it("saves back into default_preset when it is not review", async () => {
        GetMoAConfigMock.mockResolvedValue({
            enabled: true,
            default_preset: "deep",
            presets: {
                review: {
                    enabled: true,
                    aggregator: { use_primary: true },
                    reference_models: [{ provider: "OpenAI" }],
                },
                deep: {
                    enabled: true,
                    display_name: "Deep council",
                    aggregator: { use_primary: true },
                    reference_models: [{ provider: "DeepSeek" }],
                },
            },
        });
        render(<MoAConfigSection lang="en" providers={providers} />);
        await waitFor(() => expect(screen.getByTestId("moa-config-section")).toBeTruthy());
        // Loaded from "deep" → first advisor DeepSeek
        expect((screen.getByTestId("moa-advisor-0") as HTMLSelectElement).value).toBe("DeepSeek");
        fireEvent.change(screen.getByTestId("moa-advisor-0"), { target: { value: "OpenAI" } });
        fireEvent.click(screen.getByTestId("moa-config-save"));
        await waitFor(() => expect(SaveMoAConfigMock).toHaveBeenCalled());
        const arg = SaveMoAConfigMock.mock.calls[0][0] as {
            default_preset?: string;
            presets: Record<string, { reference_models?: { provider: string }[]; display_name?: string }>;
        };
        expect(arg.default_preset).toBe("deep");
        expect(arg.presets.deep.reference_models?.map((r) => r.provider)).toEqual(["OpenAI"]);
        // Untouched sibling preset
        expect(arg.presets.review.reference_models?.[0]?.provider).toBe("OpenAI");
    });

    it("arms sticky session when checkbox is checked", async () => {
        render(<MoAConfigSection lang="zh-Hans" providers={providers} />);
        await waitFor(() => expect(screen.getByTestId("moa-config-section")).toBeTruthy());
        fireEvent.click(screen.getByTestId("moa-config-enabled"));
        // Sticky UI is only visible while enabled (before reload after save).
        fireEvent.click(screen.getByTestId("moa-sticky-toggle"));
        await waitFor(() => expect(SetMoAStickyMock).toHaveBeenCalledWith(true));
    });
});
