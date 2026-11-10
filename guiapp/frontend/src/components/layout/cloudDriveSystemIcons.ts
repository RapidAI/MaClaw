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
/**
 * How many lookups may be in flight toward the host. The shell itself runs
 * one at a time; the extra call is already waiting, so the next type starts
 * as soon as the shell is free without opening a call per row.
 */
const MAX_CONCURRENT_LOOKUPS = 2;

const resolvedIcons = new Map<string, string>();
const iconFlights = new Map<string, Promise<CloudDriveIconLookup>>();
const ensureFlights = new Map<string, Promise<CloudDriveIconLookup>>();
const absentIcons = new Set<string>();
const failedAttempts = new Map<string, number>();
const iconListeners = new Map<string, Set<() => void>>();

type LookupWaiter = {
  key: string;
  epoch: number;
  settle: (granted: boolean) => void;
};

const lookupWaiters: LookupWaiter[] = [];
let activeLookups = 0;
let lookupEpoch = 0;

function acquireLookup(key: string, epoch: number): Promise<boolean> {
  if (epoch !== lookupEpoch) return Promise.resolve(false);
  // A free slot still waits its turn when someone is already queued, so a new
  // ask cannot cut in front of the list.
  if (activeLookups < MAX_CONCURRENT_LOOKUPS && lookupWaiters.length === 0) {
    activeLookups += 1;
    return Promise.resolve(true);
  }
  return new Promise((resolve) => {
    lookupWaiters.push({
      key,
      epoch,
      settle: (granted) => {
        if (!granted || epoch !== lookupEpoch) {
          resolve(false);
          return;
        }
        activeLookups += 1;
        resolve(true);
      },
    });
  });
}

function grantNextLookup(): void {
  while (activeLookups < MAX_CONCURRENT_LOOKUPS && lookupWaiters.length > 0) {
    const next = lookupWaiters.shift();
    if (!next) return;
    if (next.epoch !== lookupEpoch) {
      next.settle(false);
      continue;
    }
    next.settle(true);
  }
}

function releaseLookup(): void {
  if (activeLookups > 0) activeLookups -= 1;
  grantNextLookup();
}

/**
 * Moves these suffixes to the front of the shell queue. Types already inside
 * the shell keep running; a folder or a search can still have its icon start
 * as soon as one of those finishes, ahead of types that are no longer on screen.
 */
export function promoteCloudDriveSystemIcons(extensions: readonly string[]): void {
  if (lookupWaiters.length < 2) return;
  const index = new Map<string, number>();
  for (const extension of extensions) {
    const key = cloudDriveSystemIconKey(extension);
    if (key && !index.has(key)) index.set(key, index.size);
  }
  if (index.size === 0) return;
  const picked: LookupWaiter[][] = [];
  for (let i = 0; i < index.size; i += 1) picked.push([]);
  const rest: LookupWaiter[] = [];
  for (const waiter of lookupWaiters) {
    const at = index.get(waiter.key);
    if (at === undefined) rest.push(waiter);
    else picked[at].push(waiter);
  }
  lookupWaiters.length = 0;
  for (const group of picked) lookupWaiters.push(...group);
  lookupWaiters.push(...rest);
}

const ICON_SUFFIX = /^[a-z0-9]{1,16}$/;

/**
 * ".PDF" and "pdf" are one association. A blank string, a date fragment
 * (".05"), or a title ("05 纪要") is not a file type, so the shell is not asked.
 */
export function cloudDriveSystemIconKey(extension: string): string {
  const key = extension.trim().toLowerCase();
  if (!key || key === '.') return '';
  const dotted = key.charAt(0) === '.' ? key : `.${key}`;
  const body = dotted.slice(1);
  if (!ICON_SUFFIX.test(body) || !/[a-z]/.test(body)) return '';
  return dotted;
}

function publishCloudDriveSystemIcon(extension: string): void {
  const listeners = iconListeners.get(extension);
  if (!listeners) return;
  // Copy: a listener that unsubscribes must not skip the one after it.
  for (const listener of [...listeners]) {
    try {
      listener();
    } catch {
      // One card failing to paint must not drop the others, or reject the lookup.
    }
  }
}

const subscribeNothing: (listener: () => void) => () => void = () => () => {};

/**
 * Cards of one suffix share its answer. The returned subscribe ignores every
 * other suffix, so a PDF icon does not wake the document rows.
 */
export function subscribeCloudDriveSystemIcon(extension: string): (listener: () => void) => () => void {
  const key = cloudDriveSystemIconKey(extension);
  if (!key) return subscribeNothing;
  return (listener) => {
    let bucket = iconListeners.get(key);
    if (!bucket) {
      bucket = new Set();
      iconListeners.set(key, bucket);
    }
    bucket.add(listener);
    return () => {
      const current = iconListeners.get(key);
      if (!current) return;
      current.delete(listener);
      if (current.size === 0 && iconListeners.get(key) === current) iconListeners.delete(key);
    };
  };
}

