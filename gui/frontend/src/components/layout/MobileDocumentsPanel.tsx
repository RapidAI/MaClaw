import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type DragEvent } from 'react';
import { useDialog } from '../CustomDialog';
import { StatusGlyph } from '../ai/WorkbenchIcons';

export type MobileDocumentDraftImage = {
  id: string;
  filename?: string;
  content_type?: string;
  size?: number;
  url?: string;
};

export type MobileDocumentDraftSummary = {
  id: string;
  title: string;
  template?: string;
  updated_at?: string;
  rune_count?: number;
  preview?: string;
  markdown?: string;
  has_original?: boolean;
  source_filename?: string;
  source_content_type?: string;
  source_size?: number;
  source_download_url?: string;
  images?: MobileDocumentDraftImage[];
};

type MobileDocumentsPanelProps = {
  lang: string;
  open: boolean;
  onClose: () => void;
};

type UploadJob = {
  name: string;
  status: 'reading' | 'uploading' | 'done' | 'error';
  message?: string;
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

function callCreateDraft(
  title: string,
  content: string,
  markdown: string,
  template: string,
): Promise<MobileDocumentDraftSummary> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.CreateMobileDocumentDraft) {
    return Promise.reject(
      new Error(
        'Desktop binding missing CreateMobileDocumentDraft — rebuild GUI after pull.',
      ),
    );
  }
  return app.CreateMobileDocumentDraft(title, content, markdown, template);
}

function callDeleteDraft(id: string): Promise<void> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.DeleteMobileDocumentDraft) {
    return Promise.reject(
      new Error(
        'Desktop binding missing DeleteMobileDocumentDraft — rebuild GUI after pull.',
      ),
    );
  }
  return app.DeleteMobileDocumentDraft(id);
}

function callImportFromPath(path: string): Promise<MobileDocumentDraftSummary> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.ImportMobileDocumentFromPath) {
    return Promise.reject(
      new Error(
        'Desktop binding missing ImportMobileDocumentFromPath — rebuild GUI after pull.',
      ),
    );
  }
  return app.ImportMobileDocumentFromPath(path);
}

function callImportBytes(filename: string, base64: string): Promise<MobileDocumentDraftSummary> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.ImportMobileDocumentBytes) {
    return Promise.reject(
      new Error(
        'Desktop binding missing ImportMobileDocumentBytes — rebuild GUI after pull.',
      ),
    );
  }
  return app.ImportMobileDocumentBytes(filename, base64);
}

function callOpenOriginal(id: string): Promise<string> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.OpenMobileDocumentOriginal) {
    return Promise.reject(
      new Error(
        'Desktop binding missing OpenMobileDocumentOriginal — rebuild GUI after pull.',
      ),
    );
  }
  return app.OpenMobileDocumentOriginal(id);
}

function callSaveOriginal(id: string): Promise<string> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.SaveMobileDocumentOriginal) {
    return Promise.reject(
      new Error(
        'Desktop binding missing SaveMobileDocumentOriginal — rebuild GUI after pull.',
      ),
    );
  }
  return app.SaveMobileDocumentOriginal(id);
}

type MobileDocImagePayload = {
  content_type?: string;
  data_base64?: string;
  filename?: string;
  size?: number;
};

/** Session cache so opening the same draft does not re-download every illustration. */
const mobileDocImageCache = new Map<string, Promise<MobileDocImagePayload>>();
const MOBILE_DOC_IMAGE_CACHE_MAX = 24;

function callGetDraftImage(draftId: string, imageId: string): Promise<MobileDocImagePayload> {
  const key = `${draftId}\0${imageId}`;
  const hit = mobileDocImageCache.get(key);
  if (hit) return hit;
  const app = (window as any)?.go?.main?.App;
  if (!app?.GetMobileDocumentDraftImage) {
    return Promise.reject(
      new Error(
        'Desktop binding missing GetMobileDocumentDraftImage — rebuild GUI after pull.',
      ),
    );
  }
  const p = app
    .GetMobileDocumentDraftImage(draftId, imageId)
    .then((payload: MobileDocImagePayload) => payload)
    .catch((err: unknown) => {
      mobileDocImageCache.delete(key);
      throw err;
    });
  // Simple FIFO bound: drop oldest entries when over cap (Map preserves insertion order).
  if (mobileDocImageCache.size >= MOBILE_DOC_IMAGE_CACHE_MAX) {
    const oldest = mobileDocImageCache.keys().next().value;
    if (oldest !== undefined) mobileDocImageCache.delete(oldest);
  }
  mobileDocImageCache.set(key, p);
  return p;
}

