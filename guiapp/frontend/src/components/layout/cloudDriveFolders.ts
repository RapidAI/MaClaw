import { fileExtFromName } from '../preview/filePreviewKind';

/** Top-level cloud-drive folders. Always present, in this order. */
export const CLOUD_FOLDER_ORDER = ['documents', 'audio', 'video', 'other'] as const;
export type CloudFolderId = (typeof CLOUD_FOLDER_ORDER)[number];

/** Document subtypes. Only groups that contain files are shown. */
export const DOCUMENT_CATEGORY_ORDER = ['pdf', 'word', 'sheet', 'slides', 'markdown', 'text', 'web', 'latex'] as const;
export type DocumentCategoryId = (typeof DOCUMENT_CATEGORY_ORDER)[number];

export type CloudDriveItem = {
  id?: string;
  type?: string;
  title?: string;
  preview?: string;
  source_filename?: string;
  source_content_type?: string;
  audio?: { content_type?: string };
};

export type CloudDriveGroup<T> = {
  folder: CloudFolderId;
  items: T[];
  categories: Array<{ category: DocumentCategoryId; items: T[] }>;
};

const AUDIO_EXT = new Set(['.mp3', '.wav', '.ogg', '.m4a', '.aac', '.flac', '.oga', '.wma', '.opus', '.amr', '.m4b']);
const VIDEO_EXT = new Set(['.mp4', '.webm', '.mov', '.avi', '.mkv', '.m4v', '.wmv', '.mpeg', '.mpg', '.3gp']);
const IMAGE_EXT = new Set(['.png', '.jpg', '.jpeg', '.gif', '.webp', '.bmp', '.svg', '.ico', '.tif', '.tiff', '.heic', '.heif']);
const PDF_EXT = new Set(['.pdf']);
const WORD_EXT = new Set(['.doc', '.docx', '.docm', '.dot', '.dotx', '.odt', '.rtf', '.wps', '.wpt']);
const SHEET_EXT = new Set(['.xls', '.xlsx', '.xlsm', '.xlsb', '.csv', '.tsv', '.ods', '.et', '.ett']);
const SLIDES_EXT = new Set(['.ppt', '.pptx', '.pptm', '.pps', '.ppsx', '.odp', '.dps', '.dpt']);
const MARKDOWN_EXT = new Set(['.md', '.markdown']);
const WEB_EXT = new Set(['.html', '.htm']);
const LATEX_EXT = new Set(['.tex', '.latex', '.ltx']);
const TEXT_EXT = new Set(['.txt', '.log', '.text']);

const DOCUMENT_CATEGORIES = new Set<string>(DOCUMENT_CATEGORY_ORDER);
const KNOWN_EXT = new Set<string>([
  ...AUDIO_EXT, ...VIDEO_EXT, ...IMAGE_EXT, ...PDF_EXT, ...WORD_EXT, ...SHEET_EXT,
  ...SLIDES_EXT, ...MARKDOWN_EXT, ...WEB_EXT, ...LATEX_EXT, ...TEXT_EXT,
]);

/** Real source suffix wins. A title suffix counts only when it is a known type, so "2026.10.05 纪要" stays a note. */
function itemExtension(item: CloudDriveItem): string {
  const sourceExt = fileExtFromName(String(item.source_filename || ''));
  if (sourceExt) return sourceExt;
  const titleExt = fileExtFromName(String(item.title || ''));
  return titleExt && KNOWN_EXT.has(titleExt) ? titleExt : '';
}

function itemContentType(item: CloudDriveItem): string {
  return String(item.source_content_type || item.audio?.content_type || '').split(';')[0].trim().toLowerCase();
}

function categoryFromExt(ext: string): DocumentCategoryId | null {
  if (PDF_EXT.has(ext)) return 'pdf';
  if (WORD_EXT.has(ext)) return 'word';
  if (SHEET_EXT.has(ext)) return 'sheet';
  if (SLIDES_EXT.has(ext)) return 'slides';
  if (MARKDOWN_EXT.has(ext)) return 'markdown';
  if (WEB_EXT.has(ext)) return 'web';
  if (LATEX_EXT.has(ext)) return 'latex';
  if (TEXT_EXT.has(ext)) return 'text';
  return null;
}

function isDocumentCategory(value: string | null): value is DocumentCategoryId {
  return !!value && DOCUMENT_CATEGORIES.has(value);
}

