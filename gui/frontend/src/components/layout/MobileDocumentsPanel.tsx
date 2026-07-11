import { useCallback, useEffect, useState } from 'react';

export type MobileDocumentDraftSummary = {
  id: string;
  title: string;
  template?: string;
  updated_at?: string;
  rune_count?: number;
  preview?: string;
  markdown?: string;
};

type MobileDocumentsPanelProps = {
  lang: string;
  open: boolean;
  onClose: () => void;
};

function callListDrafts(limit: number, includeBody: boolean): Promise<MobileDocumentDraftSummary[]> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.ListMobileDocumentDrafts) {
    return Promise.reject(new Error(langMissingBinding()));
  }
  return app.ListMobileDocumentDrafts(limit, includeBody);
}

function callGetDraft(id: string): Promise<MobileDocumentDraftSummary> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.GetMobileDocumentDraft) {
    return Promise.reject(new Error(langMissingBinding()));
  }
  return app.GetMobileDocumentDraft(id);
}

function langMissingBinding() {
  return 'Desktop binding missing ListMobileDocumentDrafts — rebuild GUI after pull.';
}

/**
 * Lightweight shared-library browser for Mobile/Hub emergency drafts.
 * Same Hub data as the phone app (viewer token).
 */
export function MobileDocumentsPanel({ lang, open, onClose }: MobileDocumentsPanelProps) {
  const isZh = lang !== 'en';
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [drafts, setDrafts] = useState<MobileDocumentDraftSummary[]>([]);
  const [selected, setSelected] = useState<MobileDocumentDraftSummary | null>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const list = await callListDrafts(50, false);
      setDrafts(Array.isArray(list) ? list : []);
    } catch (e: any) {
      setError(String(e?.message || e || 'load failed'));
      setDrafts([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (open) {
      void refresh();
      setSelected(null);
    }
  }, [open, refresh]);

  if (!open) return null;

  return (
    <div
      role="dialog"
      aria-label={isZh ? 'Mobile 文稿' : 'Mobile documents'}
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 12000,
        background: 'rgba(0,0,0,0.45)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        '--wails-draggable': 'no-drag',
      } as any}
      onClick={onClose}
    >
      <div
        style={{
          width: 'min(720px, 92vw)',
          maxHeight: '80vh',
          background: 'var(--theme-bg-elevated, var(--theme-bg, #1e2430))',
          color: 'var(--theme-text-primary, #e8eef8)',
          borderRadius: 12,
          border: '1px solid var(--theme-border, rgba(255,255,255,0.08))',
          boxShadow: '0 16px 48px rgba(0,0,0,0.35)',
          display: 'flex',
          flexDirection: 'column',
          overflow: 'hidden',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <div style={{ display: 'flex', alignItems: 'center', padding: '14px 16px', gap: 12, borderBottom: '1px solid var(--theme-border, rgba(255,255,255,0.08))' }}>
          <div style={{ flex: 1, fontWeight: 700, fontSize: '1rem' }}>
            {isZh ? 'Mobile 文稿（Hub 共享）' : 'Mobile documents (Hub shared)'}
          </div>
          <button className="btn-link" type="button" onClick={() => void refresh()} style={{ fontSize: '0.85rem' }}>
            {isZh ? '刷新' : 'Refresh'}
          </button>
          <button className="btn-link" type="button" onClick={onClose} style={{ fontSize: '0.85rem' }}>
            {isZh ? '关闭' : 'Close'}
          </button>
        </div>
        <div style={{ padding: '10px 16px', fontSize: '0.8rem', opacity: 0.8 }}>
          {isZh
            ? '与手机端共用同一 Hub 草稿库；可在此查看标题、预览与正文。'
            : 'Same Hub draft library as the phone app. Browse titles, previews, and body text.'}
        </div>
        {error ? (
          <div style={{ margin: '0 16px 12px', padding: 10, borderRadius: 8, background: 'rgba(220,80,80,0.15)', color: '#ffb4b4', fontSize: '0.85rem' }}>
            {error}
          </div>
        ) : null}
        <div style={{ display: 'flex', minHeight: 280, flex: 1, overflow: 'hidden' }}>
          <div style={{ width: '42%', borderRight: '1px solid var(--theme-border, rgba(255,255,255,0.08))', overflow: 'auto' }}>
            {loading ? (
              <div style={{ padding: 16, opacity: 0.7 }}>{isZh ? '加载中…' : 'Loading…'}</div>
            ) : drafts.length === 0 ? (
              <div style={{ padding: 16, opacity: 0.7 }}>{isZh ? '暂无文稿' : 'No drafts yet'}</div>
            ) : (
              drafts.map((d) => (
                <button
                  key={d.id}
                  type="button"
                  onClick={async () => {
                    setSelected(d);
                    try {
                      const full = await callGetDraft(d.id);
                      setSelected({ ...d, ...full });
                    } catch {
                      // keep list row preview
                    }
                  }}
                  style={{
                    display: 'block',
                    width: '100%',
                    textAlign: 'left',
                    padding: '10px 14px',
                    border: 'none',
                    borderBottom: '1px solid var(--theme-border, rgba(255,255,255,0.06))',
                    background: selected?.id === d.id ? 'var(--theme-primary-container, rgba(80,120,255,0.15))' : 'transparent',
                    color: 'inherit',
                    cursor: 'pointer',
                  }}
                >
                  <div style={{ fontWeight: 600, fontSize: '0.9rem' }}>{d.title || d.id}</div>
                  <div style={{ fontSize: '0.75rem', opacity: 0.65, marginTop: 4 }}>
                    {(d.rune_count ?? 0) > 0 ? `${d.rune_count} chars` : ''}
                    {d.updated_at ? ` · ${d.updated_at}` : ''}
                  </div>
                  {d.preview ? (
                    <div style={{ fontSize: '0.78rem', opacity: 0.75, marginTop: 4, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                      {d.preview}
                    </div>
                  ) : null}
                </button>
              ))
            )}
          </div>
          <div style={{ flex: 1, overflow: 'auto', padding: 16, whiteSpace: 'pre-wrap', fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace', fontSize: '0.82rem', lineHeight: 1.45 }}>
            {selected ? (
              <>
                <div style={{ fontWeight: 700, marginBottom: 8, fontFamily: 'inherit' }}>{selected.title || selected.id}</div>
                {selected.markdown || selected.preview || (isZh ? '（无正文）' : '(empty)')}
              </>
            ) : (
              <span style={{ opacity: 0.6 }}>{isZh ? '选择左侧文稿查看' : 'Select a draft on the left'}</span>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
