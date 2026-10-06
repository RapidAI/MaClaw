import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type DragEvent, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { useDialog } from '../CustomDialog';
import { darkCodePreviewTheme, lightCodePreviewTheme } from '../ai/CodePreviewPanel';
import { StatusGlyph } from '../ai/WorkbenchIcons';
import { FilePreviewHost } from '../preview/FilePreviewHost';
import { filePreviewKindFromName, languageFromFileName, previewShouldMaterialize, rewriteMarkdownImageUrls } from '../preview/filePreviewKind';
import { consumePendingFileLibraryOpen, OPEN_FILE_LIBRARY_EVENT, peekPendingFileLibraryOpen, type FileLibraryOpenDetail } from '../../utils/fileLibraryNavigation';
import { classifyCloudDriveItem, cloudDriveItemMatchesQuery, groupCloudDrive, type CloudDriveGroup, type CloudFolderId, type DocumentCategoryId } from './cloudDriveFolders';

type ToolbarGlyphProps = { size?: number };

// Toolbar glyphs for the cloud-drive preview actions. Drawn in the same
// 24px/1.65-stroke style as WorkbenchIcons so the header reads as one product.
// Stroke is always currentColor: each button variant owns its text color and
// hover shifts, so the icon follows for free.
function toolbarGlyph(size: number, children: ReactNode) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.65}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      style={{ display: 'block', flexShrink: 0 }}
    >
      {children}
    </svg>
  );
}

function GlyphCopy({ size = 15 }: ToolbarGlyphProps) {
  return toolbarGlyph(size, <>
    <rect x="9" y="9" width="11" height="11" rx="2" />
    <path d="M5 15H4.5A1.5 1.5 0 0 1 3 13.5v-9A1.5 1.5 0 0 1 4.5 3h9A1.5 1.5 0 0 1 15 4.5V5" />
  </>);
}

function GlyphExternalOpen({ size = 15 }: ToolbarGlyphProps) {
  return toolbarGlyph(size, <>
    <path d="M14 4h6v6" />
    <path d="M20 4l-8.5 8.5" />
    <path d="M20 14.5V18a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h3.5" />
  </>);
}

function GlyphDownload({ size = 15 }: ToolbarGlyphProps) {
  return toolbarGlyph(size, <>
    <path d="M12 4v10" />
    <path d="m7 10 5 5 5-5" />
    <path d="M5 19h14" />
  </>);
}

function GlyphTrash({ size = 15 }: ToolbarGlyphProps) {
  return toolbarGlyph(size, <>
    <path d="M4 7h16" />
    <path d="M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2" />
    <path d="M6.5 7l.7 11.2A2 2 0 0 0 9.2 20h5.6a2 2 0 0 0 2-1.8L17.5 7" />
    <path d="M10 11v5" />
    <path d="M14 11v5" />
  </>);
}

function GlyphExpand({ size = 15 }: ToolbarGlyphProps) {
  return toolbarGlyph(size, <>
    <path d="M14 4h6v6" />
    <path d="M20 4l-6.5 6.5" />
    <path d="M10 20H4v-6" />
    <path d="M4 20l6.5-6.5" />
  </>);
}

function GlyphShrink({ size = 15 }: ToolbarGlyphProps) {
  return toolbarGlyph(size, <>
    <path d="M20 10h-6V4" />
    <path d="M13.5 9.5 20 4" />
    <path d="M4 14h6v6" />
    <path d="M10.5 14.5 4 20" />
  </>);
}

function GlyphPhone({ size = 15 }: ToolbarGlyphProps) {
  return toolbarGlyph(size, <>
    <rect x="7" y="3" width="10" height="18" rx="2" />
    <path d="M11 17.5h2" />
  </>);
}

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

function callMaterializeOriginal(id: string): Promise<string> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.MaterializeMobileDocumentOriginal) {
    return Promise.reject(
      new Error(
        'Desktop binding missing MaterializeMobileDocumentOriginal — rebuild GUI after pull.',
      ),
    );
  }
  return app.MaterializeMobileDocumentOriginal(id);
}

function callPreviewLocalFile(path: string): Promise<{ content?: string }> {
  const app = (window as any)?.go?.main?.App;
  if (!app?.PreviewTaskResultFile) {
    return Promise.reject(
      new Error(
        'Desktop binding missing PreviewTaskResultFile — rebuild GUI after pull.',
      ),
    );
  }
  return app.PreviewTaskResultFile(path);
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
      title: t('Remove from cloud drive', '从云盘移除'),
      aria: t(`Remove ${name} from the cloud drive`, `从云盘移除 ${name}`),
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
        `Remove “${title}” from the cloud drive? The original audio is already gone.`,
        `从云盘移除「${title}」？原始音频已不存在。`,
      ),
      heading: t('Remove from cloud drive?', '从云盘移除？'),
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
      `Delete “${title}” from the cloud drive? The phone app will no longer be able to open this file.`,
      `从云盘删除「${title}」？手机端也将无法再看到该文件。`,
    ),
    heading: t('Delete this file?', '删除该文件？'),
    confirmText: t('Delete', '删除'),
  };
}
function isProcessingAudio(item: MobileLibraryItem | null | undefined): boolean { const status = item?.processing?.status || ''; return status === 'processing' || status === 'finalizing'; }
function hasMeetingMinutes(item: MobileLibraryItem | null | undefined): boolean { return Boolean(item?.derived_documents?.minutes_draft_id); }
function formatAudioDuration(seconds?: number): string { const total = Math.max(0, Math.round(Number(seconds || 0))); const min = Math.floor(total / 60); return `${min}:${String(total % 60).padStart(2, '0')}`; }
function formatLibraryFileSize(bytes?: number): string { const value = Math.max(0, Number(bytes || 0)); if (value === 0) return '0 B'; return value < 1024 * 1024 ? `${Math.max(1, Math.round(value / 1024))} KB` : `${(value / (1024 * 1024)).toFixed(value >= 10 * 1024 * 1024 ? 0 : 1)} MB`; }
function MeetingRecordingPlayer({ item, t }: { item: MobileLibraryItem; t: (en: string, zh: string) => string }) {
  const [src, setSrc] = useState(''); const [error, setError] = useState('');
  useEffect(() => { let url = ''; let cancelled = false; setSrc(''); setError(''); if (!item.audio?.available) return () => undefined; void callGetMeetingRecordingAudio(item.id).then((payload) => { const raw = String(payload?.data_base64 || ''); if (!raw) throw new Error('empty audio'); const binary = atob(raw); const bytes = new Uint8Array(binary.length); for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i); url = URL.createObjectURL(new Blob([bytes], { type: String(payload?.content_type || item.audio?.content_type || 'audio/mp4').split(';')[0] })); if (!cancelled) setSrc(url); }).catch((e: any) => { if (!cancelled) setError(String(e?.message || e || 'load failed')); }); return () => { cancelled = true; if (url) URL.revokeObjectURL(url); }; }, [item.id, item.audio?.available, item.audio?.content_type]);
  if (!item.audio?.available) return <div>{t('Original audio is no longer available. Generated documents remain accessible.', '原始音频已不在。已生成的文档仍可打开。')}</div>;
  if (error) return <div className="mobile-documents-inline-error">{t('Unable to load embedded playback. You can still open or save the original audio.', '无法在这里播放。仍可打开或保存原始音频。')}</div>;
  if (!src) return <div>{t('Loading audio…', '正在加载音频…')}</div>;
  return <audio controls preload="metadata" src={src} className="mdoc-audio" aria-label={t('Meeting recording playback', '会议录音播放')} />;
}