/** Coarse media class from a MIME type. Document subtypes are returned as category ids. */
function classFromContentType(contentType: string): 'audio' | 'video' | 'image' | DocumentCategoryId | null {
  if (!contentType) return null;
  if (contentType.startsWith('audio/')) return 'audio';
  if (contentType.startsWith('video/')) return 'video';
  if (contentType.startsWith('image/')) return 'image';
  if (contentType === 'application/pdf' || contentType.endsWith('/pdf')) return 'pdf';
  if (
    contentType.includes('wordprocessing') ||
    contentType === 'application/msword' ||
    contentType === 'application/rtf' ||
    contentType.includes('opendocument.text')
  ) return 'word';
  if (
    contentType.includes('spreadsheet') ||
    contentType.includes('excel') ||
    contentType === 'text/csv' ||
    contentType === 'text/tab-separated-values'
  ) return 'sheet';
  if (contentType.includes('presentation') || contentType.includes('powerpoint')) return 'slides';
  if (contentType === 'text/markdown' || contentType === 'text/x-markdown') return 'markdown';
  if (contentType === 'text/html') return 'web';
  if (contentType.includes('latex') || contentType === 'application/x-tex') return 'latex';
  if (contentType === 'text/plain') return 'text';
  return null;
}

export function classifyCloudDriveItem(item: CloudDriveItem): { folder: CloudFolderId; category?: DocumentCategoryId } {
  if (item.type === 'audio') return { folder: 'audio' };

  const ext = itemExtension(item);
  const contentType = itemContentType(item);
  const fromType = classFromContentType(contentType);
  if (fromType === 'audio' || AUDIO_EXT.has(ext)) return { folder: 'audio' };
  if (fromType === 'video' || VIDEO_EXT.has(ext)) return { folder: 'video' };
  if (fromType === 'image' || IMAGE_EXT.has(ext)) return { folder: 'other' };

  const byExt = categoryFromExt(ext);
  if (byExt) return { folder: 'documents', category: byExt };
  // Generic text/plain is how desktop upload labels JSON and YAML. An unknown
  // suffix stays in Other; a specific document MIME still wins.
  if (isDocumentCategory(fromType) && (!ext || fromType !== 'text')) {
    return { folder: 'documents', category: fromType };
  }

  // Notes, transcripts, and meeting minutes often have no filename and no MIME type.
  // A nameless octet-stream is an unknown binary, not a text document.
  const type = String(item.type || '').trim();
  if (!ext && !contentType && (type === '' || type === 'document')) return { folder: 'documents', category: 'text' };
  return { folder: 'other' };
}

/** Group library rows into the four fixed folders. Document rows are split by type. */
export function groupCloudDrive<T extends CloudDriveItem>(items: readonly T[]): Array<CloudDriveGroup<T>> {
  const folders = new Map<CloudFolderId, T[]>(CLOUD_FOLDER_ORDER.map((folder) => [folder, []]));
  const categories = new Map<DocumentCategoryId, T[]>();
  for (const item of items) {
    const classified = classifyCloudDriveItem(item);
    folders.get(classified.folder)?.push(item);
    if (classified.folder !== 'documents') continue;
    const category = classified.category ?? 'text';
    const bucket = categories.get(category) ?? [];
    bucket.push(item);
    categories.set(category, bucket);
  }
  return CLOUD_FOLDER_ORDER.map((folder) => ({
    folder,
    items: folders.get(folder) ?? [],
    categories: folder === 'documents'
      ? DOCUMENT_CATEGORY_ORDER
        .filter((category) => (categories.get(category)?.length ?? 0) > 0)
        .map((category) => ({ category, items: categories.get(category) ?? [] }))
      : [],
  }));
}

const SINGLE_LATIN = /^[\u0000-\u007f]+$/;

/**
 * Filename, title, and id use substring match. `labels` match in full.
 * Chinese prefixes apply only to `prefixLabels` (type names such as "表格"),
 * so "文" does not select every file under "文档".
 */
export function cloudDriveItemMatchesQuery(
  item: CloudDriveItem,
  query: string,
  labels: readonly string[],
  prefixLabels: readonly string[] = labels,
): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  const hay = `${item.title || ''} ${item.preview || ''} ${item.source_filename || ''} ${item.id || ''}`.toLowerCase();
  if (hay.includes(q)) return true;
  const exact = labels.some((label) => label.trim().toLowerCase() === q);
  if (exact) return true;
  if (SINGLE_LATIN.test(q)) return false;
  return prefixLabels.some((label) => {
    const name = label.trim().toLowerCase();
    return name.length > 0 && name.startsWith(q);
  });
}
