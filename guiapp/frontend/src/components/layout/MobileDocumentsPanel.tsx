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
type MobileLibraryAudio = { content_type?: string; size_bytes?: number; duration_sec?: number; available?: boolean };
type MobileLibraryProcessing = { status?: string; progress?: number; message?: string; failure_code?: string };
type MobileLibraryDerivedDocuments = { transcript_draft_id?: string; minutes_draft_id?: string };
type MobileLibraryItem = MobileDocumentDraftSummary & { type?: 'document' | 'audio'; audio?: MobileLibraryAudio; processing?: MobileLibraryProcessing; derived_documents?: MobileLibraryDerivedDocuments; managed_by_recording_id?: string; retention_until?: string };
type MeetingRecordingAudioPayload = { content_type?: string; data_base64?: string };

type MobileDocumentsPanelProps = {
  lang: string;
  open: boolean;
  onClose: () => void;
  /** Render inside the main content area (files nav tab) instead of a modal overlay. */
  inline?: boolean;
};

type UploadJob = {
  name: string;
  status: 'reading' | 'uploading' | 'done' | 'error';
  message?: string;
};
type MobileDocumentQuota = { document_quota_bytes?: number; document_quota_used_bytes?: number; document_quota_remaining?: number };

function callGetDocumentQuota(): Promise<MobileDocumentQuota> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.GetMobileDocumentQuota) return Promise.reject(new Error('Desktop binding missing GetMobileDocumentQuota — rebuild GUI after pull.'));
  return app.GetMobileDocumentQuota();
}

function callListDrafts(limit: number, includeBody: boolean): Promise<MobileDocumentDraftSummary[]> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.ListMobileDocumentDrafts) {
    return Promise.reject(new Error(langMissingBinding()));
  }
  return app.ListMobileDocumentDrafts(limit, includeBody);
}
function callListLibraryItems(limit: number): Promise<MobileLibraryItem[]> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.ListMobileLibraryItems) return Promise.reject(new Error('Desktop binding missing ListMobileLibraryItems — rebuild GUI after pull.'));
  return app.ListMobileLibraryItems(limit);
}
function callGetLibraryItem(id: string): Promise<MobileLibraryItem> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.GetMobileLibraryItem) return Promise.reject(new Error('Desktop binding missing GetMobileLibraryItem — rebuild GUI after pull.'));
  return app.GetMobileLibraryItem(id);
}
function callProcessMeetingRecording(id: string): Promise<MobileLibraryItem> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.ProcessMobileMeetingRecording) return Promise.reject(new Error('Desktop binding missing ProcessMobileMeetingRecording — rebuild GUI after pull.'));
  return app.ProcessMobileMeetingRecording(id);
}
function callDeleteMeetingRecording(id: string): Promise<MobileLibraryItem> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.DeleteMobileMeetingRecording) return Promise.reject(new Error('Desktop binding missing DeleteMobileMeetingRecording — rebuild GUI after pull.'));
  return app.DeleteMobileMeetingRecording(id);
}
function callDeleteMeetingRecordingAndResults(id: string): Promise<void> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.DeleteMobileMeetingRecordingAndResults) return Promise.reject(new Error('Desktop binding missing DeleteMobileMeetingRecordingAndResults — rebuild GUI after pull.'));
  return app.DeleteMobileMeetingRecordingAndResults(id);
}
function callGetMeetingRecordingAudio(id: string): Promise<MeetingRecordingAudioPayload> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.GetMobileMeetingRecordingAudio) return Promise.reject(new Error('Desktop binding missing GetMobileMeetingRecordingAudio — rebuild GUI after pull.'));
  return app.GetMobileMeetingRecordingAudio(id);
}
function callOpenMeetingRecordingAudio(id: string): Promise<string> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.OpenMobileMeetingRecordingAudio) return Promise.reject(new Error('Desktop binding missing OpenMobileMeetingRecordingAudio — rebuild GUI after pull.'));
  return app.OpenMobileMeetingRecordingAudio(id);
}
function callSaveMeetingRecordingAudio(id: string): Promise<string> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.SaveMobileMeetingRecordingAudio) return Promise.reject(new Error('Desktop binding missing SaveMobileMeetingRecordingAudio — rebuild GUI after pull.'));
  return app.SaveMobileMeetingRecordingAudio(id);
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
      <div className="mobile-documents-inline-error" style={{ opacity: 0.65, fontSize: '0.78rem', padding: '6px 0' }}>
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
          background: 'color-mix(in srgb, var(--theme-control-well, var(--theme-surface-muted, #000)) 40%, transparent)',
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
function isAudioItem(item: MobileLibraryItem | null | undefined): boolean { return item?.type === 'audio'; }

/** List subtitle when original audio is gone: distinguish user delete vs retention expiry. */
function audioUnavailableLabel(item: MobileLibraryItem, t: (en: string, zh: string) => string): string {
  const msg = String(item.processing?.message || '').toLowerCase();
  if (msg.includes('deleted')) return t('Audio deleted', '音频已删除');
  if (msg.includes('expired')) return t('Audio expired', '音频已过期');
  return t('Audio unavailable', '音频不可用');
}

function derivedDraftIDs(item: MobileLibraryItem | null | undefined): { transcript: string; minutes: string } {
  return {
    transcript: String(item?.derived_documents?.transcript_draft_id || '').trim(),
    minutes: String(item?.derived_documents?.minutes_draft_id || '').trim(),
  };
}

function hasLinkedDerivedDocs(item: MobileLibraryItem | null | undefined): boolean {
  const ids = derivedDraftIDs(item);
  return Boolean(ids.transcript || ids.minutes);
}

/** True when linked draft IDs still appear as separate rows in a library snapshot. */
function linkedDocsPresentIn(
  list: MobileLibraryItem[],
  ids: { transcript: string; minutes: string },
): boolean {
  if (!ids.transcript && !ids.minutes) return false;
  return list.some((item) => item.id === ids.transcript || item.id === ids.minutes);
}