function libraryPreviewTheme() {
  const dark = typeof document !== 'undefined' && document.getElementById('App')?.getAttribute('data-ai-theme') === 'dark';
  return dark ? darkCodePreviewTheme : lightCodePreviewTheme;
}

function MobileDraftFilePreview({ item, lang }: { item: MobileLibraryItem; lang: string }) {
  const isZh = lang !== 'en' && !String(lang).startsWith('en');
  const fileName = item.source_filename || `${item.title || 'document'}.md`;
  const kind = filePreviewKindFromName(fileName);
  const rawMarkdown = item.markdown || item.preview || '';
  const [absPath, setAbsPath] = useState('');
  const [content, setContent] = useState(rawMarkdown);
  const needsOriginal = Boolean(item.has_original && item.id) && previewShouldMaterialize(kind, Boolean(rawMarkdown.trim()));
  const [loading, setLoading] = useState(needsOriginal);
  const theme = libraryPreviewTheme();

  useEffect(() => {
    let cancelled = false;
    const body = item.markdown || item.preview || '';
    const name = item.source_filename || `${item.title || 'document'}.md`;
    const nextKind = filePreviewKindFromName(name);
    const wantsFile = Boolean(item.has_original && item.id) && previewShouldMaterialize(nextKind, Boolean(body.trim()));
    setAbsPath('');
    setContent(body);
    setLoading(wantsFile);

    const resolveImages = (markdown: string) => rewriteMarkdownImageUrls(markdown, async (draftId, imageId) => {
      const payload = await callGetDraftImage(draftId, imageId);
      const b64 = String(payload?.data_base64 || '').trim();
      const rawType = String(payload?.content_type || 'image/png').split(';')[0].trim().toLowerCase();
      const ct = /^image\/[a-z0-9.+-]+$/.test(rawType) ? rawType : 'image/png';
      return b64 ? `data:${ct};base64,${b64}` : '';
    });

    void (async () => {
      try {
        let path = '';
        if (wantsFile && item.id) {
          path = String(await callMaterializeOriginal(item.id) || '').trim();
        }
        let nextContent = body;
        if (path && (nextKind === 'code' || nextKind === 'text' || nextKind === 'html')) {
          try {
            const preview = await callPreviewLocalFile(path);
            if (preview?.content) nextContent = String(preview.content);
          } catch {
            // Keep Hub markdown if the original cannot be decoded as text.
          }
        }
        if (nextKind === 'office' || nextKind === 'markdown') {
          nextContent = await resolveImages(nextContent);
        }
        if (cancelled) return;
        setAbsPath(path);
        setContent(nextContent);
      } catch {
        if (!cancelled) setAbsPath('');
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [item.has_original, item.id, item.markdown, item.preview, item.source_filename, item.title]);

  if (loading) {
    return (
      <div data-testid="mobile-documents-preview-loading" className="mdoc-preview-loading">
        {isZh ? '正在加载预览…' : 'Loading preview…'}
      </div>
    );
  }

  return (
    <FilePreviewHost
      fileName={fileName}
      absPath={absPath || undefined}
      content={content}
      language={languageFromFileName(fileName)}
      theme={theme}
      lang={lang}
    />
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

// --- Shared native drop channel + cross-instance drop de-dup -------------
// Wails' runtime.OnFileDrop registers GLOBAL window listeners exactly once
// (a second call is silently ignored) and OnFileDropOff removes them for
// everyone. Two panel instances can be mounted open at the same time (files
// page + AI dialog overlay), so they must share ONE registration: listeners
// are fanned out from a single runtime callback, the last subscriber leaves
// the channel open until it is the only one left, and the drop-signature
// de-dup is shared too — otherwise the same physical drop would be published
// once per instance.
const mdpDropListeners = new Set<(paths: string[]) => void>();
let mdpDropChannelActive = false;
const mdpLastDropSig = { sig: '', at: 0 };

function mdpDropSignature(paths: string[]): string {
  return paths
    .map((p) => p.replace(/[\\/]+$/, '').split(/[\\/]/).pop() || p)
    .sort()
    .join('\n');
}

/** True (and records) when this signature may be published; false when it
 * duplicates a drop handled within the last few seconds (HTML5 channel or
 * another panel instance already published it). The window only has to cover
 * the gap between the two channels of ONE physical drop (milliseconds); a
 * short window keeps deliberate quick re-drops working. */
function mdpClaimDrop(sig: string): boolean {
  const now = Date.now();
  if (sig && mdpLastDropSig.sig === sig && now - mdpLastDropSig.at < 2000) return false;
  mdpLastDropSig.sig = sig;
  mdpLastDropSig.at = now;
  return true;
}

/** Forget the last drop signature. Called when an import run ends with zero
 * successes: without this, an immediate retry of the same file within the
 * dedup window would be silently swallowed ("dragged, nothing happened"). */
function mdpResetDropDedup(): void {
  mdpLastDropSig.sig = '';
  mdpLastDropSig.at = 0;
}

function mdpAcquireDropChannel() {
  if (mdpDropChannelActive) return;
  const runtime = (window as any)?.runtime;
  if (!runtime || typeof runtime.OnFileDrop !== 'function') return;
  mdpDropChannelActive = true;
  if (!(window as any)?.chrome?.webview?.postMessageWithAdditionalObjects) {
    // Wails resolves dropped files to paths via WebView2's
    // postMessageWithAdditionalObjects (>= 1.0.1774.30). On older runtimes
    // the native channel silently never fires; the HTML5 drop events remain
    // the only path. Surface that so a future "drop does nothing" report can
    // be triaged from the console alone.
    console.warn('[cloud-drive] native file-drop unavailable: WebView2 lacks postMessageWithAdditionalObjects; falling back to HTML5 drop events only');
  }
  // useDropTarget=true makes the Wails runtime filter callbacks by the
  // element under the drop point: only drops whose computed style carries
  // --wails-drop-target:drop (declared on the panel shell, inherited by all
  // children) are delivered. Without it the native channel would also claim
  // drops aimed at other app dropzones (AI composer, virtual repository, ...)
  // and import them into the cloud drive a second time.
  runtime.OnFileDrop((_x: number, _y: number, paths: unknown[]) => {
    const clean = (Array.isArray(paths) ? paths : [])
      .map((p) => String(p || '').trim())
      .filter(Boolean);
    if (clean.length === 0) return;
    mdpDropListeners.forEach((listener) => listener(clean));
  }, true);
}

function mdpReleaseDropChannel() {
  if (!mdpDropChannelActive) return;
  mdpDropChannelActive = false;
  const runtime = (window as any)?.runtime;
  try { runtime?.OnFileDropOff?.(); } catch { /* runtime already gone */ }
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

const CLOUD_FOLDER_LABEL: Record<CloudFolderId, [string, string]> = {
  documents: ['Documents', '文档'],
  audio: ['Audio', '音频'],
  video: ['Video', '视频'],
  other: ['Other', '其它'],
};

const DOCUMENT_CATEGORY_LABEL: Record<DocumentCategoryId, [string, string]> = {
  pdf: ['PDF', 'PDF'],
  word: ['Word', 'Word'],
  sheet: ['Spreadsheets', '表格'],
  slides: ['Presentations', '演示'],
  markdown: ['Markdown', 'Markdown'],
  text: ['Text', '文本'],
  web: ['Web', '网页'],
  latex: ['LaTeX', 'LaTeX'],
};

function filterCloudGroups(groups: Array<CloudDriveGroup<MobileLibraryItem>>, query: string) {
  const q = query.trim();
  if (!q) return groups;
  return groups.map((group) => {
    if (group.folder !== 'documents') {
      const labels = CLOUD_FOLDER_LABEL[group.folder];
      return {
        ...group,
        items: group.items.filter((item) => cloudDriveItemMatchesQuery(item, q, labels)),
      };
    }
    const categories = group.categories
      .map((category) => {
        const typeLabels = DOCUMENT_CATEGORY_LABEL[category.category];
        return {
          ...category,
          items: category.items.filter((item) => cloudDriveItemMatchesQuery(
            item,
            q,
            [...CLOUD_FOLDER_LABEL.documents, ...typeLabels],
            typeLabels,
          )),
        };
      })
      .filter((category) => category.items.length > 0);
    return {
      ...group,
      categories,
      items: categories.flatMap((category) => category.items),
    };
  });
}

/**
 * Shared Hub cloud drive with MaClaw Mobile.
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
  const [folderOpen, setFolderOpen] = useState<Record<string, boolean>>({});
  const [searchOpen, setSearchOpen] = useState<Record<string, boolean>>({});
  const [appliedSearch, setAppliedSearch] = useState('');
  const fileInputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const scrollTargetRef = useRef('');
  const dragDepth = useRef(0);
  // Latest publishPaths for the native drop channel (see mdpDropListeners).
  const publishPathsRef = useRef<(paths: string[]) => void>(() => {});
  const pendingSelectIdRef = useRef('');
  const draftsRef = useRef<MobileLibraryItem[]>([]);
  // Clicks and status polls overlap. A late library GET must not replace a newer selection.
  const selectionIdRef = useRef('');
  const selectionGenRef = useRef(0);
  draftsRef.current = drafts;
  // State updates do not take effect until the next render, so use a ref to
  // synchronously guard the destructive confirmation and request lifecycle.
  const deleteInFlightRef = useRef(false);
  // Fullscreen preview: the preview subtree stays mounted while the host div
  // below is physically moved between the embedded slot and document.body, so
  // PDF scroll/zoom survive the toggle (portal container change never remounts).
  const [previewFullscreen, setPreviewFullscreen] = useState(false);
  const previewFullscreenRef = useRef(false);
  previewFullscreenRef.current = previewFullscreen;
  const [previewHost] = useState(() => {
    const el = document.createElement('div');
    el.className = 'mdoc-preview-slot-host';
    return el;
  });

  const t = useCallback(
    (en: string, zh: string) => (isZh ? zh : en),
    [isZh],
  );

  const revealCloudItem = useCallback((item: MobileLibraryItem) => {
    const classified = classifyCloudDriveItem(item);
    const patch: Record<string, boolean> = { [classified.folder]: true };
    if (classified.folder === 'documents' && classified.category) patch[`documents:${classified.category}`] = true;
    setFolderOpen((prev) => ({ ...prev, ...patch }));
    setSearchOpen((prev) => ({ ...prev, ...patch }));
  }, []);

  const rememberLibraryItem = useCallback((full: MobileLibraryItem) => {
    setDrafts((previous) => {
      if (previous.some((item) => item.id === full.id)) return previous;
      const next = [full, ...previous];
      draftsRef.current = next;
      return next;
    });
  }, []);

  const selectLibraryItemById = useCallback(async (id: string, list: MobileLibraryItem[] = draftsRef.current) => {
    const pendingId = String(id || '').trim();
    if (!pendingId) return false;
    const previousId = selectionIdRef.current;
    const gen = ++selectionGenRef.current;
    selectionIdRef.current = pendingId;
    const stillCurrent = () => selectionGenRef.current === gen && selectionIdRef.current === pendingId;
    const found = list.find((item) => item.id === pendingId);
    if (found) {
      pendingSelectIdRef.current = '';
      revealCloudItem(found);
      setSelected(found);
      try {
        const full = await callGetLibraryItem(found.id);
        if (!stillCurrent()) return true;
        const merged = { ...found, ...full };
        revealCloudItem(merged);
        setSelected((current) => current?.id === found.id ? merged : current);
      } catch {
        // keep list row
      }
      return true;
    }
    try {
      const full = await callGetLibraryItem(pendingId);
      if (!full?.id) {
        if (stillCurrent()) {
          selectionIdRef.current = previousId;
          pendingSelectIdRef.current = pendingId;
        }
        return false;
      }
      rememberLibraryItem(full);
      if (!stillCurrent()) return true;
      pendingSelectIdRef.current = '';
      revealCloudItem(full);
      setSelected(full);
      return true;
    } catch {
      if (stillCurrent()) {
        selectionIdRef.current = previousId;
        pendingSelectIdRef.current = pendingId;
      }
      return false;
    }
  }, [rememberLibraryItem, revealCloudItem]);

  // null = request failed (keep prior list); [] = successful empty library.
  // Callers must not treat null like "no items" or they drop rows after a
  // successful mutation when the follow-up list call flakes.
  const refresh = useCallback(async (): Promise<MobileLibraryItem[] | null> => {
    setLoading(true);
    setError('');
    try {
      const [list, quotaResult] = await Promise.all([
        callListLibraryItems(200),
        callGetDocumentQuota().catch(() => null),
      ]);
      const next = Array.isArray(list) ? list : [];
      setDrafts(next);
      draftsRef.current = next;
      if (quotaResult) setQuota(quotaResult);
      const pendingId = pendingSelectIdRef.current.trim();
      if (pendingId) await selectLibraryItemById(pendingId, next);
      return next;
    } catch (e: any) {
      setError(String(e?.message || e || 'load failed'));
      // Keep the previous drafts snapshot — wiping on a transient list failure
      // made soft-delete look like "nothing left in the library".
      return null;
    } finally {
      setLoading(false);
    }
  }, [selectLibraryItemById]);

  const applyLibraryOpen = useCallback((detail?: FileLibraryOpenDetail | null) => {
    if (!detail) return;
    const id = String(detail.documentId || '').trim();
    if (detail.query != null) setFilter(detail.query);
    else if (id) setFilter('');
    if (!id) return;
    pendingSelectIdRef.current = id;
    void selectLibraryItemById(id);
  }, [selectLibraryItemById]);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    const pending = peekPendingFileLibraryOpen();
    void refresh();
    selectionGenRef.current += 1;
    selectionIdRef.current = '';
    setSelected(null);
    setBanner('');
    setJobs([]);
    setFilter(pending?.query || '');
    pendingSelectIdRef.current = pending?.documentId || '';
    dragDepth.current = 0;
    setDragOver(false);
    setPreviewFullscreen(false);
    const timer = window.setTimeout(() => {
      if (!cancelled) consumePendingFileLibraryOpen();
    }, 0);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [open, refresh]);

  useEffect(() => {
    if (!open) return;
    const onOpen = (event: Event) => {
      consumePendingFileLibraryOpen();
      applyLibraryOpen((event as CustomEvent<FileLibraryOpenDetail>).detail);
    };
    window.addEventListener(OPEN_FILE_LIBRARY_EVENT, onOpen);
    return () => window.removeEventListener(OPEN_FILE_LIBRARY_EVENT, onOpen);
  }, [applyLibraryOpen, open]);

  // Native fallback channel: with DragAndDrop.EnableFileDrop enabled, Wails
  // resolves dropped files to absolute paths natively (WebView2 postMessage
  // with additional objects) and emits "wails:file-drop". This covers cases
  // where the HTML5 drop event reaches the page without usable
  // dataTransfer.files. Registration is shared module-wide (see
  // mdpDropListeners) so several open panel instances stay consistent.
  useEffect(() => {
    if (!open) return;
    const listener = (clean: string[]) => {
      if (!mdpClaimDrop(mdpDropSignature(clean))) return;
      publishPathsRef.current(clean);
    };
    mdpDropListeners.add(listener);
    mdpAcquireDropChannel();
    return () => {
      mdpDropListeners.delete(listener);
      if (mdpDropListeners.size === 0) mdpReleaseDropChannel();
    };
  }, [open]);

  // Close only via explicit Close / Esc — not when clicking the dimmed main window.
  // Inline mode is a regular page: navigation, not Esc, leaves it.
  useEffect(() => {
    if (!open || inline) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        // Fullscreen preview consumes the first Esc: restore the embedded pane.
        if (previewFullscreenRef.current) return;
        e.preventDefault();
        e.stopPropagation();
        onClose();
      }
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [open, inline, onClose]);

  // Fullscreen preview: Esc restores the embedded pane (modal and inline modes).
  useEffect(() => {
    if (!open || !previewFullscreen) return undefined;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        setPreviewFullscreen(false);
      }
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [open, previewFullscreen]);

  const libraryGroups = useMemo(() => groupCloudDrive(drafts), [drafts]);
  const cloudGroups = useMemo(() => filterCloudGroups(libraryGroups, filter), [libraryGroups, filter]);
  const filteredCount = cloudGroups.reduce((sum, group) => sum + group.items.length, 0);
  const searchKey = filter.trim().toLowerCase();
  if (searchKey !== appliedSearch) {
    setAppliedSearch(searchKey);
    if (Object.keys(searchOpen).length > 0) setSearchOpen({});
  }
  const searching = searchKey.length > 0;
  const isFolderOpen = (id: string, count: number) => {
    if (searching) {
      if (Object.prototype.hasOwnProperty.call(searchOpen, id)) return searchOpen[id];
      return count > 0;
    }
    if (Object.prototype.hasOwnProperty.call(folderOpen, id)) return folderOpen[id];
    return id === 'documents' || count > 0;
  };
  const toggleFolder = (id: string, count: number) => {
    if (searching) {
      const current = Object.prototype.hasOwnProperty.call(searchOpen, id) ? searchOpen[id] : count > 0;
      setSearchOpen((prev) => ({ ...prev, [id]: !current }));
      return;
    }
    const current = Object.prototype.hasOwnProperty.call(folderOpen, id)
      ? folderOpen[id]
      : id === 'documents' || count > 0;
    setFolderOpen((prev) => ({ ...prev, [id]: !current }));
  };
  useEffect(() => {
    if (!open || !selected?.id) {
      scrollTargetRef.current = '';
      return;
    }
    if (scrollTargetRef.current === selected.id) return;
    const list = listRef.current;
    const row = Array.from(list?.querySelectorAll('[data-library-id]') ?? []).find(
      (node) => node.getAttribute('data-library-id') === selected.id,
    );
    if (!(list instanceof HTMLElement) || !(row instanceof HTMLElement)) return;
    const listRect = list.getBoundingClientRect();
    const rowRect = row.getBoundingClientRect();
    // A zero box means layout has not happened yet. Leave the target unset so a later open can scroll.
    if (rowRect.height === 0 && rowRect.width === 0) return;
    scrollTargetRef.current = selected.id;
    if (rowRect.top < listRect.top) list.scrollTop -= listRect.top - rowRect.top;
    else if (rowRect.bottom > listRect.bottom) list.scrollTop += rowRect.bottom - listRect.bottom;
  }, [open, selected?.id, cloudGroups, folderOpen, searchOpen]);
  const fileCountLabel = filteredCount === 1
    ? t('1 file', '1 个文件')
    : t(`${filteredCount} files`, `${filteredCount} 个文件`);

  const selectDraft = async (d: MobileLibraryItem) => {
    const gen = ++selectionGenRef.current;
    selectionIdRef.current = d.id;
    revealCloudItem(d);
    setSelected(d);
    try {
      const full = await callGetLibraryItem(d.id);
      if (selectionGenRef.current !== gen || selectionIdRef.current !== d.id) return;
      const merged = { ...d, ...full };
      revealCloudItem(merged);
      setSelected((current) => current?.id === d.id ? merged : current);
    } catch {
      // keep list row
    }
  };
  const openDocumentFromAudio = async (draftID?: string) => {
    const id = String(draftID || '').trim();
    if (!id) return;
    setFilter('');
    const opened = await selectLibraryItemById(id);
    if (!opened) setError(t('Could not open that document.', '无法打开该文档。'));
  };

  const processAudio = async (item: MobileLibraryItem) => {
    if (isProcessingAudio(item)) return;
    setUploading(true); setError(''); setBanner('');
    try {
      const updated = await callProcessMeetingRecording(item.id);
      setDrafts((previous) => previous.map((candidate) => candidate.id === item.id ? { ...candidate, ...updated } : candidate));
      setBanner(t('Meeting minutes processing started. The status will refresh automatically.', '已开始生成会议纪要，状态将自动刷新。'));
      if (selectionIdRef.current !== item.id) return;
      selectionGenRef.current += 1;
      setSelected(updated);
    } catch (e: any) { setError(String(e?.message || e || 'start meeting minutes failed')); } finally { setUploading(false); }
  };

  const openAudio = async (item: MobileLibraryItem) => {
    setUploading(true); setError('');
    try {
      const path = await callOpenMeetingRecordingAudio(item.id);
      setBanner(path ? t(`Opened original audio: ${path}`, `已打开原始音频：${path}`) : t('Opened original audio', '已打开原始音频'));
    } catch (e: any) { setError(String(e?.message || e || 'open audio failed')); } finally { setUploading(false); }
  };

  const saveAudio = async (item: MobileLibraryItem) => {
    setUploading(true); setError('');
    try {
      const path = await callSaveMeetingRecordingAudio(item.id);
      setBanner(path ? t(`Saved original audio to ${path}`, `原始音频已保存到 ${path}`) : t('Save cancelled.', '已取消保存。'));
    } catch (e: any) { setError(String(e?.message || e || 'save audio failed')); } finally { setUploading(false); }
  };

  useEffect(() => {
    if (!open || !selected || !isProcessingAudio(selected)) return undefined;
    const timer = window.setInterval(() => {
      void callGetLibraryItem(selected.id).then((updated) => {
        setDrafts((current) => current.map((item) => item.id === updated.id ? { ...item, ...updated } : item));
        if (selectionIdRef.current !== updated.id) return;
        selectionGenRef.current += 1;
        setSelected(updated);
      }).catch(() => undefined);
    }, 2500);
    return () => window.clearInterval(timer);
  }, [open, selected?.id, selected?.processing?.status]);

  type ImportEntry = {
    name: string;
    // Present only for in-page File objects: size guards run before upload.
    guardSize?: number;
    run: () => Promise<MobileDocumentDraftSummary>;
  };

  // Drops are not disabled while an upload runs (no button to disable), so two
  // runImports loops can overlap. Each run is tagged; patches and the finalize
  // block only act while their run is still the latest, otherwise a slow first
  // drop would corrupt the second drop's job list and clear its states.
  const importRunIdRef = useRef(0);

  const runImports = async (entries: ImportEntry[]) => {
    if (entries.length === 0) return;
    const runId = ++importRunIdRef.current;
    const isCurrent = () => runId === importRunIdRef.current;
    setUploading(true);
    setError('');
    setBanner('');
    setJobs(entries.map((e) => ({ name: e.name, status: 'reading' })));

    let ok = 0;
    let last: MobileDocumentDraftSummary | null = null;
    let firstError = '';
    for (let i = 0; i < entries.length; i++) {
      const entry = entries[i];
      const patch = (job: Partial<UploadJob>) => {
        if (!isCurrent()) return;
        setJobs((prev) => prev.map((j, idx) => (idx === i ? { ...j, ...job } : j)));
      };
      try {
        const maxInputBytes = 400 * 1024 * 1024;
        if (entry.guardSize != null) {
          if (entry.guardSize > maxInputBytes) {
            throw new Error(t('File is too large to compress safely', '文件过大，无法安全压缩（压缩后必须 ≤100MB）'));
          }
          if (entry.guardSize <= 0) {
            throw new Error(t('File is empty', '文件内容为空'));
          }
        }
        patch({ status: 'uploading' });
        const draft = await entry.run();
        if (!draft?.id) {
          throw new Error(t('Hub did not return a file', 'Hub 未返回文件'));
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

    if (!isCurrent()) return; // a newer drop owns jobs/banner now
    setUploading(false);
    if (ok === 0) {
      // Every import failed: re-arm the drop de-dup so an immediate retry
      // of the same file is not silently swallowed by the dedup window.
      mdpResetDropDedup();
    }
    if (ok > 0) {
      setBanner(
        t(
          `${ok} file(s) added to the cloud drive. Phone app → Documents can open them.`,
          `已添加 ${ok} 个文件到云盘。手机端「文档」可直接打开。`,
        ),
      );
      await refresh();
      if (!isCurrent()) return;
      if (last?.id) {
        const lastId = last.id;
        const listed = draftsRef.current.find((item) => item.id === lastId);
        await selectDraft(listed || last);
      }
    } else if (entries.length > 0) {
      setError(
        firstError || t('No files were imported', '没有成功导入任何文件'),
      );
    }
  };

  const publishFiles = async (files: FileList | File[]) => {
    const list = Array.from(files || []);
    if (list.length === 0) return;
    await runImports(list.map((file) => ({
      name: file.name,
      guardSize: file.size,
      run: async () => {
        // Always upload ORIGINAL bytes to Hub (path when available, else base64 blob).
        const localPath = localPathOf(file);
        if (localPath) {
          try {
            return await callImportFromPath(localPath);
          } catch (pathErr: any) {
            // Fallback: some Wails builds expose a path that cannot be read; use blob.
            try {
              const b64 = await fileToBase64(file);
              return await callImportBytes(file.name, b64);
            } catch {
              throw pathErr;
            }
          }
        }
        const b64 = await fileToBase64(file);
        return await callImportBytes(file.name, b64);
      },
    })));
  };

  const publishPaths = async (paths: string[]) => {
    const clean = (Array.isArray(paths) ? paths : []).map((p) => String(p || '').trim()).filter(Boolean);
    if (clean.length === 0) return;
    await runImports(clean.map((path) => ({
      name: path.replace(/[\\/]+$/, '').split(/[\\/]/).pop() || path,
      run: () => callImportFromPath(path),
    })));
  };
  publishPathsRef.current = (paths) => { void publishPaths(paths); };

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
        if (drop.has(current.id) || managedRecordingID(current) === key) {
          selectionGenRef.current += 1;
          selectionIdRef.current = '';
          return null;
        }
        return current;
      });
    };
    // Soft-audio delete: only the audio row goes away. Linked transcript/minutes
    // documents must stay if Hub still lists them (never pass their IDs here).
    const clearAudioRowOnly = (audioID: string) => {
      const id = String(audioID || '').trim();
      if (!id) return;
      setDrafts((current) => current.filter((item) => item.id !== id));
      setSelected((current) => {
        if (current?.id !== id) return current;
        selectionGenRef.current += 1;
        selectionIdRef.current = '';
        return null;
      });
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
          `Original audio deleted for “${title}”. Nothing left to keep in the cloud drive.`,
          `已删除「${title}」的原始音频，云盘中已无关联条目。`,
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
            ? t(`Removed “${title}” from the cloud drive`, `已从云盘移除「${title}」`)
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
            'This meeting recording was already deleted. The cloud drive has been refreshed.',
            '该会议录音已在其他位置删除，云盘已刷新。',
          ),
        );
        await refresh();
      } else if (message.includes('RECORDING_NOT_FOUND') && softDeleteAudioOnly) {
        // Soft-delete 404: only drop the audio row; do not assume result docs were wiped.
        clearAudioRowOnly(d.id);
        setBanner(
          t(
            'This meeting recording was already deleted. The cloud drive has been refreshed.',
            '该会议录音已在其他位置删除，云盘已刷新。',
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
    const files = e.dataTransfer?.files;
    if (files?.length) {
      const sig = Array.from(files).map((f) => f.name).sort().join('\n');
      if (!mdpClaimDrop(sig)) return;
      void publishFiles(files);
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
            `“${title}” is already in the cloud drive (id: ${draftId}). Open MaClaw Mobile → Documents and refresh — do not share again.`,
            `「${title}」已在云盘中（${draftId}）。请在手机端「文档」刷新查看，无需再次分享，以免产生重复。`,
          ),
        );
      } catch (e: any) {
        setError(
          String(
            e?.message ||
              e ||
              t(
                'This file is missing on the cloud drive. Drop it again to re-add it.',
                '云盘上已找不到该文件，请重新拖入添加。',
              ),
          ),
        );
        selectionGenRef.current += 1;
        selectionIdRef.current = '';
        setSelected(null);
        await refresh();
      } finally {
        setUploading(false);
      }
      return;
    }
    setBanner(
      t(
        'Select a file from the list, or drop a new file to add one.',
        '请先从列表选择文件，或拖入新文件添加。',
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

  // Mirror CustomDialog: pin theme attrs so CSS variables match #App / dark schemes.
  // Computed before the `!open` early return so the preview hooks below keep a
  // stable hook order across open toggles (modal mode unmounts its JSX).
  const appEl = typeof document !== 'undefined' ? document.getElementById('App') : null;
  const appTheme = appEl?.getAttribute('data-ai-theme') || undefined;
  const appDarkScheme = appEl?.getAttribute('data-ai-dark-scheme') || undefined;
  const appLightScheme = appEl?.getAttribute('data-ai-light-scheme') || undefined;

  // Embedded slot ref: attach the host synchronously during commit so the
  // first paint already contains the preview (no detached-node flash).
  const previewSlotNodeRef = useRef<HTMLDivElement | null>(null);
  // Focus inside the host is lost the moment the previous effect's cleanup
  // detaches it from the DOM (browsers reset focus to body), so it must be
  // captured in the cleanup and restored by the next effect run.
  const pendingPreviewFocusRef = useRef<HTMLElement | null>(null);
  const attachPreviewSlot = useCallback((node: HTMLDivElement | null) => {
    previewSlotNodeRef.current = node;
    if (node && !previewFullscreenRef.current && previewHost.parentElement !== node) {
      node.appendChild(previewHost);
    }
  }, [previewHost]);

  // Move the host between the embedded slot and document.body. document.body
  // is used for fullscreen so the pane escapes every clipped/filtered ancestor
  // (the modal overlay's backdrop-filter would otherwise become the containing
  // block for fixed positioning). useLayoutEffect avoids a fullscreen flash.
  useLayoutEffect(() => {
    if (!open) return undefined;
    const host = previewHost;
    host.classList.toggle('mdoc-preview-slot-host--fullscreen', previewFullscreen);
    if (appTheme) host.setAttribute('data-ai-theme', appTheme); else host.removeAttribute('data-ai-theme');
    if (appDarkScheme) host.setAttribute('data-ai-dark-scheme', appDarkScheme); else host.removeAttribute('data-ai-dark-scheme');
    if (appLightScheme) host.setAttribute('data-ai-light-scheme', appLightScheme); else host.removeAttribute('data-ai-light-scheme');
    const moving = previewFullscreen
      ? host.parentElement !== document.body
      : Boolean(previewSlotNodeRef.current) && host.parentElement !== previewSlotNodeRef.current;
    if (previewFullscreen) {
      // Above the page content, but layered by panel mode: an inline page's
      // fullscreen preview stays below later-opened dialogs (modals 50000+,
      // CustomDialog 120000); the modal panel's own overlay is 50000, so its
      // fullscreen pane needs 60000 to cover it.
      host.style.zIndex = inline ? '49000' : '60000';
      if (moving) document.body.appendChild(host);
    } else if (previewSlotNodeRef.current && host.parentElement !== previewSlotNodeRef.current) {
      host.style.zIndex = '';
      previewSlotNodeRef.current.appendChild(host);
    }
    const toRestore = pendingPreviewFocusRef.current;
    pendingPreviewFocusRef.current = null;
    if (toRestore && host.contains(toRestore) && document.activeElement !== toRestore) {
      toRestore.focus({ preventScroll: true });
    }
    return () => {
      const active = document.activeElement;
      if (active instanceof HTMLElement && host.contains(active)) {
        pendingPreviewFocusRef.current = active;
      }
      host.style.zIndex = '';
      if (host.parentElement) host.parentElement.removeChild(host);
    };
  }, [open, previewFullscreen, previewHost, inline, appTheme, appDarkScheme, appLightScheme]);

  if (!open) return null;

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

  const folderLabel = (id: CloudFolderId) => t(CLOUD_FOLDER_LABEL[id][0], CLOUD_FOLDER_LABEL[id][1]);
  const categoryLabel = (id: DocumentCategoryId) => t(DOCUMENT_CATEGORY_LABEL[id][0], DOCUMENT_CATEGORY_LABEL[id][1]);

  // Fullscreen toggle: shared by the document and audio preview headers.
  // Rendered as a compact icon button in the toolbar; the accessible name
  // stays 全屏/恢复 so tests and screen readers keep the same contract.
  const fullscreenToggle = (
    <button
      className="mobile-documents-btn mdoc-toolbar-btn"
      type="button"
      onClick={() => setPreviewFullscreen((value) => !value)}
      aria-label={previewFullscreen ? t('Restore', '恢复') : t('Fullscreen', '全屏')}
      title={previewFullscreen
        ? t('Restore the embedded preview', '恢复嵌入预览')
        : t('Expand the preview to fill the window', '全屏查看内容')}
    >
      {previewFullscreen ? <GlyphShrink /> : <GlyphExpand />}
    </button>
  );

  // Preview pane. Rendered embedded in the body, or hoisted into a fullscreen
  // portal (previewPortal) so the document fills the window while reading.
  // Clicks only stop propagating in fullscreen: the host lives under
  // document.body there, and stopping the native bubble keeps document-level
  // "click outside closes X" handlers from reacting to preview interaction.
  const previewPane = (
    <div
      className="mobile-documents-preview-pane mdoc-preview-pane"
      data-testid="mobile-documents-preview"
      onMouseDown={(e) => { if (previewFullscreenRef.current) e.stopPropagation(); }}
      onClick={(e) => { if (previewFullscreenRef.current) e.stopPropagation(); }}
    >
      <div
        className="mobile-documents-preview-header mdoc-preview-header"
      >
        <div className="mdoc-preview-heading">
          <div className="mobile-documents-preview-title mdoc-preview-title">
            {selected ? selected.title || selected.id : t('Preview', '预览')}
          </div>
          {selected && !isAudioItem(selected) && selected.has_original && selected.source_filename ? (
            <div className="mdoc-preview-subtitle">
              {t('Original file', '原件')} · {selected.source_filename}
              {selected.source_size ? ` · ${formatLibraryFileSize(selected.source_size)}` : ''}
            </div>
          ) : null}
        </div>
        {selected ? (
          isAudioItem(selected) ? (
            <>
              <div className="mdoc-toolbar-group">
                <button type="button" className="mobile-documents-btn mdoc-toolbar-btn mdoc-toolbar-btn--primary" onClick={() => void processAudio(selected)} disabled={uploading || isProcessingAudio(selected) || hasMeetingMinutes(selected) || !selected.audio?.available}>
                  {isProcessingAudio(selected) ? t('Processing…', '处理中…') : hasMeetingMinutes(selected) ? t('Meeting minutes ready', '会议纪要已生成') : selected.processing?.status === 'failed' ? t('Retry meeting minutes', '重试生成纪要') : t('Generate meeting minutes', '生成会议纪要')}
                </button>
                {selected.audio?.available ? <><button type="button" className="mobile-documents-btn mdoc-toolbar-btn" onClick={() => void openAudio(selected)} disabled={uploading}>{t('Open audio', '打开音频')}</button><button type="button" className="mobile-documents-btn mdoc-toolbar-btn" onClick={() => void saveAudio(selected)} disabled={uploading}>{t('Save audio', '保存音频')}</button></> : null}
              </div>
              <div className="mdoc-toolbar-divider" aria-hidden="true" />
              <div className="mdoc-toolbar-group">
                <button
                  className="mobile-documents-btn mdoc-toolbar-btn mdoc-toolbar-btn--danger"
                  type="button"
                  onClick={() => void deleteDraft(selected)}
                  disabled={uploading}
                  title={libraryDeleteButtonCopy(selected, t).title}
                  aria-label={libraryDeleteButtonCopy(selected, t).aria}
                >
                  <GlyphTrash />
                </button>
                <div className="mdoc-toolbar-divider" aria-hidden="true" />
                {fullscreenToggle}
              </div>
            </>
          ) : <>
            <div className="mdoc-toolbar-group">
              <button type="button" className="mobile-documents-btn mdoc-toolbar-btn" onClick={() => void copyBody()} disabled={!selected.markdown && !selected.preview} title={t('Copy the text content', '复制正文内容')} aria-label={t('Copy', '复制')}>
                <GlyphCopy />
                <span>{t('Copy', '复制')}</span>
              </button>
              {selected.has_original ? (
                <>
                  <div className="mdoc-toolbar-divider" aria-hidden="true" />
                  <button
                    className="mobile-documents-btn mdoc-toolbar-btn"
                    type="button"
                    onClick={() => void openSelectedOriginal()}
                    disabled={uploading}
                    title={t('Open the original uploaded file', '用系统默认程序打开原件')}
                  >
                    <GlyphExternalOpen />
                    <span>{t('Open original', '打开原件')}</span>
                  </button>
                  <button
                    className="mobile-documents-btn mdoc-toolbar-btn"
                    type="button"
                    onClick={() => void saveSelectedOriginal()}
                    disabled={uploading}
                    title={t('Save the original file to disk', '将原件另存到本地')}
                  >
                    <GlyphDownload />
                    <span>{t('Save original', '保存原件')}</span>
                  </button>
                </>
              ) : null}
            </div>
            <div className="mdoc-toolbar-divider" aria-hidden="true" />
            <button
              className="mobile-documents-btn mdoc-toolbar-btn mdoc-toolbar-btn--primary"
              type="button"
              onClick={() => void shareSelectedAgain()}
              disabled={uploading}
              title={t(
                'Already in the cloud drive — confirms share without creating a duplicate',
                '文件已在云盘中；仅确认共享，不会重复创建',
              )}
            >
              <GlyphPhone />
              <span>{t('Already on Mobile', '已共享到手机')}</span>
            </button>
            <div className="mdoc-toolbar-divider" aria-hidden="true" />
            <div className="mdoc-toolbar-group">
              <button
                className="mobile-documents-btn mdoc-toolbar-btn mdoc-toolbar-btn--danger"
                type="button"
                onClick={() => void deleteDraft(selected)}
                disabled={uploading}
                title={libraryDeleteButtonCopy(selected, t).title}
                aria-label={libraryDeleteButtonCopy(selected, t).aria}
              >
                <GlyphTrash />
              </button>
              <div className="mdoc-toolbar-divider" aria-hidden="true" />
              {fullscreenToggle}
            </div>
          </>
        ) : null}
      </div>
      <div
        className="mobile-documents-preview-body"
        style={{
          flex: 1,
          overflow: 'hidden',
           padding: selected && !isAudioItem(selected) ? 0 : '24px 28px',
           fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'Inter', sans-serif",
           fontSize: '0.98rem',
           lineHeight: 1.65,
          minHeight: 0,
          opacity: selected ? 1 : 0.65,
        }}
      >
        {selected ? (
          isAudioItem(selected) ? (
            <div aria-live="polite" className="mdoc-audio-grid">
              <MeetingRecordingPlayer item={selected} t={t} />
              <div><strong>{isProcessingAudio(selected) ? t('Processing recording', '正在处理录音') : selected.processing?.status === 'failed' ? t('Processing failed', '处理失败') : selected.derived_documents?.minutes_draft_id ? t('Meeting minutes ready', '会议纪要已生成') : t('Ready for meeting minutes', '可生成会议纪要')}</strong>{selected.processing?.message ? <div className="mdoc-audio-message">{selected.processing.message}</div> : null}{isProcessingAudio(selected) ? <div className="mdoc-audio-track"><div style={{ width: `${Math.max(4, Math.min(100, Number(selected.processing?.progress || 0)))}%`, height: '100%', background: 'var(--theme-primary, #2f6fbc)', borderRadius: 3 }} /></div> : null}</div>
              {(selected.derived_documents?.transcript_draft_id || selected.derived_documents?.minutes_draft_id) ? <div className="mdoc-audio-docs">{selected.derived_documents?.transcript_draft_id ? <button type="button" className="mobile-documents-btn mdoc-toolbar-btn" onClick={() => void openDocumentFromAudio(selected.derived_documents?.transcript_draft_id)}>{t('Open transcript', '打开逐字稿')}</button> : null}{selected.derived_documents?.minutes_draft_id ? <button type="button" className="mobile-documents-btn mdoc-toolbar-btn mdoc-toolbar-btn--primary" onClick={() => void openDocumentFromAudio(selected.derived_documents?.minutes_draft_id)}>{t('Open meeting minutes', '打开会议纪要')}</button> : null}</div> : null}
              {selected.retention_until ? <div className="mdoc-retention">{t('Original audio retention until', '原始音频保留至')} {formatUpdatedAt(selected.retention_until, isZh)}</div> : null}
            </div>
          ) : <MobileDraftFilePreview key={selected.id} item={selected} lang={lang} />
        ) : (
          t('Select a file on the left, or drop files above to add them to the cloud drive.', '请选择左侧文件，或将文件拖到上方以加入云盘。')
        )}
      </div>
      {selected?.id ? (
        <div
          className="mobile-documents-preview-footer mdoc-preview-footer"
        >
          ID: {selected.id}
          {selected.has_original && selected.source_filename
            ? ` · ${t('original', '原件')}: ${selected.source_filename}`
            : ''}
        </div>
      ) : null}
    </div>
  );

  const previewPortal = createPortal(previewPane, previewHost);

  const renderLibraryRow = (d: MobileLibraryItem) => {
    const active = selected?.id === d.id;
    const deleteCopy = libraryDeleteButtonCopy(d, t);
    return (
      <div
        className={`mobile-documents-list-row${active ? ' is-active' : ''}`}
        key={d.id}
        data-library-id={d.id}
        style={{
          display: 'flex',
          alignItems: 'stretch',
          borderBottom: '1px solid var(--theme-border-subtle, #e8eef5)',
          borderLeft: active ? '3px solid var(--theme-primary, #2f6fbc)' : '3px solid transparent',
          background: active
            ? 'color-mix(in srgb, var(--theme-primary, #2f6fbc) 10%, var(--theme-surface, #ffffff))'
            : 'transparent',
        }}
      >
        <button
          className="mobile-documents-list-item mdoc-list-item"
          type="button"
          onClick={() => void selectDraft(d)}
        >
          <div className="mobile-documents-list-item-title mdoc-item-title">
            {d.title || d.id}
          </div>
          <div className="mobile-documents-list-item-meta mdoc-item-meta">
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
            {d.updated_at ? ` · ${formatUpdatedAt(d.updated_at, isZh)}` : ''}
          </div>
          {d.preview ? (
            <div className="mobile-documents-list-item-preview mdoc-item-preview">
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
  };

  const renderFolderToggle = (id: string, label: string, count: number, nested = false) => {
    const open = isFolderOpen(id, count);
    return (
      <button
        type="button"
        className={`mdoc-folder-toggle${nested ? ' mdoc-folder-toggle--nested' : ''}`}
        aria-expanded={open}
        data-testid={`cloud-folder-${id}`}
        onClick={() => toggleFolder(id, count)}
      >
        <span className={`mdoc-folder-chevron${open ? ' is-open' : ''}`} aria-hidden="true">
          <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
            <path d="M4 2.5 8 6 4 9.5" />
          </svg>
        </span>
        <span className="mdoc-folder-name">{label}</span>
        <span className="mdoc-folder-count">{count}</span>
      </button>
    );
  };

  // --wails-drop-target marks this shell as the Wails native file-drop target
  // (see mdpAcquireDropChannel). The custom property inherits to every child,
  // so the runtime's elementFromPoint check passes anywhere inside the panel;
  // overlaying app surfaces (AI dialog, composer, ...) are separate DOM
  // subtrees and stay excluded.
  const dropTargetStyle = { '--wails-drop-target': 'drop' } as CSSProperties;
  const shellStyle: CSSProperties = {
    ...(inline
      ? { ...styles.shell, width: '100%', height: '100%', borderRadius: 0, border: 'none', boxShadow: 'none' }
      : styles.shell),
    ...dropTargetStyle,
  };

  const shell = (
      <div
        className="mobile-documents-shell"
        style={shellStyle}
        onMouseDown={(e) => e.stopPropagation()}
        onClick={(e) => e.stopPropagation()}
      >
        <div style={styles.header} className="mobile-documents-header">
          <div
            className="mobile-documents-icon mdoc-icon-tile"
            aria-hidden
          >
            <svg width="30" height="30" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">
              <path d="M6.5 17.5h11a3.5 3.5 0 0 0 .2-7 5.5 5.5 0 0 0-10.6 1.5A3.2 3.2 0 0 0 6.5 17.5Z" />
            </svg>
          </div>
          <div className="mobile-documents-heading mdoc-flex-fill">
            <div className="mobile-documents-title mdoc-title">
              {t('Cloud drive', '云盘')}
            </div>
            <div className="mobile-documents-subtitle mdoc-subtitle">
              {t(
                'Shared cloud drive with the phone app. Drop files of any type here.',
                '与手机端共用的云盘。可将任意格式文件拖入此处。',
              )}
            </div>
          </div>
          <div className="mobile-documents-header-actions mdoc-header-actions">
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
          aria-label={t('Cloud drive upload drop zone', '云盘上传拖放区')}
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
          <div className="mobile-documents-dropzone-copy mdoc-flex-fill">
            <div className="mobile-documents-dropzone-title mdoc-dropzone-title">
              {dragOver
                ? t('Release to share with Mobile', '松开以上传并分享到手机')
                : t('Drag & drop files to share', '拖放文件到此处分享')}
            </div>
            <div className="mobile-documents-dropzone-help mdoc-dropzone-help">
              {t(
                'Any file type. Max 100MB after automatic compression; existing archives and DOCX/XLSX/PPTX are not recompressed.',
                '支持任意格式；自动压缩后单文件 ≤100MB。压缩包及 DOCX/XLSX/PPTX 不重复压缩。',
              )}
            </div>
            {quota ? (
              <div className="mobile-documents-quota mdoc-quota" aria-label={t('Cloud drive storage', '云盘存储空间')}>
                <div className="mobile-documents-quota-copy mdoc-quota-copy">
                  <span>{t('Used', '已用')} {formatLibraryFileSize(quota.document_quota_used_bytes)}</span>
                  <span>{t('Remaining', '剩余')} {formatLibraryFileSize(quota.document_quota_remaining)}</span>
                  <span>{t('Total', '总限额')} {formatLibraryFileSize(quota.document_quota_bytes)}</span>
                </div>
                <div className="mobile-documents-quota-track mdoc-quota-track">
                  <div style={{ height: '100%', width: `${Math.min(100, Math.max(0, 100 * Number(quota.document_quota_used_bytes || 0) / Math.max(1, Number(quota.document_quota_bytes || 1))))}%`, background: 'var(--theme-primary, #2f6fbc)' }} />
                </div>
              </div>
            ) : null}
          </div>
          <input
            ref={fileInputRef}
            type="file"
            multiple
            className="mdoc-hidden-input"
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
            className="mobile-documents-banner mdoc-banner"
            role="status"
            aria-live="polite"
          >
            {banner}
          </div>
        ) : null}
        {error ? (
          <div
            className="mobile-documents-error mdoc-error"
            role="alert"
          >
            {error}
          </div>
        ) : null}
        {jobs.length > 0 ? (
          <div className="mobile-documents-jobs mdoc-jobs" aria-live="polite" aria-label={t('Upload progress', '上传进度')}>
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
                <span className="mdoc-job-chip">
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

        <div className="mobile-documents-body mdoc-body" data-testid="mobile-documents-body">
          {/* List */}
          <div
            className="mobile-documents-list-pane mdoc-list-pane"
          >
            <div className="mobile-documents-list-tools mdoc-list-tools">
              <input
                className="mobile-documents-search mdoc-search"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder={t('Search files…', '搜索文件…')}
              />
              <div className="mobile-documents-count mdoc-count">
                {fileCountLabel}
                {drafts.length >= 200 ? t(' · latest 200', ' · 仅最近 200 个') : ''}
              </div>
            </div>
            <div className="mobile-documents-list mdoc-list" data-testid="mobile-documents-list" ref={listRef}>
              {loading && drafts.length === 0 ? (
                <div className="mdoc-list-loading">{t('Loading…', '加载中…')}</div>
              ) : searching && filteredCount === 0 ? (
                <div className="mobile-documents-empty mdoc-empty">{t('No matching files', '没有匹配的文件')}</div>
              ) : (
                cloudGroups.map((group) => {
                  if (searching && group.items.length === 0) return null;
                  const open = isFolderOpen(group.folder, group.items.length);
                  return (
                    <section key={group.folder} className="mdoc-folder">
                      {renderFolderToggle(group.folder, folderLabel(group.folder), group.items.length)}
                      {open && group.items.length === 0 ? (
                        <div className="mdoc-folder-empty">{t('This folder is empty', '此文件夹为空')}</div>
                      ) : null}
                      {open && group.items.length > 0 && (group.folder !== 'documents' || group.categories.length === 0) ? (
                        <div className="mdoc-folder-files">
                          {group.items.map(renderLibraryRow)}
                        </div>
                      ) : null}
                      {open && group.folder === 'documents'
                        ? group.categories.map((category) => {
                          const categoryId = `documents:${category.category}`;
                          return (
                            <div key={category.category}>
                              {renderFolderToggle(categoryId, categoryLabel(category.category), category.items.length, true)}
                              {isFolderOpen(categoryId, category.items.length) ? (
                                <div className="mdoc-folder-files mdoc-folder-files--deep">
                                  {category.items.map(renderLibraryRow)}
                                </div>
                              ) : null}
                            </div>
                          );
                        })
                        : null}
                    </section>
                  );
                })
              )}
            </div>
          </div>

          {/* Preview slot: the portal host div is attached here when embedded
              and moved to document.body when fullscreen (keeps preview mounted). */}
          <div
            ref={attachPreviewSlot}
            className="mobile-documents-preview-slot mdoc-preview-slot"
            data-testid="mobile-documents-preview-slot"
          />
        </div>
      </div>
  );

  if (inline) {
    return (
      <>
        <div
          role="region"
          aria-label={t('Cloud drive', '云盘')}
          data-testid="mobile-documents-inline"
          className="mobile-documents-inline mdoc-inline-root"
          data-ai-theme={appTheme}
          data-ai-dark-scheme={appDarkScheme}
          data-ai-light-scheme={appLightScheme}
        >
          {shell}
        </div>
        {previewPortal}
      </>
    );
  }

  return (
    <>
      <div
        role="dialog"
        aria-modal="true"
        aria-label={t('Cloud drive', '云盘')}
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
      {previewPortal}
    </>
  );
}
