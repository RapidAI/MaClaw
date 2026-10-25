import { useEffect, useState } from 'react';
import { cloudDriveGlyphKind, cloudDriveIconExtension, type CloudDriveItem } from './cloudDriveFolders';
import { CloudDriveFileTypeGlyph } from './cloudDriveGlyphs';
import { forgetCloudDriveSystemIcon, loadCloudDriveSystemIcon, peekCloudDriveSystemIcon } from './cloudDriveSystemIcons';

type ResolvedIcon = { extension: string; url: string };

/**
 * Row icon for a cloud-drive file: the operating system's icon for the type
 * when the host can supply one, otherwise the drawn glyph. Both occupy the same
 * 34px box, so a list mixing the two stays aligned.
 */
export function CloudDriveFileIcon({ item }: { item: CloudDriveItem }) {
  // A recording is not a file the user opens from disk, and its container
  // suffix (audio/mp4 arrives as .mp4) would point at a video association.
  const extension = item.type === 'audio' ? '' : cloudDriveIconExtension(item);
  const [resolved, setResolved] = useState<ResolvedIcon | null>(() => {
    const known = peekCloudDriveSystemIcon(extension);
    return known ? { extension, url: known } : null;
  });
  // The suffix is part of the state, so a row that switches type never paints
  // the previous type's icon while the new lookup is in flight.
  const iconURL = resolved && resolved.extension === extension ? resolved.url : null;

  useEffect(() => {
    if (!extension) return;
    const known = peekCloudDriveSystemIcon(extension);
    if (known) {
      // Keep the same object when nothing changed so the row does not re-render.
      setResolved((current) => (current?.extension === extension && current.url === known ? current : { extension, url: known }));
      return;
    }
    let active = true;
    void loadCloudDriveSystemIcon(extension).then((result) => {
      // No icon for this type: keep the drawn glyph rather than an empty box.
      if (active && result.state === 'icon') setResolved({ extension, url: result.url });
    });
    return () => { active = false; };
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
          setResolved(null);
        }}
      />
    </span>
  );
}