/** The cached icon for a suffix, or "" while it is unknown or absent. */
export function cloudDriveSystemIconURL(extension: string): string {
  return peekCloudDriveSystemIcon(extension) ?? '';
}

/** True once this suffix has an icon or a definitive "no association". */
export function cloudDriveSystemIconSettled(extension: string): boolean {
  const key = cloudDriveSystemIconKey(extension);
  if (!key) return true;
  return resolvedIcons.has(key) || absentIcons.has(key);
}

export function resetCloudDriveSystemIconCacheForTests(): void {
  lookupEpoch += 1;
  activeLookups = 0;
  const dropped = lookupWaiters.splice(0);
  const watched = [...iconListeners.keys()];
  resolvedIcons.clear();
  iconFlights.clear();
  ensureFlights.clear();
  absentIcons.clear();
  failedAttempts.clear();
  for (const waiter of dropped) waiter.settle(false);
  for (const key of watched) publishCloudDriveSystemIcon(key);
}

/**
 * Drops a cached icon that the browser could not decode. The type stays marked
 * as absent so a corrupt answer cannot restart the lookup on every render.
 */
export function forgetCloudDriveSystemIcon(extension: string): void {
  const key = cloudDriveSystemIconKey(extension);
  if (!key) return;
  resolvedIcons.delete(key);
  absentIcons.add(key);
  publishCloudDriveSystemIcon(key);
}

/** Cached icon for a suffix, or null while it is unknown. */
export function peekCloudDriveSystemIcon(extension: string): string | null {
  const key = cloudDriveSystemIconKey(extension);
  if (!key) return null;
  return resolvedIcons.get(key) ?? null;
}

/** Resolves the host icon for a suffix. Never rejects. */
export function loadCloudDriveSystemIcon(extension: string): Promise<CloudDriveIconLookup> {
  const key = cloudDriveSystemIconKey(extension);
  if (!key) return Promise.resolve(ABSENT);
  const ready = resolvedIcons.get(key);
  if (ready) return Promise.resolve({ state: 'icon', url: ready });
  if (absentIcons.has(key)) return Promise.resolve(ABSENT);
  const flying = iconFlights.get(key);
  if (flying) return flying;

  const epoch = lookupEpoch;
  const flight = limitedLookup(key, epoch);
  iconFlights.set(key, flight);
  void flight.then((result) => {
    // A test reset may have replaced this entry. A lookup cancelled by that
    // reset must not be stored as "no association".
    if (epoch !== lookupEpoch || iconFlights.get(key) !== flight) return;
    iconFlights.delete(key);
    if (result.state === 'icon') {
      // A decode failure that landed while this lookup was in flight wins.
      // Putting the icon back would repaint the broken image.
      if (absentIcons.has(key)) return;
      resolvedIcons.set(key, result.url);
      failedAttempts.delete(key);
      publishCloudDriveSystemIcon(key);
      return;
    }
    if (result.state === 'absent') {
      absentIcons.add(key);
      return;
    }
    const attempts = (failedAttempts.get(key) ?? 0) + 1;
    // Give up after the budget so a host that always errors (no icon
    // mechanism at all) is not asked once per row.
    if (attempts >= MAX_ATTEMPTS) absentIcons.add(key);
    else failedAttempts.set(key, attempts);
  });
  return flight;
}

/**
 * One ask for a suffix. A shell fault is retried until the budget is spent,
 * then the type stays absent. Rows that ask together share that whole retry,
 * not only the single lookup already in flight.
 */
export function ensureCloudDriveSystemIcon(extension: string): Promise<CloudDriveIconLookup> {
  const key = cloudDriveSystemIconKey(extension);
  if (!key) return Promise.resolve(ABSENT);
  const ready = resolvedIcons.get(key);
  if (ready) return Promise.resolve({ state: 'icon', url: ready });
  if (absentIcons.has(key)) return Promise.resolve(ABSENT);
  const flying = ensureFlights.get(key);
  if (flying) return flying;

  const epoch = lookupEpoch;
  const flight = (async () => {
    let result = await loadCloudDriveSystemIcon(key);
    for (let tries = 1; result.state === 'failed' && tries < MAX_ATTEMPTS; tries += 1) {
      if (lookupEpoch !== epoch) return ABSENT;
      result = await loadCloudDriveSystemIcon(key);
    }
    return result;
  })();
  ensureFlights.set(key, flight);
  void flight.finally(() => {
    if (ensureFlights.get(key) === flight) ensureFlights.delete(key);
  });
  return flight;
}

async function limitedLookup(key: string, epoch: number): Promise<CloudDriveIconLookup> {
  if (!await acquireLookup(key, epoch)) return ABSENT;
  try {
    // A test reset while this suffix was waiting must not hit the new host.
    if (epoch !== lookupEpoch) return ABSENT;
    return await lookupHostIcon(key);
  } finally {
    if (epoch === lookupEpoch) releaseLookup();
  }
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
