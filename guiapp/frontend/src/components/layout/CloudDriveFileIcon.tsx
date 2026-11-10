import { useCallback, useEffect, useSyncExternalStore } from 'react';
import { cloudDriveGlyphKind, cloudDriveIconExtension, type CloudDriveItem } from './cloudDriveFolders';
import { CloudDriveFileTypeGlyph } from './cloudDriveGlyphs';
import { cloudDriveSystemIconSettled, cloudDriveSystemIconURL, ensureCloudDriveSystemIcon, forgetCloudDriveSystemIcon, subscribeCloudDriveSystemIcon } from './cloudDriveSystemIcons';

/**
 * Row icon for a cloud-drive file: the operating system's icon for the type
 * when the host can supply one, otherwise the drawn glyph. Both occupy the same
 * 34px box, so a list mixing the two stays aligned.
 */
export function CloudDriveFileIcon({ item }: { item: CloudDriveItem }) {
  // A recording is not a file the user opens from disk, and its container
  // suffix (audio/mp4 arrives as .mp4) would point at a video association.
  const extension = item.type === 'audio' ? '' : cloudDriveIconExtension(item);
  // The snapshot is this row's suffix only. A row that switches type reads the
  // new suffix on the same render, so the previous type's icon cannot linger.
  const subscribe = useCallback(
    (onStoreChange: () => void) => subscribeCloudDriveSystemIcon(extension)(onStoreChange),
    [extension],
  );
  const iconURL = useSyncExternalStore(
    subscribe,
    () => cloudDriveSystemIconURL(extension),
    () => cloudDriveSystemIconURL(extension),
  );

  useEffect(() => {
    if (!extension || cloudDriveSystemIconSettled(extension)) return;
    void ensureCloudDriveSystemIcon(extension);
  }, [extension]);

  if (!iconURL) return <CloudDriveFileTypeGlyph kind={cloudDriveGlyphKind(item)} />;
  // data-kind stays for the row's semantics; --system drops the tinted tile it
  // would otherwise paint, since the host icon carries its own colors.
  return (
    <span className="mdoc-file-glyph mdoc-file-glyph--system" data-kind={cloudDriveGlyphKind(item)} aria-hidden="true">
      <img
        className="mdoc-file-glyph-img"
        src={iconURL}
        alt=""
        draggable={false}
        onError={() => {
          // A payload the WebView cannot decode falls back to the drawn glyph
          // instead of leaving a broken-image box in the row.
          forgetCloudDriveSystemIcon(extension);
        }}
      />
    </span>
  );
}
