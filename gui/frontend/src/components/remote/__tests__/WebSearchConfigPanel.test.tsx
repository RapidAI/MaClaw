// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

const getMock = vi.fn();
const saveMock = vi.fn();
const resetMock = vi.fn();
const testMock = vi.fn();
const showConfirmMock = vi.fn();

vi.mock("../../../../wailsjs/go/main/App", () => ({
    GetWebSearchStrategy: (...args: unknown[]) => getMock(...args),
    SaveWebSearchStrategy: (...args: unknown[]) => saveMock(...args),
    ResetWebSearchStrategy: (...args: unknown[]) => resetMock(...args),
    TestWebSearchEngine: (...args: unknown[]) => testMock(...args),
}));

vi.mock("../../CustomDialog", () => ({
    useDialog: () => ({ showConfirm: (...args: unknown[]) => showConfirmMock(...args) }),
}));

import { WebSearchConfigPanel } from "../WebSearchConfigPanel";

const strategy = {
    version: 1,
    preset: "mainland",
    mode: "priority",
    browser_fallback_enabled: true,
    browser_fallback_engine_id: "bing_cn",
    browser_human_assist_enabled: true,
    hedging_delay_ms: 500,
    min_results_before_hedge: 3,
    engines: [
        { id: "bing_cn", name: "Bing", enabled: true, priority: 1, transport: "http_html", needs_api_key: false, has_api_key: false },
        { id: "baidu", name: "百度", enabled: true, priority: 2, transport: "http_html", needs_api_key: false, has_api_key: false },
        { id: "google", name: "Google", enabled: true, priority: 3, transport: "browser", needs_api_key: false, has_api_key: false },
        { id: "brave", name: "Brave Search API", enabled: false, priority: 4, transport: "api", needs_api_key: true, has_api_key: false },
    ],
};

