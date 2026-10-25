import type { ReactNode } from 'react';
import type { CloudDriveGlyphKind } from './cloudDriveFolders';

// Cloud-drive icon set. Drawn in the same 24px / 1.7-stroke round style as
// WorkbenchIcons so the panel reads as one product. Stroke is always
// currentColor: the surrounding element owns the color (per-type tile for file
// glyphs, theme ink for toolbar glyphs).

function glyphSvg(size: number, strokeWidth: number, children: ReactNode) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={strokeWidth}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      style={{ display: 'block' }}
    >
      {children}
    </svg>
  );
}

/** Document sheet with folded corner — the base shape for file types.
 *  Raw paths only: it is composed into the same svg as the type marks below. */
function DocSheetPaths() {
  return <>
    <path d="M7 3.5h5.9a2 2 0 0 1 1.4.6l3.6 3.6a2 2 0 0 1 .6 1.4V18a2.5 2.5 0 0 1-2.5 2.5H7A2.5 2.5 0 0 1 4.5 18V6A2.5 2.5 0 0 1 7 3.5Z" />
    <path d="M13.6 3.8v3.7a1.6 1.6 0 0 0 1.6 1.6h3.3" />
  </>;
}

function GlyphFolderShape({ size }: { size: number }) {
  return glyphSvg(size, 1.7, <>
    <path d="M4 7A2.5 2.5 0 0 1 6.5 4.5h3.1a2 2 0 0 1 1.6.8l1.1 1.4h5.2A2.5 2.5 0 0 1 20 9.2V17a2.5 2.5 0 0 1-2.5 2.5h-11A2.5 2.5 0 0 1 4 17Z" />
  </>);
}

/** Marks drawn inside the doc sheet per file type. */
function DocMarks({ kind }: { kind: CloudDriveGlyphKind }) {
  switch (kind) {
    case 'word':
      return <path d="m8.2 11.5 1.2 5.2 1.8-3.8 1.8 3.8 1.2-5.2" />;
    case 'sheet':
      return <>
        <path d="M7.6 11.4h8.8" />
        <path d="M7.6 14.8h8.8" />
        <path d="M10.9 11.4v6.4" />
        <path d="M14.1 11.4v6.4" />
      </>;
    case 'markdown':
      return <>
        <path d="m7.2 14.6V9.4l2.6 2.9 2.6-2.9v5.2" />
        <path d="M16.4 9.6v4.6" />
        <path d="m14.7 12.5 1.7 1.7 1.7-1.7" />
      </>;
    case 'latex':
      return <path d="M15.6 9.2H8.9l3.2 2.8-3.2 2.8h6.7" />;
    case 'pdf':
      return <>
        <path d="M8.1 12.2h7.8" />
        <path d="M8.1 15.4h4.9" />
      </>;
    case 'text':
      return <>
        <path d="M8.1 10.9h7.8" />
        <path d="M8.1 13.6h7.8" />
        <path d="M8.1 16.3h4.9" />
      </>;
    default:
      return <path d="M8.1 12.6h7.8" />;
  }
}

/**
 * Colored per-type file badge. `data-kind` picks the tint in CSS; document
 * types share the sheet shape and differ by their inner marks.
 */
export function CloudDriveFileTypeGlyph({ kind, size = 17 }: { kind: CloudDriveGlyphKind; size?: number }) {
  let inner: ReactNode;
  if (kind === 'audio') {
    inner = <>
      <path d="M9.6 17.2V6.6l8-1.7v10.4" />
      <circle cx="7.2" cy="17.3" r="2.4" />
      <circle cx="15.2" cy="15.4" r="2.4" />
    </>;
  } else if (kind === 'video') {
    inner = <>
      <rect x="3.5" y="5.5" width="17" height="13" rx="2.4" />
      <path d="m10.3 9.2 4.4 2.8-4.4 2.8Z" />
    </>;
  } else if (kind === 'image') {
    inner = <>
      <rect x="3.5" y="5" width="17" height="14" rx="2.4" />
      <circle cx="8.9" cy="9.9" r="1.5" />
      <path d="m6.2 17.6 3.9-4a1.3 1.3 0 0 1 1.9 0l4.6 4.6" />
      <path d="m14 15.4 1.2-1.2a1.3 1.3 0 0 1 1.9 0l2.3 2.3" />
    </>;
  } else if (kind === 'web') {
    inner = <>
      <circle cx="12" cy="12" r="8.2" />
      <path d="M3.8 12h16.4" />
      <path d="M12 3.8c2.1 2.2 3.2 5 3.2 8.2s-1.1 6-3.2 8.2c-2.1-2.2-3.2-5-3.2-8.2s1.1-6 3.2-8.2Z" />
    </>;
  } else if (kind === 'slides') {
    inner = <>
      <rect x="3.5" y="4.5" width="17" height="11.4" rx="1.8" />
      <path d="m10.4 7.6 3.8 2.4-3.8 2.4Z" />
      <path d="M12 15.9v2.6" />
      <path d="M8.8 19.8c2 .9 4.4.9 6.4 0" />
    </>;
  } else {
    inner = <><DocSheetPaths /><DocMarks kind={kind} /></>;
  }
  return (
    <span className="mdoc-file-glyph" data-kind={kind} aria-hidden="true">
      {glyphSvg(size, 1.7, inner)}
    </span>
  );
}

export function GlyphCloudDrive({ size = 20 }: { size?: number }) {
  return glyphSvg(size, 1.7, <>
    <path d="M7 18.5h10a4 4 0 0 0 .6-7.95A5.5 5.5 0 0 0 6.9 9.6 4.5 4.5 0 0 0 7 18.5Z" />
    <path d="M9.5 13.5 12 11l2.5 2.5" />
    <path d="M12 11v5" />
  </>);
}

export function GlyphFolder({ size = 15 }: { size?: number }) {
  return <GlyphFolderShape size={size} />;
}

export function GlyphSearch({ size = 15 }: { size?: number }) {
  return glyphSvg(size, 1.7, <>
    <circle cx="11" cy="11" r="6.5" />
    <path d="m15.8 15.8 4.2 4.2" />
  </>);
}

export function GlyphUploadCloud({ size = 16 }: { size?: number }) {
  return glyphSvg(size, 1.7, <>
    <path d="M7 17.5h10a3.75 3.75 0 0 0 .6-7.46A5.25 5.25 0 0 0 7.1 9.1 4.2 4.2 0 0 0 7 17.5Z" />
    <path d="M12 15v-5.5" />
    <path d="m9.6 11.6 2.4-2.4 2.4 2.4" />
  </>);
}

export function GlyphDropInbox({ size = 22 }: { size?: number }) {
  return glyphSvg(size, 1.7, <>
    <path d="M4 13.5V17a2.5 2.5 0 0 0 2.5 2.5h11A2.5 2.5 0 0 0 20 17v-3.5" />
    <path d="M12 4v9.5" />
    <path d="m8.4 10 3.6 3.5L15.6 10" />
  </>);
}

export function GlyphRefresh({ size = 15 }: { size?: number }) {
  return glyphSvg(size, 1.7, <>
    <path d="M19.5 12a7.5 7.5 0 1 1-2.2-5.3" />
    <path d="M19.7 3.8v3.4h-3.4" />
  </>);
}

export function GlyphClose({ size = 15 }: { size?: number }) {
  return glyphSvg(size, 1.7, <>
    <path d="m6 6 12 12" />
    <path d="M18 6 6 18" />
  </>);
}