/** Match Hub markdown image URLs: /api/mobile/documents/drafts/{id}/images/{imgId} */
const MOBILE_DOC_IMAGE_RE =
  /!\[([^\]]*)\]\((\/api\/mobile\/documents\/drafts\/([^/]+)\/images\/([^)\s]+))\)/g;

type PreviewSegment =
  | { kind: 'text'; text: string }
  | { kind: 'image'; alt: string; draftId: string; imageId: string; url: string };

function splitMarkdownForPreview(markdown: string): PreviewSegment[] {
  const src = markdown || '';
  const segments: PreviewSegment[] = [];
  let last = 0;
  const re = new RegExp(MOBILE_DOC_IMAGE_RE.source, 'g');
  let m: RegExpExecArray | null;
  while ((m = re.exec(src)) !== null) {
    if (m.index > last) {
      segments.push({ kind: 'text', text: src.slice(last, m.index) });
    }
    segments.push({
      kind: 'image',
      alt: m[1] || m[4] || 'image',
      draftId: m[3],
      imageId: m[4],
      url: m[2],
    });
    last = m.index + m[0].length;
  }
  if (last < src.length) {
    segments.push({ kind: 'text', text: src.slice(last) });
  }
  if (segments.length === 0) {
    segments.push({ kind: 'text', text: src });
  }
  return segments;
}

function MobileDocImage({
  draftId,
  imageId,
  alt,
}: {
  draftId: string;
  imageId: string;
  alt: string;
}) {
  const [src, setSrc] = useState<string>('');
  const [err, setErr] = useState('');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setErr('');
    setSrc('');
    void callGetDraftImage(draftId, imageId)
      .then((payload) => {
        if (cancelled) return;
        const b64 = String(payload?.data_base64 || '').trim();
        const ct = String(payload?.content_type || 'image/png').split(';')[0].trim() || 'image/png';
        if (!b64) {
          setErr('empty image');
          return;
        }
        setSrc(`data:${ct};base64,${b64}`);
      })
      .catch((e: any) => {
        if (!cancelled) setErr(String(e?.message || e || 'load failed'));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [draftId, imageId]);

  if (loading) {
    return (
      <div style={{ opacity: 0.55, fontSize: '0.78rem', padding: '8px 0' }}>
        {alt ? `${alt}…` : '…'}
      </div>
    );
  }
  if (err || !src) {
    return (
      <div style={{ opacity: 0.65, fontSize: '0.78rem', padding: '6px 0', color: '#f0b4b4' }}>
        [{alt || imageId}]
      </div>
    );
  }
  return (
    <figure style={{ margin: '10px 0', maxWidth: '100%' }}>
      <img
        src={src}
        alt={alt}
        style={{
          maxWidth: '100%',
          maxHeight: 360,
          borderRadius: 8,
          border: '1px solid var(--theme-border, rgba(255,255,255,0.12))',
          objectFit: 'contain',
          background: 'color-mix(in srgb, var(--theme-field-bg, #000) 40%, transparent)',
          display: 'block',
        }}
      />
      {alt ? (
        <figcaption style={{ fontSize: '0.72rem', opacity: 0.55, marginTop: 4 }}>{alt}</figcaption>
      ) : null}
    </figure>
  );
}

function MobileDocPreviewBody({
  markdown,
  emptyLabel,
}: {
  markdown: string;
  emptyLabel: string;
}) {
  const segments = useMemo(() => splitMarkdownForPreview(markdown), [markdown]);
  if (!markdown.trim()) {
    return <>{emptyLabel}</>;
  }
  return (
    <>
      {segments.map((seg, i) => {
        if (seg.kind === 'text') {
          return (
            <span key={`t-${i}`} style={{ whiteSpace: 'pre-wrap' }}>
              {seg.text}
            </span>
          );
        }
        return (
          <MobileDocImage
            key={`i-${seg.draftId}-${seg.imageId}-${i}`}
            draftId={seg.draftId}
            imageId={seg.imageId}
            alt={seg.alt}
          />
        );
      })}
    </>
  );
}

async function fileToBase64(file: File): Promise<string> {
  const buf = await file.arrayBuffer();
  const bytes = new Uint8Array(buf);
  const chunk = 0x8000;
  let binary = '';
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk));
  }
  return btoa(binary);
}

function langMissingBinding() {
  return 'Desktop binding missing ListMobileDocumentDrafts — rebuild GUI after pull.';
}

/** Wails / OS drag may expose a local path on File. */
function localPathOf(file: File): string {
  const anyFile = file as File & { path?: string };
  return typeof anyFile.path === 'string' ? anyFile.path.trim() : '';
}