describe("WebSearchConfigPanel", () => {
    beforeEach(() => {
        vi.clearAllMocks();
        getMock.mockResolvedValue(strategy);
        saveMock.mockResolvedValue(undefined);
        resetMock.mockResolvedValue(strategy);
        testMock.mockResolvedValue({ result_count: 3, duration_ms: 120 });
        showConfirmMock.mockResolvedValue(true);
    });

	it("recovers when the initial strategy load fails", async () => {
		getMock
			.mockRejectedValueOnce({ message: "Search service is unavailable" })
			.mockResolvedValueOnce(strategy);
		render(<WebSearchConfigPanel lang="en" />);

		expect((await screen.findByRole("alert")).textContent).toContain("Search service is unavailable");
		fireEvent.click(screen.getByRole("button", { name: "Retry" }));

		expect(await screen.findByRole("button", { name: "Move Google up" })).toBeTruthy();
		expect(getMock).toHaveBeenCalledTimes(2);
	});

	it("prevents duplicate retry requests before the loading state renders", async () => {
		let finishRetry!: () => void;
		getMock
			.mockRejectedValueOnce({ message: "Search service is unavailable" })
			.mockImplementationOnce(() => new Promise(resolve => { finishRetry = () => resolve(strategy); }));
		render(<WebSearchConfigPanel lang="en" />);

		await screen.findByRole("alert");
		const retry = screen.getByRole("button", { name: "Retry" });
		fireEvent.click(retry);
		fireEvent.click(retry);

		expect(getMock).toHaveBeenCalledTimes(2);
		finishRetry();
		expect(await screen.findByRole("button", { name: "Move Google up" })).toBeTruthy();
	});

	it("treats an empty engine payload as recoverable invalid configuration", async () => {
		getMock.mockResolvedValueOnce({ ...strategy, engines: [] });
		render(<WebSearchConfigPanel lang="en" />);

		expect((await screen.findByRole("alert")).textContent).toContain("No search engines were returned");
		expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy();
	});

	it("filters retired Mojeek entries returned by an older backend", async () => {
		getMock.mockResolvedValueOnce({
			...strategy,
			engines: [
				...strategy.engines.slice(0, 2),
				{ id: " MoJeEk ", name: "Mojeek", enabled: true, priority: 3, transport: "http_html", needs_api_key: false, has_api_key: false },
				...strategy.engines.slice(2).map(engine => ({ ...engine, priority: engine.priority + 1 })),
			],
		});
		render(<WebSearchConfigPanel lang="en" />);

		await screen.findByRole("button", { name: "Move Google up" });
		expect(screen.queryByText("Mojeek")).toBeNull();
		expect(screen.getAllByRole("button", { name: /Drag .* to reorder/ })).toHaveLength(strategy.engines.length);
	});

	it("normalizes and deduplicates engine IDs returned by an older backend", async () => {
		getMock.mockResolvedValueOnce({
			...strategy,
			engines: [
				{ ...strategy.engines[0], id: " BING_CN " },
				{ ...strategy.engines[0], id: "bing_cn", name: "Duplicate Bing", priority: 2 },
				...strategy.engines.slice(1).map(engine => ({ ...engine, priority: engine.priority + 1 })),
			],
		});
		render(<WebSearchConfigPanel lang="en" />);

		await screen.findByRole("button", { name: "Move Google up" });
		expect(screen.queryByText("Duplicate Bing")).toBeNull();
		expect(screen.getAllByRole("button", { name: /Drag .* to reorder/ })).toHaveLength(strategy.engines.length);

		fireEvent.click(screen.getByRole("button", { name: "Save strategy" }));
		await waitFor(() => expect(saveMock).toHaveBeenCalledTimes(1));
		expect(saveMock.mock.calls[0][0].engines[0].id).toBe("bing_cn");
	});

    it("moves Google to the first position and saves the exact order", async () => {
        render(<WebSearchConfigPanel lang="en" />);
        await screen.findByRole("button", { name: "Move Google up" });

        fireEvent.click(screen.getByRole("button", { name: "Move Google up" }));
        fireEvent.click(screen.getByRole("button", { name: "Move Google up" }));
        fireEvent.click(screen.getByRole("button", { name: "Save strategy" }));

        await waitFor(() => expect(saveMock).toHaveBeenCalledTimes(1));
        const request = saveMock.mock.calls[0][0];
        expect(request.engines.map((engine: any) => engine.id)).toEqual(["google", "bing_cn", "baidu", "brave"]);
        expect(request.engines.map((engine: any) => engine.priority)).toEqual([1, 2, 3, 4]);
        expect(request.preset).toBe("custom");
    });

	it("saves the latest order when move and save happen in the same event batch", async () => {
		render(<WebSearchConfigPanel lang="en" />);
		await screen.findByRole("button", { name: "Move Google up" });

		fireEvent.click(screen.getByRole("button", { name: "Move Google up" }));
		fireEvent.click(screen.getByRole("button", { name: "Save strategy" }));

		await waitFor(() => expect(saveMock).toHaveBeenCalledTimes(1));
		expect(saveMock.mock.calls[0][0].engines.map((engine: any) => engine.id)).toEqual([
			"bing_cn", "google", "baidu", "brave",
		]);
	});

    it("tests MaClaw Hub RapidSearch while unchecked and shows a result preview", async () => {
        getMock.mockResolvedValue({
            ...strategy,
            engines: [
                ...strategy.engines,
                { id: "maclaw_hub", name: "MaClaw Hub / RapidSearch", enabled: false, priority: 5, transport: "api", needs_api_key: false, has_api_key: false },
            ],
        });
        testMock.mockResolvedValueOnce({
            result_count: 2,
            duration_ms: 1840,
            preview_title: "Go HTTP server",
            preview_url: "https://pkg.go.dev/net/http",
            preview_snippet: "ListenAndServe starts an HTTP server",
        });
        render(<WebSearchConfigPanel lang="en" />);
        const toggle = await screen.findByRole("checkbox", { name: "Enable MaClaw Hub / RapidSearch" });
        expect((toggle as HTMLInputElement).checked).toBe(false);
        const row = toggle.closest("li")!;
        fireEvent.click(row.querySelector(".web-search-config__test")!);

        await waitFor(() => expect(testMock).toHaveBeenCalledWith(expect.objectContaining({
            engine: expect.objectContaining({ id: "maclaw_hub", enabled: true, transport: "api" }),
            query: "golang http server",
            use_saved_key: false,
        })));
        expect(await screen.findByText("2 results · 1840 ms")).toBeTruthy();
        expect(screen.getByText("Go HTTP server")).toBeTruthy();
        expect(screen.getByText("https://pkg.go.dev/net/http")).toBeTruthy();
        expect(screen.getByText("ListenAndServe starts an HTTP server")).toBeTruthy();
    });

    it("shows MaClaw Hub RapidSearch unchecked without an API-key field", async () => {
        getMock.mockResolvedValue({
            ...strategy,
            engines: [
                ...strategy.engines,
                { id: "maclaw_hub", name: "MaClaw Hub / RapidSearch", enabled: false, priority: 5, transport: "api", needs_api_key: false, has_api_key: false, base_url: "https://hub.maclaw.top/searchproxy/search" },
            ],
        });
        render(<WebSearchConfigPanel lang="en" />);
        const toggle = await screen.findByRole("checkbox", { name: "Enable MaClaw Hub / RapidSearch" });
        expect((toggle as HTMLInputElement).checked).toBe(false);
        expect(screen.getByText("Uses signed-in MaClaw Hub account")).toBeTruthy();
        expect(screen.queryByLabelText("MaClaw Hub / RapidSearch API Key")).toBeNull();
    });

    it("blocks enabling an API engine until a key is entered", async () => {
        render(<WebSearchConfigPanel lang="en" />);
        await screen.findByRole("checkbox", { name: "Enable Brave Search API" });
        fireEvent.click(screen.getByRole("checkbox", { name: "Enable Brave Search API" }));
        fireEvent.click(screen.getByRole("button", { name: "Save strategy" }));

        expect((await screen.findByRole("alert")).textContent).toContain("requires an API key");
        expect(saveMock).not.toHaveBeenCalled();
    });

	it("removes a saved API key explicitly and disables that engine", async () => {
		getMock.mockResolvedValue({
			...strategy,
			engines: strategy.engines.map(engine => engine.id === "brave"
				? { ...engine, enabled: true, has_api_key: true }
				: engine),
		});
		render(<WebSearchConfigPanel lang="en" />);
		await screen.findByRole("button", { name: "Remove" });

		fireEvent.click(screen.getByRole("button", { name: "Remove" }));
		fireEvent.click(screen.getByRole("button", { name: "Save strategy" }));

		await waitFor(() => expect(saveMock).toHaveBeenCalledTimes(1));
		expect(saveMock.mock.calls[0][0].clear_api_key_engine_ids).toEqual(["brave"]);
		const brave = saveMock.mock.calls[0][0].engines.find((engine: any) => engine.id === "brave");
		expect(brave.enabled).toBe(false);
		expect(brave.api_key).toBe("");
	});

    it("tests Google through its browser transport", async () => {
        render(<WebSearchConfigPanel lang="en" />);
        await screen.findByRole("button", { name: "Move Google up" });
        const googleRow = screen.getByRole("button", { name: "Move Google up" }).closest("li")!;
        fireEvent.click(googleRow.querySelector(".web-search-config__test")!);

		await waitFor(() => expect(testMock).toHaveBeenCalledWith(expect.objectContaining({
			engine: expect.objectContaining({ id: "google", transport: "browser" }),
			use_saved_key: false,
			human_assist_enabled: true,
		})));
        expect(await screen.findByText("3 results · 120 ms")).toBeTruthy();
    });

	it("uses a saved API key for testing only while it remains selected", async () => {
		getMock.mockResolvedValue({
			...strategy,
			engines: strategy.engines.map(engine => engine.id === "brave"
				? { ...engine, enabled: true, has_api_key: true }
				: engine),
		});
		render(<WebSearchConfigPanel lang="en" />);
		const remove = await screen.findByRole("button", { name: "Remove" });
		const braveRow = remove.closest("li")!;
		fireEvent.click(braveRow.querySelector(".web-search-config__test")!);

		await waitFor(() => expect(testMock).toHaveBeenCalledWith(expect.objectContaining({
			engine: expect.objectContaining({ id: "brave", api_key: "" }),
			use_saved_key: true,
		})));

		fireEvent.click(remove);
		fireEvent.click(braveRow.querySelector(".web-search-config__test")!);
		await waitFor(() => expect(testMock).toHaveBeenCalledTimes(2));
		expect(testMock.mock.calls[1][0]).toEqual(expect.objectContaining({
			engine: expect.objectContaining({ id: "brave", api_key: "" }),
			use_saved_key: false,
		}));
	});

	it("prevents duplicate engine tests before the testing state renders", async () => {
		let finishTest!: () => void;
		testMock.mockImplementation(() => new Promise(resolve => {
			finishTest = () => resolve({ result_count: 3, duration_ms: 120 });
		}));
		render(<WebSearchConfigPanel lang="en" />);
		await screen.findByRole("button", { name: "Move Google up" });
		const googleRow = screen.getByRole("button", { name: "Move Google up" }).closest("li")!;
		const testButton = googleRow.querySelector(".web-search-config__test")!;

		fireEvent.click(testButton);
		fireEvent.click(testButton);

		expect(testMock).toHaveBeenCalledTimes(1);
		finishTest();
		expect(await screen.findByText("3 results · 120 ms")).toBeTruthy();
	});

	it("shows when an engine test recovered after one retry", async () => {
		testMock.mockResolvedValueOnce({ result_count: 3, duration_ms: 6200, retry_count: 1 });
		render(<WebSearchConfigPanel lang="en" />);
		await screen.findByRole("button", { name: "Move Google up" });
		const googleRow = screen.getByRole("button", { name: "Move Google up" }).closest("li")!;
		fireEvent.click(googleRow.querySelector(".web-search-config__test")!);

		expect(await screen.findByText("3 results · 6200 ms · retried once")).toBeTruthy();
	});

	it("clears a stale test result when that engine's settings change", async () => {
		render(<WebSearchConfigPanel lang="en" />);
		await screen.findByRole("button", { name: "Move Google up" });
		const googleRow = screen.getByRole("button", { name: "Move Google up" }).closest("li")!;
		fireEvent.click(googleRow.querySelector(".web-search-config__test")!);
		expect(await screen.findByText("3 results · 120 ms")).toBeTruthy();

		fireEvent.click(screen.getByRole("checkbox", { name: "Enable Google" }));
		expect(screen.queryByText("3 results · 120 ms")).toBeNull();
	});

	it("ignores an in-flight test result after that engine is edited", async () => {
		let finishTest!: () => void;
		testMock.mockImplementation(() => new Promise(resolve => {
			finishTest = () => resolve({ result_count: 3, duration_ms: 120 });
		}));
		render(<WebSearchConfigPanel lang="en" />);
		await screen.findByRole("button", { name: "Move Google up" });
		const googleRow = screen.getByRole("button", { name: "Move Google up" }).closest("li")!;
		fireEvent.click(googleRow.querySelector(".web-search-config__test")!);
		await screen.findByText("Testing…");
		fireEvent.click(screen.getByRole("checkbox", { name: "Enable Google" }));
		expect(screen.getByText("Testing…")).toBeTruthy();
		expect((screen.getByRole("button", { name: "Save strategy" }) as HTMLButtonElement).disabled).toBe(true);
		finishTest();

		await waitFor(() => expect(screen.queryByText("Testing…")).toBeNull());
		expect(screen.queryByText("3 results · 120 ms")).toBeNull();
	});

	it("blocks save and reset while an engine test is running", async () => {
		let finishTest!: () => void;
		testMock.mockImplementation(() => new Promise(resolve => {
			finishTest = () => resolve({ result_count: 3, duration_ms: 120 });
		}));
		render(<WebSearchConfigPanel lang="en" />);
		await screen.findByRole("button", { name: "Move Google up" });
		const googleRow = screen.getByRole("button", { name: "Move Google up" }).closest("li")!;
		fireEvent.click(googleRow.querySelector(".web-search-config__test")!);

		await screen.findByText("Testing…");
		expect((screen.getByRole("button", { name: "Save strategy" }) as HTMLButtonElement).disabled).toBe(true);
		expect((screen.getByRole("button", { name: "Reset default order" }) as HTMLButtonElement).disabled).toBe(true);
		finishTest();
		await screen.findByText("3 results · 120 ms");
		expect((screen.getByRole("button", { name: "Save strategy" }) as HTMLButtonElement).disabled).toBe(false);
	});

    it("saves the human verification assistance preference", async () => {
		render(<WebSearchConfigPanel lang="en" />);
		const toggle = await screen.findByRole("checkbox", { name: "Allow human verification assistance" });
		fireEvent.click(toggle);
        fireEvent.click(screen.getByRole("button", { name: "Save strategy" }));

        await waitFor(() => expect(saveMock).toHaveBeenCalledTimes(1));
		expect(saveMock.mock.calls[0][0].browser_human_assist_enabled).toBe(false);
	});

	it("does not enable human verification assistance when older config omits the preference", async () => {
		getMock.mockResolvedValueOnce({ ...strategy, browser_human_assist_enabled: undefined });
		render(<WebSearchConfigPanel lang="en" />);

		const toggle = await screen.findByRole("checkbox", { name: "Allow human verification assistance" });
		expect((toggle as HTMLInputElement).checked).toBe(false);
	});

	it("keeps browser-engine human assistance configurable when final fallback is disabled", async () => {
		getMock.mockResolvedValueOnce({ ...strategy, browser_fallback_enabled: false });
		render(<WebSearchConfigPanel lang="en" />);

		const toggle = await screen.findByRole("checkbox", { name: "Allow human verification assistance" });
		expect((toggle as HTMLInputElement).disabled).toBe(false);
	});

	it("explains that browser fallback can top up sparse results", async () => {
		render(<WebSearchConfigPanel lang="en" />);
		expect(await screen.findByText(/fail or return too few results/)).toBeTruthy();
	});

	it("tests a browser engine with the current unsaved human-assistance choice", async () => {
		render(<WebSearchConfigPanel lang="en" />);
		const toggle = await screen.findByRole("checkbox", { name: "Allow human verification assistance" });
		fireEvent.click(toggle);
		const googleRow = screen.getByRole("button", { name: "Move Google up" }).closest("li")!;
		fireEvent.click(googleRow.querySelector(".web-search-config__test")!);

		await waitFor(() => expect(testMock).toHaveBeenCalledWith(expect.objectContaining({
			engine: expect.objectContaining({ id: "google" }),
			human_assist_enabled: false,
		})));
	});

	it("clears browser-engine test results when human assistance changes", async () => {
		render(<WebSearchConfigPanel lang="en" />);
		await screen.findByRole("button", { name: "Move Google up" });
		const googleRow = screen.getByRole("button", { name: "Move Google up" }).closest("li")!;
		fireEvent.click(googleRow.querySelector(".web-search-config__test")!);
		expect(await screen.findByText("3 results · 120 ms")).toBeTruthy();

		fireEvent.click(screen.getByRole("checkbox", { name: "Allow human verification assistance" }));
		expect(screen.queryByText("3 results · 120 ms")).toBeNull();
	});

	it("renders structured bridge errors without object noise", async () => {
		testMock.mockRejectedValueOnce({ message: "Request timed out" });
		render(<WebSearchConfigPanel lang="en" />);
		await screen.findByRole("button", { name: "Move Google up" });
		const googleRow = screen.getByRole("button", { name: "Move Google up" }).closest("li")!;
		fireEvent.click(googleRow.querySelector(".web-search-config__test")!);

		expect(await screen.findByText("Request timed out")).toBeTruthy();
		expect(screen.queryByText("[object Object]")).toBeNull();
	});

    it("resets the selected preset while keeping the reset flow explicit", async () => {
        render(<WebSearchConfigPanel lang="en" />);
        await screen.findByRole("button", { name: "Move Google up" });
        fireEvent.click(screen.getByRole("button", { name: "Reset default order" }));
        await waitFor(() => expect(resetMock).toHaveBeenCalledWith("mainland"));
        expect(showConfirmMock).toHaveBeenCalled();
    });

	it("locks strategy editing while reset is pending", async () => {
		let finishReset!: () => void;
		resetMock.mockImplementation(() => new Promise(resolve => { finishReset = () => resolve(strategy); }));
		render(<WebSearchConfigPanel lang="en" />);
		await screen.findByRole("button", { name: "Move Google up" });

		fireEvent.click(screen.getByRole("button", { name: "Reset default order" }));
		await screen.findByRole("button", { name: "Resetting…" });
		expect((screen.getByLabelText("Preset") as HTMLSelectElement).disabled).toBe(true);
		expect((screen.getByRole("checkbox", { name: "Enable Google" }) as HTMLInputElement).disabled).toBe(true);
		expect((screen.getByRole("button", { name: "Save strategy" }) as HTMLButtonElement).disabled).toBe(true);
		finishReset();
		await screen.findByRole("button", { name: "Reset default order" });
	});

	it("clears stale engine test results after reset", async () => {
		render(<WebSearchConfigPanel lang="en" />);
		await screen.findByRole("button", { name: "Move Google up" });
		const googleRow = screen.getByRole("button", { name: "Move Google up" }).closest("li")!;
		fireEvent.click(googleRow.querySelector(".web-search-config__test")!);
		expect(await screen.findByText("3 results · 120 ms")).toBeTruthy();

		fireEvent.click(screen.getByRole("button", { name: "Reset default order" }));
		await waitFor(() => expect(resetMock).toHaveBeenCalledTimes(1));
		await waitFor(() => expect(screen.queryByText("3 results · 120 ms")).toBeNull());
	});

	it("does not reset while an engine test starts during confirmation", async () => {
		let finishConfirm!: (confirmed: boolean) => void;
		showConfirmMock.mockImplementationOnce(() => new Promise(resolve => { finishConfirm = resolve; }));
		let finishTest!: () => void;
		testMock.mockImplementationOnce(() => new Promise(resolve => {
			finishTest = () => resolve({ result_count: 3, duration_ms: 120 });
		}));
		render(<WebSearchConfigPanel lang="en" />);
		await screen.findByRole("button", { name: "Move Google up" });

		fireEvent.click(screen.getByRole("button", { name: "Reset default order" }));
		await waitFor(() => expect(showConfirmMock).toHaveBeenCalledTimes(1));
		const googleRow = screen.getByRole("button", { name: "Move Google up" }).closest("li")!;
		fireEvent.click(googleRow.querySelector(".web-search-config__test")!);
		await waitFor(() => expect(testMock).toHaveBeenCalledTimes(1));
		finishConfirm(true);

		await waitFor(() => expect(resetMock).not.toHaveBeenCalled());
		finishTest();
		expect(await screen.findByText("3 results · 120 ms")).toBeTruthy();
	});

	it("prevents duplicate reset confirmations before the dialog settles", async () => {
		let finishConfirm!: (confirmed: boolean) => void;
		showConfirmMock.mockImplementationOnce(() => new Promise(resolve => { finishConfirm = resolve; }));
		render(<WebSearchConfigPanel lang="en" />);
		await screen.findByRole("button", { name: "Reset default order" });

		const reset = screen.getByRole("button", { name: "Reset default order" });
		fireEvent.click(reset);
		fireEvent.click(reset);

		expect(showConfirmMock).toHaveBeenCalledTimes(1);
		finishConfirm(false);
		await waitFor(() => expect(resetMock).not.toHaveBeenCalled());
	});

	it("uses the latest preset when selection changes during reset confirmation", async () => {
		let finishConfirm!: (confirmed: boolean) => void;
		showConfirmMock.mockImplementationOnce(() => new Promise(resolve => { finishConfirm = resolve; }));
		render(<WebSearchConfigPanel lang="en" />);
		await screen.findByRole("button", { name: "Move Google up" });

		fireEvent.click(screen.getByRole("button", { name: "Reset default order" }));
		await waitFor(() => expect(showConfirmMock).toHaveBeenCalledTimes(1));
		fireEvent.change(screen.getByLabelText("Preset"), { target: { value: "international" } });
		finishConfirm(true);

		await waitFor(() => expect(resetMock).toHaveBeenCalledWith("international"));
	});

    it("applies the selected preset order before saving", async () => {
        render(<WebSearchConfigPanel lang="en" />);
        await screen.findByRole("button", { name: "Move Google up" });

        fireEvent.change(screen.getByLabelText("Preset"), { target: { value: "international" } });
        fireEvent.click(screen.getByRole("button", { name: "Save strategy" }));

        await waitFor(() => expect(saveMock).toHaveBeenCalledTimes(1));
        expect(saveMock.mock.calls[0][0].engines.map((engine: any) => engine.id)).toEqual([
            "google", "bing_cn", "baidu", "brave",
        ]);
        expect(saveMock.mock.calls[0][0].preset).toBe("international");
    });

    it("makes only the drag handle draggable", async () => {
        const { container } = render(<WebSearchConfigPanel lang="en" />);
        await screen.findByRole("button", { name: "Move Google up" });

        const row = screen.getByRole("button", { name: "Move Google up" }).closest("li")!;
        const handle = screen.getByRole("button", { name: "Drag Google to reorder" });
        expect(row.getAttribute("draggable")).toBeNull();
        expect(handle.getAttribute("draggable")).toBe("true");
        expect(container.querySelectorAll(".web-search-config__drag-handle").length).toBe(strategy.engines.length);
    });

	it("locks editing while a save is pending", async () => {
		let finishSave!: () => void;
		saveMock.mockImplementation(() => new Promise<void>(resolve => { finishSave = resolve; }));
		render(<WebSearchConfigPanel lang="en" />);
		await screen.findByRole("button", { name: "Move Google up" });

		fireEvent.click(screen.getByRole("button", { name: "Save strategy" }));
		await screen.findByRole("button", { name: "Saving…" });
		expect((screen.getByLabelText("Preset") as HTMLSelectElement).disabled).toBe(true);
		expect((screen.getByRole("checkbox", { name: "Enable Google" }) as HTMLInputElement).disabled).toBe(true);
		expect((screen.getByRole("button", { name: "Move Google up" }) as HTMLButtonElement).disabled).toBe(true);
		finishSave();

		await screen.findByRole("button", { name: "Saved" });
		expect(screen.getByText("Search strategy saved.")).toBeTruthy();
	});

	it("prevents duplicate saves before React renders the busy state", async () => {
		let finishSave!: () => void;
		saveMock.mockImplementation(() => new Promise<void>(resolve => { finishSave = resolve; }));
		render(<WebSearchConfigPanel lang="en" />);
		await screen.findByRole("button", { name: "Save strategy" });

		const save = screen.getByRole("button", { name: "Save strategy" });
		fireEvent.click(save);
		fireEvent.click(save);

		expect(saveMock).toHaveBeenCalledTimes(1);
		finishSave();
		await screen.findByRole("button", { name: "Saved" });
	});

	it("blocks same-batch edits once saving has started", async () => {
		let finishSave!: () => void;
		saveMock.mockImplementation(() => new Promise<void>(resolve => { finishSave = resolve; }));
		render(<WebSearchConfigPanel lang="en" />);
		await screen.findByRole("button", { name: "Move Google up" });

		fireEvent.click(screen.getByRole("button", { name: "Save strategy" }));
		fireEvent.click(screen.getByRole("button", { name: "Move Google up" }));

		expect(saveMock).toHaveBeenCalledTimes(1);
		expect(screen.getByLabelText("Priority 1").closest("li")?.textContent).toContain("Bing");
		finishSave();
		await screen.findByRole("button", { name: "Saved" });
	});

	it("includes an API-key removal when remove and save happen in the same event batch", async () => {
		getMock.mockResolvedValue({
			...strategy,
			engines: strategy.engines.map(engine => engine.id === "brave"
				? { ...engine, enabled: true, has_api_key: true }
				: engine),
		});
		render(<WebSearchConfigPanel lang="en" />);
		await screen.findByRole("button", { name: "Remove" });

		fireEvent.click(screen.getByRole("button", { name: "Remove" }));
		fireEvent.click(screen.getByRole("button", { name: "Save strategy" }));

		await waitFor(() => expect(saveMock).toHaveBeenCalledTimes(1));
		expect(saveMock.mock.calls[0][0].clear_api_key_engine_ids).toEqual(["brave"]);
	});
});
