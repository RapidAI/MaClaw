import { OpenFileOrShowInFolder } from '../../../wailsjs/go/main/App';
import { ProjectSearchIcon } from '../ai/ProjectSearchIcon';
import type { ProjectSceneDetail } from '../ai/ProjectSceneDetailPanel';

const textForLang = (lang: string, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' || lang === 'zh' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

export function SidebarTaskEvidencePanel({ detail, loading, lang }: {
    detail: ProjectSceneDetail | null;
    loading: boolean;
    lang: string;
}) {
    const artifacts = detail?.recent_artifacts || [];
    return <div style={{ margin: '3px 0 5px 22px', padding: '6px 7px', border: '1px solid var(--theme-border)', borderRadius: '6px', background: 'color-mix(in srgb, var(--theme-surface) 88%, var(--theme-text-primary) 4%)', minWidth: 0 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '6px', marginBottom: artifacts.length > 0 || loading ? '4px' : 0 }}>
            <span style={{ fontSize: '0.64rem', fontWeight: 700, color: 'var(--theme-text-secondary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {loading ? textForLang(lang, 'Loading evidence...', '正在加载证据...', '正在載入證據...') : textForLang(lang, 'Recent artifact sources', '最近产物来源', '最近產物來源')}
            </span>
            {detail?.entry_count !== undefined && <span style={{ fontSize: '0.6rem', color: 'var(--theme-text-muted)', opacity: 0.75, flexShrink: 0 }}>{detail.entry_count}</span>}
        </div>
        {!loading && artifacts.length === 0 && <div style={{ fontSize: '0.64rem', color: 'var(--theme-text-muted)', opacity: 0.75 }}>{textForLang(lang, 'No source-backed artifacts yet', '暂无可回查产物', '暫無可回查產物')}</div>}
        {artifacts.slice(0, 3).map((artifact, index) => {
            const label = artifact.title || artifact.preview || artifact.source_url || textForLang(lang, 'Artifact', '产物', '產物');
            const source = artifact.source_url ? artifact.source_url + (artifact.source_hint ? '; ' + artifact.source_hint : '') : '';
            return <div key={artifact.source_url || label + index} style={{ display: 'flex', alignItems: 'center', gap: '5px', minWidth: 0, marginTop: index === 0 ? 0 : '3px' }}>
                <span title={source || label} style={{ flex: 1, minWidth: 0, fontSize: '0.64rem', color: 'var(--theme-text-primary)', opacity: 0.82, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{label}</span>
                {artifact.source_url && <button type="button" aria-label={textForLang(lang, 'Open artifact source', '打开产物来源', '打開產物來源')} title={source} onClick={event => { event.stopPropagation(); void OpenFileOrShowInFolder(artifact.source_url || ''); }} style={{ border: 'none', background: 'transparent', color: 'var(--theme-primary)', cursor: 'pointer', width: '18px', height: '18px', padding: 0, display: 'inline-flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 }}><ProjectSearchIcon name="externalLink" size={12} /></button>}
            </div>;
        })}
    </div>;
}
