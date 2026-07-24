import { useState, useCallback, useEffect, useRef } from 'react';
import { EventsOn, EventsOff } from '../../../wailsjs/runtime';
import { KnowledgeDeepCrawlCancel } from '../../../wailsjs/go/main/App';

/**
 * DeepCrawlPanel — UI for configuring and launching a deep crawl from a seed URL.
 *
 * Provides:
 * - Seed URL input with http/https validation
 * - Depth selector (1-5, default 2)
 * - Same-domain toggle (default enabled)
 * - Save scope, topic hint, and labels fields
 * - Preview button and Start Crawl button
 * - Progress display with cancel functionality (Req 3.1, 3.2, 3.3, 3.5)
 * - Preview results display grouped by depth (Req 4.2, 4.3, 4.4, 4.5)
 *
 * Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 3.1, 3.2, 3.3, 3.5, 4.2, 4.3, 4.4, 4.5
 */

export interface DeepCrawlConfig {
    seedURL: string;
    maxDepth: number;       // 1-5, default 2
    sameDomainOnly: boolean; // default true
    saveScope: string;
    topicHint: string;
    labels: string[];
    clientRunID?: string;
}

export interface DeepCrawlProgress {
    job_id: string;
    mode?: 'preview' | 'crawl';
    client_run_id?: string;
    status: 'discovering' | 'crawling' | 'completed' | 'cancelled' | 'failed' | 'timeout' | 'limit_reached';
    current_depth: number;
    max_depth: number;
    total_discovered: number;
    completed: number;
    pending: number;
    failed: number;
    skipped: number;
    current_url?: string;
}

export interface DeepCrawlPreviewResult {
    by_depth: Array<{
        depth: number;
        total: number;
        urls: string[];
    }>;
    total_discovered?: number;
    status?: DeepCrawlProgress['status'];
}

export interface DeepCrawlRunResult {
    job_id?: string;
    status?: DeepCrawlProgress['status'];
    total_discovered?: number;
    total_saved?: number;
    failed?: number;
    skipped?: number;
}

export interface DeepCrawlPanelProps {
    lang?: string;
    /** Called when user clicks "Preview". Receives the current config. */
    onPreview?: (config: DeepCrawlConfig) => Promise<DeepCrawlPreviewResult | void> | DeepCrawlPreviewResult | void;
    /** Called when user clicks "Start Crawl". Receives the current config. */
    onStartCrawl?: (config: DeepCrawlConfig) => Promise<DeepCrawlRunResult | void> | DeepCrawlRunResult | void;
    /** Whether a crawl/preview is currently in progress. Disables buttons. */
    busy?: boolean;
}

function t(lang: string | undefined, en: string, zh: string): string {
    return lang === 'en' ? en : zh;
}

/** Validates that a URL starts with http:// or https:// */
function isValidHTTPURL(url: string): boolean {
    const trimmed = url.trim();
    if (!trimmed) return false;
    return /^https?:\/\/.+/i.test(trimmed);
}

/** Parse comma/semicolon/newline separated labels into a deduplicated array */
function parseLabels(value: string): string[] {
    const seen = new Set<string>();
    const result: string[] = [];
    for (const part of value.split(/[\r\n\t,;，；、]+/)) {
        const normalized = part.replace(/\s+/g, ' ').trim().toLowerCase();
        if (!normalized || seen.has(normalized)) continue;
        seen.add(normalized);
        result.push(normalized);
    }
    return result;
}

/** Truncate a URL for display */
function truncateURL(url: string, maxLen = 60): string {
    if (url.length <= maxLen) return url;
    return url.slice(0, maxLen - 3) + '...';
}

export function previewTotalDiscovered(result: DeepCrawlPreviewResult | null): number {
    if (!result) return 0;
    if (typeof result.total_discovered === 'number') return result.total_discovered;
    return (result.by_depth || []).reduce((sum, level) => sum + (level.total || level.urls?.length || 0), 0);
}