const TEXT_EXTS = new Set([
  'md',
  'markdown',
  'txt',
  'log',
  'json',
  'yaml',
  'yml',
  'toml',
  'csv',
  'tsv',
  'xml',
  'html',
  'htm',
  'css',
  'js',
  'ts',
  'tsx',
  'jsx',
  'go',
  'py',
  'java',
  'c',
  'h',
  'cpp',
  'rs',
  'sh',
  'bat',
  'ps1',
  'ini',
  'conf',
  'cfg',
  'env',
]);

/** Office / binary originals — always import via native path (Hub keeps original). */
const ORIGINAL_FILE_EXTS = new Set([
  'docx',
  'xlsx',
  'pdf',
  'doc',
  'xls',
  'pptx',
  'png',
  'jpg',
  'jpeg',
  'webp',
  'gif',
  'bmp',
]);

function extOf(name: string): string {
  const i = name.lastIndexOf('.');
  if (i < 0) return '';
  return name.slice(i + 1).toLowerCase();
}

function titleFromFilename(name: string): string {
  const base = name.replace(/\\/g, '/').split('/').pop() || name;
  const i = base.lastIndexOf('.');
  return (i > 0 ? base.slice(0, i) : base).trim() || base;
}

function isProbablyTextFile(file: File): boolean {
  const ext = extOf(file.name);
  if (ORIGINAL_FILE_EXTS.has(ext)) return false;
  if (TEXT_EXTS.has(ext)) return true;
  if (file.type.startsWith('text/')) return true;
  if (file.type === 'application/json' || file.type === 'application/xml') return true;
  // Small unknown types: try as text later
  return file.size > 0 && file.size < 512 * 1024 && !file.type.startsWith('image/') && !file.type.startsWith('video/') && !file.type.startsWith('audio/') && file.type !== 'application/pdf';
}

function isOriginalBinaryFile(file: File): boolean {
  const ext = extOf(file.name);
  if (ORIGINAL_FILE_EXTS.has(ext)) return true;
  if (file.type.startsWith('image/')) return true;
  if (file.type === 'application/pdf') return true;
  if (file.type.includes('officedocument') || file.type.includes('msword')) return true;
  return false;
}

function formatUpdatedAt(raw?: string, isZh?: boolean): string {
  if (!raw) return '';
  const d = Date.parse(raw);
  if (Number.isNaN(d)) return raw;
  try {
    return new Date(d).toLocaleString(isZh ? 'zh-CN' : 'en-US', {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  } catch {
    return raw;
  }
}

function readFileAsText(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result ?? ''));
    reader.onerror = () => reject(reader.error || new Error('read failed'));
    reader.readAsText(file);
  });
}

/**
 * Shared Hub document library with MaClaw Mobile.
 * Desktop can drop files here to publish drafts the phone can open immediately.
 */
