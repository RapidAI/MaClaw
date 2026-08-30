import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { GetWebSearchStrategy, ResetWebSearchStrategy, SaveWebSearchStrategy, TestWebSearchEngine } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { useDialog } from "../CustomDialog";

type Preset = "mainland" | "international" | "custom";
type SearchMode = "priority" | "smart" | "aggregate";

interface SearchEngine {
    id: string;
    name: string;
    enabled: boolean;
    priority: number;
    transport: "api" | "http_html" | "browser";
    needs_api_key: boolean;
    has_api_key: boolean;
    api_key?: string;
    base_url?: string;
}

interface SearchStrategy {
    version: number;
    preset: Preset;
    mode: SearchMode;
    engines: SearchEngine[];
    browser_fallback_enabled: boolean;
    browser_fallback_engine_id: "bing_cn" | "google";
    browser_human_assist_enabled: boolean;
    hedging_delay_ms: number;
    min_results_before_hedge: number;
}

type Props = { lang?: string };
type TestPreview = { title?: string; url?: string; snippet?: string };
type TestState = { state: "idle" | "testing" | "success" | "error"; message?: string; preview?: TestPreview };
type TestWebSearchEngineRequest = {
	engine: Pick<SearchEngine, "id" | "enabled" | "priority" | "transport" | "api_key" | "base_url">;
	query?: string;
	use_saved_key: boolean;
	human_assist_enabled: boolean;
};

const testWebSearchEngine = TestWebSearchEngine as unknown as (
	request: TestWebSearchEngineRequest,
) => ReturnType<typeof TestWebSearchEngine>;
const PRESET_ORDER: Record<Exclude<Preset, "custom">, string[]> = {
	mainland: ["bing_cn", "baidu", "duckduckgo", "google", "brave", "serper", "tinyfish", "tavily", "maclaw_hub"],
	international: ["google", "duckduckgo", "bing_cn", "baidu", "brave", "serper", "tinyfish", "tavily", "maclaw_hub"],
};
const RETIRED_ENGINE_IDS = new Set(["mojeek"]);

function webSearchErrorMessage(error: unknown, fallback: string): string {
	if (error instanceof Error && error.message.trim()) return error.message.trim();
	if (typeof error === "string" && error.trim()) return error.trim();
	if (error && typeof error === "object" && "message" in error) {
		const message = (error as { message?: unknown }).message;
		if (typeof message === "string" && message.trim()) return message.trim();
	}
	const text = String(error ?? "").trim();
	return text && text !== "[object Object]" ? text : fallback;
}

function normalizeStrategy(raw: any): SearchStrategy {
	const engines: SearchEngine[] = [];
	const seenEngineIDs = new Set<string>();
	for (const [index, engine] of (Array.isArray(raw?.engines) ? raw.engines : []).entries()) {
		const id = String(engine?.id || "").trim().toLowerCase();
		if (!id || RETIRED_ENGINE_IDS.has(id) || seenEngineIDs.has(id)) continue;
		seenEngineIDs.add(id);
		engines.push({
			id,
			name: String(engine?.name || engine?.id || id).trim() || id,
			enabled: Boolean(engine?.enabled),
			priority: Number(engine?.priority) || index + 1,
			transport: engine?.transport || "http_html",
			needs_api_key: Boolean(engine?.needs_api_key),
			has_api_key: Boolean(engine?.has_api_key),
			api_key: "",
			base_url: String(engine?.base_url || ""),
		});
	}
	engines.sort((a, b) => a.priority - b.priority);
    return {
        version: Number(raw?.version) || 1,
        preset: ["mainland", "international", "custom"].includes(raw?.preset) ? raw.preset : "mainland",
        mode: ["priority", "smart", "aggregate"].includes(raw?.mode) ? raw.mode : "priority",
		engines,
        browser_fallback_enabled: raw?.browser_fallback_enabled !== false,
        browser_fallback_engine_id: raw?.browser_fallback_engine_id === "google" ? "google" : "bing_cn",
		browser_human_assist_enabled: raw?.browser_human_assist_enabled === true,
        hedging_delay_ms: Number(raw?.hedging_delay_ms) || 500,
        min_results_before_hedge: Number(raw?.min_results_before_hedge) || 3,
    };
}