function previewStatusText(lang: string | undefined, result: DeepCrawlPreviewResult): string {
    if (result.status !== 'limit_reached') return '';
    return t(lang, 'Limit reached; showing the first discovered URLs', '已达到抓取上限，当前仅展示先发现的 URL');
}

export function DeepCrawlPanel({ lang, onPreview, onStartCrawl, busy }: DeepCrawlPanelProps) {
    const [seedURL, setSeedURL] = useState('');
    const [maxDepth, setMaxDepth] = useState(2);
    const [sameDomainOnly, setSameDomainOnly] = useState(true);
    const [saveScope, setSaveScope] = useState('');
    const [topicHint, setTopicHint] = useState('');
    const [labelsText, setLabelsText] = useState('');
    const [urlTouched, setURLTouched] = useState(false);

    // Progress state (task 6.2)
    const [progress, setProgress] = useState<DeepCrawlProgress | null>(null);
    const [isCrawling, setIsCrawling] = useState(false);
    const activeClientRunIDRef = useRef<string | null>(null);

    // Preview results state (task 6.3)
    const [previewResult, setPreviewResult] = useState<DeepCrawlPreviewResult | null>(null);
    const [expandedDepths, setExpandedDepths] = useState<Set<number>>(new Set());

    const urlValid = isValidHTTPURL(seedURL);
    const showURLError = urlTouched && seedURL.trim() !== '' && !urlValid;

    // Listen to deep-crawl-progress Wails event (Req 3.1)
    useEffect(() => {
        const unsub = EventsOn('knowledge:deep-crawl-progress', (data: DeepCrawlProgress) => {
            if (!data) return;
            // Preview and crawl share the backend transport, but they are distinct UI lifecycles.
            // Preview progress is represented by the preview result/busy state and must not
            // start or finish the crawl UI, especially when an older preview event arrives late.
            if (data.mode === 'preview') return;
            if (activeClientRunIDRef.current && data.client_run_id !== activeClientRunIDRef.current) {
                return;
            }

            setProgress(data);
            if (data.status === 'discovering' || data.status === 'crawling') {
                setIsCrawling(true);
            } else {
                setIsCrawling(false);
                if (data.client_run_id === activeClientRunIDRef.current) {
                    activeClientRunIDRef.current = null;
                }
            }
        });
        return () => {
            if (typeof unsub === 'function') unsub();
            else EventsOff('knowledge:deep-crawl-progress');
        };
    }, []);

    const buildConfig = useCallback((): DeepCrawlConfig => ({
        seedURL: seedURL.trim(),
        maxDepth,
        sameDomainOnly,
        saveScope: saveScope.trim(),
        topicHint: topicHint.trim(),
        labels: parseLabels(labelsText),
    }), [seedURL, maxDepth, sameDomainOnly, saveScope, topicHint, labelsText]);

    const handlePreview = useCallback(async () => {
        if (!urlValid || !onPreview) return;
        setPreviewResult(null);
        setProgress(null);
        const result = await onPreview(buildConfig());
        if (result && 'by_depth' in result) {
            setPreviewResult(result);
            // Expand all depth levels by default
            const depths = new Set(result.by_depth.map(d => d.depth));
            setExpandedDepths(depths);
        }
    }, [urlValid, onPreview, buildConfig]);

    const createClientRunID = useCallback(() => `deep-crawl-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`, []);

    const initialCrawlProgress = useCallback((config: DeepCrawlConfig, totalHint = 1): DeepCrawlProgress => ({
        job_id: '',
        mode: 'crawl',
        client_run_id: config.clientRunID,
        status: 'crawling',
        current_depth: 0,
        max_depth: config.maxDepth,
        total_discovered: Math.max(1, totalHint),
        completed: 0,
        pending: Math.max(1, totalHint),
        failed: 0,
        skipped: 0,
        current_url: config.seedURL,
    }), []);

    const completeCrawlFromResult = useCallback((config: DeepCrawlConfig, result: DeepCrawlRunResult | void, totalHint = 1) => {
        if (!result || activeClientRunIDRef.current !== config.clientRunID) return;
        const totalDiscovered = Math.max(1, result.total_discovered ?? totalHint);
        setProgress({
            job_id: result.job_id || '',
            mode: 'crawl',
            client_run_id: config.clientRunID,
            status: result.status || 'completed',
            current_depth: config.maxDepth,
            max_depth: config.maxDepth,
            total_discovered: totalDiscovered,
            completed: result.total_saved ?? 0,
            pending: 0,
            failed: result.failed ?? 0,
            skipped: result.skipped ?? 0,
        });
        setIsCrawling(false);
        activeClientRunIDRef.current = null;
    }, []);

    const markCrawlFailed = useCallback((config: DeepCrawlConfig) => {
        setProgress(prev => ({
            ...(prev || initialCrawlProgress(config)),
            status: 'failed',
            pending: 0,
        }));
        setIsCrawling(false);
        if (!config.clientRunID || config.clientRunID === activeClientRunIDRef.current) {
            activeClientRunIDRef.current = null;
        }
    }, [initialCrawlProgress]);

    const handleStartCrawl = useCallback(async () => {
        if (!urlValid || !onStartCrawl) return;
        const config = { ...buildConfig(), clientRunID: createClientRunID() };
        activeClientRunIDRef.current = config.clientRunID;
        setPreviewResult(null);
        setProgress(initialCrawlProgress(config));
        setIsCrawling(true);
        try {
            const result = await onStartCrawl(config);
            completeCrawlFromResult(config, result);
        } catch {
            markCrawlFailed(config);
        }
    }, [urlValid, onStartCrawl, buildConfig, createClientRunID, initialCrawlProgress, completeCrawlFromResult, markCrawlFailed]);

    // Confirm & Start Crawl from preview (Req 4.4)
    const handleConfirmPreview = useCallback(async () => {
        if (!onStartCrawl) return;
        const config = { ...buildConfig(), clientRunID: createClientRunID() };
        activeClientRunIDRef.current = config.clientRunID;
        const totalHint = previewTotalDiscovered(previewResult);
        setPreviewResult(null);
        setProgress(initialCrawlProgress(config, totalHint));
        setIsCrawling(true);
        try {
            const result = await onStartCrawl(config);
            completeCrawlFromResult(config, result, totalHint);
        } catch {
            markCrawlFailed(config);
        }
    }, [onStartCrawl, buildConfig, createClientRunID, previewResult, initialCrawlProgress, completeCrawlFromResult, markCrawlFailed]);

    // Discard preview data (Req 4.5)
    const handleDiscardPreview = useCallback(() => {
        setPreviewResult(null);
        setExpandedDepths(new Set());
    }, []);

    // Cancel crawl (Req 3.5)
    const handleCancel = useCallback(async () => {
        try {
            await KnowledgeDeepCrawlCancel();
        } catch (e) {
            // ignore cancel errors
        }
    }, []);

    // Toggle depth section expansion
    const toggleDepth = useCallback((depth: number) => {
        setExpandedDepths(prev => {
            const next = new Set(prev);
            if (next.has(depth)) next.delete(depth);
            else next.add(depth);
            return next;
        });
    }, []);

    // Compute progress percentage (Req 3.2)
    const processedCount = progress ? progress.completed + progress.failed + progress.skipped : 0;
    const progressPercent = progress && progress.total_discovered > 0
        ? Math.min(100, Math.round((processedCount / progress.total_discovered) * 100))
        : 0;

    const showProgress = !!progress;
    const showPreview = previewResult && !isCrawling;
    const previewTotal = previewTotalDiscovered(previewResult);
    const previewStatus = previewResult ? previewStatusText(lang, previewResult) : '';

    return (
        <div className="deep-crawl-panel">
            <h4 className="deep-crawl-panel__title">{t(lang, 'Deep Crawl', '深度检索')}</h4>

            {/* Seed URL */}
            <div className="deep-crawl-field">
                <label className="deep-crawl-label">{t(lang, 'Seed URL', '种子 URL')}</label>
                <input
                    className={`deep-crawl-input${showURLError ? ' deep-crawl-input--error' : ''}`}
                    type="url"
                    value={seedURL}
                    onChange={e => setSeedURL(e.target.value)}
                    onBlur={() => setURLTouched(true)}
                    placeholder="https://example.com/docs"
                />
                {showURLError && (
                    <span className="deep-crawl-error-text">
                        {t(lang, 'URL must start with http:// or https://', 'URL 必须以 http:// 或 https:// 开头')}
                    </span>
                )}
            </div>

            {/* Depth + Same-domain row */}
            <div className="deep-crawl-row">
                <div className="deep-crawl-field">
                    <label className="deep-crawl-label">{t(lang, 'Max Depth', '最大深度')}</label>
                    <select
                        className="deep-crawl-input"
                        value={maxDepth}
                        onChange={e => setMaxDepth(Number(e.target.value))}
                    >
                        {[1, 2, 3, 4, 5].map(d => (
                            <option key={d} value={d}>{d}</option>
                        ))}
                    </select>
                </div>
                <div className="deep-crawl-field deep-crawl-field--end">
                    <label className="deep-crawl-checkbox">
                        <input
                            type="checkbox"
                            checked={sameDomainOnly}
                            onChange={e => setSameDomainOnly(e.target.checked)}
                        />
                        {t(lang, 'Same domain only', '仅同域')}
                    </label>
                </div>
            </div>

            {/* Metadata fields */}
            <div className="deep-crawl-metadata-grid">
                <div className="deep-crawl-field">
                    <label className="deep-crawl-label">{t(lang, 'Save Scope', '保存范围')}</label>
                    <select
                        className="deep-crawl-input"
                        value={saveScope}
                        onChange={e => setSaveScope(e.target.value)}
                    >
                        <option value="">{t(lang, 'Default', '默认')}</option>
                        <option value="project">{t(lang, 'Project', '项目')}</option>
                        <option value="personal">{t(lang, 'Personal', '个人')}</option>
                        <option value="local_only">{t(lang, 'Local only', '仅本地')}</option>
                    </select>
                </div>
                <div className="deep-crawl-field">
                    <label className="deep-crawl-label">{t(lang, 'Topic Hint', '主题提示')}</label>
                    <input
                        className="deep-crawl-input"
                        value={topicHint}
                        onChange={e => setTopicHint(e.target.value)}
                        placeholder={t(lang, 'e.g. machine learning', '例如 机器学习')}
                    />
                </div>
                <div className="deep-crawl-field">
                    <label className="deep-crawl-label">{t(lang, 'Labels', '标签')}</label>
                    <input
                        className="deep-crawl-input"
                        value={labelsText}
                        onChange={e => setLabelsText(e.target.value)}
                        placeholder={t(lang, 'Comma-separated labels', '逗号分隔的标签')}
                    />
                </div>
            </div>

            {/* Action buttons */}
            <div className="deep-crawl-actions">
                <button
                    type="button"
                    className="deep-crawl-button"
                    disabled={!urlValid || !!busy || isCrawling}
                    onClick={handlePreview}
                >
                    {busy && !isCrawling ? t(lang, 'Working...', '处理中...') : t(lang, 'Preview', '预览')}
                </button>
                <button
                    type="button"
                    className="deep-crawl-button deep-crawl-button--primary"
                    disabled={!urlValid || !!busy || isCrawling}
                    onClick={handleStartCrawl}
                >
                    {isCrawling ? t(lang, 'Crawling...', '抓取中...') : t(lang, 'Start Crawl', '开始抓取')}
                </button>
            </div>

            {/* ── Progress Display (Task 6.2, Req 3.1, 3.2, 3.3, 3.5) ── */}
            {showProgress && progress && (
                <div className="deep-crawl-progress">
                    {/* Progress bar (Req 3.2) */}
                    <div className="deep-crawl-progress__track">
                        <div className="deep-crawl-progress__fill" style={{ width: `${progressPercent}%` }} />
                    </div>
                    <div className="deep-crawl-progress__percent">{progressPercent}%</div>

                    {/* Status text (Req 3.1) */}
                    <div className="deep-crawl-progress__status">
                        {t(lang, 'Status', '状态')}: {progress.status}
                        {' | '}
                        {t(lang, 'Depth', '深度')} {progress.current_depth}/{progress.max_depth}
                        {' | '}
                        {t(lang, 'Discovered', '已发现')}: {progress.total_discovered}
                        {' | '}
                        {t(lang, 'Completed', '已完成')}: {progress.completed}
                        {' | '}
                        {t(lang, 'Failed', '失败')}: {progress.failed}
                    </div>

                    {/* Current URL being processed */}
                    {progress.current_url && (
                        <div className="deep-crawl-progress__url" title={progress.current_url}>
                            {truncateURL(progress.current_url)}
                        </div>
                    )}

                    {/* Cancel button (Req 3.5) */}
                    {isCrawling && (
                        <button
                            type="button"
                            className="deep-crawl-button deep-crawl-button--danger"
                            onClick={handleCancel}
                        >
                            {t(lang, 'Cancel', '取消')}
                        </button>
                    )}
                </div>
            )}

            {/* ── Preview Results Display (Task 6.3, Req 4.2, 4.3, 4.4, 4.5) ── */}
            {showPreview && previewResult && (
                <div className="deep-crawl-preview">
                    {/* Total count header (Req 4.3) */}
                    <div className="deep-crawl-preview__header">
                        {t(lang,
                            `Found ${previewTotal} URLs across ${previewResult.by_depth.length} levels`,
                            `发现 ${previewTotal} 个 URL，共 ${previewResult.by_depth.length} 层`
                        )}
                        {previewStatus && (
                            <span className="deep-crawl-preview__status">
                                {previewStatus}
                            </span>
                        )}
                    </div>

                    {/* Grouped list by depth (Req 4.2) */}
                    <div className="deep-crawl-depth-list">
                        {previewResult.by_depth.map(level => (
                            <div key={level.depth} className="deep-crawl-depth-group">
                                <button
                                    type="button"
                                    className="deep-crawl-depth-header"
                                    aria-expanded={expandedDepths.has(level.depth)}
                                    onClick={() => toggleDepth(level.depth)}
                                >
                                    <span className="deep-crawl-depth-toggle">
                                        {expandedDepths.has(level.depth) ? '-' : '+'}
                                    </span>
                                    {t(lang,
                                        `Depth ${level.depth} (${level.total} URLs)`,
                                        `第 ${level.depth} 层 (${level.total} 个 URL)`
                                    )}
                                </button>
                                {expandedDepths.has(level.depth) && (
                                    <div className="deep-crawl-url-list">
                                        {level.urls.map((url, idx) => (
                                            <div key={idx} className="deep-crawl-url-item" title={url}>
                                                {truncateURL(url, 70)}
                                            </div>
                                        ))}
                                    </div>
                                )}
                            </div>
                        ))}
                    </div>

                    {/* Confirm / Discard buttons (Req 4.4, 4.5) */}
                    <div className="deep-crawl-preview__actions">
                        <button
                            type="button"
                            className="deep-crawl-button deep-crawl-button--primary"
                            onClick={handleConfirmPreview}
                            disabled={!!busy}
                        >
                            {t(lang, 'Confirm & Start Crawl', '确认并开始抓取')}
                        </button>
                        <button
                            type="button"
                            className="deep-crawl-button deep-crawl-button--danger"
                            onClick={handleDiscardPreview}
                        >
                            {t(lang, 'Discard', '放弃')}
                        </button>
                    </div>
                </div>
            )}
        </div>
    );
}

export default DeepCrawlPanel;

