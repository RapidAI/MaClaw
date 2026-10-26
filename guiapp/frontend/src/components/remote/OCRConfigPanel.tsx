import { useCallback, useEffect, useState } from 'react';
import { CheckFormulaModel, CheckOCRModel, DownloadFormulaModel, DownloadOCRModel, GetFormulaEnabled, GetOCREnabled, SetFormulaEnabled, SetOCREnabled, SetOCRModelTier } from '../../../wailsjs/go/main/App';
import { EventsOn } from '../../../wailsjs/runtime';
import { ModelStatusBox } from './ModelStatusBox';

type Props = { lang: string };

// OCRConfigPanel manages the local PP-OCRv6 models and the FormulaNet model
// used by ocr_recognize (mode=formula). Download state comes from the
// 'ocr-download-progress' and 'formula-download-progress' events.
export function OCRConfigPanel({ lang }: Props) {
    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en, [lang]);
    const [enabled, setEnabled] = useState(true);
    const [tier, setTier] = useState('small');
    const [exists, setExists] = useState(false);
    const [size, setSize] = useState(0);
    const [downloading, setDownloading] = useState(false);
    const [progress, setProgress] = useState(0);
    const [downloaded, setDownloaded] = useState(0);
    const [total, setTotal] = useState(0);
    const [error, setError] = useState('');
    const [loading, setLoading] = useState(true);
    const [formulaEnabled, setFormulaEnabled] = useState(true);
    const [formulaExists, setFormulaExists] = useState(false);
    const [formulaSize, setFormulaSize] = useState(0);
    const [formulaDownloading, setFormulaDownloading] = useState(false);
    const [formulaProgress, setFormulaProgress] = useState(0);
    const [formulaDownloaded, setFormulaDownloaded] = useState(0);
    const [formulaTotal, setFormulaTotal] = useState(0);
    const [formulaError, setFormulaError] = useState('');

    const tierOptions = [
        { id: 'tiny', en: 'Tiny (~6MB, fastest)', zh: 'Tiny（~6MB，最快）', zhHant: 'Tiny（~6MB，最快）' },
        { id: 'small', en: 'Small (default, balanced, ~31MB)', zh: 'Small（默认，均衡 ~31MB）', zhHant: 'Small（預設，均衡 ~31MB）' },
        { id: 'medium', en: 'Medium (~139MB, most accurate)', zh: 'Medium（~139MB，最准）', zhHant: 'Medium（~139MB，最準）' },
    ];

    const refresh = useCallback(async () => {
        const info: any = await CheckOCRModel();
        setExists(!!info.exists);
        setSize(info.size || 0);
        if (info.tier) setTier(info.tier);
    }, []);
    const refreshFormula = useCallback(async () => {
        const info: any = await CheckFormulaModel();
        setFormulaExists(!!info.exists);
        setFormulaSize(info.size || 0);
    }, []);

    useEffect(() => {
        Promise.all([
            refresh(),
            refreshFormula(),
            GetOCREnabled().then(setEnabled),
            GetFormulaEnabled().then(setFormulaEnabled),
        ]).catch(() => {}).finally(() => setLoading(false));
    }, [refresh, refreshFormula]);

    useEffect(() => {
        const handler = (data: any) => {
            if (data.error) { setError(data.error); setDownloading(false); return; }
            setError('');
            const pct = data.percent || 0;
            setProgress(pct); setDownloaded(data.downloaded || 0); setTotal(data.total || 0);
            if ((data.total || 0) > 0 && pct < 100) setDownloading(true);
            if (pct >= 100) {
                setDownloading(false);
                void refresh();
            }
        };
        const stopOCR = EventsOn('ocr-download-progress', handler);
        const formulaHandler = (data: any) => {
            if (data.error) { setFormulaError(data.error); setFormulaDownloading(false); return; }
            setFormulaError('');
            const pct = data.percent || 0;
            setFormulaProgress(pct); setFormulaDownloaded(data.downloaded || 0); setFormulaTotal(data.total || 0);
            if ((data.total || 0) > 0 && pct < 100) setFormulaDownloading(true);
            if (pct >= 100) {
                setFormulaDownloading(false);
                void refreshFormula();
            }
        };
        const stopFormula = EventsOn('formula-download-progress', formulaHandler);
        return () => {
            stopOCR?.();
            stopFormula?.();
        };
    }, [refresh, refreshFormula]);

    const toggle = async (next: boolean) => {
        setEnabled(next); setError('');
        if (next && !exists) setDownloading(true);
        if (!next) setDownloading(false);
        try { await SetOCREnabled(next); } catch (e: any) {
            setEnabled(!next); setDownloading(false); setError(e?.message || String(e));
        }
    };
    const handleTierChange = async (nextTier: string) => {
        const previousTier = tier;
        setTier(nextTier); setError('');
        try {
            await SetOCRModelTier(nextTier);
            // The Go side kicks a background download when the new tier's
            // models are missing and OCR is enabled; reflect that state.
            const info: any = await CheckOCRModel();
            setExists(!!info.exists);
            setSize(info.size || 0);
            if (enabled && !info.exists) setDownloading(true);
        } catch (e: any) {
            setTier(previousTier); setError(e?.message || String(e));
        }
    };
    const download = async () => {
        setDownloading(true); setProgress(0); setDownloaded(0); setTotal(0); setError('');
        try { await DownloadOCRModel(); await refresh(); } catch (e: any) {
            setError(prev => prev || (e?.message || String(e))); setDownloading(false);
        }
    };
    const toggleFormula = async (next: boolean) => {
        setFormulaEnabled(next); setFormulaError('');
        if (next && !formulaExists) setFormulaDownloading(true);
        if (!next) setFormulaDownloading(false);
        try {
            await SetFormulaEnabled(next);
            if (next) {
                const info: any = await CheckFormulaModel();
                const present = !!info?.exists;
                setFormulaExists(present);
                setFormulaSize(info?.size || 0);
                // The files may have finished downloading while the switch was
                // off. That run does not start again, so drop the spinner.
                if (present) setFormulaDownloading(false);
            }
        } catch (e: any) {
            setFormulaEnabled(!next); setFormulaDownloading(false); setFormulaError(e?.message || String(e));
        }
    };
    const downloadFormula = async () => {
        setFormulaDownloading(true); setFormulaProgress(0); setFormulaDownloaded(0); setFormulaTotal(0); setFormulaError('');
        try { await DownloadFormulaModel(); await refreshFormula(); } catch (e: any) {
            setFormulaError(prev => prev || (e?.message || String(e))); setFormulaDownloading(false);
        }
    };

    if (loading) return <div className="model-config-loading">{t('Loading...', '加载中...', '加載中...')}</div>;
    return <>
        <section className="model-config-section">
            <div className="model-config-section__head">
                <div className="model-config-section__text">
                    <h4 className="model-config-section__title">{t('OCR Model', 'OCR 文字识别模型', 'OCR 文字識別模型')}</h4>
                    <p className="model-config-section__sub">{t(
                        'PP-OCRv6 reads text from screenshots for computer use and GUI automation, fully on-device. When the main model supports image input, its own multimodal vision is used first and OCR serves as the fallback; models without vision rely on this engine. It is enabled by default and downloads from HuggingFace or your Hub mirror. Switching tiers downloads the new tier in the background when its models are missing.',
                        'PP-OCRv6 在本地识别截图中的文字，用于电脑操作与界面自动化。当主模型支持图片输入时，优先使用模型自身的多模态视觉识别，OCR 作为兜底；不支持图片的模型则由本引擎识别。默认启用，模型从 HuggingFace 或 Hub 镜像下载。切换档位时，若新档位模型缺失会在后台自动下载。',
                        'PP-OCRv6 在本機識別截圖中的文字，用於電腦操作與介面自動化。當主模型支援圖片輸入時，優先使用模型自身的多模態視覺識別，OCR 作為兜底；不支援圖片的模型則由本引擎識別。預設啟用，模型從 HuggingFace 或 Hub 鏡像下載。切換檔位時，若新檔位模型缺失會在背景自動下載。'
                    )}</p>
                </div>
                <label className="model-config-switch" title={t('Enable OCR', '启用文字识别', '啟用文字識別')}>
                    <input
                        type="checkbox"
                        className="model-config-switch__input"
                        role="switch"
                        checked={enabled}
                        onChange={e => void toggle(e.target.checked)}
                        aria-label={t('Enable OCR', '启用文字识别', '啟用文字識別')}
                    />
                </label>
            </div>
            <div className="model-config-inline-field">
                <label htmlFor="ocr-tier-select" className="model-config-inline-label">
                    {t('Model tier', '模型档位', '模型檔位')}
                </label>
                <select
                    id="ocr-tier-select"
                    value={tier}
                    onChange={e => void handleTierChange(e.target.value)}
                    disabled={downloading}
                    className="model-config-select"
                >
                    {tierOptions.map(option => (
                        <option key={option.id} value={option.id}>
                            {lang === 'en' ? option.en : lang === 'zh-Hant' ? option.zhHant : option.zh}
                        </option>
                    ))}
                </select>
            </div>
            {enabled && <ModelStatusBox exists={exists} downloading={downloading} size={size} progress={progress} downloaded={downloaded} total={total} error={error} onDownload={() => void download()} onRetry={() => void download()} accentColor="var(--theme-success)" t={t} />}
        </section>
        <section className="model-config-section">
            <div className="model-config-section__head">
                <div className="model-config-section__text">
                    <h4 className="model-config-section__title">{t('Formula recognition', '公式识别', '公式識別')}</h4>
                    <p className="model-config-section__sub">{t(
                        'FormulaNet reads one printed formula image into LaTeX, fully on-device. Call it from the OCR tool with mode=formula (one PNG, JPEG, or GIF). The model downloads from the GitHub release, then from your Hub mirror. It is enabled by default.',
                        'FormulaNet 在本地把一张印刷公式图片识别成 LaTeX。在 OCR 工具 ocr_recognize 里传 mode=formula（一张 PNG、JPEG 或 GIF）。模型先从 GitHub Release 下载，失败后再走 Hub。默认启用。',
                        'FormulaNet 在本機把一張印刷公式圖片識別成 LaTeX。在 OCR 工具 ocr_recognize 裡傳 mode=formula（一張 PNG、JPEG 或 GIF）。模型先從 GitHub Release 下載，失敗後再走 Hub。預設啟用。'
                    )}</p>
                </div>
                <label className="model-config-switch" title={t('Enable formula recognition', '启用公式识别', '啟用公式識別')}>
                    <input
                        type="checkbox"
                        className="model-config-switch__input"
                        role="switch"
                        checked={formulaEnabled}
                        onChange={e => void toggleFormula(e.target.checked)}
                        aria-label={t('Enable formula recognition', '启用公式识别', '啟用公式識別')}
                    />
                </label>
            </div>
            {formulaEnabled && <ModelStatusBox exists={formulaExists} downloading={formulaDownloading} size={formulaSize} progress={formulaProgress} downloaded={formulaDownloaded} total={formulaTotal} error={formulaError} onDownload={() => void downloadFormula()} onRetry={() => void downloadFormula()} onRedownload={() => void downloadFormula()} accentColor="var(--theme-success)" t={t} />}
        </section>
    </>;
}