/** Optimistic / fallback patch after raw-audio delete succeeds. */
function withAudioDeleted(item: MobileLibraryItem, updated?: MobileLibraryItem | null): MobileLibraryItem {
  const base = updated ? { ...item, ...updated } : { ...item };
  const msg =
    base.processing?.message ||
    item.processing?.message ||
    'raw audio deleted; transcript and minutes remain available';
  return {
    ...base,
    type: 'audio',
    audio: {
      ...(item.audio || {}),
      ...(updated?.audio || {}),
      available: false,
    },
    processing: {
      ...(item.processing || {}),
      ...(updated?.processing || {}),
      message: msg,
    },
    derived_documents: {
      ...(item.derived_documents || {}),
      ...(updated?.derived_documents || {}),
    },
  };
}
function managedRecordingID(item: MobileLibraryItem | null | undefined): string { return String(item?.managed_by_recording_id || '').trim(); }

/** Desktop library delete mode for a row. */
type LibraryDeleteMode = 'soft-audio' | 'remove-unavailable' | 'full-recording' | 'document';

function libraryDeleteMode(item: MobileLibraryItem): LibraryDeleteMode {
  if (isAudioItem(item)) {
    // Audio already gone: full remove (soft-delete again is a no-op and leaves ghosts).
    // IDs alone decide copy; both branches hit the same full-delete API.
    if (item.audio?.available === false) {
      return hasLinkedDerivedDocs(item) ? 'full-recording' : 'remove-unavailable';
    }
    return 'soft-audio';
  }
  if (managedRecordingID(item)) return 'full-recording';
  return 'document';
}

function libraryDeleteRecordingID(item: MobileLibraryItem): string {
  // Audio rows are the recording itself; managed docs point at their parent recording.
  if (isAudioItem(item)) return String(item.id || '').trim();
  return managedRecordingID(item);
}

function libraryDeleteButtonCopy(
  item: MobileLibraryItem,
  t: (en: string, zh: string) => string,
): { title: string; aria: string } {
  const name = item.title || item.id;
  const mode = libraryDeleteMode(item);
  if (mode === 'soft-audio') {
    return {
      title: t('Delete original audio', '删除原始音频'),
      aria: t(`Delete original audio for ${name}`, `删除 ${name} 的原始音频`),
    };
  }
  if (mode === 'remove-unavailable') {
    return {
      title: t('Remove from library', '从文稿库移除'),
      aria: t(`Remove ${name} from the library`, `从文稿库移除 ${name}`),
    };
  }
  if (mode === 'full-recording') {
    return {
      title: t('Delete recording and generated documents', '删除录音及生成文档'),
      aria: t(`Delete the recording and generated documents for ${name}`, `删除 ${name} 所属录音及生成文档`),
    };
  }
  return {
    title: t('Delete', '删除'),
    aria: t(`Delete ${name}`, `删除 ${name}`),
  };
}