export function MobileDocumentsPanel({ lang, open, onClose }: MobileDocumentsPanelProps) {
  const { showConfirm } = useDialog();
  const isZh = lang !== 'en' && !String(lang).startsWith('en');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [banner, setBanner] = useState('');
  const [drafts, setDrafts] = useState<MobileDocumentDraftSummary[]>([]);
  const [selected, setSelected] = useState<MobileDocumentDraftSummary | null>(null);
  const [filter, setFilter] = useState('');
  const [dragOver, setDragOver] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [jobs, setJobs] = useState<UploadJob[]>([]);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const dragDepth = useRef(0);

  const t = useCallback(
    (en: string, zh: string) => (isZh ? zh : en),
    [isZh],
  );

  const refresh = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const list = await callListDrafts(80, false);
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
      setBanner('');
      setJobs([]);
      setFilter('');
      dragDepth.current = 0;
      setDragOver(false);
    }
  }, [open, refresh]);

  // Close only via explicit Close / Esc — not when clicking the dimmed main window.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        onClose();
      }
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [open, onClose]);

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return drafts;
    return drafts.filter((d) => {
      const hay = `${d.title || ''} ${d.preview || ''} ${d.id || ''}`.toLowerCase();
      return hay.includes(q);
    });
  }, [drafts, filter]);

  const selectDraft = async (d: MobileDocumentDraftSummary) => {
    setSelected(d);
    try {
      const full = await callGetDraft(d.id);
      setSelected({ ...d, ...full });
    } catch {
      // keep list row
    }
  };

  const publishFiles = async (files: FileList | File[]) => {
    const list = Array.from(files || []);
    if (list.length === 0) return;
    setUploading(true);
    setError('');
    setBanner('');
    const nextJobs: UploadJob[] = list.map((f) => ({
      name: f.name,
      status: 'reading',
    }));
    setJobs(nextJobs);

    let ok = 0;
    let last: MobileDocumentDraftSummary | null = null;
    let firstError = '';
    for (let i = 0; i < list.length; i++) {
      const file = list[i];
      const patch = (job: Partial<UploadJob>) => {
        setJobs((prev) => prev.map((j, idx) => (idx === i ? { ...j, ...job } : j)));
      };
      try {
        const maxBytes = 25 * 1024 * 1024;
        if (file.size > maxBytes) {
          throw new Error(t('File too large (max 25MB)', '文件过大（最大 25MB）'));
        }
        if (file.size <= 0) {
          throw new Error(t('File is empty', '文件内容为空'));
        }
        patch({ status: 'reading' });
        // Always upload ORIGINAL bytes to Hub (path when available, else base64 blob).
        const localPath = localPathOf(file);
        let draft: MobileDocumentDraftSummary;
        patch({ status: 'uploading' });
        if (localPath) {
          try {
            draft = await callImportFromPath(localPath);
          } catch (pathErr: any) {
            // Fallback: some Wails builds expose a path that cannot be read; use blob.
            try {
              const b64 = await fileToBase64(file);
              draft = await callImportBytes(file.name, b64);
            } catch {
              throw pathErr;
            }
          }
        } else {
          const b64 = await fileToBase64(file);
          draft = await callImportBytes(file.name, b64);
        }
        if (!draft?.id) {
          throw new Error(t('Hub did not return a draft', 'Hub 未返回文稿'));
        }
        ok += 1;
        last = draft;
        patch({ status: 'done', message: draft?.id || 'ok' });
      } catch (e: any) {
        const msg = String(e?.message || e || 'failed');
        patch({
          status: 'error',
          message: msg,
        });
        if (!firstError) firstError = msg;
      }
    }

    setUploading(false);
    if (ok > 0) {
      setBanner(
        t(
          `${ok} file(s) added to Hub library. Phone app → Documents can open them.`,
          `已添加 ${ok} 个文件到文稿库。手机端「文档」可直接打开。`,
        ),
      );
      await refresh();
      if (last?.id) {
        await selectDraft(last);
      }
    } else if (list.length > 0) {
      setError(
        firstError || t('No files were imported', '没有成功导入任何文件'),
      );
    }
  };

  const deleteDraft = async (d: MobileDocumentDraftSummary) => {
    const title = d.title || d.id;
    const ok = await showConfirm(
      t(
        `Delete “${title}” from the shared Hub library? The phone app will no longer be able to open this document.`,
        `从共享文稿库删除「${title}」？手机端也将无法再看到该文稿。`,
      ),
      t('Delete shared document?', '删除共享文稿？'),
      {
        confirmText: t('Delete', '删除'),
        cancelText: t('Cancel', '取消'),
        confirmVariant: 'danger',
      },
    );
    if (!ok) return;
    setError('');
    setBanner('');
    try {
      await callDeleteDraft(d.id);
      if (selected?.id === d.id) setSelected(null);
      setBanner(t(`Deleted “${title}”`, `已删除「${title}」`));
      await refresh();
    } catch (e: any) {
      setError(String(e?.message || e || 'delete failed'));
    }
  };

  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    dragDepth.current = 0;
    setDragOver(false);
    if (e.dataTransfer?.files?.length) {
      void publishFiles(e.dataTransfer.files);
    }
  };

  const onDragEnter = (e: DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    dragDepth.current += 1;
    setDragOver(true);
  };

  const onDragLeave = (e: DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    dragDepth.current -= 1;
    if (dragDepth.current <= 0) {
      dragDepth.current = 0;
      setDragOver(false);
    }
  };

  const onDragOver = (e: DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'copy';
  };

  const shareSelectedAgain = async () => {
    if (!selected) return;
    setError('');
    // List items are already Hub drafts (same library as Mobile). Creating again
    // would duplicate; only confirm presence and guide the user to refresh on phone.
    const title = selected.title || selected.id || 'document';
    const draftId = (selected.id || '').trim();
    if (draftId) {
      setUploading(true);
      try {
        // Verify still exists on Hub (deleted elsewhere → clear selection).
        await callGetDraft(draftId);
        setBanner(
          t(
            `“${title}” is already in the shared library (id: ${draftId}). Open MaClaw Mobile → Documents and refresh — do not share again.`,
            `「${title}」已在共享文稿库中（${draftId}）。请在手机端「文档」刷新查看，无需再次分享，以免产生重复。`,
          ),
        );
      } catch (e: any) {
        setError(
          String(
            e?.message ||
              e ||
              t(
                'This draft is missing on Hub. Drop the file again to re-add it.',
                'Hub 上已找不到该文稿，请重新拖入文件添加。',
              ),
          ),
        );
        setSelected(null);
        await refresh();
      } finally {
        setUploading(false);
      }
      return;
    }
    setBanner(
      t(
        'Select a draft from the list, or drop a new file to add one.',
        '请先从列表选择文稿，或拖入新文件添加。',
      ),
    );
  };

  const openSelectedOriginal = async () => {
    if (!selected?.id || !selected.has_original) return;
    setError('');
    setBanner('');
    setUploading(true);
    try {
      const path = await callOpenOriginal(selected.id);
      setBanner(
        t(
          `Opened original${path ? `: ${path}` : ''}`,
          `已打开原件${path ? `：${path}` : ''}`,
        ),
      );
    } catch (e: any) {
      setError(String(e?.message || e || 'open original failed'));
    } finally {
      setUploading(false);
    }
  };

  const saveSelectedOriginal = async () => {
    if (!selected?.id || !selected.has_original) return;
    setError('');
    setBanner('');
    setUploading(true);
    try {
      const path = await callSaveOriginal(selected.id);
      if (!path) {
        setBanner(t('Save cancelled.', '已取消保存。'));
        return;
      }
      setBanner(t(`Saved original to ${path}`, `原件已保存到 ${path}`));
    } catch (e: any) {
      setError(String(e?.message || e || 'save original failed'));
    } finally {
      setUploading(false);
    }
  };

  const copyBody = async () => {
    const text = selected?.markdown || selected?.preview || '';
    if (!text) return;
    try {
      await navigator.clipboard.writeText(text);
      setBanner(t('Copied to clipboard', '已复制到剪贴板'));
    } catch {
      setError(t('Copy failed', '复制失败'));
    }
  };

  if (!open) return null;

  // Mirror CustomDialog: pin theme attrs so CSS variables match #App / dark schemes.
  const appEl = typeof document !== 'undefined' ? document.getElementById('App') : null;
  const appTheme = appEl?.getAttribute('data-ai-theme') || undefined;
  const appDarkScheme = appEl?.getAttribute('data-ai-dark-scheme') || undefined;
  const appLightScheme = appEl?.getAttribute('data-ai-light-scheme') || undefined;

  const styles = {
    overlay: {
      position: 'fixed' as const,
      inset: 0,
      zIndex: 50000,
      background: 'color-mix(in srgb, var(--theme-page-bg, #0b1220) 35%, rgba(0, 0, 0, 0.55))',
      backdropFilter: 'blur(6px)',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      // Capture pointer so clicks do not fall through to the main window chrome.
      pointerEvents: 'auto' as const,
      '--wails-draggable': 'no-drag',
    } as any,
    shell: {
      width: 'min(960px, 94vw)',
      height: 'min(720px, 88vh)',
      // Use real theme tokens (not --theme-bg* which do not exist on #App).
      background: 'var(--theme-surface, var(--theme-page-bg, #111827))',
      color: 'var(--theme-text-primary, #e5e7eb)',
      borderRadius: 16,
      border: '1px solid var(--theme-border, #334155)',
      boxShadow: '0 24px 64px rgba(0,0,0,0.45), 0 0 0 1px color-mix(in srgb, var(--theme-border, #334155) 40%, transparent) inset',
      display: 'flex',
      flexDirection: 'column' as const,
      overflow: 'hidden',
    },
    header: {
      display: 'flex',
      alignItems: 'flex-start',
      gap: 12,
      padding: '16px 18px 12px',
      borderBottom: '1px solid var(--theme-border-subtle, var(--theme-border, #1e293b))',
      background:
        'linear-gradient(180deg, color-mix(in srgb, var(--theme-primary, #8fb4dc) 10%, transparent), transparent)',
    },
    btn: {
      border: '1px solid var(--theme-border, #334155)',
      background: 'var(--theme-surface-muted, color-mix(in srgb, var(--theme-surface, #111827) 88%, #000))',
      color: 'inherit',
      borderRadius: 8,
      padding: '6px 12px',
      fontSize: '0.82rem',
      fontWeight: 600,
      cursor: 'pointer',
    } as CSSProperties,
    btnPrimary: {
      border: '1px solid color-mix(in srgb, var(--theme-primary, #8fb4dc) 55%, transparent)',
      background: 'color-mix(in srgb, var(--theme-primary, #8fb4dc) 18%, transparent)',
      color: 'var(--theme-primary, #8fb4dc)',
      borderRadius: 8,
      padding: '6px 12px',
      fontSize: '0.82rem',
      fontWeight: 650,
      cursor: 'pointer',
    } as CSSProperties,
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={t('Mobile documents', 'Mobile 文稿')}
      data-ai-theme={appTheme}
      data-ai-dark-scheme={appDarkScheme}
      data-ai-light-scheme={appLightScheme}
      style={styles.overlay}
      // Do not close on backdrop / main-window clicks — easy to dismiss mid-share.
      onMouseDown={(e) => {
        e.stopPropagation();
      }}
      onClick={(e) => {
        e.stopPropagation();
      }}
    >
      <div
        style={styles.shell}
        onMouseDown={(e) => e.stopPropagation()}
        onClick={(e) => e.stopPropagation()}
      >
        <div style={styles.header}>
          <div
            style={{
              width: 40,
              height: 40,
              borderRadius: 12,
              display: 'grid',
              placeItems: 'center',
              background: 'color-mix(in srgb, var(--theme-primary, #4f7f6f) 16%, transparent)',
              border: '1px solid color-mix(in srgb, var(--theme-primary, #4f7f6f) 30%, transparent)',
              flexShrink: 0,
            }}
            aria-hidden
          >
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7">
              <rect x="7" y="2.5" width="10" height="19" rx="2" />
              <path d="M10 5.5h4M10 9h4M10 12h4M10 15h2.5M9.5 18.5h5" strokeLinecap="round" />
            </svg>
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontWeight: 750, fontSize: '1.05rem', letterSpacing: '0.01em' }}>
              {t('Mobile document library', 'Mobile 文稿库')}
            </div>
            <div style={{ fontSize: '0.78rem', opacity: 0.72, marginTop: 4, lineHeight: 1.4 }}>
              {t(
                'Shared Hub library with the phone app. Drop original files here (Office/PDF/images/text).',
                '与手机端共用 Hub 文库。将原始文件拖入此处即可发布到手机（Office/PDF/图片/文本）。',
              )}
            </div>
          </div>
          <div style={{ display: 'flex', gap: 8, flexShrink: 0 }}>
            <button type="button" style={styles.btn} onClick={() => void refresh()} disabled={loading || uploading}>
              {loading ? t('Loading…', '加载中…') : t('Refresh', '刷新')}
            </button>
            <button type="button" style={styles.btn} onClick={onClose}>
              {t('Close', '关闭')}
            </button>
          </div>
        </div>

        {/* Drop zone */}
        <div
          onDrop={onDrop}
          onDragEnter={onDragEnter}
          onDragLeave={onDragLeave}
          onDragOver={onDragOver}
          style={{
            margin: '12px 16px 0',
            borderRadius: 12,
            border: dragOver
              ? '1.5px dashed color-mix(in srgb, var(--theme-primary, #4f7f6f) 80%, #fff)'
              : '1.5px dashed var(--theme-border, rgba(255,255,255,0.14))',
            background: dragOver
              ? 'color-mix(in srgb, var(--theme-primary, #4f7f6f) 12%, transparent)'
              : 'color-mix(in srgb, var(--theme-field-bg, #fff) 3%, transparent)',
            padding: '14px 16px',
            display: 'flex',
            alignItems: 'center',
            gap: 14,
            transition: 'background 120ms ease, border-color 120ms ease',
          }}
        >
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontWeight: 650, fontSize: '0.9rem' }}>
              {dragOver
                ? t('Release to share with Mobile', '松开以上传并分享到手机')
                : t('Drag & drop files to share', '拖放文件到此处分享')}
            </div>
            <div style={{ fontSize: '0.75rem', opacity: 0.68, marginTop: 4 }}>
              {t(
                'Original files: DOCX / PDF / images / Markdown / text. Max 25MB. Phone can preview & share the original.',
                '原件入库：DOCX / PDF / 图片 / Markdown / 文本，单文件 ≤25MB。手机可预览并分享原文件。',
              )}
            </div>
          </div>
          <input
            ref={fileInputRef}
            type="file"
            multiple
            accept=".md,.markdown,.txt,.log,.json,.yaml,.yml,.csv,.xml,.html,.css,.js,.ts,.go,.py,.sh,.ps1,.docx,.xlsx,.pdf,.doc,.png,.jpg,.jpeg,.webp,.gif,text/*,image/*"
            style={{ display: 'none' }}
            onChange={(e) => {
              if (e.target.files?.length) void publishFiles(e.target.files);
              e.target.value = '';
            }}
          />
          <button
            type="button"
            style={styles.btnPrimary}
            disabled={uploading}
            onClick={() => fileInputRef.current?.click()}
          >
            {uploading ? t('Sharing…', '分享中…') : t('Choose files', '选择文件')}
          </button>
        </div>

        {banner ? (
          <div
            style={{
              margin: '10px 16px 0',
              padding: '10px 12px',
              borderRadius: 10,
              background: 'color-mix(in srgb, var(--theme-primary, #4f7f6f) 14%, transparent)',
              border: '1px solid color-mix(in srgb, var(--theme-primary, #4f7f6f) 28%, transparent)',
              fontSize: '0.82rem',
              color: 'var(--theme-primary, #9fd4c3)',
            }}
          >
            {banner}
          </div>
        ) : null}
        {error ? (
          <div
            style={{
              margin: '10px 16px 0',
              padding: '10px 12px',
              borderRadius: 10,
              background: 'rgba(220,80,80,0.12)',
              border: '1px solid rgba(220,80,80,0.28)',
              color: '#ffb4b4',
              fontSize: '0.82rem',
            }}
          >
            {error}
          </div>
        ) : null}
        {jobs.length > 0 ? (
          <div style={{ margin: '8px 16px 0', display: 'flex', flexWrap: 'wrap', gap: 6 }}>
            {jobs.map((j, i) => (
              <span
                key={`${j.name}-${i}`}
                style={{
                  fontSize: '0.72rem',
                  padding: '3px 8px',
                  borderRadius: 999,
                  border: '1px solid var(--theme-border, rgba(255,255,255,0.1))',
                  opacity: j.status === 'error' ? 1 : 0.9,
                  color: j.status === 'error' ? '#ffb4b4' : j.status === 'done' ? '#9fd4c3' : 'inherit',
                }}
                title={j.message}
              >
                <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
                  <StatusGlyph
                    kind={j.status === 'done' ? 'ok' : j.status === 'error' ? 'error' : 'pending'}
                    size={12}
                  />
                  {j.name}
                </span>
              </span>
            ))}
          </div>
        ) : null}

        <div style={{ display: 'flex', flex: 1, minHeight: 0, marginTop: 12, borderTop: '1px solid var(--theme-border, rgba(255,255,255,0.08))' }}>
          {/* List */}
          <div
            style={{
              width: '38%',
              minWidth: 240,
              borderRight: '1px solid var(--theme-border, rgba(255,255,255,0.08))',
              display: 'flex',
              flexDirection: 'column',
              minHeight: 0,
            }}
          >
            <div style={{ padding: '10px 12px', borderBottom: '1px solid var(--theme-border, rgba(255,255,255,0.06))' }}>
              <input
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder={t('Search drafts…', '搜索文稿…')}
                style={{
                  width: '100%',
                  boxSizing: 'border-box',
                  borderRadius: 8,
                  border: '1px solid var(--theme-border, rgba(255,255,255,0.12))',
                  background: 'var(--theme-field-bg, rgba(0,0,0,0.2))',
                  color: 'inherit',
                  padding: '8px 10px',
                  fontSize: '0.82rem',
                  outline: 'none',
                }}
              />
              <div style={{ fontSize: '0.72rem', opacity: 0.55, marginTop: 6 }}>
                {t(`${filtered.length} draft(s)`, `${filtered.length} 篇文稿`)}
              </div>
            </div>
            <div style={{ flex: 1, overflow: 'auto' }}>
              {loading ? (
                <div style={{ padding: 16, opacity: 0.7 }}>{t('Loading…', '加载中…')}</div>
              ) : filtered.length === 0 ? (
                <div style={{ padding: 16, opacity: 0.7, fontSize: '0.85rem', lineHeight: 1.5 }}>
                  {t(
                    'No drafts yet. Drop a file above or create one on the phone.',
                    '暂无文稿。可拖入文件，或在手机端创建。',
                  )}
                </div>
              ) : (
                filtered.map((d) => {
                  const active = selected?.id === d.id;
                  return (
                    <div
                      key={d.id}
                      style={{
                        display: 'flex',
                        alignItems: 'stretch',
                        borderBottom: '1px solid var(--theme-border, rgba(255,255,255,0.05))',
                        borderLeft: active
                          ? '3px solid var(--theme-primary, #4f7f6f)'
                          : '3px solid transparent',
                        background: active
                          ? 'color-mix(in srgb, var(--theme-primary, #4f7f6f) 12%, transparent)'
                          : 'transparent',
                      }}
                    >
                      <button
                        type="button"
                        onClick={() => void selectDraft(d)}
                        style={{
                          flex: 1,
                          minWidth: 0,
                          textAlign: 'left',
                          padding: '12px 10px 12px 14px',
                          border: 'none',
                          background: 'transparent',
                          color: 'inherit',
                          cursor: 'pointer',
                        }}
                      >
                        <div style={{ fontWeight: 650, fontSize: '0.9rem', lineHeight: 1.3 }}>
                          {d.title || d.id}
                        </div>
                        <div style={{ fontSize: '0.72rem', opacity: 0.58, marginTop: 5 }}>
                          {d.has_original
                            ? t('Original file', '原件')
                            : (d.rune_count ?? 0) > 0
                              ? `${d.rune_count} ${t('chars', '字')}`
                              : ''}
                          {d.has_original && d.source_size
                            ? ` · ${Math.max(1, Math.round(d.source_size / 1024))} KB`
                            : ''}
                          {d.updated_at
                            ? ` · ${formatUpdatedAt(d.updated_at, isZh)}`
                            : ''}
                        </div>
                        {d.preview ? (
                          <div
                            style={{
                              fontSize: '0.76rem',
                              opacity: 0.72,
                              marginTop: 5,
                              whiteSpace: 'nowrap',
                              overflow: 'hidden',
                              textOverflow: 'ellipsis',
                            }}
                          >
                            {d.preview}
                          </div>
                        ) : null}
                      </button>
                      <button
                        type="button"
                        title={t('Delete', '删除')}
                        aria-label={t(`Delete ${d.title || d.id}`, `删除 ${d.title || d.id}`)}
                        onClick={(e) => {
                          e.stopPropagation();
                          void deleteDraft(d);
                        }}
                        style={{
                          flexShrink: 0,
                          width: 36,
                          border: 'none',
                          background: 'transparent',
                          color: 'rgba(255,180,180,0.85)',
                          cursor: 'pointer',
                          fontSize: '0.95rem',
                          opacity: 0.75,
                        }}
                      >
                        ×
                      </button>
                    </div>
                  );
                })
              )}
            </div>
          </div>

          {/* Preview */}
          <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0, minHeight: 0 }}>
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 8,
                padding: '10px 14px',
                borderBottom: '1px solid var(--theme-border, rgba(255,255,255,0.06))',
              }}
            >
              <div style={{ flex: 1, minWidth: 0, fontWeight: 700, fontSize: '0.92rem', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {selected ? selected.title || selected.id : t('Preview', '预览')}
              </div>
              {selected ? (
                <>
                  <button type="button" style={styles.btn} onClick={() => void copyBody()} disabled={!selected.markdown && !selected.preview}>
                    {t('Copy', '复制')}
                  </button>
                  {selected.has_original ? (
                    <>
                      <button
                        type="button"
                        style={styles.btn}
                        onClick={() => void openSelectedOriginal()}
                        disabled={uploading}
                        title={t('Open the original uploaded file', '用系统默认程序打开原件')}
                      >
                        {t('Open original', '打开原件')}
                      </button>
                      <button
                        type="button"
                        style={styles.btn}
                        onClick={() => void saveSelectedOriginal()}
                        disabled={uploading}
                        title={t('Save the original file to disk', '将原件另存到本地')}
                      >
                        {t('Save original', '保存原件')}
                      </button>
                    </>
                  ) : null}
                  <button
                    type="button"
                    style={styles.btnPrimary}
                    onClick={() => void shareSelectedAgain()}
                    disabled={uploading}
                    title={t(
                      'Already on Hub — confirms share without creating a duplicate',
                      '文稿已在 Hub 库中；仅确认共享，不会重复创建',
                    )}
                  >
                    {t('Already on Mobile', '已共享到手机')}
                  </button>
                  <button
                    type="button"
                    style={{
                      ...styles.btn,
                      borderColor: 'rgba(220,80,80,0.45)',
                      color: '#ffb4b4',
                    }}
                    onClick={() => void deleteDraft(selected)}
                    disabled={uploading}
                  >
                    {t('Delete', '删除')}
                  </button>
                </>
              ) : null}
            </div>
            <div
              style={{
                flex: 1,
                overflow: 'auto',
                padding: 16,
                fontFamily: "ui-monospace, 'SF Mono', 'Cascadia Code', Menlo, Consolas, monospace",
                fontSize: '0.82rem',
                lineHeight: 1.5,
                opacity: selected ? 1 : 0.65,
              }}
            >
              {selected ? (
                <MobileDocPreviewBody
                  markdown={selected.markdown || selected.preview || ''}
                  emptyLabel={t('(empty body)', '（无正文）')}
                />
              ) : (
                t('Select a draft on the left, or drop original files above to share with Mobile.', '请选择左侧文稿，或将原始文件拖到上方以分享到手机。')
              )}
            </div>
            {selected?.id ? (
              <div
                style={{
                  padding: '8px 14px',
                  borderTop: '1px solid var(--theme-border, rgba(255,255,255,0.06))',
                  fontSize: '0.7rem',
                  opacity: 0.5,
                  fontFamily: 'ui-monospace, monospace',
                }}
              >
                ID: {selected.id}
                {selected.has_original && selected.source_filename
                  ? ` · ${t('original', '原件')}: ${selected.source_filename}`
                  : ''}
              </div>
            ) : null}
          </div>
        </div>
      </div>
    </div>
  );
}
