/**
 * Host file icons for cloud-drive rows. The desktop shell resolves the icon the
 * operating system associates with a file type, so a PDF looks like the PDF
 * icon in Explorer instead of a glyph we drew ourselves. The lookup only needs
 * the suffix: the shell answers from the association and never opens the file,
 * so no path from the cloud drive has to exist locally.
 *
 * Icons are cached per suffix because the association is per type.
 */

/**
 * The host's answer for one suffix. "absent" is definitive and remembered for
 * the session: either no application claims the suffix, or the host has no icon
 * mechanism at all (macOS and Linux today). "failed" is a lookup that could not
 * answer — a transient shell fault must not cost the type its icon for the rest
 * of the session, so it is retried a bounded number of times.
 */
export type CloudDriveIconLookup =
  | { state: 'icon'; url: string }
  | { state: 'absent' }
  | { state: 'failed' };

const ABSENT: CloudDriveIconLookup = { state: 'absent' };
const FAILED: CloudDriveIconLookup = { state: 'failed' };

/** Retries a failed lookup this many times before treating it as absent. */
const MAX_ATTEMPTS = 3;

const resolvedIcons = new Map<string, string>();
const iconFlights = new Map<string, Promise<CloudDriveIconLookup>>();
const absentIcons = new Set<string>();
const failedAttempts = new Map<string, number>();

export function resetCloudDriveSystemIconCacheForTests(): void {
  resolvedIcons.clear();
  iconFlights.clear();
  absentIcons.clear();
  failedAttempts.clear();
}

/**
 * Drops a cached icon that the browser could not decode. The type stays marked
 * as absent so a corrupt answer cannot restart the lookup on every render.
 */
export function forgetCloudDriveSystemIcon(extension: string): void {
  resolvedIcons.delete(extension);
  absentIcons.add(extension);
}

/** Cached icon for a suffix, or null while it is unknown. */
export function peekCloudDriveSystemIcon(extension: string): string | null {
  return resolvedIcons.get(extension) ?? null;
}

/** Resolves the host icon for a suffix. Never rejects. */
export function loadCloudDriveSystemIcon(extension: string): Promise<CloudDriveIconLookup> {
  if (!extension) return Promise.resolve(ABSENT);
  const ready = resolvedIcons.get(extension);
  if (ready) return Promise.resolve({ state: 'icon', url: ready });
  if (absentIcons.has(extension)) return Promise.resolve(ABSENT);
  const flying = iconFlights.get(extension);
  if (flying) return flying;

  const flight = lookupHostIcon(extension);
  iconFlights.set(extension, flight);
  void flight.then((result) => {
    // A test reset may have replaced this entry.
    if (iconFlights.get(extension) !== flight) return;
    iconFlights.delete(extension);
    if (result.state === 'icon') {
      resolvedIcons.set(extension, result.url);
      failedAttempts.delete(extension);
      return;
    }
    if (result.state === 'absent') {
      absentIcons.add(extension);
      return;
    }
    const attempts = (failedAttempts.get(extension) ?? 0) + 1;
    // Give up after the budget so a host that always errors (no icon
    // mechanism at all) is not asked once per row.
    if (attempts >= MAX_ATTEMPTS) absentIcons.add(extension);
    else failedAttempts.set(extension, attempts);
  });
  return flight;
}

async function lookupHostIcon(extension: string): Promise<CloudDriveIconLookup> {
  // Dynamic access, like the rest of this panel: the binding is absent in
  // tests and on a desktop build that predates it. A missing binding cannot
  // appear later in this session, so it counts as absent.
  const app = (window as any)?.go?.main?.App;
  if (!app?.AIAssistantFileTypeIconDataURLIfAssociated) return ABSENT;
  try {
    // A name that never exists on disk keeps the lookup on the association
    // path; passing a real path would let the shell paint an executable's own
    // icon. The host answers "" when no application claims the suffix.
    const url = String((await app.AIAssistantFileTypeIconDataURLIfAssociated(`file${extension}`)) || '');
    return url ? { state: 'icon', url } : ABSENT;
  } catch {
    return FAILED;
  }
}
