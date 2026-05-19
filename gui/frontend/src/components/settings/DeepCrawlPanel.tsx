import { useState, useCallback, useEffect } from 'react';
import type { CSSProperties } from 'react';
import { colors, radius } from '../remote/styles';
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
}

export interface DeepCrawlProgress {
    job_id: string;
    status: 'discovering' | 'crawling' | 'completed' | 'cancelled' | 'failed';
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
}

export interface DeepCrawlPanelProps {
    lang?: string;
    /** Called when user clicks "Preview". Receives the current config. */
    onPreview?: (config: DeepCrawlConfig) => Promise<DeepCrawlPreviewResult | void> | DeepCrawlPreviewResult | void;
    /** Called when user clicks "Start Crawl". Receives the current config. */
    onStartCrawl?: (config: DeepCrawlConfig) => Promise<void> | void;
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

    // Preview results state (task 6.3)
    const [previewResult, setPreviewResult] = useState<DeepCrawlPreviewResult | null>(null);
    const [expandedDepths, setExpandedDepths] = useState<Set<number>>(new Set());

    const urlValid = isValidHTTPURL(seedURL);
    const showURLError = urlTouched && seedURL.trim() !== '' && !urlValid;

    // Listen to deep-crawl-progress Wails event (Req 3.1)
    useEffect(() => {
        const unsub = EventsOn('knowledge:deep-crawl-progress', (data: DeepCrawlProgress) => {
            if (!data) return;
            setProgress(data);
            if (data.status === 'discovering' || data.status === 'crawling') {
                setIsCrawling(true);
            } else {
                // completed, cancelled, or failed
                setIsCrawling(false);
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

    const handleStartCrawl = useCallback(async () => {
        if (!urlValid || !onStartCrawl) return;
        setPreviewResult(null);
        setProgress(null);
        setIsCrawling(true);
        try {
            await onStartCrawl(buildConfig());
        } finally {
            // If the call finishes before any final progress event is emitted,
            // reset isCrawling to avoid permanently stuck UI.
            setIsCrawling(false);
        }
    }, [urlValid, onStartCrawl, buildConfig]);

    // Confirm & Start Crawl from preview (Req 4.4)
    const handleConfirmPreview = useCallback(async () => {
        if (!onStartCrawl) return;
        setPreviewResult(null);
        setProgress(null);
        setIsCrawling(true);
        try {
            await onStartCrawl(buildConfig());
        } finally {
            setIsCrawling(false);
        }
    }, [onStartCrawl, buildConfig]);

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
    const progressPercent = progress && progress.total_discovered > 0
        ? Math.round((progress.completed / progress.total_discovered) * 100)
        : 0;

    const showProgress = isCrawling || (progress && (progress.status === 'discovering' || progress.status === 'crawling'));
    const showPreview = previewResult && !isCrawling;
    const previewTotal = previewTotalDiscovered(previewResult);

    return (
        <div style={panelStyle}>
            <h4 style={titleStyle}>{t(lang, 'Deep Crawl', '深度检索')}</h4>

            {/* Seed URL */}
            <div style={fieldGroupStyle}>
                <label style={labelStyle}>{t(lang, 'Seed URL', '种子 URL')}</label>
                <input
                    style={{ ...inputStyle, ...(showURLError ? errorInputStyle : {}) }}
                    type="url"
                    value={seedURL}
                    onChange={e => setSeedURL(e.target.value)}
                    onBlur={() => setURLTouched(true)}
                    placeholder="https://example.com/docs"
                />
                {showURLError && (
                    <span style={errorTextStyle}>
                        {t(lang, 'URL must start with http:// or https://', 'URL 必须以 http:// 或 https:// 开头')}
                    </span>
                )}
            </div>

            {/* Depth + Same-domain row */}
            <div style={rowStyle}>
                <div style={fieldGroupStyle}>
                    <label style={labelStyle}>{t(lang, 'Max Depth', '最大深度')}</label>
                    <select
                        style={inputStyle}
                        value={maxDepth}
                        onChange={e => setMaxDepth(Number(e.target.value))}
                    >
                        {[1, 2, 3, 4, 5].map(d => (
                            <option key={d} value={d}>{d}</option>
                        ))}
                    </select>
                </div>
                <div style={{ ...fieldGroupStyle, justifyContent: 'flex-end' }}>
                    <label style={checkboxLabelStyle}>
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
            <div style={metadataGridStyle}>
                <div style={fieldGroupStyle}>
                    <label style={labelStyle}>{t(lang, 'Save Scope', '保存范围')}</label>
                    <select
                        style={inputStyle}
                        value={saveScope}
                        onChange={e => setSaveScope(e.target.value)}
                    >
                        <option value="">{t(lang, 'Default', '默认')}</option>
                        <option value="project">{t(lang, 'Project', '项目')}</option>
                        <option value="personal">{t(lang, 'Personal', '个人')}</option>
                        <option value="local_only">{t(lang, 'Local only', '仅本地')}</option>
                    </select>
                </div>
                <div style={fieldGroupStyle}>
                    <label style={labelStyle}>{t(lang, 'Topic Hint', '主题提示')}</label>
                    <input
                        style={inputStyle}
                        value={topicHint}
                        onChange={e => setTopicHint(e.target.value)}
                        placeholder={t(lang, 'e.g. machine learning', '例如 机器学习')}
                    />
                </div>
                <div style={fieldGroupStyle}>
                    <label style={labelStyle}>{t(lang, 'Labels', '标签')}</label>
                    <input
                        style={inputStyle}
                        value={labelsText}
                        onChange={e => setLabelsText(e.target.value)}
                        placeholder={t(lang, 'Comma-separated labels', '逗号分隔的标签')}
                    />
                </div>
            </div>

            {/* Action buttons */}
            <div style={actionsStyle}>
                <button
                    type="button"
                    style={buttonStyle}
                    disabled={!urlValid || !!busy || isCrawling}
                    onClick={handlePreview}
                >
                    {busy && !isCrawling ? t(lang, 'Working...', '处理中...') : t(lang, 'Preview', '预览')}
                </button>
                <button
                    type="button"
                    style={primaryButtonStyle}
                    disabled={!urlValid || !!busy || isCrawling}
                    onClick={handleStartCrawl}
                >
                    {isCrawling ? t(lang, 'Crawling...', '抓取中...') : t(lang, 'Start Crawl', '开始抓取')}
                </button>
            </div>

            {/* ── Progress Display (Task 6.2, Req 3.1, 3.2, 3.3, 3.5) ── */}
            {showProgress && progress && (
                <div style={progressSectionStyle}>
                    {/* Progress bar (Req 3.2) */}
                    <div style={progressBarContainerStyle}>
                        <div style={{ ...progressBarFillStyle, width: `${progressPercent}%` }} />
                    </div>
                    <div style={progressPercentStyle}>{progressPercent}%</div>

                    {/* Status text (Req 3.1) */}
                    <div style={progressStatusStyle}>
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
                        <div style={currentURLStyle} title={progress.current_url}>
                            {truncateURL(progress.current_url)}
                        </div>
                    )}

                    {/* Cancel button (Req 3.5) */}
                    <button
                        type="button"
                        style={cancelButtonStyle}
                        onClick={handleCancel}
                    >
                        {t(lang, 'Cancel', '取消')}
                    </button>
                </div>
            )}

            {/* ── Preview Results Display (Task 6.3, Req 4.2, 4.3, 4.4, 4.5) ── */}
            {showPreview && previewResult && (
                <div style={previewSectionStyle}>
                    {/* Total count header (Req 4.3) */}
                    <div style={previewHeaderStyle}>
                        {t(lang,
                            `Found ${previewTotal} URLs across ${previewResult.by_depth.length} levels`,
                            `发现 ${previewTotal} 个 URL，共 ${previewResult.by_depth.length} 层`
                        )}
                    </div>

                    {/* Grouped list by depth (Req 4.2) */}
                    <div style={depthListStyle}>
                        {previewResult.by_depth.map(level => (
                            <div key={level.depth} style={depthGroupStyle}>
                                <div
                                    style={depthHeaderStyle}
                                    onClick={() => toggleDepth(level.depth)}
                                >
                                    <span style={depthToggleStyle}>
                                        {expandedDepths.has(level.depth) ? '▼' : '▶'}
                                    </span>
                                    {t(lang,
                                        `Depth ${level.depth} (${level.total} URLs)`,
                                        `第 ${level.depth} 层 (${level.total} 个 URL)`
                                    )}
                                </div>
                                {expandedDepths.has(level.depth) && (
                                    <div style={urlListStyle}>
                                        {level.urls.map((url, idx) => (
                                            <div key={idx} style={urlItemStyle} title={url}>
                                                {truncateURL(url, 70)}
                                            </div>
                                        ))}
                                    </div>
                                )}
                            </div>
                        ))}
                    </div>

                    {/* Confirm / Discard buttons (Req 4.4, 4.5) */}
                    <div style={previewActionsStyle}>
                        <button
                            type="button"
                            style={primaryButtonStyle}
                            onClick={handleConfirmPreview}
                            disabled={!!busy}
                        >
                            {t(lang, 'Confirm & Start Crawl', '确认并开始抓取')}
                        </button>
                        <button
                            type="button"
                            style={cancelButtonStyle}
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

/* ── Styles (matching KnowledgeSettingsPanel patterns) ── */

const panelStyle: CSSProperties = {
    border: `1px solid ${colors.borderLight}`,
    borderRadius: radius.sm,
    padding: 12,
    background: colors.surface,
    display: 'grid',
    gap: 12,
};

const titleStyle: CSSProperties = {
    margin: 0,
    fontSize: 13,
    fontWeight: 800,
    color: colors.text,
};

const fieldGroupStyle: CSSProperties = {
    display: 'grid',
    gap: 4,
};

const labelStyle: CSSProperties = {
    fontSize: 12,
    fontWeight: 600,
    color: colors.textSecondary,
};

const inputStyle: CSSProperties = {
    width: '100%',
    boxSizing: 'border-box',
    border: `1px solid ${colors.border}`,
    borderRadius: radius.sm,
    padding: '7px 9px',
    background: colors.surface,
    color: colors.text,
    fontSize: 13,
};

const errorInputStyle: CSSProperties = {
    borderColor: colors.danger,
};

const errorTextStyle: CSSProperties = {
    fontSize: 11,
    color: colors.danger,
};

const rowStyle: CSSProperties = {
    display: 'grid',
    gridTemplateColumns: '1fr 1fr',
    gap: 12,
    alignItems: 'end',
};

const checkboxLabelStyle: CSSProperties = {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 6,
    color: colors.textMuted,
    fontSize: 13,
    cursor: 'pointer',
};

const metadataGridStyle: CSSProperties = {
    display: 'grid',
    gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))',
    gap: 8,
};

const actionsStyle: CSSProperties = {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    flexWrap: 'wrap',
};

const buttonStyle: CSSProperties = {
    border: `1px solid ${colors.border}`,
    borderRadius: radius.sm,
    padding: '7px 10px',
    background: colors.surface,
    color: colors.text,
    cursor: 'pointer',
    fontSize: 13,
    fontWeight: 600,
};

const primaryButtonStyle: CSSProperties = {
    ...buttonStyle,
    border: `1px solid ${colors.primary}`,
    background: colors.primaryLight,
    color: colors.primaryDark,
    fontWeight: 700,
};

/* ── Progress section styles (Task 6.2) ── */

const progressSectionStyle: CSSProperties = {
    display: 'grid',
    gap: 6,
    padding: 10,
    border: `1px solid ${colors.border}`,
    borderRadius: radius.sm,
    background: colors.surfaceMuted,
};

const progressBarContainerStyle: CSSProperties = {
    width: '100%',
    height: 6,
    borderRadius: radius.pill,
    background: colors.border,
    overflow: 'hidden',
};

const progressBarFillStyle: CSSProperties = {
    height: '100%',
    borderRadius: radius.pill,
    background: colors.primary,
    transition: 'width 0.3s ease',
};

const progressPercentStyle: CSSProperties = {
    fontSize: 12,
    fontWeight: 700,
    color: colors.text,
    textAlign: 'right',
};

const progressStatusStyle: CSSProperties = {
    fontSize: 11,
    color: colors.textSecondary,
};

const currentURLStyle: CSSProperties = {
    fontSize: 11,
    color: colors.textMuted,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
};

const cancelButtonStyle: CSSProperties = {
    ...buttonStyle,
    border: `1px solid ${colors.danger}`,
    color: colors.danger,
    background: colors.dangerBg,
    padding: '5px 10px',
    fontSize: 12,
};

/* ── Preview results styles (Task 6.3) ── */

const previewSectionStyle: CSSProperties = {
    display: 'grid',
    gap: 8,
    padding: 10,
    border: `1px solid ${colors.border}`,
    borderRadius: radius.sm,
    background: colors.surfaceMuted,
};

const previewHeaderStyle: CSSProperties = {
    fontSize: 12,
    fontWeight: 700,
    color: colors.text,
};

const depthListStyle: CSSProperties = {
    display: 'grid',
    gap: 4,
    maxHeight: 240,
    overflowY: 'auto',
};

const depthGroupStyle: CSSProperties = {
    border: `1px solid ${colors.borderLight}`,
    borderRadius: radius.sm,
    background: colors.surface,
};

const depthHeaderStyle: CSSProperties = {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    padding: '6px 8px',
    fontSize: 12,
    fontWeight: 600,
    color: colors.textSecondary,
    cursor: 'pointer',
    userSelect: 'none',
};

const depthToggleStyle: CSSProperties = {
    fontSize: 10,
    color: colors.textMuted,
};

const urlListStyle: CSSProperties = {
    padding: '0 8px 6px 22px',
    display: 'grid',
    gap: 2,
};

const urlItemStyle: CSSProperties = {
    fontSize: 11,
    color: colors.textMuted,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
};

const previewActionsStyle: CSSProperties = {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
};