export function WebSearchConfigPanel({ lang }: Props) {
    const { showConfirm } = useDialog();
    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === "zh-Hans" ? zhHans : lang === "zh-Hant" ? zhHant : en, [lang]);
    const [strategy, setStrategy] = useState<SearchStrategy | null>(null);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
	const [resetting, setResetting] = useState(false);
    const [saved, setSaved] = useState(false);
    const [error, setError] = useState("");
    const [tests, setTests] = useState<Record<string, TestState>>({});
	const [clearedAPIKeyEngineIDs, setClearedAPIKeyEngineIDs] = useState<Set<string>>(() => new Set());
    const [draggedEngineID, setDraggedEngineID] = useState<string | null>(null);
    const savedTimer = useRef<number | null>(null);
	const mounted = useRef(true);
	const strategyRef = useRef<SearchStrategy | null>(null);
	const clearedAPIKeyEngineIDsRef = useRef<Set<string>>(new Set());
			const mutationInFlight = useRef(false);
			const resetConfirmationInFlight = useRef(false);
		const editVersion = useRef(0);
		const loadRequestVersion = useRef(0);
		const loadInFlightVersion = useRef<number | null>(null);
		const testRequestVersions = useRef<Record<string, number>>({});
		const testInFlightEngineIDs = useRef<Set<string>>(new Set());
	const busy = saving || resetting;
	const hasActiveTests = Object.values(tests).some(test => test.state === "testing");

	useEffect(() => { strategyRef.current = strategy; }, [strategy]);

	    const load = useCallback(async () => {
			if (loadInFlightVersion.current !== null) return;
			const requestVersion = loadRequestVersion.current + 1;
			loadRequestVersion.current = requestVersion;
			loadInFlightVersion.current = requestVersion;
        setLoading(true);
        setError("");
	        try {
				const next = normalizeStrategy(await GetWebSearchStrategy());
				if (!mounted.current || loadRequestVersion.current !== requestVersion) return;
				if (next.engines.length === 0) {
					throw new Error(t(
						"No search engines were returned. Check the application configuration and retry.",
						"未返回可用的搜索引擎，请检查应用配置后重试。",
						"未回傳可用的搜尋引擎，請檢查應用程式設定後重試。",
					));
				}
				strategyRef.current = next;
				setStrategy(next);
        } catch (err) {
			if (!mounted.current || loadRequestVersion.current !== requestVersion) return;
			setError(webSearchErrorMessage(err, t("Unable to load search strategy.", "无法加载搜索策略。")));
	        } finally {
				if (loadInFlightVersion.current === requestVersion) loadInFlightVersion.current = null;
				if (mounted.current && loadRequestVersion.current === requestVersion) setLoading(false);
	        }
	    }, [t]);

	useEffect(() => {
		mounted.current = true;
		void load();
		return () => {
			mounted.current = false;
					mutationInFlight.current = false;
					resetConfirmationInFlight.current = false;
				loadRequestVersion.current += 1;
				loadInFlightVersion.current = null;
			if (savedTimer.current !== null) window.clearTimeout(savedTimer.current);
		};
	}, [load]);

	const markEdited = useCallback(() => {
		editVersion.current += 1;
		setSaved(false);
		setError("");
	}, []);

	const markCustom = useCallback((next: SearchStrategy): SearchStrategy => ({ ...next, preset: "custom" }), []);
	const applyStrategyUpdate = useCallback((update: (current: SearchStrategy | null) => SearchStrategy | null) => {
		const current = strategyRef.current;
		const next = update(current);
		if (next === current) return;
		strategyRef.current = next;
		setStrategy(next);
	}, []);
	const applyClearedAPIKeyUpdate = useCallback((update: (current: Set<string>) => Set<string>) => {
		const next = update(clearedAPIKeyEngineIDsRef.current);
		clearedAPIKeyEngineIDsRef.current = next;
		setClearedAPIKeyEngineIDs(next);
	}, []);

	const invalidateEngineTest = useCallback((id: string) => {
		testRequestVersions.current[id] = (testRequestVersions.current[id] || 0) + 1;
		setTests(current => {
			const test = current[id];
			// Keep an in-flight test visible until its backend call settles. Its
			// request version is already invalidated, so the result will be ignored;
			// preserving the testing state also keeps Save/Reset honestly disabled.
			if (!test || test.state === "idle" || test.state === "testing") return current;
			return { ...current, [id]: { state: "idle" } };
		});
	}, []);

	const invalidateBrowserEngineTests = useCallback(() => {
		for (const engine of strategyRef.current?.engines || []) {
			if (engine.transport === "browser") invalidateEngineTest(engine.id);
		}
	}, [invalidateEngineTest]);

	const clearSavedAPIKey = useCallback((id: string) => {
		if (busy || mutationInFlight.current) return;
		markEdited();
		applyClearedAPIKeyUpdate(current => {
			const next = new Set(current);
			next.add(id);
			return next;
		});
		invalidateEngineTest(id);
		applyStrategyUpdate(current => current ? markCustom({
			...current,
			engines: current.engines.map(engine => engine.id === id
				? { ...engine, enabled: false, has_api_key: false, api_key: "" }
				: engine),
		}) : current);
	}, [applyClearedAPIKeyUpdate, applyStrategyUpdate, busy, invalidateEngineTest, markCustom, markEdited]);

	const updateEngine = useCallback((id: string, patch: Partial<SearchEngine>) => {
		if (busy || mutationInFlight.current) return;
		markEdited();
		invalidateEngineTest(id);
		applyStrategyUpdate(current => current ? markCustom({
			...current,
			engines: current.engines.map(engine => engine.id === id ? { ...engine, ...patch } : engine),
		}) : current);
	}, [applyStrategyUpdate, busy, invalidateEngineTest, markCustom, markEdited]);

    const moveEngine = useCallback((id: string, direction: -1 | 1) => {
		if (busy || mutationInFlight.current) return;
		markEdited();
		applyStrategyUpdate(current => {
            if (!current) return current;
            const index = current.engines.findIndex(engine => engine.id === id);
            const target = index + direction;
            if (index < 0 || target < 0 || target >= current.engines.length) return current;
            const engines = [...current.engines];
            [engines[index], engines[target]] = [engines[target], engines[index]];
            return markCustom({ ...current, engines: engines.map((engine, i) => ({ ...engine, priority: i + 1 })) });
        });
	}, [applyStrategyUpdate, busy, markCustom, markEdited]);

    const dropEngine = useCallback((targetID: string) => {
		if (busy || mutationInFlight.current) return;
        if (!draggedEngineID || draggedEngineID === targetID) return;
		markEdited();
		applyStrategyUpdate(current => {
            if (!current) return current;
            const from = current.engines.findIndex(engine => engine.id === draggedEngineID);
            const to = current.engines.findIndex(engine => engine.id === targetID);
            if (from < 0 || to < 0) return current;
            const engines = [...current.engines];
            const [moved] = engines.splice(from, 1);
            engines.splice(to, 0, moved);
            return markCustom({ ...current, engines: engines.map((engine, index) => ({ ...engine, priority: index + 1 })) });
        });
        setDraggedEngineID(null);
	}, [applyStrategyUpdate, busy, draggedEngineID, markCustom, markEdited]);

    const selectPreset = useCallback((preset: Preset) => {
		if (busy || mutationInFlight.current) return;
		markEdited();
		applyStrategyUpdate(current => {
            if (!current || preset === "custom") return current ? { ...current, preset } : current;
            const rank = new Map(PRESET_ORDER[preset].map((id, index) => [id, index]));
            const engines = [...current.engines]
                .sort((a, b) => (rank.get(a.id) ?? Number.MAX_SAFE_INTEGER) - (rank.get(b.id) ?? Number.MAX_SAFE_INTEGER))
                .map((engine, index) => ({ ...engine, priority: index + 1 }));
            return { ...current, preset, engines };
        });
	}, [applyStrategyUpdate, busy, markEdited]);

	    const reset = useCallback(async () => {
		    if (!strategyRef.current || mutationInFlight.current || resetConfirmationInFlight.current || testInFlightEngineIDs.current.size > 0) return;
		resetConfirmationInFlight.current = true;
		let confirmed = false;
		try {
	        confirmed = await showConfirm(t(
            "Restore this preset's engine order and enabled states? Saved API keys will be kept.",
            "恢复该预设的引擎顺序和启用状态？已保存的 API Key 会保留。",
            "恢復該預設的引擎順序和啟用狀態？已儲存的 API Key 會保留。",
        ), t("Reset search defaults", "重置搜索默认设置", "重置搜尋預設設定"), {
            confirmText: t("Reset", "重置"),
            cancelText: t("Cancel", "取消"),
	        });
		} finally {
			resetConfirmationInFlight.current = false;
		}
		if (!confirmed || !mounted.current) return;
		// The confirmation dialog is asynchronous. A test or another mutation may
		// have started while it was open, and the selected preset may have changed.
		// Re-read the refs before committing the destructive reset operation.
		if (mutationInFlight.current || testInFlightEngineIDs.current.size > 0) return;
		const currentStrategy = strategyRef.current;
		if (!currentStrategy) return;
		const preset = currentStrategy.preset === "international" ? "international" : "mainland";
		const submittedVersion = editVersion.current;
		mutationInFlight.current = true;
		setResetting(true);
        setError("");
        try {
			const next = normalizeStrategy(await ResetWebSearchStrategy(preset));
			if (!mounted.current) return;
			if (editVersion.current === submittedVersion) {
				editVersion.current += 1;
					strategyRef.current = next;
					setStrategy(next);
					clearedAPIKeyEngineIDsRef.current = new Set();
					setClearedAPIKeyEngineIDs(clearedAPIKeyEngineIDsRef.current);
					setTests({});
				setSaved(true);
			}
        } catch (err) {
			if (!mounted.current) return;
			setError(webSearchErrorMessage(err, t("Unable to reset search defaults.", "无法重置搜索默认设置。")));
        } finally {
			mutationInFlight.current = false;
			if (mounted.current) setResetting(false);
        }
	    }, [showConfirm, t]);

	    const save = useCallback(async () => {
		const submittedStrategy = strategyRef.current;
		        if (!submittedStrategy || mutationInFlight.current || testInFlightEngineIDs.current.size > 0) return;
	        const invalid = submittedStrategy.engines.find(engine => engine.enabled && engine.needs_api_key && !engine.has_api_key && !engine.api_key?.trim());
        if (invalid) {
            setError(t(`${invalid.name} requires an API key before it can be enabled.`, `${invalid.name} 需要填写 API Key 后才能启用。`));
            return;
        }
		mutationInFlight.current = true;
	        setSaving(true);
        setSaved(false);
        setError("");
		const submittedVersion = editVersion.current;
			const submittedKeys = new Map(submittedStrategy.engines.map(engine => [engine.id, engine.api_key?.trim() || ""]));
		const submittedClearedIDs = new Set(clearedAPIKeyEngineIDsRef.current);
        try {
            await SaveWebSearchStrategy({
	                ...submittedStrategy,
				clear_api_key_engine_ids: Array.from(submittedClearedIDs),
	                engines: submittedStrategy.engines.map((engine, index) => ({
                    id: engine.id,
                    enabled: engine.enabled,
                    priority: index + 1,
                    transport: engine.transport,
                    api_key: engine.api_key?.trim() || "",
                    base_url: engine.base_url || "",
                })),
            } as main.SaveWebSearchStrategyRequest);
			if (!mounted.current) return;
			applyStrategyUpdate(current => current ? {
	                ...current,
                engines: current.engines.map(engine => ({
                    ...engine,
					has_api_key: submittedClearedIDs.has(engine.id) ? false : engine.has_api_key || Boolean(submittedKeys.get(engine.id)),
					api_key: engine.api_key?.trim() === submittedKeys.get(engine.id) ? "" : engine.api_key,
                })),
	            } : current);
			applyClearedAPIKeyUpdate(current => {
				const next = new Set(current);
				for (const id of submittedClearedIDs) next.delete(id);
				return next;
			});
			const unchangedSinceSubmit = editVersion.current === submittedVersion;
			setSaved(unchangedSinceSubmit);
            if (savedTimer.current !== null) window.clearTimeout(savedTimer.current);
			if (unchangedSinceSubmit) {
				savedTimer.current = window.setTimeout(() => { setSaved(false); savedTimer.current = null; }, 1800);
			}
        } catch (err) {
			if (!mounted.current) return;
			setError(webSearchErrorMessage(err, t("Unable to save search strategy.", "无法保存搜索策略。")));
        } finally {
			mutationInFlight.current = false;
			if (mounted.current) setSaving(false);
	        }
	}, [applyClearedAPIKeyUpdate, applyStrategyUpdate, t]);

    const testEngine = useCallback(async (engine: SearchEngine) => {
		if (busy || mutationInFlight.current || testInFlightEngineIDs.current.has(engine.id)) return;
		testInFlightEngineIDs.current.add(engine.id);
		const requestVersion = (testRequestVersions.current[engine.id] || 0) + 1;
		testRequestVersions.current[engine.id] = requestVersion;
        setTests(current => ({ ...current, [engine.id]: { state: "testing" } }));
        try {
	            const result = await testWebSearchEngine({
				engine: {
					id: engine.id,
					enabled: true,
					priority: 1,
					transport: engine.transport,
					api_key: engine.api_key?.trim() || "",
					base_url: engine.base_url || "",
				},
				query: engine.id === "maclaw_hub" ? "golang http server" : undefined,
				use_saved_key: engine.has_api_key && !clearedAPIKeyEngineIDsRef.current.has(engine.id) && !engine.api_key?.trim(),
				human_assist_enabled: strategyRef.current?.browser_human_assist_enabled === true,
				});
			if (!mounted.current || testRequestVersions.current[engine.id] !== requestVersion) return;
			const typedResult = result as typeof result & {
				retry_count?: number;
				preview_title?: string;
				preview_url?: string;
				preview_snippet?: string;
			};
			const retryCount = Number(typedResult?.retry_count || 0);
			const preview: TestPreview = {
				title: String(typedResult?.preview_title || "").trim(),
				url: String(typedResult?.preview_url || "").trim(),
				snippet: String(typedResult?.preview_snippet || "").trim(),
			};
			setTests(current => ({
                ...current,
                [engine.id]: {
                    state: "success",
					message: t(
						`${result?.result_count || 0} results · ${result?.duration_ms || 0} ms${retryCount ? " · retried once" : ""}`,
						`${result?.result_count || 0} 条结果 · ${result?.duration_ms || 0} 毫秒${retryCount ? " · 已重试 1 次" : ""}`,
						`${result?.result_count || 0} 筆結果 · ${result?.duration_ms || 0} 毫秒${retryCount ? " · 已重試 1 次" : ""}`,
					),
					preview: preview.title || preview.url ? preview : undefined,
                },
            }));
        } catch (err) {
			if (!mounted.current || testRequestVersions.current[engine.id] !== requestVersion) return;
			setTests(current => ({ ...current, [engine.id]: {
				state: "error",
				message: webSearchErrorMessage(err, t("Engine test failed.", "引擎测试失败。")),
			} }));
			} finally {
				testInFlightEngineIDs.current.delete(engine.id);
				if (mounted.current && testRequestVersions.current[engine.id] !== requestVersion) {
					setTests(current => current[engine.id]?.state === "testing"
						? { ...current, [engine.id]: { state: "idle" } }
						: current);
				}
	        }
	}, [busy, t]);

    const activeCount = useMemo(() => strategy?.engines.filter(engine => engine.enabled).length || 0, [strategy]);

    if (loading) return (
        <div className="web-search-config__loading" role="status" aria-label={t("Loading search strategy", "正在加载搜索策略")}>
            <span className="web-search-config__loading-line" />
            <span className="web-search-config__loading-line" />
            <span className="web-search-config__loading-line" />
        </div>
    );
	    if (!strategy) return (
			<div className="web-search-config__load-error" role="alert">
				<p>{error || t("Search strategy is unavailable.", "搜索策略不可用。", "搜尋策略不可用。")}</p>
				<button type="button" onClick={() => void load()} disabled={loading}>
					{loading ? t("Retrying…", "正在重试…", "正在重試…") : t("Retry", "重试", "重試")}
				</button>
			</div>
		);

    return (
        <div className="web-search-config">
            <header className="web-search-config__header">
                <div>
                    <h3>{t("Search strategy", "搜索策略", "搜尋策略")}</h3>
                    <p>{t(
                        "Search works without API keys. Engines are tried from top to bottom; drag them into the order that matches your network.",
                        "无需 API Key 也能搜索。系统从上到下尝试引擎，可按你的网络环境调整顺序。",
                        "無需 API Key 也能搜尋。系統從上到下嘗試引擎，可按你的網路環境調整順序。",
                    )}</p>
                </div>
                <span className="web-search-config__count">{t(`${activeCount} active`, `已启用 ${activeCount} 个`)}</span>
            </header>

            <section className="web-search-config__controls" aria-label={t("Search defaults", "搜索默认设置")}>
                <label>
                    <span>{t("Preset", "预设")}</span>
					<select value={strategy.preset} disabled={busy} onChange={event => selectPreset(event.target.value as Preset)}>
                        <option value="mainland">{t("Mainland China first", "中国大陆优先")}</option>
                        <option value="international">{t("International first", "国际网络优先")}</option>
                        <option value="custom">{t("Custom", "自定义")}</option>
                    </select>
                </label>
                <label>
                    <span>{t("Mode", "搜索模式")}</span>
					<select value={strategy.mode} disabled={busy} onChange={event => {
						markEdited();
						applyStrategyUpdate(current => current ? { ...current, mode: event.target.value as SearchMode, preset: "custom" } : current);
					}}>
                        <option value="priority">{t("Priority", "按优先级")}</option>
                        <option value="smart" disabled>{t("Smart (coming soon)", "智能（即将推出）")}</option>
                        <option value="aggregate" disabled>{t("Aggregate (coming soon)", "聚合（即将推出）")}</option>
                    </select>
                </label>
				<button type="button" className="web-search-config__secondary" onClick={() => void reset()} disabled={busy || hasActiveTests}>
					{resetting ? t("Resetting…", "正在重置…") : t("Reset default order", "重置默认顺序")}
                </button>
            </section>

            <section className="web-search-config__engine-section">
                <div className="web-search-config__section-title">
                    <div>
                        <h4>{t("Engine order", "搜索引擎顺序")}</h4>
                        <p>{t("Use the arrow buttons or drag handles to change the attempt order.", "使用方向按钮或拖拽手柄调整尝试顺序。")}</p>
                    </div>
                </div>

                <ol className="web-search-config__engine-list" aria-label={t("Search engines in priority order", "按优先级排序的搜索引擎")}>
                    {strategy.engines.map((engine, index) => {
                        const test = tests[engine.id] || { state: "idle" as const };
                        return (
                            <li
                                key={engine.id}
                                className="web-search-config__engine"
                                data-enabled={engine.enabled ? "true" : "false"}
                                data-dragging={draggedEngineID === engine.id ? "true" : "false"}
                                onDragOver={event => { event.preventDefault(); event.dataTransfer.dropEffect = "move"; }}
                                onDrop={event => { event.preventDefault(); dropEngine(engine.id); }}
                                onDragEnd={() => setDraggedEngineID(null)}
                            >
                                <div className="web-search-config__rank" aria-label={t(`Priority ${index + 1}`, `优先级 ${index + 1}`)}>
                                    <span>{index + 1}</span>
                                    <small>{t("Priority", "顺序")}</small>
                                </div>
                                <div className="web-search-config__move">
                                    <button
                                        type="button"
                                        className="web-search-config__drag-handle"
                                        draggable
										disabled={busy}
                                        aria-label={t(`Drag ${engine.name} to reorder`, `拖拽 ${engine.name} 调整顺序`)}
                                        title={t("Drag to reorder", "拖拽调整顺序")}
                                        onDragStart={event => { event.dataTransfer.effectAllowed = "move"; setDraggedEngineID(engine.id); }}
                                        onDragEnd={() => setDraggedEngineID(null)}
                                    >
                                        <svg viewBox="0 0 16 16" aria-hidden="true">
                                            <circle cx="5" cy="3" r="1" /><circle cx="11" cy="3" r="1" />
                                            <circle cx="5" cy="8" r="1" /><circle cx="11" cy="8" r="1" />
                                            <circle cx="5" cy="13" r="1" /><circle cx="11" cy="13" r="1" />
                                        </svg>
                                    </button>
									<button type="button" aria-label={t(`Move ${engine.name} up`, `上移 ${engine.name}`)} disabled={busy || index === 0} onClick={() => moveEngine(engine.id, -1)}>↑</button>
									<button type="button" aria-label={t(`Move ${engine.name} down`, `下移 ${engine.name}`)} disabled={busy || index === strategy.engines.length - 1} onClick={() => moveEngine(engine.id, 1)}>↓</button>
                                </div>
                                <label className="web-search-config__switch">
									<input type="checkbox" checked={engine.enabled} disabled={busy} onChange={event => updateEngine(engine.id, { enabled: event.target.checked })} />
                                    <span aria-hidden="true" />
                                    <span className="web-search-config__sr-only">{t(`Enable ${engine.name}`, `启用 ${engine.name}`)}</span>
                                </label>
                                <div className="web-search-config__engine-copy">
                                    <div className="web-search-config__engine-name">
                                        <strong>{engine.name}</strong>
                                        <span data-transport={engine.transport}>{engine.transport === "api" ? "API" : engine.transport === "browser" ? t("Browser", "浏览器") : t("Direct web", "网页直连")}</span>
                                    </div>
                                    <p>{engine.needs_api_key
                                        ? engine.has_api_key ? t("API key saved", "API Key 已保存") : t("API key required", "需要 API Key")
                                        : engine.id === "maclaw_hub" ? t("Uses signed-in MaClaw Hub account", "使用已登录的 MaClaw Hub 账号", "使用已登入的 MaClaw Hub 帳號")
                                        : engine.id === "google" ? t("Free · availability depends on network", "免费 · 可用性取决于网络") : t("Free · no key needed", "免费 · 无需 Key")}</p>
                                    {test.state !== "idle" && <div className="web-search-config__test-result" data-state={test.state} role="status">
                                        {test.state === "testing" ? t("Testing…", "正在测试…") : test.message}
                                        {test.state === "success" && test.preview?.title && (
                                            <div className="web-search-config__test-preview">
                                                <strong>{test.preview.title}</strong>
                                                {test.preview.url && <span>{test.preview.url}</span>}
                                                {test.preview.snippet && <em>{test.preview.snippet}</em>}
                                            </div>
                                        )}
                                    </div>}
                                </div>
                                {engine.needs_api_key && (
									<div className="web-search-config__key-field">
										<input
											className="web-search-config__key"
											type="password"
											disabled={busy}
										value={engine.api_key || ""}
										onChange={event => {
											if (busy || mutationInFlight.current) return;
											if (event.target.value) applyClearedAPIKeyUpdate(current => {
												const next = new Set(current);
												next.delete(engine.id);
												return next;
											});
											updateEngine(engine.id, { api_key: event.target.value });
											}}
											placeholder={engine.has_api_key ? t("Saved · enter to replace", "已保存 · 输入新 Key 可替换") : "API Key"}
											autoComplete="new-password"
											aria-label={`${engine.name} API Key`}
										/>
										{engine.has_api_key && <button type="button" disabled={busy} onClick={() => clearSavedAPIKey(engine.id)}>
											{t("Remove", "移除")}
										</button>}
									</div>
                                )}
								<button type="button" className="web-search-config__test" disabled={busy || test.state === "testing"} onClick={() => void testEngine(engine)}>
                                    {test.state === "testing" ? t("Testing", "测试中") : t("Test", "测试")}
                                </button>
                            </li>
                        );
                    })}
                </ol>
            </section>

            <section className="web-search-config__fallback">
                <label className="web-search-config__fallback-toggle web-search-config__fallback-main">
					<input type="checkbox" checked={strategy.browser_fallback_enabled} disabled={busy} onChange={event => {
						if (mutationInFlight.current) return;
						markEdited();
						applyStrategyUpdate(current => current ? { ...current, browser_fallback_enabled: event.target.checked, preset: "custom" } : current);
					}} />
                    <span>
                        <strong>{t("Allow final browser fallback", "允许浏览器最终保底")}</strong>
                        <small>{t(
							"Runs after enabled engines fail or return too few results. It uses a background tab and does not reuse login cookies.",
							"在已启用引擎失败或结果不足后运行，使用后台标签页且不复用登录 Cookie。",
							"在已啟用引擎失敗或結果不足後執行，使用背景分頁且不重用登入 Cookie。",
						)}</small>
                    </span>
                </label>
                <select
					disabled={busy || !strategy.browser_fallback_enabled}
                    value={strategy.browser_fallback_engine_id}
					onChange={event => {
						if (mutationInFlight.current) return;
						markEdited();
						applyStrategyUpdate(current => current ? { ...current, browser_fallback_engine_id: event.target.value as "bing_cn" | "google", preset: "custom" } : current);
					}}
                    aria-label={t("Fallback engine", "保底引擎")}
                >
                    <option value="bing_cn">Bing</option>
                    <option value="google">Google</option>
                </select>
                <label className="web-search-config__fallback-toggle web-search-config__human-assist">
					<input
						type="checkbox"
						aria-label={t("Allow human verification assistance", "允许人工辅助验证")}
						checked={strategy.browser_human_assist_enabled}
						disabled={busy}
							onChange={event => {
								if (mutationInFlight.current) return;
								markEdited();
								invalidateBrowserEngineTests();
								applyStrategyUpdate(current => current ? { ...current, browser_human_assist_enabled: event.target.checked, preset: "custom" } : current);
						}}
					/>
					<span>
						<strong>{t("Allow human verification assistance", "允许人工辅助验证")}</strong>
						<small>{t(
						"Applies to Google and final browser fallback. If a CAPTCHA or slider appears, bring the page forward and wait for you to complete it. Search never tries to solve or bypass verification automatically.",
						"适用于 Google 和浏览器最终保底。遇到验证码或滑块时，自动打开验证页面并等待你手动完成；系统不会自动破解或绕过验证。",
						)}</small>
					</span>
				</label>
            </section>

            {error && <div className="web-search-config__error" role="alert">{error}</div>}
            <footer className="web-search-config__footer">
                <span aria-live="polite">{saved ? t("Search strategy saved.", "搜索策略已保存。") : ""}</span>
				<button type="button" className="web-search-config__save" onClick={() => void save()} disabled={busy || hasActiveTests}>
                    {saving ? t("Saving…", "正在保存…") : saved ? t("Saved", "已保存") : t("Save strategy", "保存策略")}
                </button>
            </footer>
        </div>
    );
}