function libraryDeleteConfirmCopy(
  mode: LibraryDeleteMode,
  title: string,
  t: (en: string, zh: string) => string,
): { body: string; heading: string; confirmText: string } {
  if (mode === 'soft-audio') {
    return {
      body: t(
        `Delete “${title}”? The original audio will be removed; generated transcripts and minutes remain.`,
        `删除「${title}」？原始音频将被删除，已生成的逐字稿和会议纪要会保留。`,
      ),
      heading: t('Delete original audio?', '删除原始音频？'),
      confirmText: t('Delete', '删除'),
    };
  }
  if (mode === 'remove-unavailable') {
    return {
      body: t(
        `Remove “${title}” from the shared library? The original audio is already gone.`,
        `从文稿库移除「${title}」？原始音频已不存在。`,
      ),
      heading: t('Remove from library?', '从文稿库移除？'),
      confirmText: t('Remove', '移除'),
    };
  }
  if (mode === 'full-recording') {
    return {
      body: t(
        `Delete the meeting recording for “${title}”? Its original audio and all generated transcripts and meeting minutes will be removed.`,
        `删除「${title}」所属的会议录音？原始音频、逐字稿和会议纪要将一并删除。`,
      ),
      heading: t('Delete meeting recording and results?', '删除会议录音及结果？'),
      confirmText: t('Delete all', '全部删除'),
    };
  }
  return {
    body: t(
      `Delete “${title}” from the shared Hub library? The phone app will no longer be able to open this document.`,
      `从共享文稿库删除「${title}」？手机端也将无法再看到该文稿。`,
    ),
    heading: t('Delete shared document?', '删除共享文稿？'),
    confirmText: t('Delete', '删除'),
  };
}
function isProcessingAudio(item: MobileLibraryItem | null | undefined): boolean { const status = item?.processing?.status || ''; return status === 'processing' || status === 'finalizing'; }
function hasMeetingMinutes(item: MobileLibraryItem | null | undefined): boolean { return Boolean(item?.derived_documents?.minutes_draft_id); }
function formatAudioDuration(seconds?: number): string { const total = Math.max(0, Math.round(Number(seconds || 0))); const min = Math.floor(total / 60); return `${min}:${String(total % 60).padStart(2, '0')}`; }
function formatLibraryFileSize(bytes?: number): string { const value = Math.max(0, Number(bytes || 0)); if (value === 0) return '0 B'; return value < 1024 * 1024 ? `${Math.max(1, Math.round(value / 1024))} KB` : `${(value / (1024 * 1024)).toFixed(value >= 10 * 1024 * 1024 ? 0 : 1)} MB`; }
function MeetingRecordingPlayer({ item }: { item: MobileLibraryItem }) {
  const [src, setSrc] = useState(''); const [error, setError] = useState('');
  useEffect(() => { let url = ''; let cancelled = false; setSrc(''); setError(''); if (!item.audio?.available) return () => undefined; void callGetMeetingRecordingAudio(item.id).then((payload) => { const raw = String(payload?.data_base64 || ''); if (!raw) throw new Error('empty audio'); const binary = atob(raw); const bytes = new Uint8Array(binary.length); for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i); url = URL.createObjectURL(new Blob([bytes], { type: String(payload?.content_type || item.audio?.content_type || 'audio/mp4').split(';')[0] })); if (!cancelled) setSrc(url); }).catch((e: any) => { if (!cancelled) setError(String(e?.message || e || 'load failed')); }); return () => { cancelled = true; if (url) URL.revokeObjectURL(url); }; }, [item.id, item.audio?.available, item.audio?.content_type]);
  if (!item.audio?.available) return <div>Original audio is no longer available. Generated documents remain accessible.</div>;
  if (error) return <div className="mobile-documents-inline-error">Unable to load embedded playback. You can still open or save the original audio.</div>;
  if (!src) return <div>Loading audio…</div>;
  return <audio controls preload="metadata" src={src} style={{ width: '100%' }} aria-label="Meeting recording playback" />;
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
export function MobileDocumentsPanel({ lang, open, onClose, inline = false }: MobileDocumentsPanelProps) {
  const { showConfirm } = useDialog();
  const isZh = lang !== 'en' && !String(lang).startsWith('en');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [banner, setBanner] = useState('');
  const [drafts, setDrafts] = useState<MobileLibraryItem[]>([]);
  const [selected, setSelected] = useState<MobileLibraryItem | null>(null);
  const [filter, setFilter] = useState('');
  const [dragOver, setDragOver] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [jobs, setJobs] = useState<UploadJob[]>([]);
  const [quota, setQuota] = useState<MobileDocumentQuota | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const dragDepth = useRef(0);
  // State updates do not take effect until the next render, so use a ref to
  // synchronously guard the destructive confirmation and request lifecycle.
  const deleteInFlightRef = useRef(false);

  const t = useCallback(
    (en: string, zh: string) => (isZh ? zh : en),
    [isZh],
  );

  // null = request failed (keep prior list); [] = successful empty library.
  // Callers must not treat null like "no items" or they drop rows after a
  // successful mutation when the follow-up list call flakes.
  const refresh = useCallback(async (): Promise<MobileLibraryItem[] | null> => {
    setLoading(true);
    setError('');
    try {
      const [list, quotaResult] = await Promise.all([
        callListLibraryItems(80),
        callGetDocumentQuota().catch(() => null),
      ]);
      const next = Array.isArray(list) ? list : [];
      setDrafts(next);
      if (quotaResult) setQuota(quotaResult);
      return next;
    } catch (e: any) {
      setError(String(e?.message || e || 'load failed'));
      // Keep the previous drafts snapshot — wiping on a transient list failure
      // made soft-delete look like "nothing left in the library".
      return null;
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
  // Inline mode is a regular page: navigation, not Esc, leaves it.
  useEffect(() => {
    if (!open || inline) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        onClose();
      }
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [open, inline, onClose]);

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return drafts;
    return drafts.filter((d) => {
      const hay = `${d.title || ''} ${d.preview || ''} ${d.id || ''}`.toLowerCase();
      return hay.includes(q);
    });
  }, [drafts, filter]);

  const selectDraft = async (d: MobileLibraryItem) => {
    setSelected(d);
    try {
      const full = await callGetLibraryItem(d.id);
      setSelected({ ...d, ...full });
    } catch {
      // keep list row
    }
  };
  const openDocumentFromAudio = async (draftID?: string) => {
    if (!draftID) return;
    const existing = drafts.find((item) => item.id === draftID);
    if (existing) { await selectDraft(existing); return; }
    try { setSelected(await callGetLibraryItem(draftID)); } catch (e: any) { setError(String(e?.message || e || 'open document failed')); }
  };

  const processAudio = async (item: MobileLibraryItem) => {
    if (isProcessingAudio(item)) return;
    setUploading(true); setError(''); setBanner('');
    try {
      const updated = await callProcessMeetingRecording(item.id);
      setSelected(updated);
      setDrafts((previous) => previous.map((candidate) => candidate.id === item.id ? { ...candidate, ...updated } : candidate));
      setBanner(t('Meeting minutes processing started. The status will refresh automatically.', '已开始生成会议纪要，状态将自动刷新。'));
    } catch (e: any) { setError(String(e?.message || e || 'start meeting minutes failed')); } finally { setUploading(false); }
  };

  const openAudio = async (item: MobileLibraryItem) => {
    setUploading(true); setError('');
    try { const path = await callOpenMeetingRecordingAudio(item.id); setBanner(path ? `Opened original audio: ${path}` : 'Opened original audio'); } catch (e: any) { setError(String(e?.message || e || 'open audio failed')); } finally { setUploading(false); }
  };

  const saveAudio = async (item: MobileLibraryItem) => {
    setUploading(true); setError('');
    try { const path = await callSaveMeetingRecordingAudio(item.id); setBanner(path ? `Saved original audio to ${path}` : t('Save cancelled.', '已取消保存。')); } catch (e: any) { setError(String(e?.message || e || 'save audio failed')); } finally { setUploading(false); }
  };

  useEffect(() => {
    if (!open || !selected || !isProcessingAudio(selected)) return undefined;
    const timer = window.setInterval(() => {
      void callGetLibraryItem(selected.id).then((updated) => {
        setSelected((current) => current?.id === updated.id ? updated : current);
        setDrafts((current) => current.map((item) => item.id === updated.id ? { ...item, ...updated } : item));
      }).catch(() => undefined);
    }, 2500);
    return () => window.clearInterval(timer);
  }, [open, selected?.id, selected?.processing?.status]);

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
        const maxInputBytes = 400 * 1024 * 1024;
        if (file.size > maxInputBytes) {
          throw new Error(t('File is too large to compress safely', '文件过大，无法安全压缩（压缩后必须 ≤100MB）'));
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

  const deleteDraft = async (d: MobileLibraryItem) => {
    if (deleteInFlightRef.current) return;
    deleteInFlightRef.current = true;
    const title = d.title || d.id;
    const mode = libraryDeleteMode(d);
    const softDeleteAudioOnly = mode === 'soft-audio';
    const fullDeleteRecording = mode === 'full-recording' || mode === 'remove-unavailable';
    const recordingKey = libraryDeleteRecordingID(d);
    const linked = derivedDraftIDs(d);
    const confirmCopy = libraryDeleteConfirmCopy(mode, title, t);
    let ok = false;
    try {
      ok = await showConfirm(confirmCopy.body, confirmCopy.heading, {
        confirmText: confirmCopy.confirmText,
        cancelText: t('Cancel', '取消'),
        confirmVariant: 'danger',
      });
    } catch (e: any) {
      setError(String(e?.message || e || 'delete confirmation failed'));
      deleteInFlightRef.current = false;
      return;
    }
    if (!ok) {
      deleteInFlightRef.current = false;
      return;
    }
    setUploading(true);
    setError('');
    setBanner('');
    // Full recording delete: drop the recording row, managed children, and known draft IDs.
    const clearRecordingAndResultsFromList = (key: string, extraIDs: string[] = []) => {
      const drop = new Set([d.id, key, ...extraIDs].map((id) => String(id || '').trim()).filter(Boolean));
      setDrafts((current) =>
        current.filter((item) => !drop.has(item.id) && managedRecordingID(item) !== key),
      );
      setSelected((current) => {
        if (!current) return current;
        if (drop.has(current.id) || managedRecordingID(current) === key) return null;
        return current;
      });
    };
    // Soft-audio delete: only the audio row goes away. Linked transcript/minutes
    // documents must stay if Hub still lists them (never pass their IDs here).
    const clearAudioRowOnly = (audioID: string) => {
      const id = String(audioID || '').trim();
      if (!id) return;
      setDrafts((current) => current.filter((item) => item.id !== id));
      setSelected((current) => (current?.id === id ? null : current));
    };
    const bannerAudioDeletedDocsRemain = () =>
      setBanner(
        t(
          `Original audio deleted for “${title}”. Generated documents remain available.`,
          `已删除「${title}」的原始音频，生成的文档仍可打开。`,
        ),
      );
    const bannerAudioDeletedNothingLeft = () =>
      setBanner(
        t(
          `Original audio deleted for “${title}”. Nothing left to keep in the library.`,
          `已删除「${title}」的原始音频，文稿库中已无关联条目。`,
        ),
      );
    /** Reconcile soft-audio delete with the post-refresh library list. */
    const applySoftAudioDeleteResult = (
      list: MobileLibraryItem[] | null,
      patched: MobileLibraryItem,
      opts?: { keptAudioRow?: boolean },
    ) => {
      const patchedLinked = derivedDraftIDs(patched);
      // List failed after a successful DELETE: leave optimistic UI as the caller set it.
      if (list === null) {
        if (opts?.keptAudioRow) bannerAudioDeletedDocsRemain();
        else bannerAudioDeletedNothingLeft();
        return;
      }
      const row = list.find((item) => item.id === d.id);
      if (row) {
        const next = withAudioDeleted(patched, row);
        setSelected((current) => (current?.id === d.id ? next : current));
        setDrafts((current) => current.map((item) => (item.id === d.id ? next : item)));
        bannerAudioDeletedDocsRemain();
        return;
      }
      // Audio row hidden by Hub — remove only that row. Document rows (if any) stay.
      clearAudioRowOnly(d.id);
      if (linkedDocsPresentIn(list, patchedLinked) || linkedDocsPresentIn(list, linked)) {
        bannerAudioDeletedDocsRemain();
      } else {
        bannerAudioDeletedNothingLeft();
      }
    };
    try {
      if (softDeleteAudioOnly) {
        const updated = withAudioDeleted(d, await callDeleteMeetingRecording(d.id));
        const updatedLinked = derivedDraftIDs(updated);
        // Prefer local evidence of real result docs over raw draft IDs (stale IDs are common ghosts).
        const localHasResults =
          hasLinkedDerivedDocs(updated) && linkedDocsPresentIn(drafts, updatedLinked);
        if (localHasResults) {
          setSelected((current) => (current?.id === d.id ? updated : current));
          setDrafts((current) => current.map((item) => (item.id === d.id ? updated : item)));
          applySoftAudioDeleteResult(await refresh(), updated, { keptAudioRow: true });
        } else {
          // No real result docs in the library → Hub will hide the audio row; drop it now.
          clearAudioRowOnly(d.id);
          applySoftAudioDeleteResult(await refresh(), updated, { keptAudioRow: false });
        }
      } else if (fullDeleteRecording) {
        if (!recordingKey) {
          setError(t('Missing meeting recording id for delete.', '删除失败：缺少会议录音 ID。'));
          return;
        }
        await callDeleteMeetingRecordingAndResults(recordingKey);
        clearRecordingAndResultsFromList(recordingKey, [linked.transcript, linked.minutes]);
        setBanner(
          mode === 'remove-unavailable'
            ? t(`Removed “${title}” from the library`, `已从文稿库移除「${title}」`)
            : t(
                `Deleted the meeting recording and generated documents for “${title}”`,
                `已删除「${title}」所属的会议录音及生成文档`,
              ),
        );
        await refresh();
      } else {
        await callDeleteDraft(d.id);
        setSelected((current) => (current?.id === d.id ? null : current));
        setDrafts((current) => current.filter((item) => item.id !== d.id));
        setBanner(t(`Deleted “${title}”`, `已删除「${title}」`));
        await refresh();
      }
    } catch (e: any) {
      const message = String(e?.message || e || 'delete failed');
      if (softDeleteAudioOnly && message.includes('AUDIO_IN_USE')) {
        setError(
          t(
            'The meeting recording is still processing. Wait for it to finish before deleting the original audio.',
            '会议录音仍在处理中。请等待处理完成后再删除原始音频。',
          ),
        );
      } else if (fullDeleteRecording && message.includes('RECORDING_IN_USE')) {
        setError(
          t(
            'The meeting recording is still processing. Wait for it to finish, then delete it and its generated documents.',
            '会议录音仍在处理中。请等待处理完成后，再删除录音及生成文档。',
          ),
        );
      } else if (message.includes('RECORDING_NOT_FOUND') && fullDeleteRecording) {
        // Full delete: recording already gone — drop the whole family locally.
        clearRecordingAndResultsFromList(recordingKey || d.id, [linked.transcript, linked.minutes]);
        setBanner(
          t(
            'This meeting recording was already deleted. The library has been refreshed.',
            '该会议录音已在其他位置删除，文稿库已刷新。',
          ),
        );
        await refresh();
      } else if (message.includes('RECORDING_NOT_FOUND') && softDeleteAudioOnly) {
        // Soft-delete 404: only drop the audio row; do not assume result docs were wiped.
        clearAudioRowOnly(d.id);
        setBanner(
          t(
            'This meeting recording was already deleted. The library has been refreshed.',
            '该会议录音已在其他位置删除，文稿库已刷新。',
          ),
        );
        await refresh();
      } else if (
        softDeleteAudioOnly &&
        (message.includes('LIBRARY_ITEM_NOT_FOUND') || message.includes('get mobile library item failed'))
      ) {
        // Legacy desktop/Hub pairing: DELETE may have succeeded while a follow-up
        // library GET still 404s. Prefer a soft success over a red error banner.
        const patched = withAudioDeleted(d);
        const kept =
          hasLinkedDerivedDocs(patched) && linkedDocsPresentIn(drafts, derivedDraftIDs(patched));
        if (kept) {
          setSelected((current) => (current?.id === d.id ? patched : current));
          setDrafts((current) => current.map((item) => (item.id === d.id ? patched : item)));
        } else {
          clearAudioRowOnly(d.id);
        }
        applySoftAudioDeleteResult(await refresh(), patched, { keptAudioRow: kept });
      } else {
        setError(message);
      }
    } finally {
      setUploading(false);
      deleteInFlightRef.current = false;
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
      background: 'rgba(15, 23, 42, 0.34)',
      backdropFilter: 'blur(8px)',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      padding: '24px',
      // Capture pointer so clicks do not fall through to the main window chrome.
      pointerEvents: 'auto' as const,
      '--wails-draggable': 'no-drag',
    } as any,
    shell: {
      width: 'min(1440px, calc(100vw - 64px))',
      height: 'min(1080px, calc(100vh - 64px))',
      minHeight: 0,
      // Use real theme tokens (not --theme-bg* which do not exist on #App).
      background: 'var(--theme-surface, #ffffff)',
      color: 'var(--theme-text-primary, #1c2733)',
      // Follow the shared surface scale so the panel keeps the same radius
      // as the workbench in both light and dark themes.
      // Keep the documents dialog on the shared workbench radius scale.  The
      // token set intentionally stops at --radius-lg; --radius-xl would
      // silently fall back to an oversized 24px shell.
      borderRadius: 'var(--radius-lg, 14px)',
      border: '1px solid var(--theme-border, #d9e1ec)',
      boxShadow: 'var(--shadow-lg, 0 24px 70px rgba(19,43,77,0.18), 0 4px 14px rgba(19,43,77,0.08))',
      display: 'flex',
      flexDirection: 'column' as const,
      overflow: 'hidden',
    },
    header: {
      display: 'flex',
      alignItems: 'center',
      gap: 16,
      padding: '24px 28px 20px',
      borderBottom: '1px solid var(--theme-border-subtle, var(--theme-border, #1e293b))',
      background:
        'linear-gradient(180deg, color-mix(in srgb, var(--theme-primary, #2f6fbc) 5%, var(--theme-surface, #ffffff)), var(--theme-surface, #ffffff))',
    },
    btn: {
      border: '1px solid var(--theme-border, #d9e1ec)',
      background: 'var(--theme-surface, #ffffff)',
      color: 'var(--theme-text-primary, #1c2733)',
      borderRadius: 'var(--radius-md, 12px)',
      minHeight: 44,
      padding: '0 16px',
      fontSize: '0.9rem',
      fontWeight: 600,
      cursor: 'pointer',
    } as CSSProperties,
    btnPrimary: {
      border: '1px solid color-mix(in srgb, var(--theme-primary, #2f6fbc) 48%, var(--theme-border, #d9e1ec))',
      background: 'var(--theme-primary-soft, rgba(47,111,188,0.10))',
      color: 'var(--theme-primary-strong, #235a9e)',
      borderRadius: 'var(--radius-md, 12px)',
      minHeight: 44,
      padding: '0 18px',
      fontSize: '0.9rem',
      fontWeight: 650,
      cursor: 'pointer',
    } as CSSProperties,
  };

  const shellStyle: CSSProperties = inline
    ? { ...styles.shell, width: '100%', height: '100%', borderRadius: 0, border: 'none', boxShadow: 'none' }
    : styles.shell;

  const shell = (
      <div
        className="mobile-documents-shell"
        style={shellStyle}
        onMouseDown={(e) => e.stopPropagation()}
        onClick={(e) => e.stopPropagation()}
      >
        <div style={styles.header} className="mobile-documents-header">
          <div
            className="mobile-documents-icon"
            style={{
              width: 62,
              height: 62,
              borderRadius: 16,
              display: 'grid',
              placeItems: 'center',
              background: 'color-mix(in srgb, var(--theme-primary, #2f6fbc) 12%, transparent)',
              border: '1px solid color-mix(in srgb, var(--theme-primary, #2f6fbc) 30%, transparent)',
              flexShrink: 0,
            }}
            aria-hidden
          >
            <svg width="30" height="30" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7">
              <rect x="7" y="2.5" width="10" height="19" rx="2" />
              <path d="M10 5.5h4M10 9h4M10 12h4M10 15h2.5M9.5 18.5h5" strokeLinecap="round" />
            </svg>
          </div>
          <div style={{ flex: 1, minWidth: 0 }} className="mobile-documents-heading">
            <div className="mobile-documents-title" style={{ fontWeight: 750, fontSize: '1.55rem', letterSpacing: '-0.02em' }}>
              {t('Mobile document library', '移动文稿库')}
            </div>
            <div className="mobile-documents-subtitle" style={{ fontSize: '1rem', opacity: 0.72, marginTop: 6, lineHeight: 1.45 }}>
              {t(
                'Shared Hub library with the phone app. Drop files of any type here.',
                '与手机端共用 Hub 文库。可将任意格式文件拖入此处。',
              )}
            </div>
          </div>
          <div style={{ display: 'flex', gap: 8, flexShrink: 0 }} className="mobile-documents-header-actions">
            <button type="button" className="mobile-documents-btn" style={styles.btn} onClick={() => void refresh()} disabled={loading || uploading}>
              {loading ? t('Loading…', '加载中…') : t('Refresh', '刷新')}
            </button>
            {!inline && (
              <button type="button" className="mobile-documents-btn" style={styles.btn} onClick={onClose}>
                {t('Close', '关闭')}
              </button>
            )}
          </div>
        </div>

        {/* Drop zone */}
        <div
          className="mobile-documents-dropzone"
          data-testid="mobile-documents-drop-zone"
          onDrop={onDrop}
          onDragEnter={onDragEnter}
          onDragLeave={onDragLeave}
          onDragOver={onDragOver}
          role="group"
          aria-label={t('Document upload drop zone', '文档上传拖放区')}
          style={{
             margin: '18px 26px 0',
             borderRadius: 18,
            border: dragOver
               ? '1.5px dashed color-mix(in srgb, var(--theme-primary, #2f6fbc) 80%, var(--theme-surface, #fff))'
               : '1.5px dashed var(--theme-border, #d9e1ec)',
            background: dragOver
               ? 'color-mix(in srgb, var(--theme-primary, #2f6fbc) 12%, transparent)'
              : 'color-mix(in srgb, var(--theme-control-well, var(--theme-surface-muted, #fff)) 3%, transparent)',
             padding: '24px',
            display: 'flex',
            alignItems: 'center',
             gap: 20,
            transition: 'background 120ms ease, border-color 120ms ease',
          }}
        >
          <div className="mobile-documents-dropzone-copy" style={{ flex: 1, minWidth: 0 }}>
            <div className="mobile-documents-dropzone-title" style={{ fontWeight: 700, fontSize: '1.3rem' }}>
              {dragOver
                ? t('Release to share with Mobile', '松开以上传并分享到手机')
                : t('Drag & drop files to share', '拖放文件到此处分享')}
            </div>
            <div className="mobile-documents-dropzone-help" style={{ fontSize: '1rem', opacity: 0.72, marginTop: 8 }}>
              {t(
                'Any file type. Max 100MB after automatic compression; existing archives and DOCX/XLSX/PPTX are not recompressed.',
                '支持任意格式；自动压缩后单文件 ≤100MB。压缩包及 DOCX/XLSX/PPTX 不重复压缩。',
              )}
            </div>
            {quota ? (
              <div className="mobile-documents-quota" style={{ marginTop: 14, maxWidth: 720 }} aria-label={t('Document storage usage', '文稿库存储空间')}>
                <div className="mobile-documents-quota-copy" style={{ display: 'flex', flexWrap: 'wrap', gap: '4px 18px', fontSize: '0.95rem', opacity: 0.82 }}>
                  <span>{t('Used', '已用')} {formatLibraryFileSize(quota.document_quota_used_bytes)}</span>
                  <span>{t('Remaining', '剩余')} {formatLibraryFileSize(quota.document_quota_remaining)}</span>
                  <span>{t('Total', '总限额')} {formatLibraryFileSize(quota.document_quota_bytes)}</span>
                </div>
                <div className="mobile-documents-quota-track" style={{ height: 6, marginTop: 9, borderRadius: 3, overflow: 'hidden', background: 'var(--theme-border, #d9e1ec)' }}>
                  <div style={{ height: '100%', width: `${Math.min(100, Math.max(0, 100 * Number(quota.document_quota_used_bytes || 0) / Math.max(1, Number(quota.document_quota_bytes || 1))))}%`, background: 'var(--theme-primary, #2f6fbc)' }} />
                </div>
              </div>
            ) : null}
          </div>
          <input
            ref={fileInputRef}
            type="file"
            multiple
            style={{ display: 'none' }}
            onChange={(e) => {
              if (e.target.files?.length) void publishFiles(e.target.files);
              e.target.value = '';
            }}
          />
          <button
            type="button"
            className="mobile-documents-btn mobile-documents-btn--primary"
            style={styles.btnPrimary}
            disabled={uploading}
            onClick={() => fileInputRef.current?.click()}
          >
            {uploading ? t('Sharing…', '分享中…') : t('Choose files', '选择文件')}
          </button>
        </div>

        {banner ? (
          <div
            className="mobile-documents-banner"
            role="status"
            aria-live="polite"
            style={{
               margin: '14px 26px 0',
               padding: '12px 16px',
               borderRadius: 12,
               background: 'color-mix(in srgb, var(--theme-primary, #2f6fbc) 10%, var(--theme-surface, #ffffff))',
               border: '1px solid color-mix(in srgb, var(--theme-primary, #2f6fbc) 28%, var(--theme-border, #d9e1ec))',
               fontSize: '0.9rem',
               color: 'var(--theme-primary-strong, #235a9e)',
            }}
          >
            {banner}
          </div>
        ) : null}
        {error ? (
          <div
            className="mobile-documents-error"
            role="alert"
            style={{
               margin: '14px 26px 0',
               padding: '12px 16px',
               borderRadius: 12,
              background: 'rgba(220,80,80,0.12)',
              border: '1px solid rgba(220,80,80,0.28)',
              color: 'var(--theme-danger, #c43d34)',
               fontSize: '0.9rem',
            }}
          >
            {error}
          </div>
        ) : null}
        {jobs.length > 0 ? (
          <div className="mobile-documents-jobs" aria-live="polite" aria-label={t('Upload progress', '上传进度')} style={{ margin: '10px 26px 0', display: 'flex', flexWrap: 'wrap', gap: 8 }}>
            {jobs.map((j, i) => (
              <span
                key={`${j.name}-${i}`}
                style={{
                  fontSize: '0.72rem',
                  padding: '3px 8px',
                  borderRadius: 999,
                  border: '1px solid var(--theme-border, #d9e1ec)',
                  opacity: j.status === 'error' ? 1 : 0.9,
                  color: j.status === 'error' ? 'var(--theme-danger, #c43d34)' : j.status === 'done' ? 'var(--theme-success, #18a86b)' : 'var(--theme-text-secondary, #44546a)',
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

        <div className="mobile-documents-body" data-testid="mobile-documents-body" style={{ display: 'flex', flex: 1, minHeight: 0, marginTop: 18, borderTop: '1px solid var(--theme-border-subtle, #e8eef5)' }}>
          {/* List */}
          <div
            className="mobile-documents-list-pane"
            style={{
               width: '38%',
               minWidth: 280,
               borderRight: '1px solid var(--theme-border, #d9e1ec)',
              display: 'flex',
              flexDirection: 'column',
              minHeight: 0,
            }}
          >
            <div className="mobile-documents-list-tools" style={{ padding: '18px 22px', borderBottom: '1px solid var(--theme-border-subtle, #e8eef5)' }}>
              <input
                className="mobile-documents-search"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder={t('Search drafts…', '搜索文稿…')}
                style={{
                  width: '100%',
                  boxSizing: 'border-box',
                  borderRadius: 12,
                  border: '1px solid var(--theme-border, #d9e1ec)',
                  background: 'var(--theme-surface-muted, #f0f4f9)',
                  color: 'var(--theme-text-primary, #1c2733)',
                  padding: '12px 14px',
                  fontSize: '1rem',
                  outline: 'none',
                }}
              />
              <div className="mobile-documents-count" style={{ fontSize: '0.85rem', opacity: 0.65, marginTop: 8 }}>
                {t(`${filtered.length} draft(s)`, `${filtered.length} 篇文稿`)}
              </div>
            </div>
            <div className="mobile-documents-list" data-testid="mobile-documents-list" style={{ flex: 1, overflow: 'auto' }}>
              {loading ? (
                <div style={{ padding: 16, opacity: 0.7 }}>{t('Loading…', '加载中…')}</div>
              ) : filtered.length === 0 ? (
                <div className="mobile-documents-empty" style={{ padding: 24, opacity: 0.7, fontSize: '1rem', lineHeight: 1.6 }}>
                  {t(
                    'No drafts yet. Drop a file above or create one on the phone.',
                    '暂无文稿。可拖入文件，或在手机端创建。',
                  )}
                </div>
              ) : (
                filtered.map((d) => {
                  const active = selected?.id === d.id;
                  const deleteCopy = libraryDeleteButtonCopy(d, t);
                  return (
                    <div
                      className={`mobile-documents-list-row${active ? ' is-active' : ''}`}
                      key={d.id}
                      style={{
                        display: 'flex',
                        alignItems: 'stretch',
                         borderBottom: '1px solid var(--theme-border-subtle, #e8eef5)',
                        borderLeft: active
                           ? '3px solid var(--theme-primary, #2f6fbc)'
                          : '3px solid transparent',
                        background: active
                           ? 'color-mix(in srgb, var(--theme-primary, #2f6fbc) 10%, var(--theme-surface, #ffffff))'
                          : 'transparent',
                      }}
                    >
                      <button
                        className="mobile-documents-list-item"
                        type="button"
                        onClick={() => void selectDraft(d)}
                        style={{
                          flex: 1,
                          minWidth: 0,
                          textAlign: 'left',
                           padding: '18px 16px 18px 20px',
                          border: 'none',
                          background: 'transparent',
                           color: 'var(--theme-text-primary, #1c2733)',
                          cursor: 'pointer',
                        }}
                      >
                        <div className="mobile-documents-list-item-title" style={{ fontWeight: 700, fontSize: '1rem', lineHeight: 1.35 }}>
                          {d.title || d.id}
                        </div>
                        <div className="mobile-documents-list-item-meta" style={{ fontSize: '0.82rem', opacity: 0.62, marginTop: 6 }}>
                          {isAudioItem(d)
                            ? `${d.audio?.available ? t('Recording', '录音') : audioUnavailableLabel(d, t)}${d.audio?.duration_sec ? ` · ${formatAudioDuration(d.audio.duration_sec)}` : ''}${d.audio?.size_bytes ? ` · ${formatLibraryFileSize(d.audio.size_bytes)}` : ''}`
                            : d.has_original
                              ? t('Original file', '原件')
                              : (d.rune_count ?? 0) > 0
                                ? `${d.rune_count} ${t('chars', '字')}`
                                : ''}
                          {!isAudioItem(d) && d.has_original && d.source_size
                            ? ` · ${formatLibraryFileSize(d.source_size)}`
                            : ''}
                          {d.updated_at
                            ? ` · ${formatUpdatedAt(d.updated_at, isZh)}`
                            : ''}
                        </div>
                        {d.preview ? (
                          <div
                            className="mobile-documents-list-item-preview"
                            style={{
                               fontSize: '0.86rem',
                              opacity: 0.72,
                               marginTop: 7,
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
                        className="mobile-documents-list-delete"
                        type="button"
                        title={deleteCopy.title}
                        aria-label={deleteCopy.aria}
                        disabled={uploading}
                        onClick={(e) => {
                          e.stopPropagation();
                          void deleteDraft(d);
                        }}
                        style={{
                          flexShrink: 0,
                           width: 44,
                          border: 'none',
                          background: 'transparent',
                           color: 'var(--theme-danger, #c43d34)',
                          cursor: uploading ? 'not-allowed' : 'pointer',
                          fontSize: '0.95rem',
                          opacity: uploading ? 0.45 : 0.75,
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
          <div className="mobile-documents-preview-pane" data-testid="mobile-documents-preview" style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0, minHeight: 0 }}>
            <div
              className="mobile-documents-preview-header"
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 8,
                 padding: '18px 24px',
                 borderBottom: '1px solid var(--theme-border-subtle, #e8eef5)',
              }}
            >
              <div className="mobile-documents-preview-title" style={{ flex: 1, minWidth: 0, fontWeight: 750, fontSize: '1.25rem', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {selected ? selected.title || selected.id : t('Preview', '预览')}
              </div>
              {selected ? (
                isAudioItem(selected) ? (
                  <>
                    <button type="button" className="mobile-documents-btn mobile-documents-btn--primary" style={styles.btnPrimary} onClick={() => void processAudio(selected)} disabled={uploading || isProcessingAudio(selected) || hasMeetingMinutes(selected) || !selected.audio?.available}>
                      {isProcessingAudio(selected) ? t('Processing…', '处理中…') : hasMeetingMinutes(selected) ? t('Meeting minutes ready', '会议纪要已生成') : selected.processing?.status === 'failed' ? t('Retry meeting minutes', '重试生成纪要') : t('Generate meeting minutes', '生成会议纪要')}
                    </button>
                    {selected.audio?.available ? <><button type="button" className="mobile-documents-btn" style={styles.btn} onClick={() => void openAudio(selected)} disabled={uploading}>{t('Open audio', '打开音频')}</button><button type="button" className="mobile-documents-btn" style={styles.btn} onClick={() => void saveAudio(selected)} disabled={uploading}>{t('Save audio', '保存音频')}</button></> : null}
                    <button
                      className="mobile-documents-btn mobile-documents-btn--danger"
                      type="button"
                       style={{ ...styles.btn, borderColor: 'color-mix(in srgb, var(--theme-danger, #c43d34) 45%, var(--theme-border, #d9e1ec))', color: 'var(--theme-danger, #c43d34)' }}
                      onClick={() => void deleteDraft(selected)}
                      disabled={uploading}
                    >
                      {libraryDeleteButtonCopy(selected, t).title}
                    </button>
                  </>
                ) : <>
                   <button type="button" className="mobile-documents-btn" style={styles.btn} onClick={() => void copyBody()} disabled={!selected.markdown && !selected.preview}>
                    {t('Copy', '复制')}
                  </button>
                  {selected.has_original ? (
                    <>
                      <button
                        className="mobile-documents-btn"
                        type="button"
                        style={styles.btn}
                        onClick={() => void openSelectedOriginal()}
                        disabled={uploading}
                        title={t('Open the original uploaded file', '用系统默认程序打开原件')}
                      >
                        {t('Open original', '打开原件')}
                      </button>
                      <button
                        className="mobile-documents-btn"
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
                     className="mobile-documents-btn mobile-documents-btn--primary"
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
                     className="mobile-documents-btn mobile-documents-btn--danger"
                    type="button"
                    style={{
                      ...styles.btn,
                       borderColor: 'color-mix(in srgb, var(--theme-danger, #c43d34) 45%, var(--theme-border, #d9e1ec))',
                       color: 'var(--theme-danger, #c43d34)',
                    }}
                    onClick={() => void deleteDraft(selected)}
                    disabled={uploading}
                  >
                    {libraryDeleteButtonCopy(selected, t).title}
                  </button>
                </>
              ) : null}
            </div>
            <div
              className="mobile-documents-preview-body"
              style={{
                flex: 1,
                overflow: 'auto',
                 padding: '24px 28px',
                 fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'Inter', sans-serif",
                 fontSize: '0.98rem',
                 lineHeight: 1.65,
                opacity: selected ? 1 : 0.65,
              }}
            >
              {selected ? (
                isAudioItem(selected) ? (
                  <div aria-live="polite" style={{ display: 'grid', gap: 14, fontFamily: 'inherit' }}>
                    <MeetingRecordingPlayer item={selected} />
                    <div><strong>{isProcessingAudio(selected) ? t('Processing recording', '正在处理录音') : selected.processing?.status === 'failed' ? t('Processing failed', '处理失败') : selected.derived_documents?.minutes_draft_id ? t('Meeting minutes ready', '会议纪要已生成') : t('Ready for meeting minutes', '可生成会议纪要')}</strong>{selected.processing?.message ? <div style={{ opacity: 0.7, marginTop: 4 }}>{selected.processing.message}</div> : null}{isProcessingAudio(selected) ? <div style={{ height: 6, marginTop: 10, background: 'var(--theme-border, #d9e1ec)', borderRadius: 3 }}><div style={{ width: `${Math.max(4, Math.min(100, Number(selected.processing?.progress || 0)))}%`, height: '100%', background: 'var(--theme-primary, #2f6fbc)', borderRadius: 3 }} /></div> : null}</div>
                    {(selected.derived_documents?.transcript_draft_id || selected.derived_documents?.minutes_draft_id) ? <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>{selected.derived_documents?.transcript_draft_id ? <button type="button" style={styles.btn} onClick={() => void openDocumentFromAudio(selected.derived_documents?.transcript_draft_id)}>{t('Open transcript', '打开逐字稿')}</button> : null}{selected.derived_documents?.minutes_draft_id ? <button type="button" style={styles.btnPrimary} onClick={() => void openDocumentFromAudio(selected.derived_documents?.minutes_draft_id)}>{t('Open meeting minutes', '打开会议纪要')}</button> : null}</div> : null}
                    {selected.retention_until ? <div style={{ opacity: 0.58, fontSize: '0.78rem' }}>{t('Original audio retention until', '原始音频保留至')} {formatUpdatedAt(selected.retention_until, isZh)}</div> : null}
                  </div>
                ) : <MobileDocPreviewBody markdown={selected.markdown || selected.preview || ''} emptyLabel={t('Content preview is not supported', '不支持内容预览')} />
              ) : (
                t('Select a draft on the left, or drop original files above to share with Mobile.', '请选择左侧文稿，或将原始文件拖到上方以分享到手机。')
              )}
            </div>
            {selected?.id ? (
              <div
                className="mobile-documents-preview-footer"
                style={{
                  padding: '8px 14px',
                   borderTop: '1px solid var(--theme-border-subtle, #e8eef5)',
                   fontSize: '0.78rem',
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
  );

  if (inline) {
    return (
      <div
        role="region"
        aria-label={t('Mobile documents', '移动文稿库')}
        data-testid="mobile-documents-inline"
        className="mobile-documents-inline"
        data-ai-theme={appTheme}
        data-ai-dark-scheme={appDarkScheme}
        data-ai-light-scheme={appLightScheme}
        style={{ display: 'flex', flexDirection: 'column', flex: 1, minHeight: 0, width: '100%', height: '100%' }}
      >
        {shell}
      </div>
    );
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={t('Mobile documents', '移动文稿库')}
      data-testid="mobile-documents-panel"
      className="mobile-documents-overlay"
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
      {shell}
    </div>
  );
}
