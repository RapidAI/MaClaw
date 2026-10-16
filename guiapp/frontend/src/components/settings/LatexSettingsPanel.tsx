import { useCallback, useEffect, useState } from 'react';
import { DownloadLatexTinyTeX, GetLatexTinyTeXStatus, SetLatexTinyTeXEnabled } from '../../../wailsjs/go/main/App';
import { EventsOff, EventsOn } from '../../../wailsjs/runtime';
import { localizeText } from '../../i18n';
import { formatBytes } from '../remote/ModelStatusBox';

type LatexStatus = {
    enabled?: boolean;
    ready?: boolean;
    phase?: string;
    percent?: number;
    downloaded?: number;
    total?: number;
    error?: string;
    version?: string;
    install_dir?: string;
    scheme?: string;
    bundle?: string;
    source?: string;
};

type ProgressEvent = {
    phase?: string;
    percent?: number;
    downloaded?: number;
    total?: number;
    error?: string;
};

const ACTIVE = new Set(['downloading', 'extracting', 'installing', 'verifying']);

export function LatexSettingsPanel({ lang }: { lang: string }) {
    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        localizeText(lang, en, zhHans, zhHant), [lang]);
    const [status, setStatus] = useState<LatexStatus | null>(null);
    const [loading, setLoading] = useState(true);

    const refresh = useCallback(async () => {
        const next = (await GetLatexTinyTeXStatus()) as LatexStatus;
        setStatus((prev) => {
            if (next?.phase === 'downloading' && !(next.percent) && (prev?.percent || 0) > 0) {
                return { ...next, percent: prev?.percent, downloaded: prev?.downloaded, total: prev?.total };
            }
            return next;
        });
    }, []);

    useEffect(() => {
        let cancelled = false;
        let timer = 0;
        const tick = async () => {
            try {
                await refresh();
            } catch {
                if (!cancelled) setStatus((prev) => prev ?? { enabled: true, phase: 'error', error: 'status unavailable' });
            } finally {
                if (!cancelled) setLoading(false);
            }
        };
        void tick();
        timer = window.setInterval(() => { void tick(); }, 2000);
        return () => {
            cancelled = true;
            window.clearInterval(timer);
        };
    }, [refresh]);

    useEffect(() => {
        const handler = (data: ProgressEvent) => {
            setStatus((prev) => ({
                ...(prev || { enabled: true, scheme: 'scheme-small', bundle: 'TinyTeX-0' }),
                phase: data?.phase || (data?.error ? 'error' : 'downloading'),
                percent: data?.percent || 0,
                downloaded: data?.downloaded || 0,
                total: data?.total || 0,
                error: data?.error || '',
                ready: data?.phase === 'ready' ? true : prev?.ready,
            }));
            if (data?.phase === 'ready' || data?.error) void refresh();
        };
        EventsOn('latex-tinytex-progress', handler);
        return () => { EventsOff('latex-tinytex-progress'); };
    }, [refresh]);

    const phase = status?.phase || 'idle';
    const busy = ACTIVE.has(phase);
    const phaseLabel = (value: string) => {
        switch (value) {
            case 'downloading':
                return t('Downloading TinyTeX-0…', '正在下载 TinyTeX-0…', '正在下載 TinyTeX-0…');
            case 'extracting':
                return t('Extracting…', '正在解压…', '正在解壓…');
            case 'installing':
                return t('Installing scheme-small…', '正在安装 scheme-small…', '正在安裝 scheme-small…');
            case 'verifying':
                return t('Checking xelatex…', '正在校验 xelatex…', '正在校驗 xelatex…');
            default:
                return '';
        }
    };

    const toggle = async (next: boolean) => {
        setStatus((prev) => ({ ...(prev || {}), enabled: next, error: '' }));
        try {
            await SetLatexTinyTeXEnabled(next);
            await refresh();
        } catch (err: any) {
            setStatus((prev) => ({ ...(prev || {}), enabled: !next, error: err?.message || String(err) }));
        }
    };

    const retry = async () => {
        setStatus((prev) => ({ ...(prev || {}), phase: 'downloading', error: '', percent: 0 }));
        try {
            await DownloadLatexTinyTeX();
        } catch (err: any) {
            setStatus((prev) => ({ ...(prev || {}), phase: 'error', error: err?.message || String(err) }));
        }
    };

    if (loading && !status) {
        return <div className="model-config-loading">{t('Loading...', '加载中...', '加載中...')}</div>;
    }

    const enabled = status?.enabled !== false;
    return (
        <div className="settings-content settings-content--stacked" data-testid="latex-settings">
            <section className="model-config-panel model-config-panel--spaced">
                <h4 className="model-config-heading">{t('LaTeX', 'LaTeX', 'LaTeX')}</h4>
                <div className="model-config-toggle-row">
                    <label className="model-config-check">
                        <input
                            type="checkbox"
                            checked={enabled}
                            onChange={(e) => void toggle(e.target.checked)}
                        />
                        {t('Enable TinyTeX (scheme-small)', '启用 TinyTeX（scheme-small）', '啟用 TinyTeX（scheme-small）')}
                    </label>
                </div>
                <p className="model-config-copy">{t(
                    'On by default. After install, MaClaw downloads the official TinyTeX-0 release (GitHub rstudio/tinytex-releases) and uses tlmgr to install TeX Live scheme-small, then checks xelatex --version. Chinese locales try a GitHub proxy first, then the Tsinghua CTAN mirror for packages. The files stay in the MaClaw data directory and are not a full TeX Live install.',
                    '默认开启。安装完成后会在后台下载 TinyTeX 官方发行包 TinyTeX-0（GitHub rstudio/tinytex-releases），再用 tlmgr 安装 TeX Live scheme-small，并执行 xelatex --version 校验。中文环境优先走 GitHub 加速，宏包优先用清华 CTAN 镜像。文件放在 MaClaw 数据目录，不是完整 TeX Live。',
                    '預設開啟。安裝完成後會在背景下載 TinyTeX 官方發行包 TinyTeX-0（GitHub rstudio/tinytex-releases），再用 tlmgr 安裝 TeX Live scheme-small，並執行 xelatex --version 校驗。中文環境優先走 GitHub 加速，巨集包優先用清華 CTAN 鏡像。檔案放在 MaClaw 資料目錄，不是完整 TeX Live。',
                )}</p>
                {enabled && status?.ready && !busy && (
                    <div className="model-status-box__ready" data-testid="latex-ready">
                        <span className="model-status-box__label">{t('scheme-small ready', 'scheme-small 已就绪', 'scheme-small 已就緒')}</span>
                        {status.version ? <span className="model-status-box__size">{status.version}</span> : null}
                    </div>
                )}
                {enabled && busy && (
                    <div data-testid="latex-busy">
                        <div className="model-status-box__progress-head">
                            <span className="model-status-box__label">{phaseLabel(phase)}</span>
                            {phase === 'downloading' && (
                                <span className="model-status-box__meta">
                                    {status?.percent || 0}% — {formatBytes(status?.downloaded || 0)} / {(status?.total || 0) > 0 ? formatBytes(status?.total || 0) : '?'}
                                </span>
                            )}
                        </div>
                        {phase === 'downloading' && (
                            <div className="model-status-box__progress-track">
                                <div className="model-status-box__progress-fill" style={{ width: `${status?.percent || 0}%` }} />
                            </div>
                        )}
                    </div>
                )}
                {enabled && !status?.ready && !busy && !status?.error && (
                    <div>
                        <div className="model-status-box__missing">
                            {t('TinyTeX scheme-small is not installed yet.', '尚未安装 TinyTeX scheme-small。', '尚未安裝 TinyTeX scheme-small。')}
                        </div>
                        <button type="button" className="model-status-box__primary" onClick={() => void retry()}>
                            {t('Download and verify', '下载并校验', '下載並校驗')}
                        </button>
                    </div>
                )}
                {status?.error && (
                    <div className="model-status-box__error" data-testid="latex-error">
                        <span>{t('Error: ', '错误：', '錯誤：')}{status.error}</span>
                        <button type="button" className="model-status-box__retry" onClick={() => void retry()}>
                            {t('Retry', '重试', '重試')}
                        </button>
                    </div>
                )}
                <p className="settings-help-text">
                    {t('Source', '下载源', '下載源')}: {status?.source || 'https://github.com/rstudio/tinytex-releases/releases/download/daily/TinyTeX-0-windows.exe'}
                </p>
                {status?.install_dir && (
                    <p className="settings-help-text">{t('Install directory', '安装目录', '安裝目錄')}: {status.install_dir}</p>
                )}
            </section>
        </div>
    );
}
